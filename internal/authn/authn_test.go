package authn

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fixedVerifier struct {
	identity Identity
	err      error
}

func (v fixedVerifier) Verify(context.Context, string) (Identity, error) {
	return v.identity, v.err
}

func TestBearerMiddlewareRejectsSpoofedIdentityAndInjectsVerifiedClaims(t *testing.T) {
	var gotTenant, gotActor, gotRoles string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTenant = r.Header.Get("X-Tenant-ID")
		gotActor = r.Header.Get("X-Actor-ID")
		gotRoles = r.Header.Get("X-Actor-Roles")
		w.WriteHeader(http.StatusNoContent)
	})
	handler := NewBearerMiddleware(fixedVerifier{identity: Identity{
		TenantID: "tenant-verified", ActorID: "oidc:user-1", Roles: []string{"product_manager"},
	}})(next)
	req := httptest.NewRequest(http.MethodGet, "/product/v1/decisions", nil)
	req.Header.Set("Authorization", "Bearer signed-token")
	req.Header.Set("X-Tenant-ID", "tenant-spoofed")
	req.Header.Set("X-Actor-ID", "admin")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("有效 Token 应通过，实际 %d", rec.Code)
	}
	if gotTenant != "tenant-verified" || gotActor != "oidc:user-1" || gotRoles != "product_manager" {
		t.Fatalf("未使用验签身份: tenant=%q actor=%q roles=%q", gotTenant, gotActor, gotRoles)
	}
}

func TestBearerMiddlewareFailsClosed(t *testing.T) {
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("认证失败时不应进入业务处理器")
	})
	handler := NewBearerMiddleware(fixedVerifier{err: errors.New("signature invalid")})(next)
	for _, authorization := range []string{"", "Basic abc", "Bearer invalid"} {
		req := httptest.NewRequest(http.MethodPost, "/v1/changes", nil)
		req.Header.Set("Authorization", authorization)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("Authorization=%q 应返回 401，实际 %d", authorization, rec.Code)
		}
	}
}

func TestDevelopmentHeadersRequireExplicitProductionOverride(t *testing.T) {
	_, err := NewMiddleware(context.Background(), Config{Mode: ModeDevelopmentHeaders, Production: true})
	if err == nil {
		t.Fatal("生产环境不得隐式信任开发身份头")
	}
}

func TestSameOriginRejectsCrossSiteMutation(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	handler := SameOrigin(next)
	req := httptest.NewRequest(http.MethodPost, "https://decisionproof.example/product/v1/decisions", nil)
	req.Header.Set("Origin", "https://attacker.example")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("跨站修改必须拒绝，实际 %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "https://decisionproof.example/product/v1/decisions", nil)
	req.Header.Set("Origin", "https://decisionproof.example")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("同源修改应通过，实际 %d", rec.Code)
	}
}
