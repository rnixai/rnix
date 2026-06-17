package kernel

// agent.yaml-level reasoning_effort default (the second fallback tier).
//
// Resolution chain in spawn.go, isomorphic to Model:
//   opts.ReasoningEffort (per-spawn) > agent.Manifest.Models.ReasoningEffort
//   (agent.yaml) > driver snapshot (providers.yaml) > "" (native default).
//
// These tests prove the newly inserted middle tier: the agent manifest default
// wins over the driver snapshot, but yields to a per-spawn opts override; and an
// empty manifest field preserves the driver-snapshot fallback (zero regression).
// Reuses mockLLMFileWithEffort + the no-factory spawn fixture pattern.

import (
	"testing"

	"github.com/rnixai/rnix/agents"
	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/vfs"
)

// spawnWithAgentEffort spawns a process backed by llmFile with the given agent
// manifest effort and SpawnOpts, then waits for termination. The agent has no
// provider set, so resolveLLMDevice falls to the "claude" default → /dev/llm/claude,
// which the fixture registers.
func spawnWithAgentEffort(t *testing.T, llmFile vfs.VFSFile, manifestEffort string, opts SpawnOpts) (*KernelImpl, *Process) {
	t.Helper()

	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return llmFile, nil
	})
	v := vfs.NewVFS(reg)
	k := NewKernel(v, rnixctx.NewManager(), nil)
	k.dataDir = t.TempDir()
	t.Cleanup(k.Shutdown)

	agent := &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Name: "effort-agent",
			Models: agents.AgentModels{
				ReasoningEffort: manifestEffort,
			},
		},
	}

	pid, err := k.Spawn("agent manifest effort", agent, opts)
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d not found after spawn", pid)
	}
	waitEarlyEWDone(t, proc)
	return k, proc
}

// TestAgentManifestEffort_OverridesDriverSnapshot: a non-empty manifest effort
// wins over the driver's instance snapshot (the second tier beats the third).
func TestAgentManifestEffort_OverridesDriverSnapshot(t *testing.T) {
	llm := &mockLLMFileWithEffort{
		mockLLMFile: mockLLMFile{readData: makeLLMResponse("done", 10)},
		model:       "claude-test-model",
		effort:      "low", // driver instance snapshot
	}
	_, proc := spawnWithAgentEffort(t, llm, "high", SpawnOpts{})

	if proc.ReasoningEffort != "high" {
		t.Errorf("proc.ReasoningEffort = %q, want high (agent manifest overrides driver snapshot %q)", proc.ReasoningEffort, "low")
	}
}

// TestAgentManifestEffort_YieldsToOptsOverride: a per-spawn opts override (first
// tier) wins over the agent manifest default (second tier).
func TestAgentManifestEffort_YieldsToOptsOverride(t *testing.T) {
	llm := &mockLLMFileWithEffort{
		mockLLMFile: mockLLMFile{readData: makeLLMResponse("done", 10)},
		model:       "claude-test-model",
		effort:      "low",
	}
	_, proc := spawnWithAgentEffort(t, llm, "medium", SpawnOpts{ReasoningEffort: "xhigh"})

	if proc.ReasoningEffort != "xhigh" {
		t.Errorf("proc.ReasoningEffort = %q, want xhigh (opts override beats agent manifest %q)", proc.ReasoningEffort, "medium")
	}
}

// TestAgentManifestEffort_EmptyFallsBackToSnapshot: an empty manifest field
// preserves the driver-snapshot fallback (zero regression — third tier reached).
func TestAgentManifestEffort_EmptyFallsBackToSnapshot(t *testing.T) {
	llm := &mockLLMFileWithEffort{
		mockLLMFile: mockLLMFile{readData: makeLLMResponse("done", 10)},
		model:       "claude-test-model",
		effort:      "medium",
	}
	_, proc := spawnWithAgentEffort(t, llm, "", SpawnOpts{}) // empty manifest effort

	if proc.ReasoningEffort != "medium" {
		t.Errorf("proc.ReasoningEffort = %q, want medium (empty manifest falls back to driver snapshot)", proc.ReasoningEffort)
	}
}
