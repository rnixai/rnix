package debug

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// SpanNode represents a node in the span tree.
type SpanNode struct {
	Span     *Span
	Children []*SpanNode
}

// TraceMetadata holds aggregate statistics for a trace.
type TraceMetadata struct {
	TotalSpans    int
	TotalTokens   int
	TotalDuration time.Duration
	StartTime     time.Time
	EndTime       time.Time
	ErrorCount    int
}

// SpanTree represents the hierarchical structure of spans in a trace.
type SpanTree struct {
	Root     *SpanNode
	TraceID  string
	Metadata TraceMetadata
}

type spanNodeJSON struct {
	TraceID      types.TraceID  `json:"trace_id"`
	SpanID       types.SpanID   `json:"span_id"`
	ParentSpanID types.SpanID   `json:"parent_span_id,omitempty"`
	PID          types.PID      `json:"pid"`
	Name         string         `json:"name"`
	StartTimeMs  int64          `json:"start_time_ms"`
	EndTimeMs    int64          `json:"end_time_ms"`
	DurationMs   int64          `json:"duration_ms"`
	SyscallCount int            `json:"syscall_count"`
	TokensUsed   int            `json:"tokens_used"`
	Status       SpanStatus     `json:"status"`
	Children     []spanNodeJSON `json:"children,omitempty"`
}

type traceMetaJSON struct {
	TotalSpans    int   `json:"total_spans"`
	TotalTokens   int   `json:"total_tokens"`
	TotalDurationMs int64 `json:"total_duration_ms"`
	StartTimeMs   int64 `json:"start_time_ms"`
	EndTimeMs     int64 `json:"end_time_ms"`
	ErrorCount    int   `json:"error_count"`
}

type spanTreeJSON struct {
	Root     *spanNodeJSON `json:"root"`
	TraceID  string        `json:"trace_id"`
	Metadata traceMetaJSON `json:"metadata"`
}

func (t *SpanTree) MarshalJSON() ([]byte, error) {
	out := spanTreeJSON{
		TraceID: t.TraceID,
		Metadata: traceMetaJSON{
			TotalSpans:      t.Metadata.TotalSpans,
			TotalTokens:     t.Metadata.TotalTokens,
			TotalDurationMs: t.Metadata.TotalDuration.Milliseconds(),
			StartTimeMs:     t.Metadata.StartTime.UnixMilli(),
			EndTimeMs:       t.Metadata.EndTime.UnixMilli(),
			ErrorCount:      t.Metadata.ErrorCount,
		},
	}
	if t.Root != nil {
		n := nodeToJSON(t.Root)
		out.Root = &n
	}
	return json.Marshal(out)
}

func nodeToJSON(n *SpanNode) spanNodeJSON {
	s := n.Span
	j := spanNodeJSON{
		TraceID:      s.TraceID,
		SpanID:       s.SpanID,
		ParentSpanID: s.ParentSpanID,
		PID:          s.PID,
		Name:         s.Name,
		StartTimeMs:  s.StartTime.UnixMilli(),
		EndTimeMs:    s.EndTime.UnixMilli(),
		DurationMs:   s.Duration.Milliseconds(),
		SyscallCount: s.SyscallCount,
		TokensUsed:   s.TokensUsed,
		Status:       s.Status,
	}
	for _, child := range n.Children {
		j.Children = append(j.Children, nodeToJSON(child))
	}
	return j
}

// BuildSpanTree constructs a tree from a flat list of spans.
// Root spans have empty ParentSpanID. Children are sorted by StartTime.
func BuildSpanTree(spans []*Span) *SpanTree {
	if len(spans) == 0 {
		return nil
	}

	byID := make(map[string]*SpanNode, len(spans))
	childrenOf := make(map[string][]*SpanNode)

	var meta TraceMetadata
	var traceID string

	for _, s := range spans {
		node := &SpanNode{Span: s}
		byID[string(s.SpanID)] = node

		parentKey := string(s.ParentSpanID)
		if parentKey != "" {
			childrenOf[parentKey] = append(childrenOf[parentKey], node)
		}

		if traceID == "" {
			traceID = string(s.TraceID)
		}

		meta.TotalSpans++
		meta.TotalTokens += s.TokensUsed
		if s.Status == SpanERROR || s.Status == SpanTIMEOUT {
			meta.ErrorCount++
		}
		if meta.StartTime.IsZero() || s.StartTime.Before(meta.StartTime) {
			meta.StartTime = s.StartTime
		}
		if s.EndTime.After(meta.EndTime) {
			meta.EndTime = s.EndTime
		}
	}

	if !meta.EndTime.IsZero() && !meta.StartTime.IsZero() {
		meta.TotalDuration = meta.EndTime.Sub(meta.StartTime)
	}

	for sid, children := range childrenOf {
		sort.Slice(children, func(i, j int) bool {
			return children[i].Span.StartTime.Before(children[j].Span.StartTime)
		})
		if parent, ok := byID[sid]; ok {
			parent.Children = children
		}
	}

	var roots []*SpanNode
	for _, s := range spans {
		if s.ParentSpanID == "" {
			roots = append(roots, byID[string(s.SpanID)])
		}
	}

	if len(roots) == 0 {
		roots = append(roots, byID[string(spans[0].SpanID)])
	}

	var root *SpanNode
	if len(roots) == 1 {
		root = roots[0]
	} else {
		sort.Slice(roots, func(i, j int) bool {
			return roots[i].Span.StartTime.Before(roots[j].Span.StartTime)
		})
		root = roots[0]
		root.Children = append(root.Children, roots[1:]...)
		sort.Slice(root.Children, func(i, j int) bool {
			return root.Children[i].Span.StartTime.Before(root.Children[j].Span.StartTime)
		})
	}

	return &SpanTree{
		Root:     root,
		TraceID:  traceID,
		Metadata: meta,
	}
}

// Walk performs a depth-first traversal of the span tree, calling fn for each node
// with the current depth (0 for root).
func (t *SpanTree) Walk(fn func(node *SpanNode, depth int)) {
	if t == nil || t.Root == nil {
		return
	}
	walkNode(t.Root, 0, fn)
}

func walkNode(node *SpanNode, depth int, fn func(*SpanNode, int)) {
	fn(node, depth)
	for _, child := range node.Children {
		walkNode(child, depth+1, fn)
	}
}

// FormatTraceTree formats a SpanTree as a human-readable tree string.
func FormatTraceTree(tree *SpanTree, verbose bool) string {
	if tree == nil || tree.Root == nil {
		return "No spans found for this trace.\n"
	}

	var b strings.Builder

	b.WriteString(fmt.Sprintf("Trace: %s  |  %d spans  |  %s  |  %d tokens\n\n",
		tree.TraceID,
		tree.Metadata.TotalSpans,
		formatDuration(tree.Metadata.TotalDuration),
		tree.Metadata.TotalTokens,
	))

	tree.Walk(func(node *SpanNode, depth int) {
		s := node.Span
		isLast := isLastChild(node, tree)
		prefix := buildTreePrefix(depth, isLast, node, tree)

		nameCol := fmt.Sprintf("%s (PID %d)", s.Name, s.PID)
		durCol := formatDuration(s.Duration)
		tokCol := fmt.Sprintf("%d tok", s.TokensUsed)
		statusCol := s.Status.String()

		line := fmt.Sprintf("%s%-35s %8s  %7s  %s\n",
			prefix, nameCol, durCol, tokCol, statusCol)
		b.WriteString(line)

		if verbose {
			indent := buildContinuationPrefix(depth, isLast, node, tree)
			b.WriteString(fmt.Sprintf("%s  SpanID: %s", indent, s.SpanID))
			if s.ParentSpanID != "" {
				b.WriteString(fmt.Sprintf("  Parent: %s", s.ParentSpanID))
			}
			b.WriteString(fmt.Sprintf("  Syscalls: %d  Start: %s\n",
				s.SyscallCount, s.StartTime.Format(time.RFC3339)))
		}
	})

	return b.String()
}

func isLastChild(node *SpanNode, tree *SpanTree) bool {
	if node == tree.Root {
		return true
	}
	parent := findParent(tree.Root, node)
	if parent == nil {
		return true
	}
	return parent.Children[len(parent.Children)-1] == node
}

func findParent(current *SpanNode, target *SpanNode) *SpanNode {
	for _, child := range current.Children {
		if child == target {
			return current
		}
		if p := findParent(child, target); p != nil {
			return p
		}
	}
	return nil
}

func buildTreePrefix(depth int, isLast bool, node *SpanNode, tree *SpanTree) string {
	if depth == 0 {
		return "┌─ "
	}

	var parts []string
	current := node
	for d := depth; d > 1; d-- {
		p := findParent(tree.Root, current)
		if p != nil {
			if isLastChild(p, tree) || p == tree.Root {
				// Check if parent has more siblings after it
				pp := findParent(tree.Root, p)
				if pp != nil && pp.Children[len(pp.Children)-1] != p {
					parts = append([]string{"│  "}, parts...)
				} else {
					parts = append([]string{"   "}, parts...)
				}
			} else {
				parts = append([]string{"│  "}, parts...)
			}
			current = p
		}
	}

	if isLast {
		parts = append(parts, "└─ ")
	} else {
		parts = append(parts, "├─ ")
	}

	return strings.Join(parts, "")
}

func buildContinuationPrefix(depth int, isLast bool, node *SpanNode, tree *SpanTree) string {
	if depth == 0 {
		if len(node.Children) > 0 {
			return "│  "
		}
		return "   "
	}

	var parts []string
	current := node
	for d := depth; d > 1; d-- {
		p := findParent(tree.Root, current)
		if p != nil {
			pp := findParent(tree.Root, p)
			if pp != nil && pp.Children[len(pp.Children)-1] != p {
				parts = append([]string{"│  "}, parts...)
			} else {
				parts = append([]string{"   "}, parts...)
			}
			current = p
		}
	}

	if isLast {
		parts = append(parts, "   ")
	} else {
		parts = append(parts, "│  ")
	}

	return strings.Join(parts, "")
}

// FormatTraceList formats a list of TraceSummary as a table string.
func FormatTraceList(summaries []TraceSummary) string {
	if len(summaries) == 0 {
		return "No traces found.\n"
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("%-34s  %5s  %10s  %s\n",
		"TRACE ID", "SPANS", "DURATION", "ROOT"))
	b.WriteString(strings.Repeat("─", 70) + "\n")

	for _, s := range summaries {
		tid := string(s.TraceID)
		if len(tid) > 32 {
			tid = tid[:32]
		}
		b.WriteString(fmt.Sprintf("%-34s  %5d  %10s  %s\n",
			tid, s.SpanCount, formatDuration(s.TotalDuration), s.RootSpanName))
	}

	return b.String()
}
