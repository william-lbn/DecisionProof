package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/william-lbn/DecisionProof/internal/domain"
	"github.com/william-lbn/DecisionProof/internal/integrity"
	"github.com/william-lbn/DecisionProof/internal/store"
)

// AddEvidenceRequest 是写入证据节点的命令。
type AddEvidenceRequest struct {
	TenantID             string
	ChangeID             string
	Type                 domain.EvidenceType
	SchemaVersion        string
	Verdict              domain.EvidenceVerdict
	Producer             string
	ProducerIdentity     string
	TrustClass           string
	Subject              string // P0 兼容字段。
	SubjectType          string
	SubjectID            string
	SubjectDigest        string
	SourceSystem         string
	SourceURI            string
	ExternalID           string
	SupersedesEvidenceID string
	Payload              json.RawMessage
}

// EvidenceService 负责证据规范化、语义校验和哈希链维护。
type EvidenceService struct {
	repo  store.Repository
	clock Clock
}

func NewEvidenceService(repo store.Repository, clock Clock) *EvidenceService {
	return &EvidenceService{repo: repo, clock: clock}
}

func (s *EvidenceService) Add(ctx context.Context, req AddEvidenceRequest) (domain.EvidenceNode, error) {
	ctx = store.WithTenant(ctx, req.TenantID)
	if req.TenantID == "" || req.ChangeID == "" || req.Producer == "" {
		return domain.EvidenceNode{}, errors.New("tenant_id、change_id 和 producer 不能为空")
	}
	if !req.Type.Valid() {
		return domain.EvidenceNode{}, errors.New("证据 type 不属于平台受控类型")
	}
	if !req.Verdict.Valid() {
		return domain.EvidenceNode{}, errors.New("证据 verdict 不属于平台受控结论")
	}
	if len(req.Payload) == 0 || !json.Valid(req.Payload) {
		return domain.EvidenceNode{}, errors.New("证据 payload 必须是合法 JSON")
	}
	var result domain.EvidenceNode
	err := s.repo.Transact(store.WithTenant(ctx, req.TenantID), func(tx store.Tx) error {
		change, err := tx.GetChange(req.TenantID, req.ChangeID)
		if err != nil {
			return err
		}
		nodes, err := tx.ListEvidence(req.TenantID, req.ChangeID)
		if err != nil {
			return err
		}
		if err := validateEvidenceSemantics(change, req, nodes); err != nil {
			return err
		}
		id, err := domain.NewID("ev")
		if err != nil {
			return err
		}
		payloadDigest := integrity.SHA256Bytes(req.Payload)
		prevHash := ""
		if len(nodes) > 0 {
			prevHash = nodes[len(nodes)-1].RecordHash
		}
		now := s.clock.Now()
		subjectDigest := req.SubjectDigest
		if subjectDigest == "" {
			subjectDigest = req.Subject
		}
		node := domain.EvidenceNode{
			ID: id, TenantID: req.TenantID, ChangeID: req.ChangeID,
			Type: req.Type, SchemaVersion: defaultString(req.SchemaVersion, "evidence.v2"),
			Verdict: req.Verdict, Producer: req.Producer, ProducerIdentity: req.ProducerIdentity,
			TrustClass: defaultString(req.TrustClass, "DECLARED"), Subject: req.Subject,
			SubjectType: req.SubjectType, SubjectID: req.SubjectID, SubjectDigest: subjectDigest,
			SourceSystem: req.SourceSystem, SourceURI: req.SourceURI, ExternalID: req.ExternalID,
			Payload: append(json.RawMessage(nil), req.Payload...), PayloadDigest: payloadDigest,
			PrevHash: prevHash, Sequence: uint64(len(nodes) + 1), SupersedesEvidenceID: req.SupersedesEvidenceID,
			CreatedAt: now, ValidFrom: now,
		}
		node.RecordHash, err = integrity.SHA256Object(evidenceHashInput(node))
		if err != nil {
			return err
		}
		if err := tx.AppendEvidence(node, nil); err != nil {
			return err
		}

		// 将关键内容摘要同步到变更聚合根，保证发布清单绑定的是不可变内容而非可变 URL。
		updated := change
		switch req.Type {
		case domain.EvidenceSpecDocument:
			updated.SpecDigest = payloadDigest
		case domain.EvidenceArchitectureDocument:
			updated.ArchitectureDigest = payloadDigest
		case domain.EvidenceFrozenPatch:
			updated.PatchDigest = digestOrPayload(subjectDigest, payloadDigest)
		case domain.EvidenceArtifactSignature:
			updated.ArtifactDigest = digestOrPayload(subjectDigest, payloadDigest)
		}
		if updated != change {
			updated.Version++
			updated.UpdatedAt = s.clock.Now()
			if err := tx.UpdateChange(updated, change.Version); err != nil {
				return err
			}
		}
		result = node
		return nil
	})
	return result, err
}

// VerifyChain 重新计算整条证据链，发现任何内容、顺序或哈希被篡改时立即失败。
func (s *EvidenceService) VerifyChain(ctx context.Context, tenantID, changeID string) error {
	ctx = store.WithTenant(ctx, tenantID)
	return s.repo.Transact(store.WithTenant(ctx, tenantID), func(tx store.Tx) error {
		nodes, err := tx.ListEvidence(tenantID, changeID)
		if err != nil {
			return err
		}
		prev := ""
		for _, node := range nodes {
			if node.PrevHash != prev {
				return fmt.Errorf("证据 %s 的前序哈希不匹配", node.ID)
			}
			if integrity.SHA256Bytes(node.Payload) != node.PayloadDigest {
				return fmt.Errorf("证据 %s 的 payload 摘要不匹配", node.ID)
			}
			expected, err := integrity.SHA256Object(evidenceHashInput(node))
			if err != nil {
				return err
			}
			if expected != node.RecordHash {
				return fmt.Errorf("证据 %s 的记录哈希不匹配", node.ID)
			}
			prev = node.RecordHash
		}
		return nil
	})
}

func validateEvidenceSemantics(change domain.Change, req AddEvidenceRequest, existing []domain.EvidenceNode) error {
	if req.Verdict == "" {
		return errors.New("证据 verdict 不能为空")
	}
	if req.Type == domain.EvidenceIndependentReview {
		var body struct {
			AuthorIdentity   string `json:"author_identity"`
			ReviewerIdentity string `json:"reviewer_identity"`
			PatchDigest      string `json:"patch_digest"`
			Decision         string `json:"decision"`
		}
		if err := json.Unmarshal(req.Payload, &body); err != nil {
			return fmt.Errorf("解析独立审查证据失败: %w", err)
		}
		if body.AuthorIdentity == "" || body.ReviewerIdentity == "" || body.PatchDigest == "" {
			return errors.New("独立审查证据必须包含 author_identity、reviewer_identity 和 patch_digest")
		}
		if body.AuthorIdentity == body.ReviewerIdentity || req.Producer != body.ReviewerIdentity {
			return errors.New("作者与审查者必须是不同身份，且 producer 必须等于 reviewer_identity")
		}
		if change.PatchDigest == "" || body.PatchDigest != change.PatchDigest {
			return errors.New("审查对象必须与冻结 Patch 摘要完全一致")
		}
		if req.Verdict == domain.VerdictPass && strings.ToLower(body.Decision) != "approve" {
			return errors.New("PASS 审查证据的 decision 必须为 approve")
		}
	}
	if req.Type == domain.EvidenceSecurityScan && req.Verdict == domain.VerdictPass {
		var body struct {
			CriticalFindings int `json:"critical_findings"`
			HighFindings     int `json:"high_findings"`
		}
		if err := json.Unmarshal(req.Payload, &body); err != nil {
			return fmt.Errorf("解析安全扫描证据失败: %w", err)
		}
		if body.CriticalFindings != 0 {
			return errors.New("存在严重漏洞时不能提交 PASS 安全证据")
		}
	}
	artifactSubject := req.SubjectDigest
	if artifactSubject == "" {
		artifactSubject = req.Subject
	}
	if req.Type == domain.EvidenceArtifactSignature && !strings.HasPrefix(artifactSubject, "sha256:") {
		return errors.New("制品签名证据的 subject_digest 必须是 sha256 制品摘要")
	}
	_ = existing // 预留给后续重复证据和替代关系校验。
	return nil
}

func digestOrPayload(subject, payloadDigest string) string {
	if strings.HasPrefix(subject, "sha256:") {
		return subject
	}
	return payloadDigest
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func evidenceHashInput(node domain.EvidenceNode) any {
	return struct {
		ID, TenantID, ChangeID, Type, SchemaVersion, Verdict, Producer, ProducerIdentity   string
		TrustClass, Subject, SubjectType, SubjectID, SubjectDigest                         string
		SourceSystem, SourceURI, ExternalID, PayloadDigest, PrevHash, SupersedesEvidenceID string
		Sequence                                                                           uint64
		CreatedAt, ValidFrom                                                               int64
	}{node.ID, node.TenantID, node.ChangeID, string(node.Type), node.SchemaVersion, string(node.Verdict), node.Producer, node.ProducerIdentity, node.TrustClass, node.Subject, node.SubjectType, node.SubjectID, node.SubjectDigest, node.SourceSystem, node.SourceURI, node.ExternalID, node.PayloadDigest, node.PrevHash, node.SupersedesEvidenceID, node.Sequence, node.CreatedAt.UnixNano(), node.ValidFrom.UnixNano()}
}
