// Package shell implements the shell command execution driver for /dev/shell.
package shell

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
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
	// Raised from the original 30s to 120s (Story 57.1 AC2) — 30s reliably
	// killed legitimate long commands such as `go build`/`go vet` on cold
	// caches. 120s aligns with Claude Code's Bash default timeout order of
	// magnitude. Override per-driver via DriverOpts.Timeout (config-wired in
	// cmd/rnix/main.go) or per-call via the Bash tool's optional `timeout`.
	DefaultTimeout = 120 * time.Second
	// maxCommandTimeoutSeconds is the hard ceiling for a per-call `timeout`
	// override (Story 57.1 AC3), expressed in seconds. It is applied in
	// seconds-space *before* multiplying by time.Second — clamping after the
	// multiply cannot undo an int64 overflow (review finding: timeout
	// 18446744074 wraps to +290ms, passes the >0 guard, and slips past the
	// post-multiply clamp, near-instantly killing a legitimate long command).
	maxCommandTimeoutSeconds = 600
	// maxCommandTimeout is the same ceiling as a Duration. Values above it are
	// clamped (not rejected) to keep a single command from holding a worker for
	// an unbounded time.
	maxCommandTimeout = maxCommandTimeoutSeconds * time.Second
	// waitDelay is the grace period after ctx cancellation before stdlib
	// forcibly closes stdin/stdout/stderr to unblock readers (see
	// golang/go#23019). Required because shell commands may fork background
	// daemons that inherit the pipe fds; without WaitDelay, io.Copy on those
	// pipes hangs forever and Cmd.Wait → awaitGoroutines deadlocks the
	// reasonStep goroutine.
	waitDelay = 5 * time.Second
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
					"timeout": map[string]any{
						"type":        "integer",
						"description": "Optional timeout in seconds for this command. Defaults to the driver default; values above the hard ceiling are clamped down. Use a higher value for long builds/tests.",
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
	driver      *ShellDriver
	devicePath  string
	workDir     string
	callerDepth int
	response    []byte
	offset      int
	closed      bool
}

// SetCallerProcessInfo implements vfs.CallerProcessInfoAware.
func (f *ShellFile) SetCallerProcessInfo(_ types.PID, depth int) {
	f.callerDepth = depth
}

// Write accepts a shell command string, executes it, and buffers the output.
func (f *ShellFile) Write(ctx context.Context, data []byte) error {
	if f.closed {
		return &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("shell file closed"), Code: types.ErrDriver}
	}

	command, callTimeout := extractCommand(data)
	if command == "" {
		return &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("empty command"), Code: types.ErrDriver}
	}

	// Safety check: block dangerous commands (Story 32.1 AC#9)
	if err := checkDangerousCommand(command); err != nil {
		return &types.DriverError{Op: "Write", Device: f.devicePath, Err: err, Code: types.ErrPermission}
	}

	// Resolve the effective timeout: per-call override (Story 57.1 AC3) wins
	// over the driver default; values above the hard ceiling are clamped down
	// rather than rejected.
	timeout := f.driver.defaultTimeout
	if callTimeout > 0 {
		timeout = min(callTimeout, maxCommandTimeout)
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := f.driver.cmdBuilder(ctx, "sh", "-c", command)
	if f.workDir != "" {
		cmd.Dir = f.workDir
	}

	if f.callerDepth > 0 {
		if cmd.Env == nil {
			cmd.Env = os.Environ()
		}
		cmd.Env = append(cmd.Env, fmt.Sprintf("RNIX_SPAWN_DEPTH=%d", f.callerDepth))
	}

	// Isolate the child into its own process group so cmd.Cancel can kill
	// the whole tree (including background daemons the command might fork)
	// instead of just the sh entrypoint. Without this, grandchildren that
	// inherit cmd.Stdout/cmd.Stderr keep the pipes open and io.Copy never
	// returns — see waitDelay comment above and golang/go#23019.
	// Implementation is platform-specific (no-op on Windows).
	applyProcessGroupIsolation(cmd)
	cmd.WaitDelay = waitDelay

	var combined bytes.Buffer
	cmd.Stdout = &combined
	cmd.Stderr = &combined

	err := cmd.Run()

	// AC4: a true deadline timeout. Preserve whatever the command already wrote
	// (truncated) so the agent can see where it stalled, append an explicit
	// marker, and surface the partial output inside the DriverError too — the
	// kernel returns on a Write error without ever calling Read, so the error
	// message is the agent's only window into the captured output.
	if ctx.Err() == context.DeadlineExceeded {
		fmt.Fprintf(&combined, "\n[command timed out after %v; partial output above]", timeout)
		f.response = f.finalizeResponse(combined.String())
		f.offset = 0
		return &types.DriverError{
			Op:     "Write",
			Device: f.devicePath,
			Err:    fmt.Errorf("command timed out after %v; partial output:\n%s", timeout, string(f.response)),
			Code:   types.ErrTimeout,
		}
	}

	// Review D1: the parent context was cancelled (process kill / SIGTERM /
	// daemon shutdown) rather than a deadline expiring. Like the timeout path we
	// preserve whatever the command captured so the agent / post-mortem can see
	// how far it got, tag it distinctly, and return a driver error (the command
	// did not complete). Without this the partial output fell through to the
	// generic "command execution failed" branch and was discarded.
	if ctx.Err() == context.Canceled {
		fmt.Fprint(&combined, "\n[command canceled before completion; partial output above]")
		f.response = f.finalizeResponse(combined.String())
		f.offset = 0
		return &types.DriverError{
			Op:     "Write",
			Device: f.devicePath,
			Err:    fmt.Errorf("command canceled before completion; partial output:\n%s", string(f.response)),
			Code:   types.ErrDriver,
		}
	}

	// AC5 (deferred-work #128 / ECH-M2): ErrWaitDelay fired but the context was
	// NOT cancelled — the command itself exited cleanly, only a grandchild kept
	// the inherited pipe open past the WaitDelay grace window. This is not a
	// timeout: keep the captured output and downgrade to a soft warning instead
	// of discarding everything as a hard ErrTimeout (the legacy `||` bug).
	if errors.Is(err, exec.ErrWaitDelay) && ctx.Err() == nil {
		fmt.Fprintf(&combined, "\n[warning: a background process held the output pipe open past the %v grace window; the command itself completed]", waitDelay)
		f.response = f.finalizeResponse(combined.String())
		f.offset = 0
		return nil
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

	f.response = f.finalizeResponse(combined.String())
	f.offset = 0

	return nil
}

// finalizeResponse applies the large-output truncation policy
// (EndTruncatingAccumulator + overflow spill) and returns the bytes to buffer
// as f.response. Shared by the success, timeout (AC4), and ErrWaitDelay (AC5)
// paths so partial output is truncated consistently.
func (f *ShellFile) finalizeResponse(output string) []byte {
	truncated, didTruncate := rnixctx.EndTruncatingAccumulator(output, maxOutputChars, headLines, tailLines)
	if !didTruncate {
		return []byte(output)
	}
	if f.workDir != "" {
		overflowPath, _ := rnixctx.WriteOverflow(output, f.workDir)
		if overflowPath != "" {
			truncated += fmt.Sprintf("\n[Full output saved to %s]", overflowPath)
		}
	}
	return []byte(truncated)
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

// extractCommand extracts the command string and optional per-call timeout
// from data. Accepts both JSON format {"command": "...", "timeout": N} (from
// kernel ToolData) and plain strings. The returned timeout is in seconds; 0
// means "unset — use the driver default" (Story 57.1 AC3, backward compatible:
// a plain string or JSON without a timeout field yields 0).
func extractCommand(data []byte) (string, time.Duration) {
	var req struct {
		Command string `json:"command"`
		Timeout int    `json:"timeout"`
	}
	if json.Unmarshal(data, &req) == nil && req.Command != "" {
		var timeout time.Duration
		if req.Timeout > 0 {
			// Clamp in seconds-space before converting: time.Duration(secs) *
			// time.Second overflows int64 for absurd values and would wrap to a
			// tiny/negative duration that slips past the Write-side clamp.
			secs := min(req.Timeout, maxCommandTimeoutSeconds)
			timeout = time.Duration(secs) * time.Second
		}
		return req.Command, timeout
	}
	return string(data), 0
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
	regexp.MustCompile(`\brnix\s+(apply\b|(-[a-zA-Z]*i))`),               // block recursive rnix apply/intent via shell
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
