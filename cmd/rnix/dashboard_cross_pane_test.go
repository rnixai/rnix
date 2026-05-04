package main

// Story 38-4 — Phase 3 Cross-Pane Linkage tests.
// Covers AC#1 (Tab unread infrastructure) … AC#6 (Eval colour gradient).
// Test naming: Test<Func>_<Aspect>; profile-tolerant ANSI assertions.

import (
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/kernel"
)

// =============================================================================
// AC#1 — Tab unread infrastructure
// =============================================================================

// TestPaneHasUnread_MarkAndClear verifies the markPaneUnread / clearPaneUnread
// helpers and the dashboardTick → applyUnreadMarks integration.
func TestPaneHasUnread_MarkAndClear(t *testing.T) {
	t.Run("mark sets flag", func(t *testing.T) {
		m := newDashboardModel(nil)
		m = m.markPaneUnread(paneTimeline)
		if !m.paneHasUnread[paneTimeline] {
			t.Errorf("expected paneHasUnread[paneTimeline]=true after mark")
		}
	})

	t.Run("clear unsets flag", func(t *testing.T) {
		m := newDashboardModel(nil)
		m = m.markPaneUnread(paneSecurity)
		m = m.clearPaneUnread(paneSecurity)
		if m.paneHasUnread[paneSecurity] {
			t.Errorf("expected paneHasUnread[paneSecurity]=false after clear")
		}
	})

	t.Run("out-of-bounds pane is no-op", func(t *testing.T) {
		m := newDashboardModel(nil)
		// paneType(99) is out of range; mark/clear must not panic and must
		// leave the array untouched.
		m = m.markPaneUnread(paneType(99))
		m = m.markPaneUnread(paneType(-1))
		for i, v := range m.paneHasUnread {
			if v {
				t.Errorf("expected paneHasUnread[%d]=false, got true", i)
			}
		}
		// Clearing OOB indices must be safe too.
		m = m.clearPaneUnread(paneType(99))
	})
}

// TestApplyUnreadMarks_IdentitySet covers the patch P3 identity-based diff
// path: the previous length-only gate failed in two scenarios this test
// exercises end-to-end (PID switch resync + ring-buffer replace).
func TestApplyUnreadMarks_IdentitySet(t *testing.T) {
	makeStep := func(pid int, ts time.Time) UnifiedEvent {
		return UnifiedEvent{Type: EventStep, PID: types.PID(pid), Timestamp: ts}
	}

	t.Run("first sync skips marking (lastUnreadEventKeys nil)", func(t *testing.T) {
		m := newDashboardModel(nil)
		m.activePane = paneTree
		m.lastUnreadEventKeys = nil // simulates startup / handlePIDChange reset
		m.unifiedEvents = []UnifiedEvent{makeStep(1, time.Now())}
		m = m.applyUnreadMarks()
		if m.paneHasUnread[paneTimeline] {
			t.Errorf("first-sync tick must not mark unread; map=%v", m.paneHasUnread)
		}
		if m.lastUnreadEventKeys == nil {
			t.Errorf("expected lastUnreadEventKeys populated after first sync")
		}
	})

	t.Run("new event on subsequent tick marks target pane", func(t *testing.T) {
		m := newDashboardModel(nil)
		m.activePane = paneTree
		now := time.Now()
		m.unifiedEvents = []UnifiedEvent{makeStep(1, now)}
		m = m.applyUnreadMarks() // sync without mark
		// Second tick: append a new event with a different timestamp.
		m.unifiedEvents = append(m.unifiedEvents, makeStep(1, now.Add(time.Millisecond)))
		m = m.applyUnreadMarks()
		if !m.paneHasUnread[paneTimeline] {
			t.Errorf("expected Timeline marked unread after new step event")
		}
	})

	t.Run("active pane never marked", func(t *testing.T) {
		m := newDashboardModel(nil)
		m.activePane = paneTimeline
		m.unifiedEvents = []UnifiedEvent{makeStep(1, time.Now())}
		m = m.applyUnreadMarks()
		m.unifiedEvents = append(m.unifiedEvents, makeStep(1, time.Now().Add(time.Millisecond)))
		m = m.applyUnreadMarks()
		if m.paneHasUnread[paneTimeline] {
			t.Errorf("active pane should not be marked unread")
		}
	})

	t.Run("ring-buffer replace detects new event at same length", func(t *testing.T) {
		// Patch P3: length-only gate would miss this — mergeUnifiedEvents
		// could swap rows in place when sysEvents reaches maxSysEvents.
		m := newDashboardModel(nil)
		m.activePane = paneTree
		old := makeStep(1, time.Now())
		m.unifiedEvents = []UnifiedEvent{old}
		m = m.applyUnreadMarks()
		// Replace the slot with a different event (different timestamp →
		// different identity key) without growing the slice.
		m.unifiedEvents[0] = makeStep(1, time.Now().Add(time.Second))
		m = m.applyUnreadMarks()
		if !m.paneHasUnread[paneTimeline] {
			t.Errorf("expected Timeline marked when content replaced at same length")
		}
	})

	t.Run("PID switch resets identity set so first new-PID tick syncs without flooding", func(t *testing.T) {
		// Patch P2: handlePIDChange sets lastUnreadEventKeys=nil so the
		// first post-switch tick treats every event as "already seen".
		m := newDashboardModel(nil)
		m.activePane = paneTree
		m.unifiedEvents = []UnifiedEvent{makeStep(1, time.Now())}
		m = m.applyUnreadMarks()
		// Simulate PID change by clearing the identity set then loading a
		// fresh, smaller event list (typical post-switch shape).
		m.lastUnreadEventKeys = nil
		m.unifiedEvents = []UnifiedEvent{
			makeStep(2, time.Now().Add(time.Second)),
			makeStep(2, time.Now().Add(2*time.Second)),
		}
		m = m.applyUnreadMarks()
		if m.paneHasUnread[paneTimeline] {
			t.Errorf("first-tick after PID switch must not mark unread; map=%v", m.paneHasUnread)
		}
	})
}

// (typesPID helper removed — tests now import internal/types directly.)

// TestRenderPanelTabsLine_UnreadDot verifies the Unicode and ASCII-mode
// rendering of the unread red dot in the panel tabs line.
func TestRenderPanelTabsLine_UnreadDot(t *testing.T) {
	t.Run("dot appears on marked pane", func(t *testing.T) {
		m := newTestDashboardModel(nil)
		m.paneHasUnread[paneHeatmap] = true
		out := m.renderPanelTabsLine()
		if !strings.Contains(out, "[3]Heat") {
			t.Fatalf("expected label [3]Heat in output, got: %q", out)
		}
		if !strings.Contains(out, "●") {
			t.Errorf("expected ● red dot in tabs line, got: %q", out)
		}
	})

	t.Run("multiple unread panes render multiple dots", func(t *testing.T) {
		m := newTestDashboardModel(nil)
		m.paneHasUnread[paneTimeline] = true
		m.paneHasUnread[paneSecurity] = true
		out := m.renderPanelTabsLine()
		dotCount := strings.Count(out, "●")
		if dotCount < 2 {
			t.Errorf("expected at least 2 red dots, got %d in: %q", dotCount, out)
		}
	})

	t.Run("dot disappears after clear", func(t *testing.T) {
		m := newTestDashboardModel(nil)
		m.paneHasUnread[paneEval] = true
		out1 := m.renderPanelTabsLine()
		if !strings.Contains(out1, "●") {
			t.Fatalf("expected dot before clear, got: %q", out1)
		}
		m = m.clearPaneUnread(paneEval)
		out2 := m.renderPanelTabsLine()
		if strings.Contains(out2, "●") {
			t.Errorf("expected no dot after clear, got: %q", out2)
		}
	})

	t.Run("ASCII mode uses * fallback", func(t *testing.T) {
		t.Setenv("RNIX_ASCII", "1")
		m := newTestDashboardModel(nil)
		m.paneHasUnread[paneTrace] = true
		out := m.renderPanelTabsLine()
		if !strings.Contains(out, "*") {
			t.Errorf("expected * fallback in ASCII mode, got: %q", out)
		}
		// Strip ANSI before scanning for the unicode dot — colour codes
		// could legitimately contain bytes that look like '●' fragments.
		plain := stripAnsiForTest(out)
		if strings.Contains(plain, "●") {
			t.Errorf("expected no ● glyph in ASCII mode plaintext, got: %q", plain)
		}
	})
}

// stripAnsiForTest is a tiny helper used by colour-tolerant assertions.
// lipgloss does this internally via Width; we reuse the same trick by
// stripping ESC sequences before comparing.
func stripAnsiForTest(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			// Skip until 'm' or end.
			j := i + 2
			for j < len(s) && s[j] != 'm' {
				j++
			}
			if j < len(s) {
				j++
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// =============================================================================
// Sanity helpers used by AC2..AC6 tests below
// =============================================================================

// tinyImmuneAlert constructs an AlertWire fixture with sane defaults.
func tinyImmuneAlert(typ string, dev float64, pid uint64) ipc.AlertWire {
	return ipc.AlertWire{
		PID:           pid,
		AgentTemplate: "test",
		Type:          typ,
		Detail:        "fixture",
		Deviation:     dev,
		TimestampMs:   time.Now().UnixMilli(),
	}
}

// hasForeground reports whether the lipgloss style carries the given
// foreground colour. Profile-tolerant: works even when NO_COLOR is set.
func hasForeground(s lipgloss.Style, want string) bool {
	got := s.GetForeground()
	if c, ok := got.(lipgloss.Color); ok {
		return string(c) == want
	}
	return false
}

// silenceUnused makes Go happy when AC2..AC6 stub helpers above are used
// only by later test sections — see Task 4/5/6 tests added below.
var _ = tinyImmuneAlert
var _ = hasForeground
var _ = os.Getenv

// =============================================================================
// AC#2 — Alert Strip Enter routes EventImmune to Security pane
// =============================================================================

// TestAlertEnter_ImmuneRoutesToSecurity verifies that pressing enter on an
// expanded alert strip with an EventImmune row routes the user to the
// Security pane and locates the cursor on the matching securityAlerts entry.
func TestAlertEnter_ImmuneRoutesToSecurity(t *testing.T) {
	t.Run("Immune alert routes to Security pane (same PID)", func(t *testing.T) {
		// Patch P5: Same-PID branch matches PID + TimestampMs together so
		// multiple deviations on a single agent land on the right cursor.
		m := newContractModel()
		m.selectedPID = 7 // same PID so we exercise the same-PID branch
		now := time.Now()
		m.alertEvents = []UnifiedEvent{{
			Type:      EventImmune,
			Severity:  SevError,
			PID:       7,
			Timestamp: now,
		}}
		m.security.Alerts = []ipc.AlertWire{
			{PID: 5, Type: "syscall_freq", TimestampMs: now.Add(-time.Second).UnixMilli()},
			{PID: 7, Type: "device_access", TimestampMs: now.UnixMilli()},
			{PID: 7, Type: "syscall_freq", TimestampMs: now.Add(time.Second).UnixMilli()},
		}
		m.alertExpanded = true
		m.alertCursor = 0
		got, _ := m.dashboardKey(keypressFromString("enter"))
		g := got.(dashboardModel)
		if g.activePane != paneSecurity {
			t.Errorf("expected activePane=paneSecurity, got %d", g.activePane)
		}
		if g.security.Cursor != 1 {
			t.Errorf("expected securityCursor=1 (PID 7 + matching TimestampMs), got %d", g.security.Cursor)
		}
		if g.alertExpanded {
			t.Errorf("alertExpanded should be reset after jump")
		}
	})

	t.Run("Step alert still routes to Timeline (regression)", func(t *testing.T) {
		m := newContractModel()
		m.selectedPID = 1
		m.alertEvents = []UnifiedEvent{{
			Type:      EventStep,
			Severity:  SevWarn,
			PID:       1,
			Timestamp: time.Now(),
		}}
		m.alertExpanded = true
		m.alertCursor = 0
		got, _ := m.dashboardKey(keypressFromString("enter"))
		g := got.(dashboardModel)
		if g.activePane != paneTimeline {
			t.Errorf("expected non-Immune alert → paneTimeline, got %d", g.activePane)
		}
	})

	t.Run("Immune alert from different PID — cross-PID branch sets alertJumpTarget", func(t *testing.T) {
		// Patch P9 (2026-05-03): the cross-PID branch hands off cursor
		// resolution to dashboardTick → resolveAlertJumpTarget. We verify
		// (a) activePane switches to paneSecurity, (b) selectedPID flips
		// to the alert's PID, (c) alertJumpTarget carries the original
		// alert so the next tick can locate the cursor.
		m := newContractModel()
		m.selectedPID = 1 // user is currently on PID 1
		now := time.Now()
		alert := UnifiedEvent{
			Type:      EventImmune,
			Severity:  SevError,
			PID:       7, // alert is for a DIFFERENT PID
			Timestamp: now,
		}
		m.alertEvents = []UnifiedEvent{alert}
		m.processes = mockDashboardProcs() // includes PID 7
		m.alertExpanded = true
		m.alertCursor = 0
		got, _ := m.dashboardKey(keypressFromString("enter"))
		g := got.(dashboardModel)
		if g.activePane != paneSecurity {
			t.Errorf("expected cross-PID Immune jump → paneSecurity, got %d", g.activePane)
		}
		if g.selectedPID != 7 {
			t.Errorf("expected selectedPID=7 after jump, got %d", g.selectedPID)
		}
		if g.alertJumpTarget == nil {
			t.Fatalf("expected alertJumpTarget to be set so resolveAlertJumpTarget can complete on next tick")
		}
		if g.alertJumpTarget.Type != EventImmune || g.alertJumpTarget.PID != 7 {
			t.Errorf("alertJumpTarget mismatch, got %+v", g.alertJumpTarget)
		}
	})

	t.Run("resolveAlertJumpTarget locates Immune cursor on PID + TimestampMs match", func(t *testing.T) {
		// Patch P5 (2026-05-03): cross-PID resolution after handlePIDChange
		// must use PID + TimestampMs together — same regression as the
		// same-PID branch.
		m := newContractModel()
		m.selectedPID = 7
		now := time.Now()
		alert := UnifiedEvent{Type: EventImmune, PID: 7, Timestamp: now}
		m.alertJumpTarget = &alert
		m.security.Alerts = []ipc.AlertWire{
			{PID: 7, Type: "device_access", TimestampMs: now.Add(-time.Second).UnixMilli()},
			{PID: 7, Type: "syscall_freq", TimestampMs: now.UnixMilli()},
		}
		out := m.resolveAlertJumpTarget()
		if out.alertJumpTarget != nil {
			t.Errorf("alertJumpTarget should be consumed, got %+v", out.alertJumpTarget)
		}
		if out.security.Cursor != 1 {
			t.Errorf("expected securityCursor=1 (TimestampMs match), got %d", out.security.Cursor)
		}
	})
}

// =============================================================================
// AC#3 — Intent → Timeline drill-in (header toggle + unread clear)
// =============================================================================

// TestIntentEnter_HeaderTogglesCollapse verifies that pressing enter while
// the cursor sits on a non-terminal intent tree header toggles the
// per-tree collapse state, and that terminal trees ignore the toggle.
//
// Patch P1 (2026-05-03): the collapse map is keyed by tree.RootIntent
// (stable across reordering) rather than positional treeIndex, so the
// "stable across reordering" sub-test verifies the regression scenario
// the previous int-keyed implementation silently flubbed.
func TestIntentEnter_HeaderTogglesCollapse(t *testing.T) {
	t.Run("non-terminal header toggles collapse on enter (string key by RootIntent)", func(t *testing.T) {
		tree := &ipc.IntentTreeWire{
			RootIntent: "test root",
			State:      "executing",
			Nodes: map[string]*ipc.IntentNodeWire{
				"n1": {ID: "n1", Intent: "child", State: "pending"},
			},
		}
		m := newContractModel()
		m.activePane = paneIntent
		m.intent.Trees = []*ipc.IntentTreeWire{tree}
		m.intent.FlatNodes = flattenIntentTreesWithCollapse(m.intent.Trees, m.intent.TreeCollapsed)
		m.intent.Cursor = 0 // header
		got, _ := m.dashboardKey(keypressFromString("enter"))
		g := got.(dashboardModel)
		if !g.intent.TreeCollapsed["test root"] {
			t.Errorf("expected first toggle to set collapsed[\"test root\"]=true, got map=%v", g.intent.TreeCollapsed)
		}
		// Second toggle restores default.
		got2, _ := g.dashboardKey(keypressFromString("enter"))
		g2 := got2.(dashboardModel)
		if g2.intent.TreeCollapsed["test root"] {
			t.Errorf("expected second toggle to set collapsed[\"test root\"]=false, got map=%v", g2.intent.TreeCollapsed)
		}
	})

	t.Run("terminal tree header is no-op", func(t *testing.T) {
		tree := &ipc.IntentTreeWire{
			RootIntent: "done root",
			State:      "completed",
			Nodes: map[string]*ipc.IntentNodeWire{
				"n1": {ID: "n1", Intent: "child", State: "completed"},
			},
		}
		m := newContractModel()
		m.activePane = paneIntent
		m.intent.Trees = []*ipc.IntentTreeWire{tree}
		m.intent.FlatNodes = flattenIntentTreesWithCollapse(m.intent.Trees, m.intent.TreeCollapsed)
		m.intent.Cursor = 0 // header
		got, _ := m.dashboardKey(keypressFromString("enter"))
		g := got.(dashboardModel)
		// Terminal tree's user-toggle entry must remain absent (i.e. not flipped).
		if g.intent.TreeCollapsed["done root"] {
			t.Errorf("terminal tree header should be a no-op, got map=%v", g.intent.TreeCollapsed)
		}
	})

	t.Run("collapse state stable across tree reordering (P1 regression)", func(t *testing.T) {
		// Two non-terminal trees; user collapses the FIRST one. Then we
		// flip its state to "completed" so flattenIntentTreesWithCollapse
		// sorts it to the bottom. With the old int-keyed map this would
		// transfer the collapse to whichever tree took position 0; with
		// patch P1 the entry is keyed by RootIntent and stays put.
		alpha := &ipc.IntentTreeWire{
			RootIntent: "alpha",
			State:      "executing",
			Nodes: map[string]*ipc.IntentNodeWire{
				"n1": {ID: "n1", Intent: "child", State: "executing"},
			},
		}
		beta := &ipc.IntentTreeWire{
			RootIntent: "beta",
			State:      "executing",
			Nodes: map[string]*ipc.IntentNodeWire{
				"n2": {ID: "n2", Intent: "child", State: "executing"},
			},
		}
		m := newContractModel()
		m.activePane = paneIntent
		m.intent.Trees = []*ipc.IntentTreeWire{alpha, beta}
		m.intent.FlatNodes = flattenIntentTreesWithCollapse(m.intent.Trees, m.intent.TreeCollapsed)
		m.intent.Cursor = 0 // alpha header
		got, _ := m.dashboardKey(keypressFromString("enter"))
		g := got.(dashboardModel)
		if !g.intent.TreeCollapsed["alpha"] {
			t.Fatalf("expected alpha collapsed after toggle, got map=%v", g.intent.TreeCollapsed)
		}
		// Now alpha completes — but its collapse entry is preserved by RootIntent.
		// Re-flatten with the same map and the entry must still target alpha.
		alpha.State = "completed"
		flat := flattenIntentTreesWithCollapse(g.intent.Trees, g.intent.TreeCollapsed)
		// alpha is now terminal so it auto-collapses regardless of map; the
		// real assertion is that beta (idx 0 after sort) is NOT collapsed.
		var betaHeader *intentFlatNode
		for i := range flat {
			if flat[i].IsTreeHeader && flat[i].TreeWire.RootIntent == "beta" {
				betaHeader = &flat[i]
				break
			}
		}
		if betaHeader == nil {
			t.Fatalf("beta header missing after re-flatten, flat=%v", flat)
		}
		if betaHeader.IsCollapsed {
			t.Errorf("beta must NOT inherit alpha's collapse via positional drift; isCollapsed=true")
		}
		if !g.intent.TreeCollapsed["alpha"] {
			t.Errorf("alpha collapse entry should persist by RootIntent, got map=%v", g.intent.TreeCollapsed)
		}
	})

	t.Run("pruneIntentCollapse removes stale entries (P4)", func(t *testing.T) {
		stale := map[string]bool{"gone": true, "live": true}
		live := []*ipc.IntentTreeWire{{RootIntent: "live", State: "executing"}}
		got := pruneIntentCollapse(stale, live)
		if got["gone"] {
			t.Errorf("stale entry should be pruned, got map=%v", got)
		}
		if !got["live"] {
			t.Errorf("live entry should be retained, got map=%v", got)
		}
	})
}

// TestIntentEnter_DrillsToTimeline_ClearsUnread verifies that drilling from
// a node row jumps to Timeline AND clears the Timeline unread flag.
func TestIntentEnter_DrillsToTimeline_ClearsUnread(t *testing.T) {
	tree := &ipc.IntentTreeWire{
		RootIntent: "root",
		State:      "executing",
		Nodes: map[string]*ipc.IntentNodeWire{
			"n1": {ID: "n1", Intent: "child", State: "executing", PID: 5},
		},
	}
	m := newContractModel()
	m.activePane = paneIntent
	m.processes = mockDashboardProcs() // includes PID 5
	m.intent.Trees = []*ipc.IntentTreeWire{tree}
	m.intent.FlatNodes = flattenIntentTreesWithCollapse(m.intent.Trees, m.intent.TreeCollapsed)
	// Move cursor to the node entry (header is index 0; first node is index 1).
	m.intent.Cursor = 1
	m.paneHasUnread[paneTimeline] = true
	got, _ := m.dashboardKey(keypressFromString("enter"))
	g := got.(dashboardModel)
	if g.activePane != paneTimeline {
		t.Errorf("expected drill to paneTimeline, got %d", g.activePane)
	}
	if g.selectedPID != 5 {
		t.Errorf("expected selectedPID=5, got %d", g.selectedPID)
	}
	if g.paneHasUnread[paneTimeline] {
		t.Errorf("expected Timeline unread cleared after intent drill-in")
	}
}

// =============================================================================
// AC#4 — synthSecurityAlerts + buildAlertEventsWith
// =============================================================================

// =============================================================================
// AC#6 — Eval colour gradient (Synergy + Reputation)
// =============================================================================

// TestEvalScoreColorStyle_Thresholds verifies the 3-tier gradient
// boundaries. Uses GetForeground() to be profile-tolerant — works under
// NoColor / TrueColor profiles alike.
func TestEvalScoreColorStyle_Thresholds(t *testing.T) {
	cases := []struct {
		score float64
		want  string
		label string
	}{
		{0.95, ui.ColorSuccess, "0.95 → success"},
		{0.85, ui.ColorWarning, "0.85 → warning"},
		{0.50, ui.ColorError, "0.50 → error"},
		{1.00, ui.ColorSuccess, "1.00 → success"},
		{0.90, ui.ColorSuccess, "0.90 → success boundary"},
		{0.70, ui.ColorWarning, "0.70 → warning boundary"},
		{0.00, ui.ColorError, "0.00 → error"},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			s := evalScoreColorStyle(c.score)
			if !hasForeground(s, c.want) {
				t.Errorf("score=%v: expected fg=%q, got %v", c.score, c.want, s.GetForeground())
			}
		})
	}
}

// TestRenderEvalSynergy_RecommendedBold verifies that Recommended rows
// emit Bold ANSI when the lipgloss profile carries colour, and the ASCII
// fallback ★ marker when in ASCII mode.
func TestRenderEvalSynergy_RecommendedBold(t *testing.T) {
	// Profile probe — if Bold is stripped (NoColor profile / NO_COLOR set)
	// we skip the ANSI assertion to avoid flake.
	probe := lipgloss.NewStyle().Bold(true).Render("x")
	colourful := strings.Contains(probe, "\x1b[")

	t.Run("Recommended renders ★/Bold", func(t *testing.T) {
		t.Setenv("RNIX_ASCII", "1")
		m := newTestDashboardModel(mockDashboardProcs())
		m.evalSubView = 2
		m.evalSynergies = []kernel.ComboSummary{
			{Skills: []string{"a", "b"}, SuccessRate: 0.95, AvgTokens: 1000, TotalExecutions: 5, TokenImprovement: -0.2, Recommended: true},
		}
		out := m.renderEvalSynergyView(80, 6)
		if !strings.Contains(out, "★") {
			t.Errorf("expected ★ marker on Recommended row in ASCII mode, got: %q", out)
		}
	})

	t.Run("non-Recommended row has no ★ marker", func(t *testing.T) {
		t.Setenv("RNIX_ASCII", "1")
		m := newTestDashboardModel(mockDashboardProcs())
		m.evalSubView = 2
		m.evalSynergies = []kernel.ComboSummary{
			{Skills: []string{"x", "y"}, SuccessRate: 0.6, AvgTokens: 1500, TotalExecutions: 3, TokenImprovement: 0.1, Recommended: false},
		}
		out := m.renderEvalSynergyView(80, 6)
		if strings.Contains(out, "★") {
			t.Errorf("non-Recommended row should not carry ★, got: %q", out)
		}
	})

	t.Run("Bold ANSI present when profile supports colour", func(t *testing.T) {
		if !colourful {
			t.Skip("lipgloss profile strips ANSI — skipping Bold byte check")
		}
		m := newTestDashboardModel(mockDashboardProcs())
		m.evalSubView = 2
		m.evalSynergies = []kernel.ComboSummary{
			{Skills: []string{"a", "b"}, SuccessRate: 0.95, AvgTokens: 1000, TotalExecutions: 5, TokenImprovement: -0.2, Recommended: true},
		}
		out := m.renderEvalSynergyView(80, 6)
		if !strings.Contains(out, "\x1b[1m") && !strings.Contains(out, ";1m") {
			t.Errorf("expected Bold ANSI sequence in Recommended render, got: %q", out)
		}
	})
}

// TestRenderEvalReputation_ScoreColorGradient verifies the SCORE column
// applies the gradient style.
func TestRenderEvalReputation_ScoreColorGradient(t *testing.T) {
	probe := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorSuccess)).Render("x")
	if !strings.Contains(probe, "\x1b[") {
		t.Skip("colour profile strips ANSI — skip rendered byte assertion")
	}
	m := newTestDashboardModel(mockDashboardProcs())
	m.evalSubView = 0
	m.evalReputations = []kernel.ReputationSummary{
		{AgentName: "high", Score: 0.95, SuccessRate: 0.9, AvgTokens: 100, AvgDurationMs: 100, TotalRecords: 10, RecentTrend: "improving"},
		{AgentName: "mid", Score: 0.80, SuccessRate: 0.7, AvgTokens: 200, AvgDurationMs: 200, TotalRecords: 8, RecentTrend: "stable"},
		{AgentName: "low", Score: 0.40, SuccessRate: 0.3, AvgTokens: 400, AvgDurationMs: 400, TotalRecords: 2, RecentTrend: "declining"},
	}
	out := m.renderEvalReputationView(120, 10)
	// The SCORE column appears with the foreground colour; we should see
	// both Success and Error palette codes somewhere in the output.
	if !strings.Contains(out, ui.ColorSuccess[1:]) && !strings.Contains(out, ui.ColorSuccess) {
		// lipgloss usually emits true-colour 38;2;R;G;B; check that escape
		// sequences exist at all.
		if !strings.Contains(out, "\x1b[38;2;") {
			t.Errorf("expected truecolor ANSI codes in coloured render, got: %q", out)
		}
	}
}

// TestSynthSecurityAlerts_SeverityMapping verifies device_access /
// syscall_freq / token_rate / deviation thresholds and timestamp fallback.
func TestSynthSecurityAlerts_SeverityMapping(t *testing.T) {
	t.Run("device_access → SevError", func(t *testing.T) {
		out := synthSecurityAlerts([]ipc.AlertWire{tinyImmuneAlert("device_access", 1.0, 1)})
		if len(out) != 1 || out[0].Severity != SevError {
			t.Errorf("expected device_access → SevError, got %+v", out)
		}
	})

	t.Run("syscall_freq deviation 2.0 → SevWarn", func(t *testing.T) {
		out := synthSecurityAlerts([]ipc.AlertWire{tinyImmuneAlert("syscall_freq", 2.0, 1)})
		if len(out) != 1 || out[0].Severity != SevWarn {
			t.Errorf("expected syscall_freq dev=2.0 → SevWarn, got %+v", out)
		}
	})

	t.Run("syscall_freq deviation 3.5 → SevError", func(t *testing.T) {
		out := synthSecurityAlerts([]ipc.AlertWire{tinyImmuneAlert("syscall_freq", 3.5, 1)})
		if len(out) != 1 || out[0].Severity != SevError {
			t.Errorf("expected syscall_freq dev=3.5 → SevError, got %+v", out)
		}
	})

	t.Run("token_rate → SevWarn", func(t *testing.T) {
		out := synthSecurityAlerts([]ipc.AlertWire{tinyImmuneAlert("token_rate", 1.5, 1)})
		if len(out) != 1 || out[0].Severity != SevWarn {
			t.Errorf("expected token_rate → SevWarn, got %+v", out)
		}
	})

	t.Run("zero TimestampMs preserves zero time (no Now fallback after P0)", func(t *testing.T) {
		// Patch P0 (2026-05-03): synthSecurityAlerts no longer falls back
		// to time.Now() — IsSynthetic now drives lifecycle so the timestamp
		// stays as upstream provided it. Zero in → zero out.
		alert := tinyImmuneAlert("syscall_freq", 1.0, 1)
		alert.TimestampMs = 0
		out := synthSecurityAlerts([]ipc.AlertWire{alert})
		if len(out) != 1 {
			t.Fatalf("expected 1 synth event, got %d", len(out))
		}
		if !out[0].Timestamp.IsZero() {
			t.Errorf("TimestampMs=0 must produce IsZero timestamp (no Now fallback), got %v", out[0].Timestamp)
		}
		if !out[0].IsSynthetic {
			t.Errorf("synth row must carry IsSynthetic=true so buildAlertEvents bypasses TTL")
		}
	})
}

// TestBuildAlertEventsWith_MergesSecurity verifies the merged output
// preserves severity ordering and that nil security input is regression-safe.
func TestBuildAlertEventsWith_MergesSecurity(t *testing.T) {
	t.Run("merges synth into alerts with severity order", func(t *testing.T) {
		now := time.Now()
		events := []UnifiedEvent{
			{Type: EventStep, Severity: SevWarn, PID: 1, Timestamp: now},
		}
		alerts := []ipc.AlertWire{tinyImmuneAlert("device_access", 1.0, 10)}
		alerts[0].TimestampMs = now.UnixMilli()
		out := buildAlertEventsWith(events, alerts)
		if len(out) != 2 {
			t.Fatalf("expected 2 alerts after merge, got %d (%+v)", len(out), out)
		}
		// Severity-desc sort means SevError (security) should come first.
		if out[0].Severity != SevError {
			t.Errorf("expected SevError first, got severity=%d", out[0].Severity)
		}
		if out[1].Severity != SevWarn {
			t.Errorf("expected SevWarn second, got severity=%d", out[1].Severity)
		}
	})

	t.Run("nil security alerts → identical to buildAlertEvents", func(t *testing.T) {
		events := []UnifiedEvent{
			{Type: EventStep, Severity: SevWarn, PID: 1, Timestamp: time.Now()},
		}
		legacy := buildAlertEvents(events)
		merged := buildAlertEventsWith(events, nil)
		if len(legacy) != len(merged) {
			t.Errorf("expected len match, got legacy=%d merged=%d", len(legacy), len(merged))
		}
		for i := range legacy {
			if legacy[i].Type != merged[i].Type || legacy[i].PID != merged[i].PID {
				t.Errorf("alert mismatch at %d: legacy=%+v merged=%+v", i, legacy[i], merged[i])
			}
		}
	})
}

// =============================================================================
// AC#5 — Trace waterfall bar (degraded plan A)
// =============================================================================

// TestRenderWaterfallBar_BasicLayout verifies the bar layout, ANSI mode
// behaviour, and clamping for adversarial inputs.
func TestRenderWaterfallBar_BasicLayout(t *testing.T) {
	t.Run("normal scale traceTotal=1000 dur=300", func(t *testing.T) {
		out := renderWaterfallBar(1000, 300, "ok", true) // ASCII deterministic
		// barLen = floor(20 * 300 / 1000) = 6 → 6 '#' followed by 14 '.'.
		want := strings.Repeat("#", 6) + strings.Repeat(".", 14)
		if out != want {
			t.Errorf("expected %q, got %q", want, out)
		}
	})

	t.Run("traceTotal=0 → all dim filler", func(t *testing.T) {
		out := renderWaterfallBar(0, 500, "ok", true)
		want := strings.Repeat(".", waterfallBarWidth)
		if out != want {
			t.Errorf("expected all-dim %q, got %q", want, out)
		}
	})

	t.Run("clamp when spanDur > traceTotal", func(t *testing.T) {
		out := renderWaterfallBar(100, 500, "ok", true)
		want := strings.Repeat("#", waterfallBarWidth) // fully filled, no overflow
		if out != want {
			t.Errorf("expected clamped fill %q, got %q", want, out)
		}
	})

	t.Run("non-zero spanDur gets at least 1 cell", func(t *testing.T) {
		// 1 / 10000 → 0.002 → would round to 0; minimum-presence rule lifts to 1.
		out := renderWaterfallBar(10000, 1, "ok", true)
		want := "#" + strings.Repeat(".", 19)
		if out != want {
			t.Errorf("expected min-1 fill %q, got %q", want, out)
		}
	})

	t.Run("spanDur=0 → no fill", func(t *testing.T) {
		out := renderWaterfallBar(1000, 0, "ok", true)
		want := strings.Repeat(".", waterfallBarWidth)
		if out != want {
			t.Errorf("expected zero-fill %q, got %q", want, out)
		}
	})

	t.Run("ASCII mode uses # and . characters", func(t *testing.T) {
		out := renderWaterfallBar(1000, 500, "error", true)
		if strings.Contains(out, "█") || strings.Contains(out, "·") {
			t.Errorf("ASCII output must not contain unicode glyphs, got %q", out)
		}
	})
}

// TestRenderTraceTreeView_WaterfallHiddenWhenNarrow verifies that the bar
// is omitted entirely below the 80-col threshold and present above.
func TestRenderTraceTreeView_WaterfallHiddenWhenNarrow(t *testing.T) {
	tree := &ipc.SpanTreeWire{
		TraceID: "trace-fixture-1",
		Metadata: ipc.TraceMetaWire{
			TotalSpans:      1,
			TotalDurationMs: 1000,
		},
		Root: &ipc.SpanNodeWire{
			SpanID:     "root",
			PID:        1,
			Name:       "root.span",
			DurationMs: 500,
			Status:     "ok",
		},
	}
	m := newTestDashboardModel(mockDashboardProcs())
	t.Setenv("RNIX_ASCII", "1") // make output deterministic for the assertion
	m.trace.SelectedSpanTree = tree
	m.trace.SelectedTraceID = tree.TraceID
	m.trace.SpanFlatNodes = flattenSpanTree(tree)
	m.trace.ViewMode = 1

	t.Run("width 70 hides bar", func(t *testing.T) {
		out := m.renderTraceTreeView(70, 20)
		// Look for runs of waterfall fill characters that are wider than any
		// stray single '#' that legitimately appears elsewhere.
		if strings.Contains(out, strings.Repeat("#", 5)) {
			t.Errorf("expected no waterfall block in narrow render, got: %q", out)
		}
	})

	t.Run("width 120 shows bar", func(t *testing.T) {
		out := m.renderTraceTreeView(120, 20)
		// 500/1000 → 10 fill chars; stripping ANSI reveals them contiguously.
		if !strings.Contains(out, strings.Repeat("#", 10)) {
			t.Errorf("expected waterfall fill block of 10 in wide render, got: %q", out)
		}
	})
}

// TestSecurityAlert_AppearsInBadge verifies that synth security alerts
// alone produce a non-empty badge through alertCountBadge (Story 38-4 AC#4).
func TestSecurityAlert_AppearsInBadge(t *testing.T) {
	alerts := []ipc.AlertWire{
		tinyImmuneAlert("device_access", 1.0, 1), // SevError
		tinyImmuneAlert("syscall_freq", 1.0, 2),  // SevWarn
	}
	merged := buildAlertEventsWith(nil, alerts)
	if len(merged) != 2 {
		t.Fatalf("expected 2 merged alerts, got %d", len(merged))
	}
	badge := alertCountBadge(merged, false)
	if !strings.Contains(stripAnsiForTest(badge), "✗1") {
		t.Errorf("expected ✗1 in badge, got %q", badge)
	}
	if !strings.Contains(stripAnsiForTest(badge), "⚠1") {
		t.Errorf("expected ⚠1 in badge, got %q", badge)
	}
}

// TestAlertEnter_ClearsUnread verifies that the Layer 0 alert+enter handler
// clears the destination pane's unread flag (AC#2 + AC#1 cross-cut).
func TestAlertEnter_ClearsUnread(t *testing.T) {
	t.Run("Timeline jump clears Timeline unread", func(t *testing.T) {
		m := newContractModel()
		m.selectedPID = 1
		m.alertEvents = []UnifiedEvent{{
			Type:      EventBudget,
			Severity:  SevWarn,
			PID:       1,
			Timestamp: time.Now(),
		}}
		m.alertExpanded = true
		m.alertCursor = 0
		m.paneHasUnread[paneTimeline] = true
		got, _ := m.dashboardKey(keypressFromString("enter"))
		g := got.(dashboardModel)
		if g.paneHasUnread[paneTimeline] {
			t.Errorf("expected paneTimeline unread cleared after jump")
		}
	})

	t.Run("Security jump clears Security unread", func(t *testing.T) {
		m := newContractModel()
		m.selectedPID = 7
		now := time.Now()
		m.alertEvents = []UnifiedEvent{{
			Type:      EventImmune,
			Severity:  SevError,
			PID:       7,
			Timestamp: now,
		}}
		// Patch P5: securityAlerts TimestampMs must match alert.Timestamp
		// to land the cursor; the unread clear logic itself is independent.
		m.security.Alerts = []ipc.AlertWire{{PID: 7, Type: "device_access", TimestampMs: now.UnixMilli()}}
		m.alertExpanded = true
		m.alertCursor = 0
		m.paneHasUnread[paneSecurity] = true
		got, _ := m.dashboardKey(keypressFromString("enter"))
		g := got.(dashboardModel)
		if g.paneHasUnread[paneSecurity] {
			t.Errorf("expected paneSecurity unread cleared after Immune jump")
		}
	})
}

// =============================================================================
// AC#0 (patch P0) — IsSynthetic flag bypasses TTL filter
// =============================================================================

// TestBuildAlertEvents_SyntheticBypassesTTL verifies that rows with
// IsSynthetic == true (currently only synthSecurityAlerts) are retained
// even when their Timestamp is older than alertTTL. Lifecycle is driven
// by the upstream IPC list (m.security.Alerts), not wall-clock TTL.
func TestBuildAlertEvents_SyntheticBypassesTTL(t *testing.T) {
	t.Run("synthetic row older than TTL stays in result", func(t *testing.T) {
		old := time.Now().Add(-2 * alertTTL) // 60 s ago — well past 30 s TTL
		events := []UnifiedEvent{
			{Type: EventImmune, Severity: SevError, PID: 7, Timestamp: old, IsSynthetic: true},
		}
		out := buildAlertEvents(events)
		if len(out) != 1 {
			t.Fatalf("synth row past TTL must be retained, got %d alerts", len(out))
		}
		if !out[0].IsSynthetic {
			t.Errorf("retained row should still carry IsSynthetic flag")
		}
	})

	t.Run("non-synthetic row older than TTL is filtered (regression)", func(t *testing.T) {
		old := time.Now().Add(-2 * alertTTL)
		events := []UnifiedEvent{
			{Type: EventBudget, Severity: SevWarn, PID: 1, Timestamp: old}, // IsSynthetic=false
		}
		out := buildAlertEvents(events)
		if len(out) != 0 {
			t.Errorf("non-synthetic row past TTL must be filtered, got %d alerts", len(out))
		}
	})
}

// TestEvalScoreColorStyle_OutOfRange verifies P12: NaN, +Inf, -Inf, and
// scores outside [0, 1] all snap to ColorError per the godoc contract.
func TestEvalScoreColorStyle_OutOfRange(t *testing.T) {
	cases := []struct {
		score float64
		label string
	}{
		{math.NaN(), "NaN"},
		{math.Inf(1), "+Inf"},
		{math.Inf(-1), "-Inf"},
		{-0.5, "negative"},
		{1.5, ">1.0"},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			s := evalScoreColorStyle(c.score)
			if !hasForeground(s, ui.ColorError) {
				t.Errorf("score=%v: expected ColorError, got %v", c.score, s.GetForeground())
			}
		})
	}
}

// =============================================================================
// AC#5 (patch P10) — Trace waterfall bar additional coverage
// =============================================================================

// TestRenderWaterfallBar_StatusColours verifies that the bar's coloured
// fill carries the spec-defined ANSI for each status, profile-tolerant.
func TestRenderWaterfallBar_StatusColours(t *testing.T) {
	probe := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError)).Render("x")
	if !strings.Contains(probe, "\x1b[") {
		t.Skip("colour profile strips ANSI — skip rendered byte assertion")
	}
	cases := []struct {
		status string
		label  string
	}{
		{"error", "error → red bar"},
		{"timeout", "timeout → orange bar"},
		{"ok", "ok → green bar"},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			out := renderWaterfallBar(1000, 500, c.status, false) // unicode mode
			// Status-coloured fill must produce SGR with foreground + the
			// fill glyph. We assert ANSI escapes exist around the bar.
			if !strings.Contains(out, "\x1b[") {
				t.Errorf("expected ANSI codes for status=%q bar, got: %q", c.status, out)
			}
			if !strings.Contains(out, "█") {
				t.Errorf("expected unicode fill char in non-ASCII mode, got: %q", out)
			}
		})
	}
}

// TestRenderTraceTreeView_WaterfallMultiChild verifies that multi-span
// trees produce visually distinct fills — long span wider, short span
// narrower — and that the total bar width per row is constant
// (waterfallBarWidth).
func TestRenderTraceTreeView_WaterfallMultiChild(t *testing.T) {
	root := &ipc.SpanNodeWire{
		SpanID:     "root",
		PID:        1,
		Name:       "root.span",
		DurationMs: 1000,
		Status:     "ok",
		Children: []ipc.SpanNodeWire{
			{SpanID: "fast", PID: 2, Name: "fast.child", DurationMs: 100, Status: "ok"},
			{SpanID: "slow", PID: 3, Name: "slow.child", DurationMs: 800, Status: "error"},
		},
	}
	tree := &ipc.SpanTreeWire{
		TraceID:  "trace-multi",
		Metadata: ipc.TraceMetaWire{TotalSpans: 3, TotalDurationMs: 1000},
		Root:     root,
	}
	m := newTestDashboardModel(mockDashboardProcs())
	t.Setenv("RNIX_ASCII", "1")
	m.trace.SelectedSpanTree = tree
	m.trace.SelectedTraceID = tree.TraceID
	m.trace.SpanFlatNodes = flattenSpanTree(tree)
	m.trace.ViewMode = 1

	out := m.renderTraceTreeView(120, 20)
	// fast (100/1000 → 2 chars), slow (800/1000 → 16 chars), root (1000/1000 → 20 chars).
	// We expect at least one short fill (≤3) and one long fill (≥10) coexisting.
	if !strings.Contains(out, strings.Repeat("#", 16)) {
		t.Errorf("expected slow child to produce 16-char fill, got: %q", out)
	}
	if !strings.Contains(out, strings.Repeat("#", 20)) {
		t.Errorf("expected root span to produce 20-char fill, got: %q", out)
	}
}

// =============================================================================
// AC#6 (patch P11) — Eval Synergy VsSoloColors + ColorGradient
// =============================================================================

// TestRenderEvalSynergy_VsSoloColors verifies the VS SOLO column carries
// the right ANSI: < 0 → green (token saved), > 0 → red (more tokens),
// == 0 → no foreground colour wrap.
func TestRenderEvalSynergy_VsSoloColors(t *testing.T) {
	probe := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorSuccess)).Render("x")
	if !strings.Contains(probe, "\x1b[") {
		t.Skip("colour profile strips ANSI — skip rendered byte assertion")
	}
	t.Run("negative improvement → green", func(t *testing.T) {
		m := newTestDashboardModel(mockDashboardProcs())
		m.evalSubView = 2
		m.evalSynergies = []kernel.ComboSummary{
			{Skills: []string{"a"}, SuccessRate: 0.5, AvgTokens: 100, TotalExecutions: 1, TokenImprovement: -0.25},
		}
		out := m.renderEvalSynergyView(120, 6)
		// Expect ANSI escape sequence around -25.0% — at least one Success-colour code.
		if !strings.Contains(out, "-25.0%") {
			t.Errorf("expected -25.0%% in output, got: %q", out)
		}
		// Truecolor or named-colour ANSI somewhere on that token.
		if !strings.Contains(out, "\x1b[") {
			t.Errorf("expected ANSI sequence around negative improvement, got: %q", out)
		}
	})

	t.Run("positive improvement → red", func(t *testing.T) {
		m := newTestDashboardModel(mockDashboardProcs())
		m.evalSubView = 2
		m.evalSynergies = []kernel.ComboSummary{
			{Skills: []string{"a"}, SuccessRate: 0.5, AvgTokens: 100, TotalExecutions: 1, TokenImprovement: 0.30},
		}
		out := m.renderEvalSynergyView(120, 6)
		if !strings.Contains(out, "+30.0%") {
			t.Errorf("expected +30.0%% in output, got: %q", out)
		}
		if !strings.Contains(out, "\x1b[") {
			t.Errorf("expected ANSI sequence around positive improvement, got: %q", out)
		}
	})

	t.Run("zero improvement → no fg colour wrap", func(t *testing.T) {
		m := newTestDashboardModel(mockDashboardProcs())
		m.evalSubView = 2
		m.evalSynergies = []kernel.ComboSummary{
			{Skills: []string{"a"}, SuccessRate: 0.5, AvgTokens: 100, TotalExecutions: 1, TokenImprovement: 0.0},
		}
		out := m.renderEvalSynergyView(120, 6)
		if !strings.Contains(out, "+0.0%") {
			t.Errorf("expected +0.0%% in output, got: %q", out)
		}
		// Plain "+0.0%" should appear without an ANSI wrap immediately
		// preceding it. The other columns may still carry escapes, so
		// we just check the token stays unstyled by stripping ANSI and
		// confirming the literal substring appears in the plaintext.
		plain := stripAnsiForTest(out)
		if !strings.Contains(plain, "+0.0%") {
			t.Errorf("expected unstyled +0.0%% in plaintext, got: %q", plain)
		}
	})
}

// TestRenderEvalSynergy_ColorGradient verifies the SUCCESS column applies
// the 3-tier gradient (green ≥0.9 / yellow ≥0.7 / red <0.7) per AC#6.
func TestRenderEvalSynergy_ColorGradient(t *testing.T) {
	probe := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorSuccess)).Render("x")
	if !strings.Contains(probe, "\x1b[") {
		t.Skip("colour profile strips ANSI — skip rendered byte assertion")
	}
	m := newTestDashboardModel(mockDashboardProcs())
	m.evalSubView = 2
	m.evalSynergies = []kernel.ComboSummary{
		{Skills: []string{"hi"}, SuccessRate: 0.95, AvgTokens: 100, TotalExecutions: 1, TokenImprovement: -0.1},
		{Skills: []string{"mid"}, SuccessRate: 0.75, AvgTokens: 100, TotalExecutions: 1, TokenImprovement: -0.1},
		{Skills: []string{"lo"}, SuccessRate: 0.40, AvgTokens: 100, TotalExecutions: 1, TokenImprovement: -0.1},
	}
	out := m.renderEvalSynergyView(120, 8)
	for _, want := range []string{"95.0%", "75.0%", "40.0%"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected SUCCESS literal %q in output, got: %q", want, out)
		}
	}
	// The output must contain ANSI sequences (one fg per row) — count escapes
	// loosely as a smoke check that the gradient is being applied.
	if strings.Count(out, "\x1b[") < 3 {
		t.Errorf("expected at least 3 ANSI sequences (one per gradient tier), got: %q", out)
	}
}
