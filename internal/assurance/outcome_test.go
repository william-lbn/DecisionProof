package assurance

import (
	"context"
	"testing"
	"time"
)

type metrics map[string]MetricValue

func (m metrics) Query(ctx context.Context, q string, at time.Time) (MetricValue, error) {
	return m[q], nil
}
func TestOutcomeNoDataIsInconclusive(t *testing.T) {
	r, err := EvaluateOutcome(context.Background(), metrics{"error": {NoData: true}}, OutcomeContract{Metrics: []OutcomeMetric{{Name: "error", Query: "error", Comparator: LessOrEqual, Threshold: .01, Required: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "INCONCLUSIVE" {
		t.Fatal(r)
	}
}
func TestOutcomeFailure(t *testing.T) {
	r, err := EvaluateOutcome(context.Background(), metrics{"error": {Value: .2}}, OutcomeContract{Metrics: []OutcomeMetric{{Name: "error", Query: "error", Comparator: LessOrEqual, Threshold: .01, Required: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "FAILED" {
		t.Fatal(r)
	}
}
