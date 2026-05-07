package title

import (
	"github.com/rnixai/rnix/internal/dashboard/event"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/vfs"
)

// ComputeHealthCounts aggregates error and warning counts from process state,
// recent events, and heartbeat status for the Title Bar health indicator
// (Story 34.2). Returns deduped counts so a single misbehaving process
// contributes at most 1 to each bucket regardless of how many error events
// it emits or how many budget thresholds it crosses.
//
// Story 38-5 PR11 Step 4(c) (2026-05-05 · 第 9 个会话第 11 个 commit):
// Migrated from cmd/rnix/dashboard_title.go::computeHealthCounts. Pure
// function · zero dashboardModel dependency · 0 cmd/rnix 反向依赖.
//
// Behavior contract (preserved verbatim from cmd/rnix · ATDD 34.2-UNIT-001
// 至 34.2-UNIT-008 测试覆盖):
//
//   - Errors (E):
//   - Process is StateDead && IsFailedResult(Result) → counted
//   - Event of EventError type → counted (PID-deduped)
//   - Event of EventExit type with Severity >= SevError → counted (PID-deduped)
//   - Multiple errors from same PID counted once
//   - Warnings (W):
//   - Running/Created processes with any of:
//   - ctx usage >= 80% (TokensUsed*100/ContextBudget)
//   - cost budget >= 80% (UsedCost*100/MaxCost)
//   - token budget >= 80% (TokensUsed*100/MaxTokens)
//   - Heartbeat-stalled processes (heartbeat.CurrentStalled)
//   - Multiple warnings from same PID counted once (deduped via map)
//
// 80% threshold matches PctColorStyle / StyleProviderName for visual
// consistency (Story 38.2 颜色一致性原则).
//
// Note: heartbeat == nil is allowed (initial dashboard tick before first
// heartbeat fetch returns); CurrentStalled iteration is skipped in that case.
func ComputeHealthCounts(
	processes []vfs.ProcInfo,
	events []event.UnifiedEvent,
	heartbeat *ipc.HeartbeatStatusResponse,
) (errorCount, warnCount int) {
	errorPIDs := make(map[types.PID]struct{})

	for _, p := range processes {
		if p.State == types.StateDead && ui.IsFailedResult(p.Result) {
			errorPIDs[p.PID] = struct{}{}
		}
	}

	for _, e := range events {
		if e.Type == event.EventError || (e.Type == event.EventExit && e.Severity >= event.SevError) {
			errorPIDs[e.PID] = struct{}{}
		}
	}

	warnPIDs := make(map[types.PID]struct{})
	for _, p := range processes {
		if p.State == types.StateRunning || p.State == types.StateCreated {
			// ctx usage >= 80%
			if p.ContextBudget > 0 && int64(p.TokensUsed)*100/int64(p.ContextBudget) >= 80 {
				warnPIDs[p.PID] = struct{}{}
			}
			// cost budget >= 80%
			if p.MaxCost > 0 && p.UsedCost*100/p.MaxCost >= 80 {
				warnPIDs[p.PID] = struct{}{}
			}
			// token budget >= 80%
			if p.MaxTokens > 0 && int64(p.TokensUsed)*100/p.MaxTokens >= 80 {
				warnPIDs[p.PID] = struct{}{}
			}
		}
	}

	if heartbeat != nil {
		for _, s := range heartbeat.CurrentStalled {
			warnPIDs[s.PID] = struct{}{}
		}
	}

	warnCount = len(warnPIDs)
	errorCount = len(errorPIDs)
	return
}
