package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// gracefulShutdownTimeout is the SIGTERM-to-SIGKILL escalation budget for
// transport.Close (Story 48.2 AC4). Kept package-level (not configurable) by
// design — Story 48.6 introduces per-server timeout configuration.
const gracefulShutdownTimeout = 5 * time.Second

// TransportConfig holds configuration for creating a StdioTransport.
type TransportConfig struct {
	Command       string
	Args          []string
	Env           []string
	TimeoutMillis int64
	WorkDir       string
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
	// PID 0 means "never Started a child".
	lastChildPID atomic.Int64
}

// NewStdioTransport creates a new StdioTransport with the given configuration.
func NewStdioTransport(config TransportConfig) *StdioTransport {
	return &StdioTransport{
		config: config,
	}
}

// Connect starts the MCP server process and performs the initialize handshake.
func (t *StdioTransport) Connect(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.config.Command == "" {
		return fmt.Errorf("empty command")
	}

	cmd := exec.CommandContext(ctx, t.config.Command, t.config.Args...)
	if len(t.config.Env) > 0 {
		cmd.Env = t.config.Env
	}
	if t.config.WorkDir != "" {
		cmd.Dir = t.config.WorkDir
	}

	// Story 48.2 Task 4.1: apply Setpgid (+ Pdeathsig on Linux) before Start
	// so the kernel honors it at fork time. Without this, syscall.Kill(-pgid,
	// ...) in Close cannot reach the subprocess tree.
	applyProcessGroupIsolation(cmd)

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start process: %w", err)
	}

	t.cmd = cmd
	t.lastChildPID.Store(int64(cmd.Process.Pid))
	t.stdin = json.NewEncoder(stdinPipe)
	t.stdout = bufio.NewScanner(stdoutPipe)
	t.stdout.Buffer(make([]byte, 0, 64*1024), 4*1024*1024) // 4 MB max line

	// Perform MCP initialize handshake (JSON-RPC initialize + initialized notification)
	if err := t.initialize(); err != nil {
		// Story 48.2 Task 2.4: reuse closeProcess so Connect failure also runs
		// the process-group SIGTERM → SIGKILL cleanup (instead of single-PID
		// Kill that leaks orphan grandchildren).
		_ = t.closeProcessLocked()
		return fmt.Errorf("initialize handshake: %w", err)
	}

	t.connected = true
	return nil
}

// Call sends a JSON-RPC 2.0 request and returns the result.
func (t *StdioTransport) Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.connected {
		return nil, fmt.Errorf("not connected")
	}

	id := t.nextID.Add(1)
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	if err := t.stdin.Encode(req); err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}

	// Read response
	if !t.stdout.Scan() {
		err := t.stdout.Err()
		if err == nil {
			err = fmt.Errorf("unexpected EOF")
		}
		return nil, fmt.Errorf("read response: %w", err)
	}

	var resp jsonRPCResponse
	if err := json.Unmarshal(t.stdout.Bytes(), &resp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("rpc error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	return resp.Result, nil
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

	cmd := t.cmd
	t.cmd = nil
	t.stdin = nil
	t.stdout = nil

	// AC8 zero-overhead: nothing to clean up if we never Started a process.
	if cmd == nil || cmd.Process == nil {
		return nil
	}

	// Best-effort SIGTERM to the entire process group. The platform helper
	// translates ESRCH (group already gone) into a nil return so we keep
	// going into the wait path. On Windows this is a no-op (no graceful
	// equivalent), so we proceed straight to the SIGKILL escalation when the
	// 5s wait expires.
	if err := sendGroupSIGTERM(cmd); err != nil {
		// Fall back to single-process Kill if the platform rejected the
		// group-wide signal — continue into the wait path either way.
		_ = cmd.Process.Kill()
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
func (t *StdioTransport) initialize() error {
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

	// Read initialize response
	if !t.stdout.Scan() {
		scanErr := t.stdout.Err()
		if scanErr == nil {
			scanErr = fmt.Errorf("unexpected EOF")
		}
		return fmt.Errorf("read initialize response: %w", scanErr)
	}

	var resp jsonRPCResponse
	if err := json.Unmarshal(t.stdout.Bytes(), &resp); err != nil {
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
