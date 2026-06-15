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
func DefaultRawCaptureConfig() RawCaptureConfig {
	return RawCaptureConfig{
		Enabled:        true,
		MaxOutputBytes: defaultRawCaptureMaxOutputBytes,
	}
}

// normalizeRawCaptureConfig clamps user-supplied values to safe defaults:
// zero or negative MaxOutputBytes → default 4MB. Enabled is preserved
// verbatim so callers can explicitly opt-out via Enabled=false.
func normalizeRawCaptureConfig(cfg RawCaptureConfig) RawCaptureConfig {
	if cfg.MaxOutputBytes <= 0 {
		cfg.MaxOutputBytes = defaultRawCaptureMaxOutputBytes
	}
	return cfg
}

// SetRawCaptureConfig updates the kernel-wide raw-capture policy.
// Mirrors SetGcConfig (kernel/gc_config.go). Concurrent-safe via rawCaptureMu
// (review patch P3 — rawCaptureCfg is read on every reasonStep hot path).
func (k *KernelImpl) SetRawCaptureConfig(cfg RawCaptureConfig) {
	cfg = normalizeRawCaptureConfig(cfg)
	k.rawCaptureMu.Lock()
	k.rawCaptureCfg = cfg
	k.rawCaptureMu.Unlock()
}

// RawCaptureCfg returns the active raw-capture policy. If never set
// (zero-value KernelImpl), returns DefaultRawCaptureConfig() which has
// Enabled=true + 4MB. Mirrors GcCfg (kernel/gc_config.go).
func (k *KernelImpl) RawCaptureCfg() RawCaptureConfig {
	k.rawCaptureMu.RLock()
	cfg := k.rawCaptureCfg
	k.rawCaptureMu.RUnlock()
	if cfg.MaxOutputBytes == 0 && !cfg.Enabled {
		return DefaultRawCaptureConfig()
	}
	return cfg
}
