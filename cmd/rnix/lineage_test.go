package main

// =============================================================================
// ATDD Story 20.5: Differentiation Lineage Graph -- CLI Tests
// TDD RED PHASE - All tests designed to FAIL until implementation exists
// =============================================================================
//
// Test Strategy:
// - Task 5: CLI `rnix lineage` command registration, argument parsing, output format
//
// Priority: P1 (user-facing CLI command)
// Test Level: Unit (command registration and argument parsing)
//
// Pattern: follows cmd/rnix/ctx_profile_test.go and cmd/rnix/trace_test.go

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rnixai/rnix/ipc"
	"github.com/spf13/cobra"
)

func TestLineageCmd_Registered(t *testing.T) {
	// Given: the root command with lineage subcommand
	// When: finding the 'lineage' command
	// Then: it exists
	root := &cobra.Command{Use: "rnix"}
	root.AddCommand(lineageCmd)

	found, _, err := root.Find([]string{"lineage"})
	if err != nil {
		t.Fatalf("failed to find 'lineage' command: %v", err)
	}
	if found == nil {
		t.Fatal("expected 'lineage' command to exist")
	}
}

func TestLineageCmd_InvalidPID(t *testing.T) {
	// Given: a lineage command with a non-numeric PID argument
	// When: executing the command
	// Then: output contains "invalid PID" error
	flagJSON = false
	exitCode = 0
	var buf strings.Builder
	cmd := &cobra.Command{Use: "rnix"}
	cmd.AddCommand(lineageCmd)
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"lineage", "abc"})
	_ = cmd.Execute()

	output := buf.String()
	if !strings.Contains(output, "invalid PID") {
		t.Errorf("expected 'invalid PID' error, got:\n%s", output)
	}
	if exitCode != 1 {
		t.Errorf("expected exit code 1, got %d", exitCode)
	}
}

func TestLineageCmd_InvalidPID_JSON(t *testing.T) {
	// Given: a lineage command with --json flag and non-numeric PID
	// When: executing the command
	// Then: output is JSON with OK=false
	flagJSON = true
	defer func() { flagJSON = false }()
	exitCode = 0

	var buf strings.Builder
	cmd := &cobra.Command{Use: "rnix"}
	cmd.AddCommand(lineageCmd)
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"lineage", "xyz"})
	_ = cmd.Execute()

	output := strings.TrimSpace(buf.String())
	var resp JSONResponse
	if err := json.Unmarshal([]byte(output), &resp); err != nil {
		t.Fatalf("failed to parse JSON: %v\noutput: %s", err, output)
	}
	if resp.OK {
		t.Error("expected OK=false for invalid PID")
	}
	if exitCode != 1 {
		t.Errorf("expected exit code 1, got %d", exitCode)
	}
}

func TestLineageCmd_NoDaemon(t *testing.T) {
	// Given: no daemon running (socket path points to nonexistent file)
	// When: executing lineage command with valid PID
	// Then: output contains "daemon not available" error
	ipc.SocketPathOverride = t.TempDir() + "/nonexistent.sock"
	defer func() { ipc.SocketPathOverride = "" }()

	flagJSON = false
	exitCode = 0
	var buf strings.Builder
	cmd := &cobra.Command{Use: "rnix"}
	cmd.AddCommand(lineageCmd)
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"lineage", "1"})
	_ = cmd.Execute()

	output := buf.String()
	if !strings.Contains(output, "daemon not available") {
		t.Errorf("expected 'daemon not available' error, got:\n%s", output)
	}
	if exitCode != 1 {
		t.Errorf("expected exit code 1, got %d", exitCode)
	}
}

func TestLineageCmd_NoDaemon_JSON(t *testing.T) {
	// Given: no daemon running, --json flag set
	// When: executing lineage command
	// Then: output is JSON with OK=false
	ipc.SocketPathOverride = t.TempDir() + "/nonexistent.sock"
	defer func() { ipc.SocketPathOverride = "" }()

	flagJSON = true
	defer func() { flagJSON = false }()
	exitCode = 0

	var buf strings.Builder
	cmd := &cobra.Command{Use: "rnix"}
	cmd.AddCommand(lineageCmd)
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"lineage", "1"})
	_ = cmd.Execute()

	output := strings.TrimSpace(buf.String())
	var resp JSONResponse
	if err := json.Unmarshal([]byte(output), &resp); err != nil {
		t.Fatalf("failed to parse JSON: %v\noutput: %s", err, output)
	}
	if resp.OK {
		t.Error("expected OK=false when daemon unavailable")
	}
	if exitCode != 1 {
		t.Errorf("expected exit code 1, got %d", exitCode)
	}
}

func TestLineageCmd_NoArgs(t *testing.T) {
	// Given: a lineage command with no arguments
	// When: executing the command
	// Then: cobra returns an error (requires exactly 1 arg)
	var buf strings.Builder
	cmd := &cobra.Command{Use: "rnix"}
	cmd.AddCommand(lineageCmd)
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"lineage"})
	err := cmd.Execute()

	// cobra.ExactArgs(1) produces an error when no args provided
	if err == nil {
		output := buf.String()
		// If no error, the output should at least indicate wrong usage
		if !strings.Contains(output, "usage") && !strings.Contains(output, "Usage") &&
			!strings.Contains(output, "requires") && !strings.Contains(output, "accepts") {
			t.Error("expected usage/error output for missing PID argument")
		}
	}
}
