// Package main — dashboard_security.go
//
// Story 38-5 PR11 Step 4(c) (2026-05-04 · 第 3 个 pane render 主体迁出): All
// helper bodies + renderSecurityPane main body migrated to
// internal/dashboard/security/render.go. This file now contains only:
//   - fetchImmuneStatusCmd: IPC 调用（依赖 ipc.Dial · 属 cmd/rnix 端职责）
//   - renderSecurityPane wrapper: 注入 RenderContext + renderFixedPanel border 包裹
//   - securityAdjustScroll: 仅依赖 m.height 的 scroll helper（保留在 cmd/rnix）
//   - 5 helper thin wrappers（sortAlertsByDeviation / alertTypeColor /
//     securityStatusColor / formatUptimeShort / formatTimeAgo）
//
// 行为契约保留（与 alertstrip / detail Step 4(a-2)/(c) 同模式 · 零行为变更）：
//   - Story 22-1 Immune Daemon
//   - Story 22-3 Security Status Management
//   - Story 27-8 Security Pane
//   - Story 38-4 Alert Immune routing (synthSecurityAlerts → Security pane)
package main

import (
	"os"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/dashboard/security"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"

	tea "charm.land/bubbletea/v2"
)

// =============================================================================
// Security Pane (Story 27-8)
// =============================================================================

// fetchImmuneStatusCmd — thin wrapper · Story 38-5 PR11 Step 4(b) Phase 3
//
// IPC fetch closure 已迁出至 internal/dashboard/security.FetchImmuneStatusCmd。
// 本 wrapper 保留供 cmd/rnix 端潜在直接调用 · immuneStatusMsg 是 security.ImmuneStatusMsg
// 的 type alias。
//
//nolint:unused // 保留供潜在 caller / 测试 grep（current callers 已迁至 SecurityModel.OnTick）
func fetchImmuneStatusCmd() tea.Cmd {
	return security.FetchImmuneStatusCmd(ipc.SocketPath())
}

// sortAlertsByDeviation — thin wrapper · 见 internal/dashboard/security.SortAlertsByDeviation
//
//nolint:unused // 保留供潜在外部 caller / 测试 grep 使用
func sortAlertsByDeviation(alerts []ipc.AlertWire) []ipc.AlertWire {
	return security.SortAlertsByDeviation(alerts)
}

// alertTypeColor — thin wrapper · 见 internal/dashboard/security.AlertTypeColor
//
//nolint:unused // 保留供潜在外部 caller / 测试 grep 使用
func alertTypeColor(alertType string) lipgloss.Color {
	return security.AlertTypeColor(alertType)
}

// securityStatusColor — thin wrapper · 见 internal/dashboard/security.SecurityStatusColor
//
//nolint:unused // 保留供潜在外部 caller / 测试 grep 使用
func securityStatusColor(status string) lipgloss.Color {
	return security.SecurityStatusColor(status)
}

// formatUptimeShort — thin wrapper · 见 internal/dashboard/security.FormatUptimeShort
//
//nolint:unused // 保留供潜在外部 caller / 测试 grep 使用
func formatUptimeShort(ms int64) string {
	return security.FormatUptimeShort(ms)
}

// formatTimeAgo — thin wrapper · 见 internal/dashboard/security.FormatTimeAgo
func formatTimeAgo(timestampMs int64) string {
	return security.FormatTimeAgo(timestampMs)
}

// renderSecurityPane is a thin wrapper around security.Render (Story 38-5 PR11
// Step 4(c) · 与 alertstrip Step 4(a-2) / detail Step 4(c) 同模式).
//
// cmd/rnix wrapper responsibilities:
//  1. Compute isActive + borderColor (depends on m.activePane / paneSecurity)
//  2. Detect ASCII mode from RNIX_ASCII env var
//  3. Call security.Render(state, ctx, innerH) for inner content
//  4. Wrap with renderFixedPanel(content, width, height, borderColor) outer border
func (m dashboardModel) renderSecurityPane(width, height int) string {
	isActive := m.activePane == paneSecurity

	borderColor := lipgloss.Color(ui.ColorMuted)
	if isActive {
		borderColor = lipgloss.Color(ui.ColorAgent)
	}

	innerH := max(height-2, 1)
	ascii := os.Getenv("RNIX_ASCII") == "1" || os.Getenv("RNIX_ASCII") == "true"

	content := security.Render(m.security, security.RenderContext{
		IsActive: isActive,
		ASCII:    ascii,
	}, innerH)

	return renderFixedPanel(content, width, height, borderColor)
}

// securityAdjustScroll ensures securityCursor is visible within the viewport.
// Kept in cmd/rnix because it depends on m.height (App Model dimension state).
func securityAdjustScroll(m *dashboardModel) {
	visibleLines := max(m.height/2-3, 1)
	if m.security.Cursor < m.security.ScrollOffset {
		m.security.ScrollOffset = m.security.Cursor
	}
	if m.security.Cursor >= m.security.ScrollOffset+visibleLines {
		m.security.ScrollOffset = m.security.Cursor - visibleLines + 1
	}
}
