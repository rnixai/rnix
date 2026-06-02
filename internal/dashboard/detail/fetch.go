// Package detail — fetch.go (Story 38-5 PR11 Step 4(b) Phase 3)
//
// IPC fetch closure + Msg type 迁出（与 cmd/rnix.fetchProcDetailCmd + procDetailResultMsg
// 完全等价）。本文件让 DetailModel.OnTick 可自治触发 IPC 拉取，无需通过 cmd/rnix
// 端 dashboardTick 间接编排。
//
// 调用约定（与 cmd/rnix.fetchProcDetailCmd 完全等价）：
//  1. 输入：socketPath + types.PID + uuid string
//  2. 行为：ipc.Dial(socketPath) → client.GetProcDetail(pid, uuid) → defer client.Close()
//  3. 输出：tea.Cmd 包装一个 closure · 调用时返回 DetailResultMsg
//
// 「每次 fetch 新建 + 关闭 client」是与 cmd/rnix 端 fetchProcDetailCmd 字面等价的设计 ·
// 避免共享 client 引用在 closure 异步执行时的 race condition.
package detail

import (
	tea "charm.land/bubbletea/v2"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
)

// DetailResultMsg 是 proc detail fetch 的返回 Msg。
//
// cmd/rnix 端通过 `type procDetailResultMsg = detail.DetailResultMsg` 别名接收（Story 38-5
// Code Review G3-2 fix · 2026-05-07）· 让 DetailModel.OnTick 与 cmd/rnix.fetchProcDetailCmd
// 产生的 msg 路由到同一个 `case procDetailResultMsg` 分支 · 修复 detail 卡片周期刷新
// 静默丢失的 bug.
type DetailResultMsg struct {
	PID    types.PID
	UUID   string
	Detail *ipc.GetProcDetailResponse
	Err    error
}

// FetchDetailCmd 构造 proc detail fetch 的 tea.Cmd。
//
// 调用方：
//   - DetailModel.OnTick（PaneModel 真实化路径）
//   - cmd/rnix.fetchProcDetailCmd（thin wrapper · 保留旧名让现有 callsite 零修改）
//
// 行为契约（与 cmd/rnix.fetchProcDetailCmd 完全等价）：
//   - socketPath 为空或 (pid == 0 且 uuid 为空) 时返回 nil cmd（防御性）；
//   - 否则返回 closure · 调用时 ipc.Dial(socketPath) 新建临时 client ·
//     执行 client.GetProcDetail(pid, uuid) · defer client.Close()；
//   - 错误时填 Err（包括 Dial 失败）。
func FetchDetailCmd(socketPath string, pid types.PID, uuid string) tea.Cmd {
	if socketPath == "" || (pid == 0 && uuid == "") {
		return nil
	}
	return func() tea.Msg {
		client, err := ipc.Dial(socketPath)
		if err != nil {
			return DetailResultMsg{PID: pid, UUID: uuid, Err: err}
		}
		defer client.Close()
		resp, err := client.GetProcDetail(pid, uuid)
		return DetailResultMsg{PID: pid, UUID: uuid, Detail: resp, Err: err}
	}
}

// ResumeLineageResultMsg — Story 42.3 stub.
// 返回给 dashboardModel.Update 的 lineage fetch 结果，承载 UUID + lineage 响应 +
// 错误。命中后写入 DetailState.LineageCache 触发渲染刷新。
type ResumeLineageResultMsg struct {
	UUID    string
	Lineage *ipc.GetResumeLineageResponse
	Err     error
}

// FetchResumeLineageCmd — Story 42.3 stub.
//
// 构造 lineage fetch 的 tea.Cmd。dev-story 阶段需要在 dashboard Update 的
// procResumeLineageResultMsg 分支处理消息，并将结果写入 DetailState.LineageCache。
//
// 行为契约（拟定）：
//   - socketPath 为空 / uuid 为空时返回 nil cmd（防御性）；
//   - 否则 ipc.Dial → client.GetResumeLineage(uuid) → defer client.Close()；
//   - 错误时填 Err。
func FetchResumeLineageCmd(socketPath, uuid string) tea.Cmd {
	if socketPath == "" || uuid == "" {
		return nil
	}
	return func() tea.Msg {
		client, err := ipc.Dial(socketPath)
		if err != nil {
			return ResumeLineageResultMsg{UUID: uuid, Err: err}
		}
		defer client.Close()
		resp, err := client.GetResumeLineage(uuid)
		return ResumeLineageResultMsg{UUID: uuid, Lineage: resp, Err: err}
	}
}
