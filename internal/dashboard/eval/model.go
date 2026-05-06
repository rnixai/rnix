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

// OnTick 当前阶段 noop（IPC 由 cmd/rnix dashboardTick 触发）。
func (m *EvalModel) OnTick(_ dashboardmodel.OnTickContext) tea.Cmd {
	if m == nil {
		return nil
	}
	return nil
}
