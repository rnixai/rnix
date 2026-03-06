package types

import (
	"testing"
	"time"
)

// ============================================================
// ATDD RED PHASE — Story 10.2: rnix log 分类推理日志
// Tests assert EXPECTED behavior. They will NOT COMPILE until
// LogCategory and LogEntry types are added to types.go.
// ============================================================

// --- 10.2-UNIT-001: LogCategory constants exist and have correct values ---

func TestLogCategory_Constants(t *testing.T) {
	if LogThink != "think" {
		t.Errorf("LogThink = %q, want %q", LogThink, "think")
	}
	if LogTool != "tool" {
		t.Errorf("LogTool = %q, want %q", LogTool, "tool")
	}
	if LogOutput != "output" {
		t.Errorf("LogOutput = %q, want %q", LogOutput, "output")
	}
}

// --- 10.2-UNIT-002: LogCategory type is string-based ---

func TestLogCategory_StringType(t *testing.T) {
	var cat LogCategory = "think"
	if string(cat) != "think" {
		t.Errorf("LogCategory string conversion failed: %q", cat)
	}
}

// --- 10.2-UNIT-003: LogEntry struct fields (think category) ---

func TestLogEntry_ThinkFields(t *testing.T) {
	entry := LogEntry{
		Timestamp: 500 * time.Millisecond,
		PID:       PID(5),
		Step:      3,
		Category:  LogThink,
		Content:   "analyzing code structure",
		ToolPath:  "",
	}

	if entry.Timestamp != 500*time.Millisecond {
		t.Errorf("Timestamp = %v, want 500ms", entry.Timestamp)
	}
	if entry.PID != 5 {
		t.Errorf("PID = %d, want 5", entry.PID)
	}
	if entry.Step != 3 {
		t.Errorf("Step = %d, want 3", entry.Step)
	}
	if entry.Category != LogThink {
		t.Errorf("Category = %q, want %q", entry.Category, LogThink)
	}
	if entry.Content != "analyzing code structure" {
		t.Errorf("Content = %q, want %q", entry.Content, "analyzing code structure")
	}
	if entry.ToolPath != "" {
		t.Error("ToolPath should be empty for think category")
	}
}

// --- 10.2-UNIT-004: LogEntry struct fields (tool category) ---

func TestLogEntry_ToolFields(t *testing.T) {
	entry := LogEntry{
		Timestamp: time.Second,
		PID:       PID(5),
		Step:      2,
		Category:  LogTool,
		Content:   "Read src/main.go (2847 bytes)",
		ToolPath:  "/dev/fs",
	}

	if entry.Category != LogTool {
		t.Errorf("Category = %q, want %q", entry.Category, LogTool)
	}
	if entry.ToolPath != "/dev/fs" {
		t.Errorf("ToolPath = %q, want %q", entry.ToolPath, "/dev/fs")
	}
}

// --- 10.2-UNIT-005: LogEntry struct fields (output category) ---

func TestLogEntry_OutputFields(t *testing.T) {
	entry := LogEntry{
		Timestamp: 2100 * time.Millisecond,
		PID:       PID(5),
		Step:      4,
		Category:  LogOutput,
		Content:   "fixed the race condition in main.go",
	}

	if entry.Category != LogOutput {
		t.Errorf("Category = %q, want %q", entry.Category, LogOutput)
	}
	if entry.Content != "fixed the race condition in main.go" {
		t.Errorf("Content mismatch")
	}
}
