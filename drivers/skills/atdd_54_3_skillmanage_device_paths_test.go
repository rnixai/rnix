package skills

import (
	"strings"
	"testing"
)

// ============================================================
// ATDD — Story 54.3 AC9（code-review 补修；Decker 2026-06-10 裁定纳入 54-3）
//
// SkillManage 工具（/dev/skills/manage）有两层 LLM 可见文本含设备路径，是 epic 显式
// 枚举外、与 AC5 writeback 自我繁殖源同质的漏网点——SkillManage 是 LLM 主动【创建 skill】
// 的主入口，示例 "/dev/fs /dev/shell" 直接教 LLM 用设备路径填 allowed_tools（比 writeback
// 更直接的自我繁殖源）。两处：
//   ① driver.go:64 allowed_tools 参数 ToolDef.Description（Go 硬编码，经 toolgen 注入）：
//      "Space-separated VFS device paths ... (e.g., \"/dev/fs /dev/shell\")" → 工具名
//   ② prompts/skill_manage.txt:19 顶层 Description（loadPrompt 注入 system prompt）：
//      "Space-separated device paths" → "Space-separated tool names"
//
// 本文件反向接管 54.2 退役的 TestATDD_54_2_901（该 guard 曾断言 ① 含 /dev/「等 54.3 接管」）。
// 命名权威同 INT 系列：工具名取 54.2 落定的 PascalCase（Read/Write/Bash）。
//
// DevicePath "/dev/skills/manage" 路由锚点由 TestATDD_54_2_900 守护（AC7 类比），本文件不重复。
// atdd_35_5_device_test.go 的 "/dev/fs /dev/shell" 是 create/patch 数据流【测试输入值】（非
// description），保持原样不碰（类比 AC7 的 35.5 fixture tool 字段）。
// ============================================================

// atdd543SkillManageAllowedToolsDesc 取 SkillManage 工具 allowed_tools 参数的 description
// （driver.go:64，LLM 可见）。ToolDefs() 不依赖 manager，nil 构造。
func atdd543SkillManageAllowedToolsDesc(t *testing.T) string {
	t.Helper()
	defs := NewSkillManageDriver(nil).ToolDefs()
	if len(defs) == 0 {
		t.Fatal("ToolDefs() 返回空")
	}
	props, ok := defs[0].Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatal("缺少 properties")
	}
	at, ok := props["allowed_tools"].(map[string]any)
	if !ok {
		t.Fatal("缺少 allowed_tools 参数")
	}
	desc, _ := at["description"].(string)
	return desc
}

// --- AC9: allowed_tools 参数 description 无设备路径 / 去 VFS device 词汇 ---
func TestATDD_54_3_AC9_SkillManageAllowedToolsDesc_NoDevicePath(t *testing.T) {
	desc := atdd543SkillManageAllowedToolsDesc(t)
	if strings.Contains(desc, "/dev/") {
		t.Errorf("SkillManage allowed_tools 参数 description 仍含设备路径 \"/dev/\" (AC9: driver.go:64 /dev/fs /dev/shell → Read Write Bash)\n  desc: %s", desc)
	}
	if strings.Contains(desc, "VFS device") {
		t.Errorf("SkillManage allowed_tools 参数 description 仍含 \"VFS device\" 词汇 (AC9: 去设备词汇)\n  desc: %s", desc)
	}
}

// --- AC9: allowed_tools 参数 description 用语义工具名 ---
func TestATDD_54_3_AC9_SkillManageAllowedToolsDesc_UsesToolNames(t *testing.T) {
	desc := atdd543SkillManageAllowedToolsDesc(t)
	if !strings.Contains(desc, "Read") && !strings.Contains(desc, "Write") && !strings.Contains(desc, "Bash") {
		t.Errorf("SkillManage allowed_tools 参数 description 未用语义工具名 Read/Write/Bash (AC9: 示例应示范工具名而非设备路径)\n  desc: %s", desc)
	}
}

// --- AC9: skill_manage.txt 顶层 Description 无设备路径 + 去 "device path" 词汇 ---
func TestATDD_54_3_AC9_SkillManagePrompt_NoDevicePathWording(t *testing.T) {
	prompt := loadPrompt("skill_manage")
	if prompt == "" {
		t.Fatal("loadPrompt(\"skill_manage\") returned empty")
	}
	if strings.Contains(prompt, "/dev/") {
		t.Error("skill_manage.txt 仍含设备路径 \"/dev/\" (AC9)")
	}
	if strings.Contains(prompt, "device path") {
		t.Error("skill_manage.txt 仍含 \"device path\" 词汇 (AC9: :19 device paths → tool names)")
	}
}

// --- AC9: skill_manage.txt 用 "tool names" 词汇（正向）---
func TestATDD_54_3_AC9_SkillManagePrompt_UsesToolNamesWording(t *testing.T) {
	prompt := loadPrompt("skill_manage")
	if !strings.Contains(prompt, "tool names") {
		t.Error("skill_manage.txt 未用 \"tool names\" 词汇 (AC9: :19 device paths → tool names)")
	}
}
