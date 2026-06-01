package main

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/spf13/cobra"
	"github.com/rnixai/rnix/internal/dashboard/tree"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/vfs"
)

var topCmd = &cobra.Command{
	Use:   "top",
	Short: "Real-time process monitoring dashboard",
	Long:  "Interactive TUI showing process tree, status, and resource consumption in real-time.",
	Args:  cobra.NoArgs,
	RunE:  runTop,
}

// treeNode / flatRow are type aliases to internal/dashboard/tree (Story 38-5 PR2 Step 1).
// 字段公开（Proc/Children/Prefix/Depth/Collapsed），既支持 rnix top 也供 dashboard 共享。
type treeNode = tree.TreeNode

type flatRow = tree.FlatRow

// buildTree / flattenTree thin wrappers — 主体迁出至 internal/dashboard/tree (Story 38-5 PR2 Step 1)。
func buildTree(procs []vfs.ProcInfo) []*treeNode {
	return tree.BuildTree(procs)
}

func flattenTree(roots []*treeNode) []flatRow {
	return tree.FlattenTree(roots)
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

// topDetailView renders the detail panel for a selected process,
// showing Intent, Skills, Tokens, Elapsed, Context, Devices, and Children.
func topDetailView(info vfs.ProcInfo, allProcs []vfs.ProcInfo) string {
	var b strings.Builder
	// Epic 42 fix: freeze elapsed at DeadAt for exited processes (see
	// cmd/rnix/dashboard_title.go::elapsedDuration for rationale).
	elapsed := elapsedDuration(info)

	fmt.Fprintf(&b, "── Process Detail ── PID %d ──\n", info.PID)
	fmt.Fprintf(&b, "  State:    %s\n", strings.ToLower(info.State.String()))
	fmt.Fprintf(&b, "  Intent:   %s\n", info.Intent)
	if len(info.Skills) > 0 {
		fmt.Fprintf(&b, "  Skills:   %s\n", strings.Join(info.Skills, ", "))
	} else {
		fmt.Fprintf(&b, "  Skills:   —\n")
	}
	fmt.Fprintf(&b, "  Tokens:   %s\n", ui.FormatTokens(info.TokensUsed))
	if info.ContextBudget > 0 && info.LastInputTokens > 0 {
		pct := info.LastInputTokens * 100 / info.ContextBudget
		fmt.Fprintf(&b, "  Context:  %s/%s (%d%%)\n",
			ui.FormatTokens(info.LastInputTokens),
			ui.FormatTokens(info.ContextBudget), pct)
	}
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
	fmt.Fprintf(&b, "  [Esc: back | K: kill | q: quit]")
	return b.String()
}

// --- bubbletea Model ---

type tickMsg time.Time

type topModel struct {
	processes          []vfs.ProcInfo
	rows               []flatRow
	cursor             int
	detailPID          types.PID // 0 = list view
	launchDashboardPID types.PID // Story 27-5: set by Enter to launch dashboard
	client             *ipc.Client
	width              int
	height             int
	startTime          time.Time
	connected          bool
	err                error
	statusMsg          string
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
			m.launchDashboardPID = m.rows[m.cursor].Proc.PID
			return m, tea.Quit
		}
	case "K":
		if m.cursor < len(m.rows) {
			m.killSelected(m.rows[m.cursor].Proc.PID)
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
			if r.Proc.PID == m.detailPID {
				b.WriteString("\n")
				b.WriteString(topDetailView(r.Proc, m.processes))
				b.WriteString("\n")
				v := tea.NewView(b.String())
				v.AltScreen = true
				return v
			}
		}
	}

	b.WriteString("\n")
	header := fmt.Sprintf("  %-5s %-5s %-9s %-15s %12s %8s", "PID", "PPID", "STATE", "AGENT", "TOKENS", "ELAPSED")
	b.WriteString(header)
	b.WriteString("\n")
	b.WriteString("  " + strings.Repeat("─", 62))
	b.WriteString("\n")

	for i, row := range m.rows {
		cursor := "  "
		if i == m.cursor {
			cursor = "▸ "
		}

		agent := "—"
		if len(row.Proc.Skills) > 0 {
			agent = ui.FormatSkills(row.Proc.Skills, 15, "—")
		}

		elapsed := ui.FormatDuration(elapsedDuration(row.Proc))
		var tokens string
		tokens = ui.FormatTokens(row.Proc.TokensUsed)
		if row.Proc.ContextBudget > 0 && row.Proc.LastInputTokens > 0 {
			pct := row.Proc.LastInputTokens * 100 / row.Proc.ContextBudget
			tokens = fmt.Sprintf("%s ctx:%d%%", tokens, pct)
			if pct >= 80 {
				tokens = ui.WarningStyle.Render(tokens)
			}
		}
		state := strings.ToLower(row.Proc.State.String())

		var line string
		if row.Prefix != "" {
			line = fmt.Sprintf("%s%s%-4d %-5d %-9s %-15s %12s %8s",
				cursor, row.Prefix,
				row.Proc.PID, row.Proc.PPID, state, agent, tokens, elapsed)
		} else {
			line = fmt.Sprintf("%s%-5d %-5d %-9s %-15s %12s %8s",
				cursor,
				row.Proc.PID, row.Proc.PPID, state, agent, tokens, elapsed)
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
	b.WriteString("  [Enter: dashboard | K: kill | q: quit | ↑↓/jk: navigate]")
	b.WriteString("\n")

	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

// runTop is the cobra RunE handler for the "rnix top" command.
func runTop(_ *cobra.Command, _ []string) error {
	client, err := ipc.Dial(ipc.SocketPath())
	if err != nil {
		fmt.Fprintln(rootCmd.ErrOrStderr(), "✗ No rnix daemon running. Start an agent first with: rnix -i \"intent\"")
		return nil
	}

	model := newTopModel(client)
	p := tea.NewProgram(model)
	final, err := p.Run()
	if err != nil {
		client.Close()
		return fmt.Errorf("top: %w", err)
	}

	fm, ok := final.(topModel)
	if !ok {
		client.Close()
		return nil
	}

	if fm.launchDashboardPID > 0 {
		if fm.client != nil {
			fm.client.Close()
		} else {
			client.Close()
		}
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("top: cannot find rnix executable: %w", err)
		}
		return syscall.Exec(exe, []string{"rnix", "dashboard", fmt.Sprintf("--pid=%d", fm.launchDashboardPID)}, os.Environ())
	}

	if fm.client != nil {
		fm.client.Close()
	} else {
		client.Close()
	}
	return nil
}
