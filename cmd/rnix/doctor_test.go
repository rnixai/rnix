package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/rnixai/rnix/drivers/llm"
	"github.com/spf13/cobra"
)

// TestDoctor_IsCLIDriver sanity-checks the driver classifier used to decide
// whether to run command resolvability checks.
func TestDoctor_IsCLIDriver(t *testing.T) {
	cases := map[string]bool{
		llm.DriverClaudeCLI:    true,
		llm.DriverCursorCLI:    true,
		llm.DriverQwenCLI:      true,
		llm.DriverCodexCLI:     true,
		llm.DriverOpenAI:       false,
		llm.DriverGemini:       false,
		llm.DriverAnthropic:    false,
	}
	for driver, want := range cases {
		if got := isCLIDriver(driver); got != want {
			t.Errorf("isCLIDriver(%q) = %v, want %v", driver, got, want)
		}
	}
}

// TestDoctor_DefaultCommandFor verifies every CLI driver has a known default
// binary name so `doctor` can still report missing-command errors when the
// user's providers.yaml omits the `command` field.
func TestDoctor_DefaultCommandFor(t *testing.T) {
	cliDrivers := []string{
		llm.DriverClaudeCLI,
		llm.DriverCursorCLI,
		llm.DriverQwenCLI,
		llm.DriverCodexCLI,
	}
	for _, d := range cliDrivers {
		if got := defaultCommandFor(d); got == "" {
			t.Errorf("defaultCommandFor(%q) returned empty; every CLI driver must have a default", d)
		}
	}
}

// TestDoctor_CheckProviderAuth_ClaudeSubscription exercises the subscription
// detection branch when no ANTHROPIC_API_KEY / Bedrock vars are set.
func TestDoctor_CheckProviderAuth_ClaudeSubscription(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("CLAUDE_CODE_USE_BEDROCK", "")
	t.Setenv("ANTHROPIC_BEDROCK_BASE_URL", "")

	checks := checkProviderAuth(llm.ProviderConfig{Name: "claude", Driver: llm.DriverClaudeCLI})
	if len(checks) != 1 {
		t.Fatalf("expected 1 auth check, got %d", len(checks))
	}
	if checks[0].Code != "auth_subscription_possible" {
		t.Errorf("code = %q, want auth_subscription_possible", checks[0].Code)
	}
}

// TestDoctor_CheckProviderAuth_ClaudeBedrock confirms Bedrock vars flip the
// classification to "metered_api" mode.
func TestDoctor_CheckProviderAuth_ClaudeBedrock(t *testing.T) {
	t.Setenv("CLAUDE_CODE_USE_BEDROCK", "1")
	t.Setenv("ANTHROPIC_API_KEY", "")

	checks := checkProviderAuth(llm.ProviderConfig{Name: "claude-bedrock", Driver: llm.DriverClaudeCLI})
	if len(checks) != 1 || checks[0].Code != "auth_bedrock" {
		t.Fatalf("expected auth_bedrock, got %+v", checks)
	}
}

// TestDoctor_CheckProviderAuth_APIKeyUnset surfaces a FAIL when an API-only
// driver (e.g. openai) has its api_key_env variable unset.
func TestDoctor_CheckProviderAuth_APIKeyUnset(t *testing.T) {
	t.Setenv("MY_KEY_FOR_TEST", "")
	checks := checkProviderAuth(llm.ProviderConfig{
		Name:      "ollama-test",
		Driver:    llm.DriverOpenAI,
		APIKeyEnv: "MY_KEY_FOR_TEST",
	})
	if len(checks) != 1 || checks[0].Code != "auth_api_key_unset" {
		t.Fatalf("expected auth_api_key_unset, got %+v", checks)
	}
	if checks[0].Level != checkError {
		t.Errorf("expected level=error, got %q", checks[0].Level)
	}
}

// TestDoctor_RunDoctor_NoProbe runs the CLI end-to-end with --provider targeting
// a nonexistent provider so no checks run; verifies the command exits cleanly
// and emits a report.
func TestDoctor_RunDoctor_NoProbe(t *testing.T) {
	// isolate env so the assertion is stable
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	savedJSON := flagJSON
	defer func() { flagJSON = savedJSON }()
	flagJSON = false

	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetContext(context.Background())

	exitCode = 0
	if err := runDoctor(cmd, nil); err != nil {
		t.Fatalf("runDoctor error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "rnix doctor") {
		t.Errorf("expected header in output, got:\n%s", out)
	}
	if !strings.Contains(out, "overall:") {
		t.Errorf("expected overall summary, got:\n%s", out)
	}
}

// TestDoctor_RunHelloProbe_BuildFailure confirms a broken provider config
// (unknown driver → CreateDriver error) shows up as a probe build failure.
func TestDoctor_RunHelloProbe_BuildFailure(t *testing.T) {
	result := runHelloProbe(context.Background(), llm.ProviderConfig{
		Name:   "bad",
		Driver: "not-a-real-driver",
	})
	if result.Code != "probe_driver_build_failed" {
		t.Errorf("code = %q, want probe_driver_build_failed", result.Code)
	}
	if result.Level != checkError {
		t.Errorf("level = %q, want error", result.Level)
	}
}
