package kernel

// ATDD red-phase scaffolds for Story 56.6 — 合成节点标记 + 排除 resume 误用
// (AC6) + Detail 降级前提 (AC7 UNIT-021).
//
// 缺口本质: vfs.ProcInfo 无 Synthetic 标记 → 合成观测节点无法与真实进程区分；
//   Epic 42 把 Dead 当可 resume → 合成节点会被 `rnix ps --resumable` 列出、被
//   `rnix resume <uuid>` 尝试 spawn（它根本不是 rnix 进程）。修复 = ProcInfo 加
//   Synthetic 字段（本文件 ATDD 已加最小骨架）+ procInfoDisk DTO 持久化（dev）+
//   ListResumable/Resume 排除。
//
// 红灯机制（记忆 atdd-code-story-red-mechanism-preference）:
//   🔴 RED（UNIT-014/INT-016/INT-017）: t.Skip，dev-story 移 skip 验 RED→GREEN。
//   🟢 GREEN-guard（UNIT-015/UNIT-021，不 skip）: 向后兼容 + detail 降级前提，
//      实现前后都须真。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// ---------------------------------------------------------------------------
// 🔴 56-6-UNIT-014 (P1, AC6): ProcInfo.Synthetic 经 SaveProcInfo/LoadProcHistory
// round-trip 保留。RED: procInfoDisk DTO 未接 Synthetic 字段 → 落盘即丢。
// ---------------------------------------------------------------------------
func TestATDD_56_6_UNIT_014_SyntheticFieldRoundTrips(t *testing.T) {

	baseDir := t.TempDir()
	info := vfs.ProcInfo{
		UUID: "synth-rt-1", Synthetic: true, ParentUUID: "host-rt",
		PID: 0, State: types.StateDead, CreatedAt: time.Now(),
	}
	if err := SaveProcInfo(baseDir, info); err != nil {
		t.Fatalf("SaveProcInfo: %v", err)
	}
	hist, err := LoadProcHistory(baseDir, 100)
	if err != nil {
		t.Fatalf("LoadProcHistory: %v", err)
	}
	loaded := hist.FindByUUID("synth-rt-1")
	if loaded == nil {
		t.Fatal("AC6 FAIL: 合成节点未被 LoadProcHistory 加载")
	}
	if !loaded.Synthetic {
		t.Error("AC6 FAIL: Synthetic 未 round-trip（procInfoDisk DTO 未持久化该字段）")
	}
}

// ---------------------------------------------------------------------------
// 🟢 56-6-UNIT-015 (P1, AC6 · GREEN-guard 不 skip): 旧快照缺 synthetic 字段 →
// 加载默认 false（向后兼容）。现状（无字段）+ 修复后（omitempty 默认 false）都须真。
// ---------------------------------------------------------------------------
func TestATDD_56_6_UNIT_015_LegacySnapshotDefaultsSyntheticFalse(t *testing.T) {
	baseDir := t.TempDir()
	dir := filepath.Join(baseDir, "steps", "legacy-1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 旧快照 proc-info.json：无 synthetic 键。
	legacy := `{"uuid":"legacy-1","pid":7,"state":"dead","intent":"old proc","created_at":"2026-01-01T00:00:00Z","tokens_used":0}`
	if err := os.WriteFile(filepath.Join(dir, "proc-info.json"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	hist, err := LoadProcHistory(baseDir, 100)
	if err != nil {
		t.Fatalf("LoadProcHistory: %v", err)
	}
	loaded := hist.FindByUUID("legacy-1")
	if loaded == nil {
		t.Fatal("AC6 FAIL: 旧快照未加载")
	}
	if loaded.Synthetic {
		t.Error("AC6 FAIL: 旧快照缺 synthetic 字段应默认 false（向后兼容被破坏）")
	}
}

// ---------------------------------------------------------------------------
// 🟢 56-6-UNIT-021 (P1, AC7 detail 降级前提 · GREEN-guard 不 skip): 合成节点有
// steps.jsonl + proc-info.json 但无 process-meta.json → 仍能经 LoadProcHistory
// 加载并查询（detail-from-history 从 proc-info.json 构建、不读 process-meta.json）。
// 护栏防"detail/history 强依赖 process-meta.json"回归。完整 TUI Detail 视图降级
// 入手工验证（bubbletea 视图层不宜单测）。
// ---------------------------------------------------------------------------
func TestATDD_56_6_UNIT_021_SyntheticNodeLoadsWithoutProcessMeta(t *testing.T) {
	baseDir := t.TempDir()
	info := vfs.ProcInfo{
		UUID: "synth-nometa", Synthetic: true, ParentUUID: "host",
		PID: 0, State: types.StateDead, CreatedAt: time.Now(), Intent: "subagent",
	}
	if err := SaveProcInfo(baseDir, info); err != nil { // 写 proc-info.json
		t.Fatalf("SaveProcInfo: %v", err)
	}
	// steps.jsonl 存在；故意不写 process-meta.json。
	stepsPath := filepath.Join(baseDir, "steps", "synth-nometa", "steps.jsonl")
	if err := os.WriteFile(stepsPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hist, err := LoadProcHistory(baseDir, 100)
	if err != nil {
		t.Fatalf("AC7 FAIL: 缺 process-meta.json 致 LoadProcHistory 报错: %v", err)
	}
	if hist.FindByUUID("synth-nometa") == nil {
		t.Error("AC7 FAIL: 缺 process-meta.json 的合成节点应能从 proc-info.json 加载（Detail 降级前提）")
	}
}

// ---------------------------------------------------------------------------
// 🔴 56-6-INT-016 (P0, AC6): ListResumable 排除合成节点（真实可 resume 节点保留）。
// RED: ListResumable 不识别 Synthetic（且 procInfoDisk 未持久化该字段）→ 端到端落空。
// ---------------------------------------------------------------------------
func TestATDD_56_6_INT_016_ListResumableExcludesSynthetic(t *testing.T) {

	baseDir := t.TempDir()
	writeResumableEntry566(t, baseDir, vfs.ProcInfo{
		UUID: "real-r", PID: 9, State: types.StateDead, CreatedAt: time.Now(),
	})
	writeResumableEntry566(t, baseDir, vfs.ProcInfo{
		UUID: "synth-r", Synthetic: true, ParentUUID: "host", PID: 0,
		State: types.StateDead, CreatedAt: time.Now(),
	})

	list, err := ListResumable(baseDir)
	if err != nil {
		t.Fatalf("ListResumable: %v", err)
	}
	var sawSynth, sawReal bool
	for _, p := range list {
		switch p.UUID {
		case "synth-r":
			sawSynth = true
		case "real-r":
			sawReal = true
		}
	}
	if sawSynth {
		t.Error("AC6 FAIL: ListResumable 未排除合成节点（synth-r 仍可 resume → 会试图 spawn 虚拟节点）")
	}
	if !sawReal {
		t.Error("AC6 FAIL: ListResumable 误排除真实可 resume 节点 real-r（过度过滤）")
	}
}

// ---------------------------------------------------------------------------
// 🔴 56-6-INT-017 (P0, AC6): resume 合成节点报清晰错误（不尝试 spawn）。
// RED: Resume 不识别 Synthetic → 走常规路径（非 synthetic 错误或尝试 spawn）。
// 注: dev 应在 Resume 早期经 procHistory.FindByUUID(uuid).Synthetic 拦截。
// ---------------------------------------------------------------------------
func TestATDD_56_6_INT_017_ResumeOfSyntheticNodeRejected(t *testing.T) {

	k := newSimpleKernel(t)
	k.dataDir = t.TempDir()
	uuid := "synth-resume-1"
	writeResumableEntry566(t, k.dataDir, vfs.ProcInfo{
		UUID: uuid, Synthetic: true, ParentUUID: "host", PID: 0,
		State: types.StateDead, CreatedAt: time.Now(),
	})
	// 同步注册进 procHistory，供 dev 在 Resume 早期经 FindByUUID 识别合成。
	k.procHistory.Add(vfs.ProcInfo{UUID: uuid, Synthetic: true, ParentUUID: "host", State: types.StateDead})

	_, err := k.Resume(uuid)
	if err == nil {
		t.Fatal("AC6 FAIL: resume 合成节点应返回错误，got nil（试图 spawn 虚拟节点）")
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "synthetic") && !strings.Contains(err.Error(), "合成") && !strings.Contains(msg, "not resumable") {
		t.Errorf("AC6 FAIL: 错误信息应指明合成节点不可 resume, got %q", err.Error())
	}
}

// ---------------------------------------------------------------------------
// 🔵 56-6-INT-018 (AC6 defense-in-depth, Story 56.6 code-review patch 3): a
// synthetic node present on disk but ABSENT from the in-memory procHistory
// (FIFO eviction / procHistory miss) is still rejected by the disk-level guard
// in resumeFromHistory — the in-memory guard at ResumeWithOpts is bypassed.
// ---------------------------------------------------------------------------
func TestATDD_56_6_INT_018_ResumeSyntheticFromDiskRejected(t *testing.T) {
	k := newSimpleKernel(t)
	k.dataDir = t.TempDir()
	uuid := "synth-disk-only"
	// Write under <dataDir>/global so FindBaseDirByUUID (which scans projects/
	// and global/) discovers it and routing reaches resumeFromHistory.
	writeResumableEntry566(t, filepath.Join(k.dataDir, "global"), vfs.ProcInfo{
		UUID: uuid, Synthetic: true, ParentUUID: "host", PID: 0,
		State: types.StateDead, CreatedAt: time.Now(),
	})
	// 故意不注册进 procHistory —— 模拟 FIFO 驱逐 / 内存未命中，绕过 ResumeWithOpts
	// 的内存层守卫，逼到 resumeFromHistory 的磁盘层守卫。

	_, err := k.Resume(uuid)
	if err == nil {
		t.Fatal("AC6 FAIL: 磁盘层 resume 合成节点应返回错误，got nil（试图 spawn 虚拟节点）")
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "synthetic") && !strings.Contains(err.Error(), "合成") && !strings.Contains(msg, "not resumable") {
		t.Errorf("AC6 FAIL: 磁盘层错误信息应指明合成节点不可 resume, got %q", err.Error())
	}
}

// writeResumableEntry566 落盘一个 proc-info.json + steps.jsonl，使该 UUID 在
// ListResumable/Resume 看来"可 resume"（需 steps.jsonl OR checkpoint.json）。
func writeResumableEntry566(t *testing.T, baseDir string, info vfs.ProcInfo) {
	t.Helper()
	if err := SaveProcInfo(baseDir, info); err != nil {
		t.Fatalf("SaveProcInfo(%s): %v", info.UUID, err)
	}
	stepsPath := filepath.Join(baseDir, "steps", info.UUID, "steps.jsonl")
	if err := os.WriteFile(stepsPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write steps.jsonl(%s): %v", info.UUID, err)
	}
}
