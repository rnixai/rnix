// Package timeline — helpers.go (Story 38-5 PR11 Step 4(a-2) timeline 纯 helpers
// 迁出 · 同 alertstrip Step 4(a-2) AlertStripHeight 模式)
//
// 本文件只迁出 timeline pane 的 pure helpers（无 dashboardModel 状态依赖 · 仅依赖
// stdlib + lipgloss + runewidth + ipc），让 render body 迁出（Step 4(c)）可分独立 commit
// ship · 同 spec § 04 风险 5 中断保险原则。
//
// 迁出范围：
//   - ActionColor / ActionAbbrev — step action 颜色与缩写映射
//   - ShortenArgs — 单行裁剪到 maxLen 字符宽度
//   - FormatDefaultLine — 从 StepSummaryWire + GetStepDetailResponse 派生
//     display action / summary
//   - FormatTokenCount / FormatDurationMs — 数值格式化（与 trace.FormatDurationMs /
//     eval.FormatDurationMs 等价 · 自包含避免跨 pane 依赖）
//   - TruncateRuneWidth — rune-aware 截断到指定宽度
//   - TruncateAnsi — ANSI-aware 截断（lipgloss profile-tolerant）
//   - FormatCharCount — char count 格式化（k 后缀 ≥ 1000）
//
// 不迁出（依赖 dashboardModel 内部状态或 promptRole 共享 style，留待 Step 4(c) render
// 主体迁出）：
//   - formatRoleTag / promptRoleForRole（依赖 promptRoleSystem/User/Assistant 共享 style）
//   - sysEventStyle（依赖 EventCompact 等常量 · 已在 internal/dashboard/event · 可迁但与
//     render 主体一起更自然）
//   - defaultStepFilters / isEventVisible（同上 · 与 render 协同）
//
// 已追加（Step 4(a-2) 后续 commit · 2026-05-04）：
//   - HasExpandableContent / EstimateExpandedLines / EstimateDebugLines —
//     pure step item height 估算（不读 dashboardModel 字段 · 仅依赖 detail/s 参数 ·
//     为后续 Step 4(c) render 主体迁出 timeline.unifiedItemHeight 等做铺垫）
//   - FilteredStepEntries — 按 TimelineState.StepFilters 过滤 StepEntries（pure ·
//     仅依赖 TimelineState · 为后续 timeline render 主体 + dashboard_pane_dispatcher
//     的 F3 step count 计算解耦做铺垫）
//   - UnifiedItemHeight — 估算 unified event 占用行数（pure · 解开 ev.StepEntry
//     避免反向 import event 包 · 接受 *StepEntry + StepDetailCache · 用于 timeline
//     scroll 计算）
//
// **零 cmd/rnix 反向依赖**：本包只 import internal/types + ipc + lipgloss + runewidth
// + stdlib · 与 PR2/PR3/PR4/PR11 Step 4(c) render 迁出包边界一致。
package timeline

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/rnixai/rnix/ipc"
)

// ActionColor 返回 step action type 对应的 lipgloss 显示颜色（与 cmd/rnix.actionColor 等价）。
//
// Story 27-3 mapping (preserved from cmd/rnix · 调色板 hex 字面量沿用 38-2 之前确立的
// 颜色规范 · 不引入 ui.Color* 常量映射避免改变现有视觉)：
//   - tool_call / complete → #6BCB77 (绿)
//   - plan                  → #5B9BD5 (蓝)
//   - text                  → #FFFFFF (白)
//   - spawn                 → #9B59B6 (紫)
//   - specialize            → #4EC9B0 (青)
//   - replan                → #E5C07B (黄)
//   - default               → #FFFFFF (白)
func ActionColor(action string) lipgloss.Color {
	switch action {
	case "tool_call":
		return lipgloss.Color("#6BCB77")
	case "plan":
		return lipgloss.Color("#5B9BD5")
	case "text":
		return lipgloss.Color("#FFFFFF")
	case "complete":
		return lipgloss.Color("#6BCB77")
	case "spawn":
		return lipgloss.Color("#9B59B6")
	case "specialize":
		return lipgloss.Color("#4EC9B0")
	case "replan":
		return lipgloss.Color("#E5C07B")
	default:
		return lipgloss.Color("#FFFFFF")
	}
}

// ActionAbbrev 返回 step action type 在窄屏的缩写（与 cmd/rnix.actionAbbrev 等价）。
//
// Story 27-3 + Story 38-3 AC#3 mapping:
//   - tool_call → "tool"
//   - complete → "done"
//   - specialize → "spec"
//   - text → "x"  (与 conversation role "assistant" 简称对应)
//   - default → action（原样返回）
func ActionAbbrev(action string) string {
	switch action {
	case "tool_call":
		return "tool"
	case "complete":
		return "done"
	case "specialize":
		return "spec"
	case "text":
		return "x"
	default:
		return action
	}
}

// ShortenArgs 取 input 第一行 + 截断到 maxLen rune width（与 cmd/rnix.shortenArgs 等价）。
//
// 用于 timeline summary 列显示工具调用参数：
//   - 多行 input 仅取 \n 前的内容（避免 wrap）
//   - rune-width > maxLen 截断尾部加 "…"（runewidth.Truncate 处理 CJK / emoji）
//   - 边界：input 不含 \n 时，line == input；maxLen <= 0 时由 runewidth 处理
func ShortenArgs(input string, maxLen int) string {
	line, _, _ := strings.Cut(input, "\n")
	if runewidth.StringWidth(line) > maxLen {
		return runewidth.Truncate(line, maxLen-1, "…")
	}
	return line
}

// FormatDefaultLine 从 StepSummaryWire + GetStepDetailResponse 派生 timeline 显示的
// action 与 summary（与 cmd/rnix.formatDefaultLine 等价）。
//
// 优先级：
//  1. detail != nil → action = detail.Action（detail.ToolPath 非空时覆盖）·
//     summary = detail.Summary（空时 fallback ShortenArgs(detail.ToolInput, 60)）
//  2. detail == nil → action = s.Action（s.ToolPath 非空时覆盖）· summary = s.Summary
//
// 该 helper 用于 cmd/rnix renderStepTimeline 单 step 行渲染 fallback 路径（详细 detail
// 未加载或加载失败时）。
func FormatDefaultLine(s ipc.StepSummaryWire, detail *ipc.GetStepDetailResponse) (action, summary string) {
	if detail != nil {
		action = detail.Action
		if detail.ToolPath != "" {
			action = detail.ToolPath
		}
		summary = detail.Summary
		if summary == "" && detail.ToolInput != "" {
			summary = ShortenArgs(detail.ToolInput, 60)
		}
	}
	// Fallback: use StepSummaryWire fields
	if action == "" {
		action = s.Action
		if s.ToolPath != "" {
			action = s.ToolPath
		}
	}
	if summary == "" {
		summary = s.Summary
	}
	return action, summary
}

// FormatTokenCount 格式化 token 数量（dashboard token 显示的单一权威）：
//   - n >= 1_000_000 → "%.2fM"（1.64M）
//   - 1_000 <= n < 1_000_000 → "%.1fk"（14.1k）
//   - 0 <= n < 1000 → "%d"（42 / 999）
//   - n < 0 → 递归处理符号（"-1.5k" / "-1.64M"）
func FormatTokenCount(tokens int) string {
	if tokens == math.MinInt {
		return fmt.Sprintf("%d", tokens)
	}
	if tokens < 0 {
		return "-" + FormatTokenCount(-tokens)
	}
	if tokens >= 1_000_000 {
		return fmt.Sprintf("%.2fM", float64(tokens)/1_000_000.0)
	}
	if tokens >= 1000 {
		return fmt.Sprintf("%.1fk", float64(tokens)/1000.0)
	}
	return fmt.Sprintf("%d", tokens)
}

// FormatDurationMs 格式化毫秒级 duration（与 cmd/rnix.formatTimelineDuration /
// trace.FormatDurationMs / eval.FormatDurationMs 等价）：
//   - ms < 1 → 微秒（"%.0fµs"）
//   - ms < 1000 → 毫秒（"%.0fms"）
//   - ms >= 1000 → 秒（"%.2fs"）
//
// 自包含（不引用 trace.FormatDurationMs / eval.FormatDurationMs 跨 pane）：纯函数 ·
// 8 行实现 · 多份复制成本远低于跨包依赖耦合（与 eval 同模式）。
func FormatDurationMs(ms float64) string {
	if ms < 1 {
		return fmt.Sprintf("%.0fµs", ms*1000)
	}
	if ms < 1000 {
		return fmt.Sprintf("%.0fms", ms)
	}
	return fmt.Sprintf("%.2fs", ms/1000)
}

// TruncateRuneWidth 截断字符串到 maxWidth display columns（与 cmd/rnix.truncateRuneWidth 等价）。
//
// 使用 runewidth.StringWidth 测量 + runewidth.Truncate 截断（CJK 双宽 / emoji 单宽
// 全部正确处理）· 截断时尾部加 "…"。
func TruncateRuneWidth(s string, maxWidth int) string {
	if runewidth.StringWidth(s) <= maxWidth {
		return s
	}
	return runewidth.Truncate(s, maxWidth-1, "…")
}

// TruncateAnsi 截断 ANSI-styled string by visible width（与 cmd/rnix.truncateAnsi 等价）。
//
// 用 lipgloss.Width 测量可见宽度 + lipgloss.MaxWidth 截断（保留 ANSI escape · 不破坏
// SGR 序列）· profile-tolerant（NoColor profile 下 lipgloss 自动 strip ANSI）。
//
// maxWidth <= 0 → ""（与 cmd/rnix 等价 · 边界保护）。
func TruncateAnsi(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= maxWidth {
		return s
	}
	return lipgloss.NewStyle().MaxWidth(maxWidth).Render(s)
}

// FormatCharCount 格式化 char count（与 cmd/rnix.formatCharCount 等价 · 与 FormatTokenCount
// 公式一致）：
//   - n >= 1000 → "%.1fk"
//   - n < 1000 → "%d"
func FormatCharCount(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000.0)
	}
	return fmt.Sprintf("%d", n)
}

// HasExpandableContent 判断展开 step 是否会显示新信息（与 cmd/rnix.hasExpandableContent 等价）。
//
// 用于 timeline aggregation 渲染决定 step 是否可展开（Story 27-3 三级 detail 控制）：
//   - detail == nil → true（未加载 · 视为潜在可展开 · 避免误隐藏）
//   - detail.ToolPath 与 Level 1 显示的 summary 不同 → true（path 是新信息）
//   - detail.ToolInput / ToolError / ToolResult / RawResponse 任一非空 → true
//   - detail.RequestTokens + ResponseTokens 与 s.TokenCount 不一致 → true（token breakdown 是新信息）
//
// 与 EstimateExpandedLines 配对使用：HasExpandableContent 是判断（bool） ·
// EstimateExpandedLines 是计数（int 行数）· 二者必须保持判断逻辑等价。
func HasExpandableContent(detail *ipc.GetStepDetailResponse, s ipc.StepSummaryWire) bool {
	if detail == nil {
		return true // not loaded yet — treat as potentially expandable
	}
	// ToolPath — new info if different from Level 1 display
	displayedAsSummary := s.Summary
	if s.ToolPath != "" && len(s.Summary) < 8 {
		displayedAsSummary = s.ToolPath
	}
	if detail.ToolPath != "" && detail.ToolPath != displayedAsSummary {
		return true
	}
	// ToolInput
	if detail.ToolInput != "" {
		return true
	}
	// Multi-call view (spec-tool-error-handling-fidelity)
	if len(detail.ToolCalls) >= 2 {
		return true
	}
	// ToolError or ToolResult
	if detail.ToolError != "" || detail.ToolResult != "" {
		return true
	}
	// RawResponse (fallback for any action type)
	if detail.RawResponse != "" {
		return true
	}
	// Token breakdown (only if it differs from Level 1 total)
	if (detail.RequestTokens > 0 || detail.ResponseTokens > 0) && (s.TokenCount == 0 || detail.RequestTokens+detail.ResponseTokens != s.TokenCount) {
		return true
	}
	return false
}

// EstimateExpandedLines 估算展开 step 的行数占用（与 cmd/rnix.estimateExpandedLines 等价）。
//
// 用于 timeline scroll 计算（unifiedItemHeight 调用）· 必须与实际渲染行数对齐：
//   - Path line（仅当 ToolPath 与 Level 1 summary 不同时计 1 行）
//   - Input line（ToolInput 非空时计 1 行 · 单行截断）
//   - Result/Error/RawResponse 行（按 \n 计数 · 上限 4 行 · 含 overflow 标记）
//   - Token line（仅当 token breakdown 与 s.TokenCount 不一致时计 1 行）
//   - Fallback：所有字段 deduped 时至少 1 行（与 HasExpandableContent==true 但内容空的边界对齐）
//
// **不读 dashboardModel 字段**：纯函数，仅依赖 detail/s 参数 · 与 HasExpandableContent
// 的判断逻辑必须保持对齐（HasExpandableContent==false → EstimateExpandedLines 永不被调用 ·
// HasExpandableContent==true → EstimateExpandedLines 至少返回 1）。
func EstimateExpandedLines(detail *ipc.GetStepDetailResponse, s ipc.StepSummaryWire) int {
	n := 0
	// Path — skip when Level 1 already shows it as displaySummary
	displayedAsSummary := s.Summary
	if s.ToolPath != "" && len(s.Summary) < 8 {
		displayedAsSummary = s.ToolPath
	}
	if detail.ToolPath != "" && detail.ToolPath != displayedAsSummary {
		n++ // Path line
	}
	if detail.ToolInput != "" {
		n++ // Input line
	}
	// Multi-call view: each call is one row; >20 triggers truncation to head(10)+summary(1)+tail(5)=16.
	if len(detail.ToolCalls) >= 2 {
		if len(detail.ToolCalls) > 20 {
			n += 16
		} else {
			n += len(detail.ToolCalls)
		}
	} else if detail.ToolError != "" {
		errLines := strings.Count(detail.ToolError, "\n") + 1
		n += min(errLines, 4) // Error: up to 3 lines + overflow
	} else if detail.ToolResult != "" {
		resLines := strings.Count(detail.ToolResult, "\n") + 1
		n += min(resLines, 4) // Result: up to 3 lines + overflow
	} else if detail.RawResponse != "" {
		rawLines := strings.Count(detail.RawResponse, "\n") + 1
		n += min(rawLines, 4) // RawResponse fallback: up to 3 lines + overflow
	}
	// Token breakdown — skip when total already shown in Level 1 and breakdown matches
	if (detail.RequestTokens > 0 || detail.ResponseTokens > 0) && (s.TokenCount == 0 || detail.RequestTokens+detail.ResponseTokens != s.TokenCount) {
		n++ // Token line
	}
	// Fallback line when all content was deduped
	if n == 0 {
		n++ // always at least 1 line for fallback (ToolPath/RawResponse/no-detail)
	}
	return n
}

// EstimateDebugLines 估算 debug level（Level 3）展开行数占用（与
// cmd/rnix.estimateDebugLines 等价）。
//
// 行数构成：
//   - separator 行 + messages header 行（固定 2）
//   - message preview 行（min(msgCount, 6)）
//   - hint 行（固定 1）
//
// **不读 dashboardModel 字段**：纯函数，仅依赖 detail.Messages 长度。
func EstimateDebugLines(detail *ipc.GetStepDetailResponse) int {
	n := 2 // separator + messages header
	msgCount := len(detail.Messages)
	n += min(msgCount, 6) // message preview lines
	n++                   // hint line
	return n
}

// UnifiedItemHeight 估算 unified event 占用行数（与 cmd/rnix.unifiedItemHeight 等价）。
//
// 用于 timeline scroll 计算（renderStepTimeline + ensureStepCursorVisible）：
//   - entry == nil（system event · 例如 budget alert / heartbeat / strace） → 返回 1
//   - entry.Level == LevelSummary（默认 collapsed） → 返回 1
//   - entry.Level >= LevelExpanded → 加上 EstimateExpandedLines（detail nil → +1）
//   - entry.Level >= LevelDebug → 再加上 EstimateDebugLines（detail nil 时跳过）
//
// **签名设计**：解开 event.UnifiedEvent.StepEntry 直接接受 *StepEntry · 避免
// timeline 包反向 import event 包（event 包已 import timeline.StepEntry · 循环依赖
// 防御 · 与 commit 7d1964c event helpers 同模式但反向解耦）。
//
// **不读 dashboardModel 字段**：纯函数，仅依赖 entry 与 cache 参数。
func UnifiedItemHeight(entry *StepEntry, cache map[int]*ipc.GetStepDetailResponse) int {
	if entry == nil {
		return 1 // system events are always single-line
	}
	n := 1
	if entry.Level >= LevelExpanded {
		detail := cache[entry.Summary.Step]
		if detail == nil {
			n++
		} else {
			n += EstimateExpandedLines(detail, entry.Summary)
		}
	}
	if entry.Level >= LevelDebug {
		detail := cache[entry.Summary.Step]
		if detail != nil {
			n += EstimateDebugLines(detail)
		}
	}
	return n
}

// FilteredStepEntries 按 TimelineState.StepFilters 过滤 StepEntries 返回索引切片
// （与 cmd/rnix.filteredStepEntries 等价）。
//
// 过滤逻辑（Story 27-3 + 36-3 IA refactor）：
//   - StepFilters 为 nil 或空 map → 返回全部 indices（[]int{0, 1, ..., n-1}）
//   - StepFilters 所有 value 都为 true → 同上（all-on shortcut · 等价无过滤）
//   - 否则 → 仅返回 StepFilters[entry.Summary.Action] == true 的 indices
//
// 用于 cmd/rnix/dashboard_pane_dispatcher.go::F3 计算「step-only count from
// filtered unified events, not filteredStepEntries」时的对照参考（详见 line 39
// 注释）· 不应用于 unifiedEvents 过滤（FilteredUnifiedEvents 路径）。
//
// **不读 dashboardModel 字段**：纯函数，仅依赖 TimelineState 字段。
func FilteredStepEntries(state TimelineState) []int {
	if len(state.StepFilters) == 0 {
		indices := make([]int, len(state.StepEntries))
		for i := range state.StepEntries {
			indices[i] = i
		}
		return indices
	}
	// Check if all filters are on
	allOn := true
	for _, v := range state.StepFilters {
		if !v {
			allOn = false
			break
		}
	}
	if allOn {
		indices := make([]int, len(state.StepEntries))
		for i := range state.StepEntries {
			indices[i] = i
		}
		return indices
	}
	var result []int
	for i, e := range state.StepEntries {
		if state.StepFilters[e.Summary.Action] {
			result = append(result, i)
		}
	}
	return result
}
