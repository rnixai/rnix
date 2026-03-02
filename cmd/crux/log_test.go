package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gonewx/crux/internal/types"
	"github.com/gonewx/crux/internal/ui"
	"github.com/gonewx/crux/ipc"
)

// --- 10.2-UNIT-001: FormatLogEntry think category ---

func TestFormatLogEntry_Think(t *testing.T) {
	ui.InitStyles(ui.TerminalProfile{ColorLevel: 0})
	r := &ui.Renderer{OutputMode: ui.ModeDefault, Profile: ui.TerminalProfile{ColorLevel: 0}}

	lew := ipc.LogEntryWire{
		TimestampMs: 523,
		PID:         5,
		Step:        1,
		Category:    "think",
		Content:     "analyzing code structure",
	}

	out := FormatLogEntry(r, lew)
	if !strings.Contains(out, "[think]") {
		t.Errorf("expected [think], got %q", out)
	}
	if !strings.Contains(out, "analyzing code structure") {
		t.Errorf("expected content, got %q", out)
	}
	if !strings.Contains(out, "0.523") {
		t.Errorf("expected timestamp 0.523, got %q", out)
	}
}

// --- 10.2-UNIT-002: FormatLogEntry tool category with ToolPath ---

func TestFormatLogEntry_Tool(t *testing.T) {
	ui.InitStyles(ui.TerminalProfile{ColorLevel: 0})
	r := &ui.Renderer{OutputMode: ui.ModeDefault, Profile: ui.TerminalProfile{ColorLevel: 0}}

	lew := ipc.LogEntryWire{
		TimestampMs: 524,
		PID:         5,
		Step:        1,
		Category:    "tool",
		Content:     "Read src/main.go (2847 bytes)",
		ToolPath:    "/dev/fs",
	}

	out := FormatLogEntry(r, lew)
	if !strings.Contains(out, "[tool]") {
		t.Errorf("expected [tool], got %q", out)
	}
	if !strings.Contains(out, "/dev/fs") {
		t.Errorf("expected tool path, got %q", out)
	}
	if !strings.Contains(out, "→") {
		t.Errorf("expected arrow separator, got %q", out)
	}
}

// --- 10.2-UNIT-003: FormatLogEntry output category ---

func TestFormatLogEntry_Output(t *testing.T) {
	ui.InitStyles(ui.TerminalProfile{ColorLevel: 0})
	r := &ui.Renderer{OutputMode: ui.ModeDefault, Profile: ui.TerminalProfile{ColorLevel: 0}}

	lew := ipc.LogEntryWire{
		TimestampMs: 2100,
		PID:         5,
		Step:        3,
		Category:    "output",
		Content:     "Fixed the race condition",
	}

	out := FormatLogEntry(r, lew)
	if !strings.Contains(out, "[output]") {
		t.Errorf("expected [output], got %q", out)
	}
	if !strings.Contains(out, "Fixed the race condition") {
		t.Errorf("expected content, got %q", out)
	}
}

// --- 10.2-UNIT-004: FormatLogEntry colored output ---

func TestFormatLogEntry_Colored(t *testing.T) {
	ui.InitStyles(ui.TerminalProfile{ColorLevel: 3})
	r := &ui.Renderer{OutputMode: ui.ModeDefault, Profile: ui.TerminalProfile{ColorLevel: 3}}

	cases := []struct {
		category string
		want     string
	}{
		{"think", "[think]"},
		{"tool", "[tool]"},
		{"output", "[output]"},
	}

	for _, tc := range cases {
		lew := ipc.LogEntryWire{
			TimestampMs: 100,
			PID:         1,
			Step:        1,
			Category:    tc.category,
			Content:     "test",
		}
		out := FormatLogEntry(r, lew)
		plain := ui.StripAnsi(out)
		if !strings.Contains(plain, tc.want) {
			t.Errorf("colored output for %q: stripped output %q should contain %q", tc.category, plain, tc.want)
		}
	}
}

// --- 10.2-UNIT-005: filter validation ---

func TestValidLogCategories(t *testing.T) {
	for _, cat := range []string{"think", "tool", "output"} {
		if !validLogCategories[cat] {
			t.Errorf("%q should be a valid log category", cat)
		}
	}
	for _, bad := range []string{"", "debug", "error", "info"} {
		if validLogCategories[bad] {
			t.Errorf("%q should NOT be a valid log category", bad)
		}
	}
}

// --- 10.2-UNIT-027: log command registered ---

func TestLogCommand_Registered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "log" {
			found = true
			break
		}
	}
	if !found {
		t.Error("'log' command should be registered in rootCmd")
	}
}

// --- 10.2-UNIT-028: log command has --filter flag ---

func TestLogCommand_HasFilterFlag(t *testing.T) {
	f := logCmd.Flags().Lookup("filter")
	if f == nil {
		t.Fatal("logCmd should have --filter flag")
	}
	if f.DefValue != "" {
		t.Errorf("--filter default should be empty, got %q", f.DefValue)
	}
}

// --- 10.2-UNIT-029: formatLogTimestamp formatting ---

func TestFormatLogTimestamp(t *testing.T) {
	tests := []struct {
		ms   int64
		want string
	}{
		{0, "  0.000"},
		{523, "  0.523"},
		{1523, "  1.523"},
		{60500, " 60.500"},
	}

	for _, tc := range tests {
		got := formatLogTimestamp(time.Duration(tc.ms) * time.Millisecond)
		if got != tc.want {
			t.Errorf("formatLogTimestamp(%d ms) = %q, want %q", tc.ms, got, tc.want)
		}
	}
}

// --- 10.2-UNIT-030: LogEntryWire JSON output (--json mode) ---

func TestLogEntryWire_NDJSON(t *testing.T) {
	lew := ipc.LogEntryWire{
		TimestampMs: 1523,
		PID:         types.PID(5),
		Step:        3,
		Category:    "think",
		Content:     "analyzing code",
	}

	data, err := json.Marshal(lew)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	s := string(data)
	for _, field := range []string{"timestamp_ms", "pid", "step", "category", "content"} {
		if !strings.Contains(s, field) {
			t.Errorf("JSON should contain %q: %s", field, s)
		}
	}
	if strings.Contains(s, "tool_path") {
		t.Error("tool_path should be omitted when empty")
	}
}

// --- 10.2-UNIT-031: PID not found handled via IPC ---

func TestRunLog_PIDNotFound_ViaIPC(t *testing.T) {
	sockPath, _ := setupTestIPCServer(t)

	ipc.SocketPathOverride = sockPath
	t.Cleanup(func() { ipc.SocketPathOverride = "" })

	savedJSON := flagJSON
	savedFilter := flagFilter
	savedExitCode := exitCode
	defer func() {
		flagJSON = savedJSON
		flagFilter = savedFilter
		exitCode = savedExitCode
	}()
	flagJSON = false
	flagFilter = ""
	exitCode = 0

	cmd := logCmd
	var buf strings.Builder
	cmd.SetOut(&buf)
	defer cmd.SetOut(nil)

	err := runLog(cmd, []string{"999"})
	if err != nil {
		t.Fatalf("runLog should return nil (errors handled internally), got %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "process not found") {
		t.Errorf("AC4: expected 'process not found' in output, got %q", output)
	}
	if exitCode != 1 {
		t.Errorf("expected exitCode=1 for PID not found, got %d", exitCode)
	}
}
