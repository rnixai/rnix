// Package main — dashboard_detail.go
//
// Story 38-5 PR11 Step 4(c) (2026-05-04): renderDetailPane main body migrated
// to internal/dashboard/detail/render.go (Render(state, ctx, innerW) string ·
// 与 alertstrip Step 4(a-2) 同模式)。本文件保留：
//   - fetchProcDetailCmd: IPC 调用（依赖 ipc.Dial · 属 cmd/rnix 端职责）；
//   - renderDetailPane wrapper: 注入 RenderContext + renderFixedPanel border 包裹；
//   - truncateUUID / truncateStr 改为 thin wrapper 委托 detail.TruncateUUID/Str。
//
// 行为契约保留（与 PR2/PR3/alertstrip helper 公开化同模式 · 零行为变更）：
//   - Story 27-6 detail pane rendering
//   - Story 28-4 AC-4 stale-data guard (UUID-keyed cache validation)
//   - Story 34-5 detail card behaviors
package main

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/dashboard/detail"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"

	tea "charm.land/bubbletea/v2"
)

// --- Detail Pane (Story 27-6) ---

func fetchProcDetailCmd(pid types.PID, uuid string) tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return procDetailResultMsg{PID: pid, UUID: uuid, Err: err}
		}
		defer client.Close()
		resp, err := client.GetProcDetail(pid, uuid)
		return procDetailResultMsg{PID: pid, UUID: uuid, Detail: resp, Err: err}
	}
}

// tickDetailPane 是 dashboardTick 中 Detail pane 同步块的提取版（push + OnTick + pull）。
//
// push+pull 双向同步契约（与 broadcastSelectPID 对称）：handleProcDetailResult
// 在 procDetailResultMsg 到达时写入 m.detail.Detail（cmd/rnix 端字段），但不会
// 直接落到 m.detailM.state.Detail。若没有 push，下一个 tick 末 pull 会用
// stale state.Detail 覆盖刚写入的新值，detail 面板在 fetch 周期上闪烁
// （表现：State / FD / Context 在 running ↔ suspended 间反复跳）。SetState 让
// OnTick 看到 cmd/rnix 端最新 Detail，避免周期性回退到旧值。
//
// 抽成独立方法的目的是让 regression test 直接调用，覆盖整个同步链 —— 测试
// 内嵌复制实现会让"删掉 push 后测试照样过"的情形不可见，重新引入 flicker
// 时无法察觉。
func (m dashboardModel) tickDetailPane() (dashboardModel, tea.Cmd) {
	ctx := m.makeTickCtx(paneDetail)
	m.detailM.SetState(m.detail)
	cmd := m.detailM.OnTick(ctx)
	m.detail = m.detailM.State()
	return m, cmd
}

// renderDetailPane is a thin wrapper around detail.Render (Story 38-5 PR11 Step 4(c)).
//
// cmd/rnix wrapper responsibilities:
//  1. Compute isActive + borderColor (depends on m.activePane / paneDetail · cmd/rnix 端状态)
//  2. Call detail.Render(state, ctx, innerW) for inner content
//  3. Wrap with renderFixedPanel(content, width, height, borderColor) outer border
//
// 与 heatmap pattern 一致 · 与 alertstrip Step 4(a-2) 同模式。
func (m dashboardModel) renderDetailPane(width, height int) string {
	isActive := m.activePane == paneDetail

	borderColor := lipgloss.Color(ui.ColorMuted)
	if isActive {
		borderColor = lipgloss.Color(ui.ColorAgent)
	}

	innerW := max(width-2, 1)

	content := detail.Render(m.detail, detail.RenderContext{
		SelectedPID:     m.selectedPID,
		SelectedUUID:    m.selectedUUID,
		IsActive:        isActive,
		HeartbeatStatus: m.heartbeatStatus,
	}, innerW)

	return renderFixedPanel(content, width, height, borderColor)
}

// truncateUUID is a thin wrapper around detail.TruncateUUID (Story 38-5 PR11 Step 4(c))
// kept here for caller compatibility. Existing tests grep this name.
//
//nolint:unused // 保留供潜在外部 caller / 测试 grep 使用
func truncateUUID(s string) string {
	return detail.TruncateUUID(s)
}

// truncateStr is a thin wrapper around detail.TruncateStr (Story 38-5 PR11 Step 4(c))
// kept here for caller compatibility. After intent render migration the only
// in-package caller went away; helper preserved for ATDD grep contracts and
// potential external callers.
//
//nolint:unused // 保留供潜在外部 caller / 测试 grep 使用
func truncateStr(s string, maxLen int) string {
	return detail.TruncateStr(s, maxLen)
}
