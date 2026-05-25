// Package shell implements the shell command execution driver for /dev/shell.
package shell

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

const (
	// DefaultTimeout is the default timeout for shell command execution.
	DefaultTimeout = 30 * time.Second
	// maxOutputChars is the maximum character count before shell output is truncated.
	maxOutputChars = 30000
	// headLines is the number of leading lines to preserve when truncating.
	headLines = 200
	// tailLines is the number of trailing lines to preserve when truncating.
	tailLines = 200
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

// compile-time interface check
var _ vfs.ToolDescriptor = (*ShellDriver)(nil)

// ToolDefs returns the tool definitions for the shell device.
func (d *ShellDriver) ToolDefs() []vfs.ToolDef {
	return []vfs.ToolDef{
		{
			Name:            "Bash",
			Description:     loadPrompt("Bash"),
			MaxResultTokens: 30000,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{
						"type":        "string",
						"description": "Shell command to execute",
					},
				},
				"required": []string{"command"},
			},
		},
	}
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

	command := extractCommand(data)
	if command == "" {
		return &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("empty command"), Code: types.ErrDriver}
	}

	// Safety check: block dangerous commands (Story 32.1 AC#9)
	if err := checkDangerousCommand(command); err != nil {
		return &types.DriverError{Op: "Write", Device: f.devicePath, Err: err, Code: types.ErrPermission}
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

	// Apply EndTruncatingAccumulator for large outputs.
	output := string(f.response)
	truncated, didTruncate := rnixctx.EndTruncatingAccumulator(output, maxOutputChars, headLines, tailLines)
	if didTruncate {
		if f.workDir != "" {
			overflowPath, _ := rnixctx.WriteOverflow(output, f.workDir)
			if overflowPath != "" {
				truncated += fmt.Sprintf("\n[Full output saved to %s]", overflowPath)
			}
		}
		f.response = []byte(truncated)
	}

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

// extractCommand extracts the command string from data.
// Accepts both JSON format {"command": "..."} (from kernel ToolData) and plain strings.
func extractCommand(data []byte) string {
	var req struct {
		Command string `json:"command"`
	}
	if json.Unmarshal(data, &req) == nil && req.Command != "" {
		return req.Command
	}
	return string(data)
}

// dangerousPatterns lists regex patterns for commands that are too dangerous to execute.
// Basic pattern matching only — AST parsing is deferred to Phase 3+ (Decision 35).
var dangerousPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\brm\s+(-[a-zA-Z]*f[a-zA-Z]*\s+)?/\s*$`),        // rm -rf /
	regexp.MustCompile(`\brm\s+(-[a-zA-Z]*r[a-zA-Z]*\s+)+-rf\s+/\*`),    // rm -rf /*
	regexp.MustCompile(`>\s*/dev/sd[a-z]`),                                 // > /dev/sda
	regexp.MustCompile(`\bmkfs\b`),                                         // mkfs
	regexp.MustCompile(`\bdd\b.*\bof=/dev/`),                               // dd of=/dev/
	regexp.MustCompile(`:\(\)\s*\{\s*:\|\s*:\s*&\s*\}\s*;`),               // fork bomb :(){ :|:& };
	regexp.MustCompile(`\bchmod\s+(-[rR]\s+)?777\s+/\s*$`),                // chmod 777 /
	regexp.MustCompile(`\bchmod\s+(-[rR]\s+)?777\s+/etc\b`),               // chmod 777 /etc
	regexp.MustCompile(`\b(halt|poweroff|shutdown|reboot)\b`),              // system power commands
	regexp.MustCompile(`>\s*/etc/(passwd|shadow|sudoers)`),                 // overwrite auth files
	regexp.MustCompile(`\bcurl\b.*\|\s*(ba)?sh`),                           // curl | sh (pipe to shell)
	regexp.MustCompile(`\bwget\b.*\|\s*(ba)?sh`),                           // wget | sh
	regexp.MustCompile(`\bcurl\b.*\|\s*(zsh|python[23]?|perl|ruby)\b`),    // curl | python/perl/ruby/zsh
	regexp.MustCompile(`\bwget\b.*\|\s*(zsh|python[23]?|perl|ruby)\b`),    // wget | python/perl/ruby/zsh
}

// checkDangerousCommand returns an error if the command matches a dangerous pattern.
func checkDangerousCommand(cmd string) error {
	for _, pat := range dangerousPatterns {
		if pat.MatchString(cmd) {
			return fmt.Errorf("dangerous command blocked: matches pattern %q", pat.String())
		}
	}
	return nil
}

// readOnlyPrefixes lists command prefixes that are known to be read-only.
var readOnlyPrefixes = []string{
	"ls", "cat", "head", "tail", "wc", "grep", "egrep", "fgrep",
	"find", "which", "whereis", "whoami", "hostname", "uname",
	"date", "uptime", "df", "du", "free", "ps", "top",
	"echo", "printf", "env", "printenv", "id", "pwd",
	"file", "stat", "readlink", "realpath", "basename", "dirname",
	"diff", "sort", "uniq", "tr", "cut", "awk", "sed -n",
	"git log", "git status", "git diff", "git show", "git branch",
	"git remote", "git tag", "git rev-parse", "git describe",
	"go version", "go list", "go env", "node --version",
	"python --version", "python3 --version", "pip list",
	"npm list", "npm ls", "cargo --version", "rustc --version",
}

// IsReadOnlyCommand returns true if the command appears to be read-only
// based on simple prefix matching. Used for strace event tagging.
func IsReadOnlyCommand(cmd string) bool {
	trimmed := strings.TrimSpace(cmd)
	for _, prefix := range readOnlyPrefixes {
		if trimmed == prefix || strings.HasPrefix(trimmed, prefix+" ") || strings.HasPrefix(trimmed, prefix+"\t") {
			return true
		}
	}
	return false
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
