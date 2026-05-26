package main

// =============================================================================
// Regression test: Detail pane state flicker (spec-detail-pane-state-flicker)
// =============================================================================
//
// Background: dashboardTick 的 detail tick block 必须在 OnTick 之前 push
// m.detail 到 m.detailM。缺 push 时 handleProcDetailResult 写入的最新 Detail
// 会被下一个 tick 末 pull 用 stale state.Detail 覆盖，导致 Detail 面板在
// running / suspended 间反复跳。
//
// 复现条件：
//   1. 第一次 fetch 返回 runningDetail，cache hit 把 state.Detail 设为 runningDetail。
//   2. 进程状态变化，第二次 fetch 返回 suspendedDetail，handleProcDetailResult
//      写 m.detail.Detail = suspendedDetail，但**不**动 m.detailM.state.Detail。
//   3. 下一个 tick: 如果 detail 块不 push，OnTick 看到 state.Detail = runningDetail
//      (stale)，不会更新；tick 末 pull 用 stale state 覆盖 m.detail.Detail，
//      用户看到 detail 跳回 running。
//
// 修复：detail tick block 在 OnTick 之前调 m.detailM.SetState(m.detail) push 同步。

import (
	"testing"

	"github.com/rnixai/rnix/ipc"
)

// TestRegression_DetailPaneStateFlicker_NoStaleOverwrite 验证 detail tick
// block 的 push+pull 双向同步契约：handleProcDetailResult 写入 m.detail.Detail
// 后，紧随的一次 dashboardTick 必须保留新值，不能用 m.detailM 内部 stale
// state.Detail 覆盖。
func TestRegression_DetailPaneStateFlicker_NoStaleOverwrite(t *testing.T) {
	m := newDashboardModel(nil)
	m.connected = true
	m.selectedPID = 3
	m.selectedUUID = "01900000-0000-7000-8000-000000000003"

	// 模拟实际生产流程：用户选中 PID=3 后 dashboardTick pidChanged 分支 emit
	// SelectPIDMsg，runtime 路由到 broadcastSelectPID，它做 syncStatesToModels
	// 把 m.detail.Cache (map) 推到 m.detailM.state.Cache，自此两端 Cache 共享同一
	// map 引用。Detail 字段（指针）仍是各端独立。这一步是必要的"前置仪式" ——
	// 没有它的话第一次 tickDetailPane 就会把 m.detail 整个清空，无法演示
	// 用户报告的"持续显示但状态跳动"现象（用户截图的 Cache 已就位）。
	m, _ = m.broadcastSelectPID(3)

	runningDetail := &ipc.GetProcDetailResponse{
		PID:   3,
		UUID:  m.selectedUUID,
		State: "running",
	}
	suspendedDetail := &ipc.GetProcDetailResponse{
		PID:   3,
		UUID:  m.selectedUUID,
		State: "suspended",
	}

	// 测试直接调用生产代码中抽出的 helper m.tickDetailPane()，避免在测试内
	// 重新实现同步序列 —— 否则即便生产 helper 移除 push，测试自带的 push 也
	// 让测试通过，失去回归保护意义。

	// Step 1: 第一次 fetch 返回 running.
	updated, _ := m.Update(procDetailResultMsg{
		PID:    3,
		UUID:   m.selectedUUID,
		Detail: runningDetail,
	})
	m = updated.(dashboardModel)
	if m.detail.Detail != runningDetail {
		t.Fatalf("step 1: m.detail.Detail = %v, want runningDetail (handleProcDetailResult contract)", m.detail.Detail)
	}

	// Step 2: 一个 tick 经过（cache hit 把 state.Detail 设为 runningDetail）。
	m, _ = m.tickDetailPane()
	if m.detail.Detail != runningDetail {
		t.Fatalf("step 2: m.detail.Detail = %v, want runningDetail after tick", m.detail.Detail)
	}

	// Step 3: 进程切到 suspended，下一次 fetch 写入 suspendedDetail。
	// handleProcDetailResult 写 m.detail.Detail，但不写 m.detailM.state.Detail。
	updated, _ = m.Update(procDetailResultMsg{
		PID:    3,
		UUID:   m.selectedUUID,
		Detail: suspendedDetail,
	})
	m = updated.(dashboardModel)
	if m.detail.Detail != suspendedDetail {
		t.Fatalf("step 3: m.detail.Detail = %v, want suspendedDetail right after Update", m.detail.Detail)
	}

	// Step 4: 下一个 tick。没有 push 时这一步会用 stale state.Detail (=runningDetail)
	// 覆盖 m.detail.Detail，复现用户报告的"running/suspended 来回跳"现象。
	m, _ = m.tickDetailPane()

	if m.detail.Detail != suspendedDetail {
		t.Errorf("regression: m.detail.Detail = %v, want suspendedDetail (push sync must preserve handleProcDetailResult writes)", m.detail.Detail)
	}
	if m.detail.Detail == nil || m.detail.Detail.State != "suspended" {
		t.Errorf("regression: m.detail.Detail.State = %v, want suspended", m.detail.Detail)
	}
}
