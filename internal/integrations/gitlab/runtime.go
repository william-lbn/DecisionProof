package gitlab

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/william-lbn/DecisionProof/internal/delivery"
	"github.com/william-lbn/DecisionProof/internal/domain"
	"github.com/william-lbn/DecisionProof/internal/product"
)

type IntegrationStatus string

const (
	IntegrationActive   IntegrationStatus = "ACTIVE"
	IntegrationDisabled IntegrationStatus = "DISABLED"
	IntegrationError    IntegrationStatus = "ERROR"
)

type DeliveryStatus string

const (
	DeliveryPending   DeliveryStatus = "PENDING"
	DeliveryExecuting DeliveryStatus = "EXECUTING"
	DeliverySucceeded DeliveryStatus = "SUCCEEDED"
	DeliveryFailed    DeliveryStatus = "FAILED"
	DeliveryUnknown   DeliveryStatus = "UNKNOWN"
)

type ProjectIntegration struct {
	TenantID          string            `json:"tenant_id"`
	IntegrationID     string            `json:"integration_id"`
	ProjectID         string            `json:"project_id"`
	ExternalProjectID int64             `json:"external_project_id"`
	BaseURL           string            `json:"base_url"`
	PathWithNamespace string            `json:"path_with_namespace"`
	DefaultBranch     string            `json:"default_branch"`
	Visibility        string            `json:"visibility"`
	SecretReference   string            `json:"-"`
	Status            IntegrationStatus `json:"status"`
	LastVerifiedAt    time.Time         `json:"last_verified_at"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

type DeliveryRun struct {
	TenantID        string         `json:"tenant_id"`
	DeliveryID      string         `json:"delivery_id"`
	IntegrationID   string         `json:"integration_id"`
	PackageID       string         `json:"package_id"`
	PackageDigest   string         `json:"package_digest"`
	IdempotencyKey  string         `json:"idempotency_key"`
	Status          DeliveryStatus `json:"status"`
	IssueIID        int64          `json:"issue_iid,omitempty"`
	BranchName      string         `json:"branch_name,omitempty"`
	CommitSHA       string         `json:"commit_sha,omitempty"`
	MergeRequestIID int64          `json:"merge_request_iid,omitempty"`
	Result          DeliveryResult `json:"result,omitempty"`
	LastError       string         `json:"last_error,omitempty"`
	CreatedBy       string         `json:"created_by"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

type RuntimeRepository interface {
	SaveIntegration(context.Context, ProjectIntegration) (ProjectIntegration, error)
	GetIntegration(context.Context, string, string) (ProjectIntegration, error)
	BeginDelivery(context.Context, DeliveryRun) (DeliveryRun, bool, error)
	UpdateDelivery(context.Context, DeliveryRun) (DeliveryRun, error)
}

type Credential struct {
	Token    string
	AuthMode AuthMode
}

type CredentialResolver interface {
	Resolve(context.Context, string, string) (Credential, error)
}

// StaticCredentialResolver 只解析部署时配置的单个 Secret 引用，
// 防止 API 调用者选择任意环境变量或把 Token 写进数据库。
type StaticCredentialResolver struct {
	Reference  string
	Credential Credential
}

func (r StaticCredentialResolver) Resolve(_ context.Context, _ string, reference string) (Credential, error) {
	if strings.TrimSpace(reference) == "" || reference != r.Reference || strings.TrimSpace(r.Credential.Token) == "" {
		return Credential{}, errors.New("GitLab Credential Reference 不可用")
	}
	return r.Credential, nil
}

type OnboardInput struct {
	TenantID          string
	ActorID           string
	IntegrationID     string
	ProjectID         string
	ExternalProjectID int64
}

type DeliverInput struct {
	TenantID       string
	ActorID        string
	IntegrationID  string
	PackageID      string
	IdempotencyKey string
}

type RuntimeService struct {
	Repository      RuntimeRepository
	Product         *product.Service
	Credentials     CredentialResolver
	BaseURL         string
	SecretReference string
	Now             func() time.Time
	NewClient       func(string, string, AuthMode) (*Client, error)
}

func (s *RuntimeService) Onboard(ctx context.Context, in OnboardInput) (ProjectIntegration, error) {
	if s.Repository == nil || s.Credentials == nil {
		return ProjectIntegration{}, errors.New("GitLab Runtime 未配置")
	}
	if strings.TrimSpace(in.TenantID) == "" || strings.TrimSpace(in.ActorID) == "" ||
		strings.TrimSpace(in.IntegrationID) == "" || strings.TrimSpace(in.ProjectID) == "" ||
		in.ExternalProjectID <= 0 {
		return ProjectIntegration{}, errors.New("GitLab Onboarding 参数不完整")
	}
	credential, err := s.Credentials.Resolve(ctx, in.TenantID, s.SecretReference)
	if err != nil {
		return ProjectIntegration{}, err
	}
	client, err := s.client(s.BaseURL, credential)
	if err != nil {
		return ProjectIntegration{}, err
	}
	projectView, err := client.GetProject(ctx, in.ExternalProjectID)
	if err != nil {
		return ProjectIntegration{}, fmt.Errorf("验证 GitLab Project 失败: %w", err)
	}
	if projectView.ID != in.ExternalProjectID || strings.TrimSpace(projectView.DefaultBranch) == "" {
		return ProjectIntegration{}, errors.New("GitLab Project 回读 ID 或默认分支无效")
	}
	now := s.now()
	integration := ProjectIntegration{
		TenantID: in.TenantID, IntegrationID: in.IntegrationID, ProjectID: in.ProjectID,
		ExternalProjectID: projectView.ID, BaseURL: strings.TrimRight(s.BaseURL, "/"),
		PathWithNamespace: projectView.PathWithNamespace, DefaultBranch: projectView.DefaultBranch,
		Visibility: projectView.Visibility, SecretReference: s.SecretReference,
		Status: IntegrationActive, LastVerifiedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	return s.Repository.SaveIntegration(ctx, integration)
}

// Deliver 先持久化 PENDING/EXECUTING，再允许任何外部写入。
// 已有 PENDING/UNKNOWN/EXECUTING 记录一律返回，不会因并发 HTTP 请求产生重复 GitLab 副作用。
func (s *RuntimeService) Deliver(ctx context.Context, in DeliverInput) (DeliveryRun, error) {
	if s.Repository == nil || s.Product == nil || s.Credentials == nil {
		return DeliveryRun{}, errors.New("GitLab Runtime 未配置")
	}
	if strings.TrimSpace(in.TenantID) == "" || strings.TrimSpace(in.ActorID) == "" ||
		strings.TrimSpace(in.IntegrationID) == "" || strings.TrimSpace(in.PackageID) == "" {
		return DeliveryRun{}, errors.New("GitLab Delivery 参数不完整")
	}
	key := strings.TrimSpace(in.IdempotencyKey)
	if key == "" || len(key) > 200 || strings.ContainsAny(key, " \t\r\n<>") {
		return DeliveryRun{}, errors.New("幂等键为空或格式无效")
	}
	in.IdempotencyKey = key
	integration, err := s.Repository.GetIntegration(ctx, in.TenantID, in.IntegrationID)
	if err != nil {
		return DeliveryRun{}, err
	}
	if integration.Status != IntegrationActive {
		return DeliveryRun{}, errors.New("GitLab Integration 不是 ACTIVE")
	}
	pkg, err := s.Product.GetEngineeringPackage(ctx, in.TenantID, in.PackageID)
	if err != nil {
		return DeliveryRun{}, err
	}
	if pkg.ProjectID != integration.ProjectID {
		return DeliveryRun{}, errors.New("工程就绪包与 GitLab Integration 项目不一致")
	}
	decisions, err := s.Product.GetPackageDecisions(ctx, in.TenantID, pkg)
	if err != nil {
		return DeliveryRun{}, err
	}
	bundle, err := delivery.RenderEngineeringBundle(pkg, decisions)
	if err != nil {
		return DeliveryRun{}, err
	}
	deliveryID, err := domain.NewID("gld")
	if err != nil {
		return DeliveryRun{}, err
	}
	now := s.now()
	run := DeliveryRun{
		TenantID: in.TenantID, DeliveryID: deliveryID, IntegrationID: in.IntegrationID,
		PackageID: pkg.ID, PackageDigest: pkg.Digest, IdempotencyKey: in.IdempotencyKey,
		Status: DeliveryPending, CreatedBy: in.ActorID, CreatedAt: now, UpdatedAt: now,
	}
	run, created, err := s.Repository.BeginDelivery(ctx, run)
	if err != nil {
		return DeliveryRun{}, err
	}
	if !created {
		if run.Status == DeliverySucceeded {
			return run, nil
		}
		// 不能把已有 PENDING 当成安全重试：它也可能来自仍在运行的并发请求。
		// 即使此前只是在 PENDING 后崩溃，也宁可人工确认后恢复，不允许两个执行器同时写 Provider。
		return run, fmt.Errorf("已有 GitLab Delivery 状态为 %s，必须先对账，禁止盲目重试", run.Status)
	}
	run.Status = DeliveryExecuting
	run.UpdatedAt = s.now()
	run, err = s.Repository.UpdateDelivery(ctx, run)
	if err != nil {
		return DeliveryRun{}, err
	}
	credential, err := s.Credentials.Resolve(ctx, in.TenantID, integration.SecretReference)
	if err != nil {
		return s.failRun(ctx, run, DeliveryFailed, err)
	}
	client, err := s.client(integration.BaseURL, credential)
	if err != nil {
		return s.failRun(ctx, run, DeliveryFailed, err)
	}
	result, executeErr := (DeliveryExecutor{API: client}).Execute(ctx, DeliveryRequest{
		ProjectID: integration.ExternalProjectID, DefaultBranch: integration.DefaultBranch,
		IdempotencyKey: in.IdempotencyKey, Bundle: bundle,
	})
	if executeErr != nil {
		status := DeliveryFailed
		if errors.Is(executeErr, ErrDeliveryUnknown) {
			status = DeliveryUnknown
		}
		return s.failRun(ctx, run, status, executeErr)
	}
	run.Status = DeliverySucceeded
	run.Result = result
	run.IssueIID = result.IssueIID
	run.BranchName = result.Branch
	run.CommitSHA = result.CommitSHA
	run.MergeRequestIID = result.MergeRequestIID
	run.LastError = ""
	run.UpdatedAt = s.now()
	return s.Repository.UpdateDelivery(ctx, run)
}

func (s *RuntimeService) failRun(ctx context.Context, run DeliveryRun, status DeliveryStatus, cause error) (DeliveryRun, error) {
	run.Status = status
	run.LastError = safeRuntimeError(cause)
	run.UpdatedAt = s.now()
	updated, updateErr := s.Repository.UpdateDelivery(ctx, run)
	if updateErr != nil {
		return run, fmt.Errorf("%v；保存 GitLab Delivery 失败: %w", cause, updateErr)
	}
	return updated, cause
}

func (s *RuntimeService) client(baseURL string, credential Credential) (*Client, error) {
	factory := s.NewClient
	if factory == nil {
		factory = NewClient
	}
	return factory(baseURL, credential.Token, credential.AuthMode)
}

func (s *RuntimeService) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func safeRuntimeError(err error) string {
	value := strings.TrimSpace(err.Error())
	if len(value) > 1000 {
		value = value[:1000]
	}
	return value
}

// MemoryRuntimeRepository 用于单元测试，不代表真实 Provider 验证。
type MemoryRuntimeRepository struct {
	mu           sync.Mutex
	integrations map[string]ProjectIntegration
	deliveries   map[string]DeliveryRun
}

func NewMemoryRuntimeRepository() *MemoryRuntimeRepository {
	return &MemoryRuntimeRepository{integrations: map[string]ProjectIntegration{}, deliveries: map[string]DeliveryRun{}}
}

func runtimeKey(tenantID, id string) string { return tenantID + "\x00" + id }

func (r *MemoryRuntimeRepository) SaveIntegration(_ context.Context, v ProjectIntegration) (ProjectIntegration, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := runtimeKey(v.TenantID, v.IntegrationID)
	if old, ok := r.integrations[key]; ok {
		v.CreatedAt = old.CreatedAt
	}
	r.integrations[key] = v
	return v, nil
}
func (r *MemoryRuntimeRepository) GetIntegration(_ context.Context, tenantID, integrationID string) (ProjectIntegration, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.integrations[runtimeKey(tenantID, integrationID)]
	if !ok {
		return ProjectIntegration{}, sql.ErrNoRows
	}
	return v, nil
}
func (r *MemoryRuntimeRepository) BeginDelivery(_ context.Context, v DeliveryRun) (DeliveryRun, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.deliveries {
		if existing.TenantID == v.TenantID && existing.IntegrationID == v.IntegrationID &&
			(existing.PackageDigest == v.PackageDigest || existing.IdempotencyKey == v.IdempotencyKey) {
			return existing, false, nil
		}
	}
	r.deliveries[runtimeKey(v.TenantID, v.DeliveryID)] = v
	return v, true, nil
}
func (r *MemoryRuntimeRepository) UpdateDelivery(_ context.Context, v DeliveryRun) (DeliveryRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := runtimeKey(v.TenantID, v.DeliveryID)
	if _, ok := r.deliveries[key]; !ok {
		return DeliveryRun{}, sql.ErrNoRows
	}
	r.deliveries[key] = v
	return v, nil
}

// PostgresRuntimeRepository 通过 RLS 保存 Onboarding 与 Delivery 状态。
type PostgresRuntimeRepository struct{ db *sql.DB }

func NewPostgresRuntimeRepository(db *sql.DB) (*PostgresRuntimeRepository, error) {
	if db == nil {
		return nil, errors.New("PostgreSQL DB 不能为空")
	}
	return &PostgresRuntimeRepository{db: db}, nil
}

func (r *PostgresRuntimeRepository) withTenant(ctx context.Context, tenantID string, fn func(context.Context, *sql.Tx) error) error {
	callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := r.db.BeginTx(callCtx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(callCtx, `SELECT set_config('app.tenant_id', $1, true)`, tenantID); err != nil {
		return err
	}
	if err := fn(callCtx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *PostgresRuntimeRepository) SaveIntegration(ctx context.Context, v ProjectIntegration) (ProjectIntegration, error) {
	configuration, err := json.Marshal(map[string]string{
		"path_with_namespace": v.PathWithNamespace, "default_branch": v.DefaultBranch,
		"visibility": v.Visibility,
	})
	if err != nil {
		return ProjectIntegration{}, err
	}
	err = r.withTenant(ctx, v.TenantID, func(callCtx context.Context, tx *sql.Tx) error {
		return tx.QueryRowContext(callCtx, `
INSERT INTO project_integrations
 (tenant_id,integration_id,project_id,provider,external_project_id,base_url,configuration,secret_reference,status,last_verified_at,created_at,updated_at)
VALUES ($1,$2,$3,'gitlab',$4,$5,$6,$7,$8,$9,$10,$10)
ON CONFLICT (tenant_id,integration_id) DO UPDATE SET
 project_id=EXCLUDED.project_id, external_project_id=EXCLUDED.external_project_id,
 base_url=EXCLUDED.base_url, configuration=EXCLUDED.configuration,
 secret_reference=EXCLUDED.secret_reference, status=EXCLUDED.status,
 last_verified_at=EXCLUDED.last_verified_at, updated_at=EXCLUDED.updated_at
RETURNING created_at,updated_at`,
			v.TenantID, v.IntegrationID, v.ProjectID, strconv.FormatInt(v.ExternalProjectID, 10),
			v.BaseURL, configuration, v.SecretReference, v.Status, v.LastVerifiedAt, v.UpdatedAt,
		).Scan(&v.CreatedAt, &v.UpdatedAt)
	})
	return v, err
}

func (r *PostgresRuntimeRepository) GetIntegration(ctx context.Context, tenantID, integrationID string) (ProjectIntegration, error) {
	var v ProjectIntegration
	var externalID string
	var configuration []byte
	err := r.withTenant(ctx, tenantID, func(callCtx context.Context, tx *sql.Tx) error {
		return tx.QueryRowContext(callCtx, `
SELECT tenant_id,integration_id,project_id,external_project_id,base_url,configuration,
 secret_reference,status,last_verified_at,created_at,updated_at
FROM project_integrations WHERE tenant_id=$1 AND integration_id=$2 AND provider='gitlab'`,
			tenantID, integrationID,
		).Scan(&v.TenantID, &v.IntegrationID, &v.ProjectID, &externalID, &v.BaseURL, &configuration,
			&v.SecretReference, &v.Status, &v.LastVerifiedAt, &v.CreatedAt, &v.UpdatedAt)
	})
	if err != nil {
		return ProjectIntegration{}, err
	}
	v.ExternalProjectID, err = strconv.ParseInt(externalID, 10, 64)
	if err != nil {
		return ProjectIntegration{}, errors.New("数据库中的 GitLab External Project ID 无效")
	}
	var cfg map[string]string
	if err := json.Unmarshal(configuration, &cfg); err != nil {
		return ProjectIntegration{}, err
	}
	v.PathWithNamespace, v.DefaultBranch, v.Visibility = cfg["path_with_namespace"], cfg["default_branch"], cfg["visibility"]
	return v, nil
}

func (r *PostgresRuntimeRepository) BeginDelivery(ctx context.Context, v DeliveryRun) (DeliveryRun, bool, error) {
	created := false
	err := r.withTenant(ctx, v.TenantID, func(callCtx context.Context, tx *sql.Tx) error {
		result := tx.QueryRowContext(callCtx, `
INSERT INTO gitlab_delivery_runs
 (tenant_id,delivery_id,integration_id,package_id,package_digest,idempotency_key,status,created_by,created_at,updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$9)
ON CONFLICT DO NOTHING
RETURNING delivery_id`,
			v.TenantID, v.DeliveryID, v.IntegrationID, v.PackageID, v.PackageDigest,
			v.IdempotencyKey, v.Status, v.CreatedBy, v.CreatedAt,
		)
		var id string
		if err := result.Scan(&id); err == nil {
			created = true
			return nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		return scanDelivery(tx.QueryRowContext(callCtx, `
SELECT tenant_id,delivery_id,integration_id,package_id,package_digest,idempotency_key,status,
 COALESCE(issue_iid,0),branch_name,commit_sha,COALESCE(merge_request_iid,0),result,last_error,
 created_by,created_at,updated_at
FROM gitlab_delivery_runs
WHERE tenant_id=$1 AND integration_id=$2 AND (package_digest=$3 OR idempotency_key=$4)
ORDER BY created_at LIMIT 1`, v.TenantID, v.IntegrationID, v.PackageDigest, v.IdempotencyKey), &v)
	})
	return v, created, err
}

func (r *PostgresRuntimeRepository) UpdateDelivery(ctx context.Context, v DeliveryRun) (DeliveryRun, error) {
	resultJSON, err := json.Marshal(v.Result)
	if err != nil {
		return DeliveryRun{}, err
	}
	err = r.withTenant(ctx, v.TenantID, func(callCtx context.Context, tx *sql.Tx) error {
		return scanDelivery(tx.QueryRowContext(callCtx, `
UPDATE gitlab_delivery_runs SET
 status=$3,issue_iid=NULLIF($4,0),branch_name=$5,commit_sha=$6,
 merge_request_iid=NULLIF($7,0),result=$8,last_error=$9,updated_at=$10
WHERE tenant_id=$1 AND delivery_id=$2
RETURNING tenant_id,delivery_id,integration_id,package_id,package_digest,idempotency_key,status,
 COALESCE(issue_iid,0),branch_name,commit_sha,COALESCE(merge_request_iid,0),result,last_error,
 created_by,created_at,updated_at`,
			v.TenantID, v.DeliveryID, v.Status, v.IssueIID, v.BranchName, v.CommitSHA,
			v.MergeRequestIID, resultJSON, v.LastError, v.UpdatedAt,
		), &v)
	})
	return v, err
}

func scanDelivery(row *sql.Row, v *DeliveryRun) error {
	var resultJSON []byte
	if err := row.Scan(
		&v.TenantID, &v.DeliveryID, &v.IntegrationID, &v.PackageID, &v.PackageDigest,
		&v.IdempotencyKey, &v.Status, &v.IssueIID, &v.BranchName, &v.CommitSHA,
		&v.MergeRequestIID, &resultJSON, &v.LastError, &v.CreatedBy, &v.CreatedAt, &v.UpdatedAt,
	); err != nil {
		return err
	}
	if len(resultJSON) > 0 {
		return json.Unmarshal(resultJSON, &v.Result)
	}
	return nil
}
