// Package authn 在 HTTP 边界验证身份，Domain 不依赖 OIDC 或请求头。
package authn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

type Mode string

const (
	ModeDevelopmentHeaders Mode = "development_headers"
	ModeOIDCBearer         Mode = "oidc_bearer"
)

// Identity 是经过认证适配器验证后注入下游的稳定身份。
type Identity struct {
	TenantID string
	ActorID  string
	Roles    []string
}

// Config 控制认证模式。生产环境只有显式 OIDC 配置可以启动；
// development_headers 必须通过单独开关确认其仅用于隔离实验。
type Config struct {
	Mode                            Mode
	Production                      bool
	AllowInsecureDevelopmentHeaders bool
	IssuerURL                       string
	Audience                        string
	TenantClaim                     string
	RolesClaim                      string
}

// ClaimsVerifier 隔离具体 OIDC SDK，便于用可控签名服务器做 Contract Test。
type ClaimsVerifier interface {
	Verify(ctx context.Context, rawToken string) (Identity, error)
}

// NewMiddleware 构造认证中间件。健康、就绪、指标、静态 UI 和 Webhook 不在这里认证，
// 其余 API 必须先得到可信身份；OIDC 模式会先删除客户端伪造的身份头。
func NewMiddleware(ctx context.Context, cfg Config) (func(http.Handler) http.Handler, error) {
	switch cfg.Mode {
	case ModeDevelopmentHeaders:
		if cfg.Production && !cfg.AllowInsecureDevelopmentHeaders {
			return nil, errors.New("生产环境禁止默认信任开发身份头")
		}
		return func(next http.Handler) http.Handler { return next }, nil
	case ModeOIDCBearer:
		verifier, err := NewOIDCVerifier(ctx, cfg)
		if err != nil {
			return nil, err
		}
		return NewBearerMiddleware(verifier), nil
	default:
		return nil, fmt.Errorf("不支持的认证模式 %q", cfg.Mode)
	}
}

func NewBearerMiddleware(verifier ClaimsVerifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !protectedPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			// 任何上游自报身份都必须先删除，只有验签后的 Claim 可以重新写入。
			r.Header.Del("X-Tenant-ID")
			r.Header.Del("X-Actor-ID")
			r.Header.Del("X-Actor-Roles")
			rawToken, ok := bearerToken(r.Header.Get("Authorization"))
			if !ok {
				writeAuthError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "缺少 OIDC Bearer Token")
				return
			}
			verifyCtx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			defer cancel()
			identity, err := verifier.Verify(verifyCtx, rawToken)
			if err != nil {
				writeAuthError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "OIDC Token 验证失败")
				return
			}
			if err := validateIdentity(identity); err != nil {
				writeAuthError(w, http.StatusForbidden, "INVALID_IDENTITY", err.Error())
				return
			}
			r.Header.Set("X-Tenant-ID", identity.TenantID)
			r.Header.Set("X-Actor-ID", identity.ActorID)
			r.Header.Set("X-Actor-Roles", strings.Join(identity.Roles, ","))
			next.ServeHTTP(w, r)
		})
	}
}

type OIDCVerifier struct {
	verifier    *oidc.IDTokenVerifier
	tenantClaim string
	rolesClaim  string
}

func NewOIDCVerifier(ctx context.Context, cfg Config) (*OIDCVerifier, error) {
	issuer := strings.TrimSpace(cfg.IssuerURL)
	audience := strings.TrimSpace(cfg.Audience)
	if issuer == "" || audience == "" {
		return nil, errors.New("OIDC IssuerURL 和 Audience 不能为空")
	}
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("OIDC Discovery 失败: %w", err)
	}
	tenantClaim := strings.TrimSpace(cfg.TenantClaim)
	if tenantClaim == "" {
		tenantClaim = "tenant_id"
	}
	rolesClaim := strings.TrimSpace(cfg.RolesClaim)
	if rolesClaim == "" {
		rolesClaim = "roles"
	}
	return &OIDCVerifier{
		verifier:    provider.Verifier(&oidc.Config{ClientID: audience}),
		tenantClaim: tenantClaim,
		rolesClaim:  rolesClaim,
	}, nil
}

func (v *OIDCVerifier) Verify(ctx context.Context, rawToken string) (Identity, error) {
	token, err := v.verifier.Verify(ctx, rawToken)
	if err != nil {
		return Identity{}, err
	}
	var claims map[string]json.RawMessage
	if err := token.Claims(&claims); err != nil {
		return Identity{}, fmt.Errorf("解析 OIDC Claims 失败: %w", err)
	}
	tenantID, err := stringClaim(claims, v.tenantClaim)
	if err != nil {
		return Identity{}, err
	}
	roles, err := rolesClaim(claims, v.rolesClaim)
	if err != nil {
		return Identity{}, err
	}
	if strings.TrimSpace(token.Subject) == "" {
		return Identity{}, errors.New("OIDC Token 缺少 Subject")
	}
	return Identity{TenantID: tenantID, ActorID: "oidc:" + token.Subject, Roles: roles}, nil
}

func protectedPath(requestPath string) bool {
	return strings.HasPrefix(requestPath, "/v1/") ||
		strings.HasPrefix(requestPath, "/product/v1/") ||
		strings.HasPrefix(requestPath, "/integrations/v1/")
}

func bearerToken(value string) (string, bool) {
	fields := strings.Fields(value)
	returnValue := ""
	if len(fields) == 2 && strings.EqualFold(fields[0], "Bearer") {
		returnValue = fields[1]
	}
	return returnValue, returnValue != ""
}

func validateIdentity(identity Identity) error {
	if strings.TrimSpace(identity.TenantID) == "" || strings.TrimSpace(identity.ActorID) == "" {
		return errors.New("OIDC 身份缺少租户或 Subject")
	}
	if len(identity.TenantID) > 200 || len(identity.ActorID) > 500 || len(identity.Roles) > 32 {
		return errors.New("OIDC 身份字段超过安全限制")
	}
	for _, role := range identity.Roles {
		if role == "" || len(role) > 100 || strings.ContainsAny(role, ",\r\n") {
			return errors.New("OIDC 角色格式无效")
		}
	}
	return nil
}

func stringClaim(claims map[string]json.RawMessage, name string) (string, error) {
	var value string
	if raw, ok := claims[name]; !ok || json.Unmarshal(raw, &value) != nil || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("OIDC Claim %q 缺失或不是非空字符串", name)
	}
	return strings.TrimSpace(value), nil
}

func rolesClaim(claims map[string]json.RawMessage, name string) ([]string, error) {
	raw, ok := claims[name]
	if !ok {
		return nil, fmt.Errorf("OIDC Claim %q 缺失", name)
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		var single string
		if json.Unmarshal(raw, &single) != nil {
			return nil, fmt.Errorf("OIDC Claim %q 必须是字符串或字符串数组", name)
		}
		values = strings.Split(single, ",")
	}
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; !exists {
			out = append(out, value)
			seen[value] = struct{}{}
		}
	}
	return out, nil
}

func writeAuthError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "message": message})
}
