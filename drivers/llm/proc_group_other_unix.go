//go:build unix && !linux

package llm

import (
	"os/exec"
	"syscall"
)

// setProcGroupAttr configures cmd to run in its own process group (Setpgid).
// Pdeathsig is NOT set — darwin / OpenBSD / NetBSD do not declare the field on
// syscall.SysProcAttr, so referencing it would fail to compile. FreeBSD does
// support Pdeathsig but is grouped here for simplicity (mirrors
// drivers/mcp/transport_pgrp_other_unix.go, Story 48.2).
//
// Must be called BEFORE cmd.Start().
func setProcGroupAttr(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}
