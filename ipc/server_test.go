package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/agents"
	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/debug"
	"github.com/rnixai/rnix/internal/config"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
	"github.com/rnixai/rnix/skills"
	"github.com/rnixai/rnix/vfs"
)

func setupTestServer(t *testing.T) (*Server, string, *rnixctx.Manager) {
	t.Helper()

	devReg := vfs.NewDeviceRegistry()
	// Register a no-op mock for /dev/llm/claude so tests that exercise Resume
	// (Story 44.3 AC#4 / AC#8) can open the LLM device without a real driver.
	// Returning a non-StreamObserver file means setupDriverStreamHandler is a
	// no-op, and the resumed process's reasonStep loop will exit on its first
	// LLM call attempt — that's enough for AC#4 to assert state transitions.
	_ = devReg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &noopLLMFile{}, nil
	})
	vfsInst := vfs.NewVFS(devReg)
	ctxMgr := rnixctx.NewManager()

	srv := NewServer(nil, nil, "0.1.0-test", "", "")
	kern := kernel.NewKernel(vfsInst, ctxMgr, srv.CallbackMux())
	srv.kern = kern
	srv.SetContextManager(ctxMgr)

	// Initialize record manager for recording tests
	recordDir := filepath.Join(t.TempDir(), "records")
	kern.SetRecordManager(debug.NewRecordManager(recordDir))

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

// noopLLMFile is a minimal vfs.VFSFile implementation used as a placeholder
// LLM device file in setupTestServer. It does NOT implement StreamObserver
// so kernel.setupDriverStreamHandler skips it; Write returns success without
// emitting any tool/text deltas, which is enough for tests that exercise the
// process-state side of Resume (Story 44.3) without needing a real driver.
type noopLLMFile struct{}

func (f *noopLLMFile) Read(_ int) ([]byte, error)                       { return nil, nil }
func (f *noopLLMFile) Write(_ context.Context, _ []byte) error          { return nil }
func (f *noopLLMFile) Close() error                                     { return nil }
func (f *noopLLMFile) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{Name: "/dev/llm/claude", IsDevice: true, DevicePath: "/dev/llm/claude"}, nil
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

// --- 15-4: ctx_profile handler tests ---

func TestServer_CtxProfile_InvalidPID(t *testing.T) {
	_, sockPath, _ := setupTestServer(t)
	conn := dial(t, sockPath)

	resp := sendRequest(t, conn, MethodCtxProfile, CtxProfileRequest{PID: 999})
	if resp.OK {
		t.Fatal("ctx_profile should fail for nonexistent PID")
	}
	if resp.Error == nil {
		t.Fatal("error should not be nil")
	}
	if resp.Error.Code != "NOT_FOUND" {
		t.Errorf("code = %q, want NOT_FOUND", resp.Error.Code)
	}
}

func TestServer_CtxProfile_WrongState(t *testing.T) {
	srv, sockPath, ctxMgr := setupTestServer(t)

	proc := kernel.NewProcess(0, "test-ctx-profile-dead", nil)
	// Do not call Start() - process stays in Created state
	ctxID, err := ctxMgr.CtxAlloc(100)
	if err != nil {
		t.Fatalf("CtxAlloc: %v", err)
	}
	proc.CtxID = ctxID
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)

	resp := sendRequest(t, conn, MethodCtxProfile, CtxProfileRequest{PID: proc.PID})
	if resp.OK {
		t.Fatal("ctx_profile should fail for non-Running/Zombie process")
	}
	if resp.Error == nil {
		t.Fatal("error should not be nil")
	}
	if resp.Error.Code != "INVALID" {
		t.Errorf("code = %q, want INVALID", resp.Error.Code)
	}
}

func TestServer_CtxProfile_ValidPID_Running(t *testing.T) {
	srv, sockPath, ctxMgr := setupTestServer(t)

	proc := kernel.NewProcess(0, "test-ctx-profile", nil)
	_ = proc.Start()
	ctxID, err := ctxMgr.CtxAlloc(100)
	if err != nil {
		t.Fatalf("CtxAlloc: %v", err)
	}
	proc.CtxID = ctxID
	_ = ctxMgr.SetSystemPrompt(ctxID, "You are a helpful assistant.")
	_ = ctxMgr.AppendMessage(ctxID, rnixctx.RoleUser, "Hello")
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)

	resp := sendRequest(t, conn, MethodCtxProfile, CtxProfileRequest{PID: proc.PID})
	if !resp.OK {
		t.Fatalf("ctx_profile failed: %+v", resp.Error)
	}

	var result debug.CtxProfileResult
	if err := json.Unmarshal(resp.Payload, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result.PID != proc.PID {
		t.Errorf("PID = %d, want %d", result.PID, proc.PID)
	}
	if result.CtxID != ctxID {
		t.Errorf("CtxID = %d, want %d", result.CtxID, ctxID)
	}
	if result.Classification.Active.Messages == 0 && result.Classification.Active.Tokens == 0 {
		t.Error("expected non-empty classification (active)")
	}
}

func TestServer_CtxProfile_InvalidPayload(t *testing.T) {
	_, sockPath, _ := setupTestServer(t)
	conn := dial(t, sockPath)

	// Payload with invalid PID type (string instead of number) fails Unmarshal
	req := Request{Method: MethodCtxProfile, Payload: json.RawMessage(`{"pid":"not-a-number"}`)}
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
		t.Fatal("ctx_profile should fail for invalid payload")
	}
	if resp.Error == nil || resp.Error.Code != "INVALID" {
		t.Errorf("error = %+v, want INVALID", resp.Error)
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
	mux.OnSpawn(1, "test intent", "claude", "sonnet", "019534a1-7c6b-7000-8abc-123456789012")

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
	mux.OnSpawn(999, "ignored", "", "", "")
}

func TestCallbackMux_ImplementsKernelCallbacks(t *testing.T) {
	mux := newCallbackMux()
	var _ kernel.KernelCallbacks = mux

	mux.OnComplete(1, "result", kernel.ExitStatus{Code: 0})
}

// TestNewServer verifies server creation.
func TestNewServer(t *testing.T) {
	srv := NewServer(nil, nil, "test", "", "")
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

	srv := NewServer(kern, nil, "test", "", "")
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
	found := slices.Contains(skills, "code-review")
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

// ============================================================
// ATDD RED PHASE — Story 14.1: 执行录制与持久化 (IPC Server)
//
// Tests reference MethodRecordStart, MethodRecordStop, RecordStartRequest,
// RecordStopRequest, handleRecordCommand which do NOT exist yet
// → compile failure = RED phase.
// ============================================================

// --- 14.1-IPC-001: [P0] Server handles record_start 返回 record_id ---

func TestServer_RecordStart(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "test-record-start", nil)
	_ = proc.Start()
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)

	resp := sendRequest(t, conn, MethodRecordStart, RecordStartRequest{PID: proc.PID})
	if !resp.OK {
		t.Fatalf("record_start not ok: %+v", resp.Error)
	}

	var rr RecordStartResponse
	if err := json.Unmarshal(resp.Payload, &rr); err != nil {
		t.Fatalf("unmarshal RecordStartResponse: %v", err)
	}
	if rr.RecordID == "" {
		t.Error("expected non-empty RecordID")
	}
}

// --- 14.1-IPC-002: [P0] Server handles record_stop 返回 event_count ---

func TestServer_RecordStop(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "test-record-stop", nil)
	_ = proc.Start()
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)

	// First start recording
	resp := sendRequest(t, conn, MethodRecordStart, RecordStartRequest{PID: proc.PID})
	if !resp.OK {
		t.Fatalf("record_start not ok: %+v", resp.Error)
	}

	// Reuse connection (connection reuse model)
	conn2 := dial(t, sockPath)

	// Then stop recording
	resp = sendRequest(t, conn2, MethodRecordStop, RecordStopRequest{PID: proc.PID})
	if !resp.OK {
		t.Fatalf("record_stop not ok: %+v", resp.Error)
	}

	var rr RecordStopResponse
	if err := json.Unmarshal(resp.Payload, &rr); err != nil {
		t.Fatalf("unmarshal RecordStopResponse: %v", err)
	}
	// EventCount may be 0 if no events were written
	_ = rr.EventCount
}

// --- 14.1-IPC-003: [P1] Server handles record_start 不存在的 PID 返回错误 ---

func TestServer_RecordStart_NotFound(t *testing.T) {
	_, sockPath, _ := setupTestServer(t)
	conn := dial(t, sockPath)

	resp := sendRequest(t, conn, MethodRecordStart, RecordStartRequest{PID: 99999})
	if resp.OK {
		t.Fatal("record_start should fail for nonexistent PID")
	}
	if resp.Error == nil {
		t.Fatal("error should not be nil")
	}
	if resp.Error.Code != "NOT_FOUND" {
		t.Errorf("code = %q, want NOT_FOUND", resp.Error.Code)
	}
}

// --- 14.1-IPC-004: [P1] Server handles record_start Running 进程验证 ---

func TestServer_RecordStart_RequiresRunning(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "test-record-not-running", nil)
	// Do NOT call proc.Start() — process is in Created state
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)

	resp := sendRequest(t, conn, MethodRecordStart, RecordStartRequest{PID: proc.PID})
	if resp.OK {
		t.Fatal("record_start should fail for non-Running process")
	}
}

// ============================================================
// ATDD RED PHASE — Story 14-2: 录制回放与导航 (IPC Server)
//
// Tests reference MethodReplayLoad, ReplayLoadRequest,
// ReplayLoadResponse, handleReplayLoad which do NOT exist yet
// → compile failure = RED phase.
// ============================================================

// --- 14.2-IPC-001: [P0] Server handles replay_load with valid record ---

func TestServer_ReplayLoad(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	// Create and close a recording to have a valid record on disk
	proc := kernel.NewProcess(0, "test-replay-load", nil)
	_ = proc.Start()
	srv.kern.AddProcess(proc)

	conn1 := dial(t, sockPath)
	resp := sendRequest(t, conn1, MethodRecordStart, RecordStartRequest{PID: proc.PID})
	if !resp.OK {
		t.Fatalf("record_start not ok: %+v", resp.Error)
	}
	var startResp RecordStartResponse
	json.Unmarshal(resp.Payload, &startResp)
	recordID := startResp.RecordID

	conn2 := dial(t, sockPath)
	resp = sendRequest(t, conn2, MethodRecordStop, RecordStopRequest{PID: proc.PID})
	if !resp.OK {
		t.Fatalf("record_stop not ok: %+v", resp.Error)
	}

	// Now test replay_load
	conn3 := dial(t, sockPath)
	resp = sendRequest(t, conn3, MethodReplayLoad, ReplayLoadRequest{RecordID: recordID})
	if !resp.OK {
		t.Fatalf("replay_load not ok: %+v", resp.Error)
	}

	var rr ReplayLoadResponse
	if err := json.Unmarshal(resp.Payload, &rr); err != nil {
		t.Fatalf("unmarshal ReplayLoadResponse: %v", err)
	}
	if rr.RecordID != recordID {
		t.Errorf("RecordID = %q, want %q", rr.RecordID, recordID)
	}
	if rr.Status != "stopped" {
		t.Errorf("Status = %q, want 'stopped'", rr.Status)
	}
}

// --- 14.2-IPC-002: [P0] Server handles replay_load with non-existent record ---

func TestServer_ReplayLoad_NotFound(t *testing.T) {
	_, sockPath, _ := setupTestServer(t)
	conn := dial(t, sockPath)

	resp := sendRequest(t, conn, MethodReplayLoad, ReplayLoadRequest{RecordID: "nonexistent-id"})
	if resp.OK {
		t.Fatal("replay_load should fail for non-existent record")
	}
	if resp.Error == nil {
		t.Fatal("error should not be nil")
	}
}

// ============================================================
// ATDD RED PHASE — Story 14-4: Fork-Continue 分支探索 (IPC Server)
//
// Tests reference MethodForkContinue, ForkContinueRequest,
// ForkContinueResponse, handleForkContinue which do NOT exist yet
// → compile failure = RED phase.
// ============================================================

// --- 14.4-IPC-001: [P0] Server handles fork_continue 创建新进程 ---

func TestServer_ForkContinue(t *testing.T) {
	_, sockPath, _ := setupTestServer(t)
	conn := dial(t, sockPath)

	resp := sendRequest(t, conn, MethodForkContinue, ForkContinueRequest{
		Intent:       "分析代码",
		SystemPrompt: "You are a helpful assistant",
		Messages: []ForkMessageWire{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "hi there"},
			{Role: "user", Content: "请用另一种方式优化"},
		},
		OriginalPID: 42,
	})
	if !resp.OK {
		t.Fatalf("fork_continue not ok: %+v", resp.Error)
	}

	var fcr ForkContinueResponse
	if err := json.Unmarshal(resp.Payload, &fcr); err != nil {
		t.Fatalf("unmarshal ForkContinueResponse: %v", err)
	}
	if fcr.PID == 0 {
		t.Error("expected non-zero PID for forked process")
	}
}

// --- 14.4-IPC-002: [P0] Server handles fork_continue 消息历史回放 ---

func TestServer_ForkContinue_MessagesReplayed(t *testing.T) {
	srv, sockPath, ctxMgr := setupTestServer(t)
	conn := dial(t, sockPath)

	msgs := []ForkMessageWire{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "second"},
		{Role: "user", Content: "third"},
	}

	resp := sendRequest(t, conn, MethodForkContinue, ForkContinueRequest{
		Intent:       "test fork",
		SystemPrompt: "You are a test assistant",
		Messages:     msgs,
		OriginalPID:  0,
	})
	if !resp.OK {
		t.Fatalf("fork_continue not ok: %+v", resp.Error)
	}

	var fcr ForkContinueResponse
	if err := json.Unmarshal(resp.Payload, &fcr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if fcr.PID == 0 {
		t.Fatal("expected non-zero PID")
	}

	// Verify messages were replayed to the new context
	proc, ok := srv.kern.GetProcess(fcr.PID)
	if !ok {
		t.Fatal("forked process not found in kernel")
	}
	info, err := ctxMgr.GetContextInfo(proc.CtxID)
	if err != nil {
		t.Fatalf("GetContextInfo: %v", err)
	}
	// 3 messages (system messages are skipped, so user+assistant+user = 3)
	totalMsgs, _ := info["total_messages"].(int)
	if totalMsgs != 3 {
		t.Errorf("expected 3 messages replayed to context, got %d", totalMsgs)
	}
}

// --- 14.4-IPC-003: [P0] Server handles fork_continue 新进程 PPID 指向原录制进程 ---

func TestServer_ForkContinue_PPID(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	// Create an original process so PPID can point to it
	origProc := kernel.NewProcess(0, "original-process", nil)
	_ = origProc.Start()
	srv.kern.AddProcess(origProc)

	conn := dial(t, sockPath)

	resp := sendRequest(t, conn, MethodForkContinue, ForkContinueRequest{
		Intent:       "fork test",
		SystemPrompt: "test",
		Messages:     []ForkMessageWire{{Role: "user", Content: "hello"}},
		OriginalPID:  uint64(origProc.PID),
	})
	if !resp.OK {
		t.Fatalf("fork_continue not ok: %+v", resp.Error)
	}

	var fcr ForkContinueResponse
	if err := json.Unmarshal(resp.Payload, &fcr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// The new process should have PPID pointing to original process
	if fcr.PPID != types.PID(origProc.PID) {
		t.Errorf("PPID = %d, want %d", fcr.PPID, origProc.PID)
	}
}

// --- 14.4-IPC-004: [P1] Server handles fork_continue 原进程不存在时 PPID=0 ---

func TestServer_ForkContinue_OriginalPIDNotFound(t *testing.T) {
	_, sockPath, _ := setupTestServer(t)
	conn := dial(t, sockPath)

	resp := sendRequest(t, conn, MethodForkContinue, ForkContinueRequest{
		Intent:       "fork test",
		SystemPrompt: "test",
		Messages:     []ForkMessageWire{{Role: "user", Content: "hello"}},
		OriginalPID:  99999, // Non-existent PID
	})
	if !resp.OK {
		t.Fatalf("fork_continue not ok: %+v", resp.Error)
	}

	var fcr ForkContinueResponse
	if err := json.Unmarshal(resp.Payload, &fcr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// When original PID doesn't exist, PPID should be 0 (top-level)
	if fcr.PPID != 0 {
		t.Errorf("PPID = %d, want 0 when original PID doesn't exist", fcr.PPID)
	}
}

// --- 14.4-IPC-005: [P1] Server handles fork_continue 空消息列表 ---

func TestServer_ForkContinue_EmptyMessages(t *testing.T) {
	_, sockPath, _ := setupTestServer(t)
	conn := dial(t, sockPath)

	resp := sendRequest(t, conn, MethodForkContinue, ForkContinueRequest{
		Intent:       "fork test",
		SystemPrompt: "test",
		Messages:     []ForkMessageWire{},
		OriginalPID:  0,
	})
	if !resp.OK {
		t.Fatalf("fork_continue with empty messages not ok: %+v", resp.Error)
	}

	var fcr ForkContinueResponse
	if err := json.Unmarshal(resp.Payload, &fcr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if fcr.PID == 0 {
		t.Error("expected non-zero PID even with empty messages")
	}
}

// --- 15-5: ctx_growth handler tests ---

func TestServer_CtxGrowth_InvalidPID(t *testing.T) {
	_, sockPath, _ := setupTestServer(t)
	conn := dial(t, sockPath)

	resp := sendRequest(t, conn, MethodCtxGrowth, CtxGrowthRequest{PID: 999})
	if resp.OK {
		t.Fatal("ctx_growth should fail for nonexistent PID")
	}
	if resp.Error == nil {
		t.Fatal("error should not be nil")
	}
	if resp.Error.Code != "NOT_FOUND" {
		t.Errorf("code = %q, want NOT_FOUND", resp.Error.Code)
	}
}

func TestServer_CtxGrowth_WrongState(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "test-ctx-growth-created", nil)
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)

	resp := sendRequest(t, conn, MethodCtxGrowth, CtxGrowthRequest{PID: proc.PID})
	if resp.OK {
		t.Fatal("ctx_growth should fail for non-Running process")
	}
	if resp.Error == nil {
		t.Fatal("error should not be nil")
	}
	if resp.Error.Code != "INVALID" {
		t.Errorf("code = %q, want INVALID", resp.Error.Code)
	}
}

func TestServer_CtxGrowth_ValidPID_Running(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)

	proc := kernel.NewProcess(0, "test-ctx-growth", nil)
	_ = proc.Start()
	proc.TokensUsed = 1500
	proc.ContextBudget = 8000
	proc.AppendTokenSnapshot(1, 500)
	proc.AppendTokenSnapshot(2, 1000)
	proc.AppendTokenSnapshot(3, 1500)
	srv.kern.AddProcess(proc)

	conn := dial(t, sockPath)

	resp := sendRequest(t, conn, MethodCtxGrowth, CtxGrowthRequest{PID: proc.PID})
	if !resp.OK {
		t.Fatalf("ctx_growth failed: %+v", resp.Error)
	}

	var result debug.GrowthPrediction
	if err := json.Unmarshal(resp.Payload, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result.PID != proc.PID {
		t.Errorf("PID = %d, want %d", result.PID, proc.PID)
	}
	if result.TokensUsed != 1500 {
		t.Errorf("TokensUsed = %d, want 1500", result.TokensUsed)
	}
	if result.ContextBudget != 8000 {
		t.Errorf("ContextBudget = %d, want 8000", result.ContextBudget)
	}
	if result.AlertLevel != "none" {
		t.Errorf("AlertLevel = %q, want 'none'", result.AlertLevel)
	}
	if len(result.History) != 3 {
		t.Errorf("History len = %d, want 3", len(result.History))
	}
}

// ============================================================
// Story 25-3: Project Config Merge & Module Adaptation
//
// Tests verify resolveProjectContext behavior.
// ============================================================

// --- 25.3-SRV-001: Empty projectDir returns nil config and global loader ---

func TestResolveProjectContext_EmptyProjectDir(t *testing.T) {
	srv := NewServer(nil, nil, "0.1.0-test", "", "")

	// Set a mock agent loader so we can verify it is returned
	mockLoader := func(name string) (*agents.AgentInfo, error) {
		return nil, nil
	}
	srv.agentLoader = mockLoader

	projCfg, loaderFn, err := srv.resolveProjectContext("", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if projCfg != nil {
		t.Errorf("expected nil ProjectConfig for empty projectDir, got %+v", projCfg)
	}
	if loaderFn == nil {
		t.Error("expected non-nil loader function for empty projectDir")
	}
}

// --- 25.3-SRV-002: Empty projectDir with no global config ---

func TestResolveProjectContext_EmptyProjectDir_NoGlobalConfig(t *testing.T) {
	srv := NewServer(nil, nil, "0.1.0-test", "", "")
	// globalConfig is nil

	projCfg, loaderFn, err := srv.resolveProjectContext("", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if projCfg != nil {
		t.Errorf("expected nil ProjectConfig, got %+v", projCfg)
	}
	// loaderFn should be s.agentLoader (nil in this case since not set)
	if loaderFn != nil {
		t.Error("expected nil loader when agentLoader is not set")
	}
}

// --- 25.3-SRV-003: Non-empty projectDir but no global config falls back ---

func TestResolveProjectContext_WithProjectDir_NoGlobalConfig(t *testing.T) {
	srv := NewServer(nil, nil, "0.1.0-test", "", "")
	// globalConfig is nil, so even with projectDir, should fall back to global-only mode

	projCfg, _, err := srv.resolveProjectContext("/some/project", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if projCfg != nil {
		t.Errorf("expected nil ProjectConfig when no global config, got %+v", projCfg)
	}
}

// --- 25.3-SRV-004: Non-empty projectDir with global config returns ProjectConfig ---

func TestResolveProjectContext_WithProjectDir(t *testing.T) {
	srv := NewServer(nil, nil, "0.1.0-test", "", "")

	// Set up global config
	globalAgentsDir := t.TempDir()
	globalSkillsDir := t.TempDir()
	srv.SetGlobalConfig(&config.GlobalConfig{
		Dir:       t.TempDir(),
		AgentsDir: globalAgentsDir,
		SkillsDir: globalSkillsDir,
	})

	// Create a project directory (no providers.yaml, so no merge needed)
	projectDir := t.TempDir()
	rnixDir := filepath.Join(projectDir, ".rnix")
	if err := os.MkdirAll(rnixDir, 0o755); err != nil {
		t.Fatalf("mkdir .rnix: %v", err)
	}

	projCfg, loaderFn, err := srv.resolveProjectContext(projectDir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if projCfg == nil {
		t.Fatal("expected non-nil ProjectConfig for valid projectDir")
	}
	if projCfg.ProjectDir != projectDir {
		t.Errorf("ProjectDir = %q, want %q", projCfg.ProjectDir, projectDir)
	}

	// Verify agent dirs: project first, global second
	expectedAgentDir := filepath.Join(projectDir, ".rnix", "agents")
	if len(projCfg.AgentDirs) != 2 {
		t.Fatalf("AgentDirs length = %d, want 2", len(projCfg.AgentDirs))
	}
	if projCfg.AgentDirs[0] != expectedAgentDir {
		t.Errorf("AgentDirs[0] = %q, want %q", projCfg.AgentDirs[0], expectedAgentDir)
	}
	if projCfg.AgentDirs[1] != globalAgentsDir {
		t.Errorf("AgentDirs[1] = %q, want %q", projCfg.AgentDirs[1], globalAgentsDir)
	}

	// Verify skill dirs: project first, global second
	expectedSkillDir := filepath.Join(projectDir, ".rnix", "skills")
	if len(projCfg.SkillDirs) != 2 {
		t.Fatalf("SkillDirs length = %d, want 2", len(projCfg.SkillDirs))
	}
	if projCfg.SkillDirs[0] != expectedSkillDir {
		t.Errorf("SkillDirs[0] = %q, want %q", projCfg.SkillDirs[0], expectedSkillDir)
	}
	if projCfg.SkillDirs[1] != globalSkillsDir {
		t.Errorf("SkillDirs[1] = %q, want %q", projCfg.SkillDirs[1], globalSkillsDir)
	}

	// Verify the loader function is returned (project-aware)
	if loaderFn == nil {
		t.Error("expected non-nil project-aware loader function")
	}

	// Regression guard for the "subagent uses wrong provider/model" bug:
	// ProjectConfig must carry the project-aware AgentLoader and SkillLoader
	// so kernel/tool_exec.go can route ActionSpawn / ActionSpecialize through
	// `.rnix/agents/` and `.rnix/skills/` instead of falling back to the
	// daemon's global loaders.
	if projCfg.AgentLoader == nil {
		t.Error("expected ProjectConfig.AgentLoader to be populated (regression: subagent spawn would ignore .rnix/agents overrides)")
	}
	if projCfg.SkillLoader == nil {
		t.Error("expected ProjectConfig.SkillLoader to be populated (regression: specialize would ignore .rnix/skills overrides)")
	}
}

// TestResolveProjectContext_AgentLoaderHonorsProjectOverride exercises the
// full project-loader path end-to-end: write a project-level stem/agent.yaml
// with empty models, write a (stale) global stem/agent.yaml that pins
// provider=claude, then ask the returned AgentLoader for "stem" and assert
// the project version wins. This is the inverse of the bug reported via
// strace: provider=claude [agent] when it should follow project default.
func TestResolveProjectContext_AgentLoaderHonorsProjectOverride(t *testing.T) {
	srv := NewServer(nil, nil, "0.1.0-test", "", "")

	globalDir := t.TempDir()
	globalAgentsDir := filepath.Join(globalDir, "agents")
	globalSkillsDir := filepath.Join(globalDir, "skills")
	if err := os.MkdirAll(filepath.Join(globalAgentsDir, "stem"), 0o755); err != nil {
		t.Fatalf("mkdir global stem dir: %v", err)
	}
	staleStem := []byte("name: stem\nmodels:\n  provider: claude\n  preferred: sonnet\n")
	if err := os.WriteFile(filepath.Join(globalAgentsDir, "stem", "agent.yaml"), staleStem, 0o644); err != nil {
		t.Fatalf("write global stem agent.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(globalAgentsDir, "stem", "instructions.md"), []byte("global"), 0o644); err != nil {
		t.Fatalf("write global stem instructions: %v", err)
	}
	srv.SetGlobalConfig(&config.GlobalConfig{
		Dir:       globalDir,
		AgentsDir: globalAgentsDir,
		SkillsDir: globalSkillsDir,
	})

	projectDir := t.TempDir()
	projStemDir := filepath.Join(projectDir, ".rnix", "agents", "stem")
	if err := os.MkdirAll(projStemDir, 0o755); err != nil {
		t.Fatalf("mkdir project stem dir: %v", err)
	}
	// Project override: empty models — agent should follow project/CLI default.
	cleanStem := []byte("name: stem\nmodels: {}\n")
	if err := os.WriteFile(filepath.Join(projStemDir, "agent.yaml"), cleanStem, 0o644); err != nil {
		t.Fatalf("write project stem agent.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projStemDir, "instructions.md"), []byte("project"), 0o644); err != nil {
		t.Fatalf("write project stem instructions: %v", err)
	}

	projCfg, _, err := srv.resolveProjectContext(projectDir, "")
	if err != nil {
		t.Fatalf("resolveProjectContext: %v", err)
	}
	if projCfg == nil || projCfg.AgentLoader == nil {
		t.Fatal("expected ProjectConfig with non-nil AgentLoader")
	}

	raw, err := projCfg.AgentLoader("stem")
	if err != nil {
		t.Fatalf("AgentLoader(stem): %v", err)
	}
	ai, ok := raw.(*agents.AgentInfo)
	if !ok {
		t.Fatalf("AgentLoader returned %T, want *agents.AgentInfo", raw)
	}
	if ai.Manifest.Models.Provider != "" {
		t.Errorf("Models.Provider = %q, want \"\" (project override clears it; global stale version is being loaded — the bug)", ai.Manifest.Models.Provider)
	}
	if ai.Manifest.Models.Preferred != "" {
		t.Errorf("Models.Preferred = %q, want \"\"", ai.Manifest.Models.Preferred)
	}
	if got := string(ai.Instructions); got != "project" {
		t.Errorf("Instructions = %q, want %q (project version should win)", got, "project")
	}
}

// TestResolveProjectContext_SkillLoaderHonorsProjectOverride mirrors the
// AgentLoader override test for skills: write a project-level SKILL.md and a
// (stale) global one, then assert the project version wins via the wired
// ProjectConfig.SkillLoader. Without this guard kernel/tool_exec.go's
// ActionSpecialize would silently load the global version and the project's
// `.rnix/skills/<name>/SKILL.md` override would never take effect.
func TestResolveProjectContext_SkillLoaderHonorsProjectOverride(t *testing.T) {
	srv := NewServer(nil, nil, "0.1.0-test", "", "")

	globalDir := t.TempDir()
	globalSkillsDir := filepath.Join(globalDir, "skills")
	if err := os.MkdirAll(filepath.Join(globalSkillsDir, "demo"), 0o755); err != nil {
		t.Fatalf("mkdir global skill: %v", err)
	}
	staleSkill := []byte("---\nname: demo\ndescription: stale global version\n---\n\nGLOBAL BODY\n")
	if err := os.WriteFile(filepath.Join(globalSkillsDir, "demo", "SKILL.md"), staleSkill, 0o644); err != nil {
		t.Fatalf("write global SKILL.md: %v", err)
	}
	srv.SetGlobalConfig(&config.GlobalConfig{
		Dir:       globalDir,
		AgentsDir: filepath.Join(globalDir, "agents"),
		SkillsDir: globalSkillsDir,
	})

	projectDir := t.TempDir()
	projSkillDir := filepath.Join(projectDir, ".rnix", "skills", "demo")
	if err := os.MkdirAll(projSkillDir, 0o755); err != nil {
		t.Fatalf("mkdir project skill: %v", err)
	}
	projectSkill := []byte("---\nname: demo\ndescription: project override\n---\n\nPROJECT BODY\n")
	if err := os.WriteFile(filepath.Join(projSkillDir, "SKILL.md"), projectSkill, 0o644); err != nil {
		t.Fatalf("write project SKILL.md: %v", err)
	}

	projCfg, _, err := srv.resolveProjectContext(projectDir, "")
	if err != nil {
		t.Fatalf("resolveProjectContext: %v", err)
	}
	if projCfg == nil || projCfg.SkillLoader == nil {
		t.Fatal("expected ProjectConfig with non-nil SkillLoader")
	}

	raw, err := projCfg.SkillLoader("demo")
	if err != nil {
		t.Fatalf("SkillLoader(demo): %v", err)
	}
	si, ok := raw.(*skills.SkillInfo)
	if !ok {
		t.Fatalf("SkillLoader returned %T, want *skills.SkillInfo", raw)
	}
	if si.Manifest.Description != "project override" {
		t.Errorf("Description = %q, want %q (project skill should win over stale global)", si.Manifest.Description, "project override")
	}
	if !strings.Contains(si.Body, "PROJECT BODY") {
		t.Errorf("Body = %q, expected to contain %q", si.Body, "PROJECT BODY")
	}
}

// --- 25.3-SRV-005: Invalid project providers.yaml returns error ---

func TestResolveProjectContext_InvalidProjectProviders(t *testing.T) {
	srv := NewServer(nil, nil, "0.1.0-test", "", "")

	globalDir := t.TempDir()
	srv.SetGlobalConfig(&config.GlobalConfig{
		Dir:       globalDir,
		AgentsDir: filepath.Join(globalDir, "agents"),
		SkillsDir: filepath.Join(globalDir, "skills"),
	})

	// Create project dir with invalid providers.yaml
	projectDir := t.TempDir()
	rnixDir := filepath.Join(projectDir, ".rnix")
	if err := os.MkdirAll(rnixDir, 0o755); err != nil {
		t.Fatalf("mkdir .rnix: %v", err)
	}
	invalidYAML := []byte("{{{{not valid yaml")
	if err := os.WriteFile(filepath.Join(rnixDir, "providers.yaml"), invalidYAML, 0o644); err != nil {
		t.Fatalf("write providers.yaml: %v", err)
	}

	_, _, err := srv.resolveProjectContext(projectDir, "")
	if err == nil {
		t.Fatal("expected error for invalid providers.yaml, got nil")
	}
}
