// Package inspector — diff_state.go (Story 38-5 PR11 Step 4(c))
//
// ExitDiffMode 把 InspectorState 的 7 个 diff-related 字段重置为零值
// （cmd/rnix.exitInspectorDiff 主体迁出 · viewport YOffset 保存/恢复 +
// rebuildInspectorContents 副作用由 cmd/rnix wrapper 保留）。
//
// SlideDiffBase 实现 cmd/rnix.slideDiffBase 的「按 captured delta 推算新
// base + clamp 到 [first, last] 范围」逻辑（不含 statusMsg + refresh diff
// mark 副作用 · 由 wrapper 保留）.
package inspector

import "time"

// ExitDiffMode 关闭 InspectorState 的 diff mode 并清理 7 个相关字段。
//
// 字段重置（与 cmd/rnix.exitInspectorDiff 等价）：
//   - DiffMode = false
//   - DiffBase = 0
//   - DiffDelta = 0
//   - DiffUnfolded = nil
//   - DiffPicker = false
//   - DiffPickerCursor = 0
//   - DiffDdDeadline = time.Time{}（零值 · dd double-tap 计时器清零）
//
// 不触碰：Lens / Viewports / Detail / PrevDetail 等其他状态（caller 负责
// rebuildInspectorContents + viewport YOffset 保存/恢复）。
func ExitDiffMode(state InspectorState) InspectorState {
	state.DiffMode = false
	state.DiffBase = 0
	state.DiffDelta = 0
	state.DiffUnfolded = nil
	state.DiffPicker = false
	state.DiffPickerCursor = 0
	state.DiffDdDeadline = time.Time{}
	return state
}

// SlideDiffBase 按 captured DiffDelta 距离推算新的 DiffBase（与 cmd/rnix.
// slideDiffBase clamp 逻辑等价）。
//
// 行为契约：
//   - state.Steps 为空 → 返回 (state, false)（caller 不处理）;
//   - target = newCurrent - state.DiffDelta;
//   - target < Steps[0].Step → clamp 到 Steps[0].Step + clamped=true;
//   - target > Steps[last].Step → clamp 到 Steps[last].Step + clamped=true;
//   - state.DiffBase 更新为 target;
//   - DiffUnfolded 重置为空 map（fold state index-based · diff output 变化）.
//
// 返回 clamped 信号让 caller 决定是否设置 statusMsg = "Diff base clamped"。
//
// 不触碰：DiffMode / DiffDelta / DiffPicker 等其他字段（caller 负责
// refreshInspectorDiffLensMarks 副作用）.
func SlideDiffBase(state InspectorState, newCurrent int) (newState InspectorState, clamped bool) {
	if len(state.Steps) == 0 {
		return state, false
	}
	target := newCurrent - state.DiffDelta
	first := state.Steps[0].Step
	last := state.Steps[len(state.Steps)-1].Step
	if target < first {
		target = first
		clamped = true
	} else if target > last {
		target = last
		clamped = true
	}
	state.DiffBase = target
	state.DiffUnfolded = make(map[int]bool)
	return state, clamped
}

// DiffBaseClampedMsg 是 SlideDiffBase clamp 触发时显示给用户的 status
// message（公开常量便于 i18n 集中管理 + 测试稳定性断言）。
const DiffBaseClampedMsg = "Diff base clamped"
