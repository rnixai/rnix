// Package main — dashboard_alerts.go
//
// Story 38-5 PR11 Step 4(a-2) (2026-05-04): All helper bodies migrated to
// internal/dashboard/alertstrip (builder.go + render.go); this file now contains
// only thin wrappers that delegate to the alertstrip package and preserve the
// legacy function names for existing callers.
//
// 行为契约保留（与 PR2/PR3 helper 公开化同模式 · 零行为变更 · pure code motion）：
//   - alertTTL → 现在通过 alertstrip.AlertTTL 公开常量访问（cmd/rnix 端 const alias）
//   - buildAlertEvents → alertstrip.BuildAlertEvents
//   - synthSecurityAlerts → alertstrip.SynthSecurityAlerts
//   - buildAlertEventsWith → alertstrip.BuildAlertEventsWith
//   - alertStripHeight → alertstrip.AlertStripHeight (PR12 Step 2 已迁出)
//   - alertCountBadge → alertstrip.AlertCountBadge
//   - renderAlertStrip → alertstrip.Render(state, width, maxLines) — 解耦
//     dashboardModel 引用 · 仅依赖 AlertStripState
//
// 现有 caller（dashboard.go / dashboard_keylayers.go / dashboard_test.go /
// dashboard_cross_pane_test.go）调用旧函数名不变 · wrapper 保留 cmd/rnix-internal
// 测试 grep 契约。
package main

import (
	"github.com/rnixai/rnix/internal/dashboard/alertstrip"
	"github.com/rnixai/rnix/ipc"
)

// alertTTL re-exports alertstrip.AlertTTL for backwards compatibility with
// existing tests that grep the constant name (Story 38.2 AC#4 contract).
const alertTTL = alertstrip.AlertTTL

// buildAlertEvents is a thin wrapper around alertstrip.BuildAlertEvents.
func buildAlertEvents(events []UnifiedEvent) []UnifiedEvent {
	return alertstrip.BuildAlertEvents(events)
}

// synthSecurityAlerts is a thin wrapper around alertstrip.SynthSecurityAlerts.
func synthSecurityAlerts(alerts []ipc.AlertWire) []UnifiedEvent {
	return alertstrip.SynthSecurityAlerts(alerts)
}

// buildAlertEventsWith is a thin wrapper around alertstrip.BuildAlertEventsWith.
func buildAlertEventsWith(events []UnifiedEvent, securityAlerts []ipc.AlertWire) []UnifiedEvent {
	return alertstrip.BuildAlertEventsWith(events, securityAlerts)
}

// alertStripHeight is a thin wrapper around alertstrip.AlertStripHeight
// (Story 38-5 PR12 Step 2: helper migration; preserved for caller contract).
func alertStripHeight(alertCount int, expanded bool) int {
	return alertstrip.AlertStripHeight(alertCount, expanded)
}

// alertCountBadge is a thin wrapper around alertstrip.AlertCountBadge.
func alertCountBadge(alerts []UnifiedEvent, ascii bool) string {
	return alertstrip.AlertCountBadge(alerts, ascii)
}

// renderAlertStrip is a thin wrapper around alertstrip.Render. The dashboardModel
// pointer parameter is preserved for backwards compatibility with the existing
// View() call site in dashboard.go; internally we extract the AlertStripState
// and delegate.
func renderAlertStrip(m *dashboardModel, width, maxLines int) string {
	return alertstrip.Render(m.alertStrip, width, maxLines)
}
