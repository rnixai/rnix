package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/rnixai/rnix/ipc"
)

func TestRunImmuneStatus_NoDaemon(t *testing.T) {
	// Ensure no daemon is available by using a bad socket path
	old := ipc.SocketPathOverride
	ipc.SocketPathOverride = "/tmp/rnix-immune-test-nonexistent.sock"
	defer func() { ipc.SocketPathOverride = old }()

	var buf bytes.Buffer
	cmd := immuneStatusCmd
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	oldJSON := flagJSON
	flagJSON = false
	defer func() { flagJSON = oldJSON }()

	oldExitCode := exitCode
	exitCode = 0
	defer func() { exitCode = oldExitCode }()

	_ = cmd.RunE(cmd, nil)
	output := buf.String()

	if !strings.Contains(output, "daemon not available") {
		t.Errorf("Expected 'daemon not available' message, got: %s", output)
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input    string
		maxLen   int
		expected string
	}{
		{"short", 10, "short"},
		{"exactly-20-chars-ok!", 20, "exactly-20-chars-ok!"},
		{"this-is-a-very-long-agent-template-name", 20, "this-is-a-very-long~"},
	}

	for _, tt := range tests {
		got := truncate(tt.input, tt.maxLen)
		if got != tt.expected {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.expected)
		}
	}
}

func TestFormatDurationMs(t *testing.T) {
	tests := []struct {
		ms       float64
		expected string
	}{
		{500, "500ms"},
		{1000, "1.0s"},
		{15200, "15.2s"},
		{90000, "1.5m"},
	}

	for _, tt := range tests {
		got := formatDurationMs(tt.ms)
		if got != tt.expected {
			t.Errorf("formatDurationMs(%f) = %q, want %q", tt.ms, got, tt.expected)
		}
	}
}

// --- Story 22.3: formatUptime tests ---

func TestFormatUptime(t *testing.T) {
	tests := []struct {
		ms       int64
		expected string
	}{
		{0, "0s"},
		{1000, "1s"},
		{42000, "42s"},
		{59000, "59s"},
		{60000, "1m0s"},
		{330000, "5m30s"},
		{3599000, "59m59s"},
		{3600000, "1h0m"},
		{8100000, "2h15m"},
		{86400000, "24h0m"},
	}

	for _, tt := range tests {
		got := formatUptime(tt.ms)
		if got != tt.expected {
			t.Errorf("formatUptime(%d) = %q, want %q", tt.ms, got, tt.expected)
		}
	}
}

// --- Story 22.3: securitySummary tests ---

func TestSecuritySummary(t *testing.T) {
	tests := []struct {
		alerts    int
		suspended int
		expected  string
	}{
		{0, 0, "Security: OK"},
		{1, 0, "Security: 1 alerts"},
		{0, 1, "Security: 1 suspended"},
		{2, 1, "Security: 2 alerts, 1 suspended"},
		{3, 5, "Security: 3 alerts, 5 suspended"},
	}

	for _, tt := range tests {
		got := securitySummary(tt.alerts, tt.suspended)
		if got != tt.expected {
			t.Errorf("securitySummary(%d, %d) = %q, want %q", tt.alerts, tt.suspended, got, tt.expected)
		}
	}
}

// --- IN-3 F3: rnix immune forget ---

// forgetTestReset saves/restores forget flags + exitCode around a test run.
func forgetTestReset(t *testing.T) *bytes.Buffer {
	t.Helper()

	oldJSON := flagJSON
	oldAll := flagImmuneForgetAll
	oldMetric := flagImmuneForgetMetric
	oldExitCode := exitCode
	flagJSON = false
	flagImmuneForgetAll = false
	flagImmuneForgetMetric = ""
	exitCode = 0
	t.Cleanup(func() {
		flagJSON = oldJSON
		flagImmuneForgetAll = oldAll
		flagImmuneForgetMetric = oldMetric
		exitCode = oldExitCode
	})

	var buf bytes.Buffer
	immuneForgetCmd.SetOut(&buf)
	immuneForgetCmd.SetErr(&buf)
	return &buf
}

func TestRunImmuneForget_RequiresTemplateOrAll(t *testing.T) {
	buf := forgetTestReset(t)

	_ = immuneForgetCmd.RunE(immuneForgetCmd, nil)

	if !strings.Contains(buf.String(), "a template argument or --all is required") {
		t.Errorf("expected validation error, got: %s", buf.String())
	}
	if exitCode != 1 {
		t.Errorf("exitCode = %d, want 1", exitCode)
	}
}

func TestRunImmuneForget_AllExclusiveWithTemplate(t *testing.T) {
	buf := forgetTestReset(t)
	flagImmuneForgetAll = true

	_ = immuneForgetCmd.RunE(immuneForgetCmd, []string{"some-template"})

	if !strings.Contains(buf.String(), "--all cannot be combined") {
		t.Errorf("expected exclusivity error, got: %s", buf.String())
	}
	if exitCode != 1 {
		t.Errorf("exitCode = %d, want 1", exitCode)
	}
}

func TestRunImmuneForget_NoDaemon(t *testing.T) {
	old := ipc.SocketPathOverride
	ipc.SocketPathOverride = "/tmp/rnix-immune-forget-test-nonexistent.sock"
	defer func() { ipc.SocketPathOverride = old }()

	buf := forgetTestReset(t)
	flagImmuneForgetAll = true

	_ = immuneForgetCmd.RunE(immuneForgetCmd, nil)

	if !strings.Contains(buf.String(), "daemon not available") {
		t.Errorf("expected 'daemon not available', got: %s", buf.String())
	}
	if exitCode != 1 {
		t.Errorf("exitCode = %d, want 1 (diagnostic command hard-fails)", exitCode)
	}
}
