package gitlab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type WebhookProcessor interface {
	Process(context.Context, InboxRecord) error
}

type Reconciler struct {
	Inbox       InboxStore
	Processor   WebhookProcessor
	Interval    time.Duration
	Lease       time.Duration
	MaxAttempts int
	BatchSize   int
	Now         func() time.Time
	OnError     func(error)
}

// Run 周期执行只读 Provider 对账。Webhook 到达顺序不参与状态推进；
// Worker 崩溃后的 PROCESSING 租约可回收，但达到最大次数后保持 FAILED。
func (r *Reconciler) Run(ctx context.Context) {
	interval := r.Interval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := r.RunOnce(ctx); err != nil && r.OnError != nil {
			r.OnError(err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r *Reconciler) RunOnce(ctx context.Context) error {
	if r.Inbox == nil || r.Processor == nil {
		return errors.New("Webhook Reconciler 未配置 Inbox 或 Processor")
	}
	maxAttempts := r.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	lease := r.Lease
	if lease <= 0 {
		lease = time.Minute
	}
	batch := r.BatchSize
	if batch <= 0 {
		batch = 20
	}
	now := r.now()
	if _, err := r.Inbox.RequeueStale(now.Add(-lease), maxAttempts); err != nil {
		return fmt.Errorf("回收 Webhook 处理租约失败: %w", err)
	}
	for i := 0; i < batch; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		record, claimed, err := r.Inbox.ClaimNext(r.now(), maxAttempts)
		if err != nil {
			return fmt.Errorf("领取 Webhook Inbox 失败: %w", err)
		}
		if !claimed {
			return nil
		}
		processErr := r.Processor.Process(ctx, record)
		record.UpdatedAt = r.now()
		if processErr != nil {
			record.Status = InboxFailed
			record.LastError = safeRuntimeError(processErr)
		} else {
			record.Status = InboxProcessed
			record.LastError = ""
			processedAt := record.UpdatedAt
			record.ProcessedAt = &processedAt
		}
		if err := r.Inbox.Update(record); err != nil {
			return fmt.Errorf("保存 Webhook 对账结果失败: %w", err)
		}
	}
	return nil
}

func (r *Reconciler) now() time.Time {
	if r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}

type WebhookReadAPI interface {
	GetMergeRequest(context.Context, int64, int64) (MergeRequest, error)
	GetPipeline(context.Context, int64, int64) (Pipeline, error)
	GetBranch(context.Context, int64, string) (Branch, error)
}

// GitLabReadbackProcessor 只把 Webhook 当通知，再从 GitLab API 回读 Project/IID/SHA。
// 当前阶段只确认外部事实并更新 Inbox，不直接推进发布或合并状态。
type GitLabReadbackProcessor struct {
	API          WebhookReadAPI
	Integrations RuntimeRepository
}

func (p GitLabReadbackProcessor) Process(ctx context.Context, record InboxRecord) error {
	if p.API == nil || p.Integrations == nil {
		return errors.New("GitLab Read-back Processor 未配置")
	}
	integration, err := p.Integrations.GetIntegration(ctx, record.TenantID, record.IntegrationID)
	if err != nil {
		return fmt.Errorf("读取 Webhook Integration 失败: %w", err)
	}
	if integration.Status != IntegrationActive || integration.ExternalProjectID != record.ProjectID {
		return errors.New("Webhook Project 与 ACTIVE Integration 不匹配")
	}
	var payload struct {
		ProjectID int64 `json:"project_id"`
		Project   struct {
			ID int64 `json:"id"`
		} `json:"project"`
		Ref              string `json:"ref"`
		After            string `json:"after"`
		ObjectAttributes struct {
			ID         int64  `json:"id"`
			IID        int64  `json:"iid"`
			SHA        string `json:"sha"`
			Ref        string `json:"ref"`
			LastCommit struct {
				ID string `json:"id"`
			} `json:"last_commit"`
		} `json:"object_attributes"`
	}
	if err := json.Unmarshal(record.Payload, &payload); err != nil {
		return errors.New("Webhook Payload JSON 无效")
	}
	switch strings.ToLower(strings.TrimSpace(record.EventType)) {
	case "merge request hook":
		if payload.ObjectAttributes.IID <= 0 {
			return errors.New("Merge Request Hook 缺少 IID")
		}
		mr, err := p.API.GetMergeRequest(ctx, record.ProjectID, payload.ObjectAttributes.IID)
		if err != nil {
			return fmt.Errorf("回读 GitLab MR 失败: %w", err)
		}
		expectedSHA := payload.ObjectAttributes.LastCommit.ID
		if expectedSHA != "" && mr.SHA != expectedSHA {
			return errors.New("Webhook MR Head SHA 与 API Read-back 不匹配")
		}
	case "pipeline hook":
		if payload.ObjectAttributes.ID <= 0 || payload.ObjectAttributes.SHA == "" {
			return errors.New("Pipeline Hook 缺少 Pipeline ID 或 SHA")
		}
		pipeline, err := p.API.GetPipeline(ctx, record.ProjectID, payload.ObjectAttributes.ID)
		if err != nil {
			return fmt.Errorf("回读 GitLab Pipeline 失败: %w", err)
		}
		if pipeline.SHA != payload.ObjectAttributes.SHA {
			return errors.New("Webhook Pipeline SHA 与 API Read-back 不匹配")
		}
	case "push hook":
		branchName := strings.TrimPrefix(payload.Ref, "refs/heads/")
		if branchName == "" || branchName == payload.Ref || payload.After == "" {
			return errors.New("Push Hook 缺少 Branch Ref 或 After SHA")
		}
		branch, err := p.API.GetBranch(ctx, record.ProjectID, branchName)
		if err != nil {
			return fmt.Errorf("回读 GitLab Branch 失败: %w", err)
		}
		if branch.Commit.ID != payload.After {
			return errors.New("Webhook Push SHA 与 API Read-back 不匹配")
		}
	default:
		return fmt.Errorf("暂不支持的 GitLab Webhook 类型 %q", record.EventType)
	}
	return nil
}
