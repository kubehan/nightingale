package auth

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/didi/nightingale/src/models"
	"github.com/didi/nightingale/src/modules/rdb/cache"
	"github.com/didi/nightingale/src/modules/rdb/config"
	"github.com/toolkits/pkg/logger"
)

type Authenticator struct {
	extraMode     bool
	whiteList     bool
	frozenTime    int64
	writenOffTime int64
	userExpire    bool
}

// description:"enable user expire control, active -> frozen -> writen-off"
func New(cf config.AuthExtraSection) *Authenticator {
	if !cf.Enable {
		return &Authenticator{}
	}

	return &Authenticator{
		extraMode:     true,
		whiteList:     cf.WhiteList,
		frozenTime:    86400 * int64(cf.FrozenDays),
		writenOffTime: 86400 * int64(cf.WritenOffDays),
	}
}

func (p *Authenticator) WhiteListAccess(remoteAddr string) error {
	if !p.whiteList {
		return nil
	}
	return models.WhiteListAccess(remoteAddr)
}

func (p *Authenticator) PostLogin(user *models.User, loginErr error) (err error) {
	now := time.Now().Unix()
	defer func() {
		if user == nil {
			return
		}
		if err == nil {
			user.LoggedAt = now
		}
		user.Update("status", "login_err_num", "locked_at", "updated_at", "logged_at")
	}()

	if !p.extraMode || user == nil {
		err = loginErr
		return
	}

	cf := cache.AuthConfig()

	if user.Type == models.USER_T_TEMP && (now < user.ActiveBegin || user.ActiveEnd < now) {
		err = fmt.Errorf("Temporary user has expired")
		return
	}

	status := user.Status
retry:
	switch user.Status {
	case models.USER_S_ACTIVE:
		err = activeUserAccess(cf, user, loginErr)
	case models.USER_S_INACTIVE:
		err = inactiveUserAccess(cf, user, loginErr)
	case models.USER_S_LOCKED:
		err = lockedUserAccess(cf, user, loginErr)
	case models.USER_S_FROZEN:
		err = frozenUserAccess(cf, user, loginErr)
	case models.USER_S_WRITEN_OFF:
		err = writenOffUserAccess(cf, user, loginErr)
	default:
		err = fmt.Errorf("invalid user status %d", user.Status)
	}

	// if user's status has been changed goto retry
	if user.Status != status {
		status = user.Status
		goto retry
	}
	return
}

func (p *Authenticator) ChangePassword(user *models.User, password string) (err error) {
	defer func() {
		if err == nil {
			err = user.Update("password", "passwords",
				"pwd_updated_at", "updated_at")
		}
	}()

	changePassword := func() error {
		pwd, err := models.CryptoPass(password)
		if err != nil {
			return err
		}

		now := time.Now().Unix()
		user.Password = pwd
		user.PwdUpdatedAt = now
		user.UpdatedAt = now
		return nil
	}

	if !p.extraMode {
		return changePassword()
	}

	// precheck
	cf := cache.AuthConfig()
	if err = checkPassword(cf, password); err != nil {
		return
	}

	if err = changePassword(); err != nil {
		return
	}

	var passwords []string
	err = json.Unmarshal([]byte(user.Passwords), &passwords)
	if err != nil {
		// reset passwords
		passwords = []string{user.Password}
		b, _ := json.Marshal(passwords)
		user.Passwords = string(b)
		err = nil
		return
	}

	for _, v := range passwords {
		if user.Password == v {
			err = fmt.Errorf("The password is the same as the old password")
			return
		}
	}

	passwords = append(passwords, user.Password)
	if n := len(passwords) - cf.PwdHistorySize; n > 0 {
		passwords = passwords[n:]
	}

	b, _ := json.Marshal(passwords)
	user.Passwords = string(b)
	return
}

func (p *Authenticator) CheckPassword(password string) error {
	if !p.extraMode {
		return nil
	}
	return checkPassword(cache.AuthConfig(), password)
}

func (p *Authenticator) Start() error {
	if !p.extraMode {
		return nil
	}

	go func() {
		for {
			now := time.Now().Unix()
			if p.frozenTime > 0 {
				// 3个月以上未登录，用户自动变为休眠状态
				if _, err := models.DB["rdb"].Exec("update user set status=?, updated_at=?, locked_at=? where ((logged_at > 0 and logged_at<?) or (logged_at == 0 and created_at < ?)) and status in (?,?,?)",
					models.USER_S_FROZEN, now, now, now-p.frozenTime,
					models.USER_S_ACTIVE, models.USER_S_INACTIVE, models.USER_S_LOCKED); err != nil {
					logger.Errorf("update user status error %s", err)
				}
			}

			if p.writenOffTime > 0 {
				// 变为休眠状态后1年未激活，用户自动变为已注销状态
				if _, err := models.DB["rdb"].Exec("update user set status=?, updated_at=? where locked_at<? and status=?",
					models.USER_S_WRITEN_OFF, now, now-p.writenOffTime, models.USER_S_FROZEN); err != nil {
					logger.Errorf("update user status error %s", err)
				}
			}

			// reset login err num before 24 hours ago
			if _, err := models.DB["rdb"].Exec("update user set login_err_num=0, updated_at=? where updated_at<? and login_err_num>0", now, now-86400); err != nil {
				logger.Errorf("update user login err num error %s", err)
			}

			time.Sleep(time.Hour)
		}
	}()
	return nil
}

func activeUserAccess(cf *models.AuthConfig, user *models.User, loginErr error) error {
	now := time.Now().Unix()

	if cf.MaxNumErr > 0 && user.LoginErrNum >= cf.MaxNumErr {
		user.Status = models.USER_S_LOCKED
		user.LockedAt = now
		user.UpdatedAt = now
		return nil
	}

	if loginErr != nil {
		user.LoginErrNum++
		user.UpdatedAt = now
		return fmt.Errorf("max login err %d/%d", user.LoginErrNum, cf.MaxNumErr)
	}

	user.LoginErrNum = 0
	user.UpdatedAt = now

	if cf.MaxSessionNumber > 0 {
		if n, err := models.SessionUserAll(user.Username); err != nil {
			return err
		} else if n >= cf.MaxSessionNumber {
			return fmt.Errorf("max session limit %d/%d", n, cf.MaxSessionNumber)
		}
	}

	if cf.PwdExpiresIn > 0 {
		if now-user.PwdUpdatedAt > cf.PwdExpiresIn*30*86400 {
			return fmt.Errorf("password has been expired")
		}
	}
	return nil
}
func inactiveUserAccess(cf *models.AuthConfig, user *models.User, loginErr error) error {
	return fmt.Errorf("user is inactive")
}
func lockedUserAccess(cf *models.AuthConfig, user *models.User, loginErr error) error {
	now := time.Now().Unix()
	if now-user.LockedAt > cf.LockTime*60 {
		user.Status = models.USER_S_ACTIVE
		user.LoginErrNum = 0
		user.UpdatedAt = now
		return nil
	}
	return fmt.Errorf("user is locked")
}

func frozenUserAccess(cf *models.AuthConfig, user *models.User, loginErr error) error {
	return fmt.Errorf("user is frozen")
}

func writenOffUserAccess(cf *models.AuthConfig, user *models.User, loginErr error) error {
	return fmt.Errorf("user is writen off")
}

func checkPassword(cf *models.AuthConfig, passwd string) error {
	indNum := [4]int{0, 0, 0, 0}
	spCode := []byte{'!', '@', '#', '$', '%', '^', '&', '*', '_', '-', '~', '.', ',', '<', '>', '/', ';', ':', '|', '?', '+', '='}

	if cf.PwdMinLenght > 0 && len(passwd) < cf.PwdMinLenght {
		return fmt.Errorf("password too short %d/%d",
			len(passwd), cf.PwdMinLenght)
	}

	passwdByte := []byte(passwd)

	for _, i := range passwdByte {

		if i >= 'A' && i <= 'Z' {
			indNum[0] = 1
			continue
		}

		if i >= 'a' && i <= 'z' {
			indNum[1] = 1
			continue
		}

		if i >= '0' && i <= '9' {
			indNum[2] = 1
			continue
		}

		has := false
		for _, s := range spCode {
			if i == s {
				indNum[3] = 1
				has = true
				break
			}
		}

		if !has {
			return fmt.Errorf("character: %s not supported", string(i))
		}
	}

	if cf.PwdMustIncludeFlag&models.PWD_INCLUDE_UPPER > 0 && indNum[0] == 0 {
		return fmt.Errorf("password must include upper char")
	}

	if cf.PwdMustIncludeFlag&models.PWD_INCLUDE_LOWER > 0 && indNum[1] == 0 {
		return fmt.Errorf("password must include lower char")
	}

	if cf.PwdMustIncludeFlag&models.PWD_INCLUDE_NUMBER > 0 && indNum[2] == 0 {
		return fmt.Errorf("password must include number char")
	}

	if cf.PwdMustIncludeFlag&models.PWD_INCLUDE_SPEC_CHAR > 0 && indNum[3] == 0 {
		return fmt.Errorf("password must include special char")
	}

	return nil
}
