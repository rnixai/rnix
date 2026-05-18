package kernel

import (
	"testing"
)

// =============================================================================
// ATDD 42.5: 治理层 — GcConfig 默认值与归一化 (AC#7, #8)
//
// Covers DefaultGcConfig + SetGcConfig normalization. These tests target the
// simple config-layer pieces that ship as real implementations in the RED
// phase (mirrors 42.2's CheckpointConfig pattern). All assertions hold from
// day one — no t.Skip needed.
// =============================================================================

// --- 42.5-UNIT-001: DefaultGcConfig disabled-by-default (AC#8) ---

func TestATDD_42_5_001_DefaultGcConfig_Disabled(t *testing.T) {
	cfg := DefaultGcConfig()
	if cfg.RetentionDays != 0 {
		t.Errorf("RetentionDays = %d, want 0 (AC#8: 全零 = 关闭)", cfg.RetentionDays)
	}
	if cfg.MaxEntries != 0 {
		t.Errorf("MaxEntries = %d, want 0 (AC#8: 全零 = 关闭)", cfg.MaxEntries)
	}
	if cfg.IntervalSeconds != 3600 {
		t.Errorf("IntervalSeconds = %d, want 3600 (1h default)", cfg.IntervalSeconds)
	}
}

// --- 42.5-UNIT-002: SetGcConfig normalizes bad values (AC#7, #8) ---

func TestATDD_42_5_002_SetGcConfig_NormalizesInvalid(t *testing.T) {
	k := newThrottleTestKernel(t)

	cases := []struct {
		name          string
		in            GcConfig
		wantRetention int
		wantMax       int
		wantInterval  int
	}{
		{
			name:          "all zero — disabled with default interval",
			in:            GcConfig{RetentionDays: 0, MaxEntries: 0, IntervalSeconds: 0},
			wantRetention: 0,
			wantMax:       0,
			wantInterval:  3600,
		},
		{
			name:          "negative retention normalized to 0",
			in:            GcConfig{RetentionDays: -5, MaxEntries: 100, IntervalSeconds: 600},
			wantRetention: 0,
			wantMax:       100,
			wantInterval:  600,
		},
		{
			name:          "negative max_entries normalized to 0",
			in:            GcConfig{RetentionDays: 30, MaxEntries: -10, IntervalSeconds: 600},
			wantRetention: 30,
			wantMax:       0,
			wantInterval:  600,
		},
		{
			name:          "interval < 60 clamps to 60",
			in:            GcConfig{RetentionDays: 30, MaxEntries: 100, IntervalSeconds: 30},
			wantRetention: 30,
			wantMax:       100,
			wantInterval:  60,
		},
		{
			name:          "valid passthrough",
			in:            GcConfig{RetentionDays: 30, MaxEntries: 500, IntervalSeconds: 3600},
			wantRetention: 30,
			wantMax:       500,
			wantInterval:  3600,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			k.SetGcConfig(tc.in)
			got := k.GcCfg()
			if got.RetentionDays != tc.wantRetention {
				t.Errorf("RetentionDays = %d, want %d", got.RetentionDays, tc.wantRetention)
			}
			if got.MaxEntries != tc.wantMax {
				t.Errorf("MaxEntries = %d, want %d", got.MaxEntries, tc.wantMax)
			}
			if got.IntervalSeconds != tc.wantInterval {
				t.Errorf("IntervalSeconds = %d, want %d", got.IntervalSeconds, tc.wantInterval)
			}
		})
	}
}

// --- 42.5-UNIT-003: GcCfg returns DefaultGcConfig when never set ---

func TestATDD_42_5_003_GcCfg_DefaultWhenUnset(t *testing.T) {
	k := newThrottleTestKernel(t)
	// Do NOT call SetGcConfig — the kernel starts with a zero-value GcConfig.
	got := k.GcCfg()
	if got.IntervalSeconds != 3600 {
		t.Errorf("GcCfg() IntervalSeconds = %d, want 3600 (DefaultGcConfig safe degradation)", got.IntervalSeconds)
	}
	if got.RetentionDays != 0 || got.MaxEntries != 0 {
		t.Errorf("GcCfg() without SetGcConfig must be disabled; got %+v", got)
	}
}
