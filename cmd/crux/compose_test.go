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
