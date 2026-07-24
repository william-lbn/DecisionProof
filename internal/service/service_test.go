package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/william-lbn/DecisionProof/internal/domain"
	"github.com/william-lbn/DecisionProof/internal/executor"
	"github.com/william-lbn/DecisionProof/internal/security"
	"github.com/william-lbn/DecisionProof/internal/store"
)

type fixedClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fixedClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	// 每次推进 1 纳秒，保证测试顺序稳定，同时验证业务不依赖真实墙钟等待。
	c.t = c.t.Add(time.Nanosecond)
	return c.t
}

type fixture struct {
	ctx       context.Context
	repo      *store.MemoryStore
	clock     *fixedClock
	policy    *TransitionPolicy
	changes   *ChangeService
	evidence  *EvidenceService
	actions   *ActionGateway
	localExec *executor.LocalExecutor
	releases  *ReleaseService
	change    domain.Change
	patch     string
	artifact  string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	repo := store.NewMemoryStore()
	clock := &fixedClock{t: time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)}
	policy := NewTransitionPolicy()
	changes := NewChangeService(repo, clock, policy)
	evidence := NewEvidenceService(repo, clock)
	local := executor.NewLocalExecutor()
	actions := NewActionGateway(repo, clock, NewActionPolicy(), executor.StaticRegistry{Executor: local})
	signer, err := security.NewEphemeralSigner()
	if err != nil {
		t.Fatal(err)
	}
	releases := NewReleaseService(repo, clock, policy, signer)
	change, err := changes.Create(context.Background(), "tenant-a", "order-service", "批量退款", "测试", "high")
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{
		ctx: context.Background(), repo: repo, clock: clock, policy: policy,
		changes: changes, evidence: evidence, actions: actions, localExec: local,
		releases: releases, change: change,
		patch:    "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		artifact: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	}
}

func (f *fixture) add(t *testing.T, typ domain.EvidenceType, verdict domain.EvidenceVerdict, producer, subject string, payload any) domain.EvidenceNode {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	node, err := f.evidence.Add(f.ctx, AddEvidenceRequest{
		TenantID: f.change.TenantID, ChangeID: f.change.ID, Type: typ,
		Verdict: verdict, Producer: producer, TrustClass: "VERIFIED",
		Subject: subject, Payload: data,
	})
	if err != nil {
		t.Fatalf("添加证据 %s 失败: %v", typ, err)
	}
	return node
}

func (f *fixture) move(t *testing.T, target domain.ChangeState, actor string, roles ...string) {
	t.Helper()
	var err error
	f.change, _, err = f.changes.Transition(f.ctx, f.change.TenantID, f.change.ID, actor, roles, target, "test-"+string(target))
	if err != nil {
		t.Fatalf("迁移到 %s 失败: %v", target, err)
	}
}

func (f *fixture) advanceToArtifactQualified(t *testing.T) {
	f.add(t, domain.EvidenceSpecDocument, domain.VerdictPass, "user:pm", "spec", map[string]any{"requirement": "refund"})
	f.move(t, domain.StateSpecReview, "user:pm", "product_manager")
	f.add(t, domain.EvidenceSpecApproval, domain.VerdictPass, "user:spec-reviewer", "spec", map[string]any{"approved": true})
	f.move(t, domain.StateSpecApproved, "user:spec-reviewer", "spec_reviewer")
	f.add(t, domain.EvidenceArchitectureDocument, domain.VerdictPass, "user:architect", "architecture", map[string]any{"pattern": "idempotent"})
	f.move(t, domain.StateArchitectureReview, "user:architect", "architect")
	f.add(t, domain.EvidenceArchitectureApproval, domain.VerdictPass, "user:arch-reviewer", "architecture", map[string]any{"approved": true})
	f.add(t, domain.EvidenceImplementationPlan, domain.VerdictPass, "agent:planner", "plan", map[string]any{"tasks": 2})
	f.move(t, domain.StateReadyForImplementation, "user:arch-reviewer", "architecture_reviewer")
	f.move(t, domain.StateImplementing, "agent:author", "coding_agent")
	f.add(t, domain.EvidenceAgentRun, domain.VerdictPass, "agent:author", "run", map[string]any{"sandbox": true})
	f.add(t, domain.EvidenceFrozenPatch, domain.VerdictPass, "agent:author", f.patch, map[string]any{"files": 4})
	f.move(t, domain.StatePatchFrozen, "agent:author", "coding_agent")
	f.move(t, domain.StateIndependentReview, "agent:reviewer", "review_agent")
	f.add(t, domain.EvidenceIndependentReview, domain.VerdictPass, "agent:reviewer", f.patch, map[string]any{
		"author_identity": "agent:author", "reviewer_identity": "agent:reviewer", "patch_digest": f.patch, "decision": "approve",
	})
	f.add(t, domain.EvidenceCITest, domain.VerdictPass, "controller:ci", "pipeline", map[string]any{"passed": 100, "failed": 0})
	f.add(t, domain.EvidenceSecurityScan, domain.VerdictPass, "controller:security", "scan", map[string]any{"critical_findings": 0, "high_findings": 0})
	f.move(t, domain.StateCIVerified, "controller:ci", "ci_controller")
	f.add(t, domain.EvidenceSBOM, domain.VerdictPass, "controller:build", f.artifact, map[string]any{"format": "cyclonedx"})
	f.add(t, domain.EvidenceBuildProvenance, domain.VerdictPass, "controller:chains", f.artifact, map[string]any{"slsa": "v1"})
	f.add(t, domain.EvidenceArtifactSignature, domain.VerdictPass, "controller:sigstore", f.artifact, map[string]any{"verified": true})
	f.move(t, domain.StateArtifactQualified, "controller:supply-chain", "supply_chain_controller")
}

func TestTransitionFailsClosedWithoutEvidence(t *testing.T) {
	f := newFixture(t)
	_, _, err := f.changes.Transition(f.ctx, f.change.TenantID, f.change.ID, "user:pm", []string{"product_manager"}, domain.StateSpecReview, "missing-evidence")
	if !errors.Is(err, domain.ErrEvidenceInsufficient) {
		t.Fatalf("期望缺证据拒绝，实际错误: %v", err)
	}
	current, err := f.changes.Get(f.ctx, f.change.TenantID, f.change.ID)
	if err != nil || current.State != domain.StateDraft {
		t.Fatalf("拒绝后状态不应改变: state=%s err=%v", current.State, err)
	}
}

func TestIndependentReviewRejectsSelfReview(t *testing.T) {
	f := newFixture(t)
	f.add(t, domain.EvidenceSpecDocument, domain.VerdictPass, "user:pm", "spec", map[string]any{"requirement": "refund"})
	f.move(t, domain.StateSpecReview, "user:pm", "product_manager")
	// 直接构造到 Patch 状态所需内容，测试重点是审查身份隔离。
	f.add(t, domain.EvidenceFrozenPatch, domain.VerdictPass, "agent:author", f.patch, map[string]any{"files": 1})
	data, _ := json.Marshal(map[string]any{"author_identity": "agent:author", "reviewer_identity": "agent:author", "patch_digest": f.patch, "decision": "approve"})
	_, err := f.evidence.Add(f.ctx, AddEvidenceRequest{
		TenantID: f.change.TenantID, ChangeID: f.change.ID, Type: domain.EvidenceIndependentReview,
		Verdict: domain.VerdictPass, Producer: "agent:author", Subject: f.patch, Payload: data,
	})
	if err == nil {
		t.Fatal("作者自审必须被拒绝")
	}
}

func TestReleaseManifestIsSignedAndStable(t *testing.T) {
	f := newFixture(t)
	f.advanceToArtifactQualified(t)
	f.add(t, domain.EvidenceStagingTest, domain.VerdictPass, "controller:test", f.artifact, map[string]any{"passed": true})
	f.move(t, domain.StateStagingVerified, "controller:test", "test_manager")
	f.add(t, domain.EvidenceReleaseApproval, domain.VerdictPass, "user:release", f.artifact, map[string]any{"approved": true})
	f.move(t, domain.StateReleaseApproved, "user:release", "release_manager")
	manifest, err := f.releases.GenerateManifest(f.ctx, f.change.TenantID, f.change.ID, "user:release", "production", "canary")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.releases.VerifyManifest(manifest); err != nil {
		t.Fatalf("发布清单签名验证失败: %v", err)
	}
	manifest2, err := f.releases.GenerateManifest(f.ctx, f.change.TenantID, f.change.ID, "other", "production", "blue-green")
	if err != nil {
		t.Fatal(err)
	}
	if manifest2.ContentDigest != manifest.ContentDigest {
		t.Fatal("重复生成必须返回同一不可变清单")
	}
	manifest.ArtifactDigest = f.patch
	if err := f.releases.VerifyManifest(manifest); err == nil {
		t.Fatal("修改清单后签名验证必须失败")
	}
}

func TestActionCommitThenTimeoutCanReconcileWithoutDuplicate(t *testing.T) {
	f := newFixture(t)
	proposal := domain.ActionProposal{
		TenantID: f.change.TenantID, ChangeID: f.change.ID, Actor: "agent:author",
		ActorRoles: []string{"coding_agent"}, Intent: "git.create_pull_request",
		Target: "repo://order-service", RiskLevel: "low",
		Parameters:     []byte(`{"simulate":"commit_then_timeout"}`),
		IdempotencyKey: "commit-then-timeout",
	}
	record, err := f.actions.Submit(f.ctx, proposal)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != domain.ActionUnknown {
		t.Fatalf("期望 UNKNOWN，实际 %s", record.Status)
	}
	reconciled, err := f.actions.Reconcile(f.ctx, f.change.TenantID, record.Proposal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reconciled.Status != domain.ActionSucceeded {
		t.Fatalf("对账后应成功，实际 %s", reconciled.Status)
	}
	if count := f.localExec.Count(f.change.TenantID, proposal.IdempotencyKey); count != 1 {
		t.Fatalf("外部动作只能执行一次，实际 %d", count)
	}
}

func TestConcurrentActionIdempotency(t *testing.T) {
	f := newFixture(t)
	const workers = 64
	var wg sync.WaitGroup
	wg.Add(workers)
	errs := make(chan error, workers)
	ids := make(chan string, workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			record, err := f.actions.Submit(f.ctx, domain.ActionProposal{
				TenantID: f.change.TenantID, ChangeID: f.change.ID, Actor: "agent:author",
				ActorRoles: []string{"coding_agent"}, Intent: "git.create_pull_request",
				Target: "repo://order-service", RiskLevel: "low", Parameters: []byte(`{"branch":"change"}`),
				IdempotencyKey: "create-pr-once",
			})
			if err != nil {
				errs <- err
				return
			}
			ids <- record.Proposal.ID
		}()
	}
	wg.Wait()
	close(errs)
	close(ids)
	for err := range errs {
		t.Fatal(err)
	}
	unique := map[string]bool{}
	for id := range ids {
		unique[id] = true
	}
	if len(unique) != 1 {
		t.Fatalf("幂等请求产生了 %d 个动作对象", len(unique))
	}
	if count := f.localExec.Count(f.change.TenantID, "create-pr-once"); count != 1 {
		t.Fatalf("外部执行次数应为 1，实际 %d", count)
	}
}

func TestLatestFailedEvidenceOverridesOldPass(t *testing.T) {
	f := newFixture(t)
	f.advanceToArtifactQualified(t)
	// 在旧 PASS 之后写入同类型 FAIL，后续发布资格必须使用最新结论。
	f.add(t, domain.EvidenceSecurityScan, domain.VerdictFail, "controller:security", "scan-2", map[string]any{"critical_findings": 1})
	f.add(t, domain.EvidenceStagingTest, domain.VerdictPass, "controller:test", f.artifact, map[string]any{"passed": true})
	f.move(t, domain.StateStagingVerified, "controller:test", "test_manager")
	f.add(t, domain.EvidenceReleaseApproval, domain.VerdictPass, "user:release", f.artifact, map[string]any{"approved": true})
	f.move(t, domain.StateReleaseApproved, "user:release", "release_manager")
	if _, err := f.releases.GenerateManifest(f.ctx, f.change.TenantID, f.change.ID, "user:release", "production", "canary"); !errors.Is(err, domain.ErrEvidenceInsufficient) {
		t.Fatalf("最新 FAIL 必须阻断发布清单，实际错误: %v", err)
	}
}

func TestProductionActionCannotUseSelfDeclaredApproval(t *testing.T) {
	f := newFixture(t)
	_, err := f.actions.Submit(f.ctx, domain.ActionProposal{
		TenantID: f.change.TenantID, ChangeID: f.change.ID, Actor: "user:release",
		ActorRoles: []string{"release_manager"}, Intent: "deploy.start_canary",
		Target: "k8s://production/order-service", RiskLevel: "high",
		Parameters: []byte(`{"artifact":"sha256:x"}`), IdempotencyKey: "fake-approval",
		RequiresHumanCheck: true,
	})
	if !errors.Is(err, domain.ErrEvidenceInsufficient) {
		t.Fatalf("仅自报人工审批不能通过，实际错误: %v", err)
	}
}

func TestFullLifecycleAndEvidenceChain(t *testing.T) {
	f := newFixture(t)
	f.advanceToArtifactQualified(t)
	f.add(t, domain.EvidenceStagingTest, domain.VerdictPass, "controller:test", f.artifact, map[string]any{"passed": true})
	f.move(t, domain.StateStagingVerified, "controller:test", "test_manager")
	f.add(t, domain.EvidenceReleaseApproval, domain.VerdictPass, "user:release", f.artifact, map[string]any{"approved": true})
	f.move(t, domain.StateReleaseApproved, "user:release", "release_manager")
	f.add(t, domain.EvidenceDeployment, domain.VerdictPass, "controller:argo", f.artifact, map[string]any{"generation": 9})
	f.move(t, domain.StateCanary, "controller:deploy", "deployment_controller")
	f.add(t, domain.EvidenceCanaryAnalysis, domain.VerdictPass, "controller:rollout", f.artifact, map[string]any{"error_rate": 0.001})
	f.move(t, domain.StatePromoted, "controller:rollout", "rollout_controller")
	f.add(t, domain.EvidenceRuntimeSignal, domain.VerdictObserved, "controller:otel", f.artifact, map[string]any{"window": "2h"})
	f.add(t, domain.EvidenceBusinessOutcome, domain.VerdictPass, "controller:outcome", f.artifact, map[string]any{"success_rate": 0.999})
	f.move(t, domain.StateOutcomeVerified, "controller:outcome", "outcome_controller")
	if err := f.evidence.VerifyChain(f.ctx, f.change.TenantID, f.change.ID); err != nil {
		t.Fatal(err)
	}
	if f.change.State != domain.StateOutcomeVerified {
		t.Fatalf("最终状态错误: %s", f.change.State)
	}
}

func BenchmarkTransitionPolicyEvaluate(b *testing.B) {
	policy := NewTransitionPolicy()
	change := domain.Change{State: domain.StateIndependentReview}
	evidence := []domain.EvidenceNode{
		{Type: domain.EvidenceIndependentReview, Verdict: domain.VerdictPass, Sequence: 1},
		{Type: domain.EvidenceCITest, Verdict: domain.VerdictPass, Sequence: 2},
		{Type: domain.EvidenceSecurityScan, Verdict: domain.VerdictPass, Sequence: 3},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := policy.Evaluate(change, domain.StateCIVerified, []string{"ci_controller"}, evidence); err != nil {
			b.Fatal(err)
		}
	}
}
