//go:build unix

package mcp

import (
	"errors"
	"os/exec"
	"syscall"
)

// sendGroupSIGTERM sends SIGTERM to the entire process group of cmd. Returns
// nil on ESRCH (group already gone — treat as graceful exit). Story 48.2 易错点 #3:
// the negative argument to syscall.Kill addresses the process group whose PGID
// equals |pid|; without the minus the signal would only reach the leader.
//
// Caller must guarantee cmd.Process != nil.
func sendGroupSIGTERM(cmd *exec.Cmd) error {
	pid := cmd.Process.Pid
	err := syscall.Kill(-pid, syscall.SIGTERM)
	if err == nil || errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

// sendGroupSIGKILL sends SIGKILL to the entire process group of cmd (and, as
// a belt-and-braces fallback, to the leader process itself in case Setpgid
// was somehow not honored). Returns nil on ESRCH or after a successful
// leader-only fallback.
//
// Caller must guarantee cmd.Process != nil.
func sendGroupSIGKILL(cmd *exec.Cmd) error {
	pid := cmd.Process.Pid
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		// Fall back to leader-only kill. If that succeeds, the cleanup is
		// done from our perspective — returning the group-kill error would
		// produce misleading "SIGKILL returned: <err>" logs at the call site
		// even though the leader did exit (code review F4, 2026-05-28).
		// Only bubble when both group and leader kills fail.
		if killErr := cmd.Process.Kill(); killErr != nil {
			return err
		}
	}
	return nil
}
