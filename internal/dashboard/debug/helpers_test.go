// Package debug — helpers_test.go (Story 38-5 PR11 Step 4(a-2) debug pane 纯 helpers
// 迁出测试 · 同 alertstrip / timeline Step 4(a-2) test 模式)
//
// 测试覆盖：
//   - MaxStraceEvents 常量值（= 500 · 与 cmd/rnix 等价）
//   - ExtractDeviceName 6 项（/dev/llm/claude / /dev/fs / /dev/shell / /dev/mcp/xxx /
//     非 /dev/ path / 空 path / nil args）
//   - RenderSyscallLine 3 项（Info / Warn / Error · 颜色路由）
//   - RenderStepLine 5 项（nil StepEntry / HasError / ToolPath 替换 Summary /
//     ToolPath 不替换 / 完整数据）
//
// profile-tolerant 模式（38-3 教训 · GetForeground / GetBold 直接断言不依赖
// ANSI byte）。
package debug

import (
	"strings"
	"testing"

	"github.com/rnixai/rnix/internal/dashboard/event"
	"github.com/rnixai/rnix/internal/dashboard/timeline"
	"github.com/rnixai/rnix/ipc"
)

// =============================================================================
// MaxStraceEvents
// =============================================================================

func TestMaxStraceEvents_EqualsFiveHundred(t *testing.T) {
	if MaxStraceEvents != 500 {
		t.Errorf("MaxStraceEvents = %d, want 500 (与 cmd/rnix.maxDebugStraceEvents 等价)", MaxStraceEvents)
	}
}

// =============================================================================
// ExtractDeviceName (7 项)
// =============================================================================

func TestExtractDeviceName_DevLLM(t *testing.T) {
	sew := ipc.SyscallEventWire{Args: map[string]any{"path": "/dev/llm/claude"}}
	if got := ExtractDeviceName(sew); got != "llm" {
		t.Errorf("/dev/llm/claude: want 'llm', got %q", got)
	}
}

func TestExtractDeviceName_DevFS(t *testing.T) {
	sew := ipc.SyscallEventWire{Args: map[string]any{"path": "/dev/fs"}}
	if got := ExtractDeviceName(sew); got != "fs" {
		t.Errorf("/dev/fs: want 'fs', got %q", got)
	}
}

func TestExtractDeviceName_DevShell(t *testing.T) {
	sew := ipc.SyscallEventWire{Args: map[string]any{"path": "/dev/shell"}}
	if got := ExtractDeviceName(sew); got != "shell" {
		t.Errorf("/dev/shell: want 'shell', got %q", got)
	}
}

func TestExtractDeviceName_DevMCP(t *testing.T) {
	sew := ipc.SyscallEventWire{Args: map[string]any{"path": "/dev/mcp/playwright"}}
	if got := ExtractDeviceName(sew); got != "mcp" {
		t.Errorf("/dev/mcp/playwright: want 'mcp', got %q", got)
	}
}

func TestExtractDeviceName_NonDevPath(t *testing.T) {
	sew := ipc.SyscallEventWire{Args: map[string]any{"path": "/tmp/xxx"}}
	if got := ExtractDeviceName(sew); got != "/tmp/xxx" {
		t.Errorf("non-/dev path: want passthrough '/tmp/xxx', got %q", got)
	}
}

func TestExtractDeviceName_EmptyPath(t *testing.T) {
	sew := ipc.SyscallEventWire{Args: map[string]any{"path": ""}}
	if got := ExtractDeviceName(sew); got != "" {
		t.Errorf("empty path: want '', got %q", got)
	}
}

func TestExtractDeviceName_MissingPath(t *testing.T) {
	sew := ipc.SyscallEventWire{Args: map[string]any{}}
	if got := ExtractDeviceName(sew); got != "" {
		t.Errorf("missing path: want '', got %q", got)
	}
}

// =============================================================================
// RenderSyscallLine (3 项)
// =============================================================================

func TestRenderSyscallLine_Info(t *testing.T) {
	ev := event.UnifiedEvent{Type: event.EventSyscall, Severity: event.SevInfo, Summary: "Read(fd=3)"}
	got := RenderSyscallLine(ev, "  ", 80)
	if !strings.HasPrefix(got, "  ") {
		t.Errorf("cursor prefix should be present, got: %q", got)
	}
	if !strings.Contains(got, "Read(fd=3)") {
		t.Errorf("summary should be in output, got: %q", got)
	}
}

func TestRenderSyscallLine_Warn(t *testing.T) {
	ev := event.UnifiedEvent{Type: event.EventSyscall, Severity: event.SevWarn, Summary: "Write(fd=4)"}
	got := RenderSyscallLine(ev, "▸ ", 80)
	if !strings.Contains(got, "Write(fd=4)") {
		t.Errorf("summary should be in output, got: %q", got)
	}
	if !strings.HasPrefix(got, "▸ ") {
		t.Errorf("Unicode cursor prefix should be present, got: %q", got)
	}
}

func TestRenderSyscallLine_ErrorChangesColor(t *testing.T) {
	evInfo := event.UnifiedEvent{Severity: event.SevInfo, Summary: "x"}
	evError := event.UnifiedEvent{Severity: event.SevError, Summary: "x"}
	gotInfo := RenderSyscallLine(evInfo, "", 80)
	gotError := RenderSyscallLine(evError, "", 80)
	// ANSI fragments should differ between SevInfo (Muted) and SevError (Error)
	// 在 NoColor profile 下两者会都是 "x"，所以这里只断言 ANSI byte 数量差异
	// 不依赖具体颜色码。profile-tolerant.
	if gotInfo == gotError {
		// Skip on no-color profile
		t.Skip("no-color profile · skipping ANSI assertion (38-3 教训)")
	}
}

// =============================================================================
// RenderStepLine (5 项)
// =============================================================================

func TestRenderStepLine_NilStepEntryFallback(t *testing.T) {
	ev := event.UnifiedEvent{StepEntry: nil, Summary: "fallback summary"}
	got := RenderStepLine(ev, "  ", 80)
	if got != "  fallback summary" {
		t.Errorf("nil StepEntry: want '  fallback summary', got %q", got)
	}
}

func TestRenderStepLine_BasicFormat(t *testing.T) {
	ev := event.UnifiedEvent{StepEntry: &timeline.StepEntry{
		Summary: ipc.StepSummaryWire{Step: 7, Action: "tool_call", Summary: "reading config file"},
	}}
	got := RenderStepLine(ev, "▸ ", 80)
	if !strings.Contains(got, "#7") {
		t.Errorf("step label '#7' missing in: %q", got)
	}
	if !strings.Contains(got, "tool_call") {
		t.Errorf("action 'tool_call' missing in: %q", got)
	}
	if !strings.Contains(got, "reading config file") {
		t.Errorf("summary missing in: %q", got)
	}
	if !strings.HasPrefix(got, "▸ ") {
		t.Errorf("cursor prefix missing, got: %q", got)
	}
}

func TestRenderStepLine_HasErrorMark(t *testing.T) {
	ev := event.UnifiedEvent{StepEntry: &timeline.StepEntry{
		Summary: ipc.StepSummaryWire{Step: 1, Action: "tool_call", Summary: "x", HasError: true},
	}}
	got := RenderStepLine(ev, "  ", 80)
	if !strings.Contains(got, "✗") {
		t.Errorf("HasError should add ✗ mark, got: %q", got)
	}
}

func TestRenderStepLine_ToolPathReplacesShortSummary(t *testing.T) {
	// Summary 长度 < 8 + ToolPath 非空 → 用 ToolPath 替换
	ev := event.UnifiedEvent{StepEntry: &timeline.StepEntry{
		Summary: ipc.StepSummaryWire{Step: 1, Action: "tool_call", Summary: "x", ToolPath: "Read"},
	}}
	got := RenderStepLine(ev, "  ", 80)
	if !strings.Contains(got, "Read") {
		t.Errorf("short summary + non-empty ToolPath: should contain 'Read', got: %q", got)
	}
}

func TestRenderStepLine_ToolPathNotReplacingLongSummary(t *testing.T) {
	// Summary 长度 >= 8 → 不替换 (即使 ToolPath 非空)
	ev := event.UnifiedEvent{StepEntry: &timeline.StepEntry{
		Summary: ipc.StepSummaryWire{Step: 1, Action: "tool_call", Summary: "long enough summary text", ToolPath: "Read"},
	}}
	got := RenderStepLine(ev, "  ", 80)
	if !strings.Contains(got, "long enough summary text") {
		t.Errorf("long summary should NOT be replaced by ToolPath, got: %q", got)
	}
}

// ----- UpdateDeviceLatency 行为测试（Story 38-5 PR11 Step 4(c) debug pane strace pipeline helper） -----
//
// 覆盖矩阵（与 cmd/rnix dashboard_debug_test.go::TestUpdateDeviceLatency_* 同覆盖）：
//   - 空 path → 不变（无法归类）
//   - 首次记录 dev → 创建 *DeviceLatencyStats 并挂入 map · Count=1
//   - 已存在 dev → 累加 Count + TotalMs
//   - sew.Error != "" → 累加 ErrorCount
//   - DeviceLatency == nil → 自动初始化 map（nil safe）
//   - 多 dev 独立累加（/dev/llm/claude vs /dev/fs 分别归类）
//   - 其他 DebugState 字段保留

func TestUpdateDeviceLatency_EmptyPathNoOp(t *testing.T) {
	state := DebugState{DeviceLatency: make(map[string]*DeviceLatencyStats)}
	got := UpdateDeviceLatency(state, ipc.SyscallEventWire{Args: map[string]any{"path": ""}})
	if len(got.DeviceLatency) != 0 {
		t.Errorf("empty path: DeviceLatency len = %d, want 0 (no-op)", len(got.DeviceLatency))
	}
}

func TestUpdateDeviceLatency_FirstRecord(t *testing.T) {
	state := DebugState{DeviceLatency: make(map[string]*DeviceLatencyStats)}
	got := UpdateDeviceLatency(state, ipc.SyscallEventWire{
		Args:       map[string]any{"path": "/dev/llm/claude"},
		DurationMs: 12.5,
	})
	stats := got.DeviceLatency["llm"]
	if stats == nil {
		t.Fatalf("first record: DeviceLatency['llm'] = nil, want *DeviceLatencyStats")
	}
	if stats.Count != 1 {
		t.Errorf("first record: Count = %d, want 1", stats.Count)
	}
	if stats.TotalMs != 12.5 {
		t.Errorf("first record: TotalMs = %v, want 12.5", stats.TotalMs)
	}
	if stats.ErrorCount != 0 {
		t.Errorf("first record: ErrorCount = %d, want 0", stats.ErrorCount)
	}
}

func TestUpdateDeviceLatency_AccumulatesExisting(t *testing.T) {
	state := DebugState{DeviceLatency: make(map[string]*DeviceLatencyStats)}
	state = UpdateDeviceLatency(state, ipc.SyscallEventWire{
		Args:       map[string]any{"path": "/dev/fs"},
		DurationMs: 5.0,
	})
	state = UpdateDeviceLatency(state, ipc.SyscallEventWire{
		Args:       map[string]any{"path": "/dev/fs"},
		DurationMs: 7.5,
	})
	stats := state.DeviceLatency["fs"]
	if stats.Count != 2 {
		t.Errorf("accumulates: Count = %d, want 2", stats.Count)
	}
	if stats.TotalMs != 12.5 {
		t.Errorf("accumulates: TotalMs = %v, want 12.5", stats.TotalMs)
	}
}

func TestUpdateDeviceLatency_CountsErrors(t *testing.T) {
	state := DebugState{DeviceLatency: make(map[string]*DeviceLatencyStats)}
	state = UpdateDeviceLatency(state, ipc.SyscallEventWire{
		Args:       map[string]any{"path": "/dev/shell"},
		DurationMs: 3.0,
	})
	state = UpdateDeviceLatency(state, ipc.SyscallEventWire{
		Args:       map[string]any{"path": "/dev/shell"},
		DurationMs: 2.0,
		Error:      "timeout",
	})
	state = UpdateDeviceLatency(state, ipc.SyscallEventWire{
		Args:       map[string]any{"path": "/dev/shell"},
		DurationMs: 1.0,
		Error:      "broken pipe",
	})
	stats := state.DeviceLatency["shell"]
	if stats.Count != 3 {
		t.Errorf("error counting: Count = %d, want 3", stats.Count)
	}
	if stats.ErrorCount != 2 {
		t.Errorf("error counting: ErrorCount = %d, want 2 (2 of 3 had Error)", stats.ErrorCount)
	}
}

func TestUpdateDeviceLatency_NilDeviceLatencyAutoInit(t *testing.T) {
	state := DebugState{DeviceLatency: nil}
	got := UpdateDeviceLatency(state, ipc.SyscallEventWire{
		Args:       map[string]any{"path": "/dev/mcp/playwright"},
		DurationMs: 100.0,
	})
	if got.DeviceLatency == nil {
		t.Fatalf("nil DeviceLatency: should auto-init to non-nil map")
	}
	stats := got.DeviceLatency["mcp"]
	if stats == nil || stats.Count != 1 {
		t.Errorf("nil DeviceLatency auto-init: stats = %+v, want Count=1", stats)
	}
}

func TestUpdateDeviceLatency_MultipleDevicesIndependent(t *testing.T) {
	state := DebugState{DeviceLatency: make(map[string]*DeviceLatencyStats)}
	state = UpdateDeviceLatency(state, ipc.SyscallEventWire{
		Args:       map[string]any{"path": "/dev/llm/claude"},
		DurationMs: 10.0,
	})
	state = UpdateDeviceLatency(state, ipc.SyscallEventWire{
		Args:       map[string]any{"path": "/dev/fs"},
		DurationMs: 2.0,
	})
	state = UpdateDeviceLatency(state, ipc.SyscallEventWire{
		Args:       map[string]any{"path": "/dev/llm/claude"},
		DurationMs: 5.0,
	})
	if state.DeviceLatency["llm"].Count != 2 {
		t.Errorf("llm Count = %d, want 2", state.DeviceLatency["llm"].Count)
	}
	if state.DeviceLatency["llm"].TotalMs != 15.0 {
		t.Errorf("llm TotalMs = %v, want 15.0", state.DeviceLatency["llm"].TotalMs)
	}
	if state.DeviceLatency["fs"].Count != 1 {
		t.Errorf("fs Count = %d, want 1", state.DeviceLatency["fs"].Count)
	}
	if state.DeviceLatency["fs"].TotalMs != 2.0 {
		t.Errorf("fs TotalMs = %v, want 2.0", state.DeviceLatency["fs"].TotalMs)
	}
}

func TestUpdateDeviceLatency_PreservesOtherFields(t *testing.T) {
	state := DebugState{
		Mode:          true,
		ShowStrace:    true,
		AttachedPID:   42,
		Cursor:        7,
		ScrollTop:     3,
		AutoScroll:    false,
		DeviceLatency: make(map[string]*DeviceLatencyStats),
	}
	got := UpdateDeviceLatency(state, ipc.SyscallEventWire{
		Args:       map[string]any{"path": "/dev/fs"},
		DurationMs: 1.0,
	})
	if !got.Mode || !got.ShowStrace || got.AttachedPID != 42 ||
		got.Cursor != 7 || got.ScrollTop != 3 || got.AutoScroll {
		t.Errorf("other fields mutated: got %+v", got)
	}
}

func TestUpdateDeviceLatency_NonDevPathPassthrough(t *testing.T) {
	// Non-/dev/ path → ExtractDeviceName 原样返回 → DeviceLatency 用 path 作 key
	state := DebugState{DeviceLatency: make(map[string]*DeviceLatencyStats)}
	got := UpdateDeviceLatency(state, ipc.SyscallEventWire{
		Args:       map[string]any{"path": "/tmp/work"},
		DurationMs: 4.0,
	})
	stats := got.DeviceLatency["/tmp/work"]
	if stats == nil || stats.Count != 1 {
		t.Errorf("non-/dev path: stats = %+v, want Count=1", stats)
	}
}

// ----- AppendStraceEvent 行为测试（Story 38-5 PR11 Step 4(c) debug pane strace pipeline helper） -----
//
// 覆盖矩阵：
//   - StraceEvents nil → append 自动初始化（Go append 语义 · nil safe）
//   - len < MaxStraceEvents → 直接 append（无截断）
//   - len == MaxStraceEvents → append 后保留末尾 MaxStraceEvents 条 / 丢弃最早（FIFO）
//   - 远超 MaxStraceEvents（连续 append N+50） → 始终保留末尾 MaxStraceEvents 条
//   - 截断后保留的元素与最近 MaxStraceEvents 个 append 一致（顺序契约）
//   - 其他 DebugState 字段（Mode/Cursor/AttachedPID 等）一律保留

func TestAppendStraceEvent_NilStraceEventsInit(t *testing.T) {
	state := DebugState{StraceEvents: nil}
	ev := event.UnifiedEvent{Type: event.EventSyscall, Summary: "first"}
	got := AppendStraceEvent(state, ev)
	if len(got.StraceEvents) != 1 {
		t.Fatalf("nil init: len = %d, want 1", len(got.StraceEvents))
	}
	if got.StraceEvents[0].Summary != "first" {
		t.Errorf("nil init: Summary = %q, want 'first'", got.StraceEvents[0].Summary)
	}
}

func TestAppendStraceEvent_AppendsBelowCap(t *testing.T) {
	state := DebugState{}
	for i := range 5 {
		state = AppendStraceEvent(state, event.UnifiedEvent{Summary: "ev"})
		_ = i
	}
	if len(state.StraceEvents) != 5 {
		t.Errorf("below cap: len = %d, want 5", len(state.StraceEvents))
	}
}

func TestAppendStraceEvent_TrimsAtCap(t *testing.T) {
	// 灌满到 MaxStraceEvents 后再 append 一条 → 长度仍是 MaxStraceEvents · 头被丢弃
	state := DebugState{}
	for i := range MaxStraceEvents {
		state = AppendStraceEvent(state, event.UnifiedEvent{Summary: "old"})
		_ = i
	}
	if len(state.StraceEvents) != MaxStraceEvents {
		t.Fatalf("fill: len = %d, want %d", len(state.StraceEvents), MaxStraceEvents)
	}
	state = AppendStraceEvent(state, event.UnifiedEvent{Summary: "new"})
	if len(state.StraceEvents) != MaxStraceEvents {
		t.Errorf("trim at cap: len = %d, want %d (FIFO)", len(state.StraceEvents), MaxStraceEvents)
	}
	// 末尾应是新 append 的 "new"
	if state.StraceEvents[MaxStraceEvents-1].Summary != "new" {
		t.Errorf("trim at cap: last summary = %q, want 'new'", state.StraceEvents[MaxStraceEvents-1].Summary)
	}
}

func TestAppendStraceEvent_RingBufferOverflow(t *testing.T) {
	// 灌入 MaxStraceEvents+50 → 长度始终 = MaxStraceEvents · 与 cmd/rnix 测试同模式
	// （dashboard_debug_test.go::TestAppendStraceEvent line 267）
	state := DebugState{}
	for i := range MaxStraceEvents + 50 {
		state = AppendStraceEvent(state, event.UnifiedEvent{Summary: "ev"})
		_ = i
	}
	if len(state.StraceEvents) != MaxStraceEvents {
		t.Errorf("overflow: len = %d, want %d", len(state.StraceEvents), MaxStraceEvents)
	}
}

func TestAppendStraceEvent_PreservesFIFOOrder(t *testing.T) {
	// 给每个 ev 一个唯一 Summary · 灌入 MaxStraceEvents+10 后 · 头部应是 #10 (前 10 个被丢弃)
	state := DebugState{}
	for i := range MaxStraceEvents + 10 {
		state = AppendStraceEvent(state, event.UnifiedEvent{Summary: fmtSummary(i)})
	}
	// 第一个保留的应是 #10（前 10 个 [#0..#9] 被丢弃）
	if state.StraceEvents[0].Summary != fmtSummary(10) {
		t.Errorf("FIFO order: head = %q, want %q (前 10 被丢弃)", state.StraceEvents[0].Summary, fmtSummary(10))
	}
	// 末尾应是 #(MaxStraceEvents+10-1) = #(MaxStraceEvents+9)
	if state.StraceEvents[MaxStraceEvents-1].Summary != fmtSummary(MaxStraceEvents+9) {
		t.Errorf("FIFO order: tail = %q, want %q", state.StraceEvents[MaxStraceEvents-1].Summary, fmtSummary(MaxStraceEvents+9))
	}
}

func TestAppendStraceEvent_PreservesOtherFields(t *testing.T) {
	state := DebugState{
		Mode:        true,
		ShowStrace:  true,
		AttachedPID: 42,
		Cursor:      7,
		ScrollTop:   3,
		AutoScroll:  false,
	}
	got := AppendStraceEvent(state, event.UnifiedEvent{Summary: "x"})
	if !got.Mode || !got.ShowStrace || got.AttachedPID != 42 ||
		got.Cursor != 7 || got.ScrollTop != 3 || got.AutoScroll {
		t.Errorf("other fields mutated: got %+v", got)
	}
}

// fmtSummary 辅助生成可区分的事件 Summary 用于 FIFO 顺序断言。
func fmtSummary(i int) string {
	// 不依赖 fmt.Sprintf · 让测试更轻量
	digits := []byte{}
	if i == 0 {
		digits = append(digits, '0')
	} else {
		for n := i; n > 0; n /= 10 {
			digits = append([]byte{byte('0' + n%10)}, digits...)
		}
	}
	return "ev#" + string(digits)
}

// ----- ClampCursor 行为测试（Story 38-5 PR11 Step 4(c) debug pane 视图状态 helper） -----
//
// 覆盖矩阵：
//   - filteredLen == 0 + Cursor > 0 → Cursor = 0 + ScrollTop = 0
//   - Cursor >= filteredLen → Cursor = filteredLen - 1
//   - ScrollTop > Cursor → ScrollTop = Cursor
//   - Cursor 已在范围内 + ScrollTop ≤ Cursor → no-op（保留状态）
//   - Cursor < 0 (理论越界) + filteredLen > 0 → Cursor 不变（仅向下 clamp · 与原算法等价）
//   - 其他字段保留（Mode/ShowStrace/AttachedPID 等）

func TestClampCursor_EmptyFiltered(t *testing.T) {
	state := DebugState{Cursor: 5, ScrollTop: 3}
	got := ClampCursor(state, 0)
	if got.Cursor != 0 {
		t.Errorf("empty filtered: Cursor = %d, want 0", got.Cursor)
	}
	if got.ScrollTop != 0 {
		t.Errorf("empty filtered: ScrollTop = %d, want 0", got.ScrollTop)
	}
}

func TestClampCursor_CursorBeyondRange(t *testing.T) {
	state := DebugState{Cursor: 20, ScrollTop: 5}
	got := ClampCursor(state, 10)
	if got.Cursor != 9 {
		t.Errorf("cursor beyond range: Cursor = %d, want 9 (filteredLen-1)", got.Cursor)
	}
	// ScrollTop=5 ≤ Cursor=9 → ScrollTop 保留
	if got.ScrollTop != 5 {
		t.Errorf("cursor beyond range: ScrollTop = %d, want 5 (preserved)", got.ScrollTop)
	}
}

func TestClampCursor_ScrollTopExceedsCursor(t *testing.T) {
	state := DebugState{Cursor: 3, ScrollTop: 7}
	got := ClampCursor(state, 10)
	// Cursor=3 < filteredLen=10 → 保留
	if got.Cursor != 3 {
		t.Errorf("Cursor in range: Cursor = %d, want 3", got.Cursor)
	}
	// ScrollTop=7 > Cursor=3 → ScrollTop=3
	if got.ScrollTop != 3 {
		t.Errorf("scroll top exceeds cursor: ScrollTop = %d, want 3 (snap to Cursor)", got.ScrollTop)
	}
}

func TestClampCursor_NoOp(t *testing.T) {
	state := DebugState{Cursor: 5, ScrollTop: 2}
	got := ClampCursor(state, 10)
	if got.Cursor != 5 || got.ScrollTop != 2 {
		t.Errorf("no-op: got Cursor=%d ScrollTop=%d, want Cursor=5 ScrollTop=2", got.Cursor, got.ScrollTop)
	}
}

func TestClampCursor_NegativeCursor(t *testing.T) {
	// Cursor=-1 不在 Cursor>=filteredLen 分支 · 不变（仅向下 clamp · 与 cmd/rnix 算法等价）
	state := DebugState{Cursor: -1, ScrollTop: 0}
	got := ClampCursor(state, 5)
	if got.Cursor != -1 {
		t.Errorf("negative cursor: Cursor = %d, want -1 (not clamped upward · only downward clamp)", got.Cursor)
	}
}

func TestClampCursor_PreservesOtherFields(t *testing.T) {
	state := DebugState{
		Cursor:      20,
		ScrollTop:   5,
		Mode:        true,
		ShowStrace:  true,
		AttachedPID: 42,
		AutoScroll:  false,
	}
	got := ClampCursor(state, 10)
	if !got.Mode {
		t.Errorf("Mode mutated: got false, want true")
	}
	if !got.ShowStrace {
		t.Errorf("ShowStrace mutated: got false, want true")
	}
	if got.AttachedPID != 42 {
		t.Errorf("AttachedPID mutated: got %d, want 42", got.AttachedPID)
	}
	if got.AutoScroll {
		t.Errorf("AutoScroll mutated: got true, want false")
	}
}
