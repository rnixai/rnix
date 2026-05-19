// Package tree — history.go (Story 38-5 PR11 Step 4(c) RenderHistoryStats 迁出)
//
// 本文件迁出 cmd/rnix/dashboard_history.go::renderHistoryStats 的纯统计聚合渲染
// 逻辑（History View 顶部一行 Running/Done/Failed/Total tokens/Avg 摘要行）。
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
// 行为契约（cmd/rnix.renderHistoryStats 完全等价 · 0-行为变更纯重构）：
//   - 统计 Running/Done/Failed counts + total tokens + average elapsed；
//   - State 路由：StateRunning / StateCreated → running · StateDead +
//     IsFailedResult → failed · StateDead 非 failed → done · StateZombie → running；
//   - 平均时长：仅在 deadCount > 0 时计算 · 否则显示 "—"；
//   - lipgloss style 颜色：ColorSuccess (Running ●) / ColorMuted (Done ✓) / ColorError (Failed ✕)；
//   - 返回字符串前导换行 + 空格 · 与原版 `\n %s  %s  ...` 完全一致。

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

// RenderHistoryStats renders the summary statistics line shown above the Tree
// History view (Running/Done/Failed counts + total tokens + average elapsed).
//
// Story 38-5 PR11 Step 4(c) (2026-05-05): Migrated from
// cmd/rnix/dashboard_history.go::renderHistoryStats. The cmd/rnix end retains
// a thin wrapper with `(m dashboardModel) receiver + lowercase name` so the
// existing callsite (dashboard_tree.go line 60 calling `m.renderHistoryStats(allProcs)`)
// and ATDD 29.5-UNIT-001 grep contract (dashboard_history.go must contain the
// renderHistoryStats top-level function) continue to work unchanged.
//
// Pure function · zero dashboard state dependency · all input from procs slice.
//
// Behavior contract (preserved verbatim from cmd/rnix):
//   - State routing: StateRunning/StateCreated → running · StateDead+IsFailedResult →
//     failed · StateDead non-failed → done · StateZombie → running
//   - Avg elapsed: computed only when deadCount > 0; otherwise "—"
//   - Output prefixed with newline + space; styles use ui.Color* constants
//   - lipgloss styles applied per segment (Running ● / Done ✓ / Failed ✕)
func RenderHistoryStats(procs []vfs.ProcInfo) string {
	var running, done, failed, totalTokens int
	var totalElapsed time.Duration
	deadCount := 0

	for _, p := range procs {
		totalTokens += p.TokensUsed
		switch p.State {
		case types.StateRunning, types.StateCreated:
			running++
		case types.StateDead:
			if ui.IsFailedResult(p.Result) {
				failed++
			} else {
				done++
			}
			if !p.DeadAt.IsZero() {
				totalElapsed += p.DeadAt.Sub(p.CreatedAt) - p.PausedTotal
				deadCount++
			}
		case types.StateZombie:
			running++ // count zombie as still active for display
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

	return fmt.Sprintf("\n %s  %s  %s  |  Total: %s tok  |  Avg: %s",
		runStyle.Render(fmt.Sprintf("Running: %d●", running)),
		doneStyle.Render(fmt.Sprintf("Done: %d✓", done)),
		failStyle.Render(fmt.Sprintf("Failed: %d✕", failed)),
		timeline.FormatTokenCount(totalTokens),
		avg,
	)
}
