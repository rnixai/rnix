package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/rnixai/rnix/ipc"
)

func TestCtxGrowthCmd_Registration(t *testing.T) {
	root := &cobra.Command{Use: "rnix"}
	root.AddCommand(ctxGrowthCmd)
	cmd, _, err := root.Find([]string{"ctx-growth"})
	if err != nil {
		t.Fatalf("ctx-growth command not found: %v", err)
	}
	if cmd.Use != "ctx-growth <pid>" {
		t.Errorf("Use = %q, want %q", cmd.Use, "ctx-growth <pid>")
	}
}

func TestCtxGrowthCmd_InvalidPID(t *testing.T) {
	var buf strings.Builder
	exitCode = 0
	flagJSON = false
	defer func() { exitCode = 0; flagJSON = false }()

	cmd := &cobra.Command{Use: "rnix"}
	cmd.AddCommand(ctxGrowthCmd)
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"ctx-growth", "notanumber"})
	_ = cmd.Execute()

	output := buf.String()
	if !strings.Contains(output, "invalid PID") {
		t.Errorf("expected 'invalid PID' error, got: %s", output)
	}
	if exitCode != 1 {
		t.Errorf("exitCode = %d, want 1", exitCode)
	}
}

func TestCtxGrowthCmd_DaemonUnavailable(t *testing.T) {
	ipc.SocketPathOverride = t.TempDir() + "/nonexistent.sock"
	defer func() { ipc.SocketPathOverride = "" }()

	var buf strings.Builder
	exitCode = 0
	flagJSON = false
	defer func() { exitCode = 0; flagJSON = false }()

	cmd := &cobra.Command{Use: "rnix"}
	cmd.AddCommand(ctxGrowthCmd)
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"ctx-growth", "1"})
	_ = cmd.Execute()

	output := buf.String()
	if !strings.Contains(output, "daemon not available") {
		t.Errorf("expected 'daemon not available', got: %s", output)
	}
	if exitCode != 1 {
		t.Errorf("exitCode = %d, want 1", exitCode)
	}
}

func TestCtxGrowthCmd_DaemonUnavailable_JSON(t *testing.T) {
	ipc.SocketPathOverride = t.TempDir() + "/nonexistent.sock"
	defer func() { ipc.SocketPathOverride = "" }()

	var buf strings.Builder
	exitCode = 0
	flagJSON = true
	defer func() { exitCode = 0; flagJSON = false }()

	cmd := &cobra.Command{Use: "rnix"}
	cmd.AddCommand(ctxGrowthCmd)
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"ctx-growth", "1"})
	_ = cmd.Execute()

	output := strings.TrimSpace(buf.String())
	var resp JSONResponse
	if err := json.Unmarshal([]byte(output), &resp); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, output)
	}
	if resp.OK {
		t.Error("expected OK=false for daemon unavailable")
	}
	if exitCode != 1 {
		t.Errorf("exitCode = %d, want 1", exitCode)
	}
}
