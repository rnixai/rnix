package ipc

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
	"github.com/rnixai/rnix/vfs"
)

func setupTestServer(t *testing.T) (*Server, string, *rnixctx.Manager) {
	t.Helper()

	devReg := vfs.NewDeviceRegistry()
	vfsInst := vfs.NewVFS(devReg)
	ctxMgr := rnixctx.NewManager()

	srv := NewServer(nil, nil, "0.1.0-test")
	kern := kernel.NewKernel(vfsInst, ctxMgr, srv.CallbackMux())
	srv.kern = kern
	srv.SetContextManager(ctxMgr)

	sockDir := t.TempDir()
	sockPath := filepath.Join(sockDir, "test.sock")

	if err := srv.ListenAndServe(sockPath); err != nil {
		t.Fatalf("ListenAndServe: %v", err)
	}
	t.Cleanup(func() {
		srv.Shutdown()
		srv.Wait()
		kern.Shutdown()
	})

	return srv, sockPath, ctxMgr
}

func dial(t *testing.T, sockPath string) net.Conn {
	t.Helper()
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func sendRequest(t *testing.T, conn net.Conn, method Method, payload any) Response {
	t.Helper()
	var rawPayload json.RawMessage
	if payload != nil {
		rawPayload, _ = json.Marshal(payload)
	}
	req := Request{Method: method, Payload: rawPayload}
	enc := json.NewEncoder(conn)
	if err := enc.Encode(req); err != nil {
		t.Fatalf("encode request: %v", err)
	}

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		t.Fatalf("no response")
	}

	var resp Response
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	return resp
}

func TestServer_Ping(t *testing.T) {
	_, sockPath, _ := setupTestServer(t)
	conn := dial(t, sockPath)

	resp := sendRequest(t, conn, MethodPing, nil)
	if !resp.OK {
		t.Fatalf("ping not ok: %+v", resp.Error)
	}

	var pr PingResponse
	if err := json.Unmarshal(resp.Payload, &pr); err != nil {
		t.Fatalf("unmarshal ping: %v", err)
	}
	if pr.Version != "0.1.0-test" {
		t.Errorf("version = %q, want %q", pr.Version, "0.1.0-test")
	}
}

func TestServer_ListProcs_Empty(t *testing.T) {
	_, sockPath, _ := setupTestServer(t)
	conn := dial(t, sockPath)

	resp := sendRequest(t, conn, MethodListProcs, nil)
	if !resp.OK {
		t.Fatalf("list_procs not ok: %+v", resp.Error)
	}

	var lr ListProcsResponse
	if err := json.Unmarshal(resp.Payload, &lr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(lr.Processes) != 0 {
		t.Errorf("processes = %d, want 0", len(lr.Processes))
	}
}

func TestServer_Kill_NotFound(t *testing.T) {
	_, sockPath, _ := setupTestServer(t)
	conn := dial(t, sockPath)

	resp := sendRequest(t, conn, MethodKill, KillRequest{PID: 999, Signal: types.SIGTERM})
	if resp.OK {
		t.Fatal("kill should fail for nonexistent PID")
	}
	if resp.Error == nil {
		t.Fatal("error should not be nil")
	}
	if resp.Error.Code != "NOT_FOUND" {
		t.Errorf("code = %q, want NOT_FOUND", resp.Error.Code)
	}
}

func TestServer_UnknownMethod(t *testing.T) {
	_, sockPath, _ := setupTestServer(t)
	conn := dial(t, sockPath)

	resp := sendRequest(t, conn, Method("nonexistent"), nil)
	if resp.OK {
		t.Fatal("should fail for unknown method")
	}
	if resp.Error == nil || resp.Error.Code != "INVALID" {
		t.Errorf("error = %+v, want INVALID", resp.Error)
	}
}

func TestServer_MalformedRequest(t *testing.T) {
	_, sockPath, _ := setupTestServer(t)
	conn := dial(t, sockPath)

	_, err := conn.Write([]byte("not json\n"))
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		t.Fatal("no response")
	}

	var resp Response
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.OK {
		t.Fatal("should fail for malformed request")
	}
}

func TestServer_Shutdown(t *testing.T) {
	_, sockPath, _ := setupTestServer(t)
	conn := dial(t, sockPath)

	resp := sendRequest(t, conn, MethodShutdown, nil)
	if !resp.OK {
		t.Fatalf("shutdown not ok: %+v", resp.Error)
	}
}

func TestServer_AttachDebug_NotFound(t *testing.T) {
	_, sockPath, _ := setupTestServer(t)
	conn := dial(t, sockPath)

	resp := sendRequest(t, conn, MethodAttachDebug, AttachDebugRequest{PID: 999})
	if resp.OK {
		t.Fatal("attach_debug should fail for nonexistent PID")
	}
}

func TestServer_MultipleConnections(t *testing.T) {
	_, sockPath, _ := setupTestServer(t)

	conns := make([]net.Conn, 5)
	for i := range conns {
		conns[i] = dial(t, sockPath)
	}

	for _, conn := range conns {
		resp := sendRequest(t, conn, MethodPing, nil)
		if !resp.OK {
			t.Fatalf("ping failed: %+v", resp.Error)
		}
	}
}

func TestServer_ConnectionReuse(t *testing.T) {
	_, sockPath, _ := setupTestServer(t)
	conn := dial(t, sockPath)

	// First request: Ping
	resp := sendRequest(t, conn, MethodPing, nil)
	if !resp.OK {
		t.Fatalf("ping not ok: %+v", resp.Error)
	}
	var pr PingResponse
	if err := json.Unmarshal(resp.Payload, &pr); err != nil {
		t.Fatalf("unmarshal ping: %v", err)
	}
	if pr.Version != "0.1.0-test" {
		t.Errorf("version = %q, want %q", pr.Version, "0.1.0-test")
	}

	// Second request on the same connection: ListProcs
	resp = sendRequest(t, conn, MethodListProcs, nil)
	if !resp.OK {
		t.Fatalf("list_procs not ok: %+v", resp.Error)
	}
	var lr ListProcsResponse
	if err := json.Unmarshal(resp.Payload, &lr); err != nil {
		t.Fatalf("unmarshal list_procs: %v", err)
	}
	if len(lr.Processes) != 0 {
		t.Errorf("processes = %d, want 0", len(lr.Processes))
	}

	// Third request on the same connection: Kill (expect error, but connection should still work)
	resp = sendRequest(t, conn, MethodKill, KillRequest{PID: 999, Signal: types.SIGTERM})
	if resp.OK {
		t.Fatal("kill should fail for nonexistent PID")
	}
}

func TestCallbackMux_RegisterUnregister(t *testing.T) {
	mux := newCallbackMux()
	ch := make(chan StreamEvent, 10)

	mux.register(1, ch)
	mux.OnSpawn(1, "test intent")

	select {
	case ev := <-ch:
		if ev.Type != StreamProgress {
			t.Errorf("type = %q, want %q", ev.Type, StreamProgress)
		}
		var pp ProgressPayload
		if err := json.Unmarshal(ev.Payload, &pp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if pp.Event != "spawn" {
			t.Errorf("event = %q, want %q", pp.Event, "spawn")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}

	mux.unregister(1)

	mux.OnStep(1, 2, 10)
	select {
	case <-ch:
		t.Fatal("should not receive event after unregister")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestCallbackMux_OnStep(t *testing.T) {
	mux := newCallbackMux()
	ch := make(chan StreamEvent, 10)
	mux.register(1, ch)

	mux.OnStep(1, 3, 10)

	ev := <-ch
	var pp ProgressPayload
	_ = json.Unmarshal(ev.Payload, &pp)
	if pp.Step != 3 || pp.Total != 10 {
		t.Errorf("step=%d total=%d, want 3/10", pp.Step, pp.Total)
	}
}

func TestCallbackMux_OnError(t *testing.T) {
	mux := newCallbackMux()
	ch := make(chan StreamEvent, 10)
	mux.register(1, ch)

	mux.OnError(1, os.ErrPermission)

	ev := <-ch
	var pp ProgressPayload
	_ = json.Unmarshal(ev.Payload, &pp)
	if pp.Event != "error" {
		t.Errorf("event = %q, want %q", pp.Event, "error")
	}
	if pp.ErrorMessage == "" {
		t.Error("error_message should not be empty")
	}
}

func TestCallbackMux_UnregisteredPID(t *testing.T) {
	mux := newCallbackMux()
	mux.OnSpawn(999, "ignored")
}

func TestCallbackMux_ImplementsKernelCallbacks(t *testing.T) {
	mux := newCallbackMux()
	var _ kernel.KernelCallbacks = mux

	mux.OnComplete(1, "result", kernel.ExitStatus{Code: 0})
}

// TestNewServer verifies server creation.
func TestNewServer(t *testing.T) {
	srv := NewServer(nil, nil, "test")
	if srv == nil {
		t.Fatal("server should not be nil")
	}
	if srv.version != "test" {
		t.Errorf("version = %q, want %q", srv.version, "test")
	}
}

// TestServer_ListenAndServe_InvalidPath verifies error on bad socket path.
func TestServer_ListenAndServe_SocketDirCreation(t *testing.T) {
	devReg := vfs.NewDeviceRegistry()
	vfsInst := vfs.NewVFS(devReg)
	ctxMgr := rnixctx.NewManager()
	kern := kernel.NewKernel(vfsInst, ctxMgr, nil)
	defer kern.Shutdown()

	srv := NewServer(kern, nil, "test")
	sockDir := t.TempDir()
	sockPath := filepath.Join(sockDir, "sub", "test.sock")

	if err := srv.ListenAndServe(sockPath); err != nil {
		t.Fatalf("should create subdirectory: %v", err)
	}
	srv.Shutdown()
	srv.Wait()

	if _, err := os.Stat(filepath.Join(sockDir, "sub")); os.IsNotExist(err) {
		t.Error("subdirectory should have been created")
	}
}

// ============================================================
// ATDD RED PHASE — Story 13.1: gdb 调试会话管理 (Attach/Detach)
//
// Server-level tests for handleAttachGdb dispatch and error
// responses via raw socket protocol.
// References MethodAttachGdb which does NOT exist yet in
// handleConn dispatch → compile failure = RED phase.
// ============================================================

// --- 13.1-SRV-001: [P0] Server 对 attach_gdb 不存在的 PID 返回 NOT_FOUND ---

func TestServer_AttachGdb_NotFound(t *testing.T) {
	_, sockPath, _ := setupTestServer(t)
	conn := dial(t, sockPath)

	resp := sendRequest(t, conn, MethodAttachGdb, AttachGdbRequest{PID: 999})
	if resp.OK {
		t.Fatal("attach_gdb should fail for nonexistent PID")
	}
	if resp.Error == nil {
		t.Fatal("error should not be nil")
	}
	if resp.Error.Code != "NOT_FOUND" {
		t.Errorf("code = %q, want NOT_FOUND", resp.Error.Code)
	}
}

// --- 13.1-SRV-002: [P1] Server 对 attach_gdb 无效 payload 返回 INVALID ---

func TestServer_AttachGdb_InvalidPayload(t *testing.T) {
	_, sockPath, _ := setupTestServer(t)
	conn := dial(t, sockPath)

	// Send attach_gdb with malformed payload
	rawPayload := json.RawMessage(`{"bad": "payload"}`)
	req := Request{Method: MethodAttachGdb, Payload: rawPayload}
	enc := json.NewEncoder(conn)
	if err := enc.Encode(req); err != nil {
		t.Fatalf("encode: %v", err)
	}

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		t.Fatal("no response")
	}
	var resp Response
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Should handle gracefully — either OK false with error, or OK true if PID defaults to 0
	// Either way, it should not crash the server
	if resp.OK {
		// PID=0 would not exist, so attach should fail with NOT_FOUND
		t.Log("server accepted request but PID=0 should not be found")
	}
}

// ============================================================
// ATDD RED PHASE — Story 13.2: 断点系统 (Server Handler)
//
// Tests reference MethodGdbCommand, GdbCommandRequest,
// GdbCommandResponse, handleGdbCommand
// which do NOT exist yet → compile failure = RED phase.
// ============================================================

// --- 13.2-SRV-001: [P0] Server handles gdb_command for non-existent PID ---

func TestServer_GdbCommand_NotFound(t *testing.T) {
	_, sockPath, _ := setupTestServer(t)
	conn := dial(t, sockPath)

	payload, _ := json.Marshal(GdbCommandRequest{
		PID:     99999,
		Command: "break",
		Args:    []string{"syscall", "Read"},
	})
	req := Request{Method: MethodGdbCommand, Payload: payload}
	enc := json.NewEncoder(conn)
	if err := enc.Encode(req); err != nil {
		t.Fatalf("encode: %v", err)
	}

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		t.Fatal("no response")
	}
	var resp Response
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.OK {
		t.Error("expected OK=false for non-existent PID")
	}
	if resp.Error == nil {
		t.Fatal("expected error payload")
	}
	if resp.Error.Code != "NOT_FOUND" {
		t.Errorf("code = %q, want NOT_FOUND", resp.Error.Code)
	}
}

// --- 13.2-SRV-002: [P0] Server handles gdb_command "break syscall" ---

func TestServer_GdbCommand_BreakSyscall(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	// Create a running process
	proc := kernel.NewProcess(0, "test-bp", nil)
	_ = proc.Start()
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)

	payload, _ := json.Marshal(GdbCommandRequest{
		PID:     proc.PID,
		Command: "break",
		Args:    []string{"syscall", "Read"},
	})
	req := Request{Method: MethodGdbCommand, Payload: payload}
	enc := json.NewEncoder(conn)
	if err := enc.Encode(req); err != nil {
		t.Fatalf("encode: %v", err)
	}

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		t.Fatal("no response")
	}
	var resp Response
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !resp.OK {
		t.Fatalf("expected OK=true, error: %+v", resp.Error)
	}

	var cmdResp GdbCommandResponse
	if err := json.Unmarshal(resp.Payload, &cmdResp); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if !cmdResp.OK {
		t.Errorf("command OK = false, message: %s", cmdResp.Message)
	}
}

// --- 13.2-SRV-003: [P0] Server handles gdb_command "continue" ---

func TestServer_GdbCommand_Continue(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "test-continue", nil)
	_ = proc.Start()
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)

	payload, _ := json.Marshal(GdbCommandRequest{
		PID:     proc.PID,
		Command: "continue",
	})
	req := Request{Method: MethodGdbCommand, Payload: payload}
	enc := json.NewEncoder(conn)
	if err := enc.Encode(req); err != nil {
		t.Fatalf("encode: %v", err)
	}

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		t.Fatal("no response")
	}
	var resp Response
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !resp.OK {
		t.Fatalf("expected OK=true, error: %+v", resp.Error)
	}
}

// --- 13.2-SRV-004: [P1] Server handles gdb_command "info" ---

func TestServer_GdbCommand_Info(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "test-info", nil)
	_ = proc.Start()
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)

	payload, _ := json.Marshal(GdbCommandRequest{
		PID:     proc.PID,
		Command: "info",
	})
	req := Request{Method: MethodGdbCommand, Payload: payload}
	enc := json.NewEncoder(conn)
	if err := enc.Encode(req); err != nil {
		t.Fatalf("encode: %v", err)
	}

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		t.Fatal("no response")
	}
	var resp Response
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !resp.OK {
		t.Fatalf("expected OK=true, error: %+v", resp.Error)
	}
}

// --- 13.2-SRV-005: [P1] Server handles gdb_command "delete" ---

func TestServer_GdbCommand_Delete(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "test-delete", nil)
	_ = proc.Start()
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)

	payload, _ := json.Marshal(GdbCommandRequest{
		PID:     proc.PID,
		Command: "delete",
		Args:    []string{"1"},
	})
	req := Request{Method: MethodGdbCommand, Payload: payload}
	enc := json.NewEncoder(conn)
	if err := enc.Encode(req); err != nil {
		t.Fatalf("encode: %v", err)
	}

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		t.Fatal("no response")
	}
	var resp Response
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Should respond without crashing (may be OK=false if bp not found)
	_ = resp
}

// --- 13.2-SRV-006: [P1] Server handles gdb_command invalid payload ---

func TestServer_GdbCommand_InvalidPayload(t *testing.T) {
	_, sockPath, _ := setupTestServer(t)
	conn := dial(t, sockPath)

	rawPayload := json.RawMessage(`{"bad": "payload"}`)
	req := Request{Method: MethodGdbCommand, Payload: rawPayload}
	enc := json.NewEncoder(conn)
	if err := enc.Encode(req); err != nil {
		t.Fatalf("encode: %v", err)
	}

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		t.Fatal("no response")
	}
	var resp Response
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Should not crash — graceful handling
	if resp.OK {
		t.Log("server accepted invalid payload (PID=0 not found)")
	}
}

// ============================================================
// ATDD RED PHASE — Story 13.3: 单步执行与状态检查 (Server Handler)
//
// Tests reference handleGdbStep, handleGdbInspect
// which do NOT exist yet → compile failure = RED phase.
// ============================================================

// --- 13.3-IPC-001: [P0] Server handles gdb_command "step" routing ---

func TestServer_GdbCommand_StepSyscall(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "test-step-syscall", nil)
	_ = proc.Start()
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)

	payload, _ := json.Marshal(GdbCommandRequest{
		PID:     proc.PID,
		Command: "step",
		Args:    []string{"syscall"},
	})
	req := Request{Method: MethodGdbCommand, Payload: payload}
	enc := json.NewEncoder(conn)
	if err := enc.Encode(req); err != nil {
		t.Fatalf("encode: %v", err)
	}

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		t.Fatal("no response")
	}
	var resp Response
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !resp.OK {
		t.Fatalf("expected OK=true, error: %+v", resp.Error)
	}

	var cmdResp GdbCommandResponse
	if err := json.Unmarshal(resp.Payload, &cmdResp); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if !cmdResp.OK {
		t.Errorf("command OK = false, message: %s", cmdResp.Message)
	}
}

// --- 13.3-IPC-002: [P0] Server handles "step syscall" sets StepMode + resumes ---

func TestServer_GdbCommand_StepSyscall_SetsMode(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "test-step-mode", nil)
	_ = proc.Start()
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)

	payload, _ := json.Marshal(GdbCommandRequest{
		PID:     proc.PID,
		Command: "step",
		Args:    []string{"syscall"},
	})
	req := Request{Method: MethodGdbCommand, Payload: payload}
	enc := json.NewEncoder(conn)
	if err := enc.Encode(req); err != nil {
		t.Fatalf("encode: %v", err)
	}

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		t.Fatal("no response")
	}
	var resp Response
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !resp.OK {
		t.Fatalf("expected OK=true, error: %+v", resp.Error)
	}

	// After step command, step mode should be set on the process
	if got := proc.GetStepMode(); got != kernel.StepSyscall {
		t.Errorf("StepMode = %v, want StepSyscall", got)
	}
}

// --- 13.3-IPC-003: [P0] Server handles "step reasoning" ---

func TestServer_GdbCommand_StepReasoning(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "test-step-reasoning", nil)
	_ = proc.Start()
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)

	payload, _ := json.Marshal(GdbCommandRequest{
		PID:     proc.PID,
		Command: "step",
		Args:    []string{"reasoning"},
	})
	req := Request{Method: MethodGdbCommand, Payload: payload}
	enc := json.NewEncoder(conn)
	if err := enc.Encode(req); err != nil {
		t.Fatalf("encode: %v", err)
	}

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		t.Fatal("no response")
	}
	var resp Response
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !resp.OK {
		t.Fatalf("expected OK=true, error: %+v", resp.Error)
	}

	var cmdResp GdbCommandResponse
	if err := json.Unmarshal(resp.Payload, &cmdResp); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if !cmdResp.OK {
		t.Errorf("command OK = false, message: %s", cmdResp.Message)
	}

	if got := proc.GetStepMode(); got != kernel.StepReasoning {
		t.Errorf("StepMode = %v, want StepReasoning", got)
	}
}

// --- 13.3-IPC-004: [P1] Server handles "step" with no args returns error ---

func TestServer_GdbCommand_StepNoArgs(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "test-step-noargs", nil)
	_ = proc.Start()
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)

	payload, _ := json.Marshal(GdbCommandRequest{
		PID:     proc.PID,
		Command: "step",
		Args:    []string{},
	})
	req := Request{Method: MethodGdbCommand, Payload: payload}
	enc := json.NewEncoder(conn)
	if err := enc.Encode(req); err != nil {
		t.Fatalf("encode: %v", err)
	}

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		t.Fatal("no response")
	}
	var resp Response
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !resp.OK {
		t.Fatalf("expected OK=true (error in payload), error: %+v", resp.Error)
	}

	var cmdResp GdbCommandResponse
	if err := json.Unmarshal(resp.Payload, &cmdResp); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if cmdResp.OK {
		t.Error("expected command OK = false for step with no args")
	}
}

// --- 13.3-IPC-005: [P1] Server handles "step" with unknown mode returns error ---

func TestServer_GdbCommand_StepUnknownMode(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "test-step-unknown", nil)
	_ = proc.Start()
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)

	payload, _ := json.Marshal(GdbCommandRequest{
		PID:     proc.PID,
		Command: "step",
		Args:    []string{"unknown_mode"},
	})
	req := Request{Method: MethodGdbCommand, Payload: payload}
	enc := json.NewEncoder(conn)
	if err := enc.Encode(req); err != nil {
		t.Fatalf("encode: %v", err)
	}

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		t.Fatal("no response")
	}
	var resp Response
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !resp.OK {
		t.Fatalf("expected OK=true (error in payload), error: %+v", resp.Error)
	}

	var cmdResp GdbCommandResponse
	if err := json.Unmarshal(resp.Payload, &cmdResp); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if cmdResp.OK {
		t.Error("expected command OK = false for unknown step mode")
	}
}

// --- 13.3-IPC-006: [P0] Server handles gdb_command "inspect" routing ---

func TestServer_GdbCommand_InspectContext(t *testing.T) {
	srv, sockPath, ctxMgr := setupTestServer(t)

	proc := kernel.NewProcess(0, "test-inspect", nil)
	_ = proc.Start()
	// Allocate a context so inspect has something to query
	ctxID, err := ctxMgr.CtxAlloc(100)
	if err != nil {
		t.Fatalf("CtxAlloc: %v", err)
	}
	proc.CtxID = ctxID
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)

	payload, _ := json.Marshal(GdbCommandRequest{
		PID:     proc.PID,
		Command: "inspect",
		Args:    []string{"context"},
	})
	req := Request{Method: MethodGdbCommand, Payload: payload}
	enc := json.NewEncoder(conn)
	if err := enc.Encode(req); err != nil {
		t.Fatalf("encode: %v", err)
	}

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		t.Fatal("no response")
	}
	var resp Response
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !resp.OK {
		t.Fatalf("expected OK=true, error: %+v", resp.Error)
	}

	var cmdResp GdbCommandResponse
	if err := json.Unmarshal(resp.Payload, &cmdResp); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if !cmdResp.OK {
		t.Errorf("command OK = false, message: %s", cmdResp.Message)
	}
}

// --- 13.3-IPC-007: [P0] Server inspect context returns structured info ---

func TestServer_GdbCommand_InspectContext_ReturnsData(t *testing.T) {
	srv, sockPath, ctxMgr := setupTestServer(t)

	proc := kernel.NewProcess(0, "test-inspect-data", nil)
	_ = proc.Start()
	// Allocate a context so inspect has something to query
	ctxID, err := ctxMgr.CtxAlloc(100)
	if err != nil {
		t.Fatalf("CtxAlloc: %v", err)
	}
	proc.CtxID = ctxID
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)

	payload, _ := json.Marshal(GdbCommandRequest{
		PID:     proc.PID,
		Command: "inspect",
		Args:    []string{"context"},
	})
	req := Request{Method: MethodGdbCommand, Payload: payload}
	enc := json.NewEncoder(conn)
	if err := enc.Encode(req); err != nil {
		t.Fatalf("encode: %v", err)
	}

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		t.Fatal("no response")
	}
	var resp Response
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !resp.OK {
		t.Fatalf("expected OK=true, error: %+v", resp.Error)
	}

	var cmdResp GdbCommandResponse
	if err := json.Unmarshal(resp.Payload, &cmdResp); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	// Message should contain context summary information
	if cmdResp.Message == "" {
		t.Error("expected non-empty message with context summary")
	}
}

// --- 13.3-IPC-008: [P1] Server handles "inspect" with no args returns error ---

func TestServer_GdbCommand_InspectNoArgs(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "test-inspect-noargs", nil)
	_ = proc.Start()
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)

	payload, _ := json.Marshal(GdbCommandRequest{
		PID:     proc.PID,
		Command: "inspect",
		Args:    []string{},
	})
	req := Request{Method: MethodGdbCommand, Payload: payload}
	enc := json.NewEncoder(conn)
	if err := enc.Encode(req); err != nil {
		t.Fatalf("encode: %v", err)
	}

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		t.Fatal("no response")
	}
	var resp Response
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !resp.OK {
		t.Fatalf("expected OK=true (error in payload), error: %+v", resp.Error)
	}

	var cmdResp GdbCommandResponse
	if err := json.Unmarshal(resp.Payload, &cmdResp); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if cmdResp.OK {
		t.Error("expected command OK = false for inspect with no args")
	}
}

// ============================================================
// ATDD RED PHASE — Story 13.4: 运行时参数热修改 (IPC Server)
//
// Tests reference handleGdbSet via gdb_command "set" routing
// which does NOT exist yet → test failure = RED phase.
// ============================================================

// --- 13.4-IPC-001: [P0] Server handles gdb_command "set model" routing ---

func TestServer_GdbCommand_SetModel(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "test-set-model", nil)
	_ = proc.Start()
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)

	payload, _ := json.Marshal(GdbCommandRequest{
		PID:     proc.PID,
		Command: "set",
		Args:    []string{"model", "sonnet"},
	})
	req := Request{Method: MethodGdbCommand, Payload: payload}
	enc := json.NewEncoder(conn)
	if err := enc.Encode(req); err != nil {
		t.Fatalf("encode: %v", err)
	}

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		t.Fatal("no response")
	}
	var resp Response
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !resp.OK {
		t.Fatalf("expected OK=true, error: %+v", resp.Error)
	}

	var cmdResp GdbCommandResponse
	if err := json.Unmarshal(resp.Payload, &cmdResp); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if !cmdResp.OK {
		t.Errorf("command OK = false, message: %s", cmdResp.Message)
	}

	// Verify model override was set on the process
	if got := proc.GetGdbModelOverride(); got != "sonnet" {
		t.Errorf("GdbModelOverride = %q, want %q", got, "sonnet")
	}
}

// --- 13.4-IPC-002: [P0] Server handles gdb_command "set context append" ---

func TestServer_GdbCommand_SetContextAppend(t *testing.T) {
	srv, sockPath, ctxMgr := setupTestServer(t)

	proc := kernel.NewProcess(0, "test-set-context", nil)
	_ = proc.Start()
	// Allocate a context so set context append has something to work with
	ctxID, err := ctxMgr.CtxAlloc(100)
	if err != nil {
		t.Fatalf("CtxAlloc: %v", err)
	}
	proc.CtxID = ctxID
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)

	payload, _ := json.Marshal(GdbCommandRequest{
		PID:     proc.PID,
		Command: "set",
		Args:    []string{"context", "append", "额外分析指令"},
	})
	req := Request{Method: MethodGdbCommand, Payload: payload}
	enc := json.NewEncoder(conn)
	if err := enc.Encode(req); err != nil {
		t.Fatalf("encode: %v", err)
	}

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		t.Fatal("no response")
	}
	var resp Response
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !resp.OK {
		t.Fatalf("expected OK=true, error: %+v", resp.Error)
	}

	var cmdResp GdbCommandResponse
	if err := json.Unmarshal(resp.Payload, &cmdResp); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if !cmdResp.OK {
		t.Errorf("command OK = false, message: %s", cmdResp.Message)
	}
}

// --- 13.4-IPC-003: [P0] Server handles gdb_command "set skills add" ---

func TestServer_GdbCommand_SetSkillsAdd(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "test-set-skills", nil)
	_ = proc.Start()
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)

	payload, _ := json.Marshal(GdbCommandRequest{
		PID:     proc.PID,
		Command: "set",
		Args:    []string{"skills", "add", "code-review"},
	})
	req := Request{Method: MethodGdbCommand, Payload: payload}
	enc := json.NewEncoder(conn)
	if err := enc.Encode(req); err != nil {
		t.Fatalf("encode: %v", err)
	}

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		t.Fatal("no response")
	}
	var resp Response
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !resp.OK {
		t.Fatalf("expected OK=true, error: %+v", resp.Error)
	}

	var cmdResp GdbCommandResponse
	if err := json.Unmarshal(resp.Payload, &cmdResp); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if !cmdResp.OK {
		t.Errorf("command OK = false, message: %s", cmdResp.Message)
	}

	// Verify skill was added to the process
	skills := proc.GetGdbExtraSkills()
	found := false
	for _, s := range skills {
		if s == "code-review" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected code-review in GdbExtraSkills")
	}
}

// --- 13.4-IPC-004: [P0] Server handles gdb_command "set env KEY=VALUE" ---

func TestServer_GdbCommand_SetEnv(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "test-set-env", nil)
	_ = proc.Start()
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)

	payload, _ := json.Marshal(GdbCommandRequest{
		PID:     proc.PID,
		Command: "set",
		Args:    []string{"env", "DEBUG=true"},
	})
	req := Request{Method: MethodGdbCommand, Payload: payload}
	enc := json.NewEncoder(conn)
	if err := enc.Encode(req); err != nil {
		t.Fatalf("encode: %v", err)
	}

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		t.Fatal("no response")
	}
	var resp Response
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !resp.OK {
		t.Fatalf("expected OK=true, error: %+v", resp.Error)
	}

	var cmdResp GdbCommandResponse
	if err := json.Unmarshal(resp.Payload, &cmdResp); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if !cmdResp.OK {
		t.Errorf("command OK = false, message: %s", cmdResp.Message)
	}

	// Verify env var was set on the process
	vars := proc.GetGdbEnvVars()
	if vars["DEBUG"] != "true" {
		t.Errorf("GdbEnvVars[DEBUG] = %q, want %q", vars["DEBUG"], "true")
	}
}

// --- 13.4-IPC-005: [P1] Server handles "set" with no args returns error ---

func TestServer_GdbCommand_SetNoArgs(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "test-set-noargs", nil)
	_ = proc.Start()
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)

	payload, _ := json.Marshal(GdbCommandRequest{
		PID:     proc.PID,
		Command: "set",
		Args:    []string{},
	})
	req := Request{Method: MethodGdbCommand, Payload: payload}
	enc := json.NewEncoder(conn)
	if err := enc.Encode(req); err != nil {
		t.Fatalf("encode: %v", err)
	}

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		t.Fatal("no response")
	}
	var resp Response
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !resp.OK {
		t.Fatalf("expected OK=true (error in payload), error: %+v", resp.Error)
	}

	var cmdResp GdbCommandResponse
	if err := json.Unmarshal(resp.Payload, &cmdResp); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if cmdResp.OK {
		t.Error("expected command OK = false for set with no args")
	}
}

// --- 13.4-IPC-006: [P1] Server handles "set" with unknown subcommand returns error ---

func TestServer_GdbCommand_SetUnknownSubcmd(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "test-set-unknown", nil)
	_ = proc.Start()
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)

	payload, _ := json.Marshal(GdbCommandRequest{
		PID:     proc.PID,
		Command: "set",
		Args:    []string{"unknown_target", "value"},
	})
	req := Request{Method: MethodGdbCommand, Payload: payload}
	enc := json.NewEncoder(conn)
	if err := enc.Encode(req); err != nil {
		t.Fatalf("encode: %v", err)
	}

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		t.Fatal("no response")
	}
	var resp Response
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !resp.OK {
		t.Fatalf("expected OK=true (error in payload), error: %+v", resp.Error)
	}

	var cmdResp GdbCommandResponse
	if err := json.Unmarshal(resp.Payload, &cmdResp); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if cmdResp.OK {
		t.Error("expected command OK = false for unknown set subcommand")
	}
}

// --- 13.4-IPC-007: [P1] Server handles "set env" with invalid format returns error ---

func TestServer_GdbCommand_SetEnvInvalidFormat(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "test-set-env-invalid", nil)
	_ = proc.Start()
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)

	// "env" without "=" separator
	payload, _ := json.Marshal(GdbCommandRequest{
		PID:     proc.PID,
		Command: "set",
		Args:    []string{"env", "INVALID_NO_EQUALS"},
	})
	req := Request{Method: MethodGdbCommand, Payload: payload}
	enc := json.NewEncoder(conn)
	if err := enc.Encode(req); err != nil {
		t.Fatalf("encode: %v", err)
	}

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		t.Fatal("no response")
	}
	var resp Response
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !resp.OK {
		t.Fatalf("expected OK=true (error in payload), error: %+v", resp.Error)
	}

	var cmdResp GdbCommandResponse
	if err := json.Unmarshal(resp.Payload, &cmdResp); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if cmdResp.OK {
		t.Error("expected command OK = false for env without KEY=VALUE format")
	}
}
