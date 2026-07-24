package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/william-lbn/DecisionProof/internal/product"
)

func TestHTTPJSONExtractor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Fatalf("Authorization 错误: %s", got)
		}
		var input map[string]any
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"decisions": []map[string]any{{
			"kind": "REQUIREMENT", "title": "需求", "content": "支持可信发布", "confidence": 0.9,
			"anchors": []map[string]any{{"document_id": "d1", "block_id": "b1", "locator": "line:1", "digest": "sha256:x"}},
		}}})
	}))
	defer server.Close()
	extractor := &HTTPJSONExtractor{Endpoint: server.URL, APIKey: "secret", Provider: "gateway", ModelName: "model"}
	result, digest, err := extractor.Extract(context.Background(), []product.SourceBlock{{ID: "b1", DocumentID: "d1", Text: "需求"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0].Kind != product.DecisionRequirement || digest == "" {
		t.Fatalf("提取结果错误: %#v digest=%s", result, digest)
	}
}

func TestHTTPJSONExtractorRejectsUnknownOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"decisions":[],"unexpected":true}`))
	}))
	defer server.Close()
	_, _, err := (&HTTPJSONExtractor{Endpoint: server.URL}).Extract(context.Background(), nil)
	if err == nil {
		t.Fatal("未知字段应被拒绝")
	}
}
