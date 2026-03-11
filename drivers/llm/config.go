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
	ProvidersConfigFile = "rnix-providers.yaml"
)

var validDrivers = map[string]bool{
	DriverClaudeCLI:    true,
	DriverCursorCLI:    true,
	DriverOpenAICompat: true,
}

var nameRegexp = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

type ProvidersConfig struct {
	Version   string           `yaml:"version"`
	Providers []ProviderConfig `yaml:"providers"`
}

type ProviderConfig struct {
	Name         string `yaml:"name"`
	Driver       string `yaml:"driver"`
	DefaultModel string `yaml:"default_model"`
	BaseURL      string `yaml:"base_url"`
	APIKeyEnv    string `yaml:"api_key_env"`
}

// FindProvidersConfigPath searches for rnix-providers.yaml in CWD then
// $XDG_CONFIG_HOME/rnix/. Returns the first path found, or "" if none exist.
func FindProvidersConfigPath() string {
	cwdPath := filepath.Join(".", ProvidersConfigFile)
	if abs, err := filepath.Abs(cwdPath); err == nil {
		if _, err := os.Stat(abs); err == nil {
			return abs
		}
	}

	xdgConfig := os.Getenv("XDG_CONFIG_HOME")
	if xdgConfig == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		xdgConfig = filepath.Join(home, ".config")
	}
	xdgPath := filepath.Join(xdgConfig, "rnix", ProvidersConfigFile)
	if _, err := os.Stat(xdgPath); err == nil {
		return xdgPath
	}

	return ""
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
			errs = append(errs, fmt.Errorf("provider[%d] %q: invalid driver %q (valid: %s, %s, %s)", i, p.Name, p.Driver, DriverClaudeCLI, DriverCursorCLI, DriverOpenAICompat))
		}

		if p.Driver == DriverOpenAICompat && p.BaseURL == "" {
			errs = append(errs, fmt.Errorf("provider[%d] %q: base_url is required for driver %s", i, p.Name, DriverOpenAICompat))
		}
	}

	return errors.Join(errs...)
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
		log.Println("no rnix-providers.yaml found, using default provider configuration")
		return DefaultProvidersConfig(), nil
	}
	return LoadProvidersConfig(path)
}
