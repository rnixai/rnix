// Package intent — fetch.go (Story 38-5 PR11 Step 4(b) Phase 3)
//
// IPC fetch closure + Msg type 迁出（与 cmd/rnix.fetchIntentTreesCmd + intentTreesMsg
// 完全等价）。本文件让 IntentModel.OnTick 可自治触发 IPC 拉取。
//
// 与 heatmap/fetch.go 同模式：每次 fetch 新建 + 关闭 client（避免 closure 异步执行
// 时共享 client 引用的 race condition）。
package intent

import (
	tea "charm.land/bubbletea/v2"

	"github.com/rnixai/rnix/ipc"
)

// TreesMsg 是 intent trees fetch 的返回 Msg（与 cmd/rnix.intentTreesMsg 等价）。
//
// 字段：
//   - Trees *ipc.IntentStatusResponse — fetch 成功的 intent tree 列表 · err != nil 时为 nil
//   - Err   error — fetch 失败的错误（如 IPC 断连）
//
// cmd/rnix 端 Update 路由通过 type alias `type intentTreesMsg = intent.TreesMsg`
// 接收并分发到 IntentState.Trees 字段。
type TreesMsg struct {
	Trees *ipc.IntentStatusResponse
	Err   error
}

// FetchTreesCmd 构造 intent trees fetch 的 tea.Cmd。
//
// 调用方：
//   - cmd/rnix.fetchIntentTreesCmd（thin wrapper · 调本函数）
//   - IntentModel.OnTick（PaneModel 真实化路径）
//
// 行为契约（与 cmd/rnix.fetchIntentTreesCmd 完全等价）：
//   - socketPath 为空字符串时返回 nil cmd（防御性）；
//   - 否则返回 closure · 调用时 ipc.Dial + IntentList · defer Close。
func FetchTreesCmd(socketPath string) tea.Cmd {
	if socketPath == "" {
		return nil
	}
	return func() tea.Msg {
		client, err := ipc.Dial(socketPath)
		if err != nil {
			return TreesMsg{Err: err}
		}
		defer client.Close()
		resp, err := client.IntentList()
		return TreesMsg{Trees: resp, Err: err}
	}
}
