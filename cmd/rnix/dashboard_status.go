package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/ui"
)

// --- Hint 格式化 ---

// hintKeyStyle 和 hintDescStyle 在 renderDashboardStatus 中延迟初始化。
var (
	hintKeyStyle  lipgloss.Style
	hintDescStyle lipgloss.Style
	hintInited    bool
)

func initHintStyles() {
	if hintInited {
		return
	}
	hintKeyStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAgent)).Bold(true)
	hintDescStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))
	hintInited = true
}

// hint 渲染单个 key+desc 对：高亮 key，暗淡 desc
func hint(key, desc string) string {
	return hintKeyStyle.Render(key) + hintDescStyle.Render(desc)
}

// hintGroup 用双空格连接一组 hints
func hintGroup(hints ...string) string {
	return strings.Join(hints, "  ")
}

func (m dashboardModel) renderDashboardStatus() string {
	initHintStyles()

	if m.replayMode {
		return m.renderReplayStatus()
	}

	if m.confirmKill {
		return fmt.Sprintf("  Kill PID %d? [y/N]", m.confirmPID)
	}

	rec := ""
	if m.selectedPID > 0 && m.recording[m.selectedUUID] != "" {
		rec = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError)).Render("●REC") + "  "
	}

	if m.statusMsg != "" {
		return "  " + rec + m.statusMsg + "    " + hint("q", "quit")
	}

	var core []string
	exit := hint("q", "quit")

	switch m.viewMode {
	case viewExpanded:
		core, exit = m.paneHints()
	case viewStepInspector:
		core = []string{hint("j/k", "scroll"), hint("h/l", "step"), hint("1-5", "lens"), hint("y", "copy"), hint("o", "open"), hint("?", "help")}
		exit = hint("Esc", "close")
	case viewDebug:
		core = []string{hint("j/k", "nav"), hint("s", "strace"), hint("v", "detail"), hint("f", "filter"), hint("?", "help")}
		exit = hint("d/Esc", "monitor")
	default: // viewDefault
		core = []string{hint("j/k", "nav"), hint("s/S", "sort"), hint("z", "expand"), hint("p", "pause"), hint("f", "filter"), hint("?", "help")}
	}

	hints := hintGroup(core...) + "    " + exit

	// Dead process filter indicator
	if m.isSelectedProcessDead() && m.selectedPID > 0 {
		hints += hintDescStyle.Render(fmt.Sprintf("  (PID %d)", m.selectedPID))
	}

	return "  " + rec + hints
}

// paneHints returns core hints and exit hint for the current expanded pane.
func (m dashboardModel) paneHints() (core []string, exit string) {
	exit = hint("q", "quit")

	switch m.expandedPane {
	case paneTimeline:
		if m.stepFilterMode {
			return []string{hint("t", "tool"), hint("p", "plan"), hint("a", "text"), hint("c", "done"), hint("s", "spawn"), hint("r", "repl"), hint("z", "spec"), hint("C/b/x/X/T/i", "sys"), hint("*", "all")},
				hint("f/Esc", "done")
		}
		hints := []string{hint("j/k", "nav"), hint("v", "detail"), hint("e/E", "expand"), hint("n/N", "err"), hint("f", "filter")}
		if len(m.alertEvents) > 0 {
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
