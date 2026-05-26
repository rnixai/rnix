package shell

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// TestHelperProcess is a mock process used by tests to simulate shell commands.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_TEST_PROCESS") != "1" {
		return
	}
	switch os.Getenv("GO_TEST_CASE") {
	case "echo_hello":
		fmt.Fprint(os.Stdout, "hello world")
	case "echo_long":
		fmt.Fprint(os.Stdout, "abcdefghijklmnopqrstuvwxyz")
	case "exit1":
		fmt.Fprint(os.Stdout, "some output")
		os.Exit(1)
	case "timeout":
		time.Sleep(5 * time.Second)
	case "env_home":
		fmt.Fprint(os.Stdout, os.Getenv("HOME"))
	}
	os.Exit(0)
}

func mockCmdBuilder(testCase string) CommandBuilder {
	return func(ctx context.Context, name string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=TestHelperProcess", "--"}
		cs = append(cs, args...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = append(os.Environ(), "GO_TEST_PROCESS=1", "GO_TEST_CASE="+testCase)
		return cmd
	}
}

// Task 2.3: TestShellFile_WriteAndRead_Success
func TestShellFile_WriteAndRead_Success(t *testing.T) {
	driver := NewDriverWithOptions(DriverOpts{
		CmdBuilder: mockCmdBuilder("echo_hello"),
	})
	f := &ShellFile{driver: driver, devicePath: "/dev/shell"}

	err := f.Write(context.Background(), []byte("echo hello"))
	if err != nil {
		t.Fatalf("unexpected Write error: %v", err)
	}

	data, err := f.Read(0)
	if err != nil {
		t.Fatalf("unexpected Read error: %v", err)
	}
	if string(data) != "hello world" {
		t.Errorf("expected 'hello world', got %q", string(data))
	}
}

// Task 2.4: TestShellFile_Write_EmptyCommand
func TestShellFile_Write_EmptyCommand(t *testing.T) {
	driver := NewDriver()
	f := &ShellFile{driver: driver, devicePath: "/dev/shell"}

	err := f.Write(context.Background(), []byte(""))
	if err == nil {
		t.Fatal("expected error for empty command, got nil")
	}

	var drvErr *types.DriverError
	if !errors.As(err, &drvErr) {
		t.Fatalf("expected *types.DriverError, got %T: %v", err, err)
	}
	if drvErr.Code != types.ErrDriver {
		t.Errorf("expected ErrDriver code, got %q", drvErr.Code)
	}
}

// Task 2.5: TestShellFile_Write_Timeout
func TestShellFile_Write_Timeout(t *testing.T) {
	driver := NewDriverWithOptions(DriverOpts{
		Timeout:    200 * time.Millisecond,
		CmdBuilder: mockCmdBuilder("timeout"),
	})
	f := &ShellFile{driver: driver, devicePath: "/dev/shell"}

	err := f.Write(context.Background(), []byte("sleep 10"))
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}

	var drvErr *types.DriverError
	if !errors.As(err, &drvErr) {
		t.Fatalf("expected *types.DriverError, got %T: %v", err, err)
	}
	if drvErr.Code != types.ErrTimeout {
		t.Errorf("expected ErrTimeout code, got %q", drvErr.Code)
	}
}

// Task 2.6: TestShellFile_Read_BeforeWrite
func TestShellFile_Read_BeforeWrite(t *testing.T) {
	driver := NewDriver()
	f := &ShellFile{driver: driver, devicePath: "/dev/shell"}

	_, err := f.Read(0)
	if err == nil {
		t.Fatal("expected error for read before write, got nil")
	}

	var drvErr *types.DriverError
	if !errors.As(err, &drvErr) {
		t.Fatalf("expected *types.DriverError, got %T: %v", err, err)
	}
	if !strings.Contains(drvErr.Err.Error(), "no output available") {
		t.Errorf("expected 'no output available' message, got: %v", drvErr.Err)
	}
}

// Task 2.7: TestShellFile_Read_PartialLength
func TestShellFile_Read_PartialLength(t *testing.T) {
	driver := NewDriverWithOptions(DriverOpts{
		CmdBuilder: mockCmdBuilder("echo_long"),
	})
	f := &ShellFile{driver: driver, devicePath: "/dev/shell"}

	err := f.Write(context.Background(), []byte("echo"))
	if err != nil {
		t.Fatalf("unexpected Write error: %v", err)
	}

	// Read first 5 bytes
	data, err := f.Read(5)
	if err != nil {
		t.Fatalf("unexpected Read error: %v", err)
	}
	if string(data) != "abcde" {
		t.Errorf("expected 'abcde', got %q", string(data))
	}

	// Read next 5 bytes
	data, err = f.Read(5)
	if err != nil {
		t.Fatalf("unexpected Read error: %v", err)
	}
	if string(data) != "fghij" {
		t.Errorf("expected 'fghij', got %q", string(data))
	}

	// Read remaining
	data, err = f.Read(0)
	if err != nil {
		t.Fatalf("unexpected Read error: %v", err)
	}
	if string(data) != "klmnopqrstuvwxyz" {
		t.Errorf("expected 'klmnopqrstuvwxyz', got %q", string(data))
	}
}

// Task 2.8: TestShellFile_Close_DoubleClose
func TestShellFile_Close_DoubleClose(t *testing.T) {
	driver := NewDriver()
	f := &ShellFile{driver: driver, devicePath: "/dev/shell"}

	err := f.Close()
	if err != nil {
		t.Fatalf("unexpected first Close error: %v", err)
	}

	err = f.Close()
	if err == nil {
		t.Fatal("expected error for double close, got nil")
	}

	var drvErr *types.DriverError
	if !errors.As(err, &drvErr) {
		t.Fatalf("expected *types.DriverError, got %T: %v", err, err)
	}
}

// Task 2.9: TestShellFile_Write_AfterClose
func TestShellFile_Write_AfterClose(t *testing.T) {
	driver := NewDriver()
	f := &ShellFile{driver: driver, devicePath: "/dev/shell"}
	_ = f.Close()

	err := f.Write(context.Background(), []byte("echo test"))
	if err == nil {
		t.Fatal("expected error for write after close, got nil")
	}

	var drvErr *types.DriverError
	if !errors.As(err, &drvErr) {
		t.Fatalf("expected *types.DriverError, got %T: %v", err, err)
	}
	if drvErr.Code != types.ErrDriver {
		t.Errorf("expected ErrDriver code, got %q", drvErr.Code)
	}
}

// Task 2.10: TestShellFile_Read_AfterClose
func TestShellFile_Read_AfterClose(t *testing.T) {
	driver := NewDriver()
	f := &ShellFile{driver: driver, devicePath: "/dev/shell"}
	_ = f.Close()

	_, err := f.Read(0)
	if err == nil {
		t.Fatal("expected error for read after close, got nil")
	}

	var drvErr *types.DriverError
	if !errors.As(err, &drvErr) {
		t.Fatalf("expected *types.DriverError, got %T: %v", err, err)
	}
	if drvErr.Code != types.ErrDriver {
		t.Errorf("expected ErrDriver code, got %q", drvErr.Code)
	}
}

// Task 2.11: TestShellFile_Stat
func TestShellFile_Stat(t *testing.T) {
	driver := NewDriverWithOptions(DriverOpts{
		CmdBuilder: mockCmdBuilder("echo_hello"),
	})
	f := &ShellFile{driver: driver, devicePath: "/dev/shell"}

	// Stat before write — size should be 0
	stat, err := f.Stat()
	if err != nil {
		t.Fatalf("unexpected Stat error: %v", err)
	}
	if stat.Name != "/dev/shell" {
		t.Errorf("expected Name '/dev/shell', got %q", stat.Name)
	}
	if stat.Size != 0 {
		t.Errorf("expected Size 0 before write, got %d", stat.Size)
	}
	if !stat.IsDevice {
		t.Error("expected IsDevice true")
	}
	if stat.DevicePath != "/dev/shell" {
		t.Errorf("expected DevicePath '/dev/shell', got %q", stat.DevicePath)
	}

	// Write and check size
	_ = f.Write(context.Background(), []byte("echo hello"))
	stat, err = f.Stat()
	if err != nil {
		t.Fatalf("unexpected Stat error after write: %v", err)
	}
	if stat.Size != int64(len("hello world")) {
		t.Errorf("expected Size %d after write, got %d", len("hello world"), stat.Size)
	}
}

// Task 2.12: TestShellFile_Stat_AfterClose
func TestShellFile_Stat_AfterClose(t *testing.T) {
	driver := NewDriver()
	f := &ShellFile{driver: driver, devicePath: "/dev/shell"}
	_ = f.Close()

	_, err := f.Stat()
	if err == nil {
		t.Fatal("expected error for stat after close, got nil")
	}

	var drvErr *types.DriverError
	if !errors.As(err, &drvErr) {
		t.Fatalf("expected *types.DriverError, got %T: %v", err, err)
	}
	if drvErr.Code != types.ErrDriver {
		t.Errorf("expected ErrDriver code, got %q", drvErr.Code)
	}
}

// Task 2.13: TestShellFile_Write_NonZeroExitCode
func TestShellFile_Write_NonZeroExitCode(t *testing.T) {
	driver := NewDriverWithOptions(DriverOpts{
		CmdBuilder: mockCmdBuilder("exit1"),
	})
	f := &ShellFile{driver: driver, devicePath: "/dev/shell"}

	err := f.Write(context.Background(), []byte("false"))
	if err != nil {
		t.Fatalf("non-zero exit code should NOT be a driver error, got: %v", err)
	}

	data, err := f.Read(0)
	if err != nil {
		t.Fatalf("unexpected Read error: %v", err)
	}
	output := string(data)
	if !strings.Contains(output, "some output") {
		t.Errorf("expected output to contain 'some output', got %q", output)
	}
	if !strings.Contains(output, "exit_code: 1") {
		t.Errorf("expected output to contain exit code info, got %q", output)
	}
}

// Task 2.14: TestShellDriver_InheritsEnv
func TestShellDriver_InheritsEnv(t *testing.T) {
	driver := NewDriverWithOptions(DriverOpts{
		CmdBuilder: mockCmdBuilder("env_home"),
	})
	f := &ShellFile{driver: driver, devicePath: "/dev/shell"}

	err := f.Write(context.Background(), []byte("echo $HOME"))
	if err != nil {
		t.Fatalf("unexpected Write error: %v", err)
	}

	data, err := f.Read(0)
	if err != nil {
		t.Fatalf("unexpected Read error: %v", err)
	}
	// The mock echoes os.Getenv("HOME"), which should be non-empty
	home := os.Getenv("HOME")
	if string(data) != home {
		t.Errorf("expected HOME=%q, got %q", home, string(data))
	}
}

// CR-M2: TestDefaultCommandBuilder_InheritsEnv verifies the production CommandBuilder
// creates a Cmd with nil Env, which is Go's contract for inheriting parent environment (NFR14).
func TestDefaultCommandBuilder_InheritsEnv(t *testing.T) {
	cmd := defaultCommandBuilder(context.Background(), "echo", "test")
	if cmd.Env != nil {
		t.Errorf("expected Cmd.Env to be nil (inherits parent env), got %v", cmd.Env)
	}
}

// CR-M3: TestShellFile_Read_EOF verifies Read returns nil,nil after all output is consumed.
func TestShellFile_Read_EOF(t *testing.T) {
	driver := NewDriverWithOptions(DriverOpts{
		CmdBuilder: mockCmdBuilder("echo_hello"),
	})
	f := &ShellFile{driver: driver, devicePath: "/dev/shell"}

	err := f.Write(context.Background(), []byte("echo hello"))
	if err != nil {
		t.Fatalf("unexpected Write error: %v", err)
	}

	// Consume all output
	_, err = f.Read(0)
	if err != nil {
		t.Fatalf("unexpected Read error: %v", err)
	}

	// Subsequent Read should return nil, nil (EOF)
	data, err := f.Read(0)
	if err != nil {
		t.Fatalf("expected nil error on EOF, got: %v", err)
	}
	if data != nil {
		t.Errorf("expected nil data on EOF, got %q", string(data))
	}
}

// CR-H1: TestShellFile_Write_CmdExecutionFailure verifies non-ExitError failures
// return *types.DriverError instead of being silently ignored.
func TestShellFile_Write_CmdExecutionFailure(t *testing.T) {
	// Create a CommandBuilder that returns a command for a non-existent binary
	failBuilder := func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "/nonexistent/binary/path")
	}
	driver := NewDriverWithOptions(DriverOpts{
		CmdBuilder: failBuilder,
	})
	f := &ShellFile{driver: driver, devicePath: "/dev/shell"}

	err := f.Write(context.Background(), []byte("anything"))
	if err == nil {
		t.Fatal("expected error for command execution failure, got nil")
	}

	var drvErr *types.DriverError
	if !errors.As(err, &drvErr) {
		t.Fatalf("expected *types.DriverError, got %T: %v", err, err)
	}
	if drvErr.Code != types.ErrDriver {
		t.Errorf("expected ErrDriver code, got %q", drvErr.Code)
	}
}

// TestShellFile_Write_JSONCommand verifies Write accepts JSON {"command": "..."} format
// from kernel ToolData (the format LLM tool calls produce).
func TestShellFile_Write_JSONCommand(t *testing.T) {
	driver := NewDriverWithOptions(DriverOpts{
		CmdBuilder: mockCmdBuilder("echo_hello"),
	})
	f := &ShellFile{driver: driver, devicePath: "/dev/shell"}

	// Write JSON format (as sent by kernel's reasonStep tool_call handler)
	err := f.Write(context.Background(), []byte(`{"command": "echo hello"}`))
	if err != nil {
		t.Fatalf("unexpected Write error: %v", err)
	}

	data, err := f.Read(0)
	if err != nil {
		t.Fatalf("unexpected Read error: %v", err)
	}
	if string(data) != "hello world" {
		t.Errorf("expected 'hello world', got %q", string(data))
	}
}

// TestExtractCommand verifies both JSON and plain string formats.
func TestExtractCommand(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain string", "echo hello", "echo hello"},
		{"JSON object", `{"command": "ls -la"}`, "ls -la"},
		{"JSON empty command", `{"command": ""}`, `{"command": ""}`},
		{"empty string", "", ""},
		{"invalid JSON", `{bad json`, "{bad json"},
		{"JSON without command field", `{"foo": "bar"}`, `{"foo": "bar"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractCommand([]byte(tt.input))
			if got != tt.want {
				t.Errorf("extractCommand(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestShellFile_Write_BackgroundProcessPipeLeak guards against the EchoMatrix
// PID 9 STALL regression: shell command forks a background process that
// inherits cmd.Stdout/cmd.Stderr pipes; main sh exits but grandchild keeps
// pipes open, io.Copy hangs on os.File.Read, Cmd.Wait → awaitGoroutines
// deadlocks the reasonStep goroutine.
//
// Without WaitDelay + Setpgid + Cancel(kill -pgid), Write would hang for the
// duration of the background sleep (here 30s). With the fix, ctx cancellation
// kills the whole process group and Write returns within seconds.
//
// Uses a real exec.Command (not mockCmdBuilder) because the bug is specific
// to how stdlib os/exec handles inherited pipe fds.
func TestShellFile_Write_BackgroundProcessPipeLeak(t *testing.T) {
	driver := NewDriverWithOptions(DriverOpts{
		Timeout: 500 * time.Millisecond,
	})
	f := &ShellFile{driver: driver, devicePath: "/dev/shell"}

	// Background sleep 30s holds stdout/stderr; main sh exits immediately.
	// Pre-fix: Write hangs ≥30s waiting for sleep to release the pipes.
	// Post-fix: ctx timeout (500ms) triggers cmd.Cancel which SIGKILLs the
	// whole pgrp, freeing pipes; Write returns ErrTimeout in <1s.
	// Ceiling 10s = defaultTimeout (500ms) + waitDelay (5s) + headroom for
	// race detector / virtualized CI overhead.
	start := time.Now()
	err := f.Write(context.Background(), []byte("(sleep 30 &); exit 0"))
	elapsed := time.Since(start)

	if elapsed > 10*time.Second {
		t.Fatalf("Write took %v, expected ≤10s (regression: WaitDelay/Setpgid/Cancel missing?)", elapsed)
	}
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}

	var drvErr *types.DriverError
	if !errors.As(err, &drvErr) {
		t.Fatalf("expected *types.DriverError, got %T: %v", err, err)
	}
	if drvErr.Code != types.ErrTimeout {
		t.Errorf("expected ErrTimeout code, got %q (err=%v)", drvErr.Code, drvErr.Err)
	}
}

// Task 2.15: TestFileFactory_ReturnsShellFile
func TestFileFactory_ReturnsShellFile(t *testing.T) {
	driver := NewDriver()
	factory := FileFactory(driver, "/dev/shell")

	// O_RDWR should work
	file, err := factory("", vfs.O_RDWR, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sf, ok := file.(*ShellFile)
	if !ok {
		t.Fatalf("expected *ShellFile, got %T", file)
	}
	if sf.devicePath != "/dev/shell" {
		t.Errorf("expected devicePath '/dev/shell', got %q", sf.devicePath)
	}
	if sf.driver != driver {
		t.Error("expected driver to match")
	}

	// O_RDONLY should also work
	file, err = factory("", vfs.O_RDONLY, "")
	if err != nil {
		t.Fatalf("unexpected error for O_RDONLY: %v", err)
	}
	if file == nil {
		t.Fatal("expected non-nil file for O_RDONLY")
	}

	// O_WRONLY should also work
	file, err = factory("", vfs.O_WRONLY, "")
	if err != nil {
		t.Fatalf("unexpected error for O_WRONLY: %v", err)
	}
	if file == nil {
		t.Fatal("expected non-nil file for O_WRONLY")
	}
}

func TestShellFile_Write_UsesWorkDir(t *testing.T) {
	// Use a real command to verify cmd.Dir is set correctly
	driver := NewDriver()
	factory := FileFactory(driver, "/dev/shell")

	// Create a temp directory to use as workDir
	tmpDir := t.TempDir()

	file, err := factory("", vfs.O_RDWR, tmpDir)
	if err != nil {
		t.Fatalf("FileFactory failed: %v", err)
	}
	defer file.Close()

	// pwd should return the workDir
	ctx := context.Background()
	err = file.Write(ctx, []byte("pwd"))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	data, err := file.Read(0)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	output := strings.TrimSpace(string(data))
	if output != tmpDir {
		t.Errorf("pwd output = %q, want %q", output, tmpDir)
	}
}

func TestShellDriver_ToolDefs(t *testing.T) {
	d := NewDriver()
	defs := d.ToolDefs()

	if len(defs) != 1 {
		t.Fatalf("expected 1 tool def, got %d", len(defs))
	}
	if defs[0].Name != "Bash" {
		t.Fatalf("expected tool name 'Bash', got %q", defs[0].Name)
	}
	if defs[0].Description == "" {
		t.Fatal("expected non-empty description")
	}

	params := defs[0].Parameters
	if params["type"] != "object" {
		t.Fatalf("expected parameters type 'object', got %v", params["type"])
	}
	props, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties map")
	}
	if _, ok := props["command"]; !ok {
		t.Fatal("expected 'command' property")
	}
	req, ok := params["required"].([]string)
	if !ok {
		t.Fatal("expected required to be []string")
	}
	if len(req) != 1 || req[0] != "command" {
		t.Fatalf("expected required=['command'], got %v", req)
	}
}
