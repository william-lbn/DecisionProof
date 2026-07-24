package authn

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
)

// TestOIDCDiscoveryJWKSAndSignature 使用一次性真实 RSA/JWKS 服务验证 Discovery、
// Signature、Issuer、Audience、Expiry 和 Claim 映射；它仍属于本地加密集成测试，不是企业 IdP Truth Test。
func TestOIDCDiscoveryJWKSAndSignature(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keyID := "decisionproof-test-key"
	var issuer string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer": issuer, "jwks_uri": issuer + "/jwks",
				"id_token_signing_alg_values_supported": []string{"RS256"},
			})
		case "/jwks":
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
				"kty": "RSA", "use": "sig", "alg": "RS256", "kid": keyID,
				"n": base64.RawURLEncoding.EncodeToString(privateKey.PublicKey.N.Bytes()),
				"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(privateKey.PublicKey.E)).Bytes()),
			}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	issuer = server.URL

	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: privateKey},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", keyID),
	)
	if err != nil {
		t.Fatal(err)
	}
	claims, _ := json.Marshal(map[string]any{
		"iss": issuer, "aud": "decisionproof-api", "sub": "user-1",
		"exp": time.Now().Add(time.Minute).Unix(), "iat": time.Now().Add(-time.Second).Unix(),
		"tenant_id": "tenant-oidc", "roles": []string{"product_manager", "auditor"},
	})
	object, err := signer.Sign(claims)
	if err != nil {
		t.Fatal(err)
	}
	rawToken, err := object.CompactSerialize()
	if err != nil {
		t.Fatal(err)
	}
	middleware, err := NewMiddleware(context.Background(), Config{
		Mode: ModeOIDCBearer, IssuerURL: issuer, Audience: "decisionproof-api",
		TenantClaim: "tenant_id", RolesClaim: "roles",
	})
	if err != nil {
		t.Fatal(err)
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Tenant-ID") != "tenant-oidc" ||
			r.Header.Get("X-Actor-ID") != "oidc:user-1" ||
			r.Header.Get("X-Actor-Roles") != "product_manager,auditor" {
			t.Fatalf("OIDC Claim 未正确注入: tenant=%q actor=%q roles=%q",
				r.Header.Get("X-Tenant-ID"), r.Header.Get("X-Actor-ID"), r.Header.Get("X-Actor-Roles"))
		}
		w.WriteHeader(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodGet, "/product/v1/decisions", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	rec := httptest.NewRecorder()
	middleware(next).ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("真实 RSA/JWKS Token 应通过，实际 %d: %s", rec.Code, rec.Body.String())
	}
}
