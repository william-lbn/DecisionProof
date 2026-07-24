//go:build windows

package agent

import "os/exec"

func configureProcessGroup(cmd *exec.Cmd) {}
func killProcessTree(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
