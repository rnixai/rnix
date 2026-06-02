package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/goccy/go-yaml"
)

// FeatureProfile identifies a named feature preset or "custom" for per-flag control.
type FeatureProfile string

const (
	ProfileBaseline FeatureProfile = "baseline"
	ProfileCore     FeatureProfile = "core"
	ProfileAdaptive FeatureProfile = "adaptive"
	ProfileFull     FeatureProfile = "full"
	ProfileCustom   FeatureProfile = "custom"
)

// FeatureFlags controls which emergent subsystems are active.
type FeatureFlags struct {
	Planning      bool `yaml:"planning"`
	Replan        bool `yaml:"replan"`
	Specialize    bool `yaml:"specialize"`
	DiscoverSkill bool `yaml:"discover_skill"`
	Spawn         bool `yaml:"spawn"`
	DiffMemory    bool `yaml:"diff_memory"`
	StemMatcher   bool `yaml:"stem_matcher"`
	Immune        bool `yaml:"immune"`
	Compaction    bool `yaml:"compaction"`
}

func (f FeatureFlags) String() string {
	return fmt.Sprintf("features[planning=%t replan=%t specialize=%t discover_skill=%t spawn=%t diff_memory=%t stem_matcher=%t immune=%t compaction=%t]",
		f.Planning, f.Replan, f.Specialize, f.DiscoverSkill, f.Spawn,
		f.DiffMemory, f.StemMatcher, f.Immune, f.Compaction)
}

var profilePresets = map[FeatureProfile]FeatureFlags{
	ProfileBaseline: {},
	ProfileCore: {
		Planning:   true,
		Spawn:      true,
		Compaction: true,
	},
	ProfileAdaptive: {
		Planning:      true,
		Replan:        true,
		Specialize:    true,
		DiscoverSkill: true,
		Spawn:         true,
		DiffMemory:    true,
		StemMatcher:   true,
		Compaction:    true,
	},
	ProfileFull: {
		Planning:      true,
		Replan:        true,
		Specialize:    true,
		DiscoverSkill: true,
		Spawn:         true,
		DiffMemory:    true,
		StemMatcher:   true,
		Immune:        true,
		Compaction:    true,
	},
}

var validProfiles = map[FeatureProfile]bool{
	ProfileBaseline: true,
	ProfileCore:     true,
	ProfileAdaptive: true,
	ProfileFull:     true,
	ProfileCustom:   true,
}

type featuresConfig struct {
	Profile string          `yaml:"profile"`
	Custom  map[string]bool `yaml:"custom"`
}

type configFile struct {
	Features featuresConfig `yaml:"features"`
}

// ResolveFeatures reads the features section from configPath, applies
// RNIX_FEATURE_PROFILE env override, and expands into FeatureFlags.
// Returns the resolved flags, the effective profile name, and any warnings.
// Missing/malformed config files silently default to ProfileFull.
func ResolveFeatures(configPath string) (FeatureFlags, FeatureProfile, []string) {
	var warnings []string
	var cfg configFile

	data, err := os.ReadFile(configPath)
	if err != nil {
		return profilePresets[ProfileFull], ProfileFull, warnings
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		warnings = append(warnings, fmt.Sprintf("features: failed to parse %s: %v", configPath, err))
		return profilePresets[ProfileFull], ProfileFull, warnings
	}

	profileStr := strings.TrimSpace(cfg.Features.Profile)
	if profileStr == "" {
		profileStr = string(ProfileFull)
	}

	if env := os.Getenv("RNIX_FEATURE_PROFILE"); env != "" {
		profileStr = env
	}

	profile := FeatureProfile(profileStr)

	if !validProfiles[profile] {
		warnings = append(warnings, fmt.Sprintf("features: invalid profile %q, falling back to full", profileStr))
		return profilePresets[ProfileFull], ProfileFull, warnings
	}

	if profile != ProfileCustom {
		return profilePresets[profile], profile, warnings
	}

	// custom: start with all true, override with explicit values
	flags := profilePresets[ProfileFull]
	for key, val := range cfg.Features.Custom {
		switch key {
		case "planning":
			flags.Planning = val
		case "replan":
			flags.Replan = val
		case "specialize":
			flags.Specialize = val
		case "discover_skill":
			flags.DiscoverSkill = val
		case "spawn":
			flags.Spawn = val
		case "diff_memory":
			flags.DiffMemory = val
		case "stem_matcher":
			flags.StemMatcher = val
		case "immune":
			flags.Immune = val
		case "compaction":
			flags.Compaction = val
		}
	}

	return flags, ProfileCustom, warnings
}
