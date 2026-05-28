//go:build unix

package mcp

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

// isChildAlive reports whether the process pid is alive, treating a
// SIGKILLed-but-unreaped zombie as DEAD (Story 48.5 AC1 L1 liveness).
//
// A bare syscall.Kill(pid, 0) is insufficient: a child we Started but have not
// yet cmd.Wait()'d lingers as a zombie that still answers signal 0 (it occupies
// a process-table slot). The L1 contract is "is the server usable", so a zombie
// must read as dead. On Linux we disambiguate via /proc/<pid>/stat's state
// field; on other unix without /proc we fall back to the signal-0 result
// (best-effort — those platforms are not the Story 48.5 test target).
func isChildAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	if err := syscall.Kill(pid, 0); err != nil {
		return false // ESRCH (gone) / EPERM — treat as not-ours/not-alive
	}
	if state, ok := procState(pid); ok {
		// 'Z' = zombie (exited, awaiting reap), 'X'/'x' = dead.
		return state != 'Z' && state != 'X' && state != 'x'
	}
	return true
}

// procState returns the single-character process state from /proc/<pid>/stat
// (Linux). ok=false when /proc is unavailable (e.g. darwin/BSD) or unreadable.
// The comm field (2nd) may contain spaces and parentheses, so we anchor on the
// LAST ')' — the state char follows ") ".
func procState(pid int) (byte, bool) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, false
	}
	i := bytes.LastIndexByte(data, ')')
	if i < 0 || i+2 >= len(data) {
		return 0, false
	}
	return data[i+2], true
}

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
