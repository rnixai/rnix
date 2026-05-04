// Package event — event_test.go (Story 38-5 PR11 Step 4(a))
//
// 验证 UnifiedEvent 类型族迁出后的行为契约：
//   - Severity 常量值与 cmd/rnix 旧定义完全等价（数值 0/1/2/3）；
//   - Event 类型常量字符串与 cmd/rnix 旧定义完全等价；
//   - UnifiedEventSlice 排序行为（newest first · stable）;
//   - UnifiedEvent struct 字段零值语义;
//   - StepEntry/RawEvent 指针字段的 nil-safety。
//
// 注：不重复测试 cmd/rnix 端 alias（cmd/rnix tests 已覆盖）。
package event

import (
	"sort"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/dashboard/timeline"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
)

// --- Severity 常量契约 ---

func TestSeverityConstants_NumericValues(t *testing.T) {
	cases := []struct {
		name string
		got  int
		want int
	}{
		{"SevInfo", SevInfo, 0},
		{"SevWarn", SevWarn, 1},
		{"SevError", SevError, 2},
		{"SevCritical", SevCritical, 3},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d (cmd/rnix 旧值兼容性)", c.name, c.got, c.want)
		}
	}
}

func TestSeverityConstants_OrderingInvariant(t *testing.T) {
	// Story 34.1 ordering invariant: SevInfo < SevWarn < SevError < SevCritical
	// 比较语义保留（buildAlertEvents 用 ≥ SevWarn 过滤，alertCountBadge 用 ≥ SevError 分桶）
	if SevInfo >= SevWarn || SevWarn >= SevError || SevError >= SevCritical {
		t.Errorf("Severity ordering broken: Info=%d Warn=%d Error=%d Critical=%d",
			SevInfo, SevWarn, SevError, SevCritical)
	}
}

// --- Event 类型常量契约 ---

func TestEventTypeConstants_StringValues(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"EventStep", EventStep, "step"},
		{"EventCompact", EventCompact, "compact"},
		{"EventBudget", EventBudget, "budget"},
		{"EventSpawn", EventSpawn, "spawn"},
		{"EventExit", EventExit, "exit"},
		{"EventStall", EventStall, "stall"},
		{"EventImmune", EventImmune, "immune"},
		{"EventError", EventError, "error"},
		{"EventSyscall", EventSyscall, "syscall"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q (cmd/rnix 旧值兼容性)", c.name, c.got, c.want)
		}
	}
}

// --- UnifiedEvent struct 字段语义 ---

func TestUnifiedEvent_ZeroValueSemantics(t *testing.T) {
	var ev UnifiedEvent
	if ev.Type != "" {
		t.Errorf("zero Type should be empty string, got %q", ev.Type)
	}
	if ev.Severity != 0 {
		t.Errorf("zero Severity should be 0 (== SevInfo), got %d", ev.Severity)
	}
	if !ev.Timestamp.IsZero() {
		t.Errorf("zero Timestamp should be zero time, got %v", ev.Timestamp)
	}
	if ev.PID != 0 {
		t.Errorf("zero PID should be 0, got %d", ev.PID)
	}
	if ev.StepEntry != nil {
		t.Errorf("zero StepEntry should be nil")
	}
	if ev.RawEvent != nil {
		t.Errorf("zero RawEvent should be nil")
	}
	if ev.IsSynthetic {
		t.Errorf("zero IsSynthetic should be false")
	}
}

func TestUnifiedEvent_StepEntryAssignment(t *testing.T) {
	// timeline.StepEntry assignment must compile and round-trip
	step := &timeline.StepEntry{Summary: ipc.StepSummaryWire{Step: 7, Action: "test"}}
	ev := UnifiedEvent{
		Type:      EventStep,
		Severity:  SevInfo,
		PID:       types.PID(42),
		StepEntry: step,
	}
	if ev.StepEntry == nil || ev.StepEntry.Summary.Step != 7 {
		t.Errorf("StepEntry assignment failed: %+v", ev.StepEntry)
	}
}

func TestUnifiedEvent_RawEventAssignment(t *testing.T) {
	// ipc.SyscallEventWire assignment must compile and round-trip
	raw := &ipc.SyscallEventWire{Syscall: "ctx_alloc", PID: types.PID(42)}
	ev := UnifiedEvent{
		Type:     EventSyscall,
		Severity: SevInfo,
		RawEvent: raw,
	}
	if ev.RawEvent == nil || ev.RawEvent.Syscall != "ctx_alloc" {
		t.Errorf("RawEvent assignment failed: %+v", ev.RawEvent)
	}
}

// --- UnifiedEventSlice sort.Interface ---

func TestUnifiedEventSlice_Len(t *testing.T) {
	s := UnifiedEventSlice{{}, {}, {}}
	if got := s.Len(); got != 3 {
		t.Errorf("Len() = %d, want 3", got)
	}
	var empty UnifiedEventSlice
	if got := empty.Len(); got != 0 {
		t.Errorf("empty Len() = %d, want 0", got)
	}
}

func TestUnifiedEventSlice_LessSortsNewestFirst(t *testing.T) {
	t1 := time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 5, 4, 11, 0, 0, 0, time.UTC) // newer
	s := UnifiedEventSlice{
		{Type: EventStep, Timestamp: t1},
		{Type: EventStep, Timestamp: t2},
	}
	// Less(0, 1): is s[0] (older) before s[1] (newer)? → false (newer first)
	if s.Less(0, 1) {
		t.Errorf("Less should return false when s[0] is older than s[1] (descending order)")
	}
	// Less(1, 0): is s[1] (newer) before s[0] (older)? → true
	if !s.Less(1, 0) {
		t.Errorf("Less should return true when s[i] is newer than s[j]")
	}
}

func TestUnifiedEventSlice_Swap(t *testing.T) {
	s := UnifiedEventSlice{
		{Type: "a"},
		{Type: "b"},
	}
	s.Swap(0, 1)
	if s[0].Type != "b" || s[1].Type != "a" {
		t.Errorf("Swap failed: got %+v", s)
	}
}

func TestUnifiedEventSlice_SortDescendingByTimestamp(t *testing.T) {
	t1 := time.Date(2026, 5, 4, 9, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 5, 4, 11, 0, 0, 0, time.UTC)

	s := UnifiedEventSlice{
		{Type: EventStep, Timestamp: t1, Summary: "old"},
		{Type: EventStep, Timestamp: t3, Summary: "newest"},
		{Type: EventStep, Timestamp: t2, Summary: "middle"},
	}
	sort.Sort(s)
	if s[0].Summary != "newest" || s[1].Summary != "middle" || s[2].Summary != "old" {
		t.Errorf("sort.Sort with UnifiedEventSlice should sort newest first; got [%s, %s, %s]",
			s[0].Summary, s[1].Summary, s[2].Summary)
	}
}

func TestUnifiedEventSlice_StableForEqualTimestamps(t *testing.T) {
	now := time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC)
	s := UnifiedEventSlice{
		{Type: EventStep, Timestamp: now, Summary: "first"},
		{Type: EventStep, Timestamp: now, Summary: "second"},
		{Type: EventStep, Timestamp: now, Summary: "third"},
	}
	sort.Stable(s)
	// Stable sort preserves insertion order for equal keys
	if s[0].Summary != "first" || s[1].Summary != "second" || s[2].Summary != "third" {
		t.Errorf("sort.Stable should preserve order for equal timestamps; got [%s, %s, %s]",
			s[0].Summary, s[1].Summary, s[2].Summary)
	}
}

// --- IsSynthetic flag (38-4 P0 patch contract) ---

func TestUnifiedEvent_IsSyntheticDefaultFalse(t *testing.T) {
	ev := UnifiedEvent{Type: EventStep, Timestamp: time.Now()}
	if ev.IsSynthetic {
		t.Errorf("UnifiedEvent without explicit IsSynthetic should default to false")
	}
}

func TestUnifiedEvent_IsSyntheticExplicitTrue(t *testing.T) {
	ev := UnifiedEvent{Type: EventImmune, IsSynthetic: true}
	if !ev.IsSynthetic {
		t.Errorf("explicit IsSynthetic=true should be preserved")
	}
}
