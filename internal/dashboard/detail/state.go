// Package detail — state.go (Story 38-5 PR5 Step 1)
//
// DetailState 收纳从 dashboardModel 抽离的 4 个 detail pane 相关字段
// （spec § AC5 line 151 字面要求 `detailState{Detail/PID/Cache/Tick}`）。
//
// PR5 阶段的契约：
//   - PR5 Step 1: 仅字段移动（4 字段）+ deprecated getter，行为零变化；
//   - PR5 Step 2: DetailModel.KeyLayer() 抽离（registerLayer2Detail 迁出）；
//   - PR5 Step 3: DetailModel.View()/Update() 抽离（renderDetailPane + dashboard_detail_card 迁出）；
//   - PR11 Commit 4: dashboardModel.DetailState() deprecated getter 删除。
//
// 字段语义说明（来自 cmd/rnix/dashboard.go:125-129 行 27-6 落地）：
//   - Detail:  最近一次 IPC GetProcDetail 返回的快照（仅当对应的 selectedPID + selectedUUID 命中时）；
//              用于 Detail pane 渲染 + Detail card 渲染（dashboard_detail_card.go）。
//   - PID:     Detail 字段对应的 PID（用作"是否需要重新 fetch"的判定依据）。
//   - Cache:   按 selectedUUID 索引的全部 GetProcDetailResponse 缓存（PID 复用安全 · Story 28-4 AC-4）。
//   - Tick:    全 dashboardTick 内 detail-section 的局部计数器（mod 5 周期刷新缓存 · 见 dashboard.go:872）。
//
// 边界约束（包级）：
//   - 本包**禁止反向依赖** cmd/rnix（go module 边界已强制）；
//   - DetailState 仅持有数据，不持有 IPC client（fetch 由 cmd/rnix wrapper 触发 fetchProcDetailCmd）。
package detail

import (
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
)

// DetailState 是 DetailModel 的状态容器，包含 spec § AC5 line 151 列出的 4 字段。
//
// 字段分组：
//   - 当前快照（Detail/PID）— 最新一次 IPC fetch 命中后的活跃 detail；
//   - 缓存（Cache）— UUID-keyed 历史快照池（PID 复用安全 · 28-4 AC-4 契约保留）；
//   - 节流（Tick）— 全 dashboardTick 中 detail-section 的局部计数器（5 tick 周期刷新）。
//
// 零值语义：
//   - Detail == nil：尚未 fetch / 用户未选中 PID / 选中后 UUID 不一致；
//   - PID == 0：Detail 未关联任何进程；
//   - Cache 为 nil：尚未初始化（构造 dashboardModel 时通过 make 初始化）；
//   - Tick == 0：dashboardTick 尚未推进。
//
// 并发：DetailState 仅在 dashboardModel.Update() 内被读写，由 Bubble Tea 单线程
// 模型保证（无锁）。Cache 是 map，在 dashboard.go 内的所有写入都发生在 Update
// 的 procDetailResultMsg / handlePIDChange 路径上。
type DetailState struct {
	// 当前快照
	Detail *ipc.GetProcDetailResponse
	PID    types.PID

	// 缓存（UUID-keyed · 28-4 AC-4 PID 复用安全）
	Cache map[string]*ipc.GetProcDetailResponse

	// 节流（dashboardTick 周期内的 mod-5 cache refresh 节流计数）
	Tick int
}
