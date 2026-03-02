package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sync"
	"sync/atomic"
)

// TransportConfig holds configuration for creating a StdioTransport.
type TransportConfig struct {
	Command       string
	Args          []string
	Env           []string
	TimeoutMillis int64
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
	nextID    atomic.Int64
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
	t.stdin = json.NewEncoder(stdinPipe)
	t.stdout = bufio.NewScanner(stdoutPipe)
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

// Close terminates the MCP server process and cleans up.
func (t *StdioTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.connected = false

	if t.cmd == nil || t.cmd.Process == nil {
		return nil
	}

	// Kill the process
	_ = t.cmd.Process.Kill()
	_ = t.cmd.Wait()
	return nil
}

// Ping checks if the MCP server is responsive.
func (t *StdioTransport) Ping(ctx context.Context) error {
	t.mu.Lock()
	if !t.connected {
		t.mu.Unlock()
		return fmt.Errorf("not connected")
	}
	t.mu.Unlock()

	// Check if process is still alive
	if t.cmd == nil || t.cmd.Process == nil {
		return fmt.Errorf("process not running")
	}

	// Use context deadline for timeout
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
