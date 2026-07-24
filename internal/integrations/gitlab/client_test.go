package gitlab

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestReadRetries429(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			w.Header().Set("Retry-After", "0")
			http.Error(w, "rate", http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":1,"path_with_namespace":"g/p","default_branch":"main"}`))
	}))
	defer srv.Close()
	c, _ := NewClient(srv.URL, "token", AuthPrivateToken)
	c.MaxReadAttempts = 2
	p, err := c.GetProject(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != 1 || calls != 2 {
		t.Fatalf("bad project/calls %+v %d", p, calls)
	}
}
func TestWriteDoesNotRetry(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { atomic.AddInt32(&calls, 1); http.Error(w, "down", 500) }))
	defer srv.Close()
	c, _ := NewClient(srv.URL, "token", AuthPrivateToken)
	_, err := c.CreateBranch(context.Background(), 1, "x", "main")
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Fatalf("write retried %d", calls)
	}
}

func TestClientRejectsNonAbsoluteOrCredentialedBaseURL(t *testing.T) {
	for _, value := range []string{"localhost:8080", "/gitlab", "https://user:secret@gitlab.example"} {
		if _, err := NewClient(value, "token", AuthPrivateToken); err == nil {
			t.Fatalf("必须拒绝不安全 BaseURL %q", value)
		}
	}
	if _, err := NewClient("https://gitlab.example", "", AuthPrivateToken); err == nil {
		t.Fatal("必须拒绝空 Token")
	}
	if _, err := NewClient("https://gitlab.example", "token", AuthMode("unknown")); err == nil {
		t.Fatal("必须拒绝未知 AuthMode")
	}
}

func TestClientDoesNotFollowRedirectWithToken(t *testing.T) {
	var redirected int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect-target" {
			atomic.AddInt32(&redirected, 1)
			if r.Header.Get("PRIVATE-TOKEN") != "" {
				t.Error("重定向请求泄漏了 PRIVATE-TOKEN")
			}
			return
		}
		http.Redirect(w, r, server.URL+"/redirect-target", http.StatusFound)
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "sensitive-token", AuthPrivateToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetProject(context.Background(), 1); err == nil {
		t.Fatal("GitLab Client 必须拒绝 3xx 而不是跟随")
	}
	if redirected != 0 {
		t.Fatalf("GitLab Client 跟随了重定向: %d", redirected)
	}
}

func TestProviderErrorBodyRedactsSecrets(t *testing.T) {
	body := []byte(`{"access_token":"secret-value","message":"secret-value"}`)
	safe := safeBody(body, "secret-value")
	if strings.Contains(safe, "secret-value") || !strings.Contains(safe, "[REDACTED]") {
		t.Fatalf("Provider 错误体脱敏失败: %s", safe)
	}
}

func TestWebhookHMAC(t *testing.T) {
	body := []byte(`{"x":1}`)
	secret := "s"
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	h := http.Header{}
	h.Set("Webhook-Signature", hex.EncodeToString(mac.Sum(nil)))
	if err := (WebhookVerifier{SigningSecret: secret}).Verify(h, body); err != nil {
		t.Fatal(err)
	}
	body[0] = '['
	if err := (WebhookVerifier{SigningSecret: secret}).Verify(h, body); err == nil {
		t.Fatal("expected signature failure")
	}
}
