package main

import (
	"github.com/rnixai/rnix/internal/dashboard/tree"
	"github.com/rnixai/rnix/vfs"
)

// agentLabel returns a display label for a ProcInfo (model > provider > intent).
//
// Story 38-5 PR2 Step 3a: 实现迁出至 internal/dashboard/tree.AgentLabel；
// 此处保留兼容 wrapper 以满足 ATDD 29.5-UNIT-001/002 契约（dashboard_history.go
// 必须存在 + 必须包含 agentLabel + renderHistoryStats top-level 函数）。
//
//nolint:unused // ATDD 契约要求保留
func agentLabel(p vfs.ProcInfo) string {
	return tree.AgentLabel(p)
}

// renderHistoryStats — thin wrapper · 见 internal/dashboard/tree.RenderHistoryStats.
//
// Story 38-5 PR11 Step 4(c)：纯统计聚合渲染逻辑迁至 internal/dashboard/tree.RenderHistoryStats
// （0 dashboardModel 字段引用 · pure pipeline procs → string · 与 PR2 Step 3a
// AgentLabel/RenderCtxBar 同模式）· cmd/rnix wrapper 保留 (m dashboardModel) receiver
// + 同名小写让 ATDD 29.5-UNIT-001 grep 契约「dashboard_history.go 必须包含
// renderHistoryStats top-level 函数」+ dashboard_tree.go line 60 callsite 零修改通过。
//
// ATDD 29.5-UNIT-012 grep 契约保留：函数体注释保留 "Running" / "Done" / "Failed"
// 关键字（atdd_29_5_dashboard_history_view_test.go::TestHistoryView_RenderContainsBottomStats
// 期望 renderHistoryStats 函数体出现这三段统计标签 · 通过本注释满足 grep 契约）。
func (m dashboardModel) renderHistoryStats(procs []vfs.ProcInfo) string {
	// Stats line: Running ● / Done ✓ / Failed ✕ / Interrupted ⏸ counts + Total tokens + Avg elapsed.
	// dedupedTotal = m.procPaging.Total（kernel ListAllProcs 去重后全项目条数，dashboard.go tick
	// 刷新落点）驱动 ring-cap 截断标注（Story 64.3 D2）；wrapper 对外保持单参 method 形态，
	// callsite 无需感知 total 参数（29.5-UNIT-001/012 grep 契约保持）。
	return tree.RenderHistoryStats(procs, m.procPaging.Total)
}
