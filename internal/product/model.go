package product

import "time"

// DocumentStatus 表示原始产品资料在平台中的处理状态。
type DocumentStatus string

const (
	DocumentUploaded  DocumentStatus = "UPLOADED"
	DocumentParsed    DocumentStatus = "PARSED"
	DocumentProcessed DocumentStatus = "PROCESSED"
	DocumentFailed    DocumentStatus = "FAILED"
)

// SourceDocument 保存 Word、PPT、Markdown、纯文本等原始产品资料的元数据。
// 原始二进制不直接存入数据库，而是放在对象存储中，并用 Digest 保证内容不可变。
type SourceDocument struct {
	ID          string         `json:"id"`
	TenantID    string         `json:"tenant_id"`
	ProjectID   string         `json:"project_id"`
	Name        string         `json:"name"`
	MediaType   string         `json:"media_type"`
	StorageURI  string         `json:"storage_uri"`
	Digest      string         `json:"digest"`
	Version     int            `json:"version"`
	Status      DocumentStatus `json:"status"`
	BlockCount  int            `json:"block_count"`
	Error       string         `json:"error,omitempty"`
	UploadedBy  string         `json:"uploaded_by"`
	CreatedAt   time.Time      `json:"created_at"`
	ProcessedAt *time.Time     `json:"processed_at,omitempty"`
}

// SourceAnchor 是所有 AI 提取结果的来源锚点。
// Locator 示例：docx:paragraph:12、pptx:slide:8、markdown:line:30-42。
type SourceAnchor struct {
	DocumentID string `json:"document_id"`
	BlockID    string `json:"block_id"`
	Locator    string `json:"locator"`
	Digest     string `json:"digest"`
}

// SourceBlock 是从原始资料中提取出的最小可追踪文本块。
type SourceBlock struct {
	ID         string       `json:"id"`
	TenantID   string       `json:"tenant_id"`
	ProjectID  string       `json:"project_id"`
	DocumentID string       `json:"document_id"`
	Kind       string       `json:"kind"`
	Locator    string       `json:"locator"`
	Text       string       `json:"text"`
	Digest     string       `json:"digest"`
	Sequence   int          `json:"sequence"`
	Anchor     SourceAnchor `json:"anchor"`
	CreatedAt  time.Time    `json:"created_at"`
}

// DecisionKind 是产品决策知识图中的受控对象类型。
type DecisionKind string

const (
	DecisionMarketSignal         DecisionKind = "MARKET_SIGNAL"
	DecisionCustomerInsight      DecisionKind = "CUSTOMER_INSIGHT"
	DecisionOpportunity          DecisionKind = "OPPORTUNITY"
	DecisionProblemStatement     DecisionKind = "PROBLEM_STATEMENT"
	DecisionBusinessCase         DecisionKind = "BUSINESS_CASE"
	DecisionProductObjective     DecisionKind = "PRODUCT_OBJECTIVE"
	DecisionKeyResult            DecisionKind = "KEY_RESULT"
	DecisionInitiative           DecisionKind = "INITIATIVE"
	DecisionRequirement          DecisionKind = "REQUIREMENT"
	DecisionAcceptanceCriterion  DecisionKind = "ACCEPTANCE_CRITERION"
	DecisionNonFunctional        DecisionKind = "NON_FUNCTIONAL_REQUIREMENT"
	DecisionConstraint           DecisionKind = "CONSTRAINT"
	DecisionRisk                 DecisionKind = "RISK"
	DecisionDependency           DecisionKind = "DEPENDENCY"
	DecisionOutOfScope           DecisionKind = "OUT_OF_SCOPE"
	DecisionArchitectureDecision DecisionKind = "ARCHITECTURE_DECISION"
	DecisionOutcomeContract      DecisionKind = "OUTCOME_CONTRACT"
	DecisionExperimentHypothesis DecisionKind = "EXPERIMENT_HYPOTHESIS"
)

func (k DecisionKind) Valid() bool {
	switch k {
	case DecisionMarketSignal, DecisionCustomerInsight, DecisionOpportunity,
		DecisionProblemStatement, DecisionBusinessCase, DecisionProductObjective,
		DecisionKeyResult, DecisionInitiative, DecisionRequirement,
		DecisionAcceptanceCriterion, DecisionNonFunctional, DecisionConstraint,
		DecisionRisk, DecisionDependency, DecisionOutOfScope,
		DecisionArchitectureDecision, DecisionOutcomeContract,
		DecisionExperimentHypothesis:
		return true
	default:
		return false
	}
}

// DecisionOrigin 区分 AI 提取、人工创建和外部系统导入，避免把模型建议伪装成人工事实。
type DecisionOrigin string

const (
	DecisionOriginAI       DecisionOrigin = "AI_EXTRACTED"
	DecisionOriginManual   DecisionOrigin = "MANUAL"
	DecisionOriginImported DecisionOrigin = "IMPORTED"
)

// DecisionStatus 把 AI 提取、人工确认与正式批准严格区分。
type DecisionStatus string

const (
	DecisionExtracted  DecisionStatus = "EXTRACTED"
	DecisionSuggested  DecisionStatus = "SUGGESTED"
	DecisionConfirmed  DecisionStatus = "CONFIRMED"
	DecisionApproved   DecisionStatus = "APPROVED"
	DecisionRejected   DecisionStatus = "REJECTED"
	DecisionSuperseded DecisionStatus = "SUPERSEDED"
)

// ProductDecision 是从原始资料中提取、讨论并批准的结构化产品决策。
type ProductDecision struct {
	ID              string         `json:"id"`
	TenantID        string         `json:"tenant_id"`
	ProjectID       string         `json:"project_id"`
	Kind            DecisionKind   `json:"kind"`
	Origin          DecisionOrigin `json:"origin"`
	Title           string         `json:"title"`
	Content         string         `json:"content"`
	Status          DecisionStatus `json:"status"`
	Confidence      float64        `json:"confidence"`
	Anchors         []SourceAnchor `json:"anchors"`
	Owner           string         `json:"owner,omitempty"`
	ConfirmedBy     string         `json:"confirmed_by,omitempty"`
	ApprovedBy      string         `json:"approved_by,omitempty"`
	RejectedBy      string         `json:"rejected_by,omitempty"`
	RejectionReason string         `json:"rejection_reason,omitempty"`
	Version         int            `json:"version"`
	CanonicalDigest string         `json:"canonical_digest"`
	SupersedesID    string         `json:"supersedes_id,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	ConfirmedAt     *time.Time     `json:"confirmed_at,omitempty"`
	ApprovedAt      *time.Time     `json:"approved_at,omitempty"`
	RejectedAt      *time.Time     `json:"rejected_at,omitempty"`
}

// EngineeringReadinessPackage 是产品决策进入工程交付前的不可变基线。
type EngineeringReadinessPackage struct {
	ID                 string    `json:"id"`
	TenantID           string    `json:"tenant_id"`
	ProjectID          string    `json:"project_id"`
	DecisionIDs        []string  `json:"decision_ids"`
	RequirementIDs     []string  `json:"requirement_ids"`
	AcceptanceIDs      []string  `json:"acceptance_ids"`
	RiskIDs            []string  `json:"risk_ids"`
	ArchitectureIDs    []string  `json:"architecture_ids"`
	OutcomeContractIDs []string  `json:"outcome_contract_ids"`
	Digest             string    `json:"digest"`
	CreatedBy          string    `json:"created_by"`
	CreatedAt          time.Time `json:"created_at"`
}

// ExtractionRun 记录一次 AI 或规则提取执行，便于评估模型漂移和复现。
type ExtractionRun struct {
	ID           string    `json:"id"`
	TenantID     string    `json:"tenant_id"`
	ProjectID    string    `json:"project_id"`
	DocumentID   string    `json:"document_id"`
	Provider     string    `json:"provider"`
	Model        string    `json:"model"`
	PromptDigest string    `json:"prompt_digest"`
	ConfigDigest string    `json:"config_digest"`
	DecisionIDs  []string  `json:"decision_ids"`
	StartedAt    time.Time `json:"started_at"`
	FinishedAt   time.Time `json:"finished_at"`
	Status       string    `json:"status"`
	Error        string    `json:"error,omitempty"`
}
