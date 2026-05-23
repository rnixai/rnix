package ipc

import (
	"path/filepath"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
	"github.com/rnixai/rnix/vfs"
)

func setupClientTest(t *testing.T) (*Client, *Server, *kernel.KernelImpl) {
	t.Helper()

	devReg := vfs.NewDeviceRegistry()
	vfsInst := vfs.NewVFS(devReg)
	ctxMgr := rnixctx.NewManager()

	srv := NewServer(nil, nil, "0.1.0-test", "", "")
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

	client, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { client.Close() })

	return client, srv, kern
}

func TestClient_Ping(t *testing.T) {
	client, _, _ := setupClientTest(t)

	version, err := client.Ping()
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if version != "0.1.0-test" {
		t.Errorf("version = %q, want %q", version, "0.1.0-test")
	}
}

func TestClient_ListProcs_Empty(t *testing.T) {
	client, _, _ := setupClientTest(t)

	procs, err := client.ListProcs()
	if err != nil {
		t.Fatalf("ListProcs: %v", err)
	}
	if len(procs) != 0 {
		t.Errorf("procs = %d, want 0", len(procs))
	}
}

func TestClient_Kill_NotFound(t *testing.T) {
	client, _, _ := setupClientTest(t)

	err := client.Kill(999, types.SIGTERM)
	if err == nil {
		t.Fatal("Kill should fail for nonexistent PID")
	}
}

func TestClient_Shutdown(t *testing.T) {
	devReg := vfs.NewDeviceRegistry()
	vfsInst := vfs.NewVFS(devReg)
	ctxMgr := rnixctx.NewManager()

	srv := NewServer(nil, nil, "test", "", "")
	kern := kernel.NewKernel(vfsInst, ctxMgr, nil)
	srv.kern = kern

	sockDir := t.TempDir()
	sockPath := filepath.Join(sockDir, "test.sock")

	if err := srv.ListenAndServe(sockPath); err != nil {
		t.Fatalf("ListenAndServe: %v", err)
	}
	defer func() {
		srv.Shutdown()
		srv.Wait()
		kern.Shutdown()
	}()

	client, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	if err := client.Shutdown(); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

func TestClient_DialTimeout_NoServer(t *testing.T) {
	sockDir := t.TempDir()
	sockPath := filepath.Join(sockDir, "nonexistent.sock")

	_, err := DialTimeout(sockPath, 100*time.Millisecond)
	if err == nil {
		t.Fatal("should fail connecting to nonexistent socket")
	}
}

func TestClient_AttachDebug_NotFound(t *testing.T) {
	client, _, _ := setupClientTest(t)

	err := client.AttachDebug(999, nil)
	if err == nil {
		t.Fatal("AttachDebug should fail for nonexistent PID")
	}
}
