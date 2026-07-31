package kernel

import (
	gocontext "context"
	"encoding/json"
	"fmt"
	"log"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/rnixai/rnix/agents"
	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// applyCompactTimeout resolves the compaction timeout for a freshly spawned
// process (Story 69.3 AC5): opts > agent manifest compact_timeout > leave the
// field zero so effectiveCompactTimeout() falls back to DefaultCompactTimeout
// (30s). Shape deliberately mirrors the StepTimeout block in spawn.go,
// including "a ParseDuration failure is ignored rather than fatal".
//
// ⚠️ Unlike StepTimeout, a configured 0 does NOT disable anything:
// effectiveCompactTimeout() maps 0 → 30s. A zero field is the only way to reach
// the default, so a manifest "0" is intentionally a no-op instead of an
// instant-expiry timeout (which would make compaction permanently unavailable).
//
// The 30s default is unchanged on purpose. The incident's 27/27 compaction
// timeouts were caused by cache-prefix invalidation (fixed in Story 69.1, which
// brought the call back to ~7.4s); raising the ceiling only converts a fast
// failure into a slow one. The answer to "the LLM is unavailable" is the
// mechanical fallback below, not a longer wait. This knob is an operator escape
// hatch.
func applyCompactTimeout(proc *Process, agent *agents.AgentInfo, opts SpawnOpts) {
	if opts.CompactTimeout > 0 {
		proc.CompactTimeout = opts.CompactTimeout
		return
	}
	if agent == nil || agent.Manifest.CompactTimeout == "" {
		return
	}
	d, err := time.ParseDuration(agent.Manifest.CompactTimeout)
	if err != nil {
		log.Printf("[kernel] agent %q has invalid compact_timeout %q: %v (using default %v)",
			agent.Manifest.Name, agent.Manifest.CompactTimeout, err, DefaultCompactTimeout)
		return
	}
	if d > 0 {
		proc.CompactTimeout = d
	}
}

// mechanicalFallbackName is the value of the `fallback` event key that marks a
// deterministic, LLM-free reclamation. Consumers distinguish a degraded
// compaction from a successful one purely by this key's PRESENCE — the success
// paths must never carry it (Story 69.3 AC7).
const mechanicalFallbackName = "mechanical_prune"

// mechanicalFallbackResult reports what runMechanicalFallback reclaimed.
type mechanicalFallbackResult struct {
	Pruned        int
	TokensFreed   int
	DroppedRounds int
	SlotsFreed    int
}

// freed reports whether anything at all came back.
func (r mechanicalFallbackResult) freed() bool {
	return r.TokensFreed > 0 || r.SlotsFreed > 0
}

// runMechanicalFallback is the deterministic, LLM-free reclamation used whenever
// a compaction attempt fails (Story 69.3 AC3 / AC4 / AC6). It is the reason a
// failed compaction no longer equals a dead process: the incident saw 27
// consecutive compaction failures and 0 successes, with no path that did not
// itself require the LLM.
//
// Two primitives, two axes — deliberately not merged (Story 69.3 decision 1):
//   - PruneToolResults reclaims TOKENS by rewriting cold leaked tool results in
//     place. It cannot free slots: slot usage is literally len(ctx.Messages).
//   - DropOldestRounds reclaims SLOTS by dropping whole API rounds. Only called
//     when the caller is actually short on slots (needSlots > 0), because
//     dropping history costs more than clearing stale payloads.
//
// Both preserve the tool_use ↔ tool_result pairing invariant; violating it makes
// anthropic reject the next request outright (its driver builds tool_result
// blocks straight from ToolCallID with no repair).
//
// Errors from either primitive are logged, not propagated: this is a
// best-effort last line of defence and the caller decides what to do with the
// (possibly zero) reclamation reported back.
func (k *KernelImpl) runMechanicalFallback(proc *Process, needSlots int) mechanicalFallbackResult {
	var out mechanicalFallbackResult

	if pruneRes, err := k.ctxMgr.PruneToolResults(proc.CtxID, rnixctx.PruneOpts{}); err != nil {
		log.Printf("[kernel] pid=%d mechanical prune failed: %v", proc.PID, err)
	} else if pruneRes != nil {
		out.Pruned = pruneRes.Pruned
		out.TokensFreed = pruneRes.TokensFreed
	}

	if needSlots > 0 {
		if dropRes, err := k.ctxMgr.DropOldestRounds(proc.CtxID, rnixctx.DropOpts{NeedSlots: needSlots}); err != nil {
			log.Printf("[kernel] pid=%d mechanical round drop failed: %v", proc.PID, err)
		} else if dropRes != nil {
			out.DroppedRounds = dropRes.DroppedRounds
			out.SlotsFreed = dropRes.SlotsFreed
			out.TokensFreed += dropRes.TokensFreed
		}
	}

	return out
}

// addFallbackArgs stamps a mechanical-fallback reclamation onto an event's args.
//
// AC7 (provenance): the `fallback` key is the sole discriminator, so it goes on
// degraded events ONLY. Never fill restored_items here — mechanical reclamation
// performs no post-compact restore — and never present these post_tokens as an
// LLM compaction result.
func addFallbackArgs(args map[string]any, res mechanicalFallbackResult) {
	args["fallback"] = mechanicalFallbackName
	args["pruned"] = res.Pruned
	args["tokens_freed"] = res.TokensFreed
	args["dropped_rounds"] = res.DroppedRounds
	args["slots_freed"] = res.SlotsFreed
	if !res.freed() {
		// Do not go silent when the fallback itself came up empty: the event is
		// still emitted and says so (AC3). Best-effort semantics are preserved —
		// the process is not terminated on this path.
		args["fallback_freed"] = 0
	}
}

// resumeFallbackHeadroom is the number of message slots a revived process needs
// before its first reasonStep can do anything useful (1 assistant + at least a
// couple of tool results).
const resumeFallbackHeadroom = 3

// reclaimForResume runs the mechanical fallback on behalf of a resume path and
// reports whether the process now has room to run (Story 69.3 AC6 / F1).
//
// Both resume paths used to call finishProcess(ExitContextFull) the moment the
// resume-time compaction failed — killing the process outright. That is a
// different code path from autoCompactIfNeeded (which is best-effort and never
// kills), and it is the real terminus of the incident's
// 2391→2392→2393→2394 chain: every revival immediately compacted, timed out
// after 30s, and died. Killing is now the LAST resort, reached only when the
// deterministic reclamation also came up empty.
func (k *KernelImpl) reclaimForResume(proc *Process) (mechanicalFallbackResult, bool) {
	needSlots := resumeFallbackHeadroom
	if avail, err := k.ctxMgr.AvailableSlots(proc.CtxID); err == nil && avail < resumeFallbackHeadroom {
		needSlots = resumeFallbackHeadroom - avail
	} else if err == nil {
		// Already has headroom; still prune tokens, but do not drop history.
		needSlots = 0
	}

	res := k.runMechanicalFallback(proc, needSlots)

	avail, err := k.ctxMgr.AvailableSlots(proc.CtxID)
	if err != nil {
		return res, false
	}
	return res, avail >= resumeFallbackHeadroom
}

// unloadForResume proactively reclaims context BEFORE a revived process starts
// reasoning (Story 69.3 AC5/AC6 preventive half). A snapshot restored right at
// its slot ceiling hits preCompactForToolCalls on step one, which is how a
// resumed process replays the very failure it was suspended for.
//
// Deliberately NOT gated on proc.CompactionDisabled: disabling routine
// compaction must not mean "prefer to hang". This is fault handling, not
// routine reclamation.
func (k *KernelImpl) unloadForResume(proc *Process, ctxSize int, label string) {
	if ctxSize <= 0 {
		return
	}
	used, _, err := k.ctxMgr.SlotUsage(proc.CtxID)
	if err != nil {
		return
	}
	threshold := proc.effectiveSlotCompactThreshold()
	if float64(used)/float64(ctxSize)*100 <= threshold {
		return
	}

	target := int(float64(ctxSize) * threshold / 100)
	res := k.runMechanicalFallback(proc, max(used-target, resumeFallbackHeadroom))
	postUsed, _, _ := k.ctxMgr.SlotUsage(proc.CtxID)
	log.Printf("[kernel] %s uuid=%s: pre-start unload at %d/%d slots (>%.0f%%): pruned=%d dropped_rounds=%d tokens_freed=%d → %d/%d slots",
		label, proc.UUID, used, ctxSize, threshold, res.Pruned, res.DroppedRounds, res.TokensFreed, postUsed, ctxSize)
}

// autoCompactIfNeeded checks if context token usage or slot usage exceeds the
// compact threshold and triggers automatic compaction if so. Best-effort:
// failures are logged but do not terminate the process.
func (k *KernelImpl) autoCompactIfNeeded(proc *Process, step int) {
	if proc.CompactionDisabled {
		return
	}
	// Prevent concurrent compact (auto + manual IPC)
	if !proc.compactMu.TryLock() {
		return
	}
	defer proc.compactMu.Unlock()

	usage, err := k.ctxMgr.TokenUsage(proc.CtxID)
	if err != nil {
		return
	}

	tokenThreshold := proc.effectiveCompactThreshold()
	tokenTriggered := usage.Percentage > tokenThreshold

	slotTriggered := false
	slotUsed, slotMax, slotErr := k.ctxMgr.SlotUsage(proc.CtxID)
	if slotErr == nil && slotMax > 0 {
		slotPct := float64(slotUsed) / float64(slotMax) * 100
		slotThreshold := proc.effectiveSlotCompactThreshold()
		slotTriggered = slotPct > slotThreshold
	}

	if !tokenTriggered && !slotTriggered {
		return
	}

	trigger := "token_threshold"
	if slotTriggered && !tokenTriggered {
		trigger = "slot_threshold"
	}
	if slotTriggered && tokenTriggered {
		trigger = "both"
	}

	compactStart := time.Now()
	log.Printf("[kernel] pid=%d step=%d auto-compact triggered (%s): token=%.1f%%, slots=%d/%d",
		proc.PID, step, trigger, usage.Percentage, slotUsed, slotMax)

	// Build CompactOpts using shared helpers
	readFileState := k.SnapshotReadFileState(proc)
	activeSkills := k.BuildActiveSkills(proc)

	// Get active plan from the most recent plan action in context
	activePlan := k.extractActivePlan(proc.CtxID)

	opts := rnixctx.CompactOpts{
		LLMCall:       k.BuildCompactLLMCall(proc),
		Trigger:       trigger,
		ReadFileState: readFileState,
		ActiveSkills:  activeSkills,
		ActivePlan:    activePlan,
	}

	result, err := k.ctxMgr.Compact(proc.CtxID, opts)
	if err != nil {
		// Story 69.3 AC3 — the LLM compaction failed, so fall back to the
		// deterministic reclamation instead of giving up. Before this, the
		// failure branch only logged: the incident's 27 consecutive failures
		// left the context untouched every single time, so the very next step
		// hit the same wall.
		//
		// Slot reclamation is requested only when this trigger actually
		// involved the slot axis; a pure token-threshold trigger is served by
		// the in-place prune alone (dropping history is the costlier remedy).
		needSlots := 0
		if slotTriggered && slotMax > 0 {
			// Bring usage back under the slot threshold.
			target := int(float64(slotMax) * proc.effectiveSlotCompactThreshold() / 100)
			needSlots = max(slotUsed-target, 1)
		}
		fallback := k.runMechanicalFallback(proc, needSlots)

		// pre_tokens must be sourced locally here: on the failure path `result`
		// is nil, so result.PreTokens (used by the success path below) would
		// panic. usage.Used was captured before the attempt.
		postUsage, _ := k.ctxMgr.TokenUsage(proc.CtxID)
		postSlotUsed, _, _ := k.ctxMgr.SlotUsage(proc.CtxID)

		args := map[string]any{
			"step":          step,
			"trigger":       trigger,
			"error":         err.Error(),
			"compact_error": err.Error(),
			"pre_tokens":    usage.Used,
			"post_tokens":   postUsage.Used,
			"pre_slots":     slotUsed,
			"post_slots":    postSlotUsed,
		}
		addFallbackArgs(args, fallback)
		k.emitEvent(proc, "Compact", args, nil, err, time.Since(compactStart))

		log.Printf("[kernel] pid=%d compact failed (%v); mechanical fallback: pruned=%d tokens %d→%d, dropped_rounds=%d slots %d→%d",
			proc.PID, err, fallback.Pruned, usage.Used, postUsage.Used,
			fallback.DroppedRounds, slotUsed, postSlotUsed)

		// Best-effort by contract: never terminate the process from here, even
		// when the fallback reclaimed nothing.
		return
	}

	// Clear ReadFileState after successful compact
	k.ClearReadFileState(proc)

	postSlotUsed, postSlotMax, _ := k.ctxMgr.SlotUsage(proc.CtxID)
	k.emitEvent(proc, "Compact", map[string]any{
		"step":           step,
		"trigger":        trigger,
		"pre_tokens":     result.PreTokens,
		"post_tokens":    result.PostTokens,
		"pre_slots":      slotUsed,
		"post_slots":     postSlotUsed,
		"restored_items": result.ItemsRestored,
		"duration_ms":    float64(result.Duration.Microseconds()) / 1000.0,
	}, nil, nil, result.Duration)

	log.Printf("[kernel] pid=%d compact done in %dms: %d→%d tokens, slots %d/%d→%d/%d, restored %d items",
		proc.PID, result.Duration.Milliseconds(),
		result.PreTokens, result.PostTokens,
		slotUsed, slotMax, postSlotUsed, postSlotMax,
		len(result.ItemsRestored))
}

// preCompactForToolCalls checks if there are enough message slots for the
// upcoming AppendAssistantWithToolCalls (1 assistant + N tool results) and
// triggers a compact if not. Best-effort: TryLock failure means another compact
// is running so we skip and let the caller try the append directly.
func (k *KernelImpl) preCompactForToolCalls(proc *Process, toolCallCount int, step int) error {
	required := 1 + toolCallCount
	available, err := k.ctxMgr.AvailableSlots(proc.CtxID)
	if err != nil || available >= required {
		return nil
	}

	if !proc.compactMu.TryLock() {
		return nil
	}
	defer proc.compactMu.Unlock()

	preSlotUsed, preSlotMax, _ := k.ctxMgr.SlotUsage(proc.CtxID)
	log.Printf("[kernel] pid=%d step=%d precompact: need %d slots, have %d",
		proc.PID, step, required, available)

	compactStart := time.Now()
	opts := rnixctx.CompactOpts{
		LLMCall:       k.BuildCompactLLMCall(proc),
		Trigger:       "precompact",
		ReadFileState: k.SnapshotReadFileState(proc),
		ActiveSkills:  k.BuildActiveSkills(proc),
		ActivePlan:    k.extractActivePlan(proc.CtxID),
	}

	result, err := k.ctxMgr.Compact(proc.CtxID, opts)
	if err != nil {
		// Story 69.3 AC4 — precompact pressure is PURELY about slots (see the
		// `required` computation above: tokens are never consulted), so the
		// token-axis prune cannot rescue this path. DropOldestRounds is what
		// moves AvailableSlots, which is exactly why the two primitives are
		// separate.
		//
		// Returning an error here is not the end of the world by itself
		// (tool_exec.go only logs it), but the very next
		// AppendAssistantWithToolCalls then returns ErrContextFull and the
		// process self-suspends. Reclaiming mechanically first turns that into a
		// continuable step.
		fallback := k.runMechanicalFallback(proc, required-available)
		postAvail, _ := k.ctxMgr.AvailableSlots(proc.CtxID)
		postSlotUsed, _, _ := k.ctxMgr.SlotUsage(proc.CtxID)

		args := map[string]any{
			"step":          step,
			"trigger":       "precompact",
			"error":         err.Error(),
			"compact_error": err.Error(),
			"pre_slots":     preSlotUsed,
			"post_slots":    postSlotUsed,
		}
		addFallbackArgs(args, fallback)
		k.emitEvent(proc, "Compact", args, nil, err, time.Since(compactStart))

		if postAvail >= required {
			log.Printf("[kernel] pid=%d precompact compact failed (%v) but mechanical fallback freed %d slots: need %d, have %d — continuing",
				proc.PID, err, fallback.SlotsFreed, required, postAvail)
			return nil
		}
		return fmt.Errorf("precompact failed: %w", err)
	}

	k.ClearReadFileState(proc)
	postSlotUsed, postSlotMax, _ := k.ctxMgr.SlotUsage(proc.CtxID)
	k.emitEvent(proc, "Compact", map[string]any{
		"step":           step,
		"trigger":        "precompact",
		"pre_tokens":     result.PreTokens,
		"post_tokens":    result.PostTokens,
		"pre_slots":      preSlotUsed,
		"post_slots":     postSlotUsed,
		"restored_items": result.ItemsRestored,
		"duration_ms":    float64(result.Duration.Microseconds()) / 1000.0,
	}, nil, nil, result.Duration)

	log.Printf("[kernel] pid=%d precompact done in %dms: %d→%d tokens, slots %d/%d→%d/%d, restored %d items",
		proc.PID, result.Duration.Milliseconds(),
		result.PreTokens, result.PostTokens,
		preSlotUsed, preSlotMax, postSlotUsed, postSlotMax,
		len(result.ItemsRestored))

	available, _ = k.ctxMgr.AvailableSlots(proc.CtxID)
	if available < required {
		// Story 69.3 AC4 — the compaction SUCCEEDED yet left too few slots (a
		// small context can compact down to boundary+summary and still not fit
		// 1 assistant + N tool results). Top up mechanically before declaring
		// insufficiency, same reasoning as the failure branch above.
		fallback := k.runMechanicalFallback(proc, required-available)
		postAvail, _ := k.ctxMgr.AvailableSlots(proc.CtxID)
		postSlotUsed2, _, _ := k.ctxMgr.SlotUsage(proc.CtxID)

		args := map[string]any{
			"step":       step,
			"trigger":    "precompact",
			"reason":     "post_compact_insufficient",
			"pre_slots":  postSlotUsed,
			"post_slots": postSlotUsed2,
		}
		addFallbackArgs(args, fallback)
		k.emitEvent(proc, "Compact", args, nil, nil, 0)

		if postAvail >= required {
			log.Printf("[kernel] pid=%d precompact still short after compact; mechanical fallback freed %d slots: need %d, have %d — continuing",
				proc.PID, fallback.SlotsFreed, required, postAvail)
			return nil
		}
		return fmt.Errorf("precompact freed space but still insufficient: need %d, have %d", required, postAvail)
	}
	return nil
}

// BuildCompactLLMCall creates the LLMCall callback for compact operations.
// Exported for use by IPC server's handleCompact.
func (k *KernelImpl) BuildCompactLLMCall(proc *Process) func(string, []rnixctx.Message) (string, error) {
	return func(sysPrompt string, messages []rnixctx.Message) (string, error) {
		timeout := proc.effectiveCompactTimeout()
		compactCtx, cancel := gocontext.WithTimeout(proc.ctx, timeout)
		defer cancel()

		// Open LLM device
		var fd types.FD
		var err error
		if proc.ProjectConfig != nil && proc.ProjectConfig.LLMFileOpener != nil {
			provider := strings.TrimPrefix(proc.PrimaryDevice, "/dev/llm/")
			fileAny, openErr := proc.ProjectConfig.LLMFileOpener(provider, int(vfs.O_RDWR))
			if openErr == nil {
				if vfsFile, ok := fileAny.(vfs.VFSFile); ok {
					fd = k.vfs.RegisterFD(proc.PID, vfsFile)
				} else {
					fd, err = k.vfs.Open(proc.PID, proc.PrimaryDevice, vfs.O_RDWR)
				}
			} else {
				fd, err = k.vfs.Open(proc.PID, proc.PrimaryDevice, vfs.O_RDWR)
			}
		} else {
			fd, err = k.vfs.Open(proc.PID, proc.PrimaryDevice, vfs.O_RDWR)
		}
		if err != nil {
			return "", fmt.Errorf("compact: open LLM device failed: %w", err)
		}
		defer func() { _ = k.vfs.Close(proc.PID, fd) }()

		// Build LLM request — no tools, simplified system prompt
		req := llmRequest{
			Intent:       "compact",
			SystemPrompt: sysPrompt,
			Model:        proc.Model,
			Messages:     messages,
			// No Tools — compact prompt explicitly prohibits tool use
		}
		reqJSON, err := json.Marshal(req)
		if err != nil {
			return "", fmt.Errorf("compact: marshal request failed: %w", err)
		}

		// Write request — use compactCtx for timeout propagation
		if err := k.vfs.Write(compactCtx, proc.PID, fd, reqJSON); err != nil {
			if compactCtx.Err() != nil {
				if compactCtx.Err() == gocontext.DeadlineExceeded {
					return "", fmt.Errorf("compact: LLM call timed out after %v", timeout)
				}
				return "", fmt.Errorf("compact: LLM call cancelled: %w", compactCtx.Err())
			}
			return "", fmt.Errorf("compact: LLM write failed: %w", err)
		}

		// Read response
		respData, err := k.vfs.Read(proc.PID, fd, 1<<20)
		if err != nil {
			if compactCtx.Err() != nil {
				if compactCtx.Err() == gocontext.DeadlineExceeded {
					return "", fmt.Errorf("compact: LLM call timed out after %v", timeout)
				}
				return "", fmt.Errorf("compact: LLM call cancelled: %w", compactCtx.Err())
			}
			return "", fmt.Errorf("compact: LLM read failed: %w", err)
		}

		// Parse response to extract content
		var resp llmResponse
		if err := json.Unmarshal(respData, &resp); err != nil {
			return "", fmt.Errorf("compact: unmarshal response failed: %w", err)
		}

		return resp.Content, nil
	}
}

// extractActivePlan scans the context for the most recent plan action content.
// Uses BuildPrompt to safely access messages without direct lock access.
func (k *KernelImpl) extractActivePlan(cid types.CtxID) string {
	prompt, err := k.ctxMgr.BuildPrompt(cid)
	if err != nil {
		return ""
	}

	// Scan messages in reverse to find the most recent plan
	for i := range slices.Backward(prompt.Messages) {
		msg := prompt.Messages[i]
		if msg.Role == rnixctx.RoleAssistant && strings.HasPrefix(msg.Content, "[Plan]") {
			return msg.Content
		}
	}
	return ""
}

// ExtractActivePlan is the exported variant for IPC server use.
func (k *KernelImpl) ExtractActivePlan(cid types.CtxID) string {
	return k.extractActivePlan(cid)
}

// SnapshotReadFileState returns a copy of the process's ReadFileState under lock.
func (k *KernelImpl) SnapshotReadFileState(proc *Process) map[string]rnixctx.ReadFileEntry {
	proc.mu.Lock()
	defer proc.mu.Unlock()
	if len(proc.ReadFileState) == 0 {
		return nil
	}
	snapshot := make(map[string]rnixctx.ReadFileEntry, len(proc.ReadFileState))
	maps.Copy(snapshot, proc.ReadFileState)
	return snapshot
}

// ClearReadFileState nils out the process's ReadFileState under lock.
func (k *KernelImpl) ClearReadFileState(proc *Process) {
	proc.mu.Lock()
	proc.ReadFileState = nil
	proc.mu.Unlock()
}

// BuildActiveSkills constructs SkillEntry list from the process's loaded skills.
func (k *KernelImpl) BuildActiveSkills(proc *Process) []rnixctx.SkillEntry {
	proc.mu.Lock()
	loadedSkills := make([]string, len(proc.Skills))
	copy(loadedSkills, proc.Skills)
	proc.mu.Unlock()

	var activeSkills []rnixctx.SkillEntry
	if k.skillLoader != nil {
		for _, name := range loadedSkills {
			if skill, err := k.skillLoader(name); err == nil {
				activeSkills = append(activeSkills, rnixctx.SkillEntry{
					Name:    name,
					Content: skill.Body,
				})
			}
		}
	}
	return activeSkills
}

// trackReadFile records a file read into the process's ReadFileState for
// post-compact restore (Story 31.2 AC#5).
func (k *KernelImpl) trackReadFile(proc *Process, path string, content string, mtime time.Time) {
	proc.mu.Lock()
	defer proc.mu.Unlock()
	if proc.ReadFileState == nil {
		proc.ReadFileState = make(map[string]rnixctx.ReadFileEntry)
	}
	proc.ReadFileState[path] = rnixctx.ReadFileEntry{
		Content:   content,
		Timestamp: time.Now(),
		Mtime:     mtime,
	}
}
