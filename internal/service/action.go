package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/william-lbn/DecisionProof/internal/domain"
	"github.com/william-lbn/DecisionProof/internal/executor"
	"github.com/william-lbn/DecisionProof/internal/integrity"
	"github.com/william-lbn/DecisionProof/internal/store"
)

// ActionPolicy 对可信工具网关动作实施最小权限控制。
type ActionPolicy struct{ digest string }

func NewActionPolicy() *ActionPolicy {
	model := map[string]any{
		"allowed_prefixes": []string{"git.", "ci.", "artifact.", "deploy.", "observability."},
		"high_risk_roles":  []string{"release_manager", "admin"},
	}
	digest, _ := integrity.SHA256Object(model)
	return &ActionPolicy{digest: digest}
}

func (p *ActionPolicy) Digest() string { return p.digest }

func (p *ActionPolicy) Evaluate(proposal domain.ActionProposal, change domain.Change, evidence []domain.EvidenceNode) error {
	allowedPrefix := false
	for _, prefix := range []string{"git.", "ci.", "artifact.", "deploy.", "observability."} {
		if strings.HasPrefix(proposal.Intent, prefix) {
			allowedPrefix = true
			break
		}
	}
	if !allowedPrefix {
		return fmt.Errorf("%w: 未注册的动作意图 %s", domain.ErrPolicyDenied, proposal.Intent)
	}
	production := strings.Contains(strings.ToLower(proposal.Target), "production")
	if proposal.RiskLevel == "high" || production {
		if !hasAnyRole(proposal.ActorRoles, []string{"release_manager", "admin"}) {
			return fmt.Errorf("%w: 高风险或生产动作需要 release_manager/admin", domain.ErrUnauthorized)
		}
		if !proposal.RequiresHumanCheck {
			return fmt.Errorf("%w: 高风险或生产动作必须声明需要人工门禁", domain.ErrPolicyDenied)
		}
		latestApproval := domain.VerdictFail
		for _, node := range evidence {
			if node.Type == domain.EvidenceReleaseApproval {
				latestApproval = node.Verdict
			}
		}
		if latestApproval != domain.VerdictPass {
			return fmt.Errorf("%w: 缺少最新且通过的发布批准证据", domain.ErrEvidenceInsufficient)
		}
	}
	if production && strings.HasPrefix(proposal.Intent, "deploy.") {
		if change.State != domain.StateReleaseApproved && change.State != domain.StateCanary {
			return fmt.Errorf("%w: 生产部署动作只能从 RELEASE_APPROVED/CANARY 状态发起", domain.ErrPolicyDenied)
		}
		if change.ArtifactDigest == "" {
			return fmt.Errorf("%w: 生产部署缺少不可变制品摘要", domain.ErrEvidenceInsufficient)
		}
	}
	return nil
}

// ActionGateway 把 Agent 的自由工具调用转化为受策略控制、可结算和可对账的动作事务。
type ActionGateway struct {
	repo     store.Repository
	clock    Clock
	policy   *ActionPolicy
	registry executor.Registry
}

func NewActionGateway(repo store.Repository, clock Clock, policy *ActionPolicy, registry executor.Registry) *ActionGateway {
	return &ActionGateway{repo: repo, clock: clock, policy: policy, registry: registry}
}

func (g *ActionGateway) Submit(ctx context.Context, proposal domain.ActionProposal) (domain.ActionRecord, error) {
	ctx = store.WithTenant(ctx, proposal.TenantID)
	if proposal.TenantID == "" || proposal.ChangeID == "" || proposal.Actor == "" || proposal.IdempotencyKey == "" {
		return domain.ActionRecord{}, errors.New("动作缺少 tenant_id、change_id、actor 或 idempotency_key")
	}
	if len(proposal.Parameters) == 0 {
		proposal.Parameters = []byte(`{}`)
	}
	if !jsonValid(proposal.Parameters) {
		return domain.ActionRecord{}, errors.New("动作 parameters 必须是合法 JSON")
	}
	if proposal.ID == "" {
		id, err := domain.NewID("act")
		if err != nil {
			return domain.ActionRecord{}, err
		}
		proposal.ID = id
	}
	proposal.ParametersDigest = integrity.SHA256Bytes(proposal.Parameters)

	now := g.clock.Now()
	initial := domain.ActionRecord{
		Proposal: proposal, Status: domain.ActionPending, Version: 1,
		PolicyDigest: g.policy.Digest(), CreatedAt: now, UpdatedAt: now,
	}
	var record domain.ActionRecord
	var created bool
	if err := g.repo.Transact(store.WithTenant(ctx, proposal.TenantID), func(tx store.Tx) error {
		change, err := tx.GetChange(proposal.TenantID, proposal.ChangeID)
		if err != nil {
			return err
		}
		evidence, err := tx.ListEvidence(proposal.TenantID, proposal.ChangeID)
		if err != nil {
			return err
		}
		if err := g.policy.Evaluate(proposal, change, evidence); err != nil {
			return err
		}
		record, created, err = tx.CreateActionIfAbsent(initial)
		return err
	}); err != nil {
		return domain.ActionRecord{}, err
	}
	if !created {
		return record, nil
	}

	exec, err := g.registry.Resolve(proposal.Intent)
	if err != nil {
		return g.settle(ctx, record, domain.ActionFailed, executor.Result{}, err)
	}
	record, err = g.markExecuting(ctx, record)
	if err != nil {
		return domain.ActionRecord{}, err
	}
	result, execErr := exec.Execute(ctx, proposal)
	switch {
	case execErr == nil:
		return g.settle(context.WithoutCancel(ctx), record, domain.ActionSucceeded, result, nil)
	case errors.Is(execErr, executor.ErrOutcomeUnknown), errors.Is(execErr, context.DeadlineExceeded), errors.Is(execErr, context.Canceled):
		return g.settle(context.WithoutCancel(ctx), record, domain.ActionUnknown, result, execErr)
	default:
		return g.settle(context.WithoutCancel(ctx), record, domain.ActionFailed, result, execErr)
	}
}

func (g *ActionGateway) Reconcile(ctx context.Context, tenantID, actionID string) (domain.ActionRecord, error) {
	ctx = store.WithTenant(ctx, tenantID)
	var record domain.ActionRecord
	if err := g.repo.Transact(store.WithTenant(ctx, tenantID), func(tx store.Tx) error {
		var err error
		record, err = tx.GetAction(tenantID, actionID)
		return err
	}); err != nil {
		return domain.ActionRecord{}, err
	}
	if record.Status != domain.ActionUnknown && record.Status != domain.ActionExecuting {
		return record, nil
	}
	exec, err := g.registry.Resolve(record.Proposal.Intent)
	if err != nil {
		return record, err
	}
	result, status, err := exec.Query(ctx, record.Proposal, record.ExternalRef)
	if err != nil {
		return record, err
	}
	return g.settle(ctx, record, status, result, nil)
}

func (g *ActionGateway) Get(ctx context.Context, tenantID, actionID string) (domain.ActionRecord, error) {
	ctx = store.WithTenant(ctx, tenantID)
	var result domain.ActionRecord
	err := g.repo.Transact(store.WithTenant(ctx, tenantID), func(tx store.Tx) error {
		var err error
		result, err = tx.GetAction(tenantID, actionID)
		return err
	})
	return result, err
}

func (g *ActionGateway) markExecuting(ctx context.Context, record domain.ActionRecord) (domain.ActionRecord, error) {
	updated := record
	updated.Status = domain.ActionExecuting
	updated.AttemptCount++
	updated.Version++
	updated.UpdatedAt = g.clock.Now()
	err := g.repo.Transact(store.WithTenant(ctx, record.Proposal.TenantID), func(tx store.Tx) error { return tx.UpdateAction(updated, record.Version) })
	return updated, err
}

func (g *ActionGateway) settle(ctx context.Context, record domain.ActionRecord, status domain.ActionStatus, result executor.Result, cause error) (domain.ActionRecord, error) {
	updated := record
	updated.Status = status
	updated.ExternalRef = result.ExternalRef
	updated.Receipt = result.Receipt
	updated.Version++
	updated.UpdatedAt = g.clock.Now()
	checked := g.clock.Now()
	updated.LastCheckedAt = &checked
	if cause != nil {
		updated.ErrorMessage = cause.Error()
	}
	switch status {
	case domain.ActionSucceeded:
		updated.SettlementNote = "已通过外部回执确认动作成功"
	case domain.ActionUnknown:
		updated.SettlementNote = "结果未知，禁止自动重复执行，必须对账"
	case domain.ActionFailed:
		updated.SettlementNote = "已确认动作失败或未产生目标效果"
	}
	if err := g.repo.Transact(store.WithTenant(ctx, record.Proposal.TenantID), func(tx store.Tx) error { return tx.UpdateAction(updated, record.Version) }); err != nil {
		return domain.ActionRecord{}, err
	}
	return updated, nil
}

func jsonValid(data []byte) bool {
	var v any
	return jsonUnmarshal(data, &v) == nil
}

// 使用小包装避免在核心路径传播任意 JSON 解码选项。
func jsonUnmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}
