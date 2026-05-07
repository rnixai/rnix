// Package security — model.go (Story 38-5 PR7 Step 3)
//
// SecurityModel 是 internal/dashboard/security 包的 PaneModel 实现，承载 SecurityState +
// 实现 internal/dashboard/model.PaneModel 5 方法（Init/Update/View/Name/KeyLayer/
// OnSelectPID/OnTick）。
//
// 当前阶段（38-5 PR7 Step 3）的 View() 实现说明（与 PR2 TreeModel / PR3 HeatmapModel /
// PR4 TimelineModel / PR5 DetailModel / PR6 IntentModel 同模式）：
//
//	tea.Model.View() 标准签名为 View() tea.View，无法直接传入 cmd/rnix 端运行时数据。
//	因此 SecurityModel.View() 在本阶段返回空 view，cmd/rnix 端继续通过 renderSecurityPane
//	wrapper 完成实际渲染。PR11 阶段重构时 SecurityModel 自持依赖，View() 直接调用 Render 自洽。
//
// **38-4 Alert Immune 行为契约保留**（关键）：
//   - OnSelectPID 在 PID 切换时**不**清空 ImmuneStatus / Alerts（Security pane 是跨进程
//     的全局视图 · alert 数据反映系统级 immune daemon 状态 · 与 IntentModel 同模式 ·
//     与 DetailModel 清空 Detail 形成对比）；
//   - OnTick 不直接触发 IPC（IPC 通过 cmd/rnix 端 dashboardTick 的 5 秒节流刷新触发）。
package security

import (
	tea "charm.land/bubbletea/v2"

	dashboardmodel "github.com/rnixai/rnix/internal/dashboard/model"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
)

// 编译期断言：SecurityModel 满足 model.PaneModel interface。
var _ dashboardmodel.PaneModel = (*SecurityModel)(nil)

// 编译期断言：*SecurityModel 通过指针接收者实现 StateProvider interface（用于 KeyLayer 注入）。
var _ StateProvider = (*SecurityModel)(nil)

// SecurityModel 实现 internal/dashboard/model.PaneModel interface。
//
// 字段：
//   - state:    SecurityState 包装（5 个 security 字段，由 PR7 Step 1 抽出）
//   - width:    pane 渲染宽度（由 Bubble Tea runtime 通过 WindowSizeMsg 更新）
//   - height:   pane 渲染高度
//   - active:   当前是否为 activePane（影响 border 颜色）
//
// nil 安全：所有方法在 receiver 为 nil 时返回零值（不 panic）。
type SecurityModel struct {
	state  SecurityState
	width  int
	height int
	active bool
}

// NewModel 构造一个新的 SecurityModel，初始 state 为零值。
func NewModel() *SecurityModel {
	return &SecurityModel{}
}

// State 返回当前 SecurityState 值快照。
//
//nolint:unused // 保留供 PR7-PR11 的 cmd/rnix 集成代码调用
func (m *SecurityModel) State() SecurityState {
	if m == nil {
		return SecurityState{}
	}
	return m.state
}

// SetState 替换内部 SecurityState（PR7 Step 3 过渡 API）。
//
//nolint:unused // 保留供 PR7-PR11 的 cmd/rnix 集成代码调用
func (m *SecurityModel) SetState(s SecurityState) {
	if m == nil {
		return
	}
	m.state = s
}

// SetActive 标记当前 pane 是否激活（影响 border 颜色）。
//
//nolint:unused // 保留供 PR7-PR11 的 cmd/rnix 集成代码调用
func (m *SecurityModel) SetActive(active bool) {
	if m == nil {
		return
	}
	m.active = active
}

// SecurityState 满足 StateProvider interface（让 keys.go 中的 KeyLayer ActiveModesFn cast）。
//
// 等价于 State()，但接口方法名固定为 SecurityState 以匹配 cmd/rnix 端 dashboardModel
// 在 PR7 Step 1 落地的同名 deprecated getter。
func (m *SecurityModel) SecurityState() SecurityState {
	return m.State()
}

// --- tea.Model implementation ---

// Init 满足 tea.Model interface。
//
// 当前阶段不返回 cmd（IPC 触发由 cmd/rnix 端 dashboardTick 的 fetchImmuneStatusCmd 负责）。
func (m *SecurityModel) Init() tea.Cmd {
	return nil
}

// Update 满足 tea.Model interface。当前阶段仅处理 WindowSizeMsg。
//
// 完整 immuneStatusMsg / alert drill-in 处理仍在 cmd/rnix 端 Update + dispatchPaneKey
// 内（PR11 迁回）。
func (m *SecurityModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
// **当前阶段返回空 view**：cmd/rnix 端 renderSecurityPane wrapper 完成实际渲染。
// PR11 阶段重构后 SecurityModel 自持依赖，View() 直接调用 Render 自洽。
func (m *SecurityModel) View() tea.View {
	return tea.NewView("")
}

// --- model.PaneModel-specific methods ---

// Name 返回稳定 pane 名（用于 help overlay 分组 + logging + telemetry）。
func (m *SecurityModel) Name() string {
	return "Security"
}

// KeyLayer 返回 Security pane 的 Layer 2 KeyLayer 注册体。
//
// 调用 security.KeyLayer(nil) — fallback 由 cmd/rnix 端注入到 dispatcher。
func (m *SecurityModel) KeyLayer() *ui.KeyLayer {
	return KeyLayer(nil)
}

// OnSelectPID 当 App Model 收到 selectPIDMsg 时调用。
//
// 当前阶段（PR7 Step 3）：
//   - **不**清空 ImmuneStatus / Alerts / Cursor（与 IntentModel 同模式 · 与 DetailModel
//     清空 Detail 形成对比）；
//   - 理由：Security pane 反映系统级 immune daemon 状态 · 跨进程全局视图 · PID 切换
//     不丢用户的 alert 列表浏览位置；
//   - 38-4 Alert Immune 路由保持不变（Layer 0 enter handler 仍按 EventImmune 类型路由）。
func (m *SecurityModel) OnSelectPID(_ types.PID) tea.Cmd {
	if m == nil {
		return nil
	}
	// noop · PID 切换不影响 SecurityState（与 IntentModel 同模式）
	return nil
}

// OnTick 在 dashboardTick 周期内调用。
//
// Story 38-5 PR11 Step 4(b) Phase 3 真实化：
//   - 仅在 ctx.Active（Security pane 是当前激活）+ ctx.Connected + ctx.SocketPath
//     非空时考虑 fetch；
//   - 节流条件：m.state.ImmuneStatus == nil 或 ctx.TickCount%5 == 0；
//   - 全部满足时返回 FetchImmuneStatusCmd · 否则返回 nil cmd.
//
// 行为契约 1:1 等价（与 cmd/rnix dashboardTick line 887-892 完全等价）：
//
//	if m.activePane == paneSecurity && m.connected {
//	    if m.security.ImmuneStatus == nil || m.heatmap.TickCount%5 == 0 {
//	        cmds = append(cmds, fetchImmuneStatusCmd())
//	    }
//	}
//
// nil 安全：receiver 为 nil 时返回 nil cmd。
func (m *SecurityModel) OnTick(ctx dashboardmodel.OnTickContext) tea.Cmd {
	if m == nil {
		return nil
	}
	if !ctx.Active || !ctx.Connected || ctx.SocketPath == "" {
		return nil
	}
	if m.state.ImmuneStatus != nil && ctx.TickCount%5 != 0 {
		return nil
	}
	return FetchImmuneStatusCmd(ctx.SocketPath)
}
