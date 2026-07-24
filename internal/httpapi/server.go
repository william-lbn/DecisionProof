package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/william-lbn/DecisionProof/internal/domain"
	"github.com/william-lbn/DecisionProof/internal/service"
	"github.com/william-lbn/DecisionProof/internal/telemetry"
)

// Server 汇聚平台核心服务并暴露版本化 REST API。
type Server struct {
	changes   *service.ChangeService
	evidence  *service.EvidenceService
	actions   *service.ActionGateway
	releases  *service.ReleaseService
	metrics   *telemetry.Metrics
	readiness func(context.Context) error
}

func NewServer(changes *service.ChangeService, evidence *service.EvidenceService, actions *service.ActionGateway, releases *service.ReleaseService, metrics *telemetry.Metrics, readiness ...func(context.Context) error) http.Handler {
	ready := func(context.Context) error { return nil }
	if len(readiness) > 0 && readiness[0] != nil {
		ready = readiness[0]
	}
	s := &Server{changes: changes, evidence: evidence, actions: actions, releases: releases, metrics: metrics, readiness: ready}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.health)
	mux.HandleFunc("/readyz", s.ready)
	mux.Handle("/metrics", metrics)
	mux.Handle("/v1/", withIdentity(http.HandlerFunc(s.route)))
	return s.instrument(mux)
}

func (s *Server) instrument(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.metrics.IncRequest()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ready 验证运行时依赖；数据库不可用时返回 503，Kubernetes 不会继续导入流量。
func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.readiness(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) route(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/"), "/")
	parts := strings.Split(path, "/")
	if len(parts) == 1 && parts[0] == "changes" && r.Method == http.MethodPost {
		s.createChange(w, r)
		return
	}
	if len(parts) >= 2 && parts[0] == "changes" {
		changeID := parts[1]
		switch {
		case len(parts) == 2 && r.Method == http.MethodGet:
			s.getChange(w, r, changeID)
		case len(parts) == 3 && parts[2] == "evidence" && r.Method == http.MethodPost:
			s.addEvidence(w, r, changeID)
		case len(parts) == 3 && parts[2] == "evidence" && r.Method == http.MethodGet:
			s.listEvidence(w, r, changeID)
		case len(parts) == 4 && parts[2] == "evidence" && parts[3] == "verify" && r.Method == http.MethodPost:
			s.verifyEvidence(w, r, changeID)
		case len(parts) == 3 && parts[2] == "transitions" && r.Method == http.MethodPost:
			s.transition(w, r, changeID)
		case len(parts) == 3 && parts[2] == "manifest" && r.Method == http.MethodPost:
			s.generateManifest(w, r, changeID)
		default:
			writeError(w, http.StatusNotFound, "接口不存在")
		}
		return
	}
	if len(parts) == 1 && parts[0] == "actions" && r.Method == http.MethodPost {
		s.submitAction(w, r)
		return
	}
	if len(parts) >= 2 && parts[0] == "actions" {
		actionID := parts[1]
		switch {
		case len(parts) == 2 && r.Method == http.MethodGet:
			s.getAction(w, r, actionID)
		case len(parts) == 3 && parts[2] == "reconcile" && r.Method == http.MethodPost:
			s.reconcileAction(w, r, actionID)
		default:
			writeError(w, http.StatusNotFound, "接口不存在")
		}
		return
	}
	writeError(w, http.StatusNotFound, "接口不存在")
}

func (s *Server) createChange(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ProjectID   string `json:"project_id"`
		Title       string `json:"title"`
		Description string `json:"description"`
		RiskLevel   string `json:"risk_level"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	identity := identityFromContext(r.Context())
	change, err := s.changes.Create(r.Context(), identity.TenantID, body.ProjectID, body.Title, body.Description, body.RiskLevel)
	respond(w, change, err, http.StatusCreated)
}

func (s *Server) getChange(w http.ResponseWriter, r *http.Request, changeID string) {
	identity := identityFromContext(r.Context())
	change, err := s.changes.Get(r.Context(), identity.TenantID, changeID)
	respond(w, change, err, http.StatusOK)
}

func (s *Server) addEvidence(w http.ResponseWriter, r *http.Request, changeID string) {
	var body struct {
		Type                 domain.EvidenceType    `json:"type"`
		SchemaVersion        string                 `json:"schema_version"`
		Verdict              domain.EvidenceVerdict `json:"verdict"`
		TrustClass           string                 `json:"trust_class"`
		Subject              string                 `json:"subject"`
		SubjectType          string                 `json:"subject_type"`
		SubjectID            string                 `json:"subject_id"`
		SubjectDigest        string                 `json:"subject_digest"`
		SourceSystem         string                 `json:"source_system"`
		SourceURI            string                 `json:"source_uri"`
		ExternalID           string                 `json:"external_id"`
		SupersedesEvidenceID string                 `json:"supersedes_evidence_id"`
		Payload              json.RawMessage        `json:"payload"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	identity := identityFromContext(r.Context())
	node, err := s.evidence.Add(r.Context(), service.AddEvidenceRequest{
		TenantID: identity.TenantID, ChangeID: changeID, Type: body.Type, SchemaVersion: body.SchemaVersion,
		Verdict: body.Verdict, Producer: identity.ActorID, ProducerIdentity: identity.ActorID,
		TrustClass: body.TrustClass, Subject: body.Subject, SubjectType: body.SubjectType,
		SubjectID: body.SubjectID, SubjectDigest: body.SubjectDigest, SourceSystem: body.SourceSystem,
		SourceURI: body.SourceURI, ExternalID: body.ExternalID, SupersedesEvidenceID: body.SupersedesEvidenceID,
		Payload: body.Payload,
	})
	respond(w, node, err, http.StatusCreated)
}

func (s *Server) listEvidence(w http.ResponseWriter, r *http.Request, changeID string) {
	identity := identityFromContext(r.Context())
	nodes, err := s.changes.ListEvidence(r.Context(), identity.TenantID, changeID)
	respond(w, nodes, err, http.StatusOK)
}

func (s *Server) verifyEvidence(w http.ResponseWriter, r *http.Request, changeID string) {
	identity := identityFromContext(r.Context())
	err := s.evidence.VerifyChain(r.Context(), identity.TenantID, changeID)
	respond(w, map[string]any{"verified": err == nil}, err, http.StatusOK)
}

func (s *Server) transition(w http.ResponseWriter, r *http.Request, changeID string) {
	var body struct {
		Target         string `json:"target"`
		IdempotencyKey string `json:"idempotency_key"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	target, err := domain.ParseChangeState(body.Target)
	if err != nil {
		respond(w, nil, err, http.StatusOK)
		return
	}
	identity := identityFromContext(r.Context())
	change, transition, err := s.changes.Transition(r.Context(), identity.TenantID, changeID, identity.ActorID, identity.Roles, target, body.IdempotencyKey)
	respond(w, map[string]any{"change": change, "transition": transition}, err, http.StatusOK)
}

func (s *Server) submitAction(w http.ResponseWriter, r *http.Request) {
	var proposal domain.ActionProposal
	if !decodeJSON(w, r, &proposal) {
		return
	}
	identity := identityFromContext(r.Context())
	proposal.TenantID = identity.TenantID
	proposal.Actor = identity.ActorID
	proposal.ActorRoles = identity.Roles
	record, err := s.actions.Submit(r.Context(), proposal)
	respond(w, record, err, http.StatusAccepted)
}

func (s *Server) getAction(w http.ResponseWriter, r *http.Request, actionID string) {
	identity := identityFromContext(r.Context())
	record, err := s.actions.Get(r.Context(), identity.TenantID, actionID)
	respond(w, record, err, http.StatusOK)
}

func (s *Server) reconcileAction(w http.ResponseWriter, r *http.Request, actionID string) {
	identity := identityFromContext(r.Context())
	record, err := s.actions.Reconcile(r.Context(), identity.TenantID, actionID)
	respond(w, record, err, http.StatusOK)
}

func (s *Server) generateManifest(w http.ResponseWriter, r *http.Request, changeID string) {
	var body struct {
		Environment string `json:"environment"`
		Strategy    string `json:"strategy"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	identity := identityFromContext(r.Context())
	manifest, err := s.releases.GenerateManifest(r.Context(), identity.TenantID, changeID, identity.ActorID, body.Environment, body.Strategy)
	respond(w, manifest, err, http.StatusCreated)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "请求 JSON 无效: "+err.Error())
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, "请求体只能包含一个 JSON 对象")
		return false
	}
	return true
}

func respond(w http.ResponseWriter, value any, err error, successStatus int) {
	if err == nil {
		writeJSON(w, successStatus, value)
		return
	}
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, domain.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, domain.ErrConflict), errors.Is(err, domain.ErrAlreadyExists):
		status = http.StatusConflict
	case errors.Is(err, domain.ErrUnauthorized):
		status = http.StatusForbidden
	case errors.Is(err, domain.ErrInvalidTransition), errors.Is(err, domain.ErrEvidenceInsufficient), errors.Is(err, domain.ErrPolicyDenied):
		status = http.StatusUnprocessableEntity
	default:
		status = http.StatusBadRequest
	}
	writeError(w, status, err.Error())
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": message, "status": status})
}
