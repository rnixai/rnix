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
// ATDD 44.3 — AC#4: ResumeWithOpts on a daemon-restart-reloaded Suspended
// placeholder falls back from checkpoint-based to history-based resume when
// no checkpoint.json is present, and triggers ResumeSubtree on the newly
// resumed PID to wake up any disk-reloaded descendants.
//
// Story spec (44-3 §AC4):
//   - kernel/resume.go:228-247 ResumeWithOpts: the case types.StateSuspended
//     branch currently always calls resumeFromCheckpoint. Story 44.3 adds:
//       if checkpointExists(baseDir, uuid) {
//           return k.resumeFromCheckpoint(uuid, opts, start)
//       }
//       return k.resumeFromHistory(uuid, opts, start)
//     so a daemon-restart placeholder (which has no checkpoint.json — the
//     LoadSuspendedFromDisk path does not write one) falls back to history
//     replay, identical to the existing Dead/Zombie branch behaviour.
//   - resumeFromHistory after success calls k.ResumeSubtree(newPID) so any
//     ParentUUID-linked Suspended descendants in procTable wake up as part
//     of the same logical resume action.
//
// RED phase signal:
//   - compile-fail on kernel.KernelImpl.LoadSuspendedFromDisk (Task 3.1).
//   - behavioural-fail on ResumeWithOpts returning ErrNotFound (current
//     resumeFromCheckpoint fails because checkpoint.json is missing) instead
//     of falling back to history.
// =============================================================================

// ipcSuspendDiskInfo — local copy of the synthetic on-disk shape so this
// file does not depend on the kernel-package test helper (which is in the
// `kernel` package and not exported to `ipc`). Keeping the duplication
// scoped to one file is cheaper than introducing a shared testutil module.
type ipcSuspendDiskInfo struct {
	PID            uint64   `json:"pid"`
	UUID           string   `json:"uuid"`
	OriginUUID     string   `json:"origin_uuid,omitempty"`
	PPID           uint64   `json:"ppid"`
	ParentUUID     string   `json:"parent_uuid,omitempty"`
	State          string   `json:"state"`
	Intent         string   `json:"intent"`
	Skills         []string `json:"skills,omitempty"`
	TokensUsed     int      `json:"tokens_used"`
	MaxSteps       int      `json:"max_steps,omitempty"`
	CreatedAt      string   `json:"created_at"`
	DeadAt         string   `json:"dead_at,omitempty"`
	CtxID          uint64   `json:"ctx_id"`
	AllowedDevices []string `json:"allowed_devices,omitempty"`
	Provider       string   `json:"provider,omitempty"`
	Model          string   `json:"model,omitempty"`
	ContextWindow  int      `json:"context_window,omitempty"`
	SuspendReason  string   `json:"suspend_reason,omitempty"`
	PausedAt       string   `json:"paused_at,omitempty"`
	IsPaused       bool     `json:"is_paused,omitempty"`
	PausedTotalMs  int64    `json:"paused_total_ms,omitempty"`
}

func writeIPCSuspendFixture(t *testing.T, projBase string, info ipcSuspendDiskInfo, withSteps, withMeta bool) {
	t.Helper()
	dir := filepath.Join(projBase, "steps", info.UUID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	data, _ := json.MarshalIndent(info, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "proc-info.json"), data, 0o644); err != nil {
		t.Fatalf("write proc-info.json: %v", err)
	}
	if withSteps {
		stepLine := []byte(`{"step":1,"messages":[]}` + "\n")
		if err := os.WriteFile(filepath.Join(dir, "steps.jsonl"), stepLine, 0o644); err != nil {
			t.Fatalf("write steps.jsonl: %v", err)
		}
	}
	if withMeta {
		meta := map[string]any{
			"system_prompt": "You are a resumed test agent.",
			"tools":         []any{},
		}
		mb, _ := json.Marshal(meta)
		if err := os.WriteFile(filepath.Join(dir, "process-meta.json"), mb, 0o644); err != nil {
			t.Fatalf("write process-meta.json: %v", err)
		}
	}
}

// uuidIPCForTest mirrors kernel/atdd_44_3_helpers_test.go:uuidForTest so the
// fixture UUIDs satisfy isValidUUIDFormat.
func uuidIPCForTest(tag string) string {
	pad := func(s string, n int) string {
		if len(s) >= n {
			return s[:n]
		}
		out := make([]byte, n)
		copy(out, s)
		for i := len(s); i < n; i++ {
			out[i] = '0'
		}
		return string(out)
	}
	return pad(tag, 8) + "-" + pad("4433", 4) + "-" + pad("44a3", 4) + "-" + pad("44a3", 4) + "-" + pad(tag+"44a3", 12)
}

// TestATDD_44_3_040_ResumeAfterRestart_FallbackToHistoryWhenNoCheckpoint
//
// AC#4 main path: simulate a daemon restart by reloading a Suspended
// placeholder from disk, then call kernel.ResumeWithOpts. Because there is
// NO checkpoint.json (only steps.jsonl + proc-info.json + process-meta.json),
// the new resume routing must fall through to resumeFromHistory rather than
// fail with "checkpoint not found".
//
// Pre-Task-4 behaviour: ResumeWithOpts hits the StateSuspended branch and
// unconditionally calls resumeFromCheckpoint, which returns ErrNotFound.
// Post-Task-4 behaviour: the new checkpointExists() guard sends control into
// resumeFromHistory, which succeeds.
func TestATDD_44_3_040_ResumeAfterRestart_FallbackToHistoryWhenNoCheckpoint(t *testing.T) {
	srv, _, _ := setupTestServer(t)
	_, projBase := kernel.TestSetupDataDir(t, srv.kern)

	uuid := uuidIPCForTest("rar040")
	writeIPCSuspendFixture(t, projBase, ipcSuspendDiskInfo{
		PID:           42,
		UUID:          uuid,
		State:         "suspended",
		Intent:        "AC#4 fallback to history",
		Provider:      "claude",
		Model:         "claude-opus-4-7",
		MaxSteps:      100,
		CreatedAt:     time.Now().Add(-time.Hour).Format(time.RFC3339Nano),
		CtxID:         1,
		SuspendReason: "user_paused",
		IsPaused:      true,
	}, true, true)

	// Daemon-restart emulation: LoadSuspendedFromDisk pulls the placeholder
	// into procTable so the resume path goes through the StateSuspended
	// branch (not the "not in procTable, look up disk" branch).
	if _, err := srv.kern.LoadSuspendedFromDisk(); err != nil {
		t.Fatalf("LoadSuspendedFromDisk: %v", err)
	}

	res, err := srv.kern.ResumeWithOpts(uuid, kernel.ResumeOpts{})
	if err != nil {
		t.Fatalf("ResumeWithOpts on daemon-restart placeholder: %v (expected history fallback)", err)
	}
	if res == nil || res.UUID != uuid {
		t.Fatalf("ResumeResult.UUID = %v, want %q (non-fork resume preserves UUID)", res, uuid)
	}
	// resumeFromHistory allocates a fresh PID via NewProcess; we don't pin
	// the exact value, only that the result is non-zero and corresponds to
	// a live entry.
	if res.PID == 0 {
		t.Fatalf("ResumeResult.PID = 0, want a freshly-allocated PID")
	}
	if _, ok := srv.kern.GetProcess(res.PID); !ok {
		t.Errorf("PID %d returned by Resume is not in procTable", res.PID)
	}
}

// TestATDD_44_3_041_ResumeAfterRestart_TriggersSubtreeResume
//
// AC#4 secondary path: when resumeFromHistory completes on the parent, it
// must call k.ResumeSubtree(newPID) to wake up any disk-reloaded Suspended
// children that share ParentUUID with the just-resumed UUID. Without this,
// the user would have to manually resume each descendant after every daemon
// restart — defeating the "整子树恢复" guarantee from Epic 44 §AC-EA1.
//
// We construct a 2-process tree on disk (parent + child), reload both via
// LoadSuspendedFromDisk, then call ResumeWithOpts(parent.UUID) and assert
// the child transitions to Running.
func TestATDD_44_3_041_ResumeAfterRestart_TriggersSubtreeResume(t *testing.T) {
	// Build kernel inline (skip setupTestServer) so we can register a
	// parkOnRead-gated mockLLMFile instead of the noopLLMFile that
	// setupTestServer uses. Without the gate, the resumed parent and
	// subtree-woken child complete instantly on a Read returning nil,
	// transitioning Suspended → Running → Zombie between polls — exactly
	// the race that flaked this test on coverage-instrumented CI runners.
	mockFile := &mockLLMFile{}
	reached, release := mockFile.parkOnRead()
	defer close(release)

	devReg := vfs.NewDeviceRegistry()
	_ = devReg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return mockFile, nil
	})
	vfsInst := vfs.NewVFS(devReg)
	ctxMgr := rnixctx.NewManager()
	kern := kernel.NewKernel(vfsInst, ctxMgr, nil)
	t.Cleanup(kern.Shutdown)

	_, projBase := kernel.TestSetupDataDir(t, kern)

	parentUUID := uuidIPCForTest("par041")
	childUUID := uuidIPCForTest("chl041")

	writeIPCSuspendFixture(t, projBase, ipcSuspendDiskInfo{
		PID:           50,
		UUID:          parentUUID,
		State:         "suspended",
		Intent:        "AC#4 parent",
		Provider:      "claude",
		Model:         "claude-opus-4-7",
		MaxSteps:      100,
		CreatedAt:     time.Now().Add(-time.Hour).Format(time.RFC3339Nano),
		CtxID:         1,
		SuspendReason: "user_paused",
		IsPaused:      true,
	}, true, true)
	writeIPCSuspendFixture(t, projBase, ipcSuspendDiskInfo{
		PID:           51,
		UUID:          childUUID,
		PPID:          50,
		ParentUUID:    parentUUID,
		State:         "suspended",
		Intent:        "AC#4 child",
		Provider:      "claude",
		Model:         "claude-opus-4-7",
		MaxSteps:      100,
		CreatedAt:     time.Now().Add(-time.Hour).Format(time.RFC3339Nano),
		CtxID:         2,
		SuspendReason: "user_paused",
		IsPaused:      true,
	}, true, true)

	if _, err := kern.LoadSuspendedFromDisk(); err != nil {
		t.Fatalf("LoadSuspendedFromDisk: %v", err)
	}
	childProc, ok := kern.GetProcessByUUID(childUUID)
	if !ok {
		t.Fatal("child UUID not reloaded by LoadSuspendedFromDisk")
	}
	if got := childProc.GetState(); got != types.StateSuspended {
		t.Fatalf("child state before parent resume = %s, want Suspended", got)
	}

	if _, err := kern.ResumeWithOpts(parentUUID, kernel.ResumeOpts{}); err != nil {
		t.Fatalf("ResumeWithOpts(parent): %v", err)
	}

	// Wait for at least one process to enter LLM Read — confirms parent
	// (and shortly thereafter the subtree-resumed child) reached Running.
	select {
	case <-reached:
	case <-time.After(2 * time.Second):
		t.Fatalf("no process entered LLM Read within 2s — subtree wakeup may not have triggered")
	}

	// Now poll for the child specifically. Both processes are gated in
	// Read so they cannot escape Running until close(release) below.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if childProc.GetState() == types.StateRunning {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := childProc.GetState(); got != types.StateRunning {
		t.Errorf("child state after parent resume = %s, want Running (subtree wakeup expected per AC#4)", got)
	}
}
