package ui

// ATDD for Story 66.6 — ps ACTIVE column (age + tool-call count) and the
// estimated-usage `~` render guard (AC3/AC5).

import (
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// -----------------------------------------------------------------------------
// 66-6-UI-001 (AC5): FormatActivity renders "age/count", omitting the count when
// zero, and a dash for a zero heartbeat.
// -----------------------------------------------------------------------------
func TestATDD_66_6_UI_001_FormatActivity(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.Local)
	withFixedNow(t, now)

	cases := []struct {
		name  string
		hb    time.Time
		count int
		want  string
	}{
		{"zero heartbeat → dash", time.Time{}, 594, "—"},
		{"count>0 → age/count", now.Add(-3 * time.Second), 594, "3s/594"},
		{"count 0 → age only", now.Add(-5 * time.Minute), 0, "5m"},
		{"hours unit", now.Add(-2 * time.Hour), 12, "2h/12"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := FormatActivity(c.hb, c.count, "—"); got != c.want {
				t.Errorf("FormatActivity(%v,%d) = %q, want %q", c.hb, c.count, got, c.want)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// 66-6-UI-002 (AC5): the rendered ps table ACTIVE column carries the tool-call
// count for a running process.
// -----------------------------------------------------------------------------
func TestATDD_66_6_UI_002_ActiveColumnShowsToolCount(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.Local)
	withFixedNow(t, now)
	InitStyles(defaultTestProfile())
	r, buf := testRenderer(defaultTestProfile(), ModeDefault)

	procs := []vfs.ProcInfo{{
		PID: 1, State: types.StateRunning, Intent: "long agentic step",
		Skills: []string{"coder"}, TokensUsed: 1200,
		LastHeartbeat: now.Add(-3 * time.Second), ToolCallCount: 594,
		CreatedAt: now.Add(-56 * time.Minute),
	}}
	RenderProcessTable(r, procs, false, false)
	out := buf.String()

	if !strings.Contains(out, "ACTIVE") {
		t.Fatal("expected ACTIVE column header")
	}
	if !strings.Contains(out, "3s/594") {
		t.Errorf("AC5 FAIL: ACTIVE 列未显示 age/toolcount，got:\n%s", out)
	}
}

// -----------------------------------------------------------------------------
// 66-6-UI-003 (AC3): the TOKENS column prefixes "~" ONLY when provenance is
// estimated — the guard against estimated usage masquerading as a real count.
// cli_stream / api_response / empty must NOT get the prefix.
// -----------------------------------------------------------------------------
func TestATDD_66_6_UI_003_EstimatedTokensGetTildeGuard(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.Local)
	withFixedNow(t, now)
	InitStyles(defaultTestProfile())

	render := func(prov string) string {
		r, buf := testRenderer(defaultTestProfile(), ModeDefault)
		procs := []vfs.ProcInfo{{
			PID: 1, State: types.StateRunning, Intent: "x", Skills: []string{"s"},
			TokensUsed: 999, UsageProvenance: prov,
			LastHeartbeat: now.Add(-1 * time.Second), CreatedAt: now.Add(-time.Minute),
		}}
		RenderProcessTable(r, procs, false, false)
		return buf.String()
	}

	if est := render(vfs.UsageProvenanceEstimated); !strings.Contains(est, "~999") {
		t.Errorf("AC3 FAIL: estimated provenance 应加 ~ 前缀，got:\n%s", est)
	}
	if cli := render(vfs.UsageProvenanceCLIStream); strings.Contains(cli, "~999") {
		t.Errorf("AC3 FAIL: cli_stream 不应加 ~ 前缀，got:\n%s", cli)
	}
	if none := render(""); strings.Contains(none, "~999") {
		t.Errorf("AC3 FAIL: 空 provenance 不应加 ~ 前缀，got:\n%s", none)
	}
}
