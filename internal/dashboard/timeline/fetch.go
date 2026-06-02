// Package timeline — fetch.go (Story 38-5 PR11 Step 4(b) Phase 3)
//
// IPC fetch closure + Msg type 迁出（与 cmd/rnix.fetchStepsCmd + stepListMsg 完全等价）。
// 本文件让 TimelineModel.OnTick 可自治触发 IPC 拉取。
//
// 「每次 fetch 新建 + 关闭 client」是与 cmd/rnix 端 fetchStepsCmd 字面等价的设计。
package timeline

import (
	tea "charm.land/bubbletea/v2"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
)

// StepListMsg 是 step list fetch 的返回 Msg。
//
// cmd/rnix 端通过 `type stepListMsg = timeline.StepListMsg` 别名接收（Story 38-5
// Code Review G3-1 fix · 2026-05-07）· 让 TimelineModel.OnTick 与 cmd/rnix.fetchStepsCmd
// 产生的 msg 路由到同一个 `case stepListMsg` 分支 · 修复非 pidChanged 路径上
// 增量 step 数据更新静默丢失的 bug.
type StepListMsg struct {
	UUID  string
	PID   types.PID
	Steps []ipc.StepSummaryWire
	Total int
	Err   error
}

// FetchStepsCmd 构造 step list fetch 的 tea.Cmd。
//
// 行为契约（与 cmd/rnix.fetchStepsCmd 完全等价）：
//   - socketPath 为空或 (pid == 0 且 uuid 为空) 时返回 nil cmd（防御性）；
//   - uuid 非空时调用 ListStepsByUUID(uuid, afterStep)；
//   - uuid 为空时调用 ListSteps(pid, afterStep)；
//   - 错误时填 Err（包括 Dial 失败）。
func FetchStepsCmd(socketPath string, uuid string, pid types.PID, afterStep int) tea.Cmd {
	if socketPath == "" || (pid == 0 && uuid == "") {
		return nil
	}
	return func() tea.Msg {
		client, err := ipc.Dial(socketPath)
		if err != nil {
			return StepListMsg{UUID: uuid, PID: pid, Err: err}
		}
		defer client.Close()
		var resp *ipc.ListStepsResponse
		if uuid != "" {
			resp, err = client.ListStepsByUUID(uuid, afterStep)
		} else {
			resp, err = client.ListSteps(pid, afterStep)
		}
		if err != nil {
			return StepListMsg{UUID: uuid, PID: pid, Err: err}
		}
		return StepListMsg{UUID: uuid, PID: pid, Steps: resp.Steps, Total: resp.Total}
	}
}
