package main

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/dashboard/status"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
)

// --- Hint 格式化 ---
//
// Story 38-5 PR11 Step 4(c)：hint / hintGroup / initHintStyles 三个 helper
// 整体迁出至 internal/dashboard/status 子包（与 RenderModeLabel 同包）。
// 本文件保留同名 thin wrapper 让现有 8+ 处 hint(...) callsite 零修改通过；
// hintDescStyle.Render(...) 调用改为 status.RenderDescStyle 单一入口（行为
// 1:1 等价 · 包内 7 项契约测试已显式覆盖）。

// hint 渲染单个 key+desc 对：高亮 key，暗淡 desc
func hint(key, desc string) string {
	return status.Hint(key, desc)
}

// hintGroup 用双空格连接一组 hints
func hintGroup(hints ...string) string {
	return status.HintGroup(hints...)
}

// renderModeLabel returns the styled "[MONITOR] │ " mode-label prefix to be
// injected at the left of the status bar (Story 38.2 AC#2).
//
// Story 38-5 PR11 Step 4(c)：渲染主体迁出至 internal/dashboard/status.RenderModeLabel
// （pure pipeline · 0 cmd/rnix 反向依赖 · 与 title 包同模式）。本 wrapper 保留
// 同名 receiver 让 38.2-UNIT-005..009 行为测试 + atdd_29_1 文件拆分 grep 字符串
// 通过；行为零变化（迁出包内 11 项契约测试已显式覆盖 replayMode 优先级 / iota
// 路由 / ASCII 分隔符 / Bold 等所有分支）。
func (m dashboardModel) renderModeLabel() string {
	return status.RenderModeLabel(m.replayMode, int(m.viewMode))
}

func (m dashboardModel) renderDashboardStatus() string {
	if m.replayMode {
		return m.renderReplayStatus()
	}

	if m.confirmKill {
		// Intentionally NO mode label here: the y/N confirmation flow must
		// stay visually quiet (AC#2: confirmKill suppresses mode label).
		return fmt.Sprintf("  Kill PID %d? [y/N]", m.confirmPID)
	}

	rec := ""
	if m.selectedPID > 0 && m.recording[m.selectedUUID] != "" {
		rec = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError)).Render("●REC") + "  "
	}

	modeLabel := m.renderModeLabel()

	if m.statusMsg != "" {
		// AC#2: statusMsg context still gets the mode label so the user knows
		// which mode they're in while the flash message is showing.
		return "  " + rec + modeLabel + m.statusMsg + "    " + hint("q", "quit")
	}

	var core []string
	exit := hint("q", "quit")

	switch m.viewMode {
	case viewExpanded:
		core, exit = m.paneHints()
	case viewStepInspector:
		core = []string{hint("j/k", "scroll"), hint("h/l", "step"), hint("1-6", "lens"), hint("y", "copy"), hint("o", "open"), hint("?", "help")}
		exit = hint("Esc", "close")
	case viewDebug:
		core = []string{hint("j/k", "nav"), hint("s", "strace"), hint("v", "detail"), hint("f", "filter"), hint("?", "help")}
		exit = hint("d/Esc", "monitor")
	default: // viewDefault
		core = []string{hint("j/k", "nav"), hint("s/S", "sort"), hint("z", "expand"), hint("p", "pause"), hint("f", "filter"), hint("?", "help")}
		// Story 44.4 AC#4: when the selected process is Suspended (incl.
		// daemon-restart placeholders), surface an explicit subtree-resume
		// affordance so the橙色 process doesn't look like a dead end.
		if proc := findSelectedProcess(&m); proc != nil && proc.State == types.StateSuspended {
			core = append([]string{hint("r", "resume subtree")}, core...)
		}
	}

	hints := hintGroup(core...) + "    " + exit

	// Dead process filter indicator
	if m.isSelectedProcessDead() && m.hasProcessSelected() {
		if m.selectedPID > 0 {
			hints += status.RenderDescStyle(fmt.Sprintf("  (PID %d)", m.selectedPID))
		} else if m.selectedUUID != "" {
			uuidLabel := m.selectedUUID
			if len(uuidLabel) > 8 {
				uuidLabel = uuidLabel[:8]
			}
			hints += status.RenderDescStyle(fmt.Sprintf("  (%s)", uuidLabel))
		}
	}

	return "  " + rec + modeLabel + hints
}

// paneHints returns core hints and exit hint for the current expanded pane.
func (m dashboardModel) paneHints() (core []string, exit string) {
	exit = hint("q", "quit")

	switch m.expandedPane {
	case paneTimeline:
		if m.timeline.StepFilterMode {
			return []string{hint("t", "tool"), hint("p", "plan"), hint("a", "text"), hint("c", "done"), hint("s", "spawn"), hint("r", "repl"), hint("z", "spec"), hint("C/b/x/X/T/i", "sys"), hint("*", "all")},
				hint("f/Esc", "done")
		}
		hints := []string{hint("j/k", "nav"), hint("v", "detail"), hint("e/E", "expand"), hint("n/N", "err"), hint("f", "filter")}
		if len(m.alertStrip.Events) > 0 {
			hints = append(hints, hint("a", "alerts"))
		}
		hints = append(hints, hint("?", "help"))
		return hints, exit
	case paneHeatmap:
		return []string{hint("j/k", "nav"), hint("Enter", "detail"), hint("z", "restore"), hint("?", "help")}, exit
	case paneIntent, paneSecurity:
		return []string{hint("j/k", "nav"), hint("Enter", "jump"), hint("z", "restore"), hint("?", "help")}, exit
	case paneTrace:
		return []string{hint("j/k", "nav"), hint("Enter", "expand"), hint("z", "restore"), hint("?", "help")}, exit
	case paneEval:
		return []string{hint("j/k", "nav"), hint("h/l", "view"), hint("z", "restore"), hint("?", "help")}, exit
	default:
		return []string{hint("j/k", "nav"), hint("Enter", "select"), hint("z", "restore"), hint("?", "help")}, exit
	}
}
