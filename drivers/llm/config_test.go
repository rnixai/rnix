package llm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- Test helpers ---

func writeYAML(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writeYAML: %v", err)
	}
	return path
}

const validFullYAML = `version: "1"
providers:
  - name: claude
    driver: claude-cli
    default_model: haiku
  - name: cursor
    driver: cursor-cli
  - name: ollama
    driver: openai-compat
    base_url: http://localhost:11434/v1
    default_model: llama3
  - name: groq
    driver: openai-compat
    base_url: https://api.groq.com/openai/v1
    default_model: llama-3.3-70b-versatile
    api_key_env: GROQ_API_KEY
`

const validMinimalYAML = `version: "1"
providers:
  - name: my-llm
    driver: claude-cli
`

// --- AC1: 正确解析 ---

func TestProviderConfig_DriverConstants(t *testing.T) {
	t.Parallel()
	if DriverClaudeCLI != "claude-cli" {
		t.Errorf("DriverClaudeCLI = %q, want %q", DriverClaudeCLI, "claude-cli")
	}
	if DriverCursorCLI != "cursor-cli" {
		t.Errorf("DriverCursorCLI = %q, want %q", DriverCursorCLI, "cursor-cli")
	}
	if DriverOpenAICompat != "openai-compat" {
		t.Errorf("DriverOpenAICompat = %q, want %q", DriverOpenAICompat, "openai-compat")
	}
}

func TestProvidersConfig_YAMLUnmarshal(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := writeYAML(t, dir, ProvidersConfigFile, validFullYAML)

	cfg, err := LoadProvidersConfig(path)
	if err != nil {
		t.Fatalf("LoadProvidersConfig: %v", err)
	}
	if cfg.Version != "1" {
		t.Errorf("Version = %q, want %q", cfg.Version, "1")
	}
	if len(cfg.Providers) != 4 {
		t.Fatalf("len(Providers) = %d, want 4", len(cfg.Providers))
	}
}

func TestLoadProvidersConfig_ValidFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := writeYAML(t, dir, ProvidersConfigFile, validFullYAML)

	cfg, err := LoadProvidersConfig(path)
	if err != nil {
		t.Fatalf("LoadProvidersConfig: %v", err)
	}

	// claude-cli provider
	claude := cfg.Providers[0]
	if claude.Name != "claude" {
		t.Errorf("Providers[0].Name = %q, want %q", claude.Name, "claude")
	}
	if claude.Driver != DriverClaudeCLI {
		t.Errorf("Providers[0].Driver = %q, want %q", claude.Driver, DriverClaudeCLI)
	}
	if claude.DefaultModel != "haiku" {
		t.Errorf("Providers[0].DefaultModel = %q, want %q", claude.DefaultModel, "haiku")
	}

	// cursor-cli provider
	cursor := cfg.Providers[1]
	if cursor.Name != "cursor" {
		t.Errorf("Providers[1].Name = %q, want %q", cursor.Name, "cursor")
	}
	if cursor.Driver != DriverCursorCLI {
		t.Errorf("Providers[1].Driver = %q, want %q", cursor.Driver, DriverCursorCLI)
	}

	// openai-compat provider with all fields
	groq := cfg.Providers[3]
	if groq.Name != "groq" {
		t.Errorf("Providers[3].Name = %q, want %q", groq.Name, "groq")
	}
	if groq.Driver != DriverOpenAICompat {
		t.Errorf("Providers[3].Driver = %q, want %q", groq.Driver, DriverOpenAICompat)
	}
	if groq.BaseURL != "https://api.groq.com/openai/v1" {
		t.Errorf("Providers[3].BaseURL = %q, want %q", groq.BaseURL, "https://api.groq.com/openai/v1")
	}
	if groq.DefaultModel != "llama-3.3-70b-versatile" {
		t.Errorf("Providers[3].DefaultModel = %q, want %q", groq.DefaultModel, "llama-3.3-70b-versatile")
	}
	if groq.APIKeyEnv != "GROQ_API_KEY" {
		t.Errorf("Providers[3].APIKeyEnv = %q, want %q", groq.APIKeyEnv, "GROQ_API_KEY")
	}
}

func TestLoadProvidersConfig_MinimalFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := writeYAML(t, dir, ProvidersConfigFile, validMinimalYAML)

	cfg, err := LoadProvidersConfig(path)
	if err != nil {
		t.Fatalf("LoadProvidersConfig: %v", err)
	}
	if len(cfg.Providers) != 1 {
		t.Fatalf("len(Providers) = %d, want 1", len(cfg.Providers))
	}
	p := cfg.Providers[0]
	if p.DefaultModel != "" {
		t.Errorf("DefaultModel = %q, want empty", p.DefaultModel)
	}
	if p.BaseURL != "" {
		t.Errorf("BaseURL = %q, want empty", p.BaseURL)
	}
	if p.APIKeyEnv != "" {
		t.Errorf("APIKeyEnv = %q, want empty", p.APIKeyEnv)
	}
}

func TestLoadProvidersConfig_MultipleProviders(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("version: \"1\"\nproviders:\n")
	for i := range 10 {
		sb.WriteString("  - name: provider-")
		sb.WriteString("x")
		sb.WriteString(string(rune('0' + i)))
		sb.WriteString("\n    driver: claude-cli\n")
	}
	dir := t.TempDir()
	path := writeYAML(t, dir, ProvidersConfigFile, sb.String())

	cfg, err := LoadProvidersConfig(path)
	if err != nil {
		t.Fatalf("LoadProvidersConfig: %v", err)
	}
	if len(cfg.Providers) != 10 {
		t.Errorf("len(Providers) = %d, want 10", len(cfg.Providers))
	}
}

// --- AC2: 错误处理 ---

func TestLoadProvidersConfig_YAMLSyntaxError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	badYAML := "version: \"1\"\nproviders:\n  - name: claude\n    driver claude-cli\n"
	path := writeYAML(t, dir, ProvidersConfigFile, badYAML)

	_, err := LoadProvidersConfig(path)
	if err == nil {
		t.Fatal("expected error for YAML syntax error, got nil")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "4") && !strings.Contains(errMsg, "line") {
		t.Errorf("error should contain line number info, got: %s", errMsg)
	}
}

func TestLoadProvidersConfig_MissingName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	yaml := `version: "1"
providers:
  - driver: claude-cli
`
	path := writeYAML(t, dir, ProvidersConfigFile, yaml)

	_, err := LoadProvidersConfig(path)
	if err == nil {
		t.Fatal("expected error for missing name, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "name") {
		t.Errorf("error should mention 'name', got: %s", err.Error())
	}
}

func TestLoadProvidersConfig_InvalidDriver(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	yaml := `version: "1"
providers:
  - name: bad
    driver: unknown-driver
`
	path := writeYAML(t, dir, ProvidersConfigFile, yaml)

	_, err := LoadProvidersConfig(path)
	if err == nil {
		t.Fatal("expected error for invalid driver, got nil")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "unknown-driver") {
		t.Errorf("error should mention the invalid driver value, got: %s", errMsg)
	}
}

func TestLoadProvidersConfig_MissingBaseURL(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	yaml := `version: "1"
providers:
  - name: my-openai
    driver: openai-compat
    default_model: gpt-4
`
	path := writeYAML(t, dir, ProvidersConfigFile, yaml)

	_, err := LoadProvidersConfig(path)
	if err == nil {
		t.Fatal("expected error for openai-compat missing base_url, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "base_url") {
		t.Errorf("error should mention 'base_url', got: %s", err.Error())
	}
}

func TestLoadProvidersConfig_DuplicateNames(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	yaml := `version: "1"
providers:
  - name: claude
    driver: claude-cli
  - name: claude
    driver: cursor-cli
`
	path := writeYAML(t, dir, ProvidersConfigFile, yaml)

	_, err := LoadProvidersConfig(path)
	if err == nil {
		t.Fatal("expected error for duplicate names, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "duplicate") {
		t.Errorf("error should mention 'duplicate', got: %s", err.Error())
	}
}

func TestLoadProvidersConfig_InvalidNameChars(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	yaml := `version: "1"
providers:
  - name: "bad name!"
    driver: claude-cli
`
	path := writeYAML(t, dir, ProvidersConfigFile, yaml)

	_, err := LoadProvidersConfig(path)
	if err == nil {
		t.Fatal("expected error for invalid name characters, got nil")
	}
	errMsg := strings.ToLower(err.Error())
	if !strings.Contains(errMsg, "bad name!") || !strings.Contains(errMsg, "character") || !strings.Contains(errMsg, "name") {
		t.Errorf("error should mention invalid name with bad characters, got: %s", err.Error())
	}
}

func TestLoadProvidersConfig_EmptyProviders(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	yaml := `version: "1"
providers: []
`
	path := writeYAML(t, dir, ProvidersConfigFile, yaml)

	_, err := LoadProvidersConfig(path)
	if err == nil {
		t.Fatal("expected error for empty providers list, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "empty") || !strings.Contains(strings.ToLower(err.Error()), "provider") {
		t.Errorf("error should mention empty providers, got: %s", err.Error())
	}
}

func TestLoadProvidersConfig_MultipleErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	yaml := `version: "1"
providers:
  - name: ""
    driver: bad-driver
  - name: "no spaces!"
    driver: openai-compat
`
	path := writeYAML(t, dir, ProvidersConfigFile, yaml)

	_, err := LoadProvidersConfig(path)
	if err == nil {
		t.Fatal("expected multiple validation errors, got nil")
	}
	errMsg := err.Error()
	errCount := 0
	for _, keyword := range []string{"name", "driver", "base_url"} {
		if strings.Contains(strings.ToLower(errMsg), keyword) {
			errCount++
		}
	}
	if errCount < 2 {
		t.Errorf("expected multiple errors reported, got: %s", errMsg)
	}
}

func TestValidate_TableDriven(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		cfg     ProvidersConfig
		wantErr string
	}{
		{
			name: "valid claude-cli",
			cfg: ProvidersConfig{
				Version:   "1",
				Providers: []ProviderConfig{{Name: "claude", Driver: DriverClaudeCLI}},
			},
			wantErr: "",
		},
		{
			name: "valid openai-compat with base_url",
			cfg: ProvidersConfig{
				Version: "1",
				Providers: []ProviderConfig{{
					Name: "ollama", Driver: DriverOpenAICompat,
					BaseURL: "http://localhost:11434/v1",
				}},
			},
			wantErr: "",
		},
		{
			name: "empty providers",
			cfg: ProvidersConfig{
				Version:   "1",
				Providers: []ProviderConfig{},
			},
			wantErr: "empty",
		},
		{
			name: "nil providers",
			cfg: ProvidersConfig{
				Version:   "1",
				Providers: nil,
			},
			wantErr: "empty",
		},
		{
			name: "missing name",
			cfg: ProvidersConfig{
				Version:   "1",
				Providers: []ProviderConfig{{Name: "", Driver: DriverClaudeCLI}},
			},
			wantErr: "name",
		},
		{
			name: "invalid driver",
			cfg: ProvidersConfig{
				Version:   "1",
				Providers: []ProviderConfig{{Name: "x", Driver: "nope"}},
			},
			wantErr: "driver",
		},
		{
			name: "openai-compat missing base_url",
			cfg: ProvidersConfig{
				Version:   "1",
				Providers: []ProviderConfig{{Name: "x", Driver: DriverOpenAICompat}},
			},
			wantErr: "base_url",
		},
		{
			name: "duplicate names",
			cfg: ProvidersConfig{
				Version: "1",
				Providers: []ProviderConfig{
					{Name: "a", Driver: DriverClaudeCLI},
					{Name: "a", Driver: DriverCursorCLI},
				},
			},
			wantErr: "duplicate",
		},
		{
			name: "invalid name characters",
			cfg: ProvidersConfig{
				Version:   "1",
				Providers: []ProviderConfig{{Name: "has space", Driver: DriverClaudeCLI}},
			},
			wantErr: "name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.cfg.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("Validate() unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(strings.ToLower(err.Error()), tt.wantErr) {
				t.Errorf("Validate() error = %q, want substring %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// --- AC3: 默认回退 ---

func TestDefaultProvidersConfig(t *testing.T) {
	t.Parallel()
	cfg := DefaultProvidersConfig()

	if cfg == nil {
		t.Fatal("DefaultProvidersConfig returned nil")
	}
	if cfg.Version != "1" {
		t.Errorf("Version = %q, want %q", cfg.Version, "1")
	}
	if len(cfg.Providers) != 3 {
		t.Fatalf("len(Providers) = %d, want 3", len(cfg.Providers))
	}
	if cfg.DefaultProvider != "deepseek" {
		t.Errorf("DefaultProvider = %q, want %q", cfg.DefaultProvider, "deepseek")
	}

	claude := cfg.Providers[0]
	if claude.Name != "claude" {
		t.Errorf("Providers[0].Name = %q, want %q", claude.Name, "claude")
	}
	if claude.Driver != DriverClaudeCLI {
		t.Errorf("Providers[0].Driver = %q, want %q", claude.Driver, DriverClaudeCLI)
	}
	if claude.DefaultModel != "haiku" {
		t.Errorf("Providers[0].DefaultModel = %q, want %q", claude.DefaultModel, "haiku")
	}

	cursor := cfg.Providers[1]
	if cursor.Name != "cursor" {
		t.Errorf("Providers[1].Name = %q, want %q", cursor.Name, "cursor")
	}
	if cursor.Driver != DriverCursorCLI {
		t.Errorf("Providers[1].Driver = %q, want %q", cursor.Driver, DriverCursorCLI)
	}

	deepseek := cfg.Providers[2]
	if deepseek.Name != "deepseek" {
		t.Errorf("Providers[2].Name = %q, want %q", deepseek.Name, "deepseek")
	}
	if deepseek.Driver != DriverOpenAICompat {
		t.Errorf("Providers[2].Driver = %q, want %q", deepseek.Driver, DriverOpenAICompat)
	}
	if deepseek.DefaultModel != "deepseek-v4-flash" {
		t.Errorf("Providers[2].DefaultModel = %q, want %q", deepseek.DefaultModel, "deepseek-v4-flash")
	}
	if deepseek.APIKeyEnv != "DEEPSEEK_API_KEY" {
		t.Errorf("Providers[2].APIKeyEnv = %q, want %q", deepseek.APIKeyEnv, "DEEPSEEK_API_KEY")
	}
	if got := deepseek.Models["deepseek-v4-flash"].ContextWindow; got != 1_000_000 {
		t.Errorf("deepseek-v4-flash ContextWindow = %d, want %d", got, 1_000_000)
	}
}

func TestLoadOrDefault_NoFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "no-xdg"))

	t.Chdir(dir)

	cfg, err := LoadOrDefaultProvidersConfig()
	if err != nil {
		t.Fatalf("LoadOrDefaultProvidersConfig: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if len(cfg.Providers) != 3 {
		t.Errorf("expected default 3 providers, got %d", len(cfg.Providers))
	}
	if cfg.Providers[0].Name != "claude" {
		t.Errorf("Providers[0].Name = %q, want %q", cfg.Providers[0].Name, "claude")
	}
}

func TestLoadOrDefault_ValidFile(t *testing.T) {
	dir := t.TempDir()
	yaml := `version: "1"
providers:
  - name: custom-llm
    driver: claude-cli
    default_model: sonnet
`
	writeYAML(t, dir, ProvidersConfigFile, yaml)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "no-xdg"))

	t.Chdir(dir)

	cfg, err := LoadOrDefaultProvidersConfig()
	if err != nil {
		t.Fatalf("LoadOrDefaultProvidersConfig: %v", err)
	}
	if len(cfg.Providers) != 1 {
		t.Fatalf("len(Providers) = %d, want 1", len(cfg.Providers))
	}
	if cfg.Providers[0].Name != "custom-llm" {
		t.Errorf("Providers[0].Name = %q, want %q", cfg.Providers[0].Name, "custom-llm")
	}
	if cfg.Providers[0].DefaultModel != "sonnet" {
		t.Errorf("Providers[0].DefaultModel = %q, want %q", cfg.Providers[0].DefaultModel, "sonnet")
	}
}

func TestLoadOrDefault_InvalidFile(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, ProvidersConfigFile, "not: valid: yaml: {{{\n")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "no-xdg"))

	t.Chdir(dir)

	_, err := LoadOrDefaultProvidersConfig()
	if err == nil {
		t.Fatal("expected error for invalid file, got nil")
	}
}

// --- 配置查找 ---

func TestFindProvidersConfigPath_CWD(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, ProvidersConfigFile, validMinimalYAML)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "no-xdg"))

	t.Chdir(dir)

	got := FindProvidersConfigPath()
	if got == "" {
		t.Fatal("expected non-empty path, got empty")
	}
	if filepath.Base(got) != ProvidersConfigFile {
		t.Errorf("path base = %q, want %q", filepath.Base(got), ProvidersConfigFile)
	}
}

func TestFindProvidersConfigPath_XDGConfig(t *testing.T) {
	dir := t.TempDir()
	xdgDir := filepath.Join(dir, "xdg-config", "rnix")
	if err := os.MkdirAll(xdgDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	writeYAML(t, xdgDir, ProvidersConfigFile, validMinimalYAML)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg-config"))

	cwdDir := filepath.Join(dir, "empty-cwd")
	if err := os.MkdirAll(cwdDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	t.Chdir(cwdDir)

	got := FindProvidersConfigPath()
	if got == "" {
		t.Fatal("expected non-empty path from XDG, got empty")
	}
	if !strings.Contains(got, "xdg-config") {
		t.Errorf("expected XDG path, got: %s", got)
	}
}

func TestFindProvidersConfigPath_Precedence(t *testing.T) {
	dir := t.TempDir()

	// Create in CWD
	cwdDir := filepath.Join(dir, "cwd")
	if err := os.MkdirAll(cwdDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	writeYAML(t, cwdDir, ProvidersConfigFile, validMinimalYAML)

	// Create in XDG
	xdgDir := filepath.Join(dir, "xdg-config", "rnix")
	if err := os.MkdirAll(xdgDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	writeYAML(t, xdgDir, ProvidersConfigFile, validMinimalYAML)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg-config"))

	t.Chdir(cwdDir)

	got := FindProvidersConfigPath()
	if got == "" {
		t.Fatal("expected non-empty path, got empty")
	}
	if !strings.HasPrefix(got, cwdDir) {
		t.Errorf("expected CWD path to take precedence, got: %s", got)
	}
}

func TestFindProvidersConfigPath_NotFound(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "no-xdg"))

	t.Chdir(dir)

	got := FindProvidersConfigPath()
	if got != "" {
		t.Errorf("expected empty path when not found, got: %s", got)
	}
}

// --- AC4: 性能 ---

func TestLoadProvidersConfig_Performance(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("version: \"1\"\nproviders:\n")
	for i := range 10 {
		sb.WriteString("  - name: provider-")
		sb.WriteByte(byte('a' + i))
		sb.WriteString("\n    driver: openai-compat\n    base_url: http://localhost:11434/v1\n    default_model: model-")
		sb.WriteByte(byte('a' + i))
		sb.WriteString("\n    api_key_env: KEY_")
		sb.WriteByte(byte('A' + i))
		sb.WriteString("\n")
	}
	dir := t.TempDir()
	path := writeYAML(t, dir, ProvidersConfigFile, sb.String())

	start := time.Now()
	cfg, err := LoadProvidersConfig(path)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("LoadProvidersConfig: %v", err)
	}
	if len(cfg.Providers) != 10 {
		t.Errorf("len(Providers) = %d, want 10", len(cfg.Providers))
	}
	if elapsed > 2*time.Second {
		t.Errorf("parsing took %v, want <= 2s", elapsed)
	}
}

// --- 边界情况 ---

func TestLoadProvidersConfig_FileNotFound(t *testing.T) {
	t.Parallel()
	_, err := LoadProvidersConfig("/nonexistent/path/rnix-providers.yaml")
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
}

func TestLoadProvidersConfig_EmptyFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := writeYAML(t, dir, ProvidersConfigFile, "")

	_, err := LoadProvidersConfig(path)
	if err == nil {
		t.Fatal("expected error for empty file, got nil")
	}
}

func TestProviderConfig_Validate_CLIDriverIgnoresBaseURL(t *testing.T) {
	t.Parallel()
	cfg := ProvidersConfig{
		Version: "1",
		Providers: []ProviderConfig{
			{Name: "claude", Driver: DriverClaudeCLI},
			{Name: "cursor", Driver: DriverCursorCLI},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() unexpected error for CLI drivers without base_url: %v", err)
	}
}

// --- DefaultProvider tests ---

func TestDefaultProvider_YAMLParsing(t *testing.T) {
	t.Parallel()
	yaml := `version: "1"
default_provider: groq
providers:
  - name: claude
    driver: claude-cli
  - name: groq
    driver: openai-compat
    base_url: https://api.groq.com/openai/v1
`
	dir := t.TempDir()
	path := writeYAML(t, dir, ProvidersConfigFile, yaml)
	cfg, err := LoadProvidersConfig(path)
	if err != nil {
		t.Fatalf("LoadProvidersConfig: %v", err)
	}
	if cfg.DefaultProvider != "groq" {
		t.Errorf("DefaultProvider = %q, want %q", cfg.DefaultProvider, "groq")
	}
}

func TestDefaultProvider_Validate_InvalidReference(t *testing.T) {
	t.Parallel()
	cfg := ProvidersConfig{
		Version:         "1",
		DefaultProvider: "nonexistent",
		Providers: []ProviderConfig{
			{Name: "claude", Driver: DriverClaudeCLI},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for invalid default_provider reference")
	}
	if !strings.Contains(err.Error(), "not found in providers list") {
		t.Errorf("error should mention 'not found in providers list', got: %v", err)
	}
}

func TestDefaultProvider_Validate_EmptyIsValid(t *testing.T) {
	t.Parallel()
	cfg := ProvidersConfig{
		Version: "1",
		Providers: []ProviderConfig{
			{Name: "claude", Driver: DriverClaudeCLI},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() unexpected error when default_provider is empty: %v", err)
	}
}

func TestDefaultProvider_Validate_ValidReference(t *testing.T) {
	t.Parallel()
	cfg := ProvidersConfig{
		Version:         "1",
		DefaultProvider: "claude",
		Providers: []ProviderConfig{
			{Name: "claude", Driver: DriverClaudeCLI},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() unexpected error for valid default_provider: %v", err)
	}
}

func TestResolveDefaultProvider_ExplicitValue(t *testing.T) {
	t.Parallel()
	cfg := ProvidersConfig{
		DefaultProvider: "groq",
		Providers: []ProviderConfig{
			{Name: "claude", Driver: DriverClaudeCLI},
			{Name: "groq", Driver: DriverOpenAICompat, BaseURL: "http://localhost"},
		},
	}
	if got := cfg.ResolveDefaultProvider(); got != "groq" {
		t.Errorf("ResolveDefaultProvider() = %q, want %q", got, "groq")
	}
}

func TestResolveDefaultProvider_FallsBackToFirstProvider(t *testing.T) {
	t.Parallel()
	cfg := ProvidersConfig{
		Providers: []ProviderConfig{
			{Name: "ollama", Driver: DriverOpenAICompat, BaseURL: "http://localhost"},
			{Name: "claude", Driver: DriverClaudeCLI},
		},
	}
	if got := cfg.ResolveDefaultProvider(); got != "ollama" {
		t.Errorf("ResolveDefaultProvider() = %q, want %q (first provider)", got, "ollama")
	}
}

func TestResolveDefaultProvider_BuiltinDefault(t *testing.T) {
	t.Parallel()
	cfg := DefaultProvidersConfig()
	if got := cfg.ResolveDefaultProvider(); got != "deepseek" {
		t.Errorf("DefaultProvidersConfig().ResolveDefaultProvider() = %q, want %q", got, "deepseek")
	}
}

// --- Story 40.1: permission_mode validation ---

func TestProvidersConfig_Validate_PermissionMode_Valid(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"bypassPermissions", "acceptEdits", "plan", "default"} {
		t.Run(mode, func(t *testing.T) {
			cfg := ProvidersConfig{
				Version: "1",
				Providers: []ProviderConfig{
					{Name: "claude", Driver: DriverClaudeCLI, PermissionMode: mode},
				},
			}
			if err := cfg.Validate(); err != nil {
				t.Errorf("Validate() unexpected error for permission_mode=%q: %v", mode, err)
			}
		})
	}
}

func TestProvidersConfig_Validate_PermissionMode_Invalid(t *testing.T) {
	t.Parallel()
	cfg := ProvidersConfig{
		Version: "1",
		Providers: []ProviderConfig{
			{Name: "claude", Driver: DriverClaudeCLI, PermissionMode: "yolo"},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for invalid permission_mode, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "yolo") {
		t.Errorf("error should echo invalid value, got: %s", msg)
	}
	for _, valid := range []string{"bypassPermissions", "acceptEdits", "plan", "default"} {
		if !strings.Contains(msg, valid) {
			t.Errorf("error should list valid value %q, got: %s", valid, msg)
		}
	}
}

func TestProvidersConfig_Validate_PermissionMode_NonClaudeIgnored(t *testing.T) {
	t.Parallel()
	// Setting permission_mode on non-claude-cli drivers is forward-compatible
	// (silently ignored at validation time).
	cfg := ProvidersConfig{
		Version: "1",
		Providers: []ProviderConfig{
			{Name: "cursor", Driver: DriverCursorCLI, PermissionMode: "anything-goes"},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() should ignore permission_mode for non-claude driver, got: %v", err)
	}
}

func TestProvidersConfig_PermissionMode_YAMLRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	yaml := `version: "1"
providers:
  - name: claude
    driver: claude-cli
    permission_mode: acceptEdits
`
	path := writeYAML(t, dir, ProvidersConfigFile, yaml)
	cfg, err := LoadProvidersConfig(path)
	if err != nil {
		t.Fatalf("LoadProvidersConfig: %v", err)
	}
	if got := cfg.Providers[0].PermissionMode; got != "acceptEdits" {
		t.Errorf("PermissionMode = %q, want %q", got, "acceptEdits")
	}
}

// --- Story 62.1: codex sandbox_mode validation ---

func TestProvidersConfig_Validate_CodexSandboxMode_Valid(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"read-only", "workspace-write", "danger-full-access"} {
		t.Run(mode, func(t *testing.T) {
			cfg := ProvidersConfig{
				Version: "1",
				Providers: []ProviderConfig{
					{Name: "codex", Driver: DriverCodexCLI, SandboxMode: mode},
				},
			}
			if err := cfg.Validate(); err != nil {
				t.Errorf("Validate() unexpected error for sandbox_mode=%q: %v", mode, err)
			}
		})
	}
}

func TestProvidersConfig_Validate_CodexSandboxMode_Invalid(t *testing.T) {
	t.Parallel()
	cfg := ProvidersConfig{
		Version: "1",
		Providers: []ProviderConfig{
			{Name: "codex", Driver: DriverCodexCLI, SandboxMode: "danger_full_access"},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for invalid sandbox_mode, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "danger_full_access") {
		t.Errorf("error should echo invalid value, got: %s", msg)
	}
	for _, valid := range []string{"read-only", "workspace-write", "danger-full-access"} {
		if !strings.Contains(msg, valid) {
			t.Errorf("error should list valid value %q, got: %s", valid, msg)
		}
	}
}

func TestProvidersConfig_Validate_CodexSandboxMode_NonCodexIgnored(t *testing.T) {
	t.Parallel()
	cfg := ProvidersConfig{
		Version: "1",
		Providers: []ProviderConfig{
			{Name: "claude", Driver: DriverClaudeCLI, SandboxMode: "not-a-codex-mode"},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() should ignore sandbox_mode for non-codex driver, got: %v", err)
	}
}

func TestProvidersConfig_CodexSandboxMode_YAMLRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	yaml := `version: "1"
providers:
  - name: codex
    driver: codex-cli
    sandbox_mode: danger-full-access
`
	path := writeYAML(t, dir, ProvidersConfigFile, yaml)
	cfg, err := LoadProvidersConfig(path)
	if err != nil {
		t.Fatalf("LoadProvidersConfig: %v", err)
	}
	if got := cfg.Providers[0].SandboxMode; got != "danger-full-access" {
		t.Errorf("SandboxMode = %q, want %q", got, "danger-full-access")
	}
}

func TestLoadProvidersConfig_CodexSandboxMode_Invalid(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	yaml := `version: "1"
providers:
  - name: codex
    driver: codex-cli
    sandbox_mode: yolo
`
	path := writeYAML(t, dir, ProvidersConfigFile, yaml)

	_, err := LoadProvidersConfig(path)
	if err == nil {
		t.Fatal("expected LoadProvidersConfig to reject invalid sandbox_mode, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "sandbox_mode") || !strings.Contains(msg, "yolo") {
		t.Errorf("error should mention invalid sandbox_mode value, got: %s", msg)
	}
	for _, valid := range []string{"read-only", "workspace-write", "danger-full-access"} {
		if !strings.Contains(msg, valid) {
			t.Errorf("error should list valid value %q, got: %s", valid, msg)
		}
	}
}

func TestProvidersConfig_Validate_SandboxAndPermissionModeOrthogonal(t *testing.T) {
	t.Parallel()
	cfg := ProvidersConfig{
		Version: "1",
		Providers: []ProviderConfig{
			{Name: "claude", Driver: DriverClaudeCLI, PermissionMode: "plan", SandboxMode: "not-a-codex-mode"},
			{Name: "codex", Driver: DriverCodexCLI, PermissionMode: "not-a-claude-mode", SandboxMode: "read-only"},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() should gate permission_mode and sandbox_mode by driver independently, got: %v", err)
	}
}

func TestParseProvidersConfig_WithCommand(t *testing.T) {
	t.Parallel()
	yaml := `version: "1"
providers:
  - name: claude
    driver: claude-cli
    command: /usr/local/bin/claude
  - name: cursor
    driver: cursor-cli
    command: cursor-agent
`
	cfg, err := ParseProvidersConfig([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Providers[0].Command != "/usr/local/bin/claude" {
		t.Errorf("expected command %q, got %q", "/usr/local/bin/claude", cfg.Providers[0].Command)
	}
	if cfg.Providers[1].Command != "cursor-agent" {
		t.Errorf("expected command %q, got %q", "cursor-agent", cfg.Providers[1].Command)
	}
}
