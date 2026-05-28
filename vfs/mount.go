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

	// Create mount record. refCount starts at 1 (this Mount call is the
	// first owner); subsequent Acquire calls bump it and Unmount drops it,
	// with the real Close+Unregister happening only when it returns to 0.
	mount := &MCPMount{
		Path:      path,
		Config:    config,
		Status:    MCPStatusConnected,
		transport: transport,
		refCount:  1,
	}

	// Register in DeviceRegistry so VFS Open/Read/Write/Close can route to it.
	// Story 48.1 — use RegisterWithDriver instead of Register so DeviceRegistry.
	// GetDriver(path) returns (*MCPMount, true) rather than (nil, false). This
	// lets observability surfaces (Dashboard "is this MCP path live?" probes,
	// ATDD tests that grep DeviceRegistry directly) confirm a mount succeeded
	// without having to call Open. buildToolDefs (kernel/toolgen.go) is
	// unaffected — *MCPMount does not implement ToolDescriptor, so the
	// "silently skip unknown paths (e.g., MCP devices)" branch still applies
	// and the toolMap does not gain spurious MCP entries.
	factory := mcpFileFactory(transport)
	if err := m.devReg.RegisterWithDriver(path, factory, mount); err != nil {
		_ = transport.Close()
		return fmt.Errorf("device register failed for %s: %w", path, err)
	}

	// Store mount record
	m.mounts.Store(path, mount)
	return nil
}

// Unmount releases one reference to the MCP server at the given path. The
// transport is closed and the registry entry removed only when the reference
// count drops to zero — this lets fork-resume reusers (Story 48.1 AC7) and
// Suspended-resume reusers each hold an independent owner without one's exit
// stranding the other (code-review P2 / P4).
func (m *MountManager) Unmount(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	mount, ok := m.mounts.Load(path)
	if !ok {
		return fmt.Errorf("not mounted: %s", path)
	}

	mount.refCount--
	if mount.refCount > 0 {
		// Other owners still hold the mount; only this owner has released.
		return nil
	}

	m.mounts.Delete(path)

	// Close transport. Story 48.2 AC4/AC7 — propagate Close error (notably
	// *types.DriverError{Code: ErrForceKilled} on SIGKILL escalation) so that
	// kernel/reason.go::finishProcess can annotate the Unmount event with
	// forced=true via errors.As + DriverError.Code check. Earlier `_ = Close()`
	// silently dropped the sentinel, leaving the duration ≥ 4.9s heuristic as
	// the only AC7 signal (code review F1, 2026-05-28).
	var closeErr error
	if mount.transport != nil {
		closeErr = mount.transport.Close()
	}

	// Unregister from DeviceRegistry — best effort, regardless of Close
	// outcome. A force-killed transport still needs its registry entry removed
	// so VFS Open/Read/Write/Close don't keep routing to a dead device.
	_ = m.devReg.Unregister(path)

	return closeErr
}

// Acquire records an additional owner for an existing mount. Used by the
// resume path (Story 48.1) when reattachMCPMounts finds the canonical
// `/mnt/mcp/<original-pid>-<server>` already present (either a fork-resume
// sibling beat us to it, or the original Suspended owner has not been
// cleaned up yet). Returns an error when the path is not currently mounted —
// callers should fall back to a fresh Mount in that case.
//
// Unmount must be called exactly once per Mount + Acquire (i.e. once per
// owner) for the transport to actually close.
func (m *MountManager) Acquire(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	mount, ok := m.mounts.Load(path)
	if !ok {
		return fmt.Errorf("not mounted: %s", path)
	}
	mount.refCount++
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
