package m3db

import (
	"fmt"
	"log"
	"sync"

	"github.com/didi/nightingale/src/common/dataobj"
	"github.com/didi/nightingale/src/toolkits/stats"
	"github.com/m3db/m3/src/dbnode/client"
	"github.com/m3db/m3/src/m3ninx/idx"
	"github.com/m3db/m3/src/x/ident"
	"github.com/toolkits/pkg/logger"
)

const (
	NID_NAME      = "__nid__"
	ENDPOINT_NAME = "__endpoint__"
	METRIC_NAME   = "__name__"
	SERIES_LIMIT  = 100
	DOCS_LIMIT    = 100
)

type M3dbSection struct {
	Name      string               `yaml:"name"`
	Enabled   bool                 `yaml:"enabled"`
	Namespace string               `yaml:"namespace"`
	Config    client.Configuration `yaml:",inline"`
}

type Client struct {
	sync.RWMutex

	client client.Client
	active client.Session
	opts   client.Options

	namespace string
	config    *client.Configuration

	namespaceID ident.ID
}

func NewClient(namespace string, cfg *client.Configuration) (*Client, error) {
	client, err := cfg.NewClient(client.ConfigurationParameters{})
	if err != nil {
		return nil, fmt.Errorf("unable to get new M3DB client: %v", err)
	}

	ret := &Client{
		namespace:   namespace,
		config:      cfg,
		client:      client,
		namespaceID: ident.StringID(namespace),
	}

	if _, err := ret.session(); err != nil {
		return nil, fmt.Errorf("unable to get new M3DB session: %v", err)
	}

	return ret, nil
}

// Push2Queue: push Metrics with values into m3.dbnode
func (p *Client) Push2Queue(items []*dataobj.MetricValue) {
	session, err := p.session()
	if err != nil {
		logger.Errorf("unable to get m3db session: %s", err)
		return
	}

	errCnt := 0
	for _, item := range items {
		if err := writeTagged(session, p.namespaceID, item); err != nil {
			logger.Errorf("unable to writeTagged: %s", err)
			errCnt++
		}
	}
	stats.Counter.Set("m3db.queue.err", errCnt)
}

// QueryData: || (|| endpoints...) (&& tags...)
func (p *Client) QueryData(inputs []dataobj.QueryData) []*dataobj.TsdbQueryResponse {
	logger.Debugf("query data, inputs: %+v", inputs)

	session, err := p.session()
	if err != nil {
		logger.Errorf("unable to get m3db session: %s", err)
		return nil
	}

	if len(inputs) == 0 {
		return nil
	}

	query, opts := queryDataOptions(inputs)
	ret, err := fetchTagged(session, p.namespaceID, query, opts)
	if err != nil {
		logger.Errorf("unable to query data: ", err)
		return nil
	}

	return ret
}

// QueryDataForUi: && (metric) (|| endpoints...) (&& tags...)
// get kv
func (p *Client) QueryDataForUI(input dataobj.QueryDataForUI) []*dataobj.TsdbQueryResponse {
	logger.Debugf("query data for ui, input: %+v", input)

	session, err := p.session()
	if err != nil {
		logger.Errorf("unable to get m3db session: %s", err)
		return nil
	}

	query, opts := queryDataUIOptions(input)

	ret, err := fetchTagged(session, p.namespaceID, query, opts)
	if err != nil {
		logger.Errorf("unable to query data for ui: %s", err)
		return nil
	}

	// TODO: groupKey, aggrFunc, consolFunc, Comparisons
	return ret
}

// QueryMetrics: || (&& (endpoint)) (counter)...
// return all the values that tag == __name__
func (p *Client) QueryMetrics(input dataobj.EndpointsRecv) *dataobj.MetricResp {
	session, err := p.session()
	if err != nil {
		logger.Errorf("unable to get m3db session: %s", err)
		return nil
	}

	query, opts := queryMetricsOptions(input)

	tags, err := completeTags(session, p.namespaceID, query, opts)
	if err != nil {
		logger.Errorf("unable completeTags: ", err)
		return nil
	}
	return tagsMr(tags)
}

// QueryTagPairs: && (|| endpoints...) (|| metrics...)
// return all the tags that matches
func (p *Client) QueryTagPairs(input dataobj.EndpointMetricRecv) []dataobj.IndexTagkvResp {
	session, err := p.session()
	if err != nil {
		logger.Errorf("unable to get m3db session: %s", err)
		return nil
	}

	query, opts := queryTagPairsOptions(input)

	tags, err := completeTags(session, p.namespaceID, query, opts)
	if err != nil {
		logger.Errorf("unable completeTags: ", err)
		return nil
	}

	return []dataobj.IndexTagkvResp{*tagsIndexTagkvResp(tags)}
}

// QueryIndexByClude:  || (&& (|| endpoints...) (metric) (|| include...) (&& exclude..))
// return all the tags that matches
func (p *Client) QueryIndexByClude(inputs []dataobj.CludeRecv) (ret []dataobj.XcludeResp) {
	session, err := p.session()
	if err != nil {
		logger.Errorf("unable to get m3db session: %s", err)
		return nil
	}

	for _, input := range inputs {
		ret = append(ret, p.queryIndexByClude(session, input)...)
	}

	return
}

func (p *Client) queryIndexByClude(session client.Session, input dataobj.CludeRecv) []dataobj.XcludeResp {
	query, opts := queryIndexByCludeOptions(input)

	iter, _, err := session.FetchTaggedIDs(p.namespaceID, query, opts)
	if err != nil {
		logger.Errorf("unable FetchTaggedIDs: ", err)
		return nil
	}

	// group by endpoint-metric
	respMap := make(map[string]dataobj.XcludeResp)
	for iter.Next() {
		_, _, tagIter := iter.Current()

		resp := xcludeResp(tagIter)
		key := fmt.Sprintf("%s-%s", resp.Endpoint, resp.Metric)
		if v, ok := respMap[key]; ok {
			if len(resp.Tags) > 0 {
				v.Tags = append(v.Tags, resp.Tags[0])
			}
		} else {
			respMap[key] = resp
		}
	}

	if err := iter.Err(); err != nil {
		logger.Errorf("FetchTaggedIDs iter:", err)
		return nil
	}

	resp := make([]dataobj.XcludeResp, 0, len(respMap))
	for _, v := range respMap {
		resp = append(resp, v)
	}

	return resp
}

// QueryIndexByFullTags: && (|| endpoints...) (metric) (&& Tagkv...)
// return all the tags that matches
func (p *Client) QueryIndexByFullTags(inputs []dataobj.IndexByFullTagsRecv) []dataobj.IndexByFullTagsResp {
	session, err := p.session()
	if err != nil {
		logger.Errorf("unable to get m3db session: %s", err)
		return nil
	}

	ret := make([]dataobj.IndexByFullTagsResp, len(inputs))
	for i, input := range inputs {
		ret[i] = p.queryIndexByFullTags(session, input)
	}

	return ret
}

func (p *Client) queryIndexByFullTags(session client.Session, input dataobj.IndexByFullTagsRecv) (ret dataobj.IndexByFullTagsResp) {
	log.Printf("entering queryIndexByFullTags")

	ret = dataobj.IndexByFullTagsResp{
		Metric: input.Metric,
		Tags:   []string{},
		Step:   10,
		DsType: "GAUGE",
	}

	query, opts := queryIndexByFullTagsOptions(input)
	if query.Query.Equal(idx.NewAllQuery()) {
		ret.Endpoints = input.Endpoints
		log.Printf("all query")
		return
	}

	iter, _, err := session.FetchTaggedIDs(p.namespaceID, query, opts)
	if err != nil {
		logger.Errorf("unable FetchTaggedIDs: ", err)
		return
	}

	ret.Endpoints = input.Endpoints
	for iter.Next() {
		log.Printf("iter.next() ")
		_, _, tagIter := iter.Current()
		resp := xcludeResp(tagIter)
		if len(resp.Tags) > 0 {
			ret.Tags = append(ret.Tags, resp.Tags[0])
		}
	}
	if err := iter.Err(); err != nil {
		logger.Errorf("FetchTaggedIDs iter:", err)
	}

	return ret

}

// GetInstance: && (metric) (endpoint) (&& tags...)
// return: backend list which store the series
func (p *Client) GetInstance(metric, endpoint string, tags map[string]string) []string {
	session, err := p.session()
	if err != nil {
		logger.Errorf("unable to get m3db session: %s", err)
		return nil
	}

	adminSession, ok := session.(client.AdminSession)
	if !ok {
		logger.Errorf("unable to get an admin session")
		return nil
	}

	tm, err := adminSession.TopologyMap()
	if err != nil {
		logger.Errorf("unable to get topologyMap with admin seesion")
		return nil
	}

	hosts := []string{}
	for _, host := range tm.Hosts() {
		hosts = append(hosts, host.Address())
	}

	return hosts
}
