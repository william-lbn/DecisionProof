package product

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/william-lbn/DecisionProof/internal/domain"
	"github.com/william-lbn/DecisionProof/internal/integrity"
)

// Clock 便于测试中固定时间。
type Clock interface{ Now() time.Time }
type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now().UTC() }

// DecisionDraft 是 AI/规则引擎输出的候选决策。
type DecisionDraft struct {
	Kind       DecisionKind   `json:"kind"`
	Title      string         `json:"title"`
	Content    string         `json:"content"`
	Confidence float64        `json:"confidence"`
	Anchors    []SourceAnchor `json:"anchors"`
}

// Extractor 是模型无关的结构化提取接口。
type Extractor interface {
	Name() string
	Model() string
	Extract(ctx context.Context, blocks []SourceBlock) ([]DecisionDraft, string, error)
}

// Service 管理原始资料、AI 提取、人工确认和工程就绪包。
type Service struct {
	repo  Repository
	clock Clock
}

func NewService(repo Repository, clock Clock) *Service {
	if clock == nil {
		clock = RealClock{}
	}
	return &Service{repo: repo, clock: clock}
}

func (s *Service) RegisterDocument(ctx context.Context, tenantID, projectID, name, mediaType, storageURI, digest, actor string) (SourceDocument, error) {
	_ = ctx
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(projectID) == "" || strings.TrimSpace(name) == "" || strings.TrimSpace(digest) == "" {
		return SourceDocument{}, errors.New("tenant_id、project_id、name 和 digest 不能为空")
	}
	id, err := domain.NewID("doc")
	if err != nil {
		return SourceDocument{}, err
	}
	v := SourceDocument{ID: id, TenantID: tenantID, ProjectID: projectID, Name: name, MediaType: mediaType, StorageURI: storageURI, Digest: digest, Version: 1, Status: DocumentUploaded, UploadedBy: actor, CreatedAt: s.clock.Now()}
	return v, s.repo.SaveDocument(ctx, v)
}

func (s *Service) SaveParsedBlocks(ctx context.Context, tenantID, documentID string, blocks []SourceBlock) (SourceDocument, error) {
	_ = ctx
	doc, err := s.repo.GetDocument(ctx, tenantID, documentID)
	if err != nil {
		return SourceDocument{}, err
	}
	for i := range blocks {
		blocks[i].TenantID = tenantID
		blocks[i].ProjectID = doc.ProjectID
		blocks[i].DocumentID = documentID
		blocks[i].Sequence = i + 1
		blocks[i].Anchor = SourceAnchor{DocumentID: documentID, BlockID: blocks[i].ID, Locator: blocks[i].Locator, Digest: blocks[i].Digest}
	}
	if err := s.repo.SaveBlocks(ctx, blocks); err != nil {
		return SourceDocument{}, err
	}
	now := s.clock.Now()
	doc.Status = DocumentParsed
	doc.BlockCount = len(blocks)
	doc.ProcessedAt = &now
	doc.Version++
	return doc, s.repo.UpdateDocument(ctx, doc)
}

func (s *Service) RunExtraction(ctx context.Context, tenantID, documentID string, extractor Extractor) (ExtractionRun, []ProductDecision, error) {
	if extractor == nil {
		return ExtractionRun{}, nil, errors.New("extractor 不能为空")
	}
	doc, err := s.repo.GetDocument(ctx, tenantID, documentID)
	if err != nil {
		return ExtractionRun{}, nil, err
	}
	blocks, err := s.repo.ListBlocks(ctx, tenantID, documentID)
	if err != nil {
		return ExtractionRun{}, nil, err
	}
	started := s.clock.Now()
	drafts, promptDigest, err := extractor.Extract(ctx, blocks)
	runID, idErr := domain.NewID("ext")
	if idErr != nil {
		return ExtractionRun{}, nil, idErr
	}
	run := ExtractionRun{ID: runID, TenantID: tenantID, ProjectID: doc.ProjectID, DocumentID: documentID, Provider: extractor.Name(), Model: extractor.Model(), PromptDigest: promptDigest, StartedAt: started, FinishedAt: s.clock.Now(), Status: "SUCCEEDED"}
	if err != nil {
		run.Status = "FAILED"
		run.Error = err.Error()
		_ = s.repo.SaveExtractionRun(ctx, run)
		return run, nil, err
	}
	out := make([]ProductDecision, 0, len(drafts))
	for _, d := range drafts {
		if !d.Kind.Valid() || strings.TrimSpace(d.Content) == "" {
			continue
		}
		id, e := domain.NewID("dec")
		if e != nil {
			return run, nil, e
		}
		now := s.clock.Now()
		candidate := ProductDecision{ID: id, TenantID: tenantID, ProjectID: doc.ProjectID, Kind: d.Kind, Origin: DecisionOriginAI, Title: strings.TrimSpace(d.Title), Content: strings.TrimSpace(d.Content), Status: DecisionExtracted, Confidence: clamp(d.Confidence), Anchors: append([]SourceAnchor(nil), d.Anchors...), Version: 1, CreatedAt: now, UpdatedAt: now}
		candidate.CanonicalDigest, e = integrity.SHA256Object(struct {
			Kind           DecisionKind
			Origin         DecisionOrigin
			Title, Content string
			Anchors        []SourceAnchor
		}{candidate.Kind, candidate.Origin, candidate.Title, candidate.Content, candidate.Anchors})
		if e != nil {
			return run, nil, e
		}
		out = append(out, candidate)
		run.DecisionIDs = append(run.DecisionIDs, id)
	}
	doc.Status = DocumentProcessed
	doc.Version++
	if err := s.repo.SaveExtraction(ctx, doc, run, out); err != nil {
		return run, nil, err
	}
	return run, out, nil
}

func (s *Service) ConfirmDecision(ctx context.Context, tenantID, id, actor string, expectedVersion int) (ProductDecision, error) {
	_ = ctx
	v, err := s.repo.GetDecision(ctx, tenantID, id)
	if err != nil {
		return ProductDecision{}, err
	}
	if v.Status != DecisionExtracted && v.Status != DecisionSuggested {
		return ProductDecision{}, fmt.Errorf("只有 EXTRACTED/SUGGESTED 决策可以确认")
	}
	now := s.clock.Now()
	v.Status = DecisionConfirmed
	v.ConfirmedBy = actor
	v.ConfirmedAt = &now
	v.UpdatedAt = now
	v.Version++
	return v, s.repo.UpdateDecision(ctx, v, expectedVersion)
}
func (s *Service) ApproveDecision(ctx context.Context, tenantID, id, actor string, expectedVersion int) (ProductDecision, error) {
	_ = ctx
	v, err := s.repo.GetDecision(ctx, tenantID, id)
	if err != nil {
		return ProductDecision{}, err
	}
	if v.Status != DecisionConfirmed {
		return ProductDecision{}, fmt.Errorf("只有 CONFIRMED 决策可以批准")
	}
	if strings.TrimSpace(v.ConfirmedBy) != "" && strings.EqualFold(strings.TrimSpace(v.ConfirmedBy), strings.TrimSpace(actor)) {
		return ProductDecision{}, errors.New("确认人与批准人必须相互独立")
	}
	now := s.clock.Now()
	v.Status = DecisionApproved
	v.ApprovedBy = actor
	v.ApprovedAt = &now
	v.UpdatedAt = now
	v.Version++
	return v, s.repo.UpdateDecision(ctx, v, expectedVersion)
}

// CreateManualDecision 允许产品经理在没有来源文档时直接创建决策草案。
// 人工创建对象直接进入 CONFIRMED，但仍需由另一名授权人批准。
func (s *Service) CreateManualDecision(ctx context.Context, tenantID, projectID string, kind DecisionKind, title, content, actor string) (ProductDecision, error) {
	_ = ctx
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(projectID) == "" || !kind.Valid() || strings.TrimSpace(content) == "" || strings.TrimSpace(actor) == "" {
		return ProductDecision{}, errors.New("tenant_id、project_id、合法 kind、content 和 actor 不能为空")
	}
	id, err := domain.NewID("dec")
	if err != nil {
		return ProductDecision{}, err
	}
	now := s.clock.Now()
	v := ProductDecision{
		ID: id, TenantID: tenantID, ProjectID: projectID, Kind: kind, Origin: DecisionOriginManual,
		Title: strings.TrimSpace(title), Content: strings.TrimSpace(content), Status: DecisionConfirmed,
		Confidence: 1, ConfirmedBy: actor, Version: 1, CreatedAt: now, UpdatedAt: now, ConfirmedAt: &now,
	}
	if v.Title == "" {
		v.Title = string(kind)
	}
	v.CanonicalDigest, err = decisionDigest(v)
	if err != nil {
		return ProductDecision{}, err
	}
	return v, s.repo.SaveDecision(ctx, v)
}

// ReviseDecision 修改未批准决策，并使其回到人工已确认状态。
// 已批准决策不可原地修改，后续应创建新版本并通过 supersedes 关系替代。
func (s *Service) ReviseDecision(ctx context.Context, tenantID, id, title, content, actor string, expectedVersion int) (ProductDecision, error) {
	_ = ctx
	v, err := s.repo.GetDecision(ctx, tenantID, id)
	if err != nil {
		return ProductDecision{}, err
	}
	if v.Status == DecisionApproved || v.Status == DecisionSuperseded {
		return ProductDecision{}, errors.New("已批准或已替代决策不可原地修改")
	}
	if strings.TrimSpace(content) == "" {
		return ProductDecision{}, errors.New("决策内容不能为空")
	}
	now := s.clock.Now()
	v.Title = strings.TrimSpace(title)
	v.Content = strings.TrimSpace(content)
	v.Status = DecisionConfirmed
	v.ConfirmedBy = actor
	v.ConfirmedAt = &now
	v.ApprovedBy = ""
	v.ApprovedAt = nil
	v.RejectedBy = ""
	v.RejectionReason = ""
	v.RejectedAt = nil
	v.UpdatedAt = now
	v.Version++
	v.CanonicalDigest, err = decisionDigest(v)
	if err != nil {
		return ProductDecision{}, err
	}
	return v, s.repo.UpdateDecision(ctx, v, expectedVersion)
}

// RejectDecision 记录对模型建议或人工草案的正式否决，不物理删除历史对象。
func (s *Service) RejectDecision(ctx context.Context, tenantID, id, actor, reason string, expectedVersion int) (ProductDecision, error) {
	_ = ctx
	v, err := s.repo.GetDecision(ctx, tenantID, id)
	if err != nil {
		return ProductDecision{}, err
	}
	if v.Status == DecisionApproved || v.Status == DecisionSuperseded || v.Status == DecisionRejected {
		return ProductDecision{}, errors.New("当前决策状态不允许否决")
	}
	if strings.TrimSpace(reason) == "" {
		return ProductDecision{}, errors.New("否决原因不能为空")
	}
	now := s.clock.Now()
	v.Status = DecisionRejected
	v.RejectedBy = actor
	v.RejectionReason = strings.TrimSpace(reason)
	v.RejectedAt = &now
	v.UpdatedAt = now
	v.Version++
	return v, s.repo.UpdateDecision(ctx, v, expectedVersion)
}

func decisionDigest(v ProductDecision) (string, error) {
	return integrity.SHA256Object(struct {
		Kind           DecisionKind
		Origin         DecisionOrigin
		Title, Content string
		Anchors        []SourceAnchor
	}{v.Kind, v.Origin, v.Title, v.Content, v.Anchors})
}

func (s *Service) ListDecisions(ctx context.Context, tenantID, projectID string) ([]ProductDecision, error) {
	_ = ctx
	return s.repo.ListDecisions(ctx, tenantID, projectID)
}

func (s *Service) BuildEngineeringPackage(ctx context.Context, tenantID, projectID, actor string) (EngineeringReadinessPackage, error) {
	_ = ctx
	items, err := s.repo.ListDecisions(ctx, tenantID, projectID)
	if err != nil {
		return EngineeringReadinessPackage{}, err
	}
	approved := make([]ProductDecision, 0)
	for _, v := range items {
		if v.Status == DecisionApproved {
			approved = append(approved, v)
		}
	}
	if len(approved) == 0 {
		return EngineeringReadinessPackage{}, errors.New("没有已批准的产品决策")
	}
	var req, acc, risk, arch, outcome []string
	for _, v := range approved {
		switch v.Kind {
		case DecisionRequirement:
			req = append(req, v.ID)
		case DecisionAcceptanceCriterion:
			acc = append(acc, v.ID)
		case DecisionRisk:
			risk = append(risk, v.ID)
		case DecisionArchitectureDecision:
			arch = append(arch, v.ID)
		case DecisionOutcomeContract:
			outcome = append(outcome, v.ID)
		}
	}
	if len(req) == 0 || len(acc) == 0 || len(outcome) == 0 {
		return EngineeringReadinessPackage{}, errors.New("工程就绪包至少需要已批准的 REQUIREMENT、ACCEPTANCE_CRITERION 和 OUTCOME_CONTRACT")
	}
	sort.Slice(approved, func(i, j int) bool { return approved[i].ID < approved[j].ID })
	ids := make([]string, 0, len(approved))
	for _, v := range approved {
		ids = append(ids, v.ID+":"+v.CanonicalDigest)
	}
	digest, err := integrity.SHA256Object(ids)
	if err != nil {
		return EngineeringReadinessPackage{}, err
	}
	id, err := domain.NewID("erp")
	if err != nil {
		return EngineeringReadinessPackage{}, err
	}
	pkg := EngineeringReadinessPackage{ID: id, TenantID: tenantID, ProjectID: projectID, DecisionIDs: plainIDs(approved), RequirementIDs: req, AcceptanceIDs: acc, RiskIDs: risk, ArchitectureIDs: arch, OutcomeContractIDs: outcome, Digest: digest, CreatedBy: actor, CreatedAt: s.clock.Now()}
	return pkg, s.repo.SavePackage(ctx, pkg)
}

// GetEngineeringPackage 读取已生成的不可变工程就绪包。
func (s *Service) GetEngineeringPackage(ctx context.Context, tenantID, id string) (EngineeringReadinessPackage, error) {
	_ = ctx
	return s.repo.GetPackage(ctx, tenantID, id)
}

// GetPackageDecisions 返回工程就绪包引用的决策快照，并验证每个引用都存在。
func (s *Service) GetPackageDecisions(ctx context.Context, tenantID string, pkg EngineeringReadinessPackage) ([]ProductDecision, error) {
	_ = ctx
	items := make([]ProductDecision, 0, len(pkg.DecisionIDs))
	for _, id := range pkg.DecisionIDs {
		item, err := s.repo.GetDecision(ctx, tenantID, id)
		if err != nil {
			return nil, fmt.Errorf("读取工程就绪包决策 %s 失败: %w", id, err)
		}
		items = append(items, item)
	}
	return items, nil
}

func plainIDs(v []ProductDecision) []string {
	out := make([]string, 0, len(v))
	for _, x := range v {
		out = append(out, x.ID)
	}
	return out
}
func clamp(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
