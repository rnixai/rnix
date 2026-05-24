package ipc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
)

// =============================================================================
// ATDD 44.5 — Suspend during LLM Write must transition to Suspended, not Dead.
//
// Bug: Story 44.1 added a `suspendRequested` atomic protocol with checks in
// reasonStep at three locations (defer / WaitIfPaused / step-head), but missed
// the LLM Write/Read/unmarshal error paths. When suspendOneForSubtree cancels
// proc.ctx, the in-flight Write returns context.Canceled, reasonStep treats
// that as a normal write failure → attemptFallback → finishProcess(failed),
// killing the process before suspendProcess can transition state. The
// downstream wg.Wait observes Dead and the Suspend transition fails, leaving
// the disk snapshot with the contradictory tuple (state=dead,
// suspend_reason=user_paused, exit_reason="llm write failed").
//
// RED-PHASE shape: these tests reproduce the race deterministically using the
// mockLLMFile.parkOnWrite gate (Story 44.5 AC4). Until reason.go is fixed
// (Story 44.5 AC1), each test fails with state=Dead + Exit.Reason="llm write
// failed" / events.jsonl containing "ReasonStep action=error" /
// proc-info.json carrying the same contradiction.
//
// Setup notes:
//   - setupResumeIPCTest registers /dev/llm/claude with the mockLLMFile and
//     wires up a tempdir as step data dir (so proc-info.json / events.jsonl
//     land somewhere readable by the assertion phase).
//   - Spawn() (not SkipReasonLoop) drives a real reasonStep goroutine through
//     prompt build → marshal → vfs.Write(stepCtx, llmFD, reqJSON). The first
//     Write call is the parked one.
//   - SignalTree(pid, SIGPAUSE) routes through k.SuspendSubtree →
//     suspendOneForSubtree → proc.Cancel + proc.wg.Wait + suspendProcess. The
//     wg.Wait is synchronous so SignalTree returns only after the reasoning
//     goroutine has fully exited — assertions can run immediately afterwards.
//   - close(release) is deferred as a safety net; ctx.Done in mockLLMFile.Write
//     already releases the gate before the deferred close runs.
// =============================================================================

// waitForReached blocks until reachedCh closes or the timeout fires, failing
// the test with a contextual message on timeout so the failure points at the
// spawn-to-write race rather than the assertion phase.
func waitForReached(t *testing.T, reached <-chan struct{}, timeout time.Duration, hint string) {
	t.Helper()
	select {
	case <-reached:
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for %s (%s)", hint, timeout)
	}
}

// signalTreeResult bundles the (affected, error) tuple from SignalTree so it
// can travel through the timeout channel as a single value.
type signalTreeResult struct {
	affected int
	err      error
}

// signalTreeWithTimeout calls SignalTree on a goroutine so a hung
// suspendOneForSubtree (e.g. mock that does not respect ctx.Done) surfaces as
// a clean timeout rather than a deadlocked test.
func signalTreeWithTimeout(t *testing.T, k *kernel.KernelImpl, pid types.PID, sig types.Signal, timeout time.Duration) {
	t.Helper()
	done := make(chan signalTreeResult, 1)
	go func() {
		affected, err := k.SignalTree(pid, sig)
		done <- signalTreeResult{affected: affected, err: err}
	}()
	select {
	case res := <-done:
		if res.err != nil {
			t.Fatalf("SignalTree(pid=%d, %v): %v", pid, sig, res.err)
		}
	case <-time.After(timeout):
		t.Fatalf("SignalTree(pid=%d, %v) timed out after %s — likely a wg.Wait hang",
			pid, sig, timeout)
	}
}

// TestATDD_44_5_010_PauseDuringLLMWrite_TransitionsToSuspended
//
// AC5 main behavioural assertion (also covers AC1 — the reason.go fix is the
// only thing standing between this test passing and the EchoMatrix bug).
//
// RED-phase failure (pre-fix): proc.GetState() returns Dead because reason.go
// runs attemptFallback → finishProcess(failed) before suspendProcess can take
// over. The state assertion is the load-bearing one.
func TestATDD_44_5_010_PauseDuringLLMWrite_TransitionsToSuspended(t *testing.T) {
	_, kern, _, llmFile := setupResumeIPCTest(t)

	reached, release := llmFile.parkOnWrite()
	defer close(release)

	pid, err := kern.Spawn("atdd 44.5 — pause during write", nil, kernel.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	waitForReached(t, reached, 2*time.Second, "process to enter LLM Write")

	signalTreeWithTimeout(t, kern, pid, types.SIGPAUSE, 2*time.Second)

	proc, ok := kern.GetProcess(pid)
	if !ok {
		t.Fatalf("process pid=%d vanished after SignalTree", pid)
	}

	if got := proc.GetState(); got != types.StateSuspended {
		t.Errorf("after Pause-during-Write: proc.State = %s, want Suspended "+
			"(reason.go Write err path missing IsSuspendRequested check — Story 44.5 AC1)", got)
	}
	if got := proc.GetSuspendReason(); got != "user_paused" {
		t.Errorf("proc.SuspendReason = %q, want %q (suspendProcess never reached "+
			"because finishProcess won the race)", got, "user_paused")
	}
	if proc.Exit != nil {
		if reason := proc.Exit.Reason; strings.Contains(reason, "llm write failed") {
			t.Errorf("proc.Exit.Reason = %q must NOT contain \"llm write failed\" "+
				"— Pause semantics promise no kill", reason)
		} else if !strings.Contains(reason, "suspended") {
			t.Errorf("proc.Exit.Reason = %q, want substring \"suspended\"", reason)
		}
	}
}

// TestATDD_44_5_011_PauseDuringLLMWrite_EventsLogContainsSuspendNoWriteFailed
//
// AC5 events-log assertion. The events.jsonl must record the Suspend syscall
// and MUST NOT carry a ReasonStep action=error for the cancelled Write —
// because that error event is the on-disk smoking gun of the kill-instead-of-
// suspend bug.
//
// RED-phase failure: pre-fix the events file contains a ReasonStep entry
// shaped like
//
//	{"syscall":"ReasonStep","args":{"step":1,"action":"error"},"error":"llm write failed: ..."}
//
// and no Suspend event because state never reaches Suspended.
func TestATDD_44_5_011_PauseDuringLLMWrite_EventsLogContainsSuspendNoWriteFailed(t *testing.T) {
	_, kern, baseDir, llmFile := setupResumeIPCTest(t)

	reached, release := llmFile.parkOnWrite()
	defer close(release)

	pid, err := kern.Spawn("atdd 44.5 — events log shape", nil, kernel.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	waitForReached(t, reached, 2*time.Second, "process to enter LLM Write")

	signalTreeWithTimeout(t, kern, pid, types.SIGPAUSE, 2*time.Second)

	proc, ok := kern.GetProcess(pid)
	if !ok {
		t.Fatalf("process pid=%d vanished after SignalTree", pid)
	}

	eventsPath := filepath.Join(baseDir, "data", "steps", proc.UUID, "events.jsonl")
	events, err := kernel.ReadAllEvents(eventsPath)
	if err != nil {
		t.Fatalf("ReadAllEvents(%s): %v", eventsPath, err)
	}

	var sawSuspend bool
	var reasonStepErrors []string
	for _, ev := range events {
		if ev.Syscall == "Suspend" {
			sawSuspend = true
		}
		if ev.Syscall == "ReasonStep" {
			if action, _ := ev.Args["action"].(string); action == "error" {
				reasonStepErrors = append(reasonStepErrors, ev.Error)
			}
		}
	}

	if !sawSuspend {
		t.Errorf("events.jsonl missing Suspend event "+
			"— suspendProcess never reached because finishProcess ran first "+
			"(Story 44.5 AC1 fix not applied). events=%d", len(events))
	}
	for _, errMsg := range reasonStepErrors {
		if strings.Contains(errMsg, "llm write failed") ||
			strings.Contains(errMsg, "context canceled") {
			t.Errorf("events.jsonl contains forbidden ReasonStep action=error: %q "+
				"— Pause must not be recorded as a Write failure (Story 44.5 AC5)", errMsg)
		}
	}
}

// TestATDD_44_5_012_PauseDuringLLMWrite_ProcInfoDiskFieldsConsistent
//
// AC5 disk-side assertion + AC3 field-cleanup tie-in. After SIGPAUSE the
// proc-info.json snapshot persisted by suspendProcess must show
// state=suspended with a clean exit_reason (empty or "suspended: …"); it must
// NOT carry the EchoMatrix contradiction
//
//	state=dead + suspend_reason=user_paused + exit_reason="llm write failed".
//
// Additionally, ResumedFromStep must be 0 (AC2 — pause must not pre-fill the
// resume cursor). Because this test exercises the spawn path with
// LastCompletedStep=0 (no completed steps yet) the AC2 pre-fill condition
// `if last > 0` does not trigger pre-fix either — so this test's
// ResumedFromStep assertion is a green-guard. The 021 test asserts the
// post-deletion behaviour with LastCompletedStep > 0 where the RED is visible.
func TestATDD_44_5_012_PauseDuringLLMWrite_ProcInfoDiskFieldsConsistent(t *testing.T) {
	_, kern, baseDir, llmFile := setupResumeIPCTest(t)

	reached, release := llmFile.parkOnWrite()
	defer close(release)

	pid, err := kern.Spawn("atdd 44.5 — proc-info consistency", nil, kernel.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	waitForReached(t, reached, 2*time.Second, "process to enter LLM Write")

	signalTreeWithTimeout(t, kern, pid, types.SIGPAUSE, 2*time.Second)

	proc, ok := kern.GetProcess(pid)
	if !ok {
		t.Fatalf("process pid=%d vanished after SignalTree", pid)
	}

	infoPath := filepath.Join(baseDir, "data", "steps", proc.UUID, "proc-info.json")
	data, err := os.ReadFile(infoPath)
	if err != nil {
		t.Fatalf("read proc-info.json at %s: %v", infoPath, err)
	}

	// Decoded as map[string]any so the test does not couple to ProcInfo's Go
	// struct layout (Story 44.3 froze the schema — JSON tag names are the
	// stable surface here).
	var snap map[string]any
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatalf("unmarshal proc-info.json: %v", err)
	}

	// proc-info.json uses snake_case JSON keys (see procInfoDisk in
	// kernel/process_history.go) — assertions read by JSON tag name, not Go
	// field name.
	if got, _ := snap["state"].(string); got != "suspended" {
		// State serializes as the ProcessState String() value. The bug surfaces
		// here as "dead".
		t.Errorf("proc-info.json state = %q, want %q (Story 44.5 AC5 + AC1)", got, "suspended")
	}
	if got, _ := snap["suspend_reason"].(string); got != "user_paused" {
		t.Errorf("proc-info.json suspend_reason = %q, want %q", got, "user_paused")
	}
	if got, _ := snap["exit_reason"].(string); strings.Contains(got, "llm write failed") {
		t.Errorf("proc-info.json exit_reason = %q must NOT contain \"llm write failed\" "+
			"— pause must clear the kill-path exit string (Story 44.5 AC3)", got)
	}
	// AC2 green-guard for the LastCompletedStep=0 spawn path: resumed_from_step
	// must be 0 because pause must never pre-fill it (regardless of whether the
	// pre-fill code block was deleted or not, this case is naturally clean).
	// Note: omitempty drops the key entirely when zero, so a missing key reads
	// back as the float64 zero value — the assertion still holds.
	if got, _ := snap["resumed_from_step"].(float64); got != 0 {
		t.Errorf("proc-info.json resumed_from_step = %v, want 0 (Story 44.5 AC2 — "+
			"pause must not pre-fill the resume cursor)", got)
	}
}
