package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/william-lbn/DecisionProof/internal/domain"
	"github.com/william-lbn/DecisionProof/internal/store"
)

func TestEvidenceTamperingIsDetectedAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.json")
	repo, err := store.NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	clock := RealClock{}
	changes := NewChangeService(repo, clock, NewTransitionPolicy())
	evidence := NewEvidenceService(repo, clock)
	change, err := changes.Create(context.Background(), "tenant-a", "project-a", "测试篡改", "", "low")
	if err != nil {
		t.Fatal(err)
	}
	_, err = evidence.Add(context.Background(), AddEvidenceRequest{
		TenantID: change.TenantID, ChangeID: change.ID, Type: domain.EvidenceSpecDocument,
		Verdict: domain.VerdictPass, Producer: "user:pm", Subject: "spec",
		Payload: []byte(`{"requirement":"original"}`),
	})
	if err != nil {
		t.Fatal(err)
	}

	// 模拟攻击者绕过 API 直接修改持久化文件，但不可能同步伪造已有哈希链。
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot map[string]any
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatal(err)
	}
	nodes := snapshot["evidence_nodes"].(map[string]any)
	for _, raw := range nodes {
		node := raw.(map[string]any)
		node["payload"] = map[string]any{"requirement": "tampered"}
		break
	}
	data, _ = json.Marshal(snapshot)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	reopened, err := store.NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	verifier := NewEvidenceService(reopened, clock)
	if err := verifier.VerifyChain(context.Background(), change.TenantID, change.ID); err == nil {
		t.Fatal("篡改证据后完整性验证必须失败")
	}
}
