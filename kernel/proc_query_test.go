// Package kernel — proc_query_test.go
//
// ListAllProcs 排序确定性契约测试（spec-dashboard-tree-stable-sort-identity）。
//
// 背景：ListAllProcs 的输入含 SyncMap.Range 随机序（active）与 history 插入序，
// 且 slices.SortFunc 是不稳定排序——CreatedAt 并列时若无 tiebreak，每次调用
// 返回顺序漂移 → IPC 分页窗口漂移 → Dashboard Agent Tree 行跳动。
// 本文件锁定全序比较链：CreatedAt → UUID → PID。
package kernel

import (
	"time"

	"testing"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// TestKernelImpl_ListAllProcs_DeterministicOrderOnCreatedAtTie 验证：
// 多条 CreatedAt 完全相同、UUID 不同的历史进程（乱序 Add）+ 同刻活跃进程，
// 多次调用 ListAllProcs 返回顺序恒定，且并列组内按 UUID 升序。
func TestKernelImpl_ListAllProcs_DeterministicOrderOnCreatedAtTie(t *testing.T) {
	k := NewKernel(nil, nil, nil)
	defer k.Shutdown()

	tie := time.Date(2026, 7, 4, 10, 0, 0, 0, time.UTC)

	// 历史进程：CreatedAt 完全相同，UUID 乱序 Add（PID 会被 ListAllProcs 清零）。
	historyUUIDs := []string{
		"019f2d24-0000-7000-8000-00000000000e",
		"019f2d24-0000-7000-8000-00000000000a",
		"019f2d24-0000-7000-8000-00000000000c",
		"019f2d24-0000-7000-8000-00000000000f",
		"019f2d24-0000-7000-8000-00000000000b",
		"019f2d24-0000-7000-8000-00000000000d",
	}
	for i, u := range historyUUIDs {
		k.procHistory.Add(vfs.ProcInfo{
			UUID:      u,
			PID:       types.PID(500 + i),
			State:     types.StateDead,
			CreatedAt: tie,
		})
	}

	// 活跃进程：同一 CreatedAt，UUID 与历史条目交错（活跃 PID > 0 保留）。
	activeUUIDs := []string{
		"019f2d24-0000-7000-8000-0000000000a5", // 排在所有历史条目之后（'a' 段 > '0' 段）
		"019f2d24-0000-7000-8000-000000000001", // 排在所有历史条目之前
	}
	for _, u := range activeUUIDs {
		proc := NewProcess(0, "tie-order active", nil)
		proc.mu.Lock()
		proc.UUID = u
		proc.CreatedAt = tie
		proc.State = types.StateRunning
		proc.mu.Unlock()
		k.AddProcess(proc)
	}

	// 边界锚点：CreatedAt 主键仍优先于 UUID tiebreak。
	k.procHistory.Add(vfs.ProcInfo{
		UUID:      "zzzzzzzz-0000-7000-8000-000000000000", // UUID 最大，但时间最早
		State:     types.StateDead,
		CreatedAt: tie.Add(-time.Minute),
	})
	k.procHistory.Add(vfs.ProcInfo{
		UUID:      "00000000-0000-7000-8000-000000000000", // UUID 最小，但时间最晚
		State:     types.StateDead,
		CreatedAt: tie.Add(time.Minute),
	})

	extractUUIDs := func(procs []vfs.ProcInfo) []string {
		out := make([]string, len(procs))
		for i, p := range procs {
			out[i] = p.UUID
		}
		return out
	}

	first := extractUUIDs(k.ListAllProcs())
	if len(first) != len(historyUUIDs)+len(activeUUIDs)+2 {
		t.Fatalf("expected %d procs, got %d", len(historyUUIDs)+len(activeUUIDs)+2, len(first))
	}

	// 边界锚点：主键 CreatedAt 优先。
	if first[0] != "zzzzzzzz-0000-7000-8000-000000000000" {
		t.Errorf("earliest CreatedAt should sort first regardless of UUID, got %q at index 0", first[0])
	}
	if first[len(first)-1] != "00000000-0000-7000-8000-000000000000" {
		t.Errorf("latest CreatedAt should sort last regardless of UUID, got %q at last index", first[len(first)-1])
	}

	// 并列组（index 1..len-2）内按 UUID 升序。
	tieGroup := first[1 : len(first)-1]
	for i := 1; i < len(tieGroup); i++ {
		if tieGroup[i-1] >= tieGroup[i] {
			t.Errorf("CreatedAt tie group not in ascending UUID order: index %d (%q) >= index %d (%q)",
				i-1, tieGroup[i-1], i, tieGroup[i])
		}
	}

	// 多次调用（≥5 次）顺序完全一致——覆盖 SyncMap.Range 随机序 + 不稳定排序。
	for run := range 5 {
		got := extractUUIDs(k.ListAllProcs())
		if len(got) != len(first) {
			t.Fatalf("run %d: length changed: got %d, want %d", run, len(got), len(first))
		}
		for i := range got {
			if got[i] != first[i] {
				t.Fatalf("run %d: order drifted at index %d: got %q, want %q", run, i, got[i], first[i])
			}
		}
	}
}

// TestKernelImpl_ListAllProcs_StableOrderOnEmptyUUIDFullTie 验证比较链完全并列的
// 残洞已被稳定排序关死：历史条目 PID 被清零、UUID 为空、CreatedAt 相同时，三个
// 排序键全部相等——此时顺序必须退化为 procHistory 的 FIFO 插入序并跨调用恒定，
// 而不是不稳定排序的随机序（review finding：盲审 #2）。
func TestKernelImpl_ListAllProcs_StableOrderOnEmptyUUIDFullTie(t *testing.T) {
	k := NewKernel(nil, nil, nil)
	defer k.Shutdown()

	tie := time.Date(2026, 7, 4, 11, 0, 0, 0, time.UTC)
	intents := []string{"legacy-a", "legacy-b", "legacy-c", "legacy-d"}
	for i, intent := range intents {
		k.procHistory.Add(vfs.ProcInfo{
			UUID:      "", // legacy 条目无 UUID；PID 会被 ListAllProcs 清零
			PID:       types.PID(900 + i),
			State:     types.StateDead,
			Intent:    intent,
			CreatedAt: tie,
		})
	}

	extractIntents := func(procs []vfs.ProcInfo) []string {
		out := make([]string, len(procs))
		for i, p := range procs {
			out[i] = p.Intent
		}
		return out
	}

	first := extractIntents(k.ListAllProcs())
	if len(first) != len(intents) {
		t.Fatalf("expected %d procs, got %d", len(intents), len(first))
	}
	// 稳定排序保留 FIFO 插入序。
	for i, want := range intents {
		if first[i] != want {
			t.Fatalf("full-tie group should keep FIFO insertion order: index %d got %q, want %q", i, first[i], want)
		}
	}
	for run := range 5 {
		got := extractIntents(k.ListAllProcs())
		for i := range got {
			if got[i] != first[i] {
				t.Fatalf("run %d: full-tie order drifted at index %d: got %q, want %q", run, i, got[i], first[i])
			}
		}
	}
}
