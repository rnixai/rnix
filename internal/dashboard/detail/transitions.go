// Package detail — transitions.go (Story 38-5 PR11 Step 4(b) Phase 2)
//
// HandlePIDChangeWithCache 把 cmd/rnix.handlePIDChange 中 Detail pane 的 cache
// 复用 + PID 切换处理逻辑迁入 detail 包，让 dashboard.go 不再持有 cache lookup
// 散点（行数收敛 + 行为契约保留 + 28-4 AC-4 PID 复用安全契约保留）。
//
// 与 model.OnSelectPID 的区别：
//   - OnSelectPID(pid) 仅接受 pid 参数 · 不查 cache · 直接清空 Detail+PID（Phase 3
//     broadcast 通道接管时使用 · 当前 cmd/rnix 端 inline 处理仍保留 cache 复用）；
//   - HandlePIDChangeWithCache(state, pid, uuid) 接受 PID + UUID · 查 cache 复用
//     或清空 · 行为与 cmd/rnix 端 28-4 AC-4 契约 byte-for-byte 等价。
//
// 调用约定：
//
//	m.detail = detail.HandlePIDChangeWithCache(m.detail, m.selectedPID, m.selectedUUID)
//
// 行为契约（与原 cmd/rnix.handlePIDChange line 1156-1162 等价）：
//   - cache 命中（state.Cache[uuid] != nil）→ Detail = cached, PID = pid（复用 28-4 AC-4）；
//   - cache 未命中 → Detail = nil, PID = 0（清空 · 触发后续 fetchProcDetailCmd 异步获取）；
//   - 其他字段（Cache/Tick）保留不变。
package detail

import (
	"github.com/rnixai/rnix/internal/types"
)

// HandlePIDChangeWithCache 处理 PID 切换：cache 命中复用 / 否则清空。
//
// 与 OnSelectPID(pid) 的关系：OnSelectPID 是 Phase 3 broadcast 通道入口（不知道
// uuid · 直接清空）；本函数是 Phase 2 cmd/rnix 端 inline 入口（知道 uuid · 查
// cache 复用）。两者并存是 Story 38-5 渐进迁移的产物（Phase 3 完成后会让
// OnSelectPID 接受 uuid 或者引入 OnSelectPIDWithUUID 新方法 · 删除本函数）。
//
// 调用方应在 dashboard.go::handlePIDChange 内 selectedPID > 0 分支调用：
//
//	m.detail = detail.HandlePIDChangeWithCache(m.detail, m.selectedPID, m.selectedUUID)
//
// nil safety：state.Cache 为 nil 时安全（Go map[k] 零值返回零值，ok = false）。
func HandlePIDChangeWithCache(state DetailState, pid types.PID, uuid string) DetailState {
	if cached, ok := state.Cache[uuid]; ok {
		state.Detail = cached
		state.PID = pid
	} else {
		state.Detail = nil
		state.PID = 0
	}
	return state
}
