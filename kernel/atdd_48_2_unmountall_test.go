//go:build unix

package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// ATDD 48.2 AC2 — daemon shutdown → mountMgr.UnmountAll → 全部 MCP 子进程清理
//
// Story: _bmad-output/implementation-artifacts/48-2-mcp-transport-graceful-shutdown.md
// Phase: TDD RED — test starts with t.Skip; remove the Skip after Task 2.3
//        (Close 两阶段) + Task 2.4 (Connect 失败 closeProcess) + Task 4.1
//        (Connect 时 applyProcessGroupIsolation) land. UnmountAll itself is
//        unchanged from 48.1 — what 48.2 changes is what `transport.Close()`
//        actually does when the manager invokes it.
//
// Why kernel-level rather than vfs-level: the AC's failure mode is "daemon
// shutdown leaves orphan subprocesses". The shortest path that exercises both
// `k.Shutdown` (which walks `mountMgr.UnmountAll`) AND the per-transport Close
// behaviour is to drive it through the kernel — exactly how reap.go:496 does
// in production. Putting it under vfs would skip the integration leg.
//
// Strategy:
//   - Use 3 *trackingMCPTransport* instances, each one a mock that records
//     Connect/Close call counts AND the duration of its Close.
//   - Real vfs.MountManager (not the kernel mock) so the refCount + Close
//     sequencing actually runs.
//   - Assert AFTER k.Shutdown:
//       (a) every transport saw Close exactly once
//       (b) the longest Close finished within 5.5s (NFR-48-S2)
//       (c) mgr.ListMounts() empty
//       (d) UnmountAll error nil (Story §AC2: failures don't abort the loop,
//           but with all 3 mocks healthy we expect nil err)
//   - One of the 3 transports is configured to SIMULATE the slow forced-kill
//     path: its Close blocks 4.95s then returns *types.DriverError with
//     Code=ErrForceKilled. This proves Shutdown is willing to wait the full
//     graceful budget without timing out itself.
//
// Compile note: tests reference the exported `types.ErrForceKilled` constant.
// (Earlier RED-phase drafts used `types.ErrCode("force_killed")` literal so
// the file could compile before Task 2.5 / 3.2 added the constant; code
// review F5 then renamed the underlying string to "FORCE_KILLED" so the
// literal would have diverged.)
// =============================================================================

// trackingMCPTransport is a vfs.MCPTransport mock customised for AC2:
//   - Connect / Call / Ping return nil (healthy).
//   - Close blocks `closeDelay`, increments a counter, and returns the
//     configured `closeErr`.
//
// Independent from atdd_48_1_helpers_test.go's mockMCPTransport because we
// need closeDelay semantics that the 48.1 mock doesn't expose; introducing a
// new type keeps the 48.1 mock contract stable (the 48.1 ATDD suite cares
// about Connect accounting, not Close timing).
type trackingMCPTransport struct {
	serverName string

	closeDelay time.Duration
	closeErr   error

	connectCount atomic.Int32
	closeCount   atomic.Int32
	closeStart   time.Time
	closeEnd     time.Time
	mu           sync.Mutex // protects closeStart/closeEnd
}

func (t *trackingMCPTransport) Connect(ctx context.Context) error {
	t.connectCount.Add(1)
	return nil
}

func (t *trackingMCPTransport) Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	switch method {
	case "tools/list":
		return json.RawMessage(`{"tools":[]}`), nil
	default:
		return json.RawMessage(`{}`), nil
	}
}

func (t *trackingMCPTransport) Ping(ctx context.Context) error { return nil }

// --- Story 48.5 health/status surface (no-op defaults for legacy mocks) ---
func (t *trackingMCPTransport) Status() vfs.MCPStatus { return vfs.MCPStatusConnected }
func (t *trackingMCPTransport) Alive() bool           { return true }
func (t *trackingMCPTransport) ToolCount() int        { return 0 }
func (t *trackingMCPTransport) ResourceCount() int    { return 0 }
func (t *trackingMCPTransport) LastCheck() time.Time  { return time.Time{} }
func (t *trackingMCPTransport) ReconnectCount() int   { return 0 }
func (t *trackingMCPTransport) StderrTail() []string  { return nil }

func (t *trackingMCPTransport) Close() error {
	t.mu.Lock()
	t.closeStart = time.Now()
	t.mu.Unlock()

	if t.closeDelay > 0 {
		time.Sleep(t.closeDelay)
	}

	t.mu.Lock()
	t.closeEnd = time.Now()
	t.mu.Unlock()

	t.closeCount.Add(1)
	return t.closeErr
}

func (t *trackingMCPTransport) closeElapsed() time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closeEnd.IsZero() {
		return 0
	}
	return t.closeEnd.Sub(t.closeStart)
}

// newTrackingTransportFactory returns a vfs.TransportFactory keyed by
// MCPConfig.ServerName, falling back to a fresh healthy transport if the
// ServerName isn't registered.
func newTrackingTransportFactory(transports map[string]*trackingMCPTransport) vfs.TransportFactory {
	return func(cfg vfs.MCPConfig) (vfs.MCPTransport, error) {
		if t, ok := transports[cfg.ServerName]; ok {
			t.serverName = cfg.ServerName
			return t, nil
		}
		fallback := &trackingMCPTransport{serverName: cfg.ServerName}
		transports[cfg.ServerName] = fallback
		return fallback, nil
	}
}

// -----------------------------------------------------------------------------
// AC2: daemon 优雅 shutdown → 全部 MCP server 子进程树清理（≤ 5.5s）
// -----------------------------------------------------------------------------
func TestATDD_48_2_002_DaemonShutdown_CleansAllMCPSubprocesses(t *testing.T) {
	// Three transports — two healthy fast-closers, one forced-kill simulator.
	transports := map[string]*trackingMCPTransport{
		"github":     {},
		"slack":      {},
		"playwright": {closeDelay: 4950 * time.Millisecond, closeErr: types.NewDriverError("Close", "/mnt/mcp/3-playwright", errors.New("SIGTERM ignored, force-killed"), types.ErrForceKilled)},
	}
	factory := newTrackingTransportFactory(transports)

	llmFile := &mockLLMFile{readData: makeLLMResponse("ac2 shutdown", 1)}
	k, _, _ := newTestKernel(t, llmFile)

	devReg := k.vfs.DeviceRegistry()
	mgr := vfs.NewMountManager(devReg, factory)
	k.SetMountManager(mgr)

	// Mount 3 servers at distinct paths — mimics Story 48.1's "path includes
	// PID" convention so each mount has its own refCount=1.
	mounts := []struct {
		path, server string
	}{
		{"/mnt/mcp/1-github", "github"},
		{"/mnt/mcp/2-slack", "slack"},
		{"/mnt/mcp/3-playwright", "playwright"},
	}
	for _, m := range mounts {
		cfg := vfs.MCPConfig{
			ServerName:    m.server,
			Command:       "/bin/echo",
			Args:          []string{"hello"},
			TransportType: "stdio",
		}
		if err := mgr.Mount(m.path, cfg); err != nil {
			t.Fatalf("Mount(%s): %v", m.path, err)
		}
	}
	if got := len(mgr.ListMounts()); got != 3 {
		t.Fatalf("pre-Shutdown ListMounts len=%d, want 3", got)
	}

	// k.Shutdown internally calls mountMgr.UnmountAll. We time the entire
	// shutdown to enforce the NFR-48-S2 ≤ 5.5s upper bound. The playwright
	// transport alone takes 4.95s; the other two should be <50ms each;
	// UnmountAll is serial (`for path := range paths`) so total ≈ 5.05s.
	start := time.Now()
	k.Shutdown()
	elapsed := time.Since(start)

	if elapsed > 5500*time.Millisecond {
		t.Errorf("Shutdown took %v > 5.5s NFR upper bound — UnmountAll is wasting time per transport", elapsed)
	}
	if elapsed < 4900*time.Millisecond {
		t.Errorf("Shutdown took %v < 4.9s — playwright transport's forced-kill simulation skipped (mock not wired)", elapsed)
	}

	// Every transport must have been Close'd exactly once.
	for name, tp := range transports {
		got := tp.closeCount.Load()
		if got != 1 {
			t.Errorf("transport %q Close count = %d, want 1 (UnmountAll missed it or double-closed)", name, got)
		}
	}

	// ListMounts is now empty.
	if got := len(mgr.ListMounts()); got != 0 {
		t.Errorf("post-Shutdown ListMounts len=%d, want 0; leaked mounts: %+v", got, mgr.ListMounts())
	}

	// DeviceRegistry no longer routes the paths.
	for _, m := range mounts {
		if _, ok := devReg.GetDriver(m.path); ok {
			t.Errorf("DeviceRegistry still routes %q after UnmountAll", m.path)
		}
	}

	// Per-transport elapsed sanity: playwright took ~5s, others <500ms.
	if got := transports["playwright"].closeElapsed(); got < 4900*time.Millisecond {
		t.Errorf("playwright Close elapsed = %v, want ≥ 4.9s (forced-kill path skipped?)", got)
	}
	if got := transports["github"].closeElapsed(); got > 500*time.Millisecond {
		t.Errorf("github Close elapsed = %v > 500ms — fast-path mock unexpectedly slow", got)
	}
}

// -----------------------------------------------------------------------------
// AC2 supplemental: UnmountAll keeps going on per-transport Close error
// -----------------------------------------------------------------------------
//
// Story §AC2: "UnmountAll 内部对单个 Close 失败不 abort（沿用既有 firstErr
// 模式：mount.go:171-176）". This is technically already enforced by 48.1's
// UnmountAll implementation, but Story 48.2's new Close error surface (the
// force_killed sentinel) is the most likely future regressor. This test
// pins the behaviour.
func TestATDD_48_2_002b_UnmountAll_PerTransportErrorDoesNotAbort(t *testing.T) {
	transports := map[string]*trackingMCPTransport{
		"first": {closeErr: types.NewDriverError("Close", "/mnt/mcp/1-first", errors.New("forced"), types.ErrForceKilled)},
		"second":  {},
		"third":   {closeErr: errors.New("transport stream already closed")},
	}
	factory := newTrackingTransportFactory(transports)

	llmFile := &mockLLMFile{readData: makeLLMResponse("ac2b", 1)}
	k, _, _ := newTestKernel(t, llmFile)
	devReg := k.vfs.DeviceRegistry()
	mgr := vfs.NewMountManager(devReg, factory)
	k.SetMountManager(mgr)

	paths := []string{"/mnt/mcp/1-first", "/mnt/mcp/2-second", "/mnt/mcp/3-third"}
	servers := []string{"first", "second", "third"}
	for i, path := range paths {
		cfg := vfs.MCPConfig{ServerName: servers[i], Command: "/bin/echo", TransportType: "stdio"}
		if err := mgr.Mount(path, cfg); err != nil {
			t.Fatalf("Mount(%s): %v", path, err)
		}
	}

	// Call UnmountAll directly (rather than via Shutdown) so we can capture
	// the returned error. Story 48.1's UnmountAll swallows real Close errors
	// (line 173-176 in vfs/mount.go), so even with 2 erroring transports
	// firstErr is nil for now — UnmountAll's contract is "remove all entries".
	// What matters is that no transport is skipped.
	err := mgr.UnmountAll()
	if err != nil {
		t.Logf("UnmountAll returned err=%v (informational — current impl ignores Close errs)", err)
	}

	for name, tp := range transports {
		if got := tp.closeCount.Load(); got != 1 {
			t.Errorf("transport %q Close count = %d, want 1 (abort-on-error regression)", name, got)
		}
	}
	if len(mgr.ListMounts()) != 0 {
		t.Errorf("UnmountAll left mounts behind: %+v", mgr.ListMounts())
	}
}
