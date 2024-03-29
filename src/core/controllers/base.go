// Copyright 2018 Project Harbor Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package controllers

import (
	"bytes"
	"html/template"
	"net"
	"net/http"
	"os"
	"regexp"
	"strconv"

	"github.com/astaxie/beego"
	"github.com/beego/i18n"
	"github.com/goharbor/harbor/src/common"
	"github.com/goharbor/harbor/src/common/dao"
	"github.com/goharbor/harbor/src/common/models"
	"github.com/goharbor/harbor/src/common/utils"
	email_util "github.com/goharbor/harbor/src/common/utils/email"
	"github.com/goharbor/harbor/src/common/utils/log"
	"github.com/goharbor/harbor/src/core/auth"
	"github.com/goharbor/harbor/src/core/config"
)

// CommonController handles request from UI that doesn't expect a page, such as /SwitchLanguage /logout ...
type CommonController struct {
	beego.Controller
	i18n.Locale
}

// Render returns nil.
func (cc *CommonController) Render() error {
	return nil
}

type messageDetail struct {
	Hint string
	URL  string
	UUID string
}

// Login handles login request from UI.
func (cc *CommonController) Login() {
	principal := cc.GetString("principal")
	password := cc.GetString("password")

	user, err := auth.Login(models.AuthModel{
		Principal: principal,
		Password:  password,
	})
	if err != nil {
		log.Errorf("Error occurred in UserLogin: %v", err)
		cc.CustomAbort(http.StatusUnauthorized, "")
	}

	if user == nil {
		cc.CustomAbort(http.StatusUnauthorized, "")
	}
	// 创建用户 session，直接使用用户提供的账号和密码生成。存储到对应的数据库中
	cc.SetSession("user", *user)
}

// LogOut Habor UI
func (cc *CommonController) LogOut() {
	cc.DestroySession()
}

// UserExists checks if user exists when user input value in sign in form.
func (cc *CommonController) UserExists() {
	target := cc.GetString("target")
	value := cc.GetString("value")

	user := models.User{}
	// 根据需要检查的类型，构造需要查询的数据
	switch target {
	case "username":
		user.Username = value
	case "email":
		user.Email = value
	}

	// 查询数据库中是用户或则 email 的记录
	exist, err := dao.UserExists(user, target)
	if err != nil {
		log.Errorf("Error occurred in UserExists: %v", err)
		cc.CustomAbort(http.StatusInternalServerError, "Internal error.")
	}
	cc.Data["json"] = exist
	// exist 是 bool 类型，ServeJSON 将数据发送回去. 发送到前端的 session 服务中的checkUserExisting函数。
	cc.ServeJSON()
}

// SendResetEmail verifies the Email address and contact SMTP server to send reset password Email.
func (cc *CommonController) SendResetEmail() {

	email := cc.GetString("email")

	valid, err := regexp.MatchString(`^(([^<>()[\]\\.,;:\s@\"]+(\.[^<>()[\]\\.,;:\s@\"]+)*)|(\".+\"))@((\[[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\])|(([a-zA-Z\-0-9]+\.)+[a-zA-Z]{2,}))$`, email)
	if err != nil {
		log.Errorf("failed to match regexp: %v", err)
		cc.CustomAbort(http.StatusInternalServerError, "Internal error.")
	}

	if !valid {
		cc.CustomAbort(http.StatusBadRequest, "invalid email")
	}

	queryUser := models.User{Email: email}
	u, err := dao.GetUser(queryUser)
	if err != nil {
		log.Errorf("Error occurred in GetUser: %v", err)
		cc.CustomAbort(http.StatusInternalServerError, "Internal error.")
	}
	if u == nil {
		log.Debugf("email %s not found", email)
		cc.CustomAbort(http.StatusNotFound, "email_does_not_exist")
	}

	if !isUserResetable(u) {
		log.Errorf("Resetting password for user with ID: %d is not allowed", u.UserID)
		cc.CustomAbort(http.StatusForbidden, http.StatusText(http.StatusForbidden))
	}

	uuid := utils.GenerateRandomString()
	user := models.User{ResetUUID: uuid, Email: email}
	if err = dao.UpdateUserResetUUID(user); err != nil {
		log.Errorf("failed to update user reset UUID: %v", err)
		cc.CustomAbort(http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))
	}

	messageTemplate, err := template.ParseFiles("views/reset-password-mail.tpl")
	if err != nil {
		log.Errorf("Parse email template file failed: %v", err)
		cc.CustomAbort(http.StatusInternalServerError, err.Error())
	}

	message := new(bytes.Buffer)

	harborURL, err := config.ExtEndpoint()
	if err != nil {
		log.Errorf("failed to get domain name: %v", err)
		cc.CustomAbort(http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))
	}

	err = messageTemplate.Execute(message, messageDetail{
		Hint: cc.Tr("reset_email_hint"),
		URL:  harborURL,
		UUID: uuid,
	})

	if err != nil {
		log.Errorf("Message template error: %v", err)
		cc.CustomAbort(http.StatusInternalServerError, "internal_error")
	}

	settings, err := config.Email()
	if err != nil {
		log.Errorf("failed to get email configurations: %v", err)
		cc.CustomAbort(http.StatusInternalServerError, "internal_error")
	}

	addr := net.JoinHostPort(settings.Host, strconv.Itoa(settings.Port))
	err = email_util.Send(addr,
		settings.Identity,
		settings.Username,
		settings.Password,
		60, settings.SSL,
		settings.Insecure,
		settings.From,
		[]string{email},
		"Reset Harbor user password",
		message.String())
	if err != nil {
		log.Errorf("Send email failed: %v", err)
		cc.CustomAbort(http.StatusInternalServerError, "send_email_failed")
	}
}

// ResetPassword handles request from the reset page and reset password
func (cc *CommonController) ResetPassword() {

	resetUUID := cc.GetString("reset_uuid")
	if resetUUID == "" {
		cc.CustomAbort(http.StatusBadRequest, "Reset uuid is blank.")
	}

	queryUser := models.User{ResetUUID: resetUUID}
	user, err := dao.GetUser(queryUser)

	if err != nil {
		log.Errorf("Error occurred in GetUser: %v", err)
		cc.CustomAbort(http.StatusInternalServerError, "Internal error.")
	}
	if user == nil {
		log.Error("User does not exist")
		cc.CustomAbort(http.StatusBadRequest, "User does not exist")
	}

	if !isUserResetable(user) {
		log.Errorf("Resetting password for user with ID: %d is not allowed", user.UserID)
		cc.CustomAbort(http.StatusForbidden, http.StatusText(http.StatusForbidden))
	}

	password := cc.GetString("password")

	if password != "" {
		user.Password = password
		err = dao.ResetUserPassword(*user)
		if err != nil {
			log.Errorf("Error occurred in ResetUserPassword: %v", err)
			cc.CustomAbort(http.StatusInternalServerError, "Internal error.")
		}
	} else {
		cc.CustomAbort(http.StatusBadRequest, "password_is_required")
	}
}

func isUserResetable(u *models.User) bool {
	if u == nil {
		return false
	}
	mode, err := config.AuthMode()
	if err != nil {
		log.Errorf("Failed to get the auth mode, error: %v", err)
		return false
	}
	if mode == common.DBAuth {
		return true
	}
	return u.UserID == 1
}

func init() {
	// conf/app.conf -> os.Getenv("config_path")
	// todo 控制器初始化的配置文件需要找到在哪里
	configPath := os.Getenv("CONFIG_PATH")
	if len(configPath) != 0 {
		log.Infof("Config path: %s", configPath)
		if err := beego.LoadAppConfig("ini", configPath); err != nil {
			log.Errorf("failed to load app config: %v", err)
		}
	}

}
