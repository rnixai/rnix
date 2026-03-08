package main

import (
	"testing"

	"github.com/spf13/cobra"
)

// =============================================================================
// ATDD RED PHASE -- Story 14-2: replay CLI command tests
//
// Tests reference replayCmd which does NOT exist yet in replay.go
// Compile failure = RED phase.
// =============================================================================

// --- 14.2-CLI-001: [P0] replay 子命令注册 ---

func TestReplayCommand_Registered(t *testing.T) {
	root := &cobra.Command{Use: "rnix"}
	root.AddCommand(replayCmd)

	replayFound, _, err := root.Find([]string{"replay"})
	if err != nil {
		t.Fatalf("failed to find 'replay' command: %v", err)
	}
	if replayFound == nil {
		t.Fatal("expected 'replay' command to exist")
	}
	if replayFound.Use == "" {
		t.Fatal("expected 'replay' to have a Use string")
	}
}

// --- 14.2-CLI-002: [P0] replay 需要 record-id 参数 ---

func TestReplayCommand_RequiresRecordID(t *testing.T) {
	root := &cobra.Command{Use: "rnix"}
	root.AddCommand(replayCmd)

	// Running replay without record-id argument should error
	root.SetArgs([]string{"replay"})
	if err := root.Execute(); err == nil {
		t.Error("expected error when 'replay' called without record-id argument")
	}
}

// --- 14.2-CLI-003: [P1] replay 支持 --json flag ---

func TestReplayCommand_JSONFlag(t *testing.T) {
	root := &cobra.Command{Use: "rnix"}
	root.AddCommand(replayCmd)

	// Verify --json flag exists
	replayFound, _, _ := root.Find([]string{"replay"})
	if replayFound == nil {
		t.Fatal("expected 'replay' command to exist")
	}

	jsonFlag := replayFound.Flags().Lookup("json")
	if jsonFlag == nil {
		t.Fatal("expected '--json' flag on replay command")
	}
}
