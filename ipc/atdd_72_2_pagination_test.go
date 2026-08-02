package ipc

// Story 72.2 AC10-3/4: IPC E2E tests for pagination and idx cache.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
	"github.com/rnixai/rnix/vfs"
)

// writeTestEventsUUID writes events.jsonl for a UUID under projBase.
func writeTestEventsUUID(t *testing.T, projBaseDir, procUUID string, events []kernel.SyscallEventDisk) {
	t.Helper()
	dir := filepath.Join(projBaseDir, "steps", procUUID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, ev := range events {
		if err := enc.Encode(ev); err != nil {
			t.Fatal(err)
		}
	}
}

// writeTestRawUUID writes raw.jsonl for a UUID under projBase.
func writeTestRawUUID(t *testing.T, projBaseDir, procUUID string, records []vfs.RawCapture) {
	t.Helper()
	dir := filepath.Join(projBaseDir, "steps", procUUID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, "raw.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, rec := range records {
		if err := enc.Encode(rec); err != nil {
			t.Fatal(err)
		}
	}
}

// --- list_steps pagination ---

func TestHandleListSteps_FullPath_Unchanged(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "pagination full test", nil)
	_ = proc.Start()

	_, projBase := kernel.TestSetupDataDir(t, srv.kern)
	kernel.TestSetProjectConfig(proc)

	records := make([]types.StepRecord, 5)
	for i := range 5 {
		records[i] = testStepRecord(i + 1)
	}
	writeTestStepsUUID(t, projBase, proc.UUID, records)
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodListSteps, ListStepsRequest{PID: proc.PID})
	if !resp.OK {
		t.Fatalf("request failed: %+v", resp.Error)
	}

	var result ListStepsResponse
	json.Unmarshal(resp.Payload, &result)
	if len(result.Steps) != 5 {
		t.Fatalf("len(Steps) = %d, want 5 (full path)", len(result.Steps))
	}
	if result.Total != 5 {
		t.Errorf("Total = %d, want 5", result.Total)
	}
}

func TestHandleListSteps_Pagination(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "pagination test", nil)
	_ = proc.Start()

	_, projBase := kernel.TestSetupDataDir(t, srv.kern)
	kernel.TestSetProjectConfig(proc)

	// 7 records, 5 unique steps (step 3 and 5 appear twice).
	records := []types.StepRecord{
		testStepRecord(1), testStepRecord(2), testStepRecord(3),
		testStepRecord(4), testStepRecord(5),
		testStepRecord(3), // duplicate step 3
		testStepRecord(5), // duplicate step 5
	}
	writeTestStepsUUID(t, projBase, proc.UUID, records)
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)

	// offset=2, limit=2 → deduped view [1,2,3,4,5], slice [3,4].
	resp := sendRequest(t, conn, MethodListSteps, ListStepsRequest{
		PID: proc.PID, Offset: 2, Limit: 2,
	})
	if !resp.OK {
		t.Fatalf("request failed: %+v", resp.Error)
	}

	var result ListStepsResponse
	json.Unmarshal(resp.Payload, &result)
	if len(result.Steps) != 2 {
		t.Fatalf("len(Steps) = %d, want 2", len(result.Steps))
	}
	if result.Steps[0].Step != 3 || result.Steps[1].Step != 4 {
		t.Errorf("got steps [%d, %d], want [3, 4]", result.Steps[0].Step, result.Steps[1].Step)
	}
	// Total = full file line count (7, including duplicates).
	if result.Total != 7 {
		t.Errorf("Total = %d, want 7 (full file lines incl. duplicates)", result.Total)
	}
}

func TestHandleListSteps_AfterStepWithPagination(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "afterstep+pagination test", nil)
	_ = proc.Start()

	_, projBase := kernel.TestSetupDataDir(t, srv.kern)
	kernel.TestSetProjectConfig(proc)

	records := make([]types.StepRecord, 6)
	for i := range 6 {
		records[i] = testStepRecord(i + 1)
	}
	writeTestStepsUUID(t, projBase, proc.UUID, records)
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)

	// afterStep=2 → view [3,4,5,6], offset=1 limit=2 → [4,5].
	resp := sendRequest(t, conn, MethodListSteps, ListStepsRequest{
		PID: proc.PID, AfterStep: 2, Offset: 1, Limit: 2,
	})
	if !resp.OK {
		t.Fatalf("request failed: %+v", resp.Error)
	}

	var result ListStepsResponse
	json.Unmarshal(resp.Payload, &result)
	if len(result.Steps) != 2 {
		t.Fatalf("len(Steps) = %d, want 2", len(result.Steps))
	}
	if result.Steps[0].Step != 4 || result.Steps[1].Step != 5 {
		t.Errorf("got steps [%d, %d], want [4, 5]", result.Steps[0].Step, result.Steps[1].Step)
	}
}

// --- list_events pagination ---

func TestHandleListEvents_Pagination(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "events pagination test", nil)
	_ = proc.Start()

	_, projBase := kernel.TestSetupDataDir(t, srv.kern)
	kernel.TestSetProjectConfig(proc)

	events := make([]kernel.SyscallEventDisk, 5)
	for i := range 5 {
		events[i] = kernel.SyscallEventDisk{
			TimestampMs: float64(i * 100),
			PID:         uint64(proc.PID),
			Syscall:     fmt.Sprintf("syscall_%d", i),
			DurationMs:  1.0,
		}
	}
	writeTestEventsUUID(t, projBase, proc.UUID, events)
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)

	// offset=1, limit=2 → events [1, 2].
	resp := sendRequest(t, conn, MethodListEvents, ListEventsRequest{
		PID: proc.PID, Offset: 1, Limit: 2,
	})
	if !resp.OK {
		t.Fatalf("request failed: %+v", resp.Error)
	}

	var result ListEventsResponse
	json.Unmarshal(resp.Payload, &result)
	if len(result.Events) != 2 {
		t.Fatalf("len(Events) = %d, want 2", len(result.Events))
	}
	if result.Events[0].Syscall != "syscall_1" || result.Events[1].Syscall != "syscall_2" {
		t.Errorf("got [%s, %s], want [syscall_1, syscall_2]",
			result.Events[0].Syscall, result.Events[1].Syscall)
	}
	if result.Total != 5 {
		t.Errorf("Total = %d, want 5", result.Total)
	}
}

func TestHandleListEvents_FullPath_Unchanged(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "events full test", nil)
	_ = proc.Start()

	_, projBase := kernel.TestSetupDataDir(t, srv.kern)
	kernel.TestSetProjectConfig(proc)

	events := make([]kernel.SyscallEventDisk, 3)
	for i := range 3 {
		events[i] = kernel.SyscallEventDisk{
			TimestampMs: float64(i * 100),
			PID:         uint64(proc.PID),
			Syscall:     "open",
			DurationMs:  1.0,
		}
	}
	writeTestEventsUUID(t, projBase, proc.UUID, events)
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodListEvents, ListEventsRequest{PID: proc.PID})
	if !resp.OK {
		t.Fatalf("request failed: %+v", resp.Error)
	}

	var result ListEventsResponse
	json.Unmarshal(resp.Payload, &result)
	if len(result.Events) != 3 {
		t.Fatalf("len(Events) = %d, want 3 (full path)", len(result.Events))
	}
}

// --- get_raw_capture pagination ---

func TestHandleGetRawCapture_Pagination(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "raw pagination test", nil)
	_ = proc.Start()

	_, projBase := kernel.TestSetupDataDir(t, srv.kern)
	kernel.TestSetProjectConfig(proc)

	rawRecords := make([]vfs.RawCapture, 4)
	for i := range 4 {
		rawRecords[i] = vfs.RawCapture{
			TsMs:    int64(i * 1000),
			Step:    i + 1,
			Kind:    "api",
			Request: map[string]any{"prompt": fmt.Sprintf("step %d", i+1)},
		}
	}
	writeTestRawUUID(t, projBase, proc.UUID, rawRecords)
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)

	// offset=0, limit=1 → first record only.
	resp := sendRequest(t, conn, MethodGetRawCapture, GetRawCaptureRequest{
		PID: proc.PID, Offset: 0, Limit: 1,
	})
	if !resp.OK {
		t.Fatalf("request failed: %+v", resp.Error)
	}

	var result GetRawCaptureResponse
	json.Unmarshal(resp.Payload, &result)
	if len(result.Records) != 1 {
		t.Fatalf("len(Records) = %d, want 1", len(result.Records))
	}
	if result.Records[0].Step != 1 {
		t.Errorf("Records[0].Step = %d, want 1", result.Records[0].Step)
	}
	if result.Total != 4 {
		t.Errorf("Total = %d, want 4", result.Total)
	}
}

func TestHandleGetRawCapture_StepPriorityOverPagination(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "raw step priority test", nil)
	_ = proc.Start()

	_, projBase := kernel.TestSetupDataDir(t, srv.kern)
	kernel.TestSetProjectConfig(proc)

	rawRecords := []vfs.RawCapture{
		{TsMs: 100, Step: 1, Kind: "api"},
		{TsMs: 200, Step: 2, Kind: "api"},
		{TsMs: 300, Step: 3, Kind: "api"},
	}
	writeTestRawUUID(t, projBase, proc.UUID, rawRecords)
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)

	// Step=2 + Offset/Limit → Step takes priority, pagination ignored.
	resp := sendRequest(t, conn, MethodGetRawCapture, GetRawCaptureRequest{
		PID: proc.PID, Step: 2, Offset: 0, Limit: 1,
	})
	if !resp.OK {
		t.Fatalf("request failed: %+v", resp.Error)
	}

	var result GetRawCaptureResponse
	json.Unmarshal(resp.Payload, &result)
	if len(result.Records) != 1 {
		t.Fatalf("len(Records) = %d, want 1 (Step>0 path)", len(result.Records))
	}
	if result.Records[0].Step != 2 {
		t.Errorf("Records[0].Step = %d, want 2 (Step>0 priority)", result.Records[0].Step)
	}
}

// --- get_step_detail via idx ---

func TestHandleGetStepDetail_ViaIdx(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "step detail idx test", nil)
	_ = proc.Start()

	_, projBase := kernel.TestSetupDataDir(t, srv.kern)
	kernel.TestSetProjectConfig(proc)

	records := make([]types.StepRecord, 3)
	for i := range 3 {
		records[i] = testStepRecord(i + 1)
	}
	writeTestStepsUUID(t, projBase, proc.UUID, records)

	// Build idx via RebuildIdx (dead-process rebuild; live processes skip, P4).
	stepsDir := filepath.Join(projBase, "steps", proc.UUID)
	kernel.RebuildIdx(filepath.Join(stepsDir, "steps.jsonl"), false)

	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodGetStepDetail, GetStepDetailRequest{
		PID: proc.PID, Step: 2,
	})
	if !resp.OK {
		t.Fatalf("request failed: %+v", resp.Error)
	}

	var result GetStepDetailResponse
	json.Unmarshal(resp.Payload, &result)
	if result.Step != 2 {
		t.Errorf("Step = %d, want 2", result.Step)
	}
	if result.Summary != "step 2 summary" {
		t.Errorf("Summary = %q, want 'step 2 summary'", result.Summary)
	}
}

// --- tail-follow cache ---

func TestHandleListSteps_CacheHitAndIncremental(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "cache test", nil)
	_ = proc.Start()

	_, projBase := kernel.TestSetupDataDir(t, srv.kern)
	kernel.TestSetProjectConfig(proc)

	stepsDir := filepath.Join(projBase, "steps", proc.UUID)
	os.MkdirAll(stepsDir, 0o755)
	stepsPath := filepath.Join(stepsDir, "steps.jsonl")

	// Write initial 3 records.
	records := make([]types.StepRecord, 3)
	for i := range 3 {
		records[i] = testStepRecord(i + 1)
	}
	writeTestStepsUUID(t, projBase, proc.UUID, records)
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)

	// First request: builds cache (fallback backfill, no idx yet).
	resp := sendRequest(t, conn, MethodListSteps, ListStepsRequest{PID: proc.PID})
	if !resp.OK {
		t.Fatalf("first request failed: %+v", resp.Error)
	}
	var r1 ListStepsResponse
	json.Unmarshal(resp.Payload, &r1)
	if len(r1.Steps) != 3 {
		t.Fatalf("first request: len = %d, want 3", len(r1.Steps))
	}

	// Second request: cache hit (jsonl unchanged) → same result.
	resp = sendRequest(t, conn, MethodListSteps, ListStepsRequest{PID: proc.PID})
	if !resp.OK {
		t.Fatalf("second request failed: %+v", resp.Error)
	}
	var r2 ListStepsResponse
	json.Unmarshal(resp.Payload, &r2)
	if len(r2.Steps) != 3 {
		t.Fatalf("second request: len = %d, want 3 (cache hit)", len(r2.Steps))
	}

	// Append 2 more records to jsonl (simulating live process writing).
	f, err := os.OpenFile(stepsPath, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	enc := json.NewEncoder(f)
	enc.Encode(testStepRecord(4))
	enc.Encode(testStepRecord(5))
	f.Close()

	// Third request: incremental merge → 5 steps.
	resp = sendRequest(t, conn, MethodListSteps, ListStepsRequest{PID: proc.PID})
	if !resp.OK {
		t.Fatalf("third request failed: %+v", resp.Error)
	}
	var r3 ListStepsResponse
	json.Unmarshal(resp.Payload, &r3)
	if len(r3.Steps) != 5 {
		t.Fatalf("third request: len = %d, want 5 (incremental)", len(r3.Steps))
	}
	if r3.Steps[4].Step != 5 {
		t.Errorf("last step = %d, want 5", r3.Steps[4].Step)
	}
}
