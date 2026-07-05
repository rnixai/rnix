// Package timeline — state.go (Story 38-5 PR4 Step 1)
//
// TimelineState 收纳从 dashboardModel 抽离的 16 个 timeline 相关字段（spec § 01 行 3/6/12/20 + § AC4）。
//
// PR4 阶段的契约：
//   - PR4 Step 1: 仅字段移动 + StepEntry / TimelineExpandMode 类型公开化，行为零变化；
//   - PR4 Step 2: TimelineModel.KeyLayer() 抽离（registerLayer2Timeline + handleTimelineKey 迁出）；
//   - PR4 Step 3: TimelineModel.View()/Update() 抽离（renderTimelinePane 等迁出）；
//   - PR4 Step 4: Searchable interface 实现（spec § 04 风险 3 缓解）；
//   - PR11 Commit 4: dashboardModel.TimelineState() deprecated getter 删除。
//
// 边界说明（spec § AC4 line 136 与现状的偏差）：
//   - UIState (*ui.UIState) 字段语义是「全局 UI 持久化状态」（首次升级提示 / 用户偏好等），
//     spec 字面要求迁入 TimelineState（因为目前唯一的 caller 是 maybeShowTimelineMigrationNotice）。
//     PR11 App Model 瘦身阶段可考虑迁回 App Model 或独立 plugin（如未来 ui 包扩展），本 PR 不动。
//   - StepDetailCache (map[int]*ipc.GetStepDetailResponse) 实际承担「Inspector + Timeline 共享缓存」职责
//     （Inspector 也通过该 cache 拉取 step detail），spec 仍要求迁入 TimelineState。本 PR 严格遵循；
//     Inspector 通过 dashboardModel.TimelineState().StepDetailCache 间接访问（与 PR2/PR3 deprecated getter
//     模式一致）。
package timeline

import (
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
)

// TimelineState 是 TimelineModel 的状态容器，包含 spec § AC4 列出的 16 字段。
//
// 字段分组：
//   - Timeline tracking (2):       AttachedPID/AttachedUUID — 当前 attach 的 PID/UUID（PID 切换检测）；
//   - Step timeline (9):           StepEntries/StepCursor/StepScrollTop/StepDetailCache/LastFetchedStep/
//                                  FetchingDetail/StepFilters/StepFilterMode/StepExpandedIdx —
//                                  step list 数据 + cursor + 滚动 + IPC fetch 状态 + filter 状态；
//   - Sort/Expand/UI (4):          SortAsc/ExpandMode/UIState/MigrationChecked — Story 36-4 落地：
//                                  排序方向 + 三态展开模式 + UI 持久化状态 + 首次升级提示标记；
//   - Aggregation (1):             ExpandedAggGroups — Story 30.8 AC#5 落地：聚合组展开状态。
//
// 总计：2 + 9 + 4 + 1 = 16 字段。
type TimelineState struct {
	// Timeline tracking — PID 切换检测（与 dashboardModel.selectedPID 比对）
	AttachedPID  types.PID
	AttachedUUID string

	// Step timeline (unified) — 步骤列表 + cursor + 滚动 + IPC fetch 状态
	StepEntries     []StepEntry
	StepCursor      int
	StepScrollTop   int                              // index in filtered view of first visible item
	StepDetailCache map[int]*ipc.GetStepDetailResponse
	LastFetchedStep int
	FetchingDetail  bool
	StepFilters     map[string]bool
	StepFilterMode  bool
	StepExpandedIdx int

	// Story 36-4 / Sort & Expand / UI persistence
	SortAsc           bool               // true=旧→新（底部最新），session 内持久
	ExpandMode        TimelineExpandMode // 按进程作用域，切 PID 时重置为 collapsed
	UIState           *ui.UIState        // 首次升级提示的持久化状态（spec 字面要求 · 见包级 godoc）
	MigrationChecked  bool               // session 内是否已检查过首次升级提示

	// Story 30.8 AC#5 / Timeline aggregation
	ExpandedAggGroups map[int]bool

	// Story 43-3 review patch P11 / ScriptAggGroup fold UI rendering.
	// Keyed by ScriptAggGroup.StartIdx (filtered-events slice position) so
	// independent of ToolAggGroup's step-number namespace. Per-process,
	// reset on PID switch alongside ExpandedAggGroups (see model.go::Reset).
	ExpandedScriptAggGroups map[int]bool

	// Timeline aggregation (>100 steps) 50-step chunk fold state.
	// **命名空间区别**：与 ExpandedAggGroups 彻底分离——
	//   - ExpandedAggGroups：ToolPath 折叠组，键 = StepNums[0]（步号），
	//     step ≤100 时经 BuildVisibleIndices 驱动 j/k 可见索引导航。
	//   - ExpandedChunkGroups：50-step chunk 聚合组，键 = groupIdx（0..N），
	//     step >100 时经 RenderAggregatedTimeline 渲染 + aggregation 导航分支使用。
	// 两者共用一张 map 会在低步号（0-3）时互相污染（investigation Finding 7），
	// 故拆分为独立字段。Per-process，reset on PID switch (see model.go::Reset)。
	ExpandedChunkGroups map[int]bool
}
