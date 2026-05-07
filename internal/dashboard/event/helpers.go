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
