// Package timeline — migration.go (Story 38-5 PR11 Step 4(c))
//
// Story 36-4 升序模式首次进入提示的纯逻辑迁出（不含 m.statusMsg 副作用 ·
// 由 cmd/rnix wrapper 保留）。
package timeline

import (
	"github.com/rnixai/rnix/internal/ui"
)

// MigrationCheckSaver 是 ui.SaveUIState 的接口抽象（默认实现 = ui.SaveUIState）。
// 测试可以注入 fake saver 验证「写入失败不阻塞」行为契约。
type MigrationCheckSaver func(*ui.UIState) error

// MigrationCheck 检查并标记 Story 36-4「升序模式首次进入」提示。
//
// 行为契约（与 cmd/rnix.maybeShowTimelineMigrationNotice 等价）：
//   - state.MigrationChecked==true → noop（早返回 · session 内仅检查一次）;
//   - 标记 MigrationChecked=true · 懒分配 UIState（首次为 nil）;
//   - 已展示过（TimelineSortMigrationShown）或非升序模式 → 不触发提示;
//   - 否则：标记 TimelineSortMigrationShown=true + 调 saver（默认 ui.SaveUIState）·
//     写入失败不阻塞返回 showNotice=true（cmd/rnix wrapper 据此设置 statusMsg）.
//
// saver 为 nil 时使用默认 ui.SaveUIState（生产路径 · 测试可注入 fake）。
func MigrationCheck(state TimelineState, saver MigrationCheckSaver) (newState TimelineState, showNotice bool) {
	if state.MigrationChecked {
		return state, false
	}
	state.MigrationChecked = true
	if state.UIState == nil {
		state.UIState = &ui.UIState{}
	}
	if state.UIState.TimelineSortMigrationShown || !state.SortAsc {
		return state, false
	}
	state.UIState.TimelineSortMigrationShown = true
	if saver == nil {
		saver = ui.SaveUIState
	}
	_ = saver(state.UIState) // 写入失败不阻塞 UI（与 cmd/rnix 历史行为等价）
	return state, true
}
