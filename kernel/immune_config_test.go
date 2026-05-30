package kernel

import (
	"testing"
)

func TestDefaultImmuneConfig(t *testing.T) {
	cfg := DefaultImmuneConfig()

	if !cfg.Enabled {
		t.Error("default config should have Enabled=true")
	}
	if !cfg.WarnOnly {
		t.Error("default config should have WarnOnly=true")
	}
	if cfg.DeviationThreshold != DefaultDeviationThreshold {
		t.Errorf("DeviationThreshold = %f, want %f", cfg.DeviationThreshold, DefaultDeviationThreshold)
	}
	if cfg.MinSamples != MinSamplesForProfile {
		t.Errorf("MinSamples = %d, want %d", cfg.MinSamples, MinSamplesForProfile)
	}
	if cfg.ReinforcementThreshold != DefaultReinforcementThreshold {
		t.Errorf("ReinforcementThreshold = %d, want %d", cfg.ReinforcementThreshold, DefaultReinforcementThreshold)
	}
	if cfg.MinMigrationSimilarity != MinMigrationSimilarity {
		t.Errorf("MinMigrationSimilarity = %f, want %f", cfg.MinMigrationSimilarity, MinMigrationSimilarity)
	}
}

func TestParseImmuneConfig_NilData(t *testing.T) {
	cfg, warnings := ParseImmuneConfig(nil)
	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}
	if !cfg.Enabled {
		t.Error("nil data should produce Enabled=true")
	}
	if !cfg.WarnOnly {
		t.Error("nil data should produce WarnOnly=true")
	}
}

func TestParseImmuneConfig_EmptyMap(t *testing.T) {
	cfg, warnings := ParseImmuneConfig(map[string]any{})
	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}
	if !cfg.Enabled {
		t.Error("empty map should produce Enabled=true")
	}
	if !cfg.WarnOnly {
		t.Error("empty map should produce WarnOnly=true")
	}
}

func TestParseImmuneConfig_Enabled(t *testing.T) {
	data := map[string]any{
		"enabled": true,
	}
	cfg, warnings := ParseImmuneConfig(data)
	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}
	if !cfg.Enabled {
		t.Error("expected Enabled=true")
	}
}

func TestParseImmuneConfig_AllFields(t *testing.T) {
	data := map[string]any{
		"enabled":                 true,
		"deviation_threshold":     5.0,
		"min_samples":             10,
		"reinforcement_threshold": 8,
		"min_migration_similarity": 0.5,
	}
	cfg, warnings := ParseImmuneConfig(data)
	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}
	if !cfg.Enabled {
		t.Error("expected Enabled=true")
	}
	if cfg.DeviationThreshold != 5.0 {
		t.Errorf("DeviationThreshold = %f, want 5.0", cfg.DeviationThreshold)
	}
	if cfg.MinSamples != 10 {
		t.Errorf("MinSamples = %d, want 10", cfg.MinSamples)
	}
	if cfg.ReinforcementThreshold != 8 {
		t.Errorf("ReinforcementThreshold = %d, want 8", cfg.ReinforcementThreshold)
	}
	if cfg.MinMigrationSimilarity != 0.5 {
		t.Errorf("MinMigrationSimilarity = %f, want 0.5", cfg.MinMigrationSimilarity)
	}
}

func TestParseImmuneConfig_InvalidDeviationThreshold(t *testing.T) {
	data := map[string]any{
		"deviation_threshold": -1.0,
	}
	cfg, warnings := ParseImmuneConfig(data)
	if len(warnings) != 1 {
		t.Errorf("expected 1 warning, got %d: %v", len(warnings), warnings)
	}
	if cfg.DeviationThreshold != DefaultDeviationThreshold {
		t.Errorf("should fall back to default, got %f", cfg.DeviationThreshold)
	}
}

func TestParseImmuneConfig_InvalidMinSamples(t *testing.T) {
	for _, val := range []int{0, 1, -1} {
		data := map[string]any{
			"min_samples": val,
		}
		cfg, warnings := ParseImmuneConfig(data)
		if len(warnings) != 1 {
			t.Errorf("min_samples=%d: expected 1 warning, got %d: %v", val, len(warnings), warnings)
		}
		if cfg.MinSamples != MinSamplesForProfile {
			t.Errorf("min_samples=%d: should fall back to default, got %d", val, cfg.MinSamples)
		}
	}
}

func TestParseImmuneConfig_InvalidReinforcementThreshold(t *testing.T) {
	data := map[string]any{
		"reinforcement_threshold": -5,
	}
	cfg, warnings := ParseImmuneConfig(data)
	if len(warnings) != 1 {
		t.Errorf("expected 1 warning, got %d: %v", len(warnings), warnings)
	}
	if cfg.ReinforcementThreshold != DefaultReinforcementThreshold {
		t.Errorf("should fall back to default, got %d", cfg.ReinforcementThreshold)
	}
}

func TestParseImmuneConfig_InvalidMigrationSimilarity(t *testing.T) {
	tests := []struct {
		name  string
		value float64
	}{
		{"negative", -0.1},
		{"over_one", 1.5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := map[string]any{
				"min_migration_similarity": tt.value,
			}
			cfg, warnings := ParseImmuneConfig(data)
			if len(warnings) != 1 {
				t.Errorf("expected 1 warning, got %d: %v", len(warnings), warnings)
			}
			if cfg.MinMigrationSimilarity != MinMigrationSimilarity {
				t.Errorf("should fall back to default, got %f", cfg.MinMigrationSimilarity)
			}
		})
	}
}

func TestParseImmuneConfig_IntegerThreshold(t *testing.T) {
	// YAML often parses whole numbers as int, not float64
	data := map[string]any{
		"deviation_threshold": 5,
	}
	cfg, warnings := ParseImmuneConfig(data)
	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}
	if cfg.DeviationThreshold != 5.0 {
		t.Errorf("DeviationThreshold = %f, want 5.0", cfg.DeviationThreshold)
	}
}

func TestParseImmuneConfig_ZeroDeviationThreshold(t *testing.T) {
	data := map[string]any{
		"deviation_threshold": 0.0,
	}
	cfg, warnings := ParseImmuneConfig(data)
	if len(warnings) != 1 {
		t.Errorf("expected 1 warning for zero threshold, got %d: %v", len(warnings), warnings)
	}
	if cfg.DeviationThreshold != DefaultDeviationThreshold {
		t.Errorf("should fall back to default, got %f", cfg.DeviationThreshold)
	}
}

func TestParseImmuneConfig_WarnOnly(t *testing.T) {
	t.Run("explicit_false", func(t *testing.T) {
		data := map[string]any{
			"warn_only": false,
		}
		cfg, warnings := ParseImmuneConfig(data)
		if len(warnings) != 0 {
			t.Errorf("expected no warnings, got %v", warnings)
		}
		if cfg.WarnOnly {
			t.Error("expected WarnOnly=false when explicitly set")
		}
	})

	t.Run("explicit_true", func(t *testing.T) {
		data := map[string]any{
			"warn_only": true,
		}
		cfg, warnings := ParseImmuneConfig(data)
		if len(warnings) != 0 {
			t.Errorf("expected no warnings, got %v", warnings)
		}
		if !cfg.WarnOnly {
			t.Error("expected WarnOnly=true when explicitly set")
		}
	})

	t.Run("not_specified_retains_base", func(t *testing.T) {
		base := DefaultImmuneConfig()
		base.WarnOnly = false
		data := map[string]any{
			"enabled": true,
		}
		cfg, warnings := ParseImmuneConfig(data, base)
		if len(warnings) != 0 {
			t.Errorf("expected no warnings, got %v", warnings)
		}
		if cfg.WarnOnly {
			t.Error("expected WarnOnly=false retained from base")
		}
	})
}

func TestParseImmuneConfig_MergeWithBase(t *testing.T) {
	// Global config sets threshold and enables
	globalData := map[string]any{
		"enabled":             true,
		"deviation_threshold": 5.0,
		"min_samples":         10,
	}
	globalCfg, _ := ParseImmuneConfig(globalData)

	// Project config only overrides min_samples
	projectData := map[string]any{
		"min_samples": 20,
	}
	merged, warnings := ParseImmuneConfig(projectData, globalCfg)
	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}
	// Should keep global's enabled and deviation_threshold
	if !merged.Enabled {
		t.Error("expected Enabled=true from base")
	}
	if merged.DeviationThreshold != 5.0 {
		t.Errorf("DeviationThreshold = %f, want 5.0 from base", merged.DeviationThreshold)
	}
	// Should use project's min_samples
	if merged.MinSamples != 20 {
		t.Errorf("MinSamples = %d, want 20 from override", merged.MinSamples)
	}
}
