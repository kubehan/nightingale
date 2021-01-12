package config

import "github.com/didi/nightingale/src/toolkits/i18n"

var (
	langDict = map[string]map[string]string{
		"zh": map[string]string{
			"stra not found":                        "聚合策略为找到",
			"same stra name %s in node":             "同节点下策略名称 %s 已存在",
			"collect type not support":              "采集类型不合法",
			"[%s] is blank":                         "参数[%s]值不能为空",
			"cannot convert %s to int64":            "%s 无法转为 int64 类型",
			"cannot convert %s to int":              "%s 无法转为 int 类型",
			"arg[%s] not found":                     "参数[%s]没找到",
			"cannot retrieve node[%d]: %v":          "获取不到节点[%d],原因:%v",
			"no such node[%d]":                      "节点[%d]不存在",
			"no such task[id:%d]":                   "任务[%d]不存在",
			"no such task tpl[id:%d]":               "任务模板[%d]不存在",
			"cannot retrieve screen[%d]: %v":        "获取不到大盘[%d],原因:%v",
			"no such screen[%d]":                    "大盘[%d]不存在",
			"cannot retrieve subclass[%d]: %v":      "获取不到大盘分组[%d],原因:%v",
			"no such subclass[%d]":                  "大盘分组[%d]不存在",
			"cannot retrieve chart[%d]: %v":         "获取不到大盘图表[%d],原因:%v",
			"no such chart[%d]":                     "大盘图表[%d]不存在",
			"cannot retrieve eventCur[%d]: %v":      "获取不到未恢复告警事件[%d],原因:%v",
			"no such eventCur[%d]":                  "未恢复告警事件[%d]不存在",
			"cannot retrieve event[%d]: %v":         "获取不到告警事件[%d],原因:%v",
			"no such event[%d]":                     "告警事件[%d]不存在",
			"cannot retrieve user[%d]: %v":          "获取不到用户[%d],原因:%v",
			"no such user[%d]":                      "用户[%d]不存在",
			"no such user: %s":                      "用户[%s]不存在",
			"cannot retrieve team[%d]: %v":          "获取不到团队[%d],原因:%v",
			"no such team[%d]":                      "团队[%d]不存在",
			"cannot retrieve role[%d]: %v":          "获取不到角色[%d],原因:%v",
			"no such role[%d]":                      "角色[%d]不存在",
			"no such NodeCate[id:%d]":               "节点类型[%d]没找到",
			"no such field":                         "扩展字段为找到",
			"field_type cannot modify":              "字段类型不能被修改",
			"arg[endpoints] empty":                  "参数不能[endpoints]为空",
			"arg[cur_nid_paths] empty":              "参数不能[cur_nid_paths]为空",
			"arg[tags] empty":                       "参数不能[tags]为空",
			"arg[hosts] empty":                      "参数不能[hosts]为空",
			"arg[btime,etime] empty":                "参数[btime,etime]不合规范",
			"arg[name] empty":                       "参数[name]不合规范",
			"arg[name] is blank":                    "参数[名称]不能为空",
			"arg[ids] is empty":                     "参数[ids]不能为空",
			"%s invalid":                            "%s 不符合规范",
			"%s too long > 64":                      "%s 超过64长度限制",
			"arg[%s] too long > %d":                 "参数 %s 长度不能超过 %d",
			"cate is blank":                         "节点分类不能为空",
			"uuid is blank":                         "uuid不能为空",
			"ident is blank":                        "唯一标识不能为空",
			"tenant is blank":                       "租户不能为空",
			"ids is blank":                          "ids不能为空",
			"items empty":                           "提交内容不能为空",
			"url param[%s] is blank":                "url参数[%s]不能为空",
			"query param[%s] is necessary":          "query参数[%s]不能为空",
			"ident legal characters: [a-z0-9_-]":    "唯一标识英文只能字母开头，包括数字、中划线、下划线",
			"ident length should be less than 32":   "唯一标识长度需小于32",
			"cannot modify tenant's node-category":  "租户分类不允许修改",
			"cannot modify node-category to tenant": "节点分类不允许修改为租户",
			"node is managed by other system":       "租户正在被系统系统使用",
			"resources not found by %s":             "通过 %s 没有找到资源",
			"cannot delete root user":               "root用户不能删除",
			"user not found":                        "用户未找到",
			"Repositories":                          "Repositories",
			"List of repositories to monitor":       "List of repositories to monitor",
			"Access token":                          "Access token",
			"Github API access token.  Unauthenticated requests are limited to 60 per hour": "Github API access token.  Unauthenticated requests are limited to 60 per hour",
			"Enterprise base url": "Enterprise base url",
			"Github API enterprise url. Github Enterprise accounts must specify their base url": "Github API enterprise url. Github Enterprise accounts must specify their base url",
			"HTTP timeout":                                 "HTTP timeout",
			"Timeout for HTTP requests":                    "Timeout for HTTP requests",
			"Unable to get captcha":                        "无法获得验证码",
			"Invalid captcha answer":                       "错误的验证码",
			"Username %s is invalid":                       "用户名 %s 不符合规范",
			"Username %s too long > 64":                    "用户名 %s 太长(64)",
			"Unable to get login arguments":                "无法获得登陆参数",
			"Deny Access from %s with whitelist control":   "来自 %s 的访问被白名单规则拒绝",
			"Invalid login type %s":                        "不支持的登陆类型 %s",
			"Unable to get type, sms-code | email-code":    "无法获得验证码类型",
			"Unable to get code arg":                       "无法获得验证码类型",
			"sms/email sender is disabled":                 "无法发送 短信/邮件 验证码",
			"Invalid code type %s":                         "不支持的验证码类型 %s",
			"Cannot find the user by %s":                   "无法用 %s 找到相关用户",
			"Unable to get password":                       "无法获取密码",
			"Invalid code":                                 "不符合规范的验证码",
			"The code is incorrect":                        "无效的验证码",
			"The code has expired":                         "失效的验证码",
			"Invalid arguments %s":                         "不合法的参数 %s",
			"Login fail, check your username and password": "登陆失败，请检查用户名/密码",
			"User dose not exist":                          "用户不存在",
			"Username %s already exists":                   "用户名 %s 已存在",
			"Upper char":                                   "大写字母",
			"Lower char":                                   "小写字母",
			"Number":                                       "数字",
			"Special char":                                 "特殊字符",
			"Must include %s":                              "必须包含 %s",
			"Invalid Password, %s":                         "密码不符合规范, %s",
			"character: %s not supported":                  "不支持的字符 %s",
			"Incorrect login/password %s times, you still have %s chances": "登陆失败%d次，你还有%d次机会",
			"The limited sessions %d":                                      "会话数量限制，最多%d个会话",
			"Password has been expired":                                    "密码已过期，请重置密码",
			"User is inactive":                                             "用户已禁用",
			"User is locked":                                               "用户已锁定",
			"User is frozen":                                               "用户已休眠",
			"User is writen off":                                           "用户已注销",
			"Minimum password length %d":                                   "密码最小长度 %d",
			"Password too short (min:%d) %s":                               "密码太短 (最小 %d) %s",
			"%s format error":                                              "%s 所填内容不符合规范",
			"%s %s format error":                                           "%s %s 所填内容不符合规范",
			"username too long (max:%d)":                                   "用户名太长 (最长:%d)",
			"dispname too long (max:%d)":                                   "昵称太长 (最长:%d)",
			"email %s or phone %s is exists":                               "邮箱 %s 或者 手机号 %s 已存在",
			"Password is not set":                                          "密码未设置",
			"Incorrect old password":                                       "密码错误",
			"The password is the same as the old password":                 "密码与历史密码重复",
			"phone":                      "手机号",
			"email":                      "邮箱",
			"username":                   "用户名",
			"dispname":                   "昵称",
			"Temporary user has expired": "临时账户,已过有效期",
			"Invalid user status %d":     "异常的用户状态 %d",
			"Password expired, please change the password in time": "密码过期，请及时修改密码",
			"First Login, please change the password in time":      "初始登陆，请及时修改密码",
			"no privilege":       "privilege 未设置",
			"node is nil":        "node 未设置",
			"operation is blank": "operation  未设置",
		},
	}
)

func init() {
	i18n.DictRegister(langDict)
}
