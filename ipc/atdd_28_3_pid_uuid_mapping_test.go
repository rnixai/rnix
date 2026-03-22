package ipc

// =============================================================================
// ATDD Story 28.3: IPC PID→UUID 映射
// TDD RED PHASE — All tests designed to FAIL until implementation exists
// =============================================================================
//
// Test Strategy:
//   AC-1: GetStepDetail PID query maps to UUID internally
//   AC-2: GetStepDetail UUID query skips PID mapping
//   AC-3: GetStepDetailRequest has UUID field (compile-time)
//   AC-4: PID query for reaped process returns not_found
//   AC-5: UUID query for reaped process reads from disk
//   AC-6: Other IPC methods support PID or UUID uniformly
//
// Priority: P0 (IPC query path correctness)
// Test Level: Unit + Integration (IPC server + client roundtrip)

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
	"github.com/rnixai/rnix/vfs"
)

// ===========================================================================
// AC-3: GetStepDetailRequest 新增 UUID 字段（编译期断言）
// ===========================================================================

func TestATDD_28_3_AC3_GetStepDetailRequest_HasUUIDField(t *testing.T) {
	// RED: GetStepDetailRequest does not have UUID field yet
	req := GetStepDetailRequest{
		PID:  1,
		UUID: "019576f2-0000-7000-8000-000000000001",
		Step: 1,
	}
	if req.UUID == "" {
		t.Fatal("AC-3: GetStepDetailRequest.UUID should be set")
	}
}

func TestATDD_28_3_AC3_ListStepsRequest_HasUUIDField(t *testing.T) {
	// RED: ListStepsRequest does not have UUID field yet
	req := ListStepsRequest{
		PID:  1,
		UUID: "019576f2-0000-7000-8000-000000000002",
	}
	if req.UUID == "" {
		t.Fatal("AC-3: ListStepsRequest.UUID should be set")
	}
}

func TestATDD_28_3_AC3_GetProcDetailRequest_HasUUIDField(t *testing.T) {
	// RED: GetProcDetailRequest does not have UUID field yet
	req := GetProcDetailRequest{
		PID:  1,
		UUID: "019576f2-0000-7000-8000-000000000003",
	}
	if req.UUID == "" {
		t.Fatal("AC-3: GetProcDetailRequest.UUID should be set")
	}
}

func TestATDD_28_3_AC3_KillRequest_HasUUIDField(t *testing.T) {
	// RED: KillRequest does not have UUID field yet
	req := KillRequest{
		PID:    1,
		UUID:   "019576f2-0000-7000-8000-000000000004",
		Signal: types.SIGTERM,
	}
	if req.UUID == "" {
		t.Fatal("AC-3: KillRequest.UUID should be set")
	}
}

func TestATDD_28_3_AC3_AttachDebugRequest_HasUUIDField(t *testing.T) {
	// RED: AttachDebugRequest does not have UUID field yet
	req := AttachDebugRequest{
		PID:  1,
		UUID: "019576f2-0000-7000-8000-000000000005",
	}
	if req.UUID == "" {
		t.Fatal("AC-3: AttachDebugRequest.UUID should be set")
	}
}

func TestATDD_28_3_AC3_UUID_JSON_OmitEmpty(t *testing.T) {
	// UUID field should be omitted from JSON when empty (backward compat)
	req := GetStepDetailRequest{PID: 1, Step: 1}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("AC-3: marshal: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("AC-3: unmarshal raw: %v", err)
	}
	if _, ok := raw["uuid"]; ok {
		t.Fatal("AC-3: UUID field should be omitted when empty (omitempty)")
	}
}

func TestATDD_28_3_AC3_UUID_Priority_Over_PID(t *testing.T) {
	// When both PID and UUID are provided, UUID takes priority
	req := GetStepDetailRequest{
		PID:  999,
		UUID: "019576f2-0000-7000-8000-000000000099",
		Step: 1,
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("AC-3: marshal: %v", err)
	}
	var decoded GetStepDetailRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("AC-3: unmarshal: %v", err)
	}
	if decoded.UUID != req.UUID {
		t.Errorf("AC-3: UUID = %q, want %q", decoded.UUID, req.UUID)
	}
	if decoded.PID != req.PID {
		t.Errorf("AC-3: PID = %d, want %d", decoded.PID, req.PID)
	}
}

// ===========================================================================
// AC-1: GetStepDetail PID 查询存活进程（PID→UUID 映射）
// ===========================================================================

func TestATDD_28_3_AC1_GetStepDetail_PID_LiveProcess(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "pid-to-uuid mapping test", nil)
	_ = proc.Start()
	proc.SetFinalSystemPrompt("AC1 test prompt")
	proc.SetNativeToolDefs([]vfs.ToolDef{
		{Name: "test_tool", Description: "Test tool"},
	})

	tmpDir := t.TempDir()
	srv.kern.SetStepDataDir(tmpDir)

	// Write steps under UUID directory
	writeTestStepsUUID(t, tmpDir, proc.UUID, []types.StepRecord{
		testStepRecord(1),
		testStepRecord(2),
	})
	srv.kern.AddProcess(proc)

	// Query by PID — server should internally map PID→UUID
	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodGetStepDetail, GetStepDetailRequest{PID: proc.PID, Step: 1})

	if !resp.OK {
		t.Fatalf("AC-1: GetStepDetail by PID should map to UUID path: %+v", resp.Error)
	}

	var detail GetStepDetailResponse
	if err := json.Unmarshal(resp.Payload, &detail); err != nil {
		t.Fatalf("AC-1: unmarshal: %v", err)
	}
	if detail.Step != 1 {
		t.Errorf("AC-1: Step = %d, want 1", detail.Step)
	}
	if detail.SystemPrompt != "AC1 test prompt" {
		t.Errorf("AC-1: SystemPrompt = %q, want %q", detail.SystemPrompt, "AC1 test prompt")
	}
}

// ===========================================================================
// AC-2: GetStepDetail UUID 直接查询存活进程
// ===========================================================================

func TestATDD_28_3_AC2_GetStepDetail_UUID_LiveProcess(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "uuid direct query test", nil)
	_ = proc.Start()
	proc.SetFinalSystemPrompt("AC2 UUID direct")
	proc.SetNativeToolDefs([]vfs.ToolDef{
		{Name: "fs", Description: "Filesystem"},
	})

	tmpDir := t.TempDir()
	srv.kern.SetStepDataDir(tmpDir)

	writeTestStepsUUID(t, tmpDir, proc.UUID, []types.StepRecord{
		testStepRecord(1),
		testStepRecord(2),
		testStepRecord(3),
	})
	srv.kern.AddProcess(proc)

	// RED: GetStepDetailRequest.UUID field does not exist yet
	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodGetStepDetail, GetStepDetailRequest{
		UUID: proc.UUID,
		Step: 2,
	})

	if !resp.OK {
		t.Fatalf("AC-2: GetStepDetail by UUID should skip PID mapping: %+v", resp.Error)
	}

	var detail GetStepDetailResponse
	if err := json.Unmarshal(resp.Payload, &detail); err != nil {
		t.Fatalf("AC-2: unmarshal: %v", err)
	}
	if detail.Step != 2 {
		t.Errorf("AC-2: Step = %d, want 2", detail.Step)
	}
	if detail.SystemPrompt != "AC2 UUID direct" {
		t.Errorf("AC-2: SystemPrompt = %q, want %q", detail.SystemPrompt, "AC2 UUID direct")
	}
}

func TestATDD_28_3_AC2_ListSteps_UUID_LiveProcess(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "uuid list steps test", nil)
	_ = proc.Start()

	tmpDir := t.TempDir()
	srv.kern.SetStepDataDir(tmpDir)

	writeTestStepsUUID(t, tmpDir, proc.UUID, []types.StepRecord{
		testStepRecord(1),
		testStepRecord(2),
		testStepRecord(3),
	})
	srv.kern.AddProcess(proc)

	// RED: ListStepsRequest.UUID field does not exist yet
	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodListSteps, ListStepsRequest{UUID: proc.UUID})

	if !resp.OK {
		t.Fatalf("AC-2: ListSteps by UUID should work: %+v", resp.Error)
	}

	var result ListStepsResponse
	if err := json.Unmarshal(resp.Payload, &result); err != nil {
		t.Fatalf("AC-2: unmarshal: %v", err)
	}
	if result.Total != 3 {
		t.Errorf("AC-2: Total = %d, want 3", result.Total)
	}
}

// ===========================================================================
// AC-4: PID 查询已 reap 进程返回 not_found
// ===========================================================================

func TestATDD_28_3_AC4_GetStepDetail_PID_ReapedProcess_NotFound(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	tmpDir := t.TempDir()
	srv.kern.SetStepDataDir(tmpDir)

	// Write steps under a UUID directory with process-meta containing PID
	procUUID := "019576f4-aaaa-7000-8000-aaaaaaaaaaaa"
	pid := types.PID(12345)

	uuidDir := filepath.Join(tmpDir, "data", "steps", procUUID)
	if err := os.MkdirAll(uuidDir, 0o755); err != nil {
		t.Fatalf("AC-4: mkdir: %v", err)
	}

	meta := struct {
		PID          types.PID     `json:"pid"`
		SystemPrompt string        `json:"system_prompt"`
		ToolDefs     []vfs.ToolDef `json:"tool_defs"`
	}{
		PID:          pid,
		SystemPrompt: "Reaped process prompt",
	}
	metaJSON, _ := json.Marshal(meta)
	if err := os.WriteFile(filepath.Join(uuidDir, "process-meta.json"), metaJSON, 0o644); err != nil {
		t.Fatalf("AC-4: write meta: %v", err)
	}

	writeTestStepsUUID(t, tmpDir, procUUID, []types.StepRecord{
		testStepRecord(1),
	})

	// Process NOT in memory — query by PID should return not_found
	// RED: Current behavior uses resolveStepsPathFallback which scans UUID dirs
	//      New behavior: PID query for reaped process → not_found
	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodGetStepDetail, GetStepDetailRequest{PID: pid, Step: 1})

	if resp.OK {
		t.Fatal("AC-4: PID query for reaped process should return not_found, got OK")
	}
	if resp.Error == nil {
		t.Fatal("AC-4: expected error payload")
	}
	if resp.Error.Code != "NOT_FOUND" {
		t.Errorf("AC-4: error code = %q, want %q", resp.Error.Code, "NOT_FOUND")
	}
}

func TestATDD_28_3_AC4_ListSteps_PID_ReapedProcess_NotFound(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	tmpDir := t.TempDir()
	srv.kern.SetStepDataDir(tmpDir)

	procUUID := "019576f4-bbbb-7000-8000-bbbbbbbbbbbb"
	pid := types.PID(12346)

	uuidDir := filepath.Join(tmpDir, "data", "steps", procUUID)
	if err := os.MkdirAll(uuidDir, 0o755); err != nil {
		t.Fatalf("AC-4: mkdir: %v", err)
	}
	meta := struct {
		PID types.PID `json:"pid"`
	}{PID: pid}
	metaJSON, _ := json.Marshal(meta)
	if err := os.WriteFile(filepath.Join(uuidDir, "process-meta.json"), metaJSON, 0o644); err != nil {
		t.Fatalf("AC-4: write meta: %v", err)
	}
	writeTestStepsUUID(t, tmpDir, procUUID, []types.StepRecord{
		testStepRecord(1),
		testStepRecord(2),
	})

	// PID query for reaped process → not_found
	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodListSteps, ListStepsRequest{PID: pid})

	if resp.OK {
		t.Fatal("AC-4: PID ListSteps for reaped process should return not_found, got OK")
	}
	if resp.Error == nil {
		t.Fatal("AC-4: expected error payload")
	}
	if resp.Error.Code != "NOT_FOUND" {
		t.Errorf("AC-4: error code = %q, want %q", resp.Error.Code, "NOT_FOUND")
	}
}

// ===========================================================================
// AC-5: UUID 查询已 reap 进程从磁盘读取
// ===========================================================================

func TestATDD_28_3_AC5_GetStepDetail_UUID_ReapedProcess_DiskRead(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	tmpDir := t.TempDir()
	srv.kern.SetStepDataDir(tmpDir)

	procUUID := "019576f4-cccc-7000-8000-cccccccccccc"

	uuidDir := filepath.Join(tmpDir, "data", "steps", procUUID)
	if err := os.MkdirAll(uuidDir, 0o755); err != nil {
		t.Fatalf("AC-5: mkdir: %v", err)
	}

	meta := struct {
		PID          types.PID     `json:"pid"`
		SystemPrompt string        `json:"system_prompt"`
		ToolDefs     []vfs.ToolDef `json:"tool_defs"`
	}{
		PID:          types.PID(99999),
		SystemPrompt: "Reaped UUID disk prompt",
		ToolDefs:     []vfs.ToolDef{{Name: "shell", Description: "Run command"}},
	}
	metaJSON, _ := json.Marshal(meta)
	if err := os.WriteFile(filepath.Join(uuidDir, "process-meta.json"), metaJSON, 0o644); err != nil {
		t.Fatalf("AC-5: write meta: %v", err)
	}

	writeTestStepsUUID(t, tmpDir, procUUID, []types.StepRecord{
		testStepRecord(1),
		testStepRecord(2),
	})

	// Process NOT in memory — query by UUID should read from disk
	// RED: handleGetStepDetail does not support UUID query yet
	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodGetStepDetail, GetStepDetailRequest{
		UUID: procUUID,
		Step: 1,
	})

	if !resp.OK {
		t.Fatalf("AC-5: UUID query for reaped process should read from disk: %+v", resp.Error)
	}

	var detail GetStepDetailResponse
	if err := json.Unmarshal(resp.Payload, &detail); err != nil {
		t.Fatalf("AC-5: unmarshal: %v", err)
	}
	if detail.SystemPrompt != "Reaped UUID disk prompt" {
		t.Errorf("AC-5: SystemPrompt = %q, want %q", detail.SystemPrompt, "Reaped UUID disk prompt")
	}
	if len(detail.Tools) != 1 || detail.Tools[0].Name != "shell" {
		t.Errorf("AC-5: Tools mismatch: got %+v", detail.Tools)
	}
}

func TestATDD_28_3_AC5_ListSteps_UUID_ReapedProcess_DiskRead(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	tmpDir := t.TempDir()
	srv.kern.SetStepDataDir(tmpDir)

	procUUID := "019576f4-dddd-7000-8000-dddddddddddd"

	uuidDir := filepath.Join(tmpDir, "data", "steps", procUUID)
	if err := os.MkdirAll(uuidDir, 0o755); err != nil {
		t.Fatalf("AC-5: mkdir: %v", err)
	}

	writeTestStepsUUID(t, tmpDir, procUUID, []types.StepRecord{
		testStepRecord(1),
		testStepRecord(2),
		testStepRecord(3),
	})

	// UUID query for reaped process → read from disk
	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodListSteps, ListStepsRequest{UUID: procUUID})

	if !resp.OK {
		t.Fatalf("AC-5: UUID ListSteps for reaped process should read from disk: %+v", resp.Error)
	}

	var result ListStepsResponse
	if err := json.Unmarshal(resp.Payload, &result); err != nil {
		t.Fatalf("AC-5: unmarshal: %v", err)
	}
	if result.Total != 3 {
		t.Errorf("AC-5: Total = %d, want 3", result.Total)
	}
}

// ===========================================================================
// AC-6: 其他 IPC 方法统一支持 UUID — Kill
// ===========================================================================

func TestATDD_28_3_AC6_Kill_ByUUID_LiveProcess(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "kill by uuid test", nil)
	_ = proc.Start()
	srv.kern.AddProcess(proc)

	// RED: KillRequest does not have UUID field; handleKill does not use resolveProcess
	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodKill, KillRequest{
		UUID:   proc.UUID,
		Signal: types.SIGTERM,
	})

	if !resp.OK {
		t.Fatalf("AC-6: Kill by UUID should work: %+v", resp.Error)
	}
}

func TestATDD_28_3_AC6_Kill_ByUUID_NotFound(t *testing.T) {
	_, sockPath, _ := setupTestServer(t)

	// UUID not in memory → not_found
	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodKill, KillRequest{
		UUID:   "019576f4-eeee-7000-8000-eeeeeeeeeeee",
		Signal: types.SIGTERM,
	})

	if resp.OK {
		t.Fatal("AC-6: Kill by non-existent UUID should fail")
	}
}

// ===========================================================================
// AC-6: 其他 IPC 方法统一支持 UUID — AttachDebug
// ===========================================================================

func TestATDD_28_3_AC6_AttachDebug_ByUUID(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "attach debug uuid test", nil)
	_ = proc.Start()

	srv.kern.AddProcess(proc)

	// Close the debug channel soon so the streaming handler returns
	go func() {
		time.Sleep(50 * time.Millisecond)
		close(proc.DebugChan)
	}()

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodAttachDebug, AttachDebugRequest{
		UUID: proc.UUID,
	})

	// AttachDebug is a streaming method; just verify the initial handshake succeeds
	if !resp.OK {
		t.Fatalf("AC-6: AttachDebug by UUID should work: %+v", resp.Error)
	}
	conn.Close()
}

// ===========================================================================
// AC-6: GetProcDetail 支持 UUID
// ===========================================================================

func TestATDD_28_3_AC6_GetProcDetail_ByUUID(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "proc detail uuid test", nil)
	_ = proc.Start()
	srv.kern.AddProcess(proc)

	// RED: GetProcDetailRequest does not have UUID field
	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodGetProcDetail, GetProcDetailRequest{
		UUID: proc.UUID,
	})

	if !resp.OK {
		t.Fatalf("AC-6: GetProcDetail by UUID should work: %+v", resp.Error)
	}

	var detail GetProcDetailResponse
	if err := json.Unmarshal(resp.Payload, &detail); err != nil {
		t.Fatalf("AC-6: unmarshal: %v", err)
	}
	if detail.Intent != "proc detail uuid test" {
		t.Errorf("AC-6: Intent = %q, want %q", detail.Intent, "proc detail uuid test")
	}
}

// ===========================================================================
// Kernel: GetProcessByUUID 单元测试
// ===========================================================================

func TestATDD_28_3_GetProcessByUUID_Found(t *testing.T) {
	srv, _, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "uuid lookup test", nil)
	_ = proc.Start()
	srv.kern.AddProcess(proc)

	// RED: GetProcessByUUID method does not exist yet
	found, ok := srv.kern.GetProcessByUUID(proc.UUID)
	if !ok {
		t.Fatal("GetProcessByUUID should find the process")
	}
	if found.PID != proc.PID {
		t.Errorf("GetProcessByUUID PID = %d, want %d", found.PID, proc.PID)
	}
	if found.UUID != proc.UUID {
		t.Errorf("GetProcessByUUID UUID = %q, want %q", found.UUID, proc.UUID)
	}
}

func TestATDD_28_3_GetProcessByUUID_NotFound(t *testing.T) {
	srv, _, _ := setupTestServer(t)

	// RED: GetProcessByUUID method does not exist yet
	_, ok := srv.kern.GetProcessByUUID("019576f4-ffff-7000-8000-ffffffffffff")
	if ok {
		t.Fatal("GetProcessByUUID should not find non-existent UUID")
	}
}

func TestATDD_28_3_GetProcessByUUID_MultipleProcesses(t *testing.T) {
	srv, _, _ := setupTestServer(t)

	proc1 := kernel.NewProcess(0, "multi uuid 1", nil)
	_ = proc1.Start()
	srv.kern.AddProcess(proc1)

	proc2 := kernel.NewProcess(0, "multi uuid 2", nil)
	_ = proc2.Start()
	srv.kern.AddProcess(proc2)

	proc3 := kernel.NewProcess(0, "multi uuid 3", nil)
	_ = proc3.Start()
	srv.kern.AddProcess(proc3)

	// RED: GetProcessByUUID method does not exist yet
	found, ok := srv.kern.GetProcessByUUID(proc2.UUID)
	if !ok {
		t.Fatal("GetProcessByUUID should find proc2 among multiple processes")
	}
	if found.PID != proc2.PID {
		t.Errorf("GetProcessByUUID PID = %d, want %d", found.PID, proc2.PID)
	}
}

// ===========================================================================
// resolveProcess: UUID 优先逻辑验证
// ===========================================================================

func TestATDD_28_3_ResolveProcess_UUIDPriority(t *testing.T) {
	srv, _, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "resolve priority test", nil)
	_ = proc.Start()
	srv.kern.AddProcess(proc)

	// When both PID and UUID are provided, UUID should take priority
	// RED: resolveProcess method does not exist yet
	found, ok := srv.resolveProcess(types.PID(99999), proc.UUID)
	if !ok {
		t.Fatal("resolveProcess should find by UUID even when PID is wrong")
	}
	if found.UUID != proc.UUID {
		t.Errorf("resolveProcess UUID = %q, want %q", found.UUID, proc.UUID)
	}
}

func TestATDD_28_3_ResolveProcess_FallbackToPID(t *testing.T) {
	srv, _, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "resolve fallback test", nil)
	_ = proc.Start()
	srv.kern.AddProcess(proc)

	// When UUID is empty, fall back to PID
	// RED: resolveProcess method does not exist yet
	found, ok := srv.resolveProcess(proc.PID, "")
	if !ok {
		t.Fatal("resolveProcess should fall back to PID when UUID is empty")
	}
	if found.PID != proc.PID {
		t.Errorf("resolveProcess PID = %d, want %d", found.PID, proc.PID)
	}
}

func TestATDD_28_3_ResolveProcess_BothEmpty(t *testing.T) {
	srv, _, _ := setupTestServer(t)

	// RED: resolveProcess method does not exist yet
	_, ok := srv.resolveProcess(0, "")
	if ok {
		t.Fatal("resolveProcess should return false when both PID and UUID are empty")
	}
}

// ===========================================================================
// Client roundtrip: UUID 查询
// ===========================================================================

func TestATDD_28_3_ClientRoundtrip_GetStepDetail_ByUUID(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "client uuid roundtrip", nil)
	_ = proc.Start()
	proc.SetFinalSystemPrompt("Client UUID roundtrip prompt")

	tmpDir := t.TempDir()
	srv.kern.SetStepDataDir(tmpDir)

	writeTestStepsUUID(t, tmpDir, proc.UUID, []types.StepRecord{
		testStepRecord(1),
	})
	srv.kern.AddProcess(proc)

	client, err := DialTimeout(sockPath, 3*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	// RED: Client.GetStepDetailByUUID method does not exist yet
	detail, err := client.GetStepDetailByUUID(proc.UUID, 1)
	if err != nil {
		t.Fatalf("GetStepDetailByUUID: %v", err)
	}
	if detail.SystemPrompt != "Client UUID roundtrip prompt" {
		t.Errorf("SystemPrompt = %q, want %q", detail.SystemPrompt, "Client UUID roundtrip prompt")
	}
}

func TestATDD_28_3_ClientRoundtrip_ListSteps_ByUUID(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "client uuid list roundtrip", nil)
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
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	// RED: Client.ListStepsByUUID method does not exist yet
	result, err := client.ListStepsByUUID(proc.UUID, 0)
	if err != nil {
		t.Fatalf("ListStepsByUUID: %v", err)
	}
	if result.Total != 2 {
		t.Errorf("Total = %d, want 2", result.Total)
	}
}
