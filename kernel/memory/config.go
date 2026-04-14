package memory

import (
	"github.com/goccy/go-yaml"
)

// MemoryConfig holds all memory subsystem configuration.
type MemoryConfig struct {
	Enabled     bool            `yaml:"enabled"`
	Store       StoreConfig     `yaml:"store"`
	Writeback   WritebackConfig `yaml:"writeback"`
	Recall      RecallConfig    `yaml:"recall"`
	UserProfile ProfileConfig   `yaml:"user_profile"`
	Inject      InjectConfig    `yaml:"inject"`
}

// StoreConfig controls the bounded file storage engine.
type StoreConfig struct {
	MemoryCharLimit int    `yaml:"memory_char_limit"`
	UserCharLimit   int    `yaml:"user_char_limit"`
	OverflowPolicy  string `yaml:"overflow_policy"`
}

// WritebackConfig controls async knowledge extraction after process completion.
type WritebackConfig struct {
	Enabled          bool   `yaml:"enabled"`
	Model            string `yaml:"model"`
	TriggerThreshold int    `yaml:"trigger_threshold"`
	RequireSuccess   bool   `yaml:"require_success"`
}

// RecallConfig controls cross-process search.
type RecallConfig struct {
	Enabled    bool `yaml:"enabled"`
	MaxResults int  `yaml:"max_results"`
}

// ProfileConfig controls user profile management.
type ProfileConfig struct {
	Enabled bool `yaml:"enabled"`
}

// InjectConfig controls memory injection into system prompts.
type InjectConfig struct {
	Order string `yaml:"order"`
}

// DefaultMemoryConfig returns the default configuration with all subsystems enabled.
func DefaultMemoryConfig() MemoryConfig {
	return MemoryConfig{
		Enabled: true,
		Store: StoreConfig{
			MemoryCharLimit: 4096,
			UserCharLimit:   2048,
			OverflowPolicy:  "error",
		},
		Writeback: WritebackConfig{
			Enabled:          true,
			Model:            "",
			TriggerThreshold: 5,
			RequireSuccess:   true,
		},
		Recall: RecallConfig{
			Enabled:    true,
			MaxResults: 20,
		},
		UserProfile: ProfileConfig{
			Enabled: true,
		},
		Inject: InjectConfig{
			Order: "global_first",
		},
	}
}

// ParseMemoryConfig parses YAML bytes into a MemoryConfig.
// Missing fields retain their zero values; callers should merge with DefaultMemoryConfig
// if they want defaults for unset fields.
func ParseMemoryConfig(data []byte) (MemoryConfig, error) {
	var cfg MemoryConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return MemoryConfig{}, err
	}
	return cfg, nil
}
