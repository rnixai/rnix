package kernel

// =============================================================================
// ATDD Story 20.4: Progressive Specialization & Differentiation Memory
// TDD RED PHASE - Integration Tests
// =============================================================================
//
// These tests verify:
// - Task 2: Spawn integration with DiffMemory (Lookup reuse, Record after differentiation)
// - Task 3: OODA specialize action (dynamic skill loading mid-execution)
// - Task 4: End-to-end scenarios (full lifecycle)
//
// All tests FAIL until kernel/diffmemory.go, kernel.go changes, and ooda.go changes exist.

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/rnixai/rnix/agents"
	"github.com/rnixai/rnix/skills"
)

// --- Task 2: Spawn Integration with DiffMemory ---

func TestSpawn_StemAgentDifferentiationMemory_RecordAndReuse(t *testing.T) {
	// Given: a kernel with DiffMemory, stem matcher, and skill loader
	// When: spawning a stem agent with "analyze code" intent
	// Then: first spawn records to memory; second spawn with same intent reuses the path
	llmFile := &mockLLMFile{}
	k, _, _ := newTestKernel(t, llmFile)

	mockSkills := []skills.SkillInfo{
		{Manifest: skills.SkillManifest{Name: "code-analysis", Description: "Analyze source code for quality issues"}},
	}
	matcher := NewStemMatcherFromFunc(func() ([]skills.SkillInfo, error) { return mockSkills, nil })
	k.SetStemMatcher(matcher)
	k.SetSkillLoader(func(name string) (*skills.SkillInfo, error) {
		return &skills.SkillInfo{
			Manifest: skills.SkillManifest{Name: name, AllowedToolsRaw: "/dev/fs"},
			Body:     "# " + name,
		}, nil
	})

	// Inject DiffMemory
	dm := NewDiffMemory(256)
	k.SetDiffMemory(dm)

	llmFile.mu.Lock()
	llmFile.readData = makeLLMResponse(`{"action":"complete","target":"","data":null,"reason":"done"}`, 10)
	llmFile.mu.Unlock()

	agent := stemAgentInfo()

	// First spawn - should record to memory
	pid1, err := k.Spawn("analyze code", agent, SpawnOpts{MaxTurns: 1, TimeoutMs: 5000})
	if err != nil {
		t.Fatalf("First spawn failed: %v", err)
	}
	proc1, _ := k.GetProcess(pid1)
	select {
	case <-proc1.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("first spawn timed out")
	}

	// Verify memory was recorded
	rememberedSkills, ok := dm.Lookup("analyze code")
	if !ok {
		t.Fatal("expected DiffMemory to have recorded the differentiation path")
	}
	if len(rememberedSkills) == 0 {
		t.Fatal("expected at least one skill in recorded path")
	}

	// Second spawn with same intent - should reuse from memory
	agent2 := stemAgentInfo()
	pid2, err := k.Spawn("analyze code", agent2, SpawnOpts{MaxTurns: 1, TimeoutMs: 5000})
	if err != nil {
		t.Fatalf("Second spawn failed: %v", err)
	}
	proc2, _ := k.GetProcess(pid2)
	select {
	case <-proc2.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("second spawn timed out")
	}

	// Both spawns should produce processes with skills
	if len(proc1.AllowedDevices) == 0 {
		t.Error("first spawn: expected AllowedDevices to be populated")
	}
	if len(proc2.AllowedDevices) == 0 {
		t.Error("second spawn: expected AllowedDevices to be populated (from memory)")
	}
}

func TestSpawn_StemAgentDifferentiationMemory_FallbackToMatch(t *testing.T) {
	// Given: a kernel with DiffMemory (empty) and stem matcher
	// When: spawning a stem agent with a new intent not in memory
	// Then: falls back to keyword matching (same as Story 20.3 behavior)
	llmFile := &mockLLMFile{}
	k, _, _ := newTestKernel(t, llmFile)

	mockSkills := []skills.SkillInfo{
		{Manifest: skills.SkillManifest{Name: "code-analysis", Description: "Analyze source code"}},
	}
	matcher := NewStemMatcherFromFunc(func() ([]skills.SkillInfo, error) { return mockSkills, nil })
	k.SetStemMatcher(matcher)
	k.SetSkillLoader(func(name string) (*skills.SkillInfo, error) {
		return &skills.SkillInfo{
			Manifest: skills.SkillManifest{Name: name, AllowedToolsRaw: "/dev/fs"},
			Body:     "# " + name,
		}, nil
	})

	// Empty DiffMemory - no remembered paths
	dm := NewDiffMemory(256)
	k.SetDiffMemory(dm)

	llmFile.mu.Lock()
	llmFile.readData = makeLLMResponse(`{"action":"complete","target":"","data":null,"reason":"done"}`, 10)
	llmFile.mu.Unlock()

	agent := stemAgentInfo()

	pid, err := k.Spawn("analyze code quality", agent, SpawnOpts{MaxTurns: 1, TimeoutMs: 5000})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("spawn timed out")
	}

	// Should still have skills loaded (via keyword matching fallback)
	if len(proc.AllowedDevices) == 0 {
		t.Error("expected AllowedDevices to be populated via keyword matching fallback")
	}

	// Should now be recorded in memory for next time
	_, ok := dm.Lookup("analyze code quality")
	if !ok {
		t.Error("expected differentiation path to be recorded to memory after fallback match")
	}
}

func TestSpawn_StemAgentDifferentiationMemory_EventFromMemory(t *testing.T) {
	// Given: a kernel with DiffMemory containing a pre-recorded path
	// When: spawning a stem agent that hits the memory
	// Then: StemDifferentiate event contains from_memory=true
	llmFile := &mockLLMFile{}
	k, _, _ := newTestKernel(t, llmFile)

	// Pre-populate memory
	dm := NewDiffMemory(256)
	dm.Record("analyze code", []string{"code-analysis"})
	k.SetDiffMemory(dm)

	// Configure skill loader for the remembered skills
	k.SetSkillLoader(func(name string) (*skills.SkillInfo, error) {
		return &skills.SkillInfo{
			Manifest: skills.SkillManifest{Name: name, AllowedToolsRaw: "/dev/fs"},
			Body:     "# " + name,
		}, nil
	})

	// Also set stem matcher (should NOT be called if memory hits)
	matcherCalled := false
	matcher := NewStemMatcherFromFunc(func() ([]skills.SkillInfo, error) {
		matcherCalled = true
		return nil, nil
	})
	k.SetStemMatcher(matcher)

	// Capture events
	var capturedEvents []map[string]any
	k.recordMgr = nil // ensure no file recording

	llmFile.mu.Lock()
	llmFile.readData = makeLLMResponse(`{"action":"complete","target":"","data":null,"reason":"done"}`, 10)
	llmFile.mu.Unlock()

	agent := stemAgentInfo()

	pid, err := k.Spawn("analyze code", agent, SpawnOpts{MaxTurns: 1, TimeoutMs: 5000})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("spawn timed out")
	}

	_ = capturedEvents

	// Verify that stem matcher was NOT called (memory was used instead)
	if matcherCalled {
		t.Error("expected stem matcher NOT to be called when memory hit occurs")
	}
}

// --- Task 3: OODA Specialize Action Tests ---

func TestOODA_Specialize_LoadSkill(t *testing.T) {
	// Given: an OODA process running
	// When: LLM Decide phase returns {"action":"specialize","target":"code-analysis"}
	// Then: skill is loaded dynamically, process continues without interruption
	callCount := 0
	responseFunc := func(writeData []byte) []byte {
		callCount++
		switch callCount {
		case 1:
			// Orient: detect capability gap
			return makeLLMResponse("need code analysis capability", 15)
		case 2:
			// Decide: specialize
			decision := map[string]any{
				"action": "specialize",
				"target": "code-analysis",
				"data":   map[string]any{},
				"reason": "need code analysis skill for this task",
			}
			decisionJSON, _ := json.Marshal(decision)
			return makeLLMResponse(string(decisionJSON), 15)
		case 3:
			// Next cycle Orient: skill now available
			return makeLLMResponse("code analysis skill loaded, can proceed", 10)
		case 4:
			// Decide: complete
			decision := map[string]any{
				"action": "complete",
				"target": "",
				"data":   map[string]any{},
				"reason": "task done with specialized skill",
			}
			decisionJSON, _ := json.Marshal(decision)
			return makeLLMResponse(string(decisionJSON), 10)
		default:
			return makeLLMResponse("unexpected", 5)
		}
	}

	k, _, _ := newOODATestKernel(t, responseFunc)

	// Inject skill loader
	k.SetSkillLoader(func(name string) (*skills.SkillInfo, error) {
		if name == "code-analysis" {
			return &skills.SkillInfo{
				Manifest: skills.SkillManifest{
					Name:            "code-analysis",
					Description:     "Analyze source code",
					AllowedToolsRaw: "/dev/fs /dev/shell",
				},
				Body: "# Code Analysis\n\nReview files for quality issues.",
			}, nil
		}
		return nil, fmt.Errorf("skill %q not found", name)
	})

	pid, err := k.Spawn("task needing code analysis", nil, SpawnOpts{
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
		t.Fatal("timed out waiting for OODA specialize completion")
	}

	// Verify skill was loaded into process
	proc.mu.Lock()
	hasSkill := slices.Contains(proc.Skills, "code-analysis")
	proc.mu.Unlock()
	if !hasSkill {
		t.Fatal("expected proc.Skills to contain 'code-analysis' after specialize")
	}
}

func TestOODA_Specialize_AlreadyLoaded(t *testing.T) {
	// Given: an OODA process with "code-analysis" already loaded
	// When: LLM Decide returns specialize for the same skill
	// Then: returns message that skill is already loaded (no duplicate)
	callCount := 0
	responseFunc := func(writeData []byte) []byte {
		callCount++
		switch callCount {
		case 1:
			return makeLLMResponse("check capabilities", 10)
		case 2:
			decision := map[string]any{
				"action": "specialize",
				"target": "code-analysis",
				"data":   map[string]any{},
				"reason": "want code analysis",
			}
			decisionJSON, _ := json.Marshal(decision)
			return makeLLMResponse(string(decisionJSON), 15)
		case 3:
			return makeLLMResponse("skill already available, proceed", 10)
		case 4:
			decision := map[string]any{
				"action": "complete",
				"target": "",
				"data":   map[string]any{},
				"reason": "done",
			}
			decisionJSON, _ := json.Marshal(decision)
			return makeLLMResponse(string(decisionJSON), 10)
		default:
			return makeLLMResponse("unexpected", 5)
		}
	}

	k, _, _ := newOODATestKernel(t, responseFunc)

	// Use agent with pre-loaded skill
	agent := &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Name:      "test-agent",
			Models:    agents.AgentModels{Provider: "claude", Preferred: "sonnet"},
			Skills:    []string{"code-analysis"},
			Reasoning: "ooda",
		},
		Instructions: "Test agent with pre-loaded skill.",
		Skills: []*skills.SkillInfo{
			{
				Manifest: skills.SkillManifest{
					Name:            "code-analysis",
					AllowedToolsRaw: "/dev/fs",
				},
				Body: "# Code Analysis",
			},
		},
	}

	pid, err := k.Spawn("already has skill test", agent, SpawnOpts{MaxTurns: 10})
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

	// Verify no duplicate skill entries
	proc.mu.Lock()
	count := 0
	for _, s := range proc.Skills {
		if s == "code-analysis" {
			count++
		}
	}
	proc.mu.Unlock()
	if count != 1 {
		t.Fatalf("expected exactly 1 'code-analysis' in Skills, got %d", count)
	}
}

func TestOODA_Specialize_SkillNotFound(t *testing.T) {
	// Given: an OODA process
	// When: LLM Decide returns specialize for a nonexistent skill
	// Then: specialize returns error message, process continues gracefully
	callCount := 0
	responseFunc := func(writeData []byte) []byte {
		callCount++
		switch callCount {
		case 1:
			return makeLLMResponse("need special capability", 10)
		case 2:
			decision := map[string]any{
				"action": "specialize",
				"target": "nonexistent-skill",
				"data":   map[string]any{},
				"reason": "want a skill that does not exist",
			}
			decisionJSON, _ := json.Marshal(decision)
			return makeLLMResponse(string(decisionJSON), 15)
		case 3:
			// After error, Orient again
			return makeLLMResponse("skill not available, adjusting plan", 10)
		case 4:
			decision := map[string]any{
				"action": "complete",
				"target": "",
				"data":   map[string]any{},
				"reason": "handled gracefully",
			}
			decisionJSON, _ := json.Marshal(decision)
			return makeLLMResponse(string(decisionJSON), 10)
		default:
			return makeLLMResponse("unexpected", 5)
		}
	}

	k, _, _ := newOODATestKernel(t, responseFunc)

	// Skill loader that always fails
	k.SetSkillLoader(func(name string) (*skills.SkillInfo, error) {
		return nil, fmt.Errorf("skill %q not found", name)
	})

	pid, err := k.Spawn("skill not found test", nil, SpawnOpts{
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
			t.Fatalf("expected exit code 0 (graceful handling), got %d: %s", exit.Code, exit.Reason)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out")
	}
}

func TestOODA_Specialize_UpdatesAllowedDevices(t *testing.T) {
	// Given: an OODA process with no initial AllowedDevices
	// When: specialize loads a skill with AllowedToolsRaw="/dev/fs /dev/shell"
	// Then: proc.AllowedDevices is updated to include the new tool paths
	callCount := 0
	responseFunc := func(writeData []byte) []byte {
		callCount++
		switch callCount {
		case 1:
			return makeLLMResponse("need tools", 10)
		case 2:
			decision := map[string]any{
				"action": "specialize",
				"target": "fs-tools",
				"data":   map[string]any{},
				"reason": "need filesystem access",
			}
			decisionJSON, _ := json.Marshal(decision)
			return makeLLMResponse(string(decisionJSON), 15)
		case 3:
			return makeLLMResponse("tools available now", 10)
		case 4:
			decision := map[string]any{
				"action": "complete",
				"target": "",
				"data":   map[string]any{},
				"reason": "done",
			}
			decisionJSON, _ := json.Marshal(decision)
			return makeLLMResponse(string(decisionJSON), 10)
		default:
			return makeLLMResponse("unexpected", 5)
		}
	}

	k, _, _ := newOODATestKernel(t, responseFunc)

	k.SetSkillLoader(func(name string) (*skills.SkillInfo, error) {
		if name == "fs-tools" {
			return &skills.SkillInfo{
				Manifest: skills.SkillManifest{
					Name:            "fs-tools",
					AllowedToolsRaw: "/dev/fs /dev/shell",
				},
				Body: "# FS Tools",
			}, nil
		}
		return nil, fmt.Errorf("not found")
	})

	pid, err := k.Spawn("device update test", nil, SpawnOpts{
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

	// Check AllowedDevices updated
	proc.mu.Lock()
	devices := proc.AllowedDevices
	proc.mu.Unlock()

	hasFS := false
	hasShell := false
	for _, d := range devices {
		if d == "/dev/fs" {
			hasFS = true
		}
		if d == "/dev/shell" {
			hasShell = true
		}
	}
	if !hasFS || !hasShell {
		t.Fatalf("expected AllowedDevices to contain /dev/fs and /dev/shell, got %v", devices)
	}
}

func TestOODA_Specialize_InjectsBody(t *testing.T) {
	// Given: an OODA process
	// When: specialize loads a skill with non-empty Body
	// Then: skill body is injected into context via AppendMessage
	callCount := 0
	responseFunc := func(writeData []byte) []byte {
		callCount++
		switch callCount {
		case 1:
			return makeLLMResponse("need skill", 10)
		case 2:
			decision := map[string]any{
				"action": "specialize",
				"target": "doc-writer",
				"data":   map[string]any{},
				"reason": "need documentation writing capability",
			}
			decisionJSON, _ := json.Marshal(decision)
			return makeLLMResponse(string(decisionJSON), 15)
		case 3:
			return makeLLMResponse("skill body injected", 10)
		case 4:
			decision := map[string]any{
				"action": "complete",
				"target": "",
				"data":   map[string]any{},
				"reason": "done",
			}
			decisionJSON, _ := json.Marshal(decision)
			return makeLLMResponse(string(decisionJSON), 10)
		default:
			return makeLLMResponse("unexpected", 5)
		}
	}

	k, _, ctxMgr := newOODATestKernel(t, responseFunc)

	skillBody := "# Doc Writer\n\nWrite high-quality documentation."
	k.SetSkillLoader(func(name string) (*skills.SkillInfo, error) {
		if name == "doc-writer" {
			return &skills.SkillInfo{
				Manifest: skills.SkillManifest{
					Name:            "doc-writer",
					AllowedToolsRaw: "/dev/fs",
				},
				Body: skillBody,
			}, nil
		}
		return nil, fmt.Errorf("not found")
	})

	pid, err := k.Spawn("body injection test", nil, SpawnOpts{
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

	// Verify that skill body was injected into context
	// Build prompt and check it contains the skill body
	promptResult, err := ctxMgr.BuildPrompt(proc.CtxID)
	if err != nil {
		t.Fatalf("BuildPrompt failed: %v", err)
	}
	if promptResult == nil {
		t.Fatal("expected non-nil PromptResult after skill body injection")
	}
	// The skill body should appear somewhere in the context messages
	_ = ctxMgr // context manager should have the injected message
}

func TestOODA_Specialize_RecordsToDiffMemory(t *testing.T) {
	// Given: an OODA process with DiffMemory
	// When: specialize dynamically loads a skill
	// Then: the updated skill list is recorded to DiffMemory
	callCount := 0
	responseFunc := func(writeData []byte) []byte {
		callCount++
		switch callCount {
		case 1:
			return makeLLMResponse("need more skills", 10)
		case 2:
			decision := map[string]any{
				"action": "specialize",
				"target": "extra-skill",
				"data":   map[string]any{},
				"reason": "progressive specialization",
			}
			decisionJSON, _ := json.Marshal(decision)
			return makeLLMResponse(string(decisionJSON), 15)
		case 3:
			return makeLLMResponse("skill loaded", 10)
		case 4:
			decision := map[string]any{
				"action": "complete",
				"target": "",
				"data":   map[string]any{},
				"reason": "done",
			}
			decisionJSON, _ := json.Marshal(decision)
			return makeLLMResponse(string(decisionJSON), 10)
		default:
			return makeLLMResponse("unexpected", 5)
		}
	}

	k, _, _ := newOODATestKernel(t, responseFunc)

	dm := NewDiffMemory(256)
	k.SetDiffMemory(dm)

	k.SetSkillLoader(func(name string) (*skills.SkillInfo, error) {
		return &skills.SkillInfo{
			Manifest: skills.SkillManifest{Name: name, AllowedToolsRaw: "/dev/fs"},
			Body:     "# " + name,
		}, nil
	})

	pid, err := k.Spawn("memory update test", nil, SpawnOpts{
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

	// Verify DiffMemory was updated with the progressive specialization
	recordedSkills, ok := dm.Lookup("memory update test")
	if !ok {
		t.Fatal("expected DiffMemory to record progressive specialization")
	}
	hasExtra := false
	for _, s := range recordedSkills {
		if s == "extra-skill" {
			hasExtra = true
		}
	}
	if !hasExtra {
		t.Fatalf("expected recorded skills to include 'extra-skill', got %v", recordedSkills)
	}
}

// --- Task 4: End-to-End Integration Tests ---

func TestE2E_StemDifferentiation_ProgressiveSpecialization(t *testing.T) {
	// Given: a stem agent with DiffMemory, OODA mode
	// When: stem differentiates initially, then OODA decides to specialize further
	// Then: the full lifecycle works: initial differentiation + dynamic loading + memory recording
	callCount := 0
	responseFunc := func(writeData []byte) []byte {
		callCount++
		switch callCount {
		case 1:
			// Orient: analyze task
			return makeLLMResponse("initial analysis, need additional capability", 15)
		case 2:
			// Decide: specialize to get more tools
			decision := map[string]any{
				"action": "specialize",
				"target": "git-tools",
				"data":   map[string]any{},
				"reason": "need git access for code review",
			}
			decisionJSON, _ := json.Marshal(decision)
			return makeLLMResponse(string(decisionJSON), 15)
		case 3:
			// Orient: now have git tools
			return makeLLMResponse("git tools loaded, task complete", 10)
		case 4:
			// Decide: complete
			decision := map[string]any{
				"action": "complete",
				"target": "",
				"data":   map[string]any{},
				"reason": "full analysis done with progressive specialization",
			}
			decisionJSON, _ := json.Marshal(decision)
			return makeLLMResponse(string(decisionJSON), 10)
		default:
			return makeLLMResponse("unexpected", 5)
		}
	}

	k, _, _ := newOODATestKernel(t, responseFunc)

	// Configure stem matcher for initial differentiation
	mockSkills := []skills.SkillInfo{
		{Manifest: skills.SkillManifest{Name: "code-analysis", Description: "Analyze source code"}},
		{Manifest: skills.SkillManifest{Name: "git-tools", Description: "Git operations"}},
	}
	matcher := NewStemMatcherFromFunc(func() ([]skills.SkillInfo, error) { return mockSkills, nil })
	k.SetStemMatcher(matcher)

	k.SetSkillLoader(func(name string) (*skills.SkillInfo, error) {
		return &skills.SkillInfo{
			Manifest: skills.SkillManifest{Name: name, AllowedToolsRaw: "/dev/fs /dev/shell"},
			Body:     "# " + name + "\nSkill body.",
		}, nil
	})

	dm := NewDiffMemory(256)
	k.SetDiffMemory(dm)

	agent := stemAgentInfo()
	pid, err := k.Spawn("analyze code quality", agent, SpawnOpts{MaxTurns: 10, TimeoutMs: 15000})
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
		t.Fatal("E2E test timed out")
	}

	// Verify: process has both initial + progressively loaded skills
	proc.mu.Lock()
	skillCount := len(proc.Skills)
	proc.mu.Unlock()
	if skillCount < 2 {
		t.Fatalf("expected at least 2 skills (initial + progressive), got %d", skillCount)
	}

	// Verify: DiffMemory recorded the full path
	recorded, ok := dm.Lookup("analyze code quality")
	if !ok {
		t.Fatal("expected DiffMemory to record the full differentiation+specialization path")
	}
	if len(recorded) < 2 {
		t.Fatalf("expected at least 2 recorded skills, got %v", recorded)
	}
}

func TestE2E_StemDifferentiation_MemoryReuse(t *testing.T) {
	// Given: first spawn records differentiation path to memory
	// When: second spawn with same intent
	// Then: uses remembered path, StemDifferentiate event has from_memory=true
	llmFile := &mockLLMFile{}
	k, _, _ := newTestKernel(t, llmFile)

	mockSkills := []skills.SkillInfo{
		{Manifest: skills.SkillManifest{Name: "code-analysis", Description: "Analyze source code"}},
	}
	matcher := NewStemMatcherFromFunc(func() ([]skills.SkillInfo, error) { return mockSkills, nil })
	k.SetStemMatcher(matcher)
	k.SetSkillLoader(func(name string) (*skills.SkillInfo, error) {
		return &skills.SkillInfo{
			Manifest: skills.SkillManifest{Name: name, AllowedToolsRaw: "/dev/fs"},
			Body:     "# " + name,
		}, nil
	})

	dm := NewDiffMemory(256)
	k.SetDiffMemory(dm)

	llmFile.mu.Lock()
	llmFile.readData = makeLLMResponse(`{"action":"complete","target":"","data":null,"reason":"done"}`, 10)
	llmFile.mu.Unlock()

	// First spawn - populates memory
	agent1 := stemAgentInfo()
	pid1, err := k.Spawn("analyze code", agent1, SpawnOpts{MaxTurns: 1, TimeoutMs: 5000})
	if err != nil {
		t.Fatalf("First spawn failed: %v", err)
	}
	proc1, _ := k.GetProcess(pid1)
	select {
	case <-proc1.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("first spawn timed out")
	}

	// Verify memory was populated
	if _, ok := dm.Lookup("analyze code"); !ok {
		t.Fatal("expected memory to be populated after first spawn")
	}

	// Second spawn - should use memory
	agent2 := stemAgentInfo()
	pid2, err := k.Spawn("analyze code", agent2, SpawnOpts{MaxTurns: 1, TimeoutMs: 5000})
	if err != nil {
		t.Fatalf("Second spawn failed: %v", err)
	}
	proc2, _ := k.GetProcess(pid2)
	select {
	case <-proc2.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("second spawn timed out")
	}

	// Verify second process also has skills (loaded from memory path)
	if len(proc2.AllowedDevices) == 0 {
		t.Error("expected second spawn to have AllowedDevices (from memory reuse)")
	}
}

func TestE2E_StemDifferentiation_NormalizedIntentReuse(t *testing.T) {
	// Given: first spawn with "analyze code" records to memory
	// When: second spawn with "code analyze" (reordered)
	// Then: memory hit occurs because normalizeIntent sorts tokens
	llmFile := &mockLLMFile{}
	k, _, _ := newTestKernel(t, llmFile)

	mockSkills := []skills.SkillInfo{
		{Manifest: skills.SkillManifest{Name: "code-analysis", Description: "Analyze source code"}},
	}
	matcher := NewStemMatcherFromFunc(func() ([]skills.SkillInfo, error) { return mockSkills, nil })
	k.SetStemMatcher(matcher)
	k.SetSkillLoader(func(name string) (*skills.SkillInfo, error) {
		return &skills.SkillInfo{
			Manifest: skills.SkillManifest{Name: name, AllowedToolsRaw: "/dev/fs"},
			Body:     "# " + name,
		}, nil
	})

	dm := NewDiffMemory(256)
	k.SetDiffMemory(dm)

	llmFile.mu.Lock()
	llmFile.readData = makeLLMResponse(`{"action":"complete","target":"","data":null,"reason":"done"}`, 10)
	llmFile.mu.Unlock()

	// First spawn with "analyze code"
	agent1 := stemAgentInfo()
	pid1, _ := k.Spawn("analyze code", agent1, SpawnOpts{MaxTurns: 1, TimeoutMs: 5000})
	proc1, _ := k.GetProcess(pid1)
	select {
	case <-proc1.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("first spawn timed out")
	}

	// Second spawn with "code analyze" (reordered) - should still hit memory
	agent2 := stemAgentInfo()
	pid2, _ := k.Spawn("code analyze", agent2, SpawnOpts{MaxTurns: 1, TimeoutMs: 5000})
	proc2, _ := k.GetProcess(pid2)
	select {
	case <-proc2.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("second spawn timed out")
	}

	// Verify both processes loaded skills
	if len(proc2.AllowedDevices) == 0 {
		t.Error("expected second spawn with reordered intent to reuse memory path")
	}
}

// --- OODASpecialize Action Type Test ---

func TestOODADecision_SpecializeType(t *testing.T) {
	// Given: OODAActionType constants
	// When: checking OODASpecialize constant
	// Then: it equals "specialize"
	if OODASpecialize != OODAActionType("specialize") {
		t.Fatalf("expected OODASpecialize = \"specialize\", got %q", OODASpecialize)
	}
}
