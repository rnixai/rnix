package ipc

// =============================================================================
// ATDD Story 27.3: Dashboard Timeline Three-Level Detail
// TDD RED PHASE — All tests designed to FAIL until implementation exists
// =============================================================================
//
// Test Strategy:
//   AC-6: ProgressPayload extension — HasError + DurationMs fields
//   AC-8: ListSteps IPC method — wire types, server handler, client, perf
//
// Priority: P0 (core data pipeline for dashboard timeline)
// Test Level: Unit (serialization) + Integration (IPC server+client roundtrip)

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
)

// ---------------------------------------------------------------------------
// AC-6: ProgressPayload extension — HasError and DurationMs fields
// ---------------------------------------------------------------------------

func TestATDD_27_3_AC6_ProgressPayload_HasError_Field(t *testing.T) {
	pp := ProgressPayload{
		Event:    "step_complete",
		PID:      1,
		Step:     3,
		Action:   "tool_call",
		Summary:  "/dev/shell → go build",
		HasError: true,
	}
	data, err := json.Marshal(pp)
	if err != nil {
		t.Fatalf("AC-6: marshal ProgressPayload: %v", err)
	}
	var decoded ProgressPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("AC-6: unmarshal ProgressPayload: %v", err)
	}
	if !decoded.HasError {
		t.Errorf("AC-6: HasError = false, want true")
	}
}

func TestATDD_27_3_AC6_ProgressPayload_DurationMs_Field(t *testing.T) {
	pp := ProgressPayload{
		Event:      "step_complete",
		PID:        1,
		Step:       5,
		Action:     "tool_call",
		Summary:    "/dev/fs → read main.go",
		DurationMs: 1234.5,
	}
	data, err := json.Marshal(pp)
	if err != nil {
		t.Fatalf("AC-6: marshal ProgressPayload: %v", err)
	}
	var decoded ProgressPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("AC-6: unmarshal ProgressPayload: %v", err)
	}
	if decoded.DurationMs != 1234.5 {
		t.Errorf("AC-6: DurationMs = %f, want 1234.5", decoded.DurationMs)
	}
}

func TestATDD_27_3_AC6_ProgressPayload_OmitEmpty(t *testing.T) {
	pp := ProgressPayload{
		Event:   "step_complete",
		PID:     1,
		Step:    1,
		Action:  "complete",
		Summary: "done",
	}
	data, err := json.Marshal(pp)
	if err != nil {
		t.Fatalf("AC-6: marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("AC-6: unmarshal to map: %v", err)
	}
	if _, exists := raw["has_error"]; exists {
		t.Errorf("AC-6: has_error should be omitted when false, but is present in JSON")
	}
	if _, exists := raw["duration_ms"]; exists {
		t.Errorf("AC-6: duration_ms should be omitted when zero, but is present in JSON")
	}
}

func TestATDD_27_3_AC6_CallbackMux_OnStepComplete_HasError(t *testing.T) {
	mux := newCallbackMux()
	ch := make(chan StreamEvent, 1)
	mux.register(types.PID(1), ch)

	mux.OnStepComplete(types.PID(1), 3, "tool_call", "/dev/shell → go build", true, 1500.0)

	select {
	case ev := <-ch:
		var pp ProgressPayload
		if err := json.Unmarshal(ev.Payload, &pp); err != nil {
			t.Fatalf("AC-6: unmarshal event: %v", err)
		}
		if !pp.HasError {
			t.Errorf("AC-6: HasError = false, want true")
		}
		if pp.DurationMs != 1500.0 {
			t.Errorf("AC-6: DurationMs = %f, want 1500.0", pp.DurationMs)
		}
	default:
		t.Fatal("AC-6: no event sent by OnStepComplete")
	}
}

func TestATDD_27_3_AC6_KernelCallbacks_Extended_Signature(t *testing.T) {
	var _ kernel.KernelCallbacks = (*callbackMux)(nil)
}

// ---------------------------------------------------------------------------
// AC-8: ListSteps IPC method — wire types
// ---------------------------------------------------------------------------

func TestATDD_27_3_AC8_MethodConstant(t *testing.T) {
	if MethodListSteps != "list_steps" {
		t.Fatalf("AC-8: MethodListSteps = %q, want %q", MethodListSteps, "list_steps")
	}
}

func TestATDD_27_3_AC8_ListStepsRequest_Serialization(t *testing.T) {
	req := ListStepsRequest{PID: types.PID(42), AfterStep: 5}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("AC-8: marshal ListStepsRequest: %v", err)
	}
	var decoded ListStepsRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("AC-8: unmarshal ListStepsRequest: %v", err)
	}
	if decoded.PID != 42 {
		t.Errorf("AC-8: PID = %d, want 42", decoded.PID)
	}
	if decoded.AfterStep != 5 {
		t.Errorf("AC-8: AfterStep = %d, want 5", decoded.AfterStep)
	}
}

func TestATDD_27_3_AC8_ListStepsRequest_AfterStep_OmitEmpty(t *testing.T) {
	req := ListStepsRequest{PID: types.PID(1)}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("AC-8: marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("AC-8: unmarshal to map: %v", err)
	}
	if _, exists := raw["after_step"]; exists {
		t.Errorf("AC-8: after_step should be omitted when zero")
	}
}

func TestATDD_27_3_AC8_ListStepsResponse_Serialization(t *testing.T) {
	resp := ListStepsResponse{
		Steps: []StepSummaryWire{
			{Step: 1, Action: "tool_call", Summary: "/dev/fs → read", HasError: false, DurationMs: 218.0},
			{Step: 2, Action: "tool_call", Summary: "/dev/shell → go build", HasError: true, DurationMs: 1200.0},
			{Step: 3, Action: "complete", Summary: "done", HasError: false, DurationMs: 45.0},
		},
		Total: 3,
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("AC-8: marshal ListStepsResponse: %v", err)
	}
	var decoded ListStepsResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("AC-8: unmarshal ListStepsResponse: %v", err)
	}
	if len(decoded.Steps) != 3 {
		t.Fatalf("AC-8: len(Steps) = %d, want 3", len(decoded.Steps))
	}
	if decoded.Total != 3 {
		t.Errorf("AC-8: Total = %d, want 3", decoded.Total)
	}
	if decoded.Steps[1].HasError != true {
		t.Errorf("AC-8: Steps[1].HasError = false, want true")
	}
	if decoded.Steps[0].DurationMs != 218.0 {
		t.Errorf("AC-8: Steps[0].DurationMs = %f, want 218.0", decoded.Steps[0].DurationMs)
	}
}

func TestATDD_27_3_AC8_StepSummaryWire_Fields(t *testing.T) {
	s := StepSummaryWire{
		Step:       4,
		Action:     "plan",
		Summary:    "Created plan with 5 steps",
		HasError:   false,
		DurationMs: 320.0,
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("AC-8: marshal StepSummaryWire: %v", err)
	}
	var decoded StepSummaryWire
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("AC-8: unmarshal StepSummaryWire: %v", err)
	}
	if decoded.Step != 4 || decoded.Action != "plan" || decoded.Summary != "Created plan with 5 steps" {
		t.Errorf("AC-8: StepSummaryWire roundtrip mismatch: got %+v", decoded)
	}
}

// ---------------------------------------------------------------------------
// AC-8: ListSteps server handler — running process
// ---------------------------------------------------------------------------

func TestATDD_27_3_AC8_ServerHandler_AllSteps(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "list steps test", nil)
	_ = proc.Start()

	_, projBase := kernel.TestSetupDataDir(t, srv.kern)
	kernel.TestSetProjectConfig(proc)
	writeTestStepsUUID(t, projBase, proc.UUID, []types.StepRecord{
		testStepRecord(1),
		testStepRecord(2),
		testStepRecord(3),
	})
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodListSteps, ListStepsRequest{PID: proc.PID})

	if !resp.OK {
		t.Fatalf("AC-8: request failed: %+v", resp.Error)
	}

	var result ListStepsResponse
	if err := json.Unmarshal(resp.Payload, &result); err != nil {
		t.Fatalf("AC-8: unmarshal response: %v", err)
	}
	if result.Total != 3 {
		t.Errorf("AC-8: Total = %d, want 3", result.Total)
	}
	if len(result.Steps) != 3 {
		t.Fatalf("AC-8: len(Steps) = %d, want 3", len(result.Steps))
	}
	if result.Steps[0].Step != 1 {
		t.Errorf("AC-8: Steps[0].Step = %d, want 1", result.Steps[0].Step)
	}
	if result.Steps[0].Action != "tool_call" {
		t.Errorf("AC-8: Steps[0].Action = %q, want %q", result.Steps[0].Action, "tool_call")
	}
}

// ---------------------------------------------------------------------------
// AC-8: ListSteps server handler — incremental fetch with AfterStep
// ---------------------------------------------------------------------------

func TestATDD_27_3_AC8_ServerHandler_IncrementalFetch(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "incremental test", nil)
	_ = proc.Start()

	_, projBase := kernel.TestSetupDataDir(t, srv.kern)
	kernel.TestSetProjectConfig(proc)

	steps := make([]types.StepRecord, 10)
	for i := range steps {
		steps[i] = testStepRecord(i + 1)
	}
	writeTestStepsUUID(t, projBase, proc.UUID, steps)
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodListSteps, ListStepsRequest{PID: proc.PID, AfterStep: 7})

	if !resp.OK {
		t.Fatalf("AC-8: request failed: %+v", resp.Error)
	}

	var result ListStepsResponse
	if err := json.Unmarshal(resp.Payload, &result); err != nil {
		t.Fatalf("AC-8: unmarshal response: %v", err)
	}
	if result.Total != 10 {
		t.Errorf("AC-8: Total = %d, want 10", result.Total)
	}
	if len(result.Steps) != 3 {
		t.Fatalf("AC-8: len(Steps) = %d, want 3 (steps 8, 9, 10)", len(result.Steps))
	}
	if result.Steps[0].Step != 8 {
		t.Errorf("AC-8: first returned step = %d, want 8", result.Steps[0].Step)
	}
}

// ---------------------------------------------------------------------------
// AC-8: ListSteps server handler — HasError from ToolError
// ---------------------------------------------------------------------------

func TestATDD_27_3_AC8_ServerHandler_HasError_FromToolError(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "error step test", nil)
	_ = proc.Start()

	_, projBase := kernel.TestSetupDataDir(t, srv.kern)
	kernel.TestSetProjectConfig(proc)

	rec := testStepRecord(1)
	rec.ToolError = "exit status 1"
	writeTestStepsUUID(t, projBase, proc.UUID, []types.StepRecord{rec})
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodListSteps, ListStepsRequest{PID: proc.PID})

	if !resp.OK {
		t.Fatalf("AC-8: request failed: %+v", resp.Error)
	}

	var result ListStepsResponse
	if err := json.Unmarshal(resp.Payload, &result); err != nil {
		t.Fatalf("AC-8: unmarshal: %v", err)
	}
	if len(result.Steps) != 1 {
		t.Fatalf("AC-8: len(Steps) = %d, want 1", len(result.Steps))
	}
	if !result.Steps[0].HasError {
		t.Errorf("AC-8: HasError = false, want true (ToolError was non-empty)")
	}
}

// ---------------------------------------------------------------------------
// AC-8: ListSteps server handler — DurationMs from ToolDuration
// ---------------------------------------------------------------------------

func TestATDD_27_3_AC8_ServerHandler_DurationMs(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "duration test", nil)
	_ = proc.Start()

	_, projBase := kernel.TestSetupDataDir(t, srv.kern)
	kernel.TestSetProjectConfig(proc)

	rec := testStepRecord(1)
	rec.ToolDuration = 1500 * time.Millisecond
	writeTestStepsUUID(t, projBase, proc.UUID, []types.StepRecord{rec})
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodListSteps, ListStepsRequest{PID: proc.PID})

	if !resp.OK {
		t.Fatalf("AC-8: request failed: %+v", resp.Error)
	}

	var result ListStepsResponse
	if err := json.Unmarshal(resp.Payload, &result); err != nil {
		t.Fatalf("AC-8: unmarshal: %v", err)
	}
	if len(result.Steps) != 1 {
		t.Fatalf("AC-8: len(Steps) = %d, want 1", len(result.Steps))
	}
	if result.Steps[0].DurationMs < 1499.0 || result.Steps[0].DurationMs > 1501.0 {
		t.Errorf("AC-8: DurationMs = %f, want ~1500.0", result.Steps[0].DurationMs)
	}
}

// ---------------------------------------------------------------------------
// AC-8: ListSteps server handler — reaped process (disk-only)
// ---------------------------------------------------------------------------

func TestATDD_27_3_AC8_ServerHandler_ReapedProcess(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	_, projBase := kernel.TestSetupDataDir(t, srv.kern)

	// Story 28-3: Use UUID to query reaped process (PID-only returns NOT_FOUND)
	procUUID := "019576f5-ac08-7000-8000-ac0800000001"

	uuidDir := filepath.Join(projBase, "steps", procUUID)
	if err := os.MkdirAll(uuidDir, 0o755); err != nil {
		t.Fatalf("AC-8: mkdir: %v", err)
	}

	writeTestStepsUUID(t, projBase, procUUID, []types.StepRecord{
		testStepRecord(1),
		testStepRecord(2),
	})

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodListSteps, ListStepsRequest{UUID: procUUID})

	if !resp.OK {
		t.Fatalf("AC-8: request failed for reaped process: %+v", resp.Error)
	}

	var result ListStepsResponse
	if err := json.Unmarshal(resp.Payload, &result); err != nil {
		t.Fatalf("AC-8: unmarshal: %v", err)
	}
	if result.Total != 2 {
		t.Errorf("AC-8: Total = %d, want 2", result.Total)
	}
}

// ---------------------------------------------------------------------------
// AC-8: ListSteps server handler — PID not found
// ---------------------------------------------------------------------------

func TestATDD_27_3_AC8_ServerHandler_PIDNotFound(t *testing.T) {
	_, sockPath, _ := setupTestServer(t)

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodListSteps, ListStepsRequest{PID: types.PID(99999)})

	if resp.OK {
		t.Fatal("AC-8: expected error response for non-existent PID")
	}
	if resp.Error == nil {
		t.Fatal("AC-8: expected error payload")
	}
	if resp.Error.Code != "NOT_FOUND" {
		t.Errorf("AC-8: error code = %q, want %q", resp.Error.Code, "NOT_FOUND")
	}
}

// ---------------------------------------------------------------------------
// AC-8: ListSteps client method roundtrip
// ---------------------------------------------------------------------------

func TestATDD_27_3_AC8_ClientMethod(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "client list test", nil)
	_ = proc.Start()

	_, projBase := kernel.TestSetupDataDir(t, srv.kern)
	kernel.TestSetProjectConfig(proc)
	writeTestStepsUUID(t, projBase, proc.UUID, []types.StepRecord{
		testStepRecord(1),
		testStepRecord(2),
		testStepRecord(3),
	})
	srv.kern.AddProcess(proc)

	client, err := DialTimeout(sockPath, 3*time.Second)
	if err != nil {
		t.Fatalf("AC-8: dial: %v", err)
	}
	defer client.Close()

	result, err := client.ListSteps(proc.PID, 0)
	if err != nil {
		t.Fatalf("AC-8: ListSteps: %v", err)
	}
	if result.Total != 3 {
		t.Errorf("AC-8: Total = %d, want 3", result.Total)
	}
	if len(result.Steps) != 3 {
		t.Errorf("AC-8: len(Steps) = %d, want 3", len(result.Steps))
	}
}

// ---------------------------------------------------------------------------
// AC-8: ListSteps client method — incremental
// ---------------------------------------------------------------------------

func TestATDD_27_3_AC8_ClientMethod_Incremental(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "client incremental test", nil)
	_ = proc.Start()

	_, projBase := kernel.TestSetupDataDir(t, srv.kern)
	kernel.TestSetProjectConfig(proc)

	steps := make([]types.StepRecord, 5)
	for i := range steps {
		steps[i] = testStepRecord(i + 1)
	}
	writeTestStepsUUID(t, projBase, proc.UUID, steps)
	srv.kern.AddProcess(proc)

	client, err := DialTimeout(sockPath, 3*time.Second)
	if err != nil {
		t.Fatalf("AC-8: dial: %v", err)
	}
	defer client.Close()

	result, err := client.ListSteps(proc.PID, 3)
	if err != nil {
		t.Fatalf("AC-8: ListSteps: %v", err)
	}
	if result.Total != 5 {
		t.Errorf("AC-8: Total = %d, want 5", result.Total)
	}
	if len(result.Steps) != 2 {
		t.Errorf("AC-8: len(Steps) = %d, want 2 (steps 4, 5)", len(result.Steps))
	}
}

// ---------------------------------------------------------------------------
// AC-8: Performance — ≤200ms for 30-step file (NFR)
// ---------------------------------------------------------------------------

func TestATDD_27_3_AC8_Performance(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "perf test", nil)
	_ = proc.Start()

	_, projBase := kernel.TestSetupDataDir(t, srv.kern)
	kernel.TestSetProjectConfig(proc)

	steps := make([]types.StepRecord, 30)
	for i := range steps {
		steps[i] = testStepRecord(i + 1)
	}
	writeTestStepsUUID(t, projBase, proc.UUID, steps)
	srv.kern.AddProcess(proc)

	client, err := DialTimeout(sockPath, 3*time.Second)
	if err != nil {
		t.Fatalf("AC-8: dial: %v", err)
	}
	defer client.Close()

	start := time.Now()
	result, err := client.ListSteps(proc.PID, 0)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("AC-8: ListSteps: %v", err)
	}
	if result.Total != 30 {
		t.Errorf("AC-8: Total = %d, want 30", result.Total)
	}
	if elapsed > 200*time.Millisecond {
		t.Errorf("AC-8: elapsed %v exceeds 200ms limit", elapsed)
	}
}
