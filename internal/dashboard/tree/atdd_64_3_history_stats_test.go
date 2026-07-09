// ATDD scaffolds for Story 64.3 — History 统计与状态徽章口径修正（案卷 R2 修复）。
//
// 纯 vfs.ProcInfo fixture + 包内直调（不 import kernel、不起 daemon），对齐
// atdd_56_6_synthetic_tree_test.go 先例。
//
// 红灯机制（记忆 atdd-code-story-red-mechanism-preference · Decker 偏好「骨架 + t.Skip」）：
//   阶段 A（ATDD 提交期）：history.go 签名加 dedupedTotal 参数但保旧逻辑（骨架）+
//     helpers.go effectiveResult 恒等 stub → make all 绿；RED 用例先 t.Skip。
//   阶段 B（dev 移 skip）：RED 用例在骨架（旧逻辑）上实跑必 FAIL（zombie 计 Running /
//     dead+interrupted 计 Failed / 无截断标注 / avg 含 interrupted / render 走失败分支）。
//   阶段 C（GREEN）：四段路由 + 截断标注 + effectiveResult fallback → RED 全转绿。
//
// 🟢 GREEN-guard（不 skip，骨架旧逻辑下即应绿，防 GREEN 阶段过度改）：
//   - ZombieOriginRealExitReasonStaysInBucket（D1 不扩面：dead+真实 ExitReason 仍按 exit code 归桶）
//   - TruncationNoteHiddenBelowCap（< cap / 0 不标注）

package tree

import (
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// interruptedDead 造一个 Story 64.1 归一化产物形态的条目：
// created/running 快照被 daemon 重启杀死，落盘时未退出 → dead + ExitReason=="interrupted" +
// Result="" + ExitCodeSet=false + DeadAt=mtime 近似值（非零）。
func interruptedDead(tokens int) vfs.ProcInfo {
	now := time.Now()
	return vfs.ProcInfo{
		State: types.StateDead, ExitReason: "interrupted", Result: "", ExitCodeSet: false,
		DeadAt: now, CreatedAt: now.Add(-2 * time.Second), TokensUsed: tokens,
	}
}

// zombieResidue 造运行期待 Wait 回收的短暂 Zombie（经 ListAllProcs active 段进入统计）。
func zombieResidue(tokens int) vfs.ProcInfo {
	return vfs.ProcInfo{State: types.StateZombie, TokensUsed: tokens}
}

// renderTree643 builds + flattens + renders a single-process tree（仿 renderTree566）。
// 用于 render 行级 fallback 断言：interrupted 条目的 badge / REASON 列不走失败分支。
func renderTree643(procs []vfs.ProcInfo) string {
	roots := BuildProcessTree(procs, 1, true)
	rows := FlattenTree(roots)
	state := TreeState{Rows: rows}
	ctx := RenderContext{Now: time.Now()}
	return Render(state, ctx, 80, 24)
}

// ---------------------------------------------------------------------------
// 🔴 64-3-UNIT-001 (AC1): StateZombie 归 Interrupted 段，不再计入 Running。
// RED: 旧逻辑 zombie → running++（history.go:83-84 "count zombie as still active"）。
// ---------------------------------------------------------------------------
func TestATDD_64_3_UNIT_ZombieCountsAsInterruptedNotRunning(t *testing.T) {
	out := RenderHistoryStats([]vfs.ProcInfo{zombieResidue(10)}, 0)
	if !strings.Contains(out, "Running: 0") {
		t.Errorf("zombie 不应计入 Running（应为 0），got %q", out)
	}
	if !strings.Contains(out, "Interrupted: 1") {
		t.Errorf("zombie 应归 Interrupted: 1，got %q", out)
	}
	if !strings.Contains(out, "⏸") {
		t.Errorf("Interrupted 段应含 ⏸ 符号，got %q", out)
	}
}

// ---------------------------------------------------------------------------
// 🔴 64-3-UNIT-002 (AC1): dead + ExitReason=="interrupted" 归 Interrupted 段，不计 Failed。
// RED: 旧逻辑 dead → IsProcessFailed("")→true → failed++（案卷 R2 污染成功率的根因）。
// ---------------------------------------------------------------------------
func TestATDD_64_3_UNIT_DeadInterruptedCountsAsInterruptedNotFailed(t *testing.T) {
	out := RenderHistoryStats([]vfs.ProcInfo{interruptedDead(50)}, 0)
	if !strings.Contains(out, "Interrupted: 1") {
		t.Errorf("dead+interrupted 应归 Interrupted: 1，got %q", out)
	}
	if !strings.Contains(out, "Failed: 0") {
		t.Errorf("dead+interrupted 不应计入 Failed（应为 0），got %q", out)
	}
	if !strings.Contains(out, "Running: 0") {
		t.Errorf("dead+interrupted 不应计入 Running，got %q", out)
	}
}

// ---------------------------------------------------------------------------
// 🟢 64-3-UNIT-003 (AC1 · GREEN-guard 不 skip · D1 不扩面): zombie 起源快照经 64.1
// 归一化保留真实 ExitReason（killed/completed）+ ExitCodeSet=true → 按 exit code 归
// done/failed，**不进** Interrupted 段。旧逻辑与新逻辑均按 IsProcessFailed 权威路径归桶。
// ---------------------------------------------------------------------------
func TestATDD_64_3_UNIT_ZombieOriginRealExitReasonStaysInBucket(t *testing.T) {
	now := time.Now()
	procs := []vfs.ProcInfo{
		// 真实 ExitReason（非 interrupted）+ ExitCodeSet=true + exit 0 → Done
		{State: types.StateDead, ExitReason: "completed", ExitCodeSet: true, ExitCode: 0,
			DeadAt: now, CreatedAt: now.Add(-1 * time.Second)},
		// 真实 ExitReason + exit≠0 → Failed
		{State: types.StateDead, ExitReason: "killed", ExitCodeSet: true, ExitCode: 137,
			DeadAt: now, CreatedAt: now.Add(-1 * time.Second)},
	}
	out := RenderHistoryStats(procs, 0)
	if !strings.Contains(out, "Done: 1") {
		t.Errorf("dead+completed+exit0 应归 Done: 1（不扩面进 Interrupted），got %q", out)
	}
	if !strings.Contains(out, "Failed: 1") {
		t.Errorf("dead+killed+exit137 应归 Failed: 1（不扩面进 Interrupted），got %q", out)
	}
}

// ---------------------------------------------------------------------------
// 🔴 64-3-UNIT-004 (AC2): dedupedTotal >= historyRingCap(1000) → 行尾含 "1000+" 截断标注。
// RED: 骨架忽略 dedupedTotal → 从不输出 "1000+"。
// ---------------------------------------------------------------------------
func TestATDD_64_3_UNIT_TruncationNoteShownWhenRingFull(t *testing.T) {
	out := RenderHistoryStats([]vfs.ProcInfo{zombieResidue(0)}, 1000)
	if !strings.Contains(out, "1000+") {
		t.Errorf("dedupedTotal>=1000 应含截断标注 '1000+'，got %q", out)
	}
}

// ---------------------------------------------------------------------------
// 🟢 64-3-UNIT-005 (AC2 · GREEN-guard 不 skip · 正向断言防空转): dedupedTotal < cap 与 0
// 时无截断标注。骨架与 GREEN 均不应输出 "1000+"（防 GREEN 阶段误标）。
// ---------------------------------------------------------------------------
func TestATDD_64_3_UNIT_TruncationNoteHiddenBelowCap(t *testing.T) {
	for _, total := range []int{0, 1, 999} {
		out := RenderHistoryStats([]vfs.ProcInfo{zombieResidue(0)}, total)
		if strings.Contains(out, "1000+") {
			t.Errorf("dedupedTotal=%d (<cap) 不应含截断标注，got %q", total, out)
		}
	}
}

// ---------------------------------------------------------------------------
// 🔴 64-3-UNIT-006 (AC3 端到端): 零活跃 + 若干 zombie + 若干 dead+interrupted →
// Running: 0 且 Interrupted 计数 = zombie 数 + interrupted 数。
// RED: 旧逻辑 zombie→Running + dead+interrupted→Failed → Running≠0。
// ---------------------------------------------------------------------------
func TestATDD_64_3_UNIT_ZeroActiveWithMixedResidue(t *testing.T) {
	procs := []vfs.ProcInfo{
		zombieResidue(1), zombieResidue(2), zombieResidue(3),
		interruptedDead(4), interruptedDead(5),
	}
	out := RenderHistoryStats(procs, 0)
	if !strings.Contains(out, "Running: 0") {
		t.Errorf("零活跃进程时 Running 应为 0，got %q", out)
	}
	if !strings.Contains(out, "Interrupted: 5") {
		t.Errorf("3 zombie + 2 dead+interrupted 应归 Interrupted: 5，got %q", out)
	}
	if !strings.Contains(out, "Failed: 0") {
		t.Errorf("残留条目不应污染 Failed（应为 0），got %q", out)
	}
}

// ---------------------------------------------------------------------------
// 🔴 64-3-UNIT-007 (AC6 · D6): Interrupted 段条目不计入 avg（Avg: —）；但 TokensUsed 仍全累计。
// RED: 旧逻辑 dead+interrupted 有非零 DeadAt → 计入 avg → Avg≠—。
// ---------------------------------------------------------------------------
func TestATDD_64_3_UNIT_AvgExcludesInterruptedTokensStillSummed(t *testing.T) {
	procs := []vfs.ProcInfo{zombieResidue(20), interruptedDead(50)}
	out := RenderHistoryStats(procs, 0)
	if !strings.Contains(out, "Avg: —") {
		t.Errorf("仅 interrupted 段条目时 Avg 应为 —（不计入 avg/deadCount），got %q", out)
	}
	if !strings.Contains(out, "Total: 70 tok") {
		t.Errorf("Interrupted 段 token 仍应全累计（20+50=70），got %q", out)
	}
}

// ---------------------------------------------------------------------------
// 🔴 64-3-UNIT-008 (AC4 · D5 · Unicode): render 行级 interrupted fallback。
// interrupted 条目 REASON 列走成功分支 ✓（非失败分支 ✗）。
// RED: 骨架 effectiveResult 恒等 → Result="" → IsProcessFailed→true → tokens="✗"。
// ---------------------------------------------------------------------------
func TestATDD_64_3_UNIT_RenderRowInterruptedFallback_Unicode(t *testing.T) {
	// PID=1 单进程作根（PID≠PPID 不触发根短路，见 dashboard-tree-test-pid-ppid-trap）。
	p := interruptedDead(0)
	p.PID = 1
	out := renderTree643([]vfs.ProcInfo{p})
	if strings.Contains(out, "✗") {
		t.Errorf("interrupted 条目 REASON 列不应走失败分支 ✗，got %q", out)
	}
	if !strings.Contains(out, "✓") {
		t.Errorf("interrupted 条目 REASON 列应走成功/dim 分支 ✓，got %q", out)
	}
}

// ---------------------------------------------------------------------------
// 🔴 64-3-UNIT-009 (AC4 · D5 · RNIX_ASCII): render 行级 interrupted fallback（ASCII 模式）。
// interrupted 条目 badge 走成功分支 [D]（非失败分支 [E]）。
// RED: 骨架 effectiveResult 恒等 → StateBadge(Dead,"") 走失败分支 → "[E]"。
// ---------------------------------------------------------------------------
func TestATDD_64_3_UNIT_RenderRowInterruptedFallback_ASCII(t *testing.T) {
	t.Setenv("RNIX_ASCII", "1")
	p := interruptedDead(0)
	p.PID = 1
	out := renderTree643([]vfs.ProcInfo{p})
	if strings.Contains(out, "[E]") {
		t.Errorf("interrupted 条目 badge 不应走失败分支 [E]，got %q", out)
	}
	if !strings.Contains(out, "[D]") {
		t.Errorf("interrupted 条目 badge 应走成功分支 [D]，got %q", out)
	}
}

// ---------------------------------------------------------------------------
// 🔴 64-3-UNIT-010 (AC1 · D1 恒显示): Interrupted 段为 0 时仍显示（口径稳定不跳动）。
// RED: 骨架无 Interrupted 段。
// ---------------------------------------------------------------------------
func TestATDD_64_3_UNIT_InterruptedSegmentAlwaysShownWhenZero(t *testing.T) {
	now := time.Now()
	procs := []vfs.ProcInfo{
		{State: types.StateRunning},
		{State: types.StateDead, ExitReason: "completed", ExitCodeSet: true, ExitCode: 0,
			DeadAt: now, CreatedAt: now.Add(-1 * time.Second)},
	}
	out := RenderHistoryStats(procs, 0)
	if !strings.Contains(out, "Interrupted: 0") {
		t.Errorf("无 zombie/interrupted 时 Interrupted 段仍应显示为 0（恒显示），got %q", out)
	}
}
