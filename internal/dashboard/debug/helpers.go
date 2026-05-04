// Package debug — helpers.go (Story 38-5 PR11 Step 4(a-2) debug pane 纯 helpers
// 迁出 · 同 alertstrip / timeline Step 4(a-2) pattern · spec § 04 风险 5 中断保险
// 原则把 836 行迁出拆为多个独立 ship-and-revert-able 增量)
//
// 本文件迁出 debug pane 的 pure helpers（无 dashboardModel 状态依赖 · 仅依赖
// internal/dashboard/event + internal/ui + ipc + lipgloss + stdlib）：
//
//   - MaxStraceEvents 公开常量（与 cmd/rnix.maxDebugStraceEvents 等价）
//   - ExtractDeviceName — 从 syscall args 抽取 device 短名（用于 device latency 聚合）
//   - RenderSyscallLine — 单行 syscall 渲染（与 cursor mark + truncate 协作）
//   - RenderStepLine — debug timeline 中 step 事件单行渲染
//
// **包归属决策**：debug 子 Model 包已存在（PR11 Step 1-3 落地），这些 helpers 自然
// 与 debug pane render 协同 · 放在 debug 包让后续 render 主体迁出（Step 4(c)）
// 直接调用而无跨包反向依赖。
//
// 包边界：本文件不 import cmd/rnix · 仅依赖 internal/dashboard/event +
// internal/ui + ipc + lipgloss + stdlib。
package debug

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/dashboard/event"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
)

// MaxStraceEvents 是 debug timeline 中保留的 strace event 上限（ring buffer 上限 ·
// 与 cmd/rnix.maxDebugStraceEvents = 500 等价）。
//
// 公开常量供 cmd/rnix 端 alias 兼容 + 未来 debug render 主体迁出后直接使用。
const MaxStraceEvents = 500

// ExtractDeviceName 从 syscall args 抽取 device 短名（与 cmd/rnix.extractDeviceName 等价）。
//
// 行为契约：
//   - args["path"] 不存在 OR == "" → 返回 ""
//   - path 以 "/dev/" 前缀 → 取第一个 segment（如 "/dev/llm/claude" → "llm" ·
//     "/dev/fs" → "fs" · "/dev/shell" → "shell" · "/dev/mcp/xxx" → "mcp"）
//   - 其他 path → 原样返回（fallback · 用于非 /dev/ 路径的 fallback 显示）
//
// 用于 updateDeviceLatency 聚合 device 级延迟统计（Story 34.6 strace fusion）。
func ExtractDeviceName(sew ipc.SyscallEventWire) string {
	path := ""
	if p, ok := sew.Args["path"]; ok {
		path = fmt.Sprintf("%v", p)
	}
	if path == "" {
		return ""
	}
	// Extract device from paths like /dev/llm/claude, /dev/fs, /dev/shell, /dev/mcp/xxx
	if strings.HasPrefix(path, "/dev/") {
		parts := strings.SplitN(path[5:], "/", 2)
		return parts[0]
	}
	return path
}

// RenderSyscallLine 渲染单行 syscall event（与 cmd/rnix.renderSyscallLine 等价）。
//
// 行为契约：
//   - SevError 及以上 → ColorError (红)
//   - 其他 Severity → ColorMuted (灰)
//   - 输出格式："{cursorMark}{styled summary}"（cursorMark 由 caller 注入 ·
//     如 "▸ " 或 "  "）
//
// **maxWidth 参数当前未使用**（与 cmd/rnix 行为一致 · cmd/rnix 端在外层
// truncateAnsi 包裹 · 不在内层裁剪 · 保留参数签名以便后续扩展且不破坏 caller）。
func RenderSyscallLine(ev event.UnifiedEvent, cursorMark string, _ int) string {
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))
	if ev.Severity >= event.SevError {
		style = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError))
	}

	return fmt.Sprintf("%s%s", cursorMark, style.Render(ev.Summary))
}

// RenderStepLine 渲染 debug timeline 中的 step event 单行（与 cmd/rnix.renderDebugStepLine 等价）。
//
// 行为契约：
//   - StepEntry == nil → 仅 cursorMark + Summary
//   - StepEntry != nil → "{cursorMark}#{step} {action} {summary}{errMark}"
//   - ToolPath 非空且 Summary 长度 < 8 → 用 ToolPath 替代 Summary（防止短摘要不足以
//     表达工具路径）
//   - HasError == true → 末尾追加 "{ColorError ✗}" 错误标记
//
// **maxWidth 参数当前未使用**（与 cmd/rnix 行为一致 · 同 RenderSyscallLine）。
func RenderStepLine(ev event.UnifiedEvent, cursorMark string, _ int) string {
	if ev.StepEntry == nil {
		return cursorMark + ev.Summary
	}
	s := ev.StepEntry.Summary
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	stepLabel := dimStyle.Render(fmt.Sprintf("#%d", s.Step))

	action := s.Action
	summary := s.Summary
	if s.ToolPath != "" && len(s.Summary) < 8 {
		summary = s.ToolPath
	}

	errMark := ""
	if s.HasError {
		errMark = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError)).Render(" ✗")
	}

	return fmt.Sprintf("%s%s %s %s%s", cursorMark, stepLabel, action, summary, errMark)
}
