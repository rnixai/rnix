// Package timeline — apply.go (Story 38-5 PR11 Step 4(c) ApplyNewSteps 迁出)
//
// 本文件迁出 cmd/rnix-private 的 step append 流水线纯函数（与 cmd/rnix.applyNewSteps
// 等价 · 仅依赖 TimelineState + ipc.StepSummaryWire · 与 helpers.go / role.go
// 同模式的 pure function 迁出）。
//
// **零 cmd/rnix 反向依赖**：本包只 import internal/dashboard/timeline 自身（state /
// types）+ ipc · 与 PR11 Step 4(c) 其他迁出包边界一致。
package timeline

import (
	"github.com/rnixai/rnix/ipc"
)

// ApplyNewSteps 将新拉取的 step summary 切片去重后追加到 state.StepEntries，按
// state.ExpandMode 决定每个新 step 的初始 Level（Story 36-4 三态展开模式 +
// errors-always-expand safety net）。
//
// **去重逻辑**：以 step.Step (int) 为去重 key（concurrent fetches 比如 PID-change
// fetch + tick fetch 可能返回 overlapping step ranges · 防止 Timeline 显示重复行）。
//
// **Story 36-4 expandMode 决策**：
//   - ExpandModeExpanded     → Level=LevelExpanded, AutoExpand=true（全部展开）
//   - ExpandModeErrorsOnly   → 仅 HasError 时 Level=LevelExpanded + AutoExpand
//   - ExpandModeCollapsed    → 默认 LevelSummary; HasError safety net 强制展开
//
// **行为契约（不变性 · ATDD 36-4 + 36-3 测试覆盖）**：
//   - 重复 step.Step 跳过（已知 set）
//   - 错误 step 在任何 ExpandMode 下都强制 Level=LevelExpanded + AutoExpand=true
//   - 输入 state 不修改（pure function · 通过返回值传递新 state）
//   - 输入 steps 切片不修改
//
// **性能上界**：O(N + M) where N = len(state.StepEntries), M = len(steps)。
//
// **副作用**：无（pure function · 仅返回新 state）。
//
// 调用方：cmd/rnix.dashboardModel.applyNewSteps thin wrapper（receiver 保留供
// 旧 callsites 零修改）。
func ApplyNewSteps(state TimelineState, steps []ipc.StepSummaryWire) TimelineState {
	known := make(map[int]struct{}, len(state.StepEntries))
	for _, e := range state.StepEntries {
		known[e.Summary.Step] = struct{}{}
	}

	for _, s := range steps {
		if _, dup := known[s.Step]; dup {
			continue
		}
		known[s.Step] = struct{}{}
		level := LevelSummary
		autoExpand := false
		switch state.ExpandMode {
		case ExpandModeExpanded:
			level = LevelExpanded
			autoExpand = true
		case ExpandModeErrorsOnly:
			if s.HasError {
				level = LevelExpanded
				autoExpand = true
			}
		default: // ExpandModeCollapsed — safety net：错误始终展开
			if s.HasError {
				level = LevelExpanded
				autoExpand = true
			}
		}
		state.StepEntries = append(state.StepEntries, StepEntry{
			Summary:    s,
			Level:      level,
			AutoExpand: autoExpand,
		})
	}
	return state
}
