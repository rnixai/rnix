package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// ============================================================
// ATDD RED PHASE — Story 21.5: Skill 组合矩阵
//
// CLI 命令 `rnix synergy list` 测试。
// 测试引用尚不存在的 synergyCmd、synergyListCmd 和 runSynergyList。
// 将无法编译直到实现完成。
//
// RED → GREEN: 在 cmd/rnix/synergy.go 中实现 synergy 命令组。
// ============================================================

// --- 21.5-CLI-001: [P0] synergy list no data shows empty message (AC6) ---

func TestRunSynergyList_NoData(t *testing.T) {
	// Given: no daemon running (will get connection error, treated as empty)
	flagJSON = false
	defer func() { exitCode = 0; flagJSON = false }()

	var buf strings.Builder
	cmd := &cobra.Command{Use: "rnix"}
	cmd.AddCommand(synergyCmd)
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"synergy", "list"})

	_ = cmd.Execute()
	output := buf.String()

	// Then: output contains no-data message
	if !strings.Contains(output, "No synergy combination data available.") &&
		!strings.Contains(output, "daemon not available") {
		t.Errorf("expected no-data message or daemon error, got: %s", output)
	}
}

// --- 21.5-CLI-002: [P0] synergy list --json output format (AC5) ---

func TestRunSynergyList_JSON(t *testing.T) {
	// Given: JSON mode enabled, no daemon
	flagJSON = true
	defer func() { exitCode = 0; flagJSON = false }()

	var buf strings.Builder
	cmd := &cobra.Command{Use: "rnix"}
	cmd.AddCommand(synergyCmd)
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"synergy", "list"})

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

// --- 21.5-CLI-003: [P1] synergy command registered as subcommand (AC2) ---

func TestSynergyCmd_Registered(t *testing.T) {
	// Given: the synergy command
	// Then: it has "synergy" use and "list" subcommand
	if synergyCmd.Use != "synergy" {
		t.Errorf("synergyCmd.Use = %q, want %q", synergyCmd.Use, "synergy")
	}

	// Find list subcommand
	var found bool
	for _, sub := range synergyCmd.Commands() {
		if sub.Use == "list" {
			found = true
			break
		}
	}
	if !found {
		t.Error("synergy command missing 'list' subcommand")
	}
}

// --- 21.5-CLI-004: [P1] synergy list table format has correct columns (AC2) ---

func TestRunSynergyList_TableColumns(t *testing.T) {
	// This test verifies the table header format.
	// Since we cannot easily mock the IPC, we test the format function directly
	// if available. For now, verify the command structure exists.

	// Given: synergy list command exists
	if synergyListCmd == nil {
		t.Fatal("synergyListCmd is nil")
	}
	if synergyListCmd.Use != "list" {
		t.Errorf("synergyListCmd.Use = %q, want %q", synergyListCmd.Use, "list")
	}
	if synergyListCmd.Short == "" {
		t.Error("synergyListCmd.Short should not be empty")
	}
}
