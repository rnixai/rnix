package main

import "testing"

// ============================================================
// ATDD — Story 55.2: per-request reasoning_effort 外部入口（CLI flag 层）
// AC #1 (rnix run --reasoning-effort) + AC #2 (apply 路径) + AC #9 (零回归)
//
// 真 RED（t.Skip）：flagReasoningEffort 已声明为骨架 package var，但尚未
// 注册到 rootCmd.PersistentFlags()（装配逻辑未实现）→ Lookup 返回 nil →
// 移除 t.Skip 后测试 FAIL。dev-story 在 main.go:309-312 区注册 flag +
// main.go:576 / apply.go:76 构造点透传后转 GREEN。
//
// 同构模板：apply_test.go::TestRootCmd_FallbackFlags（provider/fallback flag
// 存在性断言）——"--provider 走到哪，--reasoning-effort 跟到哪"。
// ============================================================

// --- 55-2-CLI-001 [P0]: --reasoning-effort persistent flag 已注册且为 string (AC #1) ---

func TestEffortFlag_ReasoningEffort_Registered(t *testing.T) {
	t.Skip("RED: Story 55-2 未实现 — main.go 尚未将 flagReasoningEffort 注册到 rootCmd.PersistentFlags()")

	f := rootCmd.PersistentFlags().Lookup("reasoning-effort")
	if f == nil {
		t.Fatal("AC#1: expected --reasoning-effort persistent flag to be defined")
	}
	if f.Value.Type() != "string" {
		t.Errorf("AC#1: --reasoning-effort expected string flag, got %s", f.Value.Type())
	}
	// 默认值必须为空（AC #9 零回归：不传 = 回落 driver/provider 默认）
	if f.DefValue != "" {
		t.Errorf("AC#9: --reasoning-effort default must be empty, got %q", f.DefValue)
	}
}

// --- 55-2-CLI-002 [P1]: flag 对 apply 子命令可见（persistent 继承） (AC #2) ---

func TestEffortFlag_ReasoningEffort_VisibleToApply(t *testing.T) {
	t.Skip("RED: Story 55-2 未实现 — flag 未注册，apply 子命令无法继承 --reasoning-effort")

	// persistent flag 在 root 注册后所有子命令（含 apply）均可见。
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "apply" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("apply subcommand not registered (precondition)")
	}
	// apply 通过 InheritedFlags 看到 root persistent flag
	if rootCmd.PersistentFlags().Lookup("reasoning-effort") == nil {
		t.Fatal("AC#2: --reasoning-effort must be a root persistent flag visible to apply")
	}
}
