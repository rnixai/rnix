package main

// =============================================================================
// ATDD Story 27.8: Dashboard Security Anomaly Panel
// TDD RED PHASE — All tests designed to FAIL until implementation exists
// =============================================================================
//
// Test Strategy:
//   AC-1: paneSecurity constant (=5), Tab cycling % 6
//   AC-2: Immune data fetch via IPC (immuneStatusMsg, dashboardModel fields)
//   AC-3: Alert list rendering (sorted by Deviation desc, type coloring)
//   AC-4: Alert selection (j/k) + Enter PID linkage + process-gone guard
//   AC-5: Security status summary (ok=green, warning=red/yellow)
//   AC-6: Suspended process display
//   AC-7: Immune Daemon not running fallback
//
// Priority: P0 (AC-1,2,3,4), P1 (AC-5,6,7)
// Test Level: Unit (dashboard model + rendering)

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/vfs"
)

// --- helpers ---

// newSecurityModel creates a dashboardModel configured for security pane testing.
func newSecurityModel() dashboardModel {
	m := newDashboardModel(nil)
	m.width = 120
	m.height = 40
	m.connected = true
	m.selectedPID = 1
	m.activePane = paneSecurity
	return m
}

// makeAlerts creates a set of test alerts with varying severity.
func makeAlerts() []ipc.AlertWire {
	return []ipc.AlertWire{
		{
			PID:           5,
			AgentTemplate: "code-analyst",
			Type:          "syscall_freq",
			Detail:        "Open frequency 12.0 is 5.0x baseline 2.4",
			Deviation:     5.0,
			TimestampMs:   1700000005000,
		},
		{
			PID:           8,
			AgentTemplate: "data-miner",
			Type:          "device_access",
			Detail:        "/dev/fs unauthorized access",
			Deviation:     8.5,
			TimestampMs:   1700000003000,
		},
		{
			PID:           3,
			AgentTemplate: "summarizer",
			Type:          "token_rate",
			Detail:        "Token rate 150.0 tok/s exceeds baseline 30.0",
			Deviation:     3.2,
			TimestampMs:   1700000001000,
		},
	}
}

// makeImmuneStatusOK creates an ImmuneStatusResponse with no alerts.
func makeImmuneStatusOK() *ipc.ImmuneStatusResponse {
	return &ipc.ImmuneStatusResponse{
		Running:        true,
		UptimeMs:       8100000, // ~2h15m
		ProfileCount:   3,
		ActivePIDs:     []uint64{1, 2, 3},
		SuspendedPIDs:  nil,
		Alerts:         nil,
		ThreatCount:    3,
		SecurityStatus: "ok",
	}
}

// makeImmuneStatusWarning creates an ImmuneStatusResponse with alerts and suspended PIDs.
func makeImmuneStatusWarning() *ipc.ImmuneStatusResponse {
	return &ipc.ImmuneStatusResponse{
		Running:        true,
		UptimeMs:       8100000,
		ProfileCount:   5,
		ActivePIDs:     []uint64{1, 2, 3, 8},
		SuspendedPIDs:  []uint64{5},
		Alerts:         makeAlerts(),
		ThreatCount:    7,
		SecurityStatus: "warning",
	}
}

// makeImmuneStatusNotRunning creates an ImmuneStatusResponse where daemon is not running.
func makeImmuneStatusNotRunning() *ipc.ImmuneStatusResponse {
	return &ipc.ImmuneStatusResponse{
		Running: false,
	}
}

// =============================================================================
// AC-1: paneSecurity constant + Tab cycling
// =============================================================================

// --- AC-1.1: [P0] paneSecurity equals 5 ---
func TestATDD_27_8_AC1_PaneSecurityConstant(t *testing.T) {
	// RED: paneSecurity does not exist yet — will cause compile error
	if paneSecurity != 5 {
		t.Errorf("AC-1: paneSecurity = %d, want 5", paneSecurity)
	}
}

// --- AC-1.2: [P0] Tab cycles through 7 panes (updated for Story 27-9 Trace pane) ---
func TestATDD_27_8_AC1_TabCycles6Panes(t *testing.T) {
	m := newDashboardModel(nil)
	m.width = 120
	m.height = 40
	m.activePane = paneTree // 0

	// Press Tab 7 times and verify full cycle back to paneTree
	expectedOrder := []paneType{paneTimeline, paneHeatmap, paneDetail, paneIntent, paneSecurity, paneTrace, paneTree}
	for i, expected := range expectedOrder {
		m2, _ := m.Update(tea.KeyPressMsg{Code: '\t'})
		model := m2.(dashboardModel)
		if model.activePane != expected {
			t.Errorf("AC-1: Tab press %d: activePane = %d, want %d", i+1, model.activePane, expected)
		}
		m = model
	}
}

// --- AC-1.3: [P0] Security pane border highlights when active ---
func TestATDD_27_8_AC1_SecurityPaneBorderHighlight(t *testing.T) {
	m := newSecurityModel()
	m.immuneStatus = makeImmuneStatusOK()

	output := m.renderSecurityPane(60, 20)

	if output == "" {
		t.Fatal("AC-1: renderSecurityPane returned empty string")
	}
}

// --- AC-1.4: [P1] Status bar shows security pane help ---
func TestATDD_27_8_AC1_StatusBarSecurityHelp(t *testing.T) {
	m := newSecurityModel()
	m.activePane = paneSecurity

	output := m.renderDashboardStatus()

	if !strings.Contains(output, "Navigate") || !strings.Contains(output, "Enter") {
		t.Error("AC-1: security pane status help should mention Navigate and Enter")
	}
}

// =============================================================================
// AC-2: Immune data fetch
// =============================================================================

// --- AC-2.1: [P0] dashboardModel has immune/security fields ---
func TestATDD_27_8_AC2_ModelHasSecurityFields(t *testing.T) {
	m := newSecurityModel()

	// RED: these fields do not exist yet
	if m.immuneStatus != nil {
		t.Error("AC-2: immuneStatus should be nil initially")
	}
	if m.immuneErr != nil {
		t.Error("AC-2: immuneErr should be nil initially")
	}
	if m.securityAlerts != nil {
		t.Error("AC-2: securityAlerts should be nil initially")
	}
	if m.securityCursor != 0 {
		t.Error("AC-2: securityCursor should be 0 initially")
	}
}

// --- AC-2.2: [P0] immuneStatusMsg updates model ---
func TestATDD_27_8_AC2_ImmuneStatusMsgUpdatesModel(t *testing.T) {
	m := newSecurityModel()
	status := makeImmuneStatusWarning()

	msg := immuneStatusMsg{
		status: status,
	}

	m2, _ := m.Update(msg)
	model := m2.(dashboardModel)

	if model.immuneStatus == nil {
		t.Fatal("AC-2: immuneStatus should be set after immuneStatusMsg")
	}
	if model.immuneStatus.SecurityStatus != "warning" {
		t.Errorf("AC-2: SecurityStatus = %q, want %q", model.immuneStatus.SecurityStatus, "warning")
	}
	if len(model.securityAlerts) != 3 {
		t.Errorf("AC-2: securityAlerts len = %d, want 3", len(model.securityAlerts))
	}
}

// --- AC-2.3: [P0] immuneStatusMsg with error sets immuneErr ---
func TestATDD_27_8_AC2_ImmuneStatusMsgError(t *testing.T) {
	m := newSecurityModel()

	msg := immuneStatusMsg{
		err: fmt.Errorf("connection refused"),
	}

	m2, _ := m.Update(msg)
	model := m2.(dashboardModel)

	if model.immuneErr == nil {
		t.Error("AC-2: immuneErr should be set on error")
	}
}

// --- AC-2.4: [P0] securityCursor clamped after status refresh ---
func TestATDD_27_8_AC2_CursorClampedAfterRefresh(t *testing.T) {
	m := newSecurityModel()
	// Set up 3 alerts and cursor at last position
	m.immuneStatus = makeImmuneStatusWarning()
	m.securityAlerts = makeAlerts()
	m.securityCursor = 2 // at last alert

	// Simulate refresh with fewer alerts (only 1 alert now)
	smallStatus := &ipc.ImmuneStatusResponse{
		Running:        true,
		SecurityStatus: "warning",
		Alerts: []ipc.AlertWire{
			{PID: 1, Type: "syscall_freq", Deviation: 2.0},
		},
	}
	msg := immuneStatusMsg{status: smallStatus}

	m2, _ := m.Update(msg)
	model := m2.(dashboardModel)

	if model.securityCursor >= len(model.securityAlerts) {
		t.Errorf("AC-2: securityCursor %d out of range (alerts len=%d)",
			model.securityCursor, len(model.securityAlerts))
	}
}

// =============================================================================
// AC-3: Alert list rendering
// =============================================================================

// --- AC-3.1: [P0] Alerts sorted by Deviation descending ---
func TestATDD_27_8_AC3_AlertsSortedByDeviation(t *testing.T) {
	m := newSecurityModel()
	status := makeImmuneStatusWarning()

	msg := immuneStatusMsg{status: status}
	m2, _ := m.Update(msg)
	model := m2.(dashboardModel)

	if len(model.securityAlerts) < 2 {
		t.Fatal("AC-3: need at least 2 alerts for sort test")
	}

	// Verify descending order by Deviation
	for i := 1; i < len(model.securityAlerts); i++ {
		if model.securityAlerts[i].Deviation > model.securityAlerts[i-1].Deviation {
			t.Errorf("AC-3: alerts not sorted by Deviation desc: [%d]=%f > [%d]=%f",
				i, model.securityAlerts[i].Deviation,
				i-1, model.securityAlerts[i-1].Deviation)
		}
	}

	// The highest deviation (8.5, device_access) should be first
	if model.securityAlerts[0].Deviation != 8.5 {
		t.Errorf("AC-3: first alert Deviation = %f, want 8.5", model.securityAlerts[0].Deviation)
	}
}

// --- AC-3.2: [P0] Alert type color mapping ---
func TestATDD_27_8_AC3_AlertTypeColor(t *testing.T) {
	tests := []struct {
		alertType string
		want      lipgloss.Color
	}{
		{"syscall_freq", lipgloss.Color("220")},   // yellow
		{"token_rate", lipgloss.Color("208")},      // orange
		{"device_access", lipgloss.Color("196")},   // red
		{"unknown_type", lipgloss.Color("240")},    // gray default
	}

	for _, tt := range tests {
		got := alertTypeColor(tt.alertType)
		if got != tt.want {
			t.Errorf("AC-3: alertTypeColor(%q) = %v, want %v", tt.alertType, got, tt.want)
		}
	}
}

// --- AC-3.3: [P0] renderSecurityPane shows alert details ---
func TestATDD_27_8_AC3_RenderSecurityPane_AlertDetails(t *testing.T) {
	m := newSecurityModel()
	m.immuneStatus = makeImmuneStatusWarning()
	m.securityAlerts = sortAlertsByDeviation(makeAlerts())

	output := m.renderSecurityPane(80, 30)

	// Should contain PID numbers
	if !strings.Contains(output, "5") || !strings.Contains(output, "8") {
		t.Error("AC-3: render should contain alert PIDs")
	}

	// Should contain agent template names
	if !strings.Contains(output, "code-analyst") || !strings.Contains(output, "data-miner") {
		t.Error("AC-3: render should contain agent template names")
	}

	// Should contain alert types
	if !strings.Contains(output, "syscall_freq") || !strings.Contains(output, "device_access") {
		t.Error("AC-3: render should contain alert type names")
	}

	// Should contain relative timestamps ("ago")
	if !strings.Contains(output, "ago") {
		t.Error("AC-3: render should contain alert timestamps (relative time)")
	}
}

// --- AC-3.4: [P0] renderSecurityPane with empty alerts ---
func TestATDD_27_8_AC3_RenderSecurityPane_EmptyAlerts(t *testing.T) {
	m := newSecurityModel()
	m.immuneStatus = makeImmuneStatusOK()
	m.securityAlerts = nil

	output := m.renderSecurityPane(80, 20)

	if output == "" {
		t.Fatal("AC-3: renderSecurityPane should not return empty with OK status")
	}
}

// =============================================================================
// AC-4: Alert selection + PID linkage
// =============================================================================

// --- AC-4.1: [P0] j/k moves securityCursor ---
func TestATDD_27_8_AC4_JK_MovesSecurityCursor(t *testing.T) {
	m := newSecurityModel()
	m.immuneStatus = makeImmuneStatusWarning()
	m.securityAlerts = sortAlertsByDeviation(makeAlerts())
	m.securityCursor = 0

	// Press 'j' to move down
	m2, _ := m.Update(tea.KeyPressMsg{Code: 'j'})
	model := m2.(dashboardModel)
	if model.securityCursor != 1 {
		t.Errorf("AC-4: after j, securityCursor = %d, want 1", model.securityCursor)
	}

	// Press 'k' to move back up
	m3, _ := model.Update(tea.KeyPressMsg{Code: 'k'})
	model2 := m3.(dashboardModel)
	if model2.securityCursor != 0 {
		t.Errorf("AC-4: after k, securityCursor = %d, want 0", model2.securityCursor)
	}
}

// --- AC-4.2: [P0] Enter on alert links to process ---
func TestATDD_27_8_AC4_Enter_LinksToProcess(t *testing.T) {
	m := newSecurityModel()
	m.immuneStatus = makeImmuneStatusWarning()
	m.securityAlerts = sortAlertsByDeviation(makeAlerts())
	// Add process with PID=8 (the device_access alert, sorted first by deviation)
	m.processes = []vfs.ProcInfo{{PID: 8}, {PID: 5}, {PID: 3}}
	m.securityCursor = 0 // first alert = device_access (PID=8, deviation 8.5)

	m2, _ := m.Update(tea.KeyPressMsg{Code: '\r'}) // Enter
	model := m2.(dashboardModel)

	if model.selectedPID != types.PID(8) {
		t.Errorf("AC-4: after Enter, selectedPID = %d, want 8", model.selectedPID)
	}
	if model.activePane != paneTimeline {
		t.Errorf("AC-4: after Enter, activePane = %d, want paneTimeline (%d)", model.activePane, paneTimeline)
	}
}

// --- AC-4.3: [P0] Enter on alert where process is gone shows status message ---
func TestATDD_27_8_AC4_Enter_ProcessGone_ShowsMessage(t *testing.T) {
	m := newSecurityModel()
	m.immuneStatus = makeImmuneStatusWarning()
	m.securityAlerts = sortAlertsByDeviation(makeAlerts())
	// Only PID 3 exists; PID 8 (first alert by deviation) is gone
	m.processes = []vfs.ProcInfo{{PID: 3}}
	m.securityCursor = 0 // first alert = device_access (PID=8)
	prevPID := m.selectedPID

	m2, _ := m.Update(tea.KeyPressMsg{Code: '\r'}) // Enter
	model := m2.(dashboardModel)

	// Should NOT change selectedPID
	if model.selectedPID != prevPID {
		t.Errorf("AC-4: Enter on reaped process should not change selectedPID, got %d", model.selectedPID)
	}
	// Should show status message about process not existing
	if model.statusMsg == "" {
		t.Error("AC-4: Enter on reaped process should set statusMsg")
	}
}

// --- AC-4.4: [P0] j/k does not go out of bounds ---
func TestATDD_27_8_AC4_CursorBounds(t *testing.T) {
	m := newSecurityModel()
	m.immuneStatus = makeImmuneStatusWarning()
	m.securityAlerts = sortAlertsByDeviation(makeAlerts())
	m.securityCursor = 0

	// Press 'k' at cursor=0 → should stay at 0
	m2, _ := m.Update(tea.KeyPressMsg{Code: 'k'})
	model := m2.(dashboardModel)
	if model.securityCursor != 0 {
		t.Errorf("AC-4: k at cursor=0 should stay 0, got %d", model.securityCursor)
	}

	// Move cursor to last alert
	lastIdx := len(m.securityAlerts) - 1
	model.securityCursor = lastIdx

	// Press 'j' at last position → should stay
	m3, _ := model.Update(tea.KeyPressMsg{Code: 'j'})
	model2 := m3.(dashboardModel)
	if model2.securityCursor != lastIdx {
		t.Errorf("AC-4: j at last position should stay %d, got %d", lastIdx, model2.securityCursor)
	}
}

// =============================================================================
// AC-5: Security status summary
// =============================================================================

// --- AC-5.1: [P1] OK status shows green message ---
func TestATDD_27_8_AC5_OKStatus_GreenMessage(t *testing.T) {
	m := newSecurityModel()
	m.immuneStatus = makeImmuneStatusOK()
	m.securityAlerts = nil

	output := m.renderSecurityPane(80, 20)

	// Should contain OK/normal indicator
	if !strings.Contains(output, "ok") && !strings.Contains(output, "OK") && !strings.Contains(output, "正常") {
		t.Error("AC-5: OK status should show normal/ok indicator")
	}
}

// --- AC-5.2: [P0] securityStatusColor returns correct colors ---
func TestATDD_27_8_AC5_SecurityStatusColor(t *testing.T) {
	tests := []struct {
		status string
		want   lipgloss.Color
	}{
		{"ok", lipgloss.Color("42")},       // green
		{"warning", lipgloss.Color("196")},  // red
		{"unknown", lipgloss.Color("240")},  // gray default
	}

	for _, tt := range tests {
		got := securityStatusColor(tt.status)
		if got != tt.want {
			t.Errorf("AC-5: securityStatusColor(%q) = %v, want %v", tt.status, got, tt.want)
		}
	}
}

// --- AC-5.3: [P1] Warning status shows alert count ---
func TestATDD_27_8_AC5_WarningStatus_ShowsAlertCount(t *testing.T) {
	m := newSecurityModel()
	m.immuneStatus = makeImmuneStatusWarning()
	m.securityAlerts = sortAlertsByDeviation(makeAlerts())

	output := m.renderSecurityPane(80, 20)

	// Should contain alert count or "warning"
	if !strings.Contains(output, "alert") && !strings.Contains(output, "warning") {
		t.Error("AC-5: warning status should mention alerts or warning")
	}
}

// --- AC-5.4: [P1] OK status shows uptime and threat count ---
func TestATDD_27_8_AC5_OKStatus_UptimeAndThreats(t *testing.T) {
	m := newSecurityModel()
	m.immuneStatus = makeImmuneStatusOK()
	m.securityAlerts = nil

	output := m.renderSecurityPane(80, 20)

	// Should show uptime (2h15m from 8100000ms)
	if !strings.Contains(output, "2h") {
		t.Error("AC-5: OK status should show daemon uptime")
	}
	// Should show threat count
	if !strings.Contains(output, "3") {
		t.Error("AC-5: OK status should show threat count")
	}
}

// =============================================================================
// AC-6: Suspended process display
// =============================================================================

// --- AC-6.1: [P1] Suspended PIDs shown in security pane ---
func TestATDD_27_8_AC6_SuspendedPIDs_Shown(t *testing.T) {
	m := newSecurityModel()
	m.immuneStatus = makeImmuneStatusWarning() // has SuspendedPIDs: [5]
	m.securityAlerts = sortAlertsByDeviation(makeAlerts())

	output := m.renderSecurityPane(80, 30)

	// Should contain SUSPENDED section
	if !strings.Contains(output, "SUSPENDED") && !strings.Contains(output, "suspended") {
		t.Error("AC-6: should show SUSPENDED section when SuspendedPIDs non-empty")
	}
	// Should contain the suspended PID
	if !strings.Contains(output, "5") {
		t.Error("AC-6: should show suspended PID 5")
	}
}

// --- AC-6.2: [P1] No suspended section when SuspendedPIDs empty ---
func TestATDD_27_8_AC6_NoSuspendedSection_WhenEmpty(t *testing.T) {
	m := newSecurityModel()
	m.immuneStatus = makeImmuneStatusOK() // SuspendedPIDs is nil
	m.securityAlerts = nil

	output := m.renderSecurityPane(80, 20)

	if strings.Contains(output, "SUSPENDED") {
		t.Error("AC-6: should not show SUSPENDED section when no suspended PIDs")
	}
}

// =============================================================================
// AC-7: Immune Daemon not running
// =============================================================================

// --- AC-7.1: [P1] Daemon not running shows fallback message ---
func TestATDD_27_8_AC7_DaemonNotRunning_ShowsFallback(t *testing.T) {
	m := newSecurityModel()
	m.immuneStatus = makeImmuneStatusNotRunning()
	m.securityAlerts = nil

	output := m.renderSecurityPane(80, 20)

	// Should contain not-running message
	if !strings.Contains(output, "Immune") && !strings.Contains(output, "不可用") && !strings.Contains(output, "not running") {
		t.Error("AC-7: should show Immune Daemon not running message")
	}
}

// --- AC-7.2: [P1] Daemon not running + j/k does not panic ---
func TestATDD_27_8_AC7_DaemonNotRunning_NavigationSafe(t *testing.T) {
	m := newSecurityModel()
	m.immuneStatus = makeImmuneStatusNotRunning()
	m.securityAlerts = nil
	m.securityCursor = 0

	// j/k/Enter on empty security state should not panic
	for _, key := range []rune{'j', 'k', '\r'} {
		m2, _ := m.Update(tea.KeyPressMsg{Code: key})
		_ = m2.(dashboardModel)
	}
}

// --- AC-7.3: [P1] immuneStatus nil renders gracefully ---
func TestATDD_27_8_AC7_NilImmuneStatus_Renders(t *testing.T) {
	m := newSecurityModel()
	m.immuneStatus = nil // not fetched yet
	m.securityAlerts = nil

	output := m.renderSecurityPane(80, 20)

	if output == "" {
		t.Fatal("AC-7: renderSecurityPane with nil immuneStatus should not return empty")
	}
}

// =============================================================================
// AC-3: sortAlertsByDeviation helper
// =============================================================================

// --- AC-3.5: [P0] sortAlertsByDeviation returns descending order ---
func TestATDD_27_8_AC3_SortAlertsByDeviation(t *testing.T) {
	alerts := makeAlerts()
	sorted := sortAlertsByDeviation(alerts)

	if len(sorted) != 3 {
		t.Fatalf("AC-3: sorted len = %d, want 3", len(sorted))
	}

	// Verify descending order
	for i := 1; i < len(sorted); i++ {
		if sorted[i].Deviation > sorted[i-1].Deviation {
			t.Errorf("AC-3: sort not descending: [%d].Deviation=%f > [%d].Deviation=%f",
				i, sorted[i].Deviation, i-1, sorted[i-1].Deviation)
		}
	}
}

// =============================================================================
// AC-5: formatUptimeShort helper
// =============================================================================

// --- AC-5.5: [P1] formatUptimeShort formats correctly ---
func TestATDD_27_8_AC5_FormatUptimeShort(t *testing.T) {
	tests := []struct {
		ms   int64
		want string
	}{
		{30000, "30s"},          // 30 seconds
		{90000, "1m30s"},       // 1.5 minutes
		{8100000, "2h15m"},     // 2 hours 15 minutes
	}

	for _, tt := range tests {
		got := formatUptimeShort(tt.ms)
		if got != tt.want {
			t.Errorf("AC-5: formatUptimeShort(%d) = %q, want %q", tt.ms, got, tt.want)
		}
	}
}

// =============================================================================
// AC-3: formatTimeAgo helper
// =============================================================================

// --- AC-3.6: [P1] formatTimeAgo formats relative time correctly ---
func TestATDD_27_8_AC3_FormatTimeAgo(t *testing.T) {
	now := time.Now().UnixMilli()

	tests := []struct {
		name string
		ms   int64
		want string // substring match
	}{
		{"30 seconds ago", now - 30_000, "s ago"},
		{"5 minutes ago", now - 300_000, "m ago"},
		{"2 hours ago", now - 7_200_000, "h ago"},
		{"3 days ago", now - 259_200_000, "d ago"},
		{"zero timestamp", 0, ""},
		{"negative timestamp", -1, ""},
	}

	for _, tt := range tests {
		got := formatTimeAgo(tt.ms)
		if tt.want == "" {
			if got != "" {
				t.Errorf("AC-3: formatTimeAgo(%s) = %q, want empty", tt.name, got)
			}
			continue
		}
		if !strings.Contains(got, tt.want) {
			t.Errorf("AC-3: formatTimeAgo(%s) = %q, want containing %q", tt.name, got, tt.want)
		}
	}
}

// =============================================================================
// Scroll offset tests
// =============================================================================

// --- AC-3.7: [P1] securityAdjustScroll keeps cursor visible ---
func TestATDD_27_8_SecurityAdjustScroll(t *testing.T) {
	m := newSecurityModel()
	m.height = 20 // visibleLines = max(20/2-3, 1) = 7
	m.securityScrollOffset = 0
	m.securityCursor = 10

	securityAdjustScroll(&m)

	if m.securityScrollOffset == 0 {
		t.Error("scroll offset should have adjusted for cursor=10")
	}
	// cursor should be within visible range
	visibleLines := max(m.height/2-3, 1)
	if m.securityCursor < m.securityScrollOffset || m.securityCursor >= m.securityScrollOffset+visibleLines {
		t.Errorf("cursor %d not in visible range [%d, %d)",
			m.securityCursor, m.securityScrollOffset, m.securityScrollOffset+visibleLines)
	}
}

// --- AC-3.8: [P1] securityAdjustScroll scrolls up ---
func TestATDD_27_8_SecurityAdjustScroll_Up(t *testing.T) {
	m := newSecurityModel()
	m.height = 20
	m.securityScrollOffset = 5
	m.securityCursor = 2

	securityAdjustScroll(&m)

	if m.securityScrollOffset != 2 {
		t.Errorf("scroll offset = %d, want 2 (should scroll up to cursor)", m.securityScrollOffset)
	}
}

// Ensure unused imports are consumed (build guard).
var (
	_ = sort.Slice
	_ = fmt.Sprintf
	_ = strings.Contains
	_ = lipgloss.Color("")
	_ = types.PID(0)
	_ = ipc.ImmuneStatusResponse{}
	_ = vfs.ProcInfo{}
	_ = time.Now
)
