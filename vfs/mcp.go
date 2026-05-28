package vfs

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// mcpCallTimeout is the timeout for MCP protocol calls during VFS Read operations.
// Applies to tools/list, resources/list, and resources/read.
// tools/call timeout is managed by the caller's context via VFS Write.
const mcpCallTimeout = 30 * time.Second

// MCPConfig describes how to connect to an MCP server.
type MCPConfig struct {
	ServerName    string            `json:"server_name" yaml:"server_name"`
	Command       string            `json:"command" yaml:"command"`
	Args          []string          `json:"args,omitempty" yaml:"args,omitempty"`
	Env           map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
	TransportType string            `json:"transport_type" yaml:"transport_type"` // "stdio" (default)
	WorkDir       string            `json:"work_dir,omitempty" yaml:"work_dir,omitempty"`
	Instructions  string            `json:"instructions,omitempty" yaml:"instructions,omitempty"` // usage instructions injected into system prompt
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
	MCPStatusConnected MCPStatus = iota
	MCPStatusDisconnected
	MCPStatusError
)

// MCPStatusString returns the human-readable form of an MCPStatus. Used by
// IPC wire serialization (Story 48.3 §AC1). 48.5 will extend the enum to 5
// states (Reconnecting / BackoffExhausted); this function must grow with it.
func MCPStatusString(s MCPStatus) string {
	switch s {
	case MCPStatusConnected:
		return "connected"
	case MCPStatusDisconnected:
		return "disconnected"
	case MCPStatusError:
		return "error"
	default:
		return "unknown"
	}
}

// MCPMount holds the state of a mounted MCP server.
//
// refCount is the number of live owners (1 from the initial Mount; +1 per
// Acquire). Unmount decrements it; only when it hits zero does the transport
// actually close and the mount leave the registry. Story 48.1 code-review
// P2 / P4 — without ref-counting, a fork-resume reuser shared the parent's
// mount but the parent's finishProcess unmounted it; or Suspended-resume
// cleanup deleted the old proc entry while leaving the mount live, so the
// reuser's eventual Unmount happened against an entry whose only owner was
// already gone, stranding the mount forever.
//
// Protected by MountManager.mu; never mutated outside of the manager.
type MCPMount struct {
	Path      string
	Config    MCPConfig
	Status    MCPStatus
	transport MCPTransport
	refCount  int
}

// mcpFile implements VFSFile for MCP tool invocations.
// Each Open on an MCP path creates one mcpFile instance.
// Write sends a tools/call request; Read returns the result.
//
// Story 48.1 — mu guards every field below (response / closed / writeErr).
// Without it the resume path can race Close (triggered by finishProcess →
// vfs.CloseAll) against the test goroutine's Read, since both run on
// independent goroutines after `rnix resume` returns. The race manifested
// in TestATDD_48_1_001 once the test exercised concurrent Read + Close;
// the race-detector report fingered mcp.go:111 (Read of closed) and
// mcp.go:132 (Close write of closed) on the same word.
type mcpFile struct {
	mu        sync.Mutex
	subpath   string       // e.g. "/tools/create-issue"
	transport MCPTransport // shared transport for the mount (immutable)
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
//
// Note: the transport.Call invocation is intentionally outside the per-file
// mu — Call may block on stdio I/O for up to 30s and holding the lock would
// stall a concurrent Close called by finishProcess. mu protects only the
// short pre/post field assignments.
func (f *mcpFile) Write(ctx context.Context, data []byte) error {
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return types.NewDriverError("Write", f.subpath, fmt.Errorf("mcp file closed"), types.ErrDriver)
	}
	transport := f.transport
	subpath := f.subpath
	f.mu.Unlock()

	// Parse tool name from subpath: /tools/create-issue -> create-issue
	toolName := parseToolName(subpath)

	// Build tools/call params
	params := map[string]any{
		"name":      toolName,
		"arguments": json.RawMessage(data),
	}
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return types.NewDriverError("Write", subpath, err, types.ErrInternal)
	}

	resp, err := transport.Call(ctx, "tools/call", paramsJSON)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		// File was closed while Call was in flight. Drop the response without
		// publishing it, mirroring the "Close wins" semantics of Read.
		return types.NewDriverError("Write", subpath, fmt.Errorf("mcp file closed"), types.ErrDriver)
	}
	if err != nil {
		f.writeErr = err
		return types.NewDriverError("Write", subpath, err, types.ErrServiceUnavailable)
	}
	f.response = resp
	f.writeErr = nil
	return nil
}

// Read returns the buffered response from the last Write (tool call).
func (f *mcpFile) Read(length int) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil, types.NewDriverError("Read", f.subpath, fmt.Errorf("mcp file closed"), types.ErrDriver)
	}
	if f.writeErr != nil {
		return nil, types.NewDriverError("Read", f.subpath, f.writeErr, types.ErrServiceUnavailable)
	}
	if f.response == nil {
		return nil, types.NewDriverError("Read", f.subpath, fmt.Errorf("no response available: write a request first"), types.ErrInvalid)
	}

	data, remaining := readFromBuffer(f.response, length)
	f.response = remaining
	return data, nil
}

// Close marks the file as closed. Does NOT close the MCP transport
// (the transport is shared across all files for the same mount).
func (f *mcpFile) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return types.NewDriverError("Close", f.subpath, fmt.Errorf("mcp file already closed"), types.ErrDriver)
	}
	f.closed = true
	f.response = nil
	return nil
}

// Stat returns metadata about this MCP tool file.
func (f *mcpFile) Stat() (FileStat, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return FileStat{}, types.NewDriverError("Stat", f.subpath, fmt.Errorf("mcp file closed"), types.ErrDriver)
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

// mcpFileFactory returns a VFSFileFactory that routes subpaths to the
// appropriate MCP VFSFile type based on subpath prefix.
func mcpFileFactory(transport MCPTransport) VFSFileFactory {
	return func(subpath string, flags OpenFlag, workDir string) (VFSFile, error) {
		subpath = strings.TrimRight(subpath, "/")
		if subpath == "" {
			subpath = "/"
		}

		switch {
		case subpath == "/":
			return newMCPRootFile(), nil
		case subpath == "/tools":
			return newMCPToolListFile(transport), nil
		case strings.HasPrefix(subpath, "/tools/"):
			return newMCPFile(subpath, transport), nil
		case subpath == "/resources":
			return newMCPResourceListFile(transport), nil
		case strings.HasPrefix(subpath, "/resources/"):
			return newMCPResourceFile(subpath, transport), nil
		default:
			return nil, types.NewDriverError("Open", subpath,
				fmt.Errorf("unknown mcp subpath: %s (valid: /tools, /tools/{name}, /resources, /resources/{uri})", subpath),
				types.ErrNotFound)
		}
	}
}

// readFromBuffer reads up to length bytes from buf. Returns the data read
// and the remaining buffer. Returns (nil, nil) when buffer is empty.
func readFromBuffer(buf []byte, length int) (data []byte, remaining []byte) {
	if len(buf) == 0 {
		return nil, nil
	}
	if length <= 0 || length > len(buf) {
		length = len(buf)
	}
	data = make([]byte, length)
	copy(data, buf[:length])
	return data, buf[length:]
}

// --- mcpRootFile: read-only listing of available namespaces (AC #5) ---

type mcpRootFile struct {
	response []byte
	closed   bool
}

func newMCPRootFile() *mcpRootFile {
	data, _ := json.Marshal([]string{"tools", "resources"})
	return &mcpRootFile{response: data}
}

func (f *mcpRootFile) Read(length int) ([]byte, error) {
	if f.closed {
		return nil, types.NewDriverError("Read", "/", fmt.Errorf("mcp file closed"), types.ErrDriver)
	}
	data, remaining := readFromBuffer(f.response, length)
	f.response = remaining
	return data, nil
}

func (f *mcpRootFile) Write(_ context.Context, _ []byte) error {
	return types.NewDriverError("Write", "/",
		fmt.Errorf("root listing is read-only"), types.ErrInvalid)
}

func (f *mcpRootFile) Close() error {
	if f.closed {
		return types.NewDriverError("Close", "/", fmt.Errorf("mcp file already closed"), types.ErrDriver)
	}
	f.closed = true
	f.response = nil
	return nil
}

func (f *mcpRootFile) Stat() (FileStat, error) {
	if f.closed {
		return FileStat{}, types.NewDriverError("Stat", "/", fmt.Errorf("mcp file closed"), types.ErrDriver)
	}
	return FileStat{Name: "/", IsDevice: true}, nil
}

// --- mcpToolListFile: read-only tools/list call (AC #2) ---

type mcpToolListFile struct {
	transport MCPTransport
	response  []byte
	closed    bool
	loaded    bool
}

func newMCPToolListFile(transport MCPTransport) *mcpToolListFile {
	return &mcpToolListFile{transport: transport}
}

func (f *mcpToolListFile) Read(length int) ([]byte, error) {
	if f.closed {
		return nil, types.NewDriverError("Read", "/tools", fmt.Errorf("mcp file closed"), types.ErrDriver)
	}
	if !f.loaded {
		ctx, cancel := context.WithTimeout(context.Background(), mcpCallTimeout)
		defer cancel()
		resp, err := f.transport.Call(ctx, "tools/list", nil)
		if err != nil {
			return nil, types.NewDriverError("Read", "/tools", err, types.ErrServiceUnavailable)
		}
		f.response = resp
		f.loaded = true
	}
	data, remaining := readFromBuffer(f.response, length)
	f.response = remaining
	return data, nil
}

func (f *mcpToolListFile) Write(_ context.Context, _ []byte) error {
	if f.closed {
		return types.NewDriverError("Write", "/tools", fmt.Errorf("mcp file closed"), types.ErrDriver)
	}
	return types.NewDriverError("Write", "/tools",
		fmt.Errorf("tools listing is read-only"), types.ErrInvalid)
}

func (f *mcpToolListFile) Close() error {
	if f.closed {
		return types.NewDriverError("Close", "/tools", fmt.Errorf("mcp file already closed"), types.ErrDriver)
	}
	f.closed = true
	f.response = nil
	return nil
}

func (f *mcpToolListFile) Stat() (FileStat, error) {
	if f.closed {
		return FileStat{}, types.NewDriverError("Stat", "/tools", fmt.Errorf("mcp file closed"), types.ErrDriver)
	}
	return FileStat{Name: "/tools", IsDevice: true}, nil
}

// --- mcpResourceFile: read-only resources/read call (AC #3) ---

type mcpResourceFile struct {
	subpath   string
	transport MCPTransport
	response  []byte
	closed    bool
	loaded    bool
}

func newMCPResourceFile(subpath string, transport MCPTransport) *mcpResourceFile {
	return &mcpResourceFile{subpath: subpath, transport: transport}
}

func (f *mcpResourceFile) Read(length int) ([]byte, error) {
	if f.closed {
		return nil, types.NewDriverError("Read", f.subpath, fmt.Errorf("mcp file closed"), types.ErrDriver)
	}
	if !f.loaded {
		uri := parseResourceURI(f.subpath)
		params, _ := json.Marshal(map[string]string{"uri": uri})
		ctx, cancel := context.WithTimeout(context.Background(), mcpCallTimeout)
		defer cancel()
		resp, err := f.transport.Call(ctx, "resources/read", params)
		if err != nil {
			return nil, types.NewDriverError("Read", f.subpath, err, types.ErrServiceUnavailable)
		}
		f.response = resp
		f.loaded = true
	}
	data, remaining := readFromBuffer(f.response, length)
	f.response = remaining
	return data, nil
}

func (f *mcpResourceFile) Write(_ context.Context, _ []byte) error {
	if f.closed {
		return types.NewDriverError("Write", f.subpath, fmt.Errorf("mcp file closed"), types.ErrDriver)
	}
	return types.NewDriverError("Write", f.subpath,
		fmt.Errorf("resource read is read-only"), types.ErrInvalid)
}

func (f *mcpResourceFile) Close() error {
	if f.closed {
		return types.NewDriverError("Close", f.subpath, fmt.Errorf("mcp file already closed"), types.ErrDriver)
	}
	f.closed = true
	f.response = nil
	return nil
}

func (f *mcpResourceFile) Stat() (FileStat, error) {
	if f.closed {
		return FileStat{}, types.NewDriverError("Stat", f.subpath, fmt.Errorf("mcp file closed"), types.ErrDriver)
	}
	return FileStat{Name: f.subpath, IsDevice: true}, nil
}

// parseResourceURI extracts the resource URI from a subpath.
// e.g. "/resources/repo://owner/repo" -> "repo://owner/repo"
func parseResourceURI(subpath string) string {
	return strings.TrimPrefix(subpath, "/resources/")
}

// --- mcpResourceListFile: read-only resources/list call (AC #4) ---

type mcpResourceListFile struct {
	transport MCPTransport
	response  []byte
	closed    bool
	loaded    bool
}

func newMCPResourceListFile(transport MCPTransport) *mcpResourceListFile {
	return &mcpResourceListFile{transport: transport}
}

func (f *mcpResourceListFile) Read(length int) ([]byte, error) {
	if f.closed {
		return nil, types.NewDriverError("Read", "/resources", fmt.Errorf("mcp file closed"), types.ErrDriver)
	}
	if !f.loaded {
		ctx, cancel := context.WithTimeout(context.Background(), mcpCallTimeout)
		defer cancel()
		resp, err := f.transport.Call(ctx, "resources/list", nil)
		if err != nil {
			return nil, types.NewDriverError("Read", "/resources", err, types.ErrServiceUnavailable)
		}
		f.response = resp
		f.loaded = true
	}
	data, remaining := readFromBuffer(f.response, length)
	f.response = remaining
	return data, nil
}

func (f *mcpResourceListFile) Write(_ context.Context, _ []byte) error {
	if f.closed {
		return types.NewDriverError("Write", "/resources", fmt.Errorf("mcp file closed"), types.ErrDriver)
	}
	return types.NewDriverError("Write", "/resources",
		fmt.Errorf("resource listing is read-only"), types.ErrInvalid)
}

func (f *mcpResourceListFile) Close() error {
	if f.closed {
		return types.NewDriverError("Close", "/resources", fmt.Errorf("mcp file already closed"), types.ErrDriver)
	}
	f.closed = true
	f.response = nil
	return nil
}

func (f *mcpResourceListFile) Stat() (FileStat, error) {
	if f.closed {
		return FileStat{}, types.NewDriverError("Stat", "/resources", fmt.Errorf("mcp file closed"), types.ErrDriver)
	}
	return FileStat{Name: "/resources", IsDevice: true}, nil
}
