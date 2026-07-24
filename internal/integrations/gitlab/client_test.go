package gitlab

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
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
