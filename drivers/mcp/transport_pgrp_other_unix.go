//go:build unix && !linux

package mcp

import (
	"os/exec"
	"syscall"
)

// applyProcessGroupIsolation configures cmd to run in its own process group
// (Setpgid). Pdeathsig is NOT set — darwin / OpenBSD / NetBSD do not declare
// the field on syscall.SysProcAttr, so referencing it would fail to compile.
// FreeBSD supports Pdeathsig but is grouped here for simplicity (see
// Story 48.2 Dev Notes §平台覆盖矩阵).
func applyProcessGroupIsolation(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
