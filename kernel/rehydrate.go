package kernel

import (
	"encoding/json"
	"fmt"
	"log"
	"maps"
	"os"
	"path/filepath"
	"strings"

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
//                      already set. proc.PrimaryDevice must be set by the
//                      caller (rehydrate does NOT open the LLM FD).
//   - stepsDir       — absolute path to <baseDir>/data/steps/<uuid>.
//   - ctxSizeHint    — preferred ctx slot count; 0 falls back to DefaultCtxSize.
//   - maxStep        — Story 42.3 history-replay truncation. 0 = no truncation
//                      (LoadSuspendedFromDisk default). > 0 = consider only
//                      records with step <= maxStep when picking lastStep /
//                      messages; the file is still scanned for totalSteps.
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
	metaPath := filepath.Join(stepsDir, "process-meta.json")
	metaData, err := os.ReadFile(metaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, NewSyscallError("Rehydrate", proc.PID, "",
				fmt.Errorf("process-meta.json missing for UUID %s", proc.UUID),
				types.ErrNotFound)
		}
		return 0, 0, NewSyscallError("Rehydrate", proc.PID, "",
			fmt.Errorf("read process-meta.json: %w", err), types.ErrInternal)
	}
	var meta struct {
		SystemPrompt string `json:"system_prompt"`
	}
	if err := json.Unmarshal(metaData, &meta); err != nil {
		return 0, 0, NewSyscallError("Rehydrate", proc.PID, "",
			fmt.Errorf("parse process-meta.json: %w", err), types.ErrInternal)
	}
	if meta.SystemPrompt == "" {
		return 0, 0, NewSyscallError("Rehydrate", proc.PID, "",
			fmt.Errorf("system_prompt missing in process-meta.json for UUID %s", proc.UUID),
			types.ErrInvalid)
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
	vfsDefs, vfsMap := buildToolDefs(k.vfs.DeviceRegistry(), proc.AllowedDevices, proc.PlanningEnabled)
	metaDefs, metaMap := metaToolDefs(proc.PlanningEnabled, proc.DeferredSkills)
	allDefs := make([]vfs.ToolDef, 0, len(vfsDefs)+len(metaDefs))
	allDefs = append(allDefs, vfsDefs...)
	allDefs = append(allDefs, metaDefs...)
	proc.mu.Lock()
	proc.nativeToolDefs = allDefs
	proc.toolMap = make(map[string]toolMapping, len(vfsMap)+len(metaMap))
	maps.Copy(proc.toolMap, vfsMap)
	maps.Copy(proc.toolMap, metaMap)
	proc.mu.Unlock()

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
	//    system prompt into it. Prefer the caller-supplied ctxSizeHint
	//    (originally from the disk snapshot's CtxSize) so N-step processes
	//    revived into a 256-slot context do not silently lose messages past
	//    slot 256. Zero or negative hint falls back to DefaultCtxSize.
	resumeCtxSize := ctxSizeHint
	if resumeCtxSize <= 0 {
		resumeCtxSize = DefaultCtxSize
	}
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

	snapshot := struct {
		SystemPrompt string            `json:"system_prompt"`
		Messages     []rnixctx.Message `json:"messages"`
		MaxSize      int               `json:"max_size"`
	}{
		SystemPrompt: meta.SystemPrompt,
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

	// 7. Bookkeeping: record the highest replayed step so subtree.go's
	//    resumeOneForSubtree fallback (LastCompletedStep+1) lands on a sane
	//    startStep without re-parsing steps.jsonl. Use the atomic setter
	//    rather than touching the field directly so any future readers that
	//    race with us see the value via Load().
	if lastStep > 0 {
		proc.LastCompletedStep.Store(int64(lastStep))
	}

	return lastStep, totalSteps, nil
}
