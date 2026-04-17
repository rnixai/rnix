package llm

import (
	"os/exec"
	"testing"
	"time"
)

// TestConfigureCommandGrace_DefaultWhenZero verifies that passing graceSec<=0
// falls back to DefaultGracePeriod on the underlying cmd.
func TestConfigureCommandGrace_DefaultWhenZero(t *testing.T) {
	t.Parallel()
	cmd := exec.Command("true")
	configureCommandGrace(cmd, 0)
	if cmd.WaitDelay != DefaultGracePeriod {
		t.Errorf("WaitDelay = %v, want %v (default)", cmd.WaitDelay, DefaultGracePeriod)
	}
	if cmd.Cancel == nil {
		t.Error("Cancel should be set to send SIGTERM")
	}
}

// TestConfigureCommandGrace_Custom verifies a positive graceSec translates to
// the exact WaitDelay on the cmd.
func TestConfigureCommandGrace_Custom(t *testing.T) {
	t.Parallel()
	cmd := exec.Command("true")
	configureCommandGrace(cmd, 7)
	if got, want := cmd.WaitDelay, 7*time.Second; got != want {
		t.Errorf("WaitDelay = %v, want %v", got, want)
	}
}

// TestConfigureCommandGrace_NilCmd verifies the function is safe on a nil cmd
// (e.g. when cmdBuilder returns nil in tests).
func TestConfigureCommandGrace_NilCmd(t *testing.T) {
	t.Parallel()
	configureCommandGrace(nil, 5) // must not panic
}

// TestCreateDriver_ClaudeCLI_TimeoutAndGrace verifies providers.yaml →
// driver options pipeline for the claude-cli driver.
func TestCreateDriver_ClaudeCLI_TimeoutAndGrace(t *testing.T) {
	t.Parallel()
	drv, err := CreateDriver(ProviderConfig{
		Name:       "claude",
		Driver:     DriverClaudeCLI,
		TimeoutSec: 42,
		GraceSec:   9,
	})
	if err != nil {
		t.Fatalf("CreateDriver: %v", err)
	}
	cli, ok := drv.(*ClaudeCliDriver)
	if !ok {
		t.Fatalf("expected *ClaudeCliDriver, got %T", drv)
	}
	if got, want := cli.defaultTimeout, 42*time.Second; got != want {
		t.Errorf("defaultTimeout = %v, want %v", got, want)
	}
	if cli.graceSec != 9 {
		t.Errorf("graceSec = %d, want 9", cli.graceSec)
	}
}

// TestCreateDriver_CursorCLI_TimeoutAndGrace confirms the same pipeline for
// cursor-cli; the driver is the primary non-claude CLI.
func TestCreateDriver_CursorCLI_TimeoutAndGrace(t *testing.T) {
	t.Parallel()
	drv, err := CreateDriver(ProviderConfig{
		Name:       "cursor",
		Driver:     DriverCursorCLI,
		TimeoutSec: 13,
		GraceSec:   4,
	})
	if err != nil {
		t.Fatalf("CreateDriver: %v", err)
	}
	cli, ok := drv.(*CursorCliDriver)
	if !ok {
		t.Fatalf("expected *CursorCliDriver, got %T", drv)
	}
	if got, want := cli.defaultTimeout, 13*time.Second; got != want {
		t.Errorf("defaultTimeout = %v, want %v", got, want)
	}
	if cli.graceSec != 4 {
		t.Errorf("graceSec = %d, want 4", cli.graceSec)
	}
}

// TestCreateDriver_QwenCLI_TimeoutAndGrace verifies the pipeline for qwen-cli.
func TestCreateDriver_QwenCLI_TimeoutAndGrace(t *testing.T) {
	t.Parallel()
	drv, err := CreateDriver(ProviderConfig{
		Name:       "qwen",
		Driver:     DriverQwenCLI,
		TimeoutSec: 60,
		GraceSec:   3,
	})
	if err != nil {
		t.Fatalf("CreateDriver: %v", err)
	}
	cli, ok := drv.(*QwenCliDriver)
	if !ok {
		t.Fatalf("expected *QwenCliDriver, got %T", drv)
	}
	if got, want := cli.defaultTimeout, 60*time.Second; got != want {
		t.Errorf("defaultTimeout = %v, want %v", got, want)
	}
	if cli.graceSec != 3 {
		t.Errorf("graceSec = %d, want 3", cli.graceSec)
	}
}

// TestCreateDriver_CodexCLI_TimeoutAndGrace verifies the pipeline for codex-cli.
func TestCreateDriver_CodexCLI_TimeoutAndGrace(t *testing.T) {
	t.Parallel()
	drv, err := CreateDriver(ProviderConfig{
		Name:       "codex",
		Driver:     DriverCodexCLI,
		TimeoutSec: 30,
		GraceSec:   2,
	})
	if err != nil {
		t.Fatalf("CreateDriver: %v", err)
	}
	cli, ok := drv.(*CodexCliDriver)
	if !ok {
		t.Fatalf("expected *CodexCliDriver, got %T", drv)
	}
	if got, want := cli.defaultTimeout, 30*time.Second; got != want {
		t.Errorf("defaultTimeout = %v, want %v", got, want)
	}
	if cli.graceSec != 2 {
		t.Errorf("graceSec = %d, want 2", cli.graceSec)
	}
}

// TestCreateDriver_DefaultsWhenUnconfigured verifies TimeoutSec=0 / GraceSec=0
// leaves driver defaults untouched.
func TestCreateDriver_DefaultsWhenUnconfigured(t *testing.T) {
	t.Parallel()
	drv, err := CreateDriver(ProviderConfig{
		Name:   "claude",
		Driver: DriverClaudeCLI,
	})
	if err != nil {
		t.Fatalf("CreateDriver: %v", err)
	}
	cli := drv.(*ClaudeCliDriver)
	if cli.defaultTimeout != DefaultTimeout {
		t.Errorf("defaultTimeout = %v, want default %v", cli.defaultTimeout, DefaultTimeout)
	}
	if cli.graceSec != 0 {
		t.Errorf("graceSec = %d, want 0 (driver falls back to DefaultGracePeriod)", cli.graceSec)
	}
}
