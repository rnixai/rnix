package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// Column definitions for the process table.
const (
	colWidthPID     = 5
	colWidthState   = 9
	colWidthSkill   = 15
	colWidthTokens  = 14
	colWidthCost    = 12
	colWidthActive  = 10
	colWidthElapsed = 8
	colWidthPPID    = 5
	colWidthUUID    = 11 // 8 hex chars + "..."
	colGap          = 3
)

// Column width thresholds for terminal width adaptation.
const (
	thresholdSkill   = 60
	thresholdMetrics = 80
	thresholdIntent  = 120
)

// RenderProcessTable renders a formatted process table to the renderer's writer.
// PID and STATE columns are always shown. Additional columns appear based on terminal width.
// When verbose is true, PPID and full INTENT columns are included.
// When showUUID is true, a truncated UUID column is displayed after PID.
func RenderProcessTable(r *Renderer, procs []vfs.ProcInfo, verbose, showUUID bool) {
	if len(procs) == 0 {
		fmt.Fprintln(r.Writer, "No active processes.")
		return
	}

	w := r.Profile.Width
	gap := strings.Repeat(" ", colGap)

	// Determine visible columns based on terminal width
	showSkill := w >= thresholdSkill
	showMetrics := w >= thresholdMetrics
	showIntent := w >= thresholdIntent || verbose

	// Conditional COST column: shown only when any process has MaxCost > 0
	showCost := false
	for _, p := range procs {
		if p.MaxCost > 0 {
			showCost = true
			break
		}
	}

	// Separator character
	sep := "─"
	dash := "—"
	if !r.Profile.IsUnicode {
		sep = "-"
		dash = "-"
	}

	// Build header
	var hdr strings.Builder
	fmt.Fprintf(&hdr, "%*s", colWidthPID, "PID")
	if showUUID {
		hdr.WriteString(gap)
		fmt.Fprintf(&hdr, "%-*s", colWidthUUID, "UUID")
	}
	if verbose {
		hdr.WriteString(gap)
		fmt.Fprintf(&hdr, "%*s", colWidthPPID, "PPID")
	}
	hdr.WriteString(gap)
	fmt.Fprintf(&hdr, "%-*s", colWidthState, "STATE")
	if showSkill {
		hdr.WriteString(gap)
		fmt.Fprintf(&hdr, "%-*s", colWidthSkill, "SKILL")
	}
	if showMetrics {
		hdr.WriteString(gap)
		fmt.Fprintf(&hdr, "%*s", colWidthTokens, "TOKENS")
		if showCost {
			hdr.WriteString(gap)
			fmt.Fprintf(&hdr, "%*s", colWidthCost, "COST")
		}
		hdr.WriteString(gap)
		fmt.Fprintf(&hdr, "%-*s", colWidthActive, "ACTIVE")
		hdr.WriteString(gap)
		fmt.Fprintf(&hdr, "%*s", colWidthElapsed, "ELAPSED")
	}
	if showIntent {
		hdr.WriteString(gap)
		hdr.WriteString("INTENT")
	}
	headerLine := hdr.String()
	fmt.Fprintln(r.Writer, headerLine)

	// Build separator line
	var sepLine strings.Builder
	sepLine.WriteString(strings.Repeat(sep, colWidthPID))
	if showUUID {
		sepLine.WriteString(gap)
		sepLine.WriteString(strings.Repeat(sep, colWidthUUID))
	}
	if verbose {
		sepLine.WriteString(gap)
		sepLine.WriteString(strings.Repeat(sep, colWidthPPID))
	}
	sepLine.WriteString(gap)
	sepLine.WriteString(strings.Repeat(sep, colWidthState))
	if showSkill {
		sepLine.WriteString(gap)
		sepLine.WriteString(strings.Repeat(sep, colWidthSkill))
	}
	if showMetrics {
		sepLine.WriteString(gap)
		sepLine.WriteString(strings.Repeat(sep, colWidthTokens))
		if showCost {
			sepLine.WriteString(gap)
			sepLine.WriteString(strings.Repeat(sep, colWidthCost))
		}
		sepLine.WriteString(gap)
		sepLine.WriteString(strings.Repeat(sep, colWidthActive))
		sepLine.WriteString(gap)
		sepLine.WriteString(strings.Repeat(sep, colWidthElapsed))
	}
	if showIntent {
		sepLine.WriteString(gap)
		sepLine.WriteString(strings.Repeat(sep, 6))
	}
	fmt.Fprintln(r.Writer, sepLine.String())

	// Render rows
	var active, zombie, suspended, dead, total int
	for _, proc := range procs {
		total++
		switch proc.State {
		case types.StateRunning, types.StateCreated:
			active++
		case types.StateZombie:
			zombie++
		case types.StateSuspended:
			suspended++
		case types.StateDead:
			dead++
		}

		var row strings.Builder
		if proc.PID == 0 {
			fmt.Fprintf(&row, "%*s", colWidthPID, dash)
		} else {
			fmt.Fprintf(&row, "%*d", colWidthPID, proc.PID)
		}
		if showUUID {
			row.WriteString(gap)
			uuidDisplay := truncateUUID(proc.UUID, colWidthUUID)
			fmt.Fprintf(&row, "%-*s", colWidthUUID, uuidDisplay)
		}
		if verbose {
			row.WriteString(gap)
			fmt.Fprintf(&row, "%*d", colWidthPPID, proc.PPID)
		}
		row.WriteString(gap)
		stateStr := renderState(r, proc.State, colWidthState)
		row.WriteString(stateStr)
		if showSkill {
			row.WriteString(gap)
			skillStr := FormatSkills(proc.Skills, colWidthSkill, dash)
			fmt.Fprintf(&row, "%-*s", colWidthSkill, skillStr)
		}
		if showMetrics {
			row.WriteString(gap)
			fmt.Fprintf(&row, "%*s", colWidthTokens, FormatTokensBudget(proc.TokensUsed, proc.MaxTokens))
			if showCost {
				row.WriteString(gap)
				fmt.Fprintf(&row, "%*s", colWidthCost, FormatCostBudget(proc.UsedCost, proc.MaxCost))
			}
			row.WriteString(gap)
			// ACTIVE column: relative heartbeat time for running processes
			var activeStr string
			switch proc.State {
			case types.StateRunning, types.StateCreated:
				activeStr = FormatRelativeTime(proc.LastHeartbeat)
				if IsStale(proc.LastHeartbeat, proc.StepTimeout) {
					staleWarning := "⚠️"
					if !r.Profile.IsUnicode {
						staleWarning = "!!"
					}
					activeStr = staleWarning + " " + activeStr
				}
			case types.StateSuspended:
				activeStr = dash + " (susp)"
			default:
				activeStr = dash
			}
			fmt.Fprintf(&row, "%-*s", colWidthActive, activeStr)
			row.WriteString(gap)
			var elapsed time.Duration
			if proc.State == types.StateDead && !proc.DeadAt.IsZero() {
				elapsed = proc.DeadAt.Sub(proc.CreatedAt)
			} else {
				elapsed = time.Since(proc.CreatedAt)
			}
			fmt.Fprintf(&row, "%*s", colWidthElapsed, FormatDuration(elapsed))
		}
		if showIntent {
			row.WriteString(gap)
			intentWidth := max(w-len(StripAnsi(row.String())), 0)
			intent := proc.Intent
			if !verbose && intentWidth > 3 && len(intent) > intentWidth {
				intent = intent[:intentWidth-3] + "..."
			}
			row.WriteString(intent)
		}
		fmt.Fprintln(r.Writer, row.String())
	}

	// Footer
	var parts []string
	parts = append(parts, fmt.Sprintf("%d active", active))
	if suspended > 0 {
		parts = append(parts, fmt.Sprintf("%d suspended", suspended))
	}
	if zombie > 0 {
		parts = append(parts, fmt.Sprintf("%d zombie", zombie))
	}
	if dead > 0 {
		parts = append(parts, fmt.Sprintf("%d dead", dead))
	}
	parts = append(parts, fmt.Sprintf("%d total", total))
	fmt.Fprintf(r.Writer, "\n%s\n", strings.Join(parts, ", "))
}

// renderState renders a state string with color coding and left-aligned padding.
func renderState(r *Renderer, state types.ProcessState, width int) string {
	name := state.String()
	if r.Profile.ColorLevel == 0 {
		return fmt.Sprintf("%-*s", width, name)
	}
	var styled string
	switch state {
	case types.StateRunning:
		styled = AgentStyle.Render(name)
	case types.StateZombie:
		styled = WarningStyle.Render(name)
	case types.StateSuspended:
		styled = WarningStyle.Render(name)
	case types.StateDead:
		styled = MutedStyle.Render(name)
	case types.StateCreated:
		styled = KernelStyle.Render(name)
	default:
		styled = name
	}
	// Pad to width accounting for ANSI escape sequences
	pad := width - len(name)
	if pad > 0 {
		styled += strings.Repeat(" ", pad)
	}
	return styled
}

// FormatSkills formats a skill list for display, truncating if needed.
func FormatSkills(skills []string, maxWidth int, dash string) string {
	if len(skills) == 0 {
		return dash
	}
	joined := strings.Join(skills, ",")
	if len(joined) > maxWidth {
		return joined[:maxWidth-3] + "..."
	}
	return joined
}

// FormatDuration formats a duration for human-readable display.
func FormatDuration(d time.Duration) string {
	switch {
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.1fs", d.Seconds())
	default:
		return fmt.Sprintf("%.1fm", d.Minutes())
	}
}

// FormatTokens formats an integer with comma thousand separators.
func FormatTokens(n int) string {
	s := strconv.Itoa(n)
	if len(s) <= 3 {
		return s
	}
	var buf []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			buf = append(buf, ',')
		}
		buf = append(buf, byte(c))
	}
	return string(buf)
}

// truncateUUID truncates a UUID for display. Shows first 8 hex chars + "...".
func truncateUUID(uuid string, maxWidth int) string {
	if uuid == "" {
		return "—"
	}
	if len(uuid) <= maxWidth {
		return uuid
	}
	return uuid[:8] + "..."
}

// StripAnsi removes ANSI escape sequences from a string to calculate visible width.
func StripAnsi(s string) string {
	var out strings.Builder
	inEscape := false
	for _, r := range s {
		if r == '\033' {
			inEscape = true
			continue
		}
		if inEscape {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}

// FormatRelativeTime formats a time as a relative duration string like "3s ago", "2m ago".
func FormatRelativeTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", max(int(d.Seconds()), 0))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
}

// FormatWallClock formats a time as HH:MM:SS for dashboard display.
// Returns "—" for zero time.
func FormatWallClock(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Format("15:04:05")
}

// FormatWallClockShort formats a time as HH:MM for narrow dashboard display.
// Returns "—" for zero time.
func FormatWallClockShort(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Format("15:04")
}

// FormatOffsetFromStart formats a duration offset from process start
// as a compact string like "+0.0s", "+5.2s", "+2m13s", "+1h05m".
func FormatOffsetFromStart(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("+%.1fs", d.Seconds())
	case d < time.Hour:
		m := int(d.Minutes())
		s := int(d.Seconds()) % 60
		return fmt.Sprintf("+%dm%02ds", m, s)
	default:
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		return fmt.Sprintf("+%dh%02dm", h, m)
	}
}

// IsStale returns true if a process has exceeded its step timeout.
// Returns false if timeout is disabled (0) or heartbeat is zero.
func IsStale(lastHeartbeat time.Time, stepTimeout time.Duration) bool {
	if stepTimeout <= 0 || lastHeartbeat.IsZero() {
		return false
	}
	return time.Since(lastHeartbeat) > stepTimeout
}

// FormatTokensBudget formats token usage with optional budget limit.
// Returns "12,345/50,000" when maxTokens > 0, or "12,345" otherwise.
func FormatTokensBudget(used int, maxTokens int64) string {
	if maxTokens > 0 {
		return FormatTokens(used) + "/" + FormatTokens(int(maxTokens))
	}
	return FormatTokens(used)
}

// FormatCostBudget formats cost usage with budget limit.
// Returns "$0.15/$1.00" when maxCost > 0, or "-" otherwise.
func FormatCostBudget(usedCost, maxCost float64) string {
	if maxCost > 0 {
		return fmt.Sprintf("$%.2f/$%.2f", usedCost, maxCost)
	}
	if usedCost > 0 {
		return fmt.Sprintf("$%.2f", usedCost)
	}
	return "-"
}
