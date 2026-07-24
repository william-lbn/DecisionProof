package httpapi

import (
	"context"
	"net/http"
	"strings"
)

type identityKey struct{}

// Identity 是上游认证代理注入的平台身份。
// 参考实现读取请求头仅用于本地部署；公网生产环境必须由 mTLS/OIDC 网关清洗并重写这些头。
type Identity struct {
	TenantID string
	ActorID  string
	Roles    []string
}

func withIdentity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tenant := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
		actor := strings.TrimSpace(r.Header.Get("X-Actor-ID"))
		if tenant == "" || actor == "" {
			writeError(w, http.StatusUnauthorized, "缺少可信身份头 X-Tenant-ID 或 X-Actor-ID")
			return
		}
		rolesRaw := strings.Split(r.Header.Get("X-Actor-Roles"), ",")
		roles := make([]string, 0, len(rolesRaw))
		for _, role := range rolesRaw {
			if role = strings.TrimSpace(role); role != "" {
				roles = append(roles, role)
			}
		}
		ctx := context.WithValue(r.Context(), identityKey{}, Identity{TenantID: tenant, ActorID: actor, Roles: roles})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func identityFromContext(ctx context.Context) Identity {
	identity, _ := ctx.Value(identityKey{}).(Identity)
	return identity
}
