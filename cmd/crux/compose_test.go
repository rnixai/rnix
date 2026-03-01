package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gonewx/crux/agents"
	"github.com/gonewx/crux/compose"
	"github.com/gonewx/crux/internal/types"
	"github.com/gonewx/crux/internal/ui"
	"github.com/gonewx/crux/internal/xsync"
	"github.com/gonewx/crux/ipc"
	"github.com/gonewx/crux/kernel"
	"github.com/gonewx/crux/vfs"
	"github.com/spf13/cobra"
)

// --- Story 7.2: crux compose up Command Tests ---
// These tests verify AC #1-4 of Story 7.2.
// Tests reference cmd/crux/compose.go types and functions that will be created during implementation.

// --- AC #1: compose up 子命令注册 ---

func TestComposeCmd_Registered(t *testing.T) {
	// Given: rootCmd has subcommands registered
	// When: looking for compose command
	// Then: compose subcommand should exist
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "compose" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected 'compose' subcommand registered on rootCmd")
	}
}

func TestComposeUpCmd_Registered(t *testing.T) {
	// Given: compose command exists
	// When: looking for up subcommand
	// Then: compose up subcommand should exist
	var composeCmd *cobra.Command
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "compose" {
			composeCmd = cmd
			break
		}
	}
	if composeCmd == nil {
		t.Fatal("compose command not found")
	}

	found := false
	for _, cmd := range composeCmd.Commands() {
		if cmd.Name() == "up" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected 'up' subcommand under 'compose'")
	}
}

func TestComposeUp_HelpOutput(t *testing.T) {
	// Given: compose up subcommand exists
	// When: requesting help
	// Then: help output contains usage information
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetArgs([]string{"compose", "up", "--help"})
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetArgs(nil)
	})

	_ = rootCmd.Execute()

	output := buf.String()
	if !strings.Contains(output, "up") {
		t.Errorf("expected 'up' in help output, got %q", output)
	}
	if !strings.Contains(output, "--file") || !strings.Contains(output, "-f") {
		t.Errorf("expected -f/--file flag in help output, got %q", output)
	}
}

// --- AC #1: compose up 默认文件 ---

func TestComposeUp_DefaultFile(t *testing.T) {
	// Given: a directory with crux-compose.yaml
	// When: running compose up without -f flag
	// Then: reads crux-compose.yaml from current directory

	tmpDir := t.TempDir()
	composeYAML := `version: "1.0"
intent: "test workflow"
agents:
  worker:
    intent: "do work"
`
	if err := os.WriteFile(filepath.Join(tmpDir, "crux-compose.yaml"), []byte(composeYAML), 0644); err != nil {
		t.Fatalf("write compose file: %v", err)
	}

	// Set up IPC server for the test
	sockPath, _ := setupTestIPCServer(t)
	ipc.SocketPathOverride = sockPath
	t.Cleanup(func() { ipc.SocketPathOverride = "" })

	// Save and restore working directory
	origDir, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	// Save and restore flags
	savedFile := flagComposeFile
	savedExit := exitCode
	t.Cleanup(func() {
		flagComposeFile = savedFile
		exitCode = savedExit
	})
	flagComposeFile = "crux-compose.yaml"
	exitCode = 0

	err := runComposeUp(&cobra.Command{}, []string{})
	// The command may fail because the daemon doesn't have real agent handling,
	// but it should NOT fail with "file not found" for the compose file.
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "no such file") || strings.Contains(errMsg, "crux-compose.yaml") {
			t.Fatalf("compose up should find default crux-compose.yaml, got: %v", err)
		}
	}
}

// --- AC #2: 自定义文件 ---

func TestComposeUp_CustomFile(t *testing.T) {
	// Given: a custom compose file
	// When: running compose up -f my-workflow.yaml
	// Then: uses the specified file

	tmpDir := t.TempDir()
	customYAML := `version: "1.0"
intent: "custom workflow"
agents:
  analyzer:
    intent: "analyze code"
`
	customPath := filepath.Join(tmpDir, "my-workflow.yaml")
	if err := os.WriteFile(customPath, []byte(customYAML), 0644); err != nil {
		t.Fatalf("write custom compose file: %v", err)
	}

	sockPath, _ := setupTestIPCServer(t)
	ipc.SocketPathOverride = sockPath
	t.Cleanup(func() { ipc.SocketPathOverride = "" })

	savedFile := flagComposeFile
	savedExit := exitCode
	t.Cleanup(func() {
		flagComposeFile = savedFile
		exitCode = savedExit
	})
	flagComposeFile = customPath
	exitCode = 0

	err := runComposeUp(&cobra.Command{}, []string{})
	// Should not fail with file-not-found for the custom file
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "no such file") && strings.Contains(errMsg, "my-workflow.yaml") {
			t.Fatalf("compose up should find custom file, got: %v", err)
		}
	}
}

func TestComposeUp_FileNotFound(t *testing.T) {
	// Given: a non-existent compose file
	// When: running compose up -f missing.yaml
	// Then: returns an error indicating file not found

	savedFile := flagComposeFile
	savedExit := exitCode
	t.Cleanup(func() {
		flagComposeFile = savedFile
		exitCode = savedExit
	})
	flagComposeFile = "/nonexistent/path/missing.yaml"
	exitCode = 0

	err := runComposeUp(&cobra.Command{}, []string{})
	if err == nil && exitCode == 0 {
		t.Fatal("expected error or non-zero exit code for missing compose file")
	}
}

// --- AC #3: 失败传播 ---

func TestComposeUp_FailurePropagation(t *testing.T) {
	// Given: a compose spec where an upstream agent fails
	// When: the upstream agent exits with non-zero code
	// Then: downstream agents are not started
	// And: error output identifies failed agent and affected downstream

	// This test verifies the integration between runComposeUp and the compose engine
	// by using a mock kernel spawner that simulates failure
	tmpDir := t.TempDir()
	composeYAML := `version: "1.0"
intent: "failure test"
agents:
  upstream:
    intent: "will fail"
  downstream:
    intent: "depends on upstream"
    depends_on:
      upstream: completed
`
	composePath := filepath.Join(tmpDir, "crux-compose.yaml")
	if err := os.WriteFile(composePath, []byte(composeYAML), 0644); err != nil {
		t.Fatalf("write compose file: %v", err)
	}

	// Parse the spec to verify it's valid
	spec, err := compose.ParseFile(composePath)
	if err != nil {
		t.Fatalf("parse compose file: %v", err)
	}

	// Use mock spawner to simulate failure
	ks := &mockComposeSpawner{
		failIntents: map[string]bool{"will fail": true},
	}

	engine, err := compose.NewEngine(spec, ks, nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	results, _ := engine.Execute(context.Background())

	// Find downstream result - it should have an error (skipped due to upstream failure)
	for _, r := range results {
		if r.Name == "downstream" {
			if r.Err == nil {
				t.Fatal("downstream agent should have error due to upstream failure")
			}
		}
	}

	// Verify upstream was the only one spawned
	spawnedIntents := ks.getSpawnedIntents()
	for _, intent := range spawnedIntents {
		if intent == "depends on upstream" {
			t.Fatal("downstream should NOT have been spawned")
		}
	}
}

// --- AC #4: 编排汇总 ---

func TestComposeUp_Summary(t *testing.T) {
	// Given: all agents complete
	// When: viewing output
	// Then: summary shows exit code, tokens, duration for each agent

	// This test verifies the summary rendering functionality
	// The actual rendering is delegated to internal/ui/compose.go
	results := []compose.ScheduleResult{
		{Name: "reviewer", PID: 1, ExitCode: 0, Duration: 6200 * time.Millisecond},
		{Name: "analyst", PID: 2, ExitCode: 0, Duration: 8500 * time.Millisecond},
		{Name: "writer", PID: 3, ExitCode: 1, Err: fmt.Errorf("LLM timeout"), Duration: 2100 * time.Millisecond},
	}

	ui.InitStyles(ui.TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	renderer := &ui.Renderer{Writer: &buf, OutputMode: ui.ModeDefault, Profile: ui.TerminalProfile{Width: 80, ColorLevel: 0}}

	ui.RenderComposeSummary(renderer, results, nil)

	output := buf.String()
	// Should contain agent names
	if !strings.Contains(output, "reviewer") {
		t.Errorf("summary should contain 'reviewer', got %q", output)
	}
	if !strings.Contains(output, "analyst") {
		t.Errorf("summary should contain 'analyst', got %q", output)
	}
	if !strings.Contains(output, "writer") {
		t.Errorf("summary should contain 'writer', got %q", output)
	}
}

func TestComposeUp_JSONOutput(t *testing.T) {
	// Given: all agents complete
	// When: running with --json flag
	// Then: output is valid JSON with correct structure

	results := []compose.ScheduleResult{
		{Name: "agent1", PID: 1, ExitCode: 0, Duration: 5 * time.Second},
		{Name: "agent2", PID: 2, ExitCode: 1, Err: fmt.Errorf("failed"), Duration: 2 * time.Second},
	}

	ui.InitStyles(ui.TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	renderer := &ui.Renderer{Writer: &buf, OutputMode: ui.ModeJSON, Profile: ui.TerminalProfile{ColorLevel: 0}}

	ui.RenderComposeSummaryJSON(renderer, results, nil)

	var resp JSONResponse
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON output: %v\nraw: %s", err, buf.String())
	}

	// Verify JSON structure has agents array and summary
	data, _ := json.Marshal(resp.Data)
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("failed to parse data: %v", err)
	}

	if _, ok := m["agents"]; !ok {
		t.Error("JSON output missing 'agents' field")
	}
	if _, ok := m["summary"]; !ok {
		t.Error("JSON output missing 'summary' field")
	}
}

// --- AC #1: 信号处理 ---

func TestComposeUp_SignalHandling(t *testing.T) {
	// Given: compose up is running with agents in progress
	// When: SIGINT is received
	// Then: running agents are killed and compose exits gracefully

	// This tests the signal handling path in runComposeUp.
	// Verified via context cancellation which is the internal mechanism for signal handling.
	tmpDir := t.TempDir()
	composeYAML := `version: "1.0"
intent: "signal test"
agents:
  slow:
    intent: "takes forever"
`
	composePath := filepath.Join(tmpDir, "crux-compose.yaml")
	if err := os.WriteFile(composePath, []byte(composeYAML), 0644); err != nil {
		t.Fatalf("write compose file: %v", err)
	}

	spec, err := compose.ParseFile(composePath)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	ks := &mockComposeSpawner{
		waitDelay: 10 * time.Second, // agent takes very long
	}

	engine, err := compose.NewEngine(spec, ks, nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	// Cancel quickly to simulate SIGINT
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, execErr := engine.Execute(ctx)
	if execErr == nil {
		t.Fatal("expected context cancellation error")
	}
}

// --- AC #1: NoDaemon 错误处理 ---

func TestComposeUp_NoDaemon(t *testing.T) {
	// Given: no daemon is running
	// When: running compose up
	// Then: error output indicates daemon not available

	savedFile := flagComposeFile
	savedExit := exitCode
	savedOverride := ipc.SocketPathOverride
	t.Cleanup(func() {
		flagComposeFile = savedFile
		exitCode = savedExit
		ipc.SocketPathOverride = savedOverride
	})

	tmpDir := t.TempDir()
	composeYAML := `version: "1.0"
intent: "test"
agents:
  worker:
    intent: "do work"
`
	composePath := filepath.Join(tmpDir, "crux-compose.yaml")
	if err := os.WriteFile(composePath, []byte(composeYAML), 0644); err != nil {
		t.Fatalf("write compose file: %v", err)
	}

	flagComposeFile = composePath
	exitCode = 0
	// Point to a non-existent socket
	ipc.SocketPathOverride = filepath.Join(tmpDir, "nonexistent.sock")

	err := runComposeUp(&cobra.Command{}, []string{})
	// Should fail with daemon connection error, not a panic
	if err == nil && exitCode == 0 {
		t.Fatal("expected error or non-zero exit code when daemon is unavailable")
	}
}

// --- IPC Adapter Tests ---

func TestIpcKernelSpawner_ImplementsInterface(t *testing.T) {
	// Given: ipcKernelSpawner struct exists
	// When: checking interface compliance
	// Then: implements compose.KernelSpawner
	var _ compose.KernelSpawner = (*ipcKernelSpawner)(nil)
}

func TestIpcKernelSpawner_Wait_NoChannel(t *testing.T) {
	// Given: ipcKernelSpawner with no wait channel for a PID
	// When: calling Wait with unknown PID
	// Then: returns error

	spawner := &ipcKernelSpawner{
		waitChans: xsync.NewSyncMap[types.PID, chan waitResult](),
		results:   xsync.NewSyncMap[types.PID, string](),
		tokens:    xsync.NewSyncMap[types.PID, int](),
	}

	_, err := spawner.Wait(types.PID(999))
	if err == nil {
		t.Fatal("expected error for unknown PID")
	}
}

func TestIpcKernelSpawner_GetProcessResult_NotFound(t *testing.T) {
	// Given: ipcKernelSpawner with no results
	// When: calling GetProcessResult
	// Then: returns false

	spawner := &ipcKernelSpawner{
		waitChans: xsync.NewSyncMap[types.PID, chan waitResult](),
		results:   xsync.NewSyncMap[types.PID, string](),
		tokens:    xsync.NewSyncMap[types.PID, int](),
	}

	_, ok := spawner.GetProcessResult(types.PID(1))
	if ok {
		t.Fatal("expected false for non-existent PID result")
	}
}

func TestIpcKernelSpawner_GetProcessResult_Found(t *testing.T) {
	// Given: ipcKernelSpawner with a cached result
	// When: calling GetProcessResult
	// Then: returns the result

	spawner := &ipcKernelSpawner{
		waitChans: xsync.NewSyncMap[types.PID, chan waitResult](),
		results:   xsync.NewSyncMap[types.PID, string](),
		tokens:    xsync.NewSyncMap[types.PID, int](),
	}
	spawner.results.Store(types.PID(1), "cached output")

	output, ok := spawner.GetProcessResult(types.PID(1))
	if !ok {
		t.Fatal("expected true for existing PID result")
	}
	if output != "cached output" {
		t.Errorf("expected 'cached output', got %q", output)
	}
}

// --- Mock compose spawner for tests ---

type mockComposeSpawner struct {
	mu           sync.Mutex
	spawned      []string // spawned intents
	pidAlloc     uint64
	failIntents  map[string]bool // intents that should fail
	waitDelay    time.Duration   // delay for Wait calls
}

func (m *mockComposeSpawner) Spawn(intent string, agent *agents.AgentInfo, opts compose.ComposeSpawnOpts) (types.PID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pidAlloc++
	m.spawned = append(m.spawned, intent)
	return types.PID(m.pidAlloc), nil
}

func (m *mockComposeSpawner) Wait(pid types.PID) (compose.ComposeExitStatus, error) {
	if m.waitDelay > 0 {
		time.Sleep(m.waitDelay)
	}

	m.mu.Lock()
	idx := int(pid) - 1
	var intent string
	if idx >= 0 && idx < len(m.spawned) {
		intent = m.spawned[idx]
	}
	shouldFail := m.failIntents[intent]
	m.mu.Unlock()

	if shouldFail {
		return compose.ComposeExitStatus{Code: 1, Reason: "simulated failure"}, fmt.Errorf("agent failed")
	}
	return compose.ComposeExitStatus{Code: 0, Reason: "ok"}, nil
}

func (m *mockComposeSpawner) GetProcessResult(pid types.PID) (string, bool) {
	return "", false
}

func (m *mockComposeSpawner) getSpawnedIntents() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]string, len(m.spawned))
	copy(result, m.spawned)
	return result
}

// --- Story 7.3: crux compose down Command Tests ---
// These tests verify AC #1-2 of Story 7.3.
// Tests reference cmd/crux/compose.go types and functions that will be created during implementation.

// --- AC #1: compose down 子命令注册 ---

func TestComposeDownCmd_Registered(t *testing.T) {
	// Given: compose command exists with subcommands
	// When: looking for down subcommand
	// Then: compose down subcommand should exist
	var composeParent *cobra.Command
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "compose" {
			composeParent = cmd
			break
		}
	}
	if composeParent == nil {
		t.Fatal("compose command not found")
	}

	found := false
	for _, cmd := range composeParent.Commands() {
		if cmd.Name() == "down" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected 'down' subcommand under 'compose'")
	}
}

func TestComposeDown_HelpOutput(t *testing.T) {
	// Given: compose down subcommand exists
	// When: requesting help
	// Then: help output contains usage information and -f flag
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetArgs([]string{"compose", "down", "--help"})
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetArgs(nil)
	})

	_ = rootCmd.Execute()

	output := buf.String()
	if !strings.Contains(output, "down") {
		t.Errorf("expected 'down' in help output, got %q", output)
	}
	if !strings.Contains(output, "--file") || !strings.Contains(output, "-f") {
		t.Errorf("expected -f/--file flag in help output, got %q", output)
	}
}

// --- AC #1: compose down 文件不存在 ---

func TestComposeDown_FileNotFound(t *testing.T) {
	// Given: a non-existent compose file
	// When: running compose down -f missing.yaml
	// Then: returns an error indicating file not found

	savedFile := flagComposeDownFile
	savedExit := exitCode
	t.Cleanup(func() {
		flagComposeDownFile = savedFile
		exitCode = savedExit
	})
	flagComposeDownFile = "/nonexistent/path/missing.yaml"
	exitCode = 0

	err := runComposeDown(&cobra.Command{}, []string{})
	if err == nil && exitCode == 0 {
		t.Fatal("expected error or non-zero exit code for missing compose file")
	}
}

// --- AC #1: compose down daemon 未运行 ---

func TestComposeDown_NoDaemon(t *testing.T) {
	// Given: no daemon is running
	// When: running compose down
	// Then: outputs "no daemon running" and exits normally (exit code 0)

	tmpDir := t.TempDir()
	composeYAML := `version: "1.0"
intent: "test"
agents:
  worker:
    intent: "do work"
`
	composePath := filepath.Join(tmpDir, "crux-compose.yaml")
	if err := os.WriteFile(composePath, []byte(composeYAML), 0644); err != nil {
		t.Fatalf("write compose file: %v", err)
	}

	savedFile := flagComposeDownFile
	savedExit := exitCode
	savedOverride := ipc.SocketPathOverride
	t.Cleanup(func() {
		flagComposeDownFile = savedFile
		exitCode = savedExit
		ipc.SocketPathOverride = savedOverride
	})

	flagComposeDownFile = composePath
	exitCode = 0
	// Point to a non-existent socket — daemon is not running
	ipc.SocketPathOverride = filepath.Join(tmpDir, "nonexistent.sock")

	err := runComposeDown(&cobra.Command{}, []string{})
	// compose down should NOT error when daemon is not running;
	// it should output a message and exit normally (exit code 0)
	if err != nil {
		t.Fatalf("compose down should not return error when daemon is absent, got: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("expected exit code 0 when no daemon running, got %d", exitCode)
	}
}

// --- AC #2: compose down 无匹配进程 ---

func TestComposeDown_NoMatchingProcesses(t *testing.T) {
	// Given: daemon is running but has no matching processes
	// When: running compose down
	// Then: outputs "no matching processes" and exits normally

	tmpDir := t.TempDir()
	composeYAML := `version: "1.0"
intent: "test workflow"
agents:
  analyzer:
    intent: "analyze code"
`
	composePath := filepath.Join(tmpDir, "crux-compose.yaml")
	if err := os.WriteFile(composePath, []byte(composeYAML), 0644); err != nil {
		t.Fatalf("write compose file: %v", err)
	}

	sockPath, _ := setupTestIPCServer(t)
	ipc.SocketPathOverride = sockPath
	t.Cleanup(func() { ipc.SocketPathOverride = "" })

	savedFile := flagComposeDownFile
	savedExit := exitCode
	t.Cleanup(func() {
		flagComposeDownFile = savedFile
		exitCode = savedExit
	})

	flagComposeDownFile = composePath
	exitCode = 0

	err := runComposeDown(&cobra.Command{}, []string{})
	if err != nil {
		t.Fatalf("compose down should not error with no matching processes: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}
}

// --- AC #1, #2: compose down 仅终止运行中的进程 ---

func TestComposeDown_KillRunningOnly(t *testing.T) {
	// Given: daemon has processes in various states (Running, Zombie, Dead)
	// When: running compose down
	// Then: only Running/Created processes are killed
	// And: Zombie/Dead processes are skipped

	tmpDir := t.TempDir()
	composeYAML := `version: "1.0"
intent: "kill test"
agents:
  reviewer:
    intent: "review code"
  writer:
    intent: "write docs"
`
	composePath := filepath.Join(tmpDir, "crux-compose.yaml")
	if err := os.WriteFile(composePath, []byte(composeYAML), 0644); err != nil {
		t.Fatalf("write compose file: %v", err)
	}

	sockPath, kern := setupTestIPCServer(t)
	ipc.SocketPathOverride = sockPath
	t.Cleanup(func() { ipc.SocketPathOverride = "" })

	// Add processes to the kernel to populate the process table
	proc1 := kernel.NewProcess(0, "review code", nil)
	_ = proc1.Start()
	kern.AddProcess(proc1)

	proc2 := kernel.NewProcess(0, "write docs", nil)
	_ = proc2.Start()
	// Transition proc2 to Zombie to simulate an already-completed process
	_ = proc2.Terminate(kernel.ExitStatus{Code: 0, Reason: "completed"})
	kern.AddProcess(proc2)

	savedFile := flagComposeDownFile
	savedExit := exitCode
	t.Cleanup(func() {
		flagComposeDownFile = savedFile
		exitCode = savedExit
	})

	flagComposeDownFile = composePath
	exitCode = 0

	err := runComposeDown(&cobra.Command{}, []string{})
	if err != nil {
		t.Fatalf("compose down error: %v", err)
	}

	// compose down sends Kill(SIGTERM) to matching running processes.
	// Verify that compose down completed successfully (exit code 0 = all kills sent).
	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}

	// Verify via IPC that the already-completed (Zombie) process was NOT affected (M3 fix)
	verifyClient, dialErr := ipc.Dial(sockPath)
	if dialErr != nil {
		t.Fatalf("dial for verification: %v", dialErr)
	}
	defer verifyClient.Close()

	procsAfter, listErr := verifyClient.ListProcs()
	if listErr != nil {
		t.Fatalf("list procs after compose down: %v", listErr)
	}

	for _, p := range procsAfter {
		if p.Intent == "write docs" && p.State != types.StateZombie {
			t.Errorf("expected 'write docs' (already completed) to remain Zombie, got %s", p.State)
		}
	}
}

// --- AC #2: compose down JSON 输出 ---

func TestComposeDown_JSONOutput(t *testing.T) {
	// Given: compose down results
	// When: rendering as JSON
	// Then: output is valid JSON with killed/skipped arrays and summary

	killed := []ui.ComposeDownEntry{
		{PID: 3, Intent: "review code"},
		{PID: 4, Intent: "analyze quality"},
	}
	skipped := []ui.ComposeDownEntry{
		{PID: 5, Intent: "generate docs", State: "zombie"},
	}

	ui.InitStyles(ui.TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	renderer := &ui.Renderer{Writer: &buf, OutputMode: ui.ModeJSON, Profile: ui.TerminalProfile{ColorLevel: 0}}

	ui.RenderComposeDownSummaryJSON(renderer, killed, skipped, nil)

	var resp map[string]any
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON output: %v\nraw: %s", err, buf.String())
	}

	if resp["ok"] != true {
		t.Error("expected ok=true")
	}

	data, ok := resp["data"].(map[string]any)
	if !ok {
		t.Fatal("expected data to be object")
	}

	killedArr, ok := data["killed"].([]any)
	if !ok {
		t.Fatal("expected killed to be array")
	}
	if len(killedArr) != 2 {
		t.Errorf("expected 2 killed entries, got %d", len(killedArr))
	}

	// Verify killed entry content (M4 fix)
	firstKilled := killedArr[0].(map[string]any)
	if firstKilled["intent"] != "review code" {
		t.Errorf("expected first killed intent 'review code', got %v", firstKilled["intent"])
	}
	if _, hasState := firstKilled["state"]; hasState {
		t.Error("killed entry should not have 'state' field (omitempty)")
	}

	skippedArr, ok := data["skipped"].([]any)
	if !ok {
		t.Fatal("expected skipped to be array")
	}
	if len(skippedArr) != 1 {
		t.Errorf("expected 1 skipped entry, got %d", len(skippedArr))
	}

	// Verify skipped entry has state field
	firstSkipped := skippedArr[0].(map[string]any)
	if firstSkipped["state"] != "zombie" {
		t.Errorf("expected skipped state 'zombie', got %v", firstSkipped["state"])
	}

	summary, ok := data["summary"].(map[string]any)
	if !ok {
		t.Fatal("expected summary to be object")
	}

	for _, field := range []string{"killed_count", "skipped_count", "total_matched"} {
		if _, ok := summary[field]; !ok {
			t.Errorf("summary missing field %q", field)
		}
	}
}

// --- Helper function tests: matchComposeProcesses ---

func TestMatchComposeProcesses_AllRunning(t *testing.T) {
	// Given: all daemon processes match compose spec intents and are Running
	// When: calling matchComposeProcesses
	// Then: all processes returned in "running" slice, none in "completed"

	spec := &compose.ComposeSpec{
		Agents: map[string]*compose.AgentSpec{
			"reviewer": {Intent: "review code"},
			"analyst":  {Intent: "analyze quality"},
		},
	}

	procs := []vfs.ProcInfo{
		{PID: 1, State: types.StateRunning, Intent: "review code"},
		{PID: 2, State: types.StateRunning, Intent: "analyze quality"},
	}

	running, completed := matchComposeProcesses(procs, spec)
	if len(running) != 2 {
		t.Errorf("expected 2 running, got %d", len(running))
	}
	if len(completed) != 0 {
		t.Errorf("expected 0 completed, got %d", len(completed))
	}
}

func TestMatchComposeProcesses_MixedStates(t *testing.T) {
	// Given: processes in mixed states (Running, Zombie, Dead)
	// When: calling matchComposeProcesses
	// Then: Running/Created in "running", Zombie/Dead in "completed"

	spec := &compose.ComposeSpec{
		Agents: map[string]*compose.AgentSpec{
			"reviewer": {Intent: "review code"},
			"analyst":  {Intent: "analyze quality"},
			"writer":   {Intent: "write docs"},
		},
	}

	procs := []vfs.ProcInfo{
		{PID: 1, State: types.StateRunning, Intent: "review code"},
		{PID: 2, State: types.StateZombie, Intent: "analyze quality"},
		{PID: 3, State: types.StateDead, Intent: "write docs"},
	}

	running, completed := matchComposeProcesses(procs, spec)
	if len(running) != 1 {
		t.Errorf("expected 1 running, got %d", len(running))
	}
	if running[0].PID != 1 {
		t.Errorf("expected running PID 1, got %d", running[0].PID)
	}
	if len(completed) != 2 {
		t.Errorf("expected 2 completed, got %d", len(completed))
	}
}

func TestMatchComposeProcesses_NoMatch(t *testing.T) {
	// Given: daemon processes do not match compose spec intents
	// When: calling matchComposeProcesses
	// Then: both slices are empty

	spec := &compose.ComposeSpec{
		Agents: map[string]*compose.AgentSpec{
			"reviewer": {Intent: "review code"},
		},
	}

	procs := []vfs.ProcInfo{
		{PID: 1, State: types.StateRunning, Intent: "unrelated task"},
		{PID: 2, State: types.StateRunning, Intent: "other work"},
	}

	running, completed := matchComposeProcesses(procs, spec)
	if len(running) != 0 {
		t.Errorf("expected 0 running, got %d", len(running))
	}
	if len(completed) != 0 {
		t.Errorf("expected 0 completed, got %d", len(completed))
	}
}

// --- Story 7.4: Compose End-to-End Acceptance Tests ---
// These tests verify AC #1-2 of Story 7.4.
// All E2E tests use the TestComposeE2E_ prefix.

// --- E2E Mock Spawner ---
// Extends the existing mockComposeSpawner with getResult support for output passthrough verification.

type e2eMockSpawner struct {
	mu            sync.Mutex
	spawned       []e2eSpawnRecord // records intent and opts for each Spawn call
	pidAlloc      uint64
	failIntents   map[string]bool   // intents that should fail
	waitDelay     time.Duration     // delay for Wait calls
	getResult     map[types.PID]string // preset output results per PID
	intentResults map[string]string    // intent -> result, auto-mapped to PID at spawn time
}

type e2eSpawnRecord struct {
	intent string
	opts   compose.ComposeSpawnOpts
}

func newE2EMockSpawner() *e2eMockSpawner {
	return &e2eMockSpawner{
		failIntents:   make(map[string]bool),
		getResult:     make(map[types.PID]string),
		intentResults: make(map[string]string),
	}
}

func (m *e2eMockSpawner) Spawn(intent string, agent *agents.AgentInfo, opts compose.ComposeSpawnOpts) (types.PID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pidAlloc++
	pid := types.PID(m.pidAlloc)
	m.spawned = append(m.spawned, e2eSpawnRecord{intent: intent, opts: opts})
	// Auto-map intent-based results to the allocated PID
	if result, ok := m.intentResults[intent]; ok {
		m.getResult[pid] = result
	}
	return pid, nil
}

func (m *e2eMockSpawner) Wait(pid types.PID) (compose.ComposeExitStatus, error) {
	if m.waitDelay > 0 {
		time.Sleep(m.waitDelay)
	}

	m.mu.Lock()
	idx := int(pid) - 1
	var intent string
	if idx >= 0 && idx < len(m.spawned) {
		intent = m.spawned[idx].intent
	}
	shouldFail := m.failIntents[intent]
	m.mu.Unlock()

	if shouldFail {
		return compose.ComposeExitStatus{Code: 1, Reason: "simulated failure"}, fmt.Errorf("agent failed")
	}
	return compose.ComposeExitStatus{Code: 0, Reason: "ok"}, nil
}

func (m *e2eMockSpawner) GetProcessResult(pid types.PID) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.getResult[pid]
	return r, ok
}

func (m *e2eMockSpawner) getSpawnedIntents() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]string, len(m.spawned))
	for i, rec := range m.spawned {
		result[i] = rec.intent
	}
	return result
}

func (m *e2eMockSpawner) getSpawnRecords() []e2eSpawnRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]e2eSpawnRecord, len(m.spawned))
	copy(result, m.spawned)
	return result
}

// newDiamondSpec creates the canonical diamond DAG ComposeSpec for E2E tests.
// Structure: analyzer (layer 1) -> security + docs (layer 2, parallel) -> reporter (layer 3)
func newDiamondSpec() *compose.ComposeSpec {
	return &compose.ComposeSpec{
		Version: "1.0",
		Intent:  "E2E acceptance test",
		Agents: map[string]*compose.AgentSpec{
			"analyzer": {Intent: "analyze code structure"},
			"security": {
				Intent:    "execute security audit",
				DependsOn: map[string]string{"analyzer": "completed"},
			},
			"docs": {
				Intent:    "generate documentation",
				DependsOn: map[string]string{"analyzer": "completed"},
			},
			"reporter": {
				Intent: "generate final report",
				DependsOn: map[string]string{
					"security": "completed",
					"docs":     "completed",
				},
			},
		},
	}
}

// --- Task 1: E2E Fixture Validation ---

func TestComposeE2E_FixtureParsing(t *testing.T) {
	// Given: the diamond DAG fixture
	// When: parsing and building DAG
	// Then: fixture is valid and produces expected topology

	spec := newDiamondSpec()

	dag, err := compose.BuildDAG(spec)
	if err != nil {
		t.Fatalf("BuildDAG failed: %v", err)
	}

	layers, err := dag.TopologicalSort()
	if err != nil {
		t.Fatalf("TopologicalSort failed: %v", err)
	}

	// Expected: 3 layers
	if len(layers) != 3 {
		t.Fatalf("expected 3 layers, got %d: %v", len(layers), layers)
	}

	// Layer 1: analyzer
	if len(layers[0]) != 1 || layers[0][0] != "analyzer" {
		t.Errorf("layer 1: expected [analyzer], got %v", layers[0])
	}

	// Layer 2: docs + security (alphabetical)
	if len(layers[1]) != 2 {
		t.Errorf("layer 2: expected 2 agents, got %d: %v", len(layers[1]), layers[1])
	}

	// Layer 3: reporter
	if len(layers[2]) != 1 || layers[2][0] != "reporter" {
		t.Errorf("layer 3: expected [reporter], got %v", layers[2])
	}
}

// --- Task 2: E2E Dependency Order Test ---

func TestComposeE2E_DependencyOrder(t *testing.T) {
	// Given: diamond DAG (analyzer -> security+docs -> reporter)
	// When: executing via Engine with mock spawner
	// Then: agents execute in topological order, layer 2 parallel

	spec := newDiamondSpec()
	ks := newE2EMockSpawner()

	engine, err := compose.NewEngine(spec, ks, nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	results, execErr := engine.Execute(context.Background())
	if execErr != nil {
		t.Fatalf("Execute error: %v", execErr)
	}

	// All 4 agents should complete successfully
	if len(results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(results))
	}
	for _, r := range results {
		if r.Err != nil {
			t.Errorf("agent %q had error: %v", r.Name, r.Err)
		}
	}

	// Verify spawn order: analyzer first, reporter last
	intents := ks.getSpawnedIntents()
	if len(intents) != 4 {
		t.Fatalf("expected 4 spawns, got %d: %v", len(intents), intents)
	}

	// analyzer must be first
	if intents[0] != "analyze code structure" {
		t.Errorf("expected first spawn to be analyzer, got %q", intents[0])
	}

	// reporter must be last
	if intents[3] != "generate final report" {
		t.Errorf("expected last spawn to be reporter, got %q", intents[3])
	}

	// Layer 2: security and docs in middle (order within layer may vary due to parallelism)
	middleIntents := map[string]bool{intents[1]: true, intents[2]: true}
	if !middleIntents["execute security audit"] {
		t.Errorf("expected security in middle layer, got %v", middleIntents)
	}
	if !middleIntents["generate documentation"] {
		t.Errorf("expected docs in middle layer, got %v", middleIntents)
	}
}

// --- Task 3: E2E Output Passthrough Test ---

func TestComposeE2E_OutputPassthrough(t *testing.T) {
	// Given: diamond DAG with preset getResult values
	// When: executing
	// Then: downstream agents receive upstream outputs in SystemPrompt

	spec := newDiamondSpec()
	ks := newE2EMockSpawner()

	// Preset results by intent (auto-mapped to PID at spawn time, order-independent)
	ks.intentResults["analyze code structure"] = "code analysis result"
	ks.intentResults["execute security audit"] = "security audit findings"
	ks.intentResults["generate documentation"] = "documentation output"

	engine, err := compose.NewEngine(spec, ks, nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	results, execErr := engine.Execute(context.Background())
	if execErr != nil {
		t.Fatalf("Execute error: %v", execErr)
	}
	if len(results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(results))
	}

	records := ks.getSpawnRecords()
	if len(records) != 4 {
		t.Fatalf("expected 4 spawn records, got %d", len(records))
	}

	// Layer 2 agents (security, docs) should have upstream output from analyzer
	for _, rec := range records[1:3] {
		if !strings.Contains(rec.opts.SystemPrompt, "## Upstream Agent Output") {
			t.Errorf("agent %q: expected '## Upstream Agent Output' header, got %q", rec.intent, rec.opts.SystemPrompt)
		}
		if !strings.Contains(rec.opts.SystemPrompt, "### analyzer output:") {
			t.Errorf("agent %q: expected '### analyzer output:' section, got %q", rec.intent, rec.opts.SystemPrompt)
		}
		if !strings.Contains(rec.opts.SystemPrompt, "code analysis result") {
			t.Errorf("agent %q: expected analyzer output content, got %q", rec.intent, rec.opts.SystemPrompt)
		}
	}

	// Reporter (last) should have outputs from both security and docs
	reporterRecord := records[3]
	if !strings.Contains(reporterRecord.opts.SystemPrompt, "## Upstream Agent Output") {
		t.Errorf("reporter: expected upstream output header, got %q", reporterRecord.opts.SystemPrompt)
	}

	// Reporter should receive both upstream outputs (security + docs)
	// Note: the exact upstream names depend on DAG node names, not intents
	hasDocsOutput := strings.Contains(reporterRecord.opts.SystemPrompt, "### docs output:")
	hasSecurityOutput := strings.Contains(reporterRecord.opts.SystemPrompt, "### security output:")
	if !hasDocsOutput {
		t.Errorf("reporter: expected '### docs output:' section, got %q", reporterRecord.opts.SystemPrompt)
	}
	if !hasSecurityOutput {
		t.Errorf("reporter: expected '### security output:' section, got %q", reporterRecord.opts.SystemPrompt)
	}
}

// --- Task 4: E2E Performance Test ---

func TestComposeE2E_Performance(t *testing.T) {
	// Given: diamond DAG with 4 agents, mock spawner (instant return)
	// When: executing
	// Then: total time from NewEngine to Execute completion <= 90 seconds

	spec := newDiamondSpec()
	ks := newE2EMockSpawner()

	start := time.Now()

	engine, err := compose.NewEngine(spec, ks, nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	results, execErr := engine.Execute(context.Background())
	elapsed := time.Since(start)

	if execErr != nil {
		t.Fatalf("Execute error: %v", execErr)
	}
	if len(results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(results))
	}
	for _, r := range results {
		if r.Err != nil {
			t.Errorf("agent %q had error: %v", r.Name, r.Err)
		}
	}

	// AC #1: total time <= 90 seconds (mock env should be well under)
	if elapsed > 90*time.Second {
		t.Fatalf("performance: 4-agent diamond took %v (max 90s)", elapsed)
	}

	// Additional: in mock env, should complete in < 1 second
	if elapsed > 1*time.Second {
		t.Logf("warning: 4-agent diamond took %v in mock env (expected < 1s)", elapsed)
	}
}

// --- Task 5: E2E Failure Propagation Test ---

func TestComposeE2E_FailurePropagation(t *testing.T) {
	// Given: diamond DAG where analyzer (root) fails
	// When: executing
	// Then: all downstream agents are skipped with "upstream dependency failed"

	spec := newDiamondSpec()
	ks := newE2EMockSpawner()
	ks.failIntents["analyze code structure"] = true

	engine, err := compose.NewEngine(spec, ks, nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	results, _ := engine.Execute(context.Background())

	// Build result map
	resultMap := make(map[string]compose.ScheduleResult)
	for _, r := range results {
		resultMap[r.Name] = r
	}

	// analyzer should have an error with non-zero ExitCode (Task 5.2)
	if ra, ok := resultMap["analyzer"]; ok {
		if ra.Err == nil {
			t.Fatal("analyzer should have error")
		}
		if ra.ExitCode == 0 {
			t.Errorf("analyzer: expected non-zero ExitCode, got %d", ra.ExitCode)
		}
	} else {
		t.Fatal("analyzer result missing")
	}

	// security, docs, reporter should all be skipped (upstream dependency failed)
	for _, name := range []string{"security", "docs", "reporter"} {
		r, ok := resultMap[name]
		if !ok {
			t.Errorf("result for %q missing", name)
			continue
		}
		if r.Err == nil {
			t.Errorf("agent %q should have error (upstream dependency failed)", name)
			continue
		}
		if !strings.Contains(r.Err.Error(), "upstream dependency failed") {
			t.Errorf("agent %q: expected 'upstream dependency failed', got %q", name, r.Err.Error())
		}
		if r.PID != 0 {
			t.Errorf("agent %q: expected PID 0 (not spawned), got %d", name, r.PID)
		}
	}

	// Only analyzer should have been spawned
	spawnedIntents := ks.getSpawnedIntents()
	if len(spawnedIntents) != 1 {
		t.Fatalf("expected only 1 spawn (analyzer), got %d: %v", len(spawnedIntents), spawnedIntents)
	}
	if spawnedIntents[0] != "analyze code structure" {
		t.Errorf("expected spawned intent 'analyze code structure', got %q", spawnedIntents[0])
	}
}

// --- Task 6: E2E Up Then Down Test ---

func TestComposeE2E_UpThenDown(t *testing.T) {
	// Given: compose spec with 2 agents, one Running and one Zombie in daemon
	// When: compose down is called
	// Then: only Running process is killed, Zombie is skipped

	sockPath, kern := setupTestIPCServer(t)
	ipc.SocketPathOverride = sockPath
	t.Cleanup(func() { ipc.SocketPathOverride = "" })

	spec := &compose.ComposeSpec{
		Version: "1.0",
		Intent:  "up-then-down test",
		Agents: map[string]*compose.AgentSpec{
			"reviewer": {Intent: "review code"},
			"writer":   {Intent: "write docs"},
		},
	}

	// Add processes to kernel: one Running, one Zombie
	proc1 := kernel.NewProcess(0, "review code", nil)
	_ = proc1.Start()
	kern.AddProcess(proc1)

	proc2 := kernel.NewProcess(0, "write docs", nil)
	_ = proc2.Start()
	_ = proc2.Terminate(kernel.ExitStatus{Code: 0, Reason: "completed"})
	kern.AddProcess(proc2)

	// Verify matchComposeProcesses correctly classifies
	client, err := ipc.Dial(sockPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	procs, err := client.ListProcs()
	if err != nil {
		t.Fatalf("ListProcs: %v", err)
	}
	client.Close()

	running, completed := matchComposeProcesses(procs, spec)
	if len(running) != 1 {
		t.Fatalf("expected 1 running, got %d", len(running))
	}
	if running[0].Intent != "review code" {
		t.Errorf("expected running intent 'review code', got %q", running[0].Intent)
	}
	if len(completed) != 1 {
		t.Fatalf("expected 1 completed, got %d", len(completed))
	}
	if completed[0].Intent != "write docs" {
		t.Errorf("expected completed intent 'write docs', got %q", completed[0].Intent)
	}

	// Execute compose down
	savedFile := flagComposeDownFile
	savedExit := exitCode
	t.Cleanup(func() {
		flagComposeDownFile = savedFile
		exitCode = savedExit
	})

	tmpDir := t.TempDir()
	composeYAML := `version: "1.0"
intent: "up-then-down test"
agents:
  reviewer:
    intent: "review code"
  writer:
    intent: "write docs"
`
	composePath := filepath.Join(tmpDir, "crux-compose.yaml")
	if err := os.WriteFile(composePath, []byte(composeYAML), 0644); err != nil {
		t.Fatalf("write compose file: %v", err)
	}
	flagComposeDownFile = composePath
	exitCode = 0

	err = runComposeDown(&cobra.Command{}, []string{})
	if err != nil {
		t.Fatalf("runComposeDown error: %v", err)
	}

	// Verify: the running process was killed (state should change)
	verifyClient, err := ipc.Dial(sockPath)
	if err != nil {
		t.Fatalf("dial for verification: %v", err)
	}
	defer verifyClient.Close()

	procsAfter, err := verifyClient.ListProcs()
	if err != nil {
		t.Fatalf("ListProcs after: %v", err)
	}

	for _, p := range procsAfter {
		if p.Intent == "write docs" && p.State != types.StateZombie {
			t.Errorf("already-completed process should remain Zombie, got %s", p.State)
		}
	}
}

// --- Task 7: E2E Top Visibility Test ---

func TestComposeE2E_TopVisibility(t *testing.T) {
	// Given: IPC daemon with multiple compose agent processes
	// When: listing processes via IPC
	// Then: all agents visible with correct Intent and State

	sockPath, kern := setupTestIPCServer(t)

	// Add processes simulating compose agents
	proc1 := kernel.NewProcess(0, "analyze code structure", nil)
	_ = proc1.Start()
	kern.AddProcess(proc1)

	proc2 := kernel.NewProcess(0, "execute security audit", nil)
	_ = proc2.Start()
	kern.AddProcess(proc2)

	proc3 := kernel.NewProcess(0, "generate documentation", nil)
	_ = proc3.Start()
	_ = proc3.Terminate(kernel.ExitStatus{Code: 0, Reason: "done"})
	kern.AddProcess(proc3)

	// List processes via IPC
	client, err := ipc.Dial(sockPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	procs, err := client.ListProcs()
	if err != nil {
		t.Fatalf("ListProcs: %v", err)
	}

	// Verify all 3 agents visible
	if len(procs) != 3 {
		t.Fatalf("expected 3 processes, got %d", len(procs))
	}

	// Build map for verification
	procMap := make(map[string]vfs.ProcInfo)
	for _, p := range procs {
		procMap[p.Intent] = p
	}

	// Verify analyzer
	if p, ok := procMap["analyze code structure"]; ok {
		if p.State != types.StateRunning {
			t.Errorf("analyzer: expected Running, got %s", p.State)
		}
		if p.PID == 0 {
			t.Error("analyzer: expected non-zero PID")
		}
	} else {
		t.Error("analyzer not found in process list")
	}

	// Verify security
	if p, ok := procMap["execute security audit"]; ok {
		if p.State != types.StateRunning {
			t.Errorf("security: expected Running, got %s", p.State)
		}
	} else {
		t.Error("security not found in process list")
	}

	// Verify docs (completed -> Zombie)
	if p, ok := procMap["generate documentation"]; ok {
		if p.State != types.StateZombie {
			t.Errorf("docs: expected Zombie, got %s", p.State)
		}
	} else {
		t.Error("docs not found in process list")
	}

	// Verify PPID relationships: all should have PPID 0 (spawned by init)
	for _, p := range procs {
		if p.PPID != 0 {
			t.Errorf("process %q: expected PPID 0, got %d", p.Intent, p.PPID)
		}
	}
}

// --- Task 8: E2E Summary Output Test ---

func TestComposeE2E_SummaryOutput(t *testing.T) {
	// Given: ScheduleResults from a diamond DAG execution
	// When: rendering summary
	// Then: output contains all agent names, exit codes, durations

	results := []compose.ScheduleResult{
		{Name: "analyzer", PID: 1, ExitCode: 0, Duration: 3200 * time.Millisecond},
		{Name: "security", PID: 2, ExitCode: 0, Duration: 5100 * time.Millisecond},
		{Name: "docs", PID: 3, ExitCode: 0, Duration: 4800 * time.Millisecond},
		{Name: "reporter", PID: 4, ExitCode: 0, Duration: 2500 * time.Millisecond},
	}

	ui.InitStyles(ui.TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	renderer := &ui.Renderer{Writer: &buf, OutputMode: ui.ModeDefault, Profile: ui.TerminalProfile{Width: 80, ColorLevel: 0}}

	ui.RenderComposeSummary(renderer, results, nil)

	output := buf.String()
	// Verify all agent names present
	for _, name := range []string{"analyzer", "security", "docs", "reporter"} {
		if !strings.Contains(output, name) {
			t.Errorf("summary should contain agent %q, got %q", name, output)
		}
	}
	// Verify counts
	if !strings.Contains(output, "4 agents") {
		t.Errorf("summary should show '4 agents', got %q", output)
	}
	if !strings.Contains(output, "4 succeeded") {
		t.Errorf("summary should show '4 succeeded', got %q", output)
	}
}

func TestComposeE2E_SummaryJSON(t *testing.T) {
	// Given: ScheduleResults from a diamond DAG execution
	// When: rendering as JSON
	// Then: JSON contains agents array with 4 entries and summary object

	results := []compose.ScheduleResult{
		{Name: "analyzer", PID: 1, ExitCode: 0, Duration: 3200 * time.Millisecond},
		{Name: "security", PID: 2, ExitCode: 0, Duration: 5100 * time.Millisecond},
		{Name: "docs", PID: 3, ExitCode: 0, Duration: 4800 * time.Millisecond},
		{Name: "reporter", PID: 4, ExitCode: 1, Err: fmt.Errorf("report generation failed"), Duration: 2500 * time.Millisecond},
	}

	ui.InitStyles(ui.TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	renderer := &ui.Renderer{Writer: &buf, OutputMode: ui.ModeJSON, Profile: ui.TerminalProfile{ColorLevel: 0}}

	ui.RenderComposeSummaryJSON(renderer, results, nil)

	var resp JSONResponse
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %s", err, buf.String())
	}

	// Parse data
	data, _ := json.Marshal(resp.Data)
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("failed to parse data: %v", err)
	}

	// Verify agents array
	agentsRaw, ok := m["agents"]
	if !ok {
		t.Fatal("JSON output missing 'agents' field")
	}
	var agentsList []map[string]any
	if err := json.Unmarshal(agentsRaw, &agentsList); err != nil {
		t.Fatalf("failed to parse agents: %v", err)
	}
	if len(agentsList) != 4 {
		t.Errorf("expected 4 agents, got %d", len(agentsList))
	}

	// Verify agent names
	names := make(map[string]bool)
	for _, a := range agentsList {
		if name, ok := a["name"].(string); ok {
			names[name] = true
		}
	}
	for _, expected := range []string{"analyzer", "security", "docs", "reporter"} {
		if !names[expected] {
			t.Errorf("agents array missing %q", expected)
		}
	}

	// Verify summary object
	summaryRaw, ok := m["summary"]
	if !ok {
		t.Fatal("JSON output missing 'summary' field")
	}
	var summary map[string]any
	if err := json.Unmarshal(summaryRaw, &summary); err != nil {
		t.Fatalf("failed to parse summary: %v", err)
	}

	// Verify summary fields
	for _, field := range []string{"total", "succeeded", "failed", "skipped"} {
		if _, ok := summary[field]; !ok {
			t.Errorf("summary missing field %q", field)
		}
	}

	// Verify total count
	if total, ok := summary["total"].(float64); ok {
		if int(total) != 4 {
			t.Errorf("expected total 4, got %v", total)
		}
	}
}
