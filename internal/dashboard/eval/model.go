// Package eval — model.go (Story 38-5 PR9 Step 3)
//
// EvalModel 是 internal/dashboard/eval 包的 PaneModel 实现，承载 EvalState +
// 实现 internal/dashboard/model.PaneModel 5 方法（同 PR2-PR8 模式）。
//
// **38-4 evalScoreColorStyle 行为契约保留**（关键）：
//   - EvalState 字段是 cmd/rnix 端 evalScoreColorStyle 颜色梯度的输入数据；
//   - VS SOLO 颜色 / Recommended Bold / ★ ASCII 标记的渲染保持在 cmd/rnix 端；
//   - 本 PR 不迁出 render 主体（与 PR2-PR8 同模式 · 留待 PR11）。
//
// **OnSelectPID 行为决策**（与 IntentModel/SecurityModel/TraceModel 同模式 · 跨进程全局视图）：
//   - Eval pane 的 reputation/topology/synergy 三子视图均反映系统级评估数据，
//     与单个 PID 没有强关联；
//   - PID 切换不清空 state · 与 IntentModel/SecurityModel/TraceModel 一致。
package eval

import (
	tea "charm.land/bubbletea/v2"

	dashboardmodel "github.com/rnixai/rnix/internal/dashboard/model"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
)

// 编译期断言。
var (
	_ dashboardmodel.PaneModel = (*EvalModel)(nil)
	_ StateProvider            = (*EvalModel)(nil)
)

// EvalModel 实现 PaneModel interface。
type EvalModel struct {
	state  EvalState
	width  int
	height int
	active bool
}

// NewModel 构造一个 EvalModel · 初始 state 为零值。
func NewModel() *EvalModel {
	return &EvalModel{}
}

// State 返回当前 EvalState 值快照。
//
//nolint:unused
func (m *EvalModel) State() EvalState {
	if m == nil {
		return EvalState{}
	}
	return m.state
}

// SetState 替换内部 EvalState。
//
//nolint:unused
func (m *EvalModel) SetState(s EvalState) {
	if m == nil {
		return
	}
	m.state = s
}

// SetActive 标记当前 pane 是否激活。
//
//nolint:unused
func (m *EvalModel) SetActive(active bool) {
	if m == nil {
		return
	}
	m.active = active
}

// EvalState 满足 StateProvider interface。
func (m *EvalModel) EvalState() EvalState {
	return m.State()
}

// Init 满足 tea.Model interface。
func (m *EvalModel) Init() tea.Cmd { return nil }

// Update 满足 tea.Model interface。
func (m *EvalModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m == nil {
		return m, nil
	}
	if sz, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = sz.Width
		m.height = sz.Height
	}
	return m, nil
}

// View 当前阶段返回空 view。
func (m *EvalModel) View() tea.View { return tea.NewView("") }

// Name 返回稳定 pane 名。
func (m *EvalModel) Name() string { return "Eval" }

// KeyLayer 返回 Eval pane 的 Layer 2 KeyLayer。
func (m *EvalModel) KeyLayer() *ui.KeyLayer { return KeyLayer(nil) }

// OnSelectPID 当前阶段 noop（跨 PID 全局视图）。
func (m *EvalModel) OnSelectPID(_ types.PID) tea.Cmd {
	if m == nil {
		return nil
	}
	return nil
}

// OnTick 在 dashboardTick 周期内调用。
//
// Story 38-5 PR11 Step 4(b) Phase 3 真实化：
//   - 仅在 ctx.Active（Eval pane 激活）+ ctx.Connected + ctx.SocketPath 非空时
//     考虑 fetch；
//   - 总是触发 reputation fetch（reputation == nil 或 TickCount%5 == 0）；
//   - SubView == 1 时额外触发 topology fetch（同节流条件）；
//   - SubView == 2 时额外触发 synergy fetch（同节流条件）；
//   - 多 cmd 用 tea.Batch 收集（spec § 04 风险 1 缓解）.
//
// 行为契约 1:1 等价（与 cmd/rnix dashboardTick line 917-924 完全等价）：
//
//	if m.activePane == paneEval && m.connected {
//	    if m.eval.Reputations == nil || m.heatmap.TickCount%5 == 0 {
//	        cmds = append(cmds, fetchReputationCmd())
//	    }
//	    if m.eval.SubView == 1 && (m.eval.Topology == nil || m.heatmap.TickCount%5 == 0) {
//	        cmds = append(cmds, fetchTopologyCmd())
//	    }
//	    if m.eval.SubView == 2 && (m.eval.Synergies == nil || m.heatmap.TickCount%5 == 0) {
//	        cmds = append(cmds, fetchSynergyCmd())
//	    }
//	}
//
// nil 安全：receiver 为 nil 时返回 nil cmd。
func (m *EvalModel) OnTick(ctx dashboardmodel.OnTickContext) tea.Cmd {
	if m == nil {
		return nil
	}
	if !ctx.Active || !ctx.Connected || ctx.SocketPath == "" {
		return nil
	}
	var cmds []tea.Cmd
	// Reputation: 总是 fetch（first load 或 5s 节流）
	if m.state.Reputations == nil || ctx.TickCount%5 == 0 {
		if cmd := FetchReputationCmd(ctx.SocketPath); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	// Topology: SubView == 1
	if m.state.SubView == 1 && (m.state.Topology == nil || ctx.TickCount%5 == 0) {
		if cmd := FetchTopologyCmd(ctx.SocketPath); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	// Synergy: SubView == 2
	if m.state.SubView == 2 && (m.state.Synergies == nil || ctx.TickCount%5 == 0) {
		if cmd := FetchSynergyCmd(ctx.SocketPath); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}
