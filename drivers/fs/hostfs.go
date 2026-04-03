// Package fs implements the host filesystem driver for /dev/fs.
package fs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// HostFSDriver is the driver object for the host filesystem device.
// Implements vfs.ToolDescriptor to describe read_file, write_file, list_dir tools.
type HostFSDriver struct{}

// compile-time interface check
var _ vfs.ToolDescriptor = (*HostFSDriver)(nil)

// NewDriver creates a new HostFSDriver instance.
func NewDriver() *HostFSDriver {
	return &HostFSDriver{}
}

// ToolDefs returns tool definitions for the host filesystem device.
func (d *HostFSDriver) ToolDefs() []vfs.ToolDef {
	return []vfs.ToolDef{
		{
			Name:            "read_file",
			Description:     "Read the contents of a file at the given path.",
			MaxResultTokens: 25000,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "File path relative to the working directory",
					},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:            "write_file",
			Description:     "Create or overwrite a file at the given path with the provided content.",
			MaxResultTokens: 0,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "File path relative to the working directory",
					},
					"content": map[string]any{
						"type":        "string",
						"description": "Content to write to the file",
					},
				},
				"required": []string{"path", "content"},
			},
		},
		{
			Name:            "list_dir",
			Description:     "List the contents of a directory at the given path.",
			MaxResultTokens: 5000,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "Directory path relative to the working directory",
					},
				},
				"required": []string{"path"},
			},
		},
	}
}

// FileFactoryFromDriver returns a VFSFileFactory from the driver.
func (d *HostFSDriver) FileFactory() vfs.VFSFileFactory {
	return FileFactory()
}

const (
	// maxReadFileTokens is the maximum token count for read_file results.
	maxReadFileTokens = 25000
	// maxListDirEntries is the maximum number of entries returned by list_dir.
	maxListDirEntries = 100
)

// HostFSFile implements vfs.VFSFile for host filesystem file access.
// Supports two modes:
//   - Read mode (O_RDONLY): wraps an os.File for direct reading.
//   - Command mode (O_WRONLY/O_RDWR): Write→Read pattern for write-file and list-directory operations.
type HostFSFile struct {
	file     *os.File // non-nil in read mode
	path     string   // resolved host path
	workDir  string   // sandbox root for command mode
	mode     vfs.OpenFlag
	response []byte // buffered result in command mode
	closed   bool
}

// writeRequest is the JSON payload for /dev/fs command-mode writes.
type writeRequest struct {
	Content *string `json:"content"` // pointer to distinguish absent from empty string
	Op      string  `json:"op"`      // "list"
}

// listEntry is a single entry returned by the list operation.
type listEntry struct {
	Name  string `json:"name"`
	Size  int64  `json:"size"`
	IsDir bool   `json:"is_dir"`
}

// Read reads up to length bytes from the file (read mode) or returns
// buffered command results (command mode).
func (f *HostFSFile) Read(length int) ([]byte, error) {
	if f.closed {
		return nil, fmt.Errorf("read from closed hostfs file: %s", f.path)
	}

	// Command mode: return buffered response from Write.
	if f.mode != vfs.O_RDONLY {
		if f.response == nil {
			return nil, &types.DriverError{
				Op: "Read", Device: "/dev/fs", Err: fmt.Errorf("no result: write a command first"), Code: types.ErrDriver,
			}
		}
		data := f.response
		f.response = nil // one-shot read
		return data, nil
	}

	// Read mode: delegate to underlying os.File.
	var data []byte
	var readErr error
	if length <= 0 {
		data, readErr = io.ReadAll(f.file)
	} else {
		buf := make([]byte, length)
		n, err := io.ReadAtLeast(f.file, buf, 1)
		if err != nil && err != io.ErrUnexpectedEOF {
			return nil, err
		}
		data = buf[:n]
		readErr = nil
	}
	if readErr != nil {
		return nil, readErr
	}

	// Apply token-based truncation for read_file results.
	content := string(data)
	truncated, didTruncate := rnixctx.TruncateResult(content, maxReadFileTokens)
	if didTruncate {
		originalTokens := rnixctx.EstimateTokens(content)
		shownTokens := rnixctx.EstimateTokens(truncated)

		var overflowPath string
		if f.workDir != "" {
			overflowPath, _ = rnixctx.WriteOverflow(content, f.workDir)
		}

		truncated += rnixctx.FormatTruncationNotice(originalTokens, shownTokens, overflowPath)
		return []byte(truncated), nil
	}

	return data, nil
}

// Write executes a filesystem command in command mode.
// Accepts JSON: {"content": "..."} to write a file, or {"op": "list"} to list a directory.
func (f *HostFSFile) Write(_ context.Context, data []byte) error {
	if f.closed {
		return fmt.Errorf("write to closed hostfs file: %s", f.path)
	}
	if f.mode == vfs.O_RDONLY {
		return &types.DriverError{Op: "Write", Device: "/dev/fs" + f.path, Err: fmt.Errorf("read-only mode"), Code: types.ErrPermission}
	}

	var req writeRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return &types.DriverError{Op: "Write", Device: "/dev/fs", Err: fmt.Errorf("invalid JSON payload: %w", err), Code: types.ErrDriver}
	}

	switch {
	case req.Op == "list":
		return f.execList()
	case req.Content != nil:
		return f.execWrite(*req.Content)
	default:
		return &types.DriverError{Op: "Write", Device: "/dev/fs", Err: fmt.Errorf("unknown operation: payload must contain \"content\" or \"op\""), Code: types.ErrDriver}
	}
}

// execWrite creates/overwrites a file at f.path with the given content.
func (f *HostFSFile) execWrite(content string) error {
	device := "/dev/fs"

	// Sandbox check: path must stay within workDir.
	if err := f.checkSandbox(); err != nil {
		return err
	}

	// Auto-create parent directories.
	dir := filepath.Dir(f.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return mapOSError("Write", device, err)
	}

	if err := os.WriteFile(f.path, []byte(content), 0o644); err != nil {
		return mapOSError("Write", device, err)
	}

	f.response = []byte("ok")
	return nil
}

// listDirResult wraps list entries with an optional truncation notice.
type listDirResult struct {
	Entries []listEntry `json:"entries"`
	Notice  string      `json:"notice,omitempty"`
}

// execList reads the directory at f.path and buffers the result as JSON.
func (f *HostFSFile) execList() error {
	device := "/dev/fs"

	entries, err := os.ReadDir(f.path)
	if err != nil {
		return mapOSError("Write", device, err)
	}

	listed := make([]listEntry, 0, min(len(entries), maxListDirEntries))
	for _, e := range entries {
		if len(listed) >= maxListDirEntries {
			break
		}
		info, err := e.Info()
		if err != nil {
			continue // skip entries we can't stat
		}
		listed = append(listed, listEntry{
			Name:  e.Name(),
			Size:  info.Size(),
			IsDir: e.IsDir(),
		})
	}

	out := listDirResult{Entries: listed}
	if len(entries) > maxListDirEntries {
		out.Notice = fmt.Sprintf("Showing %d of %d entries. Use glob pattern for targeted search.", maxListDirEntries, len(entries))
	}

	b, err := json.Marshal(out)
	if err != nil {
		return &types.DriverError{Op: "Write", Device: device, Err: fmt.Errorf("marshal list result: %w", err), Code: types.ErrDriver}
	}

	f.response = b
	return nil
}

// checkSandbox ensures f.path does not escape the workDir sandbox.
func (f *HostFSFile) checkSandbox() error {
	if f.workDir == "" {
		return nil // no sandbox when workDir is unset
	}
	absPath, err := filepath.Abs(f.path)
	if err != nil {
		return &types.DriverError{Op: "Write", Device: "/dev/fs", Err: fmt.Errorf("cannot resolve path: %w", err), Code: types.ErrDriver}
	}
	absWorkDir, err := filepath.Abs(f.workDir)
	if err != nil {
		return &types.DriverError{Op: "Write", Device: "/dev/fs", Err: fmt.Errorf("cannot resolve workDir: %w", err), Code: types.ErrDriver}
	}
	if !strings.HasPrefix(absPath, absWorkDir+string(filepath.Separator)) && absPath != absWorkDir {
		return &types.DriverError{
			Op: "Write", Device: "/dev/fs",
			Err:  fmt.Errorf("path outside sandbox: %s is not within %s", absPath, absWorkDir),
			Code: types.ErrPermission,
		}
	}
	return nil
}

// Close closes the underlying os.File (read mode) or releases buffers (command mode).
func (f *HostFSFile) Close() error {
	if f.closed {
		return fmt.Errorf("hostfs file already closed: %s", f.path)
	}
	f.closed = true
	f.response = nil
	if f.file != nil {
		return f.file.Close()
	}
	return nil
}

// Stat returns metadata about this host filesystem file.
func (f *HostFSFile) Stat() (vfs.FileStat, error) {
	if f.closed {
		return vfs.FileStat{}, fmt.Errorf("stat on closed hostfs file: %s", f.path)
	}
	if f.file != nil {
		info, err := f.file.Stat()
		if err != nil {
			return vfs.FileStat{}, err
		}
		return vfs.FileStat{
			Name:       info.Name(),
			Size:       info.Size(),
			IsDevice:   false,
			DevicePath: "/dev/fs",
		}, nil
	}
	// Command mode: return basic info.
	return vfs.FileStat{
		Name:       filepath.Base(f.path),
		Size:       int64(len(f.response)),
		IsDevice:   false,
		DevicePath: "/dev/fs",
	}, nil
}

// resolvePath resolves a VFS subpath to a host filesystem path using workDir.
func resolvePath(subpath, workDir string) string {
	trimmed := strings.TrimPrefix(subpath, "/")
	if workDir != "" {
		if trimmed == "" {
			return workDir // subpath "/" → workDir root
		}
		if !filepath.IsAbs(trimmed) {
			return filepath.Join(workDir, trimmed)
		}
	}
	return subpath
}

// FileFactory returns a VFSFileFactory that opens host filesystem files.
func FileFactory() vfs.VFSFileFactory {
	return func(subpath string, flags vfs.OpenFlag, workDir string) (vfs.VFSFile, error) {
		device := "/dev/fs" + subpath

		if subpath == "" {
			return nil, types.NewDriverError("Open", "/dev/fs", fmt.Errorf("missing file path: use /dev/fs/<path> (e.g. /dev/fs/src/main.go), not /dev/fs alone"), types.ErrNotFound)
		}

		resolved := resolvePath(subpath, workDir)

		// Command mode: O_WRONLY or O_RDWR — defer actual I/O to Write.
		if flags != vfs.O_RDONLY {
			return &HostFSFile{
				path:    resolved,
				workDir: workDir,
				mode:    flags,
			}, nil
		}

		// Read mode: open the file immediately.
		f, err := os.Open(resolved)
		if err != nil {
			return nil, mapOSError("Open", device, err)
		}

		// Reject directories — only regular files allowed in read mode.
		info, err := f.Stat()
		if err != nil {
			f.Close()
			return nil, mapOSError("Open", device, err)
		}
		if info.IsDir() {
			f.Close()
			return nil, types.NewDriverError("Open", device, fmt.Errorf("is a directory"), types.ErrPermission)
		}

		return &HostFSFile{
			file:    f,
			path:    resolved,
			workDir: workDir,
			mode:    vfs.O_RDONLY,
		}, nil
	}
}

// mapOSError converts an os-level error to a *types.DriverError.
func mapOSError(op, device string, err error) *types.DriverError {
	switch {
	case os.IsNotExist(err):
		return types.NewDriverError(op, device, err, types.ErrNotFound)
	case os.IsPermission(err):
		return types.NewDriverError(op, device, err, types.ErrPermission)
	default:
		return types.NewDriverError(op, device, err, types.ErrDriver)
	}
}
