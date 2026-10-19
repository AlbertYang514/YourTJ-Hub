package urlconfig

import "strings"

// IsSafeRedirect 仅允许站内相对路径，拒绝 javascript:、//host、\host、/\host 等危险值。
// 浏览器按 WHATWG URL 规范会把 \ 归一化为 /，因此路径中任何位置的反斜杠都要拦截。
// forum.IsSafeRedirect（登录页 props、同济登录）与 oauthservice（社交登录续跳）共用此规则。
func IsSafeRedirect(value string) bool {
	if value == "" {
		return false
	}
	if !strings.HasPrefix(value, "/") {
		return false
	}
	if strings.Contains(value, `\`) {
		return false
	}
	if len(value) > 1 && value[1] == '/' {
		return false
	}
	return true
}
