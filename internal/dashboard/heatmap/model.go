// Package heatmap — model.go (Story 38-5 PR3 Step 3c)
//
// HeatmapModel 是 internal/dashboard/heatmap 包的 PaneModel 实现，承载 HeatmapState
// + 实现 internal/dashboard/model.PaneModel 5 方法（Init/Update/View/Name/KeyLayer/
// OnSelectPID/OnTick）。
//
// 当前阶段（38-5 PR3 Step 3c）的 View() 实现说明：
//
//	tea.Model.View() 标准签名为 View() string，无法直接传入 RenderContext
//	（含 SelectedPID 等 cmd/rnix 端运行时数据）。因此 HeatmapModel.View() 在本阶段
//	返回空 view，cmd/rnix 端继续通过 renderHeatmapPane wrapper 调用 heatmap.Render
//	（与 PR2 Step 3b TreeModel 模式一致）。
//
//	PR11（App Model 瘦身）阶段：cmd/rnix 端会直接将 RenderContext 通过 OnTick 提前
//	注入到 HeatmapModel 内部状态（或通过新增 SetRenderContext 方法），使 View() 自洽。
package heatmap

import (
	tea "charm.land/bubbletea/v2"

	dashboardmodel "github.com/rnixai/rnix/internal/dashboard/model"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
)

// 编译期断言：HeatmapModel 满足 model.PaneModel interface。
var _ dashboardmodel.PaneModel = (*HeatmapModel)(nil)

// HeatmapModel 实现 internal/dashboard/model.PaneModel interface。
//
// 字段：
//   - state:    HeatmapState 包装（7 个 heatmap 相关字段，由 PR3 Step 1 抽出）
//   - width:    pane 渲染宽度（由 Bubble Tea runtime 通过 WindowSizeMsg 更新）
//   - height:   pane 渲染高度
//   - active:   当前是否为 activePane（影响 border 颜色）
//
// nil 安全：所有方法在 receiver 为 nil 时返回零值（不 panic）。
type HeatmapModel struct {
	state  HeatmapState
	width  int
	height int
	active bool
}

// NewModel 构造一个新的 HeatmapModel，初始 state 为零值。
func NewModel() *HeatmapModel {
	return &HeatmapModel{}
}

// State 返回当前 HeatmapState 值快照（满足 StateProvider interface）。
func (m *HeatmapModel) State() HeatmapState {
	if m == nil {
		return HeatmapState{}
	}
	return m.state
}

// SetState 替换内部 HeatmapState（PR3 Step 3c 过渡 API）。
//
//nolint:unused // 保留供 PR3-PR11 的 cmd/rnix 集成代码调用
func (m *HeatmapModel) SetState(s HeatmapState) {
	if m == nil {
		return
	}
	m.state = s
}

// SetActive 标记当前 pane 是否激活（影响 border 颜色）。
//
//nolint:unused // 保留供 PR3-PR11 的 cmd/rnix 集成代码调用
func (m *HeatmapModel) SetActive(active bool) {
	if m == nil {
		return
	}
	m.active = active
}

// HeatmapState 满足 StateProvider interface（由 keys.go 的 KeyLayer.ActiveModesFn 调用）。
//
// 等价于 State()，但接口方法名固定为 HeatmapState 以匹配 cmd/rnix 端 dashboardModel
// 在 PR3 Step 1 落地的同名 deprecated getter。
func (m *HeatmapModel) HeatmapState() HeatmapState {
	return m.State()
}

// --- tea.Model implementation ---

// Init 满足 tea.Model interface。
func (m *HeatmapModel) Init() tea.Cmd {
	return nil
}

// Update 满足 tea.Model interface。当前阶段仅处理 WindowSizeMsg。
func (m *HeatmapModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
// **当前阶段返回空 view**：cmd/rnix 端 renderHeatmapPane wrapper 调用 Render(state, ctx, w, h)
// 完成实际渲染。PR11 阶段重构后 HeatmapModel 自持依赖，View() 直接调用 Render 自洽。
func (m *HeatmapModel) View() tea.View {
	return tea.NewView("")
}

// --- model.PaneModel-specific methods ---

// Name 返回稳定 pane 名（用于 help overlay 分组 + logging + telemetry）。
func (m *HeatmapModel) Name() string {
	return "Heatmap"
}

// KeyLayer 返回 Heatmap pane 的 Layer 2 KeyLayer 注册体。
//
// 调用 heatmap.KeyLayer(nil) — fallback 由 cmd/rnix 端注入。
func (m *HeatmapModel) KeyLayer() *ui.KeyLayer {
	return KeyLayer(nil)
}

// OnSelectPID 当 App Model 收到 selectPIDMsg 时调用。
//
// 当前阶段：清空 Profile/Segments 等并触发新 PID 的 fetchHeatmapCmd。
// PR11 阶段会迁移完整 cmd 触发到这里。
func (m *HeatmapModel) OnSelectPID(pid types.PID) tea.Cmd {
	if m == nil {
		return nil
	}
	if pid == m.state.PID {
		return nil
	}
	m.state = HandlePIDChange(m.state)
	m.state.PID = pid
	return nil
}

// OnTick 在 dashboardTick 周期内调用。
//
// 当前阶段：维护 TickCount + 触发 IPC profile 刷新（cmd/rnix 端继续触发 fetchHeatmapCmd
// 节流逻辑 · spec § AC3 line 121 暗示 OnTick 应触发刷新，但完整迁移留 PR11）。
//
// Story 38-5 PR11 Step 4(b) Phase 3：签名扩展为 OnTickContext。
func (m *HeatmapModel) OnTick(_ dashboardmodel.OnTickContext) tea.Cmd {
	if m == nil {
		return nil
	}
	m.state.TickCount++
	return nil
}
