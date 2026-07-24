package assurance

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/william-lbn/DecisionProof/internal/domain"
)

// SubjectBinding 描述一次发布资格判断的精确对象边界。
type SubjectBinding struct {
	CommitSHA      string `json:"commit_sha"`
	PatchDigest    string `json:"patch_digest"`
	ArtifactDigest string `json:"artifact_digest"`
	DeploymentID   string `json:"deployment_id"`
	ReleaseDigest  string `json:"release_digest"`
	Environment    string `json:"environment"`
}

// QualifiedEvidence 是经过 Subject、TrustClass、Verdict 和失效状态校验后的结果。
type QualifiedEvidence struct {
	EvidenceIDs map[domain.EvidenceType]string `json:"evidence_ids"`
	Missing     []string                       `json:"missing"`
}

// Qualifier 防止旧 Commit、旧 Artifact 或错误环境的证据被用于当前发布。
type Qualifier struct{}

func (Qualifier) Qualify(nodes []domain.EvidenceNode, binding SubjectBinding, required []domain.EvidenceType) (QualifiedEvidence, error) {
	if strings.TrimSpace(binding.CommitSHA) == "" || strings.TrimSpace(binding.ArtifactDigest) == "" {
		return QualifiedEvidence{}, errors.New("发布资格判断必须提供 commit_sha 和 artifact_digest")
	}
	latest := map[domain.EvidenceType]domain.EvidenceNode{}
	for _, n := range nodes {
		if n.InvalidatedAt != nil {
			continue
		}
		if !matches(n, binding) {
			continue
		}
		latest[n.Type] = n
	}
	result := QualifiedEvidence{EvidenceIDs: map[domain.EvidenceType]string{}}
	for _, typ := range required {
		n, ok := latest[typ]
		if !ok || n.Verdict != domain.VerdictPass || !trusted(n.TrustClass) {
			result.Missing = append(result.Missing, string(typ))
			continue
		}
		result.EvidenceIDs[typ] = n.ID
	}
	if len(result.Missing) > 0 {
		sort.Strings(result.Missing)
		return result, fmt.Errorf("%w: %s", domain.ErrEvidenceInsufficient, strings.Join(result.Missing, ", "))
	}
	return result, nil
}

func matches(n domain.EvidenceNode, b SubjectBinding) bool {
	digest := n.SubjectDigest
	if digest == "" {
		digest = n.Subject
	}
	switch n.Type {
	case domain.EvidenceFrozenPatch, domain.EvidenceIndependentReview:
		return digest == b.PatchDigest || digest == b.CommitSHA
	case domain.EvidenceCITest, domain.EvidenceSecurityScan:
		return digest == b.CommitSHA
	case domain.EvidenceSBOM, domain.EvidenceBuildProvenance, domain.EvidenceArtifactSignature:
		return digest == b.ArtifactDigest
	case domain.EvidenceStagingTest, domain.EvidenceDeployment, domain.EvidenceCanaryAnalysis, domain.EvidenceRuntimeSignal:
		return digest == b.DeploymentID || digest == b.ArtifactDigest
	case domain.EvidenceReleaseApproval:
		return digest == b.ReleaseDigest
	default:
		return true
	}
}
func trusted(v string) bool {
	switch strings.ToUpper(v) {
	case "VERIFIED", "ATTESTED", "HUMAN_APPROVED":
		return true
	default:
		return false
	}
}
