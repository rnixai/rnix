package kernel

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// CheckpointVersion is the schema version for checkpoint files.
const CheckpointVersion = 1

// CheckpointData holds a complete process checkpoint for crash recovery.
type CheckpointData struct {
	Version         int                 `json:"version"`
	UUID            string              `json:"uuid"`
	LastStep        int                 `json:"last_step"`
	Timestamp       time.Time           `json:"timestamp"`
	ContextSnapshot json.RawMessage     `json:"context_snapshot"` // Context.Serialize() raw JSON
	ProcState       CheckpointProcState `json:"proc_state"`
}

// CheckpointProcState captures the mutable process state at checkpoint time.
type CheckpointProcState struct {
	PID                   types.PID         `json:"pid"`
	Provider              string            `json:"provider"`
	Model                 string            `json:"model"`
	Skills                []string          `json:"skills"`
	AllowedDevices        []string          `json:"allowed_devices"`
	DeniedDevices         []string          `json:"denied_devices,omitempty"`
	AllowedTools          []string          `json:"allowed_tools,omitempty"` // Story 54.1: persisted authoritative tool whitelist; omitempty keeps legacy checkpoints clean
	Intent                string            `json:"intent"`
	IntentID              string            `json:"intent_id,omitempty"`
	MaxSteps              int               `json:"max_steps"`
	UsedTokens            int               `json:"used_tokens"`
	MaxTokens             int64             `json:"max_tokens,omitempty"`
	MaxCost               float64           `json:"max_cost,omitempty"`
	UsedCost              float64           `json:"used_cost,omitempty"`
	CtxSize               int               `json:"ctx_size,omitempty"`
	ConsecutiveToolErrors int               `json:"consecutive_tool_errors"`
	SuspendReason         string            `json:"suspend_reason,omitempty"`
	ParentUUID            string            `json:"parent_uuid,omitempty"`
	EnvSnapshot           map[string]string `json:"env_snapshot"`
}

// writeCheckpoint atomically writes a checkpoint file to dir/checkpoint.json.
// It writes to a .tmp file first, then renames (atomic on Linux ext4/btrfs).
func writeCheckpoint(dir string, data *CheckpointData) error {
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("checkpoint marshal: %w", err)
	}

	tmpPath := filepath.Join(dir, "checkpoint.json.tmp")
	finalPath := filepath.Join(dir, "checkpoint.json")

	if err := os.WriteFile(tmpPath, jsonBytes, 0o600); err != nil {
		return fmt.Errorf("checkpoint write tmp: %w", err)
	}

	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("checkpoint rename: %w", err)
	}

	return nil
}

// readCheckpoint reads and deserializes a checkpoint file from dir/checkpoint.json.
func readCheckpoint(dir string) (*CheckpointData, error) {
	data, err := os.ReadFile(filepath.Join(dir, "checkpoint.json"))
	if err != nil {
		return nil, fmt.Errorf("checkpoint read: %w", err)
	}

	var cp CheckpointData
	if err := json.Unmarshal(data, &cp); err != nil {
		return nil, fmt.Errorf("checkpoint unmarshal: %w", err)
	}

	if cp.Version != CheckpointVersion {
		return nil, fmt.Errorf("checkpoint version mismatch: got %d, want %d", cp.Version, CheckpointVersion)
	}

	return &cp, nil
}

// ReadCheckpointPublic is the exported wrapper for readCheckpoint.
func ReadCheckpointPublic(dir string) (*CheckpointData, error) {
	return readCheckpoint(dir)
}

// buildCheckpointData constructs a CheckpointData from the current process state.
// contextSnapshot must be pre-serialized by the caller (in the main goroutine)
// before handing off to an async writer.
func buildCheckpointData(proc *Process, step int, contextSnapshot json.RawMessage, consecutiveToolErrors int) *CheckpointData {
	proc.mu.Lock()
	tokensUsed := proc.TokensUsed
	budgetSnap := proc.Budget
	suspendReason := proc.SuspendReason
	parentUUID := proc.ParentUUID
	skills := make([]string, len(proc.Skills))
	copy(skills, proc.Skills)
	// Story 37.6 — snapshot both device lists under the lock. DeniedDevices is
	// the new symmetric field; AllowedDevices was previously read lock-free in the
	// struct literal below (a latent -race risk), so pull both reads in here to
	// keep the snapshot consistent with skills.
	allowedDevices := append([]string(nil), proc.AllowedDevices...)
	deniedDevices := append([]string(nil), proc.DeniedDevices...)
	// Story 54.1 — snapshot the authoritative tool-name whitelist under the same
	// lock so resume / daemon-restart revival keeps tool-level enforcement.
	allowedTools := append([]string(nil), proc.AllowedTools...)
	proc.mu.Unlock()

	// Snapshot relevant environment variables for resume drift detection (Story 30.4 review)
	envSnapshot := make(map[string]string)
	for _, key := range []string{"RNIX_ENV", "RNIX_ASCII", "HOME", "RNIX_LOG_DIR"} {
		if val := os.Getenv(key); val != "" {
			envSnapshot[key] = val
		}
	}

	return &CheckpointData{
		Version:         CheckpointVersion,
		UUID:            proc.UUID,
		LastStep:        step,
		Timestamp:       time.Now(),
		ContextSnapshot: contextSnapshot,
		ProcState: CheckpointProcState{
			PID:                   proc.PID,
			Provider:              proc.Provider,
			Model:                 proc.Model,
			Skills:                skills,
			AllowedDevices:        allowedDevices,
			DeniedDevices:         deniedDevices,
			AllowedTools:          allowedTools,
			Intent:                proc.Intent,
			MaxSteps:              proc.MaxSteps,
			CtxSize:               proc.CtxSize,
			UsedTokens:            tokensUsed,
			MaxTokens:             budgetSnap.MaxTokens,
			MaxCost:               budgetSnap.MaxCost,
			UsedCost:              budgetSnap.UsedCost,
			ConsecutiveToolErrors: consecutiveToolErrors,
			SuspendReason:         suspendReason,
			ParentUUID:            parentUUID,
			EnvSnapshot:           envSnapshot,
		},
	}
}
