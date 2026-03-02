package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gonewx/crux/ipc"
	"github.com/gonewx/crux/internal/types"
)

// ============================================================
// ATDD RED PHASE — Story 10.2: crux log 分类推理日志
// Tests assert EXPECTED behavior. They will NOT COMPILE until
// cmd/crux/log.go is created with the tested functions.
// ============================================================

// --- 10.2-UNIT-027: logCmd registered in rootCmd ---

func TestLogCmd_Registered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if strings.HasPrefix(cmd.Use, "log") {
			found = true
			break
		}
	}
	if !found {
		t.Error("logCmd not registered in rootCmd")
	}
}

// --- 10.2-UNIT-028: logCmd has --filter flag ---

func TestLogCmd_HasFilterFlag(t *testing.T) {
	f := logCmd.Flags().Lookup("filter")
	if f == nil {
		t.Fatal("logCmd should have a --filter flag")
	}
	if f.DefValue != "" {
		t.Errorf("--filter default should be empty, got %q", f.DefValue)
	}
}

// --- 10.2-UNIT-029: logCmd has --json flag ---

func TestLogCmd_HasJSONFlag(t *testing.T) {
	f := logCmd.Flags().Lookup("json")
	if f == nil {
		t.Fatal("logCmd should have a --json flag")
	}
}

// --- 10.2-UNIT-030: isValidLogFilter accepts valid categories ---

func TestIsValidLogFilter_Valid(t *testing.T) {
	tests := []struct {
		filter string
		valid  bool
	}{
		{"", true},
		{"think", true},
		{"tool", true},
		{"output", true},
	}

	for _, tt := range tests {
		if got := isValidLogFilter(tt.filter); got != tt.valid {
			t.Errorf("isValidLogFilter(%q) = %v, want %v", tt.filter, got, tt.valid)
		}
	}
}

// --- 10.2-UNIT-031: isValidLogFilter rejects invalid categories ---

func TestIsValidLogFilter_Invalid(t *testing.T) {
	invalids := []string{"invalid", "THINK", "Tool", "debug", "all", "error"}
	for _, filter := range invalids {
		if isValidLogFilter(filter) {
			t.Errorf("isValidLogFilter(%q) should be false", filter)
		}
	}
}

// --- 10.2-UNIT-032: shouldShowEntry respects filter ---

func TestShouldShowEntry_Filter(t *testing.T) {
	tests := []struct {
		category string
		filter   string
		want     bool
	}{
		{"think", "", true},
		{"tool", "", true},
		{"output", "", true},
		{"think", "think", true},
		{"think", "tool", false},
		{"think", "output", false},
		{"tool", "tool", true},
		{"tool", "think", false},
		{"output", "output", true},
		{"output", "think", false},
	}

	for _, tt := range tests {
		entry := ipc.LogEntryWire{Category: tt.category}
		got := shouldShowEntry(entry, tt.filter)
		if got != tt.want {
			t.Errorf("shouldShowEntry(cat=%q, filter=%q) = %v, want %v",
				tt.category, tt.filter, got, tt.want)
		}
	}
}

// --- 10.2-UNIT-033: formatLogEntry human-readable output contains category tag ---

func TestFormatLogEntry_ContainsCategory(t *testing.T) {
	tests := []struct {
		name     string
		entry    ipc.LogEntryWire
		contains []string
	}{
		{
			name:     "think",
			entry:    ipc.LogEntryWire{TimestampMs: 523, Category: "think", Content: "analyzing code"},
			contains: []string{"[think]", "analyzing code"},
		},
		{
			name:     "tool with path",
			entry:    ipc.LogEntryWire{TimestampMs: 1204, Category: "tool", Content: "Read file.go", ToolPath: "/dev/fs"},
			contains: []string{"[tool]", "/dev/fs"},
		},
		{
			name:     "output",
			entry:    ipc.LogEntryWire{TimestampMs: 2100, Category: "output", Content: "fixed the bug"},
			contains: []string{"[output]", "fixed the bug"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatLogEntry(tt.entry)
			for _, want := range tt.contains {
				if !strings.Contains(result, want) {
					t.Errorf("formatLogEntry() = %q, want to contain %q", result, want)
				}
			}
		})
	}
}

// --- 10.2-UNIT-034: formatLogEntry includes formatted timestamp ---

func TestFormatLogEntry_Timestamp(t *testing.T) {
	entry := ipc.LogEntryWire{TimestampMs: 1523, Category: "think", Content: "test"}
	result := formatLogEntry(entry)

	if !strings.Contains(result, "1.5") {
		t.Errorf("formatted output should contain timestamp ~1.5s, got: %q", result)
	}
}

// --- 10.2-UNIT-035: formatLogEntryJSON outputs valid JSON ---

func TestFormatLogEntryJSON_ValidJSON(t *testing.T) {
	entry := ipc.LogEntryWire{
		TimestampMs: 523,
		PID:         types.PID(5),
		Step:        1,
		Category:    "think",
		Content:     "analyzing code",
	}

	result := formatLogEntryJSON(entry)

	var parsed map[string]any
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("formatLogEntryJSON output is not valid JSON: %v\nOutput: %s", err, result)
	}

	if parsed["category"] != "think" {
		t.Errorf("JSON category = %v, want %q", parsed["category"], "think")
	}
}

// --- 10.2-UNIT-036: formatLogEntryJSON NDJSON format (one JSON per line) ---

func TestFormatLogEntryJSON_NDJSON(t *testing.T) {
	entry := ipc.LogEntryWire{
		TimestampMs: 100,
		PID:         types.PID(1),
		Step:        1,
		Category:    "output",
		Content:     "result",
	}

	result := formatLogEntryJSON(entry)

	if strings.Contains(result, "\n") {
		t.Error("NDJSON entry should be a single line (no embedded newlines)")
	}
}

// --- 10.2-UNIT-037: logCmd requires exactly 1 argument (PID) ---

func TestLogCmd_RequiresOneArg(t *testing.T) {
	if logCmd.Args == nil {
		t.Fatal("logCmd should have Args validator set")
	}
	if err := logCmd.Args(logCmd, []string{}); err == nil {
		t.Error("logCmd should reject 0 arguments")
	}
	if err := logCmd.Args(logCmd, []string{"5"}); err != nil {
		t.Errorf("logCmd should accept 1 argument, got error: %v", err)
	}
	if err := logCmd.Args(logCmd, []string{"5", "extra"}); err == nil {
		t.Error("logCmd should reject 2 arguments")
	}
}
