package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/dashboard/title"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/vfs"
)

// renderDashboardTitle renders the health indicator title bar (Story 34.2).
// Format: rnix {conn} {provider} ── {counts} ── ctx {pct}% ── budget {pct}% ── {elapsed}
// Segments are progressively trimmed for narrow terminals.
func (m dashboardModel) renderDashboardTitle() string {
	ascii := ui.IsASCIIMode()

	sep := " ── "
	if ascii {
		sep = " -- "
	}

	// Connection indicator
	var connChar string
	if m.connected {
		if ascii {
			connChar = "*"
		} else {
			connChar = "●"
		}
	} else {
		if ascii {
			connChar = "o"
		} else {
			connChar = "○"
		}
	}

	// Active process count
	active := 0
	for _, p := range m.processes {
		if p.State == types.StateRunning || p.State == types.StateCreated {
			active++
		}
	}

	// Selected process
	var selectedProc *vfs.ProcInfo
	if m.hasProcessSelected() {
		for i := range m.processes {
			match := false
			if m.selectedPID > 0 {
				match = m.processes[i].PID == m.selectedPID
			} else if m.selectedUUID != "" {
				match = m.processes[i].UUID == m.selectedUUID
			}
			if match {
				selectedProc = &m.processes[i]
				break
			}
		}
	}

	// Provider segment (colored)
	providerStyled := ""
	if selectedProc != nil && selectedProc.Provider != "" {
		providerStyled = styleProviderName(m.connected, selectedProc)
	}

	// Count segment: NP ME KW with color
	errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError)).Bold(true)
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorWarning))

	ePart := fmt.Sprintf("%dE", m.errorCount)
	if m.errorCount > 0 {
		ePart = errStyle.Render(ePart)
	}
	wPart := fmt.Sprintf("%dW", m.warnCount)
	if m.warnCount > 0 {
		wPart = warnStyle.Render(wPart)
	}
	countSeg := fmt.Sprintf("%dP %s %s", active, ePart, wPart)
	countPlain := fmt.Sprintf("%dP %dE %dW", active, m.errorCount, m.warnCount)

	// ctx% segment
	ctxPct := computeCtxPercent(m.selectedPID, m.selectedUUID, m.processes)
	ctxPlain := fmt.Sprintf("ctx %d%%", ctxPct)
	ctxSeg := pctColorStyle(ctxPct).Render(ctxPlain)

	// budget% segment
	budgetPct := computeBudgetPercent(m.selectedPID, m.selectedUUID, m.processes)
	budgetPlain := fmt.Sprintf("budget %d%%", budgetPct)
	budgetSeg := pctColorStyle(budgetPct).Render(budgetPlain)

	// elapsed segment
	//
	// Epic 42 fix: freeze elapsed time once the process is no longer running.
	// Previously this used `time.Since(CreatedAt)` unconditionally, so killed /
	// dead / zombie processes appeared to keep ticking — leading users to think
	// `K` had failed when in fact only the display was stale.
	elapsedSeg := ""
	if selectedProc != nil {
		elapsedSeg = formatElapsedHHMMSS(elapsedDuration(*selectedProc))
	}

	base := "  rnix " + connChar

	// Build left part (base + optional provider)
	leftPart := base
	leftPlain := base
	if providerStyled != "" {
		leftPart = base + " " + providerStyled
		leftPlain = base + " " + selectedProc.Provider
	}

	// Build candidates from widest to narrowest (trim order: elapsed → budget → ctx → provider)
	type candidate struct {
		rendered string
		plain    string
	}

	candidates := []candidate{}

	// Level 5 (full): left + counts + ctx + budget + elapsed
	if elapsedSeg != "" {
		candidates = append(candidates, candidate{
			leftPart + sep + countSeg + sep + ctxSeg + sep + budgetSeg + sep + elapsedSeg,
			leftPlain + sep + countPlain + sep + ctxPlain + sep + budgetPlain + sep + elapsedSeg,
		})
	}
	// Level 4: left + counts + ctx + budget
	candidates = append(candidates, candidate{
		leftPart + sep + countSeg + sep + ctxSeg + sep + budgetSeg,
		leftPlain + sep + countPlain + sep + ctxPlain + sep + budgetPlain,
	})
	// Level 3: left + counts + ctx
	candidates = append(candidates, candidate{
		leftPart + sep + countSeg + sep + ctxSeg,
		leftPlain + sep + countPlain + sep + ctxPlain,
	})
	// Level 2: left + counts
	candidates = append(candidates, candidate{
		leftPart + sep + countSeg,
		leftPlain + sep + countPlain,
	})
	// Level 1 (minimum): base + NP ME (no provider, no W)
	minEPart := fmt.Sprintf("%dE", m.errorCount)
	if m.errorCount > 0 {
		minEPart = errStyle.Render(minEPart)
	}
	minCount := fmt.Sprintf("%dP %s", active, minEPart)
	candidates = append(candidates, candidate{
		base + " " + minCount,
		fmt.Sprintf("%s %dP %dE", base, active, m.errorCount),
	})

	maxW := m.width
	if maxW == 0 {
		maxW = 120
	}

	var titleLine string
	for _, c := range candidates {
		if lipgloss.Width(c.plain) <= maxW {
			titleLine = c.rendered
			break
		}
	}
	if titleLine == "" {
		titleLine = candidates[len(candidates)-1].rendered
	}

	// Panel tabs on second line (AC5: move pane labels to second row)
	if maxW >= 60 {
		tabLine := m.renderPanelTabsLine()
		// Story 38.1 AC#6: Mode Strip third line (only when modes are non-empty).
		modeStrip := m.renderModeStrip()
		if modeStrip != "" {
			return titleLine + "\n" + tabLine + "\n" + modeStrip
		}
		return titleLine + "\n" + tabLine
	}
	return titleLine
}

// renderModeStrip renders the active modes for the current pane, sourced from
// Layer 2 KeyLayer.ActiveModesFn (Story 38.1 AC#6).
//
// Format: "  ⌥ filter:tool · sort:time · follow:on" (UTF-8) or
//         "  [M] filter:tool | sort:time | follow:on" (ASCII).
//
// Returns empty string when no modes are active (caller should skip the line).
func (m dashboardModel) renderModeStrip() string {
	if m.dispatcher == nil {
		return ""
	}
	modes := m.dispatcher.ActiveModesFor(ui.PaneID(m.activePane), m)
	if len(modes) == 0 {
		return ""
	}
	ascii := ui.IsASCIIMode()
	prefix := "⌥"
	sep := " · "
	if ascii {
		prefix = "[M]"
		sep = " | "
	}
	parts := make([]string, 0, len(modes))
	for _, md := range modes {
		parts = append(parts, md.Name+":"+md.Value)
	}
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))
	return "  " + dim.Render(prefix+" "+strings.Join(parts, sep))
}

// renderPanelTabsLine renders the [1]Tree [2]Time ... pane navigation labels.
//
// Story 38-4 AC#1: when m.paneHasUnread[pane] is true, append a red dot
// (`●` in unicode mode, `*` in ASCII mode) immediately after the tab label
// — e.g. `[2]Time●` indicates the Timeline pane has unread events. The dot
// inherits the active-bold style when on the active pane (which only
// happens transiently before clearPaneUnread fires) and uses ColorError red
// otherwise. Active vs inactive ANSI colour for the rest of the label is
// unchanged from previous Stories.
//
// Width contract: this entire line is hidden by the caller when terminal
// width < 60 cols (see renderDashboardTitle), so the extra rune does not
// affect narrow-terminal layouts. Each dot adds 1 rune (no East Asian
// Width inflation; ASCII fallback is also 1 byte).
func (m dashboardModel) renderPanelTabsLine() string {
	selectedStyle := lipgloss.NewStyle().Bold(true)
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))
	dotStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError))
	ascii := ui.IsASCIIMode()
	dotChar := "●"
	if ascii {
		dotChar = "*"
	}

	panes := []struct {
		key  string
		name string
		pane paneType
	}{
		{"1", "Tree", paneTree},
		{"2", "Time", paneTimeline},
		{"3", "Heat", paneHeatmap},
		{"4", "Detail", paneDetail},
		{"5", "Intent", paneIntent},
		{"6", "Sec", paneSecurity},
		{"7", "Trace", paneTrace},
		{"8", "Eval", paneEval},
	}

	var b strings.Builder
	b.WriteString("  ")
	for i, p := range panes {
		label := fmt.Sprintf("[%s]%s", p.key, p.name)
		// Append unread dot before the trailing separator so spacing stays consistent.
		if int(p.pane) >= 0 && int(p.pane) < len(m.paneHasUnread) && m.paneHasUnread[p.pane] {
			label += dotStyle.Render(dotChar)
		}
		if i < len(panes)-1 {
			label += " "
		}
		if m.activePane == p.pane {
			b.WriteString(selectedStyle.Render(label))
		} else {
			b.WriteString(dimStyle.Render(label))
		}
	}
	return b.String()
}

// computeHealthCounts — thin wrapper · 见 internal/dashboard/title.ComputeHealthCounts.
//
// Story 38-5 PR11 Step 4(c) (2026-05-05 · 第 9 个会话第 11 个 commit):
// Title Bar 的 health indicator 计算 helper 已迁至 internal/dashboard/title/
// health_counts.go (与 ComputeCtxPercent / ComputeBudgetPercent /
// StyleProviderName 同会话连续 commit · 0 dashboardModel 依赖) · cmd/rnix
// wrapper 仅保留旧名让 1 处主代码 callsite (dashboard.go::dashboardTick
// errorCount/warnCount 计算) + 测试 callsite (dashboard_test.go::
// TestComputeHealthCounts_* ATDD 34.2-UNIT-001 至 008) + atdd_29_1 line 196
// grep 字符串 ("computeHealthCounts") 零修改通过.
func computeHealthCounts(processes []vfs.ProcInfo, events []UnifiedEvent, heartbeat *ipc.HeartbeatStatusResponse) (errorCount, warnCount int) {
	return title.ComputeHealthCounts(processes, events, heartbeat)
}

// computeCtxPercent — thin wrapper · 见 internal/dashboard/title.ComputeCtxPercent.
//
// Story 38-5 PR11 Step 4(c) (2026-05-05 · 第 9 个会话第 9 个 commit):
// Title Bar 的 ctx 使用率纯 helper 已迁至 internal/dashboard/title/health.go
// (与 ComputeBudgetPercent 同 commit · 0 dashboardModel 依赖) · cmd/rnix
// wrapper 仅保留旧名让 2 处主代码 callsite (dashboard_title.go::renderDashboardTitle
// ctxPct 计算) + 测试 callsite (dashboard_test.go::TestComputeCtxPercent_*
// ATDD 34.2-UNIT-009) + atdd_29_1 line 197 grep 字符串 ("computeCtxPercent")
// 零修改通过.
func computeCtxPercent(selectedPID types.PID, selectedUUID string, processes []vfs.ProcInfo) int {
	return title.ComputeCtxPercent(selectedPID, selectedUUID, processes)
}

// computeBudgetPercent — thin wrapper · 见 internal/dashboard/title.ComputeBudgetPercent.
//
// Story 38-5 PR11 Step 4(c) (2026-05-05 · 第 9 个会话第 9 个 commit):
// Title Bar 的 cost/token budget 使用率纯 helper 已迁至 internal/dashboard/title/
// health.go (与 ComputeCtxPercent 同 commit · 0 dashboardModel 依赖) · cmd/rnix
// wrapper 仅保留旧名让 2 处主代码 callsite (dashboard_title.go::renderDashboardTitle
// budgetPct 计算) + 测试 callsite (dashboard_test.go::TestComputeBudgetPercent_*
// ATDD 34.2-UNIT-010) + atdd_29_1 line 197 grep 字符串 ("computeBudgetPercent")
// 零修改通过.
func computeBudgetPercent(selectedPID types.PID, selectedUUID string, processes []vfs.ProcInfo) int {
	return title.ComputeBudgetPercent(selectedPID, selectedUUID, processes)
}

// styleProviderName — thin wrapper · 见 internal/dashboard/title.StyleProviderName.
//
// Story 38-5 PR11 Step 4(c) (2026-05-05 · 第 9 个会话第 10 个 commit):
// Title Bar 的 provider 健康颜色 helper 已迁至 internal/dashboard/title/
// provider.go (与 ComputeCtxPercent / ComputeBudgetPercent 同会话连续 commit ·
// 0 dashboardModel 依赖) · cmd/rnix wrapper 仅保留旧名让 1 处主代码 callsite
// (dashboard_title.go::renderDashboardTitle providerStyled 计算) + 测试
// callsite (dashboard_test.go::TestStyleProviderName ATDD 34.2-UNIT-012) +
// atdd_29_1 line 197 grep 字符串 ("styleProviderName") 零修改通过.
func styleProviderName(connected bool, proc *vfs.ProcInfo) string {
	return title.StyleProviderName(connected, proc)
}

// formatElapsedHHMMSS — thin wrapper · 见 internal/dashboard/title.FormatElapsedHHMMSS.
//
// Story 38-5 PR11 Step 4(c)：Title Bar elapsed-time 格式化纯 helper 迁至
// internal/dashboard/title 包（与 ClampPercent / PctColorStyle 同 commit 内聚）·
// cmd/rnix wrapper 仅保留旧名让 1 处 callsite（dashboard_title.go::renderDashboardTitle
// elapsedSeg 计算）零修改通过。
func formatElapsedHHMMSS(d time.Duration) string {
	return title.FormatElapsedHHMMSS(d)
}

// elapsedDuration returns wall-clock elapsed time for the given process,
// freezing at DeadAt once the process has exited.
//
// Epic 42 fix: previously `time.Since(CreatedAt)` was used unconditionally,
// causing killed/dead/zombie processes to display a still-ticking elapsed
// counter — making users think `K` had failed. After this fix:
//   - DeadAt populated → return DeadAt-CreatedAt (frozen).
//   - DeadAt zero → return time.Since(CreatedAt) (live).
//
// The kernel writes DeadAt during reap (`kernel/reap.go`) for both natural
// completion and SIGTERM-triggered termination, so any process the user sees
// as exited / killed in the tree pane will have DeadAt set.
func elapsedDuration(proc vfs.ProcInfo) time.Duration {
	if !proc.DeadAt.IsZero() && proc.DeadAt.After(proc.CreatedAt) {
		return proc.DeadAt.Sub(proc.CreatedAt)
	}
	return time.Since(proc.CreatedAt)
}

// pctColorStyle — thin wrapper · 见 internal/dashboard/title.PctColorStyle.
//
// Story 38-5 PR11 Step 4(c)：Title Bar percent 阈值颜色 helper 迁至
// internal/dashboard/title 包（与 ClampPercent / FormatElapsedHHMMSS 同 commit
// 内聚 · Story 38.2 AC#1 行为契约保留 < 60 Muted / 60-79 Warning / ≥80 Error+Bold）·
// cmd/rnix wrapper 仅保留旧名让 2 处主代码 callsite（dashboard_title.go::renderDashboardTitle
// ctxSeg/budgetSeg 渲染）+ 测试 callsite (dashboard_test.go::TestPctColorStyle_Thresholds
// 38.2-UNIT-001) 零修改通过。
func pctColorStyle(pct int) lipgloss.Style {
	return title.PctColorStyle(pct)
}
