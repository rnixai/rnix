// Package intent — model.go (Story 38-5 PR6 Step 3)
//
// IntentModel 是 internal/dashboard/intent 包的 PaneModel 实现，承载 IntentState +
// 实现 internal/dashboard/model.PaneModel 5 方法（Init/Update/View/Name/KeyLayer/
// OnSelectPID/OnTick）。
//
// 当前阶段（38-5 PR6 Step 3）的 View() 实现说明（与 PR2 TreeModel / PR3 HeatmapModel /
// PR4 TimelineModel / PR5 DetailModel 同模式）：
//
//	tea.Model.View() 标准签名为 View() tea.View，无法直接传入 cmd/rnix 端运行时数据
//	（如 m.activePane / m.selectedPID 等）。因此 IntentModel.View() 在本阶段返回空 view，
//	cmd/rnix 端继续通过 renderIntentPane wrapper 完成实际渲染。
//
//	PR11（App Model 瘦身）阶段：cmd/rnix 端会通过 SetRenderContext 注入运行时数据，
//	View() 自洽。
//
// 38-4 行为契约保留（关键）：
//   - OnSelectPID 在 PID 切换时**不**清空 TreeCollapsed 或 FlatNodes（38-4 P1 stable
//     RootIntent key 跨进程切换稳定 · 与 cmd/rnix 端 handlePIDChange 现有行为对齐）；
//   - OnTick 不直接触发 IPC（IPC 通过 cmd/rnix 端 dashboardTick 的 5 秒节流刷新触发）。
package intent

import (
	tea "charm.land/bubbletea/v2"

	dashboardmodel "github.com/rnixai/rnix/internal/dashboard/model"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
)

// 编译期断言：IntentModel 满足 model.PaneModel interface。
var _ dashboardmodel.PaneModel = (*IntentModel)(nil)

// 编译期断言：*IntentModel 通过指针接收者实现 StateProvider interface（用于 KeyLayer 注入）。
var _ StateProvider = (*IntentModel)(nil)

// IntentModel 实现 internal/dashboard/model.PaneModel interface。
//
// 字段：
//   - state:    IntentState 包装（6 个 intent 相关字段，由 PR6 Step 1 抽出）
//   - width:    pane 渲染宽度（由 Bubble Tea runtime 通过 WindowSizeMsg 更新）
//   - height:   pane 渲染高度
//   - active:   当前是否为 activePane（影响 border 颜色 + 头部高亮）
//
// nil 安全：所有方法在 receiver 为 nil 时返回零值（不 panic）。
type IntentModel struct {
	state  IntentState
	width  int
	height int
	active bool
}

// NewModel 构造一个新的 IntentModel，初始 state 为零值。
//
// 调用方可在构造后立即调 SetState 注入已有数据（cmd/rnix 端集成时使用）。
func NewModel() *IntentModel {
	return &IntentModel{}
}

// State 返回当前 IntentState 值快照（PR11 重构集成点）。
//
//nolint:unused // 保留供 PR6-PR11 的 cmd/rnix 集成代码调用
func (m *IntentModel) State() IntentState {
	if m == nil {
		return IntentState{}
	}
	return m.state
}

// SetState 替换内部 IntentState（PR6 Step 3 过渡 API）。
//
//nolint:unused // 保留供 PR6-PR11 的 cmd/rnix 集成代码调用
func (m *IntentModel) SetState(s IntentState) {
	if m == nil {
		return
	}
	m.state = s
}

// SetActive 标记当前 pane 是否激活（影响 border 颜色 + 头部 cursor 高亮）。
//
//nolint:unused // 保留供 PR6-PR11 的 cmd/rnix 集成代码调用
func (m *IntentModel) SetActive(active bool) {
	if m == nil {
		return
	}
	m.active = active
}

// IntentState 满足 StateProvider interface（让 keys.go 中的 KeyLayer ActiveModesFn cast）。
//
// 等价于 State()，但接口方法名固定为 IntentState 以匹配 cmd/rnix 端 dashboardModel
// 在 PR6 Step 1 落地的同名 deprecated getter。
func (m *IntentModel) IntentState() IntentState {
	return m.State()
}

// --- tea.Model implementation ---

// Init 满足 tea.Model interface。
//
// 当前阶段不返回 cmd（IPC 触发由 cmd/rnix 端 dashboardTick 的 fetchIntentTreesCmd 负责）。
func (m *IntentModel) Init() tea.Cmd {
	return nil
}

// Update 满足 tea.Model interface。当前阶段仅处理 WindowSizeMsg。
//
// 完整 intentTreesMsg / 节点 PID drill-in / collapse toggle 处理仍在
// cmd/rnix 端 Update + dispatchPaneKey 内（PR11 迁回）。
func (m *IntentModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m == nil {
		return m, nil
	}
	if sz, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = sz.Width
		m.height = sz.Height
	}
	return m, nil
}

// View 满足 tea.Model interface。
//
// **当前阶段返回空 view**：cmd/rnix 端 renderIntentPane wrapper 完成实际渲染。
// PR11 阶段重构后 IntentModel 自持依赖，View() 直接调用 Render 自洽。
func (m *IntentModel) View() tea.View {
	return tea.NewView("")
}

// --- model.PaneModel-specific methods ---

// Name 返回稳定 pane 名（用于 help overlay 分组 + logging + telemetry）。
func (m *IntentModel) Name() string {
	return "Intent"
}

// KeyLayer 返回 Intent pane 的 Layer 2 KeyLayer 注册体。
//
// 调用 intent.KeyLayer(nil) — fallback 由 cmd/rnix 端注入到 dispatcher。
func (m *IntentModel) KeyLayer() *ui.KeyLayer {
	return KeyLayer(nil)
}

// OnSelectPID 当 App Model 收到 selectPIDMsg 时调用。
//
// 当前阶段（PR6 Step 3）：
//   - **不**清空 Trees / FlatNodes / TreeCollapsed（与 cmd/rnix 端 handlePIDChange 现有行为对齐 ·
//     IntentTree 是跨进程的全局视图，PID 切换时数据保持，仅 cursor 可能需要重新定位）；
//   - **不**重置 Cursor（避免用户在快速切 PID 时丢失浏览位置）。
//
// PR11 阶段：完整逻辑（如根据新 PID 自动定位 cursor 到对应节点）保留在 cmd/rnix 端的可能性较高，
// 因为 IntentTree 节点的 PID 字段映射依赖共享 processes 数据。
//
// 38-4 P1 契约保留：TreeCollapsed 跨 PID 切换稳定（用户折叠状态不丢失）。
func (m *IntentModel) OnSelectPID(_ types.PID) tea.Cmd {
	if m == nil {
		return nil
	}
	// noop · PID 切换不影响 IntentState（38-4 P1 行为）
	return nil
}

// OnTick 在 dashboardTick 周期内调用。
//
// Story 38-5 PR11 Step 4(b) Phase 3 真实化：
//   - 仅在 ctx.Active（Intent pane 是当前激活 pane） + ctx.Connected + ctx.SocketPath
//     非空时考虑 fetch；
//   - 节流条件：m.state.Trees == nil 或 ctx.TickCount%5 == 0（首次拉取或 5 秒一次）；
//   - 全部满足时返回 FetchTreesCmd · 否则返回 nil cmd.
//
// 行为契约 1:1 等价（与 cmd/rnix dashboardTick line 880-885 完全等价）：
//
//	if m.activePane == paneIntent && m.connected {
//	    if m.intent.Trees == nil || m.heatmap.TickCount%5 == 0 {
//	        cmds = append(cmds, fetchIntentTreesCmd())
//	    }
//	}
//
// nil 安全：receiver 为 nil 或 ctx.SocketPath 为空时返回 nil cmd。
func (m *IntentModel) OnTick(ctx dashboardmodel.OnTickContext) tea.Cmd {
	if m == nil {
		return nil
	}
	if !ctx.Active || !ctx.Connected || ctx.SocketPath == "" {
		return nil
	}
	if m.state.Trees != nil && ctx.TickCount%5 != 0 {
		return nil
	}
	return FetchTreesCmd(ctx.SocketPath)
}
