//go:build unix

package llm

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Story 66.5 — OS process-group reaping for CLI-agent driver subprocesses.
//
// These tests exercise the production helpers configureCommandGrace /
// groupCancelSIGTERM / reapCommandGroup directly against a REAL child process
// (sh -c), because the re-exec TestHelperProcess path cannot fork a grandchild
// to prove group-scoped termination. See Dev Notes 测试标准.

// pidAlive reports whether pid can still be signalled (signal 0). ESRCH ⇒ gone.
func pidAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

// waitPidGone polls until pid is unsignalable or the deadline elapses.
func waitPidGone(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !pidAlive(pid) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return !pidAlive(pid)
}

// readGrandchildPID polls tmpfile (written by the leader script) for the
// backgrounded grandchild PID.
func readGrandchildPID(t *testing.T, path string, timeout time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			s := strings.TrimSpace(string(data))
			if s != "" {
				pid, perr := strconv.Atoi(s)
				if perr == nil && pid > 0 {
					return pid
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("grandchild pid not written to %s within %s", path, timeout)
	return 0
}

// TestATDD_66_5_ConfigureCommandGrace_SetsProcGroup asserts the property:
// after configureCommandGrace the child is destined for its own process group.
func TestATDD_66_5_ConfigureCommandGrace_SetsProcGroup(t *testing.T) {
	cmd := exec.Command("true")
	configureCommandGrace(cmd, 1)

	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Fatalf("expected Setpgid=true after configureCommandGrace, got SysProcAttr=%+v", cmd.SysProcAttr)
	}
	if cmd.Cancel == nil {
		t.Fatal("expected Cancel hook installed")
	}
	if cmd.WaitDelay != 1*time.Second {
		t.Fatalf("expected WaitDelay=1s (grace), got %s", cmd.WaitDelay)
	}
}

// TestATDD_66_5_GroupCancel_KillsGrandchild is the core end-to-end proof: a
// leader that forks a grandchild must, on ctx cancel, take the WHOLE group down
// — not just the leader. Under the pre-66.5 leader-only SIGTERM the grandchild
// would reparent to init and survive; the group SIGTERM + reap backstop kill it.
func TestATDD_66_5_GroupCancel_KillsGrandchild(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "grandchild.pid")

	// Leader (sh) backgrounds a `sleep 300` grandchild, records its pid, then
	// blocks on its own foreground `sleep 300` so it stays alive until cancel.
	script := "sleep 300 & echo $! > " + pidFile + "; sleep 300"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", script)
	configureCommandGrace(cmd, 1) // WithGrace(1): shorten the test's grace window
	if err := cmd.Start(); err != nil {
		t.Fatalf("start leader: %v", err)
	}
	leaderPID := cmd.Process.Pid
	grandPID := readGrandchildPID(t, pidFile, 5*time.Second)

	if !pidAlive(grandPID) {
		t.Fatalf("grandchild %d should be alive before cancel", grandPID)
	}

	// Trigger ctx cancel → cmd.Cancel = groupCancelSIGTERM (group SIGTERM).
	cancel()
	_ = cmd.Wait()
	// Post-Wait group SIGKILL backstop (production call sites do this).
	reapCommandGroup(cmd)

	if !waitPidGone(leaderPID, 3*time.Second) {
		t.Errorf("leader %d still alive after cancel+reap", leaderPID)
	}
	if !waitPidGone(grandPID, 3*time.Second) {
		t.Errorf("grandchild %d still alive after cancel+reap — group termination failed", grandPID)
	}
}

// TestATDD_66_5_ReapCommandGroup_IdempotentOnNaturalExit asserts that when a
// child exits on its own (no descendants), the post-Wait reap is a harmless
// no-op (group already empty ⇒ ESRCH). Guards AC2/AC6 zero-behavior-change.
func TestATDD_66_5_ReapCommandGroup_IdempotentOnNaturalExit(t *testing.T) {
	ctx := context.Background()
	cmd := exec.CommandContext(ctx, "sh", "-c", "exit 0")
	configureCommandGrace(cmd, 1)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("expected clean exit, got %v", err)
	}
	// Must not panic / must not error out; group is empty.
	reapCommandGroup(cmd)
	reapCommandGroup(cmd) // double-call also safe
}

// TestATDD_66_5_ReapCommandGroup_NilSafe guards the nil / not-started paths so
// call-site接线 never panics.
func TestATDD_66_5_ReapCommandGroup_NilSafe(t *testing.T) {
	reapCommandGroup(nil)
	reapCommandGroup(exec.Command("true")) // never Started ⇒ Process == nil
}
