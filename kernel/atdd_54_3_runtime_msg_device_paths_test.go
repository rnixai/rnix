package kernel

import (
	"os"
	"strings"
	"testing"
)

// ============================================================
// ATDD RED PHASE — Story 54.3: AC4.1（运行时消息 skill-already-loaded 去设备路径）
//
// kernel/tool_exec.go 的 specialize → skill-already-loaded 运行时消息（约 :976）
// 当前含设备路径列举 "(/dev/fs, /dev/shell, etc.)" + "VFS devices" 词汇——该消息
// 经 appendToolResult → CtxWrite → 下轮 BuildPrompt 直达 LLM。AC4.1 改为工具中立
// 表述（"your available tools"）。
//
// 载体：os.ReadFile("tool_exec.go") 逐行，**精确锁定含 "is already loaded" 的行**
// ——不可全文 grep /dev/（会误伤 :672 DeviceRegistry().Open("/dev/llm/") 路由锚点 +
// :385/:789 注释举例，均 AC7 明确保留）。注意源文件有两条 skill-already-loaded
// 消息（:976 含设备路径=改造目标；:995 race-check 后分支本就 clean=不动）；本断言
// 对两行同时生效（改造后 :976 也 clean → 全绿），天然不误伤 :995。
//
//   🔴 RED: INT-012/013
//   🟢 护栏: INT-014（/dev/llm/ 路由锚点未被「顺手」删，AC7）
//
// RED 机制 = 运行时断言失败（源文件已存在，可编译，非 t.Skip）。
// checked==0 兜底防 vacuous pass（文件重构后找不到目标行时显式失败，
// 参 skills/atdd_53_3_skill_body_naming_test.go 的 code-review 2026-06-09 Patch）。
// ============================================================

const atdd543ToolExecFile = "tool_exec.go"

// atdd543ReadToolExec 读取 kernel/tool_exec.go 源文件全文（测试 cwd = kernel/ 包目录）。
func atdd543ReadToolExec(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(atdd543ToolExecFile)
	if err != nil {
		t.Fatalf("read %s: %v", atdd543ToolExecFile, err)
	}
	return string(data)
}

// --- 54.3-INT-012: [P0] AC4.1 skill-already-loaded 消息无设备路径 ---
// 🔴 RED: 当前 :976 resultMsg 含 "(/dev/fs, /dev/shell, etc.)"。
func TestATDD_54_3_AC4_SkillAlreadyLoadedMsg_NoDevicePath(t *testing.T) {
	src := atdd543ReadToolExec(t)
	checked := 0
	for i, line := range strings.Split(src, "\n") {
		if !strings.Contains(line, "is already loaded") {
			continue
		}
		checked++
		for _, tok := range []string{"/dev/fs", "/dev/shell"} {
			if strings.Contains(line, tok) {
				t.Errorf("%s:%d skill-already-loaded 消息仍含设备路径 %q (AC4.1: 去 (/dev/fs, /dev/shell, etc.) 列举，改 \"your available tools\")\n  行: %s",
					atdd543ToolExecFile, i+1, tok, strings.TrimSpace(line))
			}
		}
	}
	if checked == 0 {
		t.Fatalf("%s 未找到含 \"is already loaded\" 的消息行——文件可能重构，请复核 AC4.1 定位", atdd543ToolExecFile)
	}
}

// --- 54.3-INT-013: [P1] AC4.1 skill-already-loaded 消息去 "VFS devices" 词汇 ---
// 🔴 RED: 当前 :976 含 "available VFS devices"。
func TestATDD_54_3_AC4_SkillAlreadyLoadedMsg_DropsVFSDevicesWording(t *testing.T) {
	src := atdd543ReadToolExec(t)
	checked := 0
	for i, line := range strings.Split(src, "\n") {
		if !strings.Contains(line, "is already loaded") {
			continue
		}
		checked++
		if strings.Contains(line, "VFS devices") {
			t.Errorf("%s:%d skill-already-loaded 消息仍含 \"VFS devices\" 词汇 (AC4.1: 改为工具中立表述)\n  行: %s",
				atdd543ToolExecFile, i+1, strings.TrimSpace(line))
		}
	}
	if checked == 0 {
		t.Fatalf("%s 未找到含 \"is already loaded\" 的消息行——文件可能重构", atdd543ToolExecFile)
	}
}

// --- 54.3-INT-014: [P1] AC7 /dev/llm/ 路由锚点保留 ---
// 🟢 护栏: 当前即绿。tool_exec.go:672 DeviceRegistry().Open("/dev/llm/"+provider)
// 是 Layer 1 内核路由锚点（Decision 45 保留），AC4.1 只改 LLM 可见消息文本、
// 不得「顺手」删路由。
func TestATDD_54_3_AC4_ToolExec_PreservesLLMRoutingAnchor(t *testing.T) {
	src := atdd543ReadToolExec(t)
	if !strings.Contains(src, "/dev/llm/") {
		t.Error("tool_exec.go 不再含 \"/dev/llm/\" 路由锚点 (AC7 红线: Layer 1 设备路由须保留，本 story 只改 LLM 可见消息文本)")
	}
}
