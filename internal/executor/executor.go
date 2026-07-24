package executor

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/william-lbn/DecisionProof/internal/domain"
)

// ErrOutcomeUnknown 表示动作可能已经在外部系统提交，但执行端未得到可确认的最终响应。
// 调用方必须保存 UNKNOWN 状态并通过 Query 对账，禁止盲目重试副作用。
var ErrOutcomeUnknown = errors.New("外部动作结果未知")

// Result 是外部系统返回的可审计回执。
type Result struct {
	ExternalRef string
	Receipt     json.RawMessage
}

// Executor 封装一个受控外部系统适配器。
type Executor interface {
	Execute(ctx context.Context, proposal domain.ActionProposal) (Result, error)
	Query(ctx context.Context, proposal domain.ActionProposal, externalRef string) (Result, domain.ActionStatus, error)
}

// Registry 根据动作意图选择执行器。
type Registry interface {
	Resolve(intent string) (Executor, error)
}
