// Package trace — model.go (Story 38-5 PR8 Step 3)
//
// TraceModel 是 internal/dashboard/trace 包的 PaneModel 实现，承载 TraceState +
// 实现 internal/dashboard/model.PaneModel 5 方法（同 PR2-PR7 模式）。
//
// **38-4 waterfall 行为契约保留**（关键）：
//   - SpanFlatNodes 字段在 cmd/rnix 端 renderWaterfallBar 渲染（status 颜色 + 20-char 长度 + ASCII fallback）
//   - flattenSpanTree → SpanFlatNodes 路径在 cmd/rnix dashboard.go 内（通过 spanTreeMsg 处理）
//   - 本 PR 不迁出 render 主体（与 PR2-PR7 同模式 · 留待 PR11）
//
// **OnSelectPID 行为决策**（与 IntentModel/SecurityModel 同模式 · 与 DetailModel 不同）：
//   - Trace pane 在 overview 视图下是跨进程的全局 trace 列表 · PID 切换不清空 Summaries；
//   - 在 spans 视图下，SelectedSpanTree 反映当前 drill-in 的 trace · 与 PID 无强关联；
//   - 因此 OnSelectPID 保持 noop · 跨进程切换不丢用户的 trace/span 浏览上下文。
package trace

import (
	"time"

	tea "charm.land/bubbletea/v2"

	dashboardmodel "github.com/rnixai/rnix/internal/dashboard/model"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
)

// 编译期断言：TraceModel 满足 PaneModel + StateProvider interface。
var (
	_ dashboardmodel.PaneModel = (*TraceModel)(nil)
	_ StateProvider            = (*TraceModel)(nil)
)

// TraceModel 实现 PaneModel interface。
type TraceModel struct {
	state  TraceState
	width  int
	height int
	active bool
}

// NewModel 构造一个 TraceModel · 初始 state 为零值。
func NewModel() *TraceModel {
	return &TraceModel{}
}

// State 返回当前 TraceState 值快照。
//
//nolint:unused // 保留供 PR8-PR11 集成代码调用
func (m *TraceModel) State() TraceState {
	if m == nil {
		return TraceState{}
	}
	return m.state
}

// SetState 替换内部 TraceState。
//
//nolint:unused
func (m *TraceModel) SetState(s TraceState) {
	if m == nil {
		return
	}
	m.state = s
}

// SetActive 标记当前 pane 是否激活。
//
//nolint:unused
func (m *TraceModel) SetActive(active bool) {
	if m == nil {
		return
	}
	m.active = active
}

// TraceState 满足 StateProvider interface。
func (m *TraceModel) TraceState() TraceState {
	return m.State()
}

// Init 满足 tea.Model interface。
func (m *TraceModel) Init() tea.Cmd { return nil }

// Update 满足 tea.Model interface · 当前阶段仅处理 WindowSizeMsg。
func (m *TraceModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m == nil {
		return m, nil
	}
	if sz, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = sz.Width
		m.height = sz.Height
	}
	return m, nil
}

// View 当前阶段返回空 view（cmd/rnix renderTracePane wrapper 完成实际渲染）。
func (m *TraceModel) View() tea.View { return tea.NewView("") }

// Name 返回稳定 pane 名。
func (m *TraceModel) Name() string { return "Trace" }

// KeyLayer 返回 Trace pane 的 Layer 2 KeyLayer。
func (m *TraceModel) KeyLayer() *ui.KeyLayer { return KeyLayer(nil) }

// OnSelectPID 当 App Model 收到 selectPIDMsg 时调用 · 当前阶段 noop（跨 PID 全局视图）。
func (m *TraceModel) OnSelectPID(_ types.PID) tea.Cmd {
	if m == nil {
		return nil
	}
	return nil
}

// OnTick 当前阶段 noop（IPC 由 cmd/rnix dashboardTick fetchTraceListCmd 5 秒节流刷新）。
func (m *TraceModel) OnTick(_ time.Time) tea.Cmd {
	if m == nil {
		return nil
	}
	return nil
}
