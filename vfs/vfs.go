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

// VFSFileFactory creates a VFSFile for a given subpath and open flags.
// subpath is the remaining path after prefix matching (empty for exact matches).
type VFSFileFactory func(subpath string, flags OpenFlag) (VFSFile, error)

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
}

// NewVFS creates a new VFS with the given device registry.
func NewVFS(devRegistry *DeviceRegistry) *VFS {
	return &VFS{
		devRegistry: devRegistry,
		fdTables:    xsync.NewSyncMap[types.PID, *fdTable](),
	}
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
	file, err := v.devRegistry.Open(path, flags)
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
	t, ok := v.fdTables.LoadAndDelete(pid)
	if !ok {
		return nil
	}
	return t.closeAll()
}
