package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/vfs"
)

const maxSysEvents = 200

// stepToUnifiedEvent converts a stepEntry to a UnifiedEvent.
// baseTime is the reference time; step index offsets ensure stable ordering.
func stepToUnifiedEvent(e *stepEntry, baseTime time.Time, index int) UnifiedEvent {
	sev := SevInfo
	if e.Summary.HasError {
		sev = SevError
	}
	// Use real timestamp from step record if available; fall back to synthetic ordering
	ts := baseTime.Add(time.Duration(index) * time.Millisecond)
	if e.Summary.TimestampMs > 0 {
		ts = baseTime.Add(time.Duration(e.Summary.TimestampMs) * time.Millisecond)
	}
	summary := fmt.Sprintf("[%d] %s — %s (%.0fms)",
		e.Summary.Step, e.Summary.Action, e.Summary.Summary, e.Summary.DurationMs)
	return UnifiedEvent{
		Type:      EventStep,
		Severity:  sev,
		Timestamp: ts,
		PID:       0, // populated by caller
		Summary:   summary,
		StepEntry: e,
	}
}

// compactEventFromSyscall converts a Compact syscall event into a UnifiedEvent.
func compactEventFromSyscall(ev ipc.SyscallEventWire) UnifiedEvent {
	ts := time.UnixMilli(ev.TimestampMs)

	preTokens := getArgInt(ev.Args, "pre_tokens")
	postTokens := getArgInt(ev.Args, "post_tokens")
	restored := getArgInt(ev.Args, "restored_items")
	durMs := getArgFloat(ev.Args, "duration_ms")

	summary := fmt.Sprintf("★ COMPACT %dK→%dK restored:%d %.1fs",
		preTokens/1000, postTokens/1000, restored, durMs/1000.0)

	return UnifiedEvent{
		Type:      EventCompact,
		Severity:  SevInfo,
		Timestamp: ts,
		PID:       ev.PID,
		Summary:   summary,
		Detail:    fmt.Sprintf("pre=%d post=%d restored=%d duration=%.0fms", preTokens, postTokens, restored, durMs),
		RawEvent:  &ev,
	}
}

// detectSpawnExitEvents compares previous and current process lists to detect spawns and exits.
// Returns nil on the first tick (when prev is empty) to avoid spurious spawn events.
func detectSpawnExitEvents(prev map[types.PID]vfs.ProcInfo, curr []vfs.ProcInfo) []UnifiedEvent {
	if len(prev) == 0 {
		return nil
	}
	var events []UnifiedEvent
	currMap := make(map[types.PID]vfs.ProcInfo, len(curr))
	for _, p := range curr {
		currMap[p.PID] = p
	}

	// Detect spawns: PID in curr but not in prev
	for _, p := range curr {
		if _, existed := prev[p.PID]; !existed {
			events = append(events, UnifiedEvent{
				Type:      EventSpawn,
				Severity:  SevInfo,
				Timestamp: p.CreatedAt,
				PID:       p.PID,
				UUID:      p.UUID,
				Summary:   fmt.Sprintf("↑ SPAWN PID %d %q", p.PID, p.Intent),
			})
		}
	}

	// Detect exits: PID in prev that is now Dead (but was not Dead before)
	for pid, prevProc := range prev {
		currProc, stillExists := currMap[pid]
		if !stillExists {
			// Process disappeared entirely
			events = append(events, UnifiedEvent{
				Type:      EventExit,
				Severity:  SevInfo,
				Timestamp: time.Now(),
				PID:       pid,
				UUID:      prevProc.UUID,
				Summary:   exitEventSummary(pid, prevProc.Intent, prevProc.Result),
				Detail:    prevProc.Result,
			})
			continue
		}
		if currProc.State == types.StateDead && prevProc.State != types.StateDead {
			sev := SevInfo
			if inferExitError(currProc) {
				sev = SevError
			}
			ts := currProc.DeadAt
			if ts.IsZero() {
				ts = time.Now()
			}
			events = append(events, UnifiedEvent{
				Type:      EventExit,
				Severity:  sev,
				Timestamp: ts,
				PID:       pid,
				UUID:      currProc.UUID,
				Summary:   exitEventSummary(pid, currProc.Intent, currProc.Result),
				Detail:    currProc.Result,
			})
		}
	}
	return events
}

// detectBudgetEvents checks token budget usage and generates warning events.
func detectBudgetEvents(processes []vfs.ProcInfo, alertSeen map[types.PID]int) []UnifiedEvent {
	var events []UnifiedEvent
	for _, p := range processes {
		if p.ContextBudget <= 0 || p.TokensUsed <= 0 {
			continue
		}
		usagePct := int(int64(p.TokensUsed) * 100 / int64(p.ContextBudget))
		var sev int
		switch {
		case usagePct >= 95:
			sev = SevError
		case usagePct >= 80:
			sev = SevWarn
		default:
			// Below threshold — clear any previous alert
			delete(alertSeen, p.PID)
			continue
		}
		// Only emit if severity changed
		if prevSev, seen := alertSeen[p.PID]; seen && prevSev == sev {
			continue
		}
		alertSeen[p.PID] = sev
		events = append(events, UnifiedEvent{
			Type:      EventBudget,
			Severity:  sev,
			Timestamp: time.Now(),
			PID:       p.PID,
			UUID:      p.UUID,
			Summary:   fmt.Sprintf("⚠ BUDGET PID %d %d%% threshold reached", p.PID, usagePct),
			Detail:    fmt.Sprintf("tokens_used=%d context_budget=%d usage=%d%%", p.TokensUsed, p.ContextBudget, usagePct),
		})
	}
	return events
}

// detectStallEvents generates stall events from heartbeat status.
func detectStallEvents(hbStatus *ipc.HeartbeatStatusResponse, stallSeen map[types.PID]struct{}) []UnifiedEvent {
	if hbStatus == nil {
		// Clear stale entries when heartbeat data is unavailable
		for pid := range stallSeen {
			delete(stallSeen, pid)
		}
		return nil
	}
	var events []UnifiedEvent
	currentPIDs := make(map[types.PID]struct{}, len(hbStatus.CurrentStalled))

	for _, sp := range hbStatus.CurrentStalled {
		currentPIDs[sp.PID] = struct{}{}
		if _, seen := stallSeen[sp.PID]; seen {
			continue // already reported
		}
		stallSeen[sp.PID] = struct{}{}
		gapSec := sp.HeartbeatGapMs / 1000
		severity := SevWarn
		if sp.ConsecutiveStalls >= 3 {
			severity = SevError
		}
		events = append(events, UnifiedEvent{
			Type:      EventStall,
			Severity:  severity,
			Timestamp: time.Now(),
			PID:       sp.PID,
			UUID:      sp.UUID,
			Summary:   fmt.Sprintf("⚠ STALL PID %d no heartbeat %ds", sp.PID, gapSec),
			Detail:    fmt.Sprintf("heartbeat_gap_ms=%d detected_ago_ms=%d consecutive_stalls=%d last_action=%s", sp.HeartbeatGapMs, sp.StalledDurationMs, sp.ConsecutiveStalls, sp.LastAction),
		})
	}

	// Clean up stall tracking for processes no longer stalled
	for pid := range stallSeen {
		if _, still := currentPIDs[pid]; !still {
			delete(stallSeen, pid)
		}
	}
	return events
}

// mergeUnifiedEvents merges step entries and system events into a single sorted list.
// When selectedUUID is non-empty, only system events matching that UUID are included.
// Falls back to PID-based filtering when UUID is empty (backward compat with old daemons).
// Story 36-4: ascending=true renders oldest-first (newest at bottom, aligns with Debug
// mode and common log tools); ascending=false preserves the legacy newest-first order.
func mergeUnifiedEvents(stepEntries []stepEntry, sysEvents []UnifiedEvent, selectedPID types.PID, selectedUUID string, processes []vfs.ProcInfo, ascending bool) []UnifiedEvent {
	merged := make([]UnifiedEvent, 0, len(stepEntries)+len(sysEvents))

	// Resolve process CreatedAt for real step timestamps
	var baseTime time.Time
	for _, p := range processes {
		if p.PID == selectedPID && (selectedUUID == "" || p.UUID == selectedUUID) {
			baseTime = p.CreatedAt
			break
		}
	}
	if baseTime.IsZero() {
		baseTime = time.Now()
	}
	for i := range stepEntries {
		ue := stepToUnifiedEvent(&stepEntries[i], baseTime, i)
		ue.PID = selectedPID
		ue.UUID = selectedUUID
		merged = append(merged, ue)
	}

	// Add system events filtered by selected process identity
	for _, ev := range sysEvents {
		if selectedUUID != "" {
			// Prefer UUID matching (exact process instance)
			if ev.UUID != selectedUUID {
				continue
			}
		} else if selectedPID > 0 {
			// Fallback: PID-only (old daemons without UUID)
			if ev.PID != selectedPID {
				continue
			}
		}
		merged = append(merged, ev)
	}

	// Sort by timestamp. Stable tie-breaker: StepEntry-carrying events sort
	// before raw sys events when timestamps collide (preserves legacy ordering).
	sort.SliceStable(merged, func(i, j int) bool {
		ti := merged[i].Timestamp
		tj := merged[j].Timestamp
		if !ti.Equal(tj) {
			if ascending {
				return ti.Before(tj)
			}
			return ti.After(tj)
		}
		// Tie-break: step entries first (under both directions)
		if (merged[i].StepEntry != nil) != (merged[j].StepEntry != nil) {
			return merged[i].StepEntry != nil
		}
		return false
	})

	return merged
}

// seedHistoricalSysEvents generates SPAWN and EXIT events for processes that were
// already in their final state when the dashboard started. This ensures the timeline
// shows meaningful history when opened after a multi-agent run has already completed.
func seedHistoricalSysEvents(processes []vfs.ProcInfo) []UnifiedEvent {
	var events []UnifiedEvent
	for _, p := range processes {
		// Seed SPAWN event using CreatedAt timestamp
		spawnTs := p.CreatedAt
		if spawnTs.IsZero() {
			spawnTs = time.Now()
		}
		events = append(events, UnifiedEvent{
			Type:      EventSpawn,
			Severity:  SevInfo,
			Timestamp: spawnTs,
			PID:       p.PID,
			UUID:      p.UUID,
			Summary:   fmt.Sprintf("↑ SPAWN PID %d %q", p.PID, p.Intent),
		})

		// Seed EXIT event only for dead processes
		if p.State != types.StateDead {
			continue
		}
		exitTs := p.DeadAt
		if exitTs.IsZero() {
			exitTs = spawnTs.Add(time.Millisecond)
		}
		sev := SevInfo
		if ui.IsFailedResult(p.Result) {
			sev = SevError
		}
		events = append(events, UnifiedEvent{
			Type:      EventExit,
			Severity:  sev,
			Timestamp: exitTs,
			PID:       p.PID,
			UUID:      p.UUID,
			Summary:   exitEventSummary(p.PID, p.Intent, p.Result),
			Detail:    p.Result,
		})
	}
	// Sort by timestamp so the timeline renders in chronological order.
	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp.Before(events[j].Timestamp)
	})
	return events
}

// sysEventDedup deduplicates system events using a seen map.
// Key format: "Type:PID:UUID:TimestampMs"
func sysEventDedup(events []UnifiedEvent, seen map[string]struct{}) []UnifiedEvent {
	var result []UnifiedEvent
	for _, e := range events {
		key := fmt.Sprintf("%s:%d:%s:%d", e.Type, e.PID, e.UUID, e.Timestamp.UnixMilli())
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, e)
	}
	return result
}

// sysEventFIFO trims system events to maxSysEvents, keeping the newest.
func sysEventFIFO(events []UnifiedEvent, seen map[string]struct{}) []UnifiedEvent {
	if len(events) <= maxSysEvents {
		return events
	}
	// Prune dedup keys for evicted events
	evicted := events[:len(events)-maxSysEvents]
	for _, e := range evicted {
		key := fmt.Sprintf("%s:%d:%s:%d", e.Type, e.PID, e.UUID, e.Timestamp.UnixMilli())
		delete(seen, key)
	}
	return events[len(events)-maxSysEvents:]
}

// pruneBudgetAlertSeen removes entries for PIDs no longer in the process list.
func pruneBudgetAlertSeen(alertSeen map[types.PID]int, processes []vfs.ProcInfo) {
	active := make(map[types.PID]struct{}, len(processes))
	for _, p := range processes {
		active[p.PID] = struct{}{}
	}
	for pid := range alertSeen {
		if _, exists := active[pid]; !exists {
			delete(alertSeen, pid)
		}
	}
}

// --- helpers ---

// exitEventSummary builds the summary line for an EXIT event.
// For failed exits with a non-empty result, the result snippet replaces the intent
// so the user can see why the process failed directly in the timeline.
// For successful or result-less exits, shows the intent as before.
func exitEventSummary(pid types.PID, intent, result string) string {
	if result != "" && ui.IsFailedResult(result) {
		snippet := truncateRuneSafe(result, 70)
		return fmt.Sprintf("↓ EXIT PID %d ✗ %s", pid, snippet)
	}
	return fmt.Sprintf("↓ EXIT PID %d %q", pid, intent)
}

func inferExitError(p vfs.ProcInfo) bool {
	if p.Result == "" {
		return false
	}
	lower := strings.ToLower(p.Result)
	return strings.Contains(lower, "error") || strings.Contains(lower, "fail") || strings.Contains(lower, "timeout")
}

func getArgInt(args map[string]any, key string) int {
	v, ok := args[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return 0
	}
}

func getArgFloat(args map[string]any, key string) float64 {
	v, ok := args[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return 0
	}
}

// fetchCompactEventsCmd returns a tea.Cmd that fetches syscall events for a process.
// Creates its own IPC client to avoid racing with the synchronous dashboardTick.
func fetchCompactEventsCmd(pid types.PID, uuid string) tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return compactEventsMsg{pid: pid, uuid: uuid, err: err}
		}
		defer client.Close()
		events, err := client.ListEvents(pid, uuid)
		return compactEventsMsg{pid: pid, uuid: uuid, events: events, err: err}
	}
}

// markPaneUnread sets the unread flag for the given pane (Story 38-4 AC#1).
//
// Returns the updated dashboardModel; callers MUST reassign with the
// returned value (`m = m.markPaneUnread(paneTimeline)`) to persist the
// change under Bubble Tea's value-passing semantics. This mirrors the
// existing `selectProcess` / `clearSearchState` pattern.
//
// Behaviour:
//   - paneType is a fixed iota (paneTree..paneEval); guard with bounds
//     check so a future enum addition cannot corrupt memory.
//   - No-op if the flag is already set (avoids redundant writes that could
//     otherwise be picked up as state changes by an observer).
//
// Performance: O(1), one bounds compare + one bool write.
//
// ASCII fallback: this is a state mutator only; ASCII rendering happens
// later in renderPanelTabsLine (`*` instead of `●`).
func (m dashboardModel) markPaneUnread(p paneType) dashboardModel {
	if int(p) < 0 || int(p) >= len(m.paneHasUnread) {
		return m
	}
	if !m.paneHasUnread[p] {
		m.paneHasUnread[p] = true
	}
	return m
}

// clearPaneUnread removes the unread flag for the given pane (Story 38-4 AC#1).
//
// Called from every pane-switch path: Layer 0 keys 1 / 2-8, Layer 1
// Default tab / shift+tab, plus the AC2/AC3/AC4 cross-pane drill-in
// paths. Idempotent and bounds-checked.
//
// Performance: O(1).
func (m dashboardModel) clearPaneUnread(p paneType) dashboardModel {
	if int(p) < 0 || int(p) >= len(m.paneHasUnread) {
		return m
	}
	m.paneHasUnread[p] = false
	return m
}

// eventTargetPane maps an event Type to the pane that owns it for the
// purposes of cross-pane unread marking (Story 38-4 AC#1 mapping table).
//
// Returns (pane, ok). When ok==false the event has no pane mapping and
// SHOULD NOT trigger a mark — most notably EventImmune which is handled
// via the dedicated AC#4 synthSecurityAlerts path so that synth events
// never appear twice. Step / Compact / Budget / Stall / Error / Syscall
// / Spawn / Exit all flow into Timeline.
func eventTargetPane(eventType string) (paneType, bool) {
	switch eventType {
	case EventStep, EventCompact, EventBudget, EventStall, EventError, EventSyscall, EventSpawn, EventExit:
		return paneTimeline, true
	case EventImmune:
		return paneSecurity, true
	default:
		return paneTimeline, false
	}
}

// unreadEventKey returns a stable identity key for an UnifiedEvent used by
// applyUnreadMarks to detect which rows are NEW relative to the previous
// tick (Story 38-4 patch P3 — replaces the brittle length-only gate).
//
// Composition: Type + PID + UUID + Timestamp.UnixNano(). Two events with
// identical fingerprints are considered the same row across ticks; this
// matches the existing sysEventDedup key shape.
func unreadEventKey(ev *UnifiedEvent) string {
	return fmt.Sprintf("%s:%d:%s:%d", ev.Type, ev.PID, ev.UUID, ev.Timestamp.UnixNano())
}

// applyUnreadMarks scans the latest unifiedEvents and marks any non-active
// target panes as unread (Story 38-4 AC#1).
//
// Patch P3 (2026-05-03): the previous implementation used a single
// `prevUnifiedEventCount int` length gate which silently failed in two
// regression scenarios:
//  1. PID switch — `mergeUnifiedEvents` rebuilds the slice from a different
//     filter set; the new len is often smaller, `len(curr) > prev` is false,
//     so the first batch on the new PID never raises a red dot.
//  2. Ring-buffer trim — `mergeUnifiedEvents` may swap rows in place when
//     sysEvents reaches `maxSysEvents`; length stays constant while
//     contents change, so the new event is invisible to the gate.
//
// The fix uses an identity set: every tick we compute the fingerprint of
// the current unifiedEvents and mark a pane unread only for events whose
// fingerprint is NOT in the previous tick's set. PID switches reset the
// set to nil via `handlePIDChange`, which triggers a one-shot "sync without
// marking" path here so the first post-switch tick does not flood the user
// with red dots for events they just selected to look at.
//
// Performance: O(N) over unifiedEvents per tick; N is capped by
// mergeUnifiedEvents to ~maxSysEvents+stepEntries (a few hundred at most).
func (m dashboardModel) applyUnreadMarks() dashboardModel {
	currKeys := make(map[string]struct{}, len(m.unifiedEvents))
	firstSync := m.lastUnreadEventKeys == nil
	for i := range m.unifiedEvents {
		ev := &m.unifiedEvents[i]
		k := unreadEventKey(ev)
		currKeys[k] = struct{}{}
		if firstSync {
			continue
		}
		if _, seen := m.lastUnreadEventKeys[k]; seen {
			continue
		}
		target, ok := eventTargetPane(ev.Type)
		if !ok || target == m.activePane {
			continue
		}
		m = m.markPaneUnread(target)
	}
	m.lastUnreadEventKeys = currKeys
	return m
}

// resolveAlertJumpTarget consumes a pending alertJumpTarget (set by the
// alert+enter path on PID change) and locates the matching cursor on the
// destination pane. Story 38-4 AC#2 extends this to handle EventImmune
// targets by scanning m.security.Alerts.
//
// Code-review patch P5 (2026-05-03): the EventImmune match now requires
// PID + Timestamp.UnixMilli to align — same rationale as the same-PID
// branch in dashboard_keylayers.go: a single agent may have multiple
// deviations sharing the same PID and a PID-only match would silently
// snap to the first one, contradicting which row the user pressed enter
// on. When the precise pair is missing (synth row pruned between tick
// and keypress) we leave the cursor untouched rather than guessing.
func (m dashboardModel) resolveAlertJumpTarget() dashboardModel {
	if m.alertStrip.JumpTarget == nil {
		return m
	}
	target := m.alertStrip.JumpTarget
	m.alertStrip.JumpTarget = nil
	if target.Type == EventImmune {
		targetMs := target.Timestamp.UnixMilli()
		for i, sa := range m.security.Alerts {
			if types.PID(sa.PID) == target.PID && sa.TimestampMs == targetMs {
				m.security.Cursor = i
				securityAdjustScroll(&m)
				break
			}
		}
		return m
	}
	filtered := m.filteredUnifiedEvents()
	for i, ev := range filtered {
		if ev.Type == target.Type && ev.Timestamp.Equal(target.Timestamp) && ev.PID == target.PID {
			m.timeline.StepCursor = i
			break
		}
	}
	return m
}

// (diffNewEventsToUnread was removed in Story 38-4 patch P3 — the
// length-based gate it served was replaced by applyUnreadMarks's
// identity-set comparison, which handles PID-switch and ring-buffer
// trim cases the length gate silently skipped.)
