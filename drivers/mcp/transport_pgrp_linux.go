//go:build linux

package mcp

import (
	"os/exec"
	"syscall"
)

// applyProcessGroupIsolation configures cmd to run in its own process group
// (Setpgid) and, on Linux, asks the kernel to deliver SIGKILL to the child
// when the parent (this daemon) exits without graceful shutdown (Pdeathsig).
//
// Story 48.2 易错点 #1: Pdeathsig only exists on Linux and FreeBSD. The
// transport_pgrp_other_unix.go counterpart sets Setpgid only. The build tag
// keeps darwin / OpenBSD / NetBSD compiles green since their
// syscall.SysProcAttr does not declare Pdeathsig.
//
// Story 48.2 易错点 #2: SysProcAttr must be assigned BEFORE cmd.Start —
// Go's runtime only honors the value at fork-time.
func applyProcessGroupIsolation(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid:   true,
		Pdeathsig: syscall.SIGKILL,
	}
}
