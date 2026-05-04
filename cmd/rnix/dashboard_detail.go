package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"

	tea "charm.land/bubbletea/v2"
)

// --- Detail Pane (Story 27-6) ---

func fetchProcDetailCmd(pid types.PID, uuid string) tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return procDetailResultMsg{pid: pid, uuid: uuid, err: err}
		}
		defer client.Close()
		resp, err := client.GetProcDetail(pid, uuid)
		return procDetailResultMsg{pid: pid, uuid: uuid, detail: resp, err: err}
	}
}

func (m dashboardModel) renderDetailPane(width, height int) string {
	isActive := m.activePane == paneDetail

	borderColor := lipgloss.Color(ui.ColorMuted)
	if isActive {
		borderColor = lipgloss.Color(ui.ColorAgent)
	}

	innerW := max(width-2, 1)

	var b strings.Builder

	b.WriteString(" Detail")
	if m.selectedPID > 0 {
		fmt.Fprintf(&b, " | PID %d", m.selectedPID)
	}
	b.WriteString("\n")

	if m.selectedPID == 0 {
		b.WriteString("\n    Select a process to view detail")
		return renderFixedPanel(b.String(), width, height, borderColor)
	}

	d := m.detail.Detail
	if d == nil || d.PID != m.selectedPID || (m.selectedUUID != "" && d.UUID != m.selectedUUID) {
		b.WriteString("\n    Loading...")
		return renderFixedPanel(b.String(), width, height, borderColor)
	}

	// Section 1: Basic info
	var uptime time.Duration
	if d.CreatedAtMs > 0 {
		created := time.UnixMilli(d.CreatedAtMs)
		if d.DeadAtMs > 0 {
			uptime = time.UnixMilli(d.DeadAtMs).Sub(created)
		} else {
			uptime = time.Since(created)
		}
	}
	fmt.Fprintf(&b, "  PID: %d  UUID: %s\n", d.PID, truncateUUID(d.UUID))
	fmt.Fprintf(&b, "  State: %s  Intent: %s\n", d.State, truncateStr(d.Intent, 40))
	fmt.Fprintf(&b, "  Provider: %s  Model: %s\n", d.Provider, d.Model)
	fmt.Fprintf(&b, "  Uptime: %s\n", ui.FormatDuration(uptime))

	// Section 1b: Allowed devices
	if len(d.AllowedDevices) > 0 {
		fmt.Fprintf(&b, "  Devices: %s\n", strings.Join(d.AllowedDevices, ", "))
	}

	// Section 2: Skills
	b.WriteString("  ──── Skills ────\n")
	if len(d.Skills) == 0 {
		b.WriteString("    (none)\n")
	}
	for _, sk := range d.Skills {
		tools := strings.Join(sk.AllowedTools, ", ")
		if tools == "" {
			tools = "—"
		}
		fmt.Fprintf(&b, "    %s → %s\n", sk.Name, tools)
	}

	// Section 3: FD table
	b.WriteString("  ──── FD Table ────\n")
	if len(d.FDTable) == 0 {
		if d.State == "dead" {
			b.WriteString("    (closed)\n")
		} else {
			b.WriteString("    (empty)\n")
		}
	}
	for _, fd := range d.FDTable {
		fmt.Fprintf(&b, "    %d: %s\n", fd.FD, fd.DevicePath)
	}

	// Section 4: Context stats
	b.WriteString("  ──── Context ────\n")
	fmt.Fprintf(&b, "    %d msgs | %s tok\n", d.ContextStats.MessageCount, ui.FormatTokens(d.ContextStats.TokensUsed))
	if d.ContextStats.ContextBudget > 0 {
		barWidth := max(innerW-10, 10)
		filled := int(d.ContextStats.UsagePct / 100.0 * float64(barWidth))
		filled = min(filled, barWidth)
		bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
		pct := min(d.ContextStats.UsagePct, 100.0)
		fmt.Fprintf(&b, "    [%s] %.0f%%\n", bar, pct)
	}

	return renderFixedPanel(b.String(), width, height, borderColor)
}

func truncateUUID(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

func truncateStr(s string, maxLen int) string {
	if maxLen < 4 {
		return s
	}
	runes := []rune(s)
	if len(runes) > maxLen {
		return string(runes[:maxLen-3]) + "..."
	}
	return s
}
