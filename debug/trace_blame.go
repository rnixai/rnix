package debug

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// BlameEntry represents a single hotspot entry with rank and percentage.
type BlameEntry struct {
	Span       *Span
	Percentage float64
	Rank       int
}

// ErrorChain represents a path from root cause to the final failure point.
type ErrorChain struct {
	Path      []*Span
	RootCause *Span
}

// BlameSummary holds aggregate statistics for the blame analysis.
type BlameSummary struct {
	TotalSpans      int
	ErrorSpans      int
	TotalDuration   time.Duration
	TotalTokens     int
	CriticalPathPct float64
}

// BlameResult contains the complete blame analysis for a trace.
type BlameResult struct {
	TraceID          string
	CriticalPath     []*SpanNode
	CriticalDuration time.Duration
	DurationHotspots []*BlameEntry
	TokenHotspots    []*BlameEntry
	ErrorChains      []ErrorChain
	Summary          BlameSummary
}

const blameTopN = 3

// AnalyzeTrace performs root-cause analysis on a SpanTree.
func AnalyzeTrace(tree *SpanTree) *BlameResult {
	if tree == nil || tree.Root == nil {
		return nil
	}

	critPath, critDur := findCriticalPath(tree)
	durHotspots := findDurationHotspots(tree, blameTopN)
	tokHotspots := findTokenHotspots(tree, blameTopN)
	errChains := findErrorChains(tree)

	var errCount int
	tree.Walk(func(node *SpanNode, _ int) {
		if node.Span.Status == SpanERROR || node.Span.Status == SpanTIMEOUT {
			errCount++
		}
	})

	var critPct float64
	if tree.Metadata.TotalDuration > 0 {
		critPct = float64(critDur) / float64(tree.Metadata.TotalDuration) * 100
	}

	return &BlameResult{
		TraceID:          tree.TraceID,
		CriticalPath:     critPath,
		CriticalDuration: critDur,
		DurationHotspots: durHotspots,
		TokenHotspots:    tokHotspots,
		ErrorChains:      errChains,
		Summary: BlameSummary{
			TotalSpans:      tree.Metadata.TotalSpans,
			ErrorSpans:      errCount,
			TotalDuration:   tree.Metadata.TotalDuration,
			TotalTokens:     tree.Metadata.TotalTokens,
			CriticalPathPct: critPct,
		},
	}
}

// findCriticalPath returns the root-to-leaf path with the longest cumulative duration.
func findCriticalPath(tree *SpanTree) ([]*SpanNode, time.Duration) {
	if tree.Root == nil {
		return nil, 0
	}

	var bestPath []*SpanNode
	var bestDur time.Duration

	var dfs func(node *SpanNode, path []*SpanNode, cumDur time.Duration)
	dfs = func(node *SpanNode, path []*SpanNode, cumDur time.Duration) {
		// Defensive copy avoids slice aliasing when siblings share underlying array
		current := make([]*SpanNode, len(path)+1)
		copy(current, path)
		current[len(path)] = node
		cumDur += node.Span.Duration

		if len(node.Children) == 0 {
			if cumDur > bestDur {
				bestDur = cumDur
				bestPath = current
			}
			return
		}

		for _, child := range node.Children {
			dfs(child, current, cumDur)
		}
	}

	dfs(tree.Root, nil, 0)
	return bestPath, bestDur
}

// findDurationHotspots returns the top-N spans by Duration descending.
func findDurationHotspots(tree *SpanTree, topN int) []*BlameEntry {
	var all []*Span
	tree.Walk(func(node *SpanNode, _ int) {
		all = append(all, node.Span)
	})

	sort.Slice(all, func(i, j int) bool {
		return all[i].Duration > all[j].Duration
	})

	totalDur := tree.Metadata.TotalDuration
	n := topN
	if n > len(all) {
		n = len(all)
	}

	entries := make([]*BlameEntry, n)
	for i := 0; i < n; i++ {
		var pct float64
		if totalDur > 0 {
			pct = float64(all[i].Duration) / float64(totalDur) * 100
		}
		entries[i] = &BlameEntry{
			Span:       all[i],
			Percentage: pct,
			Rank:       i + 1,
		}
	}
	return entries
}

// findTokenHotspots returns the top-N spans by TokensUsed descending.
func findTokenHotspots(tree *SpanTree, topN int) []*BlameEntry {
	var all []*Span
	tree.Walk(func(node *SpanNode, _ int) {
		all = append(all, node.Span)
	})

	sort.Slice(all, func(i, j int) bool {
		return all[i].TokensUsed > all[j].TokensUsed
	})

	totalTok := tree.Metadata.TotalTokens
	n := topN
	if n > len(all) {
		n = len(all)
	}

	entries := make([]*BlameEntry, n)
	for i := 0; i < n; i++ {
		var pct float64
		if totalTok > 0 {
			pct = float64(all[i].TokensUsed) / float64(totalTok) * 100
		}
		entries[i] = &BlameEntry{
			Span:       all[i],
			Percentage: pct,
			Rank:       i + 1,
		}
	}
	return entries
}

// findErrorChains finds all error propagation chains by tracing from error leaf nodes back to root.
func findErrorChains(tree *SpanTree) []ErrorChain {
	if tree.Root == nil {
		return nil
	}

	parentMap := buildParentMap(tree)

	var errorLeaves []*SpanNode
	tree.Walk(func(node *SpanNode, _ int) {
		if (node.Span.Status == SpanERROR || node.Span.Status == SpanTIMEOUT) && len(node.Children) == 0 {
			errorLeaves = append(errorLeaves, node)
		}
	})

	// Also include error nodes that have only OK children (the error originates here)
	tree.Walk(func(node *SpanNode, _ int) {
		if node.Span.Status != SpanERROR && node.Span.Status != SpanTIMEOUT {
			return
		}
		if len(node.Children) == 0 {
			return // already captured above
		}
		allChildrenOK := true
		for _, child := range node.Children {
			if child.Span.Status == SpanERROR || child.Span.Status == SpanTIMEOUT {
				allChildrenOK = false
				break
			}
		}
		if allChildrenOK {
			errorLeaves = append(errorLeaves, node)
		}
	})

	if len(errorLeaves) == 0 {
		return nil
	}

	var chains []ErrorChain
	for _, errNode := range errorLeaves {
		path := buildPathToRoot(errNode, parentMap)

		var rootCause *Span
		for _, n := range path {
			if n.Span.Status == SpanERROR || n.Span.Status == SpanTIMEOUT {
				rootCause = n.Span
				break
			}
		}
		if rootCause == nil {
			rootCause = errNode.Span
		}

		chains = append(chains, ErrorChain{
			Path:      spansFromPath(path),
			RootCause: rootCause,
		})
	}

	return chains
}

func buildParentMap(tree *SpanTree) map[*SpanNode]*SpanNode {
	pm := make(map[*SpanNode]*SpanNode)
	var walk func(node *SpanNode)
	walk = func(node *SpanNode) {
		for _, child := range node.Children {
			pm[child] = node
			walk(child)
		}
	}
	walk(tree.Root)
	return pm
}

func buildPathToRoot(node *SpanNode, parentMap map[*SpanNode]*SpanNode) []*SpanNode {
	var path []*SpanNode
	current := node
	for current != nil {
		path = append(path, current)
		current = parentMap[current]
	}
	// Reverse: root first
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path
}

func spansFromPath(nodes []*SpanNode) []*Span {
	spans := make([]*Span, len(nodes))
	for i, n := range nodes {
		spans[i] = n.Span
	}
	return spans
}

// FormatBlameResult formats a BlameResult as a human-readable string.
func FormatBlameResult(result *BlameResult) string {
	if result == nil {
		return "No blame data available.\n"
	}

	var b strings.Builder

	errLabel := ""
	if result.Summary.ErrorSpans > 0 {
		errLabel = fmt.Sprintf("  |  %d errors", result.Summary.ErrorSpans)
	}
	b.WriteString(fmt.Sprintf("Blame: %s  |  %d spans%s\n\n",
		result.TraceID,
		result.Summary.TotalSpans,
		errLabel,
	))

	// Critical Path
	b.WriteString(fmt.Sprintf("── Critical Path (%.1f%% of total) ──────────────────────\n",
		result.Summary.CriticalPathPct))
	for _, node := range result.CriticalPath {
		s := node.Span
		b.WriteString(fmt.Sprintf("→ %s (PID %d)%s%s\n",
			s.Name, s.PID,
			blamePadTo(s.Name, s.PID, 40),
			fmt.Sprintf("%s   %d tok", formatDuration(s.Duration), s.TokensUsed),
		))
	}
	b.WriteString(fmt.Sprintf("  Total: %s / %s\n\n",
		formatDuration(result.CriticalDuration),
		formatDuration(result.Summary.TotalDuration),
	))

	// Duration Hotspots
	b.WriteString("── Duration Hotspots ───────────────────────────────────\n")
	for _, e := range result.DurationHotspots {
		b.WriteString(fmt.Sprintf("#%d  %s (PID %d)%s%s   %.1f%%\n",
			e.Rank, e.Span.Name, e.Span.PID,
			blamePadTo(e.Span.Name, e.Span.PID, 34),
			formatDuration(e.Span.Duration),
			e.Percentage,
		))
	}
	b.WriteString("\n")

	// Token Hotspots
	b.WriteString("── Token Hotspots ──────────────────────────────────────\n")
	for _, e := range result.TokenHotspots {
		b.WriteString(fmt.Sprintf("#%d  %s (PID %d)%s%d tok   %.1f%%\n",
			e.Rank, e.Span.Name, e.Span.PID,
			blamePadTo(e.Span.Name, e.Span.PID, 34),
			e.Span.TokensUsed,
			e.Percentage,
		))
	}

	// Error Chains
	if len(result.ErrorChains) > 0 {
		b.WriteString("\n── Error Chains ────────────────────────────────────────\n")
		for i, chain := range result.ErrorChains {
			b.WriteString(fmt.Sprintf("Chain %d:\n", i+1))
			for j := len(chain.Path) - 1; j >= 0; j-- {
				s := chain.Path[j]
				isRoot := s == chain.RootCause
				prefix := "↑"
				suffix := s.Status.String()
				if isRoot {
					prefix = "✗"
					suffix += " [ROOT CAUSE]"
				} else if s.Status == SpanERROR || s.Status == SpanTIMEOUT {
					prefix = "✗"
				}
				b.WriteString(fmt.Sprintf("  %s %s (PID %d)%s%s\n",
					prefix, s.Name, s.PID,
					blamePadTo(s.Name, s.PID, 36),
					suffix,
				))
			}
		}
	}

	return b.String()
}

func blamePadTo(name string, pid types.PID, width int) string {
	col := fmt.Sprintf("%s (PID %d)", name, pid)
	pad := width - len(col)
	if pad < 2 {
		pad = 2
	}
	return strings.Repeat(" ", pad)
}

// --- JSON serialization ---

type blameEntryJSON struct {
	blameSpanJSON
	Percentage float64 `json:"percentage"`
	Rank       int     `json:"rank"`
}

type errorChainJSON struct {
	Path      []blameSpanJSON `json:"path"`
	RootCause blameSpanJSON   `json:"root_cause"`
}

type blameSpanJSON struct {
	TraceID    types.TraceID `json:"trace_id"`
	SpanID     types.SpanID  `json:"span_id"`
	PID        types.PID     `json:"pid"`
	Name       string        `json:"name"`
	DurationMs int64         `json:"duration_ms"`
	TokensUsed int           `json:"tokens_used"`
	Status     SpanStatus    `json:"status"`
}

type blameSummaryJSON struct {
	TotalSpans      int     `json:"total_spans"`
	ErrorSpans      int     `json:"error_spans"`
	TotalDurationMs int64   `json:"total_duration_ms"`
	TotalTokens     int     `json:"total_tokens"`
	CriticalPathPct float64 `json:"critical_path_pct"`
}

type blameResultJSON struct {
	TraceID            string           `json:"trace_id"`
	CriticalPath       []blameSpanJSON  `json:"critical_path"`
	CriticalDurationMs int64            `json:"critical_duration_ms"`
	DurationHotspots   []blameEntryJSON `json:"duration_hotspots"`
	TokenHotspots      []blameEntryJSON `json:"token_hotspots"`
	ErrorChains        []errorChainJSON `json:"error_chains"`
	Summary            blameSummaryJSON `json:"summary"`
}

func spanToBlameJSON(s *Span) blameSpanJSON {
	return blameSpanJSON{
		TraceID:    s.TraceID,
		SpanID:     s.SpanID,
		PID:        s.PID,
		Name:       s.Name,
		DurationMs: s.Duration.Milliseconds(),
		TokensUsed: s.TokensUsed,
		Status:     s.Status,
	}
}

func (r *BlameResult) MarshalJSON() ([]byte, error) {
	out := blameResultJSON{
		TraceID:            r.TraceID,
		CriticalDurationMs: r.CriticalDuration.Milliseconds(),
		Summary: blameSummaryJSON{
			TotalSpans:      r.Summary.TotalSpans,
			ErrorSpans:      r.Summary.ErrorSpans,
			TotalDurationMs: r.Summary.TotalDuration.Milliseconds(),
			TotalTokens:     r.Summary.TotalTokens,
			CriticalPathPct: r.Summary.CriticalPathPct,
		},
	}

	for _, node := range r.CriticalPath {
		out.CriticalPath = append(out.CriticalPath, spanToBlameJSON(node.Span))
	}

	for _, e := range r.DurationHotspots {
		out.DurationHotspots = append(out.DurationHotspots, blameEntryJSON{
			blameSpanJSON: spanToBlameJSON(e.Span),
			Percentage:    e.Percentage,
			Rank:          e.Rank,
		})
	}

	for _, e := range r.TokenHotspots {
		out.TokenHotspots = append(out.TokenHotspots, blameEntryJSON{
			blameSpanJSON: spanToBlameJSON(e.Span),
			Percentage:    e.Percentage,
			Rank:          e.Rank,
		})
	}

	for _, chain := range r.ErrorChains {
		ec := errorChainJSON{
			RootCause: spanToBlameJSON(chain.RootCause),
		}
		for _, s := range chain.Path {
			ec.Path = append(ec.Path, spanToBlameJSON(s))
		}
		out.ErrorChains = append(out.ErrorChains, ec)
	}

	return json.Marshal(out)
}
