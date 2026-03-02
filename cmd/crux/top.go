package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gonewx/crux/internal/types"
	"github.com/gonewx/crux/internal/ui"
	"github.com/gonewx/crux/ipc"
	"github.com/gonewx/crux/vfs"
	"github.com/spf13/cobra"
)

var topCmd = &cobra.Command{
	Use:   "top",
	Short: "Real-time process monitoring dashboard",
	Long:  "Interactive TUI showing process tree, status, and resource consumption in real-time.",
	Args:  cobra.NoArgs,
	RunE:  runTop,
}

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
	if len(procs) == 0 {
		return nil
	}

	nodes := make(map[types.PID]*treeNode, len(procs))
	for i := range procs {
		nodes[procs[i].PID] = &treeNode{proc: procs[i]}
	}

	var roots []*treeNode
	for _, n := range nodes {
		if parent, ok := nodes[n.proc.PPID]; ok {
			parent.children = append(parent.children, n)
		} else {
			roots = append(roots, n)
		}
	}

	sortNodes := func(ns []*treeNode) {
		sort.Slice(ns, func(i, j int) bool {
			return ns[i].proc.PID < ns[j].proc.PID
		})
	}
	sortNodes(roots)
	for _, n := range nodes {
		if len(n.children) > 1 {
			sortNodes(n.children)
		}
	}

	return roots
}

// flattenTree converts a tree into a flat list with indentation prefixes
// suitable for terminal display (├── for non-last children, └── for last child).
func flattenTree(roots []*treeNode) []flatRow {
	if len(roots) == 0 {
		return nil
	}

	var rows []flatRow
	var walk func(node *treeNode, depth int, parentPrefix string, isLast bool, isRoot bool)
	walk = func(node *treeNode, depth int, parentPrefix string, isLast bool, isRoot bool) {
		var prefix string
		if isRoot {
			prefix = ""
		} else if isLast {
			prefix = parentPrefix + "└─ "
		} else {
			prefix = parentPrefix + "├─ "
		}

		rows = append(rows, flatRow{
			proc:   node.proc,
			prefix: prefix,
			depth:  depth,
		})

		var childPrefix string
		if isRoot {
			childPrefix = parentPrefix
		} else if isLast {
			childPrefix = parentPrefix + "   "
		} else {
			childPrefix = parentPrefix + "│  "
		}

		for i, child := range node.children {
			walk(child, depth+1, childPrefix, i == len(node.children)-1, false)
		}
	}

	for _, root := range roots {
		walk(root, 0, "", true, true)
	}
	return rows
}

// topSummaryLine renders the top summary bar showing active count, total tokens, and uptime.
func topSummaryLine(procs []vfs.ProcInfo, uptime time.Duration) string {
	var active, zombie, totalTokens int
	for _, p := range procs {
		switch p.State {
		case types.StateRunning, types.StateCreated:
			active++
		case types.StateZombie:
			zombie++
		}
		totalTokens += p.TokensUsed
	}

	var b strings.Builder
	fmt.Fprintf(&b, "crux top — %d active", active)
	if zombie > 0 {
		fmt.Fprintf(&b, ", %d zombie", zombie)
	}
	fmt.Fprintf(&b, " | Tokens: %s | Up: %s",
		ui.FormatTokens(totalTokens), ui.FormatDuration(uptime))
	return b.String()
}

// topDetailView renders the detail panel for a selected process,
// showing Intent, Skills, Tokens, Elapsed, Context, Devices, and Children.
func topDetailView(info vfs.ProcInfo, allProcs []vfs.ProcInfo) string {
	var b strings.Builder
	elapsed := time.Since(info.CreatedAt)

	fmt.Fprintf(&b, "── Process Detail ── PID %d ──\n", info.PID)
	fmt.Fprintf(&b, "  State:    %s\n", strings.ToLower(info.State.String()))
	fmt.Fprintf(&b, "  Intent:   %s\n", info.Intent)
	if len(info.Skills) > 0 {
		fmt.Fprintf(&b, "  Skills:   %s\n", strings.Join(info.Skills, ", "))
	} else {
		fmt.Fprintf(&b, "  Skills:   —\n")
	}
	fmt.Fprintf(&b, "  Tokens:   %s\n", ui.FormatTokens(info.TokensUsed))
	fmt.Fprintf(&b, "  Elapsed:  %s\n", ui.FormatDuration(elapsed))
	fmt.Fprintf(&b, "  Context:  CtxID=%d\n", info.CtxID)
	if len(info.AllowedDevices) > 0 {
		fmt.Fprintf(&b, "  Devices:  %s\n", strings.Join(info.AllowedDevices, ", "))
	}
	var childPIDs []string
	for _, p := range allProcs {
		if p.PPID == info.PID {
			childPIDs = append(childPIDs, fmt.Sprintf("PID %d", p.PID))
		}
	}
	if len(childPIDs) > 0 {
		fmt.Fprintf(&b, "  Children: %s\n", strings.Join(childPIDs, ", "))
	}
	fmt.Fprintf(&b, "──────────────────────────────\n")
	fmt.Fprintf(&b, "  [Esc] Back  [K] Kill")
	return b.String()
}

// --- bubbletea Model ---

type tickMsg time.Time

type topModel struct {
	processes []vfs.ProcInfo
	rows      []flatRow
	cursor    int
	detailPID types.PID // 0 = list view
	client    *ipc.Client
	width     int
	height    int
	startTime time.Time
	connected bool
	err       error
	statusMsg string
}

func newTopModel(client *ipc.Client) topModel {
	return topModel{
		client:    client,
		startTime: time.Now(),
		connected: client != nil,
	}
}

func (m topModel) Init() tea.Cmd {
	return tickCmd()
}

func tickCmd() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m topModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		return m.handleTick()

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	}
	return m, nil
}

func (m topModel) handleTick() (tea.Model, tea.Cmd) {
	if m.client == nil {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			m.connected = false
			return m, tickCmd()
		}
		m.client = client
		m.connected = true
	}

	procs, err := m.client.ListProcs()
	if err != nil {
		m.client.Close()
		m.client = nil
		m.connected = false
		m.err = err
		return m, tickCmd()
	}

	m.err = nil
	m.connected = true
	m.statusMsg = ""
	m.processes = procs
	roots := buildTree(procs)
	m.rows = flattenTree(roots)

	if m.cursor >= len(m.rows) {
		m.cursor = max(0, len(m.rows)-1)
	}

	return m, tickCmd()
}

func (m topModel) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch key {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "esc":
		if m.detailPID != 0 {
			m.detailPID = 0
		}
		return m, nil
	}

	if m.detailPID != 0 {
		if key == "K" {
			m.killSelected(m.detailPID)
		}
		return m, nil
	}

	switch key {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.rows)-1 {
			m.cursor++
		}
	case "enter":
		if m.cursor < len(m.rows) {
			m.detailPID = m.rows[m.cursor].proc.PID
		}
	case "K":
		if m.cursor < len(m.rows) {
			m.killSelected(m.rows[m.cursor].proc.PID)
		}
	}

	return m, nil
}

func (m *topModel) killSelected(pid types.PID) {
	if m.client == nil || pid == 0 {
		return
	}
	if err := m.client.Kill(pid, types.SIGTERM); err != nil {
		m.statusMsg = fmt.Sprintf("✗ kill PID %d: %v", pid, err)
	} else {
		m.statusMsg = fmt.Sprintf("✓ signal sent to PID %d (SIGTERM)", pid)
	}
}

func (m topModel) View() tea.View {
	var b strings.Builder

	uptime := time.Since(m.startTime)
	summary := topSummaryLine(m.processes, uptime)
	if !m.connected {
		summary += " [disconnected]"
	}
	b.WriteString(summary)
	b.WriteString("\n")

	if m.detailPID != 0 {
		for _, r := range m.rows {
			if r.proc.PID == m.detailPID {
				b.WriteString("\n")
				b.WriteString(topDetailView(r.proc, m.processes))
				b.WriteString("\n")
				v := tea.NewView(b.String())
				v.AltScreen = true
				return v
			}
		}
	}

	b.WriteString("\n")
	header := fmt.Sprintf("  %-5s %-5s %-9s %-15s %8s %8s", "PID", "PPID", "STATE", "AGENT", "TOKENS", "ELAPSED")
	b.WriteString(header)
	b.WriteString("\n")
	b.WriteString("  " + strings.Repeat("─", 58))
	b.WriteString("\n")

	now := time.Now()
	for i, row := range m.rows {
		cursor := "  "
		if i == m.cursor {
			cursor = "▸ "
		}

		agent := "—"
		if len(row.proc.Skills) > 0 {
			agent = ui.FormatSkills(row.proc.Skills, 15, "—")
		}

		elapsed := ui.FormatDuration(now.Sub(row.proc.CreatedAt))
		tokens := ui.FormatTokens(row.proc.TokensUsed)
		state := strings.ToLower(row.proc.State.String())

		var line string
		if row.prefix != "" {
			line = fmt.Sprintf("%s%s%-4d %-5d %-9s %-15s %8s %8s",
				cursor, row.prefix,
				row.proc.PID, row.proc.PPID, state, agent, tokens, elapsed)
		} else {
			line = fmt.Sprintf("%s%-5d %-5d %-9s %-15s %8s %8s",
				cursor,
				row.proc.PID, row.proc.PPID, state, agent, tokens, elapsed)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}

	statusLines := 0
	if m.statusMsg != "" {
		statusLines = 1
	}
	usable := m.height - 4 - len(m.rows) - statusLines
	for i := 0; i < usable-2; i++ {
		b.WriteString("\n")
	}
	if m.statusMsg != "" {
		b.WriteString("  ")
		b.WriteString(m.statusMsg)
		b.WriteString("\n")
	}
	b.WriteString("  [q] Quit  [K] Kill  [Enter] Details  [↑↓/jk] Navigate")
	b.WriteString("\n")

	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

// runTop is the cobra RunE handler for the "crux top" command.
func runTop(_ *cobra.Command, _ []string) error {
	client, err := ipc.Dial(ipc.SocketPath())
	if err != nil {
		fmt.Fprintln(rootCmd.ErrOrStderr(), "✗ No crux daemon running. Start an agent first with: crux -i \"intent\"")
		return nil
	}

	model := newTopModel(client)
	p := tea.NewProgram(model)
	final, err := p.Run()
	if err != nil {
		client.Close()
		return fmt.Errorf("top: %w", err)
	}
	if fm, ok := final.(topModel); ok && fm.client != nil {
		fm.client.Close()
	} else {
		client.Close()
	}
	return nil
}
