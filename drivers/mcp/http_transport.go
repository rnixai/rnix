package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/xsync"
	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// HTTPTransport — MCP Streamable HTTP client transport (Story 59.1 / Epic 59).
//
// Implements vfs.MCPTransport over the 2025-03-26+ Streamable HTTP transport
// (single endpoint, HTTP POST/GET/DELETE + optional SSE). Wire contract:
// specs/spec-mcp-http-transport/decision-matrix.md. Decisions D1–D5:
// protocolVersion=2025-06-18; transport_type authoritative (type alias);
// headers ${ENV} interpolation; Alive()=last HTTP reachability,
// StderrTail()=HTTP diagnostic ring; remaining 5 health methods reuse the
// stdio atomic/cached semantics.
//
// Scope note (D5): server-initiated GET SSE stream + 405 graceful degrade are
// implemented (CAP-5); the client tracks the Last-Event-ID cursor and reconnects
// (CAP-6 client mechanism). Full server-side *replay* semantics are the server's
// responsibility. Project .env (EnvSnapshot) priority for ${ENV} is deferred —
// the production factory currently resolves via os.Getenv (see deferred-work).
// =============================================================================

// mcpProtocolVersion is the protocolVersion advertised in the initialize
// request and echoed in the MCP-Protocol-Version header on subsequent requests
// (decision D1). Shared constant so a future bump touches one place.
const mcpProtocolVersion = "2025-06-18"

// httpAcceptHeader is mandatory on every POST: a Streamable HTTP server returns
// 406 when the client does not advertise both JSON and SSE (the claude-agent-sdk
// #202 footgun the SPEC calls out).
const httpAcceptHeader = "application/json, text/event-stream"

// errSessionExpired is the sentinel for an HTTP 404 on a request carrying a
// session id — the server expired the session, so the caller re-initializes.
var errSessionExpired = errStub("mcp http: session expired (404)")

type errStub string

func (e errStub) Error() string { return string(e) }

// HTTPTransport speaks MCP over Streamable HTTP. client is an injection seam for
// httptest; envLookup resolves ${ENV} placeholders in headers (D3 / AC9),
// defaulting to os.Getenv when nil.
type HTTPTransport struct {
	endpoint  string
	client    *http.Client
	envLookup func(string) string

	requestTimeout time.Duration
	maxOutputBytes int64

	// resolvedHeaders is config.Headers after ${ENV} interpolation, computed
	// once at construction.
	resolvedHeaders map[string]string

	mu                sync.Mutex
	sessionID         string      // Mcp-Session-Id assigned at initialize ("" = stateless/not yet)
	negotiatedVersion string      // protocolVersion from the initialize result; "" until negotiated → applyHeaders falls back to mcpProtocolVersion
	initialized       atomic.Bool // true once initialize handshake has completed (gates MCP-Protocol-Version)
	connected         bool
	closed            bool

	nextID atomic.Int64

	// --- health/status surface (mirrors StdioTransport atomic set) ---
	status         atomic.Int32 // vfs.MCPStatus
	lastCall       atomic.Int64 // Unix-nano of last successful Call
	lastCheck      atomic.Int64 // Unix-nano of last successful readiness check
	reachable      atomic.Bool  // last HTTP exchange reached a non-5xx server (D4 Alive)
	reconnectCount atomic.Int64
	toolCount      atomic.Int64
	resourceCount  atomic.Int64

	// diagBuf is the HTTP diagnostic ring (decision D4): request errors, non-2xx
	// responses, SSE parse failures, session 400/404. Surfaced via StderrTail().
	diagBuf *xsync.RingBuffer[string]

	// server-initiated GET SSE stream (CAP-5/6)
	lastEventID  atomic.Value // string cursor for Last-Event-ID resume
	streamCancel context.CancelFunc
	streamWG     sync.WaitGroup
}

// HTTPTransportOption configures a HTTPTransport at construction (test seams).
type HTTPTransportOption func(*HTTPTransport)

// WithHTTPClient injects an *http.Client (httptest seam).
func WithHTTPClient(c *http.Client) HTTPTransportOption {
	return func(t *HTTPTransport) { t.client = c }
}

// WithEnvLookup injects the ${ENV} resolver (D3 / AC9 test seam). Mirrors
// drivers/web BuildOpts.EnvLookup.
func WithEnvLookup(fn func(string) string) HTTPTransportOption {
	return func(t *HTTPTransport) { t.envLookup = fn }
}

// NewHTTPTransport builds a Streamable HTTP transport from cfg. Zero-value
// timeout/output knobs are backfilled with the package defaults (same contract
// as NewStdioTransport). The Connect budget is governed by the caller's ctx
// (MountManager wraps it with cfg.MountTimeout), so HTTPTransport does not keep
// a separate mount-timeout field.
func NewHTTPTransport(cfg vfs.MCPConfig, opts ...HTTPTransportOption) *HTTPTransport {
	t := &HTTPTransport{
		endpoint:       cfg.URL,
		requestTimeout: cfg.RequestTimeout,
		maxOutputBytes: cfg.MaxOutputBytes,
		diagBuf:        xsync.NewRingBuffer[string](stderrRingCapacity),
	}
	for _, o := range opts {
		o(t)
	}
	if t.client == nil {
		t.client = &http.Client{}
	}
	if t.envLookup == nil {
		t.envLookup = os.Getenv
	}
	if t.requestTimeout <= 0 {
		t.requestTimeout = defaultRequestTimeout
	}
	if t.maxOutputBytes <= 0 {
		t.maxOutputBytes = defaultMaxOutputBytes
	}
	// Interpolate ${ENV} in headers once (D3 / AC9). A referenced var that
	// resolves empty is surfaced as a diagnostic so a silent "Bearer " (empty
	// token) is debuggable via `rnix mcp logs`.
	if len(cfg.Headers) > 0 {
		t.resolvedHeaders = make(map[string]string, len(cfg.Headers))
		for k, v := range cfg.Headers {
			key := k
			t.resolvedHeaders[k] = os.Expand(v, func(name string) string {
				val := t.envLookup(name)
				if val == "" {
					t.diag("[mcp] WARN: header %q references unset env var ${%s}", key, name)
				}
				return val
			})
		}
	}
	t.lastEventID.Store("")
	t.status.Store(int32(vfs.MCPStatusDisconnected))
	return t
}

// diag pushes a one-line diagnostic into the ring (surfaced by `rnix mcp logs`).
func (t *HTTPTransport) diag(format string, args ...any) {
	t.diagBuf.Push(fmt.Sprintf(format, args...))
}

// --- vfs.MCPTransport: connection lifecycle ---

// Connect performs the Streamable HTTP initialize handshake (POST initialize →
// capture Mcp-Session-Id → POST notifications/initialized), refreshes tool
// counts, and opens the best-effort server→client SSE stream.
func (t *HTTPTransport) Connect(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.endpoint == "" {
		return types.NewDriverError("Connect", "", fmt.Errorf("empty url"), types.ErrInvalid)
	}
	if err := t.initializeLocked(ctx); err != nil {
		t.status.Store(int32(vfs.MCPStatusError))
		return err
	}
	t.connected = true
	t.status.Store(int32(vfs.MCPStatusConnected))
	t.lastCall.Store(nowFunc().UnixNano())
	t.refreshCountsLocked(ctx)
	t.startServerStreamLocked()
	return nil
}

// initializeLocked runs the MCP initialize → initialized handshake. Caller holds mu.
func (t *HTTPTransport) initializeLocked(ctx context.Context) error {
	initParams := map[string]any{
		"protocolVersion": mcpProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": "rnix", "version": "1.0.0"},
	}
	params, err := json.Marshal(initParams)
	if err != nil {
		return types.NewDriverError("Connect", "initialize", err, types.ErrInternal)
	}
	res, err := t.requestLocked(ctx, "initialize", params)
	if err != nil {
		return fmt.Errorf("initialize handshake: %w", err)
	}
	// Capture the negotiated protocolVersion. The server echoes the version it
	// actually settled on in result.protocolVersion (it may DOWNGRADE the
	// client's advertised version), and the Streamable HTTP spec requires every
	// post-init request to carry THAT value in the MCP-Protocol-Version header —
	// not the client's request value. Reset first so a CAP-7 re-initialize whose
	// result omits protocolVersion falls back to the constant instead of reusing
	// the prior session's negotiated value; a missing/unparseable field then
	// leaves negotiatedVersion "" → applyHeaders uses mcpProtocolVersion.
	t.negotiatedVersion = ""
	var ir struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if uerr := json.Unmarshal(res, &ir); uerr == nil && ir.ProtocolVersion != "" {
		t.negotiatedVersion = ir.ProtocolVersion
	}
	// Negotiation done: from now on every request carries MCP-Protocol-Version
	// (spec: sent on all post-init requests regardless of whether the server
	// assigned a session).
	t.initialized.Store(true)
	// notifications/initialized is a fire-and-forget notification (202, no body).
	if err := t.notifyLocked(ctx, "notifications/initialized"); err != nil {
		t.diag("[mcp] WARN: notifications/initialized failed: %v", err)
	}
	return nil
}

// Call sends a JSON-RPC request over POST. On a 404 against a session-bearing
// transport it re-initializes once and retries (CAP-7); a 404 on a sessionless
// transport is a genuine error (likely wrong URL), not a session expiry.
func (t *HTTPTransport) Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.connected {
		return nil, types.NewDriverError("Call", method, fmt.Errorf("not connected"), types.ErrDeviceDisconnected)
	}
	hadSession := t.sessionID != ""
	res, err := t.requestLocked(ctx, method, params)
	if err == nil {
		t.lastCall.Store(nowFunc().UnixNano())
		return res, nil
	}
	// CAP-7: session expired → drop it, re-initialize, retry once. Only when we
	// actually held a session; otherwise a 404 means the endpoint/path is wrong.
	if isSessionExpired(err) {
		if !hadSession {
			return nil, types.NewDriverError("Call", method, fmt.Errorf("server returned 404 (no active session; check url)"), types.ErrNotFound)
		}
		t.diag("[mcp] session expired; re-initializing")
		t.sessionID = ""
		if ierr := t.initializeLocked(ctx); ierr != nil {
			return nil, types.NewDriverError("Call", method, fmt.Errorf("re-initialize after 404: %w", ierr), types.ErrDeviceDisconnected)
		}
		t.reconnectCount.Add(1)
		res, err = t.requestLocked(ctx, method, params)
		if err == nil {
			t.lastCall.Store(nowFunc().UnixNano())
			return res, nil
		}
		// Retry also failed — return a typed error, never the bare sentinel.
		return nil, types.NewDriverError("Call", method, fmt.Errorf("call failed after 404 re-initialize: %w", err), types.ErrDeviceDisconnected)
	}
	return res, err
}

// requestLocked issues one JSON-RPC request/response round over POST and parses
// either application/json or text/event-stream. Caller holds mu.
func (t *HTTPTransport) requestLocked(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	id := t.nextID.Add(1)
	body, err := json.Marshal(jsonRPCRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return nil, types.NewDriverError("Call", method, err, types.ErrInternal)
	}

	reqCtx := ctx
	if t.requestTimeout > 0 {
		var cancel context.CancelFunc
		reqCtx, cancel = context.WithTimeout(ctx, t.requestTimeout)
		defer cancel()
	}

	resp, err := t.doPost(reqCtx, body)
	if err != nil {
		t.diag("[mcp] %s POST error: %v", method, err)
		return nil, types.NewDriverError("Call", method, err, types.ErrServiceUnavailable)
	}
	defer func() { _ = resp.Body.Close() }()

	// initialize (and any request) may carry a fresh session id.
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		t.sessionID = sid
	}

	switch {
	case resp.StatusCode == http.StatusAccepted: // 202 — accepted, no body
		t.reachable.Store(true)
		return nil, nil
	case resp.StatusCode == http.StatusNotFound: // 404 — session expired
		t.reachable.Store(true)
		t.diag("[mcp] %s -> 404 (session expired)", method)
		return nil, errSessionExpired
	case resp.StatusCode == http.StatusBadRequest: // 400 — e.g. missing session
		t.reachable.Store(true)
		t.diag("[mcp] %s -> 400 bad request", method)
		return nil, types.NewDriverError("Call", method, fmt.Errorf("server returned 400 bad request"), types.ErrInvalid)
	case resp.StatusCode >= 500:
		t.reachable.Store(false)
		t.diag("[mcp] %s -> %d", method, resp.StatusCode)
		return nil, types.NewDriverError("Call", method, fmt.Errorf("server returned %d", resp.StatusCode), types.ErrServiceUnavailable)
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		t.reachable.Store(false)
		t.diag("[mcp] %s -> %d", method, resp.StatusCode)
		return nil, types.NewDriverError("Call", method, fmt.Errorf("server returned %d", resp.StatusCode), types.ErrServiceUnavailable)
	}
	t.reachable.Store(true)

	raw, err := t.readRPCResponse(resp, id)
	if err != nil {
		t.diag("[mcp] %s response parse error: %v", method, err)
		return nil, types.NewDriverError("Call", method, err, types.ErrDriver)
	}

	var rpc jsonRPCResponse
	if err := json.Unmarshal(raw, &rpc); err != nil {
		return nil, types.NewDriverError("Call", method, fmt.Errorf("parse response: %w", err), types.ErrDriver)
	}
	if rpc.Error != nil {
		return nil, types.NewDriverError("Call", method, fmt.Errorf("rpc error %d: %s", rpc.Error.Code, rpc.Error.Message), mcpRPCErrCode(rpc.Error.Code))
	}
	return t.truncateResult(method, rpc.Result), nil
}

// notifyLocked sends a JSON-RPC notification (no id); a Streamable HTTP server
// replies 202 with no body. Caller holds mu.
func (t *HTTPTransport) notifyLocked(ctx context.Context, method string) error {
	body, err := json.Marshal(struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
	}{JSONRPC: "2.0", Method: method})
	if err != nil {
		return err
	}
	resp, err := t.doPost(ctx, body)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 300 && resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("notification %s -> %d", method, resp.StatusCode)
	}
	return nil
}

// doPost builds and sends a POST to the MCP endpoint with the mandatory headers,
// configured custom headers, and (post-init) session + protocol-version headers.
func (t *HTTPTransport) doPost(ctx context.Context, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", httpAcceptHeader)
	t.applyHeaders(req)
	return t.client.Do(req)
}

// applyHeaders sets custom headers, the protocol-version header (once
// initialized, regardless of session — spec requirement for stateless servers),
// and the session id when the server assigned one. The protocol-version value
// is the one negotiated at initialize (falling back to the advertised constant
// when the server returned none). Read under mu: every applyHeaders caller
// holds it (requestLocked, notifyLocked, openServerStream).
func (t *HTTPTransport) applyHeaders(req *http.Request) {
	for k, v := range t.resolvedHeaders {
		req.Header.Set(k, v)
	}
	if t.initialized.Load() {
		version := t.negotiatedVersion
		if version == "" {
			version = mcpProtocolVersion
		}
		req.Header.Set("MCP-Protocol-Version", version)
	}
	if t.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", t.sessionID)
	}
}

// readRPCResponse reads the JSON-RPC response from either an application/json
// body (single object) or a text/event-stream body (the data: frame whose
// JSON-RPC id matches wantID).
func (t *HTTPTransport) readRPCResponse(resp *http.Response, wantID int64) ([]byte, error) {
	ct := resp.Header.Get("Content-Type")
	if strings.Contains(ct, "text/event-stream") {
		var found []byte
		_, err := t.scanSSE(resp.Body, func(payload []byte) bool {
			var probe jsonRPCResponse
			if err := json.Unmarshal(payload, &probe); err == nil && probe.ID == wantID {
				found = payload
				return true // stop
			}
			return false
		})
		if err != nil {
			return nil, err
		}
		if found == nil {
			return nil, fmt.Errorf("sse stream ended without a response for id %d", wantID)
		}
		return found, nil
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, t.maxOutputBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	return data, nil
}

// scanSSE parses an SSE stream frame-by-frame, tracking the event-id cursor for
// resumability and invoking onFrame for each complete data payload. Multiple
// data: lines within one frame are joined with "\n" per the SSE spec. onFrame
// returns true to stop early. Returns the number of data frames seen.
func (t *HTTPTransport) scanSSE(r io.Reader, onFrame func(payload []byte) bool) (int, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), max(4*1024*1024, int(t.maxOutputBytes)))
	var data []string
	frames := 0
	flush := func() bool {
		if len(data) == 0 {
			return false
		}
		payload := []byte(strings.Join(data, "\n"))
		data = data[:0]
		frames++
		return onFrame(payload)
	}
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "": // frame boundary
			if flush() {
				return frames, nil
			}
		case strings.HasPrefix(line, "id:"):
			t.lastEventID.Store(strings.TrimSpace(line[3:]))
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimPrefix(strings.TrimSpace(line[5:]), " "))
		}
	}
	if err := sc.Err(); err != nil {
		return frames, fmt.Errorf("sse scan: %w", err)
	}
	// flush a trailing frame with no terminating blank line
	_ = flush()
	return frames, nil
}

// truncateResult caps a result at maxOutputBytes, reusing the stdio truncation
// helpers (JSON-validity preserving). diag-logs when it cuts.
func (t *HTTPTransport) truncateResult(method string, result json.RawMessage) json.RawMessage {
	limit := t.maxOutputBytes
	if limit <= 0 || int64(len(result)) <= limit {
		return result
	}
	t.diag("[mcp] WARN: %s result truncated from %d to %d bytes (max_output_bytes)", method, len(result), limit)
	if out, ok := truncateJSONText(result, limit); ok {
		return out
	}
	return result[:limit]
}

// refreshCountsLocked best-effort caches tools/list + resources/list counts.
func (t *HTTPTransport) refreshCountsLocked(ctx context.Context) {
	if line, err := t.requestLocked(ctx, "tools/list", nil); err == nil {
		t.toolCount.Store(int64(countResultArray(line, "tools")))
	}
	if line, err := t.requestLocked(ctx, "resources/list", nil); err == nil {
		t.resourceCount.Store(int64(countResultArray(line, "resources")))
	}
	t.lastCheck.Store(nowFunc().UnixNano())
}

// --- server-initiated SSE stream (CAP-5/6) ---

// startServerStreamLocked opens the GET SSE stream in a goroutine (best effort).
// A 405 means the server offers no server-initiated stream — graceful degrade.
func (t *HTTPTransport) startServerStreamLocked() {
	ctx, cancel := context.WithCancel(context.Background())
	t.streamCancel = cancel
	t.streamWG.Add(1)
	go t.serverStreamLoop(ctx)
}

func (t *HTTPTransport) serverStreamLoop(ctx context.Context) {
	defer t.streamWG.Done()
	for attempt := 0; ; attempt++ {
		if ctx.Err() != nil {
			return
		}
		status, frames, err := t.openServerStream(ctx)
		if status == http.StatusMethodNotAllowed {
			t.diag("[mcp] server stream: 405 (no server-initiated stream)")
			return // graceful degrade — server has no GET stream
		}
		if ctx.Err() != nil {
			return
		}
		// A 200 stream that closed having pushed zero frames is a server with no
		// server-initiated messages (just a short-lived/empty GET). Do not treat
		// it as a dropped stream — exit instead of reconnecting forever.
		if status == http.StatusOK && frames == 0 && err == nil {
			t.diag("[mcp] server stream: 200 with no frames (no server push); not reconnecting")
			return
		}
		if err != nil {
			t.diag("[mcp] server stream ended: %v", err)
		}
		// Stream dropped after delivering frames (or errored): reconnect with
		// Last-Event-ID.
		t.reconnectCount.Add(1)
		select {
		case <-ctx.Done():
			return
		case <-time.After(streamReconnectBackoff(attempt)):
		}
	}
}

// openServerStream issues the GET, consumes frames until EOF/error, returning
// the HTTP status (for the 405 check), the number of frames consumed, and any
// read error.
func (t *HTTPTransport) openServerStream(ctx context.Context) (int, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.endpoint, nil)
	if err != nil {
		return 0, 0, err
	}
	req.Header.Set("Accept", "text/event-stream")
	t.mu.Lock()
	t.applyHeaders(req)
	t.mu.Unlock()
	if last, _ := t.lastEventID.Load().(string); last != "" {
		req.Header.Set("Last-Event-ID", last)
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return resp.StatusCode, 0, nil
	}
	// Consume server-pushed frames, tracking the event-id cursor. Server-initiated
	// messages are observed for liveness/resumption; tool-call replies are
	// correlated by the POST path, so we do not dispatch them here.
	frames, err := t.scanSSE(resp.Body, func([]byte) bool { return false })
	return http.StatusOK, frames, err
}

func streamReconnectBackoff(attempt int) time.Duration {
	return time.Duration(1<<min(attempt, 3)) * time.Second // 1,2,4,8s cap
}

// Close sends DELETE + Mcp-Session-Id when a session exists, tears down the
// server stream, and is idempotent.
func (t *HTTPTransport) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	t.connected = false
	t.status.Store(int32(vfs.MCPStatusDisconnected))
	cancel := t.streamCancel
	t.streamCancel = nil
	sid := t.sessionID
	t.mu.Unlock()

	if cancel != nil {
		cancel()
		// Bounded wait: the request ctx is cancelled above, which unblocks an
		// in-flight read with the default client; the timeout guards against a
		// client that ignores ctx so Close/unmount can never hang forever.
		done := make(chan struct{})
		go func() { t.streamWG.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(gracefulShutdownTimeout):
			t.diag("[mcp] close: server stream goroutine did not exit within %v", gracefulShutdownTimeout)
		}
	}
	if sid != "" {
		t.deleteSession(sid)
	}
	return nil
}

// deleteSession sends the DELETE that explicitly terminates the session (200 OK).
func (t *HTTPTransport) deleteSession(sid string) {
	ctx, cancel := context.WithTimeout(context.Background(), gracefulShutdownTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, t.endpoint, nil)
	if err != nil {
		t.diag("[mcp] DELETE build error: %v", err)
		return
	}
	for k, v := range t.resolvedHeaders {
		req.Header.Set(k, v)
	}
	req.Header.Set("Mcp-Session-Id", sid)
	resp, err := t.client.Do(req)
	if err != nil {
		t.diag("[mcp] DELETE error: %v", err)
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

// Ping POSTs a JSON-RPC ping.
func (t *HTTPTransport) Ping(ctx context.Context) error {
	if _, err := t.Call(ctx, "ping", nil); err != nil {
		return fmt.Errorf("ping: %w", err)
	}
	return nil
}

// --- vfs.MCPTransport: health / status surface ---

// Status returns the authoritative transport status (reused semantics).
func (t *HTTPTransport) Status() vfs.MCPStatus { return vfs.MCPStatus(t.status.Load()) }

// Alive reports HTTP reachability (decision D4): true once an exchange reached a
// non-5xx server, with a Connected-status fallback for the not-yet-exchanged case.
func (t *HTTPTransport) Alive() bool {
	return t.reachable.Load() || vfs.MCPStatus(t.status.Load()) == vfs.MCPStatusConnected
}

// ToolCount returns the cached tools/list count.
func (t *HTTPTransport) ToolCount() int { return int(t.toolCount.Load()) }

// ResourceCount returns the cached resources/list count.
func (t *HTTPTransport) ResourceCount() int { return int(t.resourceCount.Load()) }

// LastCheck returns the time of the last successful readiness check (zero=never).
func (t *HTTPTransport) LastCheck() time.Time {
	ns := t.lastCheck.Load()
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}

// ReconnectCount returns the monotonic successful-reconnect counter.
func (t *HTTPTransport) ReconnectCount() int { return int(t.reconnectCount.Load()) }

// StderrTail returns the captured HTTP diagnostic ring (decision D4).
func (t *HTTPTransport) StderrTail() []string { return t.diagBuf.Snapshot() }

// isSessionExpired reports whether err is (or wraps) the 404 session sentinel.
func isSessionExpired(err error) bool {
	for e := err; e != nil; {
		if e == errSessionExpired {
			return true
		}
		u, ok := e.(interface{ Unwrap() error })
		if !ok {
			break
		}
		e = u.Unwrap()
	}
	return false
}

// mcpRPCErrCode maps a JSON-RPC error code to an rnix ErrCode (best effort).
func mcpRPCErrCode(code int) types.ErrCode {
	switch code {
	case -32601: // method not found
		return types.ErrNotFound
	case -32602: // invalid params
		return types.ErrInvalid
	default:
		return types.ErrDriver
	}
}

// Compile-time assertion: HTTPTransport satisfies the transport interface.
var _ vfs.MCPTransport = (*HTTPTransport)(nil)
