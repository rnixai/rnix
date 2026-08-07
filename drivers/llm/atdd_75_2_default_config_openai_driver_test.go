package llm

import (
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

// Story 75.2: 内置默认配置 deepseek provider 已从 compat 迁移到统一
// openai driver（75.3 删除 compat driver 的前置）。E2E 链路：
// DefaultProvidersConfig → YAML round-trip → ParseProvidersConfig/Validate
// → CreateDriverWithEnv → RegisterProviders。

func TestATDD_75_2_DefaultConfig_DeepseekUsesOpenAIDriver(t *testing.T) {
	t.Parallel()
	cfg := DefaultProvidersConfig()

	if cfg.DefaultProvider != "deepseek" {
		t.Fatalf("DefaultProvider = %q, want %q", cfg.DefaultProvider, "deepseek")
	}

	var deepseek *ProviderConfig
	for i := range cfg.Providers {
		if cfg.Providers[i].Name == "deepseek" {
			deepseek = &cfg.Providers[i]
			break
		}
	}
	if deepseek == nil {
		t.Fatal("default config has no deepseek provider")
	}
	if deepseek.Driver != DriverOpenAI {
		t.Errorf("deepseek.Driver = %q, want %q (75.2 migration)", deepseek.Driver, DriverOpenAI)
	}
	// 其余字段在 75.2 不得变动（AC1）。
	if deepseek.BaseURL != "https://api.deepseek.com/v1" {
		t.Errorf("deepseek.BaseURL = %q, want %q", deepseek.BaseURL, "https://api.deepseek.com/v1")
	}
	if deepseek.APIKeyEnv != "DEEPSEEK_API_KEY" {
		t.Errorf("deepseek.APIKeyEnv = %q, want %q", deepseek.APIKeyEnv, "DEEPSEEK_API_KEY")
	}
	if deepseek.DefaultModel != "deepseek-v4-flash" {
		t.Errorf("deepseek.DefaultModel = %q, want %q", deepseek.DefaultModel, "deepseek-v4-flash")
	}
	if len(deepseek.Models) != 2 {
		t.Errorf("len(deepseek.Models) = %d, want 2", len(deepseek.Models))
	}
}

func TestATDD_75_2_DefaultConfig_RoundTripKeepsOpenAIDriver(t *testing.T) {
	t.Parallel()
	def := DefaultProvidersConfig()

	data, err := yaml.Marshal(def)
	if err != nil {
		t.Fatalf("yaml.Marshal(default config): %v", err)
	}
	parsed, err := ParseProvidersConfig(data)
	if err != nil {
		t.Fatalf("ParseProvidersConfig(round-tripped default): %v", err)
	}
	if parsed.DefaultProvider != "deepseek" {
		t.Errorf("DefaultProvider = %q, want %q", parsed.DefaultProvider, "deepseek")
	}
	for _, p := range parsed.Providers {
		if p.Name == "deepseek" && p.Driver != DriverOpenAI {
			t.Errorf("round-tripped deepseek.Driver = %q, want %q", p.Driver, DriverOpenAI)
		}
	}
}

func TestATDD_75_2_DefaultConfig_ValidatePasses(t *testing.T) {
	t.Parallel()
	// DriverOpenAI 无 base_url 必填要求；内置默认配置本身必须可校验通过
	// （75.2 迁移后不能破坏开箱即用）。
	if err := DefaultProvidersConfig().Validate(); err != nil {
		t.Errorf("DefaultProvidersConfig().Validate() = %v, want nil", err)
	}
}

func TestATDD_75_2_Factory_CreatesOpenAIDriverForDeepseek(t *testing.T) {
	t.Parallel()
	cfg := DefaultProvidersConfig()
	var pc ProviderConfig
	for _, p := range cfg.Providers {
		if p.Name == "deepseek" {
			pc = p
			break
		}
	}

	drv, err := CreateDriverWithEnv(pc, func(env string) string {
		if env == "DEEPSEEK_API_KEY" {
			return "test-key"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("CreateDriverWithEnv(deepseek): %v", err)
	}
	oai, ok := drv.(*OpenAIDriver)
	if !ok {
		t.Fatalf("driver type = %T, want *OpenAIDriver (75.3 删除 compat 后 deepseek 必须由 openai driver 服务)", drv)
	}
	info := oai.Info()
	if info.DriverType != DriverOpenAI {
		t.Errorf("Info().DriverType = %q, want %q", info.DriverType, DriverOpenAI)
	}
	if info.Name != "deepseek" {
		t.Errorf("Info().Name = %q, want %q", info.Name, "deepseek")
	}
	if info.DefaultModel != "deepseek-v4-flash" {
		t.Errorf("Info().DefaultModel = %q, want %q", info.DefaultModel, "deepseek-v4-flash")
	}
}

func TestATDD_75_2_RegisterProviders_DeepseekIsOpenAI(t *testing.T) {
	t.Parallel()
	cfg := DefaultProvidersConfig()
	driverReg := NewDriverRegistry()
	devReg := newMockDeviceRegisterer()

	if err := RegisterProviders(cfg, driverReg, devReg); err != nil {
		t.Fatalf("RegisterProviders(default config): %v", err)
	}
	if !devReg.registered["/dev/llm/deepseek"] {
		t.Errorf("expected /dev/llm/deepseek registered, got %v", devReg.registered)
	}
	drv, ok := driverReg.Get("deepseek")
	if !ok {
		t.Fatal("deepseek not in driver registry")
	}
	if _, isOpenAI := drv.(*OpenAIDriver); !isOpenAI {
		t.Errorf("deepseek registered driver type = %T, want *OpenAIDriver", drv)
	}
}

func TestATDD_75_2_OpenAIDriverNoBaseURLRequired(t *testing.T) {
	t.Parallel()
	// 迁移后的默认语义：openai driver 允许缺 base_url（官方端点），
	// 与 compat 的必填校验相反 —— 这是 75.3 删除 compat driver 前的关键差异。
	cfg := ProvidersConfig{
		Version: "1",
		Providers: []ProviderConfig{
			{Name: "ds", Driver: DriverOpenAI, DefaultModel: "deepseek-v4-flash"},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil (openai driver without base_url is legal)", err)
	}
	if _, err := CreateDriverWithEnv(cfg.Providers[0], func(string) string { return "" }); err != nil {
		t.Errorf("CreateDriverWithEnv(openai without base_url): %v, want nil", err)
	}
}

func TestATDD_75_2_InvalidDriverStillRejected(t *testing.T) {
	t.Parallel()
	// 迁移不能放宽 driver 白名单校验（回归守卫）。
	cfg := ProvidersConfig{
		Version: "1",
		Providers: []ProviderConfig{
			{Name: "bad", Driver: "openai-typo"},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want error for invalid driver")
	}
	if !strings.Contains(err.Error(), "invalid driver") {
		t.Errorf("error = %q, want mention of invalid driver", err.Error())
	}
}
