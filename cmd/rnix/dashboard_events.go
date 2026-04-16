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
	if e.summary.HasError {
		sev = SevError
	}
	// Use real timestamp from step record if available; fall back to synthetic ordering
	ts := baseTime.Add(time.Duration(index) * time.Millisecond)
	if e.summary.TimestampMs > 0 {
		ts = baseTime.Add(time.Duration(e.summary.TimestampMs) * time.Millisecond)
	}
	summary := fmt.Sprintf("[%d] %s — %s (%.0fms)",
		e.summary.Step, e.summary.Action, e.summary.Summary, e.summary.DurationMs)
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
		durSec := sp.StalledDurationMs / 1000
		events = append(events, UnifiedEvent{
			Type:      EventStall,
			Severity:  SevError,
			Timestamp: time.Now(),
			PID:       sp.PID,
			UUID:      sp.UUID,
			Summary:   fmt.Sprintf("⚠ STALL PID %d no heartbeat %ds", sp.PID, durSec),
			Detail:    fmt.Sprintf("stalled_duration_ms=%d consecutive_stalls=%d last_action=%s", sp.StalledDurationMs, sp.ConsecutiveStalls, sp.LastAction),
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
func mergeUnifiedEvents(stepEntries []stepEntry, sysEvents []UnifiedEvent, selectedPID types.PID, selectedUUID string, processes []vfs.ProcInfo) []UnifiedEvent {
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

	// Sort by timestamp descending (newest first)
	sort.Slice(merged, func(i, j int) bool {
		return merged[i].Timestamp.After(merged[j].Timestamp)
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
