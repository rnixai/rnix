package debug

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// 14.1-MODEL-001: RecordEvent JSON serialization contains all required fields
func TestRecordEvent_JSONSerialization(t *testing.T) {
	ev := RecordEvent{
		SeqNum:    1,
		Timestamp: 500 * time.Millisecond,
		PID:       42,
		Type:      RecordSyscall,
		Syscall: &SyscallEventData{
			Syscall:  "Open",
			Args:     map[string]any{"path": "/dev/llm/claude"},
			Result:   "FD(3)",
			Err:      "",
			Duration: 5 * time.Millisecond,
		},
	}

	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	requiredFields := []string{"seq_num", "timestamp", "pid", "type", "syscall"}
	for _, field := range requiredFields {
		if _, ok := parsed[field]; !ok {
			t.Fatalf("missing required field %q in JSON: %s", field, string(data))
		}
	}

	if parsed["type"] != string(RecordSyscall) {
		t.Fatalf("expected type=%q, got %v", RecordSyscall, parsed["type"])
	}

	var decoded RecordEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("roundtrip unmarshal failed: %v", err)
	}
	if decoded.SeqNum != 1 {
		t.Fatalf("expected SeqNum=1, got %d", decoded.SeqNum)
	}
	if decoded.PID != 42 {
		t.Fatalf("expected PID=42, got %d", decoded.PID)
	}
	if decoded.Type != RecordSyscall {
		t.Fatalf("expected Type=%q, got %q", RecordSyscall, decoded.Type)
	}
	if decoded.Syscall == nil {
		t.Fatal("expected Syscall data, got nil")
	}
	if decoded.Syscall.Syscall != "Open" {
		t.Fatalf("expected Syscall.Syscall=Open, got %q", decoded.Syscall.Syscall)
	}
}

// 14.1-MODEL-002: RecordEventType constants
func TestRecordEventType_Constants(t *testing.T) {
	tests := []struct {
		name     string
		typ      RecordEventType
		expected string
	}{
		{"syscall", RecordSyscall, "syscall"},
		{"context_snapshot", RecordContextSnapshot, "context_snapshot"},
		{"llm_response", RecordLLMResponse, "llm_response"},
		{"state_change", RecordStateChange, "state_change"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.typ) != tt.expected {
				t.Fatalf("expected %q, got %q", tt.expected, string(tt.typ))
			}
		})
	}
}

// 14.1-MODEL-003: RecordEvent omitempty
func TestRecordEvent_OmitEmpty(t *testing.T) {
	ev := RecordEvent{
		SeqNum:    1,
		Timestamp: 100 * time.Millisecond,
		PID:       1,
		Type:      RecordSyscall,
		Syscall: &SyscallEventData{
			Syscall:  "Read",
			Args:     map[string]any{"fd": 3},
			Result:   1024,
			Duration: 1 * time.Millisecond,
		},
	}

	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	for _, absent := range []string{"context", "llm", "state"} {
		if _, ok := parsed[absent]; ok {
			t.Fatalf("expected %q to be omitted, but it's present: %s", absent, string(data))
		}
	}

	if _, ok := parsed["syscall"]; !ok {
		t.Fatalf("expected 'syscall' to be present: %s", string(data))
	}
}

// 14.1-MODEL-004: RecordMetadata JSON roundtrip
func TestRecordMetadata_JSONRoundtrip(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	meta := RecordMetadata{
		RecordID:   "42-1709856000",
		PID:        42,
		Intent:     "test intent",
		StartTime:  now,
		EventCount: 128,
		Status:     RecordStatusRecording,
	}

	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var decoded RecordMetadata
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if decoded.RecordID != "42-1709856000" {
		t.Fatalf("expected RecordID=42-1709856000, got %q", decoded.RecordID)
	}
	if decoded.PID != 42 {
		t.Fatalf("expected PID=42, got %d", decoded.PID)
	}
	if decoded.Intent != "test intent" {
		t.Fatalf("expected Intent='test intent', got %q", decoded.Intent)
	}
	if decoded.EventCount != 128 {
		t.Fatalf("expected EventCount=128, got %d", decoded.EventCount)
	}
	if decoded.Status != RecordStatusRecording {
		t.Fatalf("expected Status=%q, got %q", RecordStatusRecording, decoded.Status)
	}

	if string(RecordStatusRecording) != "recording" {
		t.Fatalf("expected RecordStatusRecording=recording, got %q", RecordStatusRecording)
	}
	if string(RecordStatusCompleted) != "completed" {
		t.Fatalf("expected RecordStatusCompleted=completed, got %q", RecordStatusCompleted)
	}
	if string(RecordStatusStopped) != "stopped" {
		t.Fatalf("expected RecordStatusStopped=stopped, got %q", RecordStatusStopped)
	}
}

// 14.1-MODEL-005: RecordEventFromSyscall conversion
func TestRecordEventFromSyscall(t *testing.T) {
	sev := types.SyscallEvent{
		Timestamp: 500 * time.Millisecond,
		PID:       42,
		Syscall:   "Write",
		Args:      map[string]any{"fd": 3, "data": "hello"},
		Result:    5,
		Err:       nil,
		Duration:  2 * time.Millisecond,
	}

	rev := RecordEventFromSyscall(sev)

	if rev.PID != 42 {
		t.Fatalf("expected PID=42, got %d", rev.PID)
	}
	if rev.Type != RecordSyscall {
		t.Fatalf("expected Type=%q, got %q", RecordSyscall, rev.Type)
	}
	if rev.Syscall == nil {
		t.Fatal("expected Syscall data, got nil")
	}
	if rev.Syscall.Syscall != "Write" {
		t.Fatalf("expected Syscall.Syscall=Write, got %q", rev.Syscall.Syscall)
	}
	if rev.Syscall.Duration != 2*time.Millisecond {
		t.Fatalf("expected Duration=2ms, got %v", rev.Syscall.Duration)
	}
}

func TestHashSystemPrompt(t *testing.T) {
	h1 := HashSystemPrompt("hello")
	h2 := HashSystemPrompt("hello")
	h3 := HashSystemPrompt("world")

	if h1 != h2 {
		t.Error("same input should produce same hash")
	}
	if h1 == h3 {
		t.Error("different input should produce different hash")
	}
	if len(h1) != 16 {
		t.Errorf("hash length = %d, want 16 (8 bytes hex)", len(h1))
	}
}

func TestTruncateString(t *testing.T) {
	if got := TruncateString("short", 10); got != "short" {
		t.Errorf("TruncateString short = %q, want %q", got, "short")
	}
	if got := TruncateString("hello world test", 5); got != "hello..." {
		t.Errorf("TruncateString long = %q, want %q", got, "hello...")
	}
	// Verify CJK characters are truncated by rune count, not byte count
	cjk := "你好世界测试"
	if got := TruncateString(cjk, 3); got != "你好世..." {
		t.Errorf("TruncateString CJK = %q, want %q", got, "你好世...")
	}
	// Full CJK string within limit should not be truncated
	if got := TruncateString(cjk, 10); got != cjk {
		t.Errorf("TruncateString CJK no-trunc = %q, want %q", got, cjk)
	}
}
