package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/rnixai/rnix/ipc"
)

func TestCtxProfileCmd_Registered(t *testing.T) {
	root := &cobra.Command{Use: "rnix"}
	root.AddCommand(ctxProfileCmd)

	found, _, err := root.Find([]string{"ctx-profile"})
	if err != nil {
		t.Fatalf("failed to find 'ctx-profile' command: %v", err)
	}
	if found == nil {
		t.Fatal("expected 'ctx-profile' command to exist")
	}
}

func TestCtxProfileCmd_InvalidPID(t *testing.T) {
	flagJSON = false
	exitCode = 0
	var buf strings.Builder
	cmd := &cobra.Command{Use: "rnix"}
	cmd.AddCommand(ctxProfileCmd)
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"ctx-profile", "abc"})
	_ = cmd.Execute()

	output := buf.String()
	if !strings.Contains(output, "invalid PID") {
		t.Errorf("expected 'invalid PID' error, got:\n%s", output)
	}
	if exitCode != 1 {
		t.Errorf("expected exit code 1, got %d", exitCode)
	}
}

func TestCtxProfileCmd_InvalidPID_JSON(t *testing.T) {
	flagJSON = true
	defer func() { flagJSON = false }()
	exitCode = 0

	var buf strings.Builder
	cmd := &cobra.Command{Use: "rnix"}
	cmd.AddCommand(ctxProfileCmd)
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"ctx-profile", "xyz"})
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

func TestCtxProfileCmd_NoDaemon(t *testing.T) {
	ipc.SocketPathOverride = t.TempDir() + "/nonexistent.sock"
	defer func() { ipc.SocketPathOverride = "" }()

	flagJSON = false
	exitCode = 0
	var buf strings.Builder
	cmd := &cobra.Command{Use: "rnix"}
	cmd.AddCommand(ctxProfileCmd)
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"ctx-profile", "1"})
	_ = cmd.Execute()

	output := buf.String()
	if !strings.Contains(output, "daemon not available") {
		t.Errorf("expected 'daemon not available' error, got:\n%s", output)
	}
	if exitCode != 1 {
		t.Errorf("expected exit code 1, got %d", exitCode)
	}
}

func TestCtxProfileCmd_NoDaemon_JSON(t *testing.T) {
	ipc.SocketPathOverride = t.TempDir() + "/nonexistent.sock"
	defer func() { ipc.SocketPathOverride = "" }()

	flagJSON = true
	defer func() { flagJSON = false }()
	exitCode = 0

	var buf strings.Builder
	cmd := &cobra.Command{Use: "rnix"}
	cmd.AddCommand(ctxProfileCmd)
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"ctx-profile", "1"})
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
