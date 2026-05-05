// Package inspector — diff_state.go (Story 38-5 PR11 Step 4(c))
//
// ExitDiffMode 把 InspectorState 的 7 个 diff-related 字段重置为零值
// （cmd/rnix.exitInspectorDiff 主体迁出 · viewport YOffset 保存/恢复 +
// rebuildInspectorContents 副作用由 cmd/rnix wrapper 保留）。
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
