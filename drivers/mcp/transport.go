package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/xsync"
	"github.com/rnixai/rnix/vfs"
)

// gracefulShutdownTimeout is the SIGTERM-to-SIGKILL escalation budget for
// transport.Close (Story 48.2 AC4). Kept package-level (not configurable) by
// design — Story 48.6 introduces per-server timeout configuration.
const gracefulShutdownTimeout = 5 * time.Second

// Story 48.5 health-check tuning. Package-level consts (not per-server config —
// that is Story 48.6). l2IdleThreshold is the idle window after which the next
// Call runs an L2 readiness ping; l2PingTimeout bounds that ping so a hung-but-
// alive child cannot stall the call. stderrRingCapacity bounds the captured
// stderr ring buffer (AC4: "保留最近 256 行").
const (
	l2IdleThreshold    = 30 * time.Second
	l2PingTimeout      = 2 * time.Second
	stderrRingCapacity = 256
	// stderrMaxLineBytes caps a single captured stderr line. An oversized line
	// (e.g. a multi-MB stack dump) is truncated to this length rather than
	// permanently stopping the stderr pump ([Review][Patch] P5).
	stderrMaxLineBytes = 256 * 1024
)

// nowFunc is the injectable clock for L2 idle-window math. Tests swap it to
// jump past l2IdleThreshold without real sleeps (Story 48.5 §易错点 6 / 测试策略).
var nowFunc = time.Now

// TransportConfig holds configuration for creating a StdioTransport.
type TransportConfig struct {
	Command       string
	Args          []string
	Env           []string
	TimeoutMillis int64
	WorkDir       string
	// ReconnectPolicy governs the L2-triggered backoff reconnect (Story 48.5
	// AC3). Zero value → production defaults (3 retries, 1s/2s/4s backoff).
	ReconnectPolicy ReconnectPolicy
}

// jsonRPCRequest is the JSON-RPC 2.0 request envelope.
type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// jsonRPCResponse is the JSON-RPC 2.0 response envelope.
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

// jsonRPCError is the JSON-RPC 2.0 error object.
type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// StdioTransport communicates with an MCP server via stdin/stdout of a child process.
type StdioTransport struct {
	config    TransportConfig
	cmd       *exec.Cmd
	stdin     *json.Encoder
	stdout    *bufio.Scanner
	mu        sync.Mutex
	connected bool
	closed    bool // Story 48.2 AC3: idempotent Close gate
	nextID    atomic.Int64
	// lastChildPID retains the PID of the most recently Started child (set
	// just after cmd.Start, before initialize). Exposed via atomic so external
	// observers — chiefly Story 48.2 AC5 tests that need to verify process
	// group cleanup AFTER Connect has cleared t.cmd — can read it lock-free.
	// PID 0 means "never Started a child". Story 48.5 L1 uses it as the
	// liveness probe target; reconnect updates it on each re-exec.
	lastChildPID atomic.Int64

	// --- Story 48.5 authoritative health/status state (all atomic so the
	// read-only accessors below never contend with Call/reconnect) ---
	status         atomic.Int32 // vfs.MCPStatus
	lastCall       atomic.Int64 // Unix-nano of the last successful Call (L2 idle math)
	lastCheck      atomic.Int64 // Unix-nano of the last successful L2 ping (0 = never)
	reconnectCount atomic.Int64 // monotonic count of successful reconnects
	toolCount      atomic.Int64 // cached tools/list count (refreshed on connect/reconnect)
	resourceCount  atomic.Int64 // cached resources/list count

	// stderrBuf captures the child's stderr (AC4). It is a TRANSPORT field, not
	// a cmd field, so it survives reconnect (history retained + new lines
	// appended). Each Connect/reconnect spawns a fresh pump goroutine bound to
	// the new cmd's StderrPipe, all writing into this one buffer.
	stderrBuf *xsync.RingBuffer[string]

	// respCh / readDone implement the single-reader pump (Story 48.5). Exactly
	// ONE goroutine ever scans a given stdout scanner (readLoop), pushing each
	// response line onto respCh; callers consume via readResponseLocked with an
	// optional timeout. This is what makes the L2 ping / tools-list refresh
	// timeout-safe WITHOUT spawning competing Scan goroutines on one scanner
	// (the bug a naive per-call timeout goroutine would cause). readDone is
	// closed on teardown so a reader blocked sending an unconsumed line exits.
	respCh   chan readResult
	readDone chan struct{}
}

// readResult is one response line (or terminal error) from the stdout reader
// pump. id is the parsed JSON-RPC response id used to correlate the line with
// the request that asked for it ([Review][Patch] P1).
type readResult struct {
	data []byte
	id   int64
	err  error
}

// NewStdioTransport creates a new StdioTransport with the given configuration.
func NewStdioTransport(config TransportConfig) *StdioTransport {
	t := &StdioTransport{
		config:    config,
		stderrBuf: xsync.NewRingBuffer[string](stderrRingCapacity),
	}
	t.status.Store(int32(vfs.MCPStatusDisconnected))
	return t
}

// Connect starts the MCP server process and performs the initialize handshake.
func (t *StdioTransport) Connect(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.config.Command == "" {
		return fmt.Errorf("empty command")
	}

	if err := t.startProcessLocked(ctx); err != nil {
		// Story 48.2 Task 2.4 / AC5: reuse closeProcess so Connect failure runs
		// the process-group SIGTERM → SIGKILL cleanup (instead of a single-PID
		// Kill that leaks orphan grandchildren). closeProcessLocked sets
		// t.closed=true, which is correct for a transport that never came up.
		_ = t.closeProcessLocked()
		return err
	}

	t.connected = true
	t.status.Store(int32(vfs.MCPStatusConnected))
	// Seed lastCall so the first in-window Call skips L2 (zero-overhead
	// fast-path, Story 48.5 AC6). Best-effort tools/resources refresh.
	t.lastCall.Store(nowFunc().UnixNano())
	t.refreshCountsLocked(ctx)
	return nil
}

// startProcessLocked launches the child, wires stdin/stdout/stderr, starts the
// single-reader stdout pump, and runs the MCP initialize handshake. Caller MUST
// hold t.mu.
//
// The child is started with exec.Command — NOT exec.CommandContext(ctx) — so
// its lifetime is governed by Close/teardown (SIGTERM → SIGKILL), not by the
// per-call connect ctx. Binding it to ctx would (a) kill the server the moment
// a caller cancels the connect ctx after Connect returns (Story 48.5
// connectMock does exactly this) and (b) collapse Close's graceful SIGTERM
// window into an immediate ctx-driven Kill. The connect ctx still bounds the
// handshake itself via the read timeout below, preserving the mount timeout
// (NFR25 ≤ 500ms). On failure t.cmd is left populated so the caller's cleanup
// path tears the half-started process down — startProcessLocked itself does not
// decide closed vs reconnectable, keeping it reusable by Connect and reconnect.
func (t *StdioTransport) startProcessLocked(ctx context.Context) error {
	cmd := exec.Command(t.config.Command, t.config.Args...)
	if len(t.config.Env) > 0 {
		cmd.Env = t.config.Env
	}
	if t.config.WorkDir != "" {
		cmd.Dir = t.config.WorkDir
	}

	// Story 48.2 Task 4.1: apply Setpgid (+ Pdeathsig on Linux) before Start so
	// the kernel honors it at fork time. Reused on every reconnect (AC3).
	applyProcessGroupIsolation(cmd)

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	// Story 48.5 AC4: capture stderr. We use our OWN os.Pipe (not
	// cmd.StderrPipe) so cmd.Wait — launched concurrently on teardown — never
	// races the pump by closing the pipe out from under it (the cmd.StderrPipe
	// contract forbids Wait before reads complete) ([Review][Patch] P2). Must be
	// wired BEFORE Start.
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}
	cmd.Stderr = stderrW

	if err := cmd.Start(); err != nil {
		_ = stderrR.Close()
		_ = stderrW.Close()
		return fmt.Errorf("start process: %w", err)
	}
	// Close the parent's write end; the child holds its own dup. This lets the
	// pump's read end EOF when the child exits.
	_ = stderrW.Close()

	t.cmd = cmd
	t.lastChildPID.Store(int64(cmd.Process.Pid))
	t.stdin = json.NewEncoder(stdinPipe)
	sc := bufio.NewScanner(stdoutPipe)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024) // 4 MB max line
	t.stdout = sc

	// Single-reader pump for THIS cmd (Story 48.5). Fresh channel + done so a
	// reconnect's reader is fully isolated from the previous one.
	ch := make(chan readResult, 16)
	done := make(chan struct{})
	t.respCh = ch
	t.readDone = done
	go t.readLoop(sc, ch, done)

	// Spawn the stderr pump for THIS cmd. It exits on pipe EOF when the child
	// dies/Close runs, so it never outlives the process (Story 48.5 §易错点 9).
	go t.pumpStderr(stderrR)

	if err := t.initialize(ctx, handshakeTimeout(ctx)); err != nil {
		return fmt.Errorf("initialize handshake: %w", err)
	}
	return nil
}

// handshakeTimeout derives the initialize read budget from the connect ctx
// deadline (preserving the mount timeout). Zero = block indefinitely (no
// deadline set), matching the pre-48.5 unbounded handshake read.
func handshakeTimeout(ctx context.Context) time.Duration {
	dl, ok := ctx.Deadline()
	if !ok {
		return 0
	}
	d := time.Until(dl)
	if d <= 0 {
		d = time.Millisecond
	}
	return d
}

// readLoop is the SOLE scanner of a given stdout pipe. It pushes each response
// line onto ch and exits on EOF (child death) or when done is closed (teardown
// while a send is pending). Exactly one readLoop runs per cmd, so no two
// goroutines ever scan the same Scanner (Story 48.5 §易错点 9 — the race a
// per-call timeout goroutine would create).
func (t *StdioTransport) readLoop(sc *bufio.Scanner, ch chan readResult, done chan struct{}) {
	for sc.Scan() {
		b := sc.Bytes()
		cp := make([]byte, len(b))
		copy(cp, b)
		// Correlate responses by JSON-RPC id. Request ids start at 1 (nextID),
		// so a line with id==0 is a notification (notifications/*), a server log
		// line, or non-JSON garbage — drop it here so it never fills respCh and
		// stalls the pump, and so a caller can never misattribute it to a
		// request ([Review][Patch] P1).
		var probe jsonRPCResponse
		if err := json.Unmarshal(cp, &probe); err != nil || probe.ID == 0 {
			continue
		}
		select {
		case ch <- readResult{data: cp, id: probe.ID}:
		case <-done:
			return
		}
	}
	err := sc.Err()
	if err == nil {
		err = fmt.Errorf("unexpected EOF")
	}
	select {
	case ch <- readResult{err: err}:
	case <-done:
	}
}

// readResponseLocked consumes response lines from the reader pump until one
// matches wantID, returning its payload. Lines whose id does not match wantID
// are stale/out-of-order replies (e.g. a timed-out ping reply that lands after
// we moved on) and are discarded ([Review][Patch] P1). wantID==0 accepts the
// next line regardless (callers that do not correlate).
//
// timeout <= 0 means no internal timer; ctx cancellation always unblocks the
// wait so a hung child can never pin t.mu past the caller's ctx deadline
// ([Review][Patch] P6). A positive timeout additionally bounds the wait (L2
// ping / tools-list refresh). Caller MUST hold t.mu. On timeout/cancel the
// pending line stays queued in the pump (or is discarded when the transport
// reconnects and abandons the channel) — no goroutine is orphaned mid-Scan.
func (t *StdioTransport) readResponseLocked(ctx context.Context, wantID int64, timeout time.Duration) ([]byte, error) {
	ch := t.respCh
	if ch == nil {
		return nil, fmt.Errorf("no reader")
	}
	var timerC <-chan time.Time
	if timeout > 0 {
		tm := time.NewTimer(timeout)
		defer tm.Stop()
		timerC = tm.C
	}
	var ctxDone <-chan struct{}
	if ctx != nil {
		ctxDone = ctx.Done()
	}
	for {
		select {
		case r, ok := <-ch:
			if !ok {
				return nil, fmt.Errorf("reader closed")
			}
			if r.err != nil {
				return nil, r.err
			}
			if wantID != 0 && r.id != wantID {
				continue // stale / out-of-order reply — discard
			}
			return r.data, nil
		case <-timerC:
			return nil, fmt.Errorf("timeout after %v", timeout)
		case <-ctxDone:
			return nil, ctx.Err()
		}
	}
}

// stopReaderLocked releases the current reader pump (if any) so its goroutine
// exits even when blocked sending an unconsumed line. Caller MUST hold t.mu.
func (t *StdioTransport) stopReaderLocked() {
	if t.readDone != nil {
		close(t.readDone)
		t.readDone = nil
	}
	t.respCh = nil
}

// pumpStderr reads the child's stderr line-by-line into the shared ring buffer
// until EOF, then closes the read end. One goroutine per cmd; bounded by
// stderrRingCapacity. It uses bufio.Reader.ReadLine (not Scanner) so a single
// oversized line — e.g. a multi-MB panic stack dump — is truncated to
// stderrMaxLineBytes and the pump keeps scanning, instead of Scanner's
// ErrTooLong permanently halting capture and dropping every later line, which
// is exactly the diagnostic we most need on a crash ([Review][Patch] P5).
func (t *StdioTransport) pumpStderr(rc io.ReadCloser) {
	defer func() { _ = rc.Close() }()
	br := bufio.NewReaderSize(rc, 64*1024)
	var line []byte
	truncated := false
	for {
		chunk, isPrefix, err := br.ReadLine()
		if len(chunk) > 0 {
			if len(line) < stderrMaxLineBytes {
				line = append(line, chunk...)
			} else {
				truncated = true
			}
		}
		if !isPrefix && err == nil {
			if truncated {
				line = append(line, " ...[truncated]"...)
			}
			t.stderrBuf.Push(string(line))
			line = line[:0]
			truncated = false
		}
		if err != nil {
			if len(line) > 0 {
				t.stderrBuf.Push(string(line))
			}
			return
		}
	}
}

// Call sends a JSON-RPC 2.0 request and returns the result. Story 48.5 layers
// two opportunistic health checks ahead of the blocking stdio I/O:
//
//	L1 (every call, ≤1ms): is the child process alive? If not, fast-fail with
//	    ErrDeviceDisconnected instead of blocking on the 30s stdout scan (AC1).
//	L2 (only after >30s idle): ping the server (2s budget). On ping failure,
//	    mark Error and trigger the backoff reconnecter (AC2/AC3).
//
// A BackoffExhausted transport fast-fails immediately (AC3).
func (t *StdioTransport) Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Terminal state: every call fast-fails, never blocks (AC3).
	if vfs.MCPStatus(t.status.Load()) == vfs.MCPStatusBackoffExhausted {
		return nil, types.NewDriverError("Call", method,
			fmt.Errorf("mcp server unreachable: reconnect backoff exhausted"),
			types.ErrDeviceDisconnected)
	}

	if !t.connected {
		return nil, fmt.Errorf("not connected")
	}

	// L1 liveness (AC1): a local syscall, ≤1ms. A dead child short-circuits
	// before any blocking stdio.
	if !t.isChildAliveLocked() {
		t.status.Store(int32(vfs.MCPStatusError))
		return nil, types.NewDriverError("Call", method,
			fmt.Errorf("mcp child process not alive (pid %d)", t.lastChildPID.Load()),
			types.ErrDeviceDisconnected)
	}

	// L2 readiness (AC2): only after >30s idle. Zero overhead on the hot path.
	if last := t.lastCall.Load(); last != 0 {
		if nowFunc().Sub(time.Unix(0, last)) > l2IdleThreshold {
			if err := t.pingLocked(ctx, l2PingTimeout); err != nil {
				// Server unresponsive → mark + reconnect. A failed reconnect
				// leaves the transport in a non-Connected terminal state and the
				// call fast-fails.
				t.status.Store(int32(vfs.MCPStatusError))
				if rerr := t.reconnectLocked(ctx); rerr != nil {
					return nil, types.NewDriverError("Call", method,
						fmt.Errorf("mcp server unresponsive, reconnect failed: %w", rerr),
						types.ErrDeviceDisconnected)
				}
				// Reconnected: fall through to issue the original request on the
				// fresh stdin/stdout.
			} else {
				t.lastCheck.Store(nowFunc().UnixNano())
			}
		}
	}

	result, err := t.requestLocked(ctx, method, params)
	if err != nil {
		return nil, err
	}
	t.lastCall.Store(nowFunc().UnixNano())
	return result, nil
}

// requestLocked issues one JSON-RPC request/response round on the current
// stdin/stdout. Caller MUST hold t.mu and have validated connectivity. The
// stdout scan is unbounded by design — after L1+L2 the server is known
// responsive, and the existing 30s ctx budget (vfs.mcpCallTimeout) still
// governs the broader VFS Read path.
func (t *StdioTransport) requestLocked(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	id := t.nextID.Add(1)
	req := jsonRPCRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}

	if err := t.stdin.Encode(req); err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}

	// timeout 0: the request is unbounded by design (the server is known
	// responsive after L1+L2), but ctx cancellation (the VFS mcpCallTimeout)
	// still unblocks the wait so a server that hangs AFTER ping-OK cannot pin
	// t.mu and stall every concurrent Call ([Review][Patch] P6).
	line, err := t.readResponseLocked(ctx, id, 0)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var resp jsonRPCResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("rpc error %d: %s", resp.Error.Code, resp.Error.Message)
	}
	return resp.Result, nil
}

// pingLocked sends a JSON-RPC ping and waits up to timeout for any response,
// bounding the wait so a hung-but-alive child cannot stall the call. Caller
// MUST hold t.mu.
func (t *StdioTransport) pingLocked(ctx context.Context, timeout time.Duration) error {
	id := t.nextID.Add(1)
	req := jsonRPCRequest{JSONRPC: "2.0", ID: id, Method: "ping"}
	if err := t.stdin.Encode(req); err != nil {
		return fmt.Errorf("send ping: %w", err)
	}
	if _, err := t.readResponseLocked(ctx, id, timeout); err != nil {
		return fmt.Errorf("ping: %w", err)
	}
	return nil
}

// refreshCountsLocked re-reads tools/list + resources/list and caches the
// counts (Story 48.5 AC5). Best-effort on the initial Connect; the reconnect
// path uses refreshToolsLocked which propagates the tools/list error so a hung
// server fails the reconnect attempt. Caller MUST hold t.mu.
func (t *StdioTransport) refreshCountsLocked(ctx context.Context) {
	_ = t.refreshToolsLocked(ctx)
}

// refreshToolsLocked issues tools/list (mandatory) + resources/list (best
// effort) with a bounded read, caching the counts. Returns the tools/list
// error so reconnect can treat an unresponsive child as a failed attempt.
func (t *StdioTransport) refreshToolsLocked(ctx context.Context) error {
	tid := t.nextID.Add(1)
	req := jsonRPCRequest{JSONRPC: "2.0", ID: tid, Method: "tools/list"}
	if err := t.stdin.Encode(req); err != nil {
		return fmt.Errorf("tools/list send: %w", err)
	}
	line, err := t.readResponseLocked(ctx, tid, l2PingTimeout)
	if err != nil {
		return fmt.Errorf("tools/list: %w", err)
	}
	t.toolCount.Store(int64(countResultArray(line, "tools")))

	// resources/list — best effort; a server without resources still reconnects.
	rid := t.nextID.Add(1)
	rreq := jsonRPCRequest{JSONRPC: "2.0", ID: rid, Method: "resources/list"}
	if err := t.stdin.Encode(rreq); err == nil {
		if rline, rerr := t.readResponseLocked(ctx, rid, l2PingTimeout); rerr == nil {
			t.resourceCount.Store(int64(countResultArray(rline, "resources")))
		}
	}
	return nil
}

// countResultArray parses a JSON-RPC response line and returns len(result.<field>).
// Returns 0 on any parse miss (the field may be absent for minimal servers).
func countResultArray(line []byte, field string) int {
	var resp jsonRPCResponse
	if err := json.Unmarshal(line, &resp); err != nil || len(resp.Result) == 0 {
		return 0
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(resp.Result, &m); err != nil {
		return 0
	}
	raw, ok := m[field]
	if !ok {
		return 0
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return 0
	}
	return len(items)
}

// isChildAliveLocked reports whether the most-recently-Started child is alive.
// Caller MUST hold t.mu (it reads lastChildPID, which reconnect mutates).
func (t *StdioTransport) isChildAliveLocked() bool {
	return isChildAlive(int(t.lastChildPID.Load()))
}

// Close terminates the MCP server process and cleans up. Story 48.2 rewrite:
// two-phase SIGTERM → 5s wait → SIGKILL escalation against the entire process
// group; idempotent (subsequent calls return nil); does NOT hold t.mu while
// waiting for cmd.Wait so concurrent Call/Read/Write are not blocked.
//
// Return value:
//   - nil on graceful path (SIGTERM honoured within gracefulShutdownTimeout)
//     OR when the transport was never Connected / already Closed
//   - *types.DriverError{Code: types.ErrForceKilled} when SIGKILL escalation
//     was required (Story 48.2 AC4)
func (t *StdioTransport) Close() error {
	t.mu.Lock()
	// AC3 idempotence: any subsequent Close short-circuits.
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	err := t.closeProcessLocked()
	t.mu.Unlock()
	return err
}

// closeProcessLocked performs the shared cleanup body for Close (Story 48.2
// AC1, AC3, AC4, AC8) AND for Connect's failure-rollback path (AC5).
//
// Contract:
//   - Caller MUST hold t.mu. The function releases t.mu around the 5s graceful
//     wait so concurrent Call/Read/Write are not blocked (Story 48.2 易错点 #6),
//     and re-acquires before returning. Caller observing t.closed=true after
//     the call is the success signal.
//   - Sets t.closed = true and t.connected = false up front so any racing
//     Close call short-circuits (易错点 #5).
//   - Returns nil on graceful path or no-process path; *types.DriverError
//     when SIGKILL escalation occurred.
func (t *StdioTransport) closeProcessLocked() error {
	t.closed = true
	t.connected = false
	t.status.Store(int32(vfs.MCPStatusDisconnected))

	cmd := t.cmd
	t.cmd = nil
	t.stdin = nil
	t.stdout = nil
	t.stopReaderLocked() // release the reader pump goroutine

	// AC8 zero-overhead: nothing to clean up if we never Started a process.
	// Note: lastChildPID is intentionally NOT cleared here — it is a post-mortem
	// observation field used by AC5 tests to verify process-group cleanup AFTER
	// t.cmd has been niled out (see transport_close_test.go:314). Clearing it
	// would break that assertion; PID reuse race is mitigated by the AC5 test
	// reading it within 1s of Connect failure (test:323-330).
	if cmd == nil || cmd.Process == nil {
		return nil
	}

	// Best-effort SIGTERM to the entire process group. The platform helper
	// translates ESRCH (group already gone) into a nil return so we keep
	// going into the wait path. On Windows this is a no-op (no graceful
	// equivalent), so we proceed straight to the SIGKILL escalation when the
	// 5s wait expires.
	if err := sendGroupSIGTERM(cmd); err != nil {
		// Fall back to single-process SIGTERM if the platform rejected the
		// group-wide signal (e.g., EPERM in restricted namespaces). Preserve
		// the graceful semantics by sending SIGTERM (not SIGKILL — earlier
		// code used cmd.Process.Kill() here, which is SIGKILL on Unix and
		// collapsed the 5s window down to zero, defeating the whole purpose
		// of the two-phase escalation. Code review F2, 2026-05-28).
		_ = cmd.Process.Signal(syscall.SIGTERM)
	}

	// Story 48.2 易错点 #4: launch the Wait goroutine BEFORE entering the
	// timeout select. If we lazily started it inside the timeout branch we'd
	// risk missing a SIGTERM-honoured exit that lands in the gap.
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	// Story 48.2 易错点 #6: release t.mu while waiting up to 5s so Call etc.
	// don't block. The closed=true flag set above guarantees any racing Close
	// caller short-circuits before reaching this function.
	t.mu.Unlock()
	defer t.mu.Lock()

	start := time.Now()
	select {
	case <-done:
		// Graceful path — child honoured SIGTERM (or had already exited).
		return nil
	case <-time.After(gracefulShutdownTimeout):
		// AC4 escalation path. Per Story §易错点 #11: write a single log line
		// to daemon stderr (48.5 owns ring-buffered stderr capture).
		elapsed := time.Since(start)
		log.Printf("[mcp] transport %q ignored SIGTERM for %v, escalating to SIGKILL", t.config.Command, elapsed)

		// SIGKILL the whole process group. The platform helper already
		// swallows ESRCH (process group exited between timeout and now) and
		// performs the leader-only fallback if pgid signalling is rejected.
		if err := sendGroupSIGKILL(cmd); err != nil {
			// Non-fatal: we already raised SIGKILL above; log for visibility.
			log.Printf("[mcp] transport %q SIGKILL returned: %v", t.config.Command, err)
		}

		// Story 48.2 易错点 #6 continued: must <-done after SIGKILL to release
		// the Wait goroutine's fds; SIGKILL on a child completes in <100ms in
		// practice. We do NOT hard-bound this wait — runaway here implies a
		// kernel bug, and adding a watchdog would mask it.
		<-done

		return types.NewDriverError("Close", "",
			fmt.Errorf("SIGTERM ignored after %v, force-killed", gracefulShutdownTimeout),
			types.ErrForceKilled)
	}
}

// teardownCurrentProcessLocked kills the CURRENT child (SIGKILL the group) and
// reaps it asynchronously, WITHOUT marking the transport closed — the reconnect
// path uses it between attempts. Caller MUST hold t.mu. Unlike
// closeProcessLocked it does not wait the 5s graceful window: a reconnect is
// already a crash-recovery path, so we want the dead/hung child gone fast.
func (t *StdioTransport) teardownCurrentProcessLocked() {
	cmd := t.cmd
	t.cmd = nil
	t.stdin = nil
	t.stdout = nil
	t.connected = false
	t.stopReaderLocked() // release the previous reader pump before re-exec
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = sendGroupSIGKILL(cmd)
	// Reap asynchronously so we don't block under t.mu. SIGKILL completes fast;
	// the goroutine exits as soon as Wait returns, and the child's stderr pipe
	// EOFs, ending that pump goroutine too.
	go func() { _ = cmd.Wait() }()
}

// Ping checks if the MCP server is responsive by sending a JSON-RPC ping request.
func (t *StdioTransport) Ping(ctx context.Context) error {
	_, err := t.Call(ctx, "ping", nil)
	if err != nil {
		return fmt.Errorf("ping: %w", err)
	}
	return nil
}

// initialize performs the MCP protocol initialize handshake.
// Sends initialize request, validates response, then sends initialized notification.
// Must be called with mu held.
func (t *StdioTransport) initialize(ctx context.Context, timeout time.Duration) error {
	initParams := map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]string{
			"name":    "rnix",
			"version": "1.0.0",
		},
	}
	paramsJSON, err := json.Marshal(initParams)
	if err != nil {
		return fmt.Errorf("marshal init params: %w", err)
	}

	id := t.nextID.Add(1)
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  "initialize",
		Params:  paramsJSON,
	}

	if err := t.stdin.Encode(req); err != nil {
		return fmt.Errorf("send initialize: %w", err)
	}

	// Read initialize response via the reader pump (bounded by the connect ctx
	// deadline, if any). wantID==0: the handshake is a serialized first exchange
	// on a freshly started child — no prior in-flight request can have left a
	// stale reply, and readLoop already dropped notifications — so we accept the
	// first response line regardless of id. (Strict id correlation still applies
	// to every steady-state request/ping/refresh; ditto a reconnect, whose
	// nextID has advanced past the value a minimal server may echo for init.)
	line, err := t.readResponseLocked(ctx, 0, timeout)
	if err != nil {
		return fmt.Errorf("read initialize response: %w", err)
	}

	var resp jsonRPCResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		return fmt.Errorf("parse initialize response: %w", err)
	}

	if resp.Error != nil {
		return fmt.Errorf("initialize rejected: %d %s", resp.Error.Code, resp.Error.Message)
	}

	// Send initialized notification (no ID, no response expected)
	notif := struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
	}{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}
	if err := t.stdin.Encode(notif); err != nil {
		return fmt.Errorf("send initialized notification: %w", err)
	}

	return nil
}

// --- Story 48.5 read-only health/status surface (vfs.MCPTransport) ---

// Status returns the authoritative transport status.
func (t *StdioTransport) Status() vfs.MCPStatus { return vfs.MCPStatus(t.status.Load()) }

// Alive performs an L1 liveness probe of the current child.
func (t *StdioTransport) Alive() bool {
	return isChildAlive(int(t.lastChildPID.Load()))
}

// ToolCount returns the cached tools/list count.
func (t *StdioTransport) ToolCount() int { return int(t.toolCount.Load()) }

// ResourceCount returns the cached resources/list count.
func (t *StdioTransport) ResourceCount() int { return int(t.resourceCount.Load()) }

// LastCheck returns the time of the last successful L2 ping (zero = never).
func (t *StdioTransport) LastCheck() time.Time {
	ns := t.lastCheck.Load()
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}

// ReconnectCount returns the monotonic successful-reconnect counter.
func (t *StdioTransport) ReconnectCount() int { return int(t.reconnectCount.Load()) }

// StderrTail returns the captured child stderr (oldest→newest).
func (t *StdioTransport) StderrTail() []string { return t.stderrBuf.Snapshot() }
