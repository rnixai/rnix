package kernel

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rnixai/rnix/internal/types"
)

// =============================================================================
// ATDD 44.3 — AC#2: SuspendSubtree / ResumeSubtree synchronously persist
// proc-info.json so daemon restart can reload state without depending on
// reaper.
//
// Story spec (44-3 §AC2):
//   - suspendProcess (kernel/suspend.go:16-66) gains a best-effort
//     SaveProcInfo call AFTER the Suspended state transition.
//   - resumeOneForSubtree (kernel/subtree.go:266) gains a best-effort
//     SaveProcInfo call AFTER the Unsuspend → Resume emission (策略 A,
//     §Tasks/2.2 — keeps the persistence call in the kernel layer to avoid a
//     Process → Kernel reverse dependency).
//   - Both writes are best-effort: log on failure, never block the state
//     transition.
//
// RED phase signal: the test reads the on-disk JSON via readProcInfoFromDisk44_3
// AFTER calling SuspendSubtree / ResumeSubtree. Pre-Task-2 the JSON does
// not contain suspend_reason (suspendProcess never calls SaveProcInfo on
// the non-reap path), so the test fails on assertion. Compile passes —
// this AC is behaviour-driven RED.
// =============================================================================

// TestATDD_44_3_020_SuspendSubtree_PersistsSuspendReason
//
// AC#2: After SuspendSubtree successfully transitions a Running process to
// Suspended, the on-disk proc-info.json under <stepDataDir>/data/steps/
// <UUID>/ must contain the suspend_reason value the kernel set
// ("user_paused" — the value 44.1 SubtreeManager writes via
// suspendOneForSubtree → suspendProcess).
func TestATDD_44_3_020_SuspendSubtree_PersistsSuspendReason(t *testing.T) {
	k, dataDir := newReloadKernel(t)
	proc := makeRunningProc44_1(t, k, 0, "AC#2 suspend persist")

	affected, err := k.SuspendSubtree(proc.PID)
	if err != nil {
		t.Fatalf("SuspendSubtree: %v", err)
	}
	if affected != 1 {
		t.Errorf("affected = %d, want 1", affected)
	}
	assertProcState44_1(t, proc, types.StateSuspended, "after SuspendSubtree")

	// AC#2 BEHAVIOUR: the proc-info.json on disk must reflect the suspend.
	raw := readProcInfoFromDisk44_3(t, dataDir, proc.UUID)

	gotReason, _ := raw["suspend_reason"].(string)
	if gotReason != "user_paused" {
		t.Errorf("disk suspend_reason = %q, want %q (44.1 SubtreeManager value)", gotReason, "user_paused")
	}
	gotState, _ := raw["state"].(string)
	if gotState != "suspended" {
		t.Errorf("disk state = %q, want %q", gotState, "suspended")
	}
}

// TestATDD_44_3_021_ResumeSubtree_PersistsRunningState
//
// AC#2 symmetric path: after ResumeSubtree wakes a Suspended placeholder
// the disk's suspend_reason must be cleared and is_paused must be false /
// absent. This guards against the bug where SuspendReason="user_paused"
// would persist on disk across a Suspend→Resume cycle, causing the next
// daemon restart to wrongly reload the process as Suspended.
func TestATDD_44_3_021_ResumeSubtree_PersistsRunningState(t *testing.T) {
	k, dataDir := newReloadKernel(t)

	// Bring up Running, push Suspended, then Resume. We rely on 44.1's
	// SuspendSubtree to write the initial Suspended snapshot (AC#2-a above)
	// then assert ResumeSubtree overwrites it.
	proc := makeRunningProc44_1(t, k, 0, "AC#2 resume persist")
	if _, err := k.SuspendSubtree(proc.PID); err != nil {
		t.Fatalf("SuspendSubtree: %v", err)
	}
	assertProcState44_1(t, proc, types.StateSuspended, "between suspend and resume")

	// PrimaryDevice is empty for our test fixtures → resumeOneForSubtree
	// takes the awaiting_script_driver branch (Story 44.1 review F10). The
	// persistence call must fire on that branch too.
	if _, _, err := k.ResumeSubtree(proc.PID); err != nil {
		t.Fatalf("ResumeSubtree: %v", err)
	}
	assertProcState44_1(t, proc, types.StateRunning, "after ResumeSubtree")

	raw := readProcInfoFromDisk44_3(t, dataDir, proc.UUID)

	if gotState, _ := raw["state"].(string); gotState != "running" {
		t.Errorf("disk state after Resume = %q, want %q", gotState, "running")
	}
	if gotReason, _ := raw["suspend_reason"].(string); gotReason != "" {
		t.Errorf("disk suspend_reason after Resume = %q, want empty (cleared by Unsuspend)", gotReason)
	}
	// is_paused may be absent (omitempty) OR present as false. Both are
	// acceptable; what we forbid is an `is_paused: true` left over from
	// the prior Suspend cycle.
	if gotPaused, ok := raw["is_paused"].(bool); ok && gotPaused {
		t.Errorf("disk is_paused after Resume = true; want false or omitted")
	}
}

// TestATDD_44_3_022_SuspendProcess_BestEffortSurvivesDirError
//
// AC#2 best-effort contract: a write failure in the SuspendProcess
// persistence call MUST NOT propagate as a Suspend error. We emulate the
// failure by pointing stepDataDir at an unwritable path AFTER spawning the
// process, then verifying SuspendSubtree still returns nil.
//
// Why "best-effort" matters: SuspendProcess runs in the hot path of
// SIGPAUSE / dashboard p. A transient disk error (full /tmp, transient
// permission flip on a tmpfs) must not leave the process in a half-state
// where the state machine is Suspended but the caller saw an error and
// might attempt to roll back.
func TestATDD_44_3_022_SuspendProcess_BestEffortSurvivesDirError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: chmod 0o500 does not block writes, cannot deterministically exercise the failure path")
	}
	k, _ := newReloadKernel(t)
	proc := makeRunningProc44_1(t, k, 0, "AC#2 best-effort")

	// Point stepDataDir at a child of a read-only (0o500) directory so
	// SaveProcInfo's MkdirAll fails deterministically with EACCES. Using a
	// chmod'd TempDir rather than /sys avoids the previous no-op degradation
	// on sandboxes where /sys happens to be writable.
	roDir := filepath.Join(t.TempDir(), "readonly")
	if err := os.Mkdir(roDir, 0o500); err != nil {
		t.Fatalf("mkdir readonly dir: %v", err)
	}
	// Restore writable perms so t.TempDir cleanup can remove the tree.
	t.Cleanup(func() { _ = os.Chmod(roDir, 0o700) })
	k.SetStepDataDir(filepath.Join(roDir, "stepdata"))

	_, err := k.SuspendSubtree(proc.PID)
	if err != nil {
		t.Fatalf("SuspendSubtree returned error despite best-effort persistence: %v", err)
	}
	if got := proc.GetState(); got != types.StateSuspended {
		t.Errorf("state after Suspend = %s, want Suspended (state transition must succeed even if disk write fails)", got)
	}
}
