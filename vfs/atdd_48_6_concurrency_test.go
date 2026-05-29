package vfs

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// =============================================================================
// ATDD 48.6 AC1/AC2 — concurrent mount + per-server mount_timeout
//
// Story: _bmad-output/implementation-artifacts/48-6-mount-concurrency-per-server-timeout.md
// Phase: ✅ GREEN — the `//go:build atdd_48_6_red` tag was removed after the dev
//        implementation landed, folding these into `make test` (same gating flow
//        as Story 48.5). Run: go test -race -run TestATDD_48_6 ./vfs/
//
// Pre-impl RED signals (now satisfied by the implementation):
//   _001 — BEHAVIORAL red: the current global `MountManager.mu` serializes every
//          Mount, so 10 × 200ms Connect ≈ 2s, blowing the < 1s parallel bound;
//          peak concurrency stays at 1.
//   _002 — COMPILE red: MCPConfig.MountTimeout does not exist yet (Task 1.2). The
//          compile failure under the tag IS the red signal (48.5 convention).
//   _003 — BEHAVIORAL red: Mount uses the hardcoded `mountTimeout = 500ms` const,
//          so a 600ms Connect times out; the default must become 5s (Task 2.3).
//
// Injection points (dev-story Task 1.2 / 2.x):
//   MCPConfig.MountTimeout time.Duration                 // new runtime field
//   MountManager.Mount honors config.MountTimeout, default 5s (was const 500ms)
//   per-mount-entry locking so distinct paths Connect in parallel (no global mu)
//   Connect timeout → no residual half-mounted entry (Task 2.4)
//
// Reuses mockMCPTransport from mount_test.go (same package, untagged → present in
// any test build).
// =============================================================================

// newDelayedConnectFactory hands out a FRESH mockMCPTransport per Mount, each
// Connect blocking for `delay` (ctx-aware, so a per-server mount_timeout shorter
// than `delay` surfaces as a Connect timeout). inFlight/peak track overlap so a
// test can prove different paths Connect concurrently rather than serially.
func newDelayedConnectFactory(delay time.Duration, inFlight, peak *int64) TransportFactory {
	return func(config MCPConfig) (MCPTransport, error) {
		return &mockMCPTransport{
			connectFn: func(ctx context.Context) error {
				cur := atomic.AddInt64(inFlight, 1)
				for {
					p := atomic.LoadInt64(peak)
					if cur <= p || atomic.CompareAndSwapInt64(peak, p, cur) {
						break
					}
				}
				defer atomic.AddInt64(inFlight, -1)
				select {
				case <-time.After(delay):
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			},
		}, nil
	}
}

// -----------------------------------------------------------------------------
// _001: 10 agents concurrently mount distinct paths → Connect runs in parallel,
//        completing well under the serial sum, with peak overlap > 1. (AC1)
// -----------------------------------------------------------------------------
func TestATDD_48_6_001_ConcurrentMountsRunConnectInParallel(t *testing.T) {
	const n = 10
	const connectDelay = 200 * time.Millisecond

	var inFlight, peak int64
	devReg := NewDeviceRegistry()
	mgr := NewMountManager(devReg, newDelayedConnectFactory(connectDelay, &inFlight, &peak))

	start := time.Now()
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			path := fmt.Sprintf("/mnt/mcp/svc%d", i)
			cfg := MCPConfig{ServerName: path, Command: "test", TransportType: "stdio"}
			errs[i] = mgr.Mount(path, cfg)
		}(i)
	}
	wg.Wait()
	elapsed := time.Since(start)

	for i, err := range errs {
		if err != nil {
			t.Fatalf("Mount svc%d failed: %v", i, err)
		}
	}
	// Serial (global mutex) ≈ n*connectDelay = 2s; parallel ≈ connectDelay.
	if elapsed > time.Second {
		t.Errorf("10 concurrent mounts took %v — Connect appears serialized by a global lock (want < 1s; serial would be ~%v)", elapsed, n*connectDelay)
	}
	if peak < 2 {
		t.Errorf("peak concurrent Connect = %d — Connects never overlapped; the global mutex still serializes Mount", peak)
	}
	if got := len(mgr.ListMounts()); got != n {
		t.Errorf("ListMounts = %d, want %d", got, n)
	}
}

// -----------------------------------------------------------------------------
// _002: per-server mount_timeout shorter than Connect → Mount fails with a
//        timeout AND leaves NO residual half-mounted entry. (AC2)
// -----------------------------------------------------------------------------
func TestATDD_48_6_002_PerServerMountTimeout_NoResidualEntry(t *testing.T) {
	var inFlight, peak int64
	devReg := NewDeviceRegistry()
	// Connect blocks 300ms; mount_timeout is configured to 100ms → must time out.
	mgr := NewMountManager(devReg, newDelayedConnectFactory(300*time.Millisecond, &inFlight, &peak))

	cfg := MCPConfig{
		ServerName:    "slow",
		Command:       "test",
		TransportType: "stdio",
		MountTimeout:  100 * time.Millisecond, // Task 1.2 new runtime field
	}
	err := mgr.Mount("/mnt/mcp/slow", cfg)
	if err == nil {
		t.Fatal("Mount succeeded despite Connect exceeding the per-server mount_timeout; want a timeout error")
	}
	// AC2: "不残留半挂载条目" — the placeholder must be cleaned up on Connect failure.
	if _, gerr := mgr.GetStatus("/mnt/mcp/slow"); gerr == nil {
		t.Error("timed-out mount left a residual entry (GetStatus found it); want not-mounted")
	}
	for _, m := range mgr.ListMounts() {
		if m.Path == "/mnt/mcp/slow" {
			t.Error("timed-out mount still present in ListMounts; half-mount not cleaned up")
		}
	}
}

// -----------------------------------------------------------------------------
// _003: with mount_timeout unset, the default is 5s — a 600ms Connect (which the
//        legacy hardcoded 500ms cap would have rejected) now succeeds. (AC2)
// -----------------------------------------------------------------------------
func TestATDD_48_6_003_DefaultMountTimeoutIs5s(t *testing.T) {
	var inFlight, peak int64
	devReg := NewDeviceRegistry()
	// Connect takes 600ms — beats the OLD hardcoded 500ms cap but is well under
	// the new 5s default. With MountTimeout unset (zero), Mount must fall back to 5s.
	mgr := NewMountManager(devReg, newDelayedConnectFactory(600*time.Millisecond, &inFlight, &peak))

	cfg := MCPConfig{ServerName: "boot", Command: "test", TransportType: "stdio"} // no MountTimeout
	if err := mgr.Mount("/mnt/mcp/boot", cfg); err != nil {
		t.Fatalf("Mount failed under the default timeout (a 600ms Connect should fit in 5s): %v — is the default still the old 500ms?", err)
	}
	if _, gerr := mgr.GetStatus("/mnt/mcp/boot"); gerr != nil {
		t.Errorf("mount not registered after a successful Connect: %v", gerr)
	}
}

// -----------------------------------------------------------------------------
// _004 [Review][Patch] P1: the observability read paths (GetStatus / ListMounts)
//        must not data-race Mount's finalize. While a Mount is mid-Connect its
//        placeholder is already published in the SyncMap but its Status/refCount/
//        transport fields are written under the per-entry lock at finalize. A
//        lock-free reader hammering GetStatus+ListMounts during that window trips
//        -race unless the read paths also take the per-entry lock. This is the
//        regression guard for the race the original concurrency tests missed
//        (they only read AFTER wg.Wait(), never in-flight).
// -----------------------------------------------------------------------------
func TestATDD_48_6_004_ReadPathsDoNotRaceFinalize(t *testing.T) {
	var inFlight, peak int64
	devReg := NewDeviceRegistry()
	// A 200ms Connect window keeps placeholders in Connecting state long enough
	// for the readers below to observe them mid-finalize.
	mgr := NewMountManager(devReg, newDelayedConnectFactory(200*time.Millisecond, &inFlight, &peak))

	const n = 8
	stop := make(chan struct{})
	var readers sync.WaitGroup
	for range 4 {
		readers.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
					for i := range n {
						_, _ = mgr.GetStatus(fmt.Sprintf("/mnt/mcp/race%d", i))
					}
					_ = mgr.ListMounts()
				}
			}
		})
	}

	var mounters sync.WaitGroup
	for i := range n {
		mounters.Go(func() {
			path := fmt.Sprintf("/mnt/mcp/race%d", i)
			cfg := MCPConfig{ServerName: path, Command: "test", TransportType: "stdio"}
			if err := mgr.Mount(path, cfg); err != nil {
				t.Errorf("Mount %s: %v", path, err)
			}
		})
	}
	mounters.Wait()
	close(stop)
	readers.Wait()

	if got := len(mgr.ListMounts()); got != n {
		t.Errorf("ListMounts = %d, want %d", got, n)
	}
}
