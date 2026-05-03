// Package tree — model.go (Story 38-5 PR2 Step 3c)
//
// TreeModel 是 internal/dashboard/tree 包的 PaneModel 实现，承载 TreeState
// + 实现 internal/dashboard/model.PaneModel 5 方法（Init/Update/View/Name/
// KeyLayer/OnSelectPID/OnTick）。
//
// 当前阶段（38-5 PR2 Step 3c）的 View() 实现说明：
//
//	tea.Model.View() 标准签名为 View() string，无法直接传入 RenderContext
//	（包含 ReusedPIDs / Recording / CollapsedIntents 等 cmd/rnix 端运行时数据）。
//	因此 TreeModel.View() 在本阶段返回空字符串，cmd/rnix 端继续通过
//	renderDashboardTreePane wrapper 调用 tree.Render（与 PR2 Step 3b 落地一致）。
//
//	PR11（App Model 瘦身）阶段：cmd/rnix 端会直接将 RenderContext 通过 OnTick
//	提前注入到 TreeModel 内部状态（或通过新增 SetRenderContext 方法），使
//	View() 自洽不再依赖 cmd/rnix 端 wrapper。本阶段保留过渡设计。
package tree

import (
	"time"

	tea "charm.land/bubbletea/v2"

	dashboardmodel "github.com/rnixai/rnix/internal/dashboard/model"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
)

// 编译期断言：TreeModel 满足 model.PaneModel interface。
//
// 该断言在编译时验证 TreeModel 实现了 PaneModel 的全部 5 个方法（tea.Model 嵌入 +
// Name/KeyLayer/OnSelectPID/OnTick）；签名不一致时编译期立即失败，避免运行时反射开销。
var _ dashboardmodel.PaneModel = (*TreeModel)(nil)

// TreeModel 实现 internal/dashboard/model.PaneModel interface。
//
// 字段：
//   - state:    TreeState 包装（13 个 tree 相关字段，由 Story 38-5 PR2 Step 1 抽出）
//   - width:    pane 渲染宽度（由 Bubble Tea runtime 通过 WindowSizeMsg 更新）
//   - height:   pane 渲染高度
//   - active:   当前是否为 activePane（影响 border 颜色，cmd/rnix 端通过 SetActive 同步）
//
// nil 安全：所有方法在 receiver 为 nil 时返回零值（不 panic）。
//
// 当前阶段约束：
//   - View() 返回空字符串（实际渲染由 cmd/rnix 端 renderDashboardTreePane wrapper 调用 Render(state, ctx, w, h)）；
//   - Update 仅处理 WindowSizeMsg（同步 width/height）；其他消息由 cmd/rnix 端 dashboardModel 处理；
//   - OnSelectPID/OnTick 当前为占位（由 cmd/rnix 端继续调用 fetchProcessesCmd 等）；
//   - PR11 阶段会重构为完整 self-contained Model（不再依赖 cmd/rnix wrapper）。
type TreeModel struct {
	state  TreeState
	width  int
	height int
	active bool
}

// NewModel 构造一个新的 TreeModel，初始 state 为零值。
//
// 调用方应通过 SetState(s TreeState) 同步 cmd/rnix 端 dashboardModel.tree 状态
// （PR2 Step 3c 阶段的过渡 API；PR11 阶段会改为构造时直接持有所有依赖）。
func NewModel() *TreeModel {
	return &TreeModel{}
}

// State 返回当前 TreeState 值快照。
//
// 用于：
//   - tree.StateProvider interface 实现（让 KeyLayer.ActiveModesFn 读取最新状态）；
//   - cmd/rnix 端读取 TreeModel 的 internal state（过渡期 deprecated getter 替代）。
func (m *TreeModel) State() TreeState {
	if m == nil {
		return TreeState{}
	}
	return m.state
}

// SetState 替换内部 TreeState（PR2 Step 3c 过渡 API）。
//
// cmd/rnix 端 dashboardModel 用此方法把抽出的 m.tree 字段同步到 TreeModel 内部。
// PR11 阶段废弃此方法（TreeModel 持有所有依赖后状态自洽）。
//
//nolint:unused // 保留供 PR3-PR11 的 cmd/rnix 集成代码调用
func (m *TreeModel) SetState(s TreeState) {
	if m == nil {
		return
	}
	m.state = s
}

// SetActive 标记当前 pane 是否激活（影响 border 颜色）。
//
//nolint:unused // 保留供 PR3-PR11 的 cmd/rnix 集成代码调用
func (m *TreeModel) SetActive(active bool) {
	if m == nil {
		return
	}
	m.active = active
}

// TreeState 满足 StateProvider interface（由 keys.go 的 KeyLayer.ActiveModesFn 调用）。
//
// 等价于 State()，但接口方法名固定为 TreeState 以匹配 cmd/rnix 端 dashboardModel
// 在 PR2 Step 1 落地的同名 deprecated getter。
func (m *TreeModel) TreeState() TreeState {
	return m.State()
}

// --- tea.Model implementation ---

// Init 满足 tea.Model interface。当前阶段无初始化命令（cmd/rnix 端 dashboardModel
// 通过 dashboardTick 触发首次拉取）。
func (m *TreeModel) Init() tea.Cmd {
	return nil
}

// Update 满足 tea.Model interface。当前阶段仅处理 WindowSizeMsg 更新尺寸；
// 其他消息（KeyPressMsg / IPC 响应等）由 cmd/rnix 端 dashboardModel 处理。
//
// PR11 阶段重构为完整消息路由（接收 selectPIDMsg / 进程列表更新等）。
func (m *TreeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
// **当前阶段返回空 view**：tea.Model.View() 标准签名无法接受 RenderContext，
// 实际渲染由 cmd/rnix 端 renderDashboardTreePane wrapper 调用 Render(state, ctx, w, h)。
// PR11 阶段重构后 TreeModel 自持依赖，View() 直接调用 Render 自洽。
//
// 设计注解（spec § Dev Notes #2）：保留此空 View 是为了满足 PaneModel 接口契约
// （tea.Model 嵌入要求 View() tea.View），同时让 TreeModel 可以被 model.PaneModel
// 类型集合统一管理。
func (m *TreeModel) View() tea.View {
	return tea.NewView("")
}

// --- model.PaneModel-specific methods ---

// Name 返回稳定 pane 名（用于 help overlay 分组 + logging + telemetry）。
func (m *TreeModel) Name() string {
	return "Tree"
}

// KeyLayer 返回 Tree pane 的 Layer 2 KeyLayer 注册体。
//
// 调用 tree.KeyLayer(nil) — fallback 由 cmd/rnix 端在 newDispatcher() 注册时通过
// d.Layer2[paneTree].Fallback = paneFallback 单独注入（避免 model 包反向引用 cmd/rnix）。
//
// **注意**：本方法每次调用返回新的 KeyLayer 实例（非 cached）。cmd/rnix 端应在
// dispatcher 注册时调用一次并存进 d.Layer2[paneTree]，避免重复创建（PR11 重构）。
func (m *TreeModel) KeyLayer() *ui.KeyLayer {
	return KeyLayer(nil)
}

// OnSelectPID 当 App Model 收到 selectPIDMsg 时调用，子 Model 同步 cursor 到该 PID 行。
//
// 当前阶段仅是 stub（cmd/rnix 端 dashboardModel.handlePIDChange 继续处理 cursor 同步）。
// PR11 阶段会迁移为：扫描 m.state.Rows 找到 PID 对应行 → 更新 m.state.Cursor。
//
// 返回 nil cmd（无 IPC 副作用）。
func (m *TreeModel) OnSelectPID(_ types.PID) tea.Cmd {
	return nil
}

// OnTick 在 dashboardTick 周期内调用，触发 IPC 拉取或缓存刷新。
//
// 当前阶段仅是 stub（cmd/rnix 端 dashboardTick 继续触发 fetchProcessesCmd）。
// PR11 阶段会迁移为：返回 tea.Batch(fetchProcessesCmd, refreshTreeStateCmd)。
//
// 返回 nil cmd（无 IPC 副作用）。
func (m *TreeModel) OnTick(_ time.Time) tea.Cmd {
	return nil
}
