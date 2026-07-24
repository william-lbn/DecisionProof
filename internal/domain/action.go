package domain

import (
	"encoding/json"
	"time"
)

// ActionStatus 描述一次外部副作用的结算状态。
// UNKNOWN 不是失败，它表示“外部世界可能已经提交，但平台尚未获得可靠确认”，必须进入对账流程。
type ActionStatus string

const (
	ActionPending   ActionStatus = "PENDING"
	ActionExecuting ActionStatus = "EXECUTING"
	ActionSucceeded ActionStatus = "SUCCEEDED"
	ActionFailed    ActionStatus = "FAILED"
	ActionUnknown   ActionStatus = "UNKNOWN"
)

// ActionProposal 是 Agent 或人提交给可信工具网关的动作建议。
type ActionProposal struct {
	ID                 string          `json:"id"`
	TenantID           string          `json:"tenant_id"`
	ChangeID           string          `json:"change_id"`
	Actor              string          `json:"actor"`
	ActorRoles         []string        `json:"actor_roles"`
	Intent             string          `json:"intent"`
	Target             string          `json:"target"`
	RiskLevel          string          `json:"risk_level"`
	Parameters         json.RawMessage `json:"parameters"`
	ParametersDigest   string          `json:"parameters_digest"`
	IdempotencyKey     string          `json:"idempotency_key"`
	ExpectedEffects    []string        `json:"expected_effects"`
	ForbiddenEffects   []string        `json:"forbidden_effects"`
	Preconditions      []string        `json:"preconditions"`
	RequiresHumanCheck bool            `json:"requires_human_check"`
}

// ActionRecord 保存动作的结算信息与外部系统回执。
type ActionRecord struct {
	Proposal       ActionProposal  `json:"proposal"`
	Status         ActionStatus    `json:"status"`
	Version        uint64          `json:"version"`
	ExternalRef    string          `json:"external_ref,omitempty"`
	Receipt        json.RawMessage `json:"receipt,omitempty"`
	ErrorMessage   string          `json:"error_message,omitempty"`
	PolicyDigest   string          `json:"policy_digest"`
	AttemptCount   int             `json:"attempt_count"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
	LastCheckedAt  *time.Time      `json:"last_checked_at,omitempty"`
	SettlementNote string          `json:"settlement_note,omitempty"`
}
