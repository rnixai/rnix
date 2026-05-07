// Package inspector — follow_live.go (Story 38-5 PR11 Step 4(c))
//
// StopFollowLive 关闭 Inspector 的 follow-live 跟踪模式（Story 36-6 AC-13）·
// 从 cmd/rnix.stopFollowLiveWithStatus 的 state mutation 主体迁出·
// statusMsg 副作用由 cmd/rnix wrapper 根据返回的 FollowLiveStoppedMsg 决定.
package inspector

// FollowLiveStoppedMsg 是 stopFollowLive 关闭时显示给用户的 status message
// （Story 36-6 AC-13 · 公开常量便于 i18n 集中管理 + 测试稳定性断言）。
const FollowLiveStoppedMsg = "Follow live: off (F 恢复)"

// StopFollowLive 关闭 InspectorState 的 FollowLive 标志。
//
// 行为契约（与 cmd/rnix.stopFollowLiveWithStatus 等价）：
//   - FollowLive == false → noop + 返回 (state, false)（idempotent）;
//   - FollowLive == true → 清零 + 返回 (state, true)（caller 据此设置
//     status message = FollowLiveStoppedMsg）.
//
// 返回 stopped 信号让 caller 决定是否设置 statusMsg（避免 FollowLive=false
// 时的 "no-op status spam"）。
func StopFollowLive(state InspectorState) (newState InspectorState, stopped bool) {
	if !state.FollowLive {
		return state, false
	}
	state.FollowLive = false
	return state, true
}
