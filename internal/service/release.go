package service

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/william-lbn/DecisionProof/internal/domain"
	"github.com/william-lbn/DecisionProof/internal/integrity"
	"github.com/william-lbn/DecisionProof/internal/security"
	"github.com/william-lbn/DecisionProof/internal/store"
)

// ReleaseService 生成并验证发布证据清单。
type ReleaseService struct {
	repo   store.Repository
	clock  Clock
	policy *TransitionPolicy
	signer *security.Signer
}

func NewReleaseService(repo store.Repository, clock Clock, policy *TransitionPolicy, signer *security.Signer) *ReleaseService {
	return &ReleaseService{repo: repo, clock: clock, policy: policy, signer: signer}
}

// GenerateManifest 只允许为已通过发布批准门禁的变更生成不可变授权清单。
// 清单创建后同一变更不能被静默替换为不同内容。
func (s *ReleaseService) GenerateManifest(ctx context.Context, tenantID, changeID, actor, environment, strategy string) (domain.ReleaseEvidenceManifest, error) {
	ctx = store.WithTenant(ctx, tenantID)
	var manifest domain.ReleaseEvidenceManifest
	err := s.repo.Transact(store.WithTenant(ctx, tenantID), func(tx store.Tx) error {
		if existing, err := tx.GetManifestByChange(tenantID, changeID); err == nil {
			manifest = existing
			return nil
		} else if !errors.Is(err, domain.ErrNotFound) {
			return err
		}

		change, err := tx.GetChange(tenantID, changeID)
		if err != nil {
			return err
		}
		if change.State != domain.StateReleaseApproved && change.State != domain.StateCanary &&
			change.State != domain.StatePromoted && change.State != domain.StateRolledBack &&
			change.State != domain.StateOutcomeVerified && change.State != domain.StateOutcomeFailed {
			return fmt.Errorf("%w: 当前状态 %s 尚未达到发布批准门", domain.ErrPolicyDenied, change.State)
		}
		if change.SpecDigest == "" || change.ArchitectureDigest == "" || change.PatchDigest == "" || change.ArtifactDigest == "" {
			return fmt.Errorf("%w: 发布清单缺少规格、架构、Patch 或制品摘要", domain.ErrEvidenceInsufficient)
		}
		nodes, err := tx.ListEvidence(tenantID, changeID)
		if err != nil {
			return err
		}
		required := []domain.EvidenceType{
			domain.EvidenceSpecDocument, domain.EvidenceArchitectureDocument,
			domain.EvidenceFrozenPatch, domain.EvidenceIndependentReview,
			domain.EvidenceCITest, domain.EvidenceSecurityScan, domain.EvidenceSBOM,
			domain.EvidenceBuildProvenance, domain.EvidenceArtifactSignature,
			domain.EvidenceStagingTest, domain.EvidenceReleaseApproval,
		}
		latestByType := map[domain.EvidenceType]domain.EvidenceNode{}
		for _, node := range nodes {
			latestByType[node.Type] = node
		}
		evidenceIDs := map[string]string{}
		for _, typ := range required {
			node, exists := latestByType[typ]
			if !exists || node.Verdict != domain.VerdictPass {
				return fmt.Errorf("%w: 最新的发布清单证据 %s 不存在或未通过", domain.ErrEvidenceInsufficient, typ)
			}
			evidenceIDs[string(typ)] = node.ID
		}
		evidenceRoot := ""
		if len(nodes) > 0 {
			evidenceRoot = nodes[len(nodes)-1].RecordHash
		}
		id, err := domain.NewID("rel")
		if err != nil {
			return err
		}
		manifest = domain.ReleaseEvidenceManifest{
			ManifestID: id, TenantID: tenantID, ChangeID: changeID, ProjectID: change.ProjectID,
			ArtifactDigest: change.ArtifactDigest, SpecDigest: change.SpecDigest,
			ArchitectureDigest: change.ArchitectureDigest, PatchDigest: change.PatchDigest,
			EvidenceRoot: evidenceRoot, PolicyDigest: s.policy.Digest(), EvidenceIDs: evidenceIDs,
			TargetEnvironment: defaultString(environment, "staging"),
			RolloutStrategy:   defaultString(strategy, "canary"), CreatedBy: actor,
			CreatedAt: s.clock.Now(), SignatureAlgorithm: "Ed25519",
			SigningKeyID: s.signer.PublicKeyID(), PublicKey: s.signer.PublicKeyBase64(),
		}
		unsigned := manifest
		unsigned.ContentDigest = ""
		unsigned.Signature = ""
		data, err := integrity.CanonicalJSON(unsigned)
		if err != nil {
			return err
		}
		manifest.ContentDigest = integrity.SHA256Bytes(data)
		manifest.Signature = base64.StdEncoding.EncodeToString(s.signer.Sign(data))
		return tx.SaveManifest(manifest)
	})
	return manifest, err
}

func (s *ReleaseService) VerifyManifest(manifest domain.ReleaseEvidenceManifest) error {
	unsigned := manifest
	unsigned.ContentDigest = ""
	unsigned.Signature = ""
	data, err := integrity.CanonicalJSON(unsigned)
	if err != nil {
		return err
	}
	if integrity.SHA256Bytes(data) != manifest.ContentDigest {
		return errors.New("发布清单内容摘要不匹配")
	}
	signature, err := base64.StdEncoding.DecodeString(manifest.Signature)
	if err != nil {
		return fmt.Errorf("解析发布清单签名失败: %w", err)
	}
	if manifest.SignatureAlgorithm != "Ed25519" {
		return fmt.Errorf("不支持的发布清单签名算法: %s", manifest.SignatureAlgorithm)
	}
	if err := security.VerifyWithPublicKeyBase64(manifest.PublicKey, data, signature); err != nil {
		return err
	}
	// 本地服务还会校验清单公钥是否属于当前受信签名器。生产版应替换为 KMS/Sigstore
	// 信任根和密钥版本注册表，以便在轮换后继续验证历史清单。
	if manifest.SigningKeyID != s.signer.PublicKeyID() {
		return fmt.Errorf("发布清单签名密钥不在当前信任集合中: %s", manifest.SigningKeyID)
	}
	return nil
}
