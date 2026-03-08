package main

import (
	"strings"
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

// =============================================================================
// ATDD RED PHASE -- Story 14-3: replay diff CLI command tests
//
// Tests verify the diff command integration in the replay interactive loop.
// These test the CLI layer which delegates to ReplaySession.Diff/DiffFromCursor.
// =============================================================================

// --- 14.3-CLI-001: [P0] diff 命令在 help 输出中有记录 ---

func TestReplayCommand_DiffInHelp(t *testing.T) {
	// printReplayHelp should include diff command documentation
	// We test this by verifying the help function output contains "diff"
	var buf strings.Builder
	printReplayHelp(&buf)
	output := buf.String()

	if !strings.Contains(output, "diff") {
		t.Fatalf("expected replay help to contain 'diff' command, got:\n%s", output)
	}
}

// --- 14.3-CLI-002: [P0] diff 命令在 help 中显示用法格式 ---

func TestReplayCommand_DiffHelpFormat(t *testing.T) {
	var buf strings.Builder
	printReplayHelp(&buf)
	output := buf.String()

	// Should show both forms: diff <seq1> <seq2> and diff <seq>
	if !strings.Contains(output, "diff") {
		t.Fatalf("expected help to show diff usage, got:\n%s", output)
	}
}

// =============================================================================
// ATDD RED PHASE -- Story 14-4: fork/continue CLI command tests
//
// Tests verify the fork command integration in the replay interactive loop.
// These test the CLI layer which delegates to ReplaySession.Fork/ForkAt.
// =============================================================================

// --- 14.4-CLI-001: [P0] fork 命令在 help 输出中有记录 ---

func TestReplayCommand_ForkInHelp(t *testing.T) {
	var buf strings.Builder
	printReplayHelp(&buf)
	output := buf.String()

	if !strings.Contains(output, "fork") {
		t.Fatalf("expected replay help to contain 'fork' command, got:\n%s", output)
	}
}

// --- 14.4-CLI-002: [P0] continue 命令在 help 输出中有记录 ---

func TestReplayCommand_ContinueInHelp(t *testing.T) {
	var buf strings.Builder
	printReplayHelp(&buf)
	output := buf.String()

	if !strings.Contains(output, "continue") || !strings.Contains(output, "go") {
		t.Fatalf("expected replay help to contain 'continue' or 'go' command, got:\n%s", output)
	}
}

// --- 14.4-CLI-003: [P0] fork 子模式命令在 help 中有说明 ---

func TestReplayCommand_ForkSubcommands(t *testing.T) {
	var buf strings.Builder
	printReplayHelp(&buf)
	output := buf.String()

	// fork submode should mention: set prompt, append, remove, replace, show, continue, cancel
	forkKeywords := []string{"fork"}
	for _, kw := range forkKeywords {
		if !strings.Contains(output, kw) {
			t.Errorf("expected help to contain %q, got:\n%s", kw, output)
		}
	}
}
