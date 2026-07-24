package assurance

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"
)

// MetricProvider 对接 Prometheus、数据仓库或业务指标服务。
type MetricProvider interface {
	Query(ctx context.Context, query string, at time.Time) (MetricValue, error)
}
type MetricValue struct {
	Value  float64
	NoData bool
}

type Comparator string

const (
	LessOrEqual    Comparator = "LE"
	GreaterOrEqual Comparator = "GE"
	Equal          Comparator = "EQ"
)

type OutcomeMetric struct {
	Name, Query string
	Comparator  Comparator
	Threshold   float64
	Required    bool
}
type OutcomeContract struct {
	ID              string
	ObservationTime time.Time
	Metrics         []OutcomeMetric
}
type MetricVerdict struct {
	Name       string
	Value      float64
	Threshold  float64
	Comparator Comparator
	Verdict    string
	Reason     string
}
type OutcomeResult struct {
	Status  string
	Metrics []MetricVerdict
}

// EvaluateOutcome 只在全部必需指标有数据且通过时返回 VERIFIED。
// 任一必需指标无数据返回 INCONCLUSIVE；任一指标失败返回 FAILED。
func EvaluateOutcome(ctx context.Context, p MetricProvider, contract OutcomeContract) (OutcomeResult, error) {
	if p == nil {
		return OutcomeResult{}, errors.New("MetricProvider 不能为空")
	}
	if len(contract.Metrics) == 0 {
		return OutcomeResult{}, errors.New("OutcomeContract 没有指标")
	}
	result := OutcomeResult{Status: "VERIFIED"}
	for _, m := range contract.Metrics {
		v, err := p.Query(ctx, m.Query, contract.ObservationTime)
		if err != nil {
			return OutcomeResult{}, fmt.Errorf("查询指标 %s 失败: %w", m.Name, err)
		}
		verdict := MetricVerdict{Name: m.Name, Value: v.Value, Threshold: m.Threshold, Comparator: m.Comparator, Verdict: "PASS"}
		if v.NoData {
			verdict.Verdict = "NO_DATA"
			verdict.Reason = "指标没有数据"
			if m.Required && result.Status != "FAILED" {
				result.Status = "INCONCLUSIVE"
			}
		} else if !compare(v.Value, m.Comparator, m.Threshold) {
			verdict.Verdict = "FAIL"
			verdict.Reason = "指标超过契约边界"
			result.Status = "FAILED"
		}
		result.Metrics = append(result.Metrics, verdict)
	}
	return result, nil
}
func compare(v float64, c Comparator, t float64) bool {
	switch c {
	case LessOrEqual:
		return v <= t
	case GreaterOrEqual:
		return v >= t
	case Equal:
		return math.Abs(v-t) < 1e-9
	default:
		return false
	}
}
