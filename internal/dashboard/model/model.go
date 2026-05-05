// Package model — internal/dashboard 子 Model 的核心 interface 与共享 base struct（Story 38-5 PR1）。
//
// PaneModel / OverlayModel 是 Story 38-5 god struct 拆分的契约根：
// dashboardModel 中 ~160 字段被分解为 8 个 PaneModel + 3 个 OverlayModel + 共享 App 字段，
// 由本包定义两类组件的最小行为约定。
//
// 调用约定（spec § 02 line 232-248 + § 14 Bubble Tea v2 决策）：
//   - 子 Model 全部使用指针接收者实现 interface（字段 ≥ 5 时值拷贝成本不可忽略）。
//   - hook 调用方在 App Model 内部直接 m.tree.OnSelectPID(pid)，指针修改自然生效。
//   - hook 返回 tea.Cmd 必须可参与 tea.Batch（不阻塞 / 不依赖外部状态）。
//   - nil 安全：Init/Update/View/KeyLayer/OnSelectPID/OnTick/OnEnter/OnExit 在 receiver
//     为 nil 时不得 panic（model_test.go::TestPaneModel_NilSafety 强制契约）。
//
// 依赖方向：本包位于 internal/dashboard/model，**禁止反向依赖** cmd/rnix。
// 与 internal/ui 的关系：PaneModel.KeyLayer 返回 *ui.KeyLayer，KeyLayer 由 38-1 落地。
package model

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
)

// PaneModel 是 8 个 Pane 子 Model（Tree/Timeline/Heatmap/Detail/Intent/Security/Trace/Eval）的统一契约。
//
// 实现要求：
//   - 嵌入 tea.Model（Init/Update/View）— Bubble Tea v2 单向数据流；
//   - Name() 返回稳定 pane 名（用于 help overlay 分组与 logging）；
//   - KeyLayer() 返回 38-1 注册到 Dispatcher.Layer2 的 *ui.KeyLayer，**必须** stable
//     across 多次调用（KeyLayer 内部 Bindings/Docs 在 Init 后即固定）；
//   - OnSelectPID(pid) 在 App Model 收到 selectPIDMsg 时被调用，子 Model 同步 cursor /
//     attached PID / 缓存重置等。返回 tea.Cmd 由 App Model 通过 tea.Batch 收集；
//   - OnTick(now) 在 dashboardTick 周期内被调用，子 Model 触发 IPC 拉取 / 缓存刷新。
//
// nil 安全：所有方法在 receiver 为 nil 时返回零值（nil cmd / "" / nil layer），不得 panic。
type PaneModel interface {
	tea.Model
	Name() string
	KeyLayer() *ui.KeyLayer
	OnSelectPID(pid types.PID) tea.Cmd
	OnTick(now time.Time) tea.Cmd
}

// OverlayModel 是 3 个 Overlay 子 Model（Inspector/Debug/AlertStrip）的统一契约。
//
// 与 PaneModel 的差异：
//   - Overlay 有显式生命周期 OnEnter/OnExit（IsActive()==true 期间持有资源 / 独立 IPC 连接）；
//   - 不强制 Name/KeyLayer（Overlay 通常注册到 Dispatcher.Layer1 而非 Layer2）；
//   - OnSelectPID/OnTick 仅在 IsActive()==true 时才被 App Model 调用（spec § 04 风险 2）。
//
// 生命周期顺序（model_test.go::TestOverlayModel_LifecycleOrder 契约）：
//   OnEnter → IsActive==true → ...interact... → OnExit → IsActive==false
//
// Story 38-5 PR11 Step 4(b) Phase 1：OnSelectPID 加入 OverlayModel 接口让 3
// Overlay 与 8 PaneModel 在 broadcastSelectPID 通道下统一处理（spec § AC11
// 硬约束「11 个 hook」）。当前 3 Overlay 实现为 nil-safe stub · Phase 2 迁入主体。
type OverlayModel interface {
	tea.Model
	IsActive() bool
	OnEnter() tea.Cmd
	OnExit() tea.Cmd
	OnSelectPID(pid types.PID) tea.Cmd
}

// PaneState 是所有 PaneModel 共享的最小 base struct。
//
// 字段语义：
//   - PID/UUID: 当前 attached 进程；OnSelectPID 应同步本字段；
//   - Width/Height: pane 渲染尺寸（lipgloss column / row 数）；
//   - Active: 当前是否为 activePane（影响 cursor 颜色 / focus 边框）。
//
// 子 Model 可嵌入 PaneState 复用基础字段，也可仅声明自有字段而不嵌入（视字段冲突情况）。
// 38-5 PR1 阶段不强制嵌入，留给各子 Model 在 PR2-PR9 自行决定。
type PaneState struct {
	PID    types.PID
	UUID   string
	Width  int
	Height int
	Active bool
}
