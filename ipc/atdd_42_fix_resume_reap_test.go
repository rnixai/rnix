package ipc

import (
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// =============================================================================
// ATDD Epic 42 follow-up fix: handleResume must reap top-level resumed processes.
//
// Bug: When a resumed process (PPID=0) exited (kill, error, natural complete),
// nothing called Reap, so state stayed in StateZombie forever and DeadAt was
// never set. Dashboard Agent Tree elapsed time fell back to wall-clock and grew
// indefinitely (user-reported regression after Epic 42 — `817.9m` and rising).
//
// Fix: handleResume starts a background goroutine that waits for proc.Done and
// calls s.kern.Reap(pid), mirroring handleSpawn's `defer s.kern.Reap(pid)`.
// =============================================================================

// --- FIX-RESUME-REAP-001: handleResume 触发后进程退出能被 reap ---
//
// Given: 一个 Dead UUID 的完整 history（steps.jsonl + proc-info.json）
// When:  通过 IPC client.Resume 启动新进程，mock LLM 立即返回 complete
// Then:  ≤ 2 秒内新 PID 应进入 StateDead 且 DeadAt 被设置（不为零值）
func TestATDD_42_Fix_ResumeReap_001_KillReapsAndSetsDeadAt(t *testing.T) {
	client, kern, baseDir, _ := setupResumeIPCTest(t)
	uuid := "fix-reap-resume-0000-0000-000000000001"
	writeIPCTestData(t, baseDir, uuid, 3)

	resp, err := client.ResumeWithOpts(uuid, false)
	if err != nil {
		t.Fatalf("IPC Resume failed: %v", err)
	}
	if resp.PID == 0 {
		t.Fatalf("Resume returned PID 0")
	}

	// The mock LLM (setupResumeIPCTest) returns action=complete on first read,
	// so the resumed process exits on its own. Poll until reap completes.
	deadline := time.Now().Add(2 * time.Second)
	var finalState types.ProcessState
	var deadAtZero bool
	for time.Now().Before(deadline) {
		info, err := kern.GetProcInfo(resp.PID)
		if err != nil {
			// Process may have been TTL-removed from procTable — fall back to history.
			if hist := kern.FindHistoryByUUID(resp.UUID); hist != nil {
				finalState = hist.State
				deadAtZero = hist.DeadAt.IsZero()
				if finalState == types.StateDead && !deadAtZero {
					return // success
				}
			}
			time.Sleep(20 * time.Millisecond)
			continue
		}
		finalState = info.State
		deadAtZero = info.DeadAt.IsZero()
		if finalState == types.StateDead && !deadAtZero {
			return // success
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("resumed process not reaped within 2s: state=%v DeadAt.IsZero=%v (regression — handleResume goroutine missing or not triggering kern.Reap)",
		finalState, deadAtZero)
}

// --- FIX-RESUME-REAP-002: Fork resume 的 new UUID 也能被 reap ---
//
// Verifies that the reap goroutine fires for fork mode too (new UUID branch).
func TestATDD_42_Fix_ResumeReap_002_ForkAlsoReaps(t *testing.T) {
	client, kern, baseDir, _ := setupResumeIPCTest(t)
	uuid := "fix-reap-fork-0000-0000-000000000001"
	writeIPCTestData(t, baseDir, uuid, 3)

	resp, err := client.ResumeWithOpts(uuid, true)
	if err != nil {
		t.Fatalf("IPC Resume (fork) failed: %v", err)
	}
	if resp.PID == 0 {
		t.Fatalf("Resume returned PID 0")
	}
	if resp.UUID == uuid {
		t.Fatalf("fork should produce new UUID, got original")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		info, err := kern.GetProcInfo(resp.PID)
		if err == nil && info.State == types.StateDead && !info.DeadAt.IsZero() {
			return // success
		}
		if hist := kern.FindHistoryByUUID(resp.UUID); hist != nil &&
			hist.State == types.StateDead && !hist.DeadAt.IsZero() {
			return // success — already moved to history
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("forked resumed process (new uuid=%s pid=%d) not reaped within 2s", resp.UUID, resp.PID)
}
