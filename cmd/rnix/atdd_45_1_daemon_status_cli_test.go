package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/rnixai/rnix/ipc"
)

// =============================================================================
// ATDD 45.1 — AC#3 §Step 4: runDaemonStatus CLI consumes the new IPC
// `daemon_status` method and surfaces `commit:` + `built:` lines.
//
// RED PHASE signal (behavioural — no compile-fail in this file): the current
// runDaemonStatus (cmd/rnix/main.go:1262-1303) calls client.Ping() which only
// returns Version, so the rendered output has no commit/built lines. Asserting
// the new lines fails today → RED; passes once dev-story Task 3.4 swaps
// client.Ping() for client.DaemonStatus() and prints the new lines.
//
// The compile-fail RED signal for the underlying IPC plumbing
// (MethodDaemonStatus / DaemonStatusResponse / client.DaemonStatus) lives in
// ipc/atdd_45_1_daemon_status_test.go to avoid duplicating it across packages.
//
// Coverage:
//   - 020: running daemon → output contains `commit:`
//   - 021: running daemon → output contains `built:`
//   - 022: stopped daemon → output preserves `status: stopped` and does NOT
//          fabricate commit/built lines (regression guard for the
//          dial-failure early-return branch)
// =============================================================================

func setupDaemonStatusCLIServer(t *testing.T) string {
	t.Helper()
	sockPath, _ := setupTestIPCServer(t)
	prev := ipc.SocketPathOverride
	ipc.SocketPathOverride = sockPath
	t.Cleanup(func() { ipc.SocketPathOverride = prev })
	return sockPath
}

func captureRunDaemonStatus(t *testing.T) string {
	t.Helper()
	var buf bytes.Buffer
	cmd := &cobra.Command{Use: "status"}
	cmd.SetOut(&buf)
	if err := runDaemonStatus(cmd, nil); err != nil {
		t.Fatalf("runDaemonStatus: %v", err)
	}
	return buf.String()
}

// TestATDD_45_1_020_RunDaemonStatus_Running_OutputContainsCommitLine
//
// When the daemon is reachable, runDaemonStatus must surface a `commit:` line
// pulled from DaemonStatusResponse.DaemonCommit (with `unknown` fallback when
// the daemon was built without ldflags/BuildInfo VCS info — AC#3 Step 4
// `defaultIfEmpty(ds.DaemonCommit, "unknown")`).
func TestATDD_45_1_020_RunDaemonStatus_Running_OutputContainsCommitLine(t *testing.T) {
	setupDaemonStatusCLIServer(t)

	out := captureRunDaemonStatus(t)

	if !strings.Contains(out, "status:  running") {
		t.Fatalf("precondition: runDaemonStatus must report running for reachable daemon; got %q", out)
	}
	if !strings.Contains(out, "commit:") {
		t.Errorf("runDaemonStatus output does not contain \"commit:\" line.\n"+
			"AC#3 Step 4 requires `commit:  <sha or 'unknown'>` to be printed.\n"+
			"current output:\n%s", out)
	}
}

// TestATDD_45_1_021_RunDaemonStatus_Running_OutputContainsBuiltLine
//
// Parallel to 020 — assert the `built:` line is present.
func TestATDD_45_1_021_RunDaemonStatus_Running_OutputContainsBuiltLine(t *testing.T) {
	setupDaemonStatusCLIServer(t)

	out := captureRunDaemonStatus(t)

	if !strings.Contains(out, "status:  running") {
		t.Fatalf("precondition: runDaemonStatus must report running for reachable daemon; got %q", out)
	}
	if !strings.Contains(out, "built:") {
		t.Errorf("runDaemonStatus output does not contain \"built:\" line.\n"+
			"AC#3 Step 4 requires `built:   <RFC3339 or 'unknown'>` to be printed.\n"+
			"current output:\n%s", out)
	}
}

// TestATDD_45_1_022_RunDaemonStatus_Stopped_PreservesOutputContract
//
// Regression guard for the dial-failure branch (cmd/rnix/main.go:1265-1268):
// when daemon is unreachable, output must remain `status: stopped\nsocket: %s\n`
// and must NOT fabricate commit/built lines (no daemon means no provenance to
// surface; AC#3 explicitly scopes commit/built to the running-daemon path).
//
// Story 45.1 §"既有现状（不能破坏的不变量）": runDaemonStatus's dial-failure
// path is load-bearing for `rnix daemon status` before `rnix daemon` starts.
func TestATDD_45_1_022_RunDaemonStatus_Stopped_PreservesOutputContract(t *testing.T) {
	// Point at a guaranteed-nonexistent socket; Dial fails fast.
	prev := ipc.SocketPathOverride
	ipc.SocketPathOverride = filepath.Join(t.TempDir(), "nonexistent.sock")
	t.Cleanup(func() { ipc.SocketPathOverride = prev })

	out := captureRunDaemonStatus(t)

	if !strings.Contains(out, "status: stopped") {
		t.Errorf("runDaemonStatus(unreachable) output = %q, want it to contain \"status: stopped\"", out)
	}
	if strings.Contains(out, "commit:") {
		t.Errorf("runDaemonStatus(unreachable) output must NOT contain \"commit:\" line "+
			"(no daemon = no provenance); got %q", out)
	}
	if strings.Contains(out, "built:") {
		t.Errorf("runDaemonStatus(unreachable) output must NOT contain \"built:\" line "+
			"(no daemon = no provenance); got %q", out)
	}
}
