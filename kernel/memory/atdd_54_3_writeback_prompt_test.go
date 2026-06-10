package memory

import (
	"strings"
	"testing"
)

// ============================================================
// ATDD RED PHASE — Story 54.3: AC5（writeback 自我繁殖源消除）
//
// kernel/memory/prompts/writeback_skill_suggest.txt 是 writeback 子系统让 LLM
// 生成 SKILL.md 的提示词——当前 :10 示范输出「设备路径 allowed_tools」
// ("/dev/fs /dev/shell")，:16 用 "devices" 词汇。这是 Decision 45 点名的
// 「自我繁殖源」（生成的 skill 又把设备路径散播出去）。AC5 改为语义工具名 +
// "tools" 词汇。前置依赖（story AC5 记录）：54.1 validateFrontmatter 已接受工具名
// → 生成 "Read Write Bash" 形式 skill 加载时不被拒。
//
// 载体：包内私有 loadPromptTemplate("writeback_skill_suggest.txt")
// ⚠️ 注意：kernel/memory 的 loadPromptTemplate **不补 .txt，须传全名**
// （ReadFile("prompts/"+name)，与 drivers/* 的 loadPrompt 补 .txt 不同；
//  参 LoadRecallSummarizePrompt 用 "recall_summarize.txt"）。
//
//   🔴 RED: INT-021/022/023
//
// RED 机制 = 运行时内容断言失败（prompt 已存在，可编译，非 t.Skip）。
// ============================================================

const atdd543WritebackPrompt = "writeback_skill_suggest.txt"

// --- 54.3-INT-021: [P0] AC5 writeback prompt 无设备路径 ---
// 🔴 RED: 当前 :10 "/dev/fs /dev/shell"。
func TestATDD_54_3_AC5_WritebackPrompt_NoDevicePath(t *testing.T) {
	prompt := loadPromptTemplate(atdd543WritebackPrompt)
	if prompt == "" {
		t.Fatalf("loadPromptTemplate(%q) returned empty — 注意 kernel/memory 不补 .txt，须传全名", atdd543WritebackPrompt)
	}
	if strings.Contains(prompt, "/dev/") {
		t.Error("writeback_skill_suggest.txt 仍含设备路径 \"/dev/\" (AC5: :10 allowed_tools 示例 → 语义工具名)")
	}
}

// --- 54.3-INT-022: [P0] AC5 allowed_tools 示例行不含设备路径前缀 / 用语义工具名 ---
// 🔴 RED: 当前 :10 `"allowed_tools": "/dev/fs /dev/shell"`（含 "/"）。
// 逐行锁定含 "allowed_tools" 的行，断言不含 "/"（设备路径前缀）；并正向验证
// prompt 用语义工具名（Read/Write/Bash 任一，story 示例 "Read Write Bash"）。
func TestATDD_54_3_AC5_WritebackAllowedToolsExample_NoSlash(t *testing.T) {
	prompt := loadPromptTemplate(atdd543WritebackPrompt)
	if prompt == "" {
		t.Fatalf("loadPromptTemplate(%q) returned empty", atdd543WritebackPrompt)
	}
	checked := 0
	for i, line := range strings.Split(prompt, "\n") {
		if !strings.Contains(line, "allowed_tools") {
			continue
		}
		checked++
		if strings.Contains(line, "/") {
			t.Errorf("writeback prompt :%d allowed_tools 行仍含 \"/\"（设备路径前缀）(AC5: 示例 \"/dev/fs /dev/shell\" → 语义工具名如 \"Read Write Bash\")\n  行: %s",
				i+1, strings.TrimSpace(line))
		}
	}
	if checked == 0 {
		t.Fatal("writeback prompt 未找到含 \"allowed_tools\" 的行——文件可能重构，请复核 AC5 定位")
	}
	// 正向：示例用了语义工具名（该 prompt 工作流场景 = read file / run command / write file）
	if !strings.Contains(prompt, "Read") && !strings.Contains(prompt, "Write") && !strings.Contains(prompt, "Bash") {
		t.Error("writeback prompt 未用任何语义工具名 Read/Write/Bash (AC5: allowed_tools 示例应示范语义工具名而非设备路径)")
	}
}

// --- 54.3-INT-023: [P1] AC5 guideline 用 "tools" 而非 "devices" ---
// 🔴 RED: 当前 :16 "allowed_tools should only include devices actually used"。
func TestATDD_54_3_AC5_WritebackGuideline_UsesToolsNotDevices(t *testing.T) {
	prompt := loadPromptTemplate(atdd543WritebackPrompt)
	if strings.Contains(prompt, "include devices") {
		t.Error("writeback prompt guideline 仍含 \"include devices\" (AC5: :16 \"include devices\" → \"include tools\"，去设备词汇)")
	}
}
