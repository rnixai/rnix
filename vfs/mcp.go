package vfs

import (
	"context"
	"encoding/json"
	"errors"
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

// MCPConfig describes how to connect to an MCP server. It is the parsed,
// runtime form: the per-server timeout knobs are time.Duration here (decoded
// from the mcp.yaml duration strings by MCPServerConfig.ToMCPConfig), not raw
// strings. A zero MountTimeout / RequestTimeout / MaxOutputBytes means "use the
// transport default" — both the MountManager (mount.go) and NewStdioTransport
// (drivers/mcp) fall back rather than treating zero as an instant timeout
// (Story 48.6 §易错点 5).
type MCPConfig struct {
	ServerName    string            `json:"server_name" yaml:"server_name"`
	Command       string            `json:"command" yaml:"command"`
	Args          []string          `json:"args,omitempty" yaml:"args,omitempty"`
	Env           map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
	TransportType string            `json:"transport_type" yaml:"transport_type"` // "stdio" (default) | "http"
	WorkDir       string            `json:"work_dir,omitempty" yaml:"work_dir,omitempty"`
	Instructions  string            `json:"instructions,omitempty" yaml:"instructions,omitempty"` // usage instructions injected into system prompt

	// Streamable HTTP transport (Story 59.1 / Epic 59). Used when
	// TransportType == "http"; ignored for stdio. URL is the single MCP
	// endpoint; Headers are sent verbatim on every request (Bearer / API key /
	// custom), with ${ENV} interpolation resolved at transport build time.
	URL     string            `json:"url,omitempty" yaml:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`

	// Per-server timeout / output knobs (Story 48.6 FR-48-S8). Zero = default.
	MountTimeout   time.Duration `json:"mount_timeout,omitempty" yaml:"mount_timeout,omitempty"`     // Connect budget (default 5s)
	RequestTimeout time.Duration `json:"request_timeout,omitempty" yaml:"request_timeout,omitempty"` // single tools/call read budget (default 60s)
	MaxOutputBytes int64         `json:"max_output_bytes,omitempty" yaml:"max_output_bytes,omitempty"` // tools/call result cap (default 4MB)
}

// MCPTransport defines the interface for communicating with an MCP server.
// Implementations live in drivers/mcp; the interface is defined here to
// avoid a vfs -> drivers dependency (dependency inversion).
type MCPTransport interface {
	Connect(ctx context.Context) error
	Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error)
	Close() error
	Ping(ctx context.Context) error

	// --- Story 48.5 health / status surface ---
	// These let the kernel project live transport state into observability
	// surfaces (rnix mcp list / mcp logs) and emit mcp.error/mcp.reconnect
	// events at the tool-call boundary, WITHOUT vfs or kernel importing
	// drivers/mcp (the mechanism lives in drivers/mcp; only this read-only
	// interface crosses the boundary — Story 48.5 §决策 1/2).

	// Status returns the transport's authoritative connection status. MCPMount.
	// Status is a stale projection set at Mount time; callers wanting live state
	// must consult this.
	Status() MCPStatus
	// Alive reports the most recent L1 liveness verdict (child process up).
	Alive() bool
	// ToolCount / ResourceCount return the cached counts from the last
	// tools/list / resources/list (refreshed on connect + reconnect).
	ToolCount() int
	ResourceCount() int
	// LastCheck is the wall-clock time of the last successful L2 readiness ping
	// (zero time = never pinged).
	LastCheck() time.Time
	// ReconnectCount is a monotonic counter of successful reconnects; the kernel
	// compares deltas across tool calls to emit mcp.reconnect exactly once.
	ReconnectCount() int
	// StderrTail returns the captured child-process stderr ring buffer
	// (oldest→newest), surfaced by `rnix mcp logs <name>`.
	StderrTail() []string
}

// TransportFactory creates an MCPTransport from an MCPConfig.
type TransportFactory func(config MCPConfig) (MCPTransport, error)

// MCPStatus represents the connection status of an MCP mount.
type MCPStatus int

const (
	MCPStatusConnected MCPStatus = iota
	MCPStatusDisconnected
	MCPStatusError
	// Story 48.5 §易错点 5: the two states below are APPENDED after Error so
	// the existing iota values (Connected=0 / Disconnected=1 / Error=2) — which
	// IPC wire serialization and potential persistence reference — never shift.
	MCPStatusReconnecting     // reconnecter is mid-backoff re-exec
	MCPStatusBackoffExhausted // all reconnect attempts failed; calls fast-fail
	// Story 48.6 §并发化设计: a Mount placeholder is inserted with this status
	// while transport.Connect runs OUTSIDE the global lock. APPENDED last (value
	// 5) so the existing iota wire values never shift. A placeholder is never
	// surfaced to a reuser — Acquire blocks on the per-entry lock until Mount
	// finalizes to Connected (or deletes the placeholder on Connect failure).
	MCPStatusConnecting
)

// MCPStatusString returns the human-readable form of an MCPStatus. Used by
// IPC wire serialization (Story 48.3 §AC1). Story 48.5 extended the enum to 5
// states (Reconnecting / BackoffExhausted).
func MCPStatusString(s MCPStatus) string {
	switch s {
	case MCPStatusConnected:
		return "connected"
	case MCPStatusDisconnected:
		return "disconnected"
	case MCPStatusError:
		return "error"
	case MCPStatusReconnecting:
		return "reconnecting"
	case MCPStatusBackoffExhausted:
		return "backoff_exhausted"
	case MCPStatusConnecting:
		return "connecting"
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
// Concurrency (Story 48.6): the global MountManager.mu is GONE. Each entry now
// carries its OWN lock (mu, a pointer so ListMounts can copy the struct without
// tripping go vet copylocks). The mutable fields below (Status / transport /
// refCount) are guarded by that per-entry lock; the entry's presence in the
// registry is the atomic xsync.SyncMap. Mount inserts a Connecting placeholder
// with the lock already held, runs Connect OUTSIDE any global critical section
// (so distinct paths Connect in parallel), then finalizes under the held lock —
// an Acquire racing the in-flight Connect blocks on mu until finalize, then
// sees either Connected or a deleted placeholder. Read paths (GetStatus /
// ListMounts / Range) are lock-free SyncMap reads.
type MCPMount struct {
	Path      string
	Config    MCPConfig
	Status    MCPStatus
	transport MCPTransport
	refCount  int

	// mu guards Status / transport / refCount for THIS entry (Story 48.6). It is
	// a pointer so the value copies ListMounts/Transport() hand out do not copy a
	// lock (go vet copylocks). nil mu (e.g. a bare MCPMount{Path:...} built by a
	// test mock for ListMounts) is never locked by the manager.
	mu *sync.Mutex
}

// Transport exposes the mount's transport for read-only observability
// projection (Story 48.5): the kernel reads live Status()/ToolCount()/
// LastCheck()/StderrTail() off it to back `rnix mcp list` and `rnix mcp logs`
// without vfs growing a kernel/driver dependency. Returns nil for mounts
// created without a transport (defensive — never in the normal Mount path).
// The returned value is a copy of the interface held by the MCPMount value
// copy that ListMounts hands out; it points at the same underlying transport.
func (m MCPMount) Transport() MCPTransport { return m.transport }

// lock / unlock guard this entry's mutable fields (Story 48.6 per-entry lock).
// They tolerate a nil mu so a bare MCPMount{Path:...} built by a test mock for
// ListMounts is never a hazard — only entries the MountManager creates carry a
// real lock. Pointer receiver: the manager always holds *MCPMount, never a copy.
func (m *MCPMount) lock() {
	if m.mu != nil {
		m.mu.Lock()
	}
}

func (m *MCPMount) unlock() {
	if m.mu != nil {
		m.mu.Unlock()
	}
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
// mcpCallErrCode maps a transport.Call failure to an ErrCode, preserving the
// L1 ErrDeviceDisconnected sentinel so the kernel can tell a known-disconnected
// device (→ mcp.error) from a transient in-flight failure (→ ErrServiceUnavailable).
// Used on every MCP call path, including the read-only ones (tools/list,
// resources/read, resources/list) so AC1 / Decision 4 hold there too
// ([Review][Patch] P4). Story 48.5 §易错点 12.
func mcpCallErrCode(err error) types.ErrCode {
	var de *types.DriverError
	if errors.As(err, &de) && de.Code == types.ErrDeviceDisconnected {
		return types.ErrDeviceDisconnected
	}
	return types.ErrServiceUnavailable
}

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
		return types.NewDriverError("Write", subpath, err, mcpCallErrCode(err))
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
			return nil, types.NewDriverError("Read", "/tools", err, mcpCallErrCode(err))
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
			return nil, types.NewDriverError("Read", f.subpath, err, mcpCallErrCode(err))
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
			return nil, types.NewDriverError("Read", "/resources", err, mcpCallErrCode(err))
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
