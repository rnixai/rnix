// Package detail — model.go (Story 38-5 PR5 Step 3)
//
// DetailModel 是 internal/dashboard/detail 包的 PaneModel 实现，承载 DetailState +
// 实现 internal/dashboard/model.PaneModel 5 方法（Init/Update/View/Name/KeyLayer/
// OnSelectPID/OnTick）。
//
// 当前阶段（38-5 PR5 Step 3）的 View() 实现说明：
//
//	tea.Model.View() 标准签名为 View() tea.View，无法直接传入 cmd/rnix 端运行时数据
//	（如 m.activePane / m.selectedPID / m.processes / m.sysEvents 等）。因此
//	DetailModel.View() 在本阶段返回空 view，cmd/rnix 端继续通过 renderDetailPane wrapper
//	完成实际渲染（与 PR2 Step 3 TreeModel + PR3 Step 3 HeatmapModel + PR4 Step 3
//	TimelineModel 模式一致）。
//
//	PR11（App Model 瘦身）阶段：cmd/rnix 端会直接将运行时数据通过 SetRenderContext 方法
//	注入到 DetailModel 内部状态（或新增 OnEvents/OnProcesses hook），使 View() 自洽。
package detail

import (
	tea "charm.land/bubbletea/v2"

	dashboardmodel "github.com/rnixai/rnix/internal/dashboard/model"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
)

// ViewDefault mirrors cmd/rnix.viewDefault (iota 0) to avoid detail → status import.
// Used by OnTick to determine if detail card needs data in default view.
const ViewDefault = 0

// 编译期断言：DetailModel 满足 model.PaneModel interface。
var _ dashboardmodel.PaneModel = (*DetailModel)(nil)

// 编译期断言：*DetailModel 通过指针接收者实现 StateProvider interface（用于 KeyLayer 注入）。
var _ StateProvider = (*DetailModel)(nil)

// DetailModel 实现 internal/dashboard/model.PaneModel interface。
//
// 字段：
//   - state:    DetailState 包装（4 个 detail 相关字段，由 PR5 Step 1 抽出）
//   - width:    pane 渲染宽度（由 Bubble Tea runtime 通过 WindowSizeMsg 更新）
//   - height:   pane 渲染高度
//   - active:   当前是否为 activePane（影响 border 颜色）
//
// nil 安全：所有方法在 receiver 为 nil 时返回零值（不 panic）。
type DetailModel struct {
	state  DetailState
	width  int
	height int
	active bool
}

// NewModel 构造一个新的 DetailModel，初始 state 为零值。
func NewModel() *DetailModel {
	return &DetailModel{}
}

// State 返回当前 DetailState 值快照（满足 StateProvider interface）。
func (m *DetailModel) State() DetailState {
	if m == nil {
		return DetailState{}
	}
	return m.state
}

// SetState 替换内部 DetailState（PR5 Step 3 过渡 API）。
//
//nolint:unused // 保留供 PR5-PR11 的 cmd/rnix 集成代码调用
func (m *DetailModel) SetState(s DetailState) {
	if m == nil {
		return
	}
	m.state = s
}

// SetActive 标记当前 pane 是否激活（影响 border 颜色）。
//
//nolint:unused // 保留供 PR5-PR11 的 cmd/rnix 集成代码调用
func (m *DetailModel) SetActive(active bool) {
	if m == nil {
		return
	}
	m.active = active
}

// DetailState 满足 StateProvider interface（由 keys.go 中的 KeyLayer 扩展用 / PR11 完整接入）。
//
// 等价于 State()，但接口方法名固定为 DetailState 以匹配 cmd/rnix 端 dashboardModel
// 在 PR5 Step 1 落地的同名 deprecated getter。
func (m *DetailModel) DetailState() DetailState {
	return m.State()
}

// --- tea.Model implementation ---

// Init 满足 tea.Model interface。
func (m *DetailModel) Init() tea.Cmd {
	return nil
}

// Update 满足 tea.Model interface。当前阶段仅处理 WindowSizeMsg。
func (m *DetailModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
// **当前阶段返回空 view**：cmd/rnix 端 renderDetailPane wrapper 完成实际渲染。
// PR11 阶段重构后 DetailModel 自持依赖，View() 直接调用 Render 自洽。
func (m *DetailModel) View() tea.View {
	return tea.NewView("")
}

// --- model.PaneModel-specific methods ---

// Name 返回稳定 pane 名（用于 help overlay 分组 + logging + telemetry）。
func (m *DetailModel) Name() string {
	return "Detail"
}

// KeyLayer 返回 Detail pane 的 Layer 2 KeyLayer 注册体。
//
// 调用 detail.KeyLayer(nil) — fallback 由 cmd/rnix 端注入。
func (m *DetailModel) KeyLayer() *ui.KeyLayer {
	return KeyLayer(nil)
}

// OnSelectPID 当 App Model 收到 selectPIDMsg 时调用。
//
// 当前阶段（PR5 Step 3）：
//   - 若 PID 切换：清空 Detail（设为 nil · cmd/rnix 端 handlePIDChange 已等价处理）
//     并优先从 Cache 加载（28-4 AC-4 PID 复用 UUID-keyed 缓存契约保留）；
//   - 若 PID 不变：保持现有 state（调用方已处理重复点击 selectedPID 的边界）。
//
// PR11 阶段会迁移完整 cmd 触发到这里（fetchProcDetailCmd）。
func (m *DetailModel) OnSelectPID(pid types.PID) tea.Cmd {
	if m == nil {
		return nil
	}
	if pid == m.state.PID {
		return nil
	}
	// PID 切换：清空当前 Detail 但保留 Cache（PID 复用 UUID-keyed 安全契约）
	m.state.Detail = nil
	m.state.PID = 0
	return nil
}

// OnTick 在 dashboardTick 周期内调用。
//
// Story 38-5 PR11 Step 4(b) Phase 3 真实化：
//   - 仅在 Active（paneDetail 激活）或 ViewMode==ViewDefault（detail card 需要数据）
//     且 Connected + SelectedPID > 0 时执行 fetch 逻辑；
//   - 自增 Tick 计数（节流计数器）；
//   - needsFetch 判定：Detail==nil 或 PID!=SelectedPID；
//   - 每 5 ticks 对非 dead 进程刷新（删除 Cache 条目 + 设 needsFetch）；
//   - Cache 命中时直接设 Detail+PID（不发 IPC）；
//   - Cache 未命中时返回 FetchDetailCmd。
//
// 行为契约 1:1 等价（与 cmd/rnix dashboardTick detail section 完全等价 · spec § AC8）。
func (m *DetailModel) OnTick(ctx dashboardmodel.OnTickContext) tea.Cmd {
	if m == nil {
		return nil
	}
	// Guard: only fetch when detail pane active or default view needs detail card data
	if !ctx.Active && ctx.ViewMode != ViewDefault {
		return nil
	}
	if ctx.SelectedPID <= 0 || !ctx.Connected {
		return nil
	}

	m.state.Tick++

	needsFetch := m.state.Detail == nil || m.state.PID != ctx.SelectedPID
	// Refresh every 5 ticks (~5s) for live processes
	if !needsFetch && m.state.Detail != nil && m.state.Detail.State != "dead" && m.state.Tick%5 == 0 {
		delete(m.state.Cache, ctx.SelectedUUID)
		needsFetch = true
	}
	if !needsFetch {
		return nil
	}
	// Check cache first
	if cached, ok := m.state.Cache[ctx.SelectedUUID]; ok {
		m.state.Detail = cached
		m.state.PID = ctx.SelectedPID
		return nil
	}
	return FetchDetailCmd(ctx.SocketPath, ctx.SelectedPID, ctx.SelectedUUID)
}
