//go:build linux

package kernel

import (
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// TestOSReconcile_LinuxRealScanFakeKill is the one Linux integration case
// (Dev Notes Task 7): a real child process carrying RNIX_PROC_UUID in its env
// (and Setpgid) must be discovered by the REAL /proc scanner and, when unowned
// across two rounds, handed to the killer with its exact OS pid. The killer is
// faked so the test never actually signals the process (t.Cleanup reaps it).
func TestOSReconcile_LinuxRealScanFakeKill(t *testing.T) {
	const marker = "01890000-0000-7000-8000-00000000feed"

	cmd := exec.Command("sleep", "300")
	cmd.Env = append(cmd.Environ(), "RNIX_PROC_UUID="+marker)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	sleepPID := cmd.Process.Pid
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	var killed []int
	r := &osReconciler{
		scan: defaultOSProcScanner, // REAL /proc walk
		kill: func(osPid int) { killed = append(killed, osPid) },
		// Marker UUID is not in any process table ⇒ always orphan.
		owned:      func(string) bool { return false },
		candidates: map[int]string{},
	}

	// Round one: real scan must find our tagged sleep; warn only, no kill.
	r.reconcileOnce()
	if _, ok := r.candidates[sleepPID]; !ok {
		t.Fatalf("real scanner did not discover tagged sleep pid=%d (candidates=%v)", sleepPID, r.candidates)
	}
	if len(killed) != 0 {
		t.Fatalf("round one must not kill, got %v", killed)
	}

	// Round two: still orphan, still alive ⇒ reap with the exact OS pid.
	r.reconcileOnce()
	found := false
	for _, p := range killed {
		if p == sleepPID {
			found = true
		}
	}
	if !found {
		t.Fatalf("round two must reap tagged sleep pid=%d, got kills=%v", sleepPID, killed)
	}
}

// TestOSReconcile_LinuxScannerIgnoresUntagged confirms the scanner never
// surfaces a process WITHOUT the RNIX_PROC_UUID env — the "绝不按进程名猜"
// guarantee that a user's own commands are untouched.
func TestOSReconcile_LinuxScannerIgnoresUntagged(t *testing.T) {
	cmd := exec.Command("sleep", "300") // no RNIX_PROC_UUID in env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	untaggedPID := cmd.Process.Pid
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	// Let the process settle so it appears in /proc.
	time.Sleep(50 * time.Millisecond)

	for _, p := range defaultOSProcScanner() {
		if p.OSPid == untaggedPID {
			t.Fatalf("scanner must ignore untagged pid=%d (matched by name, not env)", untaggedPID)
		}
	}
}
