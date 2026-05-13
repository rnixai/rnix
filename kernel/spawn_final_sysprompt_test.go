package kernel

import (
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/agents"
	"github.com/rnixai/rnix/skills"
)

// probeAgentInfo builds a minimal sections-mode agent for tests that need
// to exercise the agent-based spawn path (HasSections=true).
func probeAgentInfo() *agents.AgentInfo {
	return &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Name:          "probe-agent",
			Description:   "Probe agent for FinalSystemPrompt capture",
			Models:        agents.AgentModels{Provider: "claude", Preferred: "sonnet"},
			ContextBudget: 16384,
		},
		Instructions: "You are a Probe Agent.",
		Skills:       []*skills.SkillInfo{},
	}
}

// TestSpawn_AgentBased_CapturesFinalSystemPromptAtSpawnTime verifies that
// `proc.FinalSystemPrompt` is non-empty by the time `Spawn` returns for an
// agent-based (sections=true) process, independent of whether the
// reasonStep goroutine ever runs.
//
// Regression context: `process-meta.json` is written at reap with
// `proc.FinalSystemPrompt` as `system_prompt`. Real-world processes
// (EchoMatrix, 2026-05-02 onward) show `system_prompt: ""` despite running
// multiple reasonStep iterations — indicating the first-step capture in
// `kernel/reason.go:368-372` is unreliable at reap time. Capturing eagerly
// at spawn time is robust against whatever timing/cancel paths prevent the
// reasonStep-side assignment from being observed.
func TestSpawn_AgentBased_CapturesFinalSystemPromptAtSpawnTime(t *testing.T) {
	llmFile := &mockLLMFile{}
	k, _, _ := newTestKernel(t, llmFile)

	// SkipReasonLoop=true: no reasoning goroutine runs, so any non-empty
	// FinalSystemPrompt MUST come from spawn-time capture.
	pid, err := k.Spawn("probe intent", probeAgentInfo(), SpawnOpts{SkipReasonLoop: true})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	defer func() {
		_ = k.Kill(pid, 9)
		time.Sleep(10 * time.Millisecond)
	}()

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d not found after spawn", pid)
	}

	proc.mu.Lock()
	fsp := proc.FinalSystemPrompt
	hasSections := proc.HasSections
	proc.mu.Unlock()

	if !hasSections {
		t.Fatalf("expected agent-based spawn to set HasSections=true")
	}
	if fsp == "" {
		t.Fatalf("expected FinalSystemPrompt to be captured at spawn time, got empty")
	}
}

// TestSpawn_NoAgent_HasSectionsAndBasePrompt verifies that all processes
// receive Rnix's base system prompt sections regardless of agent presence.
func TestSpawn_NoAgent_HasSectionsAndBasePrompt(t *testing.T) {
	llmFile := &mockLLMFile{}
	k, _, _ := newTestKernel(t, llmFile)

	pid, err := k.Spawn("hi", nil, SpawnOpts{SkipReasonLoop: true})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	defer func() {
		_ = k.Kill(pid, 9)
		time.Sleep(10 * time.Millisecond)
	}()

	proc, _ := k.GetProcess(pid)
	proc.mu.Lock()
	fsp := proc.FinalSystemPrompt
	hasSections := proc.HasSections
	sections := proc.sections
	proc.mu.Unlock()

	if !hasSections {
		t.Fatalf("all spawns must register sections; got HasSections=false for agent=nil spawn")
	}
	if sections == nil {
		t.Fatalf("sections registry must be non-nil for agent=nil spawn")
	}
	if fsp == "" {
		t.Fatalf("FinalSystemPrompt must be non-empty for agent=nil spawn (base sections); got empty")
	}
}

// TestSpawn_WithoutAgent_BaseSystemPrompt verifies that base sections
// (intro, system_rules, actions) are present in the built prompt for
// agent-less spawns, ensuring the Rnix unified prompt is always injected.
func TestSpawn_WithoutAgent_BaseSystemPrompt(t *testing.T) {
	llmFile := &mockLLMFile{}
	k, _, _ := newTestKernel(t, llmFile)

	pid, err := k.Spawn("test task", nil, SpawnOpts{SkipReasonLoop: true})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	defer func() {
		_ = k.Kill(pid, 9)
		time.Sleep(10 * time.Millisecond)
	}()

	proc, _ := k.GetProcess(pid)
	proc.mu.Lock()
	fsp := proc.FinalSystemPrompt
	proc.mu.Unlock()

	for _, marker := range []struct {
		section string
		substr  string
	}{
		{"intro", "You are Rnix"},
		{"system_rules", "# System"},
		{"actions", "Executing actions with care"},
	} {
		if !strings.Contains(fsp, marker.substr) {
			t.Errorf("base section %q missing from FinalSystemPrompt: expected substring %q", marker.section, marker.substr)
		}
	}
}

// TestSpawn_AgentBased_FinalSystemPromptSurvivesReasonStep verifies that
// the eagerly-captured value is preserved across reasonStep execution
// (i.e. the in-loop guard `if proc.FinalSystemPrompt == ""` does not
// clobber it).
func TestSpawn_AgentBased_FinalSystemPromptSurvivesReasonStep(t *testing.T) {
	llmFile := &mockLLMFile{}
	k, _, _ := newTestKernel(t, llmFile)

	llmFile.mu.Lock()
	llmFile.readData = makeCompleteResponse("done", 10)
	llmFile.mu.Unlock()

	pid, err := k.Spawn("probe intent", probeAgentInfo(), SpawnOpts{MaxTurns: 1, TimeoutMs: 5000})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("process did not exit in time")
	}

	proc.mu.Lock()
	fsp := proc.FinalSystemPrompt
	proc.mu.Unlock()
	if fsp == "" {
		t.Fatalf("FinalSystemPrompt should remain non-empty after reasonStep")
	}
}
