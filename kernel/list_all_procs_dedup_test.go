package kernel

import (
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// TestListAllProcs_DedupActiveOverHistory 验证 active 路径优先于 historical：
// 当同一 UUID 同时存在于 procTable（active）和 procHistory（historical）时，
// ListAllProcs 只保留 active 那条（避免渲染为两个平级节点）。
//
// 此场景对应用户报告的"按 r 恢复后树结构断裂"——若 Resume 路径的
// cleanupOldProcessAndHistory 漏清 historical 一侧，dedup 是兜底防线。
func TestListAllProcs_DedupActiveOverHistory(t *testing.T) {
	k := NewKernel(nil, nil, nil)
	defer k.Shutdown()

	const dupUUID = "shared-uuid-dedup-test"

	// 1) 注入一个 active 进程，UUID = dupUUID
	active := NewProcess(0, "active-after-resume", nil)
	active.UUID = dupUUID
	active.mu.Lock()
	active.State = types.StateRunning
	active.CreatedAt = time.Now()
	active.mu.Unlock()
	k.AddProcess(active)

	// 2) 直接在 procHistory 注入同 UUID 的 historical 条目（模拟漏清）
	k.procHistory.Add(vfs.ProcInfo{
		PID:       9999,
		UUID:      dupUUID,
		State:     types.StateDead,
		Intent:    "stale-history-entry",
		CreatedAt: time.Now().Add(-time.Hour),
	})

	all := k.ListAllProcs()

	count := 0
	var kept vfs.ProcInfo
	for _, p := range all {
		if p.UUID == dupUUID {
			count++
			kept = p
		}
	}
	if count != 1 {
		t.Fatalf("expected UUID %q to appear exactly once after dedup, got %d", dupUUID, count)
	}
	if kept.State != types.StateRunning {
		t.Errorf("active should win over historical: expected State=Running, got %v", kept.State)
	}
	if kept.Intent != "active-after-resume" {
		t.Errorf("active should win over historical: expected Intent=active-after-resume, got %q", kept.Intent)
	}
}

// TestListAllProcs_DedupHistoryInternal 验证 historical 内部重复条目被去重。
// 当 reap / cleanupExpiredDead 因任何原因多次 Add 同 UUID，ListAllProcs 应只输出一条。
func TestListAllProcs_DedupHistoryInternal(t *testing.T) {
	k := NewKernel(nil, nil, nil)
	defer k.Shutdown()

	const dupUUID = "history-internal-dup"

	k.procHistory.Add(vfs.ProcInfo{
		PID:       100,
		UUID:      dupUUID,
		State:     types.StateDead,
		Intent:    "first-add",
		CreatedAt: time.Now().Add(-2 * time.Hour),
	})
	k.procHistory.Add(vfs.ProcInfo{
		PID:       100,
		UUID:      dupUUID,
		State:     types.StateDead,
		Intent:    "second-add",
		CreatedAt: time.Now().Add(-time.Hour),
	})

	all := k.ListAllProcs()

	count := 0
	for _, p := range all {
		if p.UUID == dupUUID {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected historical duplicate UUID to dedupe to 1 entry, got %d", count)
	}
}

// TestListAllProcs_EmptyUUID_NotDeduped 验证空 UUID 条目不参与去重（向后兼容
// 没有 UUID 的老进程；空字符串不进 seen 集合）。
func TestListAllProcs_EmptyUUID_NotDeduped(t *testing.T) {
	k := NewKernel(nil, nil, nil)
	defer k.Shutdown()

	k.procHistory.Add(vfs.ProcInfo{
		PID:       200,
		UUID:      "",
		State:     types.StateDead,
		Intent:    "legacy-no-uuid-1",
		CreatedAt: time.Now().Add(-2 * time.Hour),
	})
	k.procHistory.Add(vfs.ProcInfo{
		PID:       201,
		UUID:      "",
		State:     types.StateDead,
		Intent:    "legacy-no-uuid-2",
		CreatedAt: time.Now().Add(-time.Hour),
	})

	all := k.ListAllProcs()

	emptyCount := 0
	for _, p := range all {
		if p.UUID == "" {
			emptyCount++
		}
	}
	if emptyCount != 2 {
		t.Errorf("empty-UUID entries should NOT be deduped, expected 2 got %d", emptyCount)
	}
}
