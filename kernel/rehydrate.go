package kernel

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// rehydrateRuntimeStateFromDisk reconstructs the in-memory runtime state that a
// reasonStep-driven process needs to behave like a freshly-spawned one. Used by
// both kernel/resume.go:resumeFromHistory (full resume) and
// kernel/load_suspended.go:LoadSuspendedFromDisk (Suspended placeholder
// revival). Keeping the two paths funneled through one helper prevents the
// silent-divergence bug that caused dashboard `r` on a daemon-restart
// placeholder to misroute through subtree.go:resumeOneForSubtree's
// PrimaryDevice=="" branch.
//
// Inputs:
//   - proc           — process whose identity / Skills / AllowedDevices are
//     already set. proc.PrimaryDevice must be set by the
//     caller (rehydrate does NOT open the LLM FD).
//   - stepsDir       — absolute path to <baseDir>/data/steps/<uuid>.
//   - ctxSizeHint    — requested ctx slot ceiling; 0 (production default since
//     Story 71.1) = no ceiling. See step 6 for why the old
//     "fall back to the snapshot's CtxSize" defence was dropped.
//   - maxStep        — Story 42.3 history-replay truncation. 0 = no truncation
//     (LoadSuspendedFromDisk default). > 0 = consider only
//     records with step <= maxStep when picking lastStep /
//     messages; the file is still scanned for totalSteps.
//
// Returns (lastStep, totalSteps, err). totalSteps is the highest step number
// observed in steps.jsonl regardless of any caller-side truncation; callers
// use it for FromStep range validation (resumeFromHistory).
//
// On success proc now has:
//   - proc.CtxSize / proc.CtxID — freshly-allocated ctx with messages +
//     system prompt deserialized in.
//   - proc.nativeToolDefs + proc.toolMap — VFS tool defs from the live
//     DeviceRegistry filtered by proc.AllowedDevices.
//   - proc.SkillBodies + proc.SkillDirs — loaded via k.skillLoader.
//   - proc.mcpDevicePaths — /dev/mcp/* or /mnt/mcp/* entries extracted from
//     proc.AllowedDevices.
//   - proc.LastCompletedStep — set to lastStep (so subtree.go fallback startStep
//     computation does not have to re-parse steps.jsonl).
//
// On failure no partial state is published: any allocated ctx is freed.
// Returned error is already a SyscallError suitable for IPC propagation.
func (k *KernelImpl) rehydrateRuntimeStateFromDisk(proc *Process, stepsDir string, ctxSizeHint int, maxStep int) (int, int, error) {
	if proc == nil {
		return 0, 0, NewSyscallError("Rehydrate", 0, "",
			fmt.Errorf("nil proc"), types.ErrInvalid)
	}
	if stepsDir == "" {
		return 0, 0, NewSyscallError("Rehydrate", proc.PID, "",
			fmt.Errorf("empty stepsDir"), types.ErrInvalid)
	}

	// 1. Read process-meta.json for system_prompt.
	//
	// Pre-Epic-44.8 daemons only wrote process-meta.json on reap, leaving
	// Suspended placeholders without one. We now write it on suspend too
	// (kernel/suspend.go), but legacy snapshots on disk still lack the file.
	// For those we fall back to synthesizing a fresh SystemPrompt by re-running
	// the section registry. The fallback is intentionally lossy — agent_instructions
	// is empty because we did not persist the agent identity — but it lets the
	// placeholder revive instead of being permanently silently broken.
	systemPrompt := ""
	metaPath := filepath.Join(stepsDir, "process-meta.json")
	metaData, metaErr := os.ReadFile(metaPath)
	switch {
	case metaErr == nil:
		var meta struct {
			SystemPrompt string `json:"system_prompt"`
		}
		if err := json.Unmarshal(metaData, &meta); err != nil {
			return 0, 0, NewSyscallError("Rehydrate", proc.PID, "",
				fmt.Errorf("parse process-meta.json: %w", err), types.ErrInternal)
		}
		systemPrompt = meta.SystemPrompt
	case os.IsNotExist(metaErr):
		log.Printf("[rehydrate] uuid=%s: process-meta.json missing (legacy snapshot or pre-suspend-meta daemon) — will synthesize fallback prompt", proc.UUID)
	default:
		return 0, 0, NewSyscallError("Rehydrate", proc.PID, "",
			fmt.Errorf("read process-meta.json: %w", metaErr), types.ErrInternal)
	}

	// 2. Parse steps.jsonl. maxStep applies the Story 42.3 FromStep truncation
	//    semantics when > 0; 0 keeps the legacy "use last record" behavior used
	//    by LoadSuspendedFromDisk.
	stepsPath := filepath.Join(stepsDir, "steps.jsonl")
	lastStep, messages, totalSteps, err := k.parseStepsJSONL(stepsPath, maxStep)
	if err != nil {
		return 0, 0, err
	}

	// 3. Rebuild toolMap + nativeToolDefs from VFS DeviceRegistry +
	//    AllowedDevices. Without this, the resumed/revived process has nil
	//    tool definitions and cannot perform any tool calls (LLM gets
	//    req.Tools = nil). Mirrors observe.go:682-690.
	vfsDefs, vfsMap := buildToolDefs(k.vfs.DeviceRegistry(), proc.AllowedDevices)
	metaDefs, metaMap := metaToolDefs(proc.FeatureFlags, proc.DeferredSkills)
	allDefs := make([]vfs.ToolDef, 0, len(vfsDefs)+len(metaDefs))
	allDefs = append(allDefs, vfsDefs...)
	allDefs = append(allDefs, metaDefs...)
	proc.mu.Lock()
	proc.nativeToolDefs = allDefs
	proc.toolMap = make(map[string]toolMapping, len(vfsMap)+len(metaMap))
	maps.Copy(proc.toolMap, vfsMap)
	maps.Copy(proc.toolMap, metaMap)
	proc.mu.Unlock()
	// 路线 B 的 MCP native ToolDef 在此之后由 reattachMCPMounts 末尾的
	// k.attachMCPToolDefs 追加——彼时 transport 已重连、proc.MCPMounts 已就绪。

	// 4. Rehydrate SkillBodies / SkillDirs from skillLoader (Edge Case Hunter
	//    Finding #15). Without these BuildPrompt degrades to skill names only
	//    and claude-cli bundle symlink creation fails. Loader-less or
	//    skill-less processes leave the maps empty — that matches the
	//    fresh-spawn path's behavior.
	if k.skillLoader != nil && len(proc.Skills) > 0 {
		bodies := make(map[string]string, len(proc.Skills))
		dirs := make(map[string]string, len(proc.Skills))
		for _, skillName := range proc.Skills {
			info, err := k.skillLoader(skillName)
			if err != nil || info == nil {
				log.Printf("[rehydrate] skill %q not loadable: %v (continuing with degraded prompt)", skillName, err)
				continue
			}
			if info.Body != "" {
				body := info.Body
				if info.Dir != "" {
					body = "Base directory for this skill: " + info.Dir + "\n\n" + body
				}
				bodies[info.Manifest.Name] = body
				if info.Dir != "" {
					dirs[info.Manifest.Name] = info.Dir
				}
			}
		}
		proc.mu.Lock()
		proc.SkillBodies = bodies
		proc.SkillDirs = dirs
		proc.mu.Unlock()
	}

	// 5. Rebuild mcpDevicePaths.
	for _, dev := range proc.AllowedDevices {
		if strings.HasPrefix(dev, "/dev/mcp/") || strings.HasPrefix(dev, "/mnt/mcp/") {
			proc.mcpDevicePaths = append(proc.mcpDevicePaths, dev)
		}
	}

	// 6. Allocate a fresh context and deserialize the on-disk messages +
	//    system prompt into it.
	//
	//    Story 71.1 AC6-④: the ctxSizeHint defence — "prefer the snapshot's
	//    CtxSize so an N-step process revived into a 256-slot context does not
	//    silently lose messages past slot 256" — is obsolete, because there is no
	//    256-slot context to be revived into any more. A hint from a pre-71.1
	//    snapshot is the old default rather than an operator choice, so honouring
	//    it would reimpose exactly the ceiling that made the defence necessary.
	//    Both remaining callers therefore pass 0 and the parameter is accepted only
	//    so an explicitly-configured ceiling can still be exercised (tests, and any
	//    future caller that wants the escape hatch).
	//
	//    Either way this argument does not decide the final ceiling: the
	//    Deserialize call below forces ctx.MaxSize to 0 so a snapshot's
	//    `max_size: 256` cannot resurrect it.
	resumeCtxSize := max(ctxSizeHint, 0)
	proc.CtxSize = resumeCtxSize
	cid, allocErr := k.ctxMgr.CtxAlloc(resumeCtxSize)
	if allocErr != nil {
		return 0, 0, NewSyscallError("Rehydrate", proc.PID, "",
			fmt.Errorf("context alloc: %w", allocErr), types.ErrInternal)
	}
	proc.CtxID = cid

	ctxObj, getErr := k.ctxMgr.GetContext(cid)
	if getErr != nil {
		_ = k.ctxMgr.CtxFree(cid)
		proc.CtxID = 0
		return 0, 0, NewSyscallError("Rehydrate", proc.PID, "",
			fmt.Errorf("context get: %w", getErr), types.ErrInternal)
	}

	// Fallback synthesis when process-meta.json was missing. Run after
	// SkillBodies / mcpDevicePaths / Skills have been rebuilt so the
	// section registry sees a fully-furnished Process. agent_instructions
	// stays empty — we never persisted agent identity — but the static
	// sections (intro / system_rules / actions / using_devices / ...) plus
	// the dynamic ones (env_info / loaded_skills / mcp_instructions / ...)
	// are enough for the LLM to keep operating. The legacy resume is
	// intentionally lossy and surfaced via the log line above.
	if systemPrompt == "" {
		sections := registerSections(proc, k, "")
		proc.mu.Lock()
		proc.HasSections = true
		proc.sections = sections
		proc.synthesizedPrompt = true
		proc.mu.Unlock()
		systemPrompt = sections.Build()
		log.Printf("[rehydrate] uuid=%s: synthesized fallback SystemPrompt (%d bytes, agent_instructions empty)",
			proc.UUID, len(systemPrompt))
	}

	snapshot := struct {
		SystemPrompt string            `json:"system_prompt"`
		Messages     []rnixctx.Message `json:"messages"`
		MaxSize      int               `json:"max_size"`
	}{
		SystemPrompt: systemPrompt,
		Messages:     messages,
		MaxSize:      resumeCtxSize,
	}
	snapJSON, _ := json.Marshal(snapshot)
	if err := ctxObj.Deserialize(snapJSON); err != nil {
		_ = k.ctxMgr.CtxFree(cid)
		proc.CtxID = 0
		return 0, 0, NewSyscallError("Rehydrate", proc.PID, "",
			fmt.Errorf("context rebuild: %w", err), types.ErrInternal)
	}

	// 7. Story 69.2 — apply the token scale to the context just allocated above.
	//    Both callers (resumeFromHistory and LoadSuspendedFromDisk) have already
	//    inherited ContextWindow / ContextBudget from their on-disk snapshot
	//    (proc-info.json persists context_window), so this is the one place both
	//    disk-backed paths get the connection. resolveContextBudget re-reads
	//    providers.yaml via contextWindowFunc, so a window edited since the
	//    snapshot was written wins over the stale inherited value. Without this,
	//    a revived process measures against DefaultTokenLimit no matter what the
	//    provider declares. Scale chain: kernel/ctx_token_limit.go.
	k.resolveContextBudget(proc)
	k.applyCtxTokenLimit(proc)

	// Story 71.3 AC5 — re-derive the compact timeout on the disk-backed resume
	// funnel (resumeFromHistory + LoadSuspendedFromDisk both reach here). When
	// the process had an explicit compact_timeout (opts/manifest), the caller
	// already restored it from proc-info.json into proc.CompactTimeout (+ the
	// explicit flag) before invoking rehydrate, so resolveCompactTimeout sees a
	// non-zero field and only checks for inversion. When nothing was explicit,
	// the field is 0 and derivation fills it from the current providers.yaml —
	// picking up a timeout_sec edited since the snapshot was written, matching
	// resolveContextBudget's "providers.yaml is the current truth" principle.
	k.resolveCompactTimeout(proc)

	// 7b. Story 69.3 AC6 — preventive unload. The snapshot just deserialized can
	//     sit at or above the slot ceiling, in which case the revived process
	//     hits preCompactForToolCalls on its very first step: it replays exactly
	//     the failure it was suspended for. Reclaim mechanically now so there is
	//     headroom to run.
	//
	//     Placed here because this function is the shared funnel for BOTH
	//     disk-backed paths (resumeFromHistory and LoadSuspendedFromDisk), so a
	//     single call covers two callers (same reasoning as the token-scale
	//     hookup above). Order relative to applyCtxTokenLimit is irrelevant
	//     (unloading edits Messages, the scale edits TokenLimit), but it MUST
	//     precede proc.Start() / reasonStep — which it does: rehydrate runs
	//     inside the resume setup, while Start() happens later in the launch
	//     goroutine.
	k.unloadForResume(proc, resumeCtxSize, "rehydrate")

	// 8. Bookkeeping: record the highest replayed step so subtree.go's
	//    resumeOneForSubtree fallback (LastCompletedStep+1) lands on a sane
	//    startStep without re-parsing steps.jsonl. Use the atomic setter
	//    rather than touching the field directly so any future readers that
	//    race with us see the value via Load().
	if lastStep > 0 {
		proc.LastCompletedStep.Store(int64(lastStep))
	}

	return lastStep, totalSteps, nil
}

// reattachMCPMounts re-mounts every MCP server persisted in info.MCPMounts
// (Story 48.1). This is the ONLY entry point that issues mountMgr.Mount on
// the resume / load_suspended hot path; do NOT call mountMgr.Mount directly
// from resumeFromHistory or LoadSuspendedFromDisk.
//
// MUST be invoked AFTER k.AddProcess(proc) AND after any AttachEventWriter
// hook so emitEvent reaches both DebugChan and events.jsonl — calling it
// inside rehydrateRuntimeStateFromDisk would emit Mount events into the void
// (Edge Case Hunter Finding #8). Spawn's MCP mount loop (kernel/spawn.go:607-651)
// achieves the same ordering by emitting before AddProcess; that path is left
// untouched because it lives upstream of any persistence we control.
//
// Behaviour:
//   - info.MCPMounts empty / nil → no-op (AC6 zero-overhead path).
//   - k.mountMgr nil → log warning, no-op (test fixtures without mountMgr).
//   - Per mount: WorkDir fallback chain (original → os.Getwd → /tmp) when the
//     persisted dir is missing (AC4).
//   - Path already in mountMgr (AC7 fork-resume collision) → skip Mount call
//     but record path / config so toolMap rebuilds against the existing
//     DeviceRegistry entry; Mount event carries reused=true.
//   - mountMgr.Mount returning ErrAlreadyMounted is also treated as reuse.
//   - Per-server mount failure → emit Mount event with err, drop the server
//     from proc.MCPMounts / proc.AllowedDevices / proc.mcpConfigs (AC3 three-
//     way alignment), keep going so a single bad server does not strand the
//     rest of the agent.
//
// Returns the count of successfully (re)mounted servers — primarily for tests;
// production callers ignore it because partial failure is not a resume abort
// condition.
func (k *KernelImpl) reattachMCPMounts(proc *Process, info vfs.ProcInfo) int {
	if proc == nil || len(info.MCPMounts) == 0 {
		return 0
	}
	if k.mountMgr == nil {
		log.Printf("[rehydrate] uuid=%s: mount manager not initialized, skipping %d MCP mount(s) — resumed process will lack MCP tool access until daemon restart with mountMgr",
			proc.UUID, len(info.MCPMounts))
		return 0
	}

	// Capture entry-time mountMgr state so we can flag the AC5 daemon-restart-
	// recovery scenario (mountMgr empty + on-disk State=suspended). Snapshot
	// once outside the per-mount loop because the first successful mount in
	// the loop would otherwise flip the result for subsequent iterations.
	daemonRestartRecovery := info.State == types.StateSuspended && len(k.mountMgr.ListMounts()) == 0

	var (
		successPaths   []string
		successConfigs []vfs.MCPConfig
		reusedPaths    []string
		failedPaths    = make(map[string]struct{})
	)

	for _, snap := range info.MCPMounts {
		path := snap.Path
		// persistCfg is the value we publish back into proc.mcpConfigs so a
		// subsequent SaveProcInfo writes the ORIGINAL WorkDir back to disk.
		// Story 48.1 code-review P3 — the previous implementation mutated
		// cfg.WorkDir on fallback and appended that mutated value to
		// successConfigs, so the next reap silently overwrote the original
		// path and broke AC4's "restore the directory then resume" promise.
		persistCfg := snap.Config
		// mountCfg is the value handed to mountMgr.Mount — only this copy
		// gets the fallback WorkDir applied.
		mountCfg := snap.Config
		mountStart := time.Now()

		eventArgs := map[string]any{
			"path":   path,
			"auto":   true,
			"source": "resume",
		}
		if daemonRestartRecovery {
			eventArgs["daemon_restart_recovery"] = true
		}

		// AC4 — cwd fallback chain. Triggered when the persisted dir is set
		// AND os.Stat fails for ANY reason (missing, EACCES, ELOOP, …).
		// Story 48.1 code-review P10 — the prior IsNotExist-only check let
		// permission / loop errors fall through and the transport would
		// fail to chdir mid-startup, surfacing as an obscure exec error.
		if mountCfg.WorkDir != "" {
			if _, statErr := os.Stat(mountCfg.WorkDir); statErr != nil {
				original := mountCfg.WorkDir
				resolved := "/tmp"
				if wd, werr := os.Getwd(); werr == nil {
					if _, sErr := os.Stat(wd); sErr == nil {
						resolved = wd
					}
				}
				mountCfg.WorkDir = resolved
				eventArgs["cwd_fallback"] = true
				eventArgs["cwd_original"] = original
				eventArgs["cwd_resolved"] = resolved
				k.emitLog(proc, 0, types.LogOutput, fmt.Sprintf(
					"MCP server %q: original WorkDir %q unusable (%v), falling back to %q",
					mountCfg.ServerName, original, statErr, resolved), "")
			}
		}

		// AC7 — already mounted (fork-resume collision or daemon-internal
		// concurrent resume). Use Acquire so the MountManager bumps the
		// refcount; later finishProcess Unmount drops exactly one owner,
		// preventing the parent-kills-fork bug (code-review P2).
		alreadyMounted := false
		for _, existing := range k.mountMgr.ListMounts() {
			if existing.Path == path {
				alreadyMounted = true
				break
			}
		}

		var (
			mountErr error
			reused   bool
		)
		if alreadyMounted {
			if acqErr := k.mountMgr.Acquire(path); acqErr != nil {
				// Pre-check saw the mount but Acquire failed — almost
				// certainly the other owner Unmount'd in the race window
				// between ListMounts and Acquire. Fall back to a fresh
				// Mount; the worst case is one extra exec.Cmd, the best
				// case avoids stranding the resumed proc without a
				// transport.
				mountErr = k.mountMgr.Mount(path, mountCfg)
				if mountErr != nil && isAlreadyMountedErr(mountErr) {
					// Real race: someone re-mounted between our Acquire
					// failure and our Mount. Treat as reuse and retry
					// Acquire so we own a reference.
					if retryErr := k.mountMgr.Acquire(path); retryErr == nil {
						mountErr = nil
						reused = true
						eventArgs["reused"] = true
					}
				}
			} else {
				reused = true
				eventArgs["reused"] = true
			}
		} else {
			mountErr = k.mountMgr.Mount(path, mountCfg)
			// Defensive: Mount itself can race with another goroutine and
			// return ErrAlreadyMounted between our ListMounts check and the
			// Mount call. Demote to Acquire so we still hold a reference.
			if mountErr != nil && isAlreadyMountedErr(mountErr) {
				if acqErr := k.mountMgr.Acquire(path); acqErr == nil {
					mountErr = nil
					reused = true
					eventArgs["reused"] = true
				}
			}
		}

		k.emitEvent(proc, "Mount", eventArgs, nil, mountErr, time.Since(mountStart))

		if mountErr == nil {
			successPaths = append(successPaths, path)
			successConfigs = append(successConfigs, persistCfg)
			if reused {
				reusedPaths = append(reusedPaths, path)
			}
		} else {
			failedPaths[path] = struct{}{}
		}
	}

	// Three-way alignment: proc.MCPMounts (paths), proc.mcpConfigs (configs),
	// proc.AllowedDevices (LLM-prompt whitelist) MUST agree on the surviving
	// set. Without this AC3 fails with "声称有工具实则调不通" — the prompt
	// advertises a tool whose mount never came up. The alignment runs in two
	// directions: (a) failed paths are pruned from AllowedDevices, and (b)
	// successful paths are added if missing, so legacy snapshots whose
	// AllowedDevices was written before mcp paths were standardized do not
	// silently strand the freshly-mounted tools (code-review P8).
	proc.mu.Lock()
	hadSynthesizedSections := proc.HasSections && proc.sections != nil && proc.synthesizedPrompt
	proc.MCPMounts = successPaths
	proc.mcpConfigs = successConfigs
	proc.mcpReusedMounts = reusedPaths
	if len(failedPaths) > 0 || len(successPaths) > 0 {
		already := make(map[string]struct{}, len(proc.AllowedDevices))
		filtered := proc.AllowedDevices[:0:0]
		for _, dev := range proc.AllowedDevices {
			if _, drop := failedPaths[dev]; drop {
				continue
			}
			already[dev] = struct{}{}
			filtered = append(filtered, dev)
		}
		for _, p := range successPaths {
			if _, have := already[p]; have {
				continue
			}
			filtered = append(filtered, p)
		}
		proc.AllowedDevices = filtered
	}
	proc.mu.Unlock()

	// Story 48.1 code-review P5 — when rehydrate fell back to synthesizing
	// the system prompt (no process-meta.json), sections.Build() ran with
	// the PRE-prune mcpConfigs. If any mount just failed, the LLM's prompt
	// still advertises tools whose transport never came up — exactly the
	// "声称有工具实则调不通" symptom AC3 was meant to eliminate. Rebuild
	// the prompt now that mcpConfigs reflects the surviving set and push
	// the new text back into the context so the first reasoning step sees
	// a clean tool inventory.
	if hadSynthesizedSections && len(failedPaths) > 0 && k.ctxMgr != nil && proc.CtxID != 0 {
		var sections *rnixctx.SectionRegistry
		proc.mu.Lock()
		sections = proc.sections
		proc.mu.Unlock()
		if sections != nil {
			rebuilt := sections.Build()
			if err := k.ctxMgr.SetSystemPrompt(proc.CtxID, rebuilt); err != nil {
				log.Printf("[rehydrate] uuid=%s: rebuild system prompt after mount prune failed: %v", proc.UUID, err)
			}
		}
	}

	// 路线 B: transports are now reconnected and proc.MCPMounts reflects the
	// surviving set — expose each server's tools as native ToolDefs. The base/
	// meta tool set was rebuilt earlier in rehydrateRuntimeStateFromDisk (before
	// this reattach), so MCP tools are appended here. Covers both resume paths
	// (resume.go and load_suspended.go call reattachMCPMounts after rehydrate).
	k.attachMCPToolDefs(proc)

	return len(successPaths)
}

// isAlreadyMountedErr unwraps the given error chain searching for a
// types.DriverError whose Code == types.ErrAlreadyMounted. Returns true on
// match. Used by reattachMCPMounts to demote a Mount→ErrAlreadyMounted into
// the reuse path even when the ListMounts pre-check missed the race window.
func isAlreadyMountedErr(err error) bool {
	if err == nil {
		return false
	}
	var de *types.DriverError
	if errors.As(err, &de) {
		return de.Code == types.ErrAlreadyMounted
	}
	return false
}
