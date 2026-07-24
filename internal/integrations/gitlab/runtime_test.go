package gitlab

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/william-lbn/DecisionProof/internal/product"
)

type contractGitLab struct {
	mu            sync.Mutex
	issue         Issue
	branch        Branch
	files         map[string]RepositoryFile
	mr            MergeRequest
	issueCreates  int
	branchCreates int
	commitCreates int
	mrCreates     int
}

func (s *contractGitLab) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v4/projects/7":
		writeContractJSON(w, http.StatusOK, Project{ID: 7, PathWithNamespace: "truth/project", DefaultBranch: "main", Visibility: "private"})
	case r.URL.Path == "/api/v4/projects/7/issues" && r.Method == http.MethodGet:
		if s.issue.IID == 0 {
			writeContractJSON(w, http.StatusOK, []Issue{})
		} else {
			writeContractJSON(w, http.StatusOK, []Issue{s.issue})
		}
	case r.URL.Path == "/api/v4/projects/7/issues" && r.Method == http.MethodPost:
		var body struct {
			Title, Description string
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.issueCreates++
		s.issue = Issue{IID: 11, Title: body.Title, Description: body.Description, State: "opened"}
		writeContractJSON(w, http.StatusCreated, s.issue)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v4/projects/7/issues/11":
		writeContractJSON(w, http.StatusOK, s.issue)
	case strings.HasPrefix(r.URL.Path, "/api/v4/projects/7/repository/branches/") && r.Method == http.MethodGet:
		if s.branch.Name == "" {
			writeContractJSON(w, http.StatusNotFound, map[string]string{"message": "404 Branch Not Found"})
		} else {
			writeContractJSON(w, http.StatusOK, s.branch)
		}
	case r.URL.Path == "/api/v4/projects/7/repository/branches" && r.Method == http.MethodPost:
		var body struct {
			Branch string `json:"branch"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.branchCreates++
		s.branch = Branch{Name: body.Branch}
		s.branch.Commit.ID = "base-sha"
		writeContractJSON(w, http.StatusCreated, s.branch)
	case strings.HasPrefix(r.URL.Path, "/api/v4/projects/7/repository/files/") && r.Method == http.MethodGet:
		escaped := strings.TrimPrefix(r.URL.EscapedPath(), "/api/v4/projects/7/repository/files/")
		filePath, _ := url.PathUnescape(escaped)
		file, ok := s.files[filePath]
		if !ok {
			writeContractJSON(w, http.StatusNotFound, map[string]string{"message": "404 File Not Found"})
		} else {
			writeContractJSON(w, http.StatusOK, file)
		}
	case r.URL.Path == "/api/v4/projects/7/repository/commits" && r.Method == http.MethodPost:
		var body struct {
			Actions []CommitAction `json:"actions"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.commitCreates++
		if s.files == nil {
			s.files = map[string]RepositoryFile{}
		}
		for _, action := range body.Actions {
			s.files[action.FilePath] = RepositoryFile{
				FilePath: action.FilePath, Encoding: "base64",
				Content: base64.StdEncoding.EncodeToString([]byte(action.Content)), LastCommitID: "commit-sha",
			}
		}
		writeContractJSON(w, http.StatusCreated, Commit{ID: "commit-sha"})
	case r.URL.Path == "/api/v4/projects/7/merge_requests" && r.Method == http.MethodGet:
		if s.mr.IID == 0 {
			writeContractJSON(w, http.StatusOK, []MergeRequest{})
		} else {
			writeContractJSON(w, http.StatusOK, []MergeRequest{s.mr})
		}
	case r.URL.Path == "/api/v4/projects/7/merge_requests" && r.Method == http.MethodPost:
		var body struct {
			SourceBranch string `json:"source_branch"`
			TargetBranch string `json:"target_branch"`
			Title        string `json:"title"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.mrCreates++
		s.mr = MergeRequest{
			IID: 22, Title: body.Title, State: "opened", Draft: true,
			SourceBranch: body.SourceBranch, TargetBranch: body.TargetBranch,
			SHA: "commit-sha", WebURL: "https://gitlab.example/mr/22",
		}
		writeContractJSON(w, http.StatusCreated, s.mr)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v4/projects/7/merge_requests/22":
		writeContractJSON(w, http.StatusOK, s.mr)
	default:
		writeContractJSON(w, http.StatusNotFound, map[string]string{"path": r.URL.Path})
	}
}

func writeContractJSON(w http.ResponseWriter, status int, value any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func TestRuntimeOnboardAndDeliveryContract(t *testing.T) {
	providerState := &contractGitLab{}
	provider := httptest.NewServer(providerState)
	defer provider.Close()
	productRepo := product.NewMemoryRepository()
	products := product.NewService(productRepo, product.RealClock{})
	ctx := context.Background()
	packageID := buildRuntimePackage(t, ctx, productRepo, products)
	runtimeRepo := NewMemoryRuntimeRepository()
	service := &RuntimeService{
		Repository: runtimeRepo, Product: products, BaseURL: provider.URL, SecretReference: "gitlab-test",
		Credentials: StaticCredentialResolver{
			Reference: "gitlab-test", Credential: Credential{Token: "test-token", AuthMode: AuthPrivateToken},
		},
	}
	integration, err := service.Onboard(ctx, OnboardInput{
		TenantID: "tenant", ActorID: "admin", IntegrationID: "gitlab", ProjectID: "project", ExternalProjectID: 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	if integration.DefaultBranch != "main" || integration.PathWithNamespace != "truth/project" {
		t.Fatalf("Onboarding 未绑定真实项目回读: %+v", integration)
	}
	deliveryInput := DeliverInput{
		TenantID: "tenant", ActorID: "delivery-manager", IntegrationID: "gitlab",
		PackageID: packageID, IdempotencyKey: "delivery:" + packageID,
	}
	first, err := service.Deliver(ctx, deliveryInput)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Deliver(ctx, deliveryInput)
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != DeliverySucceeded || second.DeliveryID != first.DeliveryID ||
		first.CommitSHA != "commit-sha" || first.MergeRequestIID != 22 {
		t.Fatalf("Runtime Delivery 结果无效: first=%+v second=%+v", first, second)
	}
	if providerState.issueCreates != 1 || providerState.branchCreates != 1 ||
		providerState.commitCreates != 1 || providerState.mrCreates != 1 {
		t.Fatalf("重复 Runtime 调用产生外部副作用: %+v", providerState)
	}
}

func TestRuntimeExistingPendingFailsClosed(t *testing.T) {
	ctx := context.Background()
	productRepo := product.NewMemoryRepository()
	products := product.NewService(productRepo, product.RealClock{})
	packageID := buildRuntimePackage(t, ctx, productRepo, products)
	pkg, err := products.GetEngineeringPackage(ctx, "tenant", packageID)
	if err != nil {
		t.Fatal(err)
	}
	runtimeRepo := NewMemoryRuntimeRepository()
	now := time.Now().UTC()
	_, err = runtimeRepo.SaveIntegration(ctx, ProjectIntegration{
		TenantID: "tenant", IntegrationID: "gitlab", ProjectID: "project",
		ExternalProjectID: 7, BaseURL: "https://gitlab.example", DefaultBranch: "main",
		SecretReference: "gitlab-test", Status: IntegrationActive, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	pending := DeliveryRun{
		TenantID: "tenant", DeliveryID: "gld-pending", IntegrationID: "gitlab",
		PackageID: packageID, PackageDigest: pkg.Digest, IdempotencyKey: "delivery-pending",
		Status: DeliveryPending, CreatedBy: "delivery-manager", CreatedAt: now, UpdatedAt: now,
	}
	if _, created, err := runtimeRepo.BeginDelivery(ctx, pending); err != nil || !created {
		t.Fatalf("预置 PENDING 失败: created=%v err=%v", created, err)
	}
	service := &RuntimeService{
		Repository: runtimeRepo, Product: products, BaseURL: "https://gitlab.example",
		SecretReference: "gitlab-test",
		Credentials: StaticCredentialResolver{
			Reference: "gitlab-test", Credential: Credential{Token: "unused", AuthMode: AuthPrivateToken},
		},
	}
	run, err := service.Deliver(ctx, DeliverInput{
		TenantID: "tenant", ActorID: "delivery-manager", IntegrationID: "gitlab",
		PackageID: packageID, IdempotencyKey: pending.IdempotencyKey,
	})
	if err == nil || run.Status != DeliveryPending {
		t.Fatalf("已有 PENDING 必须 Fail-closed: run=%+v err=%v", run, err)
	}
}

func buildRuntimePackage(t *testing.T, ctx context.Context, repo product.Repository, service *product.Service) string {
	t.Helper()
	fixtures := []struct {
		id   string
		kind product.DecisionKind
	}{
		{"req", product.DecisionRequirement},
		{"acc", product.DecisionAcceptanceCriterion},
		{"out", product.DecisionOutcomeContract},
	}
	for _, fixture := range fixtures {
		item := product.ProductDecision{
			ID: fixture.id, TenantID: "tenant", ProjectID: "project", Kind: fixture.kind,
			Title: fixture.id, Content: "approved " + fixture.id, Status: product.DecisionApproved,
			CanonicalDigest: fmt.Sprintf("sha256:%064s", fixture.id), Version: 1,
		}
		if err := repo.SaveDecision(ctx, item); err != nil {
			t.Fatal(err)
		}
	}
	pkg, err := service.BuildEngineeringPackage(ctx, "tenant", "project", "delivery-manager")
	if err != nil {
		t.Fatal(err)
	}
	return pkg.ID
}

func TestRuntimeHTTPRBAC(t *testing.T) {
	handler := RuntimeHTTPHandler{}
	req := httptest.NewRequest(http.MethodPost, "/integrations/v1/gitlab/projects", strings.NewReader(`{}`))
	req.Header.Set("X-Tenant-ID", "tenant")
	req.Header.Set("X-Actor-ID", "user")
	req.Header.Set("X-Actor-Roles", "product_manager")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("非管理员不得 Onboard GitLab Project，实际 %d", rec.Code)
	}
}
