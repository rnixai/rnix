package web

import "fmt"

// DefaultMaxResults is the default page size when a backend does not specify one.
const DefaultMaxResults = 5

// Backend driver type identifiers used in BackendConfig.Driver.
const (
	BackendDriverTavily  = "tavily"
	BackendDriverExa     = "exa"
	BackendDriverSearxng = "searxng"
)

// WebSearchConfig is the top-level schema for web-search.yaml.
//
// Loaded from <projectDir>/.rnix/web-search.yaml (project) or
// <globalDir>/web-search.yaml (global). Project config fully overrides global
// when present (no per-key merging — keeps the rule simple).
type WebSearchConfig struct {
	Version        string          `yaml:"version"`
	DefaultBackend string          `yaml:"default_backend"`
	Backends       []BackendConfig `yaml:"backends"`
}

// BackendConfig describes one search backend entry.
type BackendConfig struct {
	Name        string `yaml:"name"`
	Driver      string `yaml:"driver"`
	APIKeyEnv   string `yaml:"api_key_env"`
	BaseURL     string `yaml:"base_url"`
	MaxResults  int    `yaml:"max_results"`
	SearchDepth string `yaml:"search_depth"`
	NumResults  int    `yaml:"num_results"`
}

// Validate checks the high-level schema invariants. Per-backend validation
// (e.g. missing API key) is deferred to BuildRegistryFromConfig so individual
// failures only skip one backend instead of failing the whole daemon.
func (c *WebSearchConfig) Validate() error {
	if c == nil {
		return nil
	}
	if len(c.Backends) == 0 {
		return fmt.Errorf("web-search.yaml: backends list is empty")
	}
	seen := make(map[string]struct{}, len(c.Backends))
	for i, b := range c.Backends {
		if b.Name == "" {
			return fmt.Errorf("web-search.yaml: backends[%d].name is required", i)
		}
		if b.Driver == "" {
			return fmt.Errorf("web-search.yaml: backends[%d].driver is required (name=%q)", i, b.Name)
		}
		switch b.Driver {
		case BackendDriverTavily, BackendDriverExa, BackendDriverSearxng:
		default:
			return fmt.Errorf("web-search.yaml: backends[%d]: unknown driver %q (name=%q)", i, b.Driver, b.Name)
		}
		if _, dup := seen[b.Name]; dup {
			return fmt.Errorf("web-search.yaml: duplicate backend name %q", b.Name)
		}
		seen[b.Name] = struct{}{}
	}
	if c.DefaultBackend != "" {
		if _, ok := seen[c.DefaultBackend]; !ok {
			return fmt.Errorf("web-search.yaml: default_backend %q not in backends list", c.DefaultBackend)
		}
	}
	return nil
}
