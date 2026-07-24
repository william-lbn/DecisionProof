package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/william-lbn/DecisionProof/internal/integrity"
	"github.com/william-lbn/DecisionProof/internal/product"
)

// HTTPJSONExtractor 连接企业内部 Agent、OpenAI/Codex 代理或其他模型网关。
// 远端必须返回严格的 DecisionDraft JSON；平台不接受自由文本直接进入正式决策。
type HTTPJSONExtractor struct {
	Endpoint  string
	APIKey    string
	Provider  string
	ModelName string
	Client    *http.Client
}

func (h *HTTPJSONExtractor) Name() string {
	if h.Provider == "" {
		return "http-json"
	}
	return h.Provider
}
func (h *HTTPJSONExtractor) Model() string { return h.ModelName }
func (h *HTTPJSONExtractor) Extract(ctx context.Context, blocks []product.SourceBlock) ([]product.DecisionDraft, string, error) {
	if h.Endpoint == "" {
		return nil, "", errors.New("AI 提取 Endpoint 不能为空")
	}
	payload := map[string]any{"schema_version": "decision-draft-v1", "model": h.ModelName, "blocks": blocks}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, "", err
	}
	promptDigest := integrity.SHA256Bytes(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, promptDigest, err
	}
	req.Header.Set("Content-Type", "application/json")
	if h.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+h.APIKey)
	}
	client := h.Client
	if client == nil {
		client = &http.Client{Timeout: 90 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, promptDigest, fmt.Errorf("调用 AI 提取服务失败: %w", err)
	}
	defer resp.Body.Close()
	limited := io.LimitReader(resp.Body, 8<<20)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, promptDigest, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, promptDigest, fmt.Errorf("AI 提取服务返回 HTTP %d: %s", resp.StatusCode, string(data))
	}
	var result struct {
		Decisions []product.DecisionDraft `json:"decisions"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&result); err != nil {
		return nil, promptDigest, fmt.Errorf("AI 返回结构不符合 DecisionDraft Schema: %w", err)
	}
	return result.Decisions, promptDigest, nil
}
