// Package detail — transitions_test.go (Story 38-5 PR11 Step 4(b) Phase 2)
//
// HandlePIDChangeWithCache 行为契约测试（与 cmd/rnix.handlePIDChange line
// 1156-1162 byte-for-byte 等价 · 28-4 AC-4 PID 复用安全契约保留）。
package detail

import (
	"testing"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
)

// TestHandlePIDChangeWithCache_HitReusesEntry 验证 cache 命中时复用条目（28-4 AC-4）。
func TestHandlePIDChangeWithCache_HitReusesEntry(t *testing.T) {
	cached := &ipc.GetProcDetailResponse{PID: 100, UUID: "uuid-100"}
	state := DetailState{
		Detail: nil,
		PID:    0,
		Cache:  map[string]*ipc.GetProcDetailResponse{"uuid-100": cached},
	}

	out := HandlePIDChangeWithCache(state, types.PID(100), "uuid-100")

	if out.Detail != cached {
		t.Errorf("Detail = %p, want cached %p (28-4 AC-4 reuse)", out.Detail, cached)
	}
	if out.PID != types.PID(100) {
		t.Errorf("PID = %d, want 100", out.PID)
	}
}

// TestHandlePIDChangeWithCache_MissClearsState 验证 cache 未命中时清空 Detail+PID。
func TestHandlePIDChangeWithCache_MissClearsState(t *testing.T) {
	prev := &ipc.GetProcDetailResponse{PID: 100, UUID: "uuid-100"}
	state := DetailState{
		Detail: prev,
		PID:    100,
		Cache:  map[string]*ipc.GetProcDetailResponse{"uuid-100": prev},
	}

	out := HandlePIDChangeWithCache(state, types.PID(200), "uuid-200")

	if out.Detail != nil {
		t.Errorf("Detail = %p, want nil (cache miss → clear)", out.Detail)
	}
	if out.PID != 0 {
		t.Errorf("PID = %d, want 0 (cache miss → clear)", out.PID)
	}
}

// TestHandlePIDChangeWithCache_NilCacheSafe 验证 Cache nil 时不 panic。
func TestHandlePIDChangeWithCache_NilCacheSafe(t *testing.T) {
	state := DetailState{Detail: nil, PID: 0, Cache: nil}

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("panicked on nil Cache: %v", r)
		}
	}()

	out := HandlePIDChangeWithCache(state, types.PID(42), "uuid-42")

	if out.Detail != nil {
		t.Errorf("Detail = %p, want nil", out.Detail)
	}
	if out.PID != 0 {
		t.Errorf("PID = %d, want 0", out.PID)
	}
}

// TestHandlePIDChangeWithCache_PreservesCacheAndTick 验证 Cache + Tick 字段不变。
func TestHandlePIDChangeWithCache_PreservesCacheAndTick(t *testing.T) {
	cached := &ipc.GetProcDetailResponse{PID: 100, UUID: "uuid-100"}
	cacheMap := map[string]*ipc.GetProcDetailResponse{"uuid-100": cached}
	state := DetailState{
		Detail: nil,
		PID:    0,
		Cache:  cacheMap,
		Tick:   42,
	}

	out := HandlePIDChangeWithCache(state, types.PID(100), "uuid-100")

	if len(out.Cache) != 1 {
		t.Errorf("Cache size = %d, want 1 (preserved)", len(out.Cache))
	}
	if out.Cache["uuid-100"] != cached {
		t.Errorf("Cache[uuid-100] missing or wrong (preserved entries)")
	}
	if out.Tick != 42 {
		t.Errorf("Tick = %d, want 42 (preserved)", out.Tick)
	}
}

// TestHandlePIDChangeWithCache_EmptyUUIDMisses 验证空 UUID 走 miss 分支。
func TestHandlePIDChangeWithCache_EmptyUUIDMisses(t *testing.T) {
	cached := &ipc.GetProcDetailResponse{PID: 100, UUID: "uuid-100"}
	state := DetailState{
		Detail: cached,
		PID:    100,
		Cache:  map[string]*ipc.GetProcDetailResponse{"uuid-100": cached},
	}

	out := HandlePIDChangeWithCache(state, types.PID(0), "")

	if out.Detail != nil {
		t.Errorf("Detail = %p, want nil (empty uuid miss)", out.Detail)
	}
	if out.PID != 0 {
		t.Errorf("PID = %d, want 0 (empty uuid miss)", out.PID)
	}
}

// TestHandlePIDChangeWithCache_PIDZero_HitStillReuses 验证 PID=0 时 cache 命中仍生效。
//
// 这是 dashboard.go::handlePIDChange selectedPID==0 分支不会调用本函数的边界 case
// 守护测试（如果未来调用方误用，行为应仍是 cache 复用而非清空）。
func TestHandlePIDChangeWithCache_PIDZero_HitStillReuses(t *testing.T) {
	cached := &ipc.GetProcDetailResponse{PID: 0, UUID: "uuid-0"}
	state := DetailState{
		Cache: map[string]*ipc.GetProcDetailResponse{"uuid-0": cached},
	}

	out := HandlePIDChangeWithCache(state, types.PID(0), "uuid-0")

	if out.Detail != cached {
		t.Errorf("Detail = %p, want cached (cache hit applies regardless of pid value)", out.Detail)
	}
}
