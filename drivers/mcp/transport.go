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
	if t.config.WorkDir != "" {
		cmd.Dir = t.config.WorkDir
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
	t.stdout.Buffer(make([]byte, 0, 64*1024), 4*1024*1024) // 4 MB max line

	// Perform MCP initialize handshake (JSON-RPC initialize + initialized notification)
	if err := t.initialize(); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.cmd = nil
		t.stdin = nil
		t.stdout = nil
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
