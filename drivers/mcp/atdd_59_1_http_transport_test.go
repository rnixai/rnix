package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// ATDD 59.1 — MCP Streamable HTTP transport (Epic 59)
//
// Story: _bmad-output/implementation-artifacts/59-1-mcp-streamable-http-transport.md
// SPEC:  _bmad-output/specs/spec-mcp-http-transport/ (CAP-1..8 + decision-matrix D1..D5)
//
// These started RED (skeleton + t.Skip) and are now GREEN after dev-story filled
// drivers/mcp/http_transport.go + config.go/kernel/init.go/cmd/rnix/main.go.
// The mock Streamable HTTP server (httptest) replaces stdio's bash subprocess.
//
// Run: go test -race -run TestATDD_59_1 ./drivers/mcp/
// =============================================================================

// -----------------------------------------------------------------------------
// mock Streamable HTTP MCP server (httptest)
// -----------------------------------------------------------------------------

type mockStreamableHTTPServer struct {
	mu sync.Mutex

	sessionID   string // assigned at initialize; "" = stateless
	sseForCall  bool   // tools/* POST replies as text/event-stream when true
	noGetStream bool   // GET returns 405 when true
	emptyGet    bool   // GET returns 200 with an empty body (no frames) when true

	// fault injection for a single post-init call (CAP-7)
	failCallStatus int    // 0 = none; e.g. 404 / 400
	failCallMethod string // method to fault (e.g. "tools/list")
	faultArmed     bool

	// protocol-version negotiation knobs (ATDD 59.2): initVersionResp overrides
	// the protocolVersion echoed in the initialize RESULT ("" = mcpProtocolVersion);
	// omitInitVersion returns a result with NO protocolVersion field at all;
	// initVersionSeq (index = initCount-1) negotiates a different version per
	// initialize call (a "" element omits the field) — used to exercise CAP-7
	// re-init refresh.
	initVersionResp string
	omitInitVersion bool
	initVersionSeq  []string

	// captured request facts for assertions
	lastAccept        string
	lastContentType   string
	lastAuthorization string
	lastProtocolVer   string
	lastSessionHeader string
	lastInitVersion   string
	lastGetEventID    string
	getCount          int
	deleteCount       int
	initCount         int
	gotInitialized    bool
}

func (m *mockStreamableHTTPServer) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()
		switch r.Method {
		case http.MethodPost:
			m.handlePost(w, r)
		case http.MethodGet:
			m.getCount++
			m.lastGetEventID = r.Header.Get("Last-Event-ID")
			if m.noGetStream {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			if m.emptyGet {
				return // 200 with no frames (server has no push)
			}
			fmt.Fprint(w, "id: 1\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/message\",\"params\":{}}\n\n")
		case http.MethodDelete:
			m.deleteCount++
			m.lastSessionHeader = r.Header.Get("Mcp-Session-Id")
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
}

func (m *mockStreamableHTTPServer) handlePost(w http.ResponseWriter, r *http.Request) {
	m.lastAccept = r.Header.Get("Accept")
	m.lastContentType = r.Header.Get("Content-Type")
	m.lastAuthorization = r.Header.Get("Authorization")
	m.lastProtocolVer = r.Header.Get("MCP-Protocol-Version")
	m.lastSessionHeader = r.Header.Get("Mcp-Session-Id")

	var req jsonRPCRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	switch {
	case req.Method == "initialize":
		m.initCount++
		var p map[string]any
		_ = json.Unmarshal(req.Params, &p)
		if v, ok := p["protocolVersion"].(string); ok {
			m.lastInitVersion = v
		}
		if m.sessionID != "" {
			w.Header().Set("Mcp-Session-Id", m.sessionID)
		}
		w.Header().Set("Content-Type", "application/json")
		// Decide this initialize's negotiated version. initVersionSeq (indexed by
		// initCount) takes precedence and lets a test negotiate different versions
		// across the initial connect and a CAP-7 re-init; a "" element omits the
		// field for that call. Falls back to initVersionResp, then the constant.
		omit := m.omitInitVersion
		respVer := mcpProtocolVersion
		if m.initVersionResp != "" {
			respVer = m.initVersionResp
		}
		if len(m.initVersionSeq) > 0 {
			idx := m.initCount - 1
			if idx >= len(m.initVersionSeq) {
				idx = len(m.initVersionSeq) - 1
			}
			if v := m.initVersionSeq[idx]; v == "" {
				omit = true
			} else {
				omit, respVer = false, v
			}
		}
		if omit {
			// Server returns no protocolVersion → client must fall back to the constant.
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"capabilities":{},"serverInfo":{"name":"mock","version":"1.0.0"}}}`, req.ID)
		} else {
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"protocolVersion":%q,"capabilities":{},"serverInfo":{"name":"mock","version":"1.0.0"}}}`, req.ID, respVer)
		}
	case strings.HasPrefix(req.Method, "notifications/"):
		if req.Method == "notifications/initialized" {
			m.gotInitialized = true
		}
		w.WriteHeader(http.StatusAccepted) // 202, no body
	case m.faultArmed && req.Method == m.failCallMethod && m.failCallStatus != 0:
		m.faultArmed = false // fault fires exactly once
		w.WriteHeader(m.failCallStatus)
	case m.sseForCall:
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "id: 2\ndata: {\"jsonrpc\":\"2.0\",\"id\":%d,\"result\":{\"tools\":[]}}\n\n", req.ID)
	default:
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"tools":[]}}`, req.ID)
	}
}

func newMockHTTPServer(t *testing.T, m *mockStreamableHTTPServer) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(m.handler())
	t.Cleanup(srv.Close)
	return srv
}

// -----------------------------------------------------------------------------
// config schema plumbing (AC1, D2)
// -----------------------------------------------------------------------------

func TestATDD_59_1_010_ToMCPConfig_HTTPFields(t *testing.T) {
	cfg := MCPServerConfig{
		TransportType: "http",
		URL:           "http://127.0.0.1:39600/mcp",
		Headers:       map[string]string{"Authorization": "Bearer tok"},
	}.ToMCPConfig("mantra-gateway")

	if cfg.TransportType != "http" {
		t.Errorf("TransportType = %q, want http", cfg.TransportType)
	}
	if cfg.URL != "http://127.0.0.1:39600/mcp" {
		t.Errorf("URL = %q, want endpoint", cfg.URL)
	}
	if cfg.Headers["Authorization"] != "Bearer tok" {
		t.Errorf("Headers[Authorization] = %q, want Bearer tok", cfg.Headers["Authorization"])
	}
}

func TestATDD_59_1_011_TypeAliasResolution(t *testing.T) {
	if got := (MCPServerConfig{Type: "http"}).ResolvedTransportType(); got != "http" {
		t.Errorf("type alias: ResolvedTransportType = %q, want http", got)
	}
	if got := (MCPServerConfig{TransportType: "stdio", Type: "http"}).ResolvedTransportType(); got != "stdio" {
		t.Errorf("both set: ResolvedTransportType = %q, want stdio (transport_type wins)", got)
	}
	if got := (MCPServerConfig{Type: "HTTP"}).ResolvedTransportType(); got != "http" {
		t.Errorf("uppercase normalize: ResolvedTransportType = %q, want http", got)
	}
}

func TestATDD_59_1_012_HTTPConfigHeadersPassthrough(t *testing.T) {
	cfg := MCPServerConfig{Type: "http", URL: "http://x/mcp", Headers: map[string]string{"X-Api-Key": "k"}}.ToMCPConfig("s")
	if cfg.Headers["X-Api-Key"] != "k" {
		t.Errorf("Headers not passed through: %#v", cfg.Headers)
	}
}

// -----------------------------------------------------------------------------
// conditional validation (AC2)
// -----------------------------------------------------------------------------

func TestATDD_59_1_020_Validate_HTTPRequiresURL(t *testing.T) {
	err := MCPServerConfig{Type: "http"}.Validate() // no url
	if err == nil {
		t.Fatal("expected error: http transport requires url")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "url") {
		t.Errorf("error %q should mention url", err.Error())
	}
}

func TestATDD_59_1_021_Validate_StdioRequiresCommand(t *testing.T) {
	err := MCPServerConfig{TransportType: "stdio"}.Validate() // no command
	if err == nil {
		t.Fatal("expected error: stdio transport requires command")
	}
}

// -----------------------------------------------------------------------------
// HTTPTransport behavior (AC3..AC9)
// -----------------------------------------------------------------------------

func TestATDD_59_1_030_Connect_InitializeHandshake(t *testing.T) {
	mock := &mockStreamableHTTPServer{sessionID: "sess-abc"}
	srv := newMockHTTPServer(t, mock)
	tr := NewHTTPTransport(vfs.MCPConfig{
		TransportType: "http", URL: srv.URL,
		Headers: map[string]string{"Authorization": "Bearer tok"},
	}, WithHTTPClient(srv.Client()))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := tr.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if !strings.Contains(mock.lastAccept, "application/json") || !strings.Contains(mock.lastAccept, "text/event-stream") {
		t.Errorf("Accept header = %q, want application/json + text/event-stream", mock.lastAccept)
	}
	if mock.lastContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", mock.lastContentType)
	}
	if mock.lastAuthorization != "Bearer tok" {
		t.Errorf("Authorization = %q, want Bearer tok", mock.lastAuthorization)
	}
	if mock.lastInitVersion != mcpProtocolVersion {
		t.Errorf("initialize protocolVersion = %q, want %q", mock.lastInitVersion, mcpProtocolVersion)
	}
	if !mock.gotInitialized {
		t.Error("server never received notifications/initialized")
	}
}

func TestATDD_59_1_031_Call_SessionAndProtocolHeaders(t *testing.T) {
	mock := &mockStreamableHTTPServer{sessionID: "sess-abc"}
	srv := newMockHTTPServer(t, mock)
	tr := NewHTTPTransport(vfs.MCPConfig{TransportType: "http", URL: srv.URL}, WithHTTPClient(srv.Client()))
	ctx := context.Background()
	if err := tr.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	if _, err := tr.Call(ctx, "tools/list", nil); err != nil {
		t.Fatalf("Call: %v", err)
	}
	mock.mu.Lock()
	defer mock.mu.Unlock()
	if mock.lastSessionHeader != "sess-abc" {
		t.Errorf("Mcp-Session-Id = %q, want sess-abc", mock.lastSessionHeader)
	}
	if mock.lastProtocolVer != mcpProtocolVersion {
		t.Errorf("MCP-Protocol-Version = %q, want %q", mock.lastProtocolVer, mcpProtocolVersion)
	}
}

// _032 — stateless server (no Mcp-Session-Id) still gets MCP-Protocol-Version on
// post-init requests; session header stays absent. (CR fix: spec compliance)
func TestATDD_59_1_032_Call_StatelessProtocolVersion(t *testing.T) {
	mock := &mockStreamableHTTPServer{noGetStream: true} // sessionID "" → stateless
	srv := newMockHTTPServer(t, mock)
	tr := NewHTTPTransport(vfs.MCPConfig{TransportType: "http", URL: srv.URL}, WithHTTPClient(srv.Client()))
	ctx := context.Background()
	if err := tr.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	if _, err := tr.Call(ctx, "tools/list", nil); err != nil {
		t.Fatalf("Call: %v", err)
	}
	mock.mu.Lock()
	defer mock.mu.Unlock()
	if mock.lastProtocolVer != mcpProtocolVersion {
		t.Errorf("stateless MCP-Protocol-Version = %q, want %q (sent regardless of session)", mock.lastProtocolVer, mcpProtocolVersion)
	}
	if mock.lastSessionHeader != "" {
		t.Errorf("stateless server got Mcp-Session-Id = %q, want empty", mock.lastSessionHeader)
	}
}

func TestATDD_59_1_040_Call_JSONResponse(t *testing.T) {
	mock := &mockStreamableHTTPServer{sseForCall: false}
	srv := newMockHTTPServer(t, mock)
	tr := NewHTTPTransport(vfs.MCPConfig{TransportType: "http", URL: srv.URL}, WithHTTPClient(srv.Client()))
	ctx := context.Background()
	if err := tr.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	res, err := tr.Call(ctx, "tools/list", nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !strings.Contains(string(res), "tools") {
		t.Errorf("result %q missing tools field", string(res))
	}
}

func TestATDD_59_1_041_Call_SSEResponse(t *testing.T) {
	mock := &mockStreamableHTTPServer{sseForCall: true}
	srv := newMockHTTPServer(t, mock)
	tr := NewHTTPTransport(vfs.MCPConfig{TransportType: "http", URL: srv.URL}, WithHTTPClient(srv.Client()))
	ctx := context.Background()
	if err := tr.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	res, err := tr.Call(ctx, "tools/list", nil)
	if err != nil {
		t.Fatalf("Call (SSE): %v", err)
	}
	if !strings.Contains(string(res), "tools") {
		t.Errorf("SSE-parsed result %q missing tools field", string(res))
	}
}

func TestATDD_59_1_050_ServerStream_405Graceful(t *testing.T) {
	mock := &mockStreamableHTTPServer{noGetStream: true}
	srv := newMockHTTPServer(t, mock)
	tr := NewHTTPTransport(vfs.MCPConfig{TransportType: "http", URL: srv.URL}, WithHTTPClient(srv.Client()))
	ctx := context.Background()
	if err := tr.Connect(ctx); err != nil {
		t.Fatalf("Connect should succeed despite no GET stream: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })
	if tr.Status() != vfs.MCPStatusConnected {
		t.Errorf("Status = %v, want Connected after 405 degrade", tr.Status())
	}
	// A second call must still work — 405 on the GET stream does not poison POST.
	if _, err := tr.Call(ctx, "tools/list", nil); err != nil {
		t.Errorf("Call after 405 degrade failed: %v", err)
	}
}

// _051 — a 200 GET stream that closes with zero frames (server has no push) must
// NOT trigger an endless reconnect treadmill. (CR fix: edge-case)
func TestATDD_59_1_051_ServerStream_200EmptyNoTreadmill(t *testing.T) {
	mock := &mockStreamableHTTPServer{emptyGet: true}
	srv := newMockHTTPServer(t, mock)
	tr := NewHTTPTransport(vfs.MCPConfig{TransportType: "http", URL: srv.URL}, WithHTTPClient(srv.Client()))
	if err := tr.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	// Give the stream loop time to (not) reconnect. With the treadmill bug this
	// would climb; with the fix it exits after the first zero-frame 200.
	time.Sleep(1500 * time.Millisecond)
	if rc := tr.ReconnectCount(); rc != 0 {
		t.Errorf("ReconnectCount = %d, want 0 (200 empty stream must not reconnect)", rc)
	}
	mock.mu.Lock()
	gets := mock.getCount
	mock.mu.Unlock()
	if gets > 1 {
		t.Errorf("getCount = %d, want 1 (no reconnect treadmill)", gets)
	}
}

func TestATDD_59_1_060_Resumability_LastEventID(t *testing.T) {
	mock := &mockStreamableHTTPServer{} // GET returns one frame (id:1) then closes → drop
	srv := newMockHTTPServer(t, mock)
	tr := NewHTTPTransport(vfs.MCPConfig{TransportType: "http", URL: srv.URL}, WithHTTPClient(srv.Client()))
	if err := tr.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	// The server stream drops after one frame; the transport reconnects with
	// Last-Event-ID. Poll for the actual reconnect GET (getCount>=2); backoff is
	// ~1s so allow up to ~5s.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mock.mu.Lock()
		done := mock.getCount >= 2
		mock.mu.Unlock()
		if done {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	mock.mu.Lock()
	gotEventID := mock.lastGetEventID
	getCount := mock.getCount
	mock.mu.Unlock()
	if getCount < 2 {
		t.Fatalf("getCount = %d, want >=2 (initial + reconnect)", getCount)
	}
	if tr.ReconnectCount() < 1 {
		t.Errorf("ReconnectCount = %d, want >=1 after server stream drop", tr.ReconnectCount())
	}
	if gotEventID != "1" {
		t.Errorf("reconnect Last-Event-ID = %q, want 1 (cursor from id:1 frame)", gotEventID)
	}
}

func TestATDD_59_1_070_Close_DELETESession(t *testing.T) {
	mock := &mockStreamableHTTPServer{sessionID: "sess-xyz", noGetStream: true}
	srv := newMockHTTPServer(t, mock)
	tr := NewHTTPTransport(vfs.MCPConfig{TransportType: "http", URL: srv.URL}, WithHTTPClient(srv.Client()))
	if err := tr.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	_ = tr.Close()
	_ = tr.Close() // idempotent: second Close must not DELETE again

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if mock.deleteCount != 1 {
		t.Errorf("deleteCount = %d, want exactly 1 (idempotent Close)", mock.deleteCount)
	}
	if mock.lastSessionHeader != "sess-xyz" {
		t.Errorf("DELETE Mcp-Session-Id = %q, want sess-xyz", mock.lastSessionHeader)
	}
}

func TestATDD_59_1_071_Call_404Reinitialize(t *testing.T) {
	mock := &mockStreamableHTTPServer{
		sessionID:      "sess-1",
		noGetStream:    true,
		failCallStatus: http.StatusNotFound,
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

	// First tools/call → 404 (session expired); transport must re-initialize and retry → success.
	res, err := tr.Call(ctx, "tools/call", nil)
	if err != nil {
		t.Fatalf("Call should succeed after 404 re-initialize: %v", err)
	}
	if res == nil {
		t.Error("expected a result after retry")
	}
	mock.mu.Lock()
	defer mock.mu.Unlock()
	if mock.initCount < 2 {
		t.Errorf("initCount = %d, want >=2 (initial + re-initialize after 404)", mock.initCount)
	}
	if tr.ReconnectCount() < 1 {
		t.Errorf("ReconnectCount = %d, want >=1 after 404 re-initialize", tr.ReconnectCount())
	}
}

func TestATDD_59_1_072_Call_400Error(t *testing.T) {
	mock := &mockStreamableHTTPServer{
		noGetStream:    true,
		failCallStatus: http.StatusBadRequest,
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

	if _, err := tr.Call(ctx, "tools/call", nil); err == nil {
		t.Fatal("expected error on 400, got nil (must not be silently swallowed)")
	}
}

func TestATDD_59_1_080_Headers_EnvInterpolation(t *testing.T) {
	mock := &mockStreamableHTTPServer{noGetStream: true}
	srv := newMockHTTPServer(t, mock)
	tr := NewHTTPTransport(vfs.MCPConfig{
		TransportType: "http", URL: srv.URL,
		Headers: map[string]string{"Authorization": "Bearer ${MANTRA_TOKEN}"},
	},
		WithHTTPClient(srv.Client()),
		WithEnvLookup(func(k string) string {
			if k == "MANTRA_TOKEN" {
				return "resolved-secret"
			}
			return ""
		}),
	)
	if err := tr.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if mock.lastAuthorization != "Bearer resolved-secret" {
		t.Errorf("Authorization = %q, want Bearer resolved-secret (${ENV} interpolated)", mock.lastAuthorization)
	}
}

func TestATDD_59_1_090_HealthSemantics(t *testing.T) {
	mock := &mockStreamableHTTPServer{noGetStream: true}
	srv := newMockHTTPServer(t, mock)
	tr := NewHTTPTransport(vfs.MCPConfig{TransportType: "http", URL: srv.URL}, WithHTTPClient(srv.Client()))
	if err := tr.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })
	if !tr.Alive() {
		t.Error("Alive() should be true after a successful HTTP exchange")
	}
	if tr.Status() != vfs.MCPStatusConnected {
		t.Errorf("Status = %v, want Connected", tr.Status())
	}
}
