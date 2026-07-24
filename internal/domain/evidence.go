package domain

import (
	"encoding/json"
	"time"
)

// EvidenceType 表示证据的语义类型。类型是发布策略判断的基础，不能由调用方随意拼接。
type EvidenceType string

const (
	EvidenceSpecDocument         EvidenceType = "SPEC_DOCUMENT"
	EvidenceSpecApproval         EvidenceType = "SPEC_APPROVAL"
	EvidenceArchitectureDocument EvidenceType = "ARCHITECTURE_DOCUMENT"
	EvidenceArchitectureApproval EvidenceType = "ARCHITECTURE_APPROVAL"
	EvidenceImplementationPlan   EvidenceType = "IMPLEMENTATION_PLAN"
	EvidenceAgentRun             EvidenceType = "AGENT_RUN"
	EvidenceFrozenPatch          EvidenceType = "FROZEN_PATCH"
	EvidenceIndependentReview    EvidenceType = "INDEPENDENT_REVIEW"
	EvidenceCITest               EvidenceType = "CI_TEST"
	EvidenceSecurityScan         EvidenceType = "SECURITY_SCAN"
	EvidenceSBOM                 EvidenceType = "SBOM"
	EvidenceBuildProvenance      EvidenceType = "BUILD_PROVENANCE"
	EvidenceArtifactSignature    EvidenceType = "ARTIFACT_SIGNATURE"
	EvidenceStagingTest          EvidenceType = "STAGING_TEST"
	EvidenceReleaseApproval      EvidenceType = "RELEASE_APPROVAL"
	EvidenceCanaryAnalysis       EvidenceType = "CANARY_ANALYSIS"
	EvidenceDeployment           EvidenceType = "DEPLOYMENT"
	EvidenceRuntimeSignal        EvidenceType = "RUNTIME_SIGNAL"
	EvidenceBusinessOutcome      EvidenceType = "BUSINESS_OUTCOME"
	EvidenceRollback             EvidenceType = "ROLLBACK"
)

// EvidenceVerdict 表示证据对其声明事实的结论。
type EvidenceVerdict string

const (
	VerdictPass         EvidenceVerdict = "PASS"
	VerdictFail         EvidenceVerdict = "FAIL"
	VerdictInconclusive EvidenceVerdict = "INCONCLUSIVE"
	VerdictObserved     EvidenceVerdict = "OBSERVED"
)

// Valid 检查证据类型是否属于平台受控集合。
func (t EvidenceType) Valid() bool {
	switch t {
	case EvidenceSpecDocument, EvidenceSpecApproval, EvidenceArchitectureDocument,
		EvidenceArchitectureApproval, EvidenceImplementationPlan, EvidenceAgentRun,
		EvidenceFrozenPatch, EvidenceIndependentReview, EvidenceCITest,
		EvidenceSecurityScan, EvidenceSBOM, EvidenceBuildProvenance,
		EvidenceArtifactSignature, EvidenceStagingTest, EvidenceReleaseApproval,
		EvidenceCanaryAnalysis, EvidenceDeployment, EvidenceRuntimeSignal,
		EvidenceBusinessOutcome, EvidenceRollback:
		return true
	default:
		return false
	}
}

// Valid 检查证据结论是否属于平台受控集合。
func (v EvidenceVerdict) Valid() bool {
	switch v {
	case VerdictPass, VerdictFail, VerdictInconclusive, VerdictObserved:
		return true
	default:
		return false
	}
}

// EvidenceNode 是不可变证据节点。
// Payload 保存原始结构化证据；PayloadDigest、PrevHash 和 RecordHash 形成防篡改哈希链。
type EvidenceNode struct {
	ID                   string          `json:"id"`
	TenantID             string          `json:"tenant_id"`
	ChangeID             string          `json:"change_id"`
	Type                 EvidenceType    `json:"type"`
	SchemaVersion        string          `json:"schema_version"`
	Verdict              EvidenceVerdict `json:"verdict"`
	Producer             string          `json:"producer"`
	ProducerIdentity     string          `json:"producer_identity,omitempty"`
	TrustClass           string          `json:"trust_class"`
	Subject              string          `json:"subject,omitempty"` // 兼容 P0，V2 使用下面三个字段。
	SubjectType          string          `json:"subject_type,omitempty"`
	SubjectID            string          `json:"subject_id,omitempty"`
	SubjectDigest        string          `json:"subject_digest,omitempty"`
	SourceSystem         string          `json:"source_system,omitempty"`
	SourceURI            string          `json:"source_uri,omitempty"`
	ExternalID           string          `json:"external_id,omitempty"`
	Payload              json.RawMessage `json:"payload"`
	PayloadDigest        string          `json:"payload_digest"`
	PrevHash             string          `json:"prev_hash,omitempty"`
	RecordHash           string          `json:"record_hash"`
	Sequence             uint64          `json:"sequence"`
	SupersedesEvidenceID string          `json:"supersedes_evidence_id,omitempty"`
	CreatedAt            time.Time       `json:"created_at"`
	ValidFrom            time.Time       `json:"valid_from"`
	InvalidatedAt        *time.Time      `json:"invalidated_at,omitempty"`
}

// EvidenceEdge 表示证据图中的有向关系。
type EvidenceEdge struct {
	ID         string    `json:"id"`
	TenantID   string    `json:"tenant_id"`
	ChangeID   string    `json:"change_id"`
	FromNodeID string    `json:"from_node_id"`
	Relation   string    `json:"relation"`
	ToNodeID   string    `json:"to_node_id"`
	CreatedAt  time.Time `json:"created_at"`
}
