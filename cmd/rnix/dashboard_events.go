package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/rnixai/rnix/internal/types"
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
	ts := baseTime.Add(time.Duration(index) * time.Millisecond)
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
				Summary:   fmt.Sprintf("↓ EXIT PID %d %q", pid, prevProc.Intent),
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
				Summary:   fmt.Sprintf("↓ EXIT PID %d %q", pid, currProc.Intent),
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
func mergeUnifiedEvents(stepEntries []stepEntry, sysEvents []UnifiedEvent, selectedPID types.PID) []UnifiedEvent {
	merged := make([]UnifiedEvent, 0, len(stepEntries)+len(sysEvents))

	// Convert step entries with monotonic timestamp offsets for stable ordering
	baseTime := time.Now()
	for i := range stepEntries {
		ue := stepToUnifiedEvent(&stepEntries[i], baseTime, i)
		ue.PID = selectedPID
		merged = append(merged, ue)
	}

	// Add system events
	merged = append(merged, sysEvents...)

	// Sort by timestamp descending (newest first)
	sort.Slice(merged, func(i, j int) bool {
		return merged[i].Timestamp.After(merged[j].Timestamp)
	})

	return merged
}

// sysEventDedup deduplicates system events using a seen map.
// Key format: "Type:PID:TimestampMs"
func sysEventDedup(events []UnifiedEvent, seen map[string]struct{}) []UnifiedEvent {
	var result []UnifiedEvent
	for _, e := range events {
		key := fmt.Sprintf("%s:%d:%d", e.Type, e.PID, e.Timestamp.UnixMilli())
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
		key := fmt.Sprintf("%s:%d:%d", e.Type, e.PID, e.Timestamp.UnixMilli())
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
