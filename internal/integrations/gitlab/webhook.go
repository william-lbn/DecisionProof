package gitlab

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/william-lbn/DecisionProof/internal/integrity"
)

// WebhookVerifier 同时支持 GitLab HMAC Webhook-Signature 和旧版 X-Gitlab-Token。
type WebhookVerifier struct {
	SigningSecret string
	LegacyToken   string
}

func (v WebhookVerifier) Verify(header http.Header, body []byte) error {
	if sig := strings.TrimSpace(header.Get("Webhook-Signature")); sig != "" {
		if v.SigningSecret == "" {
			return errors.New("服务端未配置 GitLab HMAC signing secret")
		}
		provided := strings.TrimPrefix(sig, "sha256=")
		decoded, err := hex.DecodeString(provided)
		if err != nil {
			return errors.New("Webhook-Signature 不是合法十六进制摘要")
		}
		mac := hmac.New(sha256.New, []byte(v.SigningSecret))
		_, _ = mac.Write(body)
		if !hmac.Equal(decoded, mac.Sum(nil)) {
			return errors.New("GitLab Webhook HMAC 验证失败")
		}
		return nil
	}
	if v.LegacyToken != "" && hmac.Equal([]byte(header.Get("X-Gitlab-Token")), []byte(v.LegacyToken)) {
		return nil
	}
	return errors.New("GitLab Webhook 缺少有效签名")
}

// WebhookEnvelope 是持久化到 Inbox 的最小统一信封。
type WebhookEnvelope struct {
	EventUUID     string `json:"event_uuid"`
	WebhookUUID   string `json:"webhook_uuid"`
	EventType     string `json:"event_type"`
	Instance      string `json:"instance"`
	PayloadDigest string `json:"payload_digest"`
	Payload       []byte `json:"payload"`
}

func BuildEnvelope(h http.Header, body []byte) (WebhookEnvelope, error) {
	if len(body) == 0 {
		return WebhookEnvelope{}, errors.New("Webhook Body 不能为空")
	}
	e := WebhookEnvelope{EventUUID: firstNonEmpty(h.Get("X-Gitlab-Event-UUID"), h.Get("Idempotency-Key")), WebhookUUID: h.Get("X-Gitlab-Webhook-UUID"), EventType: h.Get("X-Gitlab-Event"), Instance: h.Get("X-Gitlab-Instance"), PayloadDigest: integrity.SHA256Bytes(body), Payload: append([]byte(nil), body...)}
	if e.EventUUID == "" {
		e.EventUUID = fmt.Sprintf("digest:%s", e.PayloadDigest)
	}
	return e, nil
}
func firstNonEmpty(v ...string) string {
	for _, x := range v {
		if strings.TrimSpace(x) != "" {
			return x
		}
	}
	return ""
}
