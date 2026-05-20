package kernel

import (
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/agents"
	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/config"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// providerWarningKernel builds a kernel where /dev/llm/claude and
// /dev/llm/deepseek each yield a fresh mockLLMFile whose canned response
// makes the reasonStep loop exit immediately. This isolates the test from
// LLM behavior so it can observe the provider-resolution side effects
// (proc.Provider, log warning) deterministically.
func providerWarningKernel(t testing.TB) *KernelImpl {
	reg := vfs.NewDeviceRegistry()
	for _, name := range []string{"claude", "deepseek"} {
		_ = reg.Register("/dev/llm/"+name, func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
			return &mockLLMFile{readData: makeLLMResponse("done", 1)}, nil
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

func pinnedProviderAgent(name, provider string) *agents.AgentInfo {
	return &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Name: name,
			Models: agents.AgentModels{
				Provider: provider,
			},
			ContextBudget: 4096,
		},
		Instructions: "Test agent.",
	}
}

func stemLikeAgent() *agents.AgentInfo {
	return &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Name:          "stem",
			ContextBudget: 4096,
		},
		Instructions: "Opinion-less base agent.",
	}
}

func waitForExit(t *testing.T, proc *Process) {
	t.Helper()
	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for process completion")
	}
}

func snapshotLogHistory(proc *Process) []types.LogEntry {
	proc.mu.Lock()
	defer proc.mu.Unlock()
	return proc.GetLogHistory()
}

func logHistoryContains(history []types.LogEntry, substr string) bool {
	for _, entry := range history {
		if strings.Contains(entry.Content, substr) {
			return true
		}
	}
	return false
}

// TestSpawn_ProviderManifestOverridesProjectDefault_EmitsWarning is the
// regression guard for the stem hard-coded-provider fix. When an agent
// manifest pins models.provider AND the project config provides a
// different default_provider AND the user did not pass --provider, the
// kernel must emit a stdout warning log so the silent override is visible.
func TestSpawn_ProviderManifestOverridesProjectDefault_EmitsWarning(t *testing.T) {
	k := providerWarningKernel(t)
	agent := pinnedProviderAgent("legacy-pinned", "claude")

	pid, err := k.Spawn("test pinned provider", agent, SpawnOpts{
		ProjectConfig: &config.ProjectConfig{
			DefaultProvider: "deepseek",
		},
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d missing after spawn", pid)
	}
	waitForExit(t, proc)

	if proc.Provider != "claude" {
		t.Errorf("Provider = %q, want claude (manifest wins when CLI override absent)", proc.Provider)
	}
	history := snapshotLogHistory(proc)
	if !logHistoryContains(history, "overrides project default_provider") {
		t.Errorf("expected warning log mentioning project default override; history=%v", history)
	}
}

// TestSpawn_StemFollowsProjectDefault_NoWarning verifies the primary fix:
// the stem agent (manifest.Models.Provider == "") follows the project's
// default_provider WITHOUT emitting the override warning, because there is
// no manifest opinion to override.
func TestSpawn_StemFollowsProjectDefault_NoWarning(t *testing.T) {
	k := providerWarningKernel(t)
	agent := stemLikeAgent()

	pid, err := k.Spawn("test stem follows project default", agent, SpawnOpts{
		ProjectConfig: &config.ProjectConfig{
			DefaultProvider: "deepseek",
		},
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d missing after spawn", pid)
	}
	waitForExit(t, proc)

	if proc.Provider != "deepseek" {
		t.Errorf("Provider = %q, want deepseek (stem follows project default_provider)", proc.Provider)
	}
	if logHistoryContains(snapshotLogHistory(proc), "overrides project default_provider") {
		t.Errorf("unexpected override warning emitted; stem has no manifest opinion to override")
	}
}

// TestSpawn_CLIProviderSuppressesWarning verifies the warning is silent when
// the user explicitly passes --provider, because an explicit CLI choice is
// not a silent override.
func TestSpawn_CLIProviderSuppressesWarning(t *testing.T) {
	k := providerWarningKernel(t)
	agent := pinnedProviderAgent("legacy-pinned", "claude")

	pid, err := k.Spawn("test cli explicit", agent, SpawnOpts{
		Provider: "deepseek",
		ProjectConfig: &config.ProjectConfig{
			DefaultProvider: "deepseek",
		},
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d missing after spawn", pid)
	}
	waitForExit(t, proc)

	if proc.Provider != "deepseek" {
		t.Errorf("Provider = %q, want deepseek (CLI override)", proc.Provider)
	}
	if logHistoryContains(snapshotLogHistory(proc), "overrides project default_provider") {
		t.Errorf("unexpected override warning emitted; CLI override is explicit, not silent")
	}
}
