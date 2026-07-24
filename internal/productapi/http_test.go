package productapi

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/william-lbn/DecisionProof/internal/ai"
	"github.com/william-lbn/DecisionProof/internal/product"
)

func TestProductAPIEndToEnd(t *testing.T) {
	repo := product.NewMemoryRepository()
	service := product.NewService(repo, product.RealClock{})
	handler := NewHTTPHandler(service, t.TempDir(), ai.HeuristicExtractor{})

	document := uploadMarkdown(t, handler, "product-manager", `# 发布平台
需求：平台必须阻止未验证的生产发布。
验收标准：缺少签名时发布请求必须失败。
业务结果指标：高风险发布失败率低于 1%。`)
	documentID, _ := document["id"].(string)
	if documentID == "" {
		t.Fatalf("未返回文档 ID: %#v", document)
	}

	extractReq := httptest.NewRequest(http.MethodPost, "/product/v1/documents/"+documentID+"/extract", nil)
	setIdentity(extractReq, "product-manager")
	extractRes := httptest.NewRecorder()
	handler.ServeHTTP(extractRes, extractReq)
	if extractRes.Code != http.StatusCreated {
		t.Fatalf("提取失败: %d %s", extractRes.Code, extractRes.Body.String())
	}
	var extracted struct {
		Decisions []product.ProductDecision `json:"decisions"`
	}
	if err := json.Unmarshal(extractRes.Body.Bytes(), &extracted); err != nil {
		t.Fatal(err)
	}
	if len(extracted.Decisions) < 3 {
		t.Fatalf("提取决策不足: %#v", extracted.Decisions)
	}

	for _, item := range extracted.Decisions {
		confirmed := callDecisionTransition(t, handler, item.ID, "confirm", "product-manager", item.Version, "")

		// 同一操作者不能同时确认和正式批准。
		body, _ := json.Marshal(map[string]any{"version": confirmed.Version})
		req := httptest.NewRequest(http.MethodPost, "/product/v1/decisions/"+item.ID+"/approve", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		setIdentity(req, "product-manager")
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code == http.StatusOK {
			t.Fatal("同一操作者不应同时确认和批准")
		}

		approved := callDecisionTransition(t, handler, item.ID, "approve", "product-owner", confirmed.Version, "")
		if approved.Status != product.DecisionApproved {
			t.Fatalf("批准状态错误: %#v", approved)
		}
	}

	packageBody, _ := json.Marshal(map[string]any{"project_id": "project-demo"})
	packageReq := httptest.NewRequest(http.MethodPost, "/product/v1/engineering-packages", bytes.NewReader(packageBody))
	packageReq.Header.Set("Content-Type", "application/json")
	setIdentity(packageReq, "product-owner")
	packageRes := httptest.NewRecorder()
	handler.ServeHTTP(packageRes, packageReq)
	if packageRes.Code != http.StatusCreated {
		t.Fatalf("工程就绪包创建失败: %d %s", packageRes.Code, packageRes.Body.String())
	}
	var pkg product.EngineeringReadinessPackage
	if err := json.Unmarshal(packageRes.Body.Bytes(), &pkg); err != nil {
		t.Fatal(err)
	}
	if pkg.Digest == "" || len(pkg.RequirementIDs) == 0 || len(pkg.AcceptanceIDs) == 0 || len(pkg.OutcomeContractIDs) == 0 {
		t.Fatalf("工程就绪包内容不完整: %#v", pkg)
	}
}

func TestManualDecisionReviseAndReject(t *testing.T) {
	repo := product.NewMemoryRepository()
	handler := NewHTTPHandler(product.NewService(repo, product.RealClock{}), t.TempDir())
	body, _ := json.Marshal(map[string]any{
		"project_id": "project-demo", "kind": "RISK", "title": "权限风险", "content": "Agent 可能尝试越权。",
	})
	req := httptest.NewRequest(http.MethodPost, "/product/v1/decisions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	setIdentity(req, "product-manager")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusCreated {
		t.Fatalf("人工决策创建失败: %d %s", res.Code, res.Body.String())
	}
	var created product.ProductDecision
	if err := json.Unmarshal(res.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Origin != product.DecisionOriginManual || created.Status != product.DecisionConfirmed {
		t.Fatalf("人工决策状态错误: %#v", created)
	}

	reviseBody, _ := json.Marshal(map[string]any{"version": created.Version, "title": "生产权限风险", "content": "Agent 不得持有生产集群写权限。"})
	reviseReq := httptest.NewRequest(http.MethodPatch, "/product/v1/decisions/"+created.ID, bytes.NewReader(reviseBody))
	reviseReq.Header.Set("Content-Type", "application/json")
	setIdentity(reviseReq, "security-reviewer")
	reviseRes := httptest.NewRecorder()
	handler.ServeHTTP(reviseRes, reviseReq)
	if reviseRes.Code != http.StatusOK {
		t.Fatalf("决策修订失败: %d %s", reviseRes.Code, reviseRes.Body.String())
	}
	var revised product.ProductDecision
	_ = json.Unmarshal(reviseRes.Body.Bytes(), &revised)

	rejected := callDecisionTransition(t, handler, revised.ID, "reject", "product-owner", revised.Version, "风险描述仍不够可验证")
	if rejected.Status != product.DecisionRejected || rejected.RejectionReason == "" {
		t.Fatalf("否决审计不完整: %#v", rejected)
	}
}

func uploadMarkdown(t *testing.T, handler http.Handler, actor, content string) map[string]any {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("project_id", "project-demo")
	part, err := writer.CreateFormFile("file", filepath.Base("立项资料.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte(content))
	_ = writer.Close()
	req := httptest.NewRequest(http.MethodPost, "/product/v1/documents", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	setIdentity(req, actor)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusCreated {
		t.Fatalf("上传失败: %d %s", res.Code, res.Body.String())
	}
	var result map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func callDecisionTransition(t *testing.T, handler http.Handler, id, operation, actor string, version int, reason string) product.ProductDecision {
	t.Helper()
	payload := map[string]any{"version": version}
	if reason != "" {
		payload["reason"] = reason
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/product/v1/decisions/"+id+"/"+operation, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	setIdentity(req, actor)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("%s 失败: %d %s", operation, res.Code, res.Body.String())
	}
	var result product.ProductDecision
	if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func setIdentity(req *http.Request, actor string) {
	req.Header.Set("X-Tenant-ID", "tenant-demo")
	req.Header.Set("X-Actor-ID", actor)
	switch actor {
	case "product-manager", "security-reviewer":
		req.Header.Set("X-Actor-Roles", "product_manager")
	case "product-owner":
		req.Header.Set("X-Actor-Roles", "product_approver,delivery_manager")
	default:
		req.Header.Set("X-Actor-Roles", "auditor")
	}
}

func TestProductAPIRBACFailsClosed(t *testing.T) {
	handler := NewHTTPHandler(product.NewService(product.NewMemoryRepository(), product.RealClock{}), t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/product/v1/decisions?project_id=project-demo", nil)
	req.Header.Set("X-Tenant-ID", "tenant-demo")
	req.Header.Set("X-Actor-ID", "unprivileged-user")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("无角色身份必须返回 403，实际 %d: %s", rec.Code, rec.Body.String())
	}
}
