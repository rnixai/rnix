package ipc

import (
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	cruxctx "github.com/gonewx/crux/context"
	"github.com/gonewx/crux/internal/types"
	"github.com/gonewx/crux/kernel"
	"github.com/gonewx/crux/vfs"
)

// setupIntegrationServer creates a full server+kernel with a mock LLM driver for integration testing.
func setupIntegrationServer(t *testing.T) (*Server, *kernel.KernelImpl, string) {
	t.Helper()

	devReg := vfs.NewDeviceRegistry()
	vfsInst := vfs.NewVFS(devReg)

	srv := NewServer(nil, nil, "0.1.0-test")
	kern := kernel.NewKernel(vfsInst, cruxctx.NewManager(), srv.CallbackMux())
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

	// Each request needs its own connection (one-shot protocol).
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
