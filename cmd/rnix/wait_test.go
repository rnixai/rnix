package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/kernel"
	"github.com/spf13/cobra"
)

// =============================================================================
// Story 63.1 Task 6.2: `rnix wait` cmd 层单测
//
// 覆盖 AC1（退出码传播）、AC3（timeout → 124）、AC4（--json 四字段）、
// AC5（NOT_FOUND → 1）、AC6（daemon-down 硬失败 → 1，不 EnsureDaemon）
// 以及参数错误（非数字 PID / 坏 duration → 1）。
//
// 惯例：mirror TestRunKill_* — 包变量（exitCode / flagWaitTimeout / flagJSON /
// ipc.SocketPathOverride）先存后改 defer 恢复；in-process server 用
// setupTestIPCServer（main_test.go）。
// =============================================================================

// resetWaitTestGlobals snapshots and restores the package-level state runWait
// reads, so tests stay order-independent.
func resetWaitTestGlobals(t *testing.T) {
	t.Helper()
	savedExit := exitCode
	savedTimeout := flagWaitTimeout
	savedJSON := flagJSON
	savedQuiet := flagQuiet
	savedVerbose := flagVerbose
	savedOverride := ipc.SocketPathOverride
	t.Cleanup(func() {
		exitCode = savedExit
		flagWaitTimeout = savedTimeout
		flagJSON = savedJSON
		flagQuiet = savedQuiet
		flagVerbose = savedVerbose
		ipc.SocketPathOverride = savedOverride
	})
	exitCode = 0
	flagWaitTimeout = ""
	flagJSON = false
	flagQuiet = false
	flagVerbose = false
}

func TestWaitCmd_RegisteredWithTimeoutFlag(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "wait" {
			found = true
			if c.Flags().Lookup("timeout") == nil {
				t.Error("wait command should register a --timeout flag")
			}
		}
	}
	if !found {
		t.Fatal("wait command should be registered on rootCmd")
	}
}

func TestRunWait_InvalidPID(t *testing.T) {
	resetWaitTestGlobals(t)

	err := runWait(&cobra.Command{}, []string{"abc"})
	if err != nil {
		t.Fatalf("runWait should return nil (errors handled internally), got %v", err)
	}
	if exitCode != 1 {
		t.Errorf("expected exitCode 1 for invalid PID, got %d", exitCode)
	}
}

func TestRunWait_InvalidTimeoutDuration(t *testing.T) {
	resetWaitTestGlobals(t)
	flagWaitTimeout = "bogus"

	err := runWait(&cobra.Command{}, []string{"42"})
	if err != nil {
		t.Fatalf("runWait should return nil, got %v", err)
	}
	if exitCode != 1 {
		t.Errorf("expected exitCode 1 for bad --timeout, got %d", exitCode)
	}
}

func TestRunWait_NonPositiveTimeout(t *testing.T) {
	resetWaitTestGlobals(t)
	flagWaitTimeout = "-5s"

	err := runWait(&cobra.Command{}, []string{"42"})
	if err != nil {
		t.Fatalf("runWait should return nil, got %v", err)
	}
	if exitCode != 1 {
		t.Errorf("expected exitCode 1 for non-positive --timeout, got %d", exitCode)
	}
}

// AC6: daemon-down is a hard fail (exit 1) WITHOUT EnsureDaemon. The override
// points at a nonexistent socket; if runWait ever called EnsureDaemon this
// test would hang or spawn a daemon rather than fail fast.
func TestRunWait_DaemonDown_HardFail(t *testing.T) {
	resetWaitTestGlobals(t)
	ipc.SocketPathOverride = filepath.Join(t.TempDir(), "no-such.sock")

	var errBuf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetErr(&errBuf)

	err := runWait(cmd, []string{"42"})
	if err != nil {
		t.Fatalf("runWait should return nil, got %v", err)
	}
	if exitCode != 1 {
		t.Errorf("expected exitCode 1 for daemon down, got %d", exitCode)
	}
	if !strings.Contains(errBuf.String(), "Daemon not running") {
		t.Errorf("stderr should carry the daemon-down guidance, got %q", errBuf.String())
	}
}

func TestRunWait_NotFound_ViaIPC(t *testing.T) {
	sockPath, _ := setupTestIPCServer(t)
	resetWaitTestGlobals(t)
	ipc.SocketPathOverride = sockPath

	err := runWait(&cobra.Command{}, []string{"999999"})
	if err != nil {
		t.Fatalf("runWait should return nil, got %v", err)
	}
	if exitCode != 1 {
		t.Errorf("expected exitCode 1 for NOT_FOUND, got %d", exitCode)
	}
}

// AC1: the command's own exit code equals the target's exit code.
func TestRunWait_ExitCodePropagation_ViaIPC(t *testing.T) {
	sockPath, kern := setupTestIPCServer(t)
	resetWaitTestGlobals(t)
	ipc.SocketPathOverride = sockPath

	proc := kernel.NewProcess(0, "wait target", nil)
	_ = proc.Start()
	kern.AddProcess(proc)
	if err := proc.Terminate(kernel.ExitStatus{Code: 5, Reason: "error"}); err != nil {
		t.Fatalf("Terminate: %v", err)
	}

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)

	err := runWait(cmd, []string{fmt.Sprintf("%d", proc.PID)})
	if err != nil {
		t.Fatalf("runWait should return nil, got %v", err)
	}
	if exitCode != 5 {
		t.Errorf("expected exitCode 5 (propagated), got %d", exitCode)
	}
	if !strings.Contains(out.String(), "exited with code 5") {
		t.Errorf("human output should report the exit code, got %q", out.String())
	}
}

// AC3: bounded wait on a never-terminating process exits 124 with a stderr notice.
func TestRunWait_Timeout_Exit124(t *testing.T) {
	sockPath, kern := setupTestIPCServer(t)
	resetWaitTestGlobals(t)
	ipc.SocketPathOverride = sockPath
	flagWaitTimeout = "150ms"

	// Running process with no reason loop — never terminates on its own.
	proc := kernel.NewProcess(0, "parked forever", nil)
	_ = proc.Start()
	kern.AddProcess(proc)

	var out, errBuf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)

	start := time.Now()
	err := runWait(cmd, []string{fmt.Sprintf("%d", proc.PID)})
	if err != nil {
		t.Fatalf("runWait should return nil, got %v", err)
	}
	if exitCode != 124 {
		t.Errorf("expected exitCode 124 on timeout, got %d", exitCode)
	}
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Errorf("wait returned in %v — should have waited out the 150ms budget", elapsed)
	}
	if !strings.Contains(errBuf.String(), "timed out") {
		t.Errorf("stderr should carry the timeout notice, got %q", errBuf.String())
	}
	if out.String() != "" {
		t.Errorf("stdout should stay clean on human-mode timeout, got %q", out.String())
	}
}

// AC4: --json emits pid / exit_code / exit_reason / timed_out inside data.
func TestRunWait_JSONOutput_TerminalState(t *testing.T) {
	sockPath, kern := setupTestIPCServer(t)
	resetWaitTestGlobals(t)
	ipc.SocketPathOverride = sockPath
	flagJSON = true

	proc := kernel.NewProcess(0, "json target", nil)
	_ = proc.Start()
	kern.AddProcess(proc)
	if err := proc.Terminate(kernel.ExitStatus{Code: 0, Reason: "complete"}); err != nil {
		t.Fatalf("Terminate: %v", err)
	}

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)

	err := runWait(cmd, []string{fmt.Sprintf("%d", proc.PID)})
	if err != nil {
		t.Fatalf("runWait should return nil, got %v", err)
	}
	if exitCode != 0 {
		t.Errorf("expected exitCode 0, got %d", exitCode)
	}

	var resp struct {
		OK   bool `json:"ok"`
		Data struct {
			PID        uint64  `json:"pid"`
			ExitCode   *int    `json:"exit_code"`
			ExitReason *string `json:"exit_reason"`
			TimedOut   *bool   `json:"timed_out"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("output is not valid JSON: %v (raw %q)", err, out.String())
	}
	if !resp.OK {
		t.Error("ok should be true")
	}
	if resp.Data.PID != uint64(proc.PID) {
		t.Errorf("data.pid = %d, want %d", resp.Data.PID, proc.PID)
	}
	if resp.Data.ExitCode == nil || *resp.Data.ExitCode != 0 {
		t.Errorf("data.exit_code = %v, want 0", resp.Data.ExitCode)
	}
	if resp.Data.ExitReason == nil || *resp.Data.ExitReason != "complete" {
		t.Errorf("data.exit_reason = %v, want %q", resp.Data.ExitReason, "complete")
	}
	if resp.Data.TimedOut == nil || *resp.Data.TimedOut {
		t.Errorf("data.timed_out = %v, want false", resp.Data.TimedOut)
	}
}

// AC4 (timeout leg): --json timeout keeps all four fields with timed_out=true,
// and the CLI still exits 124.
func TestRunWait_JSONOutput_TimedOut(t *testing.T) {
	sockPath, kern := setupTestIPCServer(t)
	resetWaitTestGlobals(t)
	ipc.SocketPathOverride = sockPath
	flagJSON = true
	flagWaitTimeout = "150ms"

	proc := kernel.NewProcess(0, "parked forever json", nil)
	_ = proc.Start()
	kern.AddProcess(proc)

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)

	err := runWait(cmd, []string{fmt.Sprintf("%d", proc.PID)})
	if err != nil {
		t.Fatalf("runWait should return nil, got %v", err)
	}
	if exitCode != 124 {
		t.Errorf("expected exitCode 124, got %d", exitCode)
	}

	var resp struct {
		OK   bool           `json:"ok"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("output is not valid JSON: %v (raw %q)", err, out.String())
	}
	if timedOut, ok := resp.Data["timed_out"].(bool); !ok || !timedOut {
		t.Errorf("data.timed_out = %v, want true", resp.Data["timed_out"])
	}
	for _, field := range []string{"pid", "exit_code", "exit_reason", "timed_out"} {
		if _, present := resp.Data[field]; !present {
			t.Errorf("data.%s missing from JSON output (AC4 four snake_case fields)", field)
		}
	}
}

// Quiet mode prints the bare exit code on stdout.
func TestRunWait_QuietOutput(t *testing.T) {
	sockPath, kern := setupTestIPCServer(t)
	resetWaitTestGlobals(t)
	ipc.SocketPathOverride = sockPath

	flagQuiet = true // restored by resetWaitTestGlobals

	proc := kernel.NewProcess(0, "quiet target", nil)
	_ = proc.Start()
	kern.AddProcess(proc)
	if err := proc.Terminate(kernel.ExitStatus{Code: 3, Reason: "context_full"}); err != nil {
		t.Fatalf("Terminate: %v", err)
	}

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)

	err := runWait(cmd, []string{fmt.Sprintf("%d", proc.PID)})
	if err != nil {
		t.Fatalf("runWait should return nil, got %v", err)
	}
	if exitCode != 3 {
		t.Errorf("expected exitCode 3, got %d", exitCode)
	}
	if got := strings.TrimSpace(out.String()); got != "3" {
		t.Errorf("quiet stdout = %q, want %q", got, "3")
	}
}
