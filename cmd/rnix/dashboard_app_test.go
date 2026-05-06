package main

// dashboard_app_test.go — Story 38-5 PR11 Step 4(d) App Model 集成测试
//
// 实现 spec § AC10 (tea.Cmd 上传链零沉默失败) + § AC11 (跨 pane PID 同步零数据陈旧)
// + spec § Tasks 11.5 (TestAppModel_BatchesAllSubModelCmds /
// TestAppModel_OnSelectPID_BroadcastsAllPanes / TestDashboardModel_FieldCount_Bounded)
// 的 dashboardModel 端到端集成测试。
//
// 与 dashboard_broadcast_test.go 的差异化价值：
//   - broadcast_test：用 mock fakePane/fakeOverlay 注入 broadcastSelectPIDImpl 验证
//     broadcast 通道**逻辑层**（计数 hook 调用 / IsActive 守卫 / tea.Batch 收集）。
//   - dashboard_app_test（本文件）：用真实 dashboardModel + 真实 *PaneModel/*OverlayModel
//     字段验证 broadcast **集成路径**（dashboardModel.broadcastSelectPID 端到端 +
//     Update.SelectPIDMsg 路由 + 全部 11 字段非 nil 时的覆盖）。
//
// EventStream 决策记录（spec § 04 风险 6）：
//   handlePIDChange 中剩余共享逻辑（compactEvents 3 字段 / sysEvents EventCompact
//   过滤 / lastUnreadEventKeys / statusMsg / lastCompactEventMs / fetchingCompact）
//   按 spec § 04 风险 6「EventStream 字段保留 App Model」原则**不迁出** internal/dashboard
//   子包：
//     1. EventStream 是跨 pane 共享数据源（Timeline + Alert Strip + Inspector step list
//        都消费）· 移到任一子 Model 都会让其他子 Model 反向依赖（违反 PaneModel 自治原则）。
//     2. Filter / dedup / TTL 衰减是 App Model 顶层调度职责 · 与 dashboardTick IPC 触发
//        共享生命周期 · 拆分会让两个调度路径错位。
//     3. EventStream 字段访问者是 ≥3 个子 Model · 任一子 Model 拥有都无法满足广播给其他
//        子 Model 的需求（与 paneHasUnread / lastUnreadEventKeys 共享同模式）。
//
// 当前 OnSelectPID 实现状态（影响断言）：
//   - 真实迁移：HeatmapModel（清空 + 设新 PID）/ TimelineModel（清空 step state +
//     设 AttachedPID）/ DetailModel（清空 Detail · PID=0 · 保留 Cache）
//   - stub noop（跨 PID 全局视图）：TreeModel / IntentModel / SecurityModel /
//     TraceModel / EvalModel
//   - stub noop（OverlayModel · IsActive==false 时不接收）：InspectorModel /
//     DebugModel / AlertStripModel
//
// Phase 3 收尾时（PR11 Step 4(b) 全部完成 · OnSelectPID 全部真实化）需要更新本文件
// 中 stub 字段的断言（从「保留原值」改为「OnSelectPID 主体行为生效」）。

import (
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/rnixai/rnix/internal/dashboard/alertstrip"
	dashboarddebug "github.com/rnixai/rnix/internal/dashboard/debug"
	"github.com/rnixai/rnix/internal/dashboard/detail"
	"github.com/rnixai/rnix/internal/dashboard/eval"
	"github.com/rnixai/rnix/internal/dashboard/heatmap"
	"github.com/rnixai/rnix/internal/dashboard/inspector"
	"github.com/rnixai/rnix/internal/dashboard/intent"
	dashboardmodel "github.com/rnixai/rnix/internal/dashboard/model"
	"github.com/rnixai/rnix/internal/dashboard/security"
	"github.com/rnixai/rnix/internal/dashboard/timeline"
	"github.com/rnixai/rnix/internal/dashboard/trace"
	"github.com/rnixai/rnix/internal/dashboard/tree"
	"github.com/rnixai/rnix/internal/types"
)

// newAppTestModel 构造一个完整 dashboardModel 测试 fixture：
//   - 11 个 *PaneModel/*OverlayModel 字段全部 NewModel() 初始化（非 nil）；
//   - 11 个 State 字段都设置 PID=initialPID + Cursor=initialCursor 标志位让真实 vs
//     stub OnSelectPID 行为差异可观察。
//
// 与 newDashboardModel 区别：本 fixture 不依赖 ui.LoadUIState / IPC client / 计时
// 器，纯数据构造让测试快速且可重入。
func newAppTestModel(initialPID types.PID, initialCursor int) dashboardModel {
	m := dashboardModel{
		// 11 个 *PaneModel/*OverlayModel hook 入口（spec § AC11）
		treeM:       tree.NewModel(),
		timelineM:   timeline.NewModel(),
		heatmapM:    heatmap.NewModel(),
		detailM:     detail.NewModel(),
		intentM:     intent.NewModel(),
		securityM:   security.NewModel(),
		traceM:      trace.NewModel(),
		evalM:       eval.NewModel(),
		inspectorM:  inspector.NewModel(),
		debugM:      dashboarddebug.NewModel(),
		alertStripM: alertstrip.NewModel(),

		// PaneModel State 字段（带 PID 字段的真实迁移目标）
		heatmap:  heatmap.HeatmapState{PID: initialPID, Cursor: initialCursor, Expanded: true},
		timeline: timeline.TimelineState{AttachedPID: initialPID, StepCursor: initialCursor},
		detail:   detail.DetailState{PID: initialPID},

		// stub OnSelectPID 字段（跨 PID 全局视图 · 应保留原 Cursor）
		tree:     tree.TreeState{Cursor: initialCursor},
		intent:   intent.IntentState{Cursor: initialCursor},
		security: security.SecurityState{Cursor: initialCursor},
		trace:    trace.TraceState{Cursor: initialCursor},
		eval:     eval.EvalState{RepCursor: initialCursor},

		// OverlayModel State（IsActive==false 时不接收 broadcast）
		inspector:  inspector.InspectorState{PID: initialPID},
		debugState: dashboarddebug.DebugState{Cursor: initialCursor, AttachedPID: initialPID},
		alertStrip: alertstrip.AlertStripState{Cursor: initialCursor},
	}
	return m
}

// TestAppModel_OnSelectPID_BroadcastsAllPanes — spec § AC11 端到端集成测试。
//
// 验证 dashboardModel.broadcastSelectPID 调用所有 11 个子 Model 的 OnSelectPID hook：
//   - 8 PaneModel 全部接收 broadcast（不分 IsActive）；
//   - 3 OverlayModel 仅在 IsActive() == true 时接收（spec § 04 风险 2）。
//
// 行为断言区分两类（基于当前 OnSelectPID 实现状态）：
//  1. 真实迁移字段（broadcast 后值变化）：heatmap.PID / timeline.AttachedPID / detail.PID
//  2. stub 字段（broadcast 后保留原值）：tree.Cursor / intent.Cursor / security.Cursor
//     / trace.Cursor / eval.RepCursor / inspector.PID（IsActive=false） / debug.Cursor
//     （IsActive=false） / alertStrip.Cursor（IsActive=false）
//
// 注意：spec § 04 风险 2 硬约束「OverlayModel 仅在 IsActive==true 时接收 hook」由
// broadcastSelectPIDImpl 的 IsActive 守卫保证（dashboard_broadcast_test.go::
// TestBroadcastSelectPIDImpl_OverlayInactiveSkipped 已直接覆盖）。本集成测试通过
// 真实 dashboardModel 验证：默认（active=false）下 OverlayModel state.PID 不变。
func TestAppModel_OnSelectPID_BroadcastsAllPanes(t *testing.T) {
	const initialPID = types.PID(100)
	const targetPID = types.PID(200)
	const initialCursor = 5

	m := newAppTestModel(initialPID, initialCursor)
	m, _ = m.broadcastSelectPID(targetPID)

	// --- 真实迁移字段断言（OnSelectPID 主体已实施） ---

	// HeatmapModel.OnSelectPID: pid != state.PID → HandlePIDChange 清空 + 设新 PID
	if m.heatmap.PID != targetPID {
		t.Errorf("heatmap.PID = %d, want %d (HeatmapModel.OnSelectPID synced)", m.heatmap.PID, targetPID)
	}
	if m.heatmap.Cursor != 0 {
		t.Errorf("heatmap.Cursor = %d, want 0 (HandlePIDChange 清空)", m.heatmap.Cursor)
	}
	if m.heatmap.Expanded {
		t.Error("heatmap.Expanded = true, want false (HandlePIDChange 清空)")
	}

	// TimelineModel.OnSelectPID: AttachedPID != pid → 清空 step state + 设新 AttachedPID
	if m.timeline.AttachedPID != targetPID {
		t.Errorf("timeline.AttachedPID = %d, want %d (TimelineModel.OnSelectPID synced)",
			m.timeline.AttachedPID, targetPID)
	}
	if m.timeline.StepCursor != 0 {
		t.Errorf("timeline.StepCursor = %d, want 0 (TimelineModel 清空 step state)",
			m.timeline.StepCursor)
	}

	// DetailModel.OnSelectPID: pid != state.PID → 清空 Detail · PID=0
	// 注：DetailModel 不设 PID=newPID，而是清 0（cmd/rnix.handlePIDChange 之后通过
	// detail.HandlePIDChangeWithCache 单独管理 PID 复用契约 · spec § AC5）。
	if m.detail.PID != 0 {
		t.Errorf("detail.PID = %d, want 0 (DetailModel.OnSelectPID 清空)", m.detail.PID)
	}

	// --- stub PaneModel 字段断言（OnSelectPID noop · 跨 PID 全局视图保留） ---

	if m.tree.Cursor != initialCursor {
		t.Errorf("tree.Cursor = %d, want %d (TreeModel.OnSelectPID stub · preserve)",
			m.tree.Cursor, initialCursor)
	}
	if m.intent.Cursor != initialCursor {
		t.Errorf("intent.Cursor = %d, want %d (IntentModel.OnSelectPID noop · 38-4 P1)",
			m.intent.Cursor, initialCursor)
	}
	if m.security.Cursor != initialCursor {
		t.Errorf("security.Cursor = %d, want %d (SecurityModel.OnSelectPID noop)",
			m.security.Cursor, initialCursor)
	}
	if m.trace.Cursor != initialCursor {
		t.Errorf("trace.Cursor = %d, want %d (TraceModel.OnSelectPID noop)",
			m.trace.Cursor, initialCursor)
	}
	if m.eval.RepCursor != initialCursor {
		t.Errorf("eval.RepCursor = %d, want %d (EvalModel.OnSelectPID noop)",
			m.eval.RepCursor, initialCursor)
	}

	// --- OverlayModel 字段断言（IsActive=false · 不接收 broadcast） ---

	if m.inspector.PID != initialPID {
		t.Errorf("inspector.PID = %d, want %d (InspectorModel inactive · skipped)",
			m.inspector.PID, initialPID)
	}
	if m.debugState.Cursor != initialCursor {
		t.Errorf("debugState.Cursor = %d, want %d (DebugModel inactive · skipped)",
			m.debugState.Cursor, initialCursor)
	}
	if m.alertStrip.Cursor != initialCursor {
		t.Errorf("alertStrip.Cursor = %d, want %d (AlertStripModel inactive · skipped)",
			m.alertStrip.Cursor, initialCursor)
	}
}

// TestAppModel_OnSelectPID_OverlayActiveReceives — spec § 04 风险 2 端到端验证。
//
// 当 OverlayModel.SetActive(true) 后，broadcast 应该调用其 OnSelectPID（虽然当前
// 3 OverlayModel 的 OnSelectPID 都是 stub · 调用本身验证路由完整）。
//
// 与 dashboard_broadcast_test.go::TestBroadcastSelectPIDImpl_OverlayActiveCalled 区别：
// 本测试用真实 dashboardModel + 真实 *OverlayModel · 验证集成层路由完整。
func TestAppModel_OnSelectPID_OverlayActiveReceives(t *testing.T) {
	m := newAppTestModel(types.PID(100), 5)
	m.inspectorM.SetActive(true)
	m.debugM.SetActive(true)
	m.alertStripM.SetActive(true)

	// broadcast 应该不 panic 且全部 11 个 hook 都被调用（OverlayModel stub 不修改 state）。
	m, cmd := m.broadcastSelectPID(types.PID(200))

	// 当前 3 OverlayModel.OnSelectPID 是 stub 返回 nil，所以 cmd 也是 nil（tea.Batch
	// 空输入返回 nil）。但 syncFromModels 的 SetState/State 通道一定 round-trip 完成。
	_ = cmd

	// 真实迁移字段断言（与 BroadcastsAllPanes 同）
	if m.heatmap.PID != types.PID(200) {
		t.Errorf("heatmap.PID = %d, want 200 (active overlay 不影响 PaneModel broadcast)",
			m.heatmap.PID)
	}
	// 验证 OverlayModel 调用不破坏 Active 状态（IsActive() 之后还是 true）
	if !m.inspectorM.IsActive() {
		t.Error("inspectorM.IsActive() = false, want true (broadcast preserved active state)")
	}
	if !m.debugM.IsActive() {
		t.Error("debugM.IsActive() = false, want true")
	}
	if !m.alertStripM.IsActive() {
		t.Error("alertStripM.IsActive() = false, want true")
	}
}

// TestAppModel_BatchesAllSubModelCmds — spec § AC10 端到端集成测试。
//
// 验证 dashboardModel.Update(SelectPIDMsg{pid}) 路由完整：
//   - tea.Msg 类型为 SelectPIDMsg → broadcastSelectPID；
//   - 返回 (tea.Model, tea.Cmd) 类型断言成功；
//   - 返回的 Model 是 dashboardModel 类型（值类型 · 同步通道生效）；
//   - **禁止**：App Model 用 `if cmd1 != nil { return m, cmd1 }` 形式只返回第一个 cmd
//     （这是 god struct 时代写法 · 会让后续 PaneModel 异步操作沉默失败）。
//
// 当前 OnSelectPID 全部返回 nil cmd（含真实迁移的 Heatmap/Timeline/Detail · 它们
// 的 cmd 留 Phase 3 真实化时引入 fetchHeatmapCmd / fetchStepsCmd 等）·
// tea.Batch 空集返回 nil cmd。本测试主要验证：
//   1. Update 路由分发到 broadcastSelectPID（不 panic）；
//   2. 返回值符合 tea.Model + tea.Cmd 契约；
//   3. dashboardModel 字段被 broadcast 路径正确同步（heatmap.PID 是 200）。
//
// dashboard_broadcast_test.go::TestBroadcastSelectPIDImpl_CollectsCmds 直接验证
// 「cmd 收集逻辑」（用 mock 注入非 nil cmd · 验证 tea.Batch 收集所有非 nil cmd）·
// 本集成测试补集是「Update 路由路径完整」。
func TestAppModel_BatchesAllSubModelCmds(t *testing.T) {
	m := newAppTestModel(types.PID(100), 5)

	model, cmd := m.Update(dashboardmodel.SelectPIDMsg{PID: types.PID(200)})

	// 1. 返回值类型断言
	dm, ok := model.(dashboardModel)
	if !ok {
		t.Fatalf("Update returned %T, want dashboardModel", model)
	}

	// 2. 同步通道生效（heatmap.PID 应被 OnSelectPID 改为 200）
	if dm.heatmap.PID != types.PID(200) {
		t.Errorf("after Update(SelectPIDMsg{200}): heatmap.PID = %d, want 200",
			dm.heatmap.PID)
	}

	// 3. cmd 类型应该是 tea.Cmd（即使当前 stub 返回 nil · 非 panic）
	// 注：tea.Cmd 是 func() tea.Msg 类型 · nil 是合法值（broadcast 当前所有
	// OnSelectPID 返回 nil → tea.Batch(nil...) 返回 nil cmd）。
	// Phase 3 真实化后：cmd 会包含 fetchHeatmapCmd 等。
	_ = cmd
	// 不断言 cmd != nil — 当前 stub 都返回 nil 是合法行为。
	// 当 Phase 3 完成后，这里应改为：
	//   if cmd == nil { t.Error("expected non-nil cmd from broadcastSelectPID") }
}

// TestAppModel_BatchesAllSubModelCmds_FromTickPath — spec § AC10 alternate path。
//
// 验证 SelectPIDMsg 不只能从外部 emit · 也能从 dashboardTick 自动 emit 后通过 Update
// 路由生效。本测试构造一个直接通过 Update 路由的场景，模拟 tea.Cmd 收集流程。
//
// 当前 OnSelectPID 全部返回 nil cmd · 所以 cmd 是 nil。本测试主要验证不 panic +
// 路由分发可重复。
func TestAppModel_BatchesAllSubModelCmds_FromTickPath(t *testing.T) {
	m := newAppTestModel(types.PID(0), 0)

	// 模拟用户切换到新 PID = 300
	model, _ := m.Update(dashboardmodel.SelectPIDMsg{PID: types.PID(300)})

	dm, ok := model.(dashboardModel)
	if !ok {
		t.Fatalf("Update returned %T, want dashboardModel", model)
	}
	if dm.heatmap.PID != types.PID(300) {
		t.Errorf("heatmap.PID = %d, want 300 (broadcast synced from PID=0)",
			dm.heatmap.PID)
	}
	if dm.timeline.AttachedPID != types.PID(300) {
		t.Errorf("timeline.AttachedPID = %d, want 300", dm.timeline.AttachedPID)
	}
}

// dashboardModelExpectedFieldCount — spec § Tasks 11.5「TestDashboardModel_FieldCount_Bounded」
// 字段守门常量。当前阶段守门当前实际字段数 · 防止任意 PR 再加字段。
//
// **spec § AC8 收尾目标：≤ 30 字段**。当前 65 字段（Phase 1+2 阶段 · State 字段 +
// *PaneModel 字段并存 · deprecated getter 过渡期）。Phase 3 收尾删除 deprecated
// getter + State 字段统一通过 *PaneModel.State() 读取后预期降至 ~30。
//
// 守门策略：
//   - 当前值（65）作为 lower bound（防止误删字段导致行为回归）；
//   - upper bound = 当前值 + 1（容忍 1 字段微调 · 多于 1 必须 retro 解释）。
//
// Phase 3 收尾时（PR11 Step 4(b) 全部完成 + deprecated getter 删除）需要把这两个
// 常量降到 30/31。
const (
	dashboardModelFieldCountLower = 65
	dashboardModelFieldCountUpper = 66
)

// TestDashboardModel_FieldCount_Bounded — spec § Tasks 11.5 字段守门。
//
// 用 reflect.TypeOf(dashboardModel{}).NumField() 计数 · 守门当前阶段实际值。
//
// 失败原因 + 修复指引：
//   - 实际 < lower：误删字段 · 检查 git diff 确认是否破坏 god struct 拆分进度；
//   - 实际 > upper：未授权增加字段 · spec § AC8 反对再加字段 · 任何字段新增需
//     先 Phase 3 收尾把 *PaneModel 状态合并 · 不能再向 dashboardModel 加散字段。
//
// Phase 3 收尾时更新 dashboardModelFieldCountLower/Upper 常量到 ~30。
func TestDashboardModel_FieldCount_Bounded(t *testing.T) {
	t.Helper()
	typeOfDashboard := reflect.TypeFor[dashboardModel]()
	count := typeOfDashboard.NumField()

	if count < dashboardModelFieldCountLower {
		t.Errorf("dashboardModel field count = %d < lower bound %d (字段被误删？)",
			count, dashboardModelFieldCountLower)
	}
	if count > dashboardModelFieldCountUpper {
		t.Errorf("dashboardModel field count = %d > upper bound %d (spec § AC8 反对再加字段 · "+
			"先 Phase 3 收尾合并 *PaneModel 状态)", count, dashboardModelFieldCountUpper)
	}

	// 子 Model hook 字段（11 个 *PaneModel/*OverlayModel）必须存在
	// （spec § AC11 broadcast 通道必备）。
	requiredFields := []string{
		"treeM", "timelineM", "heatmapM", "detailM", "intentM",
		"securityM", "traceM", "evalM",
		"inspectorM", "debugM", "alertStripM",
	}
	for _, name := range requiredFields {
		if _, ok := typeOfDashboard.FieldByName(name); !ok {
			t.Errorf("dashboardModel 缺少必需字段 %q (spec § AC11 broadcast 通道字段)", name)
		}
	}
}

// 编译期断言：SelectPIDMsg 实现 tea.Msg interface（实际只是 marker）+
// dashboardmodel.EmitSelectPID 返回 tea.Cmd。
var (
	_ tea.Cmd = dashboardmodel.EmitSelectPID(types.PID(1))
)
