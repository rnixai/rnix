package ipc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// ATDD 44.3 — AC#8: end-to-end Decker 6-step reproduction at the daemon /
// IPC boundary.
//
// Walks the abbreviated 10-step closed loop spelled out in 44-3 §AC8:
//   (1-3)  Spawn-equivalent: write a Suspended placeholder (with a Suspended
//          child) directly to disk so the test stays independent of LLM /
//          ScriptExecutor I/O. This emulates the state Step 1-3 would leave
//          on disk (run dev-auto.ash → spawn child → dashboard p → both
//          Suspended → 44.1 SubtreeManager writes proc-info.json).
//   (4)    Verify on-disk state == "suspended" + suspend_reason == "user_paused".
//   (5)    Daemon-restart emulation: rebuild a fresh KernelImpl on the same
//          stepDataDir and call LoadHistory + LoadSuspendedFromDisk.
//   (6)    Verify client.ListAllProcs() contains both UUIDs with
//          State==Suspended and SuspendReason="user_paused" — this is the
//          AC#6 invariant (dashboard / rnix top renders orange) embedded
//          inside the end-to-end test.
//   (7)    Verify no ReasonStep / Spawn events emitted in 200ms — daemon
//          restart did NOT auto-resume.
//   (8)    Call kern.ResumeWithOpts(parentUUID, fork=false).
//   (9)    Verify both parent and child transitioned to Running (subtree
//          wakeup landed via the AC#4 path).
//   (10)   Smoke: assert ResumeResult.UUID == parentUUID (non-fork).
//
// RED phase signal: same compile-fail on LoadSuspendedFromDisk as AC#3/AC#4,
// plus the subtree-wakeup behaviour from AC#4 that the dev-story hasn't
// implemented yet.
// =============================================================================

// TestATDD_44_3_080_EndToEnd_DaemonRestart_SuspendedPersist_Resume
//
// Closed-loop verification of the 10 acceptance steps. Single test function
// keeps the assertion order explicit and matches the way Story §AC8 was
// authored ("上述 10 步全部断言通过").
func TestATDD_44_3_080_EndToEnd_DaemonRestart_SuspendedPersist_Resume(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)
	tmpDir := t.TempDir()
	srv.kern.SetStepDataDir(tmpDir)

	parentUUID := uuidIPCForTest("e2ep080")
	childUUID := uuidIPCForTest("e2ec080")

	// Steps 1-3: leave the disk state that 44.1 SubtreeManager would have
	// written. suspend_reason="user_paused" matches the 44.1 main-path value.
	writeIPCSuspendFixture(t, tmpDir, ipcSuspendDiskInfo{
		PID:           80,
		UUID:          parentUUID,
		State:         "suspended",
		Intent:        "AC#8 parent script-runner",
		Provider:      "claude",
		Model:         "claude-opus-4-7",
		MaxSteps:      100,
		CreatedAt:     time.Now().Add(-30 * time.Minute).Format(time.RFC3339Nano),
		CtxID:         1,
		SuspendReason: "user_paused",
		IsPaused:      true,
	}, true, true)
	writeIPCSuspendFixture(t, tmpDir, ipcSuspendDiskInfo{
		PID:           81,
		UUID:          childUUID,
		PPID:          80,
		ParentUUID:    parentUUID,
		State:         "suspended",
		Intent:        "AC#8 child",
		Provider:      "claude",
		Model:         "claude-opus-4-7",
		MaxSteps:      100,
		CreatedAt:     time.Now().Add(-29 * time.Minute).Format(time.RFC3339Nano),
		CtxID:         2,
		SuspendReason: "user_paused",
		IsPaused:      true,
	}, true, true)

	// Step 4: verify on-disk state via raw JSON read (independent of
	// procInfoFromDisk and any field deserialization).
	parentDisk := readDiskProcInfoForE2E(t, tmpDir, parentUUID)
	if got, _ := parentDisk["state"].(string); got != "suspended" {
		t.Errorf("Step 4: parent disk state = %q, want %q", got, "suspended")
	}
	if got, _ := parentDisk["suspend_reason"].(string); got != "user_paused" {
		t.Errorf("Step 4: parent disk suspend_reason = %q, want %q", got, "user_paused")
	}

	// Step 5: daemon restart emulation. The existing srv.kern was created
	// by setupTestServer; we do not destroy it (its t.Cleanup is queued)
	// but switch the test focus to a freshly-built KernelImpl pointing at
	// the same stepDataDir, with its own VFS + ContextManager (no public
	// accessors exist on KernelImpl for those, so we construct fresh ones).
	freshReg := vfs.NewDeviceRegistry()
	// Register the same noopLLMFile mock setupTestServer uses, so the
	// freshly-built kernel can open /dev/llm/claude during Resume.
	_ = freshReg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &noopLLMFile{}, nil
	})
	freshVFS := vfs.NewVFS(freshReg)
	freshCtxMgr := rnixctx.NewManager()
	freshKern := kernel.NewKernel(freshVFS, freshCtxMgr, nil)
	freshKern.SetStepDataDir(tmpDir)
	t.Cleanup(freshKern.Shutdown)

	if err := freshKern.LoadHistory(); err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if _, err := freshKern.LoadSuspendedFromDisk(); err != nil {
		t.Fatalf("LoadSuspendedFromDisk: %v", err)
	}

	// Now point the server at the fresh kernel so client RPCs hit it.
	srv.kern = freshKern

	// Step 6: client.ListAllProcs() must return both UUIDs as Suspended.
	cli, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { cli.Close() })
	all, err := cli.ListAllProcs()
	if err != nil {
		t.Fatalf("Step 6: client.ListAllProcs: %v", err)
	}
	uuidStates := make(map[string]types.ProcessState)
	uuidReasons := make(map[string]string)
	for _, info := range all {
		uuidStates[info.UUID] = info.State
		uuidReasons[info.UUID] = info.SuspendReason
	}
	for label, u := range map[string]string{"parent": parentUUID, "child": childUUID} {
		st, ok := uuidStates[u]
		if !ok {
			t.Errorf("Step 6: %s UUID %s missing from ListAllProcs response", label, u)
			continue
		}
		if st != types.StateSuspended {
			t.Errorf("Step 6: %s state = %s, want Suspended (AC#6 dashboard orange)", label, st)
		}
		if uuidReasons[u] != "user_paused" {
			t.Errorf("Step 6: %s SuspendReason = %q, want %q", label, uuidReasons[u], "user_paused")
		}
	}

	// Step 7: assert no ReasonStep emitted in a 200ms window — daemon
	// restart must NOT auto-resume.
	parentProc, ok := freshKern.GetProcessByUUID(parentUUID)
	if !ok {
		t.Fatalf("Step 7: parent placeholder missing after LoadSuspendedFromDisk")
	}
	deadline := time.After(200 * time.Millisecond)
	for {
		done := false
		select {
		case ev := <-parentProc.DebugChan:
			if ev.Syscall == "ReasonStep" || ev.Syscall == "Spawn" {
				t.Errorf("Step 7: daemon-restart emitted %s (auto-resume forbidden); ev=%+v", ev.Syscall, ev)
			}
		case <-deadline:
			done = true
		}
		if done {
			break
		}
	}

	// Step 8: trigger explicit resume of the parent (emulates user dashboard R
	// or `rnix resume <parent.UUID>`).
	res, err := freshKern.ResumeWithOpts(parentUUID, kernel.ResumeOpts{})
	if err != nil {
		t.Fatalf("Step 8: ResumeWithOpts(parent): %v", err)
	}

	// Step 9: wait up to 1s for the subtree wakeup to land on the child.
	childProc, ok := freshKern.GetProcessByUUID(childUUID)
	if !ok {
		t.Fatalf("Step 9: child placeholder missing")
	}
	waitDeadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(waitDeadline) {
		if childProc.GetState() == types.StateRunning {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := childProc.GetState(); got != types.StateRunning {
		t.Errorf("Step 9: child state = %s, want Running (subtree wakeup expected per AC#4)", got)
	}

	// Step 10: smoke — non-fork resume preserves the parent UUID.
	if res.UUID != parentUUID {
		t.Errorf("Step 10: ResumeResult.UUID = %q, want %q (non-fork)", res.UUID, parentUUID)
	}
}

// readDiskProcInfoForE2E reads <baseDir>/data/steps/<uuid>/proc-info.json
// and returns it as a raw map so the e2e test can assert against JSON
// field names without depending on Go struct definitions (insulates the
// test from procInfoDisk evolution).
func readDiskProcInfoForE2E(t *testing.T, baseDir, uuid string) map[string]any {
	t.Helper()
	path := filepath.Join(baseDir, "data", "steps", uuid, "proc-info.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
	return raw
}
