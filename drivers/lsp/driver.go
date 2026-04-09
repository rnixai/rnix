// Package lsp implements the /dev/lsp VFS device for LSP-based code intelligence.
package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

const (
	// defaultLSPCommand is the default LSP server command.
	defaultLSPCommand = "gopls"
)

// lspOperation maps operation names to LSP methods.
var lspOperations = map[string]string{
	"goToDefinition":       "textDocument/definition",
	"findReferences":       "textDocument/references",
	"hover":                "textDocument/hover",
	"documentSymbol":       "textDocument/documentSymbol",
	"workspaceSymbol":      "workspace/symbol",
	"goToImplementation":   "textDocument/implementation",
	"prepareCallHierarchy": "textDocument/prepareCallHierarchy",
	"incomingCalls":        "callHierarchy/incomingCalls",
	"outgoingCalls":        "callHierarchy/outgoingCalls",
}

// LspDriver manages LSP server connections and operations.
type LspDriver struct {
	mu      sync.Mutex
	clients map[string]*Client // workDir → client
	command string
}

// compile-time interface check
var _ vfs.ToolDescriptor = (*LspDriver)(nil)

// ToolDefs returns the tool definitions for the LSP device.
func (d *LspDriver) ToolDefs() []vfs.ToolDef {
	return []vfs.ToolDef{
		{
			Name:              "lsp",
			Description:       loadPrompt("lsp"),
			IsReadOnly:        true,
			IsConcurrencySafe: true,
			IsDestructive:     false,
			ShouldDefer:       true,
			SearchHint:        "lsp code definition references symbol",
			MaxResultTokens:   100000,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"operation": map[string]any{
						"type": "string",
						"enum": []string{
							"goToDefinition", "findReferences", "hover",
							"documentSymbol", "workspaceSymbol",
							"goToImplementation", "prepareCallHierarchy",
							"incomingCalls", "outgoingCalls",
						},
						"description": "The LSP operation to perform",
					},
					"filePath": map[string]any{
						"type":        "string",
						"description": "File path relative to the working directory",
					},
					"line": map[string]any{
						"type":        "integer",
						"description": "Line number (1-based)",
					},
					"character": map[string]any{
						"type":        "integer",
						"description": "Column number (1-based)",
					},
					"query": map[string]any{
						"type":        "string",
						"description": "Search query for workspaceSymbol",
					},
					"item": map[string]any{
						"type":        "object",
						"description": "Call hierarchy item from prepareCallHierarchy",
					},
				},
				"required": []string{"operation"},
			},
		},
	}
}

// NewDriver creates an LspDriver with the default LSP command.
func NewDriver() *LspDriver {
	command := os.Getenv("RNIX_LSP_COMMAND")
	if command == "" {
		command = defaultLSPCommand
	}
	return &LspDriver{
		clients: make(map[string]*Client),
		command: command,
	}
}

// NewDriverWithCommand creates an LspDriver with a specific command.
func NewDriverWithCommand(command string) *LspDriver {
	if command == "" {
		command = defaultLSPCommand
	}
	return &LspDriver{
		clients: make(map[string]*Client),
		command: command,
	}
}

// getOrCreateClient returns an existing client for workDir or creates a new one.
func (d *LspDriver) getOrCreateClient(ctx context.Context, workDir string) (*Client, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if client, ok := d.clients[workDir]; ok {
		// Check if client is still alive by probing readDone channel.
		select {
		case <-client.readDone:
			// Client is dead — remove and fall through to create a new one.
			delete(d.clients, workDir)
		default:
			return client, nil
		}
	}

	client := NewClient(d.command, nil, workDir)
	if err := client.EnsureInitialized(ctx); err != nil {
		return nil, err
	}

	d.clients[workDir] = client
	return client, nil
}

// lspRequest is the JSON input for an LSP operation.
type lspRequest struct {
	Operation string          `json:"operation"`
	FilePath  string          `json:"filePath"`
	Line      int             `json:"line"`
	Character int             `json:"character"`
	Query     string          `json:"query"`
	Item      json.RawMessage `json:"item"`
}

// execute runs an LSP operation and returns the result as JSON.
func (d *LspDriver) execute(ctx context.Context, workDir string, req lspRequest) (string, error) {
	method, ok := lspOperations[req.Operation]
	if !ok {
		return "", &types.DriverError{
			Op:     "Write",
			Device: "/dev/lsp",
			Err:    fmt.Errorf("unknown operation: %s", req.Operation),
			Code:   types.ErrInvalid,
		}
	}

	client, err := d.getOrCreateClient(ctx, workDir)
	if err != nil {
		return "", &types.DriverError{
			Op:     "Write",
			Device: "/dev/lsp",
			Err:    fmt.Errorf("LSP server not available: %w", err),
			Code:   types.ErrServiceUnavailable,
		}
	}

	params, err := d.buildParams(req, method, workDir)
	if err != nil {
		return "", err
	}

	result, err := client.Call(ctx, method, params)
	if err != nil {
		return "", &types.DriverError{
			Op:     "Write",
			Device: "/dev/lsp",
			Err:    fmt.Errorf("%s failed: %w", req.Operation, err),
			Code:   types.ErrDriver,
		}
	}

	// Format result as indented JSON
	var formatted json.RawMessage
	if err := json.Unmarshal(result, &formatted); err != nil {
		return string(result), nil
	}
	out, _ := json.MarshalIndent(formatted, "", "  ")
	return string(out), nil
}

// buildParams constructs LSP method parameters from the request.
func (d *LspDriver) buildParams(req lspRequest, method string, workDir string) (any, error) {
	switch method {
	case "workspace/symbol":
		if req.Query == "" {
			return nil, &types.DriverError{
				Op:     "Write",
				Device: "/dev/lsp",
				Err:    fmt.Errorf("workspaceSymbol requires 'query' parameter"),
				Code:   types.ErrInvalid,
			}
		}
		return map[string]any{"query": req.Query}, nil

	case "callHierarchy/incomingCalls", "callHierarchy/outgoingCalls":
		if len(req.Item) == 0 || string(req.Item) == "null" {
			return nil, &types.DriverError{
				Op:     "Write",
				Device: "/dev/lsp",
				Err:    fmt.Errorf("%s requires 'item' parameter from prepareCallHierarchy", req.Operation),
				Code:   types.ErrInvalid,
			}
		}
		return map[string]any{"item": req.Item}, nil

	default:
		// Position-based operations
		if req.FilePath == "" {
			return nil, &types.DriverError{
				Op:     "Write",
				Device: "/dev/lsp",
				Err:    fmt.Errorf("%s requires 'filePath' parameter", req.Operation),
				Code:   types.ErrInvalid,
			}
		}

		absPath := req.FilePath
		if !filepath.IsAbs(absPath) {
			absPath = filepath.Join(workDir, req.FilePath)
		}
		absPath = filepath.Clean(absPath)
		cleanWork := filepath.Clean(workDir)
		if absPath != cleanWork && !strings.HasPrefix(absPath, cleanWork+string(filepath.Separator)) {
			return nil, &types.DriverError{
				Op:     "Write",
				Device: "/dev/lsp",
				Err:    fmt.Errorf("filePath escapes working directory: %s", req.FilePath),
				Code:   types.ErrPermission,
			}
		}
		uri := "file://" + absPath

		params := map[string]any{
			"textDocument": map[string]any{"uri": uri},
		}

		// documentSymbol doesn't need position
		if method != "textDocument/documentSymbol" {
			if req.Line <= 0 || req.Character <= 0 {
				return nil, &types.DriverError{
					Op:     "Write",
					Device: "/dev/lsp",
					Err:    fmt.Errorf("%s requires 'line' and 'character' parameters (1-based)", req.Operation),
					Code:   types.ErrInvalid,
				}
			}
			// Convert 1-based to 0-based for LSP protocol
			params["position"] = map[string]any{
				"line":      req.Line - 1,
				"character": req.Character - 1,
			}
		}

		// findReferences needs context
		if method == "textDocument/references" {
			params["context"] = map[string]any{"includeDeclaration": true}
		}

		return params, nil
	}
}

// CloseAll shuts down all LSP client connections.
func (d *LspDriver) CloseAll() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for dir, client := range d.clients {
		client.Close()
		delete(d.clients, dir)
	}
}

// LspFile implements vfs.VFSFile for LSP operations via write-then-read semantics.
type LspFile struct {
	driver     *LspDriver
	devicePath string
	workDir    string
	response   []byte
	offset     int
	closed     bool
}

// Write accepts a JSON request, dispatches to the appropriate LSP operation.
func (f *LspFile) Write(ctx context.Context, data []byte) error {
	if f.closed {
		return &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("lsp file closed"), Code: types.ErrDriver}
	}

	var req lspRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("invalid JSON input: %w", err), Code: types.ErrInvalid}
	}

	if req.Operation == "" {
		return &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("'operation' is required"), Code: types.ErrInvalid}
	}

	result, err := f.driver.execute(ctx, f.workDir, req)
	if err != nil {
		return err
	}

	f.response = []byte(result)
	f.offset = 0
	return nil
}

// Read returns buffered LSP response data up to the requested length.
func (f *LspFile) Read(length int) ([]byte, error) {
	if f.closed {
		return nil, &types.DriverError{Op: "Read", Device: f.devicePath, Err: fmt.Errorf("lsp file closed"), Code: types.ErrDriver}
	}
	if f.response == nil {
		return nil, &types.DriverError{Op: "Read", Device: f.devicePath, Err: fmt.Errorf("no data available: write a request first"), Code: types.ErrDriver}
	}

	remaining := f.response[f.offset:]
	if len(remaining) == 0 {
		return nil, nil
	}

	if length <= 0 || length > len(remaining) {
		length = len(remaining)
	}

	out := make([]byte, length)
	copy(out, remaining[:length])
	f.offset += length
	return out, nil
}

// Close marks the file as closed and releases buffers.
func (f *LspFile) Close() error {
	if f.closed {
		return &types.DriverError{Op: "Close", Device: f.devicePath, Err: fmt.Errorf("lsp file already closed"), Code: types.ErrDriver}
	}
	f.closed = true
	f.response = nil
	f.offset = 0
	return nil
}

// Stat returns metadata about this LSP device file.
func (f *LspFile) Stat() (vfs.FileStat, error) {
	if f.closed {
		return vfs.FileStat{}, &types.DriverError{Op: "Stat", Device: f.devicePath, Err: fmt.Errorf("lsp file closed"), Code: types.ErrDriver}
	}
	return vfs.FileStat{
		Name:       f.devicePath,
		Size:       int64(len(f.response)),
		IsDevice:   true,
		DevicePath: "/dev/lsp",
	}, nil
}

// FileFactory returns a VFSFileFactory that creates LspFile instances.
func FileFactory(driver *LspDriver) vfs.VFSFileFactory {
	return func(subpath string, flags vfs.OpenFlag, workDir string) (vfs.VFSFile, error) {
		return &LspFile{
			driver:     driver,
			devicePath: "/dev/lsp" + subpath,
			workDir:    workDir,
		}, nil
	}
}
