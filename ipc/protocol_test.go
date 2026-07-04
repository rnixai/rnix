package ipc

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
	"github.com/rnixai/rnix/vfs"
)

func TestRequest_MarshalRoundTrip(t *testing.T) {
	payload, _ := json.Marshal(SpawnRequest{Intent: "分析 README", Model: "sonnet"})
	req := Request{Method: MethodSpawn, Payload: payload}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Request
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Method != MethodSpawn {
		t.Errorf("method = %q, want %q", decoded.Method, MethodSpawn)
	}

	var sp SpawnRequest
	if err := json.Unmarshal(decoded.Payload, &sp); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if sp.Intent != "分析 README" {
		t.Errorf("intent = %q, want %q", sp.Intent, "分析 README")
	}
	if sp.Model != "sonnet" {
		t.Errorf("model = %q, want %q", sp.Model, "sonnet")
	}
}

func TestResponse_OKRoundTrip(t *testing.T) {
	payload, _ := json.Marshal(PingResponse{Version: "0.1.0"})
	resp := Response{OK: true, Payload: payload}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Response
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !decoded.OK {
		t.Error("ok = false, want true")
	}
	if decoded.Error != nil {
		t.Error("error should be nil for OK response")
	}
}

func TestResponse_ErrorRoundTrip(t *testing.T) {
	resp := Response{
		OK:    false,
		Error: &ErrorPayload{Code: "NOT_FOUND", Message: "process not found"},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Response
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.OK {
		t.Error("ok = true, want false")
	}
	if decoded.Error == nil {
		t.Fatal("error should not be nil")
	}
	if decoded.Error.Code != "NOT_FOUND" {
		t.Errorf("code = %q, want %q", decoded.Error.Code, "NOT_FOUND")
	}
}

func TestSpawnRequest_MarshalRoundTrip(t *testing.T) {
	sr := SpawnRequest{
		Intent:    "test intent",
		Agent:     "code-analyst",
		Model:     "opus",
		ParentPID: 42,
		MaxSteps:  20,
	}
	data, err := json.Marshal(sr)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded SpawnRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Intent != sr.Intent || decoded.Agent != sr.Agent || decoded.Model != sr.Model || decoded.ParentPID != sr.ParentPID || decoded.MaxSteps != sr.MaxSteps ||
		decoded.ContextBudget != sr.ContextBudget || decoded.TimeoutMs != sr.TimeoutMs ||
		decoded.ComposeNode != sr.ComposeNode || decoded.PipelineIndex != sr.PipelineIndex || decoded.PipelineTotal != sr.PipelineTotal {
		t.Errorf("got %+v, want %+v", decoded, sr)
	}
}

func TestSpawnRequest_ParentPIDWire(t *testing.T) {
	data, err := json.Marshal(SpawnRequest{Intent: "child", ParentPID: 99})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"parent_pid":99`) {
		t.Fatalf("parent_pid missing from wire payload: %s", data)
	}

	var decoded SpawnRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.ParentPID != 99 {
		t.Fatalf("ParentPID = %d, want 99", decoded.ParentPID)
	}
}

func TestSpawnRequest_ParentPIDOmitempty(t *testing.T) {
	data, err := json.Marshal(SpawnRequest{Intent: "root"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), "parent_pid") {
		t.Fatalf("parent_pid should be omitted when zero: %s", data)
	}
}

func TestKillRequest_MarshalRoundTrip(t *testing.T) {
	kr := KillRequest{PID: 42, Signal: types.SIGTERM}
	data, err := json.Marshal(kr)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded KillRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.PID != 42 {
		t.Errorf("pid = %d, want 42", decoded.PID)
	}
	if decoded.Signal != types.SIGTERM {
		t.Errorf("signal = %d, want %d", decoded.Signal, types.SIGTERM)
	}
}

func TestAttachDebugRequest_MarshalRoundTrip(t *testing.T) {
	ar := AttachDebugRequest{PID: 7}
	data, err := json.Marshal(ar)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded AttachDebugRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.PID != 7 {
		t.Errorf("pid = %d, want 7", decoded.PID)
	}
}

func TestListProcsResponse_MarshalRoundTrip(t *testing.T) {
	resp := ListProcsResponse{
		Processes: []ProcInfoWire{
			{PID: 1, State: types.StateRunning, Intent: "test", Skills: []string{"a"}, CreatedAt: 1000},
			{PID: 2, State: types.StateZombie, Intent: "test2", Skills: []string{}, CreatedAt: 2000},
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded ListProcsResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(decoded.Processes) != 2 {
		t.Fatalf("processes count = %d, want 2", len(decoded.Processes))
	}
	if decoded.Processes[0].PID != 1 {
		t.Errorf("first pid = %d, want 1", decoded.Processes[0].PID)
	}
	if decoded.Processes[1].State != types.StateZombie {
		t.Errorf("second state = %d, want %d", decoded.Processes[1].State, types.StateZombie)
	}
}

func TestStreamEvent_ProgressRoundTrip(t *testing.T) {
	pp := ProgressPayload{Event: "step", PID: 1, Step: 3, Total: 10}
	payload, _ := json.Marshal(pp)
	se := StreamEvent{Type: StreamProgress, Payload: payload}

	data, err := json.Marshal(se)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded StreamEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Type != StreamProgress {
		t.Errorf("type = %q, want %q", decoded.Type, StreamProgress)
	}

	var decodedPP ProgressPayload
	if err := json.Unmarshal(decoded.Payload, &decodedPP); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if decodedPP.Step != 3 || decodedPP.Total != 10 {
		t.Errorf("step=%d total=%d, want 3/10", decodedPP.Step, decodedPP.Total)
	}
}

func TestStreamEvent_SyscallEventRoundTrip(t *testing.T) {
	sew := SyscallEventWire{
		TimestampMs: 1500,
		PID:         3,
		Syscall:     "Open",
		Args:        map[string]any{"path": "/dev/llm/claude"},
		DurationMs:  12.5,
	}
	payload, _ := json.Marshal(sew)
	se := StreamEvent{Type: StreamSyscallEvent, Payload: payload}

	data, err := json.Marshal(se)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded StreamEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Type != StreamSyscallEvent {
		t.Errorf("type = %q, want %q", decoded.Type, StreamSyscallEvent)
	}
}

func TestProcInfoToWire(t *testing.T) {
	now := time.Now()
	p := vfs.ProcInfo{
		PID:        5,
		PPID:       1,
		State:      types.StateRunning,
		Intent:     "analyze",
		Skills:     []string{"code-analyst"},
		TokensUsed: 100,
		CreatedAt:  now,
		CtxID:      types.CtxID(42),
		Result:     "ok",
	}

	w := ProcInfoToWire(p)
	if w.PID != 5 {
		t.Errorf("pid = %d, want 5", w.PID)
	}
	if w.CreatedAt != now.UnixMilli() {
		t.Errorf("created_at = %d, want %d", w.CreatedAt, now.UnixMilli())
	}

	back := WireToProcInfo(w)
	if back.PID != p.PID || back.Intent != p.Intent || back.State != p.State {
		t.Errorf("roundtrip mismatch: got %+v", back)
	}
}

func TestProcInfoToWire_NilSkills(t *testing.T) {
	p := vfs.ProcInfo{PID: 1, Skills: nil}
	w := ProcInfoToWire(p)
	if w.Skills == nil {
		t.Error("skills should be non-nil empty slice for JSON serialization")
	}
	if len(w.Skills) != 0 {
		t.Errorf("skills length = %d, want 0", len(w.Skills))
	}
}

func TestSyscallEventToWire(t *testing.T) {
	e := types.SyscallEvent{
		Timestamp: 3 * time.Second,
		PID:       2,
		Syscall:   "Spawn",
		Args:      map[string]any{"intent": "test"},
		Result:    types.PID(1),
		Duration:  150 * time.Millisecond,
	}

	w := SyscallEventToWire(e)
	if w.TimestampMs != 3000 {
		t.Errorf("timestamp = %d, want 3000", w.TimestampMs)
	}
	if w.PID != 2 {
		t.Errorf("pid = %d, want 2", w.PID)
	}
	if w.Syscall != "Spawn" {
		t.Errorf("syscall = %q, want %q", w.Syscall, "Spawn")
	}
	if w.Error != "" {
		t.Errorf("error = %q, want empty", w.Error)
	}
	if w.DurationMs < 149.9 || w.DurationMs > 150.1 {
		t.Errorf("duration = %f, want ~150.0", w.DurationMs)
	}
}

func TestSyscallEventToWire_WithError(t *testing.T) {
	e := types.SyscallEvent{
		PID:     1,
		Syscall: "Open",
		Err:     os.ErrNotExist,
	}

	w := SyscallEventToWire(e)
	if w.Error == "" {
		t.Error("error should be non-empty")
	}
}

func TestSocketPath_XDGRuntimeDir(t *testing.T) {
	orig := os.Getenv("XDG_RUNTIME_DIR")
	t.Cleanup(func() { os.Setenv("XDG_RUNTIME_DIR", orig) })

	os.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	path := SocketPath()
	expected := "/run/user/1000/rnix/rnix.sock"
	if path != expected {
		t.Errorf("path = %q, want %q", path, expected)
	}
}

func TestSocketPath_Fallback(t *testing.T) {
	orig := os.Getenv("XDG_RUNTIME_DIR")
	t.Cleanup(func() { os.Setenv("XDG_RUNTIME_DIR", orig) })

	os.Unsetenv("XDG_RUNTIME_DIR")
	path := SocketPath()
	if path == "" {
		t.Error("path should not be empty")
	}
	// Should contain "rnix-" and end with "rnix.sock"
	if len(path) < 10 {
		t.Errorf("path too short: %q", path)
	}
}

// ============================================================
// ATDD RED PHASE — Story 10.3: Token 预算管理 (AC5)
//
// Tests reference SpawnRequest.ContextBudget and ProcInfoWire.ContextBudget
// which do NOT exist yet → compile failure = RED phase.
// ============================================================

// --- 10.3-UNIT-030: [P1] ProcInfoWire includes ContextBudget roundtrip ---

func TestProcInfoWire_ContextBudget_RoundTrip(t *testing.T) {
	now := time.Now()
	p := vfs.ProcInfo{
		PID:           5,
		PPID:          1,
		State:         types.StateRunning,
		Intent:        "budget roundtrip",
		Skills:        []string{"test"},
		TokensUsed:    4500,
		CreatedAt:     now,
		CtxID:         types.CtxID(42),
		ContextBudget: 5000,
	}

	w := ProcInfoToWire(p)
	if w.ContextBudget != 5000 {
		t.Errorf("wire ContextBudget: got %d, want 5000", w.ContextBudget)
	}

	back := WireToProcInfo(w)
	if back.ContextBudget != 5000 {
		t.Errorf("roundtrip ContextBudget: got %d, want 5000", back.ContextBudget)
	}
}

// --- 10.3-UNIT-031: [P1] ProcInfoWire ContextBudget=0 omitted in JSON ---

func TestProcInfoWire_ContextBudget_OmitEmpty(t *testing.T) {
	w := ProcInfoWire{
		PID:           1,
		State:         types.StateRunning,
		Skills:        []string{},
		ContextBudget: 0,
	}
	data, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if _, exists := raw["context_budget"]; exists {
		t.Error("context_budget should be omitted when 0 (omitempty)")
	}
}

// --- 10.3-UNIT-032: [P1] ProcInfoWire ContextBudget>0 present in JSON ---

func TestProcInfoWire_ContextBudget_PresentWhenSet(t *testing.T) {
	w := ProcInfoWire{
		PID:           1,
		State:         types.StateRunning,
		Skills:        []string{},
		ContextBudget: 10000,
	}
	data, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	budgetJSON, exists := raw["context_budget"]
	if !exists {
		t.Fatal("context_budget should be present when > 0")
	}

	var budget int
	if err := json.Unmarshal(budgetJSON, &budget); err != nil {
		t.Fatalf("unmarshal budget: %v", err)
	}
	if budget != 10000 {
		t.Errorf("context_budget: got %d, want 10000", budget)
	}
}

// --- 10.3-UNIT-033: [P1] SpawnRequest includes ContextBudget ---

func TestSpawnRequest_ContextBudget_RoundTrip(t *testing.T) {
	sr := SpawnRequest{
		Intent:        "budget spawn",
		Agent:         "test-agent",
		Model:         "sonnet",
		ContextBudget: 8000,
	}
	data, err := json.Marshal(sr)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded SpawnRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.ContextBudget != 8000 {
		t.Errorf("ContextBudget: got %d, want 8000", decoded.ContextBudget)
	}
}

// --- 10.3-UNIT-034: [P2] SpawnRequest ContextBudget=0 omitted ---

func TestSpawnRequest_ContextBudget_OmitEmpty(t *testing.T) {
	sr := SpawnRequest{
		Intent: "no budget",
	}
	data, err := json.Marshal(sr)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if _, exists := raw["context_budget"]; exists {
		t.Error("context_budget should be omitted when 0")
	}
}

func TestMethodConstants(t *testing.T) {
	methods := []Method{MethodPing, MethodSpawn, MethodListProcs, MethodKill, MethodAttachDebug, MethodShutdown}
	seen := make(map[Method]bool)
	for _, m := range methods {
		if m == "" {
			t.Error("method constant should not be empty")
		}
		if seen[m] {
			t.Errorf("duplicate method: %q", m)
		}
		seen[m] = true
	}
}

func TestStreamEventType_Constants(t *testing.T) {
	eventTypes := []StreamEventType{StreamProgress, StreamComplete, StreamError, StreamSyscallEvent, StreamEOF}
	seen := make(map[StreamEventType]bool)
	for _, et := range eventTypes {
		if et == "" {
			t.Error("event type should not be empty")
		}
		if seen[et] {
			t.Errorf("duplicate event type: %q", et)
		}
		seen[et] = true
	}
}

// ============================================================
// ATDD RED PHASE — Story 11.1: 管道语法 (Pipe Syntax)
//
// Tests reference MethodSpawnPipeline, SpawnPipelineRequest,
// SpawnPipelineResponse, SpawnPipelineCommand, PipelineStageWire
// which do NOT exist yet → compile failure = RED phase.
// ============================================================

// --- 11.1-INT-001a: [P1] MethodSpawnPipeline 常量存在且唯一 ---

func TestMethodSpawnPipeline_Exists(t *testing.T) {
	if MethodSpawnPipeline == "" {
		t.Error("MethodSpawnPipeline should not be empty")
	}
	if MethodSpawnPipeline == MethodSpawn {
		t.Error("MethodSpawnPipeline should differ from MethodSpawn")
	}
}

// --- 11.1-INT-001b: [P1] SpawnPipelineRequest 序列化 roundtrip ---

func TestSpawnPipelineRequest_MarshalRoundTrip(t *testing.T) {
	req := SpawnPipelineRequest{
		Commands: []SpawnPipelineCommand{
			{Intent: "分析代码", Agent: "analyst", Model: "sonnet"},
			{Intent: "写文档", Agent: "writer", Model: "opus"},
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded SpawnPipelineRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(decoded.Commands) != 2 {
		t.Fatalf("commands count = %d, want 2", len(decoded.Commands))
	}
	if decoded.Commands[0].Intent != "分析代码" {
		t.Errorf("cmd 0 intent = %q, want %q", decoded.Commands[0].Intent, "分析代码")
	}
	if decoded.Commands[0].Agent != "analyst" {
		t.Errorf("cmd 0 agent = %q, want %q", decoded.Commands[0].Agent, "analyst")
	}
	if decoded.Commands[1].Intent != "写文档" {
		t.Errorf("cmd 1 intent = %q, want %q", decoded.Commands[1].Intent, "写文档")
	}
	if decoded.Commands[1].Model != "opus" {
		t.Errorf("cmd 1 model = %q, want %q", decoded.Commands[1].Model, "opus")
	}
}

// --- 11.1-INT-001c: [P1] SpawnPipelineResponse 序列化 roundtrip ---

func TestSpawnPipelineResponse_MarshalRoundTrip(t *testing.T) {
	resp := SpawnPipelineResponse{
		Stages: []PipelineStageWire{
			{
				PID:        1,
				Intent:     "分析代码",
				Result:     "代码分析报告",
				ExitCode:   0,
				TokensUsed: 100,
				ElapsedMs:  5000,
			},
			{
				PID:        2,
				Intent:     "写文档",
				Result:     "文档内容",
				ExitCode:   0,
				TokensUsed: 200,
				ElapsedMs:  8000,
			},
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded SpawnPipelineResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(decoded.Stages) != 2 {
		t.Fatalf("stages count = %d, want 2", len(decoded.Stages))
	}
	if decoded.Stages[0].PID != 1 {
		t.Errorf("stage 0 pid = %d, want 1", decoded.Stages[0].PID)
	}
	if decoded.Stages[0].Result != "代码分析报告" {
		t.Errorf("stage 0 result = %q, want %q", decoded.Stages[0].Result, "代码分析报告")
	}
	if decoded.Stages[1].TokensUsed != 200 {
		t.Errorf("stage 1 tokens = %d, want 200", decoded.Stages[1].TokensUsed)
	}
	if decoded.Stages[1].ElapsedMs != 8000 {
		t.Errorf("stage 1 elapsed = %d, want 8000", decoded.Stages[1].ElapsedMs)
	}
}

// --- 11.1-INT-001d: [P1] PipelineStageWire ExitCode=0 保留 ---

func TestPipelineStageWire_ZeroExitCode(t *testing.T) {
	stage := PipelineStageWire{
		PID:      1,
		Intent:   "test",
		ExitCode: 0,
	}
	data, err := json.Marshal(stage)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// exit_code should be present even when 0 (not omitempty for pipeline stages)
	if _, exists := raw["exit_code"]; !exists {
		t.Error("exit_code should be present even when 0")
	}
}

// --- 11.1-INT-001e: [P1] SpawnPipelineCommand 可选字段 omitempty ---

func TestSpawnPipelineCommand_OmitEmpty(t *testing.T) {
	cmd := SpawnPipelineCommand{
		Intent: "test",
	}
	data, err := json.Marshal(cmd)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if _, exists := raw["agent"]; exists {
		t.Error("agent should be omitted when empty")
	}
	if _, exists := raw["model"]; exists {
		t.Error("model should be omitted when empty")
	}
}

// --- 11.1-INT-001f: [P2] SpawnPipelineRequest IPC Request envelope ---

func TestSpawnPipelineRequest_IPCEnvelope(t *testing.T) {
	payload, _ := json.Marshal(SpawnPipelineRequest{
		Commands: []SpawnPipelineCommand{
			{Intent: "A"},
			{Intent: "B"},
		},
	})
	req := Request{Method: MethodSpawnPipeline, Payload: payload}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Request
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Method != MethodSpawnPipeline {
		t.Errorf("method = %q, want %q", decoded.Method, MethodSpawnPipeline)
	}

	var sp SpawnPipelineRequest
	if err := json.Unmarshal(decoded.Payload, &sp); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if len(sp.Commands) != 2 {
		t.Errorf("commands = %d, want 2", len(sp.Commands))
	}
}

// ============================================================
// ATDD RED PHASE — Story 13.1: gdb 调试会话管理 (Attach/Detach)
//
// Tests reference MethodAttachGdb, AttachGdbRequest,
// AttachGdbResponse, DetachGdbRequest, GdbEventType,
// StreamGdbSyscall, StreamGdbLog, StreamGdbStateChange
// which do NOT exist yet → compile failure = RED phase.
// ============================================================

// --- 13.1-UNIT-001: [P0] MethodAttachGdb 常量存在且唯一 ---

func TestMethodAttachGdb_Exists(t *testing.T) {
	if MethodAttachGdb == "" {
		t.Error("MethodAttachGdb should not be empty")
	}
	if MethodAttachGdb == MethodAttachDebug {
		t.Error("MethodAttachGdb should differ from MethodAttachDebug")
	}
	if MethodAttachGdb == MethodAttachLog {
		t.Error("MethodAttachGdb should differ from MethodAttachLog")
	}
}

// --- 13.1-UNIT-002: [P0] AttachGdbRequest 序列化 roundtrip ---

func TestAttachGdbRequest_MarshalRoundTrip(t *testing.T) {
	req := AttachGdbRequest{PID: 42}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded AttachGdbRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.PID != 42 {
		t.Errorf("pid = %d, want 42", decoded.PID)
	}
}

// --- 13.1-UNIT-003: [P0] AttachGdbResponse 序列化 roundtrip ---

func TestAttachGdbResponse_MarshalRoundTrip(t *testing.T) {
	resp := AttachGdbResponse{
		PID:        7,
		State:      types.StateRunning,
		Intent:     "分析代码",
		Skills:     []string{"code-analyst", "reviewer"},
		TokensUsed: 500,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded AttachGdbResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.PID != 7 {
		t.Errorf("pid = %d, want 7", decoded.PID)
	}
	if decoded.State != types.StateRunning {
		t.Errorf("state = %d, want %d", decoded.State, types.StateRunning)
	}
	if decoded.Intent != "分析代码" {
		t.Errorf("intent = %q, want %q", decoded.Intent, "分析代码")
	}
	if len(decoded.Skills) != 2 {
		t.Fatalf("skills count = %d, want 2", len(decoded.Skills))
	}
	if decoded.Skills[0] != "code-analyst" {
		t.Errorf("skill[0] = %q, want %q", decoded.Skills[0], "code-analyst")
	}
	if decoded.TokensUsed != 500 {
		t.Errorf("tokens_used = %d, want 500", decoded.TokensUsed)
	}
}

// --- 13.1-UNIT-004: [P1] AttachGdbResponse nil Skills → 空数组 ---

func TestAttachGdbResponse_NilSkills(t *testing.T) {
	resp := AttachGdbResponse{
		PID:    1,
		State:  types.StateRunning,
		Skills: nil,
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Skills should serialize as [] not null
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	skillsJSON := string(raw["skills"])
	if skillsJSON == "null" {
		t.Error("skills should be [] not null for JSON portability")
	}
}

// --- 13.1-UNIT-005: [P0] DetachGdbRequest 序列化 roundtrip ---

func TestDetachGdbRequest_MarshalRoundTrip(t *testing.T) {
	req := DetachGdbRequest{PID: 15}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded DetachGdbRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.PID != 15 {
		t.Errorf("pid = %d, want 15", decoded.PID)
	}
}

// --- 13.1-UNIT-006: [P0] GdbEventType 常量存在且唯一 ---

func TestGdbEventType_Constants(t *testing.T) {
	eventTypes := []StreamEventType{StreamGdbSyscall, StreamGdbLog, StreamGdbStateChange}
	seen := make(map[StreamEventType]bool)
	for _, et := range eventTypes {
		if et == "" {
			t.Error("gdb event type should not be empty")
		}
		if seen[et] {
			t.Errorf("duplicate gdb event type: %q", et)
		}
		seen[et] = true
	}

	// Verify distinct from existing stream event types
	existingTypes := []StreamEventType{StreamProgress, StreamComplete, StreamError, StreamSyscallEvent, StreamLogEntry, StreamEOF}
	for _, gdbType := range eventTypes {
		for _, existingType := range existingTypes {
			if gdbType == existingType {
				t.Errorf("gdb event type %q conflicts with existing type", gdbType)
			}
		}
	}
}

// --- 13.1-UNIT-007: [P1] AttachGdbRequest IPC Request envelope ---

func TestAttachGdbRequest_IPCEnvelope(t *testing.T) {
	payload, _ := json.Marshal(AttachGdbRequest{PID: 99})
	req := Request{Method: MethodAttachGdb, Payload: payload}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Request
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Method != MethodAttachGdb {
		t.Errorf("method = %q, want %q", decoded.Method, MethodAttachGdb)
	}

	var ar AttachGdbRequest
	if err := json.Unmarshal(decoded.Payload, &ar); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if ar.PID != 99 {
		t.Errorf("pid = %d, want 99", ar.PID)
	}
}

// --- 13.1-UNIT-008: [P1] AttachGdbResponse 含完整进程元信息 ---

func TestAttachGdbResponse_FullMetadata(t *testing.T) {
	resp := AttachGdbResponse{
		PID:        3,
		State:      types.StateRunning,
		Intent:     "测试意图",
		Skills:     []string{"skill-a", "skill-b", "skill-c"},
		TokensUsed: 12345,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}

	requiredFields := []string{"pid", "state", "intent", "skills", "tokens_used"}
	for _, field := range requiredFields {
		if _, exists := raw[field]; !exists {
			t.Errorf("required field %q missing from AttachGdbResponse JSON", field)
		}
	}
}

// ============================================================
// ATDD RED PHASE — Story 13.2: 断点系统 (IPC 协议扩展)
//
// Tests reference MethodGdbCommand, GdbCommandRequest,
// GdbCommandResponse, StreamGdbPrompt
// which do NOT exist yet → compile failure = RED phase.
// ============================================================

// --- 13.2-IPC-001: [P0] MethodGdbCommand 常量存在且唯一 ---

func TestMethodGdbCommand_Exists(t *testing.T) {
	if MethodGdbCommand == "" {
		t.Error("MethodGdbCommand should not be empty")
	}
	if MethodGdbCommand == MethodAttachGdb {
		t.Error("MethodGdbCommand should differ from MethodAttachGdb")
	}
	if MethodGdbCommand == MethodDetachGdb {
		t.Error("MethodGdbCommand should differ from MethodDetachGdb")
	}
	if MethodGdbCommand == MethodSpawn {
		t.Error("MethodGdbCommand should differ from MethodSpawn")
	}
}

// --- 13.2-IPC-002: [P0] GdbCommandRequest 序列化 roundtrip ---

func TestGdbCommandRequest_MarshalRoundTrip(t *testing.T) {
	req := GdbCommandRequest{
		PID:     42,
		Command: "break",
		Args:    []string{"syscall", "Read"},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded GdbCommandRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.PID != 42 {
		t.Errorf("pid = %d, want 42", decoded.PID)
	}
	if decoded.Command != "break" {
		t.Errorf("command = %q, want %q", decoded.Command, "break")
	}
	if len(decoded.Args) != 2 || decoded.Args[0] != "syscall" || decoded.Args[1] != "Read" {
		t.Errorf("args = %v, want [syscall Read]", decoded.Args)
	}
}

// --- 13.2-IPC-003: [P0] GdbCommandResponse 序列化 roundtrip ---

func TestGdbCommandResponse_MarshalRoundTrip(t *testing.T) {
	resp := GdbCommandResponse{
		OK:      true,
		Message: "Breakpoint 1 set on syscall Read",
		Data:    map[string]any{"bp_id": float64(1)},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded GdbCommandResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !decoded.OK {
		t.Error("OK should be true")
	}
	if decoded.Message != "Breakpoint 1 set on syscall Read" {
		t.Errorf("message = %q", decoded.Message)
	}
	if decoded.Data == nil {
		t.Fatal("data should not be nil")
	}
	dataMap, ok := decoded.Data.(map[string]any)
	if !ok {
		t.Fatalf("data should be map[string]any, got %T", decoded.Data)
	}
	if dataMap["bp_id"] != float64(1) {
		t.Errorf("bp_id = %v, want 1", dataMap["bp_id"])
	}
}

// --- 13.2-IPC-004: [P1] GdbCommandResponse error case ---

func TestGdbCommandResponse_ErrorCase(t *testing.T) {
	resp := GdbCommandResponse{
		OK:      false,
		Message: "unknown command: foo",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded GdbCommandResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.OK {
		t.Error("OK should be false")
	}
	if decoded.Message != "unknown command: foo" {
		t.Errorf("message = %q", decoded.Message)
	}
}

// --- 13.2-IPC-005: [P0] GdbCommandRequest IPC envelope ---

func TestGdbCommandRequest_IPCEnvelope(t *testing.T) {
	payload, _ := json.Marshal(GdbCommandRequest{
		PID:     7,
		Command: "continue",
	})
	req := Request{Method: MethodGdbCommand, Payload: payload}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Request
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Method != MethodGdbCommand {
		t.Errorf("method = %q, want %q", decoded.Method, MethodGdbCommand)
	}

	var gc GdbCommandRequest
	if err := json.Unmarshal(decoded.Payload, &gc); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if gc.PID != 7 {
		t.Errorf("pid = %d, want 7", gc.PID)
	}
	if gc.Command != "continue" {
		t.Errorf("command = %q, want %q", gc.Command, "continue")
	}
}

// --- 13.2-IPC-006: [P0] StreamGdbPrompt 事件类型存在 ---

func TestStreamGdbPrompt_Exists(t *testing.T) {
	if StreamGdbPrompt == "" {
		t.Error("StreamGdbPrompt should not be empty")
	}
	// Must be distinct from other stream types
	otherTypes := []StreamEventType{
		StreamProgress, StreamComplete, StreamError,
		StreamSyscallEvent, StreamLogEntry, StreamEOF,
		StreamGdbSyscall, StreamGdbLog, StreamGdbStateChange,
	}
	for _, ot := range otherTypes {
		if StreamGdbPrompt == ot {
			t.Errorf("StreamGdbPrompt %q conflicts with existing type %q", StreamGdbPrompt, ot)
		}
	}
}

// --- 13.2-IPC-007: [P1] GdbCommandRequest Args 为空时 omitempty ---

func TestGdbCommandRequest_ArgsOmitEmpty(t *testing.T) {
	req := GdbCommandRequest{
		PID:     1,
		Command: "continue",
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if _, exists := raw["args"]; exists {
		t.Error("args should be omitted when nil/empty")
	}
}

// --- 13.2-IPC-008: [P1] GdbCommandResponse Data 为空时 omitempty ---

func TestGdbCommandResponse_DataOmitEmpty(t *testing.T) {
	resp := GdbCommandResponse{
		OK:      true,
		Message: "resumed",
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if _, exists := raw["data"]; exists {
		t.Error("data should be omitted when nil")
	}
}

// ============================================================
// ATDD RED PHASE — Story 21.1: Token 预算池与分配调度 (IPC)
//
// Tests reference MethodBudgetStatus, BudgetStatusRequest,
// BudgetStatusResponse, AgentQuotaWire which do NOT exist yet.
// They will fail to COMPILE until implementation adds these types.
//
// RED → GREEN: implement the IPC types, tests compile and pass.
// ============================================================

// --- 21.1-IPC-001: [P0] BudgetStatusRequest marshal roundtrip ---

func TestBudgetStatusRequest_MarshalRoundTrip(t *testing.T) {
	req := BudgetStatusRequest{GroupID: types.PGID(100)}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded BudgetStatusRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.GroupID != types.PGID(100) {
		t.Errorf("group_id = %d, want 100", decoded.GroupID)
	}
}

// --- 21.1-IPC-002: [P0] BudgetStatusResponse marshal roundtrip ---

func TestBudgetStatusResponse_MarshalRoundTrip(t *testing.T) {
	resp := BudgetStatusResponse{
		TotalBudget: 50000,
		Allocated:   45000,
		Consumed:    30000,
		Remaining:   20000,
		Quotas: []AgentQuotaWire{
			{PID: 1, Name: "reviewer", Priority: "high", Quota: 30000, Consumed: 20000},
			{PID: 2, Name: "summarizer", Priority: "normal", Quota: 15000, Consumed: 10000},
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded BudgetStatusResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.TotalBudget != 50000 {
		t.Errorf("total_budget = %d, want 50000", decoded.TotalBudget)
	}
	if decoded.Allocated != 45000 {
		t.Errorf("allocated = %d, want 45000", decoded.Allocated)
	}
	if decoded.Consumed != 30000 {
		t.Errorf("consumed = %d, want 30000", decoded.Consumed)
	}
	if decoded.Remaining != 20000 {
		t.Errorf("remaining = %d, want 20000", decoded.Remaining)
	}
	if len(decoded.Quotas) != 2 {
		t.Fatalf("quotas count = %d, want 2", len(decoded.Quotas))
	}
	if decoded.Quotas[0].Name != "reviewer" {
		t.Errorf("quota[0] name = %q, want 'reviewer'", decoded.Quotas[0].Name)
	}
	if decoded.Quotas[0].Priority != "high" {
		t.Errorf("quota[0] priority = %q, want 'high'", decoded.Quotas[0].Priority)
	}
}

// --- 21.1-IPC-003: [P0] MethodBudgetStatus constant exists ---

func TestMethodBudgetStatus_Exists(t *testing.T) {
	if MethodBudgetStatus == "" {
		t.Error("MethodBudgetStatus should not be empty")
	}
	// Must be distinct from other methods
	otherMethods := []Method{
		MethodPing, MethodSpawn, MethodListProcs, MethodKill,
		MethodAttachDebug, MethodShutdown, MethodProviderStatus,
	}
	for _, m := range otherMethods {
		if MethodBudgetStatus == m {
			t.Errorf("MethodBudgetStatus %q conflicts with existing method %q", MethodBudgetStatus, m)
		}
	}
}

// --- 21.1-IPC-004: [P1] BudgetStatusRequest IPC envelope ---

func TestBudgetStatusRequest_IPCEnvelope(t *testing.T) {
	payload, _ := json.Marshal(BudgetStatusRequest{GroupID: types.PGID(42)})
	req := Request{Method: MethodBudgetStatus, Payload: payload}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Request
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Method != MethodBudgetStatus {
		t.Errorf("method = %q, want %q", decoded.Method, MethodBudgetStatus)
	}

	var bs BudgetStatusRequest
	if err := json.Unmarshal(decoded.Payload, &bs); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if bs.GroupID != types.PGID(42) {
		t.Errorf("group_id = %d, want 42", bs.GroupID)
	}
}

// ==========================================================================
// Story 21.2: SLA Status IPC protocol tests
// ==========================================================================

// --- 21.2-IPC-001: SLAStatusRequest marshal roundtrip ---

func TestSLAStatusRequest_MarshalRoundTrip(t *testing.T) {
	req := SLAStatusRequest{GroupID: types.PGID(200)}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded SLAStatusRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.GroupID != types.PGID(200) {
		t.Errorf("group_id = %d, want 200", decoded.GroupID)
	}
}

// --- 21.2-IPC-002: SLAStatusResponse marshal roundtrip ---

func TestSLAStatusResponse_MarshalRoundTrip(t *testing.T) {
	resp := SLAStatusResponse{
		Results: []SLAResultWire{
			{
				AgentName:  "reviewer",
				Passed:     true,
				Checks:     []kernel.SLACheckResult{{Name: "max_tokens", Passed: true, Actual: "5000", Limit: "15000"}},
				TokensUsed: 5000,
				DurationMs: 30000,
			},
			{
				AgentName:  "summarizer",
				Passed:     false,
				Checks:     []kernel.SLACheckResult{{Name: "max_tokens", Passed: false, Actual: "12000", Limit: "8000"}},
				TokensUsed: 12000,
				DurationMs: 20000,
			},
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded SLAStatusResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(decoded.Results) != 2 {
		t.Fatalf("results count = %d, want 2", len(decoded.Results))
	}
	if decoded.Results[0].AgentName != "reviewer" {
		t.Errorf("result[0] agent = %q, want 'reviewer'", decoded.Results[0].AgentName)
	}
	if !decoded.Results[0].Passed {
		t.Error("result[0] should have passed")
	}
	if decoded.Results[1].Passed {
		t.Error("result[1] should have failed")
	}
	if decoded.Results[1].TokensUsed != 12000 {
		t.Errorf("result[1] tokens_used = %d, want 12000", decoded.Results[1].TokensUsed)
	}
}

// --- 21.2-IPC-003: SLAStatusResponse empty results ---

func TestSLAStatusResponse_EmptyResults(t *testing.T) {
	resp := SLAStatusResponse{Results: []SLAResultWire{}}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded SLAStatusResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(decoded.Results) != 0 {
		t.Errorf("results count = %d, want 0", len(decoded.Results))
	}
}

// --- 21.2-IPC-004: MethodSLAStatus constant exists and is unique ---

func TestMethodSLAStatus_Exists(t *testing.T) {
	if MethodSLAStatus == "" {
		t.Error("MethodSLAStatus should not be empty")
	}
	otherMethods := []Method{
		MethodPing, MethodSpawn, MethodListProcs, MethodKill,
		MethodAttachDebug, MethodShutdown, MethodProviderStatus,
		MethodBudgetStatus,
	}
	for _, m := range otherMethods {
		if MethodSLAStatus == m {
			t.Errorf("MethodSLAStatus %q conflicts with method %q", MethodSLAStatus, m)
		}
	}
}

// --- 21.2-IPC-005: SLAStatusRequest IPC envelope roundtrip ---

func TestSLAStatusRequest_IPCEnvelope(t *testing.T) {
	payload, _ := json.Marshal(SLAStatusRequest{GroupID: types.PGID(77)})
	req := Request{Method: MethodSLAStatus, Payload: payload}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Request
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Method != MethodSLAStatus {
		t.Errorf("method = %q, want %q", decoded.Method, MethodSLAStatus)
	}

	var ss SLAStatusRequest
	if err := json.Unmarshal(decoded.Payload, &ss); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if ss.GroupID != types.PGID(77) {
		t.Errorf("group_id = %d, want 77", ss.GroupID)
	}
}

// ============================================================
// Story 25-3: Project Config Merge & Module Adaptation
//
// Tests verify ProjectDir field serialization on SpawnRequest,
// SpawnPipelineRequest, and ExecScriptRequest.
// ============================================================

// --- 25.3-IPC-001: SpawnRequest with ProjectDir round-trip ---

func TestSpawnRequest_ProjectDir_MarshalRoundTrip(t *testing.T) {
	sr := SpawnRequest{
		Intent:     "analyze project",
		Agent:      "code-analyst",
		Model:      "sonnet",
		ProjectDir: "/home/user/my-project",
	}
	data, err := json.Marshal(sr)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded SpawnRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.ProjectDir != "/home/user/my-project" {
		t.Errorf("ProjectDir = %q, want %q", decoded.ProjectDir, "/home/user/my-project")
	}
	if decoded.Intent != "analyze project" {
		t.Errorf("Intent = %q, want %q", decoded.Intent, "analyze project")
	}
	if decoded.Agent != "code-analyst" {
		t.Errorf("Agent = %q, want %q", decoded.Agent, "code-analyst")
	}
}

// --- 25.3-IPC-002: SpawnRequest empty ProjectDir is omitted ---

func TestSpawnRequest_ProjectDir_Omitempty(t *testing.T) {
	sr := SpawnRequest{
		Intent: "no project dir",
	}
	data, err := json.Marshal(sr)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if _, exists := raw["project_dir"]; exists {
		t.Error("project_dir should be omitted when empty (omitempty)")
	}
}

// --- 25.3-IPC-003: SpawnPipelineRequest with ProjectDir ---

func TestSpawnPipelineRequest_ProjectDir(t *testing.T) {
	req := SpawnPipelineRequest{
		Commands: []SpawnPipelineCommand{
			{Intent: "step one", Agent: "analyst"},
		},
		ProjectDir: "/opt/projects/alpha",
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded SpawnPipelineRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.ProjectDir != "/opt/projects/alpha" {
		t.Errorf("ProjectDir = %q, want %q", decoded.ProjectDir, "/opt/projects/alpha")
	}
	if len(decoded.Commands) != 1 {
		t.Fatalf("commands count = %d, want 1", len(decoded.Commands))
	}
	if decoded.Commands[0].Intent != "step one" {
		t.Errorf("cmd[0] intent = %q, want %q", decoded.Commands[0].Intent, "step one")
	}

	// Empty ProjectDir should be omitted
	reqEmpty := SpawnPipelineRequest{
		Commands: []SpawnPipelineCommand{{Intent: "test"}},
	}
	dataEmpty, _ := json.Marshal(reqEmpty)
	var rawEmpty map[string]json.RawMessage
	if err := json.Unmarshal(dataEmpty, &rawEmpty); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if _, exists := rawEmpty["project_dir"]; exists {
		t.Error("project_dir should be omitted when empty in SpawnPipelineRequest")
	}
}

// --- 25.3-IPC-004: ExecScriptRequest with ProjectDir ---

func TestExecScriptRequest_ProjectDir(t *testing.T) {
	req := ExecScriptRequest{
		Script:     "spawn code-analyst \"test\"",
		ProjectDir: "/home/user/workspace",
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded ExecScriptRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.ProjectDir != "/home/user/workspace" {
		t.Errorf("ProjectDir = %q, want %q", decoded.ProjectDir, "/home/user/workspace")
	}
	if decoded.Script != "spawn code-analyst \"test\"" {
		t.Errorf("Script = %q, want %q", decoded.Script, `spawn code-analyst "test"`)
	}

	// Empty ProjectDir should be omitted
	reqEmpty := ExecScriptRequest{Script: "echo hello"}
	dataEmpty, _ := json.Marshal(reqEmpty)
	var rawEmpty map[string]json.RawMessage
	if err := json.Unmarshal(dataEmpty, &rawEmpty); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if _, exists := rawEmpty["project_dir"]; exists {
		t.Error("project_dir should be omitted when empty in ExecScriptRequest")
	}
}
