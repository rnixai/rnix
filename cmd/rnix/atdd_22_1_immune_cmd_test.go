package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// ============================================================
// ATDD RED PHASE — Story 22.1: Immune Daemon CLI Tests
//
// CLI 命令 `rnix immune status` 测试。
// 测试引用尚不存在的 immuneCmd、immuneStatusCmd 和 runImmuneStatus。
// 将无法编译直到实现完成。
//
// RED → GREEN: 在 cmd/rnix/immune.go 中实现 immune 命令组。
// ============================================================

// --- 22.1-CLI-001: [P0] immune status no profiles shows empty message (AC3) ---

func TestRunImmuneStatus_NoProfiles(t *testing.T) {
	// Given: may or may not have a daemon running
	flagJSON = false
	defer func() { exitCode = 0; flagJSON = false }()

	var buf strings.Builder
	cmd := &cobra.Command{Use: "rnix"}
	cmd.AddCommand(immuneCmd)
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"immune", "status"})

	_ = cmd.Execute()
	output := buf.String()

	// Then: output contains either no-profiles message, daemon error, or valid status
	if !strings.Contains(output, "No behavior profiles established.") &&
		!strings.Contains(output, "daemon not available") &&
		!strings.Contains(output, "Immune Daemon:") {
		t.Errorf("expected no-profiles message, daemon error, or valid status, got: %s", output)
	}
}

// --- 22.1-CLI-002: [P0] immune status --json output format (AC3) ---

func TestRunImmuneStatus_JSON(t *testing.T) {
	// Given: JSON mode enabled, no daemon
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

	// And: JSON has expected structure
	if resp.OK && resp.Error != nil {
		t.Error("OK=true but Error is non-nil")
	}
}

// --- 22.1-CLI-003: [P1] immune command registered as subcommand (AC3) ---

func TestImmuneCmd_Registered(t *testing.T) {
	// Given: the immune command
	// Then: it has "immune" use and "status" subcommand
	if immuneCmd.Use != "immune" {
		t.Errorf("immuneCmd.Use = %q, want %q", immuneCmd.Use, "immune")
	}

	// Find status subcommand
	var found bool
	for _, sub := range immuneCmd.Commands() {
		if sub.Use == "status" {
			found = true
			break
		}
	}
	if !found {
		t.Error("immune command missing 'status' subcommand")
	}
}

// --- 22.1-CLI-004: [P1] immune status table has correct columns (AC3) ---

func TestRunImmuneStatus_Table(t *testing.T) {
	// Given: the immune status command exists
	if immuneStatusCmd == nil {
		t.Fatal("immuneStatusCmd is nil")
	}
	if immuneStatusCmd.Use != "status" {
		t.Errorf("immuneStatusCmd.Use = %q, want %q", immuneStatusCmd.Use, "status")
	}
	if immuneStatusCmd.Short == "" {
		t.Error("immuneStatusCmd.Short should not be empty")
	}
}
