package telemetry

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMetricsPrometheusOutput(t *testing.T) {
	m := NewMetrics()
	m.IncRequest()
	m.IncError()
	m.IncNamed("GitLab Webhook")
	rr := httptest.NewRecorder()
	m.ServeHTTP(rr, httptest.NewRequest("GET", "/metrics", nil))
	body := rr.Body.String()
	for _, expected := range []string{
		"sdlcproof_http_requests_total 1",
		"sdlcproof_http_errors_total 1",
		"sdlcproof_gitlab_webhook_total 1",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("指标输出缺少 %q: %s", expected, body)
		}
	}
	if sanitize("A-B/C") != "a_b_c" {
		t.Fatalf("指标名称清洗不正确: %s", sanitize("A-B/C"))
	}
}
