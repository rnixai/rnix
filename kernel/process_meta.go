package kernel

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/rnixai/rnix/internal/types"
)

// processMetaFilename is the on-disk name of the process-meta snapshot.
const processMetaFilename = "process-meta.json"

// processMetaDisk mirrors the JSON shape historically written by reapProcess
// (kernel/reap.go:79-92). Defining a typed struct here lets writeProcessMeta
// be the single source of truth so suspend / reap / future writers cannot
// drift in shape.
type processMetaDisk struct {
	PID          types.PID `json:"pid"`
	SystemPrompt string    `json:"system_prompt"`
	ToolDefs     []any     `json:"tool_defs"`
}

// writeProcessMeta persists <baseDir>/data/steps/<uuid>/process-meta.json from
// the process's in-memory FinalSystemPrompt + nativeToolDefs. It is shared by
// reapProcess (called once on Zombie → Dead) and suspendProcess (called on
// Running → Suspended) so a daemon restart can rehydrate the placeholder back
// into a fully-armed Process via rehydrateRuntimeStateFromDisk.
//
// Returns nil on best-effort skip:
//   - baseDir empty or UUID empty → nothing to persist, no error.
//   - FinalSystemPrompt empty → nothing meaningful to write; skip (the caller's
//     spawn path is responsible for capturing the prompt eagerly, see
//     kernel/spawn.go:697).
//
// Caller should treat any returned error as best-effort: log but do NOT block
// the calling state transition (44.1 dashboard `p` / Ctrl+C hot path must
// stay responsive even when the filesystem is wedged).
func writeProcessMeta(baseDir string, proc *Process) error {
	if baseDir == "" || proc == nil || proc.UUID == "" {
		return nil
	}

	proc.mu.Lock()
	fsp := proc.FinalSystemPrompt
	toolDefs := proc.nativeToolDefs
	pid := proc.PID
	proc.mu.Unlock()

	if fsp == "" {
		// Spawn captures FinalSystemPrompt eagerly (kernel/spawn.go:697-712);
		// an empty value here implies the process never made it past Spawn's
		// system-prompt construction, in which case there is nothing to
		// persist. Surfacing this silently matches the pre-extract behavior:
		// reap.go also wrote an empty `system_prompt` field without erroring.
		return nil
	}

	meta := processMetaDisk{
		PID:          pid,
		SystemPrompt: fsp,
	}
	if toolDefs != nil {
		meta.ToolDefs = make([]any, len(toolDefs))
		for i, td := range toolDefs {
			meta.ToolDefs[i] = td
		}
	}
	data, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal process-meta: %w", err)
	}

	dir := filepath.Join(baseDir, "data", "steps", proc.UUID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir process-meta target: %w", err)
	}
	path := filepath.Join(dir, processMetaFilename)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write process-meta: %w", err)
	}
	return nil
}

// writeProcessMetaBestEffort wraps writeProcessMeta with the standard logging
// boilerplate every caller used to inline. Returns no error — the whole point
// is to keep state transitions moving on filesystem hiccups.
func writeProcessMetaBestEffort(baseDir string, proc *Process, callerTag string) {
	if err := writeProcessMeta(baseDir, proc); err != nil {
		log.Printf("[%s] process-meta.json write error pid=%d uuid=%s: %v",
			callerTag, proc.PID, proc.UUID, err)
	}
}
