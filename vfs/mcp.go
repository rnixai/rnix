package vfs

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gonewx/crux/internal/types"
)

// MCPConfig describes how to connect to an MCP server.
type MCPConfig struct {
	ServerName    string            `json:"server_name" yaml:"server_name"`
	Command       string            `json:"command" yaml:"command"`
	Args          []string          `json:"args,omitempty" yaml:"args,omitempty"`
	Env           map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
	TransportType string            `json:"transport_type" yaml:"transport_type"` // "stdio" (default)
}

// MCPTransport defines the interface for communicating with an MCP server.
// Implementations live in drivers/mcp; the interface is defined here to
// avoid a vfs -> drivers dependency (dependency inversion).
type MCPTransport interface {
	Connect(ctx context.Context) error
	Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error)
	Close() error
	Ping(ctx context.Context) error
}

// TransportFactory creates an MCPTransport from an MCPConfig.
type TransportFactory func(config MCPConfig) (MCPTransport, error)

// MCPStatus represents the connection status of an MCP mount.
type MCPStatus int

const (
	MCPStatusConnected    MCPStatus = iota
	MCPStatusDisconnected
	MCPStatusError
)

// MCPMount holds the state of a mounted MCP server.
type MCPMount struct {
	Path      string
	Config    MCPConfig
	Status    MCPStatus
	transport MCPTransport
}

// mcpFile implements VFSFile for MCP tool invocations.
// Each Open on an MCP path creates one mcpFile instance.
// Write sends a tools/call request; Read returns the result.
type mcpFile struct {
	subpath   string       // e.g. "/tools/create-issue"
	transport MCPTransport // shared transport for the mount
	response  []byte       // buffered response from Write
	closed    bool
	writeErr  error // error from Write, surfaced on Read
}

// newMCPFile creates a new mcpFile for the given subpath and transport.
func newMCPFile(subpath string, transport MCPTransport) *mcpFile {
	return &mcpFile{
		subpath:   subpath,
		transport: transport,
	}
}

// Write sends a tools/call request to the MCP server.
// data contains the JSON arguments for the tool call.
func (f *mcpFile) Write(ctx context.Context, data []byte) error {
	if f.closed {
		return fmt.Errorf("write to closed mcp file")
	}

	// Parse tool name from subpath: /tools/create-issue -> create-issue
	toolName := parseToolName(f.subpath)

	// Build tools/call params
	params := map[string]any{
		"name":      toolName,
		"arguments": json.RawMessage(data),
	}
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return types.NewDriverError("Write", f.subpath, err, types.ErrInternal)
	}

	resp, err := f.transport.Call(ctx, "tools/call", paramsJSON)
	if err != nil {
		f.writeErr = err
		return types.NewDriverError("Write", f.subpath, err, types.ErrServiceUnavailable)
	}

	f.response = resp
	f.writeErr = nil
	return nil
}

// Read returns the buffered response from the last Write (tool call).
func (f *mcpFile) Read(length int) ([]byte, error) {
	if f.closed {
		return nil, fmt.Errorf("read from closed mcp file")
	}
	if f.writeErr != nil {
		return nil, types.NewDriverError("Read", f.subpath, f.writeErr, types.ErrServiceUnavailable)
	}
	if f.response == nil {
		return nil, fmt.Errorf("no response available: write a request first")
	}

	remaining := f.response
	if len(remaining) == 0 {
		return nil, nil
	}

	if length <= 0 || length > len(remaining) {
		length = len(remaining)
	}

	data := make([]byte, length)
	copy(data, remaining[:length])
	f.response = remaining[length:]
	return data, nil
}

// Close marks the file as closed. Does NOT close the MCP transport
// (the transport is shared across all files for the same mount).
func (f *mcpFile) Close() error {
	if f.closed {
		return fmt.Errorf("mcp file already closed")
	}
	f.closed = true
	f.response = nil
	return nil
}

// Stat returns metadata about this MCP tool file.
func (f *mcpFile) Stat() (FileStat, error) {
	if f.closed {
		return FileStat{}, fmt.Errorf("stat on closed mcp file")
	}
	return FileStat{
		Name:     f.subpath,
		IsDevice: true,
	}, nil
}

// parseToolName extracts the tool name from a subpath.
// e.g. "/tools/create-issue" -> "create-issue"
func parseToolName(subpath string) string {
	subpath = strings.TrimPrefix(subpath, "/tools/")
	subpath = strings.TrimPrefix(subpath, "/")
	return subpath
}

// mcpFileFactory returns a VFSFileFactory that creates mcpFile instances
// for the given MCP transport.
func mcpFileFactory(transport MCPTransport) VFSFileFactory {
	return func(subpath string, flags OpenFlag) (VFSFile, error) {
		return newMCPFile(subpath, transport), nil
	}
}
