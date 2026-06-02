package llm

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/vfs"
)

// --- CreateDriver tests ---

func TestCreateDriver_ClaudeCLI(t *testing.T) {
	t.Parallel()
	d, err := CreateDriver(ProviderConfig{
		Name:   "claude",
		Driver: DriverClaudeCLI,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	info := d.Info()
	if info.Name != "claude-cli" {
		t.Errorf("expected Name %q, got %q", "claude-cli", info.Name)
	}
	if info.Provider != "claude" {
		t.Errorf("expected Provider %q, got %q", "claude", info.Provider)
	}
}

func TestCreateDriver_ClaudeCLI_WithModel(t *testing.T) {
	t.Parallel()
	d, err := CreateDriver(ProviderConfig{
		Name:         "claude",
		Driver:       DriverClaudeCLI,
		DefaultModel: "sonnet",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Info().DefaultModel != "sonnet" {
		t.Errorf("expected DefaultModel %q, got %q", "sonnet", d.Info().DefaultModel)
	}
}

func TestCreateDriver_CursorCLI(t *testing.T) {
	t.Parallel()
	d, err := CreateDriver(ProviderConfig{
		Name:   "cursor",
		Driver: DriverCursorCLI,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	info := d.Info()
	if info.Name != "cursor-cli" {
		t.Errorf("expected Name %q, got %q", "cursor-cli", info.Name)
	}
	if info.Provider != "cursor" {
		t.Errorf("expected Provider %q, got %q", "cursor", info.Provider)
	}
}

func TestCreateDriver_CursorCLI_WithModel(t *testing.T) {
	t.Parallel()
	d, err := CreateDriver(ProviderConfig{
		Name:         "cursor",
		Driver:       DriverCursorCLI,
		DefaultModel: "gpt-4",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Info().DefaultModel != "gpt-4" {
		t.Errorf("expected DefaultModel %q, got %q", "gpt-4", d.Info().DefaultModel)
	}
}

func TestCreateDriver_OpenAICompat(t *testing.T) {
	t.Parallel()
	d, err := CreateDriver(ProviderConfig{
		Name:    "ollama",
		Driver:  DriverOpenAICompat,
		BaseURL: "http://localhost:11434/v1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	info := d.Info()
	if info.Name != "ollama" {
		t.Errorf("expected Name %q, got %q", "ollama", info.Name)
	}
	if info.Provider != "ollama" {
		t.Errorf("expected Provider %q, got %q", "ollama", info.Provider)
	}
}

func TestCreateDriver_OpenAICompat_WithModel(t *testing.T) {
	t.Parallel()
	d, err := CreateDriver(ProviderConfig{
		Name:         "groq",
		Driver:       DriverOpenAICompat,
		BaseURL:      "https://api.groq.com/openai/v1",
		DefaultModel: "llama-3.3-70b-versatile",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Info().DefaultModel != "llama-3.3-70b-versatile" {
		t.Errorf("expected DefaultModel %q, got %q", "llama-3.3-70b-versatile", d.Info().DefaultModel)
	}
}

func TestCreateDriver_UnsupportedDriver(t *testing.T) {
	t.Parallel()
	_, err := CreateDriver(ProviderConfig{
		Name:   "bad",
		Driver: "nonexistent",
	})
	if err == nil {
		t.Fatal("expected error for unsupported driver, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported driver type") {
		t.Errorf("expected error to contain %q, got %q", "unsupported driver type", err.Error())
	}
}

// --- mockDeviceRegisterer for unit tests ---

type mockDeviceRegisterer struct {
	registered map[string]bool
}

func newMockDeviceRegisterer() *mockDeviceRegisterer {
	return &mockDeviceRegisterer{registered: make(map[string]bool)}
}

func (m *mockDeviceRegisterer) Register(path string, _ vfs.VFSFileFactory) error {
	m.registered[path] = true
	return nil
}

// --- RegisterProviders tests ---

func TestRegisterProviders_DefaultConfig(t *testing.T) {
	t.Parallel()
	cfg := DefaultProvidersConfig()
	driverReg := NewDriverRegistry()
	devReg := newMockDeviceRegisterer()

	if err := RegisterProviders(cfg, driverReg, devReg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if driverReg.Len() != 3 {
		t.Errorf("expected 3 drivers registered, got %d", driverReg.Len())
	}
	if len(devReg.registered) != 3 {
		t.Errorf("expected 3 device paths registered, got %d", len(devReg.registered))
	}

	if _, ok := driverReg.Get("claude"); !ok {
		t.Error("expected 'claude' in driver registry")
	}
	if _, ok := driverReg.Get("cursor"); !ok {
		t.Error("expected 'cursor' in driver registry")
	}
	if _, ok := driverReg.Get("deepseek"); !ok {
		t.Error("expected 'deepseek' in driver registry")
	}
}

func TestRegisterProviders_FullConfig(t *testing.T) {
	t.Parallel()
	cfg := &ProvidersConfig{
		Version: "1",
		Providers: []ProviderConfig{
			{Name: "claude", Driver: DriverClaudeCLI, DefaultModel: "haiku"},
			{Name: "cursor", Driver: DriverCursorCLI},
			{Name: "ollama", Driver: DriverOpenAICompat, BaseURL: "http://localhost:11434/v1", DefaultModel: "llama3"},
		},
	}
	driverReg := NewDriverRegistry()
	devReg := newMockDeviceRegisterer()

	if err := RegisterProviders(cfg, driverReg, devReg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if driverReg.Len() != 3 {
		t.Errorf("expected 3 drivers, got %d", driverReg.Len())
	}

	for _, name := range []string{"claude", "cursor", "ollama"} {
		if _, ok := driverReg.Get(name); !ok {
			t.Errorf("expected %q in driver registry", name)
		}
	}
}

func TestRegisterProviders_VFSPathCorrect(t *testing.T) {
	t.Parallel()
	cfg := &ProvidersConfig{
		Version: "1",
		Providers: []ProviderConfig{
			{Name: "claude", Driver: DriverClaudeCLI},
			{Name: "ollama", Driver: DriverOpenAICompat, BaseURL: "http://localhost:11434/v1"},
		},
	}
	driverReg := NewDriverRegistry()
	devReg := newMockDeviceRegisterer()

	if err := RegisterProviders(cfg, driverReg, devReg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !devReg.registered["/dev/llm/claude"] {
		t.Error("expected /dev/llm/claude to be registered in device registry")
	}
	if !devReg.registered["/dev/llm/ollama"] {
		t.Error("expected /dev/llm/ollama to be registered in device registry")
	}
}

func TestRegisterProviders_DriverRegistryNames(t *testing.T) {
	t.Parallel()
	cfg := &ProvidersConfig{
		Version: "1",
		Providers: []ProviderConfig{
			{Name: "cursor", Driver: DriverCursorCLI},
			{Name: "claude", Driver: DriverClaudeCLI},
			{Name: "ollama", Driver: DriverOpenAICompat, BaseURL: "http://localhost:11434/v1"},
		},
	}
	driverReg := NewDriverRegistry()
	devReg := newMockDeviceRegisterer()

	if err := RegisterProviders(cfg, driverReg, devReg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	names := driverReg.Names()
	expected := []string{"claude", "cursor", "ollama"}
	if len(names) != len(expected) {
		t.Fatalf("expected %d names, got %d: %v", len(expected), len(names), names)
	}
	for i, name := range names {
		if name != expected[i] {
			t.Errorf("names[%d] = %q, expected %q", i, name, expected[i])
		}
	}
}

func TestRegisterProviders_CreateDriverError(t *testing.T) {
	t.Parallel()
	cfg := &ProvidersConfig{
		Version: "1",
		Providers: []ProviderConfig{
			{Name: "claude", Driver: DriverClaudeCLI},
			{Name: "bad", Driver: "unknown-driver"},
		},
	}
	driverReg := NewDriverRegistry()
	devReg := newMockDeviceRegisterer()

	err := RegisterProviders(cfg, driverReg, devReg)
	if err == nil {
		t.Fatal("expected error for unsupported driver, got nil")
	}
	if !strings.Contains(err.Error(), "bad") || !strings.Contains(err.Error(), "unsupported driver type") {
		t.Errorf("error should mention provider name and issue: %v", err)
	}
}

func TestRegisterProviders_DuplicateProvider(t *testing.T) {
	t.Parallel()
	cfg := &ProvidersConfig{
		Version: "1",
		Providers: []ProviderConfig{
			{Name: "claude", Driver: DriverClaudeCLI},
			{Name: "claude", Driver: DriverClaudeCLI},
		},
	}
	driverReg := NewDriverRegistry()
	devReg := newMockDeviceRegisterer()

	err := RegisterProviders(cfg, driverReg, devReg)
	if err == nil {
		t.Fatal("expected error for duplicate provider, got nil")
	}
	if !strings.Contains(err.Error(), "claude") {
		t.Errorf("error should mention duplicate name: %v", err)
	}
}

// --- VFS Integration tests ---

func TestRegisterProviders_VFSOpenRouting(t *testing.T) {
	t.Parallel()
	cfg := &ProvidersConfig{
		Version: "1",
		Providers: []ProviderConfig{
			{Name: "claude", Driver: DriverClaudeCLI},
			{Name: "ollama", Driver: DriverOpenAICompat, BaseURL: "http://localhost:11434/v1"},
		},
	}
	driverReg := NewDriverRegistry()
	devReg := vfs.NewDeviceRegistry()

	if err := RegisterProviders(cfg, driverReg, devReg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	f, err := devReg.Open("/dev/llm/ollama", 0, "")
	if err != nil {
		t.Fatalf("failed to open /dev/llm/ollama: %v", err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if stat.DevicePath != "/dev/llm/ollama" {
		t.Errorf("expected DevicePath %q, got %q", "/dev/llm/ollama", stat.DevicePath)
	}
}

func TestRegisterProviders_VFSOpenNonexistent(t *testing.T) {
	t.Parallel()
	cfg := DefaultProvidersConfig()
	driverReg := NewDriverRegistry()
	devReg := vfs.NewDeviceRegistry()

	if err := RegisterProviders(cfg, driverReg, devReg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err := devReg.Open("/dev/llm/nonexistent", 0, "")
	if err == nil {
		t.Fatal("expected error opening nonexistent device, got nil")
	}
}

func TestRegisterProviders_DefaultConfig_VFSCompat(t *testing.T) {
	t.Parallel()
	cfg := DefaultProvidersConfig()
	driverReg := NewDriverRegistry()
	devReg := vfs.NewDeviceRegistry()

	if err := RegisterProviders(cfg, driverReg, devReg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, path := range []string{"/dev/llm/claude", "/dev/llm/cursor"} {
		f, err := devReg.Open(path, 0, "")
		if err != nil {
			t.Errorf("failed to open %s: %v", path, err)
			continue
		}
		stat, err := f.Stat()
		if err != nil {
			t.Errorf("Stat failed for %s: %v", path, err)
		} else if stat.DevicePath != path {
			t.Errorf("expected DevicePath %q, got %q", path, stat.DevicePath)
		}
		f.Close()
	}
}

// --- RunHealthChecks tests ---

func TestRunHealthChecks_HTTPProvider_Healthy(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprint(w, `{"data":[]}`)
	}))
	defer ts.Close()

	cfg := &ProvidersConfig{
		Version: "1",
		Providers: []ProviderConfig{
			{Name: "test-api", Driver: DriverOpenAICompat, BaseURL: ts.URL},
		},
	}
	driverReg := NewDriverRegistry()
	drv := NewOpenAICompatDriver("test-api", ts.URL, WithHTTPClient(ts.Client()))
	_ = driverReg.Register("test-api", drv)

	RunHealthChecks(cfg, driverReg, 3*time.Second)

	// Wait for async health check to complete
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if driverReg.GetHealth("test-api") == HealthStatusHealthy {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("health = %q, want %q", driverReg.GetHealth("test-api"), HealthStatusHealthy)
}

func TestRunHealthChecks_HTTPProvider_Unhealthy(t *testing.T) {
	t.Parallel()
	// Use unreachable address
	cfg := &ProvidersConfig{
		Version: "1",
		Providers: []ProviderConfig{
			{Name: "bad-api", Driver: DriverOpenAICompat, BaseURL: "http://127.0.0.1:1"},
		},
	}
	driverReg := NewDriverRegistry()
	drv := NewOpenAICompatDriver("bad-api", "http://127.0.0.1:1")
	_ = driverReg.Register("bad-api", drv)

	RunHealthChecks(cfg, driverReg, 3*time.Second)

	// Wait for async health check to complete
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if driverReg.GetHealth("bad-api") == HealthStatusUnhealthy {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("health = %q, want %q", driverReg.GetHealth("bad-api"), HealthStatusUnhealthy)
}

func TestRunHealthChecks_CLIProvider_Skipped(t *testing.T) {
	t.Parallel()
	cfg := &ProvidersConfig{
		Version: "1",
		Providers: []ProviderConfig{
			{Name: "claude", Driver: DriverClaudeCLI},
			{Name: "cursor", Driver: DriverCursorCLI},
		},
	}
	driverReg := NewDriverRegistry()
	_ = driverReg.Register("claude", NewClaudeCliDriver())
	_ = driverReg.Register("cursor", NewCursorCliDriver())

	RunHealthChecks(cfg, driverReg, 3*time.Second)

	// Give goroutines a chance to run (should be no goroutines for CLI)
	time.Sleep(50 * time.Millisecond)

	if got := driverReg.GetHealth("claude"); got != HealthStatusUnchecked {
		t.Errorf("claude health = %q, want %q", got, HealthStatusUnchecked)
	}
	if got := driverReg.GetHealth("cursor"); got != HealthStatusUnchecked {
		t.Errorf("cursor health = %q, want %q", got, HealthStatusUnchecked)
	}
}

func TestRunHealthChecks_NonBlocking(t *testing.T) {
	t.Parallel()
	// Server that blocks until test completes
	handlerDone := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-handlerDone:
		case <-r.Context().Done():
		}
		w.WriteHeader(200)
	}))
	defer ts.Close()
	defer close(handlerDone)

	cfg := &ProvidersConfig{
		Version: "1",
		Providers: []ProviderConfig{
			{Name: "slow-api", Driver: DriverOpenAICompat, BaseURL: ts.URL},
		},
	}
	driverReg := NewDriverRegistry()
	drv := NewOpenAICompatDriver("slow-api", ts.URL, WithHTTPClient(ts.Client()))
	_ = driverReg.Register("slow-api", drv)

	start := time.Now()
	RunHealthChecks(cfg, driverReg, 3*time.Second)
	elapsed := time.Since(start)

	if elapsed > 100*time.Millisecond {
		t.Errorf("RunHealthChecks took %v, expected < 100ms (non-blocking)", elapsed)
	}
}

// --- CreateDriverWithEnv tests ---

func TestCreateDriverWithEnv_OpenAICompat_UsesEnvLookup(t *testing.T) {
	t.Parallel()
	var calledKeys []string
	envLookup := func(key string) string {
		calledKeys = append(calledKeys, key)
		if key == "MY_API_KEY" {
			return "test-secret-key"
		}
		return ""
	}

	d, err := CreateDriverWithEnv(ProviderConfig{
		Name:      "test-api",
		Driver:    DriverOpenAICompat,
		BaseURL:   "http://localhost:1234/v1",
		APIKeyEnv: "MY_API_KEY",
	}, envLookup)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d == nil {
		t.Fatal("expected non-nil driver")
	}

	// Verify envLookup was called with the right key
	found := false
	for _, k := range calledKeys {
		if k == "MY_API_KEY" {
			found = true
		}
	}
	if !found {
		t.Errorf("envLookup was not called with %q, called with: %v", "MY_API_KEY", calledKeys)
	}
}

func TestCreateDriverWithEnv_ClaudeCLI_DoesNotCallEnvLookup(t *testing.T) {
	t.Parallel()
	called := false
	envLookup := func(key string) string {
		called = true
		return ""
	}

	_, err := CreateDriverWithEnv(ProviderConfig{
		Name:   "claude",
		Driver: DriverClaudeCLI,
	}, envLookup)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Error("envLookup should not be called for claude-cli driver")
	}
}

func TestCreateDriverWithEnv_CursorCLI_DoesNotCallEnvLookup(t *testing.T) {
	t.Parallel()
	called := false
	envLookup := func(key string) string {
		called = true
		return ""
	}

	_, err := CreateDriverWithEnv(ProviderConfig{
		Name:   "cursor",
		Driver: DriverCursorCLI,
	}, envLookup)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Error("envLookup should not be called for cursor-cli driver")
	}
}

// --- Command alias tests ---

func TestCreateDriver_ClaudeCLI_WithCommand(t *testing.T) {
	t.Parallel()
	d, err := CreateDriver(ProviderConfig{
		Name:    "claude",
		Driver:  DriverClaudeCLI,
		Command: "claude-custom",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	claude, ok := d.(*ClaudeCliDriver)
	if !ok {
		t.Fatal("expected *ClaudeCliDriver")
	}
	if claude.cliCommand != "claude-custom" {
		t.Errorf("expected cliCommand %q, got %q", "claude-custom", claude.cliCommand)
	}
}

func TestCreateDriver_CursorCLI_WithCommand(t *testing.T) {
	t.Parallel()
	d, err := CreateDriver(ProviderConfig{
		Name:    "cursor",
		Driver:  DriverCursorCLI,
		Command: "cursor-agent",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cursor, ok := d.(*CursorCliDriver)
	if !ok {
		t.Fatal("expected *CursorCliDriver")
	}
	if cursor.cliCommand != "cursor-agent" {
		t.Errorf("expected cliCommand %q, got %q", "cursor-agent", cursor.cliCommand)
	}
}

func TestCreateDriver_ClaudeCLI_DefaultCommand(t *testing.T) {
	t.Parallel()
	d, err := CreateDriver(ProviderConfig{
		Name:   "claude",
		Driver: DriverClaudeCLI,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	claude, ok := d.(*ClaudeCliDriver)
	if !ok {
		t.Fatal("expected *ClaudeCliDriver")
	}
	if claude.cliCommand != "claude" {
		t.Errorf("expected default cliCommand %q, got %q", "claude", claude.cliCommand)
	}
}

func TestCreateDriver_ClaudeCLI_ExtraArgs(t *testing.T) {
	t.Parallel()
	d, err := CreateDriver(ProviderConfig{
		Name:      "claude",
		Driver:    DriverClaudeCLI,
		ExtraArgs: []string{"--dangerously-skip-permissions", "--custom"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	claude, ok := d.(*ClaudeCliDriver)
	if !ok {
		t.Fatal("expected *ClaudeCliDriver")
	}
	if len(claude.extraArgs) != 2 {
		t.Fatalf("expected 2 extra args, got %d", len(claude.extraArgs))
	}
	if claude.extraArgs[0] != "--dangerously-skip-permissions" {
		t.Errorf("extraArgs[0] = %q, want %q", claude.extraArgs[0], "--dangerously-skip-permissions")
	}
}

// --- Story 40.1: PermissionMode propagation ---

func TestCreateDriver_ClaudeCLI_PermissionMode(t *testing.T) {
	t.Parallel()
	d, err := CreateDriver(ProviderConfig{
		Name:           "claude",
		Driver:         DriverClaudeCLI,
		PermissionMode: "plan",
	})
	if err != nil {
		t.Fatalf("CreateDriver: %v", err)
	}
	claude, ok := d.(*ClaudeCliDriver)
	if !ok {
		t.Fatal("expected *ClaudeCliDriver")
	}
	if claude.permissionMode != "plan" {
		t.Errorf("permissionMode = %q, want %q", claude.permissionMode, "plan")
	}
}

func TestCreateDriver_ClaudeCLI_PermissionMode_DefaultsToBypass(t *testing.T) {
	t.Parallel()
	d, err := CreateDriver(ProviderConfig{
		Name:   "claude",
		Driver: DriverClaudeCLI,
	})
	if err != nil {
		t.Fatalf("CreateDriver: %v", err)
	}
	claude, ok := d.(*ClaudeCliDriver)
	if !ok {
		t.Fatal("expected *ClaudeCliDriver")
	}
	if claude.permissionMode != DefaultPermissionMode {
		t.Errorf("permissionMode = %q, want default %q", claude.permissionMode, DefaultPermissionMode)
	}
}

func TestCreateDriver_CursorCLI_DefaultCommand(t *testing.T) {
	t.Parallel()
	d, err := CreateDriver(ProviderConfig{
		Name:   "cursor",
		Driver: DriverCursorCLI,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cursor, ok := d.(*CursorCliDriver)
	if !ok {
		t.Fatal("expected *CursorCliDriver")
	}
	if cursor.cliCommand != "agent" {
		t.Errorf("expected default cliCommand %q, got %q", "agent", cursor.cliCommand)
	}
}
