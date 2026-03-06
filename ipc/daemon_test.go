package ipc

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/kernel"
	"github.com/rnixai/rnix/vfs"
)

func TestTryConnect_NoDaemon(t *testing.T) {
	sockDir := t.TempDir()
	sockPath := filepath.Join(sockDir, "nonexistent.sock")

	_, err := tryConnect(sockPath)
	if err == nil {
		t.Fatal("should fail when no daemon is running")
	}
}

func TestTryConnect_Success(t *testing.T) {
	devReg := vfs.NewDeviceRegistry()
	vfsInst := vfs.NewVFS(devReg)
	ctxMgr := rnixctx.NewManager()
	kern := kernel.NewKernel(vfsInst, ctxMgr, nil)
	defer kern.Shutdown()

	srv := NewServer(kern, nil, "0.1.0-test")
	sockDir := t.TempDir()
	sockPath := filepath.Join(sockDir, "test.sock")

	if err := srv.ListenAndServe(sockPath); err != nil {
		t.Fatalf("ListenAndServe: %v", err)
	}
	defer func() {
		srv.Shutdown()
		srv.Wait()
	}()

	client, err := tryConnect(sockPath)
	if err != nil {
		t.Fatalf("tryConnect: %v", err)
	}
	defer client.Close()
}

func TestCleanStaleSocket(t *testing.T) {
	sockDir := t.TempDir()
	sockPath := filepath.Join(sockDir, "stale.sock")

	if err := os.WriteFile(sockPath, []byte("stale"), 0600); err != nil {
		t.Fatalf("create stale socket: %v", err)
	}

	cleanStaleSocket(sockPath)

	if _, err := os.Stat(sockPath); !os.IsNotExist(err) {
		t.Error("stale socket should have been removed")
	}
}

func TestCleanStaleSocket_NoFile(t *testing.T) {
	sockDir := t.TempDir()
	sockPath := filepath.Join(sockDir, "nonexistent.sock")
	cleanStaleSocket(sockPath)
}

func TestWritePIDFile(t *testing.T) {
	dir := t.TempDir()
	writePIDFile(dir, 12345)

	data, err := os.ReadFile(filepath.Join(dir, "rnix.pid"))
	if err != nil {
		t.Fatalf("read pid file: %v", err)
	}
	if string(data) != "12345\n" {
		t.Errorf("pid file = %q, want %q", string(data), "12345\n")
	}
}

func TestIsDaemonRunning_NoDaemon(t *testing.T) {
	orig := os.Getenv("XDG_RUNTIME_DIR")
	t.Cleanup(func() { os.Setenv("XDG_RUNTIME_DIR", orig) })

	tmpDir := t.TempDir()
	os.Setenv("XDG_RUNTIME_DIR", tmpDir)

	if IsDaemonRunning() {
		t.Error("should report no daemon running")
	}
}

func TestIsDaemonRunning_WithDaemon(t *testing.T) {
	devReg := vfs.NewDeviceRegistry()
	vfsInst := vfs.NewVFS(devReg)
	ctxMgr := rnixctx.NewManager()
	kern := kernel.NewKernel(vfsInst, ctxMgr, nil)
	defer kern.Shutdown()

	srv := NewServer(kern, nil, "test")
	sockDir := t.TempDir()
	sockPath := filepath.Join(sockDir, "rnix", "rnix.sock")

	orig := os.Getenv("XDG_RUNTIME_DIR")
	os.Setenv("XDG_RUNTIME_DIR", sockDir)
	t.Cleanup(func() { os.Setenv("XDG_RUNTIME_DIR", orig) })

	if err := srv.ListenAndServe(sockPath); err != nil {
		t.Fatalf("ListenAndServe: %v", err)
	}
	defer func() {
		srv.Shutdown()
		srv.Wait()
	}()

	if !IsDaemonRunning() {
		t.Error("should report daemon running")
	}
}

func TestWaitForDaemon_Timeout(t *testing.T) {
	sockDir := t.TempDir()
	sockPath := filepath.Join(sockDir, "nonexistent.sock")

	start := time.Now()
	_, err := waitForDaemon(sockPath)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("should timeout")
	}
	if elapsed < 2*time.Second {
		t.Errorf("should wait at least 2 seconds, waited %v", elapsed)
	}
}

func TestWaitForDaemon_ServerStartsLate(t *testing.T) {
	devReg := vfs.NewDeviceRegistry()
	vfsInst := vfs.NewVFS(devReg)
	ctxMgr := rnixctx.NewManager()
	kern := kernel.NewKernel(vfsInst, ctxMgr, nil)
	defer kern.Shutdown()

	srv := NewServer(kern, nil, "test")
	sockDir := t.TempDir()
	sockPath := filepath.Join(sockDir, "test.sock")

	go func() {
		time.Sleep(500 * time.Millisecond)
		_ = srv.ListenAndServe(sockPath)
	}()
	defer func() {
		srv.Shutdown()
		srv.Wait()
	}()

	client, err := waitForDaemon(sockPath)
	if err != nil {
		t.Fatalf("waitForDaemon: %v", err)
	}
	client.Close()
}

func TestDaemonExe_Default(t *testing.T) {
	orig := DaemonExe
	DaemonExe = ""
	t.Cleanup(func() { DaemonExe = orig })

	exe := daemonExe()
	if exe != os.Args[0] {
		t.Errorf("exe = %q, want %q", exe, os.Args[0])
	}
}

func TestDaemonExe_Override(t *testing.T) {
	orig := DaemonExe
	DaemonExe = "/usr/bin/rnix"
	t.Cleanup(func() { DaemonExe = orig })

	exe := daemonExe()
	if exe != "/usr/bin/rnix" {
		t.Errorf("exe = %q, want %q", exe, "/usr/bin/rnix")
	}
}
