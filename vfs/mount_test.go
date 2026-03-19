package vfs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/rnixai/rnix/internal/types"
)

// --- Mock Transport for testing ---

// mockMCPTransport implements MCPTransport for testing.
type mockMCPTransport struct {
	connectFn func(ctx context.Context) error
	callFn    func(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error)
	closeFn   func() error
	pingFn    func(ctx context.Context) error
	closed    bool
}

func (m *mockMCPTransport) Connect(ctx context.Context) error {
	if m.connectFn != nil {
		return m.connectFn(ctx)
	}
	return nil
}

func (m *mockMCPTransport) Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	if m.callFn != nil {
		return m.callFn(ctx, method, params)
	}
	return json.RawMessage(`{"result":"ok"}`), nil
}

func (m *mockMCPTransport) Close() error {
	m.closed = true
	if m.closeFn != nil {
		return m.closeFn()
	}
	return nil
}

func (m *mockMCPTransport) Ping(ctx context.Context) error {
	if m.pingFn != nil {
		return m.pingFn(ctx)
	}
	return nil
}

// mockTransportFactory creates a TransportFactory that returns mock transports.
func mockTransportFactory(transport *mockMCPTransport) TransportFactory {
	return func(config MCPConfig) (MCPTransport, error) {
		return transport, nil
	}
}

// failingTransportFactory returns a TransportFactory that always fails.
func failingTransportFactory(err error) TransportFactory {
	return func(config MCPConfig) (MCPTransport, error) {
		return nil, err
	}
}

// --- MountManager Tests ---

func TestMountManager_Mount(t *testing.T) {
	t.Run("mount registers path in DeviceRegistry", func(t *testing.T) {
		// Given: a MountManager with a DeviceRegistry and mock transport
		devReg := NewDeviceRegistry()
		transport := &mockMCPTransport{}
		mgr := NewMountManager(devReg, mockTransportFactory(transport))

		config := MCPConfig{
			ServerName:    "github",
			Command:       "mcp-server-github",
			TransportType: "stdio",
		}

		// When: Mount is called
		err := mgr.Mount("/mnt/mcp/github", config)

		// Then: no error and path is registered in DeviceRegistry
		if err != nil {
			t.Fatalf("Mount failed: %v", err)
		}

		// Verify path is accessible via DeviceRegistry
		_, err = devReg.Open("/mnt/mcp/github/tools/test", O_RDWR, "")
		if err != nil {
			t.Fatalf("expected path to be registered in DeviceRegistry, got error: %v", err)
		}
	})

	t.Run("mount calls TransportFactory and connects", func(t *testing.T) {
		// Given: a MountManager with a transport that tracks Connect calls
		devReg := NewDeviceRegistry()
		connectCalled := false
		transport := &mockMCPTransport{
			connectFn: func(ctx context.Context) error {
				connectCalled = true
				return nil
			},
		}
		mgr := NewMountManager(devReg, mockTransportFactory(transport))

		config := MCPConfig{
			ServerName:    "github",
			Command:       "mcp-server-github",
			TransportType: "stdio",
		}

		// When: Mount is called
		err := mgr.Mount("/mnt/mcp/github", config)

		// Then: Connect was called on the transport
		if err != nil {
			t.Fatalf("Mount failed: %v", err)
		}
		if !connectCalled {
			t.Fatal("expected Transport.Connect to be called")
		}
	})

	t.Run("mount with failing transport returns error", func(t *testing.T) {
		// Given: a MountManager with a failing transport factory
		devReg := NewDeviceRegistry()
		mgr := NewMountManager(devReg, failingTransportFactory(fmt.Errorf("connection refused")))

		config := MCPConfig{
			ServerName:    "github",
			Command:       "mcp-server-github",
			TransportType: "stdio",
		}

		// When: Mount is called
		err := mgr.Mount("/mnt/mcp/github", config)

		// Then: error is returned
		if err == nil {
			t.Fatal("expected error from failing transport, got nil")
		}
	})
}

func TestMountManager_MountDuplicate(t *testing.T) {
	t.Run("duplicate mount returns ErrAlreadyMounted", func(t *testing.T) {
		// Given: a MountManager with an already-mounted path
		devReg := NewDeviceRegistry()
		transport := &mockMCPTransport{}
		mgr := NewMountManager(devReg, mockTransportFactory(transport))

		config := MCPConfig{
			ServerName:    "github",
			Command:       "mcp-server-github",
			TransportType: "stdio",
		}
		if err := mgr.Mount("/mnt/mcp/github", config); err != nil {
			t.Fatalf("first Mount failed: %v", err)
		}

		// When: Mount is called again with the same path
		err := mgr.Mount("/mnt/mcp/github", config)

		// Then: error with ErrAlreadyMounted code is returned
		if err == nil {
			t.Fatal("expected ErrAlreadyMounted error for duplicate mount, got nil")
		}
		var drvErr *types.DriverError
		if !errors.As(err, &drvErr) {
			t.Fatalf("expected *types.DriverError, got %T: %v", err, err)
		}
		if drvErr.Code != types.ErrAlreadyMounted {
			t.Fatalf("expected ErrAlreadyMounted, got %v", drvErr.Code)
		}
	})
}

func TestMountManager_Unmount(t *testing.T) {
	t.Run("unmount removes path from DeviceRegistry", func(t *testing.T) {
		// Given: a MountManager with a mounted path
		devReg := NewDeviceRegistry()
		transport := &mockMCPTransport{}
		mgr := NewMountManager(devReg, mockTransportFactory(transport))

		config := MCPConfig{
			ServerName:    "github",
			Command:       "mcp-server-github",
			TransportType: "stdio",
		}
		if err := mgr.Mount("/mnt/mcp/github", config); err != nil {
			t.Fatalf("Mount failed: %v", err)
		}

		// When: Unmount is called
		err := mgr.Unmount("/mnt/mcp/github")

		// Then: no error and path is no longer in DeviceRegistry
		if err != nil {
			t.Fatalf("Unmount failed: %v", err)
		}

		_, err = devReg.Open("/mnt/mcp/github/tools/test", O_RDWR, "")
		if err == nil {
			t.Fatal("expected error after unmount, path should no longer be registered")
		}
	})

	t.Run("unmount closes Transport connection", func(t *testing.T) {
		// Given: a MountManager with a mounted path and trackable transport
		devReg := NewDeviceRegistry()
		transport := &mockMCPTransport{}
		mgr := NewMountManager(devReg, mockTransportFactory(transport))

		config := MCPConfig{
			ServerName:    "github",
			Command:       "mcp-server-github",
			TransportType: "stdio",
		}
		if err := mgr.Mount("/mnt/mcp/github", config); err != nil {
			t.Fatalf("Mount failed: %v", err)
		}

		// When: Unmount is called
		err := mgr.Unmount("/mnt/mcp/github")

		// Then: Transport.Close was called
		if err != nil {
			t.Fatalf("Unmount failed: %v", err)
		}
		if !transport.closed {
			t.Fatal("expected Transport.Close to be called on unmount")
		}
	})

	t.Run("unmount non-existent path returns error", func(t *testing.T) {
		// Given: a MountManager with no mounts
		devReg := NewDeviceRegistry()
		transport := &mockMCPTransport{}
		mgr := NewMountManager(devReg, mockTransportFactory(transport))

		// When: Unmount is called for a non-existent path
		err := mgr.Unmount("/mnt/mcp/nonexistent")

		// Then: error is returned
		if err == nil {
			t.Fatal("expected error for unmounting non-existent path, got nil")
		}
	})
}

func TestMountManager_UnmountAll(t *testing.T) {
	t.Run("unmount all cleans up all mounts", func(t *testing.T) {
		// Given: a MountManager with multiple mounted paths
		devReg := NewDeviceRegistry()
		transports := make([]*mockMCPTransport, 3)
		transportIdx := 0
		factory := func(config MCPConfig) (MCPTransport, error) {
			t := &mockMCPTransport{}
			transports[transportIdx] = t
			transportIdx++
			return t, nil
		}
		mgr := NewMountManager(devReg, factory)

		paths := []string{"/mnt/mcp/github", "/mnt/mcp/slack", "/mnt/mcp/jira"}
		for _, p := range paths {
			config := MCPConfig{ServerName: p, Command: "test", TransportType: "stdio"}
			if err := mgr.Mount(p, config); err != nil {
				t.Fatalf("Mount %s failed: %v", p, err)
			}
		}

		// When: UnmountAll is called
		err := mgr.UnmountAll()

		// Then: no error, all transports closed, all paths removed
		if err != nil {
			t.Fatalf("UnmountAll failed: %v", err)
		}
		for i, tr := range transports {
			if tr != nil && !tr.closed {
				t.Fatalf("transport %d was not closed", i)
			}
		}
		for _, p := range paths {
			_, err := devReg.Open(p+"/tools/test", O_RDWR, "")
			if err == nil {
				t.Fatalf("expected path %s to be unregistered after UnmountAll", p)
			}
		}
	})
}

func TestMountManager_GetStatus(t *testing.T) {
	t.Run("get status of mounted path", func(t *testing.T) {
		// Given: a MountManager with a mounted path
		devReg := NewDeviceRegistry()
		transport := &mockMCPTransport{}
		mgr := NewMountManager(devReg, mockTransportFactory(transport))

		config := MCPConfig{
			ServerName:    "github",
			Command:       "mcp-server-github",
			TransportType: "stdio",
		}
		if err := mgr.Mount("/mnt/mcp/github", config); err != nil {
			t.Fatalf("Mount failed: %v", err)
		}

		// When: GetStatus is called
		status, err := mgr.GetStatus("/mnt/mcp/github")

		// Then: status is Connected
		if err != nil {
			t.Fatalf("GetStatus failed: %v", err)
		}
		if status != MCPStatusConnected {
			t.Fatalf("expected MCPStatusConnected, got %v", status)
		}
	})

	t.Run("get status of non-existent path returns error", func(t *testing.T) {
		// Given: a MountManager with no mounts
		devReg := NewDeviceRegistry()
		transport := &mockMCPTransport{}
		mgr := NewMountManager(devReg, mockTransportFactory(transport))

		// When: GetStatus is called for non-existent path
		_, err := mgr.GetStatus("/mnt/mcp/nonexistent")

		// Then: error is returned
		if err == nil {
			t.Fatal("expected error for non-existent mount status, got nil")
		}
	})
}

func TestMountManager_ListMounts(t *testing.T) {
	t.Run("list mounts returns all mounted paths", func(t *testing.T) {
		// Given: a MountManager with multiple mounts
		devReg := NewDeviceRegistry()
		factory := func(config MCPConfig) (MCPTransport, error) {
			return &mockMCPTransport{}, nil
		}
		mgr := NewMountManager(devReg, factory)

		paths := []string{"/mnt/mcp/github", "/mnt/mcp/slack"}
		for _, p := range paths {
			config := MCPConfig{ServerName: p, Command: "test", TransportType: "stdio"}
			if err := mgr.Mount(p, config); err != nil {
				t.Fatalf("Mount %s failed: %v", p, err)
			}
		}

		// When: ListMounts is called
		mounts := mgr.ListMounts()

		// Then: all mounts are returned
		if len(mounts) != 2 {
			t.Fatalf("expected 2 mounts, got %d", len(mounts))
		}
	})
}

func TestMountManager_ConcurrentAccess(t *testing.T) {
	t.Run("concurrent mount and unmount is safe", func(t *testing.T) {
		// Given: a MountManager
		devReg := NewDeviceRegistry()
		factory := func(config MCPConfig) (MCPTransport, error) {
			return &mockMCPTransport{}, nil
		}
		mgr := NewMountManager(devReg, factory)

		// When: concurrent Mount and Unmount operations
		var wg sync.WaitGroup
		for i := range 20 {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				path := fmt.Sprintf("/mnt/mcp/svc%d", i)
				config := MCPConfig{ServerName: path, Command: "test", TransportType: "stdio"}
				_ = mgr.Mount(path, config)
				_ = mgr.Unmount(path)
			}(i)
		}
		wg.Wait()

		// Then: no panic, no race condition (verified by -race flag)
	})
}

// --- Integration Test: Mount + Open + Read/Write + Unmount ---

func TestMountManager_IntegrationFlow(t *testing.T) {
	t.Run("mount then open read write close unmount", func(t *testing.T) {
		// Given: a full VFS with MountManager
		devReg := NewDeviceRegistry()
		transport := &mockMCPTransport{
			callFn: func(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
				return json.RawMessage(`{"content":[{"type":"text","text":"tool result"}]}`), nil
			},
		}
		mgr := NewMountManager(devReg, mockTransportFactory(transport))

		config := MCPConfig{
			ServerName:    "github",
			Command:       "mcp-server-github",
			TransportType: "stdio",
		}

		// When: Mount, Open, Write, Read, Close, Unmount sequence
		if err := mgr.Mount("/mnt/mcp/github", config); err != nil {
			t.Fatalf("Mount failed: %v", err)
		}

		// Open via DeviceRegistry
		file, err := devReg.Open("/mnt/mcp/github/tools/create-issue", O_RDWR, "")
		if err != nil {
			t.Fatalf("Open failed: %v", err)
		}

		// Write tool call data
		writeData := []byte(`{"title":"test issue","body":"test body"}`)
		if err := file.Write(context.Background(), writeData); err != nil {
			t.Fatalf("Write failed: %v", err)
		}

		// Read tool result
		result, err := file.Read(1 << 20)
		if err != nil {
			t.Fatalf("Read failed: %v", err)
		}
		if len(result) == 0 {
			t.Fatal("expected non-empty read result")
		}

		// Close file
		if err := file.Close(); err != nil {
			t.Fatalf("Close failed: %v", err)
		}

		// Unmount
		if err := mgr.Unmount("/mnt/mcp/github"); err != nil {
			t.Fatalf("Unmount failed: %v", err)
		}
	})
}

// --- DeviceRegistry Unregister Tests ---

func TestDeviceRegistry_Unregister(t *testing.T) {
	t.Run("unregister removes device and prevents routing", func(t *testing.T) {
		// Given: a DeviceRegistry with a registered path
		reg := NewDeviceRegistry()
		factory := func(subpath string, flags OpenFlag, workDir string) (VFSFile, error) {
			return &mockFile{}, nil
		}
		if err := reg.Register("/mnt/mcp/github", factory); err != nil {
			t.Fatalf("Register failed: %v", err)
		}

		// When: Unregister is called
		err := reg.Unregister("/mnt/mcp/github")

		// Then: path is no longer accessible
		if err != nil {
			t.Fatalf("Unregister failed: %v", err)
		}
		_, err = reg.Open("/mnt/mcp/github", O_RDONLY, "")
		if err == nil {
			t.Fatal("expected error after Unregister, path should no longer route")
		}
	})

	t.Run("unregister non-existent path returns error", func(t *testing.T) {
		// Given: an empty DeviceRegistry
		reg := NewDeviceRegistry()

		// When: Unregister is called for a non-existent path
		err := reg.Unregister("/mnt/mcp/nonexistent")

		// Then: error is returned
		if err == nil {
			t.Fatal("expected error for unregistering non-existent path, got nil")
		}
	})

	t.Run("unregister then re-register succeeds", func(t *testing.T) {
		// Given: a DeviceRegistry with a registered and then unregistered path
		reg := NewDeviceRegistry()
		factory := func(subpath string, flags OpenFlag, workDir string) (VFSFile, error) {
			return &mockFile{}, nil
		}
		_ = reg.Register("/mnt/mcp/github", factory)
		_ = reg.Unregister("/mnt/mcp/github")

		// When: Register is called again for the same path
		err := reg.Register("/mnt/mcp/github", factory)

		// Then: registration succeeds
		if err != nil {
			t.Fatalf("re-Register after Unregister failed: %v", err)
		}
	})
}

// Ensure types.ErrServiceUnavailable and types.ErrAlreadyMounted are accessible.
// These are compile-time checks that will fail until the error codes are defined.
var _ types.ErrCode = types.ErrServiceUnavailable
var _ types.ErrCode = types.ErrAlreadyMounted
