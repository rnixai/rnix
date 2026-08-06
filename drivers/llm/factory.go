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
	if cfg.SandboxMode != "" && cfg.Driver != DriverCodexCLI {
		log.Printf("[llm] warning: provider %q (%s): sandbox_mode=%q ignored — "+
			"sandbox_mode applies only to codex-cli",
			cfg.Name, cfg.Driver, cfg.SandboxMode)
	}

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
		if cfg.ReasoningEffort != "" {
			opts = append(opts, WithClaudeEffort(cfg.ReasoningEffort))
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
		// Cursor CLI has no standalone effort parameter — the thinking level is
		// bound to the model-name suffix (e.g. "sonnet-4.5-thinking-high"). We do
		// NOT fake support: reasoning_effort is a no-op here, with a warning so
		// the user knows to express it via the model name instead.
		if cfg.ReasoningEffort != "" {
			log.Printf("[llm] warning: provider %q (cursor-cli): reasoning_effort=%q ignored — "+
				"Cursor CLI expresses thinking level via the model-name suffix "+
				"(e.g. default_model: sonnet-4.5-thinking-high), not a separate effort flag",
				cfg.Name, cfg.ReasoningEffort)
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
		// Qwen3-Coder has no reasoning-effort concept (non-thinking only). We do
		// NOT fake support: reasoning_effort is a no-op here, with a warning.
		if cfg.ReasoningEffort != "" {
			log.Printf("[llm] warning: provider %q (qwen-cli): reasoning_effort=%q ignored — "+
				"Qwen3-Coder has no reasoning-effort concept (non-thinking only)",
				cfg.Name, cfg.ReasoningEffort)
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
		if cfg.ReasoningEffort != "" {
			opts = append(opts, CodexWithReasoningEffort(cfg.ReasoningEffort))
		}
		if cfg.SandboxMode != "" {
			opts = append(opts, CodexWithSandboxMode(cfg.SandboxMode))
			if cfg.SandboxMode == "danger-full-access" {
				log.Printf("[llm] warning: provider %q (codex-cli): sandbox_mode=danger-full-access disables Codex sandboxing; use only for trusted projects/worktrees",
					cfg.Name)
			}
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
		if cfg.ReasoningEffort != "" {
			opts = append(opts, WithCompatReasoningEffort(cfg.ReasoningEffort))
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
			// Endpoints that return reasoning (DeepSeek V4, GLM, Qwen-thinker,
			// OpenRouter thinking) WORK on this driver — a 2026-08-04 probe
			// confirmed deepseek-v4-flash returns 200 on multi-turn requests,
			// including tool calls, with reasoning_content omitted. What breaks
			// is not the request but the observability: convertCompletion only
			// fills Content/ToolCalls, so LLMResponse.Reasoning stays empty, no
			// "reasoning" stream events reach the dashboard, and the model loses
			// sight of its own prior thinking. The same probe showed the SDK CAN
			// carry the field (option.WithJSONSet to write,
			// msg.JSON.ExtraFields to read, both streaming and not) — this
			// driver simply does not yet do it. Until it does, use
			// driver=openai-compat when the upstream emits reasoning.
			if !isLikelyOpenAIOfficial(cfg.BaseURL) {
				log.Printf("[llm] warning: provider %q: driver=openai with non-official BaseURL %q. "+
					"If the upstream returns reasoning (DeepSeek/Qwen-thinker/GLM/OpenRouter thinking), "+
					"this driver silently DROPS it — requests succeed but reasoning is lost. "+
					"Use driver=openai-compat to retain it.",
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
		if cfg.ReasoningEffort != "" {
			opts = append(opts, WithOpenAIReasoningEffort(cfg.ReasoningEffort))
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
		if cfg.ReasoningEffort != "" {
			opts = append(opts, WithGeminiThinkingLevel(cfg.ReasoningEffort))
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
		if cfg.ReasoningEffort != "" {
			opts = append(opts, WithAnthropicEffort(cfg.ReasoningEffort))
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

	case DriverReplay:
		var opts []ReplayOption
		if cfg.DefaultModel != "" {
			opts = append(opts, WithReplayDefaultScript(cfg.DefaultModel))
		}
		// Replay has no reasoning-effort concept (deterministic scripted
		// responses) — no-op with a warning, mirroring the cursor-cli/qwen-cli
		// pattern above.
		if cfg.ReasoningEffort != "" {
			log.Printf("[llm] warning: provider %q (replay): reasoning_effort=%q ignored — "+
				"replay has no reasoning-effort concept (deterministic scripted responses)",
				cfg.Name, cfg.ReasoningEffort)
		}
		return NewReplayDriver(cfg.Name, opts...), nil

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
