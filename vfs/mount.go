package vfs

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/xsync"
)

// defaultMountTimeout is the fallback Connect budget for a mount whose
// MCPConfig.MountTimeout is unset (Story 48.6 Task 2.3). It supersedes the old
// hardcoded 500ms cap (Story 9.x NFR25), which Playwright Chromium's ~15s cold
// start invalidated — that const had no test asserting 500ms, so the change is
// safe. Per-server overrides arrive via MCPConfig.MountTimeout.
const defaultMountTimeout = 5 * time.Second

// MountManager manages MCP server mounts under /mnt/mcp/.
//
// Story 48.6 removed the global serializing mutex. Concurrency is now carried
// entirely by the per-entry MCPMount.mu (for refCount / finalize / teardown of a
// single path) plus the atomic xsync.SyncMap (for registry membership). Distinct
// paths Mount/Unmount/Acquire fully in parallel; the slow transport.Connect runs
// outside any cross-path lock, so N concurrent mounts cost ~1×Connect, not
// N×Connect (AC1).
type MountManager struct {
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
//
// The path must be unique; duplicate mounts return an error with
// ErrAlreadyMounted. Story 48.6 concurrency model (§并发化设计):
//
//  1. Reserve: build a Connecting placeholder, lock its per-entry mu BEFORE
//     publishing, then LoadOrStore it. Locking before publishing means any
//     Acquire that loads the placeholder blocks on mu until we finalize — it can
//     never observe a half-mounted entry (§易错点 1). A losing LoadOrStore
//     (already mounted) returns ErrAlreadyMounted.
//  2. Connect: run transport.Connect OUTSIDE any cross-path lock, bounded by the
//     per-server MountTimeout (default 5s). This is the source of parallelism —
//     distinct paths never serialize on a global mutex.
//  3. Finalize / rollback: on success, fill in transport/Status/refCount and
//     register the device, then release mu. On Connect (or register) failure,
//     delete the placeholder (no residual half-mount — AC2) and Close the
//     transport to reap the child (reusing 48.2's process-group cleanup).
func (m *MountManager) Mount(path string, config MCPConfig) error {
	placeholder := &MCPMount{
		Path:   path,
		Config: config,
		Status: MCPStatusConnecting,
		mu:     &sync.Mutex{},
	}
	// Hold the per-entry lock from before the entry is visible until finalize, so
	// a racing Acquire blocks until the mount is Connected (or gone).
	placeholder.mu.Lock()

	if _, loaded := m.mounts.LoadOrStore(path, placeholder); loaded {
		placeholder.mu.Unlock()
		return types.NewDriverError("Mount", path, fmt.Errorf("already mounted: %s", path), types.ErrAlreadyMounted)
	}

	// We own the slot. From here, any early return MUST delete the placeholder
	// and release mu so blocked Acquires fall back to a fresh Mount.
	transport, err := m.transportFactory(config)
	if err != nil {
		m.mounts.Delete(path)
		placeholder.mu.Unlock()
		return fmt.Errorf("transport create failed for %s: %w", path, err)
	}

	// Connect with the per-server mount timeout (default 5s). Runs without any
	// cross-path lock held → distinct paths Connect concurrently.
	timeout := config.MountTimeout
	if timeout <= 0 {
		timeout = defaultMountTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := transport.Connect(ctx); err != nil {
		m.mounts.Delete(path)
		placeholder.mu.Unlock()
		_ = transport.Close() // reap any half-started child (Story 48.2 cleanup)
		return fmt.Errorf("transport connect failed for %s: %w", path, err)
	}

	// Finalize under the held per-entry lock. Set the live fields BEFORE
	// registering the device so an observer that reaches the entry via
	// DeviceRegistry.GetDriver never sees Transport()==nil.
	placeholder.transport = transport
	placeholder.Status = MCPStatusConnected
	placeholder.refCount = 1

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
	if err := m.devReg.RegisterWithDriver(path, factory, placeholder); err != nil {
		m.mounts.Delete(path)
		placeholder.mu.Unlock()
		_ = transport.Close()
		return fmt.Errorf("device register failed for %s: %w", path, err)
	}

	placeholder.mu.Unlock()
	return nil
}

// Unmount releases one reference to the MCP server at the given path. The
// transport is closed and the registry entry removed only when the reference
// count drops to zero — this lets fork-resume reusers (Story 48.1 AC7) and
// Suspended-resume reusers each hold an independent owner without one's exit
// stranding the other (code-review P2 / P4).
//
// Story 48.6 §易错点 2: the per-entry lock guards only the refCount decision +
// registry removal; transport.Close (which may block up to 5s on the 48.2
// graceful SIGTERM→SIGKILL window) runs AFTER the lock is released, so one
// path's Close never blocks another path's Mount/Unmount/Acquire.
func (m *MountManager) Unmount(path string) error {
	mount, ok := m.mounts.Load(path)
	if !ok {
		return fmt.Errorf("not mounted: %s", path)
	}

	mount.lock()

	// Story 48.6 [Review][Patch] P2: re-validate identity after potentially
	// blocking on an in-flight Mount (mirror Acquire). The entry we loaded may
	// have been deleted (Connect failed) or replaced by a fresh Mount on the same
	// path; without this guard a stray/duplicate Unmount could Delete(path) the
	// REPLACEMENT entry (SyncMap.Delete keys on path, not identity).
	if cur, ok := m.mounts.Load(path); !ok || cur != mount {
		mount.unlock()
		return fmt.Errorf("not mounted: %s", path)
	}
	// Underflow guard: a stray/duplicate Unmount (or one landing on a Connecting
	// placeholder whose refCount is still 0) must not drive refCount negative or
	// tear down a mount this caller never owned.
	if mount.refCount <= 0 {
		mount.unlock()
		return fmt.Errorf("not mounted: %s", path)
	}

	mount.refCount--
	if mount.refCount > 0 {
		// Other owners still hold the mount; only this owner has released.
		mount.unlock()
		return nil
	}

	m.mounts.Delete(path)
	// Unregister from DeviceRegistry — best effort, regardless of Close
	// outcome. A force-killed transport still needs its registry entry removed
	// so VFS Open/Read/Write/Close don't keep routing to a dead device.
	_ = m.devReg.Unregister(path)
	transport := mount.transport
	mount.unlock()

	// Close transport OUTSIDE the per-entry lock (§易错点 2). Story 48.2 AC4/AC7
	// — propagate Close error (notably *types.DriverError{Code: ErrForceKilled}
	// on SIGKILL escalation) so kernel/reason.go::finishProcess can annotate the
	// Unmount event with forced=true via errors.As + DriverError.Code check.
	if transport != nil {
		return transport.Close()
	}
	return nil
}

// Acquire records an additional owner for an existing mount. Used by the
// resume path (Story 48.1) when reattachMCPMounts finds the canonical
// `/mnt/mcp/<original-pid>-<server>` already present (either a fork-resume
// sibling beat us to it, or the original Suspended owner has not been
// cleaned up yet). Returns an error when the path is not currently mounted —
// callers should fall back to a fresh Mount in that case.
//
// Story 48.6 §易错点 1: a concurrent Mount may have published a Connecting
// placeholder. Acquire takes the per-entry lock, which Mount holds for the whole
// Connect→finalize span, so Acquire blocks until the mount is fully Connected.
// After waking it re-checks that the entry is still the live one and Connected;
// if Mount's Connect failed and deleted the placeholder, Acquire returns
// not-mounted so the caller falls back to a fresh Mount (which now wins the slot
// — no ErrAlreadyMounted death loop).
//
// Unmount must be called exactly once per Mount + Acquire (i.e. once per
// owner) for the transport to actually close.
func (m *MountManager) Acquire(path string) error {
	mount, ok := m.mounts.Load(path)
	if !ok {
		return fmt.Errorf("not mounted: %s", path)
	}
	mount.lock()
	defer mount.unlock()

	// Re-validate after potentially blocking on an in-flight Mount: the
	// placeholder we loaded may have been deleted (Connect failed) or replaced.
	if cur, ok := m.mounts.Load(path); !ok || cur != mount {
		return fmt.Errorf("not mounted: %s", path)
	}
	if mount.Status != MCPStatusConnected || mount.transport == nil {
		return fmt.Errorf("not mounted: %s (status %s)", path, MCPStatusString(mount.Status))
	}
	mount.refCount++
	return nil
}

// GetStatus returns the status of the mount at the given path.
//
// Story 48.6 [Review][Patch] P1: take the per-entry lock before reading Status.
// SyncMap only makes the *MCPMount slot atomic — it grants no happens-before on
// the struct's fields, which Mount finalize / Acquire / Unmount mutate UNDER
// mount.mu. A lock-free read here races those writes (caught by -race), so the
// observed Status could be a stale/torn value mid-finalize.
func (m *MountManager) GetStatus(path string) (MCPStatus, error) {
	mount, ok := m.mounts.Load(path)
	if !ok {
		return 0, fmt.Errorf("not mounted: %s", path)
	}
	mount.lock()
	status := mount.Status
	mount.unlock()
	return status, nil
}

// ListMounts returns all current mounts.
//
// Story 48.6 [Review][Patch] P1: copy each entry UNDER its per-entry lock. The
// value copy reads Status / refCount / transport, all of which Mount finalize
// writes while holding mount.mu — copying without the lock races those writes
// (the placeholder is published by LoadOrStore before its fields are filled in).
func (m *MountManager) ListMounts() []MCPMount {
	result := make([]MCPMount, 0)
	m.mounts.Range(func(_ string, mount *MCPMount) bool {
		mount.lock()
		result = append(result, *mount)
		mount.unlock()
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
