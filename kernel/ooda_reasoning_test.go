package kernel

// =============================================================================
// ATDD Story 20.2: OODA Configuration & Mission Command
// TDD RED PHASE - All tests designed to FAIL until implementation exists
// =============================================================================
//
// Test Strategy:
// - AC#1: Agent.yaml reasoning field propagates to Spawn (unit + integration)
// - AC#2: OODA agent autonomously spawns child with agent name (mission command)
// - AC#3: Child agent with reasoning: ooda runs its own OODA loop
//
// Priority: P1 (core agent lifecycle, high complexity, frequent usage)
// Test Level: Integration (kernel + agents interaction)

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/rnixai/rnix/agents"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/skills"
)

// --- AC #1: Spawn propagates agent.yaml reasoning mode ---

func TestSpawn_AgentReasoningOODA(t *testing.T) {
	// Given: an AgentInfo with Manifest.Reasoning = "ooda"
	// When: spawning with that agent
	// Then: the process uses OODA reasoning mode (proc.IsOODA() == true)
	responseFunc := func(writeData []byte) []byte {
		decision := map[string]any{
			"action": "complete",
			"target": "",
			"data":   map[string]any{},
			"reason": "agent reasoning test done",
		}
		decisionJSON, _ := json.Marshal(decision)
		return makeLLMResponse(string(decisionJSON), 10)
	}

	k, _, _ := newOODATestKernel(t, responseFunc)

	agent := testAgentInfoWithReasoning("ooda")

	pid, err := k.Spawn("agent ooda test", agent, SpawnOpts{
		MaxTurns: 5,
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d not found", pid)
	}

	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("expected exit code 0, got %d: %s", exit.Code, exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for OODA process completion")
	}

	if !proc.IsOODA() {
		t.Fatal("expected OODA to be enabled when agent.Manifest.Reasoning = \"ooda\"")
	}
}

func TestSpawn_AgentReasoningDefault(t *testing.T) {
	// Given: an AgentInfo with Manifest.Reasoning = "" (default)
	// When: spawning with that agent
	// Then: the process uses linear reasoning (proc.IsOODA() == false)
	llmFile := &mockLLMFile{
		readData: makeLLMResponse("linear agent response", 42),
	}
	k, _, _ := newTestKernel(t, llmFile)

	agent := testAgentInfoWithReasoning("")

	pid, err := k.Spawn("agent linear test", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d not found", pid)
	}

	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("expected exit code 0, got %d: %s", exit.Code, exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	if proc.IsOODA() {
		t.Fatal("expected OODA to be disabled when agent.Manifest.Reasoning is empty")
	}
}

func TestSpawn_ReasoningModePriority(t *testing.T) {
	// Given: agent.Manifest.Reasoning = "ooda" AND opts.ReasoningMode = ""
	// When: spawning
	// Then: agent.yaml takes priority, process is OODA-enabled
	//
	// Also: agent.Manifest.Reasoning = "" AND opts.ReasoningMode = "ooda"
	// Then: opts.ReasoningMode acts as fallback, process is OODA-enabled

	t.Run("agent_yaml_overrides_empty_opts", func(t *testing.T) {
		responseFunc := func(writeData []byte) []byte {
			decision := map[string]any{
				"action": "complete",
				"target": "",
				"data":   map[string]any{},
				"reason": "priority test",
			}
			decisionJSON, _ := json.Marshal(decision)
			return makeLLMResponse(string(decisionJSON), 10)
		}

		k, _, _ := newOODATestKernel(t, responseFunc)
		agent := testAgentInfoWithReasoning("ooda")

		pid, err := k.Spawn("priority test agent>opts", agent, SpawnOpts{
			ReasoningMode: "", // empty opts
			MaxTurns:      5,
		})
		if err != nil {
			t.Fatalf("Spawn failed: %v", err)
		}

		proc, ok := k.GetProcess(pid)
		if !ok {
			t.Fatalf("process %d not found", pid)
		}

		select {
		case exit := <-proc.Done:
			if exit.Code != 0 {
				t.Fatalf("expected exit code 0, got %d: %s", exit.Code, exit.Reason)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timed out")
		}

		if !proc.IsOODA() {
			t.Fatal("agent.yaml reasoning should override empty opts.ReasoningMode")
		}
	})

	t.Run("opts_fallback_when_agent_empty", func(t *testing.T) {
		responseFunc := func(writeData []byte) []byte {
			decision := map[string]any{
				"action": "complete",
				"target": "",
				"data":   map[string]any{},
				"reason": "opts fallback test",
			}
			decisionJSON, _ := json.Marshal(decision)
			return makeLLMResponse(string(decisionJSON), 10)
		}

		k, _, _ := newOODATestKernel(t, responseFunc)
		agent := testAgentInfoWithReasoning("") // no reasoning in agent

		pid, err := k.Spawn("priority test opts fallback", agent, SpawnOpts{
			ReasoningMode: "ooda", // opts provides the mode
			MaxTurns:      5,
		})
		if err != nil {
			t.Fatalf("Spawn failed: %v", err)
		}

		proc, ok := k.GetProcess(pid)
		if !ok {
			t.Fatalf("process %d not found", pid)
		}

		select {
		case exit := <-proc.Done:
			if exit.Code != 0 {
				t.Fatalf("expected exit code 0, got %d: %s", exit.Code, exit.Reason)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timed out")
		}

		if !proc.IsOODA() {
			t.Fatal("opts.ReasoningMode should enable OODA when agent.Reasoning is empty")
		}
	})
}

// --- AC #2: Mission Command -- OODA autonomous spawn with agent name ---

func TestOODAActSpawn_WithAgent(t *testing.T) {
	// Given: OODA process in Decide phase outputs spawn with agent name
	// When: oodaActSpawn processes the decision
	// Then: child process loads the specified agent and runs accordingly
	callCount := 0
	responseFunc := func(writeData []byte) []byte {
		callCount++
		switch callCount {
		case 1:
			// Parent Orient
			return makeLLMResponse("need sub-agent with specific capabilities", 15)
		case 2:
			// Parent Decide: spawn with agent name
			decision := map[string]any{
				"action": "spawn",
				"target": "analyze sub-task with agent",
				"data": map[string]any{
					"agent": "ooda-demo",
				},
				"reason": "delegate to specialized agent",
			}
			decisionJSON, _ := json.Marshal(decision)
			return makeLLMResponse(string(decisionJSON), 15)
		case 3:
			// Child Orient (if child is OODA)
			return makeLLMResponse("child orientation", 10)
		case 4:
			// Child Decide: complete
			decision := map[string]any{
				"action": "complete",
				"target": "",
				"data":   map[string]any{},
				"reason": "child task done",
			}
			decisionJSON, _ := json.Marshal(decision)
			return makeLLMResponse(string(decisionJSON), 10)
		case 5:
			// Parent cycle 2 Orient
			return makeLLMResponse("child completed, goals met", 15)
		case 6:
			// Parent cycle 2 Decide: complete
			decision := map[string]any{
				"action": "complete",
				"target": "",
				"data":   map[string]any{},
				"reason": "mission accomplished",
			}
			decisionJSON, _ := json.Marshal(decision)
			return makeLLMResponse(string(decisionJSON), 15)
		default:
			return makeLLMResponse("unexpected call", 5)
		}
	}

	k, _, _ := newOODATestKernel(t, responseFunc)

	// Inject agentLoader that returns the OODA demo agent
	k.SetAgentLoader(func(name string) (*agents.AgentInfo, error) {
		if name == "ooda-demo" {
			return testAgentInfoWithReasoning("ooda"), nil
		}
		return nil, types.NewErrNotFound("agent", name)
	})

	pid, err := k.Spawn("mission command test", nil, SpawnOpts{
		ReasoningMode: "ooda",
		MaxTurns:      10,
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d not found", pid)
	}

	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("expected exit code 0, got %d: %s", exit.Code, exit.Reason)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for mission command completion")
	}
}

func TestOODAActSpawn_WithoutAgent(t *testing.T) {
	// Given: OODA process spawn decision without agent field in data
	// When: oodaActSpawn processes the decision
	// Then: child process spawned without agent (bare process, linear mode)
	callCount := 0
	responseFunc := func(writeData []byte) []byte {
		callCount++
		switch callCount {
		case 1:
			return makeLLMResponse("need sub-task", 15)
		case 2:
			decision := map[string]any{
				"action": "spawn",
				"target": "sub-task without agent",
				"data":   map[string]any{}, // no "agent" field
				"reason": "delegate generic task",
			}
			decisionJSON, _ := json.Marshal(decision)
			return makeLLMResponse(string(decisionJSON), 15)
		case 3:
			// Child linear response
			return makeLLMResponse("child bare result", 10)
		case 4:
			return makeLLMResponse("child done, moving on", 15)
		case 5:
			decision := map[string]any{
				"action": "complete",
				"target": "",
				"data":   map[string]any{},
				"reason": "done",
			}
			decisionJSON, _ := json.Marshal(decision)
			return makeLLMResponse(string(decisionJSON), 15)
		default:
			return makeLLMResponse("unexpected", 5)
		}
	}

	k, _, _ := newOODATestKernel(t, responseFunc)

	pid, err := k.Spawn("spawn without agent test", nil, SpawnOpts{
		ReasoningMode: "ooda",
		MaxTurns:      10,
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d not found", pid)
	}

	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("expected exit code 0, got %d: %s", exit.Code, exit.Reason)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out")
	}
}

func TestOODAActSpawn_AgentNotFound(t *testing.T) {
	// Given: OODA process spawn decision referencing nonexistent agent
	// When: oodaActSpawn tries to load the agent
	// Then: spawn returns error message (not crash), parent can continue
	callCount := 0
	responseFunc := func(writeData []byte) []byte {
		callCount++
		switch callCount {
		case 1:
			return makeLLMResponse("delegating to unknown agent", 15)
		case 2:
			decision := map[string]any{
				"action": "spawn",
				"target": "task for nonexistent agent",
				"data": map[string]any{
					"agent": "nonexistent-agent",
				},
				"reason": "delegate to specialized agent",
			}
			decisionJSON, _ := json.Marshal(decision)
			return makeLLMResponse(string(decisionJSON), 15)
		case 3:
			// After spawn error, parent continues: Orient
			return makeLLMResponse("spawn failed, adjusting plan", 15)
		case 4:
			decision := map[string]any{
				"action": "complete",
				"target": "",
				"data":   map[string]any{},
				"reason": "handled spawn failure gracefully",
			}
			decisionJSON, _ := json.Marshal(decision)
			return makeLLMResponse(string(decisionJSON), 15)
		default:
			return makeLLMResponse("unexpected", 5)
		}
	}

	k, _, _ := newOODATestKernel(t, responseFunc)

	// Inject agentLoader that always fails
	k.SetAgentLoader(func(name string) (*agents.AgentInfo, error) {
		return nil, types.NewErrNotFound("agent", name)
	})

	pid, err := k.Spawn("agent not found test", nil, SpawnOpts{
		ReasoningMode: "ooda",
		MaxTurns:      10,
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d not found", pid)
	}

	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("expected exit code 0 (parent handles error gracefully), got %d: %s", exit.Code, exit.Reason)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out")
	}
}

// --- AC #3: Child OODA mode inheritance ---

func TestOODA_ChildInheritsOODAMode(t *testing.T) {
	// Given: parent OODA agent spawns child with agent.Reasoning = "ooda"
	// When: child is spawned via oodaActSpawn with agent loader
	// Then: child proc.IsOODA() == true (child runs its own OODA loop)
	callCount := 0
	var childPID types.PID
	responseFunc := func(writeData []byte) []byte {
		callCount++
		switch callCount {
		case 1:
			return makeLLMResponse("need OODA child", 15)
		case 2:
			decision := map[string]any{
				"action": "spawn",
				"target": "ooda child task",
				"data": map[string]any{
					"agent": "ooda-child",
				},
				"reason": "spawn OODA child",
			}
			decisionJSON, _ := json.Marshal(decision)
			return makeLLMResponse(string(decisionJSON), 15)
		case 3:
			// Child Orient
			return makeLLMResponse("child ooda orient", 10)
		case 4:
			// Child Decide: complete
			decision := map[string]any{
				"action": "complete",
				"target": "",
				"data":   map[string]any{},
				"reason": "child ooda done",
			}
			decisionJSON, _ := json.Marshal(decision)
			return makeLLMResponse(string(decisionJSON), 10)
		case 5:
			return makeLLMResponse("child returned, all done", 15)
		case 6:
			decision := map[string]any{
				"action": "complete",
				"target": "",
				"data":   map[string]any{},
				"reason": "parent done",
			}
			decisionJSON, _ := json.Marshal(decision)
			return makeLLMResponse(string(decisionJSON), 15)
		default:
			return makeLLMResponse("unexpected", 5)
		}
	}

	k, _, _ := newOODATestKernel(t, responseFunc)

	// Inject agentLoader returning OODA agent
	k.SetAgentLoader(func(name string) (*agents.AgentInfo, error) {
		if name == "ooda-child" {
			return testAgentInfoWithReasoning("ooda"), nil
		}
		return nil, types.NewErrNotFound("agent", name)
	})

	pid, err := k.Spawn("child ooda inheritance test", nil, SpawnOpts{
		ReasoningMode: "ooda",
		MaxTurns:      10,
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d not found", pid)
	}

	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("expected exit code 0, got %d: %s", exit.Code, exit.Reason)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("timed out")
	}

	// Verify child was spawned and is OODA-enabled
	// Find child process (PID > parent PID)
	for _, p := range k.ListProcesses() {
		if p.PID > pid {
			childPID = p.PID
			break
		}
	}
	if childPID == 0 {
		t.Fatal("expected child process to be spawned")
	}

	childProc, ok := k.GetProcess(childPID)
	if !ok {
		t.Fatalf("child process %d not found", childPID)
	}

	if !childProc.IsOODA() {
		t.Fatal("expected child process to have OODA enabled (inherited from agent.yaml reasoning: ooda)")
	}
}

func TestOODA_ChildLinearMode(t *testing.T) {
	// Given: parent OODA agent spawns child with agent.Reasoning = "" (linear)
	// When: child is spawned via oodaActSpawn with agent loader
	// Then: child proc.IsOODA() == false (child uses linear reasoning)
	callCount := 0
	var childPID types.PID
	responseFunc := func(writeData []byte) []byte {
		callCount++
		switch callCount {
		case 1:
			return makeLLMResponse("need linear child", 15)
		case 2:
			decision := map[string]any{
				"action": "spawn",
				"target": "linear child task",
				"data": map[string]any{
					"agent": "linear-child",
				},
				"reason": "spawn linear child",
			}
			decisionJSON, _ := json.Marshal(decision)
			return makeLLMResponse(string(decisionJSON), 15)
		case 3:
			// Child linear response
			return makeLLMResponse("child linear result", 10)
		case 4:
			return makeLLMResponse("child done", 15)
		case 5:
			decision := map[string]any{
				"action": "complete",
				"target": "",
				"data":   map[string]any{},
				"reason": "parent done",
			}
			decisionJSON, _ := json.Marshal(decision)
			return makeLLMResponse(string(decisionJSON), 15)
		default:
			return makeLLMResponse("unexpected", 5)
		}
	}

	k, _, _ := newOODATestKernel(t, responseFunc)

	// Inject agentLoader returning linear agent (no reasoning)
	k.SetAgentLoader(func(name string) (*agents.AgentInfo, error) {
		if name == "linear-child" {
			return testAgentInfoWithReasoning(""), nil
		}
		return nil, types.NewErrNotFound("agent", name)
	})

	pid, err := k.Spawn("child linear mode test", nil, SpawnOpts{
		ReasoningMode: "ooda",
		MaxTurns:      10,
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d not found", pid)
	}

	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("expected exit code 0, got %d: %s", exit.Code, exit.Reason)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out")
	}

	// Find child process
	for _, p := range k.ListProcesses() {
		if p.PID > pid {
			childPID = p.PID
			break
		}
	}
	if childPID == 0 {
		t.Fatal("expected child process to be spawned")
	}

	childProc, ok := k.GetProcess(childPID)
	if !ok {
		t.Fatalf("child process %d not found", childPID)
	}

	if childProc.IsOODA() {
		t.Fatal("expected child process to NOT have OODA enabled (agent.yaml has no reasoning field)")
	}
}

// --- Test helper ---

// testAgentInfoWithReasoning creates a test AgentInfo with the given reasoning mode.
func testAgentInfoWithReasoning(reasoning string) *agents.AgentInfo {
	return &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Name: "test-agent",
			Models: agents.AgentModels{
				Provider:  "claude",
				Preferred: "sonnet",
				Fallback:  "haiku",
			},
			ContextBudget: 4096,
			Skills:        []string{"mock-skill"},
			Reasoning:     reasoning,
		},
		Instructions: "# Test Agent\n\nYou are a test agent.",
		Skills: []*skills.SkillInfo{
			{
				Manifest: skills.SkillManifest{
					Name:            "mock-skill",
					Description:     "A mock skill for testing",
					AllowedToolsRaw: "/dev/fs /dev/shell",
				},
				Body: "# Mock Skill\n\nReview source files for quality issues.",
			},
		},
	}
}
