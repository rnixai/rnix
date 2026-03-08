package debug

import (
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

var baseTime = time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC)

func makeTestViewSpan(traceID, spanID, parentSpanID, name string, pid types.PID, startOffset, dur time.Duration, tokens int, status SpanStatus) *Span {
	return &Span{
		TraceID:      types.TraceID(traceID),
		SpanID:       types.SpanID(spanID),
		ParentSpanID: types.SpanID(parentSpanID),
		PID:          pid,
		Name:         name,
		StartTime:    baseTime.Add(startOffset),
		EndTime:      baseTime.Add(startOffset + dur),
		Duration:     dur,
		SyscallCount: 5,
		TokensUsed:   tokens,
		Status:       status,
	}
}

// --- BuildSpanTree tests ---

func TestBuildSpanTree_SingleRoot(t *testing.T) {
	spans := []*Span{
		makeTestViewSpan("t1", "s1", "", "orchestrator", 1, 0, 5*time.Second, 800, SpanOK),
	}
	tree := BuildSpanTree(spans)
	if tree == nil {
		t.Fatal("expected non-nil tree")
	}
	if tree.Root == nil {
		t.Fatal("expected non-nil root")
	}
	if tree.Root.Span.Name != "orchestrator" {
		t.Errorf("expected root name 'orchestrator', got %q", tree.Root.Span.Name)
	}
	if len(tree.Root.Children) != 0 {
		t.Errorf("expected 0 children, got %d", len(tree.Root.Children))
	}
	if tree.Metadata.TotalSpans != 1 {
		t.Errorf("expected TotalSpans=1, got %d", tree.Metadata.TotalSpans)
	}
}

func TestBuildSpanTree_TwoLevels(t *testing.T) {
	spans := []*Span{
		makeTestViewSpan("t1", "s1", "", "orchestrator", 1, 0, 10*time.Second, 800, SpanOK),
		makeTestViewSpan("t1", "s2", "s1", "analyst", 2, time.Second, 3*time.Second, 500, SpanOK),
		makeTestViewSpan("t1", "s3", "s1", "reviewer", 3, 2*time.Second, 4*time.Second, 200, SpanOK),
	}
	tree := BuildSpanTree(spans)
	if tree == nil {
		t.Fatal("expected non-nil tree")
	}
	if len(tree.Root.Children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(tree.Root.Children))
	}
	if tree.Root.Children[0].Span.Name != "analyst" {
		t.Errorf("expected first child 'analyst', got %q", tree.Root.Children[0].Span.Name)
	}
	if tree.Root.Children[1].Span.Name != "reviewer" {
		t.Errorf("expected second child 'reviewer', got %q", tree.Root.Children[1].Span.Name)
	}
}

func TestBuildSpanTree_ThreeLevels(t *testing.T) {
	spans := []*Span{
		makeTestViewSpan("t1", "s1", "", "root", 1, 0, 20*time.Second, 100, SpanOK),
		makeTestViewSpan("t1", "s2", "s1", "mid", 2, time.Second, 10*time.Second, 200, SpanOK),
		makeTestViewSpan("t1", "s3", "s2", "leaf", 3, 2*time.Second, 3*time.Second, 300, SpanOK),
	}
	tree := BuildSpanTree(spans)
	if tree == nil {
		t.Fatal("expected non-nil tree")
	}
	if len(tree.Root.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(tree.Root.Children))
	}
	mid := tree.Root.Children[0]
	if mid.Span.Name != "mid" {
		t.Errorf("expected mid child 'mid', got %q", mid.Span.Name)
	}
	if len(mid.Children) != 1 {
		t.Fatalf("expected 1 grandchild, got %d", len(mid.Children))
	}
	if mid.Children[0].Span.Name != "leaf" {
		t.Errorf("expected grandchild 'leaf', got %q", mid.Children[0].Span.Name)
	}
}

func TestBuildSpanTree_ChildrenSortedByStartTime(t *testing.T) {
	spans := []*Span{
		makeTestViewSpan("t1", "s1", "", "root", 1, 0, 20*time.Second, 100, SpanOK),
		makeTestViewSpan("t1", "s3", "s1", "late", 3, 5*time.Second, 3*time.Second, 100, SpanOK),
		makeTestViewSpan("t1", "s2", "s1", "early", 2, time.Second, 3*time.Second, 100, SpanOK),
	}
	tree := BuildSpanTree(spans)
	if tree == nil {
		t.Fatal("expected non-nil tree")
	}
	if len(tree.Root.Children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(tree.Root.Children))
	}
	if tree.Root.Children[0].Span.Name != "early" {
		t.Errorf("expected first child 'early' (earlier StartTime), got %q", tree.Root.Children[0].Span.Name)
	}
	if tree.Root.Children[1].Span.Name != "late" {
		t.Errorf("expected second child 'late' (later StartTime), got %q", tree.Root.Children[1].Span.Name)
	}
}

func TestBuildSpanTree_EmptySpans(t *testing.T) {
	tree := BuildSpanTree(nil)
	if tree != nil {
		t.Error("expected nil tree for empty spans")
	}
	tree = BuildSpanTree([]*Span{})
	if tree != nil {
		t.Error("expected nil tree for empty slice")
	}
}

func TestBuildSpanTree_MultipleRoots(t *testing.T) {
	spans := []*Span{
		makeTestViewSpan("t1", "s1", "", "first-root", 1, 0, 5*time.Second, 100, SpanOK),
		makeTestViewSpan("t1", "s2", "", "second-root", 2, time.Second, 3*time.Second, 200, SpanOK),
	}
	tree := BuildSpanTree(spans)
	if tree == nil {
		t.Fatal("expected non-nil tree")
	}
	if tree.Root.Span.Name != "first-root" {
		t.Errorf("expected earliest span as root, got %q", tree.Root.Span.Name)
	}
	if len(tree.Root.Children) != 1 {
		t.Fatalf("expected second root as child, got %d children", len(tree.Root.Children))
	}
	if tree.Root.Children[0].Span.Name != "second-root" {
		t.Errorf("expected child 'second-root', got %q", tree.Root.Children[0].Span.Name)
	}
}

func TestBuildSpanTree_Metadata(t *testing.T) {
	spans := []*Span{
		makeTestViewSpan("t1", "s1", "", "root", 1, 0, 10*time.Second, 500, SpanOK),
		makeTestViewSpan("t1", "s2", "s1", "ok-child", 2, time.Second, 3*time.Second, 300, SpanOK),
		makeTestViewSpan("t1", "s3", "s1", "err-child", 3, 2*time.Second, 4*time.Second, 200, SpanERROR),
	}
	tree := BuildSpanTree(spans)
	if tree == nil {
		t.Fatal("expected non-nil tree")
	}
	m := tree.Metadata
	if m.TotalSpans != 3 {
		t.Errorf("expected TotalSpans=3, got %d", m.TotalSpans)
	}
	if m.TotalTokens != 1000 {
		t.Errorf("expected TotalTokens=1000, got %d", m.TotalTokens)
	}
	if m.ErrorCount != 1 {
		t.Errorf("expected ErrorCount=1, got %d", m.ErrorCount)
	}
	if m.TotalDuration != 10*time.Second {
		t.Errorf("expected TotalDuration=10s, got %v", m.TotalDuration)
	}
}

// --- SpanTree.Walk tests ---

func TestSpanTree_Walk(t *testing.T) {
	spans := []*Span{
		makeTestViewSpan("t1", "s1", "", "root", 1, 0, 10*time.Second, 100, SpanOK),
		makeTestViewSpan("t1", "s2", "s1", "child-a", 2, time.Second, 3*time.Second, 100, SpanOK),
		makeTestViewSpan("t1", "s3", "s1", "child-b", 3, 2*time.Second, 3*time.Second, 100, SpanOK),
		makeTestViewSpan("t1", "s4", "s2", "grandchild", 4, 3*time.Second, 1*time.Second, 100, SpanOK),
	}
	tree := BuildSpanTree(spans)

	var visited []struct {
		name  string
		depth int
	}
	tree.Walk(func(node *SpanNode, depth int) {
		visited = append(visited, struct {
			name  string
			depth int
		}{node.Span.Name, depth})
	})

	expected := []struct {
		name  string
		depth int
	}{
		{"root", 0},
		{"child-a", 1},
		{"grandchild", 2},
		{"child-b", 1},
	}

	if len(visited) != len(expected) {
		t.Fatalf("expected %d nodes visited, got %d", len(expected), len(visited))
	}
	for i, v := range visited {
		if v.name != expected[i].name || v.depth != expected[i].depth {
			t.Errorf("visited[%d]: expected (%q, %d), got (%q, %d)",
				i, expected[i].name, expected[i].depth, v.name, v.depth)
		}
	}
}

// --- FormatTraceTree tests ---

func TestFormatTraceTree_SingleSpan(t *testing.T) {
	spans := []*Span{
		makeTestViewSpan("trace123", "s1", "", "orchestrator", 1, 0, 5*time.Second, 800, SpanOK),
	}
	tree := BuildSpanTree(spans)
	output := FormatTraceTree(tree, false)

	if !strings.Contains(output, "trace123") {
		t.Error("expected output to contain trace ID")
	}
	if !strings.Contains(output, "1 spans") {
		t.Error("expected output to contain '1 spans'")
	}
	if !strings.Contains(output, "800 tokens") {
		t.Error("expected output to contain '800 tokens'")
	}
	if !strings.Contains(output, "orchestrator") {
		t.Error("expected output to contain span name 'orchestrator'")
	}
	if !strings.Contains(output, "ok") {
		t.Error("expected output to contain status 'ok'")
	}
}

func TestFormatTraceTree_ParentChild(t *testing.T) {
	spans := []*Span{
		makeTestViewSpan("t1", "s1", "", "root", 1, 0, 10*time.Second, 500, SpanOK),
		makeTestViewSpan("t1", "s2", "s1", "child-a", 2, time.Second, 3*time.Second, 300, SpanOK),
		makeTestViewSpan("t1", "s3", "s1", "child-b", 3, 4*time.Second, 2*time.Second, 200, SpanOK),
	}
	tree := BuildSpanTree(spans)
	output := FormatTraceTree(tree, false)

	if !strings.Contains(output, "┌─") {
		t.Error("expected root prefix '┌─'")
	}
	if !strings.Contains(output, "├─") {
		t.Error("expected non-last child prefix '├─'")
	}
	if !strings.Contains(output, "└─") {
		t.Error("expected last child prefix '└─'")
	}
}

func TestFormatTraceTree_ThreeLevels(t *testing.T) {
	spans := []*Span{
		makeTestViewSpan("t1", "s1", "", "root", 1, 0, 20*time.Second, 100, SpanOK),
		makeTestViewSpan("t1", "s2", "s1", "mid", 2, time.Second, 10*time.Second, 200, SpanOK),
		makeTestViewSpan("t1", "s3", "s2", "leaf", 3, 2*time.Second, 3*time.Second, 300, SpanOK),
	}
	tree := BuildSpanTree(spans)
	output := FormatTraceTree(tree, false)

	if !strings.Contains(output, "root") {
		t.Error("expected 'root' in output")
	}
	if !strings.Contains(output, "mid") {
		t.Error("expected 'mid' in output")
	}
	if !strings.Contains(output, "leaf") {
		t.Error("expected 'leaf' in output")
	}

	lines := strings.Split(output, "\n")
	var midLine, leafLine string
	for _, l := range lines {
		if strings.Contains(l, "mid") && strings.Contains(l, "PID") {
			midLine = l
		}
		if strings.Contains(l, "leaf") && strings.Contains(l, "PID") {
			leafLine = l
		}
	}
	if midLine == "" || leafLine == "" {
		t.Fatalf("could not find mid or leaf lines in output:\n%s", output)
	}

	midIndent := countLeadingSpaces(midLine)
	leafIndent := countLeadingSpaces(leafLine)
	if leafIndent <= midIndent {
		t.Errorf("expected leaf indent (%d) > mid indent (%d)", leafIndent, midIndent)
	}
}

func TestFormatTraceTree_ErrorSpan(t *testing.T) {
	spans := []*Span{
		makeTestViewSpan("t1", "s1", "", "root", 1, 0, 5*time.Second, 500, SpanOK),
		makeTestViewSpan("t1", "s2", "s1", "fail-child", 2, time.Second, 2*time.Second, 100, SpanERROR),
	}
	tree := BuildSpanTree(spans)
	output := FormatTraceTree(tree, false)

	if !strings.Contains(output, "error") {
		t.Error("expected 'error' status in output")
	}
}

func TestFormatTraceTree_TimeoutSpan(t *testing.T) {
	spans := []*Span{
		makeTestViewSpan("t1", "s1", "", "root", 1, 0, 5*time.Second, 500, SpanOK),
		makeTestViewSpan("t1", "s2", "s1", "slow-child", 2, time.Second, 30*time.Second, 100, SpanTIMEOUT),
	}
	tree := BuildSpanTree(spans)
	output := FormatTraceTree(tree, false)

	if !strings.Contains(output, "timeout") {
		t.Error("expected 'timeout' status in output")
	}
}

func TestFormatTraceTree_Verbose(t *testing.T) {
	spans := []*Span{
		makeTestViewSpan("t1", "span-001", "", "root", 1, 0, 5*time.Second, 500, SpanOK),
	}
	tree := BuildSpanTree(spans)

	normal := FormatTraceTree(tree, false)
	verbose := FormatTraceTree(tree, true)

	if strings.Contains(normal, "SpanID:") {
		t.Error("normal mode should NOT contain SpanID")
	}
	if !strings.Contains(verbose, "SpanID:") {
		t.Error("verbose mode should contain SpanID")
	}
	if !strings.Contains(verbose, "Syscalls:") {
		t.Error("verbose mode should contain Syscalls count")
	}
}

// --- FormatTraceList tests ---

func TestFormatTraceList_Empty(t *testing.T) {
	output := FormatTraceList(nil)
	if !strings.Contains(output, "No traces found") {
		t.Errorf("expected 'No traces found', got %q", output)
	}
}

func TestFormatTraceList_Multiple(t *testing.T) {
	summaries := []TraceSummary{
		{
			TraceID:       "abcdef1234567890abcdef1234567890",
			SpanCount:     3,
			StartTime:     baseTime,
			TotalDuration: 12500 * time.Millisecond,
			RootSpanName:  "orchestrator",
		},
		{
			TraceID:       "1234567890abcdef1234567890abcdef",
			SpanCount:     5,
			StartTime:     baseTime.Add(-time.Hour),
			TotalDuration: 25300 * time.Millisecond,
			RootSpanName:  "pipeline",
		},
	}
	output := FormatTraceList(summaries)

	if !strings.Contains(output, "TRACE ID") {
		t.Error("expected table header 'TRACE ID'")
	}
	if !strings.Contains(output, "SPANS") {
		t.Error("expected table header 'SPANS'")
	}
	if !strings.Contains(output, "orchestrator") {
		t.Error("expected 'orchestrator' in output")
	}
	if !strings.Contains(output, "pipeline") {
		t.Error("expected 'pipeline' in output")
	}
}

func countLeadingSpaces(s string) int {
	count := 0
	for _, r := range s {
		if r == ' ' || r == '│' || r == '├' || r == '└' || r == '─' || r == '┌' {
			count++
		} else {
			break
		}
	}
	return count
}
