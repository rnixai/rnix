package main

// ATDD RED PHASE — Story 17.1: Dashboard 框架与智能体树窗格
// Stub file: all functions return zero values.
// Tests in dashboard_test.go assert EXPECTED behavior and FAIL until implemented.

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/vfs"
)

var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Visual debugging dashboard with multi-pane view",
	Long:  "Interactive TUI dashboard showing agent tree, timeline, and heatmap in a multi-pane layout.",
	Args:  cobra.NoArgs,
	RunE:  runDashboard,
}

type paneType int

const (
	paneTree paneType = iota
	paneTimeline
	paneHeatmap
)

type dashboardModel struct {
	client      *ipc.Client
	width       int
	height      int
	activePane  paneType
	selectedPID types.PID
	processes   []vfs.ProcInfo
	treeRows    []flatRow
	treeCursor  int
	treeOffset  int
	connected   bool
	err         error
	statusMsg   string
	startTime   time.Time
	confirmKill bool
	confirmPID  types.PID
}

func newDashboardModel(client *ipc.Client) dashboardModel {
	return dashboardModel{
		client: client,
	}
}

func (m dashboardModel) Init() tea.Cmd {
	return nil // STUB: should return tickCmd()
}

func (m dashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	_ = msg
	return m, nil // STUB: no message handling
}

func (m dashboardModel) View() tea.View {
	return tea.View{} // STUB: empty view, AltScreen=false
}

// buildProcessTree constructs a process tree from a flat ProcInfo list.
// Uses PPID to establish parent-child relationships. Orphans become roots.
// Children sorted by PID within each node.
func buildProcessTree(_ []vfs.ProcInfo) []*treeNode {
	return nil // STUB: no tree building
}

func runDashboard(_ *cobra.Command, _ []string) error {
	return nil // STUB: no-op
}
