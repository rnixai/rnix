package vfs

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/xsync"
)

// mountTimeout is the maximum time allowed for a single mount operation (NFR25: ≤ 500ms).
const mountTimeout = 500 * time.Millisecond

// MountManager manages MCP server mounts under /mnt/mcp/.
type MountManager struct {
	mu               sync.Mutex // serializes Mount/Unmount to prevent TOCTOU races
	mounts           *xsync.SyncMap[string, *MCPMount]
	devReg           *DeviceRegistry
	transportFactory TransportFactory
}

// NewMountManager creates a new MountManager with the given DeviceRegistry
// and TransportFactory.
func NewMountManager(devReg *DeviceRegistry, factory TransportFactory) *MountManager {
	return &MountManager{
		mounts:           xsync.NewSyncMap[string, *MCPMount](),
		devReg:           devReg,
		transportFactory: factory,
	}
}

// Mount mounts an MCP server at the given path.
// The path must be unique; duplicate mounts return an error with ErrAlreadyMounted.
func (m *MountManager) Mount(path string, config MCPConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if already mounted (under lock to prevent TOCTOU race)
	if _, exists := m.mounts.Load(path); exists {
		return types.NewDriverError("Mount", path, fmt.Errorf("already mounted: %s", path), types.ErrAlreadyMounted)
	}

	// Create transport via factory
	transport, err := m.transportFactory(config)
	if err != nil {
		return fmt.Errorf("transport create failed for %s: %w", path, err)
	}

	// Connect transport with timeout (NFR25: ≤ 500ms)
	ctx, cancel := context.WithTimeout(context.Background(), mountTimeout)
	defer cancel()
	if err := transport.Connect(ctx); err != nil {
		_ = transport.Close()
		return fmt.Errorf("transport connect failed for %s: %w", path, err)
	}

	// Create mount record
	mount := &MCPMount{
		Path:      path,
		Config:    config,
		Status:    MCPStatusConnected,
		transport: transport,
	}

	// Register in DeviceRegistry so VFS Open/Read/Write/Close can route to it
	factory := mcpFileFactory(transport)
	if err := m.devReg.Register(path, factory); err != nil {
		_ = transport.Close()
		return fmt.Errorf("device register failed for %s: %w", path, err)
	}

	// Store mount record
	m.mounts.Store(path, mount)
	return nil
}

// Unmount unmounts the MCP server at the given path.
// Closes the transport connection and removes from DeviceRegistry.
func (m *MountManager) Unmount(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	mount, ok := m.mounts.LoadAndDelete(path)
	if !ok {
		return fmt.Errorf("not mounted: %s", path)
	}

	// Close transport
	if mount.transport != nil {
		_ = mount.transport.Close()
	}

	// Unregister from DeviceRegistry
	_ = m.devReg.Unregister(path)

	return nil
}

// GetStatus returns the status of the mount at the given path.
func (m *MountManager) GetStatus(path string) (MCPStatus, error) {
	mount, ok := m.mounts.Load(path)
	if !ok {
		return 0, fmt.Errorf("not mounted: %s", path)
	}
	return mount.Status, nil
}

// ListMounts returns all current mounts.
func (m *MountManager) ListMounts() []MCPMount {
	result := make([]MCPMount, 0)
	m.mounts.Range(func(_ string, mount *MCPMount) bool {
		result = append(result, *mount)
		return true
	})
	return result
}

// UnmountAll unmounts all MCP servers. Called during daemon shutdown.
func (m *MountManager) UnmountAll() error {
	var paths []string
	m.mounts.Range(func(path string, _ *MCPMount) bool {
		paths = append(paths, path)
		return true
	})

	var firstErr error
	for _, path := range paths {
		if err := m.Unmount(path); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
