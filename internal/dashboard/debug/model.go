// Package debug — DebugModel implements OverlayModel interface (Story 38-5 PR11 Step 3).
//
// DebugModel 是 internal/dashboard/debug 包的 OverlayModel 实现，承载 DebugState +
// 实现 internal/dashboard/model.OverlayModel 4 方法（Init/Update/View 来自 tea.Model
// + IsActive + OnEnter + OnExit）。
//
// **34.6 strace fusion 行为契约保留**（关键 · spec § AC7 PR11 验收点）：
//   - 独立 IPC 连接 (DebugState.Client) 在 OnExit 时必须 Close（防 goroutine leak）；
//   - 历史 watermark + auto-reload 防止重复加载（Story 34.6 落地）；
//   - DeviceLatency 实时统计（按设备路径分组）；
//   - 本 PR Step 3 不迁出 render 主体（与 PR2-PR10 同模式 · 留待 PR11 Step 4 收尾）。
//
// **OverlayModel 设计同 InspectorModel（PR10 Step 3）**：
//   - OnEnter/OnExit stub · 实际 enter/exit 主体仍在 cmd/rnix.enterDebugMode / exitDebugMode；
//   - PR11 Step 4 App Model 瘦身时迁出 enter/exit 主体（含 stopStraceStream 关闭逻辑）。
package debug

import (
	tea "charm.land/bubbletea/v2"

	dashboardmodel "github.com/rnixai/rnix/internal/dashboard/model"
)

// 编译期断言：DebugModel 满足 OverlayModel + StateProvider 接口契约。
//
// 任何接口变更（例如新增方法）会让此处编译失败 · 防止悄悄破坏契约（Story 38-5 PR2-PR10 同模式）。
var (
	_ dashboardmodel.OverlayModel = (*DebugModel)(nil)
	_ StateProvider               = (*DebugModel)(nil)
)

// DebugModel 实现 OverlayModel interface（PR11 Step 3 阶段最小实现 · render 主体留 PR11 Step 4）。
//
// 字段语义：
//   - state：DebugState 持有 12 字段（PR11 Step 1 落地）；
//   - width/height：overlay 渲染尺寸；
//   - active：当前是否处于 viewDebug 模式（与 cmd/rnix.viewMode == viewDebug 一致 ·
//     通过 SetActive 同步）。
type DebugModel struct {
	state  DebugState
	width  int
	height int
	active bool
}

// NewModel 构造一个 DebugModel · 初始 state 为零值。
func NewModel() *DebugModel {
	return &DebugModel{}
}

// State 返回当前 DebugState 值快照（值拷贝 · 调用方修改不影响内部状态）。
//
// nil safety：receiver 为 nil 时返回零值 DebugState。
//
//nolint:unused
func (m *DebugModel) State() DebugState {
	if m == nil {
		return DebugState{}
	}
	return m.state
}

// SetState 替换内部 DebugState（值拷贝语义）。
//
//nolint:unused
func (m *DebugModel) SetState(s DebugState) {
	if m == nil {
		return
	}
	m.state = s
}

// SetActive 标记当前 overlay 是否处于激活状态（与 InspectorModel.SetActive 同模式）。
//
//nolint:unused
func (m *DebugModel) SetActive(active bool) {
	if m == nil {
		return
	}
	m.active = active
}

// DebugState 满足 StateProvider interface · KeyLayer.ActiveModesFn 通过此读取 state。
func (m *DebugModel) DebugState() DebugState {
	return m.State()
}

// --- tea.Model 接口 ---

// Init 满足 tea.Model interface · 当前阶段无初始化 cmd（实际 strace stream 启动由
// cmd/rnix 端 startStraceStreamCmd 触发）。
func (m *DebugModel) Init() tea.Cmd { return nil }

// Update 满足 tea.Model interface · 当前阶段仅同步 WindowSizeMsg，其他消息 noop。
//
// nil safety：receiver 为 nil 时不修改并返回 nil cmd。
func (m *DebugModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m == nil {
		return m, nil
	}
	if sz, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = sz.Width
		m.height = sz.Height
	}
	return m, nil
}

// View 当前阶段返回空 view（实际渲染由 cmd/rnix wrapper 完成 · PR11 Step 4 迁出 render 主体）。
func (m *DebugModel) View() tea.View { return tea.NewView("") }

// --- OverlayModel 接口 ---

// IsActive 返回当前 overlay 是否处于激活状态（即用户是否在 viewDebug 模式）。
//
// nil safety：receiver 为 nil 时返回 false。
func (m *DebugModel) IsActive() bool {
	if m == nil {
		return false
	}
	return m.active
}

// OnEnter 在 App Model 进入 viewDebug 时调用 · 当前阶段 stub（实际 startStraceStream
// + loadHistoricalStrace 由 cmd/rnix 端 enterDebugMode 承担 · PR11 Step 4 迁入主体）。
//
// 设计说明：原 enterDebugMode 涉及独立 IPC 连接管理（debugClient.Close + AttachDebug
// goroutine 启动），完整迁入需要：
//  1. cmd/rnix 端 selectedPID/selectedUUID 字段访问；
//  2. ipc.Dial 调用 + AttachDebug + 历史 strace events 加载；
//  3. straceToUnifiedEvent 类型转换（依赖 cmd/rnix.UnifiedEvent · UnifiedEvent
//     未迁出 internal/dashboard 共享位置）；
//
// 因此本 PR Step 3 阶段保持 stub · PR11 Step 4 收尾时一并迁入（或在评估 UnifiedEvent
// 迁移成本后决定保留 thin wrapper 模式）。
//
// nil safety：receiver 为 nil 时返回 nil cmd。
func (m *DebugModel) OnEnter() tea.Cmd {
	if m == nil {
		return nil
	}
	return nil
}

// OnExit 在 App Model 退出 viewDebug（按 d / esc）时调用 · 当前阶段 stub。
//
// 与 OnEnter 同设计原因（实际 exitDebugMode + stopStraceStream 仍在 cmd/rnix 端）。
//
// **关键约束**（PR11 Step 4 迁入主体时必须保留）：OnExit 必须确保 DebugState.Client
// 关闭（防 goroutine leak）+ 清空 DeviceLatency / StraceEvents / CtxProfile（防止
// 下次 enter 时显示陈旧数据）。
//
// nil safety：receiver 为 nil 时返回 nil cmd。
func (m *DebugModel) OnExit() tea.Cmd {
	if m == nil {
		return nil
	}
	return nil
}
