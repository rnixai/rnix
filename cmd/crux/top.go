package main

import (
	"fmt"
	"time"

	"github.com/gonewx/crux/vfs"
	"github.com/spf13/cobra"
)

// treeNode represents a process in the tree hierarchy for crux top display.
type treeNode struct {
	proc     vfs.ProcInfo
	children []*treeNode
	depth    int
}

// flatRow is a pre-rendered row from the tree, ready for display.
type flatRow struct {
	proc   vfs.ProcInfo
	prefix string
	depth  int
}

// buildTree constructs a process tree from a flat list of ProcInfo.
// Processes whose PPID is not in the list become root nodes.
// Children within each node are sorted by PID.
func buildTree(procs []vfs.ProcInfo) []*treeNode {
	// TODO: Story 10.1 — implement tree construction
	return nil
}

// flattenTree converts a tree into a flat list with indentation prefixes
// suitable for terminal display (├── for non-last children, └── for last child).
func flattenTree(roots []*treeNode) []flatRow {
	// TODO: Story 10.1 — implement tree flattening with DFS
	return nil
}

// topSummaryLine renders the top summary bar showing active count, total tokens, and uptime.
func topSummaryLine(procs []vfs.ProcInfo, uptime time.Duration) string {
	// TODO: Story 10.1 — implement summary rendering
	return ""
}

// topDetailView renders the detail panel for a selected process,
// showing Intent, Skills, Tokens, Elapsed, Context, Devices, and Children.
func topDetailView(info vfs.ProcInfo) string {
	// TODO: Story 10.1 — implement detail view rendering
	return ""
}

// runTop is the cobra RunE handler for the "crux top" command.
func runTop(_ *cobra.Command, _ []string) error {
	// TODO: Story 10.1 — implement TUI launch with bubbletea
	return fmt.Errorf("not implemented")
}
