package gitlab

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/william-lbn/DecisionProof/internal/integrity"
)

const defaultWebhookBodyLimit = 2 << 20 // 2 MiB

// WebhookHandler 完成快速、可靠且幂等的 GitLab Webhook 接收。
// 它不会在 HTTP 请求内执行业务动作，只把验证后的原始事件写入 Inbox。
type WebhookHandler struct {
	TenantID      string
	IntegrationID string
	Verifier      WebhookVerifier
	Inbox         InboxStore
	BodyLimit     int64
	Now           func() time.Time
}

func (h WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeWebhookError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "只允许 POST")
		return
	}
	if h.Inbox == nil {
		writeWebhookError(w, http.StatusServiceUnavailable, "INBOX_UNAVAILABLE", "Webhook Inbox 未配置")
		return
	}
	limit := h.BodyLimit
	if limit <= 0 {
		limit = defaultWebhookBodyLimit
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		writeWebhookError(w, http.StatusRequestEntityTooLarge, "BODY_TOO_LARGE", "Webhook 请求体超过限制")
		return
	}
	if len(body) == 0 {
		writeWebhookError(w, http.StatusBadRequest, "EMPTY_BODY", "Webhook 请求体不能为空")
		return
	}
	if err := h.Verifier.Verify(r.Header, body); err != nil {
		writeWebhookError(w, http.StatusUnauthorized, "INVALID_SIGNATURE", "Webhook 签名验证失败")
		return
	}
	var envelopePayload struct {
		Project struct {
			ID int64 `json:"id"`
		} `json:"project"`
		ProjectID int64 `json:"project_id"`
	}
	if err := json.Unmarshal(body, &envelopePayload); err != nil {
		writeWebhookError(w, http.StatusBadRequest, "INVALID_JSON", "Webhook JSON 无法解析")
		return
	}
	projectID := envelopePayload.Project.ID
	if projectID == 0 {
		projectID = envelopePayload.ProjectID
	}
	now := time.Now().UTC()
	if h.Now != nil {
		now = h.Now().UTC()
	}
	eventUUID := strings.TrimSpace(r.Header.Get("X-Gitlab-Event-UUID"))
	digest := PayloadDigest(body)
	if eventUUID == "" {
		eventUUID = "digest:" + digest
	}
	id := "WH-" + randomHex(12)
	record := InboxRecord{
		ID:            id,
		TenantID:      strings.TrimSpace(h.TenantID),
		IntegrationID: strings.TrimSpace(h.IntegrationID),
		EventUUID:     eventUUID,
		EventType:     strings.TrimSpace(r.Header.Get("X-Gitlab-Event")),
		ProjectID:     projectID,
		PayloadDigest: digest,
		Payload:       append(json.RawMessage(nil), body...),
		Status:        InboxPending,
		ReceivedAt:    now,
		UpdatedAt:     now,
	}
	stored, existing, err := h.Inbox.PutIfAbsent(record)
	if err != nil {
		writeWebhookError(w, http.StatusInternalServerError, "INBOX_WRITE_FAILED", "Webhook Inbox 写入失败")
		return
	}
	response := map[string]any{
		"accepted":        true,
		"duplicate":       !stored,
		"inbox_record_id": record.ID,
		"payload_digest":  digest,
	}
	if !stored {
		response["inbox_record_id"] = existing.ID
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(response)
}

func writeWebhookError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "message": message})
}

func randomHex(n int) string {
	if n <= 0 {
		return ""
	}
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(buf)
}

// PayloadDigest 计算 Webhook 原始请求体摘要，用于缺失 Event UUID 时的降级去重。
func PayloadDigest(body []byte) string {
	return integrity.SHA256Bytes(body)
}

// MarkProcessing 为后台 Reconciler 提供安全的处理状态更新辅助函数。
func MarkProcessing(store InboxStore, record InboxRecord, now time.Time) error {
	if store == nil {
		return errors.New("Inbox Store 不能为空")
	}
	if record.Status != InboxPending && record.Status != InboxFailed {
		return fmt.Errorf("只有 PENDING 或 FAILED 事件可进入 PROCESSING，当前状态为 %s", record.Status)
	}
	record.Status = InboxProcessing
	record.Attempts++
	record.LastError = ""
	record.UpdatedAt = now.UTC()
	return store.Update(record)
}
