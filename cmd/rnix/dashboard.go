package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
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
		client:    client,
		startTime: time.Now(),
		connected: client != nil,
	}
}

func (m dashboardModel) Init() tea.Cmd {
	return tickCmd()
}

func (m dashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		return m.dashboardTick()
	case tea.KeyPressMsg:
		return m.dashboardKey(msg)
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	}
	return m, nil
}

func (m dashboardModel) dashboardTick() (tea.Model, tea.Cmd) {
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
	roots := buildProcessTree(procs)
	m.treeRows = flattenTree(roots)

	if m.treeCursor >= len(m.treeRows) {
		m.treeCursor = max(0, len(m.treeRows)-1)
	}
	if m.treeCursor < len(m.treeRows) {
		m.selectedPID = m.treeRows[m.treeCursor].proc.PID
	}

	return m, tickCmd()
}

func (m dashboardModel) dashboardKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	if m.confirmKill {
		switch key {
		case "y":
			if m.client != nil && m.confirmPID > 0 {
				if err := m.client.Kill(m.confirmPID, types.SIGTERM); err != nil {
					m.statusMsg = fmt.Sprintf("✗ kill PID %d: %v", m.confirmPID, err)
				} else {
					m.statusMsg = fmt.Sprintf("✓ signal sent to PID %d (SIGTERM)", m.confirmPID)
				}
			}
			m.confirmKill = false
			m.confirmPID = 0
		default:
			m.confirmKill = false
			m.confirmPID = 0
		}
		return m, nil
	}

	switch key {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "tab":
		m.activePane = (m.activePane + 1) % 3
		return m, nil
	}

	if m.activePane == paneTree {
		visibleLines := m.dashboardVisibleLines()
		switch key {
		case "up", "k":
			if m.treeCursor > 0 {
				m.treeCursor--
				if m.treeCursor < len(m.treeRows) {
					m.selectedPID = m.treeRows[m.treeCursor].proc.PID
				}
				if m.treeCursor < m.treeOffset {
					m.treeOffset = m.treeCursor
				}
			}
		case "down", "j":
			if m.treeCursor < len(m.treeRows)-1 {
				m.treeCursor++
				if m.treeCursor < len(m.treeRows) {
					m.selectedPID = m.treeRows[m.treeCursor].proc.PID
				}
				if visibleLines > 0 && m.treeCursor >= m.treeOffset+visibleLines {
					m.treeOffset = m.treeCursor - visibleLines + 1
				}
			}
		case "enter":
			if m.treeCursor < len(m.treeRows) {
				m.selectedPID = m.treeRows[m.treeCursor].proc.PID
			}
		default:
			if (msg.Code == 'K' || msg.ShiftedCode == 'K') && msg.Mod&tea.ModShift != 0 {
				if len(m.treeRows) > 0 && m.treeCursor < len(m.treeRows) {
					m.confirmKill = true
					m.confirmPID = m.treeRows[m.treeCursor].proc.PID
				}
			}
		}
	}

	return m, nil
}

func (m dashboardModel) dashboardVisibleLines() int {
	v := m.height - 7
	if v < 1 {
		v = 1
	}
	return v
}

func (m dashboardModel) View() tea.View {
	content := m.renderDashboard()
	v := tea.NewView(content)
	v.AltScreen = true
	return v
}

func colorState(s types.ProcessState) string {
	name := strings.ToLower(s.String())
	switch s {
	case types.StateRunning:
		return ui.SuccessStyle.Render(name)
	case types.StateZombie:
		return ui.WarningStyle.Render(name)
	case types.StateDead:
		return ui.MutedStyle.Render(name)
	case types.StateCreated:
		return ui.KernelStyle.Render(name)
	default:
		return name
	}
}

// --- Layout rendering ---

func (m dashboardModel) renderDashboard() string {
	w := m.width
	h := m.height
	if w == 0 {
		w = 120
	}
	if h == 0 {
		h = 40
	}

	titleBar := m.renderDashboardTitle()
	statusBar := m.renderDashboardStatus()

	contentHeight := h - 4
	if contentHeight < 3 {
		contentHeight = 3
	}

	treeWidth := w * 40 / 100
	if treeWidth < 30 {
		treeWidth = 30
	}
	if treeWidth > 60 {
		treeWidth = 60
	}
	rightWidth := w - treeWidth
	if rightWidth < 10 {
		rightWidth = 10
	}

	treePane := m.renderDashboardTreePane(treeWidth, contentHeight)

	topRightH := contentHeight / 2
	bottomRightH := contentHeight - topRightH
	timelinePane := renderDashboardPlaceholder("Timeline", "Coming Soon", rightWidth, topRightH, m.activePane == paneTimeline)
	heatmapPane := renderDashboardPlaceholder("Heatmap", "Coming Soon", rightWidth, bottomRightH, m.activePane == paneHeatmap)

	rightPane := lipgloss.JoinVertical(lipgloss.Left, timelinePane, heatmapPane)
	mainContent := lipgloss.JoinHorizontal(lipgloss.Top, treePane, rightPane)

	return lipgloss.JoinVertical(lipgloss.Left, titleBar, mainContent, statusBar)
}

func (m dashboardModel) renderDashboardTitle() string {
	var b strings.Builder
	b.WriteString("  Rnix Dashboard")
	if m.connected {
		b.WriteString("  ● Connected")
	} else {
		b.WriteString("  ○ Disconnected")
	}

	active := 0
	totalTokens := 0
	for _, p := range m.processes {
		if p.State == types.StateRunning || p.State == types.StateCreated {
			active++
		}
		totalTokens += p.TokensUsed
	}
	fmt.Fprintf(&b, " | Processes: %d | Tokens: %s", active, ui.FormatTokens(totalTokens))

	return b.String()
}

func (m dashboardModel) renderDashboardStatus() string {
	if m.confirmKill {
		return fmt.Sprintf("  Kill PID %d? [y/N]", m.confirmPID)
	}
	if m.statusMsg != "" {
		return fmt.Sprintf("  %s  |  q:Quit  Tab:Switch Pane  j/k:Navigate  K:Kill", m.statusMsg)
	}
	return "  q:Quit  Tab:Switch Pane  j/k:Navigate  K:Kill  Enter:Select"
}

func (m dashboardModel) renderDashboardTreePane(width, height int) string {
	isActive := m.activePane == paneTree

	borderColor := lipgloss.Color(ui.ColorMuted)
	if isActive {
		borderColor = lipgloss.Color(ui.ColorAgent)
	}

	innerW := width - 2
	if innerW < 1 {
		innerW = 1
	}
	innerH := height - 2
	if innerH < 1 {
		innerH = 1
	}

	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(innerW).
		Height(innerH)

	var b strings.Builder
	b.WriteString(" Agent Tree \n")

	now := time.Now()
	visibleLines := innerH - 1
	if visibleLines < 1 {
		visibleLines = 1
	}

	endIdx := m.treeOffset + visibleLines
	if endIdx > len(m.treeRows) {
		endIdx = len(m.treeRows)
	}

	for i := m.treeOffset; i < endIdx; i++ {
		row := m.treeRows[i]
		cursor := "  "
		if i == m.treeCursor {
			cursor = "▸ "
		}

		state := colorState(row.proc.State)

		skills := ui.FormatSkills(row.proc.Skills, 12, "—")

		tokens := ui.FormatTokens(row.proc.TokensUsed)
		if row.proc.ContextBudget > 0 {
			pct := row.proc.TokensUsed * 100 / row.proc.ContextBudget
			tokens = fmt.Sprintf("%s/%s(%d%%)",
				ui.FormatTokens(row.proc.TokensUsed),
				ui.FormatTokens(row.proc.ContextBudget), pct)
			if pct >= 80 {
				tokens = ui.WarningStyle.Render(tokens)
			}
		}

		elapsed := ui.FormatDuration(now.Sub(row.proc.CreatedAt))

		line := fmt.Sprintf("%s%sPID %-3d %-9s %-12s %s %s",
			cursor, row.prefix, row.proc.PID, state, skills, tokens, elapsed)
		b.WriteString(line)
		b.WriteString("\n")
	}

	return style.Render(b.String())
}

func renderDashboardPlaceholder(title, placeholder string, width, height int, active bool) string {
	borderColor := lipgloss.Color(ui.ColorMuted)
	if active {
		borderColor = lipgloss.Color(ui.ColorAgent)
	}

	innerW := width - 2
	if innerW < 1 {
		innerW = 1
	}
	innerH := height - 2
	if innerH < 1 {
		innerH = 1
	}

	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(innerW).
		Height(innerH)

	content := fmt.Sprintf(" %s \n\n    (%s)", title, placeholder)
	return style.Render(content)
}

// --- Process tree construction ---

func buildProcessTree(procs []vfs.ProcInfo) []*treeNode {
	if len(procs) == 0 {
		return nil
	}

	nodes := make(map[types.PID]*treeNode, len(procs))
	for i := range procs {
		nodes[procs[i].PID] = &treeNode{proc: procs[i]}
	}

	var roots []*treeNode
	for _, n := range nodes {
		if parent, ok := nodes[n.proc.PPID]; ok && n.proc.PID != n.proc.PPID {
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

// --- Command runner ---

func runDashboard(_ *cobra.Command, _ []string) error {
	profile := ui.DetectProfile(os.Stdout)
	ui.InitStyles(profile)

	client, err := ipc.Dial(ipc.SocketPath())
	if err != nil {
		fmt.Fprintln(rootCmd.ErrOrStderr(), "✗ No rnix daemon running. Start an agent first with: rnix -i \"intent\"")
		return nil
	}

	model := newDashboardModel(client)
	p := tea.NewProgram(model)
	final, err := p.Run()
	if err != nil {
		client.Close()
		return fmt.Errorf("dashboard: %w", err)
	}
	if fm, ok := final.(dashboardModel); ok && fm.client != nil {
		fm.client.Close()
	} else {
		client.Close()
	}
	return nil
}
