package llm

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"

	"github.com/goccy/go-yaml"
)

const (
	DriverClaudeCLI     = "claude-cli"
	DriverCursorCLI     = "cursor-cli"
	DriverOpenAICompat  = "openai-compat"
	DriverOpenAI        = "openai"
	DriverGemini        = "gemini"
	ProvidersConfigFile = "providers.yaml"
)

const (
	ModeStream = "stream"
	ModeCall   = "call"
)

var validDrivers = map[string]bool{
	DriverClaudeCLI:    true,
	DriverCursorCLI:    true,
	DriverOpenAICompat: true,
	DriverOpenAI:       true,
	DriverGemini:       true,
}

var validModes = map[string]bool{
	"":         true, // default = stream
	ModeStream: true,
	ModeCall:   true,
}

var nameRegexp = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

type ProvidersConfig struct {
	Version         string           `yaml:"version"`
	DefaultProvider string           `yaml:"default_provider,omitempty"`
	Providers       []ProviderConfig `yaml:"providers"`
}

type ProviderConfig struct {
	Name           string   `yaml:"name"`
	Driver         string   `yaml:"driver"`
	Command        string   `yaml:"command"` // CLI binary name override (e.g., "agent" for cursor-cli)
	DefaultModel   string   `yaml:"default_model"`
	BaseURL        string   `yaml:"base_url"`
	APIKeyEnv      string   `yaml:"api_key_env"`
	Mode           string   `yaml:"mode"`            // "stream" (default) or "call"
	MaxTokens      int      `yaml:"max_tokens"`      // default max output tokens; 0 = use API default
	CostPerToken   float64  `yaml:"cost_per_token"`  // cost per token in USD; 0 = cost tracking disabled
	ThinkingBudget int      `yaml:"thinking_budget"` // thinking budget tokens (gemini driver only; 0 = disabled)
	ExtraArgs      []string `yaml:"extra_args"`      // additional CLI arguments (claude-cli/cursor-cli only)
}

// FindProvidersConfigPath searches for providers.yaml in CWD then
// $XDG_CONFIG_HOME/rnix/. Falls back to legacy rnix-providers.yaml for
// backward compatibility. Returns the first path found, or "" if none exist.
//
// Deprecated: Use config.ResolvePath("providers.yaml") from internal/config
// for proper global/project resolution. This function does not support
// project-level configuration (Story 25.3).
func FindProvidersConfigPath() string {
	// Search order: CWD → $XDG_CONFIG_HOME/rnix/
	// For each location, try new name first, then legacy name.
	locations := func(dir string) string {
		// New name: providers.yaml
		newPath := filepath.Join(dir, ProvidersConfigFile)
		if _, err := os.Stat(newPath); err == nil {
			return newPath
		}
		// Legacy name: rnix-providers.yaml
		legacyPath := filepath.Join(dir, "rnix-providers.yaml")
		if _, err := os.Stat(legacyPath); err == nil {
			return legacyPath
		}
		return ""
	}

	// 1. Check CWD
	if p := locations("."); p != "" {
		if abs, err := filepath.Abs(p); err == nil {
			return abs
		}
		return p
	}

	// 2. Check $XDG_CONFIG_HOME/rnix/
	xdgConfig := os.Getenv("XDG_CONFIG_HOME")
	if xdgConfig == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		xdgConfig = filepath.Join(home, ".config")
	}
	return locations(filepath.Join(xdgConfig, "rnix"))
}

// LoadProvidersConfig reads and validates a providers YAML file at the given path.
func LoadProvidersConfig(path string) (*ProvidersConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading providers config: %w", err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("providers config file is empty: %s", path)
	}

	return ParseProvidersConfig(data)
}

// ParseProvidersConfig parses and validates providers config from raw YAML bytes.
func ParseProvidersConfig(data []byte) (*ProvidersConfig, error) {
	var cfg ProvidersConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing providers config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Validate checks that the ProvidersConfig is well-formed. All validation
// errors are collected and returned together via errors.Join.
func (c *ProvidersConfig) Validate() error {
	var errs []error

	if len(c.Providers) == 0 {
		return errors.New("providers list is empty: at least one provider is required")
	}

	seen := make(map[string]bool)
	for i, p := range c.Providers {
		if p.Name == "" {
			errs = append(errs, fmt.Errorf("provider[%d]: name is required", i))
		} else {
			if !nameRegexp.MatchString(p.Name) {
				errs = append(errs, fmt.Errorf("provider[%d]: name %q contains invalid characters (must match %s)", i, p.Name, nameRegexp.String()))
			}
			if seen[p.Name] {
				errs = append(errs, fmt.Errorf("provider[%d]: duplicate provider name %q", i, p.Name))
			}
			seen[p.Name] = true
		}

		if !validDrivers[p.Driver] {
			errs = append(errs, fmt.Errorf("provider[%d] %q: invalid driver %q (valid: %s, %s, %s, %s, %s)", i, p.Name, p.Driver, DriverClaudeCLI, DriverCursorCLI, DriverOpenAICompat, DriverOpenAI, DriverGemini))
		}

		if p.Driver == DriverOpenAICompat && p.BaseURL == "" {
			errs = append(errs, fmt.Errorf("provider[%d] %q: base_url is required for driver %s", i, p.Name, DriverOpenAICompat))
		}

		if !validModes[p.Mode] {
			errs = append(errs, fmt.Errorf("provider[%d] %q: invalid mode %q (valid: stream, call)", i, p.Name, p.Mode))
		}
	}

	// Validate default_provider references an existing provider name.
	if c.DefaultProvider != "" && !seen[c.DefaultProvider] {
		errs = append(errs, fmt.Errorf("default_provider %q not found in providers list", c.DefaultProvider))
	}

	return errors.Join(errs...)
}

// ResolveDefaultProvider returns the effective default provider name.
// Priority: explicit DefaultProvider > first provider in list > "claude" (ultimate fallback).
func (c *ProvidersConfig) ResolveDefaultProvider() string {
	if c.DefaultProvider != "" {
		return c.DefaultProvider
	}
	if len(c.Providers) > 0 {
		return c.Providers[0].Name
	}
	return "claude"
}

// DefaultProvidersConfig returns a built-in config with claude-cli and cursor-cli providers.
func DefaultProvidersConfig() *ProvidersConfig {
	return &ProvidersConfig{
		Version: "1",
		Providers: []ProviderConfig{
			{Name: "claude", Driver: DriverClaudeCLI, DefaultModel: "haiku"},
			{Name: "cursor", Driver: DriverCursorCLI},
		},
	}
}

// LoadOrDefaultProvidersConfig loads the providers config from disk if found,
// otherwise returns the built-in default configuration.
func LoadOrDefaultProvidersConfig() (*ProvidersConfig, error) {
	path := FindProvidersConfigPath()
	if path == "" {
		log.Println("no providers.yaml found, using default provider configuration")
		return DefaultProvidersConfig(), nil
	}
	return LoadProvidersConfig(path)
}
