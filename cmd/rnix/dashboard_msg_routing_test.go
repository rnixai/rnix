// Package main — dashboard_msg_routing_test.go (Story 38-5 Code Review G3-1/G3-2/G3-3/G4-11 fix)
//
// 端到端 msg 路由契约测试。修复前 TimelineModel.OnTick 返回 timeline.StepListMsg
// （大写公开字段）· cmd/rnix 端 case stepListMsg 是不同的小写 struct 类型 · 路由
// 不匹配 · 消息静默丢弃。本测试验证 alias 化（type stepListMsg = timeline.StepListMsg）
// 之后两条路径（cmd/rnix 旧 fetchStepsCmd + TimelineModel.OnTick）的 msg 都能路由
// 到同一个 case 并更新 state.
//
// G3-1: timeline.StepListMsg → case stepListMsg
// G3-2: detail.DetailResultMsg → case procDetailResultMsg
// G4-11 扩展（覆盖 4 个 PaneModel.OnTick → cmd/rnix Update case 路由）：
//   - intent.TreesMsg → case intentTreesMsg
//   - security.ImmuneStatusMsg → case immuneStatusMsg
//   - trace.ListMsg → case traceListMsg
//   - eval.{ReputationMsg, TopologyMsg, SynergyMsg} → case evalReputationMsg/...
package main

import (
	"testing"

	"github.com/rnixai/rnix/internal/dashboard/detail"
	"github.com/rnixai/rnix/internal/dashboard/eval"
	"github.com/rnixai/rnix/internal/dashboard/intent"
	"github.com/rnixai/rnix/internal/dashboard/security"
	"github.com/rnixai/rnix/internal/dashboard/timeline"
	"github.com/rnixai/rnix/internal/dashboard/trace"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/kernel"
)

// TestStepListMsg_RoutedFromInternalPackage 验证 timeline.StepListMsg（internal 包 alias）
// 进入 dashboardModel.Update 后能命中 case stepListMsg 并更新 timeline state。
//
// 该测试在 G3-1 修复前会失败：修复前 cmd/rnix 端 case stepListMsg 是独立 struct 类型 ·
// timeline.StepListMsg 不匹配 · Update 默默 fall-through · m.timeline.StepEntries 不变.
func TestStepListMsg_RoutedFromInternalPackage(t *testing.T) {
	m := dashboardModel{
		selectedPID:  types.PID(42),
		selectedUUID: "uuid-42",
	}
	// 直接构造 internal 包 alias 类型 · 模拟 TimelineModel.OnTick → FetchStepsCmd 路径
	msg := timeline.StepListMsg{
		UUID: "uuid-42",
		PID:  types.PID(42),
		Steps: []ipc.StepSummaryWire{
			{Step: 1, Action: "plan", Summary: "first-step"},
			{Step: 2, Action: "tool_call", Summary: "second-step"},
		},
		Total: 2,
	}
	updated, _ := m.Update(msg)
	um, ok := updated.(dashboardModel)
	if !ok {
		t.Fatalf("Update returned non-dashboardModel: %T", updated)
	}
	if len(um.timeline.StepEntries) != 2 {
		t.Errorf("expected 2 step entries from internal-package msg, got %d", len(um.timeline.StepEntries))
	}
	if um.timeline.LastFetchedStep != 2 {
		t.Errorf("expected LastFetchedStep=2, got %d", um.timeline.LastFetchedStep)
	}
}

// TestProcDetailResultMsg_RoutedFromInternalPackage 验证 detail.DetailResultMsg
// （internal 包 alias）能命中 case procDetailResultMsg 并更新 detail state。
//
// G3-2 修复前类似 G3-1 路径失败.
func TestProcDetailResultMsg_RoutedFromInternalPackage(t *testing.T) {
	m := dashboardModel{
		selectedPID:  types.PID(7),
		selectedUUID: "uuid-7",
		detail: detail.DetailState{
			Cache: map[string]*ipc.GetProcDetailResponse{},
		},
	}
	procDetail := &ipc.GetProcDetailResponse{PID: 7, State: "running"}
	msg := detail.DetailResultMsg{
		PID:    types.PID(7),
		UUID:   "uuid-7",
		Detail: procDetail,
	}
	updated, _ := m.Update(msg)
	um, ok := updated.(dashboardModel)
	if !ok {
		t.Fatalf("Update returned non-dashboardModel: %T", updated)
	}
	if cached, found := um.detail.Cache["uuid-7"]; !found {
		t.Error("Cache should contain entry for uuid-7")
	} else if cached.State != "running" {
		t.Errorf("cached detail state = %q, want \"running\"", cached.State)
	}
	if um.detail.Detail == nil {
		t.Fatal("Detail should be set when PID/UUID match")
	}
	if um.detail.PID != types.PID(7) {
		t.Errorf("Detail.PID = %d, want 7", um.detail.PID)
	}
}

// TestStepListMsg_StaleUUID_DiscardedFromInternalPackage 验证 stale UUID 守卫
// 在 internal 包 msg 路径下也生效（与 cmd/rnix.fetchStepsCmd 路径行为一致）.
func TestStepListMsg_StaleUUID_DiscardedFromInternalPackage(t *testing.T) {
	m := dashboardModel{
		selectedPID:  types.PID(2),
		selectedUUID: "uuid-current",
	}
	staleMsg := timeline.StepListMsg{
		UUID: "uuid-stale",
		PID:  types.PID(1),
		Steps: []ipc.StepSummaryWire{
			{Step: 99, Action: "stale", Summary: "from-old-pid"},
		},
	}
	updated, _ := m.Update(staleMsg)
	um := updated.(dashboardModel)
	if len(um.timeline.StepEntries) != 0 {
		t.Errorf("stale UUID msg should be discarded · got %d entries", len(um.timeline.StepEntries))
	}
}

// --- G4-11: PaneModel.OnTick → cmd/rnix Update case 路由端到端测试 ---

// TestIntentTreesMsg_RoutedFromInternalPackage 验证 intent.TreesMsg 命中 case intentTreesMsg。
func TestIntentTreesMsg_RoutedFromInternalPackage(t *testing.T) {
	m := dashboardModel{}
	msg := intent.TreesMsg{
		Trees: &ipc.IntentStatusResponse{
			Intents: []*ipc.IntentTreeWire{
				{RootIntent: "root-1", State: "running"},
				{RootIntent: "root-2", State: "completed"},
			},
		},
	}
	updated, _ := m.Update(msg)
	um := updated.(dashboardModel)
	if len(um.intent.Trees) != 2 {
		t.Errorf("expected 2 intent trees from internal-package msg, got %d", len(um.intent.Trees))
	}
	if um.intent.TreeErr != nil {
		t.Errorf("TreeErr should be nil, got %v", um.intent.TreeErr)
	}
}

// TestImmuneStatusMsg_RoutedFromInternalPackage 验证 security.ImmuneStatusMsg 命中 case。
func TestImmuneStatusMsg_RoutedFromInternalPackage(t *testing.T) {
	m := dashboardModel{}
	msg := security.ImmuneStatusMsg{
		Status: &ipc.ImmuneStatusResponse{
			Alerts: []ipc.AlertWire{
				{PID: 42, AgentTemplate: "test-agent"},
			},
		},
	}
	updated, _ := m.Update(msg)
	um := updated.(dashboardModel)
	if um.security.ImmuneStatus == nil {
		t.Fatal("ImmuneStatus should be set after msg routing")
	}
	if len(um.security.Alerts) != 1 {
		t.Errorf("expected 1 alert, got %d", len(um.security.Alerts))
	}
}

// TestTraceListMsg_RoutedFromInternalPackage 验证 trace.ListMsg 命中 case traceListMsg。
func TestTraceListMsg_RoutedFromInternalPackage(t *testing.T) {
	m := dashboardModel{}
	msg := trace.ListMsg{
		Summaries: []ipc.TraceSummaryWire{
			{TraceID: "trace-1", StartTimeMs: 1000},
			{TraceID: "trace-2", StartTimeMs: 2000},
		},
	}
	updated, _ := m.Update(msg)
	um := updated.(dashboardModel)
	if len(um.trace.Summaries) != 2 {
		t.Errorf("expected 2 trace summaries, got %d", len(um.trace.Summaries))
	}
	// case 内部按 StartTimeMs desc 排序，trace-2 (2000) 应在前
	if um.trace.Summaries[0].TraceID != "trace-2" {
		t.Errorf("expected sort desc by StartTimeMs · first = %q, want trace-2", um.trace.Summaries[0].TraceID)
	}
}

// TestEvalReputationMsg_RoutedFromInternalPackage 验证 eval.ReputationMsg 命中 case。
func TestEvalReputationMsg_RoutedFromInternalPackage(t *testing.T) {
	m := dashboardModel{}
	msg := eval.ReputationMsg{
		Summaries: []kernel.ReputationSummary{
			{AgentName: "agent-low", Score: 0.3},
			{AgentName: "agent-high", Score: 0.9},
		},
	}
	updated, _ := m.Update(msg)
	um := updated.(dashboardModel)
	if len(um.eval.Reputations) != 2 {
		t.Errorf("expected 2 reputations, got %d", len(um.eval.Reputations))
	}
	// case 内部按 Score desc 排序 · agent-high 应在前
	if um.eval.Reputations[0].AgentName != "agent-high" {
		t.Errorf("expected sort desc by Score · first = %q, want agent-high", um.eval.Reputations[0].AgentName)
	}
}

// TestEvalTopologyMsg_RoutedFromInternalPackage 验证 eval.TopologyMsg 命中 case。
func TestEvalTopologyMsg_RoutedFromInternalPackage(t *testing.T) {
	m := dashboardModel{}
	msg := eval.TopologyMsg{
		Topology: &ipc.TopologyQueryResponse{
			Nodes: []kernel.TopologyNode{{Agent: "a"}, {Agent: "b"}},
			Edges: []kernel.CooperationEdge{},
		},
	}
	updated, _ := m.Update(msg)
	um := updated.(dashboardModel)
	if um.eval.Topology == nil {
		t.Fatal("Topology should be set")
	}
	if len(um.eval.Topology.Nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(um.eval.Topology.Nodes))
	}
}

// TestEvalSynergyMsg_RoutedFromInternalPackage 验证 eval.SynergyMsg 命中 case。
func TestEvalSynergyMsg_RoutedFromInternalPackage(t *testing.T) {
	m := dashboardModel{}
	msg := eval.SynergyMsg{
		Combos: []kernel.ComboSummary{
			{Skills: []string{"a", "b"}, SuccessRate: 0.8},
		},
	}
	updated, _ := m.Update(msg)
	um := updated.(dashboardModel)
	if len(um.eval.Synergies) != 1 {
		t.Errorf("expected 1 synergy combo, got %d", len(um.eval.Synergies))
	}
}

// 占位变量用于消除 unused import warning（types 已被前面的测试使用）。
var _ = types.PID(0)
var _ = detail.DetailState{}
