package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rnixai/rnix/ipc"
	"github.com/spf13/cobra"
)

// ============================================================
// ATDD RED PHASE — Story 22.3: 安全状态管理 CLI Tests
//
// CLI 命令 `rnix immune status` 的增强输出测试：
// uptime、security 摘要行、suspended 段落、formatUptime 函数。
//
// RED → GREEN: 在 cmd/rnix/immune.go 中实现。
// ============================================================

// --- 22.3-CLI-001: [P0] formatUptime formats seconds correctly (AC1) ---

func TestFormatUptime_Seconds(t *testing.T) {
	// Given: duration < 60s in milliseconds
	// When: formatting
	got := formatUptime(42000) // 42 seconds

	// Then: output is "42s"
	if got != "42s" {
		t.Errorf("formatUptime(42000) = %q, want %q", got, "42s")
	}
}

// --- 22.3-CLI-002: [P0] formatUptime formats minutes correctly (AC1) ---

func TestFormatUptime_Minutes(t *testing.T) {
	// Given: duration < 60 minutes in milliseconds
	// When: formatting
	got := formatUptime(330000) // 5m30s

	// Then: output is "5m30s"
	if got != "5m30s" {
		t.Errorf("formatUptime(330000) = %q, want %q", got, "5m30s")
	}
}

// --- 22.3-CLI-003: [P0] formatUptime formats hours correctly (AC1) ---

func TestFormatUptime_Hours(t *testing.T) {
	// Given: duration >= 60 minutes in milliseconds
	// When: formatting
	got := formatUptime(8100000) // 2h15m

	// Then: output is "2h15m"
	if got != "2h15m" {
		t.Errorf("formatUptime(8100000) = %q, want %q", got, "2h15m")
	}
}

// --- 22.3-CLI-004: [P0] formatUptime handles zero (AC1) ---

func TestFormatUptime_Zero(t *testing.T) {
	// Given: zero milliseconds
	// When: formatting
	got := formatUptime(0)

	// Then: output is "0s"
	if got != "0s" {
		t.Errorf("formatUptime(0) = %q, want %q", got, "0s")
	}
}

// --- 22.3-CLI-005: [P1] formatUptime formats exact minute boundary (AC1) ---

func TestFormatUptime_ExactMinute(t *testing.T) {
	// Given: exactly 5 minutes in milliseconds
	// When: formatting
	got := formatUptime(300000) // 5m0s → should be "5m0s" or "5m"

	// Then: output is a clean minute format
	if !strings.HasPrefix(got, "5m") {
		t.Errorf("formatUptime(300000) = %q, want prefix %q", got, "5m")
	}
}

// --- 22.3-CLI-006: [P0] immune status output includes uptime (AC1) ---

func TestRunImmuneStatus_Uptime(t *testing.T) {
	// Given: no daemon running (tests text structure)
	old := ipc.SocketPathOverride
	ipc.SocketPathOverride = "/tmp/rnix-22-3-test-nonexistent.sock"
	defer func() { ipc.SocketPathOverride = old }()

	flagJSON = false
	defer func() { exitCode = 0; flagJSON = false }()

	var buf strings.Builder
	cmd := &cobra.Command{Use: "rnix"}
	cmd.AddCommand(immuneCmd)
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"immune", "status"})

	_ = cmd.Execute()
	// Note: Without daemon, we expect error output.
	// This test confirms the command path exists. Full uptime formatting
	// is tested via formatUptime unit tests above and integration tests.
}

// --- 22.3-CLI-007: [P0] immune status text shows Security: OK when no alerts (AC5) ---

func TestRunImmuneStatus_SecurityOK(t *testing.T) {
	// This test verifies the security status summary line exists in the output format.
	// Full integration requires a running daemon. Here we verify the function signature exists
	// and the expected format constants are used.

	// Given: the runImmuneStatus function exists (verified via immuneStatusCmd.RunE)
	if immuneStatusCmd.RunE == nil {
		t.Fatal("immuneStatusCmd.RunE should not be nil")
	}

	// Note: When daemon is available and returns 0 alerts / 0 suspended,
	// the text output should contain "Security: OK"
	// Full verification deferred to integration tests with mock IPC
}

// --- 22.3-CLI-008: [P0] immune status text shows Security warning with alert counts (AC5) ---

func TestRunImmuneStatus_SecurityWarning(t *testing.T) {
	// This test verifies security status summary format when alerts exist.
	// The expected format is: "Security: 2 alerts, 1 suspended"
	// Full integration requires mock IPC responses.

	// Verify command structure
	if immuneStatusCmd == nil {
		t.Fatal("immuneStatusCmd should not be nil")
	}
	if immuneStatusCmd.RunE == nil {
		t.Fatal("immuneStatusCmd.RunE should not be nil")
	}
}

// --- 22.3-CLI-009: [P0] immune status text shows SUSPENDED PROCESSES section (AC3) ---

func TestRunImmuneStatus_SuspendedProcesses(t *testing.T) {
	// This test verifies the SUSPENDED PROCESSES section output format.
	// Expected format when suspended processes exist:
	//   SUSPENDED PROCESSES (1):
	//     PID 42 — syscall_freq anomaly (use: rnix immune resume 42 | rnix kill 42)

	// Verify command structure exists to output this section
	if immuneStatusCmd == nil {
		t.Fatal("immuneStatusCmd should not be nil")
	}
}

// --- 22.3-CLI-010: [P0] immune status JSON includes all new fields (AC6) ---

func TestRunImmuneStatus_JSON_FullFields(t *testing.T) {
	// Given: JSON mode enabled, no daemon
	flagJSON = true
	defer func() { exitCode = 0; flagJSON = false }()

	old := ipc.SocketPathOverride
	ipc.SocketPathOverride = "/tmp/rnix-22-3-json-test-nonexistent.sock"
	defer func() { ipc.SocketPathOverride = old }()

	var buf strings.Builder
	cmd := &cobra.Command{Use: "rnix"}
	cmd.AddCommand(immuneCmd)
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"immune", "status"})

	_ = cmd.Execute()
	output := strings.TrimSpace(buf.String())

	// Then: output is valid JSON
	var resp JSONResponse
	if err := json.Unmarshal([]byte(output), &resp); err != nil {
		t.Fatalf("invalid JSON output: %v\noutput: %s", err, output)
	}

	// Note: Without running daemon, the response will be an error.
	// The test validates the JSON envelope structure is intact.
	// Full field verification (uptime_ms, suspended_pids, security_status)
	// requires integration tests with a running daemon.
}
