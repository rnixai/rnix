package kernel

import (
	"sync"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// ============================================================
// ATDD RED PHASE — Story 22.3: 安全状态管理
//
// ImmuneDaemon.Uptime(), ImmuneDaemon.SuspendedPIDs() 以及
// 相关的 nil 保护和并发安全测试。
//
// 测试引用的方法尚不存在，测试将无法编译直到实现完成。
//
// RED → GREEN: 在 kernel/immune.go 中实现所有新增方法。
// ============================================================

// --- 22.3-UNIT-001: [P0] ImmuneDaemon.Uptime returns > 0 when running (AC1) ---

func TestImmuneDaemon_Uptime_Running(t *testing.T) {
	// Given: a running ImmuneDaemon
	dir := t.TempDir()
	store := NewImmuneStore(dir)
	daemon := NewImmuneDaemon(store)
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer daemon.Stop()

	// Allow a tiny bit of time to elapse
	time.Sleep(5 * time.Millisecond)

	// When: getting the uptime
	uptime := daemon.Uptime()

	// Then: uptime is > 0
	if uptime <= 0 {
		t.Errorf("Uptime = %v, want > 0 when daemon is running", uptime)
	}
}

// --- 22.3-UNIT-002: [P0] ImmuneDaemon.Uptime returns 0 when not running (AC1) ---

func TestImmuneDaemon_Uptime_NotRunning(t *testing.T) {
	// Given: an ImmuneDaemon that has NOT been started
	dir := t.TempDir()
	store := NewImmuneStore(dir)
	daemon := NewImmuneDaemon(store)

	// When: getting the uptime
	uptime := daemon.Uptime()

	// Then: uptime is 0
	if uptime != 0 {
		t.Errorf("Uptime = %v, want 0 when daemon is not running", uptime)
	}
}

// --- 22.3-UNIT-003: [P0] ImmuneDaemon.Uptime returns 0 on nil daemon (AC1) ---

func TestImmuneDaemon_Uptime_Nil(t *testing.T) {
	// Given: a nil ImmuneDaemon
	var daemon *ImmuneDaemon

	// When: getting the uptime
	uptime := daemon.Uptime()

	// Then: uptime is 0 and no panic
	if uptime != 0 {
		t.Errorf("Uptime = %v, want 0 for nil daemon", uptime)
	}
}

// --- 22.3-UNIT-004: [P0] ImmuneDaemon.Uptime returns 0 after Stop (AC1) ---

func TestImmuneDaemon_Uptime_AfterStop(t *testing.T) {
	// Given: a daemon that was started then stopped
	dir := t.TempDir()
	store := NewImmuneStore(dir)
	daemon := NewImmuneDaemon(store)
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	daemon.Stop()

	// When: getting the uptime after stop
	uptime := daemon.Uptime()

	// Then: uptime is 0
	if uptime != 0 {
		t.Errorf("Uptime = %v, want 0 after Stop", uptime)
	}
}

// --- 22.3-UNIT-005: [P0] ImmuneDaemon.SuspendedPIDs returns empty when no alerts (AC3) ---

func TestImmuneDaemon_SuspendedPIDs_Empty(t *testing.T) {
	// Given: a running ImmuneDaemon with no alerts
	dir := t.TempDir()
	store := NewImmuneStore(dir)
	daemon := NewImmuneDaemon(store)
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer daemon.Stop()

	// When: getting suspended PIDs
	pids := daemon.SuspendedPIDs()

	// Then: returns non-nil empty slice
	if pids == nil {
		t.Fatal("SuspendedPIDs should return non-nil empty slice, got nil")
	}
	if len(pids) != 0 {
		t.Errorf("expected 0 suspended PIDs, got %d", len(pids))
	}
}

// --- 22.3-UNIT-006: [P0] ImmuneDaemon.SuspendedPIDs returns PIDs from alerts map (AC3) ---

func TestImmuneDaemon_SuspendedPIDs_WithAlerts(t *testing.T) {
	// Given: a running ImmuneDaemon with alerts (via anomaly triggering)
	dir := t.TempDir()
	store := NewImmuneStore(dir)

	// Pre-save a profile with very tight baseline for easy triggering
	profile := &NormalProfile{
		AgentTemplate: "suspended-test-agent",
		SampleCount:   10,
		SyscallMean:   map[string]float64{"Open": 1.0},
		SyscallStdDev: map[string]float64{"Open": 0.1},
	}
	if err := store.SaveProfile(profile); err != nil {
		t.Fatalf("SaveProfile failed: %v", err)
	}

	daemon := NewImmuneDaemon(store)
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer daemon.Stop()

	daemon.SetSuspendFunc(func(pid types.PID) error { return nil })

	// Trigger alerts for two PIDs
	for _, pidVal := range []types.PID{42, 99} {
		daemon.OnProcessStart(pidVal, "suspended-test-agent")
		for i := range 20 {
			daemon.OnSyscallEvent(pidVal, types.SyscallEvent{
				PID:     pidVal,
				Syscall: "Open",
				Args:    map[string]any{"iter": i},
			})
		}
	}

	// When: getting suspended PIDs
	pids := daemon.SuspendedPIDs()

	// Then: returns correct PID list
	if len(pids) != 2 {
		t.Fatalf("expected 2 suspended PIDs, got %d", len(pids))
	}

	// And: both PIDs are present (order not guaranteed)
	pidSet := make(map[types.PID]bool)
	for _, p := range pids {
		pidSet[p] = true
	}
	if !pidSet[types.PID(42)] {
		t.Error("missing PID 42 in SuspendedPIDs")
	}
	if !pidSet[types.PID(99)] {
		t.Error("missing PID 99 in SuspendedPIDs")
	}
}

// --- 22.3-UNIT-007: [P0] ImmuneDaemon.SuspendedPIDs nil-safe (AC3) ---

func TestImmuneDaemon_SuspendedPIDs_Nil(t *testing.T) {
	// Given: a nil ImmuneDaemon
	var daemon *ImmuneDaemon

	// When: getting suspended PIDs
	pids := daemon.SuspendedPIDs()

	// Then: returns nil and no panic
	if pids != nil {
		t.Errorf("SuspendedPIDs on nil daemon should return nil, got %v", pids)
	}
}

// --- 22.3-UNIT-008: [P0] ImmuneDaemon.SuspendedPIDs updated after ClearAlert (AC3) ---

func TestImmuneDaemon_SuspendedPIDs_AfterClear(t *testing.T) {
	// Given: a daemon with one alert
	dir := t.TempDir()
	store := NewImmuneStore(dir)

	profile := &NormalProfile{
		AgentTemplate: "clear-suspended-agent",
		SampleCount:   10,
		SyscallMean:   map[string]float64{"Open": 1.0},
		SyscallStdDev: map[string]float64{"Open": 0.1},
	}
	if err := store.SaveProfile(profile); err != nil {
		t.Fatalf("SaveProfile failed: %v", err)
	}

	daemon := NewImmuneDaemon(store)
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer daemon.Stop()

	daemon.SetSuspendFunc(func(pid types.PID) error { return nil })

	pid := types.PID(55)
	daemon.OnProcessStart(pid, "clear-suspended-agent")
	for i := range 20 {
		daemon.OnSyscallEvent(pid, types.SyscallEvent{
			PID:     pid,
			Syscall: "Open",
			Args:    map[string]any{"iter": i},
		})
	}

	// Verify PID is in suspended list
	pids := daemon.SuspendedPIDs()
	if len(pids) == 0 {
		t.Fatal("expected at least 1 suspended PID before clear")
	}

	// When: clearing the alert
	daemon.ClearAlert(pid)

	// Then: PID is no longer in suspended list
	pids = daemon.SuspendedPIDs()
	for _, p := range pids {
		if p == pid {
			t.Errorf("PID %d should not be in SuspendedPIDs after ClearAlert", pid)
		}
	}
}

// --- 22.3-UNIT-009: [P1] ImmuneDaemon.Uptime concurrent access (AC1) ---

func TestImmuneDaemon_Uptime_Concurrent(t *testing.T) {
	// Given: a running ImmuneDaemon
	dir := t.TempDir()
	store := NewImmuneStore(dir)
	daemon := NewImmuneDaemon(store)
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer daemon.Stop()

	// When: multiple goroutines read Uptime concurrently
	const goroutines = 10
	var wg sync.WaitGroup

	for range goroutines {
		wg.Go(func() {
			uptime := daemon.Uptime()
			if uptime <= 0 {
				t.Errorf("Uptime = %v, want > 0", uptime)
			}
		})
	}
	wg.Wait()

	// Then: no data race (test runs with -race detector)
}

// --- 22.3-UNIT-010: [P1] ImmuneDaemon.SuspendedPIDs concurrent access (AC3) ---

func TestImmuneDaemon_SuspendedPIDs_Concurrent(t *testing.T) {
	// Given: a running ImmuneDaemon with some alerts
	dir := t.TempDir()
	store := NewImmuneStore(dir)

	profile := &NormalProfile{
		AgentTemplate: "concurrent-suspended",
		SampleCount:   10,
		SyscallMean:   map[string]float64{"Open": 1.0},
		SyscallStdDev: map[string]float64{"Open": 0.1},
	}
	if err := store.SaveProfile(profile); err != nil {
		t.Fatalf("SaveProfile failed: %v", err)
	}

	daemon := NewImmuneDaemon(store)
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer daemon.Stop()

	daemon.SetSuspendFunc(func(pid types.PID) error { return nil })

	// Trigger an alert
	pid := types.PID(77)
	daemon.OnProcessStart(pid, "concurrent-suspended")
	for i := range 20 {
		daemon.OnSyscallEvent(pid, types.SyscallEvent{
			PID:     pid,
			Syscall: "Open",
			Args:    map[string]any{"iter": i},
		})
	}

	// When: multiple goroutines read SuspendedPIDs concurrently
	const goroutines = 10
	var wg sync.WaitGroup

	for range goroutines {
		wg.Go(func() {
			_ = daemon.SuspendedPIDs()
		})
	}
	wg.Wait()

	// Then: no data race (test runs with -race detector)
}
