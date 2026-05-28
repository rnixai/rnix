package xsync

import (
	"fmt"
	"sync"
	"testing"
)

// =============================================================================
// ATDD 48.5 AC4 — internal/xsync.RingBuffer[T] (Task 1.1-1.3)
//
// Story: _bmad-output/implementation-artifacts/48-5-mcp-health-check-stderr-reconnect.md
// Phase: 🔴 RED — gated behind `//go:build atdd_48_5_red`. Default `make test`
//        does NOT compile this file, so CI stays green until dev-story lands
//        internal/xsync/ringbuf.go. Run the red suite with:
//            go test -tags=atdd_48_5_red -race ./internal/xsync/
//        Pre-implementation that command fails to compile (undefined:
//        NewRingBuffer) — that IS the red signal. Post-implementation it must
//        PASS; then dev removes the build-tag line to fold these into make test.
//
// Required injection point (dev-story Task 1.1):
//   func NewRingBuffer[T any](capacity int) *RingBuffer[T]
//   (*RingBuffer[T]).Push(v T)
//   (*RingBuffer[T]).Snapshot() []T   // oldest→newest, deep copy
//   (*RingBuffer[T]).Len() int
//
// Generic naming follows the SyncMap[K,V] / Registry[Item] convention
// (project-context.md §泛型命名). sync.Mutex protects all access.
// =============================================================================

// -----------------------------------------------------------------------------
// _001: Push + Len + Snapshot ordering (oldest → newest) below capacity
// -----------------------------------------------------------------------------
func TestATDD_48_5_001_RingBuffer_PushSnapshotOrder(t *testing.T) {
	rb := NewRingBuffer[string](256)

	rb.Push("line-1")
	rb.Push("line-2")
	rb.Push("line-3")

	if got := rb.Len(); got != 3 {
		t.Fatalf("Len() = %d, want 3", got)
	}

	snap := rb.Snapshot()
	want := []string{"line-1", "line-2", "line-3"}
	if len(snap) != len(want) {
		t.Fatalf("Snapshot len = %d, want %d: %v", len(snap), len(want), snap)
	}
	for i := range want {
		if snap[i] != want[i] {
			t.Errorf("Snapshot[%d] = %q, want %q (must be oldest→newest)", i, snap[i], want[i])
		}
	}
}

// -----------------------------------------------------------------------------
// _002: capacity overwrite — push > cap, oldest dropped, only last `cap` kept
//        (AC4: "保留最近 256 行，超出丢弃最旧")
// -----------------------------------------------------------------------------
func TestATDD_48_5_002_RingBuffer_CapacityOverwrite(t *testing.T) {
	const cap = 256
	rb := NewRingBuffer[string](cap)

	// Push 300 lines into a 256-slot buffer → first 44 are evicted.
	for i := range 300 {
		rb.Push(fmt.Sprintf("line-%d", i))
	}

	if got := rb.Len(); got != cap {
		t.Fatalf("Len() = %d, want %d (must clamp at capacity)", got, cap)
	}

	snap := rb.Snapshot()
	if len(snap) != cap {
		t.Fatalf("Snapshot len = %d, want %d", len(snap), cap)
	}
	// Oldest retained must be line-44 (300-256), newest line-299.
	if snap[0] != "line-44" {
		t.Errorf("oldest retained = %q, want %q (evicted oldest 44)", snap[0], "line-44")
	}
	if snap[cap-1] != "line-299" {
		t.Errorf("newest = %q, want %q", snap[cap-1], "line-299")
	}
}

// -----------------------------------------------------------------------------
// _003: Snapshot returns a deep copy — mutating it must NOT affect internal
//        state (Story §测试策略 "Snapshot 深拷贝（防调用者改）")
// -----------------------------------------------------------------------------
func TestATDD_48_5_003_RingBuffer_SnapshotDeepCopy(t *testing.T) {
	rb := NewRingBuffer[string](4)
	rb.Push("a")
	rb.Push("b")

	snap := rb.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("Snapshot len = %d, want 2", len(snap))
	}

	// Mutate the returned slice — the caller must not be able to corrupt the
	// buffer's internal storage through the returned slice.
	snap[0] = "MUTATED"

	again := rb.Snapshot()
	if again[0] != "a" {
		t.Errorf("internal state corrupted via returned slice: snap[0]=%q, want %q", again[0], "a")
	}
}

// -----------------------------------------------------------------------------
// _004: concurrent Push / Snapshot is race-free (run with -race).
//        ring buffer guards every access with sync.Mutex
//        (project-context.md §线程安全模式).
// -----------------------------------------------------------------------------
func TestATDD_48_5_004_RingBuffer_ConcurrentPushSnapshot(t *testing.T) {
	rb := NewRingBuffer[int](128)

	var wg sync.WaitGroup
	// 8 writers hammer Push; 4 readers hammer Snapshot/Len concurrently.
	for w := range 8 {
		base := w
		wg.Go(func() {
			for i := range 500 {
				rb.Push(base*1000 + i)
			}
		})
	}
	for range 4 {
		wg.Go(func() {
			for range 500 {
				_ = rb.Snapshot()
				_ = rb.Len()
			}
		})
	}
	wg.Wait()

	// Buffer must never exceed capacity regardless of write volume.
	if got := rb.Len(); got > 128 {
		t.Errorf("Len() = %d, exceeds capacity 128 after concurrent load", got)
	}
}
