package ipc

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
)

// ============================================================
// ATDD RED PHASE — Story 10.2: rnix log 分类推理日志
// Tests assert EXPECTED behavior. They will NOT COMPILE until
// LogEntryWire, MethodAttachLog, AttachLog, etc. are implemented.
// ============================================================

// --- 10.2-UNIT-018: LogEntryToWire conversion ---

func TestLogEntryToWire(t *testing.T) {
	entry := types.LogEntry{
		Timestamp: 1523 * time.Millisecond,
		PID:       types.PID(5),
		Step:      3,
		Category:  types.LogThink,
		Content:   "analyzing code structure",
		ToolPath:  "",
	}

	wire := LogEntryToWire(entry)

	if wire.TimestampMs != 1523 {
		t.Errorf("TimestampMs = %d, want 1523", wire.TimestampMs)
	}
	if wire.PID != 5 {
		t.Errorf("PID = %d, want 5", wire.PID)
	}
	if wire.Step != 3 {
		t.Errorf("Step = %d, want 3", wire.Step)
	}
	if wire.Category != "think" {
		t.Errorf("Category = %q, want %q", wire.Category, "think")
	}
	if wire.Content != "analyzing code structure" {
		t.Errorf("Content mismatch")
	}
	if wire.ToolPath != "" {
		t.Error("ToolPath should be empty for think category")
	}
}

// --- 10.2-UNIT-019: LogEntryToWire tool category preserves ToolPath ---

func TestLogEntryToWire_ToolPath(t *testing.T) {
	entry := types.LogEntry{
		Timestamp: 200 * time.Millisecond,
		PID:       types.PID(5),
		Step:      2,
		Category:  types.LogTool,
		Content:   "Read src/main.go (2847 bytes)",
		ToolPath:  "/dev/fs",
	}

	wire := LogEntryToWire(entry)

	if wire.Category != "tool" {
		t.Errorf("Category = %q, want %q", wire.Category, "tool")
	}
	if wire.ToolPath != "/dev/fs" {
		t.Errorf("ToolPath = %q, want %q", wire.ToolPath, "/dev/fs")
	}
}

// --- 10.2-UNIT-020: LogEntryWire JSON round-trip ---

func TestLogEntryWire_JSONRoundTrip(t *testing.T) {
	original := LogEntryWire{
		TimestampMs: 2500,
		PID:         types.PID(7),
		Step:        2,
		Category:    "tool",
		Content:     "Read src/main.go (2847 bytes)",
		ToolPath:    "/dev/fs",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded LogEntryWire
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.TimestampMs != original.TimestampMs {
		t.Errorf("TimestampMs = %d, want %d", decoded.TimestampMs, original.TimestampMs)
	}
	if decoded.PID != original.PID {
		t.Errorf("PID = %d, want %d", decoded.PID, original.PID)
	}
	if decoded.Step != original.Step {
		t.Errorf("Step = %d, want %d", decoded.Step, original.Step)
	}
	if decoded.Category != original.Category {
		t.Errorf("Category = %q, want %q", decoded.Category, original.Category)
	}
	if decoded.Content != original.Content {
		t.Errorf("Content mismatch")
	}
	if decoded.ToolPath != original.ToolPath {
		t.Errorf("ToolPath = %q, want %q", decoded.ToolPath, original.ToolPath)
	}
}

// --- 10.2-UNIT-021: LogEntryWire tool_path omitempty ---

func TestLogEntryWire_ToolPathOmitEmpty(t *testing.T) {
	wire := LogEntryWire{
		TimestampMs: 100,
		PID:         types.PID(1),
		Step:        1,
		Category:    "think",
		Content:     "reasoning",
		ToolPath:    "",
	}

	data, err := json.Marshal(wire)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var raw map[string]any
	_ = json.Unmarshal(data, &raw)
	if _, exists := raw["tool_path"]; exists {
		t.Error("tool_path should be omitted when empty (omitempty)")
	}
}

// --- 10.2-UNIT-022: AttachLogRequest marshal round-trip ---

func TestAttachLogRequest_MarshalRoundTrip(t *testing.T) {
	req := AttachLogRequest{PID: types.PID(42)}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded AttachLogRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.PID != 42 {
		t.Errorf("PID = %d, want 42", decoded.PID)
	}
}

// --- 10.2-UNIT-023: MethodAttachLog constant value ---

func TestMethodAttachLog_Constant(t *testing.T) {
	if MethodAttachLog != "attach_log" {
		t.Errorf("MethodAttachLog = %q, want %q", MethodAttachLog, "attach_log")
	}
}

// --- 10.2-UNIT-024: StreamLogEntry constant value ---

func TestStreamLogEntry_Constant(t *testing.T) {
	if StreamLogEntry != "log_entry" {
		t.Errorf("StreamLogEntry = %q, want %q", StreamLogEntry, "log_entry")
	}
}

// --- 10.2-UNIT-025: MethodAttachLog unique among Method constants ---

func TestMethodAttachLog_Unique(t *testing.T) {
	methods := []Method{
		MethodPing, MethodSpawn, MethodListProcs, MethodKill,
		MethodAttachDebug, MethodShutdown, MethodAttachLog,
	}
	seen := make(map[Method]bool)
	for _, m := range methods {
		if seen[m] {
			t.Errorf("duplicate Method constant: %q", m)
		}
		seen[m] = true
	}
}

// --- 10.2-UNIT-026: StreamLogEntry unique among StreamEventType constants ---

func TestStreamLogEntry_Unique(t *testing.T) {
	types := []StreamEventType{
		StreamProgress, StreamComplete, StreamError,
		StreamSyscallEvent, StreamEOF, StreamLogEntry,
	}
	seen := make(map[StreamEventType]bool)
	for _, st := range types {
		if seen[st] {
			t.Errorf("duplicate StreamEventType constant: %q", st)
		}
		seen[st] = true
	}
}

// --- 10.2-INTEG-001: handleAttachLog returns NOT_FOUND for non-existent PID ---

func TestHandleAttachLog_NotFound(t *testing.T) {
	srv, sockPath := setupTestServer(t)
	_ = srv

	conn := dial(t, sockPath)
	defer conn.Close()

	resp := sendRequest(t, conn, MethodAttachLog, AttachLogRequest{PID: 999})
	if resp.OK {
		t.Error("expected !OK for non-existent PID")
	}
	if resp.Error == nil {
		t.Fatal("expected error payload")
	}
	if resp.Error.Code != "NOT_FOUND" {
		t.Errorf("error code = %q, want %q", resp.Error.Code, "NOT_FOUND")
	}
}

// --- 10.2-INTEG-002: AttachLog receives entries in order ---

func TestIntegration_AttachLog_ReceivesEntries(t *testing.T) {
	_, kern, sockPath := setupIntegrationServer(t)

	proc := kernel.NewProcess(0, "log test", nil)
	_ = proc.Start()
	kern.AddProcess(proc)

	go func() {
		time.Sleep(100 * time.Millisecond)
		proc.LogChan <- types.LogEntry{
			Timestamp: 100 * time.Millisecond,
			PID:       proc.PID,
			Step:      1,
			Category:  types.LogThink,
			Content:   "thinking about the problem",
		}
		proc.LogChan <- types.LogEntry{
			Timestamp: 200 * time.Millisecond,
			PID:       proc.PID,
			Step:      1,
			Category:  types.LogTool,
			Content:   "Read src/main.go (2847 bytes)",
			ToolPath:  "/dev/fs",
		}
		proc.LogChan <- types.LogEntry{
			Timestamp: 300 * time.Millisecond,
			PID:       proc.PID,
			Step:      1,
			Category:  types.LogOutput,
			Content:   "done",
		}
		time.Sleep(50 * time.Millisecond)
		close(proc.LogChan)
	}()

	client, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	var entries []LogEntryWire
	err = client.AttachLog(proc.PID, func(lew LogEntryWire) {
		entries = append(entries, lew)
	})
	if err != nil {
		t.Fatalf("AttachLog: %v", err)
	}

	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	wantCategories := []string{"think", "tool", "output"}
	for i, want := range wantCategories {
		if entries[i].Category != want {
			t.Errorf("entry[%d].Category = %q, want %q", i, entries[i].Category, want)
		}
	}

	if entries[1].ToolPath != "/dev/fs" {
		t.Errorf("tool entry ToolPath = %q, want %q", entries[1].ToolPath, "/dev/fs")
	}
}

// --- 10.2-INTEG-003: AttachLog EOF on channel close (process exit) ---

func TestIntegration_AttachLog_EOFOnClose(t *testing.T) {
	_, kern, sockPath := setupIntegrationServer(t)

	proc := kernel.NewProcess(0, "log eof test", nil)
	_ = proc.Start()
	kern.AddProcess(proc)

	go func() {
		proc.LogChan <- types.LogEntry{
			Timestamp: 50 * time.Millisecond,
			PID:       proc.PID,
			Step:      1,
			Category:  types.LogOutput,
			Content:   "final output",
		}
		time.Sleep(50 * time.Millisecond)
		close(proc.LogChan)
	}()

	client, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	var count int
	err = client.AttachLog(proc.PID, func(lew LogEntryWire) {
		count++
	})

	if err != nil {
		t.Fatalf("AttachLog should return nil on clean EOF, got: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 entry before EOF, got %d", count)
	}
}

// --- 10.2-INTEG-004: AttachLog wire format has correct timestamp_ms ---

func TestIntegration_AttachLog_WireTimestamp(t *testing.T) {
	_, kern, sockPath := setupIntegrationServer(t)

	proc := kernel.NewProcess(0, "timestamp test", nil)
	_ = proc.Start()
	kern.AddProcess(proc)

	go func() {
		proc.LogChan <- types.LogEntry{
			Timestamp: 1523 * time.Millisecond,
			PID:       proc.PID,
			Step:      3,
			Category:  types.LogThink,
			Content:   "content",
		}
		time.Sleep(50 * time.Millisecond)
		close(proc.LogChan)
	}()

	client, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	var got LogEntryWire
	_ = client.AttachLog(proc.PID, func(lew LogEntryWire) {
		got = lew
	})

	if got.TimestampMs != 1523 {
		t.Errorf("TimestampMs = %d, want 1523", got.TimestampMs)
	}
	if got.Step != 3 {
		t.Errorf("Step = %d, want 3", got.Step)
	}
}
