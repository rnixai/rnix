package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/rnixai/rnix/drivers/llm"
	"github.com/rnixai/rnix/internal/config"
	"github.com/spf13/cobra"
)

// checkLevel categorizes a doctor check result.
type checkLevel string

const (
	checkInfo  checkLevel = "info"  // passed / informational
	checkWarn  checkLevel = "warn"  // degraded but usable
	checkError checkLevel = "error" // blocking
	checkSkip  checkLevel = "skip"  // not applicable
)

// doctorCheck is a single check result, modelled after paperclip's
// AdapterEnvironmentCheck (packages/adapters/claude-local/src/server/test.ts).
type doctorCheck struct {
	Code    string     `json:"code"`
	Level   checkLevel `json:"level"`
	Message string     `json:"message"`
	Detail  string     `json:"detail,omitempty"`
	Hint    string     `json:"hint,omitempty"`
}

// doctorProviderResult groups all checks for a single provider.
type doctorProviderResult struct {
	Provider string        `json:"provider"`
	Driver   string        `json:"driver"`
	Status   string        `json:"status"` // pass | warn | fail
	Checks   []doctorCheck `json:"checks"`
}

// doctorReport is the full doctor output.
type doctorReport struct {
	Status    string                 `json:"status"` // pass | warn | fail
	Global    []doctorCheck          `json:"global"`
	Providers []doctorProviderResult `json:"providers"`
	TestedAt  string                 `json:"tested_at"`
}

var (
	doctorProbe    bool
	doctorProvider string
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Verify rnix environment (config, providers, CLI tools)",
	Long: `Run environment health checks across rnix configuration and each
configured LLM provider. Reports cwd, command resolvability, auth mode, and
(with --probe) a live "Respond with hello." probe per provider.

Modelled after paperclip's claude-local adapter test environment.`,
	Example: `  rnix doctor                    # static checks only
  rnix doctor --probe            # include live hello probe (billed)
  rnix doctor --provider claude  # check a single provider
  rnix doctor --json             # machine-readable output`,
	RunE: runDoctor,
}

func init() {
	doctorCmd.Flags().BoolVar(&doctorProbe, "probe", false, "Run a live hello probe per provider (small token usage; billed)")
	doctorCmd.Flags().StringVar(&doctorProvider, "provider", "", "Only check the named provider (matches providers.yaml)")
	rootCmd.AddCommand(doctorCmd)
}

func runDoctor(cmd *cobra.Command, _ []string) error {
	w := cmd.OutOrStdout()
	report := doctorReport{
		TestedAt: time.Now().UTC().Format(time.RFC3339),
	}

	// ----- global checks -----
	report.Global = append(report.Global, checkGlobalConfig()...)

	// Load providers (mirror main.go path)
	providersCfg, cfgCheck := loadProvidersForDoctor()
	if cfgCheck != nil {
		report.Global = append(report.Global, *cfgCheck)
	}

	// ----- per-provider checks -----
	if providersCfg != nil {
		for _, p := range providersCfg.Providers {
			if doctorProvider != "" && p.Name != doctorProvider {
				continue
			}
			report.Providers = append(report.Providers, checkProvider(cmd.Context(), p, doctorProbe))
		}
	}

	report.Status = summarizeStatus(report)

	if flagJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return err
		}
	} else {
		renderDoctorHuman(w, report)
	}

	switch report.Status {
	case "fail":
		exitCode = 1
	case "warn":
		exitCode = 2
	}
	return nil
}

// checkGlobalConfig verifies the global rnix config directory exists and is
// readable; reports the project .rnix/ directory as informational.
func checkGlobalConfig() []doctorCheck {
	var checks []doctorCheck
	globalDir, err := config.GlobalDir()
	if err != nil {
		checks = append(checks, doctorCheck{
			Code:    "global_dir_unresolvable",
			Level:   checkError,
			Message: "cannot resolve global config directory",
			Detail:  err.Error(),
		})
		return checks
	}
	if info, err := os.Stat(globalDir); err != nil {
		checks = append(checks, doctorCheck{
			Code:    "global_dir_missing",
			Level:   checkWarn,
			Message: "global config directory does not exist",
			Detail:  globalDir,
			Hint:    "Run `rnix init` to bootstrap global config.",
		})
	} else if !info.IsDir() {
		checks = append(checks, doctorCheck{
			Code:    "global_dir_not_directory",
			Level:   checkError,
			Message: "global config path exists but is not a directory",
			Detail:  globalDir,
		})
	} else {
		checks = append(checks, doctorCheck{
			Code:    "global_dir_ok",
			Level:   checkInfo,
			Message: "global config directory present",
			Detail:  globalDir,
		})
	}

	cwd, err := os.Getwd()
	if err == nil {
		if proj, _ := config.ProjectDir(cwd); proj != "" {
			checks = append(checks, doctorCheck{
				Code:    "project_dir_ok",
				Level:   checkInfo,
				Message: "project .rnix/ detected",
				Detail:  filepath.Join(proj, ".rnix"),
			})
		} else {
			checks = append(checks, doctorCheck{
				Code:    "project_dir_missing",
				Level:   checkInfo,
				Message: "no .rnix/ found in CWD ancestry (global config only)",
				Hint:    "Run `rnix init` in your project root to enable project-scoped config.",
			})
		}
	}
	return checks
}

// loadProvidersForDoctor loads providers.yaml from the global directory using
// the same path as main.go. Returns nil config + a fatal check on hard failure.
func loadProvidersForDoctor() (*llm.ProvidersConfig, *doctorCheck) {
	globalDir, err := config.GlobalDir()
	if err != nil {
		return nil, &doctorCheck{
			Code:    "providers_load_no_global",
			Level:   checkError,
			Message: "cannot resolve global config directory",
			Detail:  err.Error(),
		}
	}
	providersPath := filepath.Join(globalDir, "providers.yaml")
	if _, err := os.Stat(providersPath); err != nil {
		if os.IsNotExist(err) {
			return llm.DefaultProvidersConfig(), &doctorCheck{
				Code:    "providers_using_defaults",
				Level:   checkInfo,
				Message: "providers.yaml not found, using built-in defaults",
				Detail:  providersPath,
				Hint:    "Run `rnix init` to create a customizable providers.yaml.",
			}
		}
		return nil, &doctorCheck{
			Code:    "providers_stat_failed",
			Level:   checkError,
			Message: "cannot access providers.yaml",
			Detail:  err.Error(),
		}
	}
	cfg, err := llm.LoadProvidersConfig(providersPath)
	if err != nil {
		return nil, &doctorCheck{
			Code:    "providers_parse_failed",
			Level:   checkError,
			Message: "providers.yaml is invalid",
			Detail:  err.Error(),
			Hint:    "Fix the YAML syntax / fields and re-run `rnix doctor`.",
		}
	}
	return cfg, &doctorCheck{
		Code:    "providers_loaded",
		Level:   checkInfo,
		Message: fmt.Sprintf("providers.yaml loaded (%d provider(s))", len(cfg.Providers)),
		Detail:  providersPath,
	}
}

// checkProvider runs all applicable checks for a single provider.
func checkProvider(ctx context.Context, p llm.ProviderConfig, probe bool) doctorProviderResult {
	result := doctorProviderResult{
		Provider: p.Name,
		Driver:   p.Driver,
	}

	// 1. Command resolvability (CLI drivers only)
	if isCLIDriver(p.Driver) {
		cmd := p.Command
		if cmd == "" {
			cmd = defaultCommandFor(p.Driver)
		}
		if cmd == "" {
			result.Checks = append(result.Checks, doctorCheck{
				Code:    "command_unknown",
				Level:   checkError,
				Message: "no CLI command configured and no default known for this driver",
				Detail:  p.Driver,
			})
		} else if _, err := exec.LookPath(cmd); err != nil {
			result.Checks = append(result.Checks, doctorCheck{
				Code:    "command_unresolvable",
				Level:   checkError,
				Message: fmt.Sprintf("%q not found in PATH", cmd),
				Detail:  err.Error(),
				Hint:    "Install the CLI tool or adjust providers.yaml `command` field.",
			})
		} else {
			result.Checks = append(result.Checks, doctorCheck{
				Code:    "command_resolvable",
				Level:   checkInfo,
				Message: fmt.Sprintf("CLI command resolvable: %s", cmd),
			})
		}
	}

	// 2. Auth / API key mode
	result.Checks = append(result.Checks, checkProviderAuth(p)...)

	// 3. Live hello probe (opt-in)
	if probe {
		result.Checks = append(result.Checks, runHelloProbe(ctx, p))
	} else {
		result.Checks = append(result.Checks, doctorCheck{
			Code:    "probe_skipped",
			Level:   checkSkip,
			Message: "hello probe skipped (pass --probe to enable)",
		})
	}

	result.Status = providerStatus(result.Checks)
	return result
}

// isCLIDriver reports whether a driver type shells out to a local CLI binary.
func isCLIDriver(d string) bool {
	switch d {
	case llm.DriverClaudeCLI, llm.DriverCursorCLI, llm.DriverQwenCLI, llm.DriverCodexCLI:
		return true
	}
	return false
}

// defaultCommandFor returns the default CLI binary name for a given driver.
func defaultCommandFor(d string) string {
	switch d {
	case llm.DriverClaudeCLI:
		return "claude"
	case llm.DriverCursorCLI:
		return "cursor-agent"
	case llm.DriverQwenCLI:
		return "qwen"
	case llm.DriverCodexCLI:
		return "codex"
	}
	return ""
}

// checkProviderAuth identifies the auth mode per driver family. Adapted from
// paperclip's resolveClaudeBillingType + testEnvironment auth section.
func checkProviderAuth(p llm.ProviderConfig) []doctorCheck {
	var checks []doctorCheck
	switch p.Driver {
	case llm.DriverClaudeCLI:
		bedrock := os.Getenv("CLAUDE_CODE_USE_BEDROCK") == "1" ||
			strings.EqualFold(os.Getenv("CLAUDE_CODE_USE_BEDROCK"), "true") ||
			os.Getenv("ANTHROPIC_BEDROCK_BASE_URL") != ""
		hasAPIKey := os.Getenv("ANTHROPIC_API_KEY") != ""
		switch {
		case bedrock:
			checks = append(checks, doctorCheck{
				Code:    "auth_bedrock",
				Level:   checkInfo,
				Message: "AWS Bedrock auth detected",
				Hint:    "Ensure AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY (or AWS_PROFILE) and AWS_REGION are set.",
			})
		case hasAPIKey:
			checks = append(checks, doctorCheck{
				Code:    "auth_api_key",
				Level:   checkWarn,
				Message: "ANTHROPIC_API_KEY is set — API auth will be used instead of Claude CLI subscription login",
				Hint:    "Unset ANTHROPIC_API_KEY to use subscription login.",
			})
		default:
			checks = append(checks, doctorCheck{
				Code:    "auth_subscription_possible",
				Level:   checkInfo,
				Message: "ANTHROPIC_API_KEY not set — subscription auth can be used if `claude login` was run",
			})
		}
	case llm.DriverCursorCLI, llm.DriverQwenCLI, llm.DriverCodexCLI:
		checks = append(checks, doctorCheck{
			Code:    "auth_cli_delegated",
			Level:   checkInfo,
			Message: fmt.Sprintf("auth handled by %s CLI itself (run its login command if needed)", p.Driver),
		})
	case llm.DriverOpenAICompat, llm.DriverOpenAI, llm.DriverGemini, llm.DriverAnthropic:
		envVar := p.APIKeyEnv
		if envVar == "" {
			envVar = defaultAPIKeyEnvFor(p.Driver)
		}
		if envVar == "" {
			checks = append(checks, doctorCheck{
				Code:    "auth_api_key_env_missing",
				Level:   checkWarn,
				Message: "no api_key_env configured and no default known",
				Hint:    "Set providers.yaml `api_key_env` for this provider.",
			})
			break
		}
		if os.Getenv(envVar) == "" {
			checks = append(checks, doctorCheck{
				Code:    "auth_api_key_unset",
				Level:   checkError,
				Message: fmt.Sprintf("required environment variable %s is not set", envVar),
				Hint:    fmt.Sprintf("export %s=...  (or use .env / .env.{RNIX_ENV})", envVar),
			})
		} else {
			checks = append(checks, doctorCheck{
				Code:    "auth_api_key_present",
				Level:   checkInfo,
				Message: fmt.Sprintf("%s is set", envVar),
			})
		}
	}
	return checks
}

// defaultAPIKeyEnvFor returns the conventional env var for API-driver providers
// when the user didn't set api_key_env explicitly.
func defaultAPIKeyEnvFor(d string) string {
	switch d {
	case llm.DriverOpenAI, llm.DriverOpenAICompat:
		return "OPENAI_API_KEY"
	case llm.DriverGemini:
		return "GEMINI_API_KEY"
	case llm.DriverAnthropic:
		return "ANTHROPIC_API_KEY"
	}
	return ""
}

// runHelloProbe sends a minimal "Respond with hello." request through the
// driver and reports whether it round-trips. Mirrors paperclip's hello probe.
func runHelloProbe(ctx context.Context, p llm.ProviderConfig) doctorCheck {
	driver, err := llm.CreateDriver(p)
	if err != nil {
		return doctorCheck{
			Code:    "probe_driver_build_failed",
			Level:   checkError,
			Message: "cannot build driver for probe",
			Detail:  err.Error(),
		}
	}

	probeCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	resp, err := driver.Call(probeCtx, llm.LLMRequest{
		Intent:    "Respond with hello.",
		TimeoutMs: 45_000,
	})
	if err != nil {
		var llmErr *llm.LLMError
		if errors.As(err, &llmErr) && errors.Is(err, llm.ErrLoginRequired) {
			hint := "Run the provider's login command and retry."
			if url := llmErr.Meta["login_url"]; url != "" {
				hint = "Run `claude login` and complete sign-in at " + url
			}
			return doctorCheck{
				Code:    "probe_login_required",
				Level:   checkError,
				Message: "probe failed: login required",
				Detail:  err.Error(),
				Hint:    hint,
			}
		}
		return doctorCheck{
			Code:    "probe_failed",
			Level:   checkError,
			Message: "hello probe failed",
			Detail:  err.Error(),
			Hint:    "Run the provider's CLI manually with the same prompt to debug.",
		}
	}

	summary := strings.TrimSpace(resp.Content)
	detail := summary
	if len(detail) > 240 {
		detail = detail[:239] + "…"
	}
	if strings.Contains(strings.ToLower(summary), "hello") {
		return doctorCheck{
			Code:    "probe_passed",
			Level:   checkInfo,
			Message: "hello probe succeeded",
			Detail:  detail,
		}
	}
	return doctorCheck{
		Code:    "probe_unexpected_output",
		Level:   checkWarn,
		Message: "probe ran but response did not contain 'hello'",
		Detail:  detail,
		Hint:    "Provider may be returning unrelated text — verify the model and system prompt.",
	}
}

func providerStatus(checks []doctorCheck) string {
	hasErr, hasWarn := false, false
	for _, c := range checks {
		switch c.Level {
		case checkError:
			hasErr = true
		case checkWarn:
			hasWarn = true
		}
	}
	switch {
	case hasErr:
		return "fail"
	case hasWarn:
		return "warn"
	}
	return "pass"
}

func summarizeStatus(r doctorReport) string {
	hasErr, hasWarn := false, false
	for _, c := range r.Global {
		switch c.Level {
		case checkError:
			hasErr = true
		case checkWarn:
			hasWarn = true
		}
	}
	for _, pr := range r.Providers {
		switch pr.Status {
		case "fail":
			hasErr = true
		case "warn":
			hasWarn = true
		}
	}
	switch {
	case hasErr:
		return "fail"
	case hasWarn:
		return "warn"
	}
	return "pass"
}

// renderDoctorHuman prints a human-readable doctor report.
func renderDoctorHuman(w io.Writer, r doctorReport) {
	fmt.Fprintf(w, "rnix doctor — tested at %s\n\n", r.TestedAt)
	fmt.Fprintln(w, "[global]")
	for _, c := range r.Global {
		fmt.Fprintln(w, formatCheck(c))
	}
	fmt.Fprintln(w)

	for _, pr := range r.Providers {
		fmt.Fprintf(w, "[provider: %s (%s)] — %s\n", pr.Provider, pr.Driver, strings.ToUpper(pr.Status))
		for _, c := range pr.Checks {
			fmt.Fprintln(w, formatCheck(c))
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintf(w, "overall: %s\n", strings.ToUpper(r.Status))
}

func formatCheck(c doctorCheck) string {
	var marker string
	switch c.Level {
	case checkInfo:
		marker = "  ok  "
	case checkWarn:
		marker = " WARN "
	case checkError:
		marker = " FAIL "
	case checkSkip:
		marker = " skip "
	}
	line := fmt.Sprintf("%s %s", marker, c.Message)
	if c.Detail != "" {
		line += "\n         detail: " + c.Detail
	}
	if c.Hint != "" {
		line += "\n         hint:   " + c.Hint
	}
	return line
}
