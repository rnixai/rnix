// Package lsp implements the LSP JSON-RPC 2.0 stdio client.
package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// jsonRPCRequest is a JSON-RPC 2.0 request.
type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// jsonRPCResponse is a JSON-RPC 2.0 response.
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

// jsonRPCError is a JSON-RPC 2.0 error.
type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *jsonRPCError) Error() string {
	return fmt.Sprintf("LSP error %d: %s", e.Code, e.Message)
}

// CmdStarter abstracts process creation for testability.
type CmdStarter func(ctx context.Context, name string, args ...string) *exec.Cmd

// Client communicates with an LSP server over stdio.
type Client struct {
	cmdStarter CmdStarter
	command    string
	args       []string
	workDir    string

	mu          sync.Mutex
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	stdout      *bufio.Reader
	initialized bool
	nextID      atomic.Int64

	// pending tracks in-flight requests
	pendingMu sync.Mutex
	pending   map[int64]chan *jsonRPCResponse

	readDone chan struct{}
}

// NewClient creates a new LSP client.
func NewClient(command string, args []string, workDir string) *Client {
	return &Client{
		cmdStarter: defaultCmdStarter,
		command:    command,
		args:       args,
		workDir:    workDir,
		pending:    make(map[int64]chan *jsonRPCResponse),
	}
}

// NewClientWithStarter creates a new LSP client with a custom command starter (for testing).
func NewClientWithStarter(starter CmdStarter, command string, args []string, workDir string) *Client {
	return &Client{
		cmdStarter: starter,
		command:    command,
		args:       args,
		workDir:    workDir,
		pending:    make(map[int64]chan *jsonRPCResponse),
	}
}

func defaultCmdStarter(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}

// EnsureInitialized starts the LSP server and performs the initialize handshake.
func (c *Client) EnsureInitialized(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.initialized {
		return nil
	}

	cmd := c.cmdStarter(ctx, c.command, c.args...)
	cmd.Dir = c.workDir

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("creating stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("creating stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting LSP server %q: %w", c.command, err)
	}

	c.cmd = cmd
	c.stdin = stdin
	c.stdout = bufio.NewReader(stdout)
	c.readDone = make(chan struct{})

	// Start reader goroutine
	go c.readLoop()

	// Send initialize request
	initParams := map[string]any{
		"processId": nil,
		"capabilities": map[string]any{
			"textDocument": map[string]any{
				"definition":        map[string]any{"dynamicRegistration": false},
				"references":        map[string]any{"dynamicRegistration": false},
				"hover":             map[string]any{"dynamicRegistration": false},
				"documentSymbol":    map[string]any{"dynamicRegistration": false},
				"implementation":    map[string]any{"dynamicRegistration": false},
				"callHierarchy":     map[string]any{"dynamicRegistration": false},
			},
			"workspace": map[string]any{
				"symbol": map[string]any{"dynamicRegistration": false},
			},
		},
		"rootUri": "file://" + c.workDir,
	}

	_, err = c.callLocked(ctx, "initialize", initParams)
	if err != nil {
		_ = c.shutdown()
		return fmt.Errorf("LSP initialize failed: %w", err)
	}

	// Send initialized notification
	if err := c.notify("initialized", map[string]any{}); err != nil {
		_ = c.shutdown()
		return fmt.Errorf("LSP initialized notification failed: %w", err)
	}

	c.initialized = true
	return nil
}

// Call sends a JSON-RPC request and waits for the response.
func (c *Client) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	if !c.initialized {
		c.mu.Unlock()
		return nil, fmt.Errorf("LSP client not initialized")
	}
	result, err := c.callLocked(ctx, method, params)
	c.mu.Unlock()
	return result, err
}

// callLocked sends a request while the mutex is held.
func (c *Client) callLocked(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.nextID.Add(1)

	ch := make(chan *jsonRPCResponse, 1)
	c.pendingMu.Lock()
	c.pending[id] = ch
	c.pendingMu.Unlock()

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	if err := c.writeMessage(req); err != nil {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return nil, err
	}

	// Wait for response
	select {
	case resp := <-ch:
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	case <-ctx.Done():
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return nil, ctx.Err()
	case <-c.readDone:
		return nil, fmt.Errorf("LSP server connection closed")
	}
}

// notify sends a JSON-RPC notification (no response expected).
func (c *Client) notify(method string, params any) error {
	msg := struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
		Params  any    `json:"params,omitempty"`
	}{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	return c.writeMessage(msg)
}

// writeMessage encodes and sends an LSP message with Content-Length header.
func (c *Client) writeMessage(msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshaling message: %w", err)
	}
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	if _, err := io.WriteString(c.stdin, header); err != nil {
		return fmt.Errorf("writing header: %w", err)
	}
	if _, err := c.stdin.Write(data); err != nil {
		return fmt.Errorf("writing body: %w", err)
	}
	return nil
}

// readLoop continuously reads LSP messages and dispatches responses.
func (c *Client) readLoop() {
	defer close(c.readDone)
	for {
		data, err := c.readMessage()
		if err != nil {
			return
		}

		var resp jsonRPCResponse
		if err := json.Unmarshal(data, &resp); err != nil {
			continue // skip malformed messages
		}

		if resp.ID == 0 && resp.Result == nil && resp.Error == nil {
			continue // notification from server, ignore
		}

		c.pendingMu.Lock()
		ch, ok := c.pending[resp.ID]
		if ok {
			delete(c.pending, resp.ID)
		}
		c.pendingMu.Unlock()

		if ok {
			ch <- &resp
		}
	}
}

// readMessage reads a single LSP message (Content-Length header + body).
func (c *Client) readMessage() ([]byte, error) {
	contentLength := -1
	for {
		line, err := c.stdout.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break // end of headers
		}
		if val, ok := strings.CutPrefix(line, "Content-Length:"); ok {
			n, err := strconv.Atoi(strings.TrimSpace(val))
			if err == nil {
				contentLength = n
			}
		}
	}

	if contentLength <= 0 {
		return nil, fmt.Errorf("missing or invalid Content-Length")
	}

	body := make([]byte, contentLength)
	if _, err := io.ReadFull(c.stdout, body); err != nil {
		return nil, err
	}
	return body, nil
}

// Close shuts down the LSP server.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.shutdown()
}

func (c *Client) shutdown() error {
	if c.cmd == nil {
		return nil
	}
	if c.stdin != nil {
		c.stdin.Close()
	}
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	_ = c.cmd.Wait()
	c.cmd = nil
	c.initialized = false
	return nil
}
