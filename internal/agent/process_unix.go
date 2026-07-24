//go:build !windows

package agent

import (
	"os/exec"
	"syscall"
)

// configureProcessGroup 让 Agent CLI 及其所有子进程进入独立进程组。
func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessTree 终止整个进程组，防止 Agent 超时后遗留编译器、测试或网络进程。
func killProcessTree(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
