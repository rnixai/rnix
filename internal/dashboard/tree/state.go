// Package tree — state.go (Story 38-5 PR2 Step 1)
//
// TreeState 收纳从 dashboardModel 抽离的 13 个 tree 相关字段（spec § 01 行 2 + 行 24/25）。
//
// PR2 阶段的契约：
//   - PR2 Step 1: 仅字段移动，所有读写仍在 App Model 内部以 m.tree.Rows 形式访问，行为零变化；
//   - PR2 Step 2: TreeModel.KeyLayer() 抽离（registerLayer2Tree 迁出）；
//   - PR2 Step 3: TreeModel.View()/Update() 抽离（renderDashboardTreePane 等迁出）；
//   - PR11 Commit 4: dashboardModel.TreeState() deprecated getter 删除（旧测试一并改写）。
package tree

import (
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// TreeState 是 TreeModel 的状态容器，包含 spec § AC2 列出的 13 字段。
//
// 字段分组：
//   - 基础列表状态（Rows/Cursor/Offset）— 渲染表格主数据 + 光标位置 + 滚动偏移；
//   - 排序状态（SortMode/SortAsc）— 0=Time / 1=PID / 2=State；
//   - Expanded-tree 搜索状态（SearchQuery/SearchMode/SearchCursor/SearchOffset）— viewExpanded+paneTree 才激活；
//   - 跨 pane 联动（LastEventByPID）— Story 34.3 AC5 most-active 标记；
//   - 选择粘性（UserManualSelect）— Story 34.3 AC5；
//   - 折叠状态（CollapsedDeadTrees）— Story 34.3 AC3，key=UUID；
//   - 新进程高亮（ProcessFirstSeenAt）— Story 36-2 first-seen tracking。
type TreeState struct {
	// 基础列表
	Rows   []FlatRow
	Cursor int
	Offset int

	// 排序
	SortMode int  // 0=Time, 1=PID, 2=State
	SortAsc  bool // false=descending (default), true=ascending

	// Expanded-tree search (only active in viewExpanded + paneTree)
	SearchQuery  string
	SearchMode   bool
	SearchCursor int
	SearchOffset int

	// Story 34.3: Process tree visual enhancement
	LastEventByPID     map[types.PID]time.Time // AC5: most active tracking
	UserManualSelect   bool                    // AC5: user manual select flag
	CollapsedDeadTrees map[string]bool         // AC3: dead subtree collapse state (key=UUID)

	// Story 36-2: New process highlight
	ProcessFirstSeenAt map[types.PID]time.Time // tracks when each PID was first seen
}
