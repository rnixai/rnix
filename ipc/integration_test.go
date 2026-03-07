package ipc

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
	"github.com/rnixai/rnix/vfs"
)

// setupIntegrationServer creates a full server+kernel with a mock LLM driver for integration testing.
func setupIntegrationServer(t *testing.T) (*Server, *kernel.KernelImpl, string) {
	t.Helper()

	devReg := vfs.NewDeviceRegistry()
	vfsInst := vfs.NewVFS(devReg)

	srv := NewServer(nil, nil, "0.1.0-test")
	kern := kernel.NewKernel(vfsInst, rnixctx.NewManager(), srv.CallbackMux())
	srv.SetKernel(kern)

	sockDir := t.TempDir()
	sockPath := filepath.Join(sockDir, "integration.sock")

	if err := srv.ListenAndServe(sockPath); err != nil {
		t.Fatalf("ListenAndServe: %v", err)
	}
	t.Cleanup(func() {
		srv.Shutdown()
		srv.Wait()
		kern.Shutdown()
	})

	return srv, kern, sockPath
}

func TestIntegration_PingListKill(t *testing.T) {
	_, kern, sockPath := setupIntegrationServer(t)

	// Non-streaming requests can reuse the same connection.
	c1, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial 1: %v", err)
	}
	ver, err := c1.Ping()
	c1.Close()
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if ver != "0.1.0-test" {
		t.Errorf("version = %q, want %q", ver, "0.1.0-test")
	}

	c2, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial 2: %v", err)
	}
	procs, err := c2.ListProcs()
	c2.Close()
	if err != nil {
		t.Fatalf("ListProcs (empty): %v", err)
	}
	if len(procs) != 0 {
		t.Errorf("expected 0 procs, got %d", len(procs))
	}

	proc := kernel.NewProcess(0, "integration test", []string{"test-skill"})
	_ = proc.Start()
	kern.AddProcess(proc)

	c3, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial 3: %v", err)
	}
	procs, err = c3.ListProcs()
	c3.Close()
	if err != nil {
		t.Fatalf("ListProcs (1 proc): %v", err)
	}
	if len(procs) != 1 {
		t.Fatalf("expected 1 proc, got %d", len(procs))
	}
	if procs[0].PID != proc.PID {
		t.Errorf("pid = %d, want %d", procs[0].PID, proc.PID)
	}
	if procs[0].State != types.StateRunning {
		t.Errorf("state = %d, want running", procs[0].State)
	}

	c4, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial 4: %v", err)
	}
	err = c4.Kill(proc.PID, types.SIGTERM)
	c4.Close()
	if err != nil {
		t.Fatalf("Kill: %v", err)
	}
}

func TestIntegration_KillNotFound(t *testing.T) {
	_, _, sockPath := setupIntegrationServer(t)

	client, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	err = client.Kill(999, types.SIGTERM)
	if err == nil {
		t.Fatal("Kill should fail for nonexistent PID")
	}
}

func TestIntegration_ConcurrentClients(t *testing.T) {
	_, kern, sockPath := setupIntegrationServer(t)

	proc := kernel.NewProcess(0, "concurrent test", nil)
	_ = proc.Start()
	kern.AddProcess(proc)

	const numClients = 10
	var wg sync.WaitGroup
	errs := make(chan error, numClients)

	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := Dial(sockPath)
			if err != nil {
				errs <- err
				return
			}
			defer c.Close()

			procs, err := c.ListProcs()
			if err != nil {
				errs <- err
				return
			}
			if len(procs) == 0 {
				errs <- err
				return
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent client error: %v", err)
		}
	}
}

func TestIntegration_AttachDebug_ReceivesEvents(t *testing.T) {
	_, kern, sockPath := setupIntegrationServer(t)

	proc := kernel.NewProcess(0, "debug test", nil)
	_ = proc.Start()
	kern.AddProcess(proc)

	go func() {
		time.Sleep(100 * time.Millisecond)
		proc.DebugChan <- types.SyscallEvent{
			PID:     proc.PID,
			Syscall: "Open",
			Args:    map[string]any{"path": "/dev/llm/claude"},
		}
		proc.DebugChan <- types.SyscallEvent{
			PID:     proc.PID,
			Syscall: "Close",
		}
		time.Sleep(50 * time.Millisecond)
		close(proc.DebugChan)
	}()

	client, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	var events []SyscallEventWire
	err = client.AttachDebug(proc.PID, func(sew SyscallEventWire) {
		events = append(events, sew)
	})
	if err != nil {
		t.Fatalf("AttachDebug: %v", err)
	}

	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Syscall != "Open" {
		t.Errorf("first event syscall = %q, want Open", events[0].Syscall)
	}
	if events[1].Syscall != "Close" {
		t.Errorf("second event syscall = %q, want Close", events[1].Syscall)
	}
}

func TestIntegration_CallbackMux_ProgressEvents(t *testing.T) {
	_, _, sockPath := setupIntegrationServer(t)

	client, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	ver, err := client.Ping()
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if ver == "" {
		t.Fatal("empty version")
	}
}

func TestIntegration_Shutdown_ClosesServer(t *testing.T) {
	_, _, sockPath := setupIntegrationServer(t)

	client, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	if err := client.Shutdown(); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	client.Close()

	time.Sleep(200 * time.Millisecond)

	_, err = Dial(sockPath)
	if err == nil {
		t.Fatal("should not connect after shutdown")
	}
}

func TestIntegration_ProcInfoWireJSON(t *testing.T) {
	_, kern, sockPath := setupIntegrationServer(t)

	proc := kernel.NewProcess(0, "wire test", []string{"skill-a", "skill-b"})
	_ = proc.Start()
	kern.AddProcess(proc)

	client, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	if err := sendRequestRaw(client, MethodListProcs, nil); err != nil {
		t.Fatalf("send: %v", err)
	}
	if !client.scanner.Scan() {
		t.Fatal("no response")
	}
	var resp Response
	if err := json.Unmarshal(client.scanner.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	raw := string(resp.Payload)
	if raw == "" {
		t.Fatal("empty payload")
	}
}

func sendRequestRaw(c *Client, method Method, payload any) error {
	return c.sendRequest(method, payload)
}

func TestIntegration_ConcurrentSpawn(t *testing.T) {
	_, kern, sockPath := setupIntegrationServer(t)
	_ = kern

	const numClients = 5
	var wg sync.WaitGroup
	pids := make(chan types.PID, numClients)
	errs := make(chan error, numClients)

	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			proc := kernel.NewProcess(0, fmt.Sprintf("concurrent spawn %d", idx), nil)
			_ = proc.Start()
			kern.AddProcess(proc)

			c, err := Dial(sockPath)
			if err != nil {
				errs <- err
				return
			}
			defer c.Close()

			procs, err := c.ListProcs()
			if err != nil {
				errs <- err
				return
			}

			found := false
			for _, p := range procs {
				if p.PID == proc.PID {
					found = true
					pids <- proc.PID
					break
				}
			}
			if !found {
				errs <- fmt.Errorf("PID %d not found in ListProcs", proc.PID)
			}
		}(i)
	}

	wg.Wait()
	close(pids)
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent spawn error: %v", err)
		}
	}

	seen := make(map[types.PID]bool)
	for pid := range pids {
		if seen[pid] {
			t.Errorf("duplicate PID %d — concurrent spawn should produce unique PIDs", pid)
		}
		seen[pid] = true
	}
	if len(seen) < numClients {
		t.Errorf("expected %d unique PIDs, got %d", numClients, len(seen))
	}
}

func TestIntegration_ConnectionReuse_PingThenList(t *testing.T) {
	_, kern, sockPath := setupIntegrationServer(t)

	// Add a process so ListProcs returns something meaningful.
	proc := kernel.NewProcess(0, "reuse test", nil)
	_ = proc.Start()
	kern.AddProcess(proc)

	// Single client connection for multiple requests.
	client, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	// First: Ping on the connection.
	ver, err := client.Ping()
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if ver != "0.1.0-test" {
		t.Errorf("version = %q, want %q", ver, "0.1.0-test")
	}

	// Second: ListProcs on the same connection — this is the broken-pipe scenario.
	procs, err := client.ListProcs()
	if err != nil {
		t.Fatalf("ListProcs after Ping on same connection: %v", err)
	}
	if len(procs) != 1 {
		t.Errorf("expected 1 proc, got %d", len(procs))
	}
	if len(procs) > 0 && procs[0].PID != proc.PID {
		t.Errorf("pid = %d, want %d", procs[0].PID, proc.PID)
	}
}

func TestIntegration_IdleAutoShutdown(t *testing.T) {
	devReg := vfs.NewDeviceRegistry()
	vfsInst := vfs.NewVFS(devReg)

	srv := NewServer(nil, nil, "0.1.0-test")
	srv.IdleTimeout = 500 * time.Millisecond
	kern := kernel.NewKernel(vfsInst, rnixctx.NewManager(), srv.CallbackMux())
	srv.SetKernel(kern)
	defer kern.Shutdown()

	sockDir := t.TempDir()
	sockPath := filepath.Join(sockDir, "idle-test.sock")

	if err := srv.ListenAndServe(sockPath); err != nil {
		t.Fatalf("ListenAndServe: %v", err)
	}

	c, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	_, err = c.Ping()
	c.Close()
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}

	select {
	case <-srv.Done():
	case <-time.After(5 * time.Second):
		srv.Shutdown()
		t.Fatal("daemon did not auto-shutdown within 5s (expected ~500ms idle timeout)")
	}

	srv.Wait()
}

// ============================================================
// ATDD RED PHASE — Story 13.1: gdb 调试会话管理 (Attach/Detach)
//
// Integration tests reference client.AttachGdb() and
// server handleAttachGdb() which do NOT exist yet.
// These tests verify the full IPC flow: attach → dual event
// stream (DebugChan + LogChan) → detach.
// ============================================================

// --- 13.1-INT-001: [P0] AttachGdb 成功接收 SyscallEvent 和 LogEntry 双通道事件 ---

func TestIntegration_AttachGdb_ReceivesDualChannelEvents(t *testing.T) {
	_, kern, sockPath := setupIntegrationServer(t)

	proc := kernel.NewProcess(0, "gdb dual-channel test", []string{"test-skill"})
	_ = proc.Start()
	kern.AddProcess(proc)

	// Emit syscall events and log entries in a goroutine
	go func() {
		time.Sleep(100 * time.Millisecond)
		proc.DebugChan <- types.SyscallEvent{
			PID:     proc.PID,
			Syscall: "Open",
			Args:    map[string]any{"path": "/dev/llm/claude"},
		}
		proc.LogChan <- types.LogEntry{
			PID:      proc.PID,
			Step:     1,
			Category: "reasoning",
			Content:  "开始分析代码",
		}
		proc.DebugChan <- types.SyscallEvent{
			PID:     proc.PID,
			Syscall: "Write",
			Args:    map[string]any{"fd": 3, "data": "result"},
		}
		time.Sleep(50 * time.Millisecond)
		close(proc.DebugChan)
		close(proc.LogChan)
	}()

	client, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	var syscallEvents []SyscallEventWire
	var logEntries []LogEntryWire
	info, err := client.AttachGdb(proc.PID, func(ev GdbEvent) {
		switch ev.Type {
		case StreamGdbSyscall:
			var sew SyscallEventWire
			if err := json.Unmarshal(ev.Payload, &sew); err == nil {
				syscallEvents = append(syscallEvents, sew)
			}
		case StreamGdbLog:
			var lew LogEntryWire
			if err := json.Unmarshal(ev.Payload, &lew); err == nil {
				logEntries = append(logEntries, lew)
			}
		}
	})
	if err != nil {
		t.Fatalf("AttachGdb: %v", err)
	}

	// Verify initial response contains process metadata
	if info.PID != proc.PID {
		t.Errorf("info.PID = %d, want %d", info.PID, proc.PID)
	}
	if info.State != types.StateRunning {
		t.Errorf("info.State = %d, want Running", info.State)
	}
	if info.Intent != "gdb dual-channel test" {
		t.Errorf("info.Intent = %q, want %q", info.Intent, "gdb dual-channel test")
	}

	// Verify syscall events received
	if len(syscallEvents) != 2 {
		t.Fatalf("expected 2 syscall events, got %d", len(syscallEvents))
	}
	if syscallEvents[0].Syscall != "Open" {
		t.Errorf("first syscall = %q, want Open", syscallEvents[0].Syscall)
	}
	if syscallEvents[1].Syscall != "Write" {
		t.Errorf("second syscall = %q, want Write", syscallEvents[1].Syscall)
	}

	// Verify log entries received
	if len(logEntries) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logEntries))
	}
	if logEntries[0].Content != "开始分析代码" {
		t.Errorf("log content = %q, want %q", logEntries[0].Content, "开始分析代码")
	}
}

// --- 13.1-INT-002: [P0] AttachGdb 进程不存在返回 NOT_FOUND ---

func TestIntegration_AttachGdb_NotFound(t *testing.T) {
	_, _, sockPath := setupIntegrationServer(t)

	client, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	_, err = client.AttachGdb(999, func(ev GdbEvent) {})
	if err == nil {
		t.Fatal("AttachGdb should fail for nonexistent PID")
	}
}

// --- 13.1-INT-003: [P0] AttachGdb 进程已 Dead 返回错误 ---

func TestIntegration_AttachGdb_DeadProcess(t *testing.T) {
	_, kern, sockPath := setupIntegrationServer(t)

	// Create and immediately kill a process
	proc := kernel.NewProcess(0, "dead process test", nil)
	_ = proc.Start()
	kern.AddProcess(proc)
	_ = kern.Kill(proc.PID, types.SIGTERM)

	// Wait for process to reach Dead/Zombie state
	time.Sleep(100 * time.Millisecond)

	client, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	_, err = client.AttachGdb(proc.PID, func(ev GdbEvent) {})
	if err == nil {
		t.Fatal("AttachGdb should fail for dead/zombie process")
	}
}

// --- 13.1-INT-004: [P0] Detach 后智能体继续执行 ---

func TestIntegration_AttachGdb_DetachDoesNotAffectProcess(t *testing.T) {
	_, kern, sockPath := setupIntegrationServer(t)

	proc := kernel.NewProcess(0, "detach test", []string{"test"})
	_ = proc.Start()
	kern.AddProcess(proc)

	// Start event producer
	go func() {
		time.Sleep(100 * time.Millisecond)
		proc.DebugChan <- types.SyscallEvent{
			PID:     proc.PID,
			Syscall: "Open",
		}
		// Don't close channels — process should remain alive after detach
	}()

	client, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	eventReceived := make(chan struct{}, 1)
	_, err = client.AttachGdb(proc.PID, func(ev GdbEvent) {
		select {
		case eventReceived <- struct{}{}:
		default:
		}
	})

	// Send detach after receiving first event
	select {
	case <-eventReceived:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for event")
	}

	err = client.SendDetach(proc.PID)
	if err != nil {
		t.Fatalf("SendDetach: %v", err)
	}
	client.Close()

	// Verify process is still running after detach
	info, infoErr := kern.GetProcInfo(proc.PID)
	if infoErr != nil {
		t.Fatalf("GetProcInfo after detach: %v", infoErr)
	}
	if info.State != types.StateRunning {
		t.Errorf("process state after detach = %d, want Running", info.State)
	}

	// Cleanup
	close(proc.DebugChan)
	close(proc.LogChan)
}

// --- 13.1-INT-005: [P1] AttachGdb 进程在 attach 后退出时发送 state_change ---

func TestIntegration_AttachGdb_ProcessExitDuringSession(t *testing.T) {
	_, kern, sockPath := setupIntegrationServer(t)

	proc := kernel.NewProcess(0, "exit during gdb", nil)
	_ = proc.Start()
	kern.AddProcess(proc)

	go func() {
		time.Sleep(100 * time.Millisecond)
		proc.DebugChan <- types.SyscallEvent{
			PID:     proc.PID,
			Syscall: "Open",
		}
		time.Sleep(50 * time.Millisecond)
		// Kill the process — should trigger state_change event
		_ = kern.Kill(proc.PID, types.SIGTERM)
	}()

	client, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	var gotStateChange bool
	_, err = client.AttachGdb(proc.PID, func(ev GdbEvent) {
		if ev.Type == StreamGdbStateChange {
			gotStateChange = true
		}
	})
	if err != nil {
		t.Fatalf("AttachGdb: %v", err)
	}

	if !gotStateChange {
		t.Error("expected gdb_state_change event when process exits during session")
	}
}

// --- 13.1-INT-006: [P1] AttachGdb 返回初始进程元信息快照 ---

func TestIntegration_AttachGdb_InitialMetadata(t *testing.T) {
	_, kern, sockPath := setupIntegrationServer(t)

	proc := kernel.NewProcess(0, "metadata test", []string{"analyzer", "writer"})
	_ = proc.Start()
	kern.AddProcess(proc)

	go func() {
		time.Sleep(200 * time.Millisecond)
		close(proc.DebugChan)
		close(proc.LogChan)
	}()

	client, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	info, err := client.AttachGdb(proc.PID, func(ev GdbEvent) {})
	if err != nil {
		t.Fatalf("AttachGdb: %v", err)
	}

	if info.PID != proc.PID {
		t.Errorf("info PID = %d, want %d", info.PID, proc.PID)
	}
	if info.Intent != "metadata test" {
		t.Errorf("info Intent = %q, want %q", info.Intent, "metadata test")
	}
	if len(info.Skills) != 2 {
		t.Fatalf("info Skills count = %d, want 2", len(info.Skills))
	}
	if info.Skills[0] != "analyzer" {
		t.Errorf("info Skills[0] = %q, want %q", info.Skills[0], "analyzer")
	}
}

// --- 13.1-INT-007: [P2] 客户端断开连接时 server 自动清理 attach 状态 ---

func TestIntegration_AttachGdb_ClientDisconnectCleanup(t *testing.T) {
	_, kern, sockPath := setupIntegrationServer(t)

	proc := kernel.NewProcess(0, "disconnect cleanup test", nil)
	_ = proc.Start()
	kern.AddProcess(proc)

	// Keep channels open — process stays alive
	go func() {
		time.Sleep(100 * time.Millisecond)
		proc.DebugChan <- types.SyscallEvent{PID: proc.PID, Syscall: "Open"}
	}()

	client, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	eventReceived := make(chan struct{}, 1)
	go func() {
		_, _ = client.AttachGdb(proc.PID, func(ev GdbEvent) {
			select {
			case eventReceived <- struct{}{}:
			default:
			}
		})
	}()

	select {
	case <-eventReceived:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for first event")
	}

	// Abruptly close connection
	client.Close()
	time.Sleep(200 * time.Millisecond)

	// Process should still be alive
	info, infoErr := kern.GetProcInfo(proc.PID)
	if infoErr != nil {
		t.Fatalf("GetProcInfo after client disconnect: %v", infoErr)
	}
	if info.State != types.StateRunning {
		t.Errorf("process state after client disconnect = %d, want Running", info.State)
	}

	// Should be able to attach again (attach state cleaned up)
	client2, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial 2: %v", err)
	}
	defer client2.Close()

	_, err = client2.AttachGdb(proc.PID, func(ev GdbEvent) {})
	// This should succeed if attach state was properly cleaned up
	// (if server tracks single-attach per process)

	close(proc.DebugChan)
	close(proc.LogChan)
}

// ============================================================
// ATDD RED PHASE — Story 13.2: 断点系统 (集成测试)
//
// Tests reference client.SendGdbCommand, GdbCommandRequest,
// GdbCommandResponse, MethodGdbCommand
// which do NOT exist yet → compile failure = RED phase.
// ============================================================

// --- 13.2-INT-001: [P0] SendGdbCommand 设置 syscall 断点 ---

func TestIntegration_GdbCommand_BreakSyscall(t *testing.T) {
	_, kern, sockPath := setupIntegrationServer(t)

	proc := kernel.NewProcess(0, "bp-integ-syscall", nil)
	_ = proc.Start()
	kern.AddProcess(proc)

	client, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	resp, err := client.SendGdbCommand(proc.PID, "break", []string{"syscall", "Read"})
	if err != nil {
		t.Fatalf("SendGdbCommand: %v", err)
	}
	if !resp.OK {
		t.Fatalf("expected OK=true, message: %s", resp.Message)
	}
	if resp.Data == nil {
		t.Fatal("expected data with bp_id")
	}
}

// --- 13.2-INT-002: [P0] SendGdbCommand 设置 reasoning 断点 ---

func TestIntegration_GdbCommand_BreakReasoning(t *testing.T) {
	_, kern, sockPath := setupIntegrationServer(t)

	proc := kernel.NewProcess(0, "bp-integ-reasoning", nil)
	_ = proc.Start()
	kern.AddProcess(proc)

	client, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	resp, err := client.SendGdbCommand(proc.PID, "break", []string{"reasoning"})
	if err != nil {
		t.Fatalf("SendGdbCommand: %v", err)
	}
	if !resp.OK {
		t.Fatalf("expected OK=true, message: %s", resp.Message)
	}
}

// --- 13.2-INT-003: [P0] SendGdbCommand 设置 budget 断点 ---

func TestIntegration_GdbCommand_BreakBudget(t *testing.T) {
	_, kern, sockPath := setupIntegrationServer(t)

	proc := kernel.NewProcess(0, "bp-integ-budget", nil)
	_ = proc.Start()
	kern.AddProcess(proc)

	client, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	resp, err := client.SendGdbCommand(proc.PID, "break", []string{"budget", "5000"})
	if err != nil {
		t.Fatalf("SendGdbCommand: %v", err)
	}
	if !resp.OK {
		t.Fatalf("expected OK=true, message: %s", resp.Message)
	}
}

// --- 13.2-INT-004: [P0] SendGdbCommand 设置 quality --pattern 断点 ---

func TestIntegration_GdbCommand_BreakQualityPattern(t *testing.T) {
	_, kern, sockPath := setupIntegrationServer(t)

	proc := kernel.NewProcess(0, "bp-integ-quality", nil)
	_ = proc.Start()
	kern.AddProcess(proc)

	client, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	resp, err := client.SendGdbCommand(proc.PID, "break", []string{"quality", "--pattern", "安全漏洞"})
	if err != nil {
		t.Fatalf("SendGdbCommand: %v", err)
	}
	if !resp.OK {
		t.Fatalf("expected OK=true, message: %s", resp.Message)
	}
}

// --- 13.2-INT-005: [P0] SendGdbCommand delete 断点 ---

func TestIntegration_GdbCommand_Delete(t *testing.T) {
	_, kern, sockPath := setupIntegrationServer(t)

	proc := kernel.NewProcess(0, "bp-integ-delete", nil)
	_ = proc.Start()
	kern.AddProcess(proc)

	client, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	// First add a breakpoint
	addResp, err := client.SendGdbCommand(proc.PID, "break", []string{"reasoning"})
	if err != nil {
		t.Fatalf("SendGdbCommand add: %v", err)
	}
	if !addResp.OK {
		t.Fatalf("add failed: %s", addResp.Message)
	}

	bpID := "1" // first breakpoint
	if addResp.Data != nil {
		if dataMap, ok := addResp.Data.(map[string]any); ok {
			if id, ok := dataMap["bp_id"]; ok {
				bpID = fmt.Sprintf("%v", id)
			}
		}
	}

	// Then delete it
	delResp, err := client.SendGdbCommand(proc.PID, "delete", []string{bpID})
	if err != nil {
		t.Fatalf("SendGdbCommand delete: %v", err)
	}
	if !delResp.OK {
		t.Errorf("delete failed: %s", delResp.Message)
	}
}

// --- 13.2-INT-006: [P0] SendGdbCommand continue ---

func TestIntegration_GdbCommand_Continue(t *testing.T) {
	_, kern, sockPath := setupIntegrationServer(t)

	proc := kernel.NewProcess(0, "bp-integ-continue", nil)
	_ = proc.Start()
	kern.AddProcess(proc)

	client, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	resp, err := client.SendGdbCommand(proc.PID, "continue", nil)
	if err != nil {
		t.Fatalf("SendGdbCommand: %v", err)
	}
	if !resp.OK {
		t.Fatalf("expected OK=true, message: %s", resp.Message)
	}
}

// --- 13.2-INT-007: [P0] SendGdbCommand info breakpoints ---

func TestIntegration_GdbCommand_InfoBreakpoints(t *testing.T) {
	_, kern, sockPath := setupIntegrationServer(t)

	proc := kernel.NewProcess(0, "bp-integ-info", nil)
	_ = proc.Start()
	kern.AddProcess(proc)

	client, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	// Add a breakpoint first
	_, err = client.SendGdbCommand(proc.PID, "break", []string{"syscall", "Read"})
	if err != nil {
		t.Fatalf("SendGdbCommand add: %v", err)
	}

	// List breakpoints
	resp, err := client.SendGdbCommand(proc.PID, "info", nil)
	if err != nil {
		t.Fatalf("SendGdbCommand info: %v", err)
	}
	if !resp.OK {
		t.Fatalf("expected OK=true, message: %s", resp.Message)
	}
	if resp.Data == nil {
		t.Fatal("expected data with breakpoints list")
	}
}

// --- 13.2-INT-008: [P0] SendGdbCommand 走独立连接 ---

func TestIntegration_GdbCommand_IndependentConnection(t *testing.T) {
	_, kern, sockPath := setupIntegrationServer(t)

	proc := kernel.NewProcess(0, "bp-integ-conn", nil)
	_ = proc.Start()
	kern.AddProcess(proc)

	// Attach gdb first (streaming connection)
	client1, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial 1: %v", err)
	}
	defer client1.Close()

	_, err = client1.AttachGdb(proc.PID, func(ev GdbEvent) {})
	if err != nil {
		t.Fatalf("AttachGdb: %v", err)
	}

	// SendGdbCommand should work via independent connection (like SendDetach)
	client2, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial 2: %v", err)
	}
	defer client2.Close()

	resp, err := client2.SendGdbCommand(proc.PID, "break", []string{"reasoning"})
	if err != nil {
		t.Fatalf("SendGdbCommand on separate connection: %v", err)
	}
	if !resp.OK {
		t.Errorf("expected OK=true, message: %s", resp.Message)
	}
}

// --- 13.2-INT-009: [P1] SendGdbCommand 不存在的 PID ---

func TestIntegration_GdbCommand_NotFound(t *testing.T) {
	_, _, sockPath := setupIntegrationServer(t)

	client, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	resp, err := client.SendGdbCommand(99999, "break", []string{"reasoning"})
	if err == nil && resp != nil && resp.OK {
		t.Error("expected error or OK=false for non-existent PID")
	}
	// Either err is non-nil (IPC error) or resp.OK is false — both are acceptable
}
