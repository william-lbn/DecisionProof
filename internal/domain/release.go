package domain

import "time"

// ReleaseEvidenceManifest 将规格、代码、制品、策略、审批和部署证据绑定到同一个不可变发布对象。
type ReleaseEvidenceManifest struct {
	ManifestID         string            `json:"manifest_id"`
	TenantID           string            `json:"tenant_id"`
	ChangeID           string            `json:"change_id"`
	ProjectID          string            `json:"project_id"`
	ArtifactDigest     string            `json:"artifact_digest"`
	SpecDigest         string            `json:"spec_digest"`
	ArchitectureDigest string            `json:"architecture_digest"`
	PatchDigest        string            `json:"patch_digest"`
	EvidenceRoot       string            `json:"evidence_root"`
	PolicyDigest       string            `json:"policy_digest"`
	EvidenceIDs        map[string]string `json:"evidence_ids"`
	TargetEnvironment  string            `json:"target_environment"`
	RolloutStrategy    string            `json:"rollout_strategy"`
	CreatedBy          string            `json:"created_by"`
	CreatedAt          time.Time         `json:"created_at"`
	ContentDigest      string            `json:"content_digest"`
	SignatureAlgorithm string            `json:"signature_algorithm"`
	SigningKeyID       string            `json:"signing_key_id"`
	PublicKey          string            `json:"public_key"`
	Signature          string            `json:"signature"`
}
