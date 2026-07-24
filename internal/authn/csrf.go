package authn

import (
	"net/http"
	"net/url"
	"strings"
)

// SameOrigin 拒绝浏览器发起的跨站修改请求。OIDC Bearer 本身不是 Cookie，
// 但这层门禁防止未来引入 Session 时静默扩大 CSRF 攻击面。
func SameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if safeMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		if strings.EqualFold(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")), "cross-site") {
			writeAuthError(w, http.StatusForbidden, "CSRF_REJECTED", "拒绝跨站修改请求")
			return
		}
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Scheme == "" || !strings.EqualFold(parsed.Host, r.Host) {
			writeAuthError(w, http.StatusForbidden, "CSRF_REJECTED", "请求 Origin 与服务 Host 不一致")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func safeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	default:
		return false
	}
}
