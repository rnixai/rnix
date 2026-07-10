//go:build linux

package llm

import (
	"os/exec"
	"syscall"
)

// setProcGroupAttr configures cmd to run in its own process group (Setpgid) so
// the whole CLI-agent subtree (e.g. claude and its sa-step-runner subagents)
// can be signalled as a group. On Linux it also asks the kernel to deliver
// SIGKILL to the leader when the parent (this daemon) dies without graceful
// shutdown (Pdeathsig) — the缺口② backstop for daemon crash / `kill -9`.
//
// Mirrors drivers/mcp/transport_pgrp_linux.go (Story 48.2). Two known caveats
// carried over verbatim:
//   - Pdeathsig only exists on Linux/FreeBSD; the proc_group_other_unix.go
//     counterpart sets Setpgid only so darwin/BSD compiles stay green.
//   - SysProcAttr must be assigned BEFORE cmd.Start — Go only honors it at
//     fork-time. All configureCommandGrace call sites run before Start/Run.
//
// Pdeathsig binds to the creating OS thread, not the process (golang known
// caveat); the mcp long-lived server shares this risk and accepts it. It only
// reaches the leader — subagents become orphans on leader death — so residual
// survivors are cleaned up by the daemon os-reconcile loop (Story 66.5 Task 5).
func setProcGroupAttr(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.SysProcAttr.Pdeathsig = syscall.SIGKILL
}
