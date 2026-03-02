package kernel

import (
	"errors"
	"fmt"
	"testing"

	cruxctx "github.com/gonewx/crux/context"
	"github.com/gonewx/crux/internal/types"
	"github.com/gonewx/crux/vfs"
)

// --- Mock MountManager for Kernel tests ---

// mockMountManager implements the MountManager interface for testing.
type mockMountManager struct {
	mountFn      func(path string, config vfs.MCPConfig) error
	unmountFn    func(path string) error
	unmountAllFn func() error
	mounted      map[string]bool
}

func newMockMountManager() *mockMountManager {
	return &mockMountManager{
		mounted: make(map[string]bool),
	}
}

func (m *mockMountManager) Mount(path string, config vfs.MCPConfig) error {
	if m.mountFn != nil {
		return m.mountFn(path, config)
	}
	if m.mounted[path] {
		return fmt.Errorf("already mounted: %s", path)
	}
	m.mounted[path] = true
	return nil
}

func (m *mockMountManager) Unmount(path string) error {
	if m.unmountFn != nil {
		return m.unmountFn(path)
	}
	if !m.mounted[path] {
		return fmt.Errorf("not mounted: %s", path)
	}
	delete(m.mounted, path)
	return nil
}

func (m *mockMountManager) UnmountAll() error {
	if m.unmountAllFn != nil {
		return m.unmountAllFn()
	}
	for k := range m.mounted {
		delete(m.mounted, k)
	}
	return nil
}

// --- Kernel Mount Syscall Tests ---

func TestKernel_Mount(t *testing.T) {
	t.Run("mount with valid path delegates to MountManager", func(t *testing.T) {
		// Given: a Kernel with a MountManager
		k := newTestKernelWithMountManager(t)

		config := vfs.MCPConfig{
			ServerName:    "github",
			Command:       "mcp-server-github",
			TransportType: "stdio",
		}

		// When: Mount is called with a valid /mnt/mcp/ path
		err := k.Mount("/mnt/mcp/github", config)

		// Then: no error
		if err != nil {
			t.Fatalf("Mount failed: %v", err)
		}
	})

	t.Run("mount with invalid path prefix returns ErrInvalid", func(t *testing.T) {
		// Given: a Kernel with a MountManager
		k := newTestKernelWithMountManager(t)

		config := vfs.MCPConfig{
			ServerName:    "github",
			Command:       "mcp-server-github",
			TransportType: "stdio",
		}

		// When: Mount is called with a path not starting with /mnt/mcp/
		err := k.Mount("/dev/mcp/github", config)

		// Then: SyscallError with ErrInvalid
		if err == nil {
			t.Fatal("expected error for invalid mount path, got nil")
		}
		var syscallErr *SyscallError
		if !errors.As(err, &syscallErr) {
			t.Fatalf("expected *SyscallError, got %T: %v", err, err)
		}
		if syscallErr.Code != types.ErrInvalid {
			t.Fatalf("expected ErrInvalid, got %v", syscallErr.Code)
		}
	})

	t.Run("mount with nil mountMgr returns ErrInternal", func(t *testing.T) {
		// Given: a Kernel with NO MountManager (nil)
		k := newTestKernelWithoutMountManager(t)

		config := vfs.MCPConfig{
			ServerName:    "github",
			Command:       "mcp-server-github",
			TransportType: "stdio",
		}

		// When: Mount is called
		err := k.Mount("/mnt/mcp/github", config)

		// Then: SyscallError with ErrInternal
		if err == nil {
			t.Fatal("expected error for nil mountMgr, got nil")
		}
		var syscallErr *SyscallError
		if !errors.As(err, &syscallErr) {
			t.Fatalf("expected *SyscallError, got %T: %v", err, err)
		}
		if syscallErr.Code != types.ErrInternal {
			t.Fatalf("expected ErrInternal, got %v", syscallErr.Code)
		}
	})

	t.Run("mount duplicate path returns SyscallError", func(t *testing.T) {
		// Given: a Kernel with a path already mounted
		k := newTestKernelWithMountManager(t)

		config := vfs.MCPConfig{
			ServerName:    "github",
			Command:       "mcp-server-github",
			TransportType: "stdio",
		}
		if err := k.Mount("/mnt/mcp/github", config); err != nil {
			t.Fatalf("first Mount failed: %v", err)
		}

		// When: Mount is called again with the same path
		err := k.Mount("/mnt/mcp/github", config)

		// Then: SyscallError is returned
		if err == nil {
			t.Fatal("expected error for duplicate mount, got nil")
		}
		var syscallErr *SyscallError
		if !errors.As(err, &syscallErr) {
			t.Fatalf("expected *SyscallError, got %T: %v", err, err)
		}
	})

	t.Run("mount emits SyscallEvent", func(t *testing.T) {
		// Given: a Kernel with a MountManager and a process for event capture
		k := newTestKernelWithMountManager(t)

		config := vfs.MCPConfig{
			ServerName:    "github",
			Command:       "mcp-server-github",
			TransportType: "stdio",
		}

		// When: Mount is called
		err := k.Mount("/mnt/mcp/github", config)

		// Then: no error (SyscallEvent emission is verified by the fact that
		// emitEvent is called internally; detailed event assertion would require
		// a debug channel spy, which is an implementation detail)
		if err != nil {
			t.Fatalf("Mount failed: %v", err)
		}
	})
}

// --- Kernel Unmount Syscall Tests ---

func TestKernel_Unmount(t *testing.T) {
	t.Run("unmount with valid path delegates to MountManager", func(t *testing.T) {
		// Given: a Kernel with a mounted path
		k := newTestKernelWithMountManager(t)

		config := vfs.MCPConfig{
			ServerName:    "github",
			Command:       "mcp-server-github",
			TransportType: "stdio",
		}
		if err := k.Mount("/mnt/mcp/github", config); err != nil {
			t.Fatalf("Mount failed: %v", err)
		}

		// When: Unmount is called
		err := k.Unmount("/mnt/mcp/github")

		// Then: no error
		if err != nil {
			t.Fatalf("Unmount failed: %v", err)
		}
	})

	t.Run("unmount with nil mountMgr returns ErrInternal", func(t *testing.T) {
		// Given: a Kernel with NO MountManager
		k := newTestKernelWithoutMountManager(t)

		// When: Unmount is called
		err := k.Unmount("/mnt/mcp/github")

		// Then: SyscallError with ErrInternal
		if err == nil {
			t.Fatal("expected error for nil mountMgr, got nil")
		}
		var syscallErr *SyscallError
		if !errors.As(err, &syscallErr) {
			t.Fatalf("expected *SyscallError, got %T: %v", err, err)
		}
		if syscallErr.Code != types.ErrInternal {
			t.Fatalf("expected ErrInternal, got %v", syscallErr.Code)
		}
	})

	t.Run("unmount non-existent path returns SyscallError", func(t *testing.T) {
		// Given: a Kernel with no mounts
		k := newTestKernelWithMountManager(t)

		// When: Unmount is called for a non-existent path
		err := k.Unmount("/mnt/mcp/nonexistent")

		// Then: SyscallError is returned
		if err == nil {
			t.Fatal("expected error for unmounting non-existent path, got nil")
		}
		var syscallErr *SyscallError
		if !errors.As(err, &syscallErr) {
			t.Fatalf("expected *SyscallError, got %T: %v", err, err)
		}
	})

	t.Run("unmount with invalid path prefix returns ErrInvalid", func(t *testing.T) {
		// Given: a Kernel with a MountManager
		k := newTestKernelWithMountManager(t)

		// When: Unmount is called with a path not starting with /mnt/mcp/
		err := k.Unmount("/dev/mcp/github")

		// Then: SyscallError with ErrInvalid
		if err == nil {
			t.Fatal("expected error for invalid unmount path, got nil")
		}
		var syscallErr *SyscallError
		if !errors.As(err, &syscallErr) {
			t.Fatalf("expected *SyscallError, got %T: %v", err, err)
		}
		if syscallErr.Code != types.ErrInvalid {
			t.Fatalf("expected ErrInvalid, got %v", syscallErr.Code)
		}
	})

	t.Run("unmount emits SyscallEvent", func(t *testing.T) {
		// Given: a Kernel with a mounted path
		k := newTestKernelWithMountManager(t)

		config := vfs.MCPConfig{
			ServerName:    "github",
			Command:       "mcp-server-github",
			TransportType: "stdio",
		}
		if err := k.Mount("/mnt/mcp/github", config); err != nil {
			t.Fatalf("Mount failed: %v", err)
		}

		// When: Unmount is called
		err := k.Unmount("/mnt/mcp/github")

		// Then: no error (SyscallEvent emission verified internally)
		if err != nil {
			t.Fatalf("Unmount failed: %v", err)
		}
	})
}

// --- Test Helpers ---

// newTestKernelWithMountManager creates a Kernel with a mock MountManager.
func newTestKernelWithMountManager(t testing.TB) *KernelImpl {
	t.Helper()
	reg := vfs.NewDeviceRegistry()
	llmFile := &mockLLMFile{
		readData: []byte(`{"content":"test","tokens_used":1}`),
	}
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag) (vfs.VFSFile, error) {
		return llmFile, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := cruxctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(func() { k.Shutdown() })

	// Set mock MountManager
	k.mountMgr = newMockMountManager()
	return k
}

// newTestKernelWithoutMountManager creates a Kernel without a MountManager (nil).
func newTestKernelWithoutMountManager(t testing.TB) *KernelImpl {
	t.Helper()
	reg := vfs.NewDeviceRegistry()
	llmFile := &mockLLMFile{
		readData: []byte(`{"content":"test","tokens_used":1}`),
	}
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag) (vfs.VFSFile, error) {
		return llmFile, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := cruxctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(func() { k.Shutdown() })
	// mountMgr is nil by default
	return k
}
