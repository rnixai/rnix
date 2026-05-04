// Package inspector — diff helpers extracted from cmd/rnix per Story 38-5
// PR11 Step 4(a-2). These were previously private helpers in
// cmd/rnix/dashboard_inspector_diff.go (Story 36-6 / 38-3 diff mode).
//
// Behaviour contract: each function is a 1:1 port of the original
// cmd/rnix helper. cmd/rnix retains thin wrappers (lowercase aliases)
// so existing callsites and tests remain zero-modification.
//
// Design decisions:
//   - All exports are pure functions (no dashboardModel dependency).
//   - DiffKind / DiffLine / FollowLiveTickMsg are migrated as exported
//     types; cmd/rnix retains type aliases (`type diffLine = inspector.DiffLine`)
//     so existing struct-literal usage and `case` type switches work unchanged.
//   - Constants exposed as exported values; cmd/rnix declares aliases for
//     internal use (`const diffEqual = inspector.DiffEqual`).
//   - This keeps Story 36-6 AC-1/3/4 (dd window) + Story 38-3 diff mode
//     visual contracts behaviourally identical.
package inspector

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
)

// DiffKind tags each line in a line-level diff.
type DiffKind int

// Diff line kinds — keep numeric values stable for cmd/rnix const aliases.
const (
	DiffEqual DiffKind = iota
	DiffAdd
	DiffDel
)

// DiffLine is a single unified-diff line. Field names are exported so
// cmd/rnix struct literals (`diffLine{kind: ..., text: ...}`) continue to
// work via the type alias path — but new construction in the inspector
// package should use the exported names directly.
type DiffLine struct {
	Kind DiffKind
	Text string
}

// Diff render thresholds — exposed for cmd/rnix const aliases.
const (
	// DiffFoldThreshold — runs of consecutive equal lines >= this many are
	// rendered as a single fold placeholder unless the caller marks the
	// region expanded in the `unfolded` map.
	DiffFoldThreshold = 3

	// DiffMaxLines — refuse to diff beyond this; callers above this limit
	// should render a "content too large" placeholder instead.
	DiffMaxLines = 5000
)

// ComputeLineDiff returns a unified line diff of base→current using the
// standard LCS dynamic-programming algorithm. Output is ordered from the
// top of the two inputs to the bottom, with deletes attached to their
// base position and adds attached to their current position.
//
// Complexity: O(len(base) * len(current)) time and space. Acceptable for
// Lens contents up to a few thousand lines; callers above DiffMaxLines
// should render a "content too large" placeholder instead.
//
// Behaviour contract (preserved from cmd/rnix.computeLineDiff):
//   - n=0 && m=0 → nil (zero-length input → nil result)
//   - n=0 → all DiffAdd entries (every current line is "new")
//   - m=0 → all DiffDel entries (every base line is "removed")
//   - LCS backtrack from (n, m) to (0, 0); reversed output flipped before return
//   - Tie-break: when dp[i-1][j] >= dp[i][j-1], the diff prefers the
//     deletion path (matches the cmd/rnix algorithm).
func ComputeLineDiff(base, current []string) []DiffLine {
	n, m := len(base), len(current)
	if n == 0 && m == 0 {
		return nil
	}
	if n == 0 {
		out := make([]DiffLine, 0, m)
		for _, line := range current {
			out = append(out, DiffLine{Kind: DiffAdd, Text: line})
		}
		return out
	}
	if m == 0 {
		out := make([]DiffLine, 0, n)
		for _, line := range base {
			out = append(out, DiffLine{Kind: DiffDel, Text: line})
		}
		return out
	}

	// LCS DP table
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			if base[i-1] == current[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}

	// Backtrack from (n, m) to (0, 0). Produces reversed output; flip at the end.
	out := make([]DiffLine, 0, n+m)
	i, j := n, m
	for i > 0 || j > 0 {
		switch {
		case i > 0 && j > 0 && base[i-1] == current[j-1]:
			out = append(out, DiffLine{Kind: DiffEqual, Text: base[i-1]})
			i--
			j--
		case j > 0 && (i == 0 || dp[i][j-1] >= dp[i-1][j]):
			out = append(out, DiffLine{Kind: DiffAdd, Text: current[j-1]})
			j--
		default:
			out = append(out, DiffLine{Kind: DiffDel, Text: base[i-1]})
			i--
		}
	}
	for a, b := 0, len(out)-1; a < b; a, b = a+1, b-1 {
		out[a], out[b] = out[b], out[a]
	}
	return out
}

// RenderDiff formats a sequence of diff lines into a display string.
// Consecutive equal runs of length >= DiffFoldThreshold are replaced by a
// single fold placeholder unless the caller has marked that region
// expanded in `unfolded` (keyed by the start-index of the run within
// `lines`). asciiMode drops lipgloss colour styling, keeping the
// `+ / - / ` prefixes intact.
//
// Behaviour contract (preserved from cmd/rnix.renderDiff):
//   - Empty input → empty string
//   - asciiMode=true → all styles cleared (no colour bytes; prefixes intact)
//   - Equal run of length >= DiffFoldThreshold and not unfolded → single
//     fold notice line "  ... N unchanged lines (Enter 展开) ..."
//   - Equal run unfolded → render every line with leading space prefix
//   - DiffAdd → "+" prefix + addStyle.Render
//   - DiffDel → "-" prefix + delStyle.Render
//   - Each rendered line ends with "\n" (the final line also has trailing newline)
func RenderDiff(lines []DiffLine, unfolded map[int]bool, asciiMode bool) string {
	if len(lines) == 0 {
		return ""
	}

	addStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorSuccess))
	delStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError))
	eqStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))
	foldStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))
	if asciiMode {
		addStyle = lipgloss.NewStyle()
		delStyle = lipgloss.NewStyle()
		eqStyle = lipgloss.NewStyle()
		foldStyle = lipgloss.NewStyle()
	}

	var b strings.Builder
	i := 0
	for i < len(lines) {
		if lines[i].Kind == DiffEqual {
			j := i
			for j < len(lines) && lines[j].Kind == DiffEqual {
				j++
			}
			run := j - i
			if run >= DiffFoldThreshold && (unfolded == nil || !unfolded[i]) {
				b.WriteString(foldStyle.Render(fmt.Sprintf("  ... %d unchanged lines (Enter 展开) ...", run)))
				b.WriteString("\n")
			} else {
				for k := i; k < j; k++ {
					b.WriteString(eqStyle.Render(" " + lines[k].Text))
					b.WriteString("\n")
				}
			}
			i = j
			continue
		}
		if lines[i].Kind == DiffAdd {
			b.WriteString(addStyle.Render("+" + lines[i].Text))
		} else {
			b.WriteString(delStyle.Render("-" + lines[i].Text))
		}
		b.WriteString("\n")
		i++
	}
	return b.String()
}

// RenderDiffBasePicker draws a horizontal base-picker overlay listing the
// available step numbers with the current cursor position highlighted.
// width is the available display width (currently unused — reserved for
// future wrap behaviour); output is a single line.
//
// Behaviour contract (preserved from cmd/rnix.renderDiffBasePicker):
//   - Empty steps → empty string
//   - cursor < 0 → clamped to 0
//   - cursor >= len(steps) → clamped to len(steps)-1
//   - asciiMode → arrows degrade ← → to < >
//   - Active item rendered with Bold + Reverse style; others dim Muted
//   - Output starts " Pick base: " and ends with "  Enter=select  Esc=cancel"
//   - The width parameter is intentionally currently ignored (`_ = width`)
func RenderDiffBasePicker(steps []ipc.StepSummaryWire, cursor int, width int) string {
	if len(steps) == 0 {
		return ""
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= len(steps) {
		cursor = len(steps) - 1
	}

	ascii := ui.IsASCIIMode()
	arrowL, arrowR := "←", "→"
	if ascii {
		arrowL, arrowR = "<", ">"
	}

	activeStyle := lipgloss.NewStyle().Bold(true).Reverse(true)
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))

	var b strings.Builder
	b.WriteString(dimStyle.Render(" Pick base: "))
	b.WriteString(dimStyle.Render(arrowL + " "))
	for i, s := range steps {
		label := fmt.Sprintf("#%d", s.Step)
		if i == cursor {
			b.WriteString(activeStyle.Render("[" + label + "]"))
		} else {
			b.WriteString(dimStyle.Render(" " + label + " "))
		}
	}
	b.WriteString(dimStyle.Render(" " + arrowR))
	b.WriteString(dimStyle.Render("  Enter=select  Esc=cancel"))

	_ = width
	return b.String()
}

// DDWindow is the inter-tap window within which two `d` presses are
// treated as the `dd` sequence that opens the diff base picker.
// Story 36-6 AC-3.
const DDWindow = 200 * time.Millisecond

// FollowLiveTickInterval is the polling cadence for auto-following new
// steps while Follow live is active. Chosen to feel responsive without
// spamming IPC. Story 36-6 AC-13.
const FollowLiveTickInterval = 800 * time.Millisecond

// FollowLiveTickMsg wakes the Update loop so the dashboard can refresh
// the step list and schedule the next tick. Follow auto-cancels itself
// by returning a nil cmd when inspectorFollowLive is false at tick time.
// The Gen field identifies the Follow activation generation — stale
// ticks scheduled during a previous on-period are discarded to avoid
// tick multiplication under rapid F toggles.
//
// Field names are exported so cmd/rnix `case followLiveTickMsg:` switches
// continue to work via the type alias path.
type FollowLiveTickMsg struct {
	PID  types.PID
	UUID string
	Gen  int
}

// FollowLiveTickCmd schedules a single follow-live tick. Callers should
// issue this only while inspectorFollowLive is true; the handler re-arms
// the timer.
//
// Behaviour contract (preserved from cmd/rnix.followLiveTickCmd):
//   - Returns a tea.Cmd that fires after FollowLiveTickInterval
//   - The fired msg is FollowLiveTickMsg{PID, UUID, Gen} (matches caller args)
//   - Cmd is nil-safe to invoke (tea.Tick handles cancellation cleanly)
func FollowLiveTickCmd(pid types.PID, uuid string, gen int) tea.Cmd {
	return tea.Tick(FollowLiveTickInterval, func(time.Time) tea.Msg {
		return FollowLiveTickMsg{PID: pid, UUID: uuid, Gen: gen}
	})
}
