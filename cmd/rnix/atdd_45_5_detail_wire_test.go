// Story 45.5: Detail pane wire-injection ATDD (epic-45 §AC-EA5 / AC5).
//
// Verifies that cmd/rnix/dashboard_detail.go::renderDetailPane forwards
// m.heartbeatStatus into the new detail.RenderContext.HeartbeatStatus
// field. End-to-end signal: when the dashboard model is populated with a
// HeartbeatStatusResponse whose CurrentStalled[] holds the selected PID,
// the rendered Detail pane must contain the stall section.
//
// Pre-impl red-phase signal: same source as the detail-layer ATDD —
// detail.RenderContext does not yet have HeartbeatStatus field, so the
// wrapper change required by Story 45.5 Task 1.2 (passing
// HeartbeatStatus: m.heartbeatStatus through to detail.RenderContext)
// cannot be in place yet. The wire path is broken → no "Stall" string
// in output → test fails.
package main

import (
	"strings"
	"testing"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
)

// TestATDD_45_5_006_RenderDetailPaneWiresHeartbeat asserts the thin
// wrapper renderDetailPane plumbs dashboardModel.heartbeatStatus into
// detail.RenderContext.HeartbeatStatus. Uses a level-4 (suspend) stall
// fixture so the output contains both the section header and the
// terminal-stage cues ("4/4" / "would suspend").
func TestATDD_45_5_006_RenderDetailPaneWiresHeartbeat(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())

	pid := types.PID(42)
	uuid := "abc12345-xxxx"

	// Selection state — the dashboardModel must consider PID 42 active so
	// renderDetailPane forwards it through RenderContext.SelectedPID.
	m.selectedPID = pid
	m.selectedUUID = uuid
	m.activePane = paneDetail

	// Detail cache — without a matching Detail entry the Loading guard in
	// detail.Render would short-circuit before reaching the stall section.
	m.detail.Detail = &ipc.GetProcDetailResponse{
		PID:      pid,
		UUID:     uuid,
		State:    "running",
		Provider: "claude",
		Model:    "sonnet",
	}

	// Stall data — the actual subject of the wire test.
	m.heartbeatStatus = &ipc.HeartbeatStatusResponse{
		Running:              true,
		CheckIntervalMs:      30_000,
		TotalStalledDetected: 1,
		CurrentStalled: []ipc.StalledProcWire{
			{
				PID:               pid,
				UUID:              uuid,
				ConsecutiveStalls: 4,
				StalledDurationMs: 240_000, // 4m0s
				HeartbeatGapMs:    240_000, // 4m0s
				LastAction:        "suspend",
			},
		},
	}

	out := m.renderDetailPane(120, 30)

	wants := []string{
		"Stall",         // section header — proves wire is connected
		"PID 42",        // selected PID echoed in summary
		"4/4",           // level suffix uses clamped value
		"would suspend", // P4-framing prefix on LastAction
		"4m0s",          // 240_000ms via time.Duration.String() (idle / gap)
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("renderDetailPane output missing %q (wire not threaded?); got:\n%s",
				want, out)
		}
	}
}
