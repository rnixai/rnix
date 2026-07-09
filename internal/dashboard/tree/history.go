// Package tree — history.go (Story 38-5 PR11 Step 4(c) RenderHistoryStats 迁出)
//
// 本文件迁出 cmd/rnix/dashboard_history.go::renderHistoryStats 的纯统计聚合渲染
// 逻辑（History View 顶部一行 Running/Done/Failed/Interrupted/Total tokens/Avg 摘要行）。
//
// **迁移动机**（PR11 Step 4(c) · 2026-05-05）：
//
//   - renderHistoryStats 在 cmd/rnix 端是 (m dashboardModel) receiver 方法，但
//     函数体不引用任何 m 字段（procs 参数已满足全部输入）· 实质是 pure pipeline
//     procs []vfs.ProcInfo → string；
//   - 与本包既有 helpers.go::AgentLabel / RenderCtxBar pure helpers 同抽象级别 ·
//     归属 tree 包合理（tree pane expanded mode 依赖此 stats line · spec § Tasks 2.3
//     RenderContext 设计已包含 HistoryStatsLine 字段）；
//   - cmd/rnix wrapper 保留 (m dashboardModel) receiver + 同名小写让 ATDD 29.5-UNIT-001
//     "dashboard_history.go 必须包含 renderHistoryStats top-level 函数" + dashboard_tree.go
//     line 60 callsite 零修改通过 · 与 PR2/PR3/PR4 等 helper 迁出 + thin wrapper 模式一致。
//
// 包边界（spec § 04 风险 3 缓解）：
//   - 不 import cmd/rnix（go module 边界已强制）；
//   - 仅依赖 fmt + time + lipgloss + ui + types + vfs（与 helpers.go 同栈）；
//   - **零** cmd/rnix-private 类型引用。
//
// 行为契约（Story 64.3 起 · 案卷 R2 修复 · 不再是「0-行为变更纯重构」）：
//   - 四段路由 Running/Done/Failed/Interrupted：
//       StateRunning/StateCreated                          → running；
//       StateDead + ExitReason=="interrupted"（64.1 归一化） → interrupted（D1）；
//       StateDead 其余 + IsProcessFailed                    → failed，否则 → done；
//       StateZombie（运行期待 Wait 回收残影）               → interrupted（D1）；
//   - Interrupted 段恒显示（含 0，口径稳定避免布局跳动）· 配色 ui.ColorWarning（黄）·
//     符号 ⏸（对齐 ui.StateSymbol zombie）· 沿用 stats 行现状硬编码 Unicode 口径（无 ASCII 分支）；
//   - 截断标注（D2）：dedupedTotal >= historyRingCap 时行尾追加含 "1000+" 的黄色标注
//     （kernel 零改动约束下展示层无法知 ring 是否真丢弃，Total 达 cap 即截断高度可疑）；
//   - Avg 口径（D6）：仅正常终结（done+failed）条目计入 avg/deadCount · Interrupted 段不计
//     （zombie 现状本就不计；dead+interrupted 的 DeadAt 是 64.1 mtime 近似值 + "中断时刻-创建
//     时刻" 非完成时长语义）· TokensUsed 全段累计（Interrupted 消耗的 token 是真实成本）；
//   - 输入源（D3）：由 cmd/rnix wrapper 传 m.processes（fetchPagedProcs cumulative set），
//     非 tree.Rows 展平行——后者随 dead 子树折叠缩水 + 含 builder 占位 synthetic 节点；
//   - lipgloss 颜色：ColorSuccess (Running ●) / ColorMuted (Done ✓) / ColorError (Failed ✕) /
//     ColorWarning (Interrupted ⏸ + 截断标注)；
//   - 返回字符串前导换行 + 空格。

package tree

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/dashboard/timeline"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/vfs"
)

// exitReasonInterrupted mirrors kernel/history_reconcile.go exitReasonInterrupted —
// the SOLE writer of ExitReason=="interrupted"（Story 64.1 daemon-restart 归一化把
// created/running/zombie 非终态快照转为 dead + exit_reason=interrupted）。tree 包不
// import kernel，展示层自持副本；zombie 起源条目保留真实 ExitReason（killed/completed/
// context_full）+ ExitCodeSet=true，不匹配此字面量 → 天然按 exit code 归 done/failed（D1 不扩面）。
const exitReasonInterrupted = "interrupted"

// historyRingCap mirrors kernel.NewProcessHistory(1000) —— ProcessHistory ring buffer
// 容量。dedupedTotal（IPC 分页去重后全项目条数）达到此值时 ring 可能已丢弃更旧条目，
// 统计行追加 "1000+" 截断标注。kernel 无导出常量，展示层自持副本（tree 不 import kernel）。
const historyRingCap = 1000

// RenderHistoryStats renders the summary statistics line shown above the Tree
// History view (Running/Done/Failed/Interrupted counts + total tokens + average elapsed).
//
// Story 38-5 PR11 Step 4(c) (2026-05-05): Migrated from
// cmd/rnix/dashboard_history.go::renderHistoryStats. The cmd/rnix end retains
// a thin wrapper with `(m dashboardModel) receiver + lowercase name` so the
// existing callsite (dashboard_tree.go calling `m.renderHistoryStats(m.processes)`)
// and ATDD 29.5-UNIT-001 grep contract (dashboard_history.go must contain the
// renderHistoryStats top-level function) continue to work unchanged.
//
// Pure function · zero dashboard state dependency · all input from parameters.
//
// dedupedTotal（Story 64.3 D2）= kernel ListAllProcs 去重后全项目条数（IPC 分页元数据
// m.procPaging.Total），仅用于 ring-cap 截断标注；wrapper 内读取后传入。
//
// Behavior contract（Story 64.3 起 · 详见文件头「行为契约」）：
//   - 四段路由：Running/Created→running · Dead+ExitReason=interrupted→interrupted ·
//     Dead 其余 IsProcessFailed→failed 否则→done · Zombie→interrupted；
//   - Interrupted 段恒显示（黄 ⏸）；截断标注 dedupedTotal>=historyRingCap 时含 "1000+"；
//   - Avg 仅 done+failed 计入（Interrupted 不计）· Token 全段累计；
//   - 输出前导换行 + 空格。
func RenderHistoryStats(procs []vfs.ProcInfo, dedupedTotal int) string {
	var running, done, failed, interrupted, totalTokens int
	var totalElapsed time.Duration
	deadCount := 0

	for _, p := range procs {
		totalTokens += p.TokensUsed
		switch p.State {
		case types.StateRunning, types.StateCreated:
			running++
		case types.StateDead:
			// Story 64.3 D1: 64.1 归一化产物（created/running 快照被 daemon 重启杀死 →
			// dead + ExitReason=interrupted + ExitCodeSet=false + Result=""）归 Interrupted
			// 段——现行全堆 Failed 污染成功率（案卷 R2）。zombie 起源条目保留真实 ExitReason +
			// ExitCodeSet=true，不匹配字面量 → 落入 else 分支按 exit code 归 done/failed（不扩面）。
			if p.ExitReason == exitReasonInterrupted {
				interrupted++
				// D6: Interrupted 段不计入 avg/deadCount（DeadAt 是 mtime 近似 + 非完成时长语义）。
			} else {
				if ui.IsProcessFailed(p.ExitCode, p.ExitCodeSet, p.Result) {
					failed++
				} else {
					done++
				}
				if !p.DeadAt.IsZero() {
					totalElapsed += p.DeadAt.Sub(p.CreatedAt) - p.PausedTotal
					deadCount++
				}
			}
		case types.StateZombie:
			// Story 64.3 D1: 运行期待 Wait 回收残影——成败已定但尚未终局呈现。计 Running
			// 是案卷 R2 缺陷；计 done/failed 抢跑 IsProcessFailed 字段时序（Exit 刚 stamp）。
			interrupted++
		}
	}

	avg := "—"
	if deadCount > 0 {
		avgDur := totalElapsed / time.Duration(deadCount)
		avg = ui.FormatDuration(avgDur)
	}

	runStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorSuccess))
	doneStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))
	failStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError))
	intrStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorWarning))

	line := fmt.Sprintf("\n %s  %s  %s  %s  |  Total: %s tok  |  Avg: %s",
		runStyle.Render(fmt.Sprintf("Running: %d●", running)),
		doneStyle.Render(fmt.Sprintf("Done: %d✓", done)),
		failStyle.Render(fmt.Sprintf("Failed: %d✕", failed)),
		intrStyle.Render(fmt.Sprintf("Interrupted: %d⏸", interrupted)),
		timeline.FormatTokenCount(totalTokens),
		avg,
	)

	// Story 64.3 D2: kernel 零改动约束下展示层无法知道 ring 是否真丢弃过条目；
	// dedupedTotal >= cap 即 ring 已满 = 截断高度可疑（active 段使 Total 可能略超 cap）。
	// 标注语义 = "至少 historyRingCap 条、可能更多"，与 "1000+" 措辞自洽。
	if dedupedTotal >= historyRingCap {
		line += intrStyle.Render(fmt.Sprintf("  |  %d+ procs", historyRingCap))
	}
	return line
}
