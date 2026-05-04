// Package inspector — model.go (Story 38-5 PR10 Step 3)
//
// InspectorModel 是 internal/dashboard/inspector 包的 OverlayModel 实现，承载 InspectorState +
// 实现 internal/dashboard/model.OverlayModel 4 方法（Init/Update/View 来自 tea.Model + IsActive
// + OnEnter + OnExit）。
//
// **38-3 / 36-6 行为契约保留**（关键 · spec § AC6 PR10 验收点）：
//   - Step Inspector 5-lens 视觉规则（38-3 落地）保持在 cmd/rnix 端不变；
//   - Diff mode（base picker overlay / dd double-tap / lens marks / per-lens viewport）
//     38-3 + 36-6 落地行为完全保留；
//   - Follow Live（生成计数器 + 自动跟随最新 step）36-6 落地行为完全保留；
//   - 本 PR Step 3 不迁出 render 主体（与 PR2-PR9 同模式 · 留待 PR11 重构）。
//
// **OnEnter / OnExit 行为决策**（与 PaneModel.OnSelectPID 设计差异）：
//   - Overlay 有显式生命周期：OnEnter 进入 inspector overlay · OnExit 退出回到 PrevMode；
//   - 当前 PR 阶段 OnEnter/OnExit 为 stub（cmd/rnix 端 enterStepInspector / esc handler
//     仍承担实际状态切换）；
//   - PR11 App Model 瘦身时迁出实际 hook 主体（包含 IPC 拉取 step list、reset cross-pane
//     search state、构造 viewports 等）。
//
// **Searchable 接入**（与 TimelineModel 同模式 · spec § 04 风险 3）：
//   - InspectorModel 实现 plugin.Searchable interface（PR10 Step 4 落地）；
//   - SearchableLines() 返回当前激活 lens 的 Contents 文本行；
//   - 解耦 SearchPlugin 与 InspectorModel 私有字段（spec § 04 风险 3 缓解）。
package inspector

import (
	tea "charm.land/bubbletea/v2"

	dashboardmodel "github.com/rnixai/rnix/internal/dashboard/model"
	"github.com/rnixai/rnix/internal/dashboard/plugin"
)

// 编译期断言：InspectorModel 满足 OverlayModel + StateProvider + plugin.Searchable 接口契约。
//
// 任何接口变更（例如新增方法）会让此处编译失败 · 防止悄悄破坏契约（Story 38-5 PR2-PR9 同模式）。
var (
	_ dashboardmodel.OverlayModel = (*InspectorModel)(nil)
	_ StateProvider               = (*InspectorModel)(nil)
	_ plugin.Searchable           = (*InspectorModel)(nil)
)

// InspectorModel 实现 OverlayModel interface（PR10 Step 3 阶段最小实现 · render 主体留 PR11）。
//
// 字段语义：
//   - state：InspectorState 持有 25 字段（PR10 Step 1 落地）；
//   - width/height：overlay 渲染尺寸（lipgloss column / row）；
//   - active：当前是否处于 viewStepInspector 模式（与 cmd/rnix.viewMode == viewStepInspector
//     语义一致 · 通过 SetActive 同步）。
type InspectorModel struct {
	state  InspectorState
	width  int
	height int
	active bool
}

// NewModel 构造一个 InspectorModel · 初始 state 为零值（与 cmd/rnix.dashboardModel 零值
// 一致：所有指针字段 nil / Lens=LensConversation / DiffMode=false / FollowLive=false）。
func NewModel() *InspectorModel {
	return &InspectorModel{}
}

// State 返回当前 InspectorState 值快照（值拷贝 · 调用方修改不影响内部状态）。
//
// nil safety：receiver 为 nil 时返回零值 InspectorState（避免 panic · 与 PR2-PR9 同模式）。
//
//nolint:unused // PR11 App Model 瘦身时统一从 dashboardModel 改为通过 m.inspector.State() 读取。
func (m *InspectorModel) State() InspectorState {
	if m == nil {
		return InspectorState{}
	}
	return m.state
}

// SetState 替换内部 InspectorState（值拷贝语义）。
//
// 使用场景：cmd/rnix 端在 PR10 过渡期通过 SetState 同步 m.inspector → InspectorModel.state；
// PR11 移除 dashboardModel.inspector 字段后，此 API 仍保留供测试 / 外部构造使用。
//
// nil safety：receiver 为 nil 时直接返回（与 PR2-PR9 同模式）。
//
//nolint:unused
func (m *InspectorModel) SetState(s InspectorState) {
	if m == nil {
		return
	}
	m.state = s
}

// SetActive 标记当前 overlay 是否处于激活状态（用户进入 viewStepInspector 时 SetActive(true)，
// 退出时 SetActive(false)）。该状态决定 IsActive() 返回值，进而影响 App Model 是否对此
// overlay 调用 OnSelectPID/OnTick（spec § 04 风险 2 · 仅 active overlay 接收 hook）。
//
//nolint:unused
func (m *InspectorModel) SetActive(active bool) {
	if m == nil {
		return
	}
	m.active = active
}

// InspectorState 满足 StateProvider interface · KeyLayer 通过该接口读取最新 state。
func (m *InspectorModel) InspectorState() InspectorState {
	return m.State()
}

// --- tea.Model 接口 ---

// Init 满足 tea.Model interface · 当前阶段无初始化 cmd（实际 step list IPC 拉取由 cmd/rnix
// 端 enterStepInspector + fetchInspectorStepListCmd 触发 · PR11 迁入 OnEnter）。
func (m *InspectorModel) Init() tea.Cmd { return nil }

// Update 满足 tea.Model interface · 当前阶段仅同步 WindowSizeMsg 到 width/height，其他
// 消息 noop（cmd/rnix 端 inspectorKey + diff handlers 仍承担实际处理 · PR11 重构主体）。
//
// nil safety：receiver 为 nil 时不修改并返回 nil cmd。
func (m *InspectorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m == nil {
		return m, nil
	}
	if sz, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = sz.Width
		m.height = sz.Height
	}
	return m, nil
}

// View 当前阶段返回空 view（与 PR2-PR9 同模式 · 实际渲染由 cmd/rnix wrapper renderStepInspector
// 完成 · PR11 迁出 render 主体）。Bubble Tea v2 签名 tea.View 而非 string。
func (m *InspectorModel) View() tea.View { return tea.NewView("") }

// --- OverlayModel 接口 ---

// IsActive 返回当前 overlay 是否处于激活状态（即用户是否在 viewStepInspector 模式）。
//
// nil safety：receiver 为 nil 时返回 false（不会被 App Model 误判为 active）。
func (m *InspectorModel) IsActive() bool {
	if m == nil {
		return false
	}
	return m.active
}

// OnEnter 在 App Model 进入 viewStepInspector 时调用 · 当前阶段 stub（实际状态切换 +
// IPC 拉取由 cmd/rnix 端 enterStepInspector 承担 · PR11 迁入主体）。
//
// 设计说明：原 enterStepInspector 涉及 25+ 字段重置（含 cross-pane search state · diff
// mode reset · follow live reset · 5 viewports init），完整迁入需要：
//  1. cmd/rnix 端 selectedPID/selectedUUID 字段访问（OverlayModel 不持有 App Model 引用）；
//  2. fetchInspectorStepListCmd IPC 调用（依赖 cmd/rnix.ipc.Dial 私有函数）；
//  3. cross-pane search state reset（需要 PR10 Step 4 SearchPlugin 抽离后才能优雅迁移）。
//
// 因此本 PR Step 3 阶段保持 stub · PR11 收尾时一并迁入。
//
// nil safety：receiver 为 nil 时返回 nil cmd。
func (m *InspectorModel) OnEnter() tea.Cmd {
	if m == nil {
		return nil
	}
	return nil
}

// OnExit 在 App Model 退出 viewStepInspector（按 esc 等）时调用 · 当前阶段 stub。
//
// 与 OnEnter 同设计原因（实际 esc handler + PrevMode 恢复仍在 cmd/rnix 端 inspectorKey）。
//
// nil safety：receiver 为 nil 时返回 nil cmd。
func (m *InspectorModel) OnExit() tea.Cmd {
	if m == nil {
		return nil
	}
	return nil
}
