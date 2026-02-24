// Package fs implements the host filesystem driver for /dev/fs.
package fs

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/gonewx/crux/internal/types"
	"github.com/gonewx/crux/vfs"
)

// HostFSFile implements vfs.VFSFile for host filesystem file access.
type HostFSFile struct {
	file   *os.File
	path   string
	closed bool
}

// Read reads up to length bytes from the file.
// If length <= 0, reads the entire file.
func (f *HostFSFile) Read(length int) ([]byte, error) {
	if f.closed {
		return nil, fmt.Errorf("read from closed hostfs file: %s", f.path)
	}
	if length <= 0 {
		return io.ReadAll(f.file)
	}
	buf := make([]byte, length)
	n, err := io.ReadAtLeast(f.file, buf, 1)
	if err != nil && err != io.ErrUnexpectedEOF {
		return nil, err
	}
	return buf[:n], nil
}

// Write returns an error because /dev/fs is a read-only device in MVP.
func (f *HostFSFile) Write(_ context.Context, _ []byte) error {
	return fmt.Errorf("read-only device: /dev/fs%s", f.path)
}

// Close closes the underlying os.File.
func (f *HostFSFile) Close() error {
	if f.closed {
		return fmt.Errorf("hostfs file already closed: %s", f.path)
	}
	f.closed = true
	return f.file.Close()
}

// Stat returns metadata about this host filesystem file.
func (f *HostFSFile) Stat() (vfs.FileStat, error) {
	if f.closed {
		return vfs.FileStat{}, fmt.Errorf("stat on closed hostfs file: %s", f.path)
	}
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

// FileFactory returns a VFSFileFactory that opens host filesystem files.
func FileFactory() vfs.VFSFileFactory {
	return func(subpath string, flags vfs.OpenFlag) (vfs.VFSFile, error) {
		device := "/dev/fs" + subpath

		if subpath == "" {
			return nil, types.NewDriverError("Open", "/dev/fs", fmt.Errorf("empty subpath"), types.ErrNotFound)
		}

		// MVP: /dev/fs is read-only
		if flags != vfs.O_RDONLY {
			return nil, types.NewDriverError("Open", device, fmt.Errorf("read-only device"), types.ErrPermission)
		}

		f, err := os.Open(subpath)
		if err != nil {
			return nil, mapOSError("Open", device, err)
		}

		// Reject directories — only regular files allowed
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
			file: f,
			path: subpath,
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
