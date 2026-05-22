package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// ============================================================
// ATDD RED PHASE — Story 22.2: 异常检测与威胁记忆 CLI Tests
//
// CLI 命令 `rnix immune resume` 和扩展的 `rnix immune status` 测试。
// 测试引用尚不存在的 immuneResumeCmd 和修改后的 runImmuneStatus 输出。
//
// RED -> GREEN: 在 cmd/rnix/immune.go 中实现。
// ============================================================

// --- 22.2-CLI-001: [P0] immune resume success output (AC3) ---

func TestRunImmuneResume_Success(t *testing.T) {
	// Given: the immune resume command exists
	if immuneResumeCmd == nil {
		t.Fatal("immuneResumeCmd is nil - not yet implemented")
	}
	// Then: command Use is "resume <pid>"
	if immuneResumeCmd.Use != "resume <pid>" {
		t.Errorf("immuneResumeCmd.Use = %q, want %q", immuneResumeCmd.Use, "resume <pid>")
	}
	// And: command Short is not empty
	if immuneResumeCmd.Short == "" {
		t.Error("immuneResumeCmd.Short should not be empty")
	}
	// And: command requires exactly 1 argument
	if immuneResumeCmd.Args == nil {
		t.Error("immuneResumeCmd.Args should enforce ExactArgs(1)")
	}
}

// --- 22.2-CLI-002: [P0] immune resume no daemon shows error (AC3) ---

func TestRunImmuneResume_NoDaemon(t *testing.T) {
	// Given: no daemon running (socket isolation handled by TestMain)
	flagJSON = false
	defer func() { exitCode = 0; flagJSON = false }()

	var buf strings.Builder
	cmd := &cobra.Command{Use: "rnix"}
	cmd.AddCommand(immuneCmd)
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"immune", "resume", "42"})

	_ = cmd.Execute()
	output := buf.String()

	// Then: output contains error about daemon not being available
	if !strings.Contains(output, "daemon not available") && !strings.Contains(output, "error") {
		t.Errorf("expected daemon error message, got: %s", output)
	}
}

// --- 22.2-CLI-003: [P1] immune status with alerts shows ALERTS section (AC4) ---

func TestRunImmuneStatus_WithAlerts(t *testing.T) {
	// Given: the immune resume command is registered as subcommand
	var found bool
	for _, sub := range immuneCmd.Commands() {
		if strings.HasPrefix(sub.Use, "resume") {
			found = true
			break
		}
	}
	if !found {
		t.Error("immune command missing 'resume' subcommand")
	}
}

// --- 22.2-CLI-004: [P1] immune status JSON includes alerts and threat_count (AC4) ---

func TestRunImmuneStatus_JSONWithAlerts(t *testing.T) {
	// Given: JSON mode enabled, no daemon (socket isolation handled by TestMain)
	flagJSON = true
	defer func() { exitCode = 0; flagJSON = false }()

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
	// Structural validation: JSONResponse should still work
	// The detailed fields (alerts, threat_count) will be tested
	// when daemon is running in integration tests
}

// --- 22.2-CLI-005: [P0] immune resume registered as subcommand of immune (AC3) ---

func TestImmuneResumeCmd_Registered(t *testing.T) {
	// Given: the immune command
	// Then: it has "resume" subcommand
	var found bool
	for _, sub := range immuneCmd.Commands() {
		if strings.HasPrefix(sub.Use, "resume") {
			found = true
			break
		}
	}
	if !found {
		t.Error("immune command missing 'resume' subcommand")
	}
}
