package shell

import (
	"strings"
	"testing"
)

// ============================================================
// ATDD RED PHASE — Story 54.3: system prompt 与运行时消息去设备路径
//
// 本文件覆盖 AC1：drivers/shell/prompts/Bash.txt 的 7 处 /dev/shell（Rnix
// shell 设备自引用，:5-11）→ 工具名 Bash；同时锁定真实 POSIX 设备路径
// (/dev/null、dd of=/dev/) 零误伤——这是 story 标注的「本 story 最高风险点」。
//
// 载体：包内私有 loadPrompt("Bash")（//go:embed prompts/*.txt，读编译进二进制、
// 注入 LLM system prompt 的真实内容）。
//
// RED 机制：被测 prompt 文件已存在 → 测试可编译 → 红灯为运行时内容断言失败
// （非编译失败、非 t.Skip）。依据 story Testing standards + 同构范例
// skills/atdd_53_3_skill_body_naming_test.go。
//
//   🔴 RED  (实现前失败，驱动开发): INT-001/002/004
//   🟢 护栏 (实现前已通过，锁定不变契约): INT-003
//
// RED → GREEN（dev-story 实现后转绿）：
//   - Bash.txt :5-11 的 /dev/shell → Bash；:5 "dedicated VFS device operation" /
//     "dedicated operation" 措辞 → "dedicated tool"。
//   - :36/:37 /dev/null、:42 dd of=/dev/ 保持原样（INT-003 护栏拦宽匹配误伤）。
//
// 设计说明（不用 t.Skip）：本项目 ATDD 惯例为直接生效的运行时断言；护栏须始终
// 运行以实时拦截「s#/dev/#…# 宽匹配误伤真实 Unix 路径」红线。所有断言针对真实
// 预期行为，无占位断言。
// ============================================================

// atdd543RnixShellDevice 是 Bash.txt 中指代「Rnix shell 设备 / 本工具自身」的
// 设备路径——AC1 要全部换成工具名 Bash。
const atdd543RnixShellDevice = "/dev/shell"

// --- 54.3-INT-001: [P0] AC1 Bash.txt 无 Rnix /dev/shell 自引用 ---
// 🔴 RED: 当前 :5-11 共 7 处 /dev/shell。
func TestATDD_54_3_AC1_BashPrompt_NoShellDevicePath(t *testing.T) {
	prompt := loadPrompt("Bash")
	if prompt == "" {
		t.Fatal("loadPrompt(\"Bash\") returned empty — embed 路径或文件名错误")
	}
	if strings.Contains(prompt, atdd543RnixShellDevice) {
		t.Errorf("Bash.txt 仍含 %q (AC1: :5-11 的 7 处 Rnix shell 自引用须改为工具名 Bash)", atdd543RnixShellDevice)
	}
}

// --- 54.3-INT-002: [P1] AC1 Bash.txt 用 Bash 工具名（/dev/shell→Bash 正向验证）---
// 🔴 RED: 当前 Bash.txt 通篇零 "Bash" 字样（全用 /dev/shell 自指）。
func TestATDD_54_3_AC1_BashPrompt_UsesBashToolName(t *testing.T) {
	prompt := loadPrompt("Bash")
	if !strings.Contains(prompt, "Bash") {
		t.Error("Bash.txt 未引用工具名 \"Bash\" (AC1: :5-11 各 (NOT … via /dev/shell) → (NOT … via Bash))")
	}
}

// --- 54.3-INT-003: [P0] AC1/AC7 真实 POSIX 设备路径 / shell 语法零误伤（最高风险护栏）---
// 🟢 护栏: 当前即绿。/dev/null、dd of=/dev/ 是真实 Unix 设备路径 / 危险命令示例
// （非 Rnix 设备），dev 用 s#/dev/#…# 宽匹配会误伤——本护栏实时拦截。
func TestATDD_54_3_AC1_BashPrompt_PreservesRealUnixPaths(t *testing.T) {
	prompt := loadPrompt("Bash")
	// :36/:37 后台进程脱离 I/O 的标准写法 nohup/setsid CMD </dev/null >/dev/null
	if !strings.Contains(prompt, "</dev/null") {
		t.Error("Bash.txt 不再含 \"</dev/null\" (AC1/AC7 红线: :36/:37 真实 POSIX 空设备须保留，禁止 /dev/ 宽匹配误伤)")
	}
	// :42 危险命令示例 dd of=/dev/（写裸块设备）
	if !strings.Contains(prompt, "dd of=/dev/") {
		t.Error("Bash.txt 不再含 \"dd of=/dev/\" (AC1/AC7 红线: :42 危险命令示例须保留)")
	}
}

// --- 54.3-INT-004: [P2] AC1 去 "VFS device" 措辞 ---
// 🔴 RED: 当前 :5 含 "dedicated VFS device operation"。story AC1 建议改为
// "dedicated tool"（与 Claude Code Bash.txt 基线一致，去设备词汇）。
func TestATDD_54_3_AC1_BashPrompt_DropsVFSDeviceWording(t *testing.T) {
	prompt := loadPrompt("Bash")
	if strings.Contains(prompt, "VFS device") {
		t.Error("Bash.txt 仍含 \"VFS device\" 措辞 (AC1: :5 \"dedicated VFS device operation\" → \"dedicated tool\"，去设备词汇)")
	}
}
