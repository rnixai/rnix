package fs

import (
	"strings"
	"testing"
)

// ============================================================
// ATDD RED PHASE — Story 54.3: AC2
//
// drivers/fs/prompts/Grep.txt:4 与 Read.txt:10 的 /dev/shell → 工具名 Bash。
// 载体：包内私有 loadPrompt("Grep"/"Read")（//go:embed prompts/*.txt，补 .txt）。
// RED 机制 = 运行时内容断言失败（prompt 已存在，可编译，非 t.Skip）。
//
//   🔴 RED: INT-005/006/007/008
// （本 AC 无护栏——Grep/Read 仅此一处 /dev/shell，无真实 Unix 路径混入。）
//
// 设计说明：不用 t.Skip（项目 ATDD 惯例，见 skills/atdd_53_3_skill_body_naming_test.go）。
// ============================================================

// --- 54.3-INT-005: [P0] AC2 Grep.txt 无 /dev/shell ---
// 🔴 RED: 当前 :4 "NEVER invoke `grep` or `rg` as a /dev/shell command"。
func TestATDD_54_3_AC2_GrepPrompt_NoShellDevicePath(t *testing.T) {
	prompt := loadPrompt("Grep")
	if prompt == "" {
		t.Fatal("loadPrompt(\"Grep\") returned empty")
	}
	if strings.Contains(prompt, "/dev/shell") {
		t.Error("Grep.txt 仍含 \"/dev/shell\" (AC2: :4 as a /dev/shell command → via Bash)")
	}
}

// --- 54.3-INT-006: [P1] AC2 Grep.txt 用 Bash ---
// 🔴 RED: 当前 Grep.txt 无 "Bash"（实测 grep -c = 0）。
func TestATDD_54_3_AC2_GrepPrompt_UsesBash(t *testing.T) {
	prompt := loadPrompt("Grep")
	if !strings.Contains(prompt, "Bash") {
		t.Error("Grep.txt 未引用 \"Bash\" (AC2: /dev/shell 命令 → via Bash)")
	}
}

// --- 54.3-INT-007: [P0] AC2 Read.txt 无 /dev/shell ---
// 🔴 RED: 当前 :10 "ls via /dev/shell"。
func TestATDD_54_3_AC2_ReadPrompt_NoShellDevicePath(t *testing.T) {
	prompt := loadPrompt("Read")
	if prompt == "" {
		t.Fatal("loadPrompt(\"Read\") returned empty")
	}
	if strings.Contains(prompt, "/dev/shell") {
		t.Error("Read.txt 仍含 \"/dev/shell\" (AC2: :10 ls via /dev/shell → ls via Bash)")
	}
}

// --- 54.3-INT-008: [P1] AC2 Read.txt 用 Bash ---
// 🔴 RED: 当前 Read.txt 无 "Bash"（实测 grep -c = 0）。
func TestATDD_54_3_AC2_ReadPrompt_UsesBash(t *testing.T) {
	prompt := loadPrompt("Read")
	if !strings.Contains(prompt, "Bash") {
		t.Error("Read.txt 未引用 \"Bash\" (AC2: ls via /dev/shell → ls via Bash)")
	}
}
