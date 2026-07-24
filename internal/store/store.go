package store

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/william-lbn/DecisionProof/internal/domain"
)

// Repository 是控制内核唯一允许访问的持久化抽象。
// 所有写操作必须在 Transact 中完成，以保证状态、证据和审计记录原子提交。
type Repository interface {
	Transact(ctx context.Context, fn func(Tx) error) error
}

// Tx 提供事务内的数据操作。实现必须保证：
//   - 同一事务内读到自己的写入；
//   - 事务失败时不留下部分结果；
//   - UpdateChange 和 UpdateAction 执行乐观并发校验。
type Tx interface {
	CreateChange(change domain.Change) error
	GetChange(tenantID, changeID string) (domain.Change, error)
	UpdateChange(change domain.Change, expectedVersion uint64) error

	AppendEvidence(node domain.EvidenceNode, edges []domain.EvidenceEdge) error
	ListEvidence(tenantID, changeID string) ([]domain.EvidenceNode, error)
	GetEvidence(tenantID, evidenceID string) (domain.EvidenceNode, error)

	AppendTransition(record domain.TransitionRecord) error
	GetTransitionByIdempotency(tenantID, changeID, key string) (domain.TransitionRecord, error)
	ListTransitions(tenantID, changeID string) ([]domain.TransitionRecord, error)

	CreateActionIfAbsent(record domain.ActionRecord) (domain.ActionRecord, bool, error)
	GetAction(tenantID, actionID string) (domain.ActionRecord, error)
	GetActionByIdempotency(tenantID, key string) (domain.ActionRecord, error)
	UpdateAction(record domain.ActionRecord, expectedVersion uint64) error

	SaveManifest(manifest domain.ReleaseEvidenceManifest) error
	GetManifestByChange(tenantID, changeID string) (domain.ReleaseEvidenceManifest, error)
}

// Snapshot 是单机验证存储的完整快照格式。
// 它不是外部 API；字段采用导出形式是为了能够可靠序列化和灾难恢复。
type Snapshot struct {
	Changes                 map[string]domain.Change                  `json:"changes"`
	EvidenceNodes           map[string]domain.EvidenceNode            `json:"evidence_nodes"`
	EvidenceEdges           map[string]domain.EvidenceEdge            `json:"evidence_edges"`
	Transitions             map[string]domain.TransitionRecord        `json:"transitions"`
	TransitionByIdempotency map[string]string                         `json:"transition_by_idempotency"`
	Actions                 map[string]domain.ActionRecord            `json:"actions"`
	ActionByIdempotency     map[string]string                         `json:"action_by_idempotency"`
	Manifests               map[string]domain.ReleaseEvidenceManifest `json:"manifests"`
	ManifestByChange        map[string]string                         `json:"manifest_by_change"`
}

// NewSnapshot 返回所有映射均已初始化的空快照。
func NewSnapshot() Snapshot {
	return Snapshot{
		Changes:                 map[string]domain.Change{},
		EvidenceNodes:           map[string]domain.EvidenceNode{},
		EvidenceEdges:           map[string]domain.EvidenceEdge{},
		Transitions:             map[string]domain.TransitionRecord{},
		TransitionByIdempotency: map[string]string{},
		Actions:                 map[string]domain.ActionRecord{},
		ActionByIdempotency:     map[string]string{},
		Manifests:               map[string]domain.ReleaseEvidenceManifest{},
		ManifestByChange:        map[string]string{},
	}
}

func changeKey(tenantID, changeID string) string { return tenantID + "/" + changeID }
func objectKey(tenantID, objectID string) string { return tenantID + "/" + objectID }
func transitionIdemKey(tenantID, changeID, key string) string {
	return strings.Join([]string{tenantID, changeID, key}, "/")
}
func actionIdemKey(tenantID, key string) string { return tenantID + "/" + key }

// snapshotTx 在已复制的快照上实现事务语义。
type snapshotTx struct{ snapshot *Snapshot }

func (tx *snapshotTx) CreateChange(change domain.Change) error {
	key := changeKey(change.TenantID, change.ID)
	if _, ok := tx.snapshot.Changes[key]; ok {
		return domain.ErrAlreadyExists
	}
	tx.snapshot.Changes[key] = change
	return nil
}

func (tx *snapshotTx) GetChange(tenantID, changeID string) (domain.Change, error) {
	change, ok := tx.snapshot.Changes[changeKey(tenantID, changeID)]
	if !ok {
		return domain.Change{}, domain.ErrNotFound
	}
	return change, nil
}

func (tx *snapshotTx) UpdateChange(change domain.Change, expectedVersion uint64) error {
	key := changeKey(change.TenantID, change.ID)
	current, ok := tx.snapshot.Changes[key]
	if !ok {
		return domain.ErrNotFound
	}
	if current.Version != expectedVersion {
		return fmt.Errorf("%w: 当前版本=%d, 期望版本=%d", domain.ErrConflict, current.Version, expectedVersion)
	}
	tx.snapshot.Changes[key] = change
	return nil
}

func (tx *snapshotTx) AppendEvidence(node domain.EvidenceNode, edges []domain.EvidenceEdge) error {
	key := objectKey(node.TenantID, node.ID)
	if _, ok := tx.snapshot.EvidenceNodes[key]; ok {
		return domain.ErrAlreadyExists
	}
	if _, ok := tx.snapshot.Changes[changeKey(node.TenantID, node.ChangeID)]; !ok {
		return domain.ErrNotFound
	}
	tx.snapshot.EvidenceNodes[key] = node
	for _, edge := range edges {
		edgeKey := objectKey(edge.TenantID, edge.ID)
		if _, exists := tx.snapshot.EvidenceEdges[edgeKey]; exists {
			return domain.ErrAlreadyExists
		}
		tx.snapshot.EvidenceEdges[edgeKey] = edge
	}
	return nil
}

func (tx *snapshotTx) ListEvidence(tenantID, changeID string) ([]domain.EvidenceNode, error) {
	result := make([]domain.EvidenceNode, 0)
	for _, node := range tx.snapshot.EvidenceNodes {
		if node.TenantID == tenantID && node.ChangeID == changeID {
			result = append(result, node)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Sequence < result[j].Sequence
	})
	return result, nil
}

func (tx *snapshotTx) GetEvidence(tenantID, evidenceID string) (domain.EvidenceNode, error) {
	node, ok := tx.snapshot.EvidenceNodes[objectKey(tenantID, evidenceID)]
	if !ok {
		return domain.EvidenceNode{}, domain.ErrNotFound
	}
	return node, nil
}

func (tx *snapshotTx) AppendTransition(record domain.TransitionRecord) error {
	key := objectKey(record.TenantID, record.ID)
	idem := transitionIdemKey(record.TenantID, record.ChangeID, record.IdempotencyKey)
	if _, exists := tx.snapshot.TransitionByIdempotency[idem]; exists {
		return domain.ErrAlreadyExists
	}
	tx.snapshot.Transitions[key] = record
	tx.snapshot.TransitionByIdempotency[idem] = record.ID
	return nil
}

func (tx *snapshotTx) GetTransitionByIdempotency(tenantID, changeID, key string) (domain.TransitionRecord, error) {
	id, ok := tx.snapshot.TransitionByIdempotency[transitionIdemKey(tenantID, changeID, key)]
	if !ok {
		return domain.TransitionRecord{}, domain.ErrNotFound
	}
	record, ok := tx.snapshot.Transitions[objectKey(tenantID, id)]
	if !ok {
		return domain.TransitionRecord{}, domain.ErrNotFound
	}
	return record, nil
}

func (tx *snapshotTx) ListTransitions(tenantID, changeID string) ([]domain.TransitionRecord, error) {
	result := make([]domain.TransitionRecord, 0)
	for _, record := range tx.snapshot.Transitions {
		if record.TenantID == tenantID && record.ChangeID == changeID {
			result = append(result, record)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.Before(result[j].CreatedAt) })
	return result, nil
}

func (tx *snapshotTx) CreateActionIfAbsent(record domain.ActionRecord) (domain.ActionRecord, bool, error) {
	idem := actionIdemKey(record.Proposal.TenantID, record.Proposal.IdempotencyKey)
	if existingID, ok := tx.snapshot.ActionByIdempotency[idem]; ok {
		existing := tx.snapshot.Actions[objectKey(record.Proposal.TenantID, existingID)]
		return existing, false, nil
	}
	key := objectKey(record.Proposal.TenantID, record.Proposal.ID)
	if _, exists := tx.snapshot.Actions[key]; exists {
		return domain.ActionRecord{}, false, domain.ErrAlreadyExists
	}
	tx.snapshot.Actions[key] = record
	tx.snapshot.ActionByIdempotency[idem] = record.Proposal.ID
	return record, true, nil
}

func (tx *snapshotTx) GetAction(tenantID, actionID string) (domain.ActionRecord, error) {
	record, ok := tx.snapshot.Actions[objectKey(tenantID, actionID)]
	if !ok {
		return domain.ActionRecord{}, domain.ErrNotFound
	}
	return record, nil
}

func (tx *snapshotTx) GetActionByIdempotency(tenantID, key string) (domain.ActionRecord, error) {
	id, ok := tx.snapshot.ActionByIdempotency[actionIdemKey(tenantID, key)]
	if !ok {
		return domain.ActionRecord{}, domain.ErrNotFound
	}
	return tx.GetAction(tenantID, id)
}

func (tx *snapshotTx) UpdateAction(record domain.ActionRecord, expectedVersion uint64) error {
	key := objectKey(record.Proposal.TenantID, record.Proposal.ID)
	current, ok := tx.snapshot.Actions[key]
	if !ok {
		return domain.ErrNotFound
	}
	if current.Version != expectedVersion {
		return fmt.Errorf("%w: 动作当前版本=%d, 期望版本=%d", domain.ErrConflict, current.Version, expectedVersion)
	}
	tx.snapshot.Actions[key] = record
	return nil
}

func (tx *snapshotTx) SaveManifest(manifest domain.ReleaseEvidenceManifest) error {
	changeKeyValue := changeKey(manifest.TenantID, manifest.ChangeID)
	if oldID, exists := tx.snapshot.ManifestByChange[changeKeyValue]; exists {
		old := tx.snapshot.Manifests[objectKey(manifest.TenantID, oldID)]
		if old.ContentDigest == manifest.ContentDigest {
			return nil
		}
		return fmt.Errorf("同一变更已经存在不同内容的发布清单: %w", domain.ErrConflict)
	}
	tx.snapshot.Manifests[objectKey(manifest.TenantID, manifest.ManifestID)] = manifest
	tx.snapshot.ManifestByChange[changeKeyValue] = manifest.ManifestID
	return nil
}

func (tx *snapshotTx) GetManifestByChange(tenantID, changeID string) (domain.ReleaseEvidenceManifest, error) {
	id, ok := tx.snapshot.ManifestByChange[changeKey(tenantID, changeID)]
	if !ok {
		return domain.ReleaseEvidenceManifest{}, domain.ErrNotFound
	}
	manifest, ok := tx.snapshot.Manifests[objectKey(tenantID, id)]
	if !ok {
		return domain.ReleaseEvidenceManifest{}, domain.ErrNotFound
	}
	return manifest, nil
}
