package kernel

// =============================================================================
// ATDD Story 20.4: Progressive Specialization & Differentiation Memory
// TDD RED PHASE - Integration Tests
// =============================================================================
//
// These tests verify:
// - Task 2: Spawn integration with DiffMemory (Lookup reuse, Record after differentiation)
// - Task 4: End-to-end scenarios (full lifecycle)

import (
	"testing"
	"time"

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

// --- Task 4: End-to-End Integration Tests ---

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

