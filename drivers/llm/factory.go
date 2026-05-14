package llm

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/rnixai/rnix/vfs"
)

// DeviceRegisterer abstracts VFS device registration for testability.
// vfs.DeviceRegistry naturally satisfies this interface.
type DeviceRegisterer interface {
	Register(path string, factory vfs.VFSFileFactory) error
}

// CreateDriver creates an LLMDriver instance from a ProviderConfig.
// It uses os.Getenv to resolve API key environment variables.
func CreateDriver(cfg ProviderConfig) (LLMDriver, error) {
	return CreateDriverWithEnv(cfg, os.Getenv)
}

// CreateDriverWithEnv creates an LLMDriver instance from a ProviderConfig,
// using the provided envLookup function to resolve environment variables.
// This allows project-level .env overrides without polluting os.Environ.
func CreateDriverWithEnv(cfg ProviderConfig, envLookup func(string) string) (LLMDriver, error) {
	switch cfg.Driver {
	case DriverClaudeCLI:
		var opts []ClaudeCliOption
		if cfg.Command != "" {
			opts = append(opts, WithCommand(cfg.Command))
		}
		if cfg.DefaultModel != "" {
			opts = append(opts, WithModel(cfg.DefaultModel))
		}
		if len(cfg.ExtraArgs) > 0 {
			opts = append(opts, WithExtraArgs(cfg.ExtraArgs))
		}
		if cfg.TimeoutSec > 0 {
			opts = append(opts, WithTimeout(time.Duration(cfg.TimeoutSec)*time.Second))
		}
		if cfg.GraceSec > 0 {
			opts = append(opts, WithGrace(cfg.GraceSec))
		}
		if cfg.PermissionMode != "" {
			opts = append(opts, WithPermissionMode(cfg.PermissionMode))
		}
		return NewClaudeCliDriver(opts...), nil

	case DriverCursorCLI:
		var opts []CursorCliOption
		if cfg.Command != "" {
			opts = append(opts, CursorWithCommand(cfg.Command))
		}
		if cfg.DefaultModel != "" {
			opts = append(opts, CursorWithModel(cfg.DefaultModel))
		}
		if cfg.TimeoutSec > 0 {
			opts = append(opts, CursorWithTimeout(time.Duration(cfg.TimeoutSec)*time.Second))
		}
		if cfg.GraceSec > 0 {
			opts = append(opts, CursorWithGrace(cfg.GraceSec))
		}
		return NewCursorCliDriver(opts...), nil

	case DriverQwenCLI:
		var opts []QwenCliOption
		if cfg.Command != "" {
			opts = append(opts, QwenWithCommand(cfg.Command))
		}
		if cfg.DefaultModel != "" {
			opts = append(opts, QwenWithModel(cfg.DefaultModel))
		}
		if len(cfg.ExtraArgs) > 0 {
			opts = append(opts, QwenWithExtraArgs(cfg.ExtraArgs))
		}
		if cfg.TimeoutSec > 0 {
			opts = append(opts, QwenWithTimeout(time.Duration(cfg.TimeoutSec)*time.Second))
		}
		if cfg.GraceSec > 0 {
			opts = append(opts, QwenWithGrace(cfg.GraceSec))
		}
		return NewQwenCliDriver(opts...), nil

	case DriverCodexCLI:
		var opts []CodexCliOption
		if cfg.Command != "" {
			opts = append(opts, CodexWithCommand(cfg.Command))
		}
		if cfg.DefaultModel != "" {
			opts = append(opts, CodexWithModel(cfg.DefaultModel))
		}
		if len(cfg.ExtraArgs) > 0 {
			opts = append(opts, CodexWithExtraArgs(cfg.ExtraArgs))
		}
		if cfg.TimeoutSec > 0 {
			opts = append(opts, CodexWithTimeout(time.Duration(cfg.TimeoutSec)*time.Second))
		}
		if cfg.GraceSec > 0 {
			opts = append(opts, CodexWithGrace(cfg.GraceSec))
		}
		return NewCodexCliDriver(opts...), nil

	case DriverOpenAICompat:
		var opts []CompatOption
		if cfg.DefaultModel != "" {
			opts = append(opts, WithCompatModel(cfg.DefaultModel))
		}
		if cfg.MaxTokens > 0 {
			opts = append(opts, WithCompatMaxTokens(cfg.MaxTokens))
		}
		if cfg.APIKeyEnv != "" {
			if key := envLookup(cfg.APIKeyEnv); key != "" {
				opts = append(opts, WithAPIKey(key))
			} else {
				log.Printf("[llm] warning: provider %q: API key env var %s not set", cfg.Name, cfg.APIKeyEnv)
			}
		}
		if cfg.TimeoutSec > 0 {
			opts = append(opts, WithCompatTimeout(time.Duration(cfg.TimeoutSec)*time.Second))
		}
		if cfg.ThinkingBudget > 0 {
			opts = append(opts, WithCompatThinkingBudget(cfg.ThinkingBudget))
		}
		return NewOpenAICompatDriver(cfg.Name, cfg.BaseURL, opts...), nil

	case DriverOpenAI:
		var opts []OpenAIOption
		if cfg.DefaultModel != "" {
			opts = append(opts, WithOpenAIModel(cfg.DefaultModel))
		}
		if cfg.MaxTokens > 0 {
			opts = append(opts, WithOpenAIMaxTokens(cfg.MaxTokens))
		}
		if cfg.BaseURL != "" {
			opts = append(opts, WithOpenAIBaseURL(cfg.BaseURL))
			// The openai_official driver targets the openai-go SDK and does
			// NOT round-trip reasoning_content. Endpoints that return it
			// (DeepSeek thinking, GLM, Qwen reasoner, OpenRouter thinking)
			// will reject multi-turn assistant turns with HTTP 400 once the
			// reasoning_content is dropped. Use driver=openai_compat there.
			if !isLikelyOpenAIOfficial(cfg.BaseURL) {
				log.Printf("[llm] warning: provider %q: driver=openai with non-official BaseURL %q. "+
					"If the upstream returns reasoning_content (DeepSeek/Qwen-thinker/GLM thinking), "+
					"switch to driver=openai_compat to avoid HTTP 400 on multi-turn round-trip.",
					cfg.Name, cfg.BaseURL)
			}
		}
		if cfg.APIKeyEnv != "" {
			if key := envLookup(cfg.APIKeyEnv); key != "" {
				opts = append(opts, WithOpenAIKey(key))
			} else {
				log.Printf("[llm] warning: provider %q: API key env var %s not set", cfg.Name, cfg.APIKeyEnv)
			}
		}
		if cfg.TimeoutSec > 0 {
			opts = append(opts, WithOpenAITimeout(time.Duration(cfg.TimeoutSec)*time.Second))
		}
		return NewOpenAIDriver(cfg.Name, opts...), nil

	case DriverGemini:
		var opts []GeminiOption
		if cfg.DefaultModel != "" {
			opts = append(opts, WithGeminiModel(cfg.DefaultModel))
		}
		if cfg.ThinkingBudget > 0 {
			opts = append(opts, WithGeminiThinkingBudget(cfg.ThinkingBudget))
		}
		if cfg.APIKeyEnv != "" {
			if key := envLookup(cfg.APIKeyEnv); key != "" {
				opts = append(opts, WithGeminiAPIKey(key))
			} else {
				log.Printf("[llm] warning: provider %q: API key env var %s not set", cfg.Name, cfg.APIKeyEnv)
			}
		}
		if cfg.TimeoutSec > 0 {
			opts = append(opts, WithGeminiTimeout(time.Duration(cfg.TimeoutSec)*time.Second))
		}
		return NewGeminiDriver(cfg.Name, opts...), nil

	case DriverAnthropic:
		var opts []AnthropicOption
		if cfg.DefaultModel != "" {
			opts = append(opts, WithAnthropicModel(cfg.DefaultModel))
		}
		if cfg.MaxTokens > 0 {
			opts = append(opts, WithAnthropicMaxTokens(cfg.MaxTokens))
		}
		if cfg.ThinkingBudget > 0 {
			opts = append(opts, WithAnthropicThinkingBudget(cfg.ThinkingBudget))
		}
		if cfg.BaseURL != "" {
			opts = append(opts, WithAnthropicBaseURL(cfg.BaseURL))
		}
		if cfg.APIKeyEnv != "" {
			if key := envLookup(cfg.APIKeyEnv); key != "" {
				opts = append(opts, WithAnthropicKey(key))
			} else {
				log.Printf("[llm] warning: provider %q: API key env var %s not set", cfg.Name, cfg.APIKeyEnv)
			}
		}
		if cfg.TimeoutSec > 0 {
			opts = append(opts, WithAnthropicTimeout(time.Duration(cfg.TimeoutSec)*time.Second))
		}
		return NewAnthropicDriver(cfg.Name, opts...), nil

	default:
		return nil, fmt.Errorf("unsupported driver type: %q", cfg.Driver)
	}
}

// isLikelyOpenAIOfficial returns true when baseURL points at an OpenAI
// official endpoint (api.openai.com or its known regional variants). This is
// a best-effort heuristic used only to silence the openai_compat suggestion
// warning — false negatives are acceptable (the warning is informational, not
// an error) and false positives only suppress the warning.
func isLikelyOpenAIOfficial(baseURL string) bool {
	u := strings.ToLower(baseURL)
	return strings.Contains(u, "api.openai.com") || strings.Contains(u, "openai.azure.com")
}

// RegisterProviders creates driver instances from config and registers them
// in both the DriverRegistry and VFS DeviceRegistry.
// It uses fail-fast: any error stops registration immediately.
func RegisterProviders(cfg *ProvidersConfig, driverReg *DriverRegistry, devReg DeviceRegisterer) error {
	for _, pc := range cfg.Providers {
		driver, err := CreateDriver(pc)
		if err != nil {
			return fmt.Errorf("provider %q: %w", pc.Name, err)
		}

		if err := driverReg.Register(pc.Name, driver); err != nil {
			return fmt.Errorf("provider %q: driver registry: %w", pc.Name, err)
		}

		vfsPath := "/dev/llm/" + pc.Name
		if err := devReg.Register(vfsPath, FileFactory(driver, vfsPath, pc.Mode)); err != nil {
			return fmt.Errorf("provider %q: device registry: %w", pc.Name, err)
		}
	}

	names := driverReg.Names()
	var parts []string
	for _, n := range names {
		parts = append(parts, n+" → /dev/llm/"+n)
	}
	log.Printf("[llm] registered %d providers: %s", len(names), strings.Join(parts, ", "))

	return nil
}

// RunHealthChecks performs async health checks on all registered providers
// that implement the HealthChecker interface. CLI-based drivers are marked unchecked.
// The function returns immediately; health checks run in background goroutines.
// timeout is the per-provider health check deadline.
func RunHealthChecks(cfg *ProvidersConfig, reg *DriverRegistry, timeout time.Duration) {
	for _, pc := range cfg.Providers {
		drv, ok := reg.Get(pc.Name)
		if !ok {
			continue
		}
		hc, ok := drv.(HealthChecker)
		if !ok {
			// CLI drivers (claude-cli, cursor-cli) don't support health check
			continue
		}
		go func(name string, checker HealthChecker) {
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			if err := checker.HealthCheck(ctx); err != nil {
				reg.SetHealth(name, HealthStatusUnhealthy)
				log.Printf("[llm] provider %q: health check failed: %v", name, err)
			} else {
				reg.SetHealth(name, HealthStatusHealthy)
				log.Printf("[llm] provider %q: healthy", name)
			}
		}(pc.Name, hc)
	}
}
