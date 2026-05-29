package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rnixai/rnix/ipc"
)

// =============================================================================
// Story 48.3 AC2 — `rnix mcp test <name>` CLI command (GREEN tests)
//
// CLI tests focus on the renderer (renderMCPTestHuman / renderMCPTestJSON) +
// the daemon-down hard-fail branch. Wire shape + daemon-side behaviour are
// covered by ipc/atdd_48_3_mcp_test_wire_test.go::_021 series.
// =============================================================================

// -----------------------------------------------------------------------------
// _005: happy path render — 4 stages all OK, summary line emitted
// -----------------------------------------------------------------------------
func TestATDD_48_3_005_McpTest_HappyPath(t *testing.T) {
	resp := &ipc.MCPTestResponse{
		Server: "playwright",
		OK:     true,
		Stages: []ipc.MCPTestStageWire{
			{Name: "connect", OK: true, DurationMs: 123},
			{Name: "tools_list", OK: true, DurationMs: 30},
			{Name: "resources_list", OK: true, DurationMs: 12},
			{Name: "prompts_list", OK: true, DurationMs: 10},
		},
		Tools: 8, Resources: 0, Prompts: 0,
		ServerInfo: "playwright-mcp v0.2.1",
	}

	var buf bytes.Buffer
	renderMCPTestHuman(&buf, resp)
	out := buf.String()

	for _, want := range []string{"[1/4]", "connect", "OK", "tools/list", "tools=8", "playwright-mcp v0.2.1"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in human output:\n%s", want, out)
		}
	}
}

// -----------------------------------------------------------------------------
// _006: server not found → "Available: ..." hint, exit 1
// -----------------------------------------------------------------------------
func TestATDD_48_3_006_McpTest_ServerNotFound(t *testing.T) {
	// We exercise the CLI's error mapping via formatMCPTestErr; full daemon
	// round-trip lives in ipc/atdd_48_3_mcp_test_wire_test.go::_021b.
	err := mockIPCError("[NOT_FOUND] server \"playwriht\" not found in mcp.yaml. Available: playwright, deepwiki.")
	msg := formatMCPTestErr("playwriht", err)
	for _, want := range []string{"playwriht", "Available", "playwright"} {
		if !strings.Contains(msg, want) {
			t.Errorf("missing %q in formatted error:\n%s", want, msg)
		}
	}

	if got := classifyMCPTestErr(err); got != "NOT_FOUND" {
		t.Errorf("classifyMCPTestErr = %q, want NOT_FOUND", got)
	}
}

// -----------------------------------------------------------------------------
// _007: connect fail → stage[0]=FAILED + subsequent stages absent
// -----------------------------------------------------------------------------
func TestATDD_48_3_007_McpTest_ConnectFail(t *testing.T) {
	resp := &ipc.MCPTestResponse{
		Server: "broken",
		OK:     false,
		Stages: []ipc.MCPTestStageWire{
			{Name: "connect", OK: false, DurationMs: 42, Error: "exec: \"nonexistent\": executable file not found in $PATH"},
		},
	}

	var buf bytes.Buffer
	renderMCPTestHuman(&buf, resp)
	out := buf.String()

	if !strings.Contains(out, "FAILED") {
		t.Errorf("expect stage marker FAILED, got: %s", out)
	}
	if !strings.Contains(out, "executable file not found") {
		t.Errorf("expect underlying error in detail, got: %s", out)
	}
	// stages 2/3 must NOT be emitted (only stage[0] is in the wire).
	if strings.Contains(out, "[2/") {
		t.Errorf("stage[1] should not appear after connect failure, got: %s", out)
	}
}

// -----------------------------------------------------------------------------
// _008: initialize hangs → TIMEOUT label (renderer maps context-deadline err)
// -----------------------------------------------------------------------------
func TestATDD_48_3_008_McpTest_InitializeTimeout(t *testing.T) {
	resp := &ipc.MCPTestResponse{
		Server: "slow",
		OK:     false,
		Stages: []ipc.MCPTestStageWire{
			{Name: "connect", OK: false, DurationMs: 10000, Error: "context deadline exceeded"},
		},
	}
	var buf bytes.Buffer
	renderMCPTestHuman(&buf, resp)
	out := buf.String()

	if !strings.Contains(out, "TIMEOUT") {
		t.Errorf("expect TIMEOUT marker for ctx-deadline error, got: %s", out)
	}
}

// -----------------------------------------------------------------------------
// Regression: -32601 "Method not found" on an optional stage renders as N/A
// (not supported), NOT red FAILED — e.g. Playwright is tools-only. The overall
// verdict stays OK because resp.OK reflects the connect stage.
// -----------------------------------------------------------------------------
func TestMcpTest_UnsupportedCapabilityRendersNA(t *testing.T) {
	resp := &ipc.MCPTestResponse{
		Server: "playwright",
		OK:     true, // connect succeeded → overall OK
		Stages: []ipc.MCPTestStageWire{
			{Name: "connect", OK: true, DurationMs: 819},
			{Name: "tools_list", OK: true, DurationMs: 1},
			{Name: "resources_list", OK: false, DurationMs: 0, Error: "rpc error -32601: Method not found"},
			{Name: "prompts_list", OK: false, DurationMs: 0, Error: "rpc error -32601: Method not found"},
		},
		Tools: 23,
	}

	var buf bytes.Buffer
	renderMCPTestHuman(&buf, resp)
	out := buf.String()

	if !strings.Contains(out, "N/A") || !strings.Contains(out, "not supported") {
		t.Errorf("expect optional stage to render N/A (not supported), got:\n%s", out)
	}
	if strings.Contains(out, "FAILED") {
		t.Errorf("-32601 must NOT render as FAILED, got:\n%s", out)
	}
	// Core stages stay OK; overall summary line still emitted.
	if !strings.Contains(out, "tools=23") {
		t.Errorf("expect tools/list OK detail, got:\n%s", out)
	}
}

// -----------------------------------------------------------------------------
// _009: --json mode shape — MCPTestResponse-compatible JSON
// -----------------------------------------------------------------------------
func TestATDD_48_3_009_McpTest_JSONMode(t *testing.T) {
	resp := &ipc.MCPTestResponse{
		Server: "playwright",
		OK:     true,
		Stages: []ipc.MCPTestStageWire{{Name: "connect", OK: true, DurationMs: 100}},
	}
	var buf bytes.Buffer
	renderMCPTestJSON(&buf, resp)

	var payload JSONResponse
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("JSON parse: %v\n%s", err, buf.String())
	}
	if !payload.OK {
		t.Error("OK=false in JSON payload despite resp.OK=true")
	}
	data, ok := payload.Data.(map[string]any)
	if !ok {
		t.Fatalf("Data shape: %T", payload.Data)
	}
	if _, ok := data["server"]; !ok {
		t.Error("missing 'server' field")
	}
	if _, ok := data["stages"]; !ok {
		t.Error("missing 'stages' field")
	}
}

// -----------------------------------------------------------------------------
// _010: daemon down → hard fail exit 1 (NOT graceful empty)
// -----------------------------------------------------------------------------
func TestATDD_48_3_010_McpTest_DaemonDown(t *testing.T) {
	defer withMCPListSocketOverride(t)()
	defer resetExitCode(t)()

	var buf bytes.Buffer
	cmd, _, _ := rootCmd.Find([]string{"mcp", "test"})
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	if err := cmd.RunE(cmd, []string{"playwright"}); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	if exitCode != 1 {
		t.Errorf("exitCode=%d, want 1 (test requires daemon; differs from list)", exitCode)
	}
	out := buf.String()
	if !strings.Contains(out, "Daemon not running") {
		t.Errorf("output should mention daemon-down; got: %s", out)
	}
}

// mockIPCError fabricates an error matching the shape returned by
// client.MCPTest when the daemon returns a non-OK Response{Error: ...}.
type mockIPCErr struct{ msg string }

func (e *mockIPCErr) Error() string { return e.msg }

func mockIPCError(msg string) error { return &mockIPCErr{msg: msg} }
