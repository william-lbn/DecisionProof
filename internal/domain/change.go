package domain

import "time"

// Change 是平台对一次需求变更的权威聚合根。
// Version 用于乐观并发控制，防止两个并发流程覆盖彼此的状态。
type Change struct {
	ID                 string      `json:"id"`
	TenantID           string      `json:"tenant_id"`
	ProjectID          string      `json:"project_id"`
	Title              string      `json:"title"`
	Description        string      `json:"description,omitempty"`
	RiskLevel          string      `json:"risk_level"`
	State              ChangeState `json:"state"`
	Version            uint64      `json:"version"`
	SpecDigest         string      `json:"spec_digest,omitempty"`
	ArchitectureDigest string      `json:"architecture_digest,omitempty"`
	PatchDigest        string      `json:"patch_digest,omitempty"`
	ArtifactDigest     string      `json:"artifact_digest,omitempty"`
	CreatedAt          time.Time   `json:"created_at"`
	UpdatedAt          time.Time   `json:"updated_at"`
}

// TransitionRecord 保存每一次状态迁移的审计信息。
// EvidenceRoot 是迁移时该变更证据链的尾哈希，可用于证明迁移依据没有被静默替换。
type TransitionRecord struct {
	ID             string      `json:"id"`
	TenantID       string      `json:"tenant_id"`
	ChangeID       string      `json:"change_id"`
	From           ChangeState `json:"from"`
	To             ChangeState `json:"to"`
	Actor          string      `json:"actor"`
	ActorRoles     []string    `json:"actor_roles"`
	IdempotencyKey string      `json:"idempotency_key"`
	PolicyDigest   string      `json:"policy_digest"`
	EvidenceRoot   string      `json:"evidence_root"`
	CreatedAt      time.Time   `json:"created_at"`
}
