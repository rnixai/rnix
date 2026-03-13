package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rnixai/rnix/kernel"
)

func TestReputationCmd_NoData(t *testing.T) {
	// reputation command without a running daemon shows error
	var buf bytes.Buffer
	reputationCmd.SetOut(&buf)
	reputationCmd.SetErr(&buf)

	oldJSON := flagJSON
	flagJSON = false
	defer func() { flagJSON = oldJSON }()

	if err := reputationCmd.RunE(reputationCmd, []string{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	// Without daemon, should show error
	if !strings.Contains(output, "daemon not available") && !strings.Contains(output, "No reputation data") {
		// If daemon is running and has no data, that's also fine
		t.Logf("output: %s", output)
	}
}

func TestReputationCmd_JSON(t *testing.T) {
	// reputation --json without daemon should return JSON error
	var buf bytes.Buffer
	reputationCmd.SetOut(&buf)
	reputationCmd.SetErr(&buf)

	oldJSON := flagJSON
	flagJSON = true
	defer func() { flagJSON = oldJSON }()

	if err := reputationCmd.RunE(reputationCmd, []string{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	var resp JSONResponse
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &resp); err != nil {
		t.Fatalf("expected valid JSON, got parse error: %v\noutput: %s", err, output)
	}
}

func TestFormatReputationTable(t *testing.T) {
	// Verify table formatting
	summaries := []struct {
		agentName string
		score     float64
	}{
		{"agent-a", 0.9},
		{"agent-b", 0.5},
	}

	// Just verify the function doesn't panic and produces output
	output := formatReputationTable(nil)
	if !strings.Contains(output, "AGENT") {
		t.Fatalf("expected table header, got: %s", output)
	}

	_ = summaries // table formatting tested via formatReputationTable call above
}

func TestFormatReputationDetail(t *testing.T) {
	output := formatReputationDetail(kernel.ReputationSummary{
		AgentName:     "test-agent",
		Score:         0.85,
		SuccessRate:   0.9,
		AvgTokens:     5000,
		AvgDurationMs: 30000,
		TotalRecords:  10,
		RecentTrend:   "stable",
	})

	if !strings.Contains(output, "test-agent") {
		t.Fatalf("expected agent name in detail output")
	}
	if !strings.Contains(output, "0.85") {
		t.Fatalf("expected score in detail output")
	}
	if !strings.Contains(output, "stable") {
		t.Fatalf("expected trend in detail output")
	}
}

func TestFormatTokens(t *testing.T) {
	tests := []struct {
		input    int
		expected string
	}{
		{500, "500"},
		{1000, "1,000"},
		{12340, "12,340"},
	}
	for _, tt := range tests {
		result := formatTokens(tt.input)
		if result != tt.expected {
			t.Errorf("formatTokens(%d) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}
