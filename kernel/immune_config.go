package kernel

import "fmt"

// ImmuneConfig holds configuration for the immune system.
// Parsed from the "immune" section of .rnix/config.yaml.
type ImmuneConfig struct {
	Enabled                bool    `json:"enabled" yaml:"enabled"`
	WarnOnly               bool    `json:"warn_only" yaml:"warn_only"`
	DeviationThreshold     float64 `json:"deviation_threshold" yaml:"deviation_threshold"`
	MinSamples             int     `json:"min_samples" yaml:"min_samples"`
	ReinforcementThreshold int     `json:"reinforcement_threshold" yaml:"reinforcement_threshold"`
	MinMigrationSimilarity float64 `json:"min_migration_similarity" yaml:"min_migration_similarity"`
	ThreatTTLDays          int     `json:"threat_ttl_days" yaml:"threat_ttl_days"`
	MaxThreats             int     `json:"max_threats" yaml:"max_threats"`
}

// DefaultThreatTTLDays is the default expiry for threat signatures (IN-3 F3).
// Signatures older than this are dropped at daemon startup. 0 disables expiry.
const DefaultThreatTTLDays = 30

// DefaultMaxThreats caps the per-project threat memory size (IN-3 F3).
// When exceeded, the oldest signatures are evicted. 0 disables the cap.
const DefaultMaxThreats = 500

// DefaultImmuneConfig returns the default immune configuration.
// Enabled=true and WarnOnly=true: immune system is on by default in observation mode.
func DefaultImmuneConfig() ImmuneConfig {
	return ImmuneConfig{
		Enabled:                true,
		WarnOnly:               true,
		DeviationThreshold:     DefaultDeviationThreshold,
		MinSamples:             MinSamplesForProfile,
		ReinforcementThreshold: DefaultReinforcementThreshold,
		MinMigrationSimilarity: MinMigrationSimilarity,
		ThreatTTLDays:          DefaultThreatTTLDays,
		MaxThreats:             DefaultMaxThreats,
	}
}

// ParseImmuneConfig parses an ImmuneConfig from a generic map (from YAML).
// Fields not present in data retain their values from base.
// Invalid values fall back to the base value and produce warnings.
func ParseImmuneConfig(data map[string]any, base ...ImmuneConfig) (ImmuneConfig, []string) {
	cfg := DefaultImmuneConfig()
	if len(base) > 0 {
		cfg = base[0]
	}
	if data == nil {
		return cfg, nil
	}

	var warnings []string

	if v, ok := data["enabled"]; ok {
		if b, ok := v.(bool); ok {
			cfg.Enabled = b
		}
	}

	if v, ok := data["warn_only"]; ok {
		if b, ok := v.(bool); ok {
			cfg.WarnOnly = b
		}
	}

	if v, ok := data["deviation_threshold"]; ok {
		if f, parsed := toFloat64(v); parsed {
			if f <= 0 {
				warnings = append(warnings, fmt.Sprintf("immune: deviation_threshold %.1f invalid (must be >0), using default %.1f", f, DefaultDeviationThreshold))
			} else {
				cfg.DeviationThreshold = f
			}
		}
	}

	if v, ok := data["min_samples"]; ok {
		if n, parsed := toInt(v); parsed {
			if n < 2 {
				warnings = append(warnings, fmt.Sprintf("immune: min_samples %d invalid (must be >=2), using default %d", n, MinSamplesForProfile))
			} else {
				cfg.MinSamples = n
			}
		}
	}

	if v, ok := data["reinforcement_threshold"]; ok {
		if n, parsed := toInt(v); parsed {
			if n < 1 {
				warnings = append(warnings, fmt.Sprintf("immune: reinforcement_threshold %d invalid (must be >=1), using default %d", n, DefaultReinforcementThreshold))
			} else {
				cfg.ReinforcementThreshold = n
			}
		}
	}

	if v, ok := data["min_migration_similarity"]; ok {
		if f, parsed := toFloat64(v); parsed {
			if f < 0 || f > 1 {
				warnings = append(warnings, fmt.Sprintf("immune: min_migration_similarity %.2f invalid (must be 0~1), using default %.2f", f, MinMigrationSimilarity))
			} else {
				cfg.MinMigrationSimilarity = f
			}
		}
	}

	if v, ok := data["threat_ttl_days"]; ok {
		if n, parsed := toInt(v); parsed {
			if n < 0 {
				warnings = append(warnings, fmt.Sprintf("immune: threat_ttl_days %d invalid (must be >=0, 0=never expire), using default %d", n, DefaultThreatTTLDays))
			} else {
				cfg.ThreatTTLDays = n
			}
		}
	}

	if v, ok := data["max_threats"]; ok {
		if n, parsed := toInt(v); parsed {
			if n < 0 {
				warnings = append(warnings, fmt.Sprintf("immune: max_threats %d invalid (must be >=0, 0=no cap), using default %d", n, DefaultMaxThreats))
			} else {
				cfg.MaxThreats = n
			}
		}
	}

	return cfg, warnings
}

// toFloat64 attempts to convert a YAML-parsed value to float64.
func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint64:
		return float64(n), true
	default:
		return 0, false
	}
}

// toInt attempts to convert a YAML-parsed value to int.
func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case uint64:
		return int(n), true
	case float64:
		return int(n), true
	case float32:
		return int(n), true
	default:
		return 0, false
	}
}
