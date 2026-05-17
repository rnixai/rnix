package kernel

import "time"

// CheckpointConfig configures periodic checkpoint throttling (Story 42.2).
//
// IntervalSteps: minimum step delta between checkpoints (step-count trigger).
// IntervalSeconds: minimum wall-clock seconds between checkpoints (time trigger).
// A checkpoint fires when either threshold is reached AND step > lastCheckpointStep.
type CheckpointConfig struct {
	IntervalSteps   int
	IntervalSeconds int
}

const (
	defaultCheckpointIntervalSteps   = 5
	defaultCheckpointIntervalSeconds = 30
)

// DefaultCheckpointConfig returns the policy default: 5 steps, 30 seconds.
func DefaultCheckpointConfig() CheckpointConfig {
	return CheckpointConfig{
		IntervalSteps:   defaultCheckpointIntervalSteps,
		IntervalSeconds: defaultCheckpointIntervalSeconds,
	}
}

// normalizeCheckpointConfig replaces zero or negative values with policy defaults.
func normalizeCheckpointConfig(cfg CheckpointConfig) CheckpointConfig {
	if cfg.IntervalSteps <= 0 {
		cfg.IntervalSteps = defaultCheckpointIntervalSteps
	}
	if cfg.IntervalSeconds <= 0 {
		cfg.IntervalSeconds = defaultCheckpointIntervalSeconds
	}
	return cfg
}

// SetCheckpointConfig updates the kernel-wide checkpoint policy.
// Zero or negative values are normalized to defaults (AC#9).
func (k *KernelImpl) SetCheckpointConfig(cfg CheckpointConfig) {
	k.checkpointCfg = normalizeCheckpointConfig(cfg)
}

// CheckpointCfg returns the active checkpoint policy. If never set, returns
// the policy default (AC#9 safe degradation).
func (k *KernelImpl) CheckpointCfg() CheckpointConfig {
	if k.checkpointCfg.IntervalSteps <= 0 || k.checkpointCfg.IntervalSeconds <= 0 {
		return DefaultCheckpointConfig()
	}
	return k.checkpointCfg
}

// ShouldCheckpoint reports whether a checkpoint should be written for this step,
// based on step-count or time-window thresholds with lastCheckpointStep dedup.
//
//   - AC#1: step - lastCheckpointStep >= IntervalSteps triggers a write.
//   - AC#2: time.Since(lastCheckpointTime) >= IntervalSeconds triggers a write.
//   - AC#3: step <= lastCheckpointStep never triggers (monotonic dedup).
//
// First-time invocation (lastCheckpointStep=0, lastCheckpointTime=zero) honors
// the step-count threshold: step >= IntervalSteps yields true.
func (k *KernelImpl) ShouldCheckpoint(proc *Process, step int) bool {
	cfg := k.CheckpointCfg()
	proc.mu.Lock()
	lastStep := proc.lastCheckpointStep
	lastTime := proc.lastCheckpointTime
	proc.mu.Unlock()

	if step <= lastStep {
		return false // AC#3: dedup; never re-checkpoint the same step
	}
	if step-lastStep >= cfg.IntervalSteps {
		return true // AC#1: step-count trigger
	}
	if !lastTime.IsZero() && time.Since(lastTime) >= time.Duration(cfg.IntervalSeconds)*time.Second {
		return true // AC#2: time-window trigger
	}
	return false
}
