package memory

import (
	"strings"
	"testing"
)

// ============================================================
// ATDD RED PHASE — Story 54.3: AC3
//
// drivers/memory/prompts/memory_profile.txt:18 的 /dev/memory/commit → 工具名
// MemoryCommit（54.2 已把该工具 ToolDef.Name 改为 MemoryCommit）。
// 载体：包内私有 loadPrompt("memory_profile")（//go:embed prompts/*.txt，补 .txt）。
//
//   🔴 RED: INT-009/010
//   🟢 护栏: INT-011（memory_commit/recall 已 clean，story 明示本 story 不动）
//
// RED 机制 = 运行时内容断言失败（prompt 已存在，可编译，非 t.Skip）。
// ============================================================

// --- 54.3-INT-009: [P0] AC3 memory_profile.txt 无设备路径 ---
// 🔴 RED: 当前 :18 "(use /dev/memory/commit instead)"。
func TestATDD_54_3_AC3_MemoryProfilePrompt_NoDevicePath(t *testing.T) {
	prompt := loadPrompt("memory_profile")
	if prompt == "" {
		t.Fatal("loadPrompt(\"memory_profile\") returned empty")
	}
	if strings.Contains(prompt, "/dev/") {
		t.Error("memory_profile.txt 仍含设备路径 \"/dev/\" (AC3: :18 /dev/memory/commit → MemoryCommit)")
	}
}

// --- 54.3-INT-010: [P1] AC3 memory_profile.txt 用 MemoryCommit 工具名 ---
// 🔴 RED: 当前无 "MemoryCommit"（用 /dev/memory/commit 设备路径，实测 grep -c = 0）。
func TestATDD_54_3_AC3_MemoryProfilePrompt_UsesMemoryCommit(t *testing.T) {
	prompt := loadPrompt("memory_profile")
	if !strings.Contains(prompt, "MemoryCommit") {
		t.Error("memory_profile.txt 未引用 \"MemoryCommit\" (AC3: :18 /dev/memory/commit → MemoryCommit，54.2 落定的 PascalCase 工具名)")
	}
}

// --- 54.3-INT-011: [P1] AC3/AC7 memory_commit / memory_recall prompt 保持 clean ---
// 🟢 护栏: 当前即绿（story 已核实这两个 prompt 无设备路径）。锁定「本 story 不动」，
// 防 dev 在去设备路径时误改这两个无关 prompt。
func TestATDD_54_3_AC3_MemoryCommitRecallPrompts_StayClean(t *testing.T) {
	for _, name := range []string{"memory_commit", "memory_recall"} {
		prompt := loadPrompt(name)
		if prompt == "" {
			t.Fatalf("loadPrompt(%q) returned empty", name)
		}
		if strings.Contains(prompt, "/dev/") {
			t.Errorf("%s.txt 含设备路径 \"/dev/\" (AC3/AC7: story 明示这两个 prompt 已 clean、本 story 不动——若此处红说明被误改)", name)
		}
	}
}
