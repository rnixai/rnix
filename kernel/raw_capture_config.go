package kernel

// RawCaptureConfig is the kernel-level switch for LLM raw request/response
// logging (Story 56.1; Epic 56 CAP-4).
//
// Critical invariant: default MUST be Enabled=true (CAP-4 默认开). This config
// is *orthogonal* to FeatureFlags — it is NOT a feature-flag-style profile
// gate (FeatureFlags is profile-presets; baseline profile disables most
// flags). Bridging from internal/config is value-copy at daemon startup,
// mirroring FeatureFlags bridge (kernel/feature_flags.go:3-5 注释).
type RawCaptureConfig struct {
	Enabled        bool
	MaxOutputBytes int64
}

const defaultRawCaptureMaxOutputBytes = 4 * 1024 * 1024 // 4MB (Story 56.1 AC#5)

// DefaultRawCaptureConfig returns the policy default: Enabled=true,
// MaxOutputBytes=4MB. CAP-4 mandates default-on.
//
// 56.1 RED skeleton: returns the zero value so the default-on assertion
// fails. Dev-story sets Enabled=true and MaxOutputBytes=defaultRawCaptureMaxOutputBytes.
func DefaultRawCaptureConfig() RawCaptureConfig {
	_ = defaultRawCaptureMaxOutputBytes // referenced by dev-story; keep alive
	return RawCaptureConfig{}
}

// normalizeRawCaptureConfig clamps user-supplied values to safe defaults:
// negative MaxOutputBytes → default 4MB; zero MaxOutputBytes → default 4MB.
//
// 56.1 RED skeleton: returns cfg unchanged.
func normalizeRawCaptureConfig(cfg RawCaptureConfig) RawCaptureConfig {
	return cfg
}

// SetRawCaptureConfig updates the kernel-wide raw-capture policy.
// Mirrors SetGcConfig (kernel/gc_config.go).
func (k *KernelImpl) SetRawCaptureConfig(cfg RawCaptureConfig) {
	k.rawCaptureCfg = normalizeRawCaptureConfig(cfg)
}

// RawCaptureCfg returns the active raw-capture policy. If never set
// (zero-value KernelImpl), returns DefaultRawCaptureConfig() which has
// Enabled=true + 4MB. Mirrors GcCfg (kernel/gc_config.go).
func (k *KernelImpl) RawCaptureCfg() RawCaptureConfig {
	if k.rawCaptureCfg.MaxOutputBytes == 0 && !k.rawCaptureCfg.Enabled {
		return DefaultRawCaptureConfig()
	}
	return k.rawCaptureCfg
}
