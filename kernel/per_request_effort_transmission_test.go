package kernel

// Per-request reasoning_effort kernel transmission coverage
// (spec-reasoning-effort-per-request).
//
// Two layers are proven here:
//   1. spawn.go priority: SpawnOpts.ReasoningEffort (non-empty) overrides the
//      driver snapshot (ReasoningEffortProvider); empty falls back to it.
//   2. reason.go construction: proc.ReasoningEffort is threaded into the
//      llmRequest written to the LLM VFS device (reasoning_effort JSON field),
//      so the driver receives it across the VFS boundary.
//
// Reuses mockLLMFileWithEffort (atdd_56_5_early_eventwriter_test.go) and the
// no-factory spawn fixture pattern, but with a caller-supplied SpawnOpts.

import (
	"encoding/json"
	"testing"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/vfs"
)

// spawnWithEffortOpts spawns a normal agent process (no EventWriterFactory)
// backed by llmFile, with the given SpawnOpts, and waits for it to terminate.
// Mirrors spawnEarlyEWNoFactory but threads caller opts (e.g. ReasoningEffort).
func spawnWithEffortOpts(t *testing.T, llmFile vfs.VFSFile, opts SpawnOpts) (*KernelImpl, *Process) {
	t.Helper()

	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return llmFile, nil
	})
	v := vfs.NewVFS(reg)
	k := NewKernel(v, rnixctx.NewManager(), nil)
	k.dataDir = t.TempDir()
	t.Cleanup(k.Shutdown)

	pid, err := k.Spawn("per-request effort transmission", nil, opts)
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

// TestSpawnOpts_ReasoningEffort_OverridesDriverSnapshot: a non-empty
// SpawnOpts.ReasoningEffort wins over the driver's instance snapshot.
func TestSpawnOpts_ReasoningEffort_OverridesDriverSnapshot(t *testing.T) {
	llm := &mockLLMFileWithEffort{
		mockLLMFile: mockLLMFile{readData: makeLLMResponse("done", 10)},
		model:       "claude-test-model",
		effort:      "low", // driver instance snapshot
	}
	_, proc := spawnWithEffortOpts(t, llm, SpawnOpts{ReasoningEffort: "high"})

	if proc.ReasoningEffort != "high" {
		t.Errorf("proc.ReasoningEffort = %q, want high (SpawnOpts overrides driver snapshot %q)", proc.ReasoningEffort, "low")
	}
}

// TestSpawnOpts_ReasoningEffort_EmptyFallsBackToSnapshot: an empty opts field
// preserves the Story 55.2 driver-snapshot behavior (zero-regression).
func TestSpawnOpts_ReasoningEffort_EmptyFallsBackToSnapshot(t *testing.T) {
	llm := &mockLLMFileWithEffort{
		mockLLMFile: mockLLMFile{readData: makeLLMResponse("done", 10)},
		model:       "claude-test-model",
		effort:      "medium", // driver instance snapshot
	}
	_, proc := spawnWithEffortOpts(t, llm, SpawnOpts{}) // empty ReasoningEffort

	if proc.ReasoningEffort != "medium" {
		t.Errorf("proc.ReasoningEffort = %q, want medium (fallback to driver snapshot)", proc.ReasoningEffort)
	}
}

// TestReasonStep_ReasoningEffort_ThreadedIntoLLMRequest: proc.ReasoningEffort
// reaches the llmRequest written to the LLM device. The mock captures the
// request body; we decode reasoning_effort to prove the reason.go construction
// + VFS JSON tag alignment carry the value to the driver.
func TestReasonStep_ReasoningEffort_ThreadedIntoLLMRequest(t *testing.T) {
	llm := &mockLLMFileWithEffort{
		mockLLMFile: mockLLMFile{readData: makeLLMResponse("done", 10)},
		model:       "claude-test-model",
		effort:      "high",
	}
	_, proc := spawnWithEffortOpts(t, llm, SpawnOpts{})

	// proc.ReasoningEffort snapshotted from the driver.
	if proc.ReasoningEffort != "high" {
		t.Fatalf("precondition: proc.ReasoningEffort = %q, want high", proc.ReasoningEffort)
	}

	llm.mu.Lock()
	written := llm.writeData
	llm.mu.Unlock()
	if len(written) == 0 {
		t.Fatal("no request written to LLM device")
	}

	var req map[string]any
	if err := json.Unmarshal(written, &req); err != nil {
		t.Fatalf("unmarshal written request: %v", err)
	}
	if got, _ := req["reasoning_effort"].(string); got != "high" {
		t.Errorf("llmRequest reasoning_effort = %q, want high (proc value threaded into request)", got)
	}
}

// TestReasonStep_ReasoningEffort_OptsOverrideReachesRequest: end-to-end across
// both layers — SpawnOpts override → proc → llmRequest reasoning_effort field.
func TestReasonStep_ReasoningEffort_OptsOverrideReachesRequest(t *testing.T) {
	llm := &mockLLMFileWithEffort{
		mockLLMFile: mockLLMFile{readData: makeLLMResponse("done", 10)},
		model:       "claude-test-model",
		effort:      "low", // driver default; opts must win end-to-end
	}
	_, _ = spawnWithEffortOpts(t, llm, SpawnOpts{ReasoningEffort: "xhigh"})

	llm.mu.Lock()
	written := llm.writeData
	llm.mu.Unlock()
	if len(written) == 0 {
		t.Fatal("no request written to LLM device")
	}

	var req map[string]any
	if err := json.Unmarshal(written, &req); err != nil {
		t.Fatalf("unmarshal written request: %v", err)
	}
	if got, _ := req["reasoning_effort"].(string); got != "xhigh" {
		t.Errorf("llmRequest reasoning_effort = %q, want xhigh (opts override reaches request, no whitelist)", got)
	}
}

// TestReasonStep_ReasoningEffort_EmptyOmittedFromRequest: zero-regression — when
// neither opts nor driver supply an effort, the request omits the field
// (omitempty), matching pre-spec behavior bit-for-bit.
func TestReasonStep_ReasoningEffort_EmptyOmittedFromRequest(t *testing.T) {
	llm := &mockLLMFile{readData: makeLLMResponse("done", 10)} // no effort provider
	_, proc := spawnWithEffortOpts(t, llm, SpawnOpts{})

	if proc.ReasoningEffort != "" {
		t.Fatalf("precondition: proc.ReasoningEffort = %q, want empty", proc.ReasoningEffort)
	}

	llm.mu.Lock()
	written := llm.writeData
	llm.mu.Unlock()
	if len(written) == 0 {
		t.Fatal("no request written to LLM device")
	}

	var req map[string]any
	if err := json.Unmarshal(written, &req); err != nil {
		t.Fatalf("unmarshal written request: %v", err)
	}
	if _, present := req["reasoning_effort"]; present {
		t.Errorf("reasoning_effort present in request, want omitted when unset (omitempty)")
	}
}
