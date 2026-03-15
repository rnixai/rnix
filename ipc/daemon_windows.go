//go:build windows

package ipc

import "os/exec"

// setDaemonSysProcAttr is a no-op on Windows (Setsid is Unix-only).
func setDaemonSysProcAttr(cmd *exec.Cmd) {}
