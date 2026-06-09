package skills

import "testing"

// ATDD Story 54.1 / AC3 — validateFrontmatter 接受语义工具名（兼容期仍接受设备路径）。
//
// RED 形态：骨架 + t.Skip（与 kernel/atdd_54_1_*.go 一致，用户 Decker 2026-06-09 拍板）。
// validateFrontmatter 签名已存在（manager.go:136），无需骨架——RED 用例直接调现有函数
// 断言新行为：当前实现要求 allowed-tools 以 /dev/ 或 /mnt/mcp/ 前缀，故工具名被拒，
// 移除 t.Skip 后失败 = RED；dev-story 落地「接受已知工具名 + 兼容设备路径」后转绿。
//
// 本 story 不改任何内置 skill 的 frontmatter（那是 54.5），仅改 validateFrontmatter 能力。

// 54.1-UNIT-005 [RED] AC3：validateFrontmatter 接受语义工具名（Read / Write / Bash / Glob）。
func TestATDD_54_1_ValidateFrontmatter_AcceptsToolNames(t *testing.T) {
	accepted := []string{"Read", "Write", "Edit", "Glob", "Grep", "Bash"}
	for _, tool := range accepted {
		t.Run(tool, func(t *testing.T) {
			if err := validateFrontmatter("test-skill", "a test skill", tool); err != nil {
				t.Errorf("AC3: validateFrontmatter(_, _, %q) = %v, want nil（应接受语义工具名）", tool, err)
			}
		})
	}
}

// 54.1-UNIT-006 GREEN 护栏 AC3：兼容期仍接受设备路径；既非工具名又非合法设备路径的值仍拒。
// 当前即应通过——守护「接受工具名」改造不破坏设备路径兼容（向后兼容），也不把校验放开成全接受。
func TestATDD_54_1_ValidateFrontmatter_GreenGuard(t *testing.T) {
	t.Run("accepts_device_paths_compat", func(t *testing.T) {
		for _, dev := range []string{"/dev/fs", "/dev/shell", "/mnt/mcp/1-server/tools/screenshot"} {
			if err := validateFrontmatter("test-skill", "a test skill", dev); err != nil {
				t.Errorf("AC3 兼容: validateFrontmatter(_, _, %q) = %v, want nil（兼容期保持接受设备路径）", dev, err)
			}
		}
	})
	t.Run("rejects_bogus_value", func(t *testing.T) {
		// "Bogus" 既非已知工具名、又非合法设备路径 → 必须拒（防止改造把校验放开成全接受）。
		if err := validateFrontmatter("test-skill", "a test skill", "Bogus"); err == nil {
			t.Error(`AC3: validateFrontmatter(_, _, "Bogus") = nil, want error（既非工具名又非设备路径须拒）`)
		}
	})
}
