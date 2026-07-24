package gitlab

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

type fixedWebhookProcessor struct {
	calls int
	err   error
}

func (p *fixedWebhookProcessor) Process(context.Context, InboxRecord) error {
	p.calls++
	return p.err
}

func TestReconcilerClaimsAndProcessesOnce(t *testing.T) {
	inbox := NewMemoryInbox()
	now := time.Unix(100, 0).UTC()
	record := InboxRecord{
		ID: "wh-1", TenantID: "tenant", IntegrationID: "gitlab", EventUUID: "event-1",
		EventType: "Push Hook", Payload: json.RawMessage(`{}`), Status: InboxPending,
		ReceivedAt: now, UpdatedAt: now,
	}
	if stored, _, err := inbox.PutIfAbsent(record); err != nil || !stored {
		t.Fatal(err)
	}
	processor := &fixedWebhookProcessor{}
	reconciler := &Reconciler{
		Inbox: inbox, Processor: processor, Now: func() time.Time { return now.Add(time.Second) },
		MaxAttempts: 3,
	}
	if err := reconciler.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	processed, err := inbox.Get(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if processed.Status != InboxProcessed || processed.Attempts != 1 || processor.calls != 1 {
		t.Fatalf("Webhook 未被原子处理一次: record=%+v calls=%d", processed, processor.calls)
	}
	if err := reconciler.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if processor.calls != 1 {
		t.Fatalf("PROCESSED 事件不应重复处理，calls=%d", processor.calls)
	}
}

func TestReconcilerRequeuesStaleLeaseButHonorsAttemptLimit(t *testing.T) {
	inbox := NewMemoryInbox()
	now := time.Unix(200, 0).UTC()
	record := InboxRecord{
		ID: "wh-stale", TenantID: "tenant", IntegrationID: "gitlab", EventUUID: "event-stale",
		Status: InboxProcessing, Attempts: 3, ReceivedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour),
	}
	if stored, _, err := inbox.PutIfAbsent(record); err != nil || !stored {
		t.Fatal(err)
	}
	processor := &fixedWebhookProcessor{}
	reconciler := &Reconciler{
		Inbox: inbox, Processor: processor, Now: func() time.Time { return now },
		Lease: time.Minute, MaxAttempts: 3,
	}
	if err := reconciler.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	failed, err := inbox.Get(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != InboxFailed || processor.calls != 0 {
		t.Fatalf("达到重试上限的租约不得再次领取: record=%+v calls=%d", failed, processor.calls)
	}
}

type fixedReadbackAPI struct {
	mr       MergeRequest
	pipeline Pipeline
	branch   Branch
}

func (f fixedReadbackAPI) GetMergeRequest(context.Context, int64, int64) (MergeRequest, error) {
	return f.mr, nil
}
func (f fixedReadbackAPI) GetPipeline(context.Context, int64, int64) (Pipeline, error) {
	return f.pipeline, nil
}
func (f fixedReadbackAPI) GetBranch(context.Context, int64, string) (Branch, error) {
	return f.branch, nil
}

func TestGitLabReadbackProcessorRejectsWebhookSHAMismatch(t *testing.T) {
	integrations := NewMemoryRuntimeRepository()
	_, _ = integrations.SaveIntegration(context.Background(), ProjectIntegration{
		TenantID: "tenant", IntegrationID: "gitlab", ExternalProjectID: 7, Status: IntegrationActive,
	})
	processor := GitLabReadbackProcessor{
		API:          fixedReadbackAPI{mr: MergeRequest{IID: 3, SHA: "api-sha"}},
		Integrations: integrations,
	}
	record := InboxRecord{
		TenantID: "tenant", IntegrationID: "gitlab", ProjectID: 7, EventType: "Merge Request Hook",
		Payload: json.RawMessage(`{"project":{"id":7},"object_attributes":{"iid":3,"last_commit":{"id":"webhook-sha"}}}`),
	}
	if err := processor.Process(context.Background(), record); err == nil {
		t.Fatal("Webhook SHA 与 API 不一致时必须失败")
	}
}
