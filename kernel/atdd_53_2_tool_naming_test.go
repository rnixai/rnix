package kernel

import (
	"testing"

	"github.com/rnixai/rnix/vfs"
)

// ATDD 红灯脚手架 — Story 53.2 / AC4：移除 list_dir，目录枚举能力并入 Glob。
//
// 本文件覆盖 AC4 在 **Layer 2 路由层 kernel** 的契约（测试 ID 53.2-UNIT-006）。
// 复用 toolgen_test.go 同包已有的 mockToolDescriptor。
//
// TDD 阶段: RED —— t.Skip() 标记未激活；dev-story 移除 t.Skip 行激活验证 RED→GREEN。

// 53.2-UNIT-006 [RED] buildToolDefs 不再把 list_dir 标记为 FSOperation。
//
// 这是对 toolgen.go:46 FSOperation switch 的隔离单测：用 mock 驱动呈现一个名为
// "list_dir" 的工具，验证 kernel 路由是否还会把它当作 /dev/fs 文件操作来标记。
//   - impl 前: switch case 含 "list_dir" → toolMap["list_dir"].FSOperation == "list_dir" → 断言 == "" FAILS（RED）。
//   - impl 后: switch 去掉 "list_dir" → list_dir 仍作为通用 vfs 工具入 toolMap，但 FSOperation == "" → PASS。
//
// 注：真实 /dev/fs 驱动在 AC4 后将完全不再发射 list_dir ToolDef（由
// drivers/fs 的 53.2-UNIT-003 覆盖）；本测试专注 kernel switch 这一行的逻辑。
func TestATDD_53_2_UNIT_006_BuildToolDefsDoesNotTagListDirAsFSOperation(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	factory := func(subpath string, flags vfs.OpenFlag, workDir string) (vfs.VFSFile, error) {
		return nil, nil
	}
	fsDriver := &mockToolDescriptor{defs: []vfs.ToolDef{
		{Name: "Read", Description: "Read"},
		{Name: "Glob", Description: "Glob"},
		{Name: "list_dir", Description: "List"},
	}}
	_ = reg.RegisterWithDriver("/dev/fs", factory, fsDriver)

	_, toolMap := buildToolDefs(reg, nil)

	m, ok := toolMap["list_dir"]
	if !ok {
		t.Fatalf("list_dir 应仍作为通用 vfs 工具存在于 toolMap（移除的是 FSOperation 标记，非 toolMap 条目本身）")
	}
	if m.FSOperation != "" {
		t.Errorf("list_dir 不应被标记为 FSOperation，got %q want \"\"（toolgen.go:46 switch 应去掉 \"list_dir\" case）", m.FSOperation)
	}

	// 反向 sanity：真正 anchored 的 fs 工具仍被正确标记，证明本测试不是误判。
	if toolMap["Read"].FSOperation != "Read" {
		t.Errorf("Read 仍应标记为 FSOperation Read，got %q", toolMap["Read"].FSOperation)
	}
	if toolMap["Glob"].FSOperation != "Glob" {
		t.Errorf("Glob 仍应标记为 FSOperation Glob，got %q", toolMap["Glob"].FSOperation)
	}
}
