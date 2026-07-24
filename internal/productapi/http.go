package productapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/william-lbn/DecisionProof/internal/ai"
	"github.com/william-lbn/DecisionProof/internal/delivery"
	"github.com/william-lbn/DecisionProof/internal/document"
	"github.com/william-lbn/DecisionProof/internal/product"
)

// HTTPHandler 提供产品资料输入、AI 提取、人工确认和工程就绪包 API。
type HTTPHandler struct {
	service   *product.Service
	objectDir string
	maxUpload int64
	extractor product.Extractor
}

func NewHTTPHandler(service *product.Service, objectDir string, extractor ...product.Extractor) http.Handler {
	_ = os.MkdirAll(objectDir, 0o750)
	selected := product.Extractor(ai.HeuristicExtractor{})
	if len(extractor) > 0 && extractor[0] != nil {
		selected = extractor[0]
	}
	return &HTTPHandler{service: service, objectDir: objectDir, maxUpload: 50 << 20, extractor: selected}
}

func (h *HTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/product/v1/"), "/")
	parts := strings.Split(path, "/")
	switch {
	case path == "documents" && r.Method == http.MethodPost:
		h.upload(w, r)
	case len(parts) == 3 && parts[0] == "documents" && parts[2] == "extract" && r.Method == http.MethodPost:
		h.extract(w, r, parts[1])
	case path == "decisions" && r.Method == http.MethodGet:
		h.listDecisions(w, r)
	case path == "decisions" && r.Method == http.MethodPost:
		h.createDecision(w, r)
	case len(parts) == 2 && parts[0] == "decisions" && r.Method == http.MethodPatch:
		h.reviseDecision(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "decisions" && parts[2] == "confirm" && r.Method == http.MethodPost:
		h.confirm(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "decisions" && parts[2] == "approve" && r.Method == http.MethodPost:
		h.approve(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "decisions" && parts[2] == "reject" && r.Method == http.MethodPost:
		h.reject(w, r, parts[1])
	case path == "engineering-packages" && r.Method == http.MethodPost:
		h.packageReady(w, r)
	case len(parts) == 3 && parts[0] == "engineering-packages" && parts[2] == "gitlab-bundle" && r.Method == http.MethodGet:
		h.gitLabBundle(w, r, parts[1])
	default:
		writeJSON(w, http.StatusNotFound, map[string]any{"code": "NOT_FOUND", "message": "产品 API 不存在"})
	}
}

func identity(r *http.Request) (string, string, error) {
	tenant := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
	actor := strings.TrimSpace(r.Header.Get("X-Actor-ID"))
	if tenant == "" || actor == "" {
		return "", "", errors.New("缺少可信身份头 X-Tenant-ID 或 X-Actor-ID")
	}
	return tenant, actor, nil
}
func (h *HTTPHandler) upload(w http.ResponseWriter, r *http.Request) {
	tenant, actor, err := identity(r)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, h.maxUpload)
	if err := r.ParseMultipartForm(h.maxUpload); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("解析上传表单失败: %w", err))
		return
	}
	projectID := strings.TrimSpace(r.FormValue("project_id"))
	file, header, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("必须上传 file 字段"))
		return
	}
	defer file.Close()
	stored, err := h.storeUpload(file, header)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	blocks, digest, mediaType, err := document.ExtractFile(stored)
	if err != nil {
		_ = os.Remove(stored)
		writeErr(w, http.StatusUnprocessableEntity, err)
		return
	}
	doc, err := h.service.RegisterDocument(r.Context(), tenant, projectID, header.Filename, mediaType, "file://"+stored, digest, actor)
	if err != nil {
		_ = os.Remove(stored)
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	doc, err = h.service.SaveParsedBlocks(r.Context(), tenant, doc.ID, blocks)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, doc)
}
func (h *HTTPHandler) storeUpload(file multipart.File, header *multipart.FileHeader) (string, error) {
	name := filepath.Base(header.Filename)
	if name == "." || name == "" {
		return "", errors.New("上传文件名无效")
	}
	tmp, err := os.CreateTemp(h.objectDir, "upload-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	committed := false
	defer func() {
		_ = tmp.Close()
		if !committed {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err = io.Copy(tmp, file); err != nil {
		return "", err
	}
	if err = tmp.Sync(); err != nil {
		return "", err
	}
	if err = tmp.Close(); err != nil {
		return "", err
	}
	final := tmpName + filepath.Ext(name)
	if err = os.Rename(tmpName, final); err != nil {
		return "", err
	}
	committed = true
	return final, nil
}
func (h *HTTPHandler) extract(w http.ResponseWriter, r *http.Request, documentID string) {
	tenant, _, err := identity(r)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, err)
		return
	}
	run, decisions, err := h.service.RunExtraction(r.Context(), tenant, documentID, h.extractor)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"run": run, "decisions": decisions})
}
func (h *HTTPHandler) createDecision(w http.ResponseWriter, r *http.Request) {
	tenant, actor, err := identity(r)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, err)
		return
	}
	var body struct {
		ProjectID string               `json:"project_id"`
		Kind      product.DecisionKind `json:"kind"`
		Title     string               `json:"title"`
		Content   string               `json:"content"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	v, err := h.service.CreateManualDecision(r.Context(), tenant, body.ProjectID, body.Kind, body.Title, body.Content, actor)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

func (h *HTTPHandler) reviseDecision(w http.ResponseWriter, r *http.Request, id string) {
	tenant, actor, err := identity(r)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, err)
		return
	}
	var body struct {
		Title   string `json:"title"`
		Content string `json:"content"`
		Version int    `json:"version"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	v, err := h.service.ReviseDecision(r.Context(), tenant, id, body.Title, body.Content, actor, body.Version)
	if err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (h *HTTPHandler) listDecisions(w http.ResponseWriter, r *http.Request) {
	tenant, _, err := identity(r)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, err)
		return
	}
	items, err := h.service.ListDecisions(r.Context(), tenant, r.URL.Query().Get("project_id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}
func (h *HTTPHandler) confirm(w http.ResponseWriter, r *http.Request, id string) {
	tenant, actor, err := identity(r)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, err)
		return
	}
	version, err := parseVersion(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	v, err := h.service.ConfirmDecision(r.Context(), tenant, id, actor, version)
	if err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (h *HTTPHandler) approve(w http.ResponseWriter, r *http.Request, id string) {
	tenant, actor, err := identity(r)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, err)
		return
	}
	version, err := parseVersion(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	v, err := h.service.ApproveDecision(r.Context(), tenant, id, actor, version)
	if err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (h *HTTPHandler) reject(w http.ResponseWriter, r *http.Request, id string) {
	tenant, actor, err := identity(r)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, err)
		return
	}
	var body struct {
		Version int    `json:"version"`
		Reason  string `json:"reason"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	v, err := h.service.RejectDecision(r.Context(), tenant, id, actor, body.Reason, body.Version)
	if err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (h *HTTPHandler) packageReady(w http.ResponseWriter, r *http.Request) {
	tenant, actor, err := identity(r)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, err)
		return
	}
	var body struct {
		ProjectID string `json:"project_id"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	v, err := h.service.BuildEngineeringPackage(r.Context(), tenant, body.ProjectID, actor)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}
func decodeJSONBody(r *http.Request, target any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return fmt.Errorf("请求 JSON 无效: %w", err)
	}
	return nil
}

func (h *HTTPHandler) gitLabBundle(w http.ResponseWriter, r *http.Request, packageID string) {
	tenant, _, err := identity(r)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, err)
		return
	}
	pkg, err := h.service.GetEngineeringPackage(r.Context(), tenant, packageID)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	decisions, err := h.service.GetPackageDecisions(r.Context(), tenant, pkg)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	bundle, err := delivery.RenderEngineeringBundle(pkg, decisions)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err)
		return
	}
	writeJSON(w, http.StatusOK, bundle)
}

func parseVersion(r *http.Request) (int, error) {
	var body struct {
		Version int `json:"version"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		return 0, err
	}
	if body.Version <= 0 {
		return 0, errors.New("version 必须大于 0")
	}
	return body.Version, nil
}
func writeErr(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]any{"code": "PRODUCT_API_ERROR", "message": err.Error(), "status": status})
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
