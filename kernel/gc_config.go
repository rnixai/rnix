package kernel

// GcConfig configures the disk garbage collector for `.rnix/data/steps/<uuid>/`
// (Story 42.5).
//
//   - RetentionDays: time-window cleanup threshold. dead_at older than this many
//     days makes the entry a candidate. 0 disables the time-window rule.
//   - MaxEntries: count-window cleanup threshold. When total terminated entries
//     exceed this, the oldest are reaped until the count is at or below MaxEntries.
//     0 disables the count-window rule.
//   - IntervalSeconds: background daemon scan period. Floored to 60 by
//     normalizeGcConfig; 0 uses the default (3600 = 1 hour).
//
// Both retention rules are AND-relaxed: a candidate matching either rule is
// reaped. When both are zero, gc is fully disabled (AC#8).
type GcConfig struct {
	RetentionDays   int
	MaxEntries      int
	IntervalSeconds int
}

const (
	defaultGcIntervalSeconds = 3600
	minGcIntervalSeconds     = 60
)

// DefaultGcConfig returns the policy default: gc disabled, scan period 1 hour.
//
// Zero RetentionDays + zero MaxEntries means gc is OFF until the user opts in
// via ~/.config/rnix/config.yaml (AC#8: 全零 = 关闭).
func DefaultGcConfig() GcConfig {
	return GcConfig{
		RetentionDays:   0,
		MaxEntries:      0,
		IntervalSeconds: defaultGcIntervalSeconds,
	}
}

// normalizeGcConfig clamps user-supplied values to safe defaults.
//
//   - Negative RetentionDays / MaxEntries → 0 (disable that rule, do not abort).
//   - IntervalSeconds == 0 → defaultGcIntervalSeconds.
//   - IntervalSeconds in (0, minGcIntervalSeconds) → minGcIntervalSeconds
//     (防止过频扫描)
func normalizeGcConfig(cfg GcConfig) GcConfig {
	if cfg.RetentionDays < 0 {
		cfg.RetentionDays = 0
	}
	if cfg.MaxEntries < 0 {
		cfg.MaxEntries = 0
	}
	if cfg.IntervalSeconds == 0 {
		cfg.IntervalSeconds = defaultGcIntervalSeconds
	} else if cfg.IntervalSeconds < minGcIntervalSeconds {
		cfg.IntervalSeconds = minGcIntervalSeconds
	}
	return cfg
}

// SetGcConfig updates the kernel-wide gc policy (Story 42.5 AC#7).
// Zero or negative values are normalized; the returned GcCfg() reflects the
// post-normalization values.
func (k *KernelImpl) SetGcConfig(cfg GcConfig) {
	k.gcCfg = normalizeGcConfig(cfg)
}

// GcCfg returns the active gc policy. If never set (zero-value KernelImpl),
// returns DefaultGcConfig() which is "gc disabled, scan every 3600s" — safe
// degradation matching AC#8.
func (k *KernelImpl) GcCfg() GcConfig {
	if k.gcCfg.IntervalSeconds == 0 {
		return DefaultGcConfig()
	}
	return k.gcCfg
}
