package main

import (
	"bytes"
	"strings"
	"testing"
)

// =============================================================================
// ATDD 48.5 AC4/AC5 — `rnix mcp logs` CLI + `mcp list` LAST CHECK backfill
//
// Story: _bmad-output/implementation-artifacts/48-5-mcp-health-check-stderr-reconnect.md
// Phase: 🔴 RED — `//go:build atdd_48_5_red`. Run:
//            go test -tags=atdd_48_5_red -race -run TestATDD_48_5_06 ./cmd/rnix/
//        Pre-impl: compile fails on undefined renderMCPLogsHuman / unregistered
//        `mcp logs` command — the red signal. _065 additionally fails behaviorally
//        because the current lastCheckLabel ignores its arg and always returns "—".
//
// Reuses withMCPListSocketOverride + resetExitCode from
// atdd_48_3_mcp_list_cmd_test.go (same package, untagged → always compiled).
// Daemon-down (NOT_FOUND / live-line) wire concerns are delegated to the IPC
// suite (ipc/atdd_48_5_mcp_logs_test.go), mirroring the 48.3 split.
//
// Injection points (dev-story Task 7.6, Task 9.2):
//   mcpLogsCmd registered under mcpCmd  (rootCmd.Find(["mcp","logs"]))
//   runMCPLogs (daemon-down friendly, exit 0 like `mcp list`)
//   renderMCPLogsHuman(w io.Writer, lines []string)
//   lastCheckLabel(ms int64) → "Ns ago" for non-zero (was always placeholder)
// =============================================================================

// -----------------------------------------------------------------------------
// _060: daemon down → friendly message, exitCode stays 0 (parity with mcp list).
// -----------------------------------------------------------------------------
func TestATDD_48_5_060_McpLogs_DaemonDown_GracefulExit0(t *testing.T) {
	defer withMCPListSocketOverride(t)()
	defer resetExitCode(t)()

	cmd, _, err := rootCmd.Find([]string{"mcp", "logs"})
	if err != nil {
		t.Fatalf("mcp logs command not registered: %v", err)
	}
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	if err := cmd.RunE(cmd, []string{"playwright"}); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	if exitCode != 0 {
		t.Errorf("exitCode = %d, want 0 (daemon-down logs is not an error, parity with `mcp list`)", exitCode)
	}
	out := buf.String()
	if !strings.Contains(strings.ToLower(out), "daemon not running") {
		t.Errorf("output = %q; want daemon-down hint", out)
	}
}

// -----------------------------------------------------------------------------
// _061: renderMCPLogsHuman emits each captured stderr line verbatim.
// -----------------------------------------------------------------------------
func TestATDD_48_5_061_McpLogs_RenderLines(t *testing.T) {
	var buf bytes.Buffer
	renderMCPLogsHuman(&buf, []string{
		"npx error: ENOENT",
		"playwright: chromium not found",
	})
	out := buf.String()
	for _, want := range []string{"npx error: ENOENT", "chromium not found"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderMCPLogsHuman output missing %q:\n%s", want, out)
		}
	}
}

// -----------------------------------------------------------------------------
// _062: --quiet daemon-down → silent (no hint), exitCode 0.
// -----------------------------------------------------------------------------
func TestATDD_48_5_062_McpLogs_QuietMode_DaemonDown(t *testing.T) {
	defer withMCPListSocketOverride(t)()
	defer resetExitCode(t)()

	oldQuiet := flagQuiet
	flagQuiet = true
	defer func() { flagQuiet = oldQuiet }()

	cmd, _, _ := rootCmd.Find([]string{"mcp", "logs"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	if err := cmd.RunE(cmd, []string{"playwright"}); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("exitCode = %d, want 0", exitCode)
	}
	if strings.TrimSpace(buf.String()) != "" {
		t.Errorf("--quiet daemon-down should be silent, got: %q", buf.String())
	}
}

// -----------------------------------------------------------------------------
// _065: AC5 — lastCheckLabel renders a relative "Ns ago" for non-zero ms.
//        Current impl always returns the placeholder (ignores its arg), so this
//        is the behavioral red signal for the LAST CHECK column backfill.
// -----------------------------------------------------------------------------
func TestATDD_48_5_065_McpList_LastCheckRelative(t *testing.T) {
	// 12 seconds ago, expressed in milliseconds.
	label := lastCheckLabel(12_000)
	if label == mcpPlaceholder() {
		t.Fatalf("lastCheckLabel(12000) = placeholder %q; want a relative time (AC5 backfill not landed)", label)
	}
	if !strings.Contains(label, "ago") {
		t.Errorf("lastCheckLabel(12000) = %q; want a relative label containing 'ago' (e.g. \"12s ago\")", label)
	}

	// Zero (never checked) must still render the placeholder.
	if got := lastCheckLabel(0); got != mcpPlaceholder() {
		t.Errorf("lastCheckLabel(0) = %q, want placeholder %q (never-checked stays empty)", got, mcpPlaceholder())
	}
}
