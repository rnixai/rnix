// ATDD Story 43.3 - Timeline Renderer for Script Trace Events
//
// Red-phase tests for `BuildScriptAggGroups` + `ScriptAggGroup` type +
// `SysEventStyle` (EventScript branch) + `DefaultStepFilters` (EventScript key)
// + `IsEventVisible` (EventScript filter routing) helpers in
// internal/dashboard/event/helpers.go (additions).
//
// Acceptance Criteria covered:
//   - AC#4: EventScript 接入 UnifiedEvent 类型族 + 样式 + filter
//   - AC#5: 高频事件折叠（ScriptAggGroup · 阈值复用 AggThreshold=3）
//
// RED 信号（dev-story 实施前 `go test -tags atdd_red ./internal/dashboard/event/...` 应失败）：
//   - undefined: EventScript
//   - undefined: BuildScriptAggGroups
//   - undefined: ScriptAggGroup
//   - SysEventStyle(EventScript) returns default ColorMuted (灰) instead of blue #5B9BD5
//   - DefaultStepFilters()[EventScript] returns false (key absent)
//
// 实施完成后应能编译并通过；最后移除 build tag 让 `make all` 接管。

package event

import (
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
)

// =============================================================================
// EventScript 常量 + SysEventStyle 分支 — AC#4
// =============================================================================

// TestEventScript_ConstantValue — AC#4 EventScript 常量值 = "script"
// （与 ScriptExecutor 概念一致 · 与已有 "compact"/"budget"/"stall" 同风格全小写）。
func TestEventScript_ConstantValue(t *testing.T) {
	if EventScript != "script" {
		t.Errorf("EventScript = %q, want \"script\"", EventScript)
	}
}

// TestSysEventStyle_Script_SevInfo — AC#4 SysEventStyle(EventScript, SevInfo)
// 返回蓝色 #5B9BD5 · 与 ActionColor("plan") 同色 · 表示"流程性事件"。
func TestSysEventStyle_Script_SevInfo(t *testing.T) {
	ev := UnifiedEvent{Type: EventScript, Severity: SevInfo}
	style := SysEventStyle(ev)
	// lipgloss.Style 比较通过渲染后比较前缀 ANSI escape；
	// 这里通过 GetForeground() 获取颜色断言更稳。
	fg := style.GetForeground()
	want := lipgloss.Color("#5B9BD5")
	if fg != want {
		t.Errorf("SysEventStyle(EventScript, SevInfo) foreground = %v, want %v (blue)", fg, want)
	}
}

// TestSysEventStyle_Script_SevError — AC#4 错误压过类型色：Severity >= SevError 时
// 返回 ColorError 红色（与 EventExit 同模式）。
func TestSysEventStyle_Script_SevError(t *testing.T) {
	ev := UnifiedEvent{Type: EventScript, Severity: SevError}
	style := SysEventStyle(ev)
	fg := style.GetForeground()
	want := lipgloss.Color(ui.ColorError)
	if fg != want {
		t.Errorf("SysEventStyle(EventScript, SevError) foreground = %v, want %v (ColorError)", fg, want)
	}
}

// TestSysEventStyle_Script_SevCritical — AC#4 SevCritical >= SevError 也走红色分支。
func TestSysEventStyle_Script_SevCritical(t *testing.T) {
	ev := UnifiedEvent{Type: EventScript, Severity: SevCritical}
	style := SysEventStyle(ev)
	fg := style.GetForeground()
	want := lipgloss.Color(ui.ColorError)
	if fg != want {
		t.Errorf("SysEventStyle(EventScript, SevCritical) foreground = %v, want %v", fg, want)
	}
}

// =============================================================================
// DefaultStepFilters — AC#4
// =============================================================================

// TestDefaultStepFilters_IncludesScript — AC#4 filter map 默认含 EventScript: true
// （让 Timeline 默认显示 Script 事件 · 用户可按 S 关闭）。
func TestDefaultStepFilters_IncludesScript(t *testing.T) {
	filters := DefaultStepFilters()
	v, ok := filters[EventScript]
	if !ok {
		t.Fatalf("DefaultStepFilters missing key %q (EventScript)", EventScript)
	}
	if !v {
		t.Errorf("DefaultStepFilters[EventScript] = false, want true")
	}
}

// =============================================================================
// IsEventVisible — AC#4
// =============================================================================

// TestIsEventVisible_Script_FilterOff — AC#4 关闭 EventScript filter →
// IsEventVisible 返回 false（与其他 system event 同模式）。
func TestIsEventVisible_Script_FilterOff(t *testing.T) {
	ev := UnifiedEvent{Type: EventScript}
	filters := DefaultStepFilters()
	filters[EventScript] = false
	if IsEventVisible(ev, filters) {
		t.Error("IsEventVisible(EventScript, filter=off) = true, want false")
	}
}

// TestIsEventVisible_Script_FilterOn — AC#4 默认开启时 visible。
func TestIsEventVisible_Script_FilterOn(t *testing.T) {
	ev := UnifiedEvent{Type: EventScript}
	filters := DefaultStepFilters()
	if !IsEventVisible(ev, filters) {
		t.Error("IsEventVisible(EventScript, default filters) = false, want true")
	}
}

// TestIsEventVisible_Script_DoesNotCollideWithSpawn — AC#4 EventScript filter
// 与已有 step-action "spawn" / "sys_spawn" 完全独立（不同 key namespace）。
func TestIsEventVisible_Script_DoesNotCollideWithSpawn(t *testing.T) {
	scriptEv := UnifiedEvent{Type: EventScript}
	// 关闭 sys_spawn 不应影响 Script 可见性
	filters := DefaultStepFilters()
	filters["sys_spawn"] = false
	if !IsEventVisible(scriptEv, filters) {
		t.Error("EventScript should not be hidden by sys_spawn=false (filter keys are independent)")
	}
	// 关闭 EventScript 不应影响 sys spawn 事件
	filters2 := DefaultStepFilters()
	filters2[EventScript] = false
	spawnEv := UnifiedEvent{Type: EventSpawn}
	if !IsEventVisible(spawnEv, filters2) {
		t.Error("EventSpawn should not be hidden by EventScript=false")
	}
}

// =============================================================================
// BuildScriptAggGroups + ScriptAggGroup — AC#5
// =============================================================================

// mkScriptEv 是测试 helper：构造 EventScript 类型的 UnifiedEvent，
// 含 RawEvent.Args 中的 line/stmt_kind 字段（聚合需要这两个 key）。
func mkScriptEv(t *testing.T, syscall string, line int, stmtKind string, sev int) UnifiedEvent {
	t.Helper()
	wire := ipc.SyscallEventWire{
		TimestampMs: time.Now().UnixMilli(),
		Syscall:     syscall,
		Args: map[string]any{
			"line":      line,
			"stmt_kind": stmtKind,
		},
	}
	return UnifiedEvent{
		Type:     EventScript,
		Severity: sev,
		Summary:  "stub",
		RawEvent: &wire,
	}
}

// TestBuildScriptAggGroups_EmptyInput — AC#5 边界：nil / 空 slice → nil 或空 slice。
func TestBuildScriptAggGroups_EmptyInput(t *testing.T) {
	if got := BuildScriptAggGroups(nil); len(got) != 0 {
		t.Errorf("nil input: want 0 groups, got %d", len(got))
	}
	if got := BuildScriptAggGroups([]UnifiedEvent{}); len(got) != 0 {
		t.Errorf("empty input: want 0 groups, got %d", len(got))
	}
}

// TestBuildScriptAggGroups_BelowThreshold — AC#5 < AggThreshold(=3) 条同 stmt_kind
// 不聚合（与 BuildToolAggGroups 同语义）。
func TestBuildScriptAggGroups_BelowThreshold(t *testing.T) {
	events := []UnifiedEvent{
		mkScriptEv(t, "ScriptStmtBegin", 10, "assign", SevInfo),
		mkScriptEv(t, "ScriptStmtEnd", 10, "assign", SevInfo),
	}
	got := BuildScriptAggGroups(events)
	if len(got) != 0 {
		t.Errorf("2 events (< AggThreshold=3): want 0 groups, got %d", len(got))
	}
}

// TestBuildScriptAggGroups_AtThreshold — AC#5 3 条连续同 stmt_kind StmtBegin
// 触发 1 个 group。
func TestBuildScriptAggGroups_AtThreshold(t *testing.T) {
	events := []UnifiedEvent{
		mkScriptEv(t, "ScriptStmtBegin", 10, "assign", SevInfo),
		mkScriptEv(t, "ScriptStmtBegin", 11, "assign", SevInfo),
		mkScriptEv(t, "ScriptStmtBegin", 12, "assign", SevInfo),
	}
	got := BuildScriptAggGroups(events)
	if len(got) != 1 {
		t.Fatalf("3 events (= AggThreshold): want 1 group, got %d", len(got))
	}
	g := got[0]
	if g.StartIdx != 0 || g.EndIdx != 3 {
		t.Errorf("group bounds: want [0,3), got [%d,%d)", g.StartIdx, g.EndIdx)
	}
	if g.StmtKind != "assign" {
		t.Errorf("StmtKind = %q, want \"assign\"", g.StmtKind)
	}
	if g.Count != 3 {
		t.Errorf("Count = %d, want 3", g.Count)
	}
	if g.FirstLine != 10 {
		t.Errorf("FirstLine = %d, want 10", g.FirstLine)
	}
	if g.LastLine != 12 {
		t.Errorf("LastLine = %d, want 12", g.LastLine)
	}
}

// TestBuildScriptAggGroups_FivePairsAggregateAsOne — AC#5 spec 示例：
// 5 条 StmtBegin + 5 条 StmtEnd 同 stmt_kind=assign → 1 个 fold group
// 显示 "L10-L14 ▸ assign × 5"（聚合按总条数 10）。
// 注意：spec 文本 "× 5" 是 begin/end 对数，但 BuildScriptAggGroups 应统计实际事件
// 总条数（10）；fold 行 Summary 由 renderer 格式化时处理。
func TestBuildScriptAggGroups_FivePairsAggregateAsOne(t *testing.T) {
	var events []UnifiedEvent
	for i := range 5 {
		events = append(events,
			mkScriptEv(t, "ScriptStmtBegin", 10+i, "assign", SevInfo),
			mkScriptEv(t, "ScriptStmtEnd", 10+i, "assign", SevInfo),
		)
	}
	got := BuildScriptAggGroups(events)
	if len(got) != 1 {
		t.Fatalf("want 1 group from 10 same-kind events, got %d", len(got))
	}
	g := got[0]
	if g.StmtKind != "assign" {
		t.Errorf("StmtKind = %q, want \"assign\"", g.StmtKind)
	}
	if g.FirstLine != 10 || g.LastLine != 14 {
		t.Errorf("line range: got [%d,%d], want [10,14]", g.FirstLine, g.LastLine)
	}
}

// TestBuildScriptAggGroups_ErrorBreaksRun — AC#5 含 error 的 ScriptStmtEnd
// （Severity >= SevError）永远不被吞进 fold；即使周围都是同 stmt_kind 的
// 成功 begin/end，错误条目单独显示。
func TestBuildScriptAggGroups_ErrorBreaksRun(t *testing.T) {
	events := []UnifiedEvent{
		mkScriptEv(t, "ScriptStmtBegin", 10, "pipeline", SevInfo),
		mkScriptEv(t, "ScriptStmtEnd", 10, "pipeline", SevInfo),
		mkScriptEv(t, "ScriptStmtBegin", 11, "pipeline", SevInfo),
		mkScriptEv(t, "ScriptStmtEnd", 11, "pipeline", SevError), // error · 不可聚合
		mkScriptEv(t, "ScriptStmtBegin", 12, "pipeline", SevInfo),
		mkScriptEv(t, "ScriptStmtEnd", 12, "pipeline", SevInfo),
	}
	got := BuildScriptAggGroups(events)
	// 上下两段各 3 个 / 2 个 event，均不达阈值（中间被 error 切断）：
	//   - 段 1 [0..2]: begin, end, begin → 3 events 同 stmt_kind, 可聚合
	//   - 段 2 [4..5]: begin, end → 2 events 同 stmt_kind, 不达阈值
	// 因此 want 1 group from 段 1。允许 0 个 group（段 1 中混合 begin/end 也算 3 同 kind）。
	for _, g := range got {
		// 关键断言：含 error 的 idx=3 不在任何 group 范围内
		if 3 >= g.StartIdx && 3 < g.EndIdx {
			t.Errorf("error event (idx=3, SevError) absorbed into group [%d,%d)", g.StartIdx, g.EndIdx)
		}
	}
}

// TestBuildScriptAggGroups_MixedKindsDontMerge — AC#5 不同 stmt_kind 不聚合：
// assign / spawn / while 交替 → 都不达阈值 → 0 group。
func TestBuildScriptAggGroups_MixedKindsDontMerge(t *testing.T) {
	events := []UnifiedEvent{
		mkScriptEv(t, "ScriptStmtBegin", 10, "assign", SevInfo),
		mkScriptEv(t, "ScriptStmtBegin", 11, "spawn", SevInfo),
		mkScriptEv(t, "ScriptStmtBegin", 12, "while", SevInfo),
		mkScriptEv(t, "ScriptStmtBegin", 13, "assign", SevInfo),
	}
	got := BuildScriptAggGroups(events)
	if len(got) != 0 {
		t.Errorf("mixed stmt_kind: want 0 groups, got %d", len(got))
	}
}

// TestBuildScriptAggGroups_DoesNotAggregateSpawnWhileCondition — AC#5 关键约束：
// 不对 ScriptSpawn / ScriptWhileIter / ScriptCondition 聚合（这三类是用户最关心的
// "事件性事件"，每条单独显示）。仅 StmtBegin/StmtEnd 参与聚合。
func TestBuildScriptAggGroups_DoesNotAggregateSpawnWhileCondition(t *testing.T) {
	cases := []struct {
		name    string
		syscall string
	}{
		{"spawn", "ScriptSpawn"},
		{"while_iter", "ScriptWhileIter"},
		{"condition", "ScriptCondition"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			events := []UnifiedEvent{
				mkScriptEv(t, tc.syscall, 10, "", SevInfo),
				mkScriptEv(t, tc.syscall, 11, "", SevInfo),
				mkScriptEv(t, tc.syscall, 12, "", SevInfo),
				mkScriptEv(t, tc.syscall, 13, "", SevInfo),
				mkScriptEv(t, tc.syscall, 14, "", SevInfo),
			}
			got := BuildScriptAggGroups(events)
			if len(got) != 0 {
				t.Errorf("%s should never aggregate (event-of-interest type): want 0 groups, got %d", tc.syscall, len(got))
			}
		})
	}
}

// TestBuildScriptAggGroups_NonScriptEventsAreSkipped — AC#5 BuildScriptAggGroups
// 只看 Type==EventScript 的事件，其他类型（EventStep / EventCompact / ...）跳过。
// 与 BuildToolAggGroups 只看 EventStep 同模式 · 两类聚合在同一 Timeline 上不冲突。
func TestBuildScriptAggGroups_NonScriptEventsAreSkipped(t *testing.T) {
	events := []UnifiedEvent{
		{Type: EventStep},                                            // 跳过
		mkScriptEv(t, "ScriptStmtBegin", 10, "assign", SevInfo),      // 参与
		{Type: EventCompact},                                         // 跳过
		mkScriptEv(t, "ScriptStmtBegin", 11, "assign", SevInfo),      // 参与
		mkScriptEv(t, "ScriptStmtBegin", 12, "assign", SevInfo),      // 参与
		// idx 1, 3, 4 形成同 stmt_kind 3 条；但它们不"连续"（idx 2 是 EventCompact）。
		// 是否切断由实现决定 · 这里只断言非 Script 事件不被纳入 group 内部。
	}
	got := BuildScriptAggGroups(events)
	for _, g := range got {
		for i := g.StartIdx; i < g.EndIdx; i++ {
			if events[i].Type != EventScript {
				t.Errorf("group [%d,%d) contains non-script event at idx %d (Type=%q)",
					g.StartIdx, g.EndIdx, i, events[i].Type)
			}
		}
	}
}

// TestScriptAggGroup_ZeroValueIsSafe — ScriptAggGroup 零值不引发后续 panic
// （Renderer 可能在边界场景下使用零值 · 与 ToolAggGroup 同模式）。
func TestScriptAggGroup_ZeroValueIsSafe(t *testing.T) {
	var g ScriptAggGroup
	if g.StartIdx != 0 || g.EndIdx != 0 || g.StmtKind != "" || g.Count != 0 {
		t.Errorf("zero value not zero: %+v", g)
	}
}
