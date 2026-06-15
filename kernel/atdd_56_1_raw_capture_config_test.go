package kernel

import (
	"reflect"
	"testing"
)

// ============================================================================
// ATDD Story 56.1 — kernel.RawCaptureConfig 默认开 + 正交 (AC#5)
//
// 56-1-UNIT-006/007/008. CAP-4 红线：默认开必须始终成立，且配置不得回归到
// FeatureFlags（baseline profile 全 false 会让默认开失效）。
//
// RED：DefaultRawCaptureConfig 当前返回零值（Enabled=false / 0B），UNIT-006
// 真实失败；UNIT-007 normalize 是 t.Skip("RED")；UNIT-008 是 GREEN-stays-GREEN
// 护栏（FeatureFlags 当前不含 RawCapture 字段，骨架立即通过；若 dev-story
// 不慎把开关塞进 FeatureFlags，这条会立即变红）。
// ============================================================================

// 56-1-UNIT-006 (P0, CAP-4 红线): DefaultRawCaptureConfig 必须默认开 + 4MB。
func TestATDD_56_1_006_DefaultRawCaptureConfig_EnabledAnd4MB(t *testing.T) {
	cfg := DefaultRawCaptureConfig()
	if !cfg.Enabled {
		t.Errorf("DefaultRawCaptureConfig.Enabled = false, want true (CAP-4 默认开红线)")
	}
	const want = 4 * 1024 * 1024
	if cfg.MaxOutputBytes != want {
		t.Errorf("DefaultRawCaptureConfig.MaxOutputBytes = %d, want %d (4MB)",
			cfg.MaxOutputBytes, want)
	}
}

// 56-1-UNIT-007: SetRawCaptureConfig normalize — 零值 / 负值 兜底为默认。
func TestATDD_56_1_007_SetRawCaptureConfig_Normalizes(t *testing.T) {
	t.Skip("RED: 56-1 dev-story removes this skip after implementing normalizeRawCaptureConfig")

	const wantDefault = int64(4 * 1024 * 1024)

	cases := []struct {
		name        string
		in          RawCaptureConfig
		wantEnabled bool
		wantBytes   int64
	}{
		{
			name:        "zero MaxOutputBytes 兜底为 4MB",
			in:          RawCaptureConfig{Enabled: true, MaxOutputBytes: 0},
			wantEnabled: true,
			wantBytes:   wantDefault,
		},
		{
			name:        "negative MaxOutputBytes 兜底为 4MB",
			in:          RawCaptureConfig{Enabled: true, MaxOutputBytes: -1024},
			wantEnabled: true,
			wantBytes:   wantDefault,
		},
		{
			name:        "valid passthrough",
			in:          RawCaptureConfig{Enabled: false, MaxOutputBytes: 8192},
			wantEnabled: false,
			wantBytes:   8192,
		},
		{
			name:        "Enabled=true + 1MB passthrough",
			in:          RawCaptureConfig{Enabled: true, MaxOutputBytes: 1 << 20},
			wantEnabled: true,
			wantBytes:   1 << 20,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			k := &KernelImpl{}
			k.SetRawCaptureConfig(tc.in)
			got := k.RawCaptureCfg()
			if got.Enabled != tc.wantEnabled {
				t.Errorf("Enabled = %v, want %v", got.Enabled, tc.wantEnabled)
			}
			if got.MaxOutputBytes != tc.wantBytes {
				t.Errorf("MaxOutputBytes = %d, want %d", got.MaxOutputBytes, tc.wantBytes)
			}
		})
	}
}

// 56-1-UNIT-008 (P0, 防回归到 FeatureFlags): RawCapture 必须与 FeatureFlags 正交。
//
// 反模式守卫 — Dev Notes 红线规定：开关不得放进 FeatureFlags（ProfileBaseline
// 预设全 false → baseline 进程会默认关 raw capture，违反 CAP-4）。
//
// 这条断言通过反射枚举 FeatureFlags 字段，确保里面**没有** RawCapture 相关字段。
// 如果 dev-story 不慎把 raw capture 开关塞进 FeatureFlags，这条会立即变红。
//
// GREEN-stays-GREEN 护栏：当前 FeatureFlags 没有 RawCapture 字段，骨架直接 PASS。
func TestATDD_56_1_008_RawCapture_OrthogonalToFeatureFlags(t *testing.T) {
	rt := reflect.TypeFor[FeatureFlags]()
	for f := range rt.Fields() {
		name := f.Name
		// 任何包含 "Raw" / "Capture" 的 FeatureFlags 字段都视作违反正交红线
		if containsAny(name, "Raw", "Capture") {
			t.Errorf("FeatureFlags has forbidden field %q — RawCaptureConfig must be orthogonal "+
				"(Dev Notes 红线: ProfileBaseline 全 false 会让 CAP-4 默认开失效)", name)
		}
	}

	// 同时确认 baseline profile 下 RawCaptureCfg().Enabled 仍为 true（终态行为：
	// 在零值 KernelImpl 上调用应当回退到 DefaultRawCaptureConfig）。
	k := &KernelImpl{}
	cfg := k.RawCaptureCfg()
	if !cfg.Enabled {
		t.Errorf("zero-value KernelImpl.RawCaptureCfg().Enabled = false, "+
			"want true (CAP-4 默认开)；got cfg=%+v", cfg)
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(sub) == 0 {
			continue
		}
		if indexOf(s, sub) >= 0 {
			return true
		}
	}
	return false
}

func indexOf(s, sub string) int {
	n, m := len(s), len(sub)
	for i := 0; i+m <= n; i++ {
		if s[i:i+m] == sub {
			return i
		}
	}
	return -1
}
