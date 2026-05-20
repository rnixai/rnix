// Package timeline — model.go (Story 38-5 PR4 Step 3)
//
// TimelineModel 实现 internal/dashboard/model.PaneModel + plugin.Searchable 接口
// （Searchable.SearchableLines() 在 PR4 Step 4 落地）。
//
// 当前阶段（38-5 PR4 Step 3）的 View() 实现说明：
//
//	tea.Model.View() 标准签名为 View() string，无法直接传入 RenderContext
//	（含 unifiedEvents/processes 等 cmd/rnix 端运行时数据）。因此 TimelineModel.View() 在本阶段
//	返回空 view，cmd/rnix 端继续通过 renderTimelinePane wrapper 调用现有渲染逻辑
//	（与 PR2 Step 3 TreeModel + PR3 Step 3 HeatmapModel 模式一致）。
//
//	PR11（App Model 瘦身）阶段：cmd/rnix 端会直接将 RenderContext 通过 OnTick 提前
//	注入到 TimelineModel 内部状态（或通过新增 SetRenderContext 方法），使 View() 自洽。
//
// 当前阶段 OnSelectPID/OnTick 的实现说明：
//
//	OnSelectPID 仅在 PID 切换时清空内部 step state（不发 IPC · IPC 仍由 cmd/rnix 端
//	dashboardTick + handleTimelinePIDChange 触发，与 PR2/PR3 同模式）。
//	OnTick 仅维护轻量计数（不发 IPC · 实际 step list 拉取仍由 dashboardTick）。
//
// PR4 Step 4 之后 TimelineModel 会实现 plugin.Searchable interface，让 SearchPlugin
// 通过 SearchableLines() 接入 Inspector + Timeline 跨 pane 搜索（spec § 04 风险 3 缓解）。
package timeline

import (
	tea "charm.land/bubbletea/v2"

	dashboardmodel "github.com/rnixai/rnix/internal/dashboard/model"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
)

// 编译期断言：TimelineModel 满足 model.PaneModel interface。
var _ dashboardmodel.PaneModel = (*TimelineModel)(nil)

// TimelineModel 实现 internal/dashboard/model.PaneModel interface。
//
// 字段：
//   - state:    TimelineState 包装（16 个 timeline 相关字段，由 PR4 Step 1 抽出）
//   - width:    pane 渲染宽度（由 Bubble Tea runtime 通过 WindowSizeMsg 更新）
//   - height:   pane 渲染高度
//   - active:   当前是否为 activePane（影响 border 颜色）
//
// nil 安全：所有方法在 receiver 为 nil 时返回零值（不 panic）。
type TimelineModel struct {
	state  TimelineState
	width  int
	height int
	active bool
}

// NewModel 构造一个新的 TimelineModel，初始 state 为零值。
func NewModel() *TimelineModel {
	return &TimelineModel{}
}

// State 返回当前 TimelineState 值快照。
func (m *TimelineModel) State() TimelineState {
	if m == nil {
		return TimelineState{}
	}
	return m.state
}

// SetState 替换内部 state（用于 cmd/rnix 端 dashboardTick 同步状态）。
func (m *TimelineModel) SetState(s TimelineState) {
	if m == nil {
		return
	}
	m.state = s
}

// TimelineState 实现 timeline.StateProvider interface（供 KeyLayer ActiveModesFn 读取）。
//
// 与 dashboardModel.TimelineState() 同名（值快照拷贝），让两者满足同一契约。
func (m *TimelineModel) TimelineState() TimelineState {
	if m == nil {
		return TimelineState{}
	}
	return m.state
}

// SetActive 标记当前 pane 为 active（影响 border 颜色）。
func (m *TimelineModel) SetActive(active bool) {
	if m == nil {
		return
	}
	m.active = active
}

// --- tea.Model interface ---

// Init 是 Bubble Tea v2 标准接口。Timeline pane 启动时不发 IPC（IPC 由 dashboardTick 触发）。
func (m *TimelineModel) Init() tea.Cmd {
	return nil
}

// Update 处理 WindowSizeMsg 同步 width/height。
//
// 当前阶段（38-5 PR4 Step 3）实现：
//   - WindowSizeMsg: 同步 width/height（Bubble Tea runtime 协议）
//   - 其他 Msg: nil（实际 keypress 由 cmd/rnix 端 handleTimelineKey 处理）
func (m *TimelineModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m == nil {
		return m, nil
	}
	if sz, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = sz.Width
		m.height = sz.Height
	}
	return m, nil
}

// View 当前阶段返回空字符串（实际渲染由 cmd/rnix 端 wrapper 完成 · PR11 重构）。
//
// 与 TreeModel/HeatmapModel 同模式：tea.Model.View() 签名无法接收 RenderContext，
// PR11 通过 SetRenderContext + OnTick 注入运行时数据后再让本方法自洽。
func (m *TimelineModel) View() tea.View {
	if m == nil {
		return tea.View{}
	}
	return tea.View{}
}

// --- PaneModel-only methods ---

// Name 返回 pane 显示名称（用于 KeyLayer.Name + help overlay）。
func (m *TimelineModel) Name() string {
	return "Timeline"
}

// KeyLayer 返回当前 pane 的 Layer 2 KeyLayer（PR4 Step 2 落地）。
//
// 当前阶段返回 KeyLayer(nil)，cmd/rnix 端 newDispatcher 会传入实际 paneFallback。
// PR11 重构时 cmd/rnix 端会直接调用 m.timeline.KeyLayer(paneFallback)，本方法逐步成为唯一入口。
func (m *TimelineModel) KeyLayer() *ui.KeyLayer {
	return KeyLayer(nil)
}

// OnSelectPID 在 PID 切换时清空 step state（与 dashboardModel.handleTimelinePIDChange 同模式）。
//
// 当前阶段实现：
//   - 同 PID: noop（避免不必要的 state 清空）
//   - 异 PID: 清空 StepEntries / StepCursor / StepScrollTop / StepDetailCache /
//     LastFetchedStep / FetchingDetail / StepExpandedIdx / ExpandedAggGroups
//   - 不发 IPC（实际 step list 拉取由 cmd/rnix 端 dashboardTick 在 selectedPID 变化后触发）
//
// nil 安全：receiver 为 nil 时返回 nil cmd。
func (m *TimelineModel) OnSelectPID(pid types.PID) tea.Cmd {
	if m == nil {
		return nil
	}
	if m.state.AttachedPID == pid {
		return nil
	}
	// 清空 step state（与 cmd/rnix.handleTimelinePIDChange 行为对齐）
	m.state.AttachedPID = pid
	m.state.StepEntries = nil
	m.state.StepCursor = 0
	m.state.StepScrollTop = 0
	m.state.StepDetailCache = make(map[int]*ipc.GetStepDetailResponse)
	m.state.LastFetchedStep = 0
	m.state.FetchingDetail = false
	m.state.StepExpandedIdx = -1
	m.state.ExpandedAggGroups = make(map[int]bool)
	m.state.ExpandedScriptAggGroups = make(map[int]bool) // Story 43-3 review patch P11
	// ExpandMode 切 PID 时重置为 collapsed（Story 36-4 行为）
	m.state.ExpandMode = ExpandModeCollapsed
	return nil
}

// OnTick 在 dashboardTick 周期内调用。
//
// Story 38-5 PR11 Step 4(b) Phase 3 真实化：
//   - 每 tick 在 Connected + SelectedPID > 0 时触发 FetchStepsCmd；
//   - afterStep = state.LastFetchedStep（增量拉取 · 仅获取新 step）；
//   - 不节流（每 tick 都 fetch · 与 cmd/rnix dashboardTick line 855-856 行为等价）；
//   - 调用方保证仅在 !pidChanged 时调用（pidChanged 时 handlePIDChange 已触发
//     afterStep=0 的全量 fetch · 不重复）。
//
// nil 安全：receiver 为 nil 时返回 nil cmd。
func (m *TimelineModel) OnTick(ctx dashboardmodel.OnTickContext) tea.Cmd {
	if m == nil {
		return nil
	}
	if !ctx.Connected || ctx.SelectedPID <= 0 {
		return nil
	}
	return FetchStepsCmd(ctx.SocketPath, ctx.SelectedUUID, ctx.SelectedPID, m.state.LastFetchedStep)
}
