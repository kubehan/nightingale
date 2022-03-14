package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"html/template"
	"io/ioutil"
	"net/http"
	"os/exec"
	"path"
	"plugin"
	"runtime"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/tidwall/gjson"
	"github.com/toolkits/pkg/file"
	"github.com/toolkits/pkg/logger"
	"github.com/toolkits/pkg/runner"

	"github.com/didi/nightingale/v5/src/models"
	"github.com/didi/nightingale/v5/src/pkg/sys"
	"github.com/didi/nightingale/v5/src/server/config"
	"github.com/didi/nightingale/v5/src/server/memsto"
	"github.com/didi/nightingale/v5/src/server/sender"
	"github.com/didi/nightingale/v5/src/storage"
)

var tpls = make(map[string]*template.Template)

var fns = template.FuncMap{
	"unescaped":  func(str string) interface{} { return template.HTML(str) },
	"urlconvert": func(str string) interface{} { return template.URL(str) },
	"timeformat": func(ts int64, pattern ...string) string {
		defp := "2006-01-02 15:04:05"
		if len(pattern) > 0 {
			defp = pattern[0]
		}
		return time.Unix(ts, 0).Format(defp)
	},
	"timestamp": func(pattern ...string) string {
		defp := "2006-01-02 15:04:05"
		if len(pattern) > 0 {
			defp = pattern[0]
		}
		return time.Now().Format(defp)
	},
}

func initTpls() error {
	if config.C.Alerting.TemplatesDir == "" {
		config.C.Alerting.TemplatesDir = path.Join(runner.Cwd, "etc", "template")
	}

	filenames, err := file.FilesUnder(config.C.Alerting.TemplatesDir)
	if err != nil {
		return errors.WithMessage(err, "failed to exec FilesUnder")
	}

	if len(filenames) == 0 {
		return errors.New("no tpl files under " + config.C.Alerting.TemplatesDir)
	}

	tplFiles := make([]string, 0, len(filenames))
	for i := 0; i < len(filenames); i++ {
		if strings.HasSuffix(filenames[i], ".tpl") {
			tplFiles = append(tplFiles, filenames[i])
		}
	}

	if len(tplFiles) == 0 {
		return errors.New("no tpl files under " + config.C.Alerting.TemplatesDir)
	}

	for i := 0; i < len(tplFiles); i++ {
		tplpath := path.Join(config.C.Alerting.TemplatesDir, tplFiles[i])

		tpl, err := template.New(tplFiles[i]).Funcs(fns).ParseFiles(tplpath)
		if err != nil {
			return errors.WithMessage(err, "failed to parse tpl: "+tplpath)
		}

		tpls[tplFiles[i]] = tpl
	}

	return nil
}

type Notice struct {
	Event *models.AlertCurEvent `json:"event"`
	Tpls  map[string]string     `json:"tpls"`
}

func genNotice(event *models.AlertCurEvent) Notice {
	// build notice body with templates
	ntpls := make(map[string]string)
	for filename, tpl := range tpls {
		var body bytes.Buffer
		if err := tpl.Execute(&body, event); err != nil {
			ntpls[filename] = err.Error()
		} else {
			ntpls[filename] = body.String()
		}
	}

	return Notice{Event: event, Tpls: ntpls}
}

func alertingRedisPub(bs []byte) {
	// pub all alerts to redis
	if config.C.Alerting.RedisPub.Enable {
		err := storage.Redis.Publish(context.Background(), config.C.Alerting.RedisPub.ChannelKey, bs).Err()
		if err != nil {
			logger.Errorf("event_notify: redis publish %s err: %v", config.C.Alerting.RedisPub.ChannelKey, err)
		}
	}
}

func handleNotice(notice Notice, bs []byte) {
	go alertingCallScript(bs)

	go alertingCallPlugin(bs)

	if !config.C.Alerting.NotifyBuiltinEnable {
		return
	}

	emailset := make(map[string]struct{})
	phoneset := make(map[string]struct{})
	wecomset := make(map[string]struct{})
	dingtalkset := make(map[string]struct{})
	feishuset := make(map[string]struct{})

	for _, user := range notice.Event.NotifyUsersObj {
		if user.Email != "" {
			emailset[user.Email] = struct{}{}
		}

		if user.Phone != "" {
			phoneset[user.Phone] = struct{}{}
		}

		bs, err := user.Contacts.MarshalJSON()
		if err != nil {
			logger.Errorf("handle_notice: failed to marshal contacts: %v", err)
			continue
		}

		ret := gjson.GetBytes(bs, "dingtalk_robot_token")
		if ret.Exists() {
			dingtalkset[ret.String()] = struct{}{}
		}

		ret = gjson.GetBytes(bs, "wecom_robot_token")
		if ret.Exists() {
			wecomset[ret.String()] = struct{}{}
		}

		ret = gjson.GetBytes(bs, "feishu_robot_token")
		if ret.Exists() {
			feishuset[ret.String()] = struct{}{}
		}
	}

	phones := StringSetKeys(phoneset)

	for _, ch := range notice.Event.NotifyChannelsJSON {
		switch ch {
		case "email":
			if len(emailset) == 0 {
				continue
			}

			subject, has := notice.Tpls["subject.tpl"]
			if !has {
				subject = "subject.tpl not found"
			}

			content, has := notice.Tpls["mailbody.tpl"]
			if !has {
				content = "mailbody.tpl not found"
			}

			sender.WriteEmail(subject, content, StringSetKeys(emailset))
		case "dingtalk":
			if len(dingtalkset) == 0 {
				continue
			}

			content, has := notice.Tpls["dingtalk.tpl"]
			if !has {
				content = "dingtalk.tpl not found"
			}

			sender.SendDingtalk(sender.DingtalkMessage{
				Title:     notice.Event.RuleName,
				Text:      content,
				AtMobiles: phones,
				Tokens:    StringSetKeys(dingtalkset),
			})
		case "wecom":
			if len(wecomset) == 0 {
				continue
			}

			content, has := notice.Tpls["wecom.tpl"]
			if !has {
				content = "wecom.tpl not found"
			}
			sender.SendWecom(sender.WecomMessage{
				Text:   content,
				Tokens: StringSetKeys(wecomset),
			})
		case "feishu":
			if len(feishuset) == 0 {
				continue
			}

			content, has := notice.Tpls["feishu.tpl"]
			if !has {
				content = "feishu.tpl not found"
			}
			sender.SendFeishu(sender.FeishuMessage{
				Text:      content,
				AtMobiles: phones,
				Tokens:    StringSetKeys(feishuset),
			})
		default:
			logger.Info("channel ", ch, " not supported by golang")
		}
	}
}

func notify(event *models.AlertCurEvent) {
	logEvent(event, "notify")

	notice := genNotice(event)
	stdinBytes, err := json.Marshal(notice)
	if err != nil {
		logger.Errorf("event_notify: failed to marshal notice: %v", err)
		return
	}

	alertingRedisPub(stdinBytes)
	alertingWebhook(event)

	handleNotice(notice, stdinBytes)

	// handle alert subscribes
	subs, has := memsto.AlertSubscribeCache.Get(event.RuleId)
	if has {
		handleSubscribes(*event, subs)
	}

	subs, has = memsto.AlertSubscribeCache.Get(0)
	if has {
		handleSubscribes(*event, subs)
	}
}

func alertingWebhook(event *models.AlertCurEvent) {
	conf := config.C.Alerting.Webhook

	if !conf.Enable {
		return
	}

	if conf.Url == "" {
		return
	}

	bs, err := json.Marshal(event)
	if err != nil {
		return
	}

	bf := bytes.NewBuffer(bs)

	req, err := http.NewRequest("POST", conf.Url, bf)
	if err != nil {
		logger.Warning("alertingWebhook failed to new request", err)
		return
	}

	if conf.BasicAuthUser != "" && conf.BasicAuthPass != "" {
		req.SetBasicAuth(conf.BasicAuthUser, conf.BasicAuthPass)
	}

	if len(conf.Headers) > 0 && len(conf.Headers)%2 == 0 {
		for i := 0; i < len(conf.Headers); i += 2 {
			req.Header.Set(conf.Headers[i], conf.Headers[i+1])
		}
	}

	client := http.Client{
		Timeout: conf.TimeoutDuration,
	}

	var resp *http.Response
	resp, err = client.Do(req)
	if err != nil {
		logger.Warning("alertingWebhook failed to call url, error: ", err)
		return
	}

	var body []byte
	if resp.Body != nil {
		defer resp.Body.Close()
		body, _ = ioutil.ReadAll(resp.Body)
	}

	logger.Debugf("alertingWebhook done, url: %s, response code: %d, body: %s", conf.Url, resp.StatusCode, string(body))
}

func handleSubscribes(event models.AlertCurEvent, subs []*models.AlertSubscribe) {
	for i := 0; i < len(subs); i++ {
		handleSubscribe(event, subs[i])
	}
}

func handleSubscribe(event models.AlertCurEvent, sub *models.AlertSubscribe) {
	if !matchTags(event.TagsMap, sub.ITags) {
		return
	}

	if sub.RedefineSeverity == 1 {
		event.Severity = sub.NewSeverity
	}

	if sub.RedefineChannels == 1 {
		event.NotifyChannels = sub.NewChannels
		event.NotifyChannelsJSON = strings.Fields(sub.NewChannels)
	}

	event.NotifyGroups = sub.UserGroupIds
	event.NotifyGroupsJSON = strings.Fields(sub.UserGroupIds)
	if len(event.NotifyGroupsJSON) == 0 {
		return
	}

	logEvent(&event, "subscribe")

	fillUsers(&event)

	notice := genNotice(&event)
	stdinBytes, err := json.Marshal(notice)
	if err != nil {
		logger.Errorf("event_notify: failed to marshal notice: %v", err)
		return
	}

	handleNotice(notice, stdinBytes)
}

func alertingCallScript(stdinBytes []byte) {
	if !config.C.Alerting.CallScript.Enable {
		return
	}

	// no notify.py? do nothing
	if config.C.Alerting.CallScript.ScriptPath == "" {
		return
	}

	fpath := config.C.Alerting.CallScript.ScriptPath
	cmd := exec.Command(fpath)
	cmd.Stdin = bytes.NewReader(stdinBytes)

	// combine stdout and stderr
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := startCmd(cmd)
	if err != nil {
		logger.Errorf("event_notify: run cmd err: %v", err)
		return
	}

	err, isTimeout := sys.WrapTimeout(cmd, time.Duration(30)*time.Second)

	if isTimeout {
		if err == nil {
			logger.Errorf("event_notify: timeout and killed process %s", fpath)
		}

		if err != nil {
			logger.Errorf("event_notify: kill process %s occur error %v", fpath, err)
		}

		return
	}

	if err != nil {
		logger.Errorf("event_notify: exec script %s occur error: %v, output: %s", fpath, err, buf.String())
		return
	}

	logger.Infof("event_notify: exec %s output: %s", fpath, buf.String())
}

type Notifier interface {
	Descript() string
	Notify([]byte)
}

// call notify.so via golang plugin build
// ig. etc/script/notify/notify.py
func alertingCallPlugin(stdinBytes []byte) {
	if runtime.GOOS == "windows" {
		logger.Errorf("call notify plugin on unsupported os: %s", runtime.GOOS)
		return
	}
	if !config.C.Alerting.CallPlugin.Enable {
		return
	}
	p, err := plugin.Open(config.C.Alerting.CallPlugin.PluginPath)
	if err != nil {
		logger.Errorf("failed to open notify plugin: %v", err)
		return
	}
	caller, err := p.Lookup(config.C.Alerting.CallPlugin.Caller)
	if err != nil {
		logger.Errorf("failed to load caller: %v", err)
		return
	}
	notifier, ok := caller.(Notifier)
	if !ok {
		logger.Errorf("notifier interface not implemented): %v", err)
		return
	}
	notifier.Notify(stdinBytes)
	logger.Debugf("alertingCallPlugin done. %s", notifier.Descript())
}
