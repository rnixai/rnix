package ipc

// HasError aggregation tests for handleListSteps.
//
// Background: StepRecord.ToolError historically captured only the last tool call
// in a parallel batch. When the last call succeeded but earlier calls in the same
// step failed (e.g. an LLM step that called Agent(failed) + Read(ok)), the step
// was rendered as clean in the Timeline pane — masking real failures.
//
// Fix: handleListSteps now also inspects StepRecord.ToolCalls. Any element with
// a non-empty Error contributes to StepSummaryWire.HasError. Old records that
// only have ToolError set still light up, and new records benefit from per-call
// aggregation without changing the ToolError field semantics.

import (
	"encoding/json"
	"testing"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
)

// Direct unit test of the hasToolCallError helper.
func TestHasToolCallError_EmptySlice(t *testing.T) {
	if hasToolCallError(nil) {
		t.Errorf("hasToolCallError(nil) = true, want false")
	}
	if hasToolCallError([]types.ToolCallRecord{}) {
		t.Errorf("hasToolCallError([]) = true, want false")
	}
}

func TestHasToolCallError_AllSucceeded(t *testing.T) {
	calls := []types.ToolCallRecord{
		{Name: "Read", Result: "ok"},
		{Name: "Edit", Result: "ok"},
		{Name: "Grep", Result: "match"},
	}
	if hasToolCallError(calls) {
		t.Errorf("hasToolCallError(all-success) = true, want false")
	}
}

func TestHasToolCallError_FirstFailedRest_Succeeded(t *testing.T) {
	// This is the failure pattern the fix exists for: a non-trailing call failed.
	calls := []types.ToolCallRecord{
		{Name: "Agent", Error: "Agent error: agent \"general-purpose\" load failed"},
		{Name: "Read", Result: "ok"},
		{Name: "Edit", Result: "ok"},
	}
	if !hasToolCallError(calls) {
		t.Errorf("hasToolCallError(first-failed) = false, want true")
	}
}

func TestHasToolCallError_MiddleFailed(t *testing.T) {
	calls := []types.ToolCallRecord{
		{Name: "Read", Result: "ok"},
		{Name: "Bash", Error: "Tool error: exit status 1"},
		{Name: "Edit", Result: "ok"},
	}
	if !hasToolCallError(calls) {
		t.Errorf("hasToolCallError(middle-failed) = false, want true")
	}
}

func TestHasToolCallError_LastFailed(t *testing.T) {
	calls := []types.ToolCallRecord{
		{Name: "Read", Result: "ok"},
		{Name: "Bash", Error: "Tool error: exit status 1"},
	}
	if !hasToolCallError(calls) {
		t.Errorf("hasToolCallError(last-failed) = false, want true")
	}
}

// End-to-end test: a StepRecord with empty ToolError but a failed call in
// ToolCalls must surface as HasError=true through the IPC layer. This is the
// EchoMatrix bmad-autopilot scenario condensed to a single step record.
func TestHandleListSteps_HasError_AggregatesFromToolCalls(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "haserror aggregation test", nil)
	_ = proc.Start()

	tmpDir := t.TempDir()
	srv.kern.SetStepDataDir(tmpDir)

	rec := testStepRecord(1)
	rec.ToolError = "" // last call succeeded
	rec.ToolCalls = []types.ToolCallRecord{
		{Name: "Agent", Path: "spawn", Error: "Agent error: agent \"general-purpose\" load failed"},
		{Name: "Read", Path: "/dev/fs", Result: "ok"},
	}
	writeTestStepsUUID(t, tmpDir, proc.UUID, []types.StepRecord{rec})
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodListSteps, ListStepsRequest{PID: proc.PID})

	if !resp.OK {
		t.Fatalf("request failed: %+v", resp.Error)
	}

	var result ListStepsResponse
	if err := json.Unmarshal(resp.Payload, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(result.Steps) != 1 {
		t.Fatalf("len(Steps) = %d, want 1", len(result.Steps))
	}
	if !result.Steps[0].HasError {
		t.Errorf("HasError = false, want true (ToolError empty but a call had Error)")
	}
}

// Regression: when both ToolError and ToolCalls signal an error, HasError
// stays true (no double-negative, no panic).
func TestHandleListSteps_HasError_BothSources(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "haserror both test", nil)
	_ = proc.Start()

	tmpDir := t.TempDir()
	srv.kern.SetStepDataDir(tmpDir)

	rec := testStepRecord(1)
	rec.ToolError = "last call failed"
	rec.ToolCalls = []types.ToolCallRecord{
		{Name: "Bash", Path: "/dev/shell", Error: "exit 1"},
		{Name: "Bash", Path: "/dev/shell", Error: "last call failed"},
	}
	writeTestStepsUUID(t, tmpDir, proc.UUID, []types.StepRecord{rec})
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodListSteps, ListStepsRequest{PID: proc.PID})

	if !resp.OK {
		t.Fatalf("request failed: %+v", resp.Error)
	}

	var result ListStepsResponse
	if err := json.Unmarshal(resp.Payload, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !result.Steps[0].HasError {
		t.Errorf("HasError = false, want true")
	}
}

// Negative: empty ToolError + all-success ToolCalls → HasError must remain false
// so successful steps don't get falsely flagged.
func TestHandleListSteps_HasError_NoneWhenAllSucceed(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "haserror none test", nil)
	_ = proc.Start()

	tmpDir := t.TempDir()
	srv.kern.SetStepDataDir(tmpDir)

	rec := testStepRecord(1)
	rec.ToolError = ""
	rec.ToolCalls = []types.ToolCallRecord{
		{Name: "Read", Path: "/dev/fs", Result: "file contents"},
		{Name: "Edit", Path: "/dev/fs", Result: "edited"},
	}
	writeTestStepsUUID(t, tmpDir, proc.UUID, []types.StepRecord{rec})
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodListSteps, ListStepsRequest{PID: proc.PID})

	if !resp.OK {
		t.Fatalf("request failed: %+v", resp.Error)
	}

	var result ListStepsResponse
	if err := json.Unmarshal(resp.Payload, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.Steps[0].HasError {
		t.Errorf("HasError = true, want false (all calls succeeded)")
	}
}
