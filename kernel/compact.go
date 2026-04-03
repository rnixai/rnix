package kernel

import (
	"encoding/json"
	"fmt"
	"log"
	"maps"
	"strings"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// autoCompactIfNeeded checks if context token usage exceeds the compact threshold
// and triggers automatic compaction if so. Best-effort: failures are logged but
// do not terminate the process (AC#2).
func (k *KernelImpl) autoCompactIfNeeded(proc *Process, step int) {
	// Prevent concurrent compact (auto + manual IPC)
	if !proc.compactMu.TryLock() {
		return
	}
	defer proc.compactMu.Unlock()

	usage, err := k.ctxMgr.TokenUsage(proc.CtxID)
	if err != nil {
		return
	}

	threshold := proc.effectiveCompactThreshold()
	if usage.Percentage <= threshold {
		return
	}

	compactStart := time.Now()
	log.Printf("[kernel] pid=%d step=%d auto-compact triggered: %.1f%% > %.1f%%", proc.PID, step, usage.Percentage, threshold)

	// Build CompactOpts using shared helpers
	readFileState := k.SnapshotReadFileState(proc)
	activeSkills := k.BuildActiveSkills(proc)

	// Get active plan from the most recent plan action in context
	activePlan := k.extractActivePlan(proc.CtxID)

	opts := rnixctx.CompactOpts{
		LLMCall:       k.BuildCompactLLMCall(proc),
		Trigger:       "auto",
		ReadFileState: readFileState,
		ActiveSkills:  activeSkills,
		ActivePlan:    activePlan,
	}

	result, err := k.ctxMgr.Compact(proc.CtxID, opts)
	if err != nil {
		log.Printf("[kernel] pid=%d compact failed: %v", proc.PID, err)
		k.emitEvent(proc, "Compact", map[string]any{
			"step":    step,
			"trigger": "auto",
			"error":   err.Error(),
		}, nil, err, time.Since(compactStart))
		return
	}

	// Clear ReadFileState after successful compact
	k.ClearReadFileState(proc)

	k.emitEvent(proc, "Compact", map[string]any{
		"step":           step,
		"trigger":        "auto",
		"pre_tokens":     result.PreTokens,
		"post_tokens":    result.PostTokens,
		"restored_items": result.ItemsRestored,
		"duration_ms":    float64(result.Duration.Microseconds()) / 1000.0,
	}, nil, nil, result.Duration)

	log.Printf("[kernel] pid=%d compact done: %d→%d tokens, restored %d items",
		proc.PID, result.PreTokens, result.PostTokens, len(result.ItemsRestored))
}

// BuildCompactLLMCall creates the LLMCall callback for compact operations.
// Exported for use by IPC server's handleCompact.
func (k *KernelImpl) BuildCompactLLMCall(proc *Process) func(string, []rnixctx.Message) (string, error) {
	return func(sysPrompt string, messages []rnixctx.Message) (string, error) {
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

		// Write request
		if err := k.vfs.Write(proc.ctx, proc.PID, fd, reqJSON); err != nil {
			return "", fmt.Errorf("compact: LLM write failed: %w", err)
		}

		// Read response
		respData, err := k.vfs.Read(proc.PID, fd, 1<<20)
		if err != nil {
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
	for i := len(prompt.Messages) - 1; i >= 0; i-- {
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
func (k *KernelImpl) trackReadFile(proc *Process, path string, content string) {
	proc.mu.Lock()
	defer proc.mu.Unlock()
	if proc.ReadFileState == nil {
		proc.ReadFileState = make(map[string]rnixctx.ReadFileEntry)
	}
	proc.ReadFileState[path] = rnixctx.ReadFileEntry{
		Content:   content,
		Timestamp: time.Now(),
	}
}
