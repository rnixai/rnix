// Package eval — state.go (Story 38-5 PR9 Step 1)
//
// EvalState 字段抽离自 cmd/rnix/dashboard.go::dashboardModel 的 13 个 eval 字段
// （SubView + Reputation 三子视图 + Topology 三子视图 + Synergy 三子视图各 4 字段）。
//
// 设计原则与 PR2-PR8 同模式（值类型 · 字段公开 · nil 安全）。
//
// **38-4 evalScoreColorStyle 行为契约保留**（关键 · spec § AC5 PR9 验收点）：
//   - EvalState 字段是 38-4 落地的 evalScoreColorStyle 颜色梯度的输入数据；
//   - VS SOLO 颜色 / Recommended Bold / ★ ASCII 标记的渲染路径保持在 cmd/rnix 端不变；
//   - 本 PR 不改 render 主体，仅做字段位置迁移。
package eval

import (
	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/kernel"
)

// EvalState 持有 Eval pane 的完整状态（reputation / topology / synergy 三子视图）。
//
// 字段语义（与原 dashboardModel 的 eval* 字段完全等价）：
//   - SubView：当前子视图（0=reputation, 1=topology, 2=synergy）；
//   - Reputations / RepErr / RepCursor / RepScrollOffset：Reputation 子视图数据；
//   - Topology / TopoErr / TopoCursor / TopoScrollOffset：Topology 子视图数据；
//   - Synergies / SynErr / SynCursor / SynScrollOffset：Synergy 子视图数据。
type EvalState struct {
	SubView int // 0=reputation, 1=topology, 2=synergy

	// Reputation sub-view (Story 27-10 + 38-4 evalScoreColorStyle)
	Reputations     []kernel.ReputationSummary
	RepErr          error
	RepCursor       int
	RepScrollOffset int

	// Topology sub-view
	Topology         *ipc.TopologyQueryResponse
	TopoErr          error
	TopoCursor       int
	TopoScrollOffset int

	// Synergy sub-view (Story 27-10 + 38-4 VS SOLO + Recommended + ★ marker)
	Synergies       []kernel.ComboSummary
	SynErr          error
	SynCursor       int
	SynScrollOffset int
}
