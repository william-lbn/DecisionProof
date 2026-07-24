package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// CLIProvider 通过标准输入向 Codex CLI、OpenHands Agent Server 包装器或企业内部 Agent 下发 Task JSON。
// Agent 应把事件以 NDJSON 写入 stderr，把最终 Result JSON 写入 stdout。
type CLIProvider struct {
	ProviderName string
	ModelName    string
	Command      string
	Args         []string
	WorkDir      string
	ExtraEnv     map[string]string
	Timeout      time.Duration
	mu           sync.Mutex
	cancel       map[string]context.CancelFunc
}

func NewCLIProvider(name, command string, args ...string) *CLIProvider {
	return &CLIProvider{ProviderName: name, Command: command, Args: args, Timeout: 2 * time.Hour, cancel: map[string]context.CancelFunc{}}
}
func (p *CLIProvider) Name() string {
	if p.ProviderName == "" {
		return "cli-agent"
	}
	return p.ProviderName
}
func (p *CLIProvider) Run(ctx context.Context, task Task, onEvent func(Event)) (Result, error) {
	if strings.TrimSpace(task.RunID) == "" || strings.TrimSpace(p.Command) == "" {
		return Result{}, errors.New("run_id 和 command 不能为空")
	}
	deadline := p.Timeout
	if deadline <= 0 {
		deadline = 2 * time.Hour
	}
	runCtx, cancel := context.WithTimeout(ctx, deadline)
	p.mu.Lock()
	p.cancel[task.RunID] = cancel
	p.mu.Unlock()
	defer func() { cancel(); p.mu.Lock(); delete(p.cancel, task.RunID); p.mu.Unlock() }()
	payload, err := json.Marshal(task)
	if err != nil {
		return Result{}, err
	}
	cmd := exec.CommandContext(runCtx, p.Command, p.Args...)
	configureProcessGroup(cmd)
	cmd.Dir = p.WorkDir
	cmd.Env = os.Environ()
	for k, v := range p.ExtraEnv {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.Stdin = bytes.NewReader(payload)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Result{}, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return Result{}, err
	}
	started := time.Now().UTC()
	if err := cmd.Start(); err != nil {
		return Result{}, fmt.Errorf("启动 Agent CLI 失败: %w", err)
	}
	processDone := make(chan struct{})
	defer close(processDone)
	go func() {
		select {
		case <-runCtx.Done():
			// CommandContext 只能保证终止直接子进程；这里额外清理进程组，
			// 防止 shell 启动的编译器、测试或 Agent 子进程继续持有管道和凭据。
			killProcessTree(cmd)
		case <-processDone:
		}
	}()
	// stdout 和 stderr 必须并发排空，并且在 Wait 之前完成读取。
	// 否则 Agent 输出较多时可能阻塞，或 Wait 提前关闭管道造成 file already closed。
	type outputRead struct {
		data []byte
		err  error
	}
	stdoutResult := make(chan outputRead, 1)
	eventErr := make(chan error, 1)
	go func() {
		out, readErr := io.ReadAll(io.LimitReader(stdout, 16<<20))
		stdoutResult <- outputRead{data: out, err: readErr}
	}()
	go func() { eventErr <- scanEvents(stderr, task.RunID, onEvent) }()
	readResult := <-stdoutResult
	scanErr := <-eventErr
	waitErr := cmd.Wait()
	out, readErr := readResult.data, readResult.err
	if readErr != nil {
		return Result{}, readErr
	}
	if scanErr != nil {
		return Result{}, scanErr
	}
	if waitErr != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return Result{}, fmt.Errorf("Agent 执行超时: %w", runCtx.Err())
		}
		return Result{}, fmt.Errorf("Agent CLI 失败: %w", waitErr)
	}
	var result Result
	dec := json.NewDecoder(bytes.NewReader(out))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&result); err != nil {
		return Result{}, fmt.Errorf("Agent 最终输出不是合法 Result JSON: %w; stdout=%s", err, safe(out))
	}
	if result.RunID == "" {
		result.RunID = task.RunID
	}
	if result.Provider == "" {
		result.Provider = p.Name()
	}
	if result.Model == "" {
		result.Model = p.ModelName
	}
	if result.StartedAt.IsZero() {
		result.StartedAt = started
	}
	if result.FinishedAt.IsZero() {
		result.FinishedAt = time.Now().UTC()
	}
	return result, nil
}
func (p *CLIProvider) Cancel(ctx context.Context, runID string) error {
	_ = ctx
	p.mu.Lock()
	cancel, ok := p.cancel[runID]
	p.mu.Unlock()
	if !ok {
		return errors.New("Agent Run 不存在或已结束")
	}
	cancel()
	return nil
}
func scanEvents(r io.Reader, runID string, onEvent func(Event)) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 4<<20)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var event Event
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}
		if event.RunID == "" {
			event.RunID = runID
		}
		if event.OccurredAt.IsZero() {
			event.OccurredAt = time.Now().UTC()
		}
		if onEvent != nil {
			onEvent(event)
		}
	}
	return scanner.Err()
}
func safe(v []byte) string {
	s := strings.TrimSpace(string(v))
	if len(s) > 2048 {
		s = s[:2048] + "…"
	}
	return s
}

// WriteTaskFile 为需要文件输入的 Agent Runtime 生成权限收紧的临时任务文件。
func WriteTaskFile(dir string, task Task) (string, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, task.RunID+".json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}
