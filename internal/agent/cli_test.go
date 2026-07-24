package agent

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestCLIProviderStructuredProtocol(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("测试依赖 POSIX shell")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "agent.sh")
	content := `#!/bin/sh
cat >/dev/null
echo '{"run_id":"","sequence":1,"type":"tool.completed","payload":{"exit_code":0}}' >&2
echo '{"run_id":"run-1","status":"SUCCEEDED","commit_sha":"abc123","patch_digest":"sha256:patch","summary":"完成"}'
`
	if err := os.WriteFile(script, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	provider := NewCLIProvider("test-agent", script)
	provider.ModelName = "test-model"
	var events []Event
	result, err := provider.Run(context.Background(), Task{RunID: "run-1", ProjectID: "p1"}, func(event Event) { events = append(events, event) })
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "SUCCEEDED" || result.Provider != "test-agent" || result.Model != "test-model" || len(events) != 1 || events[0].RunID != "run-1" {
		t.Fatalf("协议结果错误: result=%#v events=%#v", result, events)
	}
}

func TestCLIProviderTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("测试依赖 POSIX shell")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "sleep.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 2\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	provider := NewCLIProvider("test-agent", script)
	provider.Timeout = 50 * time.Millisecond
	_, err := provider.Run(context.Background(), Task{RunID: "run-timeout"}, nil)
	if err == nil {
		t.Fatal("超时任务应失败")
	}
}

func TestWriteTaskFilePermissions(t *testing.T) {
	path, err := WriteTaskFile(t.TempDir(), Task{RunID: "run-file"})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("任务文件权限错误: %o", info.Mode().Perm())
	}
}
