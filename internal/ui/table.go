package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mattn/go-runewidth"

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
	colWidthActive  = 13 // Story 66.6: widened for "age/toolcalls" (e.g. "⚠️ 59m/9999")
	colWidthElapsed = 8
	colWidthPPID    = 5
	colWidthUUID    = 7 // "…" / "~" prefix + last 6 chars (ShortUUID form)
	colWidthExit    = 20 // verbose-only EXIT reason column (display width)
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
		if verbose {
			fmt.Fprintf(&hdr, "%-*s", colWidthExit, "EXIT")
			hdr.WriteString(gap)
		}
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
		if verbose {
			sepLine.WriteString(strings.Repeat(sep, colWidthExit))
			sepLine.WriteString(gap)
		}
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
			uuidDisplay := truncateUUID(proc.UUID)
			// %-*s pads by bytes, but the Unicode "…" prefix is 3 bytes / 1
			// column — pad by rune count to keep column alignment.
			row.WriteString(uuidDisplay)
			if pad := colWidthUUID - runeLen(uuidDisplay); pad > 0 {
				row.WriteString(strings.Repeat(" ", pad))
			}
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
			// Story 66.6: guard against estimated usage silently masquerading as a
			// real count — an "estimated" provenance prefixes a "~". No producer
			// emits estimated in this story; the branch exists so any future
			// estimator is visually distinct at the display layer.
			tokStr := FormatTokensBudget(proc.TokensUsed, proc.MaxTokens)
			if proc.UsageProvenance == vfs.UsageProvenanceEstimated {
				tokStr = "~" + tokStr
			}
			fmt.Fprintf(&row, "%*s", colWidthTokens, tokStr)
			if showCost {
				row.WriteString(gap)
				fmt.Fprintf(&row, "%*s", colWidthCost, FormatCostBudget(proc.UsedCost, proc.MaxCost))
			}
			row.WriteString(gap)
			// ACTIVE column: compact last-event age + cumulative tool-call count
			// (Story 66.6) — e.g. "3s/594" — so the column keeps moving during a
			// long agentic step even when usage data is unavailable.
			var activeStr string
			switch proc.State {
			case types.StateRunning, types.StateCreated:
				activeStr = FormatActivity(proc.LastHeartbeat, proc.ToolCallCount, dash)
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
			if verbose {
				// EXIT column: the authoritative exit reason for dead/zombie
				// processes (e.g. an async HTTP 429 rate-limit that killed the
				// process after resume). Display-width-aware truncate + pad so
				// CJK reasons keep column alignment.
				exitStr := dash
				if proc.ExitReason != "" {
					exitStr = runewidth.Truncate(proc.ExitReason, colWidthExit, "…")
					if !r.Profile.IsUnicode {
						exitStr = runewidth.Truncate(proc.ExitReason, colWidthExit, "...")
					}
				}
				if pad := colWidthExit - runeLen(exitStr); pad > 0 {
					exitStr += strings.Repeat(" ", pad)
				}
				row.WriteString(exitStr)
				row.WriteString(gap)
			}
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

// truncateUUID renders a UUID for the ps table UUID column via ShortUUID
// (suffix short form). An empty UUID renders as an em dash placeholder.
func truncateUUID(uuid string) string {
	if uuid == "" {
		return "—"
	}
	return ShortUUID(uuid)
}

// ShortUUID returns the global short display identifier for a UUID: an
// ellipsis prefix ("…", or "~" when RNIX_ASCII=1) followed by the LAST 6
// runes.
//
// Suffix rather than prefix: process UUIDs are UUIDv7 (time-ordered), so
// processes created in the same time window share nearly identical leading
// characters (the timestamp half) and a prefix has no distinguishing power.
// The random entropy concentrates in the tail, making the last 6 characters
// highly distinctive.
//
// Inputs shorter than 6 runes (including "") are returned unchanged so
// callers' existing empty/short-value handling stays in effect. Rune-based
// slicing keeps the result valid UTF-8 even for dirty multi-byte data.
//
// Width invariant: the result always renders 7 columns for canonical UUIDs.
// U+2026 "…" is East-Asian-Ambiguous — in CJK locales runewidth resolves it
// to 2 columns, which would silently break every fixed-width table column
// sized for 7 — so those environments fall back to the ASCII "~" marker.
func ShortUUID(uuid string) string {
	r := []rune(uuid)
	if len(r) < 6 {
		return uuid
	}
	tail := string(r[len(r)-6:])
	if IsASCIIMode() || runewidth.RuneWidth('…') != 1 {
		return "~" + tail
	}
	return "…" + tail
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

// nowFunc returns the current time. Package-level indirection so tests can
// inject a fixed "now" for the date-aware formatting helpers below.
var nowFunc = time.Now

// FormatRelativeTime formats a time as a relative duration string like
// "3s ago", "2m ago", "5h ago", "2d ago".
func FormatRelativeTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := nowFunc().Sub(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", max(int(d.Seconds()), 0))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// FormatActivity renders the ps ACTIVE column (Story 66.6): a compact
// last-event age plus the cumulative tool-call count in the form "3s/594". The
// count is omitted when zero to keep the fresh-spawn view clean; a zero
// heartbeat (no driver activity yet) renders the dash placeholder. dash is the
// caller's unicode/ascii-aware em-dash so callers keep a single glyph policy.
func FormatActivity(lastHeartbeat time.Time, toolCallCount int, dash string) string {
	if lastHeartbeat.IsZero() {
		return dash
	}
	age := formatAgeShort(nowFunc().Sub(lastHeartbeat))
	if toolCallCount > 0 {
		return age + "/" + strconv.Itoa(toolCallCount)
	}
	return age
}

// formatAgeShort renders a duration as a compact age ("3s", "5m", "2h", "1d")
// without the " ago" suffix FormatRelativeTime uses — the ACTIVE column packs
// the age next to a tool-call count and cannot spare the suffix width.
func formatAgeShort(d time.Duration) string {
	switch {
	case d < time.Minute:
		return strconv.Itoa(max(int(d.Seconds()), 0)) + "s"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h"
	default:
		return strconv.Itoa(int(d.Hours()/24)) + "d"
	}
}

// formatWallClockIn renders t with the date-aware layout: same calendar day
// as now → time only (zero noise on the high-frequency today view); same
// year but a different day → "01-02 " date prefix; different year → full
// "2006-01-02 " prefix.
//
// t is normalized to now's location BEFORE the calendar-day comparison and
// the rendering — comparing t.Date() in t's own zone against now.Date() in
// the local zone would misjudge "today" near midnight for non-local inputs
// (e.g. a UTC-parsed timestamp on a UTC+8 host), and would print a clock
// the user can't correlate with their wall time.
func formatWallClockIn(t time.Time, timeLayout string) string {
	now := nowFunc()
	t = t.In(now.Location())
	ty, tm, td := t.Date()
	ny, nm, nd := now.Date()
	switch {
	case ty == ny && tm == nm && td == nd:
		return t.Format(timeLayout)
	case ty == ny:
		return t.Format("01-02 " + timeLayout)
	default:
		return t.Format("2006-01-02 " + timeLayout)
	}
}

// FormatWallClock formats a time for dashboard display: "15:04:05" for
// today, "01-02 15:04:05" for another day this year, "2006-01-02 15:04:05"
// across years. Returns "—" for zero time.
func FormatWallClock(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return formatWallClockIn(t, "15:04:05")
}

// FormatWallClockShort formats a time for narrow dashboard display:
// "15:04" for today, "01-02 15:04" for another day this year,
// "2006-01-02 15:04" across years. Returns "—" for zero time.
func FormatWallClockShort(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return formatWallClockIn(t, "15:04")
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
