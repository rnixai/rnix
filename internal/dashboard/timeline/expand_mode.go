// Package timeline — expand_mode.go (Story 38-5 PR11 Step 4(c))
//
// ApplyExpandMode + ToggleSortDirection 实现 Story 36-4 的 e/E/C/o 4 键
// expand mode + 排序切换的核心 state mutation 逻辑（cmd/rnix.handleTimelineKey
// 4 个 case 分支主体迁出 · 不含 m.statusMsgTTL 副作用 · 状态消息作为返回值
// 由 cmd/rnix wrapper 写入 m.statusMsg）。
package timeline

import (
	"github.com/rnixai/rnix/ipc"
)

// 文案常量（公开供测试断言文案稳定性 · 便于将来 i18n 时集中管理）。
const (
	ExpandStatusAll          = "Expand mode: all"
	ExpandStatusAllNoSteps   = "Expand mode: all (no steps yet)"
	ExpandStatusErrorsOnly   = "Expand mode: errors only"
	ExpandStatusCollapsed    = "Expand mode: collapsed"
	SortStatusAscending      = "Timeline 已切换到升序（旧→新）"
	SortStatusDescending     = "Timeline 已切换到降序（新→旧）"
)

// HasExpandableContentFn 抽象 timeline.HasExpandableContent 签名，让测试可注入
// fake 验证 detail==nil / detail+expandable 双路径。生产路径默认使用
// HasExpandableContent。
type HasExpandableContentFn func(detail *ipc.GetStepDetailResponse, s ipc.StepSummaryWire) bool

// ApplyExpandMode 把 timeline state 切换到指定 expand mode 并应用对应的 step
// level 调整。
//
// 行为契约（与 cmd/rnix.handleTimelineKey e/E/C 分支等价）：
//   - ExpandModeExpanded: 遍历 StepEntries · level<Expanded 且 (detail==nil ||
//     hasExpandable(detail, summary)) → 升级为 Expanded · 计数 expanded;
//     若 expanded==0 && len==0 → 返回 ExpandStatusAllNoSteps（早期空 list 提示）;
//     否则 → ExpandStatusAll;
//   - ExpandModeErrorsOnly: 遍历 StepEntries · HasError → Expanded / else → Summary;
//   - ExpandModeCollapsed: 全部 → Summary;
//   - 其他 mode 值: 仅设 ExpandMode · 不调整 entries · 返回空 statusMsg.
//
// hasExpandable 为 nil 时使用包内 HasExpandableContent（生产路径 · 测试可注入 fake）。
func ApplyExpandMode(state TimelineState, mode TimelineExpandMode, hasExpandable HasExpandableContentFn) (newState TimelineState, statusMsg string) {
	state.ExpandMode = mode
	if hasExpandable == nil {
		hasExpandable = HasExpandableContent
	}
	switch mode {
	case ExpandModeExpanded:
		expanded := 0
		for i := range state.StepEntries {
			entry := &state.StepEntries[i]
			if entry.Level < LevelExpanded {
				detail := state.StepDetailCache[entry.Summary.Step]
				if detail == nil || hasExpandable(detail, entry.Summary) {
					entry.Level = LevelExpanded
					expanded++
				}
			}
		}
		if expanded == 0 && len(state.StepEntries) == 0 {
			return state, ExpandStatusAllNoSteps
		}
		return state, ExpandStatusAll
	case ExpandModeErrorsOnly:
		for i := range state.StepEntries {
			entry := &state.StepEntries[i]
			if entry.Summary.HasError {
				entry.Level = LevelExpanded
			} else {
				entry.Level = LevelSummary
			}
		}
		return state, ExpandStatusErrorsOnly
	case ExpandModeCollapsed:
		for i := range state.StepEntries {
			state.StepEntries[i].Level = LevelSummary
		}
		return state, ExpandStatusCollapsed
	}
	return state, ""
}

// ToggleSortDirection 切换 SortAsc 标志并返回对应 status message。
//
// 行为契约（与 cmd/rnix.handleTimelineKey "o" 分支等价）：
//   - SortAsc: false → true → SortStatusAscending（旧→新 · 升序 · 最新在底）;
//   - SortAsc: true → false → SortStatusDescending（新→旧 · 降序 · 最新在顶）.
func ToggleSortDirection(state TimelineState) (newState TimelineState, statusMsg string) {
	state.SortAsc = !state.SortAsc
	if state.SortAsc {
		return state, SortStatusAscending
	}
	return state, SortStatusDescending
}
