package delivery

import (
	"strings"
	"testing"
	"time"

	"github.com/william-lbn/DecisionProof/internal/product"
)

func TestRenderEngineeringBundle(t *testing.T) {
	pkg := product.EngineeringReadinessPackage{
		ID: "ERP-001", ProjectID: "order-service", Digest: "sha256:package", CreatedBy: "owner",
		DecisionIDs: []string{"req-1", "acc-1", "out-1"}, CreatedAt: time.Now(),
	}
	decisions := []product.ProductDecision{
		{ID: "req-1", Kind: product.DecisionRequirement, Title: "退款", Content: "支持批量退款", Status: product.DecisionApproved, CanonicalDigest: "sha256:req"},
		{ID: "acc-1", Kind: product.DecisionAcceptanceCriterion, Title: "验收", Content: "重复退款为零", Status: product.DecisionApproved, CanonicalDigest: "sha256:acc"},
		{ID: "out-1", Kind: product.DecisionOutcomeContract, Title: "结果", Content: "成功率不低于 99.5%", Status: product.DecisionApproved, CanonicalDigest: "sha256:out"},
	}
	bundle, err := RenderEngineeringBundle(pkg, decisions)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.SuggestedBranch != "decisionproof/erp-001" || len(bundle.Files) != 2 {
		t.Fatalf("Bundle 元数据错误: %#v", bundle)
	}
	if !strings.Contains(bundle.IssueDescription, "支持批量退款") || !strings.Contains(bundle.IssueDescription, pkg.Digest) {
		t.Fatalf("Issue 内容不完整: %s", bundle.IssueDescription)
	}
}

func TestRenderRejectsUnapprovedDecision(t *testing.T) {
	pkg := product.EngineeringReadinessPackage{ID: "ERP-1", Digest: "sha256:x", DecisionIDs: []string{"d1"}}
	_, err := RenderEngineeringBundle(pkg, []product.ProductDecision{{ID: "d1", Status: product.DecisionConfirmed}})
	if err == nil {
		t.Fatal("未批准决策不应被渲染")
	}
}
