package ipc

// =============================================================================
// ATDD Story 27.6: GetProcDetail IPC Method
// TDD RED PHASE — All tests designed to FAIL until implementation exists
// =============================================================================
//
// Test Strategy:
//   AC-1: Protocol wire types (GetProcDetailRequest/Response) exist and serialize
//   AC-1: Server handler returns full detail for running process
//   AC-1: Environment variables with sensitive keys are masked
//   AC-1: Skills include AllowedTools from skill loader
//   AC-1: FD table reflects open file descriptors
//   AC-1: Context stats (message count, tokens, budget, usage %)
//   AC-4: Performance — ≤1s for IPC roundtrip
//   AC-5: Error — PID not found returns not_found
//   AC-6: Dead process returns historical data with empty FD table
//
// Priority: P0 (AC-1 core wire types + server handler), P1 (AC-6 dead proc),
//           P2 (AC-4 performance)
// Test Level: Integration (IPC server+client roundtrip)

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
)

// ---------------------------------------------------------------------------
// AC-1: Protocol wire types exist and serialize correctly
// ---------------------------------------------------------------------------

func TestATDD_27_6_AC1_MethodConstant(t *testing.T) {

	if MethodGetProcDetail != "get_proc_detail" {
		t.Fatalf("AC-1: MethodGetProcDetail = %q, want %q", MethodGetProcDetail, "get_proc_detail")
	}
}

func TestATDD_27_6_AC1_GetProcDetailRequest_Serialization(t *testing.T) {

	req := GetProcDetailRequest{PID: types.PID(42)}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("AC-1: marshal GetProcDetailRequest: %v", err)
	}
	var decoded GetProcDetailRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("AC-1: unmarshal GetProcDetailRequest: %v", err)
	}
	if decoded.PID != 42 {
		t.Errorf("AC-1: roundtrip mismatch: got PID=%d", decoded.PID)
	}
}

func TestATDD_27_6_AC1_GetProcDetailResponse_Serialization(t *testing.T) {

	resp := GetProcDetailResponse{
		PID:      types.PID(1),
		UUID:     "01960abc-def0-7000-8000-000000000001",
		PPID:     types.PID(0),
		State:    "running",
		Intent:   "analyze code",
		Provider: "claude",
		Model:    "opus-4",
		CreatedAtMs: time.Now().UnixMilli(),
		Skills: []SkillInfoWire{
			{Name: "code-analysis", AllowedTools: []string{"/dev/fs", "/dev/shell"}},
		},
		AllowedDevices: []string{"/dev/fs", "/dev/shell", "/dev/llm/claude"},
		EnvSnapshot:    map[string]string{"RNIX_ENV": "development"},
		FDTable: []FDEntryWire{
			{FD: types.FD(0), DevicePath: "/dev/llm/claude"},
			{FD: types.FD(1), DevicePath: "/dev/fs"},
		},
		ContextStats: ContextStatsWire{
			MessageCount:  42,
			TokensUsed:    12500,
			ContextBudget: 20000,
			UsagePct:      62.5,
		},
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("AC-1: marshal GetProcDetailResponse: %v", err)
	}
	var decoded GetProcDetailResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("AC-1: unmarshal GetProcDetailResponse: %v", err)
	}
	if decoded.PID != 1 {
		t.Errorf("AC-1: PID mismatch: got %d", decoded.PID)
	}
	if decoded.UUID != resp.UUID {
		t.Errorf("AC-1: UUID mismatch: got %q", decoded.UUID)
	}
	if decoded.Provider != "claude" || decoded.Model != "opus-4" {
		t.Errorf("AC-1: Provider/Model mismatch: got %q/%q", decoded.Provider, decoded.Model)
	}
	if len(decoded.Skills) != 1 || decoded.Skills[0].Name != "code-analysis" {
		t.Errorf("AC-1: Skills mismatch: got %+v", decoded.Skills)
	}
	if len(decoded.FDTable) != 2 {
		t.Errorf("AC-1: FDTable length = %d, want 2", len(decoded.FDTable))
	}
	if decoded.ContextStats.UsagePct != 62.5 {
		t.Errorf("AC-1: UsagePct = %f, want 62.5", decoded.ContextStats.UsagePct)
	}
}

func TestATDD_27_6_AC1_SkillInfoWire_Serialization(t *testing.T) {
	si := SkillInfoWire{
		Name:         "shell-ops",
		AllowedTools: []string{"/dev/shell", "/dev/fs"},
	}
	data, err := json.Marshal(si)
	if err != nil {
		t.Fatalf("AC-1: marshal SkillInfoWire: %v", err)
	}
	var decoded SkillInfoWire
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("AC-1: unmarshal SkillInfoWire: %v", err)
	}
	if decoded.Name != "shell-ops" || len(decoded.AllowedTools) != 2 {
		t.Errorf("AC-1: SkillInfoWire roundtrip mismatch: got %+v", decoded)
	}
}

func TestATDD_27_6_AC1_FDEntryWire_Serialization(t *testing.T) {
	fe := FDEntryWire{FD: types.FD(3), DevicePath: "/dev/mcp/github"}
	data, err := json.Marshal(fe)
	if err != nil {
		t.Fatalf("AC-1: marshal FDEntryWire: %v", err)
	}
	var decoded FDEntryWire
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("AC-1: unmarshal FDEntryWire: %v", err)
	}
	if decoded.FD != 3 || decoded.DevicePath != "/dev/mcp/github" {
		t.Errorf("AC-1: FDEntryWire roundtrip mismatch: got %+v", decoded)
	}
}

func TestATDD_27_6_AC1_ContextStatsWire_Serialization(t *testing.T) {
	cs := ContextStatsWire{
		MessageCount:  10,
		TokensUsed:    5000,
		ContextBudget: 10000,
		UsagePct:      50.0,
	}
	data, err := json.Marshal(cs)
	if err != nil {
		t.Fatalf("AC-1: marshal ContextStatsWire: %v", err)
	}
	var decoded ContextStatsWire
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("AC-1: unmarshal ContextStatsWire: %v", err)
	}
	if decoded.MessageCount != 10 || decoded.TokensUsed != 5000 {
		t.Errorf("AC-1: ContextStatsWire mismatch: got %+v", decoded)
	}
	if decoded.UsagePct != 50.0 {
		t.Errorf("AC-1: UsagePct = %f, want 50.0", decoded.UsagePct)
	}
}

// ---------------------------------------------------------------------------
// AC-1: Server handler — running process returns full detail
// ---------------------------------------------------------------------------

func TestATDD_27_6_AC1_RunningProcess_ReturnsDetail(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "test detail intent", nil)
	_ = proc.Start()
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodGetProcDetail, GetProcDetailRequest{PID: proc.PID})

	if !resp.OK {
		t.Fatalf("AC-1: request failed: %+v", resp.Error)
	}

	var detail GetProcDetailResponse
	if err := json.Unmarshal(resp.Payload, &detail); err != nil {
		t.Fatalf("AC-1: unmarshal response: %v", err)
	}
	if detail.PID != proc.PID {
		t.Errorf("AC-1: PID = %d, want %d", detail.PID, proc.PID)
	}
	if detail.State != "running" {
		t.Errorf("AC-1: State = %q, want %q", detail.State, "running")
	}
	if detail.Intent != "test detail intent" {
		t.Errorf("AC-1: Intent = %q, want %q", detail.Intent, "test detail intent")
	}
	if detail.UUID == "" {
		t.Error("AC-1: UUID should not be empty")
	}
}

// ---------------------------------------------------------------------------
// AC-1: Environment variable masking
// ---------------------------------------------------------------------------

func TestATDD_27_6_AC1_EnvSnapshot_SensitiveKeysMasked(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "env masking test", nil)
	_ = proc.Start()
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodGetProcDetail, GetProcDetailRequest{PID: proc.PID})

	if !resp.OK {
		t.Fatalf("AC-1: request failed: %+v", resp.Error)
	}

	var detail GetProcDetailResponse
	if err := json.Unmarshal(resp.Payload, &detail); err != nil {
		t.Fatalf("AC-1: unmarshal: %v", err)
	}

	// Sensitive keys (KEY, SECRET, TOKEN, PASSWORD) should be masked as "***"
	for key, val := range detail.EnvSnapshot {
		for _, sensitive := range []string{"KEY", "SECRET", "TOKEN", "PASSWORD"} {
			if contains(key, sensitive) && val != "***" {
				t.Errorf("AC-1: env key %q contains %q but value not masked: %q", key, sensitive, val)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// AC-1: FD table for running process
// ---------------------------------------------------------------------------

func TestATDD_27_6_AC1_FDTable_RunningProcess(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "fd table test", nil)
	_ = proc.Start()
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodGetProcDetail, GetProcDetailRequest{PID: proc.PID})

	if !resp.OK {
		t.Fatalf("AC-1: request failed: %+v", resp.Error)
	}

	var detail GetProcDetailResponse
	if err := json.Unmarshal(resp.Payload, &detail); err != nil {
		t.Fatalf("AC-1: unmarshal: %v", err)
	}

	// FDTable should be a non-nil slice (may be empty for test process with no opened files)
	if detail.FDTable == nil {
		t.Error("AC-1: FDTable should not be nil (use empty slice)")
	}
}

// ---------------------------------------------------------------------------
// AC-1: Context stats
// ---------------------------------------------------------------------------

func TestATDD_27_6_AC1_ContextStats_ReturnsStats(t *testing.T) {
	srv, sockPath, ctxMgr := setupTestServer(t)

	proc := kernel.NewProcess(0, "ctx stats test", nil)
	_ = proc.Start()
	srv.kern.AddProcess(proc)

	// Allocate context and write some messages
	ctxID, err := ctxMgr.CtxAlloc(128)
	if err != nil {
		t.Fatalf("AC-1: CtxAlloc: %v", err)
	}
	_ = ctxID // Process needs to have this ctxID associated

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodGetProcDetail, GetProcDetailRequest{PID: proc.PID})

	if !resp.OK {
		t.Fatalf("AC-1: request failed: %+v", resp.Error)
	}

	var detail GetProcDetailResponse
	if err := json.Unmarshal(resp.Payload, &detail); err != nil {
		t.Fatalf("AC-1: unmarshal: %v", err)
	}

	// ContextStats should have zero or positive values
	if detail.ContextStats.ContextBudget < 0 {
		t.Errorf("AC-1: ContextBudget should be >= 0, got %d", detail.ContextStats.ContextBudget)
	}
	if detail.ContextStats.UsagePct < 0 || detail.ContextStats.UsagePct > 100 {
		t.Errorf("AC-1: UsagePct out of range [0,100]: %f", detail.ContextStats.UsagePct)
	}
}

// ---------------------------------------------------------------------------
// AC-1: Client method roundtrip
// ---------------------------------------------------------------------------

func TestATDD_27_6_AC1_ClientMethod_Roundtrip(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "client roundtrip", nil)
	_ = proc.Start()
	srv.kern.AddProcess(proc)

	client, err := DialTimeout(sockPath, 3*time.Second)
	if err != nil {
		t.Fatalf("AC-1: dial: %v", err)
	}
	defer client.Close()

	detail, err := client.GetProcDetail(proc.PID)
	if err != nil {
		t.Fatalf("AC-1: GetProcDetail: %v", err)
	}
	if detail.PID != proc.PID {
		t.Errorf("AC-1: PID = %d, want %d", detail.PID, proc.PID)
	}
	if detail.Intent != "client roundtrip" {
		t.Errorf("AC-1: Intent = %q, want %q", detail.Intent, "client roundtrip")
	}
}

// ---------------------------------------------------------------------------
// AC-4: Performance — ≤1s for IPC roundtrip
// ---------------------------------------------------------------------------

func TestATDD_27_6_AC4_Performance(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "perf test", nil)
	_ = proc.Start()
	srv.kern.AddProcess(proc)

	client, err := DialTimeout(sockPath, 3*time.Second)
	if err != nil {
		t.Fatalf("AC-4: dial: %v", err)
	}
	defer client.Close()

	start := time.Now()
	detail, err := client.GetProcDetail(proc.PID)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("AC-4: GetProcDetail: %v", err)
	}
	if detail.PID != proc.PID {
		t.Errorf("AC-4: PID mismatch")
	}
	if elapsed > 1*time.Second {
		t.Errorf("AC-4: elapsed %v exceeds 1s limit (NFR63-obs)", elapsed)
	}
}

// ---------------------------------------------------------------------------
// AC-5 (mapped): Error — PID not found
// ---------------------------------------------------------------------------

func TestATDD_27_6_AC5_PIDNotFound(t *testing.T) {
	_, sockPath, _ := setupTestServer(t)

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodGetProcDetail, GetProcDetailRequest{PID: types.PID(99999)})

	if resp.OK {
		t.Fatal("AC-5: expected error response for non-existent PID")
	}
	if resp.Error == nil {
		t.Fatal("AC-5: expected error payload")
	}
	if resp.Error.Code != "not_found" {
		t.Errorf("AC-5: error code = %q, want %q", resp.Error.Code, "not_found")
	}
}

// ---------------------------------------------------------------------------
// AC-6: Dead process returns historical data with empty FD table
// ---------------------------------------------------------------------------

func TestATDD_27_6_AC6_DeadProcess_ReturnsHistoricalData(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "dead proc test", nil)
	_ = proc.Start()
	proc.Finish("completed", 0, nil)
	_ = proc.Reap()
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodGetProcDetail, GetProcDetailRequest{PID: proc.PID})

	if !resp.OK {
		t.Fatalf("AC-6: request failed: %+v", resp.Error)
	}

	var detail GetProcDetailResponse
	if err := json.Unmarshal(resp.Payload, &detail); err != nil {
		t.Fatalf("AC-6: unmarshal: %v", err)
	}

	// Dead process should still return basic info
	if detail.PID != proc.PID {
		t.Errorf("AC-6: PID = %d, want %d", detail.PID, proc.PID)
	}
	if detail.Intent != "dead proc test" {
		t.Errorf("AC-6: Intent = %q, want %q", detail.Intent, "dead proc test")
	}

	// FD table should be empty for dead process (reaper clears it)
	if len(detail.FDTable) != 0 {
		t.Errorf("AC-6: FDTable should be empty for dead process, got %d entries", len(detail.FDTable))
	}

	// Context stats should gracefully return zeros if context freed
	// (no error, just zero values)
	if detail.ContextStats.UsagePct < 0 {
		t.Errorf("AC-6: UsagePct should be >= 0 for dead process")
	}
}

func TestATDD_27_6_AC6_DeadProcess_HasDeadAtMs(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "dead at test", nil)
	_ = proc.Start()
	proc.Finish("done", 0, nil)
	_ = proc.Reap()
	proc.DeadAt = time.Now() // In production, kernel's reapProcess sets this
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodGetProcDetail, GetProcDetailRequest{PID: proc.PID})

	if !resp.OK {
		t.Fatalf("AC-6: request failed: %+v", resp.Error)
	}

	var detail GetProcDetailResponse
	if err := json.Unmarshal(resp.Payload, &detail); err != nil {
		t.Fatalf("AC-6: unmarshal: %v", err)
	}

	if detail.DeadAtMs == 0 {
		t.Error("AC-6: DeadAtMs should be non-zero for dead process")
	}
}

// ---------------------------------------------------------------------------
// Helper
// ---------------------------------------------------------------------------

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsUpper(s, substr))
}

func containsUpper(s, substr string) bool {
	upper := func(r byte) byte {
		if r >= 'a' && r <= 'z' {
			return r - 32
		}
		return r
	}
	su := make([]byte, len(s))
	for i := range len(s) {
		su[i] = upper(s[i])
	}
	subu := make([]byte, len(substr))
	for i := range len(substr) {
		subu[i] = upper(substr[i])
	}
	for i := 0; i <= len(su)-len(subu); i++ {
		match := true
		for j := range len(subu) {
			if su[i+j] != subu[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
