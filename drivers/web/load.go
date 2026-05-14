package web

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/goccy/go-yaml"
)

const (
	// WebSearchConfigFile is the filename for the web-search config in both
	// global (~/.config/rnix/) and project (.rnix/) locations.
	WebSearchConfigFile = "web-search.yaml"

	// EnvTavilyAPIKey, EnvExaAPIKey, EnvSearxngURL are the auto-detect env vars.
	EnvTavilyAPIKey = "TAVILY_API_KEY"
	EnvExaAPIKey    = "EXA_API_KEY"
	EnvSearxngURL   = "RNIX_SEARCH_URL"
)

// BuildOpts holds shared dependencies for backend construction.
//
// HTTPClient: injected for testability — backend implementations MUST NOT
// reach for http.DefaultClient.
//
// EnvLookup: indirection layer for environment variables; resolves API keys
// from project-level .env (via EnvSnapshot) before falling back to os.Getenv.
// Mirrors drivers/llm/factory.go's CreateDriverWithEnv pattern.
type BuildOpts struct {
	HTTPClient HTTPClient
	EnvLookup  func(string) string
}

// LoadWebSearchConfig reads web-search.yaml from the project directory first,
// then falls back to the global directory. Returns (nil, nil) when neither
// file exists so callers can degrade to BuildRegistryAuto.
//
// projectDir and globalDir may be empty strings; empty paths are skipped.
func LoadWebSearchConfig(globalDir, projectDir string) (*WebSearchConfig, error) {
	candidates := make([]string, 0, 2)
	if projectDir != "" {
		candidates = append(candidates, filepath.Join(projectDir, ".rnix", WebSearchConfigFile))
	}
	if globalDir != "" {
		candidates = append(candidates, filepath.Join(globalDir, WebSearchConfigFile))
	}

	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("reading %s: %w", path, err)
		}
		var cfg WebSearchConfig
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, err)
		}
		if err := cfg.Validate(); err != nil {
			return nil, fmt.Errorf("validating %s: %w", path, err)
		}
		return &cfg, nil
	}
	return nil, nil
}

// BuildRegistryFromConfig translates a WebSearchConfig into a populated
// SearchBackendRegistry. Backends whose API key is missing or whose required
// fields are empty are skipped with a single warning log line instead of
// failing the whole construction — this lets daemon startup succeed even when
// only a subset of backends is fully configured.
func BuildRegistryFromConfig(cfg *WebSearchConfig, opts BuildOpts) *SearchBackendRegistry {
	registry := NewSearchBackendRegistry()
	if cfg == nil {
		return registry
	}
	envLookup := opts.EnvLookup
	if envLookup == nil {
		envLookup = os.Getenv
	}
	client := opts.HTTPClient
	if client == nil {
		client = defaultHTTPClient()
	}

	for _, bc := range cfg.Backends {
		backend, err := buildBackend(bc, client, envLookup)
		if err != nil {
			log.Printf("[web] backend %s skipped: %v", bc.Name, err)
			continue
		}
		registry.Register(bc.Name, backend)
	}

	if registry.Len() == 0 && len(cfg.Backends) > 0 {
		log.Printf("[web] warning: web-search.yaml loaded but all %d backends were skipped (missing API keys/URLs)", len(cfg.Backends))
	}

	desired := cfg.DefaultBackend
	if desired != "" {
		if _, ok := registry.Get(desired); !ok && registry.Len() > 0 {
			fallback := registry.Names()[0]
			log.Printf("[web] warning: default_backend %q not available, falling back to %q", desired, fallback)
			desired = fallback
		}
	}
	if desired == "" {
		if names := registry.Names(); len(names) > 0 {
			desired = names[0]
		}
	}
	if desired != "" {
		if _, ok := registry.Get(desired); ok {
			_ = registry.SetDefault(desired)
		}
	}
	return registry
}

// buildBackend constructs one backend from a BackendConfig, returning an
// explanatory error if the config is incomplete (missing key, missing URL).
func buildBackend(bc BackendConfig, client HTTPClient, envLookup func(string) string) (SearchBackend, error) {
	switch bc.Driver {
	case BackendDriverTavily:
		key := resolveAPIKey(bc.APIKeyEnv, EnvTavilyAPIKey, envLookup)
		if key == "" {
			return nil, fmt.Errorf("missing API key (set %s)", firstNonEmpty(bc.APIKeyEnv, EnvTavilyAPIKey))
		}
		maxResults := bc.MaxResults
		if maxResults == 0 {
			maxResults = DefaultMaxResults
		}
		return NewTavilyBackend(client, key, maxResults, bc.SearchDepth), nil

	case BackendDriverExa:
		key := resolveAPIKey(bc.APIKeyEnv, EnvExaAPIKey, envLookup)
		if key == "" {
			return nil, fmt.Errorf("missing API key (set %s)", firstNonEmpty(bc.APIKeyEnv, EnvExaAPIKey))
		}
		numResults := bc.NumResults
		if numResults == 0 {
			numResults = bc.MaxResults
		}
		if numResults == 0 {
			numResults = DefaultMaxResults
		}
		return NewExaBackend(client, key, numResults), nil

	case BackendDriverSearxng:
		base := bc.BaseURL
		if base == "" {
			base = envLookup(EnvSearxngURL)
		}
		if base == "" {
			return nil, fmt.Errorf("missing base_url (set %s)", EnvSearxngURL)
		}
		return NewSearxngBackend(client, base), nil

	default:
		return nil, fmt.Errorf("unsupported driver %q", bc.Driver)
	}
}

// BuildRegistryAuto implements the zero-config convenience path: scan the
// environment in priority order TAVILY > EXA > SEARXNG and register the first
// backend whose required variable is set. The registered backend becomes the
// default.
//
// Returns an empty (but non-nil) registry when no env hint is present;
// WebDriver then surfaces ErrSearchNotConfigured on the first web_search call.
func BuildRegistryAuto(opts BuildOpts) *SearchBackendRegistry {
	registry := NewSearchBackendRegistry()
	envLookup := opts.EnvLookup
	if envLookup == nil {
		envLookup = os.Getenv
	}
	client := opts.HTTPClient
	if client == nil {
		client = defaultHTTPClient()
	}

	if key := envLookup(EnvTavilyAPIKey); key != "" {
		registry.Register(BackendDriverTavily, NewTavilyBackend(client, key, DefaultMaxResults, ""))
		_ = registry.SetDefault(BackendDriverTavily)
		log.Printf("[web] search backend auto-detected: %s", BackendDriverTavily)
		return registry
	}
	if key := envLookup(EnvExaAPIKey); key != "" {
		registry.Register(BackendDriverExa, NewExaBackend(client, key, DefaultMaxResults))
		_ = registry.SetDefault(BackendDriverExa)
		log.Printf("[web] search backend auto-detected: %s", BackendDriverExa)
		return registry
	}
	if base := envLookup(EnvSearxngURL); base != "" {
		registry.Register(BackendDriverSearxng, NewSearxngBackend(client, base))
		_ = registry.SetDefault(BackendDriverSearxng)
		log.Printf("[web] search backend auto-detected: %s", BackendDriverSearxng)
		return registry
	}
	return registry
}

func resolveAPIKey(configEnv, defaultEnv string, envLookup func(string) string) string {
	envName := firstNonEmpty(configEnv, defaultEnv)
	if envName == "" {
		return ""
	}
	return envLookup(envName)
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}
