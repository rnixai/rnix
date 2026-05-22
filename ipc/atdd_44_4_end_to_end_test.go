package ipc

import (
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
)

// =============================================================================
// ATDD 44.4 — AC#6: Decker 6-step reproduction closed loop at the daemon / IPC
// boundary, plus AC#7: CLI (`rnix resume <uuid>` → ResumeWithOptsV3) vs
// dashboard r (ResumeSubtree) equivalence.
//
// RED PHASE signal: `client.PauseSubtree` / `client.ResumeSubtree` are
// undefined until dev-story Task 1.3 — compile-fail RED (same として AC#1).
// Once the methods land, the assertions encode the behavioural contract:
//   - PauseSubtree suspends the whole subtree (suspend_reason "user_paused").
//   - ResumeSubtree on the CHILD does NOT walk up to the parent.
//   - ResumeSubtree on the PARENT resumes it and SKIPS the now-Dead child
//     (Decker step-5 core fix — no error, subtree leaves stall).
//   - CLI ResumeWithOptsV3 and dashboard ResumeSubtree converge on the same
//     final Running set.
//
// Construction reuses the ipc-package helpers from 44.3 (same package, visible
// across files): setupTestServer, writeIPCSuspendFixture, ipcSuspendDiskInfo,
// uuidIPCForTest. Live Running processes use srv.kern.Spawn(SkipReasonLoop) so
// the loop stays free of LLM I/O.
// =============================================================================

// TestATDD_44_4_060_EndToEnd_DeckerSixSteps
//
// Walks the Decker reproduction: spawn parent→child, dashboard p (PauseSubtree),
// Ctrl+C-equivalent (process stays Suspended), dashboard r on the child, then
// dashboard r on the parent skipping the Dead child.
func TestATDD_44_4_060_EndToEnd_DeckerSixSteps(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	// Step 1: build the Running script-runner parent + Running child tree.
	parentPID, err := srv.kern.Spawn("dev-auto.ash main loop", nil, kernel.SpawnOpts{SkipReasonLoop: true})
	if err != nil {
		t.Fatalf("Spawn parent: %v", err)
	}
	childPID, err := srv.kern.Spawn("spawned child task", nil, kernel.SpawnOpts{SkipReasonLoop: true, ParentPID: parentPID})
	if err != nil {
		t.Fatalf("Spawn child: %v", err)
	}

	cli, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { cli.Close() })

	// Step 2: dashboard `p` == client.PauseSubtree(parent). Whole subtree
	// Suspended with suspend_reason "user_paused" (44.1 SubtreeManager value).
	pauseResp, err := cli.PauseSubtree(parentPID)
	if err != nil {
		t.Fatalf("Step 2 PauseSubtree: %v", err)
	}
	if pauseResp.Affected != 2 {
		t.Errorf("Step 2: PauseSubtree Affected = %d, want 2 (parent + child)", pauseResp.Affected)
	}
	reasons := suspendReasonsByPID(t, cli)
	for label, pid := range map[string]types.PID{"parent": parentPID, "child": childPID} {
		proc, _ := srv.kern.GetProcess(pid)
		if proc == nil || proc.GetState() != types.StateSuspended {
			t.Errorf("Step 2: %s state = %v, want Suspended", label, proc)
		}
		if reasons[pid] != "user_paused" {
			t.Errorf("Step 2: %s suspend_reason = %q, want %q", label, reasons[pid], "user_paused")
		}
	}

	// Step 3: Ctrl+C equivalent — the CLI conn drops but the daemon keeps the
	// processes Suspended (44.2). We assert the invariant directly.
	if proc, _ := srv.kern.GetProcess(parentPID); proc == nil || proc.GetState() != types.StateSuspended {
		t.Errorf("Step 3: parent must remain Suspended after CLI disconnect")
	}

	// Step 4: dashboard `r` on the CHILD (Decker step 3) — only the child
	// subtree resumes; the parent must NOT be walked up to (不向上倒查父).
	if _, err := cli.ResumeSubtree(childPID); err != nil {
		t.Fatalf("Step 4 ResumeSubtree(child): %v", err)
	}
	if proc, _ := srv.kern.GetProcess(childPID); proc == nil || proc.GetState() != types.StateRunning {
		t.Errorf("Step 4: child state = %v, want Running after ResumeSubtree(child)", proc)
	}
	if proc, _ := srv.kern.GetProcess(parentPID); proc == nil || proc.GetState() != types.StateSuspended {
		t.Errorf("Step 4: parent state = %v, want still Suspended (resume must not walk up)", proc)
	}

	// Child runs to completion → Dead (Running→Zombie→Dead).
	childProc, _ := srv.kern.GetProcess(childPID)
	if err := childProc.Terminate(kernel.ExitStatus{Code: 0, Reason: "completed"}); err != nil {
		t.Fatalf("Step 4: Terminate child: %v", err)
	}
	if err := childProc.Transition(types.StateDead); err != nil {
		t.Fatalf("Step 4: Transition child to Dead: %v", err)
	}

	// Step 5: dashboard `r` on the PARENT (Decker steps 5-6, the core fix) —
	// the parent resumes; the now-Dead child is SKIPPED, not an error.
	resumeResp, err := cli.ResumeSubtree(parentPID)
	if err != nil {
		t.Fatalf("Step 5 ResumeSubtree(parent): %v (must skip Dead child, not error)", err)
	}
	if resumeResp.Affected < 1 {
		t.Errorf("Step 5: ResumeSubtree Affected = %d, want >=1 (parent resumed)", resumeResp.Affected)
	}
	if resumeResp.Skipped < 1 {
		t.Errorf("Step 5: ResumeSubtree Skipped = %d, want >=1 (Dead child skipped)", resumeResp.Skipped)
	}

	// Step 6: parent is Running again — the subtree has left the stall. With
	// SkipReasonLoop fixtures there is no reasonStep / ScriptExecutor to emit a
	// follow-up Spawn, so Running is the observable proxy for "主循环可继续推进"
	// (the real events.jsonl Spawn check belongs to the manual smoke in Task 7.5).
	if proc, _ := srv.kern.GetProcess(parentPID); proc == nil || proc.GetState() != types.StateRunning {
		t.Errorf("Step 6: parent state = %v, want Running (subtree no longer stalled)", proc)
	}
}

// TestATDD_44_4_061_EndToEnd_CLIvsDashboardResumeEquivalence
//
// AC#7: for an equivalent Suspended parent+child subtree, the CLI path
// (client.ResumeWithOptsV3 — Epic 42 UUID resume, which 44.3 made trigger
// subtree wakeup) and the dashboard path (client.ResumeSubtree) must converge
// on the same final Running set. Disk placeholders + LoadSuspendedFromDisk
// mirror the daemon-restart shape so ResumeWithOptsV3 has history to replay.
func TestATDD_44_4_061_EndToEnd_CLIvsDashboardResumeEquivalence(t *testing.T) {
	// setupResumeIPCTest wires a parkable mockLLMFile (vs setupTestServer's
	// fire-and-exit noopLLMFile). The CLI path (ResumeWithOpts) rebuilds a full
	// reasonStep loop; without a gate that loop would race to completion → Dead
	// under load (the documented 42.x flaky). parkOnRead holds every resumed
	// reasonStep at its first LLM Read so the processes stay Running for the
	// convergence assertion — mirroring a real driver, where a resumed process
	// stays Running while the LLM is thinking.
	cli, kern, baseDir, llmFile := setupResumeIPCTest(t)

	_, release := llmFile.parkOnRead()
	defer close(release) // unblock parked reasonSteps before kernel.Shutdown

	// Tree A — resumed via the CLI path (ResumeWithOptsV3 on the parent UUID).
	parentAUUID := uuidIPCForTest("e2a060")
	childAUUID := uuidIPCForTest("e2a061")
	writeSuspendedPairFixture(t, baseDir, parentAUUID, childAUUID, 60, 61)

	// Tree B — resumed via the dashboard path (ResumeSubtree on the parent PID).
	parentBUUID := uuidIPCForTest("e2b060")
	childBUUID := uuidIPCForTest("e2b061")
	writeSuspendedPairFixture(t, baseDir, parentBUUID, childBUUID, 62, 63)

	if _, err := kern.LoadSuspendedFromDisk(); err != nil {
		t.Fatalf("LoadSuspendedFromDisk: %v", err)
	}

	// CLI path.
	if _, err := cli.ResumeWithOptsV3(parentAUUID, false, 0, "", ""); err != nil {
		t.Fatalf("CLI ResumeWithOptsV3(parentA): %v", err)
	}

	// Dashboard path — resolve the reloaded parent's fresh PID by UUID.
	parentBProc, ok := kern.GetProcessByUUID(parentBUUID)
	if !ok {
		t.Fatalf("tree B parent placeholder missing after reload")
	}
	if _, err := cli.ResumeSubtree(parentBProc.PID); err != nil {
		t.Fatalf("dashboard ResumeSubtree(parentB): %v", err)
	}

	// Both subtrees must end with parent + child Running.
	waitUUIDRunning(t, kern, parentAUUID)
	waitUUIDRunning(t, kern, childAUUID)
	waitUUIDRunning(t, kern, parentBUUID)
	waitUUIDRunning(t, kern, childBUUID)
}

// writeSuspendedPairFixture writes a Suspended parent + Suspended child pair to
// disk (with steps + meta) so LoadSuspendedFromDisk pulls them back as
// placeholders. suspend_reason "user_paused" matches the 44.1 main-path value.
func writeSuspendedPairFixture(t *testing.T, tmpDir, parentUUID, childUUID string, parentPID, childPID uint64) {
	t.Helper()
	writeIPCSuspendFixture(t, tmpDir, ipcSuspendDiskInfo{
		PID:           parentPID,
		UUID:          parentUUID,
		State:         "suspended",
		Intent:        "AC#7 parent",
		Provider:      "claude",
		Model:         "claude-opus-4-7",
		MaxSteps:      100,
		CreatedAt:     time.Now().Add(-20 * time.Minute).Format(time.RFC3339Nano),
		CtxID:         1,
		SuspendReason: "user_paused",
		IsPaused:      true,
	}, true, true)
	writeIPCSuspendFixture(t, tmpDir, ipcSuspendDiskInfo{
		PID:           childPID,
		UUID:          childUUID,
		PPID:          parentPID,
		ParentUUID:    parentUUID,
		State:         "suspended",
		Intent:        "AC#7 child",
		Provider:      "claude",
		Model:         "claude-opus-4-7",
		MaxSteps:      100,
		CreatedAt:     time.Now().Add(-19 * time.Minute).Format(time.RFC3339Nano),
		CtxID:         2,
		SuspendReason: "user_paused",
		IsPaused:      true,
	}, true, true)
}

// suspendReasonsByPID returns a PID→suspend_reason map from the live procTable.
func suspendReasonsByPID(t *testing.T, cli *Client) map[types.PID]string {
	t.Helper()
	procs, err := cli.ListProcs()
	if err != nil {
		t.Fatalf("ListProcs: %v", err)
	}
	out := make(map[types.PID]string, len(procs))
	for _, p := range procs {
		out[p.PID] = p.SuspendReason
	}
	return out
}

// waitUUIDRunning polls up to 1s for the process identified by UUID to reach
// Running, mirroring the 44.3 subtree-wakeup wait style.
func waitUUIDRunning(t *testing.T, kern *kernel.KernelImpl, uuid string) {
	t.Helper()
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if proc, ok := kern.GetProcessByUUID(uuid); ok && proc.GetState() == types.StateRunning {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	proc, _ := kern.GetProcessByUUID(uuid)
	st := types.StateDead
	if proc != nil {
		st = proc.GetState()
	}
	t.Errorf("UUID %s state = %s, want Running (CLI/dashboard resume must converge)", uuid, st)
}
