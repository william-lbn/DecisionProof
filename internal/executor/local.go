package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/william-lbn/DecisionProof/internal/domain"
)

// LocalExecutor 是可重复、可故障注入的真实进程内执行器。
// 它不模拟 Kubernetes 或 GitHub，只用于验证工具网关的幂等、未知结果和对账语义。
type LocalExecutor struct {
	mu           sync.Mutex
	effects      map[string]Result
	executeCount map[string]int
}

func NewLocalExecutor() *LocalExecutor {
	return &LocalExecutor{effects: map[string]Result{}, executeCount: map[string]int{}}
}

func (e *LocalExecutor) Execute(ctx context.Context, proposal domain.ActionProposal) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	key := proposal.TenantID + "/" + proposal.IdempotencyKey
	e.executeCount[key]++
	if existing, ok := e.effects[key]; ok {
		return existing, nil
	}

	var params map[string]any
	_ = json.Unmarshal(proposal.Parameters, &params)
	result := Result{
		ExternalRef: "local://effect/" + proposal.ID,
		Receipt: mustJSON(map[string]any{
			"intent": proposal.Intent,
			"target": proposal.Target,
			"effect": "committed",
		}),
	}
	// 先落地外部效果，再根据参数注入“提交后响应丢失”，用于验证不可盲重试原则。
	e.effects[key] = result
	if mode, _ := params["simulate"].(string); mode == "commit_then_timeout" {
		return Result{ExternalRef: result.ExternalRef}, ErrOutcomeUnknown
	}
	if mode, _ := params["simulate"].(string); mode == "fail_before_commit" {
		delete(e.effects, key)
		return Result{}, fmt.Errorf("故障注入：提交前失败")
	}
	return result, nil
}

func (e *LocalExecutor) Query(ctx context.Context, proposal domain.ActionProposal, externalRef string) (Result, domain.ActionStatus, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, domain.ActionUnknown, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	key := proposal.TenantID + "/" + proposal.IdempotencyKey
	result, ok := e.effects[key]
	if !ok {
		return Result{}, domain.ActionFailed, nil
	}
	return result, domain.ActionSucceeded, nil
}

func (e *LocalExecutor) Count(tenantID, idempotencyKey string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.executeCount[tenantID+"/"+idempotencyKey]
}

// StaticRegistry 为测试和单执行器部署提供最小注册表。
type StaticRegistry struct{ Executor Executor }

func (r StaticRegistry) Resolve(intent string) (Executor, error) {
	if r.Executor == nil {
		return nil, fmt.Errorf("意图 %s 没有注册执行器", intent)
	}
	return r.Executor, nil
}

func mustJSON(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}
