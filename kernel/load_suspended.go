package kernel

import (
	"log"
	"path/filepath"
	"slices"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
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
	if k.dataDir == "" {
		return 0, nil
	}

	var candidates []vfs.ProcInfo
	for _, baseDir := range AllBaseDirs(k.dataDir) {
		c, err := ListResumable(baseDir)
		if err != nil {
			log.Printf("[load_suspended] warn: scan %s: %v", baseDir, err)
			continue
		}
		candidates = append(candidates, c...)
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
		proc.ReasoningEffort = info.ReasoningEffort
		// PrimaryDevice — Epic 44 follow-up fix: persist + restore so a
		// daemon-restart placeholder routes through resumeOneForSubtree's
		// reasonStep-driven branch instead of falling into the PrimaryDevice==""
		// script-runner branch (which silently returns without launching
		// reasonStep — the "Running but no progress" bug).
		//
		// Legacy compatibility: snapshots written before primary_device was
		// persisted carry an empty string. When Provider is set we derive
		// PrimaryDevice via the same resolveLLMDevice helper that Spawn/Resume
		// use, so old proc-info.json files still revive correctly. Failure
		// here leaves PrimaryDevice="" — dashboard `r` will then misroute,
		// but the user can still rescue the placeholder via `rnix resume`.
		proc.PrimaryDevice = info.PrimaryDevice
		if proc.PrimaryDevice == "" && info.Provider != "" {
			if dev, _, derr := k.resolveLLMDevice(nil, info.Provider); derr == nil {
				proc.PrimaryDevice = dev
			} else {
				log.Printf("[load_suspended] uuid=%s: cannot derive PrimaryDevice for provider %q: %v (placeholder loaded but dashboard `r` may misroute)",
					info.UUID, info.Provider, derr)
			}
		}
		proc.ContextWindow = info.ContextWindow
		proc.AllowedDevices = append([]string(nil), info.AllowedDevices...)
		// Story 37.6 — restore the device blocklist symmetric to AllowedDevices so
		// a Suspended process revived after a daemon restart keeps the
		// anti-recursive-orchestration guard (deny /dev/intent) installed at spawn.
		proc.DeniedDevices = append([]string(nil), info.DeniedDevices...)
		// Story 54.1 (NFR87) — restore the tool-name whitelist symmetric to
		// AllowedDevices; legacy suspended snapshots without it rebuild by
		// expanding AllowedDevices so revived processes keep tool-level enforcement.
		k.restoreAllowedTools(proc, info.AllowedTools)
		proc.ContextBudget = info.ContextBudget
		proc.ComposeNode = info.ComposeNode
		proc.ComposeDeps = append([]string(nil), info.ComposeDeps...)
		proc.PipelineIndex = info.PipelineIndex
		proc.PipelineTotal = info.PipelineTotal
		proc.ResumedFromStep = info.ResumedFromStep
		// Story 71.3 AC5 — restore the explicit compact timeout so rehydrate's
		// resolveCompactTimeout sees a non-zero field and skips derivation.
		// Without this, daemon restart silently replaces the operator's explicit
		// value with a derived one, and the next SaveProcInfo (flag=false)
		// permanently erases it from disk. Shape copied from resume.go:965-967.
		if info.CompactTimeout > 0 {
			proc.CompactTimeout = info.CompactTimeout
			proc.compactTimeoutExplicit = true
		}

		proc.mu.Lock()
		proc.SuspendReason = info.SuspendReason
		// Story 73.3 / D1 — restore the quota wake instant: daemon restart must
		// not lose the window reset time, or the quota wake scanner would never
		// fire for placeholders (the whole "95.5h window survives daemon
		// restarts" value proposition).
		proc.ResumeAt = info.ResumeAt
		proc.pausedAt = info.PausedAt
		proc.pausedTotal = info.PausedTotal
		proc.TokensUsed = info.TokensUsed
		// Story 74.2 — third restore path (epic 漏列, create-story 修正 3):
		// daemon-restart placeholder revival must carry the four-way
		// cumulatives or suspend → restart → resume zeroes them.
		proc.CumInputTokens = info.CumInputTokens
		proc.CumCachedInputTokens = info.CumCachedInputTokens
		proc.CumCacheCreationInputTokens = info.CumCacheCreationInputTokens
		proc.CumOutputTokens = info.CumOutputTokens
		// Story 71.1 code-review P1: same rationale as resumeFromHistory — the
		// backpressure numerator must be live from step one, not 0 until the
		// first post-resume LLM response.
		proc.LastInputTokens = info.LastInputTokens
		proc.CreatedAt = info.CreatedAt
		proc.LastHeartbeat = info.LastHeartbeat
		// Story 48.1 — pre-populate MCPMounts / mcpConfigs from disk so
		// downstream rehydrate consumers (mcp_instructions section if the
		// SystemPrompt fallback path triggers) see the original set BEFORE
		// reattachMCPMounts (called after Resurrect emit) prunes failures.
		if len(info.MCPMounts) > 0 {
			paths := make([]string, 0, len(info.MCPMounts))
			cfgs := make([]vfs.MCPConfig, 0, len(info.MCPMounts))
			for _, m := range info.MCPMounts {
				paths = append(paths, m.Path)
				cfgs = append(cfgs, m.Config)
			}
			proc.MCPMounts = paths
			proc.mcpConfigs = cfgs
		}
		proc.mu.Unlock()

		// Re-attach ProjectConfig so project-only LLM providers are available.
		projectDirCandidate := info.ProjectDir
		if projectDirCandidate == "" {
			log.Printf("[load_suspended] uuid=%s: project_dir missing in proc-info.json — skipping project context", info.UUID)
		}
		if projectDirCandidate != "" && k.projectConfigLoader != nil {
			if pcfg, perr := k.projectConfigLoader(projectDirCandidate); perr != nil {
				log.Printf("[load_suspended] uuid=%s: projectConfigLoader(%q) failed: %v — placeholder loaded without project context",
					info.UUID, projectDirCandidate, perr)
			} else if pcfg != nil {
				proc.ProjectConfig = pcfg
				if pcfg.ProjectDir != "" {
					k.vfs.SetWorkDir(proc.PID, pcfg.ProjectDir)
				}
			}
		}

		// Rehydrate the in-memory runtime state (ctx + tool defs + skill
		// bodies + mcp paths) before the state transition so an observable
		// "Suspended" placeholder is already complete. resumeOneForSubtree
		// must NOT have to rebuild any of this — without it the placeholder
		// is a stripped-down shadow and tool calls / BuildPrompt fail on the
		// next resume.
		//
		// Best-effort: when stepDataDir is empty (some test harnesses) or the
		// on-disk artifacts are corrupted, log + skip the placeholder rather
		// than publish a half-ctx revived proc that crashes on resume. The
		// skip is silent to procTable callers — daemon comes up either way.
		//
		// Reasoned-tool placeholders are required to carry primary_device on
		// disk. When the snapshot predates this field (legacy data) we still
		// load the placeholder so the user can manually `rnix resume <uuid>`
		// — that path goes through ResumeWithOpts → resumeFromHistory which
		// re-derives PrimaryDevice from Provider. The dashboard `r` path,
		// however, will misroute legacy placeholders into the script-runner
		// branch until a re-resume rewrites their proc-info.json.
		if baseDir := FindBaseDirByUUID(k.dataDir, info.UUID); baseDir != "" {
			stepsDir := filepath.Join(baseDir, "steps", info.UUID)
			if _, _, rehErr := k.rehydrateRuntimeStateFromDisk(proc, stepsDir, 0, 0); rehErr != nil {
				log.Printf("[load_suspended] rehydrate uuid=%s failed: %v — skipping placeholder", info.UUID, rehErr)
				continue
			}
		}

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
			if proc.CtxID > 0 {
				_ = k.ctxMgr.CtxFree(proc.CtxID)
				proc.CtxID = 0
			}
			continue
		}
		if err := proc.Suspend(); err != nil {
			log.Printf("[load_suspended] suspend uuid=%s failed: %v", info.UUID, err)
			if proc.CtxID > 0 {
				_ = k.ctxMgr.CtxFree(proc.CtxID)
				proc.CtxID = 0
			}
			continue
		}

		k.procTable.Store(proc.PID, proc)
		k.msgQueues.Store(proc.PID, newMessageQueue())

		// Attach event/step writers BEFORE the Resurrect emit so the
		// Resurrect + downstream Mount events land in events.jsonl rather
		// than the [event-drop] warning sink. Suspended placeholders never
		// run reasonStep (the legacy attachment site), so without this
		// hook every event a placeholder ever emits — across this load
		// cycle and any subsequent dashboard inspection — would be lost
		// from disk. Story 48.1 code-review P1.
		k.attachStepObservation(proc)

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

		// Story 48.1 — Re-mount MCP transports for the revived placeholder.
		// Sequenced AFTER procTable.Store + Resurrect emit so the Mount
		// events follow Resurrect in chronological order on DebugChan (the
		// only sink available here — LoadSuspendedFromDisk does not yet
		// attach an EventWriter; tests assert via drainMountEvents).
		// daemon_restart_recovery=true is set automatically by the helper
		// when mountMgr was empty AND info.State==suspended (AC5).
		k.reattachMCPMounts(proc, info)

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

// AutoResumeDaemonShutdown resumes all processes that were suspended as
// collateral of a daemon stop (SuspendReason=="daemon_shutdown"). These
// processes were not intentionally paused by the user — the daemon shutdown
// interrupted them mid-flight. Auto-resuming restores the pre-shutdown
// execution state so `rnix apply` reconnection (Phase 1) finds them Running
// rather than Suspended.
func (k *KernelImpl) AutoResumeDaemonShutdown() int {
	var targets []string
	k.procTable.Range(func(_ types.PID, proc *Process) bool {
		if proc.GetState() == types.StateSuspended && proc.GetSuspendReason() == "daemon_shutdown" {
			targets = append(targets, proc.UUID)
		}
		return true
	})

	resumed := 0
	for _, uuid := range targets {
		if _, err := k.ResumeWithOpts(uuid, ResumeOpts{}); err != nil {
			log.Printf("[kernel] auto-resume daemon_shutdown uuid=%s failed: %v", uuid, err)
			continue
		}
		log.Printf("[kernel] auto-resumed daemon_shutdown process uuid=%s", uuid)
		resumed++
	}
	return resumed
}
