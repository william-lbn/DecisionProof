package telemetry

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// Metrics 是无外部依赖的最小 Prometheus 文本指标实现。
// 生产环境可替换为 OpenTelemetry SDK；业务代码只依赖这里暴露的窄接口。
type Metrics struct {
	requests atomic.Uint64
	errors   atomic.Uint64
	mu       sync.RWMutex
	counters map[string]uint64
}

func NewMetrics() *Metrics { return &Metrics{counters: map[string]uint64{}} }

func (m *Metrics) IncRequest() { m.requests.Add(1) }
func (m *Metrics) IncError()   { m.errors.Add(1) }

func (m *Metrics) IncNamed(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counters[sanitize(name)]++
}

func (m *Metrics) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintf(w, "# HELP sdlcproof_http_requests_total API 请求总数\n")
	fmt.Fprintf(w, "# TYPE sdlcproof_http_requests_total counter\n")
	fmt.Fprintf(w, "sdlcproof_http_requests_total %d\n", m.requests.Load())
	fmt.Fprintf(w, "# HELP sdlcproof_http_errors_total API 错误总数\n")
	fmt.Fprintf(w, "# TYPE sdlcproof_http_errors_total counter\n")
	fmt.Fprintf(w, "sdlcproof_http_errors_total %d\n", m.errors.Load())

	m.mu.RLock()
	keys := make([]string, 0, len(m.counters))
	for key := range m.counters {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(w, "sdlcproof_%s_total %d\n", key, m.counters[key])
	}
	m.mu.RUnlock()
}

func sanitize(value string) string {
	value = strings.ToLower(value)
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}
