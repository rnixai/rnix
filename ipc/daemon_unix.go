//go:build unix

package ipc

import (
	"os/exec"
	"syscall"
)

// setDaemonSysProcAttr sets process attributes so the daemon runs in its own session (Unix).
func setDaemonSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
