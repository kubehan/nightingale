package http

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"log"
	"math/rand"
	"path"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mojocn/base64Captcha"
	"github.com/toolkits/pkg/file"
	"github.com/toolkits/pkg/logger"
	"github.com/toolkits/pkg/str"

	"github.com/didi/nightingale/src/common/dataobj"
	"github.com/didi/nightingale/src/models"
	"github.com/didi/nightingale/src/modules/rdb/auth"
	"github.com/didi/nightingale/src/modules/rdb/config"
	"github.com/didi/nightingale/src/modules/rdb/redisc"
	"github.com/didi/nightingale/src/modules/rdb/ssoc"
)

var (
	loginCodeSmsTpl     *template.Template
	loginCodeEmailTpl   *template.Template
	errUnsupportCaptcha = errors.New("unsupported captcha")
	errInvalidAnswer    = errors.New("Invalid captcha answer")

	// TODO: set false
	debug = true

	// https://captcha.mojotv.cn
	captchaDirver = base64Captcha.DriverString{
		Height:          30,
		Width:           120,
		ShowLineOptions: 0,
		Length:          4,
		Source:          "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789",
		//ShowLineOptions: 14,
	}
)

func getConfigFile(name, ext string) (string, error) {
	if p := path.Join(path.Join(file.SelfDir(), "etc", name+".local."+ext)); file.IsExist(p) {
		return p, nil
	}
	if p := path.Join(path.Join(file.SelfDir(), "etc", name+"."+ext)); file.IsExist(p) {
		return p, nil
	} else {
		return "", fmt.Errorf("file %s not found", p)
	}

}

func init() {
	filename, err := getConfigFile("login-code-sms", "tpl")
	if err != nil {
		log.Fatal(err)
	}

	loginCodeSmsTpl, err = template.ParseFiles(filename)
	if err != nil {
		log.Fatalf("open %s err: %s", filename, err)
	}

	filename, err = getConfigFile("login-code-email", "tpl")
	if err != nil {
		log.Fatal(err)
	}
	loginCodeEmailTpl, err = template.ParseFiles(filename)
	if err != nil {
		log.Fatalf("open %s err: %s", filename, err)
	}
}

// login for UI
func login(c *gin.Context) {
	var in loginInput
	bind(c, &in)
	in.RemoteAddr = c.ClientIP()

	err := func() error {
		if err := in.Validate(); err != nil {
			return err
		}

		if config.Config.Auth.Captcha {
			c, err := models.CaptchaGet("captcha_id=?", in.CaptchaId)
			if err != nil {
				return err
			}
			if strings.ToLower(c.Answer) != strings.ToLower(in.Answer) {
				return errInvalidAnswer
			}
		}

		user, err := authLogin(in.v1LoginInput())
		if err != nil {
			logger.Debugf("login error %s", err)
			return err
		}

		sessionLogin(c, user.Username, in.RemoteAddr)
		return nil
	}()
	renderMessage(c, err)
}

func logout(c *gin.Context) {
	func() {
		username := sessionUsername(c)
		if username == "" {
			return
		}
		sessionDestory(c)
		models.LoginLogNew(username, c.ClientIP(), "out", nil)
	}()

	if config.Config.SSO.Enable {
		redirect := queryStr(c, "redirect", "/")
		c.Redirect(302, ssoc.LogoutLocation(redirect))
		return
	}

	if redirect := queryStr(c, "redirect", ""); redirect != "" {
		c.Redirect(302, redirect)
		return
	}

	c.String(200, "logout successfully")
}

type authRedirect struct {
	Redirect string `json:"redirect"`
	Msg      string `json:"msg"`
}

func authAuthorizeV2(c *gin.Context) {
	resp, err := func() (*authRedirect, error) {
		redirect := queryStr(c, "redirect", "/")

		username := sessionUsername(c)
		if username != "" { // alread login
			return &authRedirect{Redirect: redirect}, nil
		}

		if !config.Config.SSO.Enable {
			return &authRedirect{Redirect: "/login"}, nil
		}

		if redirect, err := ssoc.Authorize(redirect); err != nil {
			return nil, err
		} else {
			return &authRedirect{Redirect: redirect}, nil
		}
	}()
	renderData(c, resp, err)
}

func authCallbackV2(c *gin.Context) {
	code := queryStr(c, "code", "")
	state := queryStr(c, "state", "")
	redirect := queryStr(c, "redirect", "")

	ret := &authRedirect{Redirect: redirect}
	if code == "" && redirect != "" {
		logger.Debugf("sso.callback()  can't get code and redirect is not set")
		renderData(c, ret, nil)
		return
	}

	var user *models.User
	var err error
	ret.Redirect, user, err = ssoc.Callback(code, state)
	if err != nil {
		logger.Debugf("sso.callback() error %s", err)
		renderData(c, ret, err)
		return
	}

	logger.Debugf("sso.callback() successfully, set username %s", user.Username)
	sessionLogin(c, user.Username, c.ClientIP())
	renderData(c, ret, nil)
}

func logoutV2(c *gin.Context) {
	redirect := queryStr(c, "redirect", "")
	ret := &authRedirect{Redirect: redirect}

	username := sessionUsername(c)
	if username == "" {
		renderData(c, ret, nil)
		return
	}

	sessionDestory(c)
	ret.Msg = "logout successfully"

	if config.Config.SSO.Enable {
		if redirect == "" {
			redirect = "/"
		}
		ret.Redirect = ssoc.LogoutLocation(redirect)
	}

	renderData(c, ret, nil)

	models.LoginLogNew(username, c.ClientIP(), "out", nil)
}

type loginInput struct {
	Username   string   `json:"username"`
	Password   string   `json:"password"`
	CaptchaId  string   `json:"captcha_id"`
	Answer     string   `json:"answer" description:"captcha answer"`
	Type       string   `json:"type" description:"sms-code|email-code|password|ldap"`
	Args       []string `json:"args" description:""`
	RemoteAddr string   `json:"remote_addr" description:"use for server account(v1)"`
	IsLDAP     int      `json:"is_ldap" description:"deprecated"`
}

func (p *loginInput) Validate() error {
	if p.IsLDAP == 1 {
		p.Type = models.LOGIN_T_LDAP
	}
	if p.Type == "" {
		p.Type = models.LOGIN_T_PWD
	}
	if p.Type == models.LOGIN_T_PWD || p.Type == models.LOGIN_T_LDAP {
		if len(p.Args) == 0 {
			if str.Dangerous(p.Username) {
				return _e("%s invalid", p.Username)
			}
			if len(p.Username) > 64 {
				return _e("%s too long > 64", p.Username)
			}
			p.Args = []string{p.Username, p.Password}
		}
	}

	if len(p.Args) == 0 {
		return _e("login args must be set")
	}
	return nil
}

func (p *loginInput) v1LoginInput() *v1LoginInput {
	return &v1LoginInput{
		Type:       p.Type,
		Args:       p.Args,
		RemoteAddr: p.RemoteAddr,
	}
}

type v1LoginInput struct {
	Type       string   `param:"data" json:"type"`
	Args       []string `param:"data" json:"args"`
	RemoteAddr string   `param:"data" json:"remote_addr"`
}

func (p *v1LoginInput) Validate() error {
	if p.Type == "" {
		p.Type = models.LOGIN_T_PWD
	}

	if len(p.Args) == 0 {
		return fmt.Errorf("login args must be set")
	}

	return nil
}

// v1Login called by sso.rdb module
func v1Login(c *gin.Context) {
	var in v1LoginInput
	bind(c, &in)

	user, err := authLogin(&in)
	renderData(c, user, err)
}

// authLogin called by /v1/rdb/login, /api/rdb/auth/login
func authLogin(in *v1LoginInput) (user *models.User, err error) {
	if err = in.Validate(); err != nil {
		return
	}

	if err := auth.WhiteListAccess(in.RemoteAddr); err != nil {
		return nil, err
	}
	defer func() {
		models.LoginLogNew(in.Args[0], in.RemoteAddr, "in", err)
	}()

	switch strings.ToLower(in.Type) {
	case models.LOGIN_T_LDAP:
		user, err = models.LdapLogin(in.Args[0], in.Args[1])
	case models.LOGIN_T_PWD:
		user, err = models.PassLogin(in.Args[0], in.Args[1])
	case models.LOGIN_T_SMS:
		user, err = models.SmsCodeLogin(in.Args[0], in.Args[1])
	case models.LOGIN_T_EMAIL:
		user, err = models.EmailCodeLogin(in.Args[0], in.Args[1])
	default:
		err = fmt.Errorf("invalid login type %s", in.Type)
	}

	if err = auth.PostLogin(user, err); err != nil {
		return nil, err
	}

	return user, nil
}

type sendCodeInput struct {
	Type string `json:"type" description:"sms-code, email-code"`
	Arg  string `json:"arg"`
}

func (p *sendCodeInput) Validate() error {
	if p.Type == "" {
		return fmt.Errorf("unable to get type, sms-code | email-code")
	}
	if p.Arg == "" {
		return fmt.Errorf("unable to get arg")
	}
	return nil
}

func sendLoginCode(c *gin.Context) {
	var in sendCodeInput
	bind(c, &in)

	msg, err := func() (string, error) {
		if err := in.Validate(); err != nil {
			return "", err
		}
		if !config.Config.Redis.Enable {
			return "", fmt.Errorf("sms/email sender is disabled")
		}

		if err := in.Validate(); err != nil {
			return "", err
		}

		// general a random code and add cache
		code := fmt.Sprintf("%06d", rand.Intn(1000000))

		loginCode := &models.LoginCode{
			Code:      code,
			LoginType: models.LOGIN_T_LOGIN,
			CreatedAt: time.Now().Unix(),
		}

		var (
			user      *models.User
			buf       bytes.Buffer
			queueName string
		)

		switch in.Type {
		case models.LOGIN_T_SMS:
			user, _ = models.UserGet("phone=?", in.Arg)
			if err := loginCodeSmsTpl.Execute(&buf, loginCode); err != nil {
				return "", err
			}
			queueName = config.SMS_QUEUE_NAME
		case models.LOGIN_T_EMAIL:
			user, _ = models.UserGet("email=?", in.Arg)
			if err := loginCodeEmailTpl.Execute(&buf, loginCode); err != nil {
				return "", err
			}
			queueName = config.MAIL_QUEUE_NAME
		default:
			return "", fmt.Errorf("invalid type %s", in.Type)
		}

		if user == nil {
			return "", fmt.Errorf("user informations is invalid")
		}

		loginCode.Username = user.Username
		if err := loginCode.Save(); err != nil {
			return "", err
		}

		if err := redisc.Write(&dataobj.Message{Tos: []string{in.Arg}, Content: buf.String()}, queueName); err != nil {
			return "", err
		}

		if debug {
			return fmt.Sprintf("[debug]: %s", buf.String()), nil
		}

		return "successed", nil

	}()
	renderData(c, msg, err)
}

func sendRstCode(c *gin.Context) {
	var in sendCodeInput
	bind(c, &in)
	logger.Debugf("rst code input %#v", in)

	msg, err := func() (string, error) {
		if err := in.Validate(); err != nil {
			return "", err
		}
		if !config.Config.Redis.Enable {
			return "", fmt.Errorf("email/sms sender is disabled")
		}

		if err := in.Validate(); err != nil {
			return "", err
		}

		// general a random code and add cache
		code := fmt.Sprintf("%06d", rand.Intn(1000000))

		loginCode := &models.LoginCode{
			Code:      code,
			LoginType: models.LOGIN_T_RST,
			CreatedAt: time.Now().Unix(),
		}

		var (
			user      *models.User
			buf       bytes.Buffer
			queueName string
		)

		switch in.Type {
		case models.LOGIN_T_SMS:
			user, _ = models.UserGet("phone=?", in.Arg)
			if err := loginCodeSmsTpl.Execute(&buf, loginCode); err != nil {
				return "", err
			}
			queueName = config.SMS_QUEUE_NAME
		case models.LOGIN_T_EMAIL:
			user, _ = models.UserGet("email=?", in.Arg)
			if err := loginCodeEmailTpl.Execute(&buf, loginCode); err != nil {
				return "", err
			}
			queueName = config.MAIL_QUEUE_NAME
		default:
			return "", fmt.Errorf("invalid type %s", in.Type)
		}

		if user == nil {
			return "", fmt.Errorf("User %s's infomation is incorrect", in.Arg)
		}

		loginCode.Username = user.Username
		if err := loginCode.Save(); err != nil {
			return "", err
		}

		if err := redisc.Write(&dataobj.Message{Tos: []string{in.Arg}, Content: buf.String()}, queueName); err != nil {
			return "", err
		}

		if debug {
			return fmt.Sprintf("[debug] msg: %s", buf.String()), nil
		}

		return "successed", nil
	}()
	renderData(c, msg, err)
}

type rstPasswordInput struct {
	Type     string `json:"type"`
	Arg      string `json:"arg"`
	Code     string `json:"code"`
	Password string `json:"password"`
	DryRun   bool   `json:"dryRun"`
}

func (p *rstPasswordInput) Validate() error {
	if p.Type == "" {
		return fmt.Errorf("unable to get type, sms-code | email-code")
	}
	if p.Arg == "" {
		return fmt.Errorf("unable to get arg")
	}
	if !p.DryRun && p.Password == "" {
		return fmt.Errorf("password must be set")
	}
	return nil
}

func rstPassword(c *gin.Context) {
	var in rstPasswordInput
	bind(c, &in)

	err := func() error {
		if err := in.Validate(); err != nil {
			return err
		}

		var user *models.User

		switch in.Type {
		case models.LOGIN_T_SMS:
			user, _ = models.UserGet("phone=?", in.Arg)
		case models.LOGIN_T_EMAIL:
			user, _ = models.UserGet("email=?", in.Arg)
		default:
			return fmt.Errorf("invalid type %s", in.Type)
		}

		if user == nil {
			return fmt.Errorf("User %s's infomation is incorrect", in.Arg)
		}

		lc, err := models.LoginCodeGet("code=? and login_type=?", in.Code, models.LOGIN_T_RST)
		if err != nil {
			return fmt.Errorf("invalid code")
		}

		if time.Now().Unix()-lc.CreatedAt > models.LOGIN_EXPIRES_IN {
			return fmt.Errorf("the code has expired")
		}

		if in.DryRun {
			return nil
		}
		defer lc.Del()

		// update password
		return auth.ChangePassword(user, in.Password)
	}()

	if err != nil {
		renderData(c, nil, err)
	} else {
		renderData(c, "reset successfully", nil)
	}
}

func captchaGet(c *gin.Context) {
	ret, err := func() (*models.Captcha, error) {
		if !config.Config.Auth.Captcha {
			return nil, errUnsupportCaptcha
		}

		driver := captchaDirver.ConvertFonts()
		id, content, answer := driver.GenerateIdQuestionAnswer()
		item, err := driver.DrawCaptcha(content)
		if err != nil {
			return nil, err
		}

		ret := &models.Captcha{
			CaptchaId: id,
			Answer:    answer,
			Image:     item.EncodeB64string(),
			CreatedAt: time.Now().Unix(),
		}

		if err := ret.Save(); err != nil {
			return nil, err
		}

		return ret, nil
	}()

	renderData(c, ret, err)
}

func authSettings(c *gin.Context) {
	renderData(c, struct {
		Sso bool `json:"sso"`
	}{
		Sso: config.Config.SSO.Enable,
	}, nil)
}

func authConfigsGet(c *gin.Context) {
	config, err := models.AuthConfigGet()
	renderData(c, config, err)
}

func authConfigsPut(c *gin.Context) {
	var in models.AuthConfig
	bind(c, &in)

	err := models.AuthConfigSet(&in)
	renderData(c, "", err)
}

type createWhiteListInput struct {
	StartIp   string `json:"startIp"`
	EndIp     string `json:"endIp"`
	StartTime int64  `json:"startTime"`
	EndTime   int64  `json:"endTime"`
}

func whiteListPost(c *gin.Context) {
	var in createWhiteListInput
	bind(c, &in)

	username := loginUser(c).Username
	ts := time.Now().Unix()

	wl := models.WhiteList{
		StartIp:   in.StartIp,
		EndIp:     in.EndIp,
		StartTime: in.StartTime,
		EndTime:   in.EndTime,
		CreatedAt: ts,
		UpdatedAt: ts,
		Creator:   username,
		Updater:   username,
	}
	if err := wl.Validate(); err != nil {
		bomb("invalid arguments %s", err)
	}
	dangerous(wl.Save())

	renderData(c, gin.H{"id": wl.Id}, nil)
}

func whiteListsGet(c *gin.Context) {
	limit := queryInt(c, "limit", 20)
	query := queryStr(c, "query", "")

	total, err := models.WhiteListTotal(query)
	dangerous(err)

	list, err := models.WhiteListGets(query, limit, offset(c, limit))
	dangerous(err)

	renderData(c, gin.H{
		"list":  list,
		"total": total,
	}, nil)
}

func whiteListGet(c *gin.Context) {
	id := urlParamInt64(c, "id")
	ret, err := models.WhiteListGet("id=?", id)
	renderData(c, ret, err)
}

type updateWhiteListInput struct {
	StartIp   string `json:"startIp"`
	EndIp     string `json:"endIp"`
	StartTime int64  `json:"startTime"`
	EndTime   int64  `json:"endTime"`
}

func whiteListPut(c *gin.Context) {
	var in updateWhiteListInput
	bind(c, &in)

	wl, err := models.WhiteListGet("id=?", urlParamInt64(c, "id"))
	if err != nil {
		bomb("not found white list")
	}

	wl.StartIp = in.StartIp
	wl.EndIp = in.EndIp
	wl.StartTime = in.StartTime
	wl.EndTime = in.EndTime
	wl.UpdatedAt = time.Now().Unix()
	wl.Updater = loginUser(c).Username

	if err := wl.Validate(); err != nil {
		bomb("invalid arguments %s", err)
	}

	renderMessage(c, wl.Update("start_ip", "end_ip", "start_time", "end_time", "updated_at", "updater"))
}

func whiteListDel(c *gin.Context) {
	wl, err := models.WhiteListGet("id=?", urlParamInt64(c, "id"))
	dangerous(err)

	renderMessage(c, wl.Del())
}

func v1SessionGet(c *gin.Context) {
	sess, err := models.SessionGetWithCache(urlParamStr(c, "sid"))
	renderData(c, sess, err)
}

func v1SessionGetUser(c *gin.Context) {
	user, err := models.SessionGetUserWithCache(urlParamStr(c, "sid"))
	renderData(c, user, err)
}

func v1SessionDelete(c *gin.Context) {
	sid := urlParamStr(c, "sid")
	logger.Debugf("session del sid %s", sid)
	renderMessage(c, models.SessionDel(sid))
}
