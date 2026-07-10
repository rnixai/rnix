// Package event — helpers.go (Story 38-5 PR11 Step 4(a-2) timeline event-related
// helpers 迁出 · 续 helpers.go pure helpers commit · 同 alertstrip Step 4(a-2)
// pattern)
//
// 本文件迁出依赖 UnifiedEvent + Event* 常量的 timeline 辅助函数（无 dashboardModel
// 状态依赖 · 仅依赖 internal/ui + lipgloss + 本包自身的 UnifiedEvent + Severity 类型）：
//
//   - SysEventStyle — system event 类型 → lipgloss style 映射
//   - DefaultStepFilters — 默认 step filter map（全部 action / system event 启用）
//   - IsEventVisible — UnifiedEvent 是否通过 filter 检查
//   - BuildToolAggGroups + ToolAggGroup struct + AggThreshold const —
//     连续相同 ToolPath step 的语义聚合（非 bulk 模式 · <100 steps）
//
// **包归属决策**（spec § 04 风险 3 缓解）：
// 这些 helpers 操作 UnifiedEvent 类型 · 与 event 包同 type universe · 放在 event 包
// 避免 timeline → event 反向依赖循环（event.UnifiedEvent.StepEntry 已是
// *timeline.StepEntry · 把 helpers 放 timeline 会造成 import cycle）。
//
// 包边界：本文件不 import cmd/rnix · 仅依赖 internal/ui + lipgloss + ipc + stdlib ·
// 与 event.go 同模式。
package event

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/ui"
)

// AggThreshold 是 timeline 语义聚合的最小连续 step 数（非 bulk 模式 · <100 steps）。
//
// 与 cmd/rnix.timelineAggThreshold = 3 等价 · 公开常量供 cmd/rnix 端 alias 兼容
// + 测试断言（dashboard_test.go 等）使用。
const AggThreshold = 3

// ToolAggGroup 表示一段连续的、共享相同 ToolPath 的 step 事件
// （与 cmd/rnix.toolAggGroup 等价）。
//
// 字段语义（公开 · 与原 cmd/rnix struct 字段语义一致）：
//   - StartIdx：filtered unified events 中的起始下标；
//   - EndIdx：exclusive 结束下标；
//   - ToolPath：本组共享的 ToolPath；
//   - StepNums：本组各 step 的 step 编号（用于显示 "step 12-15"）。
type ToolAggGroup struct {
	StartIdx int
	EndIdx   int
	ToolPath string
	StepNums []int
}

// BuildToolAggGroups 扫描 unified events 识别连续相同 ToolPath 的 step events
// （≥ AggThreshold）并返回聚合分组（与 cmd/rnix.buildToolAggGroups 等价）。
//
// 行为契约：
//   - 跳过 StepEntry==nil 或 ToolPath=="" 的 event；
//   - 仅当连续 step ≥ AggThreshold (=3) 时形成一个 group；
//   - 否则各 step 独立显示（不合并）。
//
// 用于 timeline aggregated 模式渲染（renderAggregatedTimeline · cmd/rnix 端
// 仍持有 render 主体）。
func BuildToolAggGroups(events []UnifiedEvent) []ToolAggGroup {
	var groups []ToolAggGroup
	n := len(events)
	i := 0
	for i < n {
		ev := events[i]
		if ev.StepEntry == nil || ev.StepEntry.Summary.ToolPath == "" {
			i++
			continue
		}
		tp := ev.StepEntry.Summary.ToolPath
		runStart := i
		var stepNums []int
		stepNums = append(stepNums, ev.StepEntry.Summary.Step)
		j := i + 1
		for j < n {
			ej := events[j]
			if ej.StepEntry == nil || ej.StepEntry.Summary.ToolPath != tp {
				break
			}
			stepNums = append(stepNums, ej.StepEntry.Summary.Step)
			j++
		}
		if len(stepNums) >= AggThreshold {
			groups = append(groups, ToolAggGroup{
				StartIdx: runStart,
				EndIdx:   j,
				ToolPath: tp,
				StepNums: stepNums,
			})
		}
		i = j
	}
	return groups
}

// SysEventStyle 返回 system event 类型对应的 lipgloss style
// （与 cmd/rnix.sysEventStyle 等价）。
//
// 颜色映射 (preserved from cmd/rnix · 与 38-2 / 34.1 / 34.6 落地一致)：
//   - EventCompact → #00CED1 (cyan) Bold （Story 34.1 compact event）
//   - EventBudget  → ColorWarning (黄)
//   - EventSpawn   → ColorSuccess (绿)
//   - EventExit    → SevError 时 ColorError (红) / 否则 ColorMuted (灰)
//   - EventStall   → ColorWarning Bold
//   - EventImmune  → #9B59B6 (紫)
//   - default      → ColorMuted (灰)
func SysEventStyle(ev UnifiedEvent) lipgloss.Style {
	switch ev.Type {
	case EventCompact:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#00CED1")).Bold(true)
	case EventBudget:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorWarning))
	case EventSpawn:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorSuccess))
	case EventExit:
		if ev.Severity >= SevError {
			return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError))
		}
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))
	case EventStall:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorWarning)).Bold(true)
	case EventImmune:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#9B59B6"))
	case EventScript:
		if ev.Severity >= SevError {
			return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError))
		}
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#5B9BD5"))
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))
	}
}

// DefaultStepFilters 返回所有 action types 与 system event types 默认启用的 filter map
// （与 cmd/rnix.defaultStepFilters 等价）。
//
// Filter map 结构：
//   - step action filters：tool_call / plan / text / complete / spawn / replan / specialize
//   - system event filters：EventCompact / EventBudget / sys_spawn (F7 区别于 step "spawn") /
//     EventExit / EventStall / EventImmune / EventSyscall (Story 34.6 strace events)
//
// "sys_spawn" 是 F7 落地的特殊 filter key（与 step-action "spawn" 区分 · 防止键位冲突 ·
// IsEventVisible 中显式 routing）。
func DefaultStepFilters() map[string]bool {
	return map[string]bool{
		"tool_call":    true,
		"plan":         true,
		"text":         true,
		"complete":     true,
		"spawn":        true,
		"replan":       true,
		"specialize":   true,
		EventCompact:   true,
		EventBudget:    true,
		"sys_spawn":    true, // F7: distinct from step-action "spawn" filter key
		EventExit:      true,
		EventStall:     true,
		EventImmune:    true,
		EventSyscall:   true, // Story 34.6: strace events in debug mode
		EventScript:    true, // Story 43-3: script trace events from ScriptExecutor
	}
}

// IsEventVisible 检查 UnifiedEvent 是否通过 filter（与 cmd/rnix.isEventVisible 等价）。
//
// 行为契约：
//   - filters == nil OR len == 0 → 全部 visible；
//   - EventStep + StepEntry != nil → 检查 step.Action filter；
//   - EventStep + StepEntry == nil → visible（fallback · 边界）；
//   - EventSpawn → 用 "sys_spawn" filter key（F7 区别于 step "spawn"）；
//   - 其他 system event 类型 → 用 ev.Type filter。
func IsEventVisible(ev UnifiedEvent, filters map[string]bool) bool {
	if len(filters) == 0 {
		return true
	}
	if ev.Type == EventStep {
		if ev.StepEntry != nil {
			return filters[ev.StepEntry.Summary.Action]
		}
		return true
	}
	// F7: system spawn uses distinct filter key "sys_spawn" to avoid collision with step-action "spawn"
	if ev.Type == EventSpawn {
		return filters["sys_spawn"]
	}
	return filters[ev.Type]
}

// FilterUnifiedEvents 按 filters 过滤 events 切片（与 cmd/rnix.filteredUnifiedEvents 等价）。
//
// 优化路径：
//   - filters 为 nil 或空 map → 直接返回原 slice（零拷贝 · 等价无过滤）
//   - 所有 filter value 都为 true → 同上 all-on shortcut（性能优化 · 避免逐项 check）
//   - 否则 → 逐项调 IsEventVisible 过滤
//
// 用于 cmd/rnix renderStepTimeline + ensureStepCursorVisible + dashboard_keylayers
// 的 cursor 计算 · 接受 events slice + filters map 作参数 · 不读 dashboardModel 字段。
//
// **包归属**：放在 event 包而非 timeline 包 · 避免 timeline → event 反向 import 循环
// （event.UnifiedEvent.StepEntry 已 *timeline.StepEntry · 与 commit 7d1964c 同模式）。
func FilterUnifiedEvents(events []UnifiedEvent, filters map[string]bool) []UnifiedEvent {
	if len(filters) == 0 {
		return events
	}
	allOn := true
	for _, v := range filters {
		if !v {
			allOn = false
			break
		}
	}
	if allOn {
		return events
	}
	var result []UnifiedEvent
	for _, ev := range events {
		if IsEventVisible(ev, filters) {
			result = append(result, ev)
		}
	}
	return result
}

// ScriptAggGroup 表示一段连续的、共享相同 stmt_kind 的 EventScript 事件
// （Story 43-3 AC#5 · 与 ToolAggGroup 同 fold pattern · 独立 namespace）。
//
// 字段语义：
//   - StartIdx  : events 切片中的起始下标（inclusive）
//   - EndIdx    : 结束下标（exclusive · 与 ToolAggGroup 同模式）
//   - StmtKind  : 本组共享的 stmt_kind（如 "assign"/"spawn"/"if"）
//   - FirstLine : 段内首个事件的 args["line"]（fold 行 "L10-L14" 的 10）
//   - LastLine  : 段内末个事件的 args["line"]（fold 行 "L10-L14" 的 14）
//   - Count     : 段内事件总条数（包括 begin + end 对 · 5 对 = 10）
type ScriptAggGroup struct {
	StartIdx  int
	EndIdx    int
	StmtKind  string
	FirstLine int
	LastLine  int
	Count     int
}

// BuildScriptAggGroups 扫描 unified events 识别连续相同 stmt_kind 的
// ScriptStmtBegin / ScriptStmtEnd 事件（≥ AggThreshold）并返回聚合分组。
//
// 行为契约（Story 43-3 AC#5）：
//   - 只看 Type==EventScript 的事件；其他类型直接跳过（next idx）
//   - 仅 ScriptStmtBegin / ScriptStmtEnd 参与聚合（ScriptSpawn / ScriptWhileIter
//     / ScriptCondition 是"事件性事件" · 每条独立显示 · 永不聚合）
//   - 含 error 的事件（Severity >= SevError）强制切断聚合并跳过自身（不在任何
//     group 内部 · 错误条目独立显示）
//   - 不同 stmt_kind 不聚合（assign 与 spawn 交替 → 0 group）
//   - 连续 ≥ AggThreshold (=3) 条同 stmt_kind 才形成 group · 否则各事件独立
func BuildScriptAggGroups(events []UnifiedEvent) []ScriptAggGroup {
	var groups []ScriptAggGroup
	n := len(events)
	i := 0
	for i < n {
		ev := events[i]
		if !isAggregatableScriptEvent(ev) {
			i++
			continue
		}
		kind := scriptStmtKindArg(ev)
		firstLine := scriptLineArg(ev)
		lastLine := firstLine
		count := 1
		runStart := i
		j := i + 1
		for j < n {
			ej := events[j]
			if !isAggregatableScriptEvent(ej) {
				break
			}
			if scriptStmtKindArg(ej) != kind {
				break
			}
			lastLine = scriptLineArg(ej)
			count++
			j++
		}
		if count >= AggThreshold {
			groups = append(groups, ScriptAggGroup{
				StartIdx:  runStart,
				EndIdx:    j,
				StmtKind:  kind,
				FirstLine: firstLine,
				LastLine:  lastLine,
				Count:     count,
			})
		}
		i = j
	}
	return groups
}

// isAggregatableScriptEvent reports whether the event participates in
// ScriptAggGroup folding: must be Type==EventScript, syscall in
// {ScriptStmtBegin, ScriptStmtEnd}, Severity < SevError (error events are
// never absorbed — Spec AC#5).
func isAggregatableScriptEvent(ev UnifiedEvent) bool {
	if ev.Type != EventScript || ev.Severity >= SevError || ev.RawEvent == nil {
		return false
	}
	switch ev.RawEvent.Syscall {
	case "ScriptStmtBegin", "ScriptStmtEnd":
		return true
	}
	return false
}

// scriptStmtKindArg pulls args["stmt_kind"] as string (empty when missing).
func scriptStmtKindArg(ev UnifiedEvent) string {
	if ev.RawEvent == nil {
		return ""
	}
	v, ok := ev.RawEvent.Args["stmt_kind"]
	if !ok {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// scriptLineArg pulls args["line"] as int (0 when missing / not numeric).
func scriptLineArg(ev UnifiedEvent) int {
	if ev.RawEvent == nil {
		return 0
	}
	v, ok := ev.RawEvent.Args["line"]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

// =============================================================================
// Story 60.2 — DriverThinking 思考阶段聚合（debug pane 防刷屏）
//
// 以下为 ATDD 红灯期骨架（Story 60.2 · 骨架+t.Skip 机制）：
//   - 类型/签名就位让 helpers_thinking_test.go 可编译；
//   - 函数体仅返回零值（未实现）→ 断言真实行为的测试在 RED 期 t.Skip；
//   - dev-story 移除 skip + 填实逻辑，验 RED→GREEN。
//
// 设计参考 BuildScriptAggGroups(:254)/ScriptAggGroup(:234)：连续同类事件聚合。
// 与 Script 聚合的差异：DriverThinking 事件经 StraceToUnifiedEvent 转为
// Type==EventSyscall（非独立 EventType），故聚合键在 RawEvent.Syscall=="DriverThinking"，
// 而非 ev.Type。分块边界由 args.subtype=="started" 决定。
// =============================================================================

// ThinkingAggGroup 表示一段连续的 DriverThinking 事件聚合块
// （一次思考 = 1 条 subtype=started + N 条 subtype=delta · Story 60.2 AC#1）。
//
// 字段语义（参考 ToolAggGroup(:43) / ScriptAggGroup(:234)）：
//   - StartIdx   : events 切片中的起始下标（inclusive）
//   - EndIdx     : 结束下标（exclusive · 与 ToolAggGroup 同模式）
//   - DeltaCount : 段内 subtype=delta 事件条数（用于摘要 "74 delta"）
//   - TotalBytes : 段内所有 delta content 的累计字节数（用于摘要 "6.5KB"）
//   - DurationMs : 首末事件 ts 之差（毫秒 · 用于摘要 "2.1s" · 不可得时为 0）
type ThinkingAggGroup struct {
	StartIdx   int
	EndIdx     int
	DeltaCount int
	TotalBytes int
	DurationMs float64
}

// BuildThinkingAggGroups 扫描 unified events 识别连续的 DriverThinking 事件段，
// 以 args.subtype=="started" 为新块边界，遇非 DriverThinking 事件结束当前块
// （Story 60.2 AC#1）。
//
// 行为契约：
//   - 只看 ev.RawEvent != nil && ev.RawEvent.Syscall == "DriverThinking" 的事件；
//   - subtype=="started" 标记一个新思考块的起点（在已开块内遇到 started → 断块）；
//   - 遇到非 DriverThinking 事件（DriverToolCall/step 等）则当前块结束；
//   - Args 类型断言失败安全降级（不 panic · 视为无 subtype 的 thinking 事件）。
//
// **聚合阈值决策**（dev-story · 与 tool/script 聚合不同）：思考块用「任意 ≥1 条
// DriverThinking 即成块」而非复用 AggThreshold(=3)。理由：API driver 单次会话
// DriverThinking 可达 14841 条（占总事件 90%+），不存在「保留散落 thinking 行」
// 的价值——防刷屏要求**一律折叠**，哪怕只有 1 条 started。
func BuildThinkingAggGroups(events []UnifiedEvent) []ThinkingAggGroup {
	var groups []ThinkingAggGroup
	n := len(events)
	i := 0
	for i < n {
		if !isThinkingEvent(events[i]) {
			i++
			continue
		}
		// 一个新思考块从 i 开始（i 处的 subtype 通常是 started，但防御降级时可能为空）。
		runStart := i
		firstTs := events[i].RawEvent.TimestampMs
		lastTs := firstTs
		deltaCount := 0
		totalBytes := 0
		if subtype, content := thinkingArgs(events[i]); subtype == "delta" {
			deltaCount++
			totalBytes += len(content)
		}
		j := i + 1
		for j < n {
			if !isThinkingEvent(events[j]) {
				break
			}
			subtype, content := thinkingArgs(events[j])
			if subtype == "started" {
				break // started 是新思考块的边界 → 结束当前块
			}
			if subtype == "delta" {
				deltaCount++
				totalBytes += len(content)
			}
			lastTs = events[j].RawEvent.TimestampMs
			j++
		}
		groups = append(groups, ThinkingAggGroup{
			StartIdx:   runStart,
			EndIdx:     j,
			DeltaCount: deltaCount,
			TotalBytes: totalBytes,
			DurationMs: float64(lastTs - firstTs),
		})
		i = j
	}
	return groups
}

// isThinkingEvent reports whether ev is a DriverThinking syscall event
// (聚合键在 RawEvent.Syscall · 经 StraceToUnifiedEvent 后 Type==EventSyscall)。
func isThinkingEvent(ev UnifiedEvent) bool {
	return ev.RawEvent != nil && ev.RawEvent.Syscall == "DriverThinking"
}

// thinkingArgs 从 DriverThinking 事件的 args 安全抽取 subtype / content
// （类型断言失败降级为空串 · 不 panic · Story 60.2 关键约束「Args 类型防御」）。
func thinkingArgs(ev UnifiedEvent) (subtype, content string) {
	if ev.RawEvent == nil || ev.RawEvent.Args == nil {
		return "", ""
	}
	if v, ok := ev.RawEvent.Args["subtype"]; ok {
		if s, ok := v.(string); ok {
			subtype = s
		}
	}
	if v, ok := ev.RawEvent.Args["content"]; ok {
		if s, ok := v.(string); ok {
			content = s
		}
	}
	return subtype, content
}

// ReconstructThinkingText 按 group 内 events 顺序拼接各 delta 的 args.content，
// 还原该思考块的思考全文；started 标记不入正文（Story 60.2 AC#2）。
//
// 行为契约：
//   - 仅拼接 subtype=="delta" 事件的 content；
//   - 跳过 subtype=="started"（不污染正文）；
//   - 空思考（仅 started · 无 delta）→ 返回 ""；
//   - StartIdx/EndIdx 越界安全 clamp（renderer 边界防御）。
func ReconstructThinkingText(events []UnifiedEvent, g ThinkingAggGroup) string {
	lo := g.StartIdx
	hi := g.EndIdx
	if lo < 0 {
		lo = 0
	}
	if hi > len(events) {
		hi = len(events)
	}
	var b strings.Builder
	for i := lo; i < hi; i++ {
		if subtype, content := thinkingArgs(events[i]); subtype == "delta" {
			b.WriteString(content)
		}
	}
	return b.String()
}

// FormatThinkingSummary 生成折叠思考块的单行摘要文本（Story 60.2 AC#1/AC#3）。
//
// 形如：`💭 thinking (74 delta · 6.5KB · 2.1s)`（Unicode）
//
//	`[think] thinking (74 delta 6.5KB 2.1s)`（ascii==true · RNIX_ASCII=1 · 无
//	Unicode glyph + 无中点分隔符）
//
// ascii 参数由调用方传入（dev 在渲染层 wire ui.IsASCIIMode()）· 保持本函数纯函数可单测。
func FormatThinkingSummary(g ThinkingAggGroup, ascii bool) string {
	icon := "💭"
	sep := " · "
	if ascii {
		icon = "[think]"
		sep = " "
	}
	inner := fmt.Sprintf("%d delta%s%s%s%s",
		g.DeltaCount, sep, formatThinkingBytes(g.TotalBytes), sep, formatThinkingDuration(g.DurationMs))
	return fmt.Sprintf("%s thinking (%s)", icon, inner)
}

// formatThinkingBytes 把字节数格式化为短标签（<1KiB → "NB" · 否则 "X.YKB"）。
func formatThinkingBytes(n int) string {
	if n < 1024 {
		return fmt.Sprintf("%dB", n)
	}
	return fmt.Sprintf("%.1fKB", float64(n)/1024)
}

// formatThinkingDuration 把毫秒格式化为短标签（<1000ms → "Nms" · 否则 "X.Ys"）。
func formatThinkingDuration(ms float64) string {
	if ms < 0 {
		ms = 0 // 乱序/时钟回拨 ts 致负 duration → 钳零，不显示 "-50ms"（Story 60.2 code-review Patch P3）
	}
	if ms < 1000 {
		return fmt.Sprintf("%dms", int(ms))
	}
	return fmt.Sprintf("%.1fs", ms/1000)
}

// =============================================================================
// Story 65.3 — DriverToolCall input_delta 分片聚合（debug pane 防刷屏）
//
// 复用 60.2 BuildThinkingAggGroups 骨骼；单测见 helpers_toolinput_test.go。
//
// 与 60.2 thinking 聚合的结构差异（story 分片事件形态表 + 裁决）：
//   - 分片判据 = RawEvent.Syscall=="DriverToolCall" && args.content=="input_delta"
//     （判 content 不判 subtype——input_delta 分片事件无 subtype 键）；
//   - started 行**不吸进组、原样透传**（裁决 1 · 与 60.2 把 started 吸进组不同）；
//     组 = 连续 input_delta run，遇任何非 input_delta 事件断块；
//   - 工具名不在分片上——扫描时维护「最近一次 content=="started" 的 args.tool」游标，
//     开组时快照进 ToolInputAggGroup.ToolName（裁决 1）；组前无 started → ToolName=""
//     摘要降级（裁决 5）；
//   - TotalBytes = 各分片 len(partial_json) 累计；DurationMs = 组内首末事件 ts 差。
// =============================================================================

// ToolInputAggGroup 表示一段连续的 DriverToolCall input_delta 分片聚合块
// （一次工具输入 = N 条 content==input_delta 分片 · Story 65.3 AC#1）。
//
// 字段语义（参考 ThinkingAggGroup(:373)）：
//   - StartIdx   : events 切片中的起始下标（inclusive · 组首分片）
//   - EndIdx     : 结束下标（exclusive · 与 ThinkingAggGroup 同模式）
//   - ToolName   : 组前最近一次 started 事件的 args.tool（回溯快照 · 无则 ""）
//   - DeltaCount : 段内 input_delta 分片条数（用于摘要 "14 delta"）
//   - TotalBytes : 段内所有分片 partial_json 的累计字节数（用于摘要 "2.3KB"）
//   - DurationMs : 首末分片 ts 之差（毫秒 · 用于摘要 "1.2s" · 不可得时为 0）
type ToolInputAggGroup struct {
	StartIdx   int
	EndIdx     int
	ToolName   string
	DeltaCount int
	TotalBytes int
	DurationMs float64
}

// BuildToolInputAggGroups 扫描 unified events 识别连续的 DriverToolCall input_delta
// 分片段（Story 65.3 AC#1 · 裁决 1/2）。
//
// 行为契约：
//   - 只看分片事件（RawEvent.Syscall==DriverToolCall && args.content==input_delta）；
//   - 遇到任何非 input_delta 事件（started/aggregate/completed/其它 syscall/step）断块；
//   - 任意 ≥1 条分片即成组（裁决 2 · 不用 AggThreshold=3）；
//   - 维护「最近一次 DriverToolCall content==started 的 args.tool」游标 · 开组时快照进
//     ToolName（started 本身透传不入组 · 游标是「最近 started」而非「紧邻前一事件」）；
//   - Args 类型断言失败安全降级（不 panic · 视为无 tool/partial_json · 仿 thinkingArgs(:450)）。
//
func BuildToolInputAggGroups(events []UnifiedEvent) []ToolInputAggGroup {
	var groups []ToolInputAggGroup
	n := len(events)
	lastToolName := "" // 「最近一次 content==started 的 args.tool」游标（裁决 1）
	i := 0
	for i < n {
		if !isToolInputDeltaEvent(events[i]) {
			if tool, content, _ := toolCallArgs(events[i]); content == "started" && events[i].RawEvent != nil && events[i].RawEvent.Syscall == "DriverToolCall" {
				lastToolName = tool
			}
			i++
			continue
		}
		runStart := i
		firstTs := events[i].RawEvent.TimestampMs
		lastTs := firstTs
		deltaCount := 0
		totalBytes := 0
		j := i
		for j < n && isToolInputDeltaEvent(events[j]) {
			_, _, partial := toolCallArgs(events[j])
			deltaCount++
			totalBytes += len(partial)
			lastTs = events[j].RawEvent.TimestampMs
			j++
		}
		groups = append(groups, ToolInputAggGroup{
			StartIdx:   runStart,
			EndIdx:     j,
			ToolName:   lastToolName,
			DeltaCount: deltaCount,
			TotalBytes: totalBytes,
			DurationMs: float64(lastTs - firstTs),
		})
		i = j
	}
	return groups
}

// isToolInputDeltaEvent reports whether ev is a DriverToolCall input_delta 分片
// （判 content 不判 subtype——分片事件无 subtype 键 · aggregate 无 content 键取零值透传）。
func isToolInputDeltaEvent(ev UnifiedEvent) bool {
	if ev.RawEvent == nil || ev.RawEvent.Syscall != "DriverToolCall" {
		return false
	}
	_, content, _ := toolCallArgs(ev)
	return content == "input_delta"
}

// toolCallArgs 从 DriverToolCall 事件的 args 安全抽取 tool / content / partial_json
// （类型断言失败降级为空串 · 不 panic · 仿 thinkingArgs(:450)）。
func toolCallArgs(ev UnifiedEvent) (tool, content, partialJSON string) {
	if ev.RawEvent == nil || ev.RawEvent.Args == nil {
		return "", "", ""
	}
	if s, ok := ev.RawEvent.Args["tool"].(string); ok {
		tool = s
	}
	if s, ok := ev.RawEvent.Args["content"].(string); ok {
		content = s
	}
	if s, ok := ev.RawEvent.Args["partial_json"].(string); ok {
		partialJSON = s
	}
	return tool, content, partialJSON
}

// ReconstructToolInput 按 group 内 events 顺序拼接各分片的 args.partial_json，
// 还原该工具调用的完整输入 JSON 文本（Story 65.3 AC#2 · 裁决 5 · 不做 pretty-print）。
//
// 行为契约：
//   - 仅拼接 input_delta 分片的 partial_json；
//   - 空组（无分片）→ 返回 ""；
//   - StartIdx/EndIdx 越界安全 clamp（对齐 ReconstructThinkingText(:475)）。
func ReconstructToolInput(events []UnifiedEvent, g ToolInputAggGroup) string {
	lo := g.StartIdx
	hi := g.EndIdx
	if lo < 0 {
		lo = 0
	}
	if hi > len(events) {
		hi = len(events)
	}
	var b strings.Builder
	for i := lo; i < hi; i++ {
		if isToolInputDeltaEvent(events[i]) {
			_, _, partial := toolCallArgs(events[i])
			b.WriteString(partial)
		}
	}
	return b.String()
}

// FormatToolInputSummary 生成折叠工具输入块的单行摘要文本（Story 65.3 AC#1/裁决 5）。
//
// 形如：`🔧 fs_write input (14 delta · 2.3KB · 1.2s)`（Unicode）
//
//	`[tool] fs_write input (14 delta 2.3KB 1.2s)`（ascii==true · RNIX_ASCII=1）
//
// ToolName=="" 时降级省略工具名：`🔧 tool input (…)` / `[tool] tool input (…)`。
// icon/分隔符模式照抄 FormatThinkingSummary(:501)（💭→🔧 · [think]→[tool]）；
// bytes/duration 直接复用包内 formatThinkingBytes/formatThinkingDuration（含负 duration 钳零）。
func FormatToolInputSummary(g ToolInputAggGroup, ascii bool) string {
	icon := "🔧"
	sep := " · "
	if ascii {
		icon = "[tool]"
		sep = " "
	}
	name := g.ToolName
	if name == "" {
		name = "tool" // 组前无 started → 工具名降级占位（裁决 5）
	}
	inner := fmt.Sprintf("%d delta%s%s%s%s",
		g.DeltaCount, sep, formatThinkingBytes(g.TotalBytes), sep, formatThinkingDuration(g.DurationMs))
	return fmt.Sprintf("%s %s input (%s)", icon, name, inner)
}
