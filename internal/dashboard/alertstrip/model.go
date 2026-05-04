// Package alertstrip — model.go (Story 38-5 PR12 Step 2+3)
//
// AlertStripModel 是 internal/dashboard/alertstrip 包的 OverlayModel 实现，
// 承载 AlertStripState (PR12 Step 1 落地的 2 标量字段) + 实现
// internal/dashboard/model.OverlayModel 4 方法（Init/Update/View 来自 tea.Model
// + IsActive + OnEnter + OnExit）。
//
// **38-2 / 38-4 行为契约保留**（关键 · spec § PR12 验收点）：
//   - alertStripHeight (折叠 2 行 / 展开 max 8 行) 行为保留 · helper 迁出至本包；
//   - alertCountBadge (右对齐 ✗N ⚠M) 行为保留在 cmd/rnix 端（依赖 UnifiedEvent 类型）；
//   - synthSecurityAlerts → buildAlertEventsWith 路径（38-4 Alert Immune 路由）
//     保留在 cmd/rnix 端（依赖 UnifiedEvent + ipc.AlertWire 类型 cascade · 与 PR11
//     Events/StraceEvents 决策同模式）；
//   - renderAlertStrip 主体保留在 cmd/rnix 端（与 PR2-PR11 render 主体留 PR11 同模式）。
//
// **OnEnter / OnExit 行为决策**（与 PaneModel.OnSelectPID 设计差异）：
//   - Overlay 显式生命周期：OnEnter 进入 alert strip expanded 模式 · OnExit 折叠回去；
//   - 当前 PR 阶段 OnEnter/OnExit 为 stub（cmd/rnix 端 dashboard_keylayers.go::a 键
//     handler 仍承担实际状态切换）；
//   - PR11 App Model 瘦身时迁出实际 hook 主体。
//
// **IsActive 决策**：与 InspectorModel/DebugModel 不同，alert strip 总是显示
// （只是折叠/展开切换），所以 IsActive 返回 Expanded 字段（True 时 OverlayModel 接收
// OnSelectPID/OnTick hook · 折叠时不接收 · spec § 04 风险 2 · "仅 active overlay
// 接收 hook"）。
package alertstrip

import (
	tea "charm.land/bubbletea/v2"

	dashboardmodel "github.com/rnixai/rnix/internal/dashboard/model"
)

// 编译期断言：AlertStripModel 满足 OverlayModel + StateProvider 接口契约。
//
// 任何接口变更（例如新增方法）会让此处编译失败 · 防止悄悄破坏契约
// （Story 38-5 PR2-PR11 同模式）。
var (
	_ dashboardmodel.OverlayModel = (*AlertStripModel)(nil)
	_ StateProvider               = (*AlertStripModel)(nil)
)

// AlertStripModel 实现 OverlayModel interface（PR12 Step 2+3 阶段最小实现 ·
// helpers/render 主体留 cmd/rnix 端依赖 UnifiedEvent 类型 cascade · PR11 后置评估）。
//
// 字段语义：
//   - state：AlertStripState 持有 2 标量字段（Expanded/Cursor · PR12 Step 1 落地）；
//   - width/height：strip 渲染尺寸（lipgloss column / row · 通常 width = 终端宽度 ·
//     height = alertStripHeight 计算值）；
//   - active：当前 overlay 是否激活（与 state.Expanded 等价，但显式独立字段允许
//     未来 alert strip 总是显示但不接收 hook 等扩展）。当前阶段 SetActive 由
//     cmd/rnix 端在 a 键切换时同步。
type AlertStripModel struct {
	state  AlertStripState
	width  int
	height int
	active bool
}

// NewModel 构造一个 AlertStripModel · 初始 state 为零值（Expanded=false / Cursor=0 ·
// 与 cmd/rnix.dashboardModel 零值一致）。
func NewModel() *AlertStripModel {
	return &AlertStripModel{}
}

// State 返回当前 AlertStripState 值快照（值拷贝 · 调用方修改不影响内部状态）。
//
// nil safety：receiver 为 nil 时返回零值 AlertStripState（避免 panic · 与 PR2-PR11
// 同模式）。
//
//nolint:unused // PR11 App Model 瘦身时统一从 dashboardModel 改为通过 m.alertStrip.State() 读取。
func (m *AlertStripModel) State() AlertStripState {
	if m == nil {
		return AlertStripState{}
	}
	return m.state
}

// SetState 替换内部 AlertStripState（值拷贝语义）。
//
// 使用场景：cmd/rnix 端在 PR12 过渡期通过 SetState 同步 m.alertStrip → AlertStripModel.state；
// PR11 移除 dashboardModel.alertStrip 字段后，此 API 仍保留供测试 / 外部构造使用。
//
// nil safety：receiver 为 nil 时直接返回（与 PR2-PR11 同模式）。
//
//nolint:unused
func (m *AlertStripModel) SetState(s AlertStripState) {
	if m == nil {
		return
	}
	m.state = s
}

// SetActive 标记当前 overlay 是否处于激活状态（用户按 `a` 展开 alert strip 时
// SetActive(true)，折叠时 SetActive(false)）。该状态决定 IsActive() 返回值，
// 进而影响 App Model 是否对此 overlay 调用 OnSelectPID/OnTick（spec § 04
// 风险 2 · 仅 active overlay 接收 hook）。
//
//nolint:unused
func (m *AlertStripModel) SetActive(active bool) {
	if m == nil {
		return
	}
	m.active = active
}

// AlertStripState 满足 StateProvider interface · 允许其他包通过 ctx.(StateProvider)
// cast 读取 state 而无需直接持有 AlertStripModel 引用。
func (m *AlertStripModel) AlertStripState() AlertStripState {
	return m.State()
}

// --- tea.Model 接口 ---

// Init 满足 tea.Model interface · 当前阶段无初始化 cmd（实际 alert events 由
// cmd/rnix 端 dashboardTick + buildAlertEventsWith 周期生成 · 不需要在 Init 触发）。
func (m *AlertStripModel) Init() tea.Cmd { return nil }

// Update 满足 tea.Model interface · 当前阶段仅同步 WindowSizeMsg 到 width/height，
// 其他消息 noop（cmd/rnix 端 dashboard_keylayers.go::a/[/]/enter handler 仍承担
// 实际处理 · PR11 重构主体）。
//
// nil safety：receiver 为 nil 时不修改并返回 nil cmd。
func (m *AlertStripModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m == nil {
		return m, nil
	}
	if sz, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = sz.Width
		m.height = sz.Height
	}
	return m, nil
}

// View 当前阶段返回空 view（与 PR2-PR11 同模式 · 实际渲染由 cmd/rnix wrapper
// renderAlertStrip 完成 · PR11 后置评估 render 主体迁出 · UnifiedEvent 类型
// cascade 阻塞）。Bubble Tea v2 签名 tea.View 而非 string。
func (m *AlertStripModel) View() tea.View { return tea.NewView("") }

// --- OverlayModel 接口 ---

// IsActive 返回当前 overlay 是否处于激活状态（用户按 `a` 展开 alert strip · 与
// state.Expanded 字段等价但显式独立允许未来扩展）。
//
// 行为契约（spec § 04 风险 2）：仅当 IsActive==true 时 App Model 才对本
// overlay 调用 OnSelectPID/OnTick · 折叠时不参与 hook broadcast。
//
// nil safety：receiver 为 nil 时返回 false（不会被 App Model 误判为 active）。
func (m *AlertStripModel) IsActive() bool {
	if m == nil {
		return false
	}
	return m.active
}

// OnEnter 在 App Model 展开 alert strip（按 `a` 键）时调用 · 当前阶段 stub
// （实际状态切换由 cmd/rnix 端 dashboard_keylayers.go::a 键 handler 承担 ·
// PR11 迁入主体）。
//
// 设计说明：原 a 键 handler 涉及 alertStrip.Expanded 切换 + alertStrip.Cursor
// clamp（折叠时重置为 0），完整迁入需要：
//  1. dashboardModel.alertEvents 字段访问（OverlayModel 不持有 App Model 引用）；
//  2. alertStripHeight helper 调用（依赖 alertEvents 长度）。
//
// 因此本 PR Step 2+3 阶段保持 stub · PR11 收尾时一并迁入。
//
// nil safety：receiver 为 nil 时返回 nil cmd。
func (m *AlertStripModel) OnEnter() tea.Cmd {
	if m == nil {
		return nil
	}
	return nil
}

// OnExit 在 App Model 折叠 alert strip（按 `a` 键再次切换）时调用 · 当前阶段 stub。
//
// 与 OnEnter 同设计原因（实际折叠 handler + Cursor 重置仍在 cmd/rnix 端
// dashboard_keylayers.go）。
//
// nil safety：receiver 为 nil 时返回 nil cmd。
func (m *AlertStripModel) OnExit() tea.Cmd {
	if m == nil {
		return nil
	}
	return nil
}

// --- Helpers (UnifiedEvent-independent) ---

// AlertStripHeight returns the number of lines the alert strip should occupy.
//
// Behaviour (Story 34.4 + 38-2 AC#3 contract preserved):
//   - alertCount == 0 → 0 lines (strip is hidden entirely);
//   - expanded == false → min(alertCount, 2) lines (collapsed mode shows up to 2);
//   - expanded == true  → min(alertCount, 8) lines (expanded mode shows up to 8 with cursor).
//
// This helper is UnifiedEvent-independent (only int/bool params) so it can live
// in the alertstrip package without importing cmd/rnix. cmd/rnix-side
// alertStripHeight is a thin wrapper for backwards compatibility (Story 38-5
// PR12 Step 2 · 与 PR2/PR3 helper 公开化同模式).
//
// Performance: O(1) · trivial min comparison.
func AlertStripHeight(alertCount int, expanded bool) int {
	if alertCount == 0 {
		return 0
	}
	if expanded {
		return minInt(alertCount, 8)
	}
	return minInt(alertCount, 2)
}

// minInt is the package-private 2-arg int min helper. Go 1.21+ has builtin min,
// but using a local helper keeps the alertstrip package self-contained for
// older Go versions and avoids accidental shadowing.
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
