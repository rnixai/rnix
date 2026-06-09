package memory

import (
	"testing"

	"github.com/rnixai/rnix/vfs"
)

// ATDD Story 54.2 — memory 3 工具 ToolDef.Name 改 PascalCase（AC1）+ FileStat.Name 同步消除
// snake_case 残留（AC1 / 决策点 D2）。RED 断言新名（改名前运行时失败），t.Skip 标记；
// green-guard（不 skip、立即绿）守护 DevicePath（设备路由锚点）与 FileStat.Name 分离、本 story 不变。
// 三个 driver 的 ToolDefs()/File.Stat() 均不依赖 store/searcher，故测试用 nil 构造隔离。

// --- AC1：memory 3 工具呈现名改 PascalCase（RED 脚手架）---

// TestATDD_54_2_200 断言 memory_commit/recall/profile 的 ToolDef.Name 改为 PascalCase。
func TestATDD_54_2_200_MemoryTools_PascalCaseNames(t *testing.T) {
	t.Skip("RED: 待 54.2 实现——drivers/memory/{driver.go:29,dev_recall.go:35,dev_profile.go:27}")

	cases := []struct {
		toolDefs func() []vfs.ToolDef
		want     string
		old      string
	}{
		{NewDriver(nil).ToolDefs, "MemoryCommit", "memory_commit"},
		{NewRecallDriver(nil, nil).ToolDefs, "MemoryRecall", "memory_recall"},
		{NewProfileDriver(nil).ToolDefs, "MemoryProfile", "memory_profile"},
	}
	for _, c := range cases {
		defs := c.toolDefs()
		if len(defs) != 1 {
			t.Fatalf("%s: 期望 1 个 tool def，实际 %d", c.want, len(defs))
		}
		if defs[0].Name != c.want {
			t.Errorf("ToolDef.Name = %q, want %q（旧名 %q）", defs[0].Name, c.want, c.old)
		}
	}
}

// TestATDD_54_2_210 断言 FileStat.Name 同步 PascalCase 以消除 snake_case 残留（AC1 / D2）。
// 注意：FileStat.Name 当前是工具名字符串（memory_commit 等），与 DevicePath 字段分离、无功能耦合。
func TestATDD_54_2_210_MemoryFileStat_PascalCaseNames(t *testing.T) {
	t.Skip("RED: 待 54.2 实现（决策点 D2：推荐一并改名；若 dev 严格限定 ToolDef.Name 则调整本测试）")

	cases := []struct {
		factory vfs.VFSFileFactory
		want    string
	}{
		{FileFactory(NewDriver(nil)), "MemoryCommit"},
		{RecallFileFactory(NewRecallDriver(nil, nil)), "MemoryRecall"},
		{ProfileFileFactory(NewProfileDriver(nil)), "MemoryProfile"},
	}
	for _, c := range cases {
		f, err := c.factory("", vfs.O_RDWR, "")
		if err != nil {
			t.Fatalf("factory(%q): %v", c.want, err)
		}
		stat, err := f.Stat()
		if err != nil {
			t.Fatalf("%s Stat: %v", c.want, err)
		}
		if stat.Name != c.want {
			t.Errorf("FileStat.Name = %q, want %q", stat.Name, c.want)
		}
	}
}

// --- green-guard（不 skip、立即绿）---

// TestATDD_54_2_900 守护 DevicePath（设备路由锚点，AC5）不变——它与 FileStat.Name 是不同字段，
// 本 story 改 Name 时不得波及 DevicePath。
func TestATDD_54_2_900_GreenGuard_MemoryDevicePathsUnchanged(t *testing.T) {
	cases := []struct {
		factory  vfs.VFSFileFactory
		wantPath string
	}{
		{FileFactory(NewDriver(nil)), "/dev/memory/commit"},
		{RecallFileFactory(NewRecallDriver(nil, nil)), "/dev/memory/recall"},
		{ProfileFileFactory(NewProfileDriver(nil)), "/dev/memory/profile"},
	}
	for _, c := range cases {
		f, err := c.factory("", vfs.O_RDWR, "")
		if err != nil {
			t.Fatalf("factory(%q): %v", c.wantPath, err)
		}
		stat, err := f.Stat()
		if err != nil {
			t.Fatalf("%s Stat: %v", c.wantPath, err)
		}
		if stat.DevicePath != c.wantPath {
			t.Errorf("DevicePath = %q, want %q（设备路由不可随改名变）", stat.DevicePath, c.wantPath)
		}
	}
}
