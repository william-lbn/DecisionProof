package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/william-lbn/DecisionProof/internal/domain"
	"github.com/william-lbn/DecisionProof/internal/executor"
	"github.com/william-lbn/DecisionProof/internal/security"
	"github.com/william-lbn/DecisionProof/internal/service"
	"github.com/william-lbn/DecisionProof/internal/store"
)

// demo 在一个进程内跑通“需求—架构—代码—测试—制品—灰度—结果”的完整证据链。
func main() {
	ctx := context.Background()
	repo := store.NewMemoryStore()
	clock := service.RealClock{}
	transitionPolicy := service.NewTransitionPolicy()
	changes := service.NewChangeService(repo, clock, transitionPolicy)
	evidence := service.NewEvidenceService(repo, clock)
	local := executor.NewLocalExecutor()
	actions := service.NewActionGateway(repo, clock, service.NewActionPolicy(), executor.StaticRegistry{Executor: local})
	signer, err := security.NewEphemeralSigner()
	must(err)
	releases := service.NewReleaseService(repo, clock, transitionPolicy, signer)

	change, err := changes.Create(ctx, "tenant-demo", "order-service", "批量退款", "支持批量退款并禁止重复退款", "high")
	must(err)
	add := func(typ domain.EvidenceType, verdict domain.EvidenceVerdict, producer, subject string, payload any) {
		data, _ := json.Marshal(payload)
		_, err := evidence.Add(ctx, service.AddEvidenceRequest{TenantID: change.TenantID, ChangeID: change.ID, Type: typ, Verdict: verdict, Producer: producer, TrustClass: "VERIFIED", Subject: subject, Payload: data})
		must(err)
	}
	move := func(target domain.ChangeState, actor string, roles ...string) {
		var err error
		change, _, err = changes.Transition(ctx, change.TenantID, change.ID, actor, roles, target, "demo-"+string(target))
		must(err)
	}

	add(domain.EvidenceSpecDocument, domain.VerdictPass, "user:pm", "spec", map[string]any{"requirement": "批量退款", "forbidden": []string{"duplicate_refund"}})
	move(domain.StateSpecReview, "user:pm", "product_manager")
	add(domain.EvidenceSpecApproval, domain.VerdictPass, "user:spec-reviewer", "spec", map[string]any{"approved": true})
	move(domain.StateSpecApproved, "user:spec-reviewer", "spec_reviewer")
	add(domain.EvidenceArchitectureDocument, domain.VerdictPass, "user:architect", "architecture", map[string]any{"pattern": "idempotent-command"})
	move(domain.StateArchitectureReview, "user:architect", "architect")
	add(domain.EvidenceArchitectureApproval, domain.VerdictPass, "user:arch-reviewer", "architecture", map[string]any{"approved": true})
	add(domain.EvidenceImplementationPlan, domain.VerdictPass, "agent:planner", "plan", map[string]any{"tasks": 3})
	move(domain.StateReadyForImplementation, "user:arch-reviewer", "architecture_reviewer")
	move(domain.StateImplementing, "agent:author", "coding_agent")
	add(domain.EvidenceAgentRun, domain.VerdictPass, "agent:author", "run", map[string]any{"sandbox": "isolated"})
	patchDigest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	add(domain.EvidenceFrozenPatch, domain.VerdictPass, "agent:author", patchDigest, map[string]any{"files": 4})
	move(domain.StatePatchFrozen, "agent:author", "coding_agent")
	move(domain.StateIndependentReview, "agent:reviewer", "review_agent")
	add(domain.EvidenceIndependentReview, domain.VerdictPass, "agent:reviewer", patchDigest, map[string]any{"author_identity": "agent:author", "reviewer_identity": "agent:reviewer", "patch_digest": patchDigest, "decision": "approve"})
	add(domain.EvidenceCITest, domain.VerdictPass, "controller:ci", "pipeline", map[string]any{"passed": 128, "failed": 0})
	add(domain.EvidenceSecurityScan, domain.VerdictPass, "controller:security", "scan", map[string]any{"critical_findings": 0, "high_findings": 0})
	move(domain.StateCIVerified, "controller:ci", "ci_controller")
	artifactDigest := "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	add(domain.EvidenceSBOM, domain.VerdictPass, "controller:build", artifactDigest, map[string]any{"format": "cyclonedx"})
	add(domain.EvidenceBuildProvenance, domain.VerdictPass, "controller:tekton-chains", artifactDigest, map[string]any{"slsa": "v1"})
	add(domain.EvidenceArtifactSignature, domain.VerdictPass, "controller:sigstore", artifactDigest, map[string]any{"verified": true})
	move(domain.StateArtifactQualified, "controller:supply-chain", "supply_chain_controller")

	add(domain.EvidenceStagingTest, domain.VerdictPass, "controller:test", artifactDigest, map[string]any{"scenarios": 25})
	move(domain.StateStagingVerified, "controller:test", "test_manager")
	add(domain.EvidenceReleaseApproval, domain.VerdictPass, "user:release-manager", artifactDigest, map[string]any{"approved": true})
	move(domain.StateReleaseApproved, "user:release-manager", "release_manager")

	manifest, err := releases.GenerateManifest(ctx, change.TenantID, change.ID, "user:release-manager", "production", "canary")
	must(err)
	must(releases.VerifyManifest(manifest))

	action, err := actions.Submit(ctx, domain.ActionProposal{
		TenantID: change.TenantID, ChangeID: change.ID, Actor: "user:release-manager",
		ActorRoles: []string{"release_manager"}, Intent: "deploy.start_canary",
		Target: "k8s://production/order-service", RiskLevel: "high",
		Parameters: []byte(`{"artifact":"` + artifactDigest + `"}`), IdempotencyKey: "deploy-demo",
		RequiresHumanCheck: true,
	})
	must(err)
	add(domain.EvidenceDeployment, domain.VerdictPass, "controller:argo-rollouts", artifactDigest, map[string]any{"external_ref": action.ExternalRef})
	move(domain.StateCanary, "controller:deploy", "deployment_controller")
	add(domain.EvidenceCanaryAnalysis, domain.VerdictPass, "controller:rollout", artifactDigest, map[string]any{"error_rate": 0.001})
	move(domain.StatePromoted, "controller:rollout", "rollout_controller")
	add(domain.EvidenceRuntimeSignal, domain.VerdictObserved, "controller:otel", artifactDigest, map[string]any{"window": "2h"})
	add(domain.EvidenceBusinessOutcome, domain.VerdictPass, "controller:outcome", artifactDigest, map[string]any{"duplicate_refund": 0, "success_rate": 0.999})
	move(domain.StateOutcomeVerified, "controller:outcome", "outcome_controller")
	must(evidence.VerifyChain(ctx, change.TenantID, change.ID))

	output, _ := json.MarshalIndent(map[string]any{"change": change, "manifest": manifest, "action": action}, "", "  ")
	fmt.Println(string(output))
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
