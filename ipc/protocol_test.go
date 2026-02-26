package ipc

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/gonewx/crux/internal/types"
	"github.com/gonewx/crux/vfs"
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
		Intent:   "test intent",
		Agent:    "code-analyst",
		Model:    "opus",
		MaxSteps: 20,
	}
	data, err := json.Marshal(sr)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded SpawnRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded != sr {
		t.Errorf("got %+v, want %+v", decoded, sr)
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
	expected := "/run/user/1000/crux/crux.sock"
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
	// Should contain "crux-" and end with "crux.sock"
	if len(path) < 10 {
		t.Errorf("path too short: %q", path)
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
