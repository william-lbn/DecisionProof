package gitlab

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

type RuntimeHTTPHandler struct {
	Service *RuntimeService
}

func (h RuntimeHTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/integrations/v1/gitlab/projects" && r.Method == http.MethodPost:
		if !requireRuntimeRole(w, r, "integration_admin", "platform_admin") {
			return
		}
		h.onboard(w, r)
	case r.URL.Path == "/integrations/v1/gitlab/deliveries" && r.Method == http.MethodPost:
		if !requireRuntimeRole(w, r, "delivery_manager") {
			return
		}
		h.deliver(w, r)
	default:
		writeRuntimeJSON(w, http.StatusNotFound, map[string]string{"code": "NOT_FOUND", "message": "GitLab Runtime API 不存在"})
	}
}

func (h RuntimeHTTPHandler) onboard(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IntegrationID     string `json:"integration_id"`
		ProjectID         string `json:"project_id"`
		ExternalProjectID int64  `json:"external_project_id"`
	}
	if !decodeRuntimeJSON(w, r, &body) {
		return
	}
	tenantID, actorID := runtimeIdentity(r)
	result, err := h.Service.Onboard(r.Context(), OnboardInput{
		TenantID: tenantID, ActorID: actorID, IntegrationID: body.IntegrationID,
		ProjectID: body.ProjectID, ExternalProjectID: body.ExternalProjectID,
	})
	if err != nil {
		writeRuntimeJSON(w, http.StatusBadGateway, map[string]string{"code": "GITLAB_ONBOARD_FAILED", "message": safeRuntimeError(err)})
		return
	}
	writeRuntimeJSON(w, http.StatusCreated, result)
}

func (h RuntimeHTTPHandler) deliver(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IntegrationID  string `json:"integration_id"`
		PackageID      string `json:"package_id"`
		IdempotencyKey string `json:"idempotency_key"`
	}
	if !decodeRuntimeJSON(w, r, &body) {
		return
	}
	tenantID, actorID := runtimeIdentity(r)
	run, err := h.Service.Deliver(r.Context(), DeliverInput{
		TenantID: tenantID, ActorID: actorID, IntegrationID: body.IntegrationID,
		PackageID: body.PackageID, IdempotencyKey: body.IdempotencyKey,
	})
	if err != nil {
		status := http.StatusBadGateway
		code := "GITLAB_DELIVERY_FAILED"
		if run.Status == DeliveryUnknown || run.Status == DeliveryExecuting {
			status, code = http.StatusConflict, "GITLAB_DELIVERY_REQUIRES_RECONCILIATION"
		} else if errors.Is(err, ErrDeliveryConflict) {
			status, code = http.StatusConflict, "GITLAB_DELIVERY_CONFLICT"
		}
		writeRuntimeJSON(w, status, map[string]any{"code": code, "message": safeRuntimeError(err), "delivery": run})
		return
	}
	writeRuntimeJSON(w, http.StatusCreated, run)
}

func requireRuntimeRole(w http.ResponseWriter, r *http.Request, required ...string) bool {
	tenantID, actorID := runtimeIdentity(r)
	if tenantID == "" || actorID == "" {
		writeRuntimeJSON(w, http.StatusUnauthorized, map[string]string{"code": "UNAUTHENTICATED", "message": "缺少可信身份"})
		return false
	}
	granted := map[string]struct{}{}
	for _, role := range strings.Split(r.Header.Get("X-Actor-Roles"), ",") {
		if role = strings.TrimSpace(role); role != "" {
			granted[role] = struct{}{}
		}
	}
	for _, role := range required {
		if _, ok := granted[role]; ok {
			return true
		}
	}
	writeRuntimeJSON(w, http.StatusForbidden, map[string]string{
		"code": "FORBIDDEN", "message": "当前身份缺少所需角色 " + strings.Join(required, " 或 "),
	})
	return false
}

func runtimeIdentity(r *http.Request) (string, string) {
	return strings.TrimSpace(r.Header.Get("X-Tenant-ID")), strings.TrimSpace(r.Header.Get("X-Actor-ID"))
}

func decodeRuntimeJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		writeRuntimeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_JSON", "message": "请求 JSON 无效"})
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeRuntimeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_JSON", "message": "请求只能包含一个 JSON 对象"})
		return false
	}
	return true
}

func writeRuntimeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
