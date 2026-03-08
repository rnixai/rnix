package debug

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

func makeBlameSpan(traceID, spanID, parentSpanID, name string, pid types.PID, startOffset, dur time.Duration, tokens int, status SpanStatus) *Span {
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

func buildBlameTree(spans ...*Span) *SpanTree {
	return BuildSpanTree(spans)
}

// --- AnalyzeTrace tests ---

func TestAnalyzeTrace_LinearChain(t *testing.T) {
	tree := buildBlameTree(
		makeBlameSpan("t1", "s1", "", "A", 1, 0, 5*time.Second, 800, SpanOK),
		makeBlameSpan("t1", "s2", "s1", "B", 2, time.Second, 3*time.Second, 500, SpanOK),
		makeBlameSpan("t1", "s3", "s2", "C", 3, 2*time.Second, 1*time.Second, 200, SpanOK),
	)
	result := AnalyzeTrace(tree)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// Critical path should be A→B→C
	if len(result.CriticalPath) != 3 {
		t.Fatalf("expected 3 nodes in critical path, got %d", len(result.CriticalPath))
	}
	if result.CriticalPath[0].Span.Name != "A" {
		t.Errorf("expected first in path = 'A', got %q", result.CriticalPath[0].Span.Name)
	}
	if result.CriticalPath[2].Span.Name != "C" {
		t.Errorf("expected last in path = 'C', got %q", result.CriticalPath[2].Span.Name)
	}

	// Duration hotspots: A(5s) > B(3s) > C(1s)
	if len(result.DurationHotspots) != 3 {
		t.Fatalf("expected 3 duration hotspots, got %d", len(result.DurationHotspots))
	}
	if result.DurationHotspots[0].Span.Name != "A" {
		t.Errorf("expected #1 duration hotspot = 'A', got %q", result.DurationHotspots[0].Span.Name)
	}
	if result.DurationHotspots[0].Rank != 1 {
		t.Errorf("expected rank 1, got %d", result.DurationHotspots[0].Rank)
	}

	// Token hotspots: A(800) > B(500) > C(200)
	if result.TokenHotspots[0].Span.Name != "A" {
		t.Errorf("expected #1 token hotspot = 'A', got %q", result.TokenHotspots[0].Span.Name)
	}
}

func TestAnalyzeTrace_BranchTree(t *testing.T) {
	tree := buildBlameTree(
		makeBlameSpan("t1", "s1", "", "root", 1, 0, 10*time.Second, 100, SpanOK),
		makeBlameSpan("t1", "s2", "s1", "fast-child", 2, time.Second, 2*time.Second, 300, SpanOK),
		makeBlameSpan("t1", "s3", "s1", "slow-child", 3, 2*time.Second, 7*time.Second, 500, SpanOK),
	)
	result := AnalyzeTrace(tree)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// Critical path should choose slow-child: root(10s) + slow-child(7s) = 17s
	if len(result.CriticalPath) != 2 {
		t.Fatalf("expected 2 nodes in critical path, got %d", len(result.CriticalPath))
	}
	if result.CriticalPath[0].Span.Name != "root" {
		t.Errorf("expected path[0] = 'root', got %q", result.CriticalPath[0].Span.Name)
	}
	if result.CriticalPath[1].Span.Name != "slow-child" {
		t.Errorf("expected path[1] = 'slow-child', got %q", result.CriticalPath[1].Span.Name)
	}
}

func TestAnalyzeTrace_AllOK(t *testing.T) {
	tree := buildBlameTree(
		makeBlameSpan("t1", "s1", "", "root", 1, 0, 5*time.Second, 500, SpanOK),
		makeBlameSpan("t1", "s2", "s1", "child", 2, time.Second, 2*time.Second, 300, SpanOK),
	)
	result := AnalyzeTrace(tree)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if len(result.ErrorChains) != 0 {
		t.Errorf("expected 0 error chains for all-OK trace, got %d", len(result.ErrorChains))
	}
	if result.Summary.ErrorSpans != 0 {
		t.Errorf("expected 0 error spans, got %d", result.Summary.ErrorSpans)
	}

	// Should still have duration and token hotspots
	if len(result.DurationHotspots) == 0 {
		t.Error("expected duration hotspots even for all-OK trace")
	}
	if len(result.TokenHotspots) == 0 {
		t.Error("expected token hotspots even for all-OK trace")
	}
}

func TestAnalyzeTrace_AllError(t *testing.T) {
	tree := buildBlameTree(
		makeBlameSpan("t1", "s1", "", "root", 1, 0, 10*time.Second, 500, SpanERROR),
		makeBlameSpan("t1", "s2", "s1", "child-a", 2, time.Second, 3*time.Second, 300, SpanERROR),
		makeBlameSpan("t1", "s3", "s1", "child-b", 3, 2*time.Second, 4*time.Second, 200, SpanTIMEOUT),
	)
	result := AnalyzeTrace(tree)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if result.Summary.ErrorSpans != 3 {
		t.Errorf("expected 3 error spans, got %d", result.Summary.ErrorSpans)
	}
	if len(result.ErrorChains) == 0 {
		t.Error("expected at least one error chain")
	}
}

func TestAnalyzeTrace_MixedErrors(t *testing.T) {
	tree := buildBlameTree(
		makeBlameSpan("t1", "s1", "", "root", 1, 0, 10*time.Second, 500, SpanOK),
		makeBlameSpan("t1", "s2", "s1", "ok-child", 2, time.Second, 3*time.Second, 300, SpanOK),
		makeBlameSpan("t1", "s3", "s1", "err-child", 3, 2*time.Second, 4*time.Second, 200, SpanERROR),
	)
	result := AnalyzeTrace(tree)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if result.Summary.ErrorSpans != 1 {
		t.Errorf("expected 1 error span, got %d", result.Summary.ErrorSpans)
	}
	if len(result.ErrorChains) != 1 {
		t.Fatalf("expected 1 error chain, got %d", len(result.ErrorChains))
	}

	chain := result.ErrorChains[0]
	if chain.RootCause.Name != "err-child" {
		t.Errorf("expected root cause = 'err-child', got %q", chain.RootCause.Name)
	}
}

func TestAnalyzeTrace_TokenIndependentOfDuration(t *testing.T) {
	// fast-but-expensive has short duration but high tokens
	tree := buildBlameTree(
		makeBlameSpan("t1", "s1", "", "root", 1, 0, 10*time.Second, 100, SpanOK),
		makeBlameSpan("t1", "s2", "s1", "slow-cheap", 2, time.Second, 8*time.Second, 50, SpanOK),
		makeBlameSpan("t1", "s3", "s1", "fast-expensive", 3, 2*time.Second, 1*time.Second, 900, SpanOK),
	)
	result := AnalyzeTrace(tree)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// Duration: root(10s) > slow-cheap(8s) > fast-expensive(1s)
	if result.DurationHotspots[0].Span.Name != "root" {
		t.Errorf("expected #1 duration = 'root', got %q", result.DurationHotspots[0].Span.Name)
	}

	// Token: fast-expensive(900) > root(100) > slow-cheap(50)
	if result.TokenHotspots[0].Span.Name != "fast-expensive" {
		t.Errorf("expected #1 token = 'fast-expensive', got %q", result.TokenHotspots[0].Span.Name)
	}
}

func TestAnalyzeTrace_Summary(t *testing.T) {
	tree := buildBlameTree(
		makeBlameSpan("t1", "s1", "", "root", 1, 0, 10*time.Second, 500, SpanOK),
		makeBlameSpan("t1", "s2", "s1", "child", 2, time.Second, 3*time.Second, 300, SpanERROR),
	)
	result := AnalyzeTrace(tree)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	s := result.Summary
	if s.TotalSpans != 2 {
		t.Errorf("expected TotalSpans=2, got %d", s.TotalSpans)
	}
	if s.ErrorSpans != 1 {
		t.Errorf("expected ErrorSpans=1, got %d", s.ErrorSpans)
	}
	if s.TotalTokens != 800 {
		t.Errorf("expected TotalTokens=800, got %d", s.TotalTokens)
	}
	if s.CriticalPathPct <= 0 {
		t.Error("expected positive CriticalPathPct")
	}
}

// --- findCriticalPath tests ---

func TestFindCriticalPath_DeepTree(t *testing.T) {
	tree := buildBlameTree(
		makeBlameSpan("t1", "s1", "", "L0", 1, 0, 2*time.Second, 100, SpanOK),
		makeBlameSpan("t1", "s2", "s1", "L1", 2, time.Second, 3*time.Second, 100, SpanOK),
		makeBlameSpan("t1", "s3", "s2", "L2", 3, 2*time.Second, 4*time.Second, 100, SpanOK),
		makeBlameSpan("t1", "s4", "s3", "L3", 4, 3*time.Second, 5*time.Second, 100, SpanOK),
	)

	path, dur := findCriticalPath(tree)
	if len(path) != 4 {
		t.Fatalf("expected 4 nodes in critical path, got %d", len(path))
	}
	if path[0].Span.Name != "L0" || path[3].Span.Name != "L3" {
		t.Errorf("expected path L0→L1→L2→L3, got %s→...→%s", path[0].Span.Name, path[3].Span.Name)
	}
	expectedDur := 14 * time.Second // 2+3+4+5
	if dur != expectedDur {
		t.Errorf("expected critical duration %v, got %v", expectedDur, dur)
	}
}

func TestFindCriticalPath_SingleNode(t *testing.T) {
	tree := buildBlameTree(
		makeBlameSpan("t1", "s1", "", "root", 1, 0, 5*time.Second, 500, SpanOK),
	)

	path, dur := findCriticalPath(tree)
	if len(path) != 1 {
		t.Fatalf("expected 1 node in critical path, got %d", len(path))
	}
	if path[0].Span.Name != "root" {
		t.Errorf("expected 'root', got %q", path[0].Span.Name)
	}
	if dur != 5*time.Second {
		t.Errorf("expected 5s, got %v", dur)
	}
}

// --- findErrorChains tests ---

func TestFindErrorChains_SingleErrorLeaf(t *testing.T) {
	tree := buildBlameTree(
		makeBlameSpan("t1", "s1", "", "root", 1, 0, 10*time.Second, 500, SpanOK),
		makeBlameSpan("t1", "s2", "s1", "ok-child", 2, time.Second, 3*time.Second, 300, SpanOK),
		makeBlameSpan("t1", "s3", "s1", "err-child", 3, 2*time.Second, 4*time.Second, 200, SpanERROR),
	)

	chains := findErrorChains(tree)
	if len(chains) != 1 {
		t.Fatalf("expected 1 error chain, got %d", len(chains))
	}

	chain := chains[0]
	if chain.RootCause.Name != "err-child" {
		t.Errorf("expected root cause = 'err-child', got %q", chain.RootCause.Name)
	}

	// Path: root → err-child (from root to the error node)
	if len(chain.Path) != 2 {
		t.Fatalf("expected 2 nodes in path, got %d", len(chain.Path))
	}
	if chain.Path[0].Name != "root" {
		t.Errorf("expected path[0] = 'root', got %q", chain.Path[0].Name)
	}
	if chain.Path[1].Name != "err-child" {
		t.Errorf("expected path[1] = 'err-child', got %q", chain.Path[1].Name)
	}
}

func TestFindErrorChains_MiddleAndLeafError(t *testing.T) {
	// root(ok) → mid(error) → leaf(error)
	// RootCause should be mid (first error in path from root)
	tree := buildBlameTree(
		makeBlameSpan("t1", "s1", "", "root", 1, 0, 10*time.Second, 500, SpanOK),
		makeBlameSpan("t1", "s2", "s1", "mid", 2, time.Second, 5*time.Second, 300, SpanERROR),
		makeBlameSpan("t1", "s3", "s2", "leaf", 3, 2*time.Second, 2*time.Second, 200, SpanERROR),
	)

	chains := findErrorChains(tree)
	if len(chains) != 1 {
		t.Fatalf("expected 1 error chain, got %d", len(chains))
	}

	chain := chains[0]
	// RootCause should be "mid" since it's the first error along the path from root
	if chain.RootCause.Name != "mid" {
		t.Errorf("expected root cause = 'mid', got %q", chain.RootCause.Name)
	}
}

func TestFindErrorChains_MultipleIndependentErrors(t *testing.T) {
	tree := buildBlameTree(
		makeBlameSpan("t1", "s1", "", "root", 1, 0, 10*time.Second, 500, SpanOK),
		makeBlameSpan("t1", "s2", "s1", "err-a", 2, time.Second, 3*time.Second, 300, SpanERROR),
		makeBlameSpan("t1", "s3", "s1", "err-b", 3, 2*time.Second, 4*time.Second, 200, SpanTIMEOUT),
	)

	chains := findErrorChains(tree)
	if len(chains) < 2 {
		t.Fatalf("expected at least 2 error chains, got %d", len(chains))
	}
}

func TestFindErrorChains_TimeoutStatus(t *testing.T) {
	tree := buildBlameTree(
		makeBlameSpan("t1", "s1", "", "root", 1, 0, 30*time.Second, 500, SpanOK),
		makeBlameSpan("t1", "s2", "s1", "timeout-child", 2, time.Second, 30*time.Second, 100, SpanTIMEOUT),
	)

	chains := findErrorChains(tree)
	if len(chains) != 1 {
		t.Fatalf("expected 1 error chain for timeout, got %d", len(chains))
	}
	if chains[0].RootCause.Status != SpanTIMEOUT {
		t.Errorf("expected TIMEOUT root cause, got %v", chains[0].RootCause.Status)
	}
}

// --- FormatBlameResult tests ---

func TestFormatBlameResult_WithErrors(t *testing.T) {
	tree := buildBlameTree(
		makeBlameSpan("trace-abc", "s1", "", "orchestrator", 1, 0, 10*time.Second, 800, SpanOK),
		makeBlameSpan("trace-abc", "s2", "s1", "analyst", 2, time.Second, 3*time.Second, 500, SpanOK),
		makeBlameSpan("trace-abc", "s3", "s1", "reviewer", 3, 2*time.Second, 4*time.Second, 200, SpanERROR),
	)
	result := AnalyzeTrace(tree)
	output := FormatBlameResult(result)

	if !strings.Contains(output, "Critical Path") {
		t.Error("expected 'Critical Path' section")
	}
	if !strings.Contains(output, "Duration Hotspots") {
		t.Error("expected 'Duration Hotspots' section")
	}
	if !strings.Contains(output, "Token Hotspots") {
		t.Error("expected 'Token Hotspots' section")
	}
	if !strings.Contains(output, "Error Chains") {
		t.Error("expected 'Error Chains' section")
	}
	if !strings.Contains(output, "[ROOT CAUSE]") {
		t.Error("expected '[ROOT CAUSE]' marker")
	}
	if !strings.Contains(output, "trace-abc") {
		t.Error("expected trace ID in output")
	}
}

func TestFormatBlameResult_NoErrors(t *testing.T) {
	tree := buildBlameTree(
		makeBlameSpan("t1", "s1", "", "root", 1, 0, 5*time.Second, 500, SpanOK),
		makeBlameSpan("t1", "s2", "s1", "child", 2, time.Second, 2*time.Second, 300, SpanOK),
	)
	result := AnalyzeTrace(tree)
	output := FormatBlameResult(result)

	if strings.Contains(output, "Error Chains") {
		t.Error("expected NO 'Error Chains' section for all-OK trace")
	}
	if !strings.Contains(output, "Critical Path") {
		t.Error("expected 'Critical Path' section even for all-OK trace")
	}
	if !strings.Contains(output, "Duration Hotspots") {
		t.Error("expected 'Duration Hotspots' section even for all-OK trace")
	}
}

func TestFormatBlameResult_Ranking(t *testing.T) {
	tree := buildBlameTree(
		makeBlameSpan("t1", "s1", "", "A", 1, 0, 10*time.Second, 100, SpanOK),
		makeBlameSpan("t1", "s2", "s1", "B", 2, time.Second, 5*time.Second, 200, SpanOK),
		makeBlameSpan("t1", "s3", "s1", "C", 3, 2*time.Second, 1*time.Second, 50, SpanOK),
	)
	result := AnalyzeTrace(tree)
	output := FormatBlameResult(result)

	if !strings.Contains(output, "#1") {
		t.Error("expected '#1' ranking")
	}
	if !strings.Contains(output, "#2") {
		t.Error("expected '#2' ranking")
	}
	if !strings.Contains(output, "#3") {
		t.Error("expected '#3' ranking")
	}
	if !strings.Contains(output, "%") {
		t.Error("expected percentage in output")
	}
}

// --- JSON serialization test ---

func TestBlameResult_MarshalJSON(t *testing.T) {
	tree := buildBlameTree(
		makeBlameSpan("t1", "s1", "", "root", 1, 0, 10*time.Second, 500, SpanOK),
		makeBlameSpan("t1", "s2", "s1", "child", 2, time.Second, 3*time.Second, 300, SpanERROR),
	)
	result := AnalyzeTrace(tree)

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("MarshalJSON failed: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if _, ok := parsed["trace_id"]; !ok {
		t.Error("expected 'trace_id' field in JSON")
	}
	if _, ok := parsed["critical_path"]; !ok {
		t.Error("expected 'critical_path' field in JSON")
	}
	if _, ok := parsed["duration_hotspots"]; !ok {
		t.Error("expected 'duration_hotspots' field in JSON")
	}
	if _, ok := parsed["error_chains"]; !ok {
		t.Error("expected 'error_chains' field in JSON")
	}
	if _, ok := parsed["summary"]; !ok {
		t.Error("expected 'summary' field in JSON")
	}
	if _, ok := parsed["critical_duration_ms"]; !ok {
		t.Error("expected 'critical_duration_ms' field in JSON")
	}
}
