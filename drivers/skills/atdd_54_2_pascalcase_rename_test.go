package skills

import (
	"strings"
	"testing"

	"github.com/rnixai/rnix/vfs"
)

// ATDD Story 54.2 — skill_manage 工具 ToolDef.Name 改 PascalCase（AC1）+ FileStat.Name 同步
// （AC1 / 决策点 D2）。RED 断言新名（改名前运行时失败），t.Skip 标记；green-guard（不 skip、
// 立即绿）守护 DevicePath 不变，且 allowed_tools 参数描述内的【设备路径示例】保持原样——
// 设备路径改写归 54.3/54.5，非本 story（工具名）范围。ToolDefs()/Stat() 不依赖 manager，nil 构造。

// --- AC1：skill_manage 呈现名改 PascalCase（RED 脚手架）---

// TestATDD_54_2_300 断言 skill_manage 的 ToolDef.Name 改为 SkillManage。
func TestATDD_54_2_300_SkillManage_PascalCaseName(t *testing.T) {
	t.Skip("RED: 待 54.2 实现——drivers/skills/driver.go:38 Name 改 SkillManage")

	defs := NewSkillManageDriver(nil).ToolDefs()
	if len(defs) != 1 {
		t.Fatalf("期望 1 个 tool def，实际 %d", len(defs))
	}
	if defs[0].Name != "SkillManage" {
		t.Errorf("ToolDef.Name = %q, want %q（旧名 skill_manage）", defs[0].Name, "SkillManage")
	}
}

// TestATDD_54_2_310 断言 FileStat.Name 同步 PascalCase（AC1 / D2，drivers/skills/file.go:74）。
func TestATDD_54_2_310_SkillManageFileStat_PascalCaseName(t *testing.T) {
	t.Skip("RED: 待 54.2 实现（决策点 D2：FileStat.Name 一并改名消除残留）")

	f, err := SkillManageFileFactory(NewSkillManageDriver(nil))("", vfs.O_RDWR, "")
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	stat, err := f.Stat()
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if stat.Name != "SkillManage" {
		t.Errorf("FileStat.Name = %q, want %q", stat.Name, "SkillManage")
	}
}

// --- green-guard（不 skip、立即绿）---

// TestATDD_54_2_900 守护 DevicePath（设备路由锚点）不变。
func TestATDD_54_2_900_GreenGuard_SkillDevicePathUnchanged(t *testing.T) {
	f, err := SkillManageFileFactory(NewSkillManageDriver(nil))("", vfs.O_RDWR, "")
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	stat, err := f.Stat()
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if stat.DevicePath != "/dev/skills/manage" {
		t.Errorf("DevicePath = %q, want %q", stat.DevicePath, "/dev/skills/manage")
	}
}

// TestATDD_54_2_901 守护 allowed_tools 参数描述内的设备路径示例（"/dev/fs /dev/shell"）保持原样。
// 那是【设备路径】问题，归 54.3/54.5，本 story（改工具名）不得顺手删改——guard 确认未被波及。
func TestATDD_54_2_901_GreenGuard_AllowedToolsParamUntouched(t *testing.T) {
	defs := NewSkillManageDriver(nil).ToolDefs()
	props, ok := defs[0].Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatal("缺少 properties")
	}
	at, ok := props["allowed_tools"].(map[string]any)
	if !ok {
		t.Fatal("缺少 allowed_tools 参数")
	}
	desc, _ := at["description"].(string)
	if !strings.Contains(desc, "/dev/") {
		t.Errorf("allowed_tools 描述应保留设备路径示例（设备路径改写归 54.3/54.5），got %q", desc)
	}
}
