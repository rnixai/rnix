// Package security — fetch.go (Story 38-5 PR11 Step 4(b) Phase 3)
//
// IPC fetch closure + Msg type 迁出（与 cmd/rnix.fetchImmuneStatusCmd + immuneStatusMsg
// 完全等价）。本文件让 SecurityModel.OnTick 可自治触发 IPC 拉取。
//
// 与 heatmap/fetch.go + intent/fetch.go 同模式。
package security

import (
	tea "charm.land/bubbletea/v2"

	"github.com/rnixai/rnix/ipc"
)

// ImmuneStatusMsg 是 immune status fetch 的返回 Msg（与 cmd/rnix.immuneStatusMsg 等价）。
type ImmuneStatusMsg struct {
	Status *ipc.ImmuneStatusResponse
	Err    error
}

// FetchImmuneStatusCmd 构造 immune status fetch 的 tea.Cmd。
//
// 行为契约（与 cmd/rnix.fetchImmuneStatusCmd 完全等价）。
func FetchImmuneStatusCmd(socketPath string) tea.Cmd {
	if socketPath == "" {
		return nil
	}
	return func() tea.Msg {
		client, err := ipc.Dial(socketPath)
		if err != nil {
			return ImmuneStatusMsg{Err: err}
		}
		defer client.Close()
		resp, err := client.ImmuneStatus()
		return ImmuneStatusMsg{Status: resp, Err: err}
	}
}
