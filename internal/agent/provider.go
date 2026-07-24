package agent

import (
	"context"
	"encoding/json"
	"time"
)

// Task 是控制面下发给 Coding/Review/Test Agent 的稳定协议。
type Task struct {
	RunID           string            `json:"run_id"`
	TenantID        string            `json:"tenant_id"`
	ProjectID       string            `json:"project_id"`
	ChangeID        string            `json:"change_id"`
	Role            string            `json:"role"`
	RepositoryURL   string            `json:"repository_url"`
	BaseCommit      string            `json:"base_commit"`
	Branch          string            `json:"branch"`
	Instructions    string            `json:"instructions"`
	EngineeringPack json.RawMessage   `json:"engineering_package"`
	AllowedPaths    []string          `json:"allowed_paths"`
	DeniedPaths     []string          `json:"denied_paths"`
	TestCommands    []string          `json:"test_commands"`
	Environment     map[string]string `json:"environment"`
	Deadline        time.Time         `json:"deadline"`
}

// Event 是 Agent 执行的可审计事件；平台不保存模型隐藏思维链。
type Event struct {
	RunID      string          `json:"run_id"`
	Sequence   uint64          `json:"sequence"`
	Type       string          `json:"type"`
	OccurredAt time.Time       `json:"occurred_at"`
	Payload    json.RawMessage `json:"payload"`
}

// Result 是 Agent 的结构化完成结果。
type Result struct {
	RunID          string          `json:"run_id"`
	Status         string          `json:"status"`
	Provider       string          `json:"provider"`
	Model          string          `json:"model"`
	CommitSHA      string          `json:"commit_sha,omitempty"`
	PatchDigest    string          `json:"patch_digest,omitempty"`
	MergeRequestID string          `json:"merge_request_id,omitempty"`
	Summary        string          `json:"summary,omitempty"`
	Artifacts      []string        `json:"artifacts,omitempty"`
	Raw            json.RawMessage `json:"raw,omitempty"`
	StartedAt      time.Time       `json:"started_at"`
	FinishedAt     time.Time       `json:"finished_at"`
}

// Provider 允许 Codex、OpenHands、企业自研 Agent 或 A2A Agent 通过统一协议接入。
type Provider interface {
	Name() string
	Run(ctx context.Context, task Task, onEvent func(Event)) (Result, error)
	Cancel(ctx context.Context, runID string) error
}
