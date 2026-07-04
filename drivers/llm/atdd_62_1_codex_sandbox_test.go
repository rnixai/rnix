package llm

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

// Story 62.1 Task 1 — providers.yaml sandbox_mode must reach the codex driver
// instance, and unsafe/no-op configurations must be visible at construction.

func TestATDD_62_1_Task1_CodexSandboxMode_Option(t *testing.T) {
	t.Parallel()
	d := NewCodexCliDriver(CodexWithSandboxMode("read-only"))
	if d.sandboxMode != "read-only" {
		t.Errorf("sandboxMode = %q, want read-only", d.sandboxMode)
	}
}

func TestATDD_62_1_Task1_CodexSandboxMode_FactoryPipeline(t *testing.T) {
	t.Parallel()
	drv := mustCreateDriver(t, ProviderConfig{
		Name:        "codex",
		Driver:      DriverCodexCLI,
		SandboxMode: "danger-full-access",
	})
	codex, ok := drv.(*CodexCliDriver)
	if !ok {
		t.Fatalf("CreateDriver returned %T, want *CodexCliDriver", drv)
	}
	if codex.sandboxMode != "danger-full-access" {
		t.Errorf("sandboxMode = %q, want danger-full-access", codex.sandboxMode)
	}
}

func TestATDD_62_1_Task1_CodexSandboxMode_DangerWarning(t *testing.T) {
	got := captureFactoryLog(t, ProviderConfig{
		Name:        "codex",
		Driver:      DriverCodexCLI,
		SandboxMode: "danger-full-access",
	})
	if !strings.Contains(got, "codex-cli") || !strings.Contains(got, "sandbox_mode") ||
		!strings.Contains(got, "danger-full-access") {
		t.Errorf("expected codex-cli danger-full-access sandbox warning, got: %q", got)
	}
}

func TestATDD_62_1_Task1_CodexSandboxMode_NonCodexWarning(t *testing.T) {
	got := captureFactoryLog(t, ProviderConfig{
		Name:        "claude",
		Driver:      DriverClaudeCLI,
		SandboxMode: "danger-full-access",
	})
	if !strings.Contains(got, "claude-cli") || !strings.Contains(got, "sandbox_mode") ||
		!strings.Contains(got, "ignored") {
		t.Errorf("expected non-codex sandbox_mode ignored warning, got: %q", got)
	}
}

func TestATDD_62_1_Task2_CodexSandboxMode_ConfiguredCallAndStreamArgs(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"read-only", "workspace-write", "danger-full-access"} {
		t.Run(mode+"/call", func(t *testing.T) {
			t.Parallel()
			var capturedArgs []string
			d := NewCodexCliDriver(
				CodexWithSandboxMode(mode),
				CodexWithCommandBuilder(func(ctx context.Context, name string, args ...string) *exec.Cmd {
					capturedArgs = args
					return codexMockCmdBuilder("codex_call_success")(ctx, name, args...)
				}),
			)
			if _, err := d.Call(context.Background(), LLMRequest{Intent: "sandbox call"}); err != nil {
				t.Fatalf("Call: %v", err)
			}
			joined := strings.Join(capturedArgs, " ")
			if !argsContainPair(capturedArgs, "--sandbox", mode) {
				t.Errorf("expected --sandbox %s, got: %s", mode, joined)
			}
			if strings.Contains(joined, "--full-auto") {
				t.Errorf("unexpected --full-auto, got: %s", joined)
			}
		})

		t.Run(mode+"/stream", func(t *testing.T) {
			t.Parallel()
			var capturedArgs []string
			d := NewCodexCliDriver(
				CodexWithSandboxMode(mode),
				CodexWithCommandBuilder(func(ctx context.Context, name string, args ...string) *exec.Cmd {
					capturedArgs = args
					return codexMockCmdBuilder("codex_stream_success")(ctx, name, args...)
				}),
			)
			ch, err := d.Stream(context.Background(), LLMRequest{Intent: "sandbox stream"})
			if err != nil {
				t.Fatalf("Stream: %v", err)
			}
			for range ch {
			}
			joined := strings.Join(capturedArgs, " ")
			if !argsContainPair(capturedArgs, "--sandbox", mode) {
				t.Errorf("expected --sandbox %s, got: %s", mode, joined)
			}
			if strings.Contains(joined, "--full-auto") {
				t.Errorf("unexpected --full-auto, got: %s", joined)
			}
		})
	}
}

func TestATDD_62_1_Task2_CodexSandboxMode_DefaultAndOrder(t *testing.T) {
	t.Parallel()
	var capturedArgs []string
	const prompt = "order sentinel prompt"
	d := NewCodexCliDriver(
		CodexWithExtraArgs([]string{"--sentinel-extra"}),
		CodexWithCommandBuilder(func(ctx context.Context, name string, args ...string) *exec.Cmd {
			capturedArgs = args
			return codexMockCmdBuilder("codex_call_success")(ctx, name, args...)
		}),
	)
	if _, err := d.Call(context.Background(), LLMRequest{Intent: prompt}); err != nil {
		t.Fatalf("Call: %v", err)
	}

	joined := strings.Join(capturedArgs, " ")
	if !argsContainPair(capturedArgs, "--sandbox", "workspace-write") {
		t.Fatalf("expected default --sandbox workspace-write, got: %s", joined)
	}
	if strings.Contains(joined, "--full-auto") {
		t.Fatalf("unexpected --full-auto, got: %s", joined)
	}
	sandboxIdx := indexOf(capturedArgs, "--sandbox")
	extraIdx := indexOf(capturedArgs, "--sentinel-extra")
	if sandboxIdx < 0 || extraIdx < 0 {
		t.Fatalf("missing flags: %s", joined)
	}
	if sandboxIdx > extraIdx {
		t.Errorf("--sandbox (%d) must precede extraArgs (%d): %s", sandboxIdx, extraIdx, joined)
	}
	if capturedArgs[len(capturedArgs)-1] != prompt {
		t.Errorf("prompt must be the trailing arg, got args: %s", joined)
	}
}

func TestATDD_62_1_Task3_CodexRawCapture_ArgvContainsSandbox(t *testing.T) {
	t.Parallel()
	d := NewCodexCliDriver(
		CodexWithSandboxMode("danger-full-access"),
		CodexWithCommandBuilder(codexMockCmdBuilder("codex_call_success")),
	)
	f := openLLMFile(t, d, ModeCall)
	defer f.Close()
	writeStringReq(t, f, `{"intent":"capture sandbox argv"}`)

	cap := f.LastRawCapture()
	if cap == nil {
		t.Fatal("LastRawCapture() == nil")
	}
	argv, ok := cap.Request["argv"].([]string)
	if !ok {
		t.Fatalf("Request[argv] not []string: %T", cap.Request["argv"])
	}
	joined := strings.Join(argv, " ")
	if !argsContainPair(argv, "--sandbox", "danger-full-access") {
		t.Errorf("raw argv missing --sandbox danger-full-access: %s", joined)
	}
	if strings.Contains(joined, "--full-auto") {
		t.Errorf("raw argv still contains --full-auto: %s", joined)
	}
}
