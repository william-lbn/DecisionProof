package service

import (
	"fmt"
	"sort"
	"strings"

	"github.com/william-lbn/DecisionProof/internal/domain"
	"github.com/william-lbn/DecisionProof/internal/integrity"
)

// TransitionRule 描述一条确定性生命周期门禁。
type TransitionRule struct {
	From             domain.ChangeState    `json:"from"`
	To               domain.ChangeState    `json:"to"`
	RequiredEvidence []domain.EvidenceType `json:"required_evidence"`
	AllowedRoles     []string              `json:"allowed_roles"`
}

// TransitionPolicy 是内置的最小策略引擎。
// 生产环境可以在其前面接入 OPA，但最终输入、策略包摘要和决策都必须写入证据链。
type TransitionPolicy struct {
	rules  map[string]TransitionRule
	digest string
}

func NewTransitionPolicy() *TransitionPolicy {
	rules := []TransitionRule{
		{domain.StateDraft, domain.StateSpecReview, []domain.EvidenceType{domain.EvidenceSpecDocument}, []string{"product_manager", "admin"}},
		{domain.StateSpecReview, domain.StateSpecApproved, []domain.EvidenceType{domain.EvidenceSpecDocument, domain.EvidenceSpecApproval}, []string{"product_manager", "spec_reviewer", "admin"}},
		{domain.StateSpecApproved, domain.StateArchitectureReview, []domain.EvidenceType{domain.EvidenceArchitectureDocument}, []string{"architect", "admin"}},
		{domain.StateArchitectureReview, domain.StateReadyForImplementation, []domain.EvidenceType{domain.EvidenceArchitectureApproval, domain.EvidenceImplementationPlan}, []string{"architect", "architecture_reviewer", "admin"}},
		{domain.StateReadyForImplementation, domain.StateImplementing, []domain.EvidenceType{domain.EvidenceImplementationPlan}, []string{"developer", "coding_agent", "admin"}},
		{domain.StateImplementing, domain.StatePatchFrozen, []domain.EvidenceType{domain.EvidenceAgentRun, domain.EvidenceFrozenPatch}, []string{"developer", "coding_agent", "admin"}},
		{domain.StatePatchFrozen, domain.StateIndependentReview, []domain.EvidenceType{domain.EvidenceFrozenPatch}, []string{"reviewer", "review_agent", "admin"}},
		{domain.StateIndependentReview, domain.StateCIVerified, []domain.EvidenceType{domain.EvidenceIndependentReview, domain.EvidenceCITest, domain.EvidenceSecurityScan}, []string{"ci_controller", "release_manager", "admin"}},
		{domain.StateCIVerified, domain.StateArtifactQualified, []domain.EvidenceType{domain.EvidenceSBOM, domain.EvidenceBuildProvenance, domain.EvidenceArtifactSignature}, []string{"supply_chain_controller", "release_manager", "admin"}},
		{domain.StateArtifactQualified, domain.StateStagingVerified, []domain.EvidenceType{domain.EvidenceStagingTest}, []string{"test_manager", "release_manager", "admin"}},
		{domain.StateStagingVerified, domain.StateReleaseApproved, []domain.EvidenceType{domain.EvidenceReleaseApproval}, []string{"release_manager", "admin"}},
		{domain.StateReleaseApproved, domain.StateCanary, []domain.EvidenceType{domain.EvidenceDeployment}, []string{"deployment_controller", "release_manager", "admin"}},
		{domain.StateCanary, domain.StatePromoted, []domain.EvidenceType{domain.EvidenceCanaryAnalysis}, []string{"rollout_controller", "release_manager", "admin"}},
		{domain.StateCanary, domain.StateRolledBack, []domain.EvidenceType{domain.EvidenceRollback}, []string{"rollout_controller", "release_manager", "admin"}},
		{domain.StatePromoted, domain.StateOutcomeVerified, []domain.EvidenceType{domain.EvidenceRuntimeSignal, domain.EvidenceBusinessOutcome}, []string{"outcome_controller", "product_manager", "admin"}},
		{domain.StatePromoted, domain.StateOutcomeFailed, []domain.EvidenceType{domain.EvidenceRuntimeSignal, domain.EvidenceBusinessOutcome}, []string{"outcome_controller", "product_manager", "admin"}},
	}
	m := make(map[string]TransitionRule, len(rules))
	for _, rule := range rules {
		m[ruleKey(rule.From, rule.To)] = rule
	}
	digest, _ := integrity.SHA256Object(rules)
	return &TransitionPolicy{rules: m, digest: digest}
}

func ruleKey(from, to domain.ChangeState) string { return string(from) + "->" + string(to) }

// Digest 返回当前内置策略的不可变摘要。
func (p *TransitionPolicy) Digest() string { return p.digest }

// Evaluate 检查角色、状态边和必需证据。
func (p *TransitionPolicy) Evaluate(change domain.Change, target domain.ChangeState, roles []string, evidence []domain.EvidenceNode) error {
	rule, ok := p.rules[ruleKey(change.State, target)]
	if !ok {
		return fmt.Errorf("%w: %s -> %s", domain.ErrInvalidTransition, change.State, target)
	}
	if !hasAnyRole(roles, rule.AllowedRoles) {
		return fmt.Errorf("%w: 当前角色不能执行 %s -> %s", domain.ErrUnauthorized, change.State, target)
	}

	latest := map[domain.EvidenceType]domain.EvidenceVerdict{}
	for _, node := range evidence {
		// ListEvidence 按 Sequence 升序返回，因此后出现的同类型证据覆盖旧结论。
		latest[node.Type] = node.Verdict
	}
	missing := make([]string, 0)
	for _, required := range rule.RequiredEvidence {
		verdict, exists := latest[required]
		if !exists || (verdict != domain.VerdictPass && verdict != domain.VerdictObserved) {
			missing = append(missing, string(required))
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("%w: 缺少 %s", domain.ErrEvidenceInsufficient, strings.Join(missing, ", "))
	}

	// 业务结果失败只能进入 OUTCOME_FAILED，成功只能进入 OUTCOME_VERIFIED。
	if target == domain.StateOutcomeVerified || target == domain.StateOutcomeFailed {
		var outcome *domain.EvidenceNode
		for i := range evidence {
			if evidence[i].Type == domain.EvidenceBusinessOutcome {
				outcome = &evidence[i]
			}
		}
		if outcome == nil {
			return domain.ErrEvidenceInsufficient
		}
		if target == domain.StateOutcomeVerified && outcome.Verdict != domain.VerdictPass {
			return fmt.Errorf("%w: 业务结果未通过，不能标记为成功", domain.ErrPolicyDenied)
		}
		if target == domain.StateOutcomeFailed && outcome.Verdict == domain.VerdictPass {
			return fmt.Errorf("%w: 业务结果已通过，不能标记为失败", domain.ErrPolicyDenied)
		}
	}
	return nil
}

func hasAnyRole(actual, allowed []string) bool {
	set := make(map[string]struct{}, len(actual))
	for _, role := range actual {
		set[role] = struct{}{}
	}
	for _, role := range allowed {
		if _, ok := set[role]; ok {
			return true
		}
	}
	return false
}
