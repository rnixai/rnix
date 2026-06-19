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

// FilterDebugEvents 返回 debug pane 当前可见的事件子集（与 cmd/rnix.filteredDebugEvents
// 1:1 行为等价 · Story 38-5 PR11 Step 4(c) debug pane filter helper · 同
// AppendStraceEvent / UpdateDeviceLatency / ClampCursor functional 设计模式）。
//
// **行为契约（不变性 · 与 cmd/rnix 端 1:1 等价）**：
//   - len(state.Events) == 0 → 返回 nil（短路 · 与 cmd/rnix 一致）
//   - ev.Type == EventSyscall + state.ShowStrace == false → 跳过（隐藏 syscall）
//   - !event.IsEventVisible(ev, stepFilters) → 跳过（按 timeline filter 过滤）
//   - 其他 → 加入 result
//
// **签名设计**：
//   - 接受 DebugState（仅读 Events / ShowStrace · 不修改 state）；
//   - stepFilters 通过参数注入（避免 debug → timeline 反向 import · 与
//     event.IsEventVisible / event.FilterUnifiedEvents 同 stepFilters 注入模式）；
//   - 返回 `[]event.UnifiedEvent` 与 cmd/rnix wrapper 的 `[]UnifiedEvent` 兼容
//     （UnifiedEvent 是 alias `event.UnifiedEvent`）。
//
// **依赖**：
//   - event.IsEventVisible（已就位 · PR11 Step 4(a-2) helpers.go:166）；
//   - event.UnifiedEvent + event.EventSyscall（已就位 · PR11 Step 4(a)）。
//
// **使用场景**：
//   - cmd/rnix.filteredDebugEvents wrapper（dashboard_debug.go × 3 callsite ·
//     clampDebugCursor / renderDebugTimelineContent / handleDebugKey）；
//   - PR11 Step 4(c) 后续 debug render 主体迁出共用此 helper。
func FilterDebugEvents(state DebugState, stepFilters map[string]bool) []event.UnifiedEvent {
	if len(state.Events) == 0 {
		return nil
	}
	var result []event.UnifiedEvent
	for _, ev := range state.Events {
		if ev.Type == event.EventSyscall && !state.ShowStrace {
			continue
		}
		if !event.IsEventVisible(ev, stepFilters) {
			continue
		}
		result = append(result, ev)
	}
	return result
}

// UpdateDeviceLatency 累加单条 syscall 到 device 级延迟统计（与 cmd/rnix.updateDeviceLatency
// 1:1 行为等价 · Story 38-5 PR11 Step 4(c) debug pane strace pipeline helper · 与
// AppendStraceEvent / ClampCursor 同 functional state mutator 模式）。
//
// **行为契约（不变性 · 与 cmd/rnix 端 1:1 等价）**：
//   - ExtractDeviceName(sew) == "" → 返回 state 不变（路径无 /dev/ 前缀且为空 · 无法归类）；
//   - state.DeviceLatency == nil → 自动初始化为空 map（nil safe · cmd/rnix 端
//     newDashboardModel 已 make · 此处保险）；
//   - DeviceLatency[dev] == nil → 初始化新 *DeviceLatencyStats 后挂入 map；
//   - stats.Count++ / stats.TotalMs += sew.DurationMs 累计；
//   - sew.Error != "" → stats.ErrorCount++ 累计错误；
//   - 其他 DebugState 字段（Mode/Cursor/StraceEvents 等）一律保留。
//
// **签名设计**：
//   - 接受 DebugState（值类型 · 仅修改 DeviceLatency map · 返回新状态）；
//   - 与 AppendStraceEvent / ClampCursor 同模式（functional state mutator）；
//   - 内部调用 ExtractDeviceName（debug 包内 cohesion · 0 cmd/rnix 反向依赖）；
//   - cmd/rnix wrapper 一行 `m.debugState = UpdateDeviceLatency(m.debugState, sew)`。
//
// **使用场景**：
//   - cmd/rnix.updateDeviceLatency wrapper（dashboard_debug.go × 2 callsite ·
//     debugStraceMsg + debugStraceStreamErrMsg 两条 strace 事件路径 · 与
//     appendStraceEvent 同 callsite）；
//   - PR11 Step 4(c) 后续 debug render 主体迁出共用此 helper。
//
// **指针语义说明**：DeviceLatencyStats 通过 `*DeviceLatencyStats` 存于 map · 因此
// 累加 (`stats.Count++`) 透过指针修改即时生效 · 即使 state 是值类型 · 内部 map
// 是 reference 类型 · 与 cmd/rnix 端语义完全等价。
func UpdateDeviceLatency(state DebugState, sew ipc.SyscallEventWire) DebugState {
	dev := ExtractDeviceName(sew)
	if dev == "" {
		return state
	}
	if state.DeviceLatency == nil {
		state.DeviceLatency = make(map[string]*DeviceLatencyStats)
	}
	stats := state.DeviceLatency[dev]
	if stats == nil {
		stats = &DeviceLatencyStats{}
		state.DeviceLatency[dev] = stats
	}
	stats.Count++
	stats.TotalMs += sew.DurationMs
	if sew.Error != "" {
		stats.ErrorCount++
	}
	return state
}

// AppendStraceEvent 把 strace UnifiedEvent 追加到 state.StraceEvents 环形缓冲，
// 并在长度超过 MaxStraceEvents 时按 FIFO 截断（与 cmd/rnix.appendStraceEvent
// 1:1 行为等价 · Story 38-5 PR11 Step 4(c) debug pane strace pipeline helper）。
//
// **行为契约（不变性 · 与 cmd/rnix 端 1:1 等价）**：
//   - len(StraceEvents) < MaxStraceEvents → 直接 append（无截断）；
//   - len(StraceEvents) == MaxStraceEvents → append 后保留末尾 MaxStraceEvents 条
//     （丢弃最早一条 · 与 cmd/rnix 端 `events[len-Max:]` 完全等价）；
//   - StraceEvents == nil → append 自动初始化（Go append 语义 · nil safe）；
//   - 其他 DebugState 字段（Mode/Cursor/ScrollTop/AttachedPID 等）一律保留。
//
// **签名设计**：
//   - 接受 DebugState（值类型 · 仅修改 StraceEvents · 返回新状态）；
//   - 与 ClampCursor 同模式（functional state mutator）；
//   - cmd/rnix wrapper 一行 `m.debugState = AppendStraceEvent(m.debugState, ev)`。
//
// **使用场景**：
//   - cmd/rnix.appendStraceEvent wrapper（dashboard_debug.go × 2 callsite ·
//     debugStraceMsg + debugStraceStreamErrMsg 两条 strace 事件路径）；
//   - PR11 Step 4(c) 后续 debug render 主体迁出共用此 helper（avoid ring buffer
//     代码在 cmd/rnix 与 debug 包重复实现）。
func AppendStraceEvent(state DebugState, ev event.UnifiedEvent) DebugState {
	state.StraceEvents = append(state.StraceEvents, ev)
	if len(state.StraceEvents) > MaxStraceEvents {
		state.StraceEvents = state.StraceEvents[len(state.StraceEvents)-MaxStraceEvents:]
	}
	return state
}

// ClampCursor 调整 state.Cursor / state.ScrollTop 让 cursor 在 [0, filteredLen)
// 范围内（与 cmd/rnix.clampDebugCursor 1:1 行为等价 · Story 38-5 PR11 Step 4(c)
// debug pane 视图状态 helper）。
//
// **行为契约（不变性 · 与 cmd/rnix 端 1:1 等价）**：
//   - state.Cursor >= filteredLen → Cursor = max(filteredLen-1, 0)（clamp 到末尾或 0）
//   - state.ScrollTop > state.Cursor → ScrollTop = Cursor（防止滚动位置超过 cursor）
//   - filteredLen == 0 + Cursor != 0 → Cursor = 0 + ScrollTop = 0（空列表防御）
//   - filteredLen >= Cursor → Cursor 不变（Cursor 在范围内）
//
// **签名设计**：
//   - 接受 DebugState（值类型 · 仅修改 Cursor / ScrollTop · 返回新状态）
//   - filteredLen 通过参数传入 · 让 debug 包零 cmd/rnix 反向依赖
//   - cmd/rnix wrapper 计算 filtered 后调用 · 与 timeline.EnsureStepCursorVisible 同模式
//
// **使用场景**：
//   - cmd/rnix.clampDebugCursor wrapper（dashboard_debug.go × 3 callsite）
//   - PR11 Step 4(c) 后续 debug render 主体迁出共用此 helper
func ClampCursor(state DebugState, filteredLen int) DebugState {
	if state.Cursor >= filteredLen {
		state.Cursor = max(filteredLen-1, 0)
	}
	if state.ScrollTop > state.Cursor {
		state.ScrollTop = state.Cursor
	}
	return state
}

// MaxThinkingExpandLines 限定单个展开思考块投影出的正文行上限（Story 60.2 AC#2
// 「可截断/限高」· 防止 6.5KB 思考全文一次性铺满 pane）。
const MaxThinkingExpandLines = 12

// thinkingExpandWrapWidth — 展开正文行的软换行宽度（rune 计 · 有界 debug 视图够用）。
const thinkingExpandWrapWidth = 80

// CollapseThinkingGroups 把 raw（已过滤）debug 事件投影为「显示行」切片：每段连续
// DriverThinking syscall 事件折叠成**单个** EventThinking 摘要行（防刷屏核心 · API
// driver 单会话可达 14841 条 → 折叠后仅 1 行）；已展开的块（key 命中 expanded）额外
// 投影出有界的思考正文行。非 DriverThinking 事件原样透传（AC#4 · 逐字节不变）。
//
// 返回切片即 debug pane 的 cursor / scroll / render 操作对象（经 filteredDebugEvents
// 接入）→ cursor 数学保持一致：折叠块是单个可导航行 · 展开仅追加有界行数。
//
// 行为契约：
//   - 无 DriverThinking 事件（groups 空）→ 直接返回 raw（零拷贝 · 等价无折叠）；
//   - 摘要行：Type=EventThinking · RawEvent!=nil（携带块首事件 ts 作展开键）·
//     Summary="<fold mark> <FormatThinkingSummary>" · Detail=思考全文；
//   - 正文行（仅展开时）：Type=EventThinking · RawEvent==nil · Summary=缩进后分行文本。
func CollapseThinkingGroups(raw []event.UnifiedEvent, expanded map[int64]bool, ascii bool) []event.UnifiedEvent {
	groups := event.BuildThinkingAggGroups(raw)
	if len(groups) == 0 {
		return raw
	}
	foldMark, openMark := "▶", "▼"
	if ascii {
		foldMark, openMark = ">", "v"
	}
	out := make([]event.UnifiedEvent, 0, len(raw))
	gi := 0
	for i := 0; i < len(raw); {
		if gi < len(groups) && groups[gi].StartIdx == i {
			g := groups[gi]
			gi++
			var key int64
			if raw[g.StartIdx].RawEvent != nil {
				key = raw[g.StartIdx].RawEvent.TimestampMs
			}
			isOpen := expanded[key]
			mark := foldMark
			if isOpen {
				mark = openMark
			}
			fullText := event.ReconstructThinkingText(raw, g)
			out = append(out, event.UnifiedEvent{
				Type:        event.EventThinking,
				Severity:    event.SevInfo,
				Summary:     mark + " " + event.FormatThinkingSummary(g, ascii),
				Detail:      fullText,
				RawEvent:    &ipc.SyscallEventWire{Syscall: "DriverThinking", TimestampMs: key},
				IsSynthetic: true,
			})
			if isOpen {
				out = append(out, thinkingExpandRows(fullText, ascii)...)
			}
			i = g.EndIdx
			continue
		}
		out = append(out, raw[i])
		i++
	}
	return out
}

// thinkingExpandRows 把思考全文拆成有界的缩进正文行（每行一个合成 EventThinking
// 显示行 · RawEvent==nil 以区别于摘要行）。超过 MaxThinkingExpandLines 时截断并附尾标。
func thinkingExpandRows(text string, ascii bool) []event.UnifiedEvent {
	const indent = "    "
	mkRow := func(s string) event.UnifiedEvent {
		return event.UnifiedEvent{Type: event.EventThinking, Severity: event.SevInfo, Summary: indent + s, IsSynthetic: true}
	}
	if text == "" {
		return []event.UnifiedEvent{mkRow("(no thinking text)")}
	}
	lines := wrapThinkingText(text, thinkingExpandWrapWidth)
	truncated := false
	if len(lines) > MaxThinkingExpandLines {
		lines = lines[:MaxThinkingExpandLines]
		truncated = true
	}
	rows := make([]event.UnifiedEvent, 0, len(lines)+1)
	for _, ln := range lines {
		rows = append(rows, mkRow(ln))
	}
	if truncated {
		ell := "…"
		if ascii {
			ell = "..."
		}
		rows = append(rows, mkRow(ell+" (truncated)"))
	}
	return rows
}

// wrapThinkingText 按 "\n" 分段并对每段做 rune 计宽的硬换行（width<1 归一化为 1）。
func wrapThinkingText(text string, width int) []string {
	if width < 1 {
		width = 1
	}
	var out []string
	for para := range strings.SplitSeq(text, "\n") {
		r := []rune(para)
		if len(r) == 0 {
			out = append(out, "")
			continue
		}
		for len(r) > width {
			out = append(out, string(r[:width]))
			r = r[width:]
		}
		out = append(out, string(r))
	}
	return out
}
