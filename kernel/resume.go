package kernel

import (
	"bufio"
	gocontext "context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/config"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// ResumeResult holds the output of a successful Resume call.
type ResumeResult struct {
	PID             types.PID
	UUID            string
	ResumedFromStep int
}

// ResumeOpts holds options for the extended Resume call.
type ResumeOpts struct {
	Fork bool // true = new UUID + origin_uuid tracking; false = inherit original UUID

	// FromStep — Story 42.3 stub: truncates history replay to the first FromStep
	// records, then continues reasoning from step FromStep+1. 0 = no truncation.
	// Negative values are rejected at the resume entry. Effective only on the
	// history path; checkpoint path rejects (FromStep>0 + FromStep<cpData.LastStep).
	// Behavior wired in dev-story; ATDD red-phase: field exists, logic pending.
	FromStep int

	// ProjectConfig — Epic 42 fix: per-resume project snapshot mirroring SpawnOpts.
	// When non-nil, resume paths use ProjectConfig.LLMFileOpener instead of falling
	// back to the global VFS driver, so resumed processes inherit project-level
	// .env / providers.yaml / agent dirs. Required for full-state resume semantics
	// per cc-src loadConversationForResume. nil = global mode (back-compat).
	ProjectConfig *config.ProjectConfig

	// NewInput — apex 10-11 B-route continuation: appended as the next user turn
	// after the historical context is restored and before reasonStep starts, so a
	// resume continues the conversation with the user's newest input. Covers both
	// the checkpoint and the history path. On the history path a non-empty
	// NewInput also restores the previous round's final assistant output from
	// proc-info.json (anti-replay — parseStepsJSONL only carries the last step's
	// INPUT messages, so without the restored output the resumed LLM believes its
	// last turn never happened and replays it). Empty = pure resume, zero
	// behavior change.
	NewInput string

	// LaunchReady — apex 10-11 started-semantics fix: when non-nil, the resumed
	// process's reasoning goroutine blocks on this channel before it starts, so
	// the caller (the resume_watch IPC handler) can write its initial OK response
	// FIRST — the client's "started" signal then reflects a launched process
	// instead of racing the LLM round, and a pre-OK read failure can't leave a
	// running fork behind (double-run). The handler closes it right after writing
	// OK (and via a defer safety-net so the goroutine can never leak). nil (CLI /
	// dashboard / auto-resume / one-shot handleResume) = launch immediately, zero
	// behavior change.
	LaunchReady <-chan struct{}
}

// readProcInfoResult best-effort reads the previous round's final assistant
// output from proc-info.json's result field. Empty on any miss — the caller
// simply skips the anti-replay restoration.
func readProcInfoResult(stepsDir string) string {
	data, err := os.ReadFile(filepath.Join(stepsDir, "proc-info.json"))
	if err != nil {
		return ""
	}
	var info struct {
		Result string `json:"result"`
	}
	if json.Unmarshal(data, &info) != nil {
		return ""
	}
	return info.Result
}

// snapshotEndsWithAssistant reports whether the checkpoint context snapshot's
// last message is an assistant turn carrying finalOutput — i.e. the snapshot
// already contains the previous round's final output and the anti-replay
// restoration must not duplicate it.
func snapshotEndsWithAssistant(contextSnapshot json.RawMessage, finalOutput string) bool {
	var snap struct {
		Messages []rnixctx.Message `json:"messages"`
	}
	if json.Unmarshal(contextSnapshot, &snap) != nil || len(snap.Messages) == 0 {
		return false
	}
	last := snap.Messages[len(snap.Messages)-1]
	return last.Role == rnixctx.RoleAssistant &&
		strings.TrimSpace(last.Content) == strings.TrimSpace(finalOutput)
}

// injectResumeNewInput appends the caller's NewInput as the next user turn on a
// freshly-restored resume context, optionally preceded by the previous round's
// final assistant output (finalOutput; empty = skip — the restored context
// already ends with it). Called before the process is registered / reasonStep
// starts; on failure the caller frees the ctx and aborts the resume.
func (k *KernelImpl) injectResumeNewInput(proc *Process, uuid, newInput, finalOutput, path string) error {
	if res := strings.TrimSpace(finalOutput); res != "" {
		if apErr := k.ctxMgr.AppendMessage(proc.CtxID, rnixctx.RoleAssistant, res); apErr != nil {
			return fmt.Errorf("append final assistant output: %w", apErr)
		}
		log.Printf("[resume] uuid=%s: restored final assistant output (%d chars) before NewInput (%s path)",
			uuid, len(res), path)
	}
	if apErr := k.ctxMgr.AppendMessage(proc.CtxID, rnixctx.RoleUser, newInput); apErr != nil {
		return fmt.Errorf("append new input: %w", apErr)
	}
	log.Printf("[resume] uuid=%s: appended NewInput (%d chars) as next user turn (%s path)",
		uuid, len(newInput), path)
	return nil
}

// resolveResumeProjectConfig determines the effective ProjectConfig for a resume,
// mirroring the Spawn path's project-level provider escape hatch (spawn.go:588).
//
// The resumed process's OWN project is authoritative. Callers (CLI resume,
// dashboard r/fork, compose-resume) derive opts.ProjectConfig from the operator's
// cwd, which is the WRONG project whenever the operator runs from somewhere other
// than the project the process belongs to — that wrong config resolves the wrong
// (or no) project-level provider and fails the resume. So when the process's own
// project_dir is known (history: diskInfo.ProjectDir; checkpoint: persisted
// cp.ProjectDir; or oldProc.ProjectConfig.ProjectDir) AND differs from the
// caller-supplied opts.ProjectConfig's project, rebuild from the process's own
// project_dir and prefer it.
//
// Otherwise priority is unchanged (preserving spec-resume-project-provider-fix):
//  1. opts.ProjectConfig — caller-supplied. apply's reconnect-resume runs from the
//     process's own project, so opts == own (no mismatch, this branch is used).
//  2. oldProc.ProjectConfig — placeholder rebuilt by LoadSuspendedFromDisk.
//     AutoResumeDaemonShutdown resumes with an empty ResumeOpts{}, so for
//     daemon-self-start auto-resume of a project-level provider this is the
//     only in-memory source.
//  3. projectConfigLoader(projectDir) — last-resort rebuild from the on-disk
//     project_dir.
//
// Returns nil when none yields a config — callers then fall through to the
// global resolveLLMDevice validation, preserving back-compat for global
// providers and fail-fast for genuinely unknown ones.
func (k *KernelImpl) resolveResumeProjectConfig(opts ResumeOpts, oldProc *Process, projectDir string) *config.ProjectConfig {
	// The process's own authoritative project dir: prefer the explicit projectDir
	// (history diskInfo.ProjectDir / checkpoint cp.ProjectDir), fall back to the
	// rebuilt placeholder's project dir.
	ownDir := projectDir
	if ownDir == "" && oldProc != nil && oldProc.ProjectConfig != nil {
		ownDir = oldProc.ProjectConfig.ProjectDir
	}
	// Caller-supplied config is for a DIFFERENT project than the process's own
	// (operator resumed from another project's cwd) — honor the process's own.
	if ownDir != "" && opts.ProjectConfig != nil && opts.ProjectConfig.ProjectDir != ownDir && k.projectConfigLoader != nil {
		pc, err := k.projectConfigLoader(ownDir)
		switch {
		case err != nil:
			log.Printf("[resume] projectConfigLoader(%q) failed: %v; falling back to existing priority chain", ownDir, err)
		case pc != nil:
			log.Printf("[resume] caller projectDir %q != process projectDir %q; honoring process's own project",
				opts.ProjectConfig.ProjectDir, ownDir)
			return pc
		}
		// (nil, nil) — ownDir resolved to global/no-project (e.g. its .rnix was
		// removed). Do NOT short-circuit to nil: fall through to opts/oldProc so
		// the existing fallbacks still apply.
	}
	if opts.ProjectConfig != nil {
		return opts.ProjectConfig
	}
	if oldProc != nil && oldProc.ProjectConfig != nil {
		return oldProc.ProjectConfig
	}
	if projectDir != "" && k.projectConfigLoader != nil {
		pc, err := k.projectConfigLoader(projectDir)
		if err != nil {
			log.Printf("[resume] projectConfigLoader(%q) failed: %v; falling back to global provider validation", projectDir, err)
			return nil
		}
		return pc
	}
	return nil
}

// resolveResumeLLMDevice mirrors the Spawn path's branch at spawn.go:588 — when
// the effective ProjectConfig carries an LLMFileOpener, the provider may exist
// only at project level (e.g. opencodego defined solely in .rnix/providers.yaml),
// so we skip the global DriverRegistry validation and build the device path
// directly. openLLMDeviceForResume then opens it through the project opener.
// Without a project opener we fall through to the global resolveLLMDevice check,
// keeping the original error semantics for global / unknown providers.
func (k *KernelImpl) resolveResumeLLMDevice(effPC *config.ProjectConfig, provider string) (string, error) {
	if effPC != nil && effPC.LLMFileOpener != nil {
		return "/dev/llm/" + provider, nil
	}
	dev, _, err := k.resolveLLMDevice(nil, provider)
	return dev, err
}

// openLLMDeviceForResume mirrors spawn.go:528-545 — prefers proc.ProjectConfig
// .LLMFileOpener when the resumed process has a ProjectConfig, falling back to
// the global VFS driver otherwise. This is the single fix point for the Dashboard
// `r` 401 bug: before this helper, both resume paths called k.vfs.Open directly
// and silently dropped project-level API keys.
//
// Returns the registered FD on success. On failure returns an error suitable
// for wrapping in a SyscallError with the caller's op string.
func (k *KernelImpl) openLLMDeviceForResume(proc *Process, llmDevice string) (types.FD, error) {
	if proc.ProjectConfig != nil && proc.ProjectConfig.LLMFileOpener != nil {
		providerName := strings.TrimPrefix(llmDevice, "/dev/llm/")
		file, openErr := proc.ProjectConfig.LLMFileOpener(providerName, int(vfs.O_RDWR))
		if openErr == nil {
			if vfsFile, ok := file.(vfs.VFSFile); ok {
				return k.vfs.RegisterFD(proc.PID, vfsFile), nil
			}
			log.Printf("[resume] warning: LLMFileOpener returned non-VFSFile type %T for provider %q, falling back to global VFS", file, providerName)
		} else {
			log.Printf("[resume] LLMFileOpener for provider %q failed: %v; falling back to global VFS", providerName, openErr)
		}
	}
	return k.vfs.Open(proc.PID, llmDevice, vfs.O_RDWR)
}

// restoreParentLinkage restores the parent-child linkage for a resumed process.
// Resume creates a new in-memory Process; without this helper its PPID would
// stay 0 and the parent's Children slice would not include the new PID, so the
// Dashboard tree (BuildProcessTree) would render the resumed process as a
// top-level root next to its original parent — the "脱离了进程树" bug.
//
// Behavior:
//   - ParentUUID == "" → no-op (true root processes).
//   - Parent still alive in procTable → set proc.PPID and call parent.AddChild.
//   - Parent already dead (only on disk / procHistory) → leave PPID = 0 but
//     proc.ParentUUID is still set, so BuildProcessTree's UUID-keyed lookup
//     can attach the resumed node to its (dead) parent.
//
// In all cases proc.ParentUUID is set so the on-disk proc-info.json snapshot
// preserves lineage for the next daemon restart.
//
// Story 44.1 — the previous "walk up and Unsuspend cli_disconnected ancestors"
// branch (`reactivateCliDisconnectedAncestors`) was removed. Subtree resume is
// now the user's explicit choice via SIGRESUME / `rnix resume <ancestor-uuid>`
// / dashboard `R` on the ancestor row, not an implicit side effect of resuming
// any descendant. The dashboard-`r` regression that motivated Epic 44 is gone
// because the "child resume silently fails to wake the script runner ancestor"
// path no longer exists — the user is in control.
func (k *KernelImpl) restoreParentLinkage(proc *Process, parentUUID string) {
	if parentUUID == "" {
		return
	}
	proc.ParentUUID = parentUUID
	if parent, ok := k.GetProcessByUUID(parentUUID); ok {
		proc.PPID = parent.PID
		parent.AddChild(proc.PID)
	}
}

// SuspendReasonCLIDisconnected is the canonical SuspendReason value set when a
// script-runner process is suspended because its CLI client disconnected. It
// is the single source of truth shared between the kernel resume logic
// (kernel/resume.go) and the IPC handler that sets the reason
// (ipc/server_pipeline.go), so both agree on spelling at compile time.
//
// Story 44.1 — this constant is intentionally retained even though
// reactivateCliDisconnectedAncestors was removed. Story 44.2 (script-runner
// suspend redesign) still emits this reason string when the CLI disconnects,
// but the new contract is: the user manually `rnix resume <uuid>` or hits
// dashboard `R` on the ancestor to wake the subtree. No automatic ancestor
// wakeup happens on descendant resume anymore.
const SuspendReasonCLIDisconnected = "cli_disconnected"

// cleanupOldProcessAndHistory removes the old in-memory Process and procHistory
// entry that share the resume UUID. It is a no-op when fork=true (preserving
// the original lineage for the forked branch). Called by both resumeFromCheckpoint
// and resumeFromHistory after all new-process allocations succeed.
//
// Story 48.1 code-review P4 — also releases the old process's MCP mount
// references. Without this, a non-fork resume of a Suspended process left
// oldProc's mounts (Status=Connected, refCount=1) live in the registry; the
// new proc's reattachMCPMounts then took an Acquire on each path, so when
// the new proc exited its Unmount only dropped refCount to 1 and the
// transport leaked until daemon shutdown.
func (k *KernelImpl) cleanupOldProcessAndHistory(oldProc *Process, uuid string, fork bool) {
	if fork {
		log.Printf("[resume-trace] cleanupOldProcessAndHistory skipped (fork=true) uuid=%s", uuid)
		return
	}
	if oldProc != nil {
		snap := oldProc.GetDetailSnapshot()
		log.Printf("[resume-trace] cleanupOldProcessAndHistory delete uuid=%s pid=%d state=%s tokens=%d ctx_id=%d",
			uuid, snap.PID, snap.State, snap.TokensUsed, snap.CtxID)
		if oldProc.CtxID > 0 {
			_ = k.ctxMgr.CtxFree(oldProc.CtxID)
		}
		// Release oldProc's MCP mount references before we drop the
		// process record. Mounts that aren't reused by the resumer drop to
		// refCount 0 and are torn down; mounts the resumer will Acquire
		// stay live with their refcount carried forward.
		if k.mountMgr != nil && len(oldProc.MCPMounts) > 0 {
			for _, path := range oldProc.MCPMounts {
				if err := k.mountMgr.Unmount(path); err != nil {
					log.Printf("[resume-trace] cleanupOldProcessAndHistory unmount uuid=%s path=%s: %v",
						uuid, path, err)
				}
			}
		}
		k.procTable.Delete(oldProc.PID)
		if queue, ok := k.msgQueues.LoadAndDelete(oldProc.PID); ok {
			queue.close()
		}
	} else {
		log.Printf("[resume-trace] cleanupOldProcessAndHistory no-oldProc uuid=%s (procHistory.RemoveByUUID only)", uuid)
	}
	k.procHistory.RemoveByUUID(uuid)
}

// checkpointExists reports whether a checkpoint.json file is present on disk
// for the given UUID. Used by ResumeWithOpts to apply the spec L114-117
// checkpoint > history preference uniformly across all routing branches.
func checkpointExists(baseDir, uuid string) bool {
	if baseDir == "" || uuid == "" {
		return false
	}
	cpPath := filepath.Join(baseDir, "steps", uuid, "checkpoint.json")
	_, err := os.Stat(cpPath)
	return err == nil
}

// isValidUUIDFormat validates that a UUID string contains only safe characters
// (alphanumeric and dashes) to prevent path traversal attacks.
func isValidUUIDFormat(uuid string) bool {
	if len(uuid) < 1 || len(uuid) > 64 {
		return false
	}
	for _, c := range uuid {
		isAlnum := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		if !isAlnum && c != '-' {
			return false
		}
	}
	return true
}

// Resume restores a previously suspended or interrupted process from its checkpoint.
// It reads the checkpoint from disk, creates a new Process with a new PID,
// restores all state, and resumes the reasoning loop from the next step.
// The original UUID is preserved; the PID is newly allocated.
//
// Resume is a thin wrapper over ResumeWithOpts(uuid, ResumeOpts{}) — kept for
// backward compatibility with existing callers (30-4 tests, IPC client). All new
// code paths should call ResumeWithOpts directly. The previous standalone
// implementation has been removed; ResumeWithOpts → resumeFromCheckpoint is
// the single source of truth.
func (k *KernelImpl) Resume(uuid string) (*ResumeResult, error) {
	return k.ResumeWithOpts(uuid, ResumeOpts{})
}

// compareEnvSnapshot compares checkpoint env snapshot with current environment
// and emits warning events for any differences.
func (k *KernelImpl) compareEnvSnapshot(proc *Process, snapshot map[string]string) {
	var diffs []string
	for key, oldVal := range snapshot {
		newVal := os.Getenv(key)
		if newVal != oldVal {
			if newVal == "" {
				diffs = append(diffs, fmt.Sprintf("%s: removed (was set)", key))
			} else {
				diffs = append(diffs, fmt.Sprintf("%s: changed", key))
			}
		}
	}
	if len(diffs) > 0 {
		k.emitEvent(proc, "ResumeEnvDrift", map[string]any{
			"changed_vars": diffs,
			"count":        len(diffs),
		}, nil, nil, 0)
		log.Printf("[kernel] resume uuid=%s env drift: %d vars changed", proc.UUID, len(diffs))
	}
}

// ResumeWithOpts is the extended Resume that supports fork mode and history-based resume.
// It routes to the appropriate path based on process state:
//   - Suspended in procTable → original checkpoint-based path (30-4)
//   - Dead/Zombie in procTable → ResumeFromHistory
//   - Running in procTable → rejected
//   - Not in procTable → disk lookup → ResumeFromHistory
func (k *KernelImpl) ResumeWithOpts(uuid string, opts ResumeOpts) (*ResumeResult, error) {
	start := time.Now()

	if uuid == "" {
		return nil, NewSyscallError("Resume", 0, "", fmt.Errorf("empty UUID"), types.ErrInvalid)
	}
	if !isValidUUIDFormat(uuid) {
		return nil, NewSyscallError("Resume", 0, "", fmt.Errorf("invalid UUID format: %s", uuid), types.ErrInvalid)
	}

	// Story 56.6: synthetic CLI-subagent observation nodes are not real rnix
	// processes — they have no spawnable state. Reject resume early (Epic 42
	// treats Dead as resumable, so this must be explicit) before any disk /
	// checkpoint routing tries to spawn a virtual node.
	if k.procHistory != nil {
		if info := k.procHistory.FindByUUID(uuid); info != nil && info.Synthetic {
			return nil, NewSyscallError("Resume", 0, "",
				fmt.Errorf("UUID %s is a synthetic CLI-subagent observation node and is not resumable", uuid),
				types.ErrInvalid)
		}
	}

	k.resumeMu.Lock()
	defer k.resumeMu.Unlock()

	log.Printf("[resume-trace] ResumeWithOpts enter uuid=%s fork=%v from_step=%d", uuid, opts.Fork, opts.FromStep)
	defer func() {
		log.Printf("[resume-trace] ResumeWithOpts exit uuid=%s elapsed=%s", uuid, time.Since(start))
	}()

	baseDir := FindBaseDirByUUID(k.dataDir, uuid)
	if baseDir == "" {
		// Story 42.5 AC#6 — distinguish "garbage collected" from
		// "never spawned" using procHistory.HasEverSeen.
		if k.procHistory != nil && k.procHistory.HasEverSeen(uuid) {
			return nil, NewSyscallError("Resume", 0, "",
				fmt.Errorf("process data has been garbage collected (UUID %s)", uuid),
				types.ErrNotFound)
		}
		return nil, NewSyscallError("Resume", 0, "",
			fmt.Errorf("no data found for UUID %s: never spawned or never persisted", uuid),
			types.ErrNotFound)
	}

	// Route based on process state in procTable
	if found, ok := k.GetProcessByUUID(uuid); ok {
		snap := found.GetDetailSnapshot()
		state := snap.State
		log.Printf("[resume-trace] live proc found uuid=%s pid=%d state=%s tokens=%d ctx_id=%d",
			uuid, snap.PID, state, snap.TokensUsed, snap.CtxID)
		switch state {
		case types.StateSuspended:
			// Story 44.3 AC#4 — daemon-restart placeholders (created by
			// LoadSuspendedFromDisk) have no checkpoint.json. Fall back to
			// the history path when checkpoint is absent, mirroring the
			// Dead/Zombie branch below. The pre-44.3 behaviour always called
			// resumeFromCheckpoint, which failed with ErrNotFound for these
			// placeholders.
			if checkpointExists(baseDir, uuid) {
				log.Printf("[resume-trace] route=resumeFromCheckpoint (Suspended+checkpoint) uuid=%s", uuid)
				return k.resumeFromCheckpoint(uuid, opts, start)
			}
			log.Printf("[resume-trace] route=resumeFromHistory (Suspended+no-checkpoint) uuid=%s pid=%d", uuid, found.PID)
			return k.resumeFromHistory(uuid, opts, start)
		case types.StateRunning:
			return nil, NewSyscallError("Resume", found.PID, "",
				fmt.Errorf("process with UUID %s is in Running state, cannot resume", uuid), types.ErrInvalid)
		case types.StateDead, types.StateZombie:
			// Prefer checkpoint.json if present (spec L114-117: checkpoint > history).
			// Falls back to history-based resume when no checkpoint was written.
			if checkpointExists(baseDir, uuid) {
				return k.resumeFromCheckpoint(uuid, opts, start)
			}
			return k.resumeFromHistory(uuid, opts, start)
		default:
			return nil, NewSyscallError("Resume", found.PID, "",
				fmt.Errorf("process with UUID %s is in %s state", uuid, state), types.ErrInvalid)
		}
	}

	// Not in procTable — check disk
	stepsDir := filepath.Join(baseDir, "steps", uuid)
	if _, err := os.Stat(stepsDir); err != nil {
		if os.IsNotExist(err) {
			// Story 42.5 AC#6 — distinguish "garbage collected" from
			// "never spawned" using procHistory.HasEverSeen (true when the
			// UUID was Add'd then RemoveByUUID'd, the path gc takes).
			if k.procHistory != nil && k.procHistory.HasEverSeen(uuid) {
				return nil, NewSyscallError("Resume", 0, "",
					fmt.Errorf("process data has been garbage collected (UUID %s)", uuid),
					types.ErrNotFound)
			}
			return nil, NewSyscallError("Resume", 0, "",
				fmt.Errorf("no data found for UUID %s: never spawned or never persisted", uuid),
				types.ErrNotFound)
		}
		return nil, NewSyscallError("Resume", 0, "",
			fmt.Errorf("stat steps dir: %w", err), types.ErrInternal)
	}

	// Prefer checkpoint path if checkpoint.json exists (shared with the
	// Dead/Zombie branch above via the checkpointExists helper).
	if checkpointExists(baseDir, uuid) {
		return k.resumeFromCheckpoint(uuid, opts, start)
	}

	return k.resumeFromHistory(uuid, opts, start)
}

// resumeFromCheckpoint handles the checkpoint-based resume path (original 30-4 logic).
func (k *KernelImpl) resumeFromCheckpoint(uuid string, opts ResumeOpts, start time.Time) (*ResumeResult, error) {
	baseDir := FindBaseDirByUUID(k.dataDir, uuid)
	if baseDir == "" {
		return nil, NewSyscallError("Resume", 0, "", fmt.Errorf("no on-disk data for UUID %s", uuid), types.ErrNotFound)
	}
	stepsDir := filepath.Join(baseDir, "steps", uuid)

	// Story 42.3 — reject negative FromStep on this path too (defensive symmetry).
	if opts.FromStep < 0 {
		return nil, NewCompactSyscallError("Resume", types.ErrInvalid,
			fmt.Errorf("ErrInvalid: from_step %d invalid (must be >= 0)", opts.FromStep))
	}

	cpData, err := ReadCheckpointPublic(stepsDir)
	if err != nil {
		if os.IsNotExist(err) || strings.Contains(err.Error(), "checkpoint read") {
			return nil, NewSyscallError("Resume", 0, "", fmt.Errorf("checkpoint not found for UUID %s", uuid), types.ErrNotFound)
		}
		return nil, NewSyscallError("Resume", 0, "", fmt.Errorf("checkpoint corrupted: %w", err), types.ErrInvalid)
	}

	if cpData.UUID != uuid {
		return nil, NewSyscallError("Resume", 0, "", fmt.Errorf("checkpoint UUID mismatch: got %s, want %s", cpData.UUID, uuid), types.ErrInvalid)
	}

	// Story 42.3 — checkpoint + --from-step conflict.
	// checkpoint.json is a complete ContextSnapshot at cpData.LastStep; we cannot
	// partially deserialize it back to an arbitrary earlier step. Reject when the
	// caller asks for any truncation that does not exactly match the checkpoint
	// anchor: < cpData.LastStep (would need partial replay) AND > cpData.LastStep
	// (caller asked for steps past the snapshot — user intent silently dropped).
	if opts.FromStep > 0 && opts.FromStep != cpData.LastStep {
		return nil, NewCompactSyscallError("Resume", types.ErrInvalid,
			fmt.Errorf("ErrInvalid: from_step %d requires history path; checkpoint at step %d (full snapshot, no partial replay)",
				opts.FromStep, cpData.LastStep))
	}

	cp := cpData.ProcState
	startStep := cpData.LastStep + 1
	if cp.MaxSteps > 0 && startStep > cp.MaxSteps {
		return nil, NewSyscallError("Resume", 0, "",
			fmt.Errorf("checkpoint at max step %d/%d — nothing to resume", cpData.LastStep, cp.MaxSteps), types.ErrInvalid)
	}

	// Remove old process from procTable if Suspended
	var oldProc *Process
	if found, ok := k.GetProcessByUUID(uuid); ok {
		oldProc = found
	}

	// Resolve the effective ProjectConfig (process-own > opts > oldProc > loader)
	// BEFORE the LLM-device check so project-level providers (defined only in
	// .rnix/providers.yaml) survive the same way they do on the Spawn path.
	// cp.ProjectDir is the process's OWN project, now persisted into the checkpoint
	// (CheckpointProcState.ProjectDir), so resolveResumeProjectConfig can honor it
	// over a wrong operator cwd. Empty on pre-fix checkpoints → opts/oldProc fallback.
	effPC := k.resolveResumeProjectConfig(opts, oldProc, cp.ProjectDir)

	llmDevice, resolveErr := k.resolveResumeLLMDevice(effPC, cp.Provider)
	if resolveErr != nil {
		return nil, NewSyscallError("Resume", 0, "",
			fmt.Errorf("LLM provider %q not available: %w", cp.Provider, resolveErr), types.ErrDriver)
	}

	if oldProc != nil {
		// Defer cleanup of old process to AFTER all allocations succeed
		// (see post-Open block). This avoids destroying the original on early failure.
		// fork=true skips cleanup entirely so original keeps running.
		_ = oldProc
	}

	proc := NewProcess(0, cp.Intent, cp.Skills)
	// Epic 42 fix: inherit ProjectConfig so openLLMDeviceForResume + per-process
	// VFS WorkDir match the original Spawn path. Use the resolved effPC (not
	// opts.ProjectConfig directly) so auto-resume's empty ResumeOpts still
	// inherits a rebuilt ProjectConfig.
	proc.ProjectConfig = effPC
	if proc.ProjectConfig != nil && proc.ProjectConfig.ProjectDir != "" {
		k.vfs.SetWorkDir(proc.PID, proc.ProjectConfig.ProjectDir)
	}
	if opts.Fork {
		// Fork mode: NewProcess already generated a fresh UUIDv7; assert it
		// differs from the origin to make the intent explicit (defensive against
		// future NewProcess refactors).
		if proc.UUID == "" || proc.UUID == uuid {
			return nil, NewSyscallError("Resume", proc.PID, "",
				fmt.Errorf("fork: new UUID generation produced empty or duplicate UUID %q", proc.UUID), types.ErrInternal)
		}
		proc.OriginUUID = uuid
	} else {
		// Non-fork: inherit original UUID
		proc.UUID = uuid
	}
	// Restore parent linkage from the checkpoint snapshot. Older checkpoints
	// (written before ParentUUID was persisted) leave the field empty, which
	// no-ops the helper — the resumed process becomes a true root, matching
	// pre-fix behavior.
	k.restoreParentLinkage(proc, cp.ParentUUID)
	proc.Provider = cp.Provider
	proc.Model = cp.Model
	proc.ReasoningEffort = cp.ReasoningEffort
	proc.AllowedDevices = append([]string(nil), cp.AllowedDevices...)
	proc.DeniedDevices = append([]string(nil), cp.DeniedDevices...)
	// Story 54.1 (NFR87) — restore the tool-name whitelist; legacy checkpoints
	// without it rebuild from AllowedDevices.
	k.restoreAllowedTools(proc, cp.AllowedTools)
	proc.MaxSteps = cp.MaxSteps
	proc.ResumedFromStep = startStep
	proc.mu.Lock()
	proc.TokensUsed = cp.UsedTokens
	proc.Budget = ProcessBudget{MaxTokens: cp.MaxTokens, MaxCost: cp.MaxCost, UsedCost: cp.UsedCost}
	proc.mu.Unlock()

	// Story 71.1 AC4/AC6: resumed processes run with NO slot ceiling.
	//
	// This is forced rather than chosen: the Deserialize call below sets
	// ctx.MaxSize to 0 unconditionally (AC6-④, so a pre-71.1 snapshot's
	// `max_size: 256` cannot resurrect the ceiling), so whatever we allocate with
	// here is overwritten moments later. Passing cp.CtxSize would therefore only
	// make proc.CtxSize advertise a ceiling the context does not actually enforce.
	//
	// Honest consequence, recorded in deferred-work: an explicitly configured
	// ctx_size escape hatch does NOT survive a resume. cp.CtxSize is still
	// persisted, so re-honouring it is a one-line change if that turns out to
	// matter.
	resumeCtxSize := 0
	proc.CtxSize = resumeCtxSize
	cid, err := k.ctxMgr.CtxAlloc(resumeCtxSize)
	if err != nil {
		return nil, NewSyscallError("Resume", proc.PID, "", fmt.Errorf("context alloc: %w", err), types.ErrInternal)
	}
	proc.CtxID = cid

	ctx, getErr := k.ctxMgr.GetContext(cid)
	if getErr != nil {
		_ = k.ctxMgr.CtxFree(cid)
		return nil, NewSyscallError("Resume", proc.PID, "", fmt.Errorf("context get: %w", getErr), types.ErrInternal)
	}
	if err := ctx.Deserialize(cpData.ContextSnapshot); err != nil {
		_ = k.ctxMgr.CtxFree(cid)
		return nil, NewSyscallError("Resume", proc.PID, "", fmt.Errorf("context deserialize: %w", err), types.ErrInternal)
	}

	// Story 69.2 — the checkpoint snapshot schema (kernel/checkpoint.go) carries
	// only CtxSize, never ContextWindow / ContextBudget, so this path has no
	// window to inherit and must RECOMPUTE from the Provider / Model restored
	// above. Recomputing rather than widening the snapshot schema is deliberate:
	// it also fixes existing checkpoint files (a new field would only help ones
	// written from now on), and it picks up a providers.yaml that changed after
	// the checkpoint was taken. Without this, ContextBudget stays 0, the
	// applyCtxTokenLimit guard skips silently, and every `rnix resume` degrades
	// the token scale back to DefaultTokenLimit — exactly how the incident
	// replayed itself four times in a row. Scale chain: kernel/ctx_token_limit.go.
	k.resolveContextBudget(proc)
	k.applyCtxTokenLimit(proc)

	// Story 69.3 AC6 — preventive unload, checkpoint-path twin of the call in
	// rehydrate.go. This path does NOT go through rehydrate (it CtxAllocs and
	// Deserializes on its own, just above), so covering only the funnel would
	// leave `rnix resume`'s checkpoint branch with no headroom — the same path
	// split Story 69.2 discovered. Runs before the launch goroutine's
	// proc.Start(), so the first reasonStep already sees the reclaimed context.
	k.unloadForResume(proc, resumeCtxSize, "resume(checkpoint)")

	// apex 10-11: checkpoint-path NewInput. Checkpoints are written on the step's
	// INPUT side (last_step anchors before that round's final assistant output),
	// so the snapshot usually lacks the previous round's final output — same
	// replay hazard as the history path (verified against a real daemon: R4 of
	// the 10-11 e2e "restarted from zero" because R3's checkpoint tail ended at
	// a user+tool turn). Restore it from proc-info.json's result unless the
	// snapshot tail already carries it (guard against double-append for
	// checkpoints that do include the final output).
	if strings.TrimSpace(opts.NewInput) != "" {
		finalOutput := ""
		if res := strings.TrimSpace(readProcInfoResult(stepsDir)); res != "" && !snapshotEndsWithAssistant(cpData.ContextSnapshot, res) {
			finalOutput = res
		}
		if injErr := k.injectResumeNewInput(proc, uuid, opts.NewInput, finalOutput, "checkpoint"); injErr != nil {
			_ = k.ctxMgr.CtxFree(cid)
			return nil, NewSyscallError("Resume", proc.PID, "", injErr, types.ErrInternal)
		}
	}

	proc.mu.Lock()
	proc.StepTimeout = 5 * time.Minute
	proc.LastHeartbeat = time.Now()
	proc.mu.Unlock()

	if len(cp.EnvSnapshot) > 0 {
		k.compareEnvSnapshot(proc, cp.EnvSnapshot)
	}

	proc.PrimaryDevice = llmDevice
	llmFD, openErr := k.openLLMDeviceForResume(proc, llmDevice)
	if openErr != nil {
		_ = k.ctxMgr.CtxFree(cid)
		return nil, NewSyscallError("Resume", proc.PID, llmDevice,
			fmt.Errorf("LLM device open failed: %w", openErr), types.ErrDriver)
	}
	proc.FDTable[llmFD] = nil
	k.setupDriverStreamHandler(proc, llmFD)

	// All allocations and Open succeeded — now safe to clean up old process and
	// procHistory entry (skip entirely when forking, so original keeps running).
	log.Printf("[resume-trace] resumeFromCheckpoint pre-cleanup uuid=%s newPID=%d oldProc=%v",
		uuid, proc.PID, oldProc != nil)
	k.cleanupOldProcessAndHistory(oldProc, uuid, opts.Fork)

	if proc.cancel != nil {
		proc.cancel() // release ctx created by NewProcess (no listeners yet, but be defensive)
	}
	gctx, cancel := gocontext.WithCancel(gocontext.Background())
	gctx = ContextWithPID(gctx, proc.PID)
	proc.cancel = cancel
	proc.ctx = gctx

	k.AddProcess(proc)
	k.msgQueues.Store(proc.PID, newMessageQueue())
	log.Printf("[resume-trace] resumeFromCheckpoint post-AddProcess uuid=%s newPID=%d state=%s tokens=%d",
		uuid, proc.PID, proc.GetState(), proc.TokensUsed)

	proc.mu.Lock()
	proc.SuspendReason = cp.SuspendReason
	proc.mu.Unlock()

	spawnOpts := SpawnOpts{Model: cp.Model, StartStep: startStep}

	k.emitEvent(proc, "Resume", map[string]any{
		"uuid":          uuid,
		"from_step":     startStep,
		"provider":      cp.Provider,
		"model":         cp.Model,
		"checkpoint":    cpData.LastStep,
		"fork":          opts.Fork,
		"new_input_len": len(opts.NewInput),
	}, proc.PID, nil, time.Since(start))

	proc.wg.Go(func() {
		defer func() { _ = k.vfs.CloseAll(proc.PID) }()
		// apex 10-11 started-semantics: when the caller supplied a LaunchReady
		// gate (resume_watch IPC handler), hold the reasoning round until the
		// handler has written its initial OK response, so the client learns the
		// process launched before any LLM work happens. nil gate = launch now.
		if opts.LaunchReady != nil {
			select {
			case <-opts.LaunchReady:
			case <-gctx.Done():
				return
			}
		}
		_ = proc.Start()

		if cp.SuspendReason == "context_full" {
			proc.compactMu.Lock()
			compactOpts := rnixctx.CompactOpts{
				LLMCall:       k.BuildCompactLLMCall(proc),
				Trigger:       "context_full_resume",
				ReadFileState: k.SnapshotReadFileState(proc),
				ActiveSkills:  k.BuildActiveSkills(proc),
				ActivePlan:    k.extractActivePlan(proc.CtxID),
			}
			_, compactErr := k.ctxMgr.Compact(proc.CtxID, compactOpts)
			proc.compactMu.Unlock()
			if compactErr != nil {
				// Story 69.3 F1 — this used to kill the process outright, which
				// is what made "resume → compact → 30s timeout → dead" repeat
				// four times in the incident. Try the deterministic reclamation
				// before giving up; only a genuinely unrecoverable context ends
				// the process now.
				fallback, ok := k.reclaimForResume(proc)
				postSlotUsed, _, _ := k.ctxMgr.SlotUsage(proc.CtxID)
				args := map[string]any{
					"step":       startStep,
					"trigger":    "context_full_resume",
					"error":      compactErr.Error(),
					"post_slots": postSlotUsed,
				}
				addFallbackArgs(args, fallback)
				k.emitEvent(proc, "Compact", args, nil, compactErr, 0)

				if !ok {
					log.Printf("[kernel] pid=%d resume compact failed and mechanical fallback could not free room: %v", proc.PID, compactErr)
					k.finishProcess(proc, ExitStatus{Code: ExitContextFull, Reason: "context_full: resume compact failed", Err: compactErr})
					return
				}
				log.Printf("[kernel] pid=%d resume compact failed (%v); mechanical fallback freed pruned=%d dropped_rounds=%d — continuing",
					proc.PID, compactErr, fallback.Pruned, fallback.DroppedRounds)
				proc.mu.Lock()
				proc.SuspendReason = ""
				proc.mu.Unlock()
				k.reasonStep(proc, llmFD, spawnOpts)
				return
			}
			k.ClearReadFileState(proc)
			proc.mu.Lock()
			proc.SuspendReason = ""
			proc.mu.Unlock()
		}

		k.reasonStep(proc, llmFD, spawnOpts)
	})

	if k.callbacks != nil {
		k.callbacks.OnSpawn(proc.PID, proc.Intent, proc.Provider, proc.Model, proc.UUID)
	}

	log.Printf("[kernel] resume(checkpoint) uuid=%s new_pid=%d from_step=%d fork=%v (took %v)",
		uuid, proc.PID, startStep, opts.Fork, time.Since(start))

	return &ResumeResult{
		PID:             proc.PID,
		UUID:            proc.UUID,
		ResumedFromStep: startStep,
	}, nil
}

// resumeFromHistory handles resume for Dead/Zombie processes using disk data
// (steps.jsonl + process-meta.json + proc-info.json).
func (k *KernelImpl) resumeFromHistory(uuid string, opts ResumeOpts, start time.Time) (*ResumeResult, error) {
	baseDir := FindBaseDirByUUID(k.dataDir, uuid)
	if baseDir == "" {
		return nil, NewSyscallError("Resume", 0, "", fmt.Errorf("no on-disk data for UUID %s", uuid), types.ErrNotFound)
	}
	stepsDir := filepath.Join(baseDir, "steps", uuid)

	// Story 42.3 — reject negative FromStep up-front (AC#2). Zero is valid (=
	// "no truncation"); positive values get range-checked after we read steps.jsonl.
	if opts.FromStep < 0 {
		return nil, NewCompactSyscallError("Resume", types.ErrInvalid,
			fmt.Errorf("ErrInvalid: from_step %d invalid (must be >= 0)", opts.FromStep))
	}

	// Read proc-info.json for provider/model/skills/intent
	procInfoPath := filepath.Join(stepsDir, "proc-info.json")
	procInfoData, err := os.ReadFile(procInfoPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Story 42.5 AC#6 — distinguish "garbage collected" from
			// "never spawned". HasEverSeen returns true when the UUID was
			// previously Add'd to procHistory and later RemoveByUUID'd
			// (the path gc takes when reaping a directory).
			if k.procHistory != nil && k.procHistory.HasEverSeen(uuid) {
				return nil, NewSyscallError("Resume", 0, "",
					fmt.Errorf("process data has been garbage collected (UUID %s)", uuid),
					types.ErrNotFound)
			}
			return nil, NewSyscallError("Resume", 0, "",
				fmt.Errorf("no data found for UUID %s: never spawned or never persisted", uuid),
				types.ErrNotFound)
		}
		return nil, NewSyscallError("Resume", 0, "",
			fmt.Errorf("read proc-info.json: %w", err), types.ErrInternal)
	}

	var diskInfo procInfoDisk
	if err := json.Unmarshal(procInfoData, &diskInfo); err != nil {
		return nil, NewSyscallError("Resume", 0, "",
			fmt.Errorf("parse proc-info.json: %w", err), types.ErrInternal)
	}

	// Story 56.6: disk-level guard against resuming a synthetic CLI-subagent
	// observation node. The in-memory guard in ResumeWithOpts covers the common
	// case, but this path is reachable when procHistory is nil or the synthetic
	// UUID was evicted from the in-memory FIFO cap while its proc-info.json
	// remains on disk — a synthetic node is not a real rnix process and has no
	// spawnable state, so reject it here too (defense in depth for AC6).
	if diskInfo.Synthetic {
		return nil, NewSyscallError("Resume", 0, "",
			fmt.Errorf("UUID %s is a synthetic CLI-subagent observation node and is not resumable", uuid),
			types.ErrInvalid)
	}

	// Story 48.1 — promote diskInfo to vfs.ProcInfo so MCPMounts is in the
	// shape reattachMCPMounts (called after AddProcess below) expects.
	resumeInfo := procInfoFromDisk(diskInfo)

	if diskInfo.Provider == "" {
		// Don't silently fall back to "claude" — the original process may have
		// been running on cursor/openai/etc and the behavior/cost/model contract
		// would diverge silently.
		return nil, NewSyscallError("Resume", 0, "",
			fmt.Errorf("provider missing in proc-info.json for UUID %s", uuid), types.ErrInvalid)
	}
	provider := diskInfo.Provider

	// Locate old Dead/Zombie/Suspended process if present (cleanup deferred until
	// all allocations succeed; skipped entirely when forking so original is
	// preserved). Resolved BEFORE the LLM-device check so its rebuilt
	// ProjectConfig can feed the effective-ProjectConfig resolution below.
	var oldProc *Process
	if found, ok := k.GetProcessByUUID(uuid); ok {
		oldProc = found
	}

	// Resolve the effective ProjectConfig (opts > oldProc > loader) BEFORE the
	// LLM-device check so project-level providers (defined only in
	// .rnix/providers.yaml) survive the same way they do on the Spawn path.
	// The loader fallback uses diskInfo.ProjectDir, which covers daemon-self-start
	// AutoResumeDaemonShutdown (empty ResumeOpts; oldProc may itself be nil).
	effPC := k.resolveResumeProjectConfig(opts, oldProc, diskInfo.ProjectDir)

	llmDevice, resolveErr := k.resolveResumeLLMDevice(effPC, provider)
	if resolveErr != nil {
		return nil, NewSyscallError("Resume", 0, "",
			fmt.Errorf("LLM provider %q not available: %w", provider, resolveErr), types.ErrDriver)
	}

	// Create new process
	proc := NewProcess(0, diskInfo.Intent, diskInfo.Skills)
	// Epic 42 fix: inherit ProjectConfig so openLLMDeviceForResume + per-process
	// VFS WorkDir match the original Spawn path. Use the resolved effPC (not
	// opts.ProjectConfig directly) so auto-resume's empty ResumeOpts still
	// inherits the placeholder/loader-rebuilt ProjectConfig.
	proc.ProjectConfig = effPC
	if proc.ProjectConfig != nil && proc.ProjectConfig.ProjectDir != "" {
		k.vfs.SetWorkDir(proc.PID, proc.ProjectConfig.ProjectDir)
	}
	if opts.Fork {
		// Fork mode: NewProcess already generated a fresh UUIDv7; assert it
		// differs from the origin to make the intent explicit. The UUIDv7
		// time-ordered generator makes a collision practically impossible,
		// but defensive checking guards against future refactors of NewProcess.
		if proc.UUID == "" || proc.UUID == uuid {
			return nil, NewSyscallError("Resume", proc.PID, "",
				fmt.Errorf("fork: new UUID generation produced empty or duplicate UUID %q", proc.UUID), types.ErrInternal)
		}
		proc.OriginUUID = uuid
	} else {
		// Non-fork: inherit original UUID
		proc.UUID = uuid
	}
	proc.Provider = provider
	proc.Model = diskInfo.Model
	proc.ReasoningEffort = diskInfo.ReasoningEffort
	proc.PrimaryDevice = llmDevice // must be set before rehydrate so the caller
	// can route reasonStep-driven processes correctly on later resume legs.
	proc.AllowedDevices = append([]string(nil), diskInfo.AllowedDevices...)
	proc.DeniedDevices = append([]string(nil), diskInfo.DeniedDevices...)
	// Story 54.1 (NFR87) — restore tool whitelist; legacy disk snapshots rebuild
	// from AllowedDevices.
	k.restoreAllowedTools(proc, diskInfo.AllowedTools)
	proc.MaxSteps = diskInfo.MaxSteps
	// Restore additional state captured on disk so the resumed process is not a
	// stripped-down shadow of the original (Edge Case Hunter Finding #5 & #10).
	// restoreParentLinkage sets ParentUUID, plus PPID + parent.AddChild when the
	// parent is still in procTable — fixes the "resume detaches from tree" bug.
	k.restoreParentLinkage(proc, diskInfo.ParentUUID)
	proc.ContextBudget = diskInfo.ContextBudget
	proc.ContextWindow = diskInfo.ContextWindow
	proc.ComposeNode = diskInfo.ComposeNode
	proc.ComposeDeps = append([]string(nil), diskInfo.ComposeDeps...)
	proc.PipelineIndex = diskInfo.PipelineIndex
	proc.PipelineTotal = diskInfo.PipelineTotal
	proc.mu.Lock()
	proc.TokensUsed = diskInfo.TokensUsed
	// Story 48.1 — pre-populate MCPMounts / mcpConfigs from disk so
	// downstream consumers that may run inside rehydrate's
	// SystemPrompt-synthesis fallback path (sections registry → mcp_instructions
	// section reads mcpConfigs) see the original set BEFORE reattachMCPMounts
	// prunes failures. fork and non-fork take the same path because the
	// snapshot belongs to the source UUID, not the new PID.
	if len(resumeInfo.MCPMounts) > 0 {
		paths := make([]string, 0, len(resumeInfo.MCPMounts))
		cfgs := make([]vfs.MCPConfig, 0, len(resumeInfo.MCPMounts))
		for _, m := range resumeInfo.MCPMounts {
			paths = append(paths, m.Path)
			cfgs = append(cfgs, m.Config)
		}
		proc.MCPMounts = paths
		proc.mcpConfigs = cfgs
	}
	proc.mu.Unlock()

	// Rehydrate the in-memory runtime state (toolMap + nativeToolDefs,
	// SkillBodies + SkillDirs, mcpDevicePaths, freshly-allocated ctx with
	// system prompt + messages deserialized in) from the on-disk artifacts.
	// This is the same helper that LoadSuspendedFromDisk uses to revive
	// placeholders — keeping both paths funneled through one function
	// prevents the "daemon-restart placeholder misroutes through script
	// runner branch" silent-divergence bug.
	lastStep, totalSteps, rehydrateErr := k.rehydrateRuntimeStateFromDisk(proc, stepsDir, 0, opts.FromStep)
	if rehydrateErr != nil {
		// rehydrate already freed any partial ctx it allocated; relabel the
		// SyscallError's Syscall field as "Resume" so IPC consumers see the
		// caller op and not the helper's name (the underlying error / code
		// are preserved).
		if se, ok := rehydrateErr.(*SyscallError); ok {
			se.Syscall = "Resume"
		}
		return nil, rehydrateErr
	}

	// Story 42.3 — out-of-range check (AC#2). Spec-literal error string.
	// Done after rehydrate because totalSteps comes from parseStepsJSONL which
	// rehydrate runs.
	if opts.FromStep > 0 && opts.FromStep > totalSteps {
		_ = k.ctxMgr.CtxFree(proc.CtxID)
		proc.CtxID = 0
		return nil, NewCompactSyscallError("Resume", types.ErrInvalid,
			fmt.Errorf("ErrInvalid: from_step %d exceeds total steps (actual: %d)", opts.FromStep, totalSteps))
	}

	// startStep semantics:
	//   - opts.FromStep > 0: truncate at FromStep, continue from FromStep+1.
	//   - opts.FromStep == 0: default → continue from lastStep+1.
	var startStep int
	if opts.FromStep > 0 {
		startStep = opts.FromStep + 1
	} else {
		startStep = lastStep + 1
	}
	proc.ResumedFromStep = startStep

	// apex 10-11: history-path NewInput. parseStepsJSONL restores the LAST step's
	// INPUT messages, which never include that round's final assistant output —
	// restore it from proc-info.json's result before appending the new user turn,
	// otherwise the resumed LLM replays its previous round (anti-replay,
	// spike-verified). Injected before AddProcess so a failure aborts cleanly.
	if strings.TrimSpace(opts.NewInput) != "" {
		if injErr := k.injectResumeNewInput(proc, uuid, opts.NewInput, diskInfo.Result, "history"); injErr != nil {
			_ = k.ctxMgr.CtxFree(proc.CtxID)
			proc.CtxID = 0
			return nil, NewSyscallError("Resume", proc.PID, "", injErr, types.ErrInternal)
		}
	}

	proc.mu.Lock()
	proc.StepTimeout = 5 * time.Minute
	proc.LastHeartbeat = time.Now()
	proc.mu.Unlock()

	llmFD, openErr := k.openLLMDeviceForResume(proc, llmDevice)
	if openErr != nil {
		_ = k.ctxMgr.CtxFree(proc.CtxID)
		proc.CtxID = 0
		return nil, NewSyscallError("Resume", proc.PID, llmDevice,
			fmt.Errorf("LLM device open failed: %w", openErr), types.ErrDriver)
	}
	proc.FDTable[llmFD] = nil
	k.setupDriverStreamHandler(proc, llmFD)

	// All allocations and Open succeeded — now safe to clean up old process and
	// procHistory entry (skip entirely when forking, so original keeps running).
	log.Printf("[resume-trace] resumeFromHistory pre-cleanup uuid=%s newPID=%d oldProc=%v",
		uuid, proc.PID, oldProc != nil)
	k.cleanupOldProcessAndHistory(oldProc, uuid, opts.Fork)

	if proc.cancel != nil {
		proc.cancel() // release ctx created by NewProcess (no listeners yet, but be defensive)
	}
	gctx, cancel := gocontext.WithCancel(gocontext.Background())
	gctx = ContextWithPID(gctx, proc.PID)
	proc.cancel = cancel
	proc.ctx = gctx

	k.AddProcess(proc)
	k.msgQueues.Store(proc.PID, newMessageQueue())
	// Attach step / event writers BEFORE we emit ResumeFromHistory + Mount
	// events so events.jsonl carries the full audit trail. Without this the
	// reasonStep goroutine started below is the only attachment site and
	// every emitEvent in this critical section falls into the [event-drop]
	// warning path — Story 48.1 code-review P1.
	k.attachStepObservation(proc)
	log.Printf("[resume-trace] resumeFromHistory post-AddProcess uuid=%s newPID=%d state=%s tokens=%d ctx_id=%d",
		uuid, proc.PID, proc.GetState(), proc.TokensUsed, proc.CtxID)

	// Story 42.3 — synchronously persist the resumed process's proc-info.json
	// so downstream tooling (Dashboard lineage queries, test fixtures) can
	// observe origin_uuid / resumed_from_step before the first checkpoint
	// or reap. Best-effort: failure logged but not fatal — the reap path
	// will write a fresh snapshot on exit.
	if procInfo, piErr := k.GetProcInfo(proc.PID); piErr == nil && procInfo != nil {
		if saveErr := SaveProcInfo(baseDir, *procInfo); saveErr != nil {
			log.Printf("[resume] SaveProcInfo(new uuid=%s) failed: %v", proc.UUID, saveErr)
		}
	}

	// Determine if context_full exit — trigger compact after start.
	// Read ExitReason field (written by reap from proc.Exit.Reason); fall back
	// to legacy Result field for proc-info.json written before this field existed.
	exitReason := diskInfo.ExitReason
	if exitReason == "" {
		exitReason = diskInfo.Result
	}
	isContextFull := strings.Contains(strings.ToLower(exitReason), "context_full")

	spawnOpts := SpawnOpts{Model: diskInfo.Model, StartStep: startStep}

	k.emitEvent(proc, "ResumeFromHistory", map[string]any{
		"uuid":          uuid,
		"from_step":     startStep,
		"provider":      provider,
		"model":         diskInfo.Model,
		"fork":          opts.Fork,
		"last_step":     lastStep,
		"new_input_len": len(opts.NewInput),
	}, proc.PID, nil, time.Since(start))

	// Story 48.1 — Re-mount MCP transports from the disk snapshot. Sequenced
	// after AddProcess + ResumeFromHistory emit so events.jsonl ordering matches
	// AC1 §Then 5 (ResumeFromHistory → Mount(source="resume") → ...) and the
	// Mount events reach both DebugChan and the writer if any. Partial failure
	// (AC3) is non-fatal; reattachMCPMounts records the err and prunes the
	// failed paths from proc.MCPMounts / mcpConfigs / AllowedDevices internally.
	k.reattachMCPMounts(proc, resumeInfo)

	proc.wg.Go(func() {
		defer func() { _ = k.vfs.CloseAll(proc.PID) }()
		// apex 10-11 started-semantics: when the caller supplied a LaunchReady
		// gate (resume_watch IPC handler), hold the reasoning round until the
		// handler has written its initial OK response, so the client learns the
		// process launched before any LLM work happens. nil gate = launch now.
		if opts.LaunchReady != nil {
			select {
			case <-opts.LaunchReady:
			case <-gctx.Done():
				return
			}
		}
		_ = proc.Start()

		if isContextFull {
			compactStart := time.Now()
			proc.compactMu.Lock()
			compactOpts := rnixctx.CompactOpts{
				LLMCall:       k.BuildCompactLLMCall(proc),
				Trigger:       "context_full_resume",
				ReadFileState: k.SnapshotReadFileState(proc),
				ActiveSkills:  k.BuildActiveSkills(proc),
				ActivePlan:    k.extractActivePlan(proc.CtxID),
			}
			compactResult, compactErr := k.ctxMgr.Compact(proc.CtxID, compactOpts)
			proc.compactMu.Unlock()
			if compactErr != nil {
				// Story 69.3 F1 — second, independent kill point (disk resume).
				// Same treatment as the checkpoint path: reclaim mechanically
				// first, terminate only if that leaves no room either.
				fallback, ok := k.reclaimForResume(proc)
				postSlotUsed, _, _ := k.ctxMgr.SlotUsage(proc.CtxID)
				args := map[string]any{
					"step":       startStep,
					"trigger":    "context_full_resume",
					"error":      compactErr.Error(),
					"post_slots": postSlotUsed,
				}
				addFallbackArgs(args, fallback)
				k.emitEvent(proc, "Compact", args, nil, compactErr, time.Since(compactStart))

				if !ok {
					log.Printf("[kernel] pid=%d resume(history) compact failed and mechanical fallback could not free room: %v", proc.PID, compactErr)
					k.finishProcess(proc, ExitStatus{Code: ExitContextFull, Reason: "context_full: resume compact failed", Err: compactErr})
					return
				}
				log.Printf("[kernel] pid=%d resume(history) compact failed (%v); mechanical fallback freed pruned=%d dropped_rounds=%d — continuing",
					proc.PID, compactErr, fallback.Pruned, fallback.DroppedRounds)
				proc.mu.Lock()
				proc.SuspendReason = ""
				proc.mu.Unlock()
				k.reasonStep(proc, llmFD, spawnOpts)
				return
			}
			k.emitEvent(proc, "Compact", map[string]any{
				"step":           startStep,
				"trigger":        "context_full_resume",
				"pre_tokens":     compactResult.PreTokens,
				"post_tokens":    compactResult.PostTokens,
				"restored_items": compactResult.ItemsRestored,
				"duration_ms":    float64(compactResult.Duration.Microseconds()) / 1000.0,
			}, nil, nil, compactResult.Duration)
			k.ClearReadFileState(proc)
		}

		k.reasonStep(proc, llmFD, spawnOpts)
	})

	// Story 44.3 AC#4 — subtree wakeup. When the just-resumed process is the
	// parent of any disk-reloaded Suspended placeholders (LoadSuspendedFromDisk
	// reconstructs ParentUUID-based linkage), trigger ResumeSubtree so the
	// entire subtree comes back together. Without this the user would have to
	// manually resume each descendant after every daemon restart, breaking the
	// "整子树恢复" guarantee from Epic 44 §AC-EA1.
	//
	// Re-attach Suspended children scanned by ParentUUID — cleanupOldProcessAndHistory
	// removed the old placeholder, so the new proc starts with empty Children.
	// Done synchronously while we still hold resumeMu (we are inside a critical
	// section started by ResumeWithOpts), and uses resumeSubtreeLocked to avoid
	// the double-lock that would deadlock on the public ResumeSubtree entry.
	if !opts.Fork {
		k.procTable.Range(func(_ types.PID, candidate *Process) bool {
			if candidate.PID == proc.PID {
				return true
			}
			if candidate.ParentUUID != proc.UUID {
				return true
			}
			if candidate.GetState() != types.StateSuspended {
				return true
			}
			if !slices.Contains(proc.GetChildren(), candidate.PID) {
				proc.AddChild(candidate.PID)
				candidate.PPID = proc.PID
			}
			return true
		})
		// Best-effort: failures are logged inside resumeSubtreeLocked / per
		// child; do not block the Resume return on subtree-wakeup errors.
		if _, _, subErr := k.resumeSubtreeLocked(proc); subErr != nil {
			log.Printf("[resume] subtree wakeup after history-resume uuid=%s failed: %v",
				uuid, subErr)
		}
	}

	if k.callbacks != nil {
		k.callbacks.OnSpawn(proc.PID, proc.Intent, proc.Provider, proc.Model, proc.UUID)
	}

	log.Printf("[kernel] resume(history) uuid=%s new_pid=%d from_step=%d fork=%v (took %v)",
		uuid, proc.PID, startStep, opts.Fork, time.Since(start))

	return &ResumeResult{
		PID:             proc.PID,
		UUID:            proc.UUID,
		ResumedFromStep: startStep,
	}, nil
}

// parseStepsJSONL reads steps.jsonl and extracts the last step number and messages.
// Messages semantics: each StepRecord.Messages field already contains the cumulative
// message history at the time of that LLM call (see observe.go writeStepRecord).
// Therefore we use the LAST record's messages, NOT the concatenation of all records
// (which would yield O(N^2) duplicates).
//
// Story 42.3 — maxStep parameter:
//   - maxStep == 0  → no truncation (legacy behavior preserved)
//   - maxStep  > 0  → only records with `step <= maxStep` are considered when
//     building the resumed-from-step boundary; the returned `lastStep` is capped
//     at `maxStep` and `messages` is sourced from the highest step <= maxStep.
//
// Returns (lastStep, messages, totalSteps, err) where totalSteps is the highest
// step number observed across the entire file (independent of maxStep) — callers
// use it for out-of-range validation in ResumeWithOpts.
func (k *KernelImpl) parseStepsJSONL(path string, maxStep int) (int, []rnixctx.Message, int, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil, 0, NewSyscallError("Resume", 0, "",
				fmt.Errorf("steps.jsonl not found"), types.ErrNotFound)
		}
		return 0, nil, 0, NewSyscallError("Resume", 0, "",
			fmt.Errorf("open steps.jsonl: %w", err), types.ErrInternal)
	}
	defer f.Close()

	var lastStep int   // highest step considered for messages (bounded by maxStep)
	var totalSteps int // highest step across the entire file (for range validation)
	var lastMessagesRaw json.RawMessage
	var seenInWindow bool // true once we encountered any record within [0..maxStep]
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var record stepRecordPartial
		if err := json.Unmarshal(line, &record); err != nil {
			return 0, nil, 0, NewSyscallError("Resume", 0, "",
				fmt.Errorf("parse steps.jsonl line: %w", err), types.ErrInternal)
		}
		if record.Step > totalSteps {
			totalSteps = record.Step
		}
		// Apply maxStep truncation. maxStep == 0 means "no truncation".
		if maxStep > 0 && record.Step > maxStep {
			continue
		}
		// Track the highest record within the [0..maxStep] window.
		// Use strict `>` (not `>=`) so a later duplicate record at the same step
		// cannot overwrite earlier captured messages with a shorter payload.
		// `seenInWindow` ensures the first record is always captured even when
		// `lastStep` is still zero-initialized.
		if !seenInWindow || record.Step > lastStep {
			seenInWindow = true
			lastStep = record.Step
			if len(record.Messages) > 0 {
				lastMessagesRaw = append(lastMessagesRaw[:0], record.Messages...)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, nil, 0, NewSyscallError("Resume", 0, "",
			fmt.Errorf("scan steps.jsonl: %w", err), types.ErrInternal)
	}

	var messages []rnixctx.Message
	if len(lastMessagesRaw) > 0 {
		if err := json.Unmarshal(lastMessagesRaw, &messages); err != nil {
			// Legacy/corrupt schema: log loud, return empty messages so caller can decide.
			// Don't fail the resume — system_prompt + most recent steps may be enough.
			log.Printf("[resume] parseStepsJSONL: messages unmarshal failed at step %d: %v", lastStep, err)
		}
	}
	return lastStep, messages, totalSteps, nil
}

// stepRecordPartial is a minimal struct for parsing steps.jsonl during resume.
type stepRecordPartial struct {
	Step     int             `json:"step"`
	Messages json.RawMessage `json:"messages"`
}
