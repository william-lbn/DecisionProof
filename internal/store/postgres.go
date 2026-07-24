package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/william-lbn/DecisionProof/internal/domain"
)

// PostgresStore 是多副本生产部署使用的事务存储适配器。
// 本包只依赖 database/sql；实际二进制可选择 pgx/stdlib 等驱动并在启动时完成注册。
type PostgresStore struct{ db *sql.DB }

func NewPostgresStore(db *sql.DB) (*PostgresStore, error) {
	if db == nil {
		return nil, errors.New("PostgreSQL 数据库连接不能为空")
	}
	return &PostgresStore{db: db}, nil
}

func (s *PostgresStore) Transact(ctx context.Context, fn func(Tx) error) error {
	tenantID, ok := tenantFromContext(ctx)
	if !ok {
		return errors.New("PostgreSQL 事务缺少可信租户上下文")
	}
	// 只对 PostgreSQL 明确报告“事务已中止”的序列化冲突和死锁重试。
	// 对网络断开、提交结果未知等错误绝不盲目重试，避免重复产生数据库外副作用。
	for attempt := 0; attempt < 4; attempt++ {
		err := s.transactOnce(ctx, tenantID, fn)
		if err == nil {
			return nil
		}
		if !isRetryablePostgresError(err) || attempt == 3 {
			return err
		}
		delay := time.Duration(10*(1<<attempt)) * time.Millisecond
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return errors.New("PostgreSQL 事务重试耗尽")
}

func (s *PostgresStore) transactOnce(ctx context.Context, tenantID string, fn func(Tx) error) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("开启数据库事务失败: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantID); err != nil {
		return fmt.Errorf("设置 PostgreSQL 租户上下文失败: %w", err)
	}
	if err := fn(&postgresTx{ctx: ctx, tx: tx}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交数据库事务失败: %w", err)
	}
	return nil
}

type postgresStateError interface {
	SQLState() string
}

func isRetryablePostgresError(err error) bool {
	var state postgresStateError
	if !errors.As(err, &state) {
		return false
	}
	switch state.SQLState() {
	case "40001", "40P01": // serialization_failure, deadlock_detected
		return true
	default:
		return false
	}
}

type postgresTx struct {
	ctx context.Context
	tx  *sql.Tx
}

func (p *postgresTx) CreateChange(change domain.Change) error {
	_, err := p.tx.ExecContext(p.ctx, `
INSERT INTO changes
(tenant_id, change_id, project_id, title, description, risk_level, state, version,
 spec_digest, architecture_digest, patch_digest, artifact_digest, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		change.TenantID, change.ID, change.ProjectID, change.Title, change.Description,
		change.RiskLevel, change.State, change.Version, change.SpecDigest,
		change.ArchitectureDigest, change.PatchDigest, change.ArtifactDigest,
		change.CreatedAt, change.UpdatedAt)
	if err != nil {
		return fmt.Errorf("创建变更失败: %w", err)
	}
	return nil
}

func (p *postgresTx) GetChange(tenantID, changeID string) (domain.Change, error) {
	var c domain.Change
	err := p.tx.QueryRowContext(p.ctx, `
SELECT change_id, tenant_id, project_id, title, description, risk_level, state, version,
       spec_digest, architecture_digest, patch_digest, artifact_digest, created_at, updated_at
FROM changes WHERE tenant_id=$1 AND change_id=$2`, tenantID, changeID).Scan(
		&c.ID, &c.TenantID, &c.ProjectID, &c.Title, &c.Description, &c.RiskLevel,
		&c.State, &c.Version, &c.SpecDigest, &c.ArchitectureDigest, &c.PatchDigest,
		&c.ArtifactDigest, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Change{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Change{}, fmt.Errorf("读取变更失败: %w", err)
	}
	return c, nil
}

func (p *postgresTx) UpdateChange(change domain.Change, expectedVersion uint64) error {
	result, err := p.tx.ExecContext(p.ctx, `
UPDATE changes SET project_id=$3,title=$4,description=$5,risk_level=$6,state=$7,version=$8,
 spec_digest=$9,architecture_digest=$10,patch_digest=$11,artifact_digest=$12,updated_at=$13
WHERE tenant_id=$1 AND change_id=$2 AND version=$14`,
		change.TenantID, change.ID, change.ProjectID, change.Title, change.Description,
		change.RiskLevel, change.State, change.Version, change.SpecDigest,
		change.ArchitectureDigest, change.PatchDigest, change.ArtifactDigest,
		change.UpdatedAt, expectedVersion)
	if err != nil {
		return fmt.Errorf("更新变更失败: %w", err)
	}
	return requireOneRow(result, "变更版本冲突")
}

func (p *postgresTx) AppendEvidence(node domain.EvidenceNode, edges []domain.EvidenceEdge) error {
	_, err := p.tx.ExecContext(p.ctx, `
INSERT INTO evidence_nodes
(tenant_id,evidence_id,change_id,evidence_type,schema_version,verdict,producer,producer_identity,
 trust_class,subject,subject_type,subject_id,subject_digest,source_system,source_uri,external_id,
 payload,payload_digest,prev_hash,record_hash,sequence,supersedes_evidence_id,created_at,valid_from,invalidated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25)`,
		node.TenantID, node.ID, node.ChangeID, node.Type, node.SchemaVersion, node.Verdict,
		node.Producer, node.ProducerIdentity, node.TrustClass, node.Subject, node.SubjectType,
		node.SubjectID, node.SubjectDigest, node.SourceSystem, node.SourceURI, node.ExternalID,
		[]byte(node.Payload), node.PayloadDigest, node.PrevHash, node.RecordHash, node.Sequence,
		node.SupersedesEvidenceID, node.CreatedAt, node.ValidFrom, node.InvalidatedAt)
	if err != nil {
		return fmt.Errorf("写入证据节点失败: %w", err)
	}
	for _, edge := range edges {
		_, err := p.tx.ExecContext(p.ctx, `
INSERT INTO evidence_edges
(tenant_id,edge_id,change_id,from_node_id,relation,to_node_id,created_at)
VALUES ($1,$2,$3,$4,$5,$6,$7)`, edge.TenantID, edge.ID, edge.ChangeID,
			edge.FromNodeID, edge.Relation, edge.ToNodeID, edge.CreatedAt)
		if err != nil {
			return fmt.Errorf("写入证据关系失败: %w", err)
		}
	}
	return nil
}

func (p *postgresTx) ListEvidence(tenantID, changeID string) ([]domain.EvidenceNode, error) {
	rows, err := p.tx.QueryContext(p.ctx, `
SELECT evidence_id,tenant_id,change_id,evidence_type,schema_version,verdict,producer,producer_identity,
       trust_class,subject,subject_type,subject_id,subject_digest,source_system,source_uri,external_id,payload,
       payload_digest,prev_hash,record_hash,sequence,supersedes_evidence_id,created_at,valid_from,invalidated_at
FROM evidence_nodes WHERE tenant_id=$1 AND change_id=$2 ORDER BY sequence`, tenantID, changeID)
	if err != nil {
		return nil, fmt.Errorf("查询证据节点失败: %w", err)
	}
	defer rows.Close()
	result := make([]domain.EvidenceNode, 0)
	for rows.Next() {
		var node domain.EvidenceNode
		var payload []byte
		if err := rows.Scan(&node.ID, &node.TenantID, &node.ChangeID, &node.Type, &node.SchemaVersion,
			&node.Verdict, &node.Producer, &node.ProducerIdentity, &node.TrustClass, &node.Subject,
			&node.SubjectType, &node.SubjectID, &node.SubjectDigest, &node.SourceSystem, &node.SourceURI,
			&node.ExternalID, &payload, &node.PayloadDigest, &node.PrevHash, &node.RecordHash, &node.Sequence,
			&node.SupersedesEvidenceID, &node.CreatedAt, &node.ValidFrom, &node.InvalidatedAt); err != nil {
			return nil, fmt.Errorf("解析证据节点失败: %w", err)
		}
		node.Payload = append(json.RawMessage(nil), payload...)
		result = append(result, node)
	}
	return result, rows.Err()
}

func (p *postgresTx) GetEvidence(tenantID, evidenceID string) (domain.EvidenceNode, error) {
	var node domain.EvidenceNode
	var payload []byte
	err := p.tx.QueryRowContext(p.ctx, `
SELECT evidence_id,tenant_id,change_id,evidence_type,schema_version,verdict,producer,producer_identity,
       trust_class,subject,subject_type,subject_id,subject_digest,source_system,source_uri,external_id,payload,
       payload_digest,prev_hash,record_hash,sequence,supersedes_evidence_id,created_at,valid_from,invalidated_at
FROM evidence_nodes WHERE tenant_id=$1 AND evidence_id=$2`, tenantID, evidenceID).Scan(
		&node.ID, &node.TenantID, &node.ChangeID, &node.Type, &node.SchemaVersion, &node.Verdict,
		&node.Producer, &node.ProducerIdentity, &node.TrustClass, &node.Subject, &node.SubjectType,
		&node.SubjectID, &node.SubjectDigest, &node.SourceSystem, &node.SourceURI, &node.ExternalID,
		&payload, &node.PayloadDigest, &node.PrevHash, &node.RecordHash, &node.Sequence,
		&node.SupersedesEvidenceID, &node.CreatedAt, &node.ValidFrom, &node.InvalidatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.EvidenceNode{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.EvidenceNode{}, fmt.Errorf("读取证据节点失败: %w", err)
	}
	node.Payload = payload
	return node, nil
}

func (p *postgresTx) AppendTransition(record domain.TransitionRecord) error {
	roles, _ := json.Marshal(record.ActorRoles)
	_, err := p.tx.ExecContext(p.ctx, `
INSERT INTO transition_records
(tenant_id,transition_id,change_id,from_state,to_state,actor,actor_roles,idempotency_key,
 policy_digest,evidence_root,created_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, record.TenantID, record.ID,
		record.ChangeID, record.From, record.To, record.Actor, roles, record.IdempotencyKey,
		record.PolicyDigest, record.EvidenceRoot, record.CreatedAt)
	if err != nil {
		return fmt.Errorf("写入迁移记录失败: %w", err)
	}
	return nil
}

func (p *postgresTx) GetTransitionByIdempotency(tenantID, changeID, key string) (domain.TransitionRecord, error) {
	return p.getTransition(`tenant_id=$1 AND change_id=$2 AND idempotency_key=$3`, tenantID, changeID, key)
}

func (p *postgresTx) getTransition(where string, args ...any) (domain.TransitionRecord, error) {
	var r domain.TransitionRecord
	var roles []byte
	err := p.tx.QueryRowContext(p.ctx, `
SELECT transition_id,tenant_id,change_id,from_state,to_state,actor,actor_roles,idempotency_key,
       policy_digest,evidence_root,created_at
FROM transition_records WHERE `+where, args...).Scan(&r.ID, &r.TenantID, &r.ChangeID, &r.From,
		&r.To, &r.Actor, &roles, &r.IdempotencyKey, &r.PolicyDigest, &r.EvidenceRoot, &r.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.TransitionRecord{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.TransitionRecord{}, fmt.Errorf("读取迁移记录失败: %w", err)
	}
	_ = json.Unmarshal(roles, &r.ActorRoles)
	return r, nil
}

func (p *postgresTx) ListTransitions(tenantID, changeID string) ([]domain.TransitionRecord, error) {
	rows, err := p.tx.QueryContext(p.ctx, `
SELECT transition_id,tenant_id,change_id,from_state,to_state,actor,actor_roles,idempotency_key,
       policy_digest,evidence_root,created_at
FROM transition_records WHERE tenant_id=$1 AND change_id=$2 ORDER BY created_at`, tenantID, changeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.TransitionRecord
	for rows.Next() {
		var r domain.TransitionRecord
		var roles []byte
		if err := rows.Scan(&r.ID, &r.TenantID, &r.ChangeID, &r.From, &r.To, &r.Actor,
			&roles, &r.IdempotencyKey, &r.PolicyDigest, &r.EvidenceRoot, &r.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(roles, &r.ActorRoles)
		result = append(result, r)
	}
	return result, rows.Err()
}

func (p *postgresTx) CreateActionIfAbsent(record domain.ActionRecord) (domain.ActionRecord, bool, error) {
	proposal, err := json.Marshal(record.Proposal)
	if err != nil {
		return domain.ActionRecord{}, false, err
	}
	result, err := p.tx.ExecContext(p.ctx, `
INSERT INTO action_records
(tenant_id,action_id,idempotency_key,proposal,status,version,external_ref,receipt,error_message,
 policy_digest,attempt_count,created_at,updated_at,last_checked_at,settlement_note)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
ON CONFLICT (tenant_id,idempotency_key) DO NOTHING`, record.Proposal.TenantID,
		record.Proposal.ID, record.Proposal.IdempotencyKey, proposal, record.Status, record.Version,
		record.ExternalRef, nullableBytes(record.Receipt), record.ErrorMessage, record.PolicyDigest,
		record.AttemptCount, record.CreatedAt, record.UpdatedAt, record.LastCheckedAt, record.SettlementNote)
	if err != nil {
		return domain.ActionRecord{}, false, fmt.Errorf("创建动作记录失败: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return domain.ActionRecord{}, false, err
	}
	if affected == 1 {
		return record, true, nil
	}
	existing, err := p.GetActionByIdempotency(record.Proposal.TenantID, record.Proposal.IdempotencyKey)
	return existing, false, err
}

func (p *postgresTx) GetAction(tenantID, actionID string) (domain.ActionRecord, error) {
	return p.getAction(`tenant_id=$1 AND action_id=$2`, tenantID, actionID)
}

func (p *postgresTx) GetActionByIdempotency(tenantID, key string) (domain.ActionRecord, error) {
	return p.getAction(`tenant_id=$1 AND idempotency_key=$2`, tenantID, key)
}

func (p *postgresTx) getAction(where string, args ...any) (domain.ActionRecord, error) {
	var r domain.ActionRecord
	var proposal, receipt []byte
	var checked sql.NullTime
	err := p.tx.QueryRowContext(p.ctx, `
SELECT proposal,status,version,external_ref,receipt,error_message,policy_digest,attempt_count,
       created_at,updated_at,last_checked_at,settlement_note
FROM action_records WHERE `+where, args...).Scan(&proposal, &r.Status, &r.Version, &r.ExternalRef,
		&receipt, &r.ErrorMessage, &r.PolicyDigest, &r.AttemptCount, &r.CreatedAt, &r.UpdatedAt,
		&checked, &r.SettlementNote)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ActionRecord{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.ActionRecord{}, fmt.Errorf("读取动作记录失败: %w", err)
	}
	if err := json.Unmarshal(proposal, &r.Proposal); err != nil {
		return domain.ActionRecord{}, fmt.Errorf("解析动作建议失败: %w", err)
	}
	r.Receipt = append(json.RawMessage(nil), receipt...)
	if checked.Valid {
		r.LastCheckedAt = &checked.Time
	}
	return r, nil
}

func (p *postgresTx) UpdateAction(record domain.ActionRecord, expectedVersion uint64) error {
	proposal, err := json.Marshal(record.Proposal)
	if err != nil {
		return err
	}
	result, err := p.tx.ExecContext(p.ctx, `
UPDATE action_records SET proposal=$3,status=$4,version=$5,external_ref=$6,receipt=$7,
 error_message=$8,policy_digest=$9,attempt_count=$10,updated_at=$11,last_checked_at=$12,settlement_note=$13
WHERE tenant_id=$1 AND action_id=$2 AND version=$14`, record.Proposal.TenantID,
		record.Proposal.ID, proposal, record.Status, record.Version, record.ExternalRef,
		nullableBytes(record.Receipt), record.ErrorMessage, record.PolicyDigest, record.AttemptCount,
		record.UpdatedAt, record.LastCheckedAt, record.SettlementNote, expectedVersion)
	if err != nil {
		return fmt.Errorf("更新动作记录失败: %w", err)
	}
	return requireOneRow(result, "动作版本冲突")
}

func (p *postgresTx) SaveManifest(manifest domain.ReleaseEvidenceManifest) error {
	data, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	result, err := p.tx.ExecContext(p.ctx, `
INSERT INTO release_manifests
(tenant_id,manifest_id,change_id,content_digest,manifest,created_at)
VALUES ($1,$2,$3,$4,$5,$6)
ON CONFLICT (tenant_id,change_id) DO NOTHING`, manifest.TenantID, manifest.ManifestID,
		manifest.ChangeID, manifest.ContentDigest, data, manifest.CreatedAt)
	if err != nil {
		return fmt.Errorf("保存发布清单失败: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 1 {
		return nil
	}
	existing, err := p.GetManifestByChange(manifest.TenantID, manifest.ChangeID)
	if err != nil {
		return err
	}
	if existing.ContentDigest != manifest.ContentDigest {
		return fmt.Errorf("同一变更已存在不同发布清单: %w", domain.ErrConflict)
	}
	return nil
}

func (p *postgresTx) GetManifestByChange(tenantID, changeID string) (domain.ReleaseEvidenceManifest, error) {
	var data []byte
	err := p.tx.QueryRowContext(p.ctx, `
SELECT manifest FROM release_manifests WHERE tenant_id=$1 AND change_id=$2`, tenantID, changeID).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ReleaseEvidenceManifest{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.ReleaseEvidenceManifest{}, fmt.Errorf("读取发布清单失败: %w", err)
	}
	var manifest domain.ReleaseEvidenceManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return domain.ReleaseEvidenceManifest{}, fmt.Errorf("解析发布清单失败: %w", err)
	}
	return manifest, nil
}

func requireOneRow(result sql.Result, message string) error {
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return fmt.Errorf("%s: %w", message, domain.ErrConflict)
	}
	return nil
}

func nullableBytes(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}
