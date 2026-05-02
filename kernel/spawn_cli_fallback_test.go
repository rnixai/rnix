package kernel

import (
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/vfs"
)

// newCliFallbackKernel registers two providers ("claude" and "deepseek")
// for testing CLI fallback override / cross-provider auto-disable logic.
// Uses SkipReasonLoop spawns, so the mock LLM file is intentionally minimal.
func newCliFallbackKernel(t testing.TB) *KernelImpl {
	reg := vfs.NewDeviceRegistry()
	for _, name := range []string{"claude", "deepseek"} {
		_ = reg.Register("/dev/llm/"+name, func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
			return &mockLLMFile{}, nil
		})
	}
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)

	providerSet := map[string]bool{"claude": true, "deepseek": true}
	k.SetProviderResolver(
		func() []string { return []string{"claude", "deepseek"} },
		func(name string) bool { return providerSet[name] },
	)
	return k
}

// TestSpawn_CLIFallbackOverride verifies that opts.FallbackModel and
// opts.FallbackProvider override the agent manifest fallback config.
func TestSpawn_CLIFallbackOverride(t *testing.T) {
	k := newCliFallbackKernel(t)
	agent := fallbackAgentInfo("claude", "sonnet", "haiku", "")

	pid, err := k.Spawn("test", agent, SpawnOpts{
		SkipReasonLoop:   true,
		FallbackModel:    "deepseek-v4-flash",
		FallbackProvider: "deepseek",
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d missing after spawn", pid)
	}

	if proc.FallbackModel != "deepseek-v4-flash" {
		t.Errorf("FallbackModel = %q, want deepseek-v4-flash (CLI override)", proc.FallbackModel)
	}
	if proc.FallbackProvider != "deepseek" {
		t.Errorf("FallbackProvider = %q, want deepseek (CLI override)", proc.FallbackProvider)
	}
	if proc.FallbackDevice != "/dev/llm/deepseek" {
		t.Errorf("FallbackDevice = %q, want /dev/llm/deepseek", proc.FallbackDevice)
	}
}

// TestSpawn_CLIFallbackOverride_SameProvider verifies CLI fallback model
// alone (no fallback provider) defaults to the resolved primary provider.
func TestSpawn_CLIFallbackOverride_SameProvider(t *testing.T) {
	k := newCliFallbackKernel(t)
	agent := fallbackAgentInfo("claude", "sonnet", "haiku", "")

	pid, err := k.Spawn("test", agent, SpawnOpts{
		SkipReasonLoop: true,
		FallbackModel:  "claude-3-5-haiku",
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	if proc.FallbackModel != "claude-3-5-haiku" {
		t.Errorf("FallbackModel = %q", proc.FallbackModel)
	}
	if proc.FallbackProvider != "claude" {
		t.Errorf("FallbackProvider = %q, want claude (same-provider default)", proc.FallbackProvider)
	}
}

// TestSpawn_CrossProviderAutoDisablesFallback verifies that when CLI
// --provider differs from the agent manifest provider AND no CLI
// fallback flags are given, the manifest fallback is suppressed —
// avoiding 404s when the manifest's fallback model doesn't exist on
// the new provider (rnix-eval / deepseek scenario).
func TestSpawn_CrossProviderAutoDisablesFallback(t *testing.T) {
	k := newCliFallbackKernel(t)
	agent := fallbackAgentInfo("claude", "sonnet", "haiku", "")

	pid, err := k.Spawn("test", agent, SpawnOpts{
		SkipReasonLoop: true,
		Provider:       "deepseek", // overrides agent's "claude" primary
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	if proc.FallbackModel != "" {
		t.Errorf("FallbackModel = %q, want empty (auto-disabled on cross-provider)", proc.FallbackModel)
	}
	if proc.FallbackProvider != "" {
		t.Errorf("FallbackProvider = %q, want empty", proc.FallbackProvider)
	}
	if proc.FallbackDevice != "" {
		t.Errorf("FallbackDevice = %q, want empty", proc.FallbackDevice)
	}
}

// TestSpawn_SameProviderKeepsManifestFallback verifies the regression
// safeguard: when CLI does NOT override provider (or matches the
// manifest), the agent manifest fallback continues to apply.
func TestSpawn_SameProviderKeepsManifestFallback(t *testing.T) {
	k := newCliFallbackKernel(t)
	agent := fallbackAgentInfo("claude", "sonnet", "haiku", "")

	pid, err := k.Spawn("test", agent, SpawnOpts{
		SkipReasonLoop: true,
		Provider:       "claude", // matches agent manifest
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	if proc.FallbackModel != "haiku" {
		t.Errorf("FallbackModel = %q, want haiku (manifest fallback preserved)", proc.FallbackModel)
	}
	if proc.FallbackProvider != "claude" {
		t.Errorf("FallbackProvider = %q, want claude", proc.FallbackProvider)
	}
}

// TestSpawn_CrossProviderEmitsAutoDisabledEvent verifies the dashboard /
// strace observability contract: when fallback is auto-disabled, a
// ReasonStep event with action="fallback_auto_disabled" must appear on
// the process DebugChan so operators can see why fallback silently
// stopped working after they passed --provider.
func TestSpawn_CrossProviderEmitsAutoDisabledEvent(t *testing.T) {
	k := newCliFallbackKernel(t)
	agent := fallbackAgentInfo("claude", "sonnet", "haiku", "")

	pid, err := k.Spawn("test", agent, SpawnOpts{
		SkipReasonLoop: true,
		Provider:       "deepseek",
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	proc, _ := k.GetProcess(pid)

	// emitEvent is synchronous (writes to a buffered chan via select).
	// After Spawn returns, the auto-disabled event is already in the
	// channel — drain non-blockingly with a tiny timeout for safety.
	deadline := time.After(500 * time.Millisecond)
	found := false
	var args map[string]any
DrainLoop:
	for {
		select {
		case evt, ok := <-proc.DebugChan:
			if !ok {
				break DrainLoop
			}
			a := evt.Args
			if a == nil {
				continue
			}
			if action, _ := a["action"].(string); action == "fallback_auto_disabled" {
				found = true
				args = a
				break DrainLoop
			}
		case <-deadline:
			break DrainLoop
		}
	}

	if !found {
		t.Fatal("expected fallback_auto_disabled event in DebugChan, none found")
	}
	if got, _ := args["cli_provider"].(string); got != "deepseek" {
		t.Errorf("cli_provider = %q, want deepseek", got)
	}
	if got, _ := args["agent_provider"].(string); got != "claude" {
		t.Errorf("agent_provider = %q, want claude", got)
	}
	if got, _ := args["agent_fallback"].(string); got != "haiku" {
		t.Errorf("agent_fallback = %q, want haiku", got)
	}
}

// TestSpawn_CrossProviderWithExplicitFallbackProvider verifies that an
// explicit --fallback-provider keeps the manifest fallback alive even
// when the CLI overrides the primary provider.
func TestSpawn_CrossProviderWithExplicitFallbackProvider(t *testing.T) {
	k := newCliFallbackKernel(t)
	agent := fallbackAgentInfo("claude", "sonnet", "haiku", "")

	pid, err := k.Spawn("test", agent, SpawnOpts{
		SkipReasonLoop:   true,
		Provider:         "deepseek",
		FallbackProvider: "claude", // user pinned fallback explicitly
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	if proc.FallbackModel != "haiku" {
		t.Errorf("FallbackModel = %q, want haiku (explicit --fallback-provider keeps manifest)", proc.FallbackModel)
	}
	if proc.FallbackProvider != "claude" {
		t.Errorf("FallbackProvider = %q, want claude", proc.FallbackProvider)
	}
}
