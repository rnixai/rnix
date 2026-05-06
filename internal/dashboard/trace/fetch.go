// Package trace — fetch.go (Story 38-5 PR11 Step 4(b) Phase 3)
//
// IPC fetch closure + Msg type 迁出（与 cmd/rnix.fetchTraceListCmd + traceListMsg
// 完全等价）。本文件让 TraceModel.OnTick 可自治触发 IPC 拉取。
package trace

import (
	tea "charm.land/bubbletea/v2"

	"github.com/rnixai/rnix/ipc"
)

// ListMsg 是 trace list fetch 的返回 Msg（与 cmd/rnix.traceListMsg 等价）。
type ListMsg struct {
	Summaries []ipc.TraceSummaryWire
	Err       error
}

// FetchListCmd 构造 trace list fetch 的 tea.Cmd（与 cmd/rnix.fetchTraceListCmd 等价）。
func FetchListCmd(socketPath string) tea.Cmd {
	if socketPath == "" {
		return nil
	}
	return func() tea.Msg {
		client, err := ipc.Dial(socketPath)
		if err != nil {
			return ListMsg{Err: err}
		}
		defer client.Close()
		summaries, err := client.TraceList()
		return ListMsg{Summaries: summaries, Err: err}
	}
}
