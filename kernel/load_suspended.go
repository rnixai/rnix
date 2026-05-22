package kernel

import (
	"fmt"
	"log"
	"slices"

	"github.com/rnixai/rnix/internal/types"
)

// LoadSuspendedFromDisk scans <stepDataDir>/data/steps/*/proc-info.json and
// restores any process with state=suspended back into procTable as a
// suspended placeholder. The placeholder has no reasonStep goroutine —
// daemon restart deliberately does not auto-resume; the user must invoke
// `rnix resume <uuid>` or dashboard `R` explicitly.
//
// Story 44.3 — designed for the "daemon stop / daemon start" round trip from
// Decker's 6-step reproduction. Dead / Zombie entries are intentionally
// ignored here — they belong to procHistory via LoadHistory, not procTable.
//
// Idempotent: a UUID already present in procTable (e.g., the loader was
// called twice) is skipped so daemon-restart loops do not duplicate entries.
//
// Returns the number of placeholders that were inserted into procTable.
func (k *KernelImpl) LoadSuspendedFromDisk() (int, error) {
	if k.stepDataDir == "" {
		return 0, nil
	}

	candidates, err := ListResumable(k.stepDataDir)
	if err != nil {
		return 0, fmt.Errorf("scan resumable: %w", err)
	}

	loaded := 0
	for _, info := range candidates {
		if info.State != types.StateSuspended {
			continue
		}
		if info.UUID == "" {
			continue
		}
		// Idempotent: a UUID already in procTable means a previous reload
		// already created the placeholder. Skip without rewriting state so
		// the loader can be safely called more than once across daemon
		// restart loops.
		if _, ok := k.GetProcessByUUID(info.UUID); ok {
			continue
		}

		proc := NewProcess(0, info.Intent, info.Skills)

		// Restore identity + metadata fields read from disk. These writes
		// happen before the placeholder is published to procTable so no
		// external observer can race with the mutation; no lock is required.
		proc.UUID = info.UUID
		proc.OriginUUID = info.OriginUUID
		proc.ParentUUID = info.ParentUUID
		// PPID intentionally left as 0 — the previous PID space is gone with
		// the daemon restart. UUID-based BuildProcessTree relies on
		// ParentUUID, not PPID, so the tree linkage survives.
		proc.CtxSize = info.CtxSize
		proc.MaxSteps = info.MaxSteps
		proc.Provider = info.Provider
		proc.Model = info.Model
		proc.ContextWindow = info.ContextWindow
		proc.AllowedDevices = append([]string(nil), info.AllowedDevices...)
		proc.ContextBudget = info.ContextBudget
		proc.ComposeNode = info.ComposeNode
		proc.ComposeDeps = append([]string(nil), info.ComposeDeps...)
		proc.PipelineIndex = info.PipelineIndex
		proc.PipelineTotal = info.PipelineTotal
		proc.ResumedFromStep = info.ResumedFromStep

		proc.mu.Lock()
		proc.SuspendReason = info.SuspendReason
		proc.pausedAt = info.PausedAt
		proc.pausedTotal = info.PausedTotal
		proc.TokensUsed = info.TokensUsed
		proc.CreatedAt = info.CreatedAt
		proc.LastHeartbeat = info.LastHeartbeat
		proc.mu.Unlock()

		// Carry the suspend-requested atomic so the placeholder is
		// behaviourally identical to one produced by SuspendSubtree (44.1
		// invariant — reasonStep / IsSuspendRequested observers must see the
		// same flag value regardless of whether the suspend happened pre- or
		// post-restart).
		proc.suspendRequested.Store(true)

		// Move through the legal state path Created → Running → Suspended
		// instead of introducing a direct Created→Suspended transition
		// (which would weaken the state machine for all other callers).
		// The Running window is unobservable: the placeholder is not in
		// procTable yet, no events fire, no goroutine is spawned.
		if err := proc.Start(); err != nil {
			log.Printf("[load_suspended] start uuid=%s failed: %v", info.UUID, err)
			continue
		}
		if err := proc.Suspend(); err != nil {
			log.Printf("[load_suspended] suspend uuid=%s failed: %v", info.UUID, err)
			continue
		}

		k.procTable.Store(proc.PID, proc)
		k.msgQueues.Store(proc.PID, newMessageQueue())

		// Emit a Resurrect audit-trail event so the Timeline pane can show
		// the daemon-restart anchor. Args carry trigger + provenance so
		// downstream consumers (Inspector / Timeline) can distinguish a
		// reload from a fresh Spawn.
		k.emitEvent(proc, "Resurrect", map[string]any{
			"pid":            proc.PID,
			"uuid":           proc.UUID,
			"from_disk":      true,
			"trigger":        "daemon_restart",
			"suspend_reason": info.SuspendReason,
		}, nil, nil, 0)

		loaded++
	}

	if loaded > 0 {
		log.Printf("[kernel] reloaded %d suspended placeholder(s) from disk", loaded)
	}

	// Second pass — rebuild parent.Children linkage via ParentUUID. Without
	// this, collectSubtreePIDs (kernel/subtree.go) walks an empty Children
	// slice and never sees the reloaded descendants, breaking the AC#4
	// subtree-wakeup guarantee on the next `rnix resume <parent.UUID>`.
	//
	// Done as a separate pass because ListResumable orders entries
	// most-recent-first (DeadAt / CreatedAt After) — children typically load
	// before their parent, so during the first pass parent.AddChild would
	// no-op when the parent is not yet in procTable.
	//
	// Run unconditionally (not gated on loaded>0): when a parent's proc-info
	// lands on disk in a later daemon cycle than its children, the children
	// were reloaded in an earlier call (so this call's loaded counts only the
	// parent, but the AddChild target is a child reloaded previously). The
	// scan is idempotent and cheap (one procTable.Range), so there is no
	// reason to risk leaving orphans stranded behind a fragile loaded>0 proxy.
	k.procTable.Range(func(_ types.PID, child *Process) bool {
		if child.ParentUUID == "" {
			return true
		}
		parent, ok := k.GetProcessByUUID(child.ParentUUID)
		if !ok {
			return true
		}
		// Idempotent: skip if already a child to survive double-reload.
		if slices.Contains(parent.GetChildren(), child.PID) {
			return true
		}
		parent.AddChild(child.PID)
		child.PPID = parent.PID
		return true
	})
	return loaded, nil
}
