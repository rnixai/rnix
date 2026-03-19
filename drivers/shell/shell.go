// Package shell implements the shell command execution driver for /dev/shell.
package shell

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

const (
	// DefaultTimeout is the default timeout for shell command execution.
	DefaultTimeout = 30 * time.Second
)

// CommandBuilder is a function type that creates exec.Cmd instances.
// In production, this wraps exec.CommandContext; in tests, it can be replaced with a mock.
type CommandBuilder func(ctx context.Context, name string, args ...string) *exec.Cmd

// defaultCommandBuilder wraps exec.CommandContext for production use.
func defaultCommandBuilder(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}

// DriverOpts holds configuration options for ShellDriver.
type DriverOpts struct {
	Timeout    time.Duration
	CmdBuilder CommandBuilder
}

// ShellDriver manages shell command execution.
type ShellDriver struct {
	defaultTimeout time.Duration
	cmdBuilder     CommandBuilder
}

// NewDriver creates a ShellDriver with default configuration.
func NewDriver() *ShellDriver {
	return &ShellDriver{
		defaultTimeout: DefaultTimeout,
		cmdBuilder:     defaultCommandBuilder,
	}
}

// NewDriverWithOptions creates a ShellDriver with custom configuration.
func NewDriverWithOptions(opts DriverOpts) *ShellDriver {
	d := &ShellDriver{
		defaultTimeout: opts.Timeout,
		cmdBuilder:     opts.CmdBuilder,
	}
	if d.defaultTimeout <= 0 {
		d.defaultTimeout = DefaultTimeout
	}
	if d.cmdBuilder == nil {
		d.cmdBuilder = defaultCommandBuilder
	}
	return d
}

// ShellFile implements vfs.VFSFile for shell command execution via write-then-read semantics.
type ShellFile struct {
	driver     *ShellDriver
	devicePath string
	workDir    string
	response   []byte
	offset     int
	closed     bool
}

// Write accepts a shell command string, executes it, and buffers the output.
func (f *ShellFile) Write(ctx context.Context, data []byte) error {
	if f.closed {
		return &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("shell file closed"), Code: types.ErrDriver}
	}

	command := string(data)
	if command == "" {
		return &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("empty command"), Code: types.ErrDriver}
	}

	ctx, cancel := context.WithTimeout(ctx, f.driver.defaultTimeout)
	defer cancel()

	cmd := f.driver.cmdBuilder(ctx, "sh", "-c", command)
	if f.workDir != "" {
		cmd.Dir = f.workDir
	}

	var combined bytes.Buffer
	cmd.Stdout = &combined
	cmd.Stderr = &combined

	err := cmd.Run()

	if ctx.Err() == context.DeadlineExceeded {
		return &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("command timed out after %v", f.driver.defaultTimeout), Code: types.ErrTimeout}
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			// Non-zero exit code is NOT a driver error — append exit code info to output
			fmt.Fprintf(&combined, "\nexit_code: %d", exitErr.ExitCode())
		} else {
			// Non-ExitError failures (command not found, I/O errors) are driver errors
			return &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("command execution failed: %w", err), Code: types.ErrDriver}
		}
	}

	f.response = combined.Bytes()
	f.offset = 0
	return nil
}

// Read returns buffered command output up to the requested length.
func (f *ShellFile) Read(length int) ([]byte, error) {
	if f.closed {
		return nil, &types.DriverError{Op: "Read", Device: f.devicePath, Err: fmt.Errorf("shell file closed"), Code: types.ErrDriver}
	}
	if f.response == nil {
		return nil, &types.DriverError{Op: "Read", Device: f.devicePath, Err: fmt.Errorf("no output available: write a command first"), Code: types.ErrDriver}
	}

	remaining := f.response[f.offset:]
	if len(remaining) == 0 {
		return nil, nil
	}

	if length <= 0 || length > len(remaining) {
		length = len(remaining)
	}

	data := make([]byte, length)
	copy(data, remaining[:length])
	f.offset += length
	return data, nil
}

// Close marks the file as closed and releases buffers.
func (f *ShellFile) Close() error {
	if f.closed {
		return &types.DriverError{Op: "Close", Device: f.devicePath, Err: fmt.Errorf("shell file already closed"), Code: types.ErrDriver}
	}
	f.closed = true
	f.response = nil
	f.offset = 0
	return nil
}

// Stat returns metadata about this shell device file.
func (f *ShellFile) Stat() (vfs.FileStat, error) {
	if f.closed {
		return vfs.FileStat{}, &types.DriverError{Op: "Stat", Device: f.devicePath, Err: fmt.Errorf("shell file closed"), Code: types.ErrDriver}
	}
	return vfs.FileStat{
		Name:       f.devicePath,
		Size:       int64(len(f.response)),
		IsDevice:   true,
		DevicePath: "/dev/shell",
	}, nil
}

// FileFactory returns a VFSFileFactory that creates ShellFile instances for the given driver.
// basePath is the device mount path (e.g., "/dev/shell").
func FileFactory(driver *ShellDriver, basePath string) vfs.VFSFileFactory {
	return func(subpath string, flags vfs.OpenFlag, workDir string) (vfs.VFSFile, error) {
		return &ShellFile{
			driver:     driver,
			devicePath: basePath + subpath,
			workDir:    workDir,
		}, nil
	}
}
