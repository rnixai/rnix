package kernel

import (
	"testing"
)

// =============================================================================
// ATDD 42.2: 韧性层 — CheckpointConfig 默认值与归一化（AC#9）
//
// Covers DefaultCheckpointConfig + SetCheckpointConfig normalization of zero
// or negative interval values.
//
// RED PHASE: stubs return zero value / no-op (see checkpoint_config.go).
// =============================================================================

// --- 42.2-UNIT-004: 默认值生效 (AC#9) ---

func TestATDD_42_2_004_DefaultConfig_Values(t *testing.T) {
	t.Skip("RED PHASE: DefaultCheckpointConfig returns zero value; dev-story sets {5, 30}")

	cfg := DefaultCheckpointConfig()
	if cfg.IntervalSteps != 5 {
		t.Errorf("IntervalSteps = %d, want 5 (AC#9 default)", cfg.IntervalSteps)
	}
	if cfg.IntervalSeconds != 30 {
		t.Errorf("IntervalSeconds = %d, want 30 (AC#9 default)", cfg.IntervalSeconds)
	}
}

// --- 42.2-UNIT-005: 零值/负值归一化 (AC#9) ---

func TestATDD_42_2_005_SetCheckpointConfig_NormalizesInvalid(t *testing.T) {
	t.Skip("RED PHASE: SetCheckpointConfig is a no-op stub; dev-story implements normalization")

	k := newThrottleTestKernel(t)

	cases := []struct {
		name      string
		in        CheckpointConfig
		wantSteps int
		wantSecs  int
	}{
		{"both zero", CheckpointConfig{IntervalSteps: 0, IntervalSeconds: 0}, 5, 30},
		{"both negative", CheckpointConfig{IntervalSteps: -1, IntervalSeconds: -5}, 5, 30},
		{"steps zero only", CheckpointConfig{IntervalSteps: 0, IntervalSeconds: 10}, 5, 10},
		{"seconds zero only", CheckpointConfig{IntervalSteps: 3, IntervalSeconds: 0}, 3, 30},
		{"valid passthrough", CheckpointConfig{IntervalSteps: 7, IntervalSeconds: 45}, 7, 45},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			k.SetCheckpointConfig(tc.in)
			got := k.CheckpointCfg()
			if got.IntervalSteps != tc.wantSteps {
				t.Errorf("IntervalSteps = %d, want %d", got.IntervalSteps, tc.wantSteps)
			}
			if got.IntervalSeconds != tc.wantSecs {
				t.Errorf("IntervalSeconds = %d, want %d", got.IntervalSeconds, tc.wantSecs)
			}
		})
	}
}
