package product

import (
	"context"
	"testing"
	"time"

	"github.com/william-lbn/DecisionProof/internal/integrity"
)

type fixedClock struct{ t time.Time }

func (f fixedClock) Now() time.Time { return f.t }

type fakeExtractor struct{}

func (fakeExtractor) Name() string  { return "fake" }
func (fakeExtractor) Model() string { return "test" }
func (fakeExtractor) Extract(ctx context.Context, blocks []SourceBlock) ([]DecisionDraft, string, error) {
	_ = ctx
	anchor := blocks[0].Anchor
	return []DecisionDraft{{Kind: DecisionRequirement, Title: "需求", Content: "支持可信发布", Confidence: 0.9, Anchors: []SourceAnchor{anchor}}, {Kind: DecisionAcceptanceCriterion, Title: "验收", Content: "无签名制品拒绝", Confidence: 0.9, Anchors: []SourceAnchor{anchor}}, {Kind: DecisionOutcomeContract, Title: "结果", Content: "失败发布率低于1%", Confidence: 0.9, Anchors: []SourceAnchor{anchor}}}, "prompt", nil
}

func TestDecisionToEngineeringPackage(t *testing.T) {
	repo := NewMemoryRepository()
	svc := NewService(repo, fixedClock{time.Unix(100, 0).UTC()})
	ctx := context.Background()
	doc, err := svc.RegisterDocument(ctx, "t1", "p1", "prd.md", "text/markdown", "file://prd", "sha256:x", "u1")
	if err != nil {
		t.Fatal(err)
	}
	text := "需求：支持可信发布"
	block := SourceBlock{ID: "b1", Kind: "line", Locator: "line:1", Text: text, Digest: integrity.SHA256Bytes([]byte(text))}
	if _, err = svc.SaveParsedBlocks(ctx, "t1", doc.ID, []SourceBlock{block}); err != nil {
		t.Fatal(err)
	}
	_, decisions, err := svc.RunExtraction(ctx, "t1", doc.ID, fakeExtractor{})
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 3 {
		t.Fatalf("want 3, got %d", len(decisions))
	}
	for _, d := range decisions {
		confirmed, err := svc.ConfirmDecision(ctx, "t1", d.ID, "pm", 1)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = svc.ApproveDecision(ctx, "t1", d.ID, "owner", confirmed.Version); err != nil {
			t.Fatal(err)
		}
	}
	pkg, err := svc.BuildEngineeringPackage(ctx, "t1", "p1", "owner")
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Digest == "" || len(pkg.RequirementIDs) != 1 || len(pkg.AcceptanceIDs) != 1 || len(pkg.OutcomeContractIDs) != 1 {
		t.Fatalf("bad package %+v", pkg)
	}
}

func TestMemoryRepositoryExtractionIsAtomic(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryRepository()
	doc := SourceDocument{ID: "doc-atomic", TenantID: "tenant-atomic", ProjectID: "project-atomic", Status: DocumentUploaded, Version: 1}
	if err := repo.SaveDocument(ctx, doc); err != nil {
		t.Fatal(err)
	}
	existing := ProductDecision{ID: "decision-conflict", TenantID: doc.TenantID, ProjectID: doc.ProjectID}
	if err := repo.SaveDecision(ctx, existing); err != nil {
		t.Fatal(err)
	}
	processed := doc
	processed.Status = DocumentProcessed
	processed.Version = 2
	run := ExtractionRun{ID: "run-atomic", TenantID: doc.TenantID, ProjectID: doc.ProjectID, DocumentID: doc.ID}
	newDecision := ProductDecision{ID: "decision-new", TenantID: doc.TenantID, ProjectID: doc.ProjectID}
	if err := repo.SaveExtraction(ctx, processed, run, []ProductDecision{newDecision, existing}); err == nil {
		t.Fatal("决策冲突时原子提取必须失败")
	}
	stored, err := repo.GetDocument(ctx, doc.TenantID, doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != DocumentUploaded || stored.Version != 1 {
		t.Fatalf("失败后文档状态被部分更新: %#v", stored)
	}
	if _, err := repo.GetDecision(ctx, doc.TenantID, newDecision.ID); err != ErrNotFound {
		t.Fatalf("失败后不应留下部分新决策: %v", err)
	}
	if _, ok := repo.runs[key(doc.TenantID, run.ID)]; ok {
		t.Fatal("失败后不应留下提取运行记录")
	}
}

func TestApproveRequiresConfirm(t *testing.T) {
	repo := NewMemoryRepository()
	svc := NewService(repo, nil)
	d := ProductDecision{ID: "d1", TenantID: "t", ProjectID: "p", Kind: DecisionRequirement, Title: "x", Content: "y", Status: DecisionExtracted, Version: 1}
	if err := repo.SaveDecision(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApproveDecision(context.Background(), "t", "d1", "u", 1); err == nil {
		t.Fatal("expected error")
	}
}

func TestApprovalRequiresIndependentActor(t *testing.T) {
	repo := NewMemoryRepository()
	svc := NewService(repo, nil)
	now := time.Now().UTC()
	d := ProductDecision{ID: "d-independent", TenantID: "t", ProjectID: "p", Kind: DecisionRequirement, Origin: DecisionOriginManual, Title: "x", Content: "y", Status: DecisionConfirmed, ConfirmedBy: "same-user", Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := repo.SaveDecision(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApproveDecision(context.Background(), "t", d.ID, "same-user", 1); err == nil {
		t.Fatal("确认人不应批准自己的决策")
	}
}

func TestEngineeringPackageFailsClosedWhenReferencedDecisionChanges(t *testing.T) {
	repo := NewMemoryRepository()
	svc := NewService(repo, RealClock{})
	ctx := context.Background()
	decisions := []ProductDecision{
		{ID: "req", TenantID: "t1", ProjectID: "p1", Kind: DecisionRequirement, Status: DecisionApproved, CanonicalDigest: "sha256:req", Version: 1},
		{ID: "acc", TenantID: "t1", ProjectID: "p1", Kind: DecisionAcceptanceCriterion, Status: DecisionApproved, CanonicalDigest: "sha256:acc", Version: 1},
		{ID: "out", TenantID: "t1", ProjectID: "p1", Kind: DecisionOutcomeContract, Status: DecisionApproved, CanonicalDigest: "sha256:out", Version: 1},
	}
	for _, item := range decisions {
		if err := repo.SaveDecision(ctx, item); err != nil {
			t.Fatal(err)
		}
	}
	pkg, err := svc.BuildEngineeringPackage(ctx, "t1", "p1", "delivery-manager")
	if err != nil {
		t.Fatal(err)
	}
	changed := decisions[0]
	changed.CanonicalDigest = "sha256:changed"
	changed.Version++
	if err := repo.UpdateDecision(ctx, changed, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetPackageDecisions(ctx, "t1", pkg); err == nil {
		t.Fatal("引用决策变化后必须拒绝工程包交付")
	}
}
