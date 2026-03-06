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

// --- BUG-006: checkIdle / tryAutoShutdown exclude Zombie/Dead processes ---

func TestTryAutoShutdown_ZombieOnlyProcs(t *testing.T) {
	// Given: a server with only Zombie-state processes and no active connections
	// When: tryAutoShutdown is called
	// Then: server shuts down (Zombie processes do NOT count as active)
	devReg := vfs.NewDeviceRegistry()
	_ = devReg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag) (vfs.VFSFile, error) {
		return nil, types.NewDriverError("Open", "/dev/llm/claude", nil, types.ErrNotFound)
	})
	vfsInst := vfs.NewVFS(devReg)
	ctxMgr := rnixctx.NewManager()

	srv := NewServer(nil, nil, "test")
	srv.IdleTimeout = 100 * time.Millisecond
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

	// Add a Zombie process directly to the kernel's process table
	p := kernel.NewProcess(0, "zombie-proc", nil)
	_ = p.Transition(types.StateRunning)                        // Created → Running
	_ = p.Terminate(kernel.ExitStatus{Code: 0, Reason: "done"}) // Running → Zombie
	kern.AddProcess(p)

	// No active connections (activeConns is 0 by default)
	// Call tryAutoShutdown — should recognize no active procs
	srv.tryAutoShutdown()

	// Verify the server shut down
	select {
	case <-srv.Done():
		// Success: server recognized no active processes
	case <-time.After(2 * time.Second):
		t.Fatal("server did not shut down — Zombie processes may be counted as active")
	}
}

func TestTryAutoShutdown_RunningProcsPreventShutdown(t *testing.T) {
	// Given: a server with a Running-state process and no active connections
	// When: tryAutoShutdown is called
	// Then: server does NOT shut down
	devReg := vfs.NewDeviceRegistry()
	vfsInst := vfs.NewVFS(devReg)
	ctxMgr := rnixctx.NewManager()

	srv := NewServer(nil, nil, "test")
	srv.IdleTimeout = 100 * time.Millisecond
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

	// Add a Running process
	p := kernel.NewProcess(0, "running-proc", nil)
	_ = p.Transition(types.StateRunning) // Created → Running
	kern.AddProcess(p)

	// tryAutoShutdown should NOT shut down
	srv.tryAutoShutdown()

	select {
	case <-srv.Done():
		t.Fatal("server should NOT have shut down with Running processes")
	case <-time.After(200 * time.Millisecond):
		// Success: server is still running
	}
}
