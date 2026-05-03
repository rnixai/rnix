package main

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/debug"
	"github.com/rnixai/rnix/internal/dashboard/heatmap"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"

	tea "charm.land/bubbletea/v2"
)

// --- Heatmap logic (Story 17-3) ---
//
// Story 38-5 PR3 Step 1: 全部 helper 迁出至 internal/dashboard/heatmap，cmd/rnix 端保留 thin wrapper
// 让旧 caller / 测试 grep 字符串零变化。详见 internal/dashboard/heatmap/builder.go。

// segmentKindLabel — thin wrapper · 见 internal/dashboard/heatmap.SegmentKindLabel
//
//nolint:unused // 保留供潜在外部 caller / 测试 grep 使用
func segmentKindLabel(kind segmentKind) string {
	return heatmap.SegmentKindLabel(kind)
}

// activityLabel — thin wrapper · 见 internal/dashboard/heatmap.ActivityLabel
//
//nolint:unused // 保留供潜在外部 caller / 测试 grep 使用
func activityLabel(a activityLevel) string {
	return heatmap.ActivityLabel(a)
}

// dim — thin wrapper · 见 internal/dashboard/heatmap.Dim
//
//nolint:unused // 保留供潜在外部 caller 使用
func dim(hexColor string) string {
	return heatmap.Dim(hexColor)
}

// segmentColor — thin wrapper · 见 internal/dashboard/heatmap.SegmentColor
func segmentColor(kind segmentKind, activity activityLevel) string {
	return heatmap.SegmentColor(kind, activity)
}

// mapConsumerKind — thin wrapper · 见 internal/dashboard/heatmap.MapConsumerKind
//
// 保留供 dashboard_test.go::17.3-UNIT-012 / CR-FIX-004 测试调用。
func mapConsumerKind(kind string) segmentKind {
	return heatmap.MapConsumerKind(kind)
}

// buildHeatmapSegments — thin wrapper · 见 internal/dashboard/heatmap.BuildSegments
//
// heatmapSegment 是 heatmap.Segment 的 type alias（见 dashboard_types.go），可直接返回 BuildSegments
// 的结果而无需类型转换。
func buildHeatmapSegments(profile *debug.CtxProfileResult) []heatmapSegment {
	return heatmap.BuildSegments(profile)
}

func fetchHeatmapCmd(pid types.PID) tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return heatmapProfileMsg{err: err}
		}
		defer client.Close()
		profile, err := client.CtxProfile(pid)
		return heatmapProfileMsg{profile: profile, err: err}
	}
}

// handleHeatmapPIDChange — thin wrapper · Story 38-5 PR3 Step 3
//
// 委托至 internal/dashboard/heatmap.HandlePIDChange 处理 PID 切换的状态清理。
// 行为零变化（与 PR3 Step 1 落地一致）。
func (m dashboardModel) handleHeatmapPIDChange() dashboardModel {
	if m.selectedPID == m.heatmap.PID {
		return m
	}
	m.heatmap = heatmap.HandlePIDChange(m.heatmap)
	return m
}

// handleHeatmapKey — thin wrapper · Story 38-5 PR3 Step 3
//
// 委托至 internal/dashboard/heatmap.HandleKey 处理 pane-specific 键位（j/k 导航 / enter
// 切 expanded / "/" 提示不支持搜索）。statusMsg 设置 + TTL 由 cmd/rnix 端补齐
// （HandleKey 仅返回 statusMsg 文本，状态消息生命周期管理仍属 cmd/rnix 端职责）。
func (m dashboardModel) handleHeatmapKey(key string) dashboardModel {
	newState, statusMsg, _ := heatmap.HandleKey(m.heatmap, key)
	m.heatmap = newState
	if statusMsg != "" {
		m.statusMsg = statusMsg
		m.statusMsgTTL = statusMsgDefaultTTL
	}
	return m
}

// renderHeatmapPane — thin wrapper · Story 38-5 PR3 Step 3
//
// 渲染主体迁出至 internal/dashboard/heatmap.Render（371 行实现）。
// cmd/rnix 端 wrapper 负责：
//  1. 计算 isActive + borderColor（依赖 m.activePane / paneHeatmap，属 cmd/rnix 内部状态）
//  2. 调用 heatmap.Render(state, ctx, innerW, innerH) 取得 inner content
//  3. 用 renderFixedPanel(content, width, height, borderColor) 包裹外层 border
//
// 包边界考量：renderFixedPanel + paneHeatmap 常量是 cmd/rnix 端 helper，迁入 internal 包
// 会引入额外类型。本阶段 wrapper 模式与 PR2 Step 3b TreeModel 一致。
func (m dashboardModel) renderHeatmapPane(width, height int) string {
	isActive := m.activePane == paneHeatmap

	borderColor := lipgloss.Color(ui.ColorMuted)
	if isActive {
		borderColor = lipgloss.Color(ui.ColorAgent)
	}

	innerW := max(width-2, 1)

	content := heatmap.Render(m.heatmap, heatmap.RenderContext{
		IsActive:    isActive,
		SelectedPID: m.selectedPID,
	}, innerW, height)

	return renderFixedPanel(content, width, height, borderColor)
}
