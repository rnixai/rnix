package title

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/vfs"
)

// StyleProviderName returns the LLM provider name colored by the running
// process's health status, used by the Title Bar provider segment (Story 34.2).
// Healthy processes appear green; warning processes (high ctx usage) appear
// yellow; failed/disconnected processes appear red.
//
// Story 38-5 PR11 Step 4(c) (2026-05-05 · 第 9 个会话第 10 个 commit):
// Migrated from cmd/rnix/dashboard_title.go::styleProviderName. Pure function ·
// zero dashboardModel dependency · 0 cmd/rnix 反向依赖.
//
// Behavior contract (preserved verbatim from cmd/rnix · ATDD 34.2-UNIT-012 测试覆盖):
//
//   - proc == nil → ""                        (nil-safe / no provider segment)
//   - proc.Provider == "" → ""                (provider not yet detected)
//   - !connected → ColorError (red)           (daemon disconnected = unhealthy)
//   - proc.State == StateDead && IsFailedResult(Result) → ColorError (red)
//     (dead + non-zero exit / error output)
//   - ContextBudget > 0 && TokensUsed*100/ContextBudget >= 80 → ColorWarning
//     (yellow · approaching limit · 与 PctColorStyle 80% 阈值一致 · Story 38.2 颜色统一原则)
//   - else → ColorSuccess (green)             (healthy default)
//
// The 80% ctx warning threshold matches PctColorStyle's threshold so users see
// consistent color semantics across "approaching limit" vs "over limit" states
// (Story 38.2 落地的颜色一致性原则).
//
// Returns the provider name wrapped in lipgloss SGR escape codes so callers
// can append directly to the title bar string. Profile-tolerant: under
// NoColor profile, lipgloss.Render strips ANSI codes leaving just the plain
// provider name (verified by ATDD 34.2-UNIT-012).
func StyleProviderName(connected bool, proc *vfs.ProcInfo) string {
	if proc == nil || proc.Provider == "" {
		return ""
	}

	var color string
	switch {
	case !connected:
		color = ui.ColorError
	case proc.State == types.StateDead && ui.IsProcessFailed(proc.ExitCode, proc.ExitCodeSet, proc.Result):
		color = ui.ColorError
	case proc.ContextBudget > 0 && proc.LastInputTokens > 0 && int64(proc.LastInputTokens)*100/int64(proc.ContextBudget) >= 80:
		color = ui.ColorWarning
	default:
		color = ui.ColorSuccess
	}

	return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(proc.Provider)
}
