package main

import (
	"testing"

	"github.com/spf13/cobra"
)

// ============================================================
// ATDD RED PHASE — Story 14.1: 执行录制与持久化 (CLI record 命令)
//
// Tests reference recordCmd, recordStartCmd, recordStopCmd,
// recordListCmd which do NOT exist yet in record.go
// → compile failure = RED phase.
// ============================================================

// --- 14.1-CLI-001: [P0] record start 子命令注册并解析 PID 参数 ---

func TestRecordCommand_StartRegistered(t *testing.T) {
	// Verify recordCmd exists and has "start" subcommand
	root := &cobra.Command{Use: "rnix"}
	root.AddCommand(recordCmd)

	// Find the "start" subcommand
	startCmd, _, err := root.Find([]string{"record", "start"})
	if err != nil {
		t.Fatalf("failed to find 'record start' command: %v", err)
	}
	if startCmd == nil {
		t.Fatal("expected 'record start' subcommand to exist")
	}
	if startCmd.Use == "" {
		t.Fatal("expected 'record start' to have a Use string")
	}
}

// --- 14.1-CLI-002: [P0] record stop 子命令注册并解析 PID 参数 ---

func TestRecordCommand_StopRegistered(t *testing.T) {
	root := &cobra.Command{Use: "rnix"}
	root.AddCommand(recordCmd)

	stopCmd, _, err := root.Find([]string{"record", "stop"})
	if err != nil {
		t.Fatalf("failed to find 'record stop' command: %v", err)
	}
	if stopCmd == nil {
		t.Fatal("expected 'record stop' subcommand to exist")
	}
}

// --- 14.1-CLI-003: [P0] record list 子命令注册 ---

func TestRecordCommand_ListRegistered(t *testing.T) {
	root := &cobra.Command{Use: "rnix"}
	root.AddCommand(recordCmd)

	listCmd, _, err := root.Find([]string{"record", "list"})
	if err != nil {
		t.Fatalf("failed to find 'record list' command: %v", err)
	}
	if listCmd == nil {
		t.Fatal("expected 'record list' subcommand to exist")
	}
}

// --- 14.1-CLI-004: [P1] record start 无 PID 参数校验 ---

func TestRecordCommand_StartRequiresPID(t *testing.T) {
	// recordStartCmd should require exactly 1 arg (PID)
	root := &cobra.Command{Use: "rnix"}
	root.AddCommand(recordCmd)

	// Verify Args validation is set
	startCmd, _, _ := root.Find([]string{"record", "start"})
	if startCmd == nil {
		t.Fatal("expected 'record start' to exist")
	}

	// The command should have Args validation (ExactArgs(1) or similar)
	// Trying to run with no args should produce an error
	root.SetArgs([]string{"record", "start"})
	if err := root.Execute(); err == nil {
		t.Error("expected error when 'record start' called without PID argument")
	}
}
