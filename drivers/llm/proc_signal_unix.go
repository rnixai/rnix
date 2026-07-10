//go:build unix

package llm

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// groupCancelSIGTERM sends SIGTERM to the entire process group of cmd. It is
// installed as cmd.Cancel by configureCommandGrace so that ctx cancellation
// (caller timeout / explicit Kill / step retry / suspend) terminates the whole
// CLI-agent subtree, not just the leader.
//
// The negative argument to syscall.Kill addresses the process group whose PGID
// equals |pid| (requires Setpgid at Start — see setProcGroupAttr). Per the
// os/exec.Cmd contract, Cancel must wrap os.ErrProcessDone on ESRCH so Wait
// does not surface an already-gone group as the run error (mirrors
// drivers/shell/shell_pgrp_unix.go). If the group kill fails for any other
// reason, fall back to signalling the leader alone.
//
// Caller (cmd.Cancel) guarantees cmd.Process != nil is checked first.
func groupCancelSIGTERM(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return os.ErrProcessDone
	}
	pid := cmd.Process.Pid
	err := syscall.Kill(-pid, syscall.SIGTERM)
	if err == nil {
		return nil
	}
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	// Group kill failed for a non-ESRCH reason (e.g. Setpgid not honored) —
	// fall back to leader-only SIGTERM so the graceful window still applies.
	return cmd.Process.Signal(syscall.SIGTERM)
}

// reapCommandGroup sends SIGKILL to the entire process group of cmd after
// cmd.Wait/cmd.Run returns, establishing the invariant "Wait returned ⇒ group
// empty". It clears subagent残留 that ignored the earlier group SIGTERM (Go's
// WaitDelay only force-kills the leader) as well as post-exit stragglers.
//
// On the normal path the group is already empty, so the group kill returns
// ESRCH and this is an idempotent no-op (AC2/AC6 zero-behavior-change basis).
// A non-ESRCH failure falls back to a leader-only kill; only when both fail is
// the error meaningful, and even then callers treat reap as best-effort
// (mirrors drivers/mcp/transport_signal_unix.go sendGroupSIGKILL, code-review
// F4). The pgid-reuse race (group empties, PGID reassigned, then this SIGKILL
// lands) is vanishingly small and accepted here as it is in the shell/mcp
// precedents.
func reapCommandGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		// Best-effort leader fallback; ignore its error (process may be gone).
		_ = cmd.Process.Kill()
	}
}
