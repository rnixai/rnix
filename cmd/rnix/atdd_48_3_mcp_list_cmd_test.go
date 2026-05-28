package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rnixai/rnix/ipc"
)

// =============================================================================
// Story 48.3 AC1 — `rnix mcp list` CLI command (GREEN tests)
//
// These tests exercise the renderer + daemon-down branches directly. Real
// daemon end-to-end coverage lives in ipc/atdd_48_3_mcp_list_test.go (the
// CLI delegates wire shape concerns there); here we pin the human/JSON/quiet
// rendering and the daemon-down graceful-empty behaviour.
// =============================================================================

// withMCPListSocketOverride flips ipc.SocketPathOverride to a nonexistent
// path so runMCPList takes the daemon-down branch.
func withMCPListSocketOverride(t *testing.T) func() {
	t.Helper()
	old := ipc.SocketPathOverride
	ipc.SocketPathOverride = "/tmp/rnix-test-nonexistent-48-3.sock"
	return func() { ipc.SocketPathOverride = old }
}

// resetExitCode resets the package-level exitCode around a test.
func resetExitCode(t *testing.T) func() {
	t.Helper()
	old := exitCode
	exitCode = 0
	return func() { exitCode = old }
}

// -----------------------------------------------------------------------------
// _001: empty mounts → friendly textual message; exitCode 0
// -----------------------------------------------------------------------------
func TestATDD_48_3_001_McpList_NoMounts(t *testing.T) {
	defer withMCPListSocketOverride(t)()
	defer resetExitCode(t)()

	var buf bytes.Buffer
	cmd, _, err := rootCmd.Find([]string{"mcp", "list"})
	if err != nil {
		t.Fatalf("mcp list command not registered: %v", err)
	}
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	if exitCode != 0 {
		t.Errorf("exitCode = %d, want 0 (empty list is not an error)", exitCode)
	}
	out := buf.String()
	if !strings.Contains(out, "Daemon not running") && !strings.Contains(out, "No active MCP mounts") {
		t.Errorf("output = %q; want daemon-down hint or empty message", out)
	}
}

// -----------------------------------------------------------------------------
// _002: render path — feed wires directly through renderMCPListHuman
// -----------------------------------------------------------------------------
func TestATDD_48_3_002_McpList_TwoMounts_HumanOutput(t *testing.T) {
	mounts := []ipc.MCPMountWire{
		{Name: "playwright", Path: "/mnt/mcp/100-playwright", Transport: "stdio", Status: "connected", Tools: 0, Resources: 0},
		{Name: "deepwiki", Path: "/mnt/mcp/101-deepwiki", Transport: "stdio", Status: "connected"},
	}
	var buf bytes.Buffer
	renderMCPListHuman(&buf, mounts)
	out := buf.String()

	for _, want := range []string{"NAME", "TRANSPORT", "STATUS", "playwright", "deepwiki", "connected", "stdio"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in human output:\n%s", want, out)
		}
	}
	// Placeholder for Tools/Resources/LAST CHECK pre-48.5.
	if !strings.Contains(out, "—") {
		t.Errorf("expected em-dash placeholder for empty fields, got:\n%s", out)
	}
}

// -----------------------------------------------------------------------------
// _003: --json mode emits JSONResponse{OK:true, Data:{mounts:[]}}
// -----------------------------------------------------------------------------
func TestATDD_48_3_003_McpList_JSONMode(t *testing.T) {
	defer withMCPListSocketOverride(t)()
	defer resetExitCode(t)()

	oldJSON := flagJSON
	flagJSON = true
	defer func() { flagJSON = oldJSON }()

	var buf bytes.Buffer
	cmd, _, _ := rootCmd.Find([]string{"mcp", "list"})
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	var resp JSONResponse
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("JSON parse: %v\noutput=%s", err, buf.String())
	}
	if !resp.OK {
		t.Error("OK=false; daemon-down is not an error for list")
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("Data shape: %T", resp.Data)
	}
	mounts, ok := data["mounts"].([]any)
	if !ok {
		t.Fatalf("mounts shape: %T", data["mounts"])
	}
	if len(mounts) != 0 {
		t.Errorf("expected empty mounts in daemon-down JSON, got %d", len(mounts))
	}
}

// -----------------------------------------------------------------------------
// _004: daemon NOT running → friendly empty list, NOT auto-start, exitCode 0
// -----------------------------------------------------------------------------
func TestATDD_48_3_004_McpList_DaemonDown_GracefulEmpty(t *testing.T) {
	defer withMCPListSocketOverride(t)()
	defer resetExitCode(t)()

	var buf bytes.Buffer
	cmd, _, _ := rootCmd.Find([]string{"mcp", "list"})
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	if exitCode != 0 {
		t.Errorf("exitCode=%d, want 0 (daemon-down list is not an error)", exitCode)
	}
	out := buf.String()
	if !strings.Contains(out, "Daemon not running") && !strings.Contains(out, "No active MCP mounts") {
		t.Errorf("output = %q; want daemon-down hint or empty message", out)
	}
}
