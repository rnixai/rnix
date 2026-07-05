// Package timeline — transitions.go (Story 38-5 PR11 Step 4(c))
//
// HandlePIDUUIDChange 把 cmd/rnix.handleTimelinePIDChange 的 timeline 状态重置
// 主体迁出至包内（不含 m.search 跨 plugin 重置 · 由 cmd/rnix wrapper 保留）。
//
// 与 model.go::OnSelectPID 的差异：
//   - OnSelectPID 用 PID 比较（PaneModel 通用接口语义 · 未来 App Model 集成）;
//   - HandlePIDUUIDChange 用 UUID 比较（cmd/rnix 端 dashboardTick 触发 · PID 复用
//     场景下 UUID 才是唯一标识 · 与 cmd/rnix 端历史行为完全等价）;
//   - HandlePIDUUIDChange 额外重置 StepFilterMode（cmd/rnix 端语义：切 PID 退出 filter
//     输入态 · OnSelectPID 不重置因为 PaneModel 接口语境下 filter 是用户偏好级状态）。
//
// 现有 cmd/rnix 端 callsite（迁出后零修改）：
//   - dashboard_timeline.go::handleTimelinePIDChange — wrapper 调本函数 + 重置 m.search
package timeline

import (
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
)

// HandlePIDUUIDChange 在 selectedUUID 变化时重置 timeline 状态。
//
// 行为契约（与 cmd/rnix.handleTimelinePIDChange 等价）：
//   - 若 uuid == state.AttachedUUID：noop（早返回 · 同 UUID 不重置）;
//   - 否则：更新 AttachedPID/AttachedUUID + 清空 9 个 step 字段 + ExpandMode=collapsed;
//   - 不触碰 SortAsc/UIState/MigrationChecked/StepFilters（用户偏好 · 不应跨 PID 重置）.
//
// 字段重置清单（9 项 · 与 cmd/rnix 行历史保持 byte-for-byte 等价）：
//   - StepEntries=nil / StepCursor=0 / StepScrollTop=0
//   - StepDetailCache=空 map（重新分配避免悬挂引用）
//   - LastFetchedStep=0 / FetchingDetail=false
//   - StepFilterMode=false（退出 filter 输入态 · 见包级 godoc 差异说明）
//   - StepExpandedIdx=-1
//   - ExpandedAggGroups=空 map（重新分配）
//   - ExpandMode=ExpandModeCollapsed（Story 36-4 行为 · 按进程作用域）.
//
// 返回新 state（值类型 · 调用方负责赋回）。
func HandlePIDUUIDChange(state TimelineState, pid types.PID, uuid string) TimelineState {
	if uuid == state.AttachedUUID {
		return state
	}
	state.AttachedPID = pid
	state.AttachedUUID = uuid
	state.StepEntries = nil
	state.StepCursor = 0
	state.StepScrollTop = 0
	state.StepDetailCache = make(map[int]*ipc.GetStepDetailResponse)
	state.LastFetchedStep = 0
	state.FetchingDetail = false
	state.StepFilterMode = false
	state.StepExpandedIdx = -1
	state.ExpandedAggGroups = make(map[int]bool)
	state.ExpandedChunkGroups = make(map[int]bool) // spec-timeline-agg-nav-fix
	state.ExpandMode = ExpandModeCollapsed
	return state
}

// SearchResetter 是 SearchPlugin 解耦接口（避免 timeline → plugin 包硬依赖）。
//
// 仅声明 ExitSearchMode 方法（4 字段重置 · 与 cmd/rnix.handleTimelinePIDChange
// 中 m.search.Mode/Query/Matches/MatchIdx = 0 行为字面等价）。
//
// 实际实现由 plugin.SearchPlugin 满足（编译期断言已在 plugin 包内）。
//
// nil safety：HandlePIDUUIDChangeWithSearch 内部判断 nil。
type SearchResetter interface {
	ExitSearchMode()
}

// HandlePIDUUIDChangeWithSearch 在 PID 切换时重置 timeline state + search plugin
// 状态（cmd/rnix.handleTimelinePIDChange wrapper 整体迁出 · Story 38-5 PR11
// Step 4(b) Phase 2）。
//
// 行为契约（与 cmd/rnix.handleTimelinePIDChange 完全等价）：
//  1. prevUUID := state.AttachedUUID
//  2. state = HandlePIDUUIDChange(state, pid, uuid)（timeline state 重置）
//  3. 若 uuid == prevUUID：noop（同 UUID 不重置 search）
//  4. 否则 resetter.ExitSearchMode()（search 4 字段重置 · Story 36-5 P-1
//     per-process 语义 · 与原 cmd/rnix wrapper 字面等价）.
//
// nil safety：resetter 为 nil 时仅做 timeline state 重置，不重置 search.
//
// 调用方：dashboard.go::handlePIDChange（取代原 m.handleTimelinePIDChange()）.
//
// 返回新 state（值类型 · 调用方负责赋回 · search 通过 pointer 修改）。
func HandlePIDUUIDChangeWithSearch(state TimelineState, resetter SearchResetter, pid types.PID, uuid string) TimelineState {
	prevUUID := state.AttachedUUID
	state = HandlePIDUUIDChange(state, pid, uuid)
	if uuid == prevUUID {
		return state
	}
	if resetter != nil {
		resetter.ExitSearchMode()
	}
	return state
}
