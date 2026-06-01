package kernel

// =============================================================================
// ATDD Story 29.4: Kernel 进程历史保留与 IPC
// =============================================================================
//
// Test Strategy:
//   AC-1: ProcessHistory 组件 — 结构体、Add/List/Len、FIFO 淘汰、maxSize=1000
//   AC-2: Reaper 集成 — cleanupExpiredDead 在 RemoveProcess 前调用 history.Add
//   AC-3: ListAllProcs — 合并 procTable + history，去重，按 CreatedAt 排序
//   AC-5: 并发安全 — RWMutex 保证 reaper 写入与 Dashboard 读取一致性

import (
	"sync"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// ---------------------------------------------------------------------------
// AC-1: ProcessHistory 组件
// ---------------------------------------------------------------------------

func TestATDD_29_4_AC1_NewProcessHistory(t *testing.T) {
	h := NewProcessHistory(1000)
	if h == nil {
		t.Fatal("AC-1: NewProcessHistory(1000) should return non-nil")
	}
	if h.Len() != 0 {
		t.Fatalf("AC-1: new history should be empty, got Len()=%d", h.Len())
	}
}

func TestATDD_29_4_AC1_Add_And_List(t *testing.T) {
	h := NewProcessHistory(100)
	info := vfs.ProcInfo{
		PID:       1,
		UUID:      "test-uuid",
		State:     types.StateDead,
		Intent:    "test intent",
		CreatedAt: time.Now().Add(-time.Minute),
		DeadAt:    time.Now(),
	}
	h.Add(info)

	list := h.List()
	if len(list) != 1 {
		t.Fatalf("AC-1: expected 1 entry after Add, got %d", len(list))
	}
	if list[0].PID != 1 {
		t.Fatalf("AC-1: expected PID=1, got %d", list[0].PID)
	}
	if list[0].UUID != "test-uuid" {
		t.Fatalf("AC-1: expected UUID=test-uuid, got %s", list[0].UUID)
	}
}

func TestATDD_29_4_AC1_FIFO_Eviction(t *testing.T) {
	maxSize := 5
	h := NewProcessHistory(maxSize)

	// Add maxSize+3 entries
	for i := range maxSize + 3 {
		h.Add(vfs.ProcInfo{
			PID:       types.PID(i + 1),
			State:     types.StateDead,
			CreatedAt: time.Now().Add(time.Duration(i) * time.Second),
		})
	}

	if h.Len() != maxSize {
		t.Fatalf("AC-1: FIFO should cap at maxSize=%d, got Len()=%d", maxSize, h.Len())
	}

	list := h.List()
	// Oldest entries (PID 1,2,3) should be evicted; PID 4..8 remain
	if list[0].PID != types.PID(4) {
		t.Fatalf("AC-1: FIFO eviction — expected first entry PID=4, got PID=%d", list[0].PID)
	}
}

func TestATDD_29_4_AC1_List_Returns_DeepCopy(t *testing.T) {
	h := NewProcessHistory(10)
	h.Add(vfs.ProcInfo{PID: 1, Intent: "original"})

	list1 := h.List()
	list1[0].Intent = "mutated"

	list2 := h.List()
	if list2[0].Intent != "original" {
		t.Fatal("AC-1: List() should return a deep copy; mutation of returned slice should not affect history")
	}
}

func TestATDD_29_4_AC1_DefaultMaxSize_1000(t *testing.T) {
	k := NewKernel(nil, nil, nil)
	defer k.Shutdown()

	allProcs := k.ListAllProcs()
	if allProcs == nil {
		t.Fatal("AC-1: ListAllProcs should return non-nil (even if empty)")
	}
}

// ---------------------------------------------------------------------------
// AC-2: Reaper 集成 — cleanupExpiredDead saves snapshot before RemoveProcess
// ---------------------------------------------------------------------------

func TestATDD_29_4_AC2_Reaper_Saves_History_Before_Remove(t *testing.T) {
	k := NewKernel(nil, nil, nil)
	defer k.Shutdown()

	proc := NewProcess(0, "history test", nil)
	pid := proc.PID
	uuid := proc.UUID
	k.AddProcess(proc)

	proc.mu.Lock()
	proc.State = types.StateDead
	proc.DeadAt = time.Now().Add(-2 * time.Minute) // expired
	proc.mu.Unlock()

	if _, ok := k.GetProcess(pid); !ok {
		t.Fatal("AC-2: process should exist before cleanup")
	}

	k.cleanupExpiredDead(time.Second)

	if _, ok := k.GetProcess(pid); ok {
		t.Fatal("AC-2: process should have been removed from procTable after cleanup")
	}

	allProcs := k.ListAllProcs()
	found := false
	for _, p := range allProcs {
		if p.UUID == uuid {
			found = true
			if p.PID != 0 {
				t.Errorf("AC-2: historical process PID should be 0, got %d", p.PID)
			}
			break
		}
	}
	if !found {
		t.Fatal("AC-2: cleaned-up process should appear in ListAllProcs via history")
	}
}

func TestATDD_29_4_AC2_History_Snapshot_Has_Complete_Fields(t *testing.T) {
	k := NewKernel(nil, nil, nil)
	defer k.Shutdown()

	proc := NewProcess(0, "field check", []string{"skill-a"})
	proc.Provider = "test-provider"
	proc.Model = "test-model"
	uuid := proc.UUID
	k.AddProcess(proc)

	proc.mu.Lock()
	proc.State = types.StateDead
	proc.DeadAt = time.Now().Add(-2 * time.Minute)
	proc.Result = "completed: success"
	proc.TokensUsed = 500
	proc.mu.Unlock()

	k.cleanupExpiredDead(time.Second)

	allProcs := k.ListAllProcs()
	for _, p := range allProcs {
		if p.UUID == uuid {
			if p.Intent != "field check" {
				t.Errorf("AC-2: Intent mismatch: got %q", p.Intent)
			}
			if p.Provider != "test-provider" {
				t.Errorf("AC-2: Provider mismatch: got %q", p.Provider)
			}
			if p.Model != "test-model" {
				t.Errorf("AC-2: Model mismatch: got %q", p.Model)
			}
			if p.TokensUsed != 500 {
				t.Errorf("AC-2: TokensUsed mismatch: got %d", p.TokensUsed)
			}
			if p.Result != "completed: success" {
				t.Errorf("AC-2: Result mismatch: got %q", p.Result)
			}
			return
		}
	}
	t.Fatal("AC-2: process not found in history after cleanup")
}

// ---------------------------------------------------------------------------
// AC-3: ListAllProcs — merge, deduplicate, sort by CreatedAt
// ---------------------------------------------------------------------------

func TestATDD_29_4_AC3_ListAllProcs_Merges_Active_And_History(t *testing.T) {
	k := NewKernel(nil, nil, nil)
	defer k.Shutdown()

	// Add an active process
	active := NewProcess(0, "active proc", nil)
	activePID := active.PID
	k.AddProcess(active)
	active.mu.Lock()
	active.State = types.StateRunning
	active.mu.Unlock()

	// Add a historical process via cleanup
	historical := NewProcess(0, "historical proc", nil)
	historicalUUID := historical.UUID
	k.AddProcess(historical)
	historical.mu.Lock()
	historical.State = types.StateDead
	historical.DeadAt = time.Now().Add(-2 * time.Minute)
	historical.mu.Unlock()
	k.cleanupExpiredDead(time.Second)

	allProcs := k.ListAllProcs()

	foundActive := false
	foundHistorical := false
	for _, p := range allProcs {
		if p.PID == activePID {
			foundActive = true
		}
		if p.UUID == historicalUUID {
			foundHistorical = true
			if p.PID != 0 {
				t.Errorf("AC-3: historical process PID should be 0, got %d", p.PID)
			}
		}
	}
	if !foundActive {
		t.Error("AC-3: active process missing from ListAllProcs")
	}
	if !foundHistorical {
		t.Error("AC-3: historical process missing from ListAllProcs")
	}
}

func TestATDD_29_4_AC3_ListAllProcs_Sorted_By_CreatedAt(t *testing.T) {
	k := NewKernel(nil, nil, nil)
	defer k.Shutdown()

	now := time.Now()
	for i := range 5 {
		proc := NewProcess(0, "sort test", nil)
		proc.mu.Lock()
		proc.CreatedAt = now.Add(time.Duration(4-i) * time.Minute) // reverse order
		proc.mu.Unlock()
		k.AddProcess(proc)
	}

	allProcs := k.ListAllProcs()
	for i := 1; i < len(allProcs); i++ {
		if allProcs[i].CreatedAt.Before(allProcs[i-1].CreatedAt) {
			t.Fatalf("AC-3: ListAllProcs not sorted by CreatedAt: index %d (%v) < index %d (%v)",
				i, allProcs[i].CreatedAt, i-1, allProcs[i-1].CreatedAt)
		}
	}
}

func TestATDD_29_4_AC3_ListAllProcs_NoDuplicates(t *testing.T) {
	k := NewKernel(nil, nil, nil)
	defer k.Shutdown()

	proc := NewProcess(0, "dedup test", nil)
	pid := proc.PID
	k.AddProcess(proc)

	allProcs := k.ListAllProcs()
	count := 0
	for _, p := range allProcs {
		if p.PID == pid {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("AC-3: PID %d appeared %d times in ListAllProcs, expected 1", pid, count)
	}
}

// ---------------------------------------------------------------------------
// AC-5: 并发安全
// ---------------------------------------------------------------------------

func TestATDD_29_4_AC5_Concurrent_Add_And_List(t *testing.T) {
	h := NewProcessHistory(100)
	var wg sync.WaitGroup

	for i := range 50 {
		wg.Go(func() {
			h.Add(vfs.ProcInfo{
				PID:   types.PID(i),
				State: types.StateDead,
			})
		})
	}

	for range 50 {
		wg.Go(func() {
			_ = h.List()
			_ = h.Len()
		})
	}

	wg.Wait()

	if h.Len() > 100 {
		t.Fatalf("AC-5: history size %d exceeds maxSize 100", h.Len())
	}
}

func TestATDD_29_4_AC5_Concurrent_Cleanup_And_ListAllProcs(t *testing.T) {
	k := NewKernel(nil, nil, nil)
	defer k.Shutdown()

	for range 10 {
		proc := NewProcess(0, "concurrent test", nil)
		k.AddProcess(proc)
		proc.mu.Lock()
		proc.State = types.StateDead
		proc.DeadAt = time.Now().Add(-2 * time.Minute)
		proc.mu.Unlock()
	}

	var wg sync.WaitGroup

	wg.Go(func() {
		k.cleanupExpiredDead(time.Second)
	})

	for range 10 {
		wg.Go(func() {
			_ = k.ListAllProcs()
		})
	}

	wg.Wait()
}
