// Package tree — render.go (Story 38-5 PR2 Step 3b)
//
// Tree pane 视图渲染主体，从 cmd/rnix/dashboard_tree.go::renderDashboardTreePane
// 整体迁入。函数返回**未含 border 的 raw 内容**（按 innerW/innerH 渲染），由
// cmd/rnix 端 wrapper 用 renderFixedPanel 包 border + active highlighting。
//
// 包边界约束：
//   - 本文件位于 internal/dashboard/tree，**禁止反向依赖** cmd/rnix；
//   - 通过 RenderContext 接受 cmd/rnix 端构造好的运行时数据（ReusedPIDs/
//     Recording/CollapsedIntents/HistoryStatsLine 等）；
//   - 渲染逻辑与 38-1/38-2/38-3/38-4 落地行为完全等价（红点 / 折叠 / 新进程高亮 /
//     suspend reason / orchestration annotation 全部保留）。
package tree

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/rnixai/rnix/internal/dashboard/timeline"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
)

// HighlightDisplayWindow 是新生成进程在 Agent Tree 中保持高亮的时长。
//
// Story 36-2 AC-4 落地：超过此窗口（默认 1500ms）后新进程图标 (✨/*) 与高亮背景消失。
const HighlightDisplayWindow = 1500 * time.Millisecond

// SortDirLabels 是 sort 方向标签矩阵 [sortMode][ascIndex]。
//
// ascIndex: 0=desc, 1=asc。Unicode 模式（默认）。
//
// 此变量同时被 cmd/rnix 端 treeSortDirLabels 引用（type alias 兼容）。
var SortDirLabels = [3][2]string{
	{"新→旧", "旧→新"}, // SortMode 0=Time
	{"大→小", "小→大"}, // SortMode 1=PID
	{"↓", "↑"},     // SortMode 2=State
}

// SortDirLabelsASCII 是 sort 方向标签矩阵 [sortMode][ascIndex] 的 ASCII fallback 版本。
//
// 当 RNIX_ASCII=1 时使用。
var SortDirLabelsASCII = [3][2]string{
	{"新->旧", "旧->新"},
	{"大->小", "小->大"},
	{"v", "^"},
}

// RenderContext 是 Render 调用时需要的运行时上下文。
//
// 包含 cmd/rnix 端在每次 render 调用前构造的临时数据（不便放进 TreeState 的、
// 跨 pane 共享的或依赖 dashboardModel 私有字段构造的）。
//
// 字段语义：
//   - IsActive:          当前 pane 是否激活（cmd/rnix: m.activePane == paneTree）；
//   - IsExpanded:        当前是否在 expanded view（cmd/rnix: viewMode==viewExpanded && expandedPane==paneTree）；
//   - Now:               当前时刻（cmd/rnix: time.Now()，注入便于测试 mock）；
//   - ReusedPIDs:        PID 复用映射（cmd/rnix 通过 ReusedPIDs(processes) 构造）；
//   - Recording:         UUID → recording id 映射；
//   - CollapsedIntents:  UUID → collapsed intent 显示文本（cmd/rnix 通过 BuildCollapsedIntents(state) 构造）；
//   - HistoryStatsLine:  预渲染的 history stats 行（cmd/rnix 端调用 renderHistoryStats 后传入；
//                          仅 expanded 模式使用 · 非 expanded 模式可为空字符串）。
//
// 所有字段均为只读快照（render 不会修改）。
type RenderContext struct {
	IsActive         bool
	IsExpanded       bool
	Now              time.Time
	ReusedPIDs       map[types.PID]int
	Recording        map[string]string
	CollapsedIntents map[string]string
	HistoryStatsLine string
}

// Render 渲染 Tree pane 的视图（不含 border / 不含 active highlight）。
//
// 返回的字符串将被 cmd/rnix 端 renderDashboardTreePane wrapper 用 renderFixedPanel
// 包裹 border 后输出到屏幕。
//
// 参数：
//   - state:  当前 TreeState（按值传递，避免渲染过程中被外部修改）
//   - ctx:    渲染运行时上下文（见 RenderContext godoc）
//   - innerW: panel 内部宽度（不含 border 的 2 列）；调用方计算 max(width-2, 1)
//   - innerH: panel 内部高度（不含 border 的 2 行）；调用方计算 max(height-2, 1)
//
// 渲染规则（与 38-1/38-2/38-3/38-4 落地完全等价）：
//   - Title line：sort/dir/search 模式提示 · expanded 模式额外显示搜索状态/位置/计数
//   - 每行：cursor + collapse prefix + tree prefix + intent (truncated) + suffix
//   - 行样式：normal / new process highlight / stale (red) / suspended (yellow) /
//     most-active (bold) / dead-fail (red) / dead-ok (dim)
//   - context bar：仅 running/created 进程在 innerW ≥ 30 时显示
//   - stats bar：仅 expanded 模式 · 通过 ctx.HistoryStatsLine 注入
//
// 性能上界：O(innerH × intent_avg_width)，每行 ~10 个字符串拼接 + 1 lipgloss render；
// typical innerH=20-40，可在 16ms (60fps) 内完成。
func Render(state TreeState, ctx RenderContext, innerW, innerH int) string {
	innerW = max(innerW, 1)
	innerH = max(innerH, 1)

	// Determine which rows and cursor position to use
	displayRows := state.Rows
	displayCursor := state.Cursor
	displayOffset := state.Offset
	if ctx.IsExpanded && state.SearchQuery != "" {
		displayRows = FilteredRows(state)
		displayCursor = state.SearchCursor
		displayOffset = state.SearchOffset
	}

	var b strings.Builder
	sortLabel := SortLabels[clampSortMode(state.SortMode)]
	dirArrow := "↓"
	if state.SortAsc {
		dirArrow = "↑"
	}
	ascIdx := 0
	if state.SortAsc {
		ascIdx = 1
	}
	dirLabel := SortDirLabels[clampSortMode(state.SortMode)][ascIdx]
	if ui.IsASCIIMode() {
		dirLabel = SortDirLabelsASCII[clampSortMode(state.SortMode)][ascIdx]
	}

	// Title line — show search state when in expanded mode
	if ctx.IsExpanded {
		pos := min(displayCursor+1, len(displayRows))
		total := len(displayRows)
		if total == 0 {
			pos = 0
		}
		if state.SearchMode {
			fmt.Fprintf(&b, " Agent Tree [%s %s %s] %d/%d | Search: %s_\n",
				sortLabel, dirArrow, dirLabel, pos, total, state.SearchQuery)
		} else if state.SearchQuery != "" {
			fmt.Fprintf(&b, " Agent Tree [%s %s %s] %d/%d | Filter: %q  (Esc to clear)\n",
				sortLabel, dirArrow, dirLabel, pos, total, state.SearchQuery)
		} else {
			if len(state.Rows) > 0 {
				fmt.Fprintf(&b, " Agent Tree [%s %s %s] %d/%d | / to search\n",
					sortLabel, dirArrow, dirLabel, min(state.Cursor+1, len(state.Rows)), len(state.Rows))
			} else {
				fmt.Fprintf(&b, " Agent Tree [%s %s %s] | / to search\n", sortLabel, dirArrow, dirLabel)
			}
		}
	} else {
		if len(state.Rows) > 0 {
			pos := min(state.Cursor+1, len(state.Rows))
			fmt.Fprintf(&b, " Agent Tree [%s %s %s] %d/%d\n", sortLabel, dirArrow, dirLabel, pos, len(state.Rows))
		} else {
			fmt.Fprintf(&b, " Agent Tree [%s %s %s]\n", sortLabel, dirArrow, dirLabel)
		}
	}

	// Stats bar occupies 2 lines (blank line + content) when expanded
	statsLines := 0
	if ctx.IsExpanded {
		statsLines = 2
	}
	visibleLines := max(innerH-1-statsLines, 1)
	showCtxBar := innerW >= 30

	linesRendered := 0
	for i := displayOffset; i < len(displayRows) && linesRendered < visibleLines; i++ {
		row := displayRows[i]
		cursor := "  "
		if i == displayCursor {
			cursor = "▸ "
		}

		stateMark := ui.StateBadge(row.Proc.State, row.Proc.Result)

		if row.Proc.IsPaused && (row.Proc.State == types.StateRunning || row.Proc.State == types.StateCreated) {
			if ui.IsASCIIMode() {
				stateMark = "[P]"
			} else {
				stateMark = "⏸"
			}
		}

		// Stale detection (Story 30.5)
		isStale := !row.Proc.IsPaused &&
			(row.Proc.State == types.StateRunning || row.Proc.State == types.StateCreated) &&
			ui.IsStale(row.Proc.LastHeartbeat, row.Proc.StepTimeout)

		// Suspended reason abbreviation (Story 30.8 AC#6)
		isSuspended := row.Proc.State == types.StateSuspended
		if isSuspended {
			stateMark += " " + SuspendReasonAbbrev(row.Proc.SuspendReason)
		}

		// Collapsed dead subtree prefix (Story 34.3 AC3)
		collapsePrefix := ""
		if row.Collapsed {
			if ui.IsASCIIMode() {
				collapsePrefix = "> "
			} else {
				collapsePrefix = "▶ "
			}
		}

		// Intent: use collapsed prefix if available (AC4)
		intent := strings.ReplaceAll(row.Proc.Intent, "\n", " ")
		if intent == "" {
			intent = "—"
		}
		if collapsed, ok := ctx.CollapsedIntents[row.Proc.UUID]; ok {
			intent = collapsed
		}

		// Compact metadata suffix
		pidPart := fmt.Sprintf("%d", row.Proc.PID)

		isDead := row.Proc.State == types.StateDead || row.Proc.State == types.StateZombie
		isDeadOk := isDead && !ui.IsFailedResult(row.Proc.Result)
		isDeadFail := isDead && ui.IsFailedResult(row.Proc.Result)
		var tokens string
		var elapsed string

		if isDead {
			if ctx.IsExpanded {
				resultText := strings.Map(func(r rune) rune {
					if r == '\n' || r == '\r' || r == '\t' {
						return ' '
					}
					return r
				}, row.Proc.Result)
				resultText = strings.TrimSpace(resultText)
				if resultText == "" {
					resultText = "—"
				}
				runes := []rune(resultText)
				if len(runes) > 22 {
					resultText = string(runes[:21]) + "…"
				}
				if ui.IsFailedResult(row.Proc.Result) {
					tokens = ui.ErrorStyle.Render("✗ " + resultText)
				} else {
					tokens = "✓ " + resultText
				}
			} else {
				if ui.IsFailedResult(row.Proc.Result) {
					if ui.IsASCIIMode() {
						tokens = ui.ErrorStyle.Render("x")
					} else {
						tokens = ui.ErrorStyle.Render("✗")
					}
				} else {
					if ui.IsASCIIMode() {
						tokens = "ok"
					} else {
						tokens = "✓"
					}
				}
			}
			if !row.Proc.DeadAt.IsZero() {
				elapsed = ui.FormatDuration(row.Proc.DeadAt.Sub(row.Proc.CreatedAt))
			} else if row.Proc.IsPaused && !row.Proc.PausedAt.IsZero() {
				elapsed = ui.FormatDuration(row.Proc.PausedAt.Sub(row.Proc.CreatedAt))
			} else {
				elapsed = ui.FormatDuration(ctx.Now.Sub(row.Proc.CreatedAt))
			}
		} else {
			if row.Proc.ContextBudget > 0 {
				pct := max(0, min(int(int64(row.Proc.TokensUsed)*100/int64(row.Proc.ContextBudget)), 100))
				tokens = fmt.Sprintf("%s/%s(%d%%)",
					timeline.FormatTokenCount(row.Proc.TokensUsed),
					timeline.FormatTokenCount(row.Proc.ContextBudget), pct)
				if pct >= 80 {
					tokens = ui.WarningStyle.Render(tokens)
				}
			} else {
				tokens = timeline.FormatTokenCount(row.Proc.TokensUsed)
			}
			if row.Proc.IsPaused && !row.Proc.PausedAt.IsZero() {
				elapsed = ui.FormatDuration(row.Proc.PausedAt.Sub(row.Proc.CreatedAt))
			} else {
				elapsed = ui.FormatDuration(ctx.Now.Sub(row.Proc.CreatedAt))
			}
		}

		// PID reuse indicator
		if _, isReused := ctx.ReusedPIDs[row.Proc.PID]; isReused {
			uuidShort := row.Proc.UUID
			if len(uuidShort) > 6 {
				uuidShort = uuidShort[:6]
			}
			pidPart = fmt.Sprintf("%d(%s)", row.Proc.PID, uuidShort)
		}

		// Recording indicator
		rec := ""
		if ctx.Recording[row.Proc.UUID] != "" {
			rec = " ●"
		}

		// Orchestration annotation (Story 34.7)
		orchAnnotation := OrchestrationAnnotation(row.Proc)

		// Wall-clock start time (compact: HH:MM when width allows, expanded: HH:MM:SS)
		var wallClock string
		if !row.Proc.CreatedAt.IsZero() {
			if ctx.IsExpanded {
				wallClock = ui.FormatWallClock(row.Proc.CreatedAt)
			} else if innerW >= 50 {
				wallClock = ui.FormatWallClockShort(row.Proc.CreatedAt)
			}
		}

		// New process highlight (Story 36-2 AC-4)
		isNewProcess := false
		if firstSeen, ok := state.ProcessFirstSeenAt[row.Proc.PID]; ok && ctx.Now.Sub(firstSeen) < HighlightDisplayWindow {
			isNewProcess = true
		}

		highlightPrefix := ""
		if isNewProcess {
			if ui.IsASCIIMode() {
				highlightPrefix = "* "
			} else {
				highlightPrefix = "✨ "
			}
		}

		// Calculate available width for intent (elastic).
		prefixW := lipgloss.Width(highlightPrefix + cursor + collapsePrefix + row.Prefix)
		suffixParts := []string{pidPart, stateMark, wallClock, tokens, elapsed}
		if orchAnnotation != "" {
			suffixParts = append(suffixParts, orchAnnotation)
		}
		var filtered []string
		for _, p := range suffixParts {
			if p != "" {
				filtered = append(filtered, p)
			}
		}
		suffixStr := strings.Join(filtered, " ") + rec
		suffixW := lipgloss.Width(suffixStr)
		intentW := max(innerW-prefixW-suffixW-3, 8)
		intentTrunc := truncateRuneWidth(intent, intentW)

		// Most active process highlight (Story 34.3 AC5)
		isMostActive := false
		if row.Proc.State == types.StateRunning || row.Proc.State == types.StateCreated {
			if lastEvt, ok := state.LastEventByPID[row.Proc.PID]; ok {
				if ctx.Now.Sub(lastEvt) < 2*time.Second {
					isMostActive = true
				}
			}
		}

		intentPad := max(intentW-runewidth.StringWidth(intentTrunc), 0)

		line := fmt.Sprintf("%s%s%s%s%s%s %s",
			highlightPrefix, cursor, collapsePrefix, row.Prefix, intentTrunc, strings.Repeat(" ", intentPad), suffixStr)
		if isNewProcess {
			if ui.IsASCIIMode() {
				line = lipgloss.NewStyle().Reverse(true).Render(line)
			} else {
				line = ui.HighlightStyle.Render(line)
			}
		} else if isStale {
			line = lipgloss.NewStyle().
				Foreground(lipgloss.Color("196")).
				Render(line)
		} else if isSuspended {
			line = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#FFD93D")).
				Render(line)
		} else if isMostActive {
			line = lipgloss.NewStyle().Bold(true).Render(line)
		} else if isDeadFail {
			line = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError)).Render(line)
		} else if isDeadOk {
			line = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(line)
		}
		b.WriteString(line)
		b.WriteString("\n")
		linesRendered++

		// Context bar for running/created processes only (Story 34.3 AC2)
		if showCtxBar && (row.Proc.State == types.StateRunning || row.Proc.State == types.StateCreated) && linesRendered < visibleLines {
			if row.Proc.ContextBudget > 0 {
				barPrefix := strings.Repeat(" ", lipgloss.Width(cursor+collapsePrefix+row.Prefix))
				ctxBar := RenderCtxBar(row.Proc.TokensUsed, row.Proc.ContextBudget, 10)
				b.WriteString(barPrefix + ctxBar + "\n")
				linesRendered++
			}
		}
	}

	// Stats bar — only in expanded mode
	if ctx.IsExpanded {
		b.WriteString(ctx.HistoryStatsLine)
		b.WriteString("\n")
	}

	return b.String()
}

// clampSortMode 把任意 SortMode 整数限制在 [0, len(SortLabels)-1] 范围内。
//
// 防止越界：当 state.SortMode > 2 时回退到 0（Time）。
func clampSortMode(m int) int {
	if m < 0 || m >= len(SortLabels) {
		return 0
	}
	return m
}

// truncateRuneWidth truncates a string to fit within maxWidth display columns.
//
// 复制自 cmd/rnix/dashboard_timeline.go::truncateRuneWidth（保持行为一致）。
// CJK 双宽字符 + emoji 通过 runewidth 库处理。
func truncateRuneWidth(s string, maxWidth int) string {
	if runewidth.StringWidth(s) <= maxWidth {
		return s
	}
	return runewidth.Truncate(s, maxWidth-1, "…")
}
