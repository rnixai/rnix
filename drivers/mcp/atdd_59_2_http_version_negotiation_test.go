package mcp

import (
	"context"
	"testing"

	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// ATDD 59.2 — MCP Streamable HTTP protocol-version negotiation
//
// Spec: _bmad-output/implementation-artifacts/spec-mcp-http-version-negotiation-reload.md
// Investigation: investigations/mcp-http-transport-400-investigation.md
//
// Regression guard for the 400 `-32001` bug: rnix used to hard-code
// MCP-Protocol-Version: <client constant> on every post-init request, ignoring
// the version the server negotiated in the initialize result. A strict server
// (mantra-gateway negotiates 2025-03-26) then rejected tools/list with 400.
//
// Reuses the mockStreamableHTTPServer harness from atdd_59_1_http_transport_test.go;
// the initVersionResp / omitInitVersion knobs drive the two negotiation paths.
//
// Run: go test -race -run TestATDD_59_2 ./drivers/mcp/
// =============================================================================

// _001 — the server downgrades to a version different from the client constant;
// every post-init request must carry the NEGOTIATED version, while the
// initialize REQUEST still advertises the client's expected constant.
func TestATDD_59_2_001_NegotiatedVersionUsedInPostInitHeader(t *testing.T) {
	const negotiated = "2025-03-26"
	if negotiated == mcpProtocolVersion {
		t.Fatalf("test invariant: negotiated %q must differ from the client constant %q to prove the fix", negotiated, mcpProtocolVersion)
	}

	mock := &mockStreamableHTTPServer{sessionID: "sess-neg", initVersionResp: negotiated}
	srv := newMockHTTPServer(t, mock)
	tr := NewHTTPTransport(vfs.MCPConfig{TransportType: "http", URL: srv.URL}, WithHTTPClient(srv.Client()))

	ctx := context.Background()
	if err := tr.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	if _, err := tr.Call(ctx, "tools/list", nil); err != nil {
		t.Fatalf("Call tools/list: %v", err)
	}

	mock.mu.Lock()
	defer mock.mu.Unlock()
	// The initialize REQUEST still advertises the client's expected constant...
	if mock.lastInitVersion != mcpProtocolVersion {
		t.Errorf("initialize request protocolVersion = %q, want client constant %q", mock.lastInitVersion, mcpProtocolVersion)
	}
	// ...but post-init requests MUST send the negotiated version, not the constant.
	if mock.lastProtocolVer != negotiated {
		t.Errorf("post-init MCP-Protocol-Version = %q, want negotiated %q (the 400 regression)", mock.lastProtocolVer, negotiated)
	}
}

// _002 — a server that returns no protocolVersion in its initialize result must
// leave the client falling back to the advertised constant (parse miss must not
// blank the header).
func TestATDD_59_2_002_FallbackToConstantWhenServerOmitsVersion(t *testing.T) {
	mock := &mockStreamableHTTPServer{sessionID: "sess-omit", omitInitVersion: true}
	srv := newMockHTTPServer(t, mock)
	tr := NewHTTPTransport(vfs.MCPConfig{TransportType: "http", URL: srv.URL}, WithHTTPClient(srv.Client()))

	ctx := context.Background()
	if err := tr.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	if _, err := tr.Call(ctx, "tools/list", nil); err != nil {
		t.Fatalf("Call tools/list: %v", err)
	}

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if mock.lastProtocolVer != mcpProtocolVersion {
		t.Errorf("fallback MCP-Protocol-Version = %q, want constant %q when server omits protocolVersion", mock.lastProtocolVer, mcpProtocolVersion)
	}
}

// _003 — CAP-7 re-initialize after a 404 REFRESHES the negotiated version to the
// re-init's result. First connect negotiates v1; the re-init negotiates a
// DIFFERENT v2, and post-reinit requests must carry v2. The per-init
// initVersionSeq makes this discriminating: a transport that failed to re-read
// the version on re-init would leave the stale v1 and fail.
func TestATDD_59_2_003_ReInitRefreshesNegotiatedVersion(t *testing.T) {
	const v1, v2 = "2025-03-26", "2024-11-05"
	mock := &mockStreamableHTTPServer{
		sessionID:      "sess-cap7",
		noGetStream:    true,
		initVersionSeq: []string{v1, v2}, // connect → v1, re-init → v2
		failCallStatus: 404,
		failCallMethod: "tools/call",
		faultArmed:     true,
	}
	srv := newMockHTTPServer(t, mock)
	tr := NewHTTPTransport(vfs.MCPConfig{TransportType: "http", URL: srv.URL}, WithHTTPClient(srv.Client()))

	ctx := context.Background()
	if err := tr.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	// tools/call → 404 → re-initialize (negotiates v2) → retry succeeds.
	if _, err := tr.Call(ctx, "tools/call", nil); err != nil {
		t.Fatalf("Call after 404 re-init: %v", err)
	}

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if mock.initCount < 2 {
		t.Fatalf("initCount = %d, want >=2 (initial + re-init)", mock.initCount)
	}
	if mock.lastProtocolVer != v2 {
		t.Errorf("post-reinit MCP-Protocol-Version = %q, want refreshed %q (not stale v1 %q)", mock.lastProtocolVer, v2, v1)
	}
}

// _004 — regression guard: a CAP-7 re-initialize whose result OMITS
// protocolVersion must reset the negotiated value so post-reinit requests fall
// back to the constant — never reuse the prior session's negotiated v1. Catches
// the set-only/never-cleared staleness bug.
func TestATDD_59_2_004_ReInitOmitsVersion_FallsBackNotStale(t *testing.T) {
	const v1 = "2025-03-26"
	if v1 == mcpProtocolVersion {
		t.Fatalf("test invariant: v1 %q must differ from the client constant %q", v1, mcpProtocolVersion)
	}
	mock := &mockStreamableHTTPServer{
		sessionID:      "sess-cap7-omit",
		noGetStream:    true,
		initVersionSeq: []string{v1, ""}, // connect → v1, re-init → omit version
		failCallStatus: 404,
		failCallMethod: "tools/call",
		faultArmed:     true,
	}
	srv := newMockHTTPServer(t, mock)
	tr := NewHTTPTransport(vfs.MCPConfig{TransportType: "http", URL: srv.URL}, WithHTTPClient(srv.Client()))

	ctx := context.Background()
	if err := tr.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	if _, err := tr.Call(ctx, "tools/call", nil); err != nil {
		t.Fatalf("Call after 404 re-init: %v", err)
	}

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if mock.initCount < 2 {
		t.Fatalf("initCount = %d, want >=2 (initial + re-init)", mock.initCount)
	}
	if mock.lastProtocolVer != mcpProtocolVersion {
		t.Errorf("post-reinit MCP-Protocol-Version = %q, want fallback constant %q (stale v1 %q must not persist)", mock.lastProtocolVer, mcpProtocolVersion, v1)
	}
}
