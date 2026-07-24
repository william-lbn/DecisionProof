package gitlab

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWebhookHandlerDeduplicatesUUID(t *testing.T) {
	secret := "test-secret"
	body := []byte(`{"project":{"id":101},"object_kind":"merge_request"}`)
	inbox := NewMemoryInbox()
	handler := WebhookHandler{
		IntegrationID: "gitlab-demo",
		Verifier:      WebhookVerifier{SigningSecret: secret},
		Inbox:         inbox,
		Now:           func() time.Time { return time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC) },
	}

	first := performWebhookRequest(t, handler, body, secret, "event-001")
	if first.Code != http.StatusAccepted {
		t.Fatalf("首次请求状态错误: %d, body=%s", first.Code, first.Body.String())
	}
	var firstPayload map[string]any
	if err := json.Unmarshal(first.Body.Bytes(), &firstPayload); err != nil {
		t.Fatal(err)
	}
	if duplicate, _ := firstPayload["duplicate"].(bool); duplicate {
		t.Fatal("首次请求不应被标记为重复")
	}

	second := performWebhookRequest(t, handler, body, secret, "event-001")
	var secondPayload map[string]any
	if err := json.Unmarshal(second.Body.Bytes(), &secondPayload); err != nil {
		t.Fatal(err)
	}
	if duplicate, _ := secondPayload["duplicate"].(bool); !duplicate {
		t.Fatal("重复 Event UUID 应被去重")
	}
	records, err := inbox.List(InboxPending, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].ProjectID != 101 {
		t.Fatalf("Inbox 内容不符合预期: %#v", records)
	}
}

func TestWebhookHandlerRejectsInvalidSignature(t *testing.T) {
	handler := WebhookHandler{
		IntegrationID: "gitlab-demo",
		Verifier:      WebhookVerifier{SigningSecret: "correct-secret"},
		Inbox:         NewMemoryInbox(),
	}
	req := httptest.NewRequest(http.MethodPost, "/webhooks/gitlab", bytes.NewBufferString(`{"project_id":1}`))
	req.Header.Set("Webhook-Signature", "sha256=invalid")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("签名错误应返回 401，实际 %d", recorder.Code)
	}
}

func TestFileInboxPersistsAndDeduplicates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inbox.json")
	inbox, err := NewFileInbox(path)
	if err != nil {
		t.Fatal(err)
	}
	record := InboxRecord{
		ID: "WH-1", IntegrationID: "int-1", EventUUID: "uuid-1", EventType: "Push Hook",
		PayloadDigest: "sha256:test", Payload: json.RawMessage(`{"project_id":1}`), Status: InboxPending,
		ReceivedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	stored, _, err := inbox.PutIfAbsent(record)
	if err != nil || !stored {
		t.Fatalf("首次持久化失败: stored=%v err=%v", stored, err)
	}
	reloaded, err := NewFileInbox(path)
	if err != nil {
		t.Fatal(err)
	}
	stored, existing, err := reloaded.PutIfAbsent(record)
	if err != nil || stored || existing.ID != record.ID {
		t.Fatalf("重启后去重失败: stored=%v existing=%#v err=%v", stored, existing, err)
	}
}

func performWebhookRequest(t *testing.T, handler http.Handler, body []byte, secret, uuid string) *httptest.ResponseRecorder {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	req := httptest.NewRequest(http.MethodPost, "/webhooks/gitlab", bytes.NewReader(body))
	req.Header.Set("Webhook-Signature", signature)
	req.Header.Set("X-Gitlab-Event-UUID", uuid)
	req.Header.Set("X-Gitlab-Event", "Merge Request Hook")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func TestFileInboxRollsBackMemoryWhenPersistFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inbox.json")
	inbox, err := NewFileInbox(path)
	if err != nil {
		t.Fatal(err)
	}
	// 切换到已有目录，使最终 Rename 失败。
	badPath := filepath.Join(t.TempDir(), "directory")
	if err := os.Mkdir(badPath, 0o700); err != nil {
		t.Fatal(err)
	}
	inbox.path = badPath
	record := InboxRecord{ID: "WH-fail", IntegrationID: "i", EventUUID: "u", PayloadDigest: "sha256:x", Status: InboxPending, ReceivedAt: time.Now(), UpdatedAt: time.Now()}
	if _, _, err := inbox.PutIfAbsent(record); err == nil {
		t.Fatal("持久化失败应返回错误")
	}
	if _, err := inbox.Get(record.ID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("持久化失败后内存状态未回滚: %v", err)
	}
}
