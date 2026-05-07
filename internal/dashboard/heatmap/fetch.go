// Package heatmap — fetch.go (Story 38-5 PR11 Step 4(b) Phase 3)
//
// IPC fetch closure + Msg type 迁出（与 cmd/rnix.fetchHeatmapCmd + heatmapProfileMsg
// 完全等价）。本文件让 HeatmapModel.OnTick 可自治触发 IPC 拉取，无需通过 cmd/rnix
// 端 dashboardTick 间接编排。
//
// 调用约定（与 cmd/rnix.fetchHeatmapCmd 完全等价）：
//  1. 输入：*ipc.Client（已连接） + types.PID（待 fetch 的进程 PID）
//  2. 行为：执行 client.CtxProfile(pid) · 错误时回填 ProfileMsg.Err
//  3. 输出：tea.Cmd 包装一个 closure · 调用时返回 ProfileMsg
//
// 注意：本函数不关闭 client（与 cmd/rnix.fetchHeatmapCmd 不同 · 因为本设计下 client
// 由 OnTickContext 共享传入 · 由 cmd/rnix 端 dashboardModel 管理生命周期 · client.Close
// 由 dashboardModel 拥有调用权）。
package heatmap

import (
	tea "charm.land/bubbletea/v2"

	"github.com/rnixai/rnix/debug"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
)

// ProfileMsg 是 ctx profile fetch 的返回 Msg（与 cmd/rnix.heatmapProfileMsg 等价）。
//
// 字段：
//   - Profile *debug.CtxProfileResult — fetch 成功的 profile 数据 · err != nil 时为 nil
//   - Err     error — fetch 失败的错误（如 IPC 断连 / 进程不存在 / 权限不足）
//
// cmd/rnix 端 Update 路由通过 type alias `type heatmapProfileMsg = heatmap.ProfileMsg`
// 接收并分发到现有 HeatmapState 字段。
type ProfileMsg struct {
	Profile *debug.CtxProfileResult
	Err     error
}

// FetchProfileCmd 构造 ctx profile fetch 的 tea.Cmd。
//
// 调用方：
//   - cmd/rnix.fetchHeatmapCmd（thin wrapper · 调本函数）
//   - HeatmapModel.OnTick（PaneModel 真实化路径 · 接受 ctx.SocketPath + selectedPID 触发）
//
// 行为契约（与 cmd/rnix.fetchHeatmapCmd 完全等价）：
//   - socketPath 为空字符串或 pid <= 0 时 · 返回 nil cmd（不发 IPC · 防御性）；
//   - 否则返回 closure · 调用时 ipc.Dial(socketPath) 新建临时 client · 执行
//     client.CtxProfile(pid) · defer client.Close()；
//   - 错误时填 Err（包括 Dial 失败）；
//   - 「每次 fetch 新建 + 关闭 client」是与 cmd/rnix 端 fetchHeatmapCmd 字面等价的设计 ·
//     避免共享 client 引用在 closure 异步执行时的 race condition.
func FetchProfileCmd(socketPath string, pid types.PID) tea.Cmd {
	if socketPath == "" || pid <= 0 {
		return nil
	}
	return func() tea.Msg {
		client, err := ipc.Dial(socketPath)
		if err != nil {
			return ProfileMsg{Err: err}
		}
		defer client.Close()
		profile, err := client.CtxProfile(pid)
		return ProfileMsg{Profile: profile, Err: err}
	}
}
