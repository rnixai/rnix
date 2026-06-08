package debug

import "testing"

// ATDD 红灯脚手架 — Story 53.2 / AC4：移除 list_dir，目录枚举能力并入 Glob。
//
// 本文件覆盖 AC4 在 **遥测分类层 debug** 的契约（测试 ID 53.2-UNIT-007），
// 即 story AC4 line 94 / Combination Matrix line 282 标记的 "最易漏" 点：
// ctx_profile.go:258 的 heatmap 小写分类键集合 knownTools 仍含 "list_dir"。
//
// TDD 生命周期: 最初以 t.Skip() 红灯脚手架生成（决策强制型），dev-story 阶段已移除 t.Skip
// 激活；当前已激活（无 t.Skip）。
//
// ⚠️ 决策注记（不可静默忽略）：
//   story 明确此处是决策点 —— "删（最可能）" 或 "按 heatmap 历史记录语义保留"。
//   "list_dir" 当前位于 ctx_profile.go 的 "当前 canonical 名" 组（254-258），
//   **非** legacy 历史兼容段（250-253）。本测试编码 story 主路径 = 删除该键。
//   若 dev 经核查派生逻辑后选择"保留"（把 list_dir 下移进 legacy 段以兼容历史
//   step 记录的 heatmap 分类），这是被 story 授权的有意识决策：届时应把本测试
//   反转为 GUARD（断言仍分类为 "list_dir"）并在此注明理由，**而非**静默删除。
//   本测试的价值在于强制 dev 触碰 ctx_profile 的 list_dir 派生逻辑，杜绝漏改。

// 53.2-UNIT-007 [RED] extractToolName 不再把内容分类为 "list_dir"。
//
//   - impl 前: knownTools 含 "list_dir" → 内容 "list_dir output..." 命中 → 返回 "list_dir" → 断言 != FAILS（RED）。
//   - impl 后（主路径=删键）: 该样本无其余 knownTool 子串命中 → 返回 "unknown" → PASS。
func TestATDD_53_2_UNIT_007_ExtractToolNameDropsListDir(t *testing.T) {
	// 模仿历史 step 记录里 list_dir 工具结果文本。
	msg := CtxMessage{Role: "tool", Content: "list_dir output: foo bar"}
	got := extractToolName(msg)
	if got == "list_dir" {
		t.Errorf("list_dir 工具既已移除，extractToolName 不应再分类为 %q"+
			"（ctx_profile.go:258 knownTools 应去掉 \"list_dir\"；若按历史兼容保留，见文件头决策注记反转本测试）", got)
	}
}
