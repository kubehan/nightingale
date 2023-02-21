package sender

import (
	"fmt"
	"html/template"
	"strings"
	"time"

	"github.com/toolkits/pkg/logger"

	"github.com/didi/nightingale/v5/src/models"
	"github.com/didi/nightingale/v5/src/pkg/poster"
)

type feishuContent struct {
	Text string `json:"text"`
}

type feishuAt struct {
	AtMobiles []string `json:"atMobiles"`
	IsAtAll   bool     `json:"isAtAll"`
}

type Conf struct {
	WideScreenMode bool `json:"wide_screen_mode"`
	EnableForward  bool `json:"enable_forward"`
}

type Te struct {
	Content string `json:"content"`
	Tag     string `json:"tag"`
}

type Element struct {
	Tag      string    `json:"tag"`
	Text     Te        `json:"text"`
	Content  string    `json:"content"`
	Elements []Element `json:"elements"`
}

type Titles struct {
	Content string `json:"content"`
	Tag     string `json:"tag"`
}

type Headers struct {
	Title    Titles `json:"title"`
	Template string `json:"template"`
}

type Cards struct {
	Config   Conf      `json:"config"`
	Elements []Element `json:"elements"`
	Header   Headers   `json:"header"`
}

type feishu struct {
	Msgtype string        `json:"msg_type"`
	Content feishuContent `json:"content"`
	At      feishuAt      `json:"at"`
	Email   string        `json:"email"` //@所使用字段
	Card    Cards         `json:"card"`
}

type FeishuSender struct {
	tpl *template.Template
}

func (fs *FeishuSender) Send(ctx MessageContext) {
	if len(ctx.Users) == 0 || ctx.Rule == nil || ctx.Event == nil {
		return
	}
	urls, _ := fs.extract(ctx.Users)
	message := BuildTplMessage(fs.tpl, ctx.Event)
	for _, url := range urls {
		var color string
		if strings.Count(message, "Recovered") > 0 && strings.Count(message, "Triggered") > 0 {
			color = "orange"
		} else if strings.Count(message, "Recovered") > 0 {
			color = "green"
		} else {
			color = "red"
		}
		SendTitle := fmt.Sprintf("🔔 [告警提醒] - %s", ctx.Rule.Name)
		body := feishu{
			Msgtype: "interactive",
			Card: Cards{
				Config: Conf{
					WideScreenMode: true,
					EnableForward:  true,
				},
				Header: Headers{
					Title: Titles{
						Content: SendTitle,
						Tag:     "plain_text",
					},
					Template: color,
				},
				Elements: []Element{
					Element{
						Tag: "div",
						Text: Te{
							Content: message,
							Tag:     "lark_md",
						},
					},
					{
						Tag: "hr",
					},
					{
						Tag: "note",
						Elements: []Element{
							{
								Content: SendTitle,
								Tag:     "lark_md",
							},
						},
					},
				},
			},
		}
		fs.doSend(url, body)
	}
}

func (fs *FeishuSender) SendRaw(users []*models.User, title, message string) {
	if len(users) == 0 {
		return
	}
	urls, _ := fs.extract(users)
	body := feishu{
		Msgtype: "text",
		Content: feishuContent{
			Text: message,
		},
	}
	for _, url := range urls {
		fs.doSend(url, body)
	}
}

func (fs *FeishuSender) extract(users []*models.User) ([]string, []string) {
	urls := make([]string, 0, len(users))
	ats := make([]string, 0, len(users))

	for _, user := range users {
		if user.Phone != "" {
			ats = append(ats, user.Phone)
		}
		if token, has := user.ExtractToken(models.Feishu); has {
			url := token
			if !strings.HasPrefix(token, "https://") {
				url = "https://open.feishu.cn/open-apis/bot/v2/hook/" + token
			}
			urls = append(urls, url)
		}
	}
	return urls, ats
}

func (fs *FeishuSender) doSend(url string, body feishu) {
	res, code, err := poster.PostJSON(url, time.Second*5, body, 3)
	if err != nil {
		logger.Errorf("feishu_sender: result=fail url=%s code=%d error=%v response=%s", url, code, err, string(res))
	} else {
		logger.Infof("feishu_sender: result=succ url=%s code=%d response=%s", url, code, string(res))
	}
}
