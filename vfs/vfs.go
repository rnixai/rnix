// Package vfs implements the virtual file system layer for Rnix.
package vfs

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/xsync"
)

// OpenFlag represents file open mode flags.
type OpenFlag int

const (
	O_RDONLY OpenFlag = iota
	O_WRONLY
	O_RDWR
)

// FileStat holds metadata about a VFS file or device.
type FileStat struct {
	Name       string
	Size       int64
	IsDevice   bool
	DevicePath string
}

// VFSFile is the interface that all device drivers must implement.
type VFSFile interface {
	Read(length int) ([]byte, error)
	Write(ctx context.Context, data []byte) error
	Close() error
	Stat() (FileStat, error)
}

// StreamObserver is an optional interface for VFSFile implementations that
// support streaming with intermediate events (e.g., LLM driver internal steps).
// The kernel checks for this interface after Open and sets a handler to receive
// events emitted during Write (e.g., tool_call events from cursor/claude CLI).
type StreamObserver interface {
	SetStreamHandler(fn func(event map[string]any))
}

// ToolDef describes a tool that a VFS device provides.
// Fields and JSON tags are intentionally identical to llm.ToolDef for
// serialization compatibility across package boundaries.
//
// Subpath is an internal-only routing hint (json:"-"): when a single device
// driver multiplexes multiple tools onto distinct subpaths (e.g. /dev/intent
// dispatches /decompose, /confirm, /status, /execute via VFSFile.subpath),
// the driver sets Subpath here so the kernel's toolMap maps the tool name
// to the full device+subpath path. Empty Subpath means the tool opens the
// device root (the existing behavior for /dev/shell, /dev/fs, etc.).
type ToolDef struct {
	Name              string         `json:"name"`
	Description       string         `json:"description,omitempty"`
	Parameters        map[string]any `json:"parameters,omitempty"`        // JSON Schema
	MaxResultTokens   int            `json:"max_result_tokens,omitempty"` // 0 = unlimited
	IsReadOnly        bool           `json:"is_read_only,omitempty"`
	IsConcurrencySafe bool           `json:"is_concurrency_safe,omitempty"`
	IsDestructive     bool           `json:"is_destructive,omitempty"`
	ShouldDefer       bool           `json:"should_defer,omitempty"`
	SearchHint        string         `json:"search_hint,omitempty"`
	Subpath           string         `json:"-"` // routing hint, never serialized to LLM
}

// ToolDescriptor is an optional interface for VFS device drivers that can
// describe their capabilities as structured tool definitions.
// The kernel collects ToolDefs at spawn time to build native function-calling
// tool lists or auto-generated text protocols.
type ToolDescriptor interface {
	ToolDefs() []ToolDef
}

// CallerProviderAware is an optional interface for VFSFile implementations
// that need the calling process's LLM provider. The kernel sets this after
// Open so the device can route internal LLM calls through the correct provider.
type CallerProviderAware interface {
	SetCallerProvider(provider string)
}

// LLMOpenerAware is an optional interface for VFSFile implementations that
// need to open LLM devices (including project-level providers not in the global
// device registry). The kernel injects an opener after Open.
type LLMOpenerAware interface {
	SetLLMOpener(opener func(provider string) (VFSFile, error))
}

// ProjectConfigAware is an optional interface for VFSFile implementations that
// need the calling process's project config (for spawning sub-processes that
// use project-level providers). The kernel injects this after Open.
type ProjectConfigAware interface {
	SetProjectConfig(cfg any)
}

// ToolCapable is an optional interface for VFSFile implementations that
// indicate whether the underlying driver supports native tool calling
// (i.e., the LLM driver implements ToolCallingDriver).
type ToolCapable interface {
	SupportsToolCalling() bool
}

// ModelInfoProvider is an optional interface for VFSFile implementations
// that can report the default model configured for their driver.
// The kernel uses this at spawn time to backfill Process.Model when
// no explicit model was specified.
type ModelInfoProvider interface {
	DefaultModel() string
}

// VFSFileFactory creates a VFSFile for a given subpath and open flags.
// subpath is the remaining path after prefix matching (empty for exact matches).
// workDir is the per-process working directory; empty string means no workDir set.
type VFSFileFactory func(subpath string, flags OpenFlag, workDir string) (VFSFile, error)

// VFSError represents an error from VFS operations.
type VFSError struct {
	Op     string
	PID    types.PID
	Device string
	Err    error
	Code   types.ErrCode
}

// Error returns a formatted error string.
func (e *VFSError) Error() string {
	return fmt.Sprintf("[%s] PID %d %s: %s (%v)", e.Code, e.PID, e.Op, e.Device, e.Err)
}

// Unwrap returns the underlying error.
func (e *VFSError) Unwrap() error {
	return e.Err
}

// newVFSError creates a new VFSError.
func newVFSError(op string, pid types.PID, device string, err error, code types.ErrCode) *VFSError {
	return &VFSError{
		Op:     op,
		PID:    pid,
		Device: device,
		Err:    err,
		Code:   code,
	}
}

// fdTable manages file descriptors for a single process.
type fdTable struct {
	mu     sync.RWMutex
	files  map[types.FD]VFSFile
	nextFD types.FD
}

// newFDTable creates a new fdTable with FD allocation starting at 3.
func newFDTable() *fdTable {
	return &fdTable{
		files:  make(map[types.FD]VFSFile),
		nextFD: 3,
	}
}

// alloc allocates the next FD, stores the file, and returns the FD.
func (t *fdTable) alloc(file VFSFile) types.FD {
	t.mu.Lock()
	defer t.mu.Unlock()
	fd := t.nextFD
	t.nextFD++
	t.files[fd] = file
	return fd
}

// get retrieves the VFSFile for the given FD.
func (t *fdTable) get(fd types.FD) (VFSFile, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	f, ok := t.files[fd]
	return f, ok
}

// getAndRemove atomically retrieves and removes the VFSFile for the given FD.
func (t *fdTable) getAndRemove(fd types.FD) (VFSFile, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	f, ok := t.files[fd]
	if ok {
		delete(t.files, fd)
	}
	return f, ok
}

// closeAll closes all open files and clears the table.
func (t *fdTable) closeAll() error {
	t.mu.Lock()
	files := make([]VFSFile, 0, len(t.files))
	for fd, f := range t.files {
		files = append(files, f)
		delete(t.files, fd)
	}
	t.mu.Unlock()

	var firstErr error
	for _, f := range files {
		if err := f.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// VFS provides the unified virtual file system interface.
type VFS struct {
	devRegistry *DeviceRegistry
	fdTables    *xsync.SyncMap[types.PID, *fdTable]
	workDirs    *xsync.SyncMap[types.PID, string]
}

// NewVFS creates a new VFS with the given device registry.
func NewVFS(devRegistry *DeviceRegistry) *VFS {
	return &VFS{
		devRegistry: devRegistry,
		fdTables:    xsync.NewSyncMap[types.PID, *fdTable](),
		workDirs:    xsync.NewSyncMap[types.PID, string](),
	}
}

// DeviceRegistry returns the underlying device registry.
// Used by the kernel to access driver objects for ToolDescriptor discovery.
func (v *VFS) DeviceRegistry() *DeviceRegistry {
	return v.devRegistry
}

// SetWorkDir registers a working directory for the given process.
// Called during Spawn, before the reasonStep goroutine starts.
func (v *VFS) SetWorkDir(pid types.PID, dir string) {
	v.workDirs.Store(pid, dir)
}

// GetWorkDir returns the working directory for the given process.
// Returns empty string if no workDir is registered.
func (v *VFS) GetWorkDir(pid types.PID) string {
	dir, _ := v.workDirs.Load(pid)
	return dir
}

// getOrCreateFDTable returns the fdTable for the given PID, creating one if needed.
func (v *VFS) getOrCreateFDTable(pid types.PID) *fdTable {
	t, _ := v.fdTables.LoadOrStore(pid, newFDTable())
	return t
}

// RegisterFD registers a pre-created VFSFile in a process's fdTable.
// Used by the kernel to register pipe endpoints without going through device registry.
func (v *VFS) RegisterFD(pid types.PID, file VFSFile) types.FD {
	t := v.getOrCreateFDTable(pid)
	return t.alloc(file)
}

// getFDTable returns the fdTable for the given PID, or nil if not found.
func (v *VFS) getFDTable(pid types.PID) *fdTable {
	t, ok := v.fdTables.Load(pid)
	if !ok {
		return nil
	}
	return t
}

// driverErrCode extracts the error code from a *types.DriverError, defaulting to ErrDriver.
func driverErrCode(err error) types.ErrCode {
	var drvErr *types.DriverError
	if errors.As(err, &drvErr) {
		return drvErr.Code
	}
	return types.ErrDriver
}

// Open opens a device path and returns a new FD for the process.
func (v *VFS) Open(pid types.PID, path string, flags OpenFlag) (types.FD, error) {
	workDir := v.GetWorkDir(pid)
	file, err := v.devRegistry.Open(path, flags, workDir)
	if err != nil {
		code := driverErrCode(err)
		if errors.Is(err, errDeviceNotFound) {
			code = types.ErrNotFound
		}
		return 0, newVFSError("Open", pid, path, err, code)
	}
	t := v.getOrCreateFDTable(pid)
	fd := t.alloc(file)
	return fd, nil
}

// GetFile returns the VFSFile associated with the given FD for the process.
// Returns nil if the FD or process is not found.
func (v *VFS) GetFile(pid types.PID, fd types.FD) VFSFile {
	t := v.getFDTable(pid)
	if t == nil {
		return nil
	}
	file, ok := t.get(fd)
	if !ok {
		return nil
	}
	return file
}

// Read reads from the file associated with the given FD.
func (v *VFS) Read(pid types.PID, fd types.FD, length int) ([]byte, error) {
	t := v.getFDTable(pid)
	if t == nil {
		return nil, newVFSError("Read", pid, "", fmt.Errorf("invalid fd: %d", fd), types.ErrNotFound)
	}
	file, ok := t.get(fd)
	if !ok {
		return nil, newVFSError("Read", pid, "", fmt.Errorf("invalid fd: %d", fd), types.ErrNotFound)
	}
	data, err := file.Read(length)
	if err != nil {
		return nil, newVFSError("Read", pid, "", err, driverErrCode(err))
	}
	return data, nil
}

// Write writes data to the file associated with the given FD.
func (v *VFS) Write(ctx context.Context, pid types.PID, fd types.FD, data []byte) error {
	t := v.getFDTable(pid)
	if t == nil {
		return newVFSError("Write", pid, "", fmt.Errorf("invalid fd: %d", fd), types.ErrNotFound)
	}
	file, ok := t.get(fd)
	if !ok {
		return newVFSError("Write", pid, "", fmt.Errorf("invalid fd: %d", fd), types.ErrNotFound)
	}
	if err := file.Write(ctx, data); err != nil {
		return newVFSError("Write", pid, "", err, driverErrCode(err))
	}
	return nil
}

// Close closes the file associated with the given FD and removes it from the table.
func (v *VFS) Close(pid types.PID, fd types.FD) error {
	t := v.getFDTable(pid)
	if t == nil {
		return newVFSError("Close", pid, "", fmt.Errorf("invalid fd: %d", fd), types.ErrNotFound)
	}
	file, ok := t.getAndRemove(fd)
	if !ok {
		return newVFSError("Close", pid, "", fmt.Errorf("invalid fd: %d", fd), types.ErrNotFound)
	}
	if err := file.Close(); err != nil {
		return newVFSError("Close", pid, "", err, driverErrCode(err))
	}
	return nil
}

// Stat returns metadata about the given path.
func (v *VFS) Stat(path string) (FileStat, error) {
	stat, err := v.devRegistry.Stat(path)
	if err != nil {
		code := types.ErrDriver
		if errors.Is(err, errDeviceNotFound) {
			code = types.ErrNotFound
		}
		return FileStat{}, newVFSError("Stat", 0, path, err, code)
	}
	return stat, nil
}

// CloseAll closes all FDs for the given PID and removes the fdTable.
func (v *VFS) CloseAll(pid types.PID) error {
	v.workDirs.Delete(pid)
	t, ok := v.fdTables.LoadAndDelete(pid)
	if !ok {
		return nil
	}
	return t.closeAll()
}
