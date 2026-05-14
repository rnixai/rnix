package ui

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/rnixai/rnix/compose"
	"github.com/rnixai/rnix/internal/types"
)

// composeAgentStatus determines the display status for a compose agent result.
func composeAgentStatus(r compose.ScheduleResult) string {
	if r.Err != nil {
		if r.PID == 0 && strings.Contains(r.Err.Error(), "upstream dependency failed") {
			return "skipped"
		}
		return "failed"
	}
	return "done"
}

// RenderComposeProgress outputs a real-time progress line for a compose agent.
// Format: [compose] [N/total] name: status (exit_code)
func RenderComposeProgress(r *Renderer, name string, status string, index int, total int, exitCode int) {
	if r.OutputMode == ModeQuiet || r.OutputMode == ModeJSON {
		return
	}

	prefix := KernelStyle.Render("[compose]")
	counter := MutedStyle.Render(fmt.Sprintf("[%d/%d]", index, total))

	var line string
	switch status {
	case "spawning":
		line = fmt.Sprintf("%s %s %s: %s", prefix, counter, name, status)
	case "done":
		line = fmt.Sprintf("%s %s %s: %s (%d)", prefix, counter, name, SuccessStyle.Render("done"), exitCode)
	case "failed":
		line = fmt.Sprintf("%s %s %s: %s (%d)", prefix, counter, name, ErrorStyle.Render("failed"), exitCode)
	case "skipped":
		line = fmt.Sprintf("%s %s %s: %s", prefix, counter, name, WarningStyle.Render("skipped"))
	default:
		line = fmt.Sprintf("%s %s %s: %s", prefix, counter, name, status)
	}

	fmt.Fprintln(r.Writer, line)
}

// RenderComposeHeader outputs the compose orchestration header line.
func RenderComposeHeader(r *Renderer, intent string) {
	if r.OutputMode == ModeQuiet || r.OutputMode == ModeJSON {
		return
	}

	prefix := KernelStyle.Render("[compose]")
	fmt.Fprintf(r.Writer, "%s Starting orchestration: %q\n", prefix, intent)
}

// RenderComposeSummary outputs the compose orchestration summary table.
func RenderComposeSummary(r *Renderer, results []compose.ScheduleResult, tokenMap map[string]int) {
	if r.OutputMode == ModeQuiet {
		return
	}

	prefix := KernelStyle.Render("[compose]")
	fmt.Fprintf(r.Writer, "%s Orchestration complete\n\n", prefix)

	// Calculate counts
	var succeeded, failed, skipped int
	var totalDuration time.Duration
	var totalTokens int
	for _, res := range results {
		status := composeAgentStatus(res)
		switch status {
		case "done":
			succeeded++
		case "failed":
			failed++
		case "skipped":
			skipped++
		}
		totalDuration += res.Duration
		if tokenMap != nil {
			totalTokens += tokenMap[res.Name]
		}
	}
	total := len(results)

	// Header
	fmt.Fprintf(r.Writer, "  %-16s %-10s %-6s %-8s %s\n", "Agent", "Status", "Exit", "Tokens", "Duration")
	fmt.Fprintf(r.Writer, "  %-16s %-10s %-6s %-8s %s\n",
		strings.Repeat("\u2500", 16),
		strings.Repeat("\u2500", 10),
		strings.Repeat("\u2500", 6),
		strings.Repeat("\u2500", 8),
		strings.Repeat("\u2500", 8))

	// Rows
	for _, res := range results {
		status := composeAgentStatus(res)

		exitStr := fmt.Sprintf("%d", res.ExitCode)
		durStr := fmt.Sprintf("%.1fs", res.Duration.Seconds())
		tokenStr := "-"

		if tokenMap != nil {
			if t, ok := tokenMap[res.Name]; ok && t > 0 {
				tokenStr = formatTokenCount(t)
			}
		}

		if status == "skipped" {
			exitStr = "-"
			durStr = "-"
			tokenStr = "-"
		}

		var styledStatus string
		switch status {
		case "done":
			styledStatus = SuccessStyle.Render(status)
		case "failed":
			styledStatus = ErrorStyle.Render(status)
		case "skipped":
			styledStatus = WarningStyle.Render(status)
		}

		fmt.Fprintf(r.Writer, "  %-16s %-10s %-6s %-8s %s\n", res.Name, styledStatus, exitStr, tokenStr, durStr)
	}

	// Footer
	fmt.Fprintln(r.Writer)
	fmt.Fprintf(r.Writer, "  Total: %d agents | %d succeeded | %d failed | %d skipped\n",
		total, succeeded, failed, skipped)
	fmt.Fprintf(r.Writer, "  Tokens: %s | Duration: %.1fs\n", formatTokenCount(totalTokens), totalDuration.Seconds())
}

// formatTokenCount formats a token count with k/m suffix.
//
// Story 41.2 AC#6/#7: behavior aligned with timeline.FormatTokenCount (single
// authority for dashboard token display). Local re-implementation avoids a
// cyclic import because internal/dashboard/timeline already imports internal/ui.
//
//   - n < 0           → recursive sign handling ("-1.5k" / "-1.64M")
//   - 0 <= n < 1000   → "%d"            (42 / 999)
//   - 1000 <= n < 1M  → "%.1fk"         (14.1k)
//   - n >= 1M         → "%.2fM"         (1.64M)
func formatTokenCount(n int) string {
	if n == math.MinInt {
		return fmt.Sprintf("%d", n)
	}
	if n < 0 {
		return "-" + formatTokenCount(-n)
	}
	if n >= 1_000_000 {
		return fmt.Sprintf("%.2fM", float64(n)/1_000_000.0)
	}
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000.0)
	}
	return fmt.Sprintf("%d", n)
}

// composeJSONAgent is the JSON representation of a single agent in compose summary.
type composeJSONAgent struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	ExitCode   int    `json:"exit_code"`
	TokensUsed int    `json:"tokens_used"`
	ElapsedMs  int64  `json:"elapsed_ms"`
}

// composeJSONSummary is the JSON summary section.
type composeJSONSummary struct {
	Total          int   `json:"total"`
	Succeeded      int   `json:"succeeded"`
	Failed         int   `json:"failed"`
	Skipped        int   `json:"skipped"`
	TotalTokens    int   `json:"total_tokens"`
	TotalElapsedMs int64 `json:"total_elapsed_ms"`
}

// composeJSONData holds the data section of the JSON response.
type composeJSONData struct {
	Agents  []composeJSONAgent `json:"agents"`
	Summary composeJSONSummary `json:"summary"`
}

// composeJSONResponse is the top-level JSON response.
type composeJSONResponse struct {
	OK   bool            `json:"ok"`
	Data composeJSONData `json:"data"`
}

// RenderComposeSummaryJSON outputs the compose summary as JSON.
func RenderComposeSummaryJSON(r *Renderer, results []compose.ScheduleResult, tokenMap map[string]int) {
	agentEntries := make([]composeJSONAgent, 0, len(results))
	var succeeded, failed, skipped int
	var totalElapsedMs int64
	var totalTokens int

	for _, res := range results {
		status := composeAgentStatus(res)
		switch status {
		case "done":
			succeeded++
		case "failed":
			failed++
		case "skipped":
			skipped++
		}

		elapsedMs := res.Duration.Milliseconds()
		totalElapsedMs += elapsedMs

		tokensUsed := 0
		if tokenMap != nil {
			tokensUsed = tokenMap[res.Name]
		}
		totalTokens += tokensUsed

		agentEntries = append(agentEntries, composeJSONAgent{
			Name:       res.Name,
			Status:     status,
			ExitCode:   res.ExitCode,
			TokensUsed: tokensUsed,
			ElapsedMs:  elapsedMs,
		})
	}

	resp := composeJSONResponse{
		OK: true,
		Data: composeJSONData{
			Agents: agentEntries,
			Summary: composeJSONSummary{
				Total:          len(results),
				Succeeded:      succeeded,
				Failed:         failed,
				Skipped:        skipped,
				TotalTokens:    totalTokens,
				TotalElapsedMs: totalElapsedMs,
			},
		},
	}

	data, _ := json.Marshal(resp)
	fmt.Fprintln(r.Writer, string(data))
}

// --- Compose Down UI ---

// ComposeDownEntry represents a single process entry in compose down results.
type ComposeDownEntry struct {
	PID    types.PID
	Intent string
	State  string // only set for skipped (completed) processes
}

// composeDownJSONEntry is the JSON representation of a compose down process entry.
type composeDownJSONEntry struct {
	PID    types.PID `json:"pid"`
	Intent string    `json:"intent"`
	State  string    `json:"state,omitempty"`
}

// composeDownJSONSummary is the JSON summary for compose down.
type composeDownJSONSummary struct {
	KilledCount  int `json:"killed_count"`
	SkippedCount int `json:"skipped_count"`
	TotalMatched int `json:"total_matched"`
}

// composeDownJSONData holds the data section of compose down JSON response.
type composeDownJSONData struct {
	Killed  []composeDownJSONEntry `json:"killed"`
	Skipped []composeDownJSONEntry `json:"skipped"`
	Errors  []string               `json:"errors,omitempty"`
	Summary composeDownJSONSummary `json:"summary"`
}

// composeDownJSONResponse is the top-level JSON response for compose down.
type composeDownJSONResponse struct {
	OK   bool                `json:"ok"`
	Data composeDownJSONData `json:"data"`
}

// RenderComposeDownHeader outputs the compose down header line.
func RenderComposeDownHeader(r *Renderer, file string) {
	if r.OutputMode == ModeQuiet || r.OutputMode == ModeJSON {
		return
	}
	prefix := KernelStyle.Render("[compose]")
	fmt.Fprintf(r.Writer, "%s Stopping orchestration from %q\n", prefix, file)
}

// RenderComposeDownSummary outputs the compose down teardown summary.
func RenderComposeDownSummary(r *Renderer, killed []ComposeDownEntry, skipped []ComposeDownEntry, errors []string) {
	if r.OutputMode == ModeQuiet {
		return
	}

	prefix := KernelStyle.Render("[compose]")

	for _, entry := range killed {
		fmt.Fprintf(r.Writer, "%s PID %d: killed (SIGTERM) — %q\n", prefix, entry.PID, entry.Intent)
	}
	for _, entry := range skipped {
		fmt.Fprintf(r.Writer, "%s PID %d: skipped (already completed) — %q\n", prefix, entry.PID, entry.Intent)
	}
	for _, errMsg := range errors {
		fmt.Fprintf(r.Writer, "%s %s: %s\n", prefix, ErrorStyle.Render("error"), errMsg)
	}

	fmt.Fprintf(r.Writer, "%s Teardown complete: %d killed, %d skipped\n", prefix, len(killed), len(skipped))
}

// RenderComposeDownSummaryJSON outputs the compose down summary as JSON.
func RenderComposeDownSummaryJSON(r *Renderer, killed []ComposeDownEntry, skipped []ComposeDownEntry, errors []string) {
	killedEntries := make([]composeDownJSONEntry, 0, len(killed))
	for _, entry := range killed {
		killedEntries = append(killedEntries, composeDownJSONEntry{
			PID:    entry.PID,
			Intent: entry.Intent,
		})
	}

	skippedEntries := make([]composeDownJSONEntry, 0, len(skipped))
	for _, entry := range skipped {
		skippedEntries = append(skippedEntries, composeDownJSONEntry(entry))
	}

	resp := composeDownJSONResponse{
		OK: true,
		Data: composeDownJSONData{
			Killed:  killedEntries,
			Skipped: skippedEntries,
			Errors:  errors,
			Summary: composeDownJSONSummary{
				KilledCount:  len(killed),
				SkippedCount: len(skipped),
				TotalMatched: len(killed) + len(skipped),
			},
		},
	}

	data, _ := json.Marshal(resp)
	fmt.Fprintln(r.Writer, string(data))
}
