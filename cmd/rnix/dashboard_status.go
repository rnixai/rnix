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

// renderModeLabel returns the styled "[MONITOR] │ " mode-label prefix to be
// injected at the left of the status bar (Story 38.2 AC#2).
//
// Priority order (highest first): replayMode → viewStepInspector → viewDebug →
// viewExpanded → viewDefault. The trailing UTF-8 separator '│' (ASCII '|')
// plus a single space is included; the caller appends hints with the standard
// double-space delimiter.
//
// Returns empty string only if all known view modes fail to match (defensive;
// the iota-based enum guarantees a default fallback to viewDefault).
func (m dashboardModel) renderModeLabel() string {
	ascii := ui.IsASCIIMode()
	sep := "│"
	if ascii {
		sep = "|"
	}
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))

	var label string
	var color string
	var bold bool
	switch {
	case m.replayMode:
		label = "[REPLAY]"
		color = "#D08770" // align with spec c-sys orange
	case m.viewMode == viewStepInspector:
		label = "[INSPECTOR]"
		color = colorIPC // #9B59B6 purple
	case m.viewMode == viewDebug:
		label = "[DEBUG]"
		color = ui.ColorWarning
		bold = true
	case m.viewMode == viewExpanded:
		label = "[EXPANDED]"
		color = ui.ColorAgent
	default: // viewDefault (and the now-unused viewHistory iota slot)
		label = "[MONITOR]"
		color = ui.ColorMuted
	}

	style := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
	if bold {
		style = style.Bold(true)
	}
	return style.Render(label) + "  " + dim.Render(sep) + "  "
}

func (m dashboardModel) renderDashboardStatus() string {
	initHintStyles()

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

	return "  " + rec + modeLabel + hints
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
