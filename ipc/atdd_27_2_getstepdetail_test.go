package ipc

// =============================================================================
// ATDD Story 27.2: GetStepDetail IPC Method
// TDD RED PHASE — All tests designed to FAIL until implementation exists
// =============================================================================
//
// Test Strategy:
//   AC-1: Protocol wire types exist and serialize correctly
//   AC-2: Server handler — running process returns step detail
//   AC-3: Server handler — zombie/dead process returns step detail
//   AC-4: Server handler — reaped process reads from disk
//   AC-5: Error — PID not found
//   AC-6: Error — step not found
//   AC-7: Client method roundtrip
//   AC-8: Performance — ≤500ms for 30-step file
//
// Priority: P0 (core observation infrastructure)
// Test Level: Integration (IPC server+client roundtrip)

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
	"github.com/rnixai/rnix/vfs"
)

// ---------------------------------------------------------------------------
// AC-1: Protocol wire types exist and serialize correctly
// ---------------------------------------------------------------------------

func TestATDD_27_2_AC1_MethodConstant(t *testing.T) {
	if MethodGetStepDetail != "get_step_detail" {
		t.Fatalf("AC-1: MethodGetStepDetail = %q, want %q", MethodGetStepDetail, "get_step_detail")
	}
}

func TestATDD_27_2_AC1_GetStepDetailRequest_Serialization(t *testing.T) {
	req := GetStepDetailRequest{PID: types.PID(42), Step: 3}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("AC-1: marshal GetStepDetailRequest: %v", err)
	}
	var decoded GetStepDetailRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("AC-1: unmarshal GetStepDetailRequest: %v", err)
	}
	if decoded.PID != 42 || decoded.Step != 3 {
		t.Errorf("AC-1: roundtrip mismatch: got PID=%d Step=%d", decoded.PID, decoded.Step)
	}
}

func TestATDD_27_2_AC1_GetStepDetailResponse_Serialization(t *testing.T) {
	resp := GetStepDetailResponse{
		SystemPrompt: "You are a helpful agent.",
		Tools: []ToolDefWire{
			{Name: "read", Description: "Read a file", Parameters: map[string]any{"path": "string"}},
		},
		Step:         3,
		Messages:     []MessageWire{{Role: "user", Content: "hello"}},
		MessageCount: 5,
		TokenCount:   1200,
		RawResponse:  `{"role":"assistant","content":"hi"}`,
		Action:       "tool_call",
		Summary:      "/dev/fs read config.yaml",
		ToolPath:     "/dev/fs",
		ToolInput:    `{"path":"config.yaml"}`,
		ToolResult:   "key: value",
		ToolError:    "",
		ToolDurationMs: 42.5,
		RequestTokens:  800,
		ResponseTokens: 400,
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("AC-1: marshal GetStepDetailResponse: %v", err)
	}
	var decoded GetStepDetailResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("AC-1: unmarshal GetStepDetailResponse: %v", err)
	}
	if decoded.SystemPrompt != resp.SystemPrompt {
		t.Errorf("AC-1: SystemPrompt mismatch: got %q", decoded.SystemPrompt)
	}
	if len(decoded.Tools) != 1 || decoded.Tools[0].Name != "read" {
		t.Errorf("AC-1: Tools mismatch: got %+v", decoded.Tools)
	}
	if decoded.Step != 3 {
		t.Errorf("AC-1: Step mismatch: got %d", decoded.Step)
	}
	if len(decoded.Messages) != 1 || decoded.Messages[0].Role != "user" {
		t.Errorf("AC-1: Messages mismatch: got %+v", decoded.Messages)
	}
	if decoded.ToolDurationMs != 42.5 {
		t.Errorf("AC-1: ToolDurationMs mismatch: got %f", decoded.ToolDurationMs)
	}
}

func TestATDD_27_2_AC1_MessageWire_WithToolCalls(t *testing.T) {
	mw := MessageWire{
		Role:    "assistant",
		Content: "",
		ToolCalls: []ToolCallWire{
			{ID: "tc_1", Name: "read_file", Input: map[string]any{"path": "/etc/hosts"}},
		},
	}
	data, err := json.Marshal(mw)
	if err != nil {
		t.Fatalf("AC-1: marshal MessageWire: %v", err)
	}
	var decoded MessageWire
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("AC-1: unmarshal MessageWire: %v", err)
	}
	if len(decoded.ToolCalls) != 1 || decoded.ToolCalls[0].ID != "tc_1" {
		t.Errorf("AC-1: ToolCalls roundtrip mismatch: got %+v", decoded.ToolCalls)
	}
}

func TestATDD_27_2_AC1_MessageWire_ToolResult(t *testing.T) {
	mw := MessageWire{
		Role:       "tool",
		Content:    "file contents here",
		ToolCallID: "tc_1",
	}
	data, err := json.Marshal(mw)
	if err != nil {
		t.Fatalf("AC-1: marshal tool result MessageWire: %v", err)
	}
	var decoded MessageWire
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("AC-1: unmarshal tool result MessageWire: %v", err)
	}
	if decoded.ToolCallID != "tc_1" || decoded.Content != "file contents here" {
		t.Errorf("AC-1: tool result roundtrip mismatch: got %+v", decoded)
	}
}

func TestATDD_27_2_AC1_ToolDefWire_Serialization(t *testing.T) {
	td := ToolDefWire{
		Name:        "execute_shell",
		Description: "Execute a shell command",
		Parameters:  map[string]any{"command": "string"},
	}
	data, err := json.Marshal(td)
	if err != nil {
		t.Fatalf("AC-1: marshal ToolDefWire: %v", err)
	}
	var decoded ToolDefWire
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("AC-1: unmarshal ToolDefWire: %v", err)
	}
	if decoded.Name != "execute_shell" {
		t.Errorf("AC-1: ToolDefWire.Name mismatch: got %q", decoded.Name)
	}
}

// ---------------------------------------------------------------------------
// AC-2: Server handler — running process returns step detail
// ---------------------------------------------------------------------------

func TestATDD_27_2_AC2_RunningProcess(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	// Create a running process with system prompt and tool defs
	proc := kernel.NewProcess(0, "test intent", nil)
	_ = proc.Start()
	proc.SetFinalSystemPrompt("You are a test agent.")
	proc.SetNativeToolDefs([]vfs.ToolDef{
		{Name: "read_file", Description: "Read a file", Parameters: map[string]any{"path": "string"}},
	})

	// Write steps to disk (UUID path since Story 28-2)
	tmpDir := t.TempDir()
	srv.kern.SetStepDataDir(tmpDir)
	writeTestStepsUUID(t, tmpDir, proc.UUID, []types.StepRecord{
		testStepRecord(1),
		testStepRecord(2),
		testStepRecord(3),
	})
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodGetStepDetail, GetStepDetailRequest{PID: proc.PID, Step: 3})

	if !resp.OK {
		t.Fatalf("AC-2: request failed: %+v", resp.Error)
	}

	var detail GetStepDetailResponse
	if err := json.Unmarshal(resp.Payload, &detail); err != nil {
		t.Fatalf("AC-2: unmarshal response: %v", err)
	}
	if detail.SystemPrompt != "You are a test agent." {
		t.Errorf("AC-2: SystemPrompt = %q, want %q", detail.SystemPrompt, "You are a test agent.")
	}
	if len(detail.Tools) != 1 || detail.Tools[0].Name != "read_file" {
		t.Errorf("AC-2: Tools mismatch: got %+v", detail.Tools)
	}
	if detail.Step != 3 {
		t.Errorf("AC-2: Step = %d, want 3", detail.Step)
	}
	if detail.Action != "tool_call" {
		t.Errorf("AC-2: Action = %q, want %q", detail.Action, "tool_call")
	}
	if detail.MessageCount != 3 {
		t.Errorf("AC-2: MessageCount = %d, want 3", detail.MessageCount)
	}
	if detail.RequestTokens != 100 {
		t.Errorf("AC-2: RequestTokens = %d, want 100", detail.RequestTokens)
	}
	if detail.ResponseTokens != 50 {
		t.Errorf("AC-2: ResponseTokens = %d, want 50", detail.ResponseTokens)
	}
}

// ---------------------------------------------------------------------------
// AC-3: Server handler — zombie/dead process returns step detail
// ---------------------------------------------------------------------------

func TestATDD_27_2_AC3_ZombieProcess(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "zombie intent", nil)
	_ = proc.Start()
	proc.SetFinalSystemPrompt("Zombie prompt")
	proc.SetNativeToolDefs(nil)

	tmpDir := t.TempDir()
	srv.kern.SetStepDataDir(tmpDir)
	writeTestStepsUUID(t, tmpDir, proc.UUID, []types.StepRecord{
		testStepRecord(1),
	})

	// Transition to zombie
	proc.Finish("done", 0, nil)
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodGetStepDetail, GetStepDetailRequest{PID: proc.PID, Step: 1})

	if !resp.OK {
		t.Fatalf("AC-3: request failed: %+v", resp.Error)
	}

	var detail GetStepDetailResponse
	if err := json.Unmarshal(resp.Payload, &detail); err != nil {
		t.Fatalf("AC-3: unmarshal response: %v", err)
	}
	if detail.SystemPrompt != "Zombie prompt" {
		t.Errorf("AC-3: SystemPrompt = %q, want %q", detail.SystemPrompt, "Zombie prompt")
	}
	if detail.Step != 1 {
		t.Errorf("AC-3: Step = %d, want 1", detail.Step)
	}
}

// ---------------------------------------------------------------------------
// AC-4: Server handler — reaped process reads from disk
// ---------------------------------------------------------------------------

func TestATDD_27_2_AC4_ReapedProcess(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	tmpDir := t.TempDir()
	srv.kern.SetStepDataDir(tmpDir)

	pid := types.PID(9999)

	// Write process-meta.json and steps.jsonl to disk (simulating reaper output)
	stepsDir := filepath.Join(tmpDir, "data", "steps", fmt.Sprintf("%d", pid))
	if err := os.MkdirAll(stepsDir, 0o755); err != nil {
		t.Fatalf("AC-4: mkdir: %v", err)
	}

	meta := struct {
		SystemPrompt string    `json:"system_prompt"`
		ToolDefs     []vfs.ToolDef `json:"tool_defs"`
	}{
		SystemPrompt: "Reaped system prompt",
		ToolDefs: []vfs.ToolDef{
			{Name: "shell", Description: "Run command"},
		},
	}
	metaJSON, _ := json.Marshal(meta)
	if err := os.WriteFile(filepath.Join(stepsDir, "process-meta.json"), metaJSON, 0o644); err != nil {
		t.Fatalf("AC-4: write meta: %v", err)
	}

	writeTestSteps(t, tmpDir, pid, []types.StepRecord{
		testStepRecord(1),
		testStepRecord(2),
		testStepRecord(3),
	})

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodGetStepDetail, GetStepDetailRequest{PID: pid, Step: 2})

	if !resp.OK {
		t.Fatalf("AC-4: request failed: %+v", resp.Error)
	}

	var detail GetStepDetailResponse
	if err := json.Unmarshal(resp.Payload, &detail); err != nil {
		t.Fatalf("AC-4: unmarshal response: %v", err)
	}
	if detail.SystemPrompt != "Reaped system prompt" {
		t.Errorf("AC-4: SystemPrompt = %q, want %q", detail.SystemPrompt, "Reaped system prompt")
	}
	if len(detail.Tools) != 1 || detail.Tools[0].Name != "shell" {
		t.Errorf("AC-4: Tools mismatch: got %+v", detail.Tools)
	}
	if detail.Step != 2 {
		t.Errorf("AC-4: Step = %d, want 2", detail.Step)
	}
}

// ---------------------------------------------------------------------------
// AC-5: Error — PID not found
// ---------------------------------------------------------------------------

func TestATDD_27_2_AC5_PIDNotFound(t *testing.T) {
	_, sockPath, _ := setupTestServer(t)

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodGetStepDetail, GetStepDetailRequest{PID: types.PID(88888), Step: 1})

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
// AC-6: Error — step out of range
// ---------------------------------------------------------------------------

func TestATDD_27_2_AC6_StepNotFound(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "step test", nil)
	_ = proc.Start()
	proc.SetFinalSystemPrompt("test prompt")

	tmpDir := t.TempDir()
	srv.kern.SetStepDataDir(tmpDir)
	writeTestStepsUUID(t, tmpDir, proc.UUID, []types.StepRecord{
		testStepRecord(1),
		testStepRecord(2),
	})
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodGetStepDetail, GetStepDetailRequest{PID: proc.PID, Step: 99})

	if resp.OK {
		t.Fatal("AC-6: expected error response for out-of-range step")
	}
	if resp.Error == nil {
		t.Fatal("AC-6: expected error payload")
	}
	if resp.Error.Code != "not_found" {
		t.Errorf("AC-6: error code = %q, want %q", resp.Error.Code, "not_found")
	}
}

// ---------------------------------------------------------------------------
// AC-7: Client method roundtrip
// ---------------------------------------------------------------------------

func TestATDD_27_2_AC7_ClientMethod(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "client test", nil)
	_ = proc.Start()
	proc.SetFinalSystemPrompt("Client roundtrip prompt")
	proc.SetNativeToolDefs([]vfs.ToolDef{
		{Name: "fs", Description: "Filesystem access"},
	})

	tmpDir := t.TempDir()
	srv.kern.SetStepDataDir(tmpDir)
	writeTestStepsUUID(t, tmpDir, proc.UUID, []types.StepRecord{
		testStepRecord(1),
	})
	srv.kern.AddProcess(proc)

	client, err := DialTimeout(sockPath, 3*time.Second)
	if err != nil {
		t.Fatalf("AC-7: dial: %v", err)
	}
	defer client.Close()

	detail, err := client.GetStepDetail(proc.PID, 1)
	if err != nil {
		t.Fatalf("AC-7: GetStepDetail: %v", err)
	}
	if detail.SystemPrompt != "Client roundtrip prompt" {
		t.Errorf("AC-7: SystemPrompt = %q, want %q", detail.SystemPrompt, "Client roundtrip prompt")
	}
	if detail.Step != 1 {
		t.Errorf("AC-7: Step = %d, want 1", detail.Step)
	}
	if len(detail.Tools) != 1 {
		t.Errorf("AC-7: Tools length = %d, want 1", len(detail.Tools))
	}
}

// ---------------------------------------------------------------------------
// AC-8: Performance — ≤500ms for 30-step file (NFR61)
// ---------------------------------------------------------------------------

func TestATDD_27_2_AC8_Performance(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "perf test", nil)
	_ = proc.Start()
	proc.SetFinalSystemPrompt("perf prompt")

	tmpDir := t.TempDir()
	srv.kern.SetStepDataDir(tmpDir)

	// Write 30 steps to simulate typical usage
	steps := make([]types.StepRecord, 30)
	for i := range steps {
		steps[i] = testStepRecord(i + 1)
	}
	writeTestStepsUUID(t, tmpDir, proc.UUID, steps)
	srv.kern.AddProcess(proc)

	client, err := DialTimeout(sockPath, 3*time.Second)
	if err != nil {
		t.Fatalf("AC-8: dial: %v", err)
	}
	defer client.Close()

	start := time.Now()
	detail, err := client.GetStepDetail(proc.PID, 30)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("AC-8: GetStepDetail: %v", err)
	}
	if detail.Step != 30 {
		t.Errorf("AC-8: Step = %d, want 30", detail.Step)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("AC-8: elapsed %v exceeds 500ms limit (NFR61)", elapsed)
	}
}

// ---------------------------------------------------------------------------
// AC-2 supplement: Messages conversion (json.RawMessage → []MessageWire)
// ---------------------------------------------------------------------------

func TestATDD_27_2_AC2_MessagesConversion(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "msg conversion test", nil)
	_ = proc.Start()
	proc.SetFinalSystemPrompt("conversion prompt")

	tmpDir := t.TempDir()
	srv.kern.SetStepDataDir(tmpDir)

	// Build a step with rich message content
	msgs := []rnixctx.Message{
		{Role: rnixctx.RoleUser, Content: "Analyze this code"},
		{
			Role:    rnixctx.RoleAssistant,
			Content: "",
			ToolCalls: []rnixctx.ToolCall{
				{ID: "tc_abc", Name: "read_file", Input: map[string]any{"path": "main.go"}},
			},
		},
		{Role: rnixctx.RoleTool, Content: "package main\nfunc main() {}", ToolCallID: "tc_abc"},
	}
	msgsJSON, _ := json.Marshal(msgs)

	rec := types.StepRecord{
		Step:           1,
		Timestamp:      time.Second * 5,
		Messages:       msgsJSON,
		MessageCount:   3,
		TokenCount:     500,
		RawResponse:    `{"role":"assistant"}`,
		Action:         "tool_call",
		Summary:        "read main.go",
		ToolPath:       "/dev/fs",
		ToolInput:      `{"path":"main.go"}`,
		ToolResult:     "package main\nfunc main() {}",
		ToolDuration:   30 * time.Millisecond,
		RequestTokens:  300,
		ResponseTokens: 200,
	}
	writeTestStepsUUID(t, tmpDir, proc.UUID, []types.StepRecord{rec})
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodGetStepDetail, GetStepDetailRequest{PID: proc.PID, Step: 1})
	if !resp.OK {
		t.Fatalf("AC-2 messages: request failed: %+v", resp.Error)
	}

	var detail GetStepDetailResponse
	if err := json.Unmarshal(resp.Payload, &detail); err != nil {
		t.Fatalf("AC-2 messages: unmarshal: %v", err)
	}

	if len(detail.Messages) != 3 {
		t.Fatalf("AC-2 messages: len(Messages) = %d, want 3", len(detail.Messages))
	}

	// Verify user message
	if detail.Messages[0].Role != "user" || detail.Messages[0].Content != "Analyze this code" {
		t.Errorf("AC-2 messages[0]: %+v", detail.Messages[0])
	}

	// Verify assistant with tool calls
	if detail.Messages[1].Role != "assistant" || len(detail.Messages[1].ToolCalls) != 1 {
		t.Errorf("AC-2 messages[1]: %+v", detail.Messages[1])
	}
	if detail.Messages[1].ToolCalls[0].ID != "tc_abc" || detail.Messages[1].ToolCalls[0].Name != "read_file" {
		t.Errorf("AC-2 messages[1].ToolCalls[0]: %+v", detail.Messages[1].ToolCalls[0])
	}

	// Verify tool result
	if detail.Messages[2].Role != "tool" || detail.Messages[2].ToolCallID != "tc_abc" {
		t.Errorf("AC-2 messages[2]: %+v", detail.Messages[2])
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// testStepRecord creates a StepRecord with deterministic test data.
func testStepRecord(step int) types.StepRecord {
	msgs := []rnixctx.Message{
		{Role: rnixctx.RoleUser, Content: fmt.Sprintf("step %d user message", step)},
		{Role: rnixctx.RoleAssistant, Content: fmt.Sprintf("step %d response", step)},
		{Role: rnixctx.RoleTool, Content: "tool output", ToolCallID: fmt.Sprintf("tc_%d", step)},
	}
	msgsJSON, _ := json.Marshal(msgs)

	return types.StepRecord{
		Step:           step,
		Timestamp:      time.Duration(step) * time.Second,
		Messages:       msgsJSON,
		MessageCount:   3,
		TokenCount:     150 * step,
		RawResponse:    fmt.Sprintf(`{"step":%d}`, step),
		Action:         "tool_call",
		Summary:        fmt.Sprintf("step %d summary", step),
		ToolPath:       "/dev/fs",
		ToolInput:      fmt.Sprintf(`{"step":%d}`, step),
		ToolResult:     fmt.Sprintf("result_%d", step),
		ToolDuration:   time.Duration(step*10) * time.Millisecond,
		RequestTokens:  100,
		ResponseTokens: 50,
	}
}

// writeTestSteps writes StepRecords as NDJSON to the expected directory structure.
func writeTestSteps(t *testing.T, baseDir string, pid types.PID, records []types.StepRecord) {
	t.Helper()
	dir := filepath.Join(baseDir, "data", "steps", fmt.Sprintf("%d", pid))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("writeTestSteps mkdir: %v", err)
	}
	f, err := os.Create(filepath.Join(dir, "steps.jsonl"))
	if err != nil {
		t.Fatalf("writeTestSteps create: %v", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, rec := range records {
		if err := enc.Encode(rec); err != nil {
			t.Fatalf("writeTestSteps encode: %v", err)
		}
	}
}
