package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/william-lbn/DecisionProof/internal/domain"
	"github.com/william-lbn/DecisionProof/internal/store"
)

// ChangeService 管理变更聚合根和生命周期迁移。
type ChangeService struct {
	repo   store.Repository
	clock  Clock
	policy *TransitionPolicy
}

func NewChangeService(repo store.Repository, clock Clock, policy *TransitionPolicy) *ChangeService {
	return &ChangeService{repo: repo, clock: clock, policy: policy}
}

func (s *ChangeService) Create(ctx context.Context, tenantID, projectID, title, description, riskLevel string) (domain.Change, error) {
	if tenantID == "" || projectID == "" || title == "" {
		return domain.Change{}, errors.New("tenant_id、project_id 和 title 不能为空")
	}
	id, err := domain.NewID("chg")
	if err != nil {
		return domain.Change{}, err
	}
	now := s.clock.Now()
	change := domain.Change{
		ID: id, TenantID: tenantID, ProjectID: projectID, Title: title,
		Description: description, RiskLevel: defaultString(riskLevel, "medium"),
		State: domain.StateDraft, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	ctx = store.WithTenant(ctx, tenantID)
	err = s.repo.Transact(store.WithTenant(ctx, tenantID), func(tx store.Tx) error { return tx.CreateChange(change) })
	return change, err
}

func (s *ChangeService) Get(ctx context.Context, tenantID, changeID string) (domain.Change, error) {
	ctx = store.WithTenant(ctx, tenantID)
	var change domain.Change
	err := s.repo.Transact(store.WithTenant(ctx, tenantID), func(tx store.Tx) error {
		var err error
		change, err = tx.GetChange(tenantID, changeID)
		return err
	})
	return change, err
}

// Transition 以幂等方式执行状态迁移。
// 同一幂等键重复提交时返回第一次迁移结果，不会再次改变状态或写入重复审计记录。
func (s *ChangeService) Transition(ctx context.Context, tenantID, changeID, actor string, roles []string, target domain.ChangeState, idempotencyKey string) (domain.Change, domain.TransitionRecord, error) {
	ctx = store.WithTenant(ctx, tenantID)
	if idempotencyKey == "" || actor == "" {
		return domain.Change{}, domain.TransitionRecord{}, errors.New("actor 和 idempotency_key 不能为空")
	}
	var updated domain.Change
	var record domain.TransitionRecord
	err := s.repo.Transact(store.WithTenant(ctx, tenantID), func(tx store.Tx) error {
		if existing, err := tx.GetTransitionByIdempotency(tenantID, changeID, idempotencyKey); err == nil {
			current, getErr := tx.GetChange(tenantID, changeID)
			updated, record = current, existing
			return getErr
		} else if !errors.Is(err, domain.ErrNotFound) {
			return err
		}

		change, err := tx.GetChange(tenantID, changeID)
		if err != nil {
			return err
		}
		evidence, err := tx.ListEvidence(tenantID, changeID)
		if err != nil {
			return err
		}
		if err := s.policy.Evaluate(change, target, roles, evidence); err != nil {
			return err
		}
		evidenceRoot := ""
		if len(evidence) > 0 {
			evidenceRoot = evidence[len(evidence)-1].RecordHash
		}
		transitionID, err := domain.NewID("tr")
		if err != nil {
			return err
		}
		record = domain.TransitionRecord{
			ID: transitionID, TenantID: tenantID, ChangeID: changeID,
			From: change.State, To: target, Actor: actor, ActorRoles: append([]string(nil), roles...),
			IdempotencyKey: idempotencyKey, PolicyDigest: s.policy.Digest(),
			EvidenceRoot: evidenceRoot, CreatedAt: s.clock.Now(),
		}
		updated = change
		updated.State = target
		updated.Version++
		updated.UpdatedAt = s.clock.Now()
		if err := tx.UpdateChange(updated, change.Version); err != nil {
			return err
		}
		if err := tx.AppendTransition(record); err != nil {
			return fmt.Errorf("写入迁移审计记录失败: %w", err)
		}
		return nil
	})
	return updated, record, err
}

func (s *ChangeService) ListEvidence(ctx context.Context, tenantID, changeID string) ([]domain.EvidenceNode, error) {
	ctx = store.WithTenant(ctx, tenantID)
	var result []domain.EvidenceNode
	err := s.repo.Transact(store.WithTenant(ctx, tenantID), func(tx store.Tx) error {
		var err error
		result, err = tx.ListEvidence(tenantID, changeID)
		return err
	})
	return result, err
}
