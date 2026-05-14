package web

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/rnixai/rnix/internal/types"
)

// fakeBackend is a minimal SearchBackend used to verify registry plumbing.
type fakeBackend struct {
	name  string
	count int
	mu    sync.Mutex
}

func (f *fakeBackend) Search(_ context.Context, _ SearchParams) ([]SearchResult, error) {
	f.mu.Lock()
	f.count++
	f.mu.Unlock()
	return []SearchResult{{Title: f.name}}, nil
}

func TestSearchRegistry_RegisterGetDefault(t *testing.T) {
	r := NewSearchBackendRegistry()
	a, b := &fakeBackend{name: "a"}, &fakeBackend{name: "b"}
	r.Register("a", a)
	r.Register("b", b)

	if _, err := r.Default(); err == nil {
		t.Error("Default() should error before SetDefault")
	}
	if err := r.SetDefault("missing"); err == nil {
		t.Error("SetDefault to unknown name should error")
	}
	if err := r.SetDefault("a"); err != nil {
		t.Fatalf("SetDefault failed: %v", err)
	}
	got, err := r.Default()
	if err != nil {
		t.Fatalf("Default after SetDefault failed: %v", err)
	}
	if got != a {
		t.Errorf("Default returned wrong backend")
	}
	name, ok := r.DefaultName()
	if !ok || name != "a" {
		t.Errorf("DefaultName = %q,%v; want 'a',true", name, ok)
	}
	if r.Len() != 2 {
		t.Errorf("expected Len=2, got %d", r.Len())
	}
	if names := r.Names(); len(names) != 2 || names[0] != "a" || names[1] != "b" {
		t.Errorf("Names sort wrong: %v", names)
	}
}

func TestSearchRegistry_Default_NotConfigured_ReturnsGuidance(t *testing.T) {
	r := NewSearchBackendRegistry()
	_, err := r.Default()
	if err == nil {
		t.Fatal("expected ErrSearchNotConfigured")
	}
	var de *types.DriverError
	if !errors.As(err, &de) {
		t.Fatalf("expected *types.DriverError, got %T", err)
	}
	if de.Code != types.ErrServiceUnavailable {
		t.Errorf("expected ErrServiceUnavailable, got %v", de.Code)
	}
	// All three configuration paths must be mentioned
	msg := de.Error()
	for _, want := range []string{"TAVILY_API_KEY", "EXA_API_KEY", "RNIX_SEARCH_URL", "web-search.yaml"} {
		if !contains(msg, want) {
			t.Errorf("error should mention %q; got %q", want, msg)
		}
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && indexOf(s, sub) >= 0 }
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestSearchRegistry_ConcurrentRegisterAndGet(t *testing.T) {
	r := NewSearchBackendRegistry()
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			r.Register("b", &fakeBackend{name: "b"})
		}(i)
		go func() {
			defer wg.Done()
			_, _ = r.Get("b")
		}()
	}
	wg.Wait()
}

func TestSearchRegistry_LoadFromYAML(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, WebSearchConfigFile)
	yamlContent := `version: "1"
default_backend: exa
backends:
  - name: tavily
    driver: tavily
    api_key_env: TEST_TAVILY_KEY
  - name: exa
    driver: exa
    api_key_env: TEST_EXA_KEY
  - name: local-searxng
    driver: searxng
    base_url: http://localhost:8888
`
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// projectDir lookup goes to <projectDir>/.rnix/<file>, so we pass dir as globalDir
	cfg, err := LoadWebSearchConfig(dir, "")
	if err != nil {
		t.Fatalf("LoadWebSearchConfig: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil cfg")
	}
	if cfg.DefaultBackend != "exa" {
		t.Errorf("default_backend = %q", cfg.DefaultBackend)
	}
	if len(cfg.Backends) != 3 {
		t.Fatalf("expected 3 backends, got %d", len(cfg.Backends))
	}
	if cfg.Backends[0].Driver != "tavily" || cfg.Backends[0].APIKeyEnv != "TEST_TAVILY_KEY" {
		t.Errorf("tavily backend parse wrong: %+v", cfg.Backends[0])
	}

	envLookup := func(k string) string {
		return map[string]string{
			"TEST_TAVILY_KEY": "tk",
			"TEST_EXA_KEY":    "ek",
		}[k]
	}
	registry := BuildRegistryFromConfig(cfg, BuildOpts{EnvLookup: envLookup, HTTPClient: dummyClient()})
	if registry.Len() != 3 {
		t.Errorf("expected 3 backends registered, got %d", registry.Len())
	}
	name, _ := registry.DefaultName()
	if name != "exa" {
		t.Errorf("expected default=exa, got %q", name)
	}
}

func TestSearchRegistry_LoadFromYAML_ProjectOverridesGlobal(t *testing.T) {
	globalDir := t.TempDir()
	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".rnix"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	_ = os.WriteFile(filepath.Join(globalDir, WebSearchConfigFile), []byte(`version: "1"
backends:
  - {name: gx, driver: searxng, base_url: http://global:8888}
`), 0o600)
	_ = os.WriteFile(filepath.Join(projectDir, ".rnix", WebSearchConfigFile), []byte(`version: "1"
backends:
  - {name: px, driver: searxng, base_url: http://project:9999}
`), 0o600)

	cfg, err := LoadWebSearchConfig(globalDir, projectDir)
	if err != nil {
		t.Fatalf("LoadWebSearchConfig: %v", err)
	}
	if len(cfg.Backends) != 1 || cfg.Backends[0].Name != "px" {
		t.Errorf("project should override global, got %+v", cfg.Backends)
	}
}

func TestSearchRegistry_EnvSnapshotResolution(t *testing.T) {
	dotenv := map[string]string{"TAVILY_API_KEY": "from-dotenv"}
	envLookup := func(k string) string {
		if v, ok := dotenv[k]; ok {
			return v
		}
		return ""
	}
	cfg := &WebSearchConfig{
		Backends: []BackendConfig{
			{Name: "tavily", Driver: BackendDriverTavily, APIKeyEnv: "TAVILY_API_KEY"},
		},
	}
	r := BuildRegistryFromConfig(cfg, BuildOpts{EnvLookup: envLookup, HTTPClient: dummyClient()})
	if r.Len() != 1 {
		t.Fatalf("expected 1 backend, got %d", r.Len())
	}
	// Ensure os.Getenv was NOT consulted by leaving env untouched.
}

func TestSearchRegistry_AutoDetect_TavilyKeyOnly(t *testing.T) {
	envLookup := func(k string) string {
		if k == "TAVILY_API_KEY" {
			return "tk"
		}
		return ""
	}
	r := BuildRegistryAuto(BuildOpts{EnvLookup: envLookup, HTTPClient: dummyClient()})
	name, ok := r.DefaultName()
	if !ok || name != BackendDriverTavily {
		t.Errorf("expected default=tavily, got %q,%v", name, ok)
	}
}

func TestSearchRegistry_AutoDetect_ExaKeyOnly(t *testing.T) {
	envLookup := func(k string) string {
		if k == "EXA_API_KEY" {
			return "ek"
		}
		return ""
	}
	r := BuildRegistryAuto(BuildOpts{EnvLookup: envLookup, HTTPClient: dummyClient()})
	name, _ := r.DefaultName()
	if name != BackendDriverExa {
		t.Errorf("expected default=exa, got %q", name)
	}
}

func TestSearchRegistry_AutoDetect_SearxngOnly(t *testing.T) {
	envLookup := func(k string) string {
		if k == "RNIX_SEARCH_URL" {
			return "http://localhost:8888"
		}
		return ""
	}
	r := BuildRegistryAuto(BuildOpts{EnvLookup: envLookup, HTTPClient: dummyClient()})
	name, _ := r.DefaultName()
	if name != BackendDriverSearxng {
		t.Errorf("expected default=searxng, got %q", name)
	}
}

func TestSearchRegistry_AutoDetect_PriorityOrder(t *testing.T) {
	envLookup := func(k string) string {
		// All three set — Tavily should win
		return map[string]string{
			"TAVILY_API_KEY":  "tk",
			"EXA_API_KEY":     "ek",
			"RNIX_SEARCH_URL": "http://x",
		}[k]
	}
	r := BuildRegistryAuto(BuildOpts{EnvLookup: envLookup, HTTPClient: dummyClient()})
	name, _ := r.DefaultName()
	if name != BackendDriverTavily {
		t.Errorf("Tavily must take priority, got %q", name)
	}
}

func TestSearchRegistry_AutoDetect_NoConfig_ClearError(t *testing.T) {
	envLookup := func(string) string { return "" }
	r := BuildRegistryAuto(BuildOpts{EnvLookup: envLookup, HTTPClient: dummyClient()})
	if r.Len() != 0 {
		t.Errorf("expected empty registry, got %d backends", r.Len())
	}
	_, err := r.Default()
	if err == nil {
		t.Fatal("Default() should return guidance error")
	}
	if !contains(err.Error(), "TAVILY_API_KEY") || !contains(err.Error(), "web-search.yaml") {
		t.Errorf("expected guidance to mention all options; got: %v", err)
	}
}

func TestWebDriver_Search_DispatchToDefaultBackend(t *testing.T) {
	fake := &fakeBackend{name: "fb"}
	r := NewSearchBackendRegistry()
	r.Register("fb", fake)
	_ = r.SetDefault("fb")

	d := NewDriverWithOptions(DriverOpts{SearchRegistry: r})
	f := &WebFile{driver: d, devicePath: "/dev/web"}
	if err := f.Write(context.Background(), []byte(`{"query":"test"}`)); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if fake.count != 1 {
		t.Errorf("expected backend called once, got %d", fake.count)
	}
}

func TestWebDriver_Search_NoBackend_ReturnsGuidance(t *testing.T) {
	d := NewDriverWithOptions(DriverOpts{SearchRegistry: NewSearchBackendRegistry()})
	f := &WebFile{driver: d, devicePath: "/dev/web"}
	err := f.Write(context.Background(), []byte(`{"query":"test"}`))
	if err == nil {
		t.Fatal("expected error")
	}
	var de *types.DriverError
	if !errors.As(err, &de) {
		t.Fatalf("expected *types.DriverError, got %T", err)
	}
	if de.Code != types.ErrServiceUnavailable {
		t.Errorf("expected ErrServiceUnavailable, got %v", de.Code)
	}
}

func TestBuildRegistryFromConfig_SkipsMissingKey(t *testing.T) {
	cfg := &WebSearchConfig{
		Backends: []BackendConfig{
			{Name: "no-key", Driver: BackendDriverTavily, APIKeyEnv: "MISSING"},
			{Name: "ok", Driver: BackendDriverSearxng, BaseURL: "http://x"},
		},
	}
	envLookup := func(string) string { return "" }
	r := BuildRegistryFromConfig(cfg, BuildOpts{EnvLookup: envLookup, HTTPClient: dummyClient()})
	if r.Len() != 1 {
		t.Errorf("expected 1 registered (other skipped), got %d", r.Len())
	}
	if _, ok := r.Get("ok"); !ok {
		t.Error("expected 'ok' backend registered")
	}
}

func TestWebSearchConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     WebSearchConfig
		wantErr bool
	}{
		{"empty backends", WebSearchConfig{}, true},
		{"missing name", WebSearchConfig{Backends: []BackendConfig{{Driver: "tavily"}}}, true},
		{"unknown driver", WebSearchConfig{Backends: []BackendConfig{{Name: "x", Driver: "nope"}}}, true},
		{"duplicate name", WebSearchConfig{Backends: []BackendConfig{
			{Name: "x", Driver: "tavily"}, {Name: "x", Driver: "exa"},
		}}, true},
		{"unknown default", WebSearchConfig{DefaultBackend: "z", Backends: []BackendConfig{
			{Name: "x", Driver: "tavily"},
		}}, true},
		{"ok", WebSearchConfig{Backends: []BackendConfig{{Name: "x", Driver: "tavily"}}}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate(): err=%v, wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func dummyClient() HTTPClient {
	return newMockHTTPClient(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Request: req}, nil
	})
}
