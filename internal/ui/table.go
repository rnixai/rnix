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
	colWidthTokens  = 8
	colWidthElapsed = 8
	colWidthPPID    = 5
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
func RenderProcessTable(r *Renderer, procs []vfs.ProcInfo, verbose bool) {
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
		sepLine.WriteString(gap)
		sepLine.WriteString(strings.Repeat(sep, colWidthElapsed))
	}
	if showIntent {
		sepLine.WriteString(gap)
		sepLine.WriteString(strings.Repeat(sep, 6))
	}
	fmt.Fprintln(r.Writer, sepLine.String())

	// Render rows
	var active, zombie, total int
	for _, proc := range procs {
		total++
		switch proc.State {
		case types.StateRunning, types.StateCreated:
			active++
		case types.StateZombie:
			zombie++
		}

		var row strings.Builder
		fmt.Fprintf(&row, "%*d", colWidthPID, proc.PID)
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
			fmt.Fprintf(&row, "%*s", colWidthTokens, FormatTokens(proc.TokensUsed))
			row.WriteString(gap)
			elapsed := time.Since(proc.CreatedAt)
			fmt.Fprintf(&row, "%*s", colWidthElapsed, FormatDuration(elapsed))
		}
		if showIntent {
			row.WriteString(gap)
			intentWidth := w - len(StripAnsi(row.String()))
			if intentWidth < 0 {
				intentWidth = 0
			}
			intent := proc.Intent
			if !verbose && intentWidth > 3 && len(intent) > intentWidth {
				intent = intent[:intentWidth-3] + "..."
			}
			row.WriteString(intent)
		}
		fmt.Fprintln(r.Writer, row.String())
	}

	// Footer
	fmt.Fprintf(r.Writer, "\n%d active, %d zombie, %d total\n", active, zombie, total)
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
