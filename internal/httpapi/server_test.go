package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/william-lbn/DecisionProof/internal/executor"
	"github.com/william-lbn/DecisionProof/internal/security"
	"github.com/william-lbn/DecisionProof/internal/service"
	"github.com/william-lbn/DecisionProof/internal/store"
	"github.com/william-lbn/DecisionProof/internal/telemetry"
)

func testHandler(t *testing.T) http.Handler {
	t.Helper()
	repo := store.NewMemoryStore()
	clock := service.RealClock{}
	policy := service.NewTransitionPolicy()
	signer, err := security.NewEphemeralSigner()
	if err != nil {
		t.Fatal(err)
	}
	local := executor.NewLocalExecutor()
	return NewServer(
		service.NewChangeService(repo, clock, policy),
		service.NewEvidenceService(repo, clock),
		service.NewActionGateway(repo, clock, service.NewActionPolicy(), executor.StaticRegistry{Executor: local}),
		service.NewReleaseService(repo, clock, policy, signer),
		telemetry.NewMetrics(),
	)
}

func TestAPIRequiresTrustedIdentity(t *testing.T) {
	h := testHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/changes", bytes.NewBufferString(`{"project_id":"p","title":"t"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("缺少身份时应返回 401，实际 %d", rec.Code)
	}
}

func TestCreateAndGetChange(t *testing.T) {
	h := testHandler(t)
	createReq := httptest.NewRequest(http.MethodPost, "/v1/changes", bytes.NewBufferString(`{"project_id":"order-service","title":"批量退款","risk_level":"high"}`))
	createReq.Header.Set("X-Tenant-ID", "tenant-a")
	createReq.Header.Set("X-Actor-ID", "user:pm")
	createReq.Header.Set("X-Actor-Roles", "product_manager")
	createRec := httptest.NewRecorder()
	h.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("创建失败: code=%d body=%s", createRec.Code, createRec.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil || created.ID == "" {
		t.Fatalf("解析创建响应失败: %v", err)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/v1/changes/"+created.ID, nil).WithContext(context.Background())
	getReq.Header.Set("X-Tenant-ID", "tenant-a")
	getReq.Header.Set("X-Actor-ID", "user:pm")
	getRec := httptest.NewRecorder()
	h.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("读取失败: code=%d body=%s", getRec.Code, getRec.Body.String())
	}
}

func TestRejectUnknownJSONField(t *testing.T) {
	h := testHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/changes", bytes.NewBufferString(`{"project_id":"p","title":"t","unexpected":true}`))
	req.Header.Set("X-Tenant-ID", "tenant-a")
	req.Header.Set("X-Actor-ID", "user:pm")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("未知字段应被拒绝，实际 %d", rec.Code)
	}
}

func TestReadinessFailsClosedWhenDependencyIsUnavailable(t *testing.T) {
	repo := store.NewMemoryStore()
	clock := service.RealClock{}
	policy := service.NewTransitionPolicy()
	signer, err := security.NewEphemeralSigner()
	if err != nil {
		t.Fatal(err)
	}
	h := NewServer(
		service.NewChangeService(repo, clock, policy),
		service.NewEvidenceService(repo, clock),
		service.NewActionGateway(repo, clock, service.NewActionPolicy(), executor.StaticRegistry{Executor: executor.NewLocalExecutor()}),
		service.NewReleaseService(repo, clock, policy, signer),
		telemetry.NewMetrics(),
		func(context.Context) error { return errors.New("database unavailable") },
	)
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("依赖不可用时 Readiness 必须返回 503，实际 %d", rec.Code)
	}
}
