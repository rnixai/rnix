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
	Intent                string            `json:"intent"`
	IntentID              string            `json:"intent_id,omitempty"`
	MaxSteps              int               `json:"max_steps"`
	UsedTokens            int               `json:"used_tokens"`
	ConsecutiveToolErrors int               `json:"consecutive_tool_errors"`
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
	skills := make([]string, len(proc.Skills))
	copy(skills, proc.Skills)
	proc.mu.Unlock()

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
			AllowedDevices:        append([]string(nil), proc.AllowedDevices...),
			Intent:                proc.Intent,
			MaxSteps:              proc.MaxSteps,
			UsedTokens:            tokensUsed,
			ConsecutiveToolErrors: consecutiveToolErrors,
		},
	}
}
