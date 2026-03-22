package kernel

import (
	"sync"
	"testing"
	"time"

	"github.com/rnixai/rnix/agents"
	"github.com/rnixai/rnix/skills"
	"github.com/rnixai/rnix/internal/types"
)

// =============================================================================
// ATDD Story 20.3: Stem Agent & Auto-Differentiation -- Integration Tests
// TDD RED PHASE - All tests designed to FAIL until implementation exists
// =============================================================================

// --- Task 3: Stem Agent Spawn Differentiation (AC: #1, #2) ---

// stemAgentInfo creates a stem agent definition for testing.
func stemAgentInfo() *agents.AgentInfo {
	return &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Name:          "stem",
			Description:   "Universal base agent that differentiates based on intent",
			Models:        agents.AgentModels{Provider: "claude", Preferred: "sonnet"},
			ContextBudget: 16384,
			Skills:        []string{}, // empty: differentiation fills this
			
		},
		Instructions: "You are a Stem Agent. Execute your mission using available tools.",
		Skills:       []*skills.SkillInfo{}, // empty: differentiation fills this
	}
}

func TestSpawn_StemAgentDifferentiation(t *testing.T) {
	// Given a kernel with stem matcher and skill loader configured
	llmFile := &mockLLMFile{}
	k, _, _ := newTestKernel(t, llmFile)

	// Configure stem matcher with mock discovery
	mockSkills := []skills.SkillInfo{
		{Manifest: skills.SkillManifest{Name: "code-analysis", Description: "Analyze source code for quality issues and bugs"}},
		{Manifest: skills.SkillManifest{Name: "git-tools", Description: "Git version control operations"}},
	}
	discoverFn := func() ([]skills.SkillInfo, error) { return mockSkills, nil }
	matcher := NewStemMatcherFromFunc(discoverFn)
	k.SetStemMatcher(matcher)

	// Configure skill loader that returns full skill info
	k.SetSkillLoader(func(name string) (*skills.SkillInfo, error) {
		for _, s := range mockSkills {
			if s.Manifest.Name == name {
				return &skills.SkillInfo{
					Manifest: skills.SkillManifest{
						Name:            s.Manifest.Name,
						Description:     s.Manifest.Description,
						AllowedToolsRaw: "/dev/fs /dev/shell",
					},
					Body: "# " + name + "\n\nSkill body for testing.",
				}, nil
			}
		}
		return nil, errTestDiscovery
	})

	// Set LLM response for linear reasoning
	llmFile.mu.Lock()
	llmFile.readData = makeLLMResponse("Orientation: analyzing code quality", 10)
	llmFile.mu.Unlock()

	agent := stemAgentInfo()

	// When spawning with stem agent and code analysis intent
	pid, err := k.Spawn("analyze code quality", agent, SpawnOpts{
		MaxTurns:  1,
		TimeoutMs: 5000,
	})

	// Then spawn succeeds
	if err != nil {
		t.Fatalf("Spawn stem agent returned error: %v", err)
	}
	if pid == 0 {
		t.Fatal("Spawn returned PID 0")
	}

	// Wait briefly for process to start
	time.Sleep(100 * time.Millisecond)

	// Verify process exists and has differentiated skills
	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d not found after spawn", pid)
	}

	// Verify skills were loaded via differentiation
	if len(proc.AllowedDevices) == 0 {
		t.Error("expected AllowedDevices to be populated after stem differentiation")
	}

}

func TestSpawn_StemAgentNoMatch(t *testing.T) {
	// Given a kernel with stem matcher configured but no matching skills
	llmFile := &mockLLMFile{}
	k, _, _ := newTestKernel(t, llmFile)

	// Configure stem matcher with skills that won't match
	mockSkills := []skills.SkillInfo{
		{Manifest: skills.SkillManifest{Name: "code-analysis", Description: "Analyze source code"}},
	}
	discoverFn := func() ([]skills.SkillInfo, error) { return mockSkills, nil }
	matcher := NewStemMatcherFromFunc(discoverFn)
	k.SetStemMatcher(matcher)
	k.SetSkillLoader(func(name string) (*skills.SkillInfo, error) {
		return nil, errTestDiscovery
	})

	// Set LLM response for bare process
	llmFile.mu.Lock()
	llmFile.readData = makeLLMResponse("done", 10)
	llmFile.mu.Unlock()

	agent := stemAgentInfo()

	// When spawning with an unrelated intent
	pid, err := k.Spawn("cook dinner recipe", agent, SpawnOpts{
		MaxTurns:  1,
		TimeoutMs: 5000,
	})

	// Then spawn still succeeds (runs as bare process without skills)
	if err != nil {
		t.Fatalf("Spawn stem agent with no match returned error: %v", err)
	}
	if pid == 0 {
		t.Fatal("Spawn returned PID 0")
	}
}

func TestSpawn_StemAgentDifferentiationLog(t *testing.T) {
	// Given a kernel with recording enabled and stem matcher
	llmFile := &mockLLMFile{}
	k, _, _ := newTestKernel(t, llmFile)

	mockSkills := []skills.SkillInfo{
		{Manifest: skills.SkillManifest{Name: "code-analysis", Description: "Analyze source code for quality issues"}},
	}
	discoverFn := func() ([]skills.SkillInfo, error) { return mockSkills, nil }
	matcher := NewStemMatcherFromFunc(discoverFn)
	k.SetStemMatcher(matcher)
	k.SetSkillLoader(func(name string) (*skills.SkillInfo, error) {
		return &skills.SkillInfo{
			Manifest: skills.SkillManifest{Name: name, Description: "test"},
			Body:     "# " + name,
		}, nil
	})

	// Capture events
	var events []string
	var eventMu sync.Mutex
	captureCallback := &testCallbacks{
		onSpawn: func(pid types.PID, intent, provider, model, uuid string) {
			eventMu.Lock()
			defer eventMu.Unlock()
			events = append(events, "spawn:"+intent)
		},
	}
	k.callbacks = captureCallback

	llmFile.mu.Lock()
	llmFile.readData = makeLLMResponse(`{"action":"complete","target":"","data":null,"reason":"done"}`, 10)
	llmFile.mu.Unlock()

	agent := stemAgentInfo()

	// When spawning with stem agent
	pid, err := k.Spawn("analyze code", agent, SpawnOpts{
		MaxTurns:  1,
		TimeoutMs: 5000,
	})

	_ = pid

	// Then differentiation produces log output
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}

	// Wait for events to propagate
	time.Sleep(100 * time.Millisecond)

	eventMu.Lock()
	defer eventMu.Unlock()
	if len(events) == 0 {
		t.Error("expected at least one event from stem differentiation")
	}
}

// testCallbacks is a minimal KernelCallbacks implementation for testing.
type testCallbacks struct {
	onSpawn        func(pid types.PID, intent, provider, model, uuid string)
	onStep         func(pid types.PID, step int, total int)
	onStepComplete func(pid types.PID, step int, action string, summary string, hasError bool, durationMs float64)
	onComplete     func(pid types.PID, result string, exit ExitStatus)
	onError        func(pid types.PID, err error)
}

func (tc *testCallbacks) OnSpawn(pid types.PID, intent, provider, model, uuid string) {
	if tc.onSpawn != nil {
		tc.onSpawn(pid, intent, provider, model, uuid)
	}
}
func (tc *testCallbacks) OnStep(pid types.PID, step int, total int) {
	if tc.onStep != nil {
		tc.onStep(pid, step, total)
	}
}
func (tc *testCallbacks) OnStepComplete(pid types.PID, step int, action string, summary string, hasError bool, durationMs float64) {
	if tc.onStepComplete != nil {
		tc.onStepComplete(pid, step, action, summary, hasError, durationMs)
	}
}
func (tc *testCallbacks) OnComplete(pid types.PID, result string, exit ExitStatus) {
	if tc.onComplete != nil {
		tc.onComplete(pid, result, exit)
	}
}
func (tc *testCallbacks) OnError(pid types.PID, err error) {
	if tc.onError != nil {
		tc.onError(pid, err)
	}
}

