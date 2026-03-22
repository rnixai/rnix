package ipc

// =============================================================================
// ATDD Story 28.2: StepRecord 路径迁移到 UUID — IPC 路径解析
// TDD RED PHASE — All tests designed to FAIL until implementation exists
// =============================================================================
//
// Test Strategy:
//   AC-6: IPC 路径解析使用 UUID
//     - GetStepDetail resolves UUID path from proc.UUID (running process)
//     - ListSteps resolves UUID path from proc.UUID (running process)
//     - Fallback for reaped process tries UUID path before legacy PID
//     - Legacy PID path still works for backward compat
//
// Priority: P0 (IPC data path correctness)
// Test Level: Integration (IPC server + client roundtrip)

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
	"github.com/rnixai/rnix/vfs"
)

// ---------------------------------------------------------------------------
// AC-6: GetStepDetail — running process resolves via proc.UUID
// ---------------------------------------------------------------------------

func TestATDD_28_2_AC6_GetStepDetail_ResolvesUUIDPath(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "uuid get detail test", nil)
	_ = proc.Start()
	proc.SetFinalSystemPrompt("UUID test prompt")
	proc.SetNativeToolDefs([]vfs.ToolDef{
		{Name: "test_tool", Description: "Test tool"},
	})

	tmpDir := t.TempDir()
	srv.kern.SetStepDataDir(tmpDir)

	// Write steps to UUID directory (not PID directory)
	writeTestStepsUUID(t, tmpDir, proc.UUID, []types.StepRecord{
		testStepRecord(1),
		testStepRecord(2),
	})
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodGetStepDetail, GetStepDetailRequest{PID: proc.PID, Step: 1})

	// RED: Current resolveStepsPathFromProc uses PID, not UUID
	if !resp.OK {
		t.Fatalf("AC-6: GetStepDetail should resolve via UUID path: %+v", resp.Error)
	}

	var detail GetStepDetailResponse
	if err := json.Unmarshal(resp.Payload, &detail); err != nil {
		t.Fatalf("AC-6: unmarshal: %v", err)
	}
	if detail.Step != 1 {
		t.Errorf("AC-6: Step = %d, want 1", detail.Step)
	}
	if detail.SystemPrompt != "UUID test prompt" {
		t.Errorf("AC-6: SystemPrompt = %q, want %q", detail.SystemPrompt, "UUID test prompt")
	}
	if len(detail.Tools) != 1 || detail.Tools[0].Name != "test_tool" {
		t.Errorf("AC-6: Tools mismatch: got %+v", detail.Tools)
	}
}

func TestATDD_28_2_AC6_GetStepDetail_UUID_Step2(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "uuid step2 test", nil)
	_ = proc.Start()
	proc.SetFinalSystemPrompt("step2 prompt")

	tmpDir := t.TempDir()
	srv.kern.SetStepDataDir(tmpDir)

	writeTestStepsUUID(t, tmpDir, proc.UUID, []types.StepRecord{
		testStepRecord(1),
		testStepRecord(2),
		testStepRecord(3),
	})
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodGetStepDetail, GetStepDetailRequest{PID: proc.PID, Step: 2})

	if !resp.OK {
		t.Fatalf("AC-6: GetStepDetail step 2 via UUID path: %+v", resp.Error)
	}

	var detail GetStepDetailResponse
	if err := json.Unmarshal(resp.Payload, &detail); err != nil {
		t.Fatalf("AC-6: unmarshal: %v", err)
	}
	if detail.Step != 2 {
		t.Errorf("AC-6: Step = %d, want 2", detail.Step)
	}
}

// ---------------------------------------------------------------------------
// AC-6: ListSteps — running process resolves via proc.UUID
// ---------------------------------------------------------------------------

func TestATDD_28_2_AC6_ListSteps_ResolvesUUIDPath(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "uuid list test", nil)
	_ = proc.Start()

	tmpDir := t.TempDir()
	srv.kern.SetStepDataDir(tmpDir)

	writeTestStepsUUID(t, tmpDir, proc.UUID, []types.StepRecord{
		testStepRecord(1),
		testStepRecord(2),
		testStepRecord(3),
	})
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodListSteps, ListStepsRequest{PID: proc.PID})

	// RED: Current resolveStepsPathFromProc uses PID
	if !resp.OK {
		t.Fatalf("AC-6: ListSteps should resolve via UUID path: %+v", resp.Error)
	}

	var result ListStepsResponse
	if err := json.Unmarshal(resp.Payload, &result); err != nil {
		t.Fatalf("AC-6: unmarshal: %v", err)
	}
	if result.Total != 3 {
		t.Errorf("AC-6: Total = %d, want 3", result.Total)
	}
	if len(result.Steps) != 3 {
		t.Fatalf("AC-6: len(Steps) = %d, want 3", len(result.Steps))
	}
}

func TestATDD_28_2_AC6_ListSteps_UUID_Incremental(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "uuid incremental test", nil)
	_ = proc.Start()

	tmpDir := t.TempDir()
	srv.kern.SetStepDataDir(tmpDir)

	steps := make([]types.StepRecord, 10)
	for i := range steps {
		steps[i] = testStepRecord(i + 1)
	}
	writeTestStepsUUID(t, tmpDir, proc.UUID, steps)
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodListSteps, ListStepsRequest{PID: proc.PID, AfterStep: 7})

	if !resp.OK {
		t.Fatalf("AC-6: ListSteps incremental via UUID: %+v", resp.Error)
	}

	var result ListStepsResponse
	if err := json.Unmarshal(resp.Payload, &result); err != nil {
		t.Fatalf("AC-6: unmarshal: %v", err)
	}
	if result.Total != 10 {
		t.Errorf("AC-6: Total = %d, want 10", result.Total)
	}
	if len(result.Steps) != 3 {
		t.Fatalf("AC-6: len(Steps) = %d, want 3 (steps 8, 9, 10)", len(result.Steps))
	}
	if result.Steps[0].Step != 8 {
		t.Errorf("AC-6: first returned step = %d, want 8", result.Steps[0].Step)
	}
}

// ---------------------------------------------------------------------------
// AC-6: Fallback — reaped process (not in memory) uses UUID path
// ---------------------------------------------------------------------------

func TestATDD_28_2_AC6_Fallback_ReapedProcess_UUID(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	tmpDir := t.TempDir()
	srv.kern.SetStepDataDir(tmpDir)

	procUUID := "019576f2-1234-7def-8abc-def012345678"
	pid := types.PID(555)

	uuidDir := filepath.Join(tmpDir, "data", "steps", procUUID)
	if err := os.MkdirAll(uuidDir, 0o755); err != nil {
		t.Fatalf("AC-6: mkdir: %v", err)
	}

	meta := struct {
		PID          types.PID     `json:"pid"`
		SystemPrompt string        `json:"system_prompt"`
		ToolDefs     []vfs.ToolDef `json:"tool_defs"`
	}{
		PID:          pid,
		SystemPrompt: "Reaped UUID prompt",
		ToolDefs:     []vfs.ToolDef{{Name: "shell", Description: "Run command"}},
	}
	metaJSON, _ := json.Marshal(meta)
	if err := os.WriteFile(filepath.Join(uuidDir, "process-meta.json"), metaJSON, 0o644); err != nil {
		t.Fatalf("AC-6: write meta: %v", err)
	}

	writeTestStepsUUID(t, tmpDir, procUUID, []types.StepRecord{
		testStepRecord(1),
		testStepRecord(2),
	})

	// Story 28-3: Use UUID to query reaped process (PID-only returns NOT_FOUND)
	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodGetStepDetail, GetStepDetailRequest{UUID: procUUID, Step: 1})

	if !resp.OK {
		t.Fatalf("AC-6: reaped process should resolve via UUID fallback: %+v", resp.Error)
	}

	var detail GetStepDetailResponse
	if err := json.Unmarshal(resp.Payload, &detail); err != nil {
		t.Fatalf("AC-6: unmarshal: %v", err)
	}
	if detail.SystemPrompt != "Reaped UUID prompt" {
		t.Errorf("AC-6: SystemPrompt = %q, want %q", detail.SystemPrompt, "Reaped UUID prompt")
	}
	if len(detail.Tools) != 1 || detail.Tools[0].Name != "shell" {
		t.Errorf("AC-6: Tools mismatch: got %+v", detail.Tools)
	}
}

func TestATDD_28_2_AC6_Fallback_ReapedProcess_ListSteps_UUID(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	tmpDir := t.TempDir()
	srv.kern.SetStepDataDir(tmpDir)

	procUUID := "019576f3-5678-7abc-9012-abcdef012345"
	pid := types.PID(666)

	uuidDir := filepath.Join(tmpDir, "data", "steps", procUUID)
	if err := os.MkdirAll(uuidDir, 0o755); err != nil {
		t.Fatalf("AC-6: mkdir: %v", err)
	}

	meta := struct {
		PID types.PID `json:"pid"`
	}{PID: pid}
	metaJSON, _ := json.Marshal(meta)
	if err := os.WriteFile(filepath.Join(uuidDir, "process-meta.json"), metaJSON, 0o644); err != nil {
		t.Fatalf("AC-6: write meta: %v", err)
	}

	writeTestStepsUUID(t, tmpDir, procUUID, []types.StepRecord{
		testStepRecord(1),
		testStepRecord(2),
		testStepRecord(3),
	})

	// Story 28-3: Use UUID to query reaped process (PID-only returns NOT_FOUND)
	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodListSteps, ListStepsRequest{UUID: procUUID})
	if !resp.OK {
		t.Fatalf("AC-6: reaped ListSteps should resolve via UUID fallback: %+v", resp.Error)
	}

	var result ListStepsResponse
	if err := json.Unmarshal(resp.Payload, &result); err != nil {
		t.Fatalf("AC-6: unmarshal: %v", err)
	}
	if result.Total != 3 {
		t.Errorf("AC-6: Total = %d, want 3", result.Total)
	}
}

// ---------------------------------------------------------------------------
// AC-6 + AC-3: Legacy PID path still works (backward compat through IPC)
// ---------------------------------------------------------------------------

// Story 28-3: Legacy PID-only path for reaped processes now returns NOT_FOUND.
// Clients should use UUID for querying reaped processes.
func TestATDD_28_2_AC6_LegacyPIDPath_GetStepDetail_ReturnsNotFound(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	tmpDir := t.TempDir()
	srv.kern.SetStepDataDir(tmpDir)

	pid := types.PID(8888)
	stepsDir := filepath.Join(tmpDir, "data", "steps", fmt.Sprintf("%d", pid))
	if err := os.MkdirAll(stepsDir, 0o755); err != nil {
		t.Fatalf("AC-6: mkdir: %v", err)
	}

	writeTestSteps(t, tmpDir, pid, []types.StepRecord{
		testStepRecord(1),
	})

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodGetStepDetail, GetStepDetailRequest{PID: pid, Step: 1})

	// Story 28-3: PID-only query for reaped process returns NOT_FOUND
	if resp.OK {
		t.Fatal("AC-6: legacy PID-only path for reaped process should return NOT_FOUND after Story 28-3")
	}
	if resp.Error == nil || resp.Error.Code != "NOT_FOUND" {
		t.Errorf("AC-6: expected NOT_FOUND, got %+v", resp.Error)
	}
}

func TestATDD_28_2_AC6_LegacyPIDPath_ListSteps_ReturnsNotFound(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	tmpDir := t.TempDir()
	srv.kern.SetStepDataDir(tmpDir)

	pid := types.PID(7777)
	writeTestSteps(t, tmpDir, pid, []types.StepRecord{
		testStepRecord(1),
		testStepRecord(2),
	})

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodListSteps, ListStepsRequest{PID: pid})

	// Story 28-3: PID-only query for reaped process returns NOT_FOUND
	if resp.OK {
		t.Fatal("AC-6: legacy PID-only ListSteps for reaped process should return NOT_FOUND after Story 28-3")
	}
	if resp.Error == nil || resp.Error.Code != "NOT_FOUND" {
		t.Errorf("AC-6: expected NOT_FOUND, got %+v", resp.Error)
	}
}

// ---------------------------------------------------------------------------
// AC-6: Client method roundtrip via UUID path
// ---------------------------------------------------------------------------

func TestATDD_28_2_AC6_ClientRoundtrip_GetStepDetail_UUID(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "client uuid test", nil)
	_ = proc.Start()
	proc.SetFinalSystemPrompt("Client UUID prompt")
	proc.SetNativeToolDefs([]vfs.ToolDef{
		{Name: "fs", Description: "Filesystem"},
	})

	tmpDir := t.TempDir()
	srv.kern.SetStepDataDir(tmpDir)

	writeTestStepsUUID(t, tmpDir, proc.UUID, []types.StepRecord{
		testStepRecord(1),
	})
	srv.kern.AddProcess(proc)

	client, err := DialTimeout(sockPath, 3*time.Second)
	if err != nil {
		t.Fatalf("AC-6: dial: %v", err)
	}
	defer client.Close()

	detail, err := client.GetStepDetail(proc.PID, 1)
	if err != nil {
		t.Fatalf("AC-6: GetStepDetail client: %v", err)
	}
	if detail.SystemPrompt != "Client UUID prompt" {
		t.Errorf("AC-6: SystemPrompt = %q, want %q", detail.SystemPrompt, "Client UUID prompt")
	}
	if detail.Step != 1 {
		t.Errorf("AC-6: Step = %d, want 1", detail.Step)
	}
}

func TestATDD_28_2_AC6_ClientRoundtrip_ListSteps_UUID(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "client uuid list", nil)
	_ = proc.Start()

	tmpDir := t.TempDir()
	srv.kern.SetStepDataDir(tmpDir)

	writeTestStepsUUID(t, tmpDir, proc.UUID, []types.StepRecord{
		testStepRecord(1),
		testStepRecord(2),
	})
	srv.kern.AddProcess(proc)

	client, err := DialTimeout(sockPath, 3*time.Second)
	if err != nil {
		t.Fatalf("AC-6: dial: %v", err)
	}
	defer client.Close()

	result, err := client.ListSteps(proc.PID, 0)
	if err != nil {
		t.Fatalf("AC-6: ListSteps client: %v", err)
	}
	if result.Total != 2 {
		t.Errorf("AC-6: Total = %d, want 2", result.Total)
	}
	if len(result.Steps) != 2 {
		t.Errorf("AC-6: len(Steps) = %d, want 2", len(result.Steps))
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func writeTestStepsUUID(t *testing.T, baseDir string, procUUID string, records []types.StepRecord) {
	t.Helper()
	dir := filepath.Join(baseDir, "data", "steps", procUUID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("writeTestStepsUUID mkdir: %v", err)
	}
	f, err := os.Create(filepath.Join(dir, "steps.jsonl"))
	if err != nil {
		t.Fatalf("writeTestStepsUUID create: %v", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, rec := range records {
		if err := enc.Encode(rec); err != nil {
			t.Fatalf("writeTestStepsUUID encode: %v", err)
		}
	}
}
