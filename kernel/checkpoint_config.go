package kernel

// CheckpointConfig configures periodic checkpoint throttling (Story 42.2).
type CheckpointConfig struct {
	IntervalSteps   int
	IntervalSeconds int
}

// DefaultCheckpointConfig returns the policy default: 5 steps, 30 seconds.
//
// RED PHASE (Story 42.2): stub returns zero value; dev-story will return {5, 30}.
func DefaultCheckpointConfig() CheckpointConfig {
	return CheckpointConfig{}
}

// SetCheckpointConfig updates the kernel-wide checkpoint policy.
// Zero or negative values are normalized to defaults.
//
// RED PHASE (Story 42.2): no-op stub.
func (k *KernelImpl) SetCheckpointConfig(cfg CheckpointConfig) {
	// stub
}

// CheckpointCfg returns the active checkpoint policy.
//
// RED PHASE (Story 42.2): always returns DefaultCheckpointConfig().
func (k *KernelImpl) CheckpointCfg() CheckpointConfig {
	return DefaultCheckpointConfig()
}

// ShouldCheckpoint reports whether a checkpoint should be written for this step,
// based on step-count or time-window thresholds with lastCheckpointStep dedup.
//
// RED PHASE (Story 42.2): always returns false.
func (k *KernelImpl) ShouldCheckpoint(proc *Process, step int) bool {
	return false
}
