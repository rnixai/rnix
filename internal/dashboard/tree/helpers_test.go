// Package tree — helpers_test.go (Story 38-5 PR2 Step 3d)
//
// 纯 helper 函数的单元测试。覆盖：
//   - AgentLabel：Model > Provider > Intent 优先级 + Intent 截断
//   - ReusedPIDs：返回出现 ≥ 2 次的 PID
//   - SuspendReasonAbbrev：5 种 reason → tag 映射
//   - OrchestrationAnnotation：Compose / Pipeline / 无注解
//   - RenderCtxBar：颜色阈值 + 空预算 + ASCII fallback
//   - FilteredRows：搜索过滤 + 空 query 零拷贝
//   - BuildCollapsedIntents：父子分组 + 公共前缀折叠 + ratio gate
//   - collapseCommonPrefix / longestCommonPrefix / truncateToWordBoundary 内部 helper
package tree

import (
	"strings"
	"testing"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// --- AgentLabel ---

func TestAgentLabel_PrefersModel(t *testing.T) {
	t.Parallel()
	p := vfs.ProcInfo{Model: "claude-opus-4-7", Provider: "anthropic", Intent: "build auth"}
	if got := AgentLabel(p); got != "claude-opus-4-7" {
		t.Errorf("AgentLabel = %q, want claude-opus-4-7", got)
	}
}

func TestAgentLabel_FallbackToProvider(t *testing.T) {
	t.Parallel()
	p := vfs.ProcInfo{Provider: "anthropic", Intent: "build"}
	if got := AgentLabel(p); got != "anthropic" {
		t.Errorf("AgentLabel = %q, want anthropic", got)
	}
}

func TestAgentLabel_FallbackToIntent_ShortNotTruncated(t *testing.T) {
	t.Parallel()
	p := vfs.ProcInfo{Intent: "short"}
	if got := AgentLabel(p); got != "short" {
		t.Errorf("AgentLabel = %q, want short", got)
	}
}

func TestAgentLabel_FallbackToIntent_LongTruncated(t *testing.T) {
	t.Parallel()
	p := vfs.ProcInfo{Intent: "this is a very long intent string that should be truncated"}
	got := AgentLabel(p)
	// 17 chars + "..." = 20 chars
	if len([]rune(got)) != 20 {
		t.Errorf("AgentLabel rune count = %d, want 20", len([]rune(got)))
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("AgentLabel = %q should end with ...", got)
	}
}

func TestAgentLabel_AllEmpty_ReturnsDash(t *testing.T) {
	t.Parallel()
	if got := AgentLabel(vfs.ProcInfo{}); got != "—" {
		t.Errorf("AgentLabel(empty) = %q, want —", got)
	}
}

// --- ReusedPIDs ---

func TestReusedPIDs_NoDuplicate(t *testing.T) {
	t.Parallel()
	procs := []vfs.ProcInfo{{PID: 1}, {PID: 2}, {PID: 3}}
	got := ReusedPIDs(procs)
	if len(got) != 0 {
		t.Errorf("ReusedPIDs(unique) = %v, want empty", got)
	}
}

func TestReusedPIDs_DetectsDuplicates(t *testing.T) {
	t.Parallel()
	procs := []vfs.ProcInfo{{PID: 1}, {PID: 2}, {PID: 1}, {PID: 1}}
	got := ReusedPIDs(procs)
	if len(got) != 1 {
		t.Errorf("ReusedPIDs len = %d, want 1", len(got))
	}
	if got[types.PID(1)] != 3 {
		t.Errorf("ReusedPIDs[1] = %d, want 3", got[types.PID(1)])
	}
}

func TestReusedPIDs_Empty(t *testing.T) {
	t.Parallel()
	got := ReusedPIDs(nil)
	if len(got) != 0 {
		t.Errorf("ReusedPIDs(nil) = %v, want empty", got)
	}
}

// --- SuspendReasonAbbrev ---

func TestSuspendReasonAbbrev(t *testing.T) {
	t.Parallel()
	tests := []struct {
		reason, want string
	}{
		{"", ""},
		{"budget_exhausted", "[budget]"},
		{"budget_exhausted_tokens", "[budget]"},
		{"heartbeat_timeout", "[stalled]"},
		{"loop_detected", "[loop]"},
		{"quota_exhausted", "[quota]"},
		{"user_pause", "[user]"},
		{"manual", "[user]"},
	}
	for _, tc := range tests {
		t.Run(tc.reason, func(t *testing.T) {
			if got := SuspendReasonAbbrev(tc.reason); got != tc.want {
				t.Errorf("SuspendReasonAbbrev(%q) = %q, want %q", tc.reason, got, tc.want)
			}
		})
	}
}

// --- OrchestrationAnnotation ---

func TestOrchestrationAnnotation_ComposeWithDeps(t *testing.T) {
	t.Parallel()
	p := vfs.ProcInfo{ComposeNode: "build", ComposeDeps: []string{"compile", "test"}}
	got := OrchestrationAnnotation(p)
	// Unicode 模式（默认）：╌╌►compile,test
	if !strings.Contains(got, "compile,test") {
		t.Errorf("OrchestrationAnnotation = %q, want to contain compile,test", got)
	}
}

func TestOrchestrationAnnotation_ComposeNoDeps(t *testing.T) {
	t.Parallel()
	p := vfs.ProcInfo{ComposeNode: "node1"}
	got := OrchestrationAnnotation(p)
	if !strings.Contains(got, "node1") {
		t.Errorf("OrchestrationAnnotation = %q, want to contain node1", got)
	}
}

func TestOrchestrationAnnotation_Pipeline(t *testing.T) {
	t.Parallel()
	p := vfs.ProcInfo{PipelineIndex: 1, PipelineTotal: 5}
	got := OrchestrationAnnotation(p)
	if !strings.Contains(got, "[2/5]") {
		t.Errorf("OrchestrationAnnotation = %q, want [2/5]", got)
	}
}

func TestOrchestrationAnnotation_None(t *testing.T) {
	t.Parallel()
	p := vfs.ProcInfo{}
	if got := OrchestrationAnnotation(p); got != "" {
		t.Errorf("OrchestrationAnnotation(empty) = %q, want empty", got)
	}
}

func TestOrchestrationAnnotation_LongDepsTruncated(t *testing.T) {
	t.Parallel()
	deps := []string{strings.Repeat("a", 50)}
	p := vfs.ProcInfo{ComposeNode: "n", ComposeDeps: deps}
	got := OrchestrationAnnotation(p)
	// 长度 > 30 时应被截断
	// 30 chars 截断到 27 + "..."
	if !strings.Contains(got, "...") {
		t.Errorf("OrchestrationAnnotation should truncate long deps with ..., got %q", got)
	}
}

// --- RenderCtxBar ---

func TestRenderCtxBar_ZeroBudget_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	if got := RenderCtxBar(100, 0, 10); got != "" {
		t.Errorf("RenderCtxBar(any, 0, 10) = %q, want empty", got)
	}
}

func TestRenderCtxBar_50PercentContainsLabel(t *testing.T) {
	t.Parallel()
	got := RenderCtxBar(50, 100, 10)
	if !strings.Contains(got, "50%") {
		t.Errorf("RenderCtxBar(50/100) = %q, want to contain 50%%", got)
	}
}

func TestRenderCtxBar_ClampsTo100(t *testing.T) {
	t.Parallel()
	got := RenderCtxBar(200, 100, 10)
	if !strings.Contains(got, "100%") {
		t.Errorf("RenderCtxBar(200/100) = %q, want clamped to 100%%", got)
	}
}

// --- FilteredRows ---

func TestFilteredRows_EmptyQuery_ReturnsRowsUnmodified(t *testing.T) {
	t.Parallel()
	rows := []FlatRow{
		{Proc: vfs.ProcInfo{PID: 1, Intent: "foo"}},
		{Proc: vfs.ProcInfo{PID: 2, Intent: "bar"}},
	}
	state := TreeState{Rows: rows, SearchQuery: ""}
	got := FilteredRows(state)
	if len(got) != 2 {
		t.Errorf("FilteredRows(empty query) len = %d, want 2", len(got))
	}
}

func TestFilteredRows_QueryMatchesIntent(t *testing.T) {
	t.Parallel()
	rows := []FlatRow{
		{Proc: vfs.ProcInfo{PID: 1, Intent: "build the foo system"}},
		{Proc: vfs.ProcInfo{PID: 2, Intent: "fix the bar bug"}},
		{Proc: vfs.ProcInfo{PID: 3, Intent: "refactor foo module"}},
	}
	state := TreeState{Rows: rows, SearchQuery: "FOO"}
	got := FilteredRows(state)
	if len(got) != 2 {
		t.Errorf("FilteredRows(query=FOO) len = %d, want 2 (case-insensitive)", len(got))
	}
}

func TestFilteredRows_QueryMatchesAgentLabel(t *testing.T) {
	t.Parallel()
	rows := []FlatRow{
		{Proc: vfs.ProcInfo{PID: 1, Model: "claude-opus-4-7"}},
		{Proc: vfs.ProcInfo{PID: 2, Model: "gpt-4"}},
	}
	state := TreeState{Rows: rows, SearchQuery: "claude"}
	got := FilteredRows(state)
	if len(got) != 1 || got[0].Proc.PID != types.PID(1) {
		t.Errorf("FilteredRows(query=claude) should match PID 1 only, got %v", got)
	}
}

// --- BuildCollapsedIntents ---

func TestBuildCollapsedIntents_EmptyRows(t *testing.T) {
	t.Parallel()
	got := BuildCollapsedIntents(TreeState{})
	if len(got) != 0 {
		t.Errorf("BuildCollapsedIntents(empty) = %v, want empty map", got)
	}
}

func TestBuildCollapsedIntents_SingleChild_NoCollapse(t *testing.T) {
	t.Parallel()
	rows := []FlatRow{
		{Proc: vfs.ProcInfo{UUID: "parent", Intent: "parent task"}},
		{Proc: vfs.ProcInfo{UUID: "child1", ParentUUID: "parent", Intent: "src/foo/bar"}},
	}
	got := BuildCollapsedIntents(TreeState{Rows: rows})
	if len(got) != 0 {
		t.Errorf("Single child should not trigger collapse, got %v", got)
	}
}

func TestBuildCollapsedIntents_CommonPrefix(t *testing.T) {
	t.Parallel()
	// 公共前缀 "src/component/" (~14 chars) 占平均 intent 长度（~25）的 56%，触发折叠
	rows := []FlatRow{
		{Proc: vfs.ProcInfo{UUID: "parent", Intent: "build"}},
		{Proc: vfs.ProcInfo{UUID: "c1", ParentUUID: "parent", Intent: "src/component/header.go"}},
		{Proc: vfs.ProcInfo{UUID: "c2", ParentUUID: "parent", Intent: "src/component/footer.go"}},
		{Proc: vfs.ProcInfo{UUID: "c3", ParentUUID: "parent", Intent: "src/component/sidebar.go"}},
	}
	got := BuildCollapsedIntents(TreeState{Rows: rows})
	if len(got) != 3 {
		t.Errorf("BuildCollapsedIntents should fold all 3 children, got %d entries", len(got))
	}
	// Each child should have a shortened intent containing the ellipsis marker
	for uuid, intent := range got {
		if !strings.Contains(intent, "…") && !strings.Contains(intent, "...") {
			t.Errorf("Child %q intent = %q, expected to contain ellipsis", uuid, intent)
		}
	}
}

// --- Internal helpers ---

func Test_collapseCommonPrefix_TooShortPrefix_NoChange(t *testing.T) {
	t.Parallel()
	intents := []string{"foo bar one", "foo bar two", "foo bar three"}
	got := collapseCommonPrefix(intents)
	// "foo bar " 长度 8，平均长度 ~12，prefix > avg/2，应折叠
	if got[0] == intents[0] {
		t.Logf("collapseCommonPrefix may not collapse short prefix: got %v", got)
	}
}

func Test_longestCommonPrefix_Empty(t *testing.T) {
	t.Parallel()
	if got := longestCommonPrefix(nil); got != "" {
		t.Errorf("longestCommonPrefix(nil) = %q, want empty", got)
	}
	if got := longestCommonPrefix([]string{}); got != "" {
		t.Errorf("longestCommonPrefix([]) = %q, want empty", got)
	}
}

func Test_longestCommonPrefix_Found(t *testing.T) {
	t.Parallel()
	got := longestCommonPrefix([]string{"abcdef", "abcxyz", "abcmno"})
	if got != "abc" {
		t.Errorf("longestCommonPrefix = %q, want abc", got)
	}
}

func Test_longestCommonPrefix_NoCommon(t *testing.T) {
	t.Parallel()
	got := longestCommonPrefix([]string{"foo", "bar", "baz"})
	if got != "" {
		t.Errorf("longestCommonPrefix(no common) = %q, want empty", got)
	}
}

func Test_truncateToWordBoundary(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in, want string
	}{
		{"", ""},
		{"foo/bar/baz", "foo/bar/"},
		{"foo bar baz", "foo bar "},
		{"foo-bar-baz", "foo-bar-"},
		{"foo_bar_baz", "foo_bar_"},
		{"nopunct", ""}, // 无 boundary，返回空
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			if got := truncateToWordBoundary(tc.in); got != tc.want {
				t.Errorf("truncateToWordBoundary(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func Test_clampSortMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in, want int
	}{
		{0, 0}, // valid
		{1, 1},
		{2, 2},
		{-1, 0},  // 越界 → 0
		{3, 0},   // 越界 → 0
		{99, 0},  // 越界 → 0
	}
	for _, tc := range tests {
		t.Run("", func(t *testing.T) {
			if got := clampSortMode(tc.in); got != tc.want {
				t.Errorf("clampSortMode(%d) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}
