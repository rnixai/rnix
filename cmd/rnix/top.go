package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/vfs"
	"github.com/spf13/cobra"
)

var topCmd = &cobra.Command{
	Use:   "top",
	Short: "Real-time process monitoring dashboard",
	Long:  "Interactive TUI showing process tree, status, and resource consumption in real-time.",
	Args:  cobra.NoArgs,
	RunE:  runTop,
}

// treeNode represents a process in the tree hierarchy for rnix top display.
type treeNode struct {
	proc     vfs.ProcInfo
	children []*treeNode
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
	fmt.Fprintf(&b, "rnix top — %d active", active)
	if zombie > 0 {
		fmt.Fprintf(&b, ", %d zombie", zombie)
	}
	fmt.Fprintf(&b, " | Tokens: %s | Up: %s",
		ui.FormatTokens(totalTokens), ui.FormatDuration(uptime))
	return b.String()
}

// --- bubbletea Model ---

type appMode int

const (
	appModeTop   appMode = iota
	appModeWatch
)

type tickMsg time.Time

type topModel struct {
	processes []vfs.ProcInfo
	rows      []flatRow
	cursor    int
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
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.rows)-1 {
			m.cursor++
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

	b.WriteString("\n")
	header := fmt.Sprintf("  %-5s %-5s %-9s %-15s %12s %8s", "PID", "PPID", "STATE", "AGENT", "TOKENS", "ELAPSED")
	b.WriteString(header)
	b.WriteString("\n")
	b.WriteString("  " + strings.Repeat("─", 62))
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
		var tokens string
		if row.proc.ContextBudget > 0 {
			tokens = fmt.Sprintf("%s/%s",
				ui.FormatTokens(row.proc.TokensUsed),
				ui.FormatTokens(row.proc.ContextBudget))
			if row.proc.TokensUsed >= row.proc.ContextBudget*80/100 {
				tokens = ui.WarningStyle.Render(tokens)
			}
		} else {
			tokens = ui.FormatTokens(row.proc.TokensUsed)
		}
		state := strings.ToLower(row.proc.State.String())

		var line string
		if row.prefix != "" {
			line = fmt.Sprintf("%s%s%-4d %-5d %-9s %-15s %12s %8s",
				cursor, row.prefix,
				row.proc.PID, row.proc.PPID, state, agent, tokens, elapsed)
		} else {
			line = fmt.Sprintf("%s%-5d %-5d %-9s %-15s %12s %8s",
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
	b.WriteString("  [q] Quit  [K] Kill  [Enter] Watch  [↑↓/jk] Navigate")
	b.WriteString("\n")

	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

// --- appModel: unified BubbleTea wrapper for top↔watch navigation ---

type appModel struct {
	mode     appMode
	topModel topModel
	watch    *watchModel
	dialFn   func() (*ipc.Client, error) // nil = use ipc.Dial(ipc.SocketPath())
}

func (m appModel) Init() tea.Cmd {
	return m.topModel.Init()
}

func (m appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		if key.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if m.mode == appModeTop && key.String() == "enter" {
			return m.switchToWatch()
		}
		if m.mode == appModeWatch && m.watch != nil &&
			key.String() == "q" && m.watch.state != watchStatePager {
			return m.backToTop()
		}
	}

	if wsm, ok := msg.(tea.WindowSizeMsg); ok {
		m.topModel.width = wsm.Width
		m.topModel.height = wsm.Height
		if m.watch != nil {
			m.watch.width = wsm.Width
			m.watch.height = wsm.Height
		}
		return m, nil
	}

	switch m.mode {
	case appModeTop:
		updated, cmd := m.topModel.Update(msg)
		m.topModel = updated.(topModel)
		return m, cmd
	case appModeWatch:
		if m.watch == nil {
			return m, nil
		}
		updated, cmd := m.watch.Update(msg)
		w := updated.(watchModel)
		m.watch = &w
		return m, cmd
	}
	return m, nil
}

func (m appModel) View() tea.View {
	if m.mode == appModeWatch && m.watch != nil {
		return m.watch.View()
	}
	return m.topModel.View()
}

func (m appModel) dial() (*ipc.Client, error) {
	if m.dialFn != nil {
		return m.dialFn()
	}
	return ipc.Dial(ipc.SocketPath())
}

func (m appModel) switchToWatch() (tea.Model, tea.Cmd) {
	if len(m.topModel.rows) == 0 || m.topModel.cursor >= len(m.topModel.rows) {
		return m, nil
	}
	pid := m.topModel.rows[m.topModel.cursor].proc.PID

	streamClient, err := m.dial()
	if err != nil {
		m.topModel.statusMsg = fmt.Sprintf("✗ watch: %v", err)
		return m, nil
	}
	queryClient, err := m.dial()
	if err != nil {
		if streamClient != nil {
			streamClient.Close()
		}
		m.topModel.statusMsg = fmt.Sprintf("✗ watch: %v", err)
		return m, nil
	}

	profile := ui.DetectProfile(os.Stdout)
	wm := newWatchModel(pid, streamClient, queryClient, profile)
	wm.embeddedInTop = true
	wm.width = m.topModel.width
	wm.height = m.topModel.height
	m.watch = &wm
	m.mode = appModeWatch
	return m, wm.Init()
}

func (m appModel) backToTop() (tea.Model, tea.Cmd) {
	if m.watch != nil {
		if m.watch.streamClient != nil {
			m.watch.streamClient.Close()
		}
		if m.watch.queryClient != nil {
			m.watch.queryClient.Close()
		}
		m.watch = nil
	}
	m.mode = appModeTop
	return m, tickCmd()
}

// runTop is the cobra RunE handler for the "rnix top" command.
func runTop(_ *cobra.Command, _ []string) error {
	client, err := ipc.Dial(ipc.SocketPath())
	if err != nil {
		fmt.Fprintln(rootCmd.ErrOrStderr(), "✗ No rnix daemon running. Start an agent first with: rnix -i \"intent\"")
		return nil
	}

	tm := newTopModel(client)
	model := appModel{mode: appModeTop, topModel: tm}
	p := tea.NewProgram(model)
	final, runErr := p.Run()
	if runErr != nil {
		client.Close()
		return fmt.Errorf("top: %w", runErr)
	}
	if fm, ok := final.(appModel); ok {
		if fm.topModel.client != nil {
			fm.topModel.client.Close()
		}
		if fm.watch != nil {
			if fm.watch.streamClient != nil {
				fm.watch.streamClient.Close()
			}
			if fm.watch.queryClient != nil {
				fm.watch.queryClient.Close()
			}
		}
	} else {
		client.Close()
	}
	return nil
}
