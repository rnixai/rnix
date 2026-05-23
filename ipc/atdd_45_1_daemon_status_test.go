package ipc

import (
	"testing"
	"time"
)

// =============================================================================
// ATDD 45.1 — AC#3: IPC `daemon_status` method (4-step extension spec)
//
// RED PHASE signals (compile-fail):
//   - MethodDaemonStatus constant does not exist in protocol.go
//   - DaemonStatusResponse type does not exist in protocol.go
//   - client.DaemonStatus() method does not exist on *Client
//   - NewServer(nil, nil, version, commit, buildDate string) 5-arg signature
//     does not exist; current signature is 3-arg (kern, agentLoader, version)
//
// All tests fail to COMPILE until dev-story Task 3.1-3.3 land. Same compile-fail
// RED style 44.4 used for undefined Method constants / new client wrappers.
//
// Construction: setupTestServer(t) returns the current 3-arg server for
// existing callsites; the new tests construct their own server via the new
// NewServer 5-arg constructor so the RED signal on the signature change is
// explicit and isolated to this file.
// =============================================================================

const (
	testDaemonStatusVersion   = "v0.8.0-test"
	testDaemonStatusCommit    = "test1234"
	testDaemonStatusBuildDate = "2026-05-23T00:00:00Z"
)

// setupDaemonStatusServer builds a server using the NEW 5-arg NewServer
// signature (AC#3 Step 2). Once Task 3.2 lands, this constructor compiles;
// today, it fails — exactly the RED signal we want for the signature change.
func setupDaemonStatusServer(t *testing.T) (*Server, string) {
	t.Helper()

	srv := NewServer(nil, nil, testDaemonStatusVersion, testDaemonStatusCommit, testDaemonStatusBuildDate)

	sockDir := t.TempDir()
	sockPath := sockDir + "/test-daemon-status.sock"

	if err := srv.ListenAndServe(sockPath); err != nil {
		t.Fatalf("ListenAndServe: %v", err)
	}
	t.Cleanup(func() {
		srv.Shutdown()
		srv.Wait()
	})

	return srv, sockPath
}

// TestATDD_45_1_010_DaemonStatus_Returns3Fields
//
// AC#3 §Step 1: client.DaemonStatus() returns DaemonStatusResponse populated
// with version + daemon_commit + daemon_build_date. The fields must round-trip
// across the NDJSON wire, matching the values NewServer was constructed with.
func TestATDD_45_1_010_DaemonStatus_Returns3Fields(t *testing.T) {
	_, sockPath := setupDaemonStatusServer(t)

	cli, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { cli.Close() })

	resp, err := cli.DaemonStatus()
	if err != nil {
		t.Fatalf("DaemonStatus: %v", err)
	}
	if resp == nil {
		t.Fatalf("DaemonStatus returned nil response")
	}

	if resp.Version != testDaemonStatusVersion {
		t.Errorf("DaemonStatus Version = %q, want %q", resp.Version, testDaemonStatusVersion)
	}
	if resp.DaemonCommit != testDaemonStatusCommit {
		t.Errorf("DaemonStatus DaemonCommit = %q, want %q", resp.DaemonCommit, testDaemonStatusCommit)
	}
	if resp.DaemonBuildDate != testDaemonStatusBuildDate {
		t.Errorf("DaemonStatus DaemonBuildDate = %q, want %q", resp.DaemonBuildDate, testDaemonStatusBuildDate)
	}
}

// TestATDD_45_1_011_Ping_StillReturnsVersion_BackwardCompat
//
// AC#3 §"既有现状（不能破坏的不变量）" + §"代码位置精确锚点" line 504:
// client.Ping() must remain unchanged. ipc/daemon.go:56 EnsureDaemon path
// still polls Ping, so PingResponse.Version contract is load-bearing.
//
// Test guards against the "while we're touching server.go, let's just merge
// Ping into DaemonStatus" refactor temptation — Story 45.1 explicitly forbids
// it (Decision §"不删除 client.Ping() — 保留作为最轻量的 alive probe").
func TestATDD_45_1_011_Ping_StillReturnsVersion_BackwardCompat(t *testing.T) {
	_, sockPath := setupDaemonStatusServer(t)

	cli, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { cli.Close() })

	version, err := cli.Ping()
	if err != nil {
		t.Fatalf("Ping (must remain alive after daemon_status lands): %v", err)
	}
	if version != testDaemonStatusVersion {
		t.Errorf("Ping Version = %q, want %q (must mirror server.version)", version, testDaemonStatusVersion)
	}
}

// TestATDD_45_1_012_DaemonStatus_StartedAtAndPid_Populated
//
// AC#3 §Step 1 wire type: DaemonStatusResponse.DaemonPID (omitempty) and
// StartedAt (UnixMilli) are informational fields. The server populates them at
// handle-time (os.Getpid + s.startedAt.UnixMilli). Test asserts they are
// non-zero — guards against forgetting to wire them in handleDaemonStatus.
//
// We capture wall-clock just before the call so we can sanity-check the
// startedAt is within a reasonable window (not zero, not future, not ancient).
func TestATDD_45_1_012_DaemonStatus_StartedAtAndPid_Populated(t *testing.T) {
	before := time.Now().UnixMilli()
	_, sockPath := setupDaemonStatusServer(t)

	cli, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { cli.Close() })

	resp, err := cli.DaemonStatus()
	if err != nil {
		t.Fatalf("DaemonStatus: %v", err)
	}

	after := time.Now().UnixMilli()

	if resp.DaemonPID <= 0 {
		t.Errorf("DaemonStatus DaemonPID = %d, want > 0 (os.Getpid)", resp.DaemonPID)
	}
	// startedAt must be within [before, after+1s slack] — proves NewServer
	// initialised it on construction, not on a stale or future timestamp.
	if resp.StartedAt < before-1000 || resp.StartedAt > after+1000 {
		t.Errorf("DaemonStatus StartedAt = %d, want within [%d, %d] (server construction window)",
			resp.StartedAt, before-1000, after+1000)
	}
}
