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
//   - hasExpandableContent / estimateExpandedLines / estimateDebugLines（依赖
//     StepSummaryWire 字段比对 · 与 render 协同）
//
// **零 cmd/rnix 反向依赖**：本包只 import internal/types + ipc + lipgloss + runewidth
// + stdlib · 与 PR2/PR3/PR4/PR11 Step 4(c) render 迁出包边界一致。
package timeline

import (
	"fmt"
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

// FormatTokenCount 格式化 token 数量（与 cmd/rnix.formatTokenCount 等价）：
//   - n >= 1000 → "%.1fk"（1.5k）
//   - n < 1000 → "%d"（500）
func FormatTokenCount(tokens int) string {
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
