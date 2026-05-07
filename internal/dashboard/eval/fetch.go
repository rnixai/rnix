// Package eval — fetch.go (Story 38-5 PR11 Step 4(b) Phase 3)
//
// IPC fetch closures + Msg types 迁出（与 cmd/rnix.fetchReputationCmd /
// fetchTopologyCmd / fetchSynergyCmd + 3 个 Msg type 完全等价）。本文件让
// EvalModel.OnTick 可自治触发 IPC 拉取（含 SubView 路由：reputation 总是 fetch ·
// topology 仅 SubView==1 · synergy 仅 SubView==2）。
package eval

import (
	tea "charm.land/bubbletea/v2"

	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/kernel"
)

// ReputationMsg 是 reputation fetch 的返回 Msg（与 cmd/rnix.evalReputationMsg 等价）。
type ReputationMsg struct {
	Summaries []kernel.ReputationSummary
	Err       error
}

// TopologyMsg 是 topology fetch 的返回 Msg（与 cmd/rnix.evalTopologyMsg 等价）。
type TopologyMsg struct {
	Topology *ipc.TopologyQueryResponse
	Err      error
}

// SynergyMsg 是 synergy fetch 的返回 Msg（与 cmd/rnix.evalSynergyMsg 等价）。
type SynergyMsg struct {
	Combos []kernel.ComboSummary
	Err    error
}

// FetchReputationCmd 构造 reputation fetch tea.Cmd（与 cmd/rnix.fetchReputationCmd 等价）。
func FetchReputationCmd(socketPath string) tea.Cmd {
	if socketPath == "" {
		return nil
	}
	return func() tea.Msg {
		client, err := ipc.Dial(socketPath)
		if err != nil {
			return ReputationMsg{Err: err}
		}
		defer client.Close()
		resp, err := client.ReputationStatus("")
		if err != nil {
			return ReputationMsg{Err: err}
		}
		if resp != nil {
			return ReputationMsg{Summaries: resp.Summaries}
		}
		return ReputationMsg{}
	}
}

// FetchTopologyCmd 构造 topology fetch tea.Cmd（与 cmd/rnix.fetchTopologyCmd 等价）。
func FetchTopologyCmd(socketPath string) tea.Cmd {
	if socketPath == "" {
		return nil
	}
	return func() tea.Msg {
		client, err := ipc.Dial(socketPath)
		if err != nil {
			return TopologyMsg{Err: err}
		}
		defer client.Close()
		resp, err := client.TopologyQuery()
		if err != nil {
			return TopologyMsg{Err: err}
		}
		return TopologyMsg{Topology: resp}
	}
}

// FetchSynergyCmd 构造 synergy fetch tea.Cmd（与 cmd/rnix.fetchSynergyCmd 等价）。
func FetchSynergyCmd(socketPath string) tea.Cmd {
	if socketPath == "" {
		return nil
	}
	return func() tea.Msg {
		client, err := ipc.Dial(socketPath)
		if err != nil {
			return SynergyMsg{Err: err}
		}
		defer client.Close()
		resp, err := client.SynergyList()
		if err != nil {
			return SynergyMsg{Err: err}
		}
		if resp != nil {
			return SynergyMsg{Combos: resp.Combos}
		}
		return SynergyMsg{}
	}
}
