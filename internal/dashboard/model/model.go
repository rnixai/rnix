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

// OnTickContext 是 OnTick hook 的 App Model → 子 Model 上下文注入容器（Story 38-5 PR11 Step 4(b) Phase 3）。
//
// 设计动机（spec § 04 风险 6 + ADR-40 § 4.4）：
//   - dashboardTick 内 IPC 编排原本是 god struct 顶层调度（cmd/rnix-private 字段散布在
//     375 行函数体），Phase 3 拆分让各 PaneModel.OnTick 自治触发 IPC，但子 Model 必须
//     知道一些 App Model 顶层状态才能决定是否需要 fetch（如 connected / activePane /
//     tickCount 节流计数 / selectedPID）。直接传 dashboardModel 会形成反向依赖，因此
//     用 struct 封装最小必要上下文，避免子 Model 知晓 dashboardModel 的内部布局。
//   - 字段语义：
//       Now                   — 时间戳（用于过期判断、动画 keyframe）
//       Active                — PaneModel: 是否 == activePane；Overlay: IsActive() 返回值（App Model 已守卫）
//       Connected             — IPC client 是否已连接（fetch 命令仅在 connected 时触发）
//       SelectedPID           — 当前选中进程（PID 复用场景下需配合 SelectedUUID 使用）
//       SelectedUUID          — 当前选中进程 UUID（PID 复用场景下唯一标识 · 28-4 AC-2/3 契约）
//       SelectedProcessIsDead — 当前选中进程是否已 dead（避免对死进程发起 fetch · cmd/rnix 端预先 lookup）
//       TickCount             — App Model 周期计数器（用于子 Model 节流：TickCount%5==0 → 5 秒一次）
//       ViewMode              — 当前 viewMode（int 形式 · 子 Model 不知道 cmd/rnix.viewMode 类型 · 通过 int cast 解耦 · 与 InspectorState.PrevMode 同模式）
//       SocketPath            — Unix domain socket 路径（PaneModel.OnTick 内部调 ipc.Dial(SocketPath) 创建临时 client · 与 cmd/rnix.fetchXxxCmd 「每次 fetch 新建 + 关闭 client」行为字面等价 · 避免 client 共享引用的 race condition · 空字符串时 PaneModel 应跳过 fetch）
//
// 不在 ctx 中的字段（按 spec § 04 风险 6 决策保留 App Model）：
//   - processes []vfs.ProcInfo — EventStream 跨 pane 共享 · 各子 Model 通过 OnSelectPID hook 间接消费
//   - errorCount/warnCount — Title bar 顶层渲染 · 不属于任何 PaneModel
//   - sysEvents/unifiedEvents — EventStream 字段保留 App Model（spec § Decision 6）
//
// nil 安全：OnTickContext 是值类型 · zero value (`OnTickContext{}`) 是合法的 ctx
// （PaneModel.OnTick 在 ctx 全零值时应返回 nil cmd · 不 panic · SocketPath==""/Connected==false 时跳过 IPC fetch）。
type OnTickContext struct {
	Now                   time.Time
	Active                bool
	Connected             bool
	SelectedPID           types.PID
	SelectedUUID          string
	SelectedProcessIsDead bool
	TickCount             int
	ViewMode              int
	SocketPath            string
}

// PaneModel 是 8 个 Pane 子 Model（Tree/Timeline/Heatmap/Detail/Intent/Security/Trace/Eval）的统一契约。
//
// 实现要求：
//   - 嵌入 tea.Model（Init/Update/View）— Bubble Tea v2 单向数据流；
//   - Name() 返回稳定 pane 名（用于 help overlay 分组与 logging）；
//   - KeyLayer() 返回 38-1 注册到 Dispatcher.Layer2 的 *ui.KeyLayer，**必须** stable
//     across 多次调用（KeyLayer 内部 Bindings/Docs 在 Init 后即固定）；
//   - OnSelectPID(pid) 在 App Model 收到 selectPIDMsg 时被调用，子 Model 同步 cursor /
//     attached PID / 缓存重置等。返回 tea.Cmd 由 App Model 通过 tea.Batch 收集；
//   - OnTick(ctx) 在 dashboardTick 周期内被调用，子 Model 根据 ctx 触发 IPC 拉取 /
//     缓存刷新。Story 38-5 PR11 Step 4(b) Phase 3：签名从 OnTick(now time.Time)
//     扩展为 OnTick(ctx OnTickContext) 让子 Model 自治决定是否 fetch（spec § AC8）。
//     **Code Review #3 落地契约**：OnTick 仅返回 tea.Cmd（fetch trigger），**不得**
//     mutate 子 Model 内部 state；state mutation 须经 IPC msg 异步回流 → Update 路径。
//     当前唯一例外是 DetailModel.OnTick（PR11 历史遗留），cmd/rnix 端通过显式
//     m.detail = m.detailM.State() 回写覆盖该副作用。新 PaneModel 实现禁止复制此模式。
//
// nil 安全：所有方法在 receiver 为 nil 时返回零值（nil cmd / "" / nil layer），不得 panic。
type PaneModel interface {
	tea.Model
	Name() string
	KeyLayer() *ui.KeyLayer
	OnSelectPID(pid types.PID) tea.Cmd
	OnTick(ctx OnTickContext) tea.Cmd
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

// State sync convention（Story 38-5 PR11 Step 4(b) Phase 2 契约 · Code Review #6 落地）.
//
// 所有具体 PaneModel/OverlayModel 实现 **必须** 提供等价于以下签名的两个方法：
//
//	func (m *XxxModel) State() XxxState   // pull internal state out
//	func (m *XxxModel) SetState(s XxxState) // push external state in
//
// 这是 cmd/rnix 端 broadcastSelectPID Phase 2 双向同步 (syncStatesToModels /
// syncStatesFromModels) 依赖的契约。由于 Go 不支持 interface method 泛型，本契约
// 通过 godoc + 各子 Model 测试（TestXxxModel_StateRoundTrip）强制，**不**通过
// PaneModel/OverlayModel interface 体现 — interface 体现的是行为契约（hook 调用），
// State/SetState 是具体类型的同步辅助。
//
// 错误模式：
//   - 实现 SetState 但未实现 State → cmd/rnix 端 syncStatesFromModels 编译失败；
//   - 实现 State 返回 *XxxState 而非 XxxState → 共享指针，broadcast 后修改污染 cmd/rnix 端；
//   - SetState 不做深拷贝（如 map / slice 共享）→ 同样污染。
//
// 当前实现（PR11 落地 + Code Review #6 验证）：
//   - tree, timeline, heatmap, detail, intent, security, trace, eval, inspector, debug, alertstrip
//     11 个具体类型均提供 State()/SetState() 方法（grep -E "^func .*State\(\)" 验证）。
//
// State sync 与 OnTick 路径的隐式契约（Code Review #3 落地）：
//   - **OnTick 不得 mutate 子 Model 内部 state**；state mutation 须经 Update 路径
//     （IPC msg 异步回流 → Update.case → SetState）。
//   - 例外：DetailModel.OnTick 出于历史原因允许在 fetch 触发后立刻 mutate Tick
//     字段，cmd/rnix 端通过显式 m.detail = m.detailM.State() 回写。
//   - 其它 PaneModel.OnTick 仅返回 tea.Cmd（fetch trigger），不 mutate state，
//     因此 cmd/rnix 端 dashboardTick 不需要 OnTick 后 pull state。
