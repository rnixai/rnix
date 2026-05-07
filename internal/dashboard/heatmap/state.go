// Package heatmap — state.go (Story 38-5 PR3 Step 1)
//
// HeatmapState 收纳从 dashboardModel 抽离的 7 个 heatmap 相关字段（spec § 01 行 4 + § AC3）。
//
// PR3 阶段的契约：
//   - PR3 Step 1: 仅字段移动 + Segment 类型公开化 + buildHeatmapSegments + helper 迁出，行为零变化；
//   - PR3 Step 2: HeatmapModel.KeyLayer() 抽离（registerLayer2Heatmap 迁出）；
//   - PR3 Step 3: HeatmapModel.View()/Update() 抽离（renderHeatmapPane + handleHeatmapKey 迁出）；
//   - PR11 Commit 4: dashboardModel.HeatmapState() deprecated getter 删除。
//
// 边界说明（spec § 01 行 4 与现状的偏差）：
//   - TickCount 字段在现有代码中虽以 heatmapTickCount 命名但实际承担**全 dashboard 的 tick 计数**职责
//     （cmd/rnix/dashboard.go 中 17 处使用，被 intent / immune / compact / trace / eval / debug 多个 pane
//     用作 mod-5/mod-3 节流）。spec § AC3 line 121 字面要求迁出至 HeatmapState.TickCount，本 PR 严格遵循；
//     其他 pane 通过 m.heatmap.TickCount 跨 pane 访问。PR11 App Model 瘦身阶段可考虑重命名为更通用的
//     dashboardTick 并迁回 App Model（或引入共享 tickPlugin），本 PR 不动。
package heatmap

import (
	"github.com/rnixai/rnix/debug"
	"github.com/rnixai/rnix/internal/types"
)

// HeatmapState 是 HeatmapModel 的状态容器，包含 spec § AC3 列出的 7 字段。
//
// 字段分组：
//   - 数据源（Profile/PID/Segments/Err）— IPC 拉取的 CtxProfileResult + 解析后的可视化片段；
//   - 交互（Cursor/Expanded）— 当前选中片段索引 + 是否展开 detail；
//   - 节流（TickCount）— 全 dashboard tick 节流计数（spec § AC3 line 121 命名 · 见包级 godoc 边界说明）。
type HeatmapState struct {
	// 数据源
	Profile  *debug.CtxProfileResult
	PID      types.PID
	Segments []Segment
	Err      error

	// 交互
	Cursor   int
	Expanded bool

	// 节流（实际全 dashboard tick · 见 state.go 包级 godoc 边界说明）
	TickCount int
}
