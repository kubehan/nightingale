package sender

import (
	"context"
	"crypto/tls"
	"errors"
	"html/template"
	"time"

	"github.com/ccfos/nightingale/v6/alert/aconf"
	"github.com/ccfos/nightingale/v6/models"

	"github.com/toolkits/pkg/logger"

	"gopkg.in/gomail.v2"
)

var mailch chan *gomail.Message

type EmailSender struct {
	subjectTpl *template.Template
	contentTpl *template.Template
	smtp       aconf.SMTPConfig
}

func (es *EmailSender) Send(ctx MessageContext) {
	if len(ctx.Users) == 0 || len(ctx.Events) == 0 {
		return
	}
	tos := extract(ctx.Users)
	var subject string

	if es.subjectTpl != nil {
		subject = BuildTplMessage(models.Email, es.subjectTpl, []*models.AlertCurEvent{ctx.Events[0]})
	} else {
		subject = ctx.Events[0].RuleName
	}
	content := BuildTplMessage(models.Email, es.contentTpl, ctx.Events)
	es.WriteEmail(subject, content, tos)

	ctx.Stats.AlertNotifyTotal.WithLabelValues(models.Email).Add(float64(len(tos)))
}

func extract(users []*models.User) []string {
	tos := make([]string, 0, len(users))
	for _, u := range users {
		if u.Email != "" {
			tos = append(tos, u.Email)
		}
	}
	return tos
}

func SendEmail(subject, content string, tos []string, stmp aconf.SMTPConfig) error {
	conf := stmp

	d := gomail.NewDialer(conf.Host, conf.Port, conf.User, conf.Pass)
	if conf.InsecureSkipVerify {
		d.TLSConfig = &tls.Config{InsecureSkipVerify: true}
	}

	m := gomail.NewMessage()

	m.SetHeader("From", stmp.From)
	m.SetHeader("To", tos...)
	m.SetHeader("Subject", subject)
	m.SetBody("text/html", content)

	err := d.DialAndSend(m)
	if err != nil {
		return errors.New("email_sender: failed to send: " + err.Error())
	}
	return nil
}

func (es *EmailSender) WriteEmail(subject, content string, tos []string) {
	m := gomail.NewMessage()

	m.SetHeader("From", es.smtp.From)
	m.SetHeader("To", tos...)
	m.SetHeader("Subject", subject)
	m.SetBody("text/html", content)

	mailch <- m
}

var retryInterval = 1 * time.Second

func dialSmtp(d *gomail.Dialer) (closer gomail.SendCloser, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return nil, errors.New("email_sender: failed to dial after 1 min, cancel")
		default:

			if s, err := d.Dial(); err != nil {
				logger.Errorf("email_sender: failed to dial smtp: %s", err)
				time.Sleep(retryInterval)
				retryInterval *= 2
			} else {
				return s, nil
			}
		}

	}
}

var mailQuit = make(chan struct{})

type emailRebooter struct{}

func (s *emailRebooter) Reset(smtp aconf.SMTPConfig) {
	close(mailQuit)
	mailQuit = make(chan struct{})
	startEmailSender(smtp)
}

func NewEmailRebooter() *emailRebooter {
	return &emailRebooter{}
}

func RestartEmailSender(smtp aconf.SMTPConfig) {
	close(mailQuit)
	mailQuit = make(chan struct{})
	startEmailSender(smtp)
}

func InitEmailSender(smtp aconf.SMTPConfig) {
	mailch = make(chan *gomail.Message, 100000)
	startEmailSender(smtp)
}

func startEmailSender(smtp aconf.SMTPConfig) {
	conf := smtp
	if conf.Host == "" || conf.Port == 0 {
		logger.Warning("SMTP configurations invalid")
		return
	}
	logger.Infof("start email sender... conf.Host:%+v,conf.Port:%+v", conf.Host, conf.Port)

	d := gomail.NewDialer(conf.Host, conf.Port, conf.User, conf.Pass)
	if conf.InsecureSkipVerify {
		d.TLSConfig = &tls.Config{InsecureSkipVerify: true}
	}

	var s gomail.SendCloser
	var open bool
	var size int
	var err error
	for {
		select {
		case <-mailQuit:
			return
		case m, ok := <-mailch:
			if !ok {
				return
			}

			if !open {
				if s, err = dialSmtp(d); err != nil {
					logger.Error(err.Error())
					continue
				}
				open = true
			}
			if err = gomail.Send(s, m); err != nil {
				logger.Errorf("email_sender: failed to send: %s", err)

				// close and retry
				if err = s.Close(); err != nil {
					logger.Warningf("email_sender: failed to close smtp connection: %s", err)
				}

				if s, err = dialSmtp(d); err != nil {
					logger.Error(err.Error())
					continue
				}

				open = true

				if err = gomail.Send(s, m); err != nil {
					logger.Errorf("email_sender: failed to retry send: %s", err)
				}
			} else {
				logger.Infof("email_sender: result=succ subject=%v to=%v", m.GetHeader("Subject"), m.GetHeader("To"))
			}

			size++

			if size >= conf.Batch {
				if err = s.Close(); err != nil {
					logger.Warningf("email_sender: failed to close smtp connection: %s", err)
				}
				open = false
				size = 0
			}

		// Close the connection to the SMTP server if no email was sent in
		// the last 30 seconds.
		case <-time.After(30 * time.Second):
			if open {
				if err = s.Close(); err != nil {
					logger.Warningf("email_sender: failed to close smtp connection: %s", err)
				}
				open = false
			}
		}
	}
}
