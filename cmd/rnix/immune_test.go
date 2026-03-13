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
