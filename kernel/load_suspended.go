package kernel

import (
	"fmt"
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

		// Re-attach ProjectConfig. Without this, placeholders for processes
		// that used a project-only LLM provider (e.g. EchoMatrix's
		// opencodego) fail their resume with "device not found:
		// /dev/llm/<provider>" because the global VFS has no such device.
		// resolveProjectContext (wrapped by k.projectConfigLoader) re-runs
		// the same .env / providers.yaml / loader merge that handleSpawn
		// performs, so the revived placeholder behaves identically to a
		// freshly-spawned one with the same ProjectDir.
		//
		// projectDirCandidate resolution order:
		//   1. info.ProjectDir from proc-info.json — set by post-Epic-44.8
		//      spawn/suspend writes; the authoritative source.
		//   2. Fallback: filepath.Dir(k.stepDataDir). The daemon spawned
		//      these processes from its own cwd, and SetStepDataDir uses
		//      filepath.Join(cwd, ".rnix"), so the parent of stepDataDir
		//      points back at the original project root. Without this fallback
		//      every legacy snapshot — written before project_dir persistence
		//      landed — would be silently stuck after a daemon restart.
		//
		// Best-effort: a loader error degrades to "no project context", in
		// which case the placeholder still loads but its first resume will
		// surface the underlying error (missing device / bad providers.yaml)
		// to the user instead of silently hanging.
		projectDirCandidate := info.ProjectDir
		if projectDirCandidate == "" && k.stepDataDir != "" {
			if guessed := filepath.Dir(k.stepDataDir); guessed != "" && guessed != "." && guessed != "/" {
				projectDirCandidate = guessed
				log.Printf("[load_suspended] uuid=%s: project_dir missing on disk — fallback to filepath.Dir(stepDataDir) = %q",
					info.UUID, projectDirCandidate)
			}
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
		if k.stepDataDir != "" {
			stepsDir := filepath.Join(k.stepDataDir, "data", "steps", info.UUID)
			if _, _, rehErr := k.rehydrateRuntimeStateFromDisk(proc, stepsDir, info.CtxSize, 0); rehErr != nil {
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
