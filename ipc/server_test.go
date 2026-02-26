package ipc

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gonewx/crux/agents"
	cruxctx "github.com/gonewx/crux/context"
	"github.com/gonewx/crux/internal/types"
	"github.com/gonewx/crux/kernel"
	"github.com/gonewx/crux/vfs"
)

func setupTestServer(t *testing.T) (*Server, string) {
	t.Helper()

	devReg := vfs.NewDeviceRegistry()
	vfsInst := vfs.NewVFS(devReg)
	ctxMgr := cruxctx.NewManager()

	srv := NewServer(nil, nil, "0.1.0-test")
	kern := kernel.NewKernel(vfsInst, ctxMgr, srv.CallbackMux())
	srv.kern = kern

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

	return srv, sockPath
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
	_, sockPath := setupTestServer(t)
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
	_, sockPath := setupTestServer(t)
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
	_, sockPath := setupTestServer(t)
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
	_, sockPath := setupTestServer(t)
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
	_, sockPath := setupTestServer(t)
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
	_, sockPath := setupTestServer(t)
	conn := dial(t, sockPath)

	resp := sendRequest(t, conn, MethodShutdown, nil)
	if !resp.OK {
		t.Fatalf("shutdown not ok: %+v", resp.Error)
	}
}

func TestServer_AttachDebug_NotFound(t *testing.T) {
	_, sockPath := setupTestServer(t)
	conn := dial(t, sockPath)

	resp := sendRequest(t, conn, MethodAttachDebug, AttachDebugRequest{PID: 999})
	if resp.OK {
		t.Fatal("attach_debug should fail for nonexistent PID")
	}
}

func TestServer_MultipleConnections(t *testing.T) {
	_, sockPath := setupTestServer(t)

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
	_, sockPath := setupTestServer(t)
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
	ctxMgr := cruxctx.NewManager()
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

// Stub agent loader for tests
func stubAgentLoader(name string) (*agents.AgentInfo, error) {
	return nil, os.ErrNotExist
}
