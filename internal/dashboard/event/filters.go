// Package event — filters.go (Story 38-5 PR11 Step 4(c))
//
// Step filter key handling 主体迁出 · cmd/rnix.handleStepFilterKey 的纯
// filter map 切换逻辑（不含 m.statusMsg 副作用 · 由 cmd/rnix wrapper 保留）。
package event

// FilterHelpMsg 是 default 分支显示的帮助文案，cmd/rnix wrapper 据 ApplyStepFilterKey
// 返回的 showHelp 设置 m.statusMsg。公开常量便于测试断言文案稳定性。
const FilterHelpMsg = "Filter: t/p/a/c/s/r/z (step) | C/b/x/X/T/i (sys) | * all | Esc exit"

// ApplyStepFilterKey 处理 Step Filter 输入态下的按键切换。
//
// 参数：
//   - filters: 当前 step filter map（nil 则懒分配为 DefaultStepFilters）;
//   - filterMode: 当前是否处于 filter 输入模式;
//   - key: 输入按键字符串.
//
// 返回：
//   - newFilters: 更新后的 filter map（可能与入参共享底层 map · in-place toggle）;
//   - newFilterMode: 更新后的 filter 模式（按 f/esc 切回 false）;
//   - showHelp: default 分支命中（未识别按键）· cmd/rnix wrapper 据此显示
//     FilterHelpMsg.
//
// 行为契约（与 cmd/rnix.handleStepFilterKey 等价）：
//   - Row 1 step actions: t/p/a/c/s/r/z → 切换 tool_call/plan/text/complete/spawn/replan/specialize;
//   - Row 2 system events: C/b/x/X/T/i → 切换 EventCompact/EventBudget/sys_spawn/EventExit/EventStall/EventImmune;
//   - "*" → 重置为 DefaultStepFilters（全部启用）;
//   - "f"/"esc" → 退出 filter 输入态;
//   - 其他 → showHelp=true（cmd/rnix wrapper 显示帮助文案）.
func ApplyStepFilterKey(filters map[string]bool, filterMode bool, key string) (newFilters map[string]bool, newFilterMode bool, showHelp bool) {
	if filters == nil {
		filters = DefaultStepFilters()
	}
	switch key {
	// Row 1: Step action types (lowercase)
	case "t":
		filters["tool_call"] = !filters["tool_call"]
	case "p":
		filters["plan"] = !filters["plan"]
	case "a":
		filters["text"] = !filters["text"]
	case "c":
		filters["complete"] = !filters["complete"]
	case "s":
		filters["spawn"] = !filters["spawn"]
	case "r":
		filters["replan"] = !filters["replan"]
	case "z":
		filters["specialize"] = !filters["specialize"]
	// Row 2: System event types
	case "C":
		filters[EventCompact] = !filters[EventCompact]
	case "b":
		filters[EventBudget] = !filters[EventBudget]
	case "x":
		filters["sys_spawn"] = !filters["sys_spawn"]
	case "X":
		filters[EventExit] = !filters[EventExit]
	case "T":
		filters[EventStall] = !filters[EventStall]
	case "i":
		filters[EventImmune] = !filters[EventImmune]
	case "*":
		filters = DefaultStepFilters()
	case "f", "esc":
		filterMode = false
	default:
		showHelp = true
	}
	return filters, filterMode, showHelp
}
