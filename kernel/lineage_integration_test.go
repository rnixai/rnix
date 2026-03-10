package kernel

// =============================================================================
// ATDD Story 20.5: Differentiation Lineage Graph -- Integration Tests
// TDD RED PHASE - All tests designed to FAIL until implementation exists
// =============================================================================
//
// Test Strategy:
// - Task 2: Spawn integration (lineage created on stem differentiation)
// - Task 3: OODA specialize integration (progressive lineage recorded)
// - Task 4: Kernel GetLineage method
// - Task 6: End-to-end lineage flows
//
// Priority: P0/P1 (core lineage tracking across differentiation paths)
// Test Level: Integration

import (
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/agents"
	"github.com/rnixai/rnix/skills"
)

// --- Task 2: Spawn Integration with Lineage Recording (AC: #1) ---

func TestSpawn_StemAgent_RecordsLineage(t *testing.T) {
	// Given: a kernel with stem matcher configured
	// When: spawning a stem agent that successfully differentiates
	// Then: proc.lineage contains an "initial" event with the loaded skills
	llmFile := &mockLLMFile{}
	k, _, _ := newTestKernel(t, llmFile)

	mockSkills := []skills.SkillInfo{
		{Manifest: skills.SkillManifest{Name: "code-analysis", Description: "Analyze source code for quality issues"}},
		{Manifest: skills.SkillManifest{Name: "git-tools", Description: "Git version control operations"}},
	}
	discoverFn := func() ([]skills.SkillInfo, error) { return mockSkills, nil }
	matcher := NewStemMatcherFromFunc(discoverFn)
	k.SetStemMatcher(matcher)

	k.SetSkillLoader(func(name string) (*skills.SkillInfo, error) {
		for _, s := range mockSkills {
			if s.Manifest.Name == name {
				return &skills.SkillInfo{
					Manifest: skills.SkillManifest{
						Name:            s.Manifest.Name,
						Description:     s.Manifest.Description,
						AllowedToolsRaw: "/dev/fs",
					},
					Body: "# " + name,
				}, nil
			}
		}
		return nil, errTestDiscovery
	})

	llmFile.mu.Lock()
	llmFile.readData = makeLLMResponse("done", 10)
	llmFile.mu.Unlock()

	agent := stemAgentInfo()
	pid, err := k.Spawn("analyze code quality", agent, SpawnOpts{
		MaxTurns:  1,
		TimeoutMs: 5000,
	})
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d not found", pid)
	}

	lineage := proc.GetLineage()
	if lineage == nil {
		t.Fatal("expected lineage to be created after stem differentiation, got nil")
	}

	events := lineage.Events()
	if len(events) == 0 {
		t.Fatal("expected at least one lineage event after stem differentiation")
	}

	first := events[0]
	if first.Phase != "initial" {
		t.Errorf("expected first event phase='initial', got %q", first.Phase)
	}
	if first.Trigger != "analyze code quality" {
		t.Errorf("expected trigger='analyze code quality', got %q", first.Trigger)
	}
	if len(first.Skills) == 0 {
		t.Fatal("expected at least 1 skill in initial event")
	}
	if first.Skills[0] != "code-analysis" {
		t.Errorf("expected first skill='code-analysis', got %q", first.Skills[0])
	}
}

func TestSpawn_StemAgent_LineageFromMemory(t *testing.T) {
	// Given: a kernel with diffMemory containing a remembered path
	// When: spawning a stem agent that reuses memory
	// Then: lineage event has FromMemory=true
	llmFile := &mockLLMFile{}
	k, _, _ := newTestKernel(t, llmFile)

	mockSkills := []skills.SkillInfo{
		{Manifest: skills.SkillManifest{Name: "code-analysis", Description: "Analyze source code"}},
	}
	discoverFn := func() ([]skills.SkillInfo, error) { return mockSkills, nil }
	matcher := NewStemMatcherFromFunc(discoverFn)
	k.SetStemMatcher(matcher)

	k.SetSkillLoader(func(name string) (*skills.SkillInfo, error) {
		return &skills.SkillInfo{
			Manifest: skills.SkillManifest{Name: name, Description: "test", AllowedToolsRaw: "/dev/fs"},
			Body:     "# " + name,
		}, nil
	})

	// Pre-populate differentiation memory
	dm := NewDiffMemory(10)
	dm.Record("analyze code", []string{"code-analysis"})
	k.SetDiffMemory(dm)

	llmFile.mu.Lock()
	llmFile.readData = makeLLMResponse("done", 10)
	llmFile.mu.Unlock()

	agent := stemAgentInfo()
	pid, err := k.Spawn("analyze code", agent, SpawnOpts{
		MaxTurns:  1,
		TimeoutMs: 5000,
	})
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d not found", pid)
	}

	lineage := proc.GetLineage()
	if lineage == nil {
		t.Fatal("expected lineage after memory-reuse differentiation, got nil")
	}

	events := lineage.Events()
	if len(events) == 0 {
		t.Fatal("expected at least one lineage event")
	}
	if !events[0].FromMemory {
		t.Error("expected FromMemory=true for memory-reused differentiation")
	}
}

func TestSpawn_NonStemAgent_NoLineage(t *testing.T) {
	// Given: a kernel with a non-stem agent
	// When: spawning a regular (non-stem) agent
	// Then: proc.lineage is nil (no lineage tracking for non-stem agents)
	llmFile := &mockLLMFile{}
	k, _, _ := newTestKernel(t, llmFile)

	llmFile.mu.Lock()
	llmFile.readData = makeLLMResponse("done", 10)
	llmFile.mu.Unlock()

	agent := &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Name:          "regular-agent",
			Description:   "A regular non-stem agent",
			Models:        agents.AgentModels{Provider: "claude", Preferred: "sonnet"},
			ContextBudget: 4096,
			Skills:        []string{"some-skill"},
		},
		Instructions: "You are a regular agent.",
	}

	pid, err := k.Spawn("do something", agent, SpawnOpts{
		MaxTurns:  1,
		TimeoutMs: 5000,
	})
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d not found", pid)
	}

	lineage := proc.GetLineage()
	if lineage != nil {
		t.Error("expected nil lineage for non-stem agent, got non-nil")
	}
}

// --- Task 3: OODA Specialize Integration with Lineage Recording (AC: #2) ---

func TestOODA_Specialize_RecordsLineage(t *testing.T) {
	// Given: a running stem agent with an initial lineage event
	// When: oodaActSpecialize successfully loads a new skill
	// Then: lineage has a "progressive" event appended
	llmFile := &mockLLMFile{}
	k, _, _ := newTestKernel(t, llmFile)

	// Set up skill loader
	k.SetSkillLoader(func(name string) (*skills.SkillInfo, error) {
		return &skills.SkillInfo{
			Manifest: skills.SkillManifest{
				Name:            name,
				Description:     "Dynamically loaded skill",
				AllowedToolsRaw: "/dev/shell",
			},
			Body: "# " + name + "\nDynamic skill body.",
		}, nil
	})

	// Create a process with lineage already initialized (simulating post-spawn)
	proc := NewProcess(0, "analyze code quality", []string{"code-analysis"})
	proc.oodaEnabled = true
	proc.oodaState = &OODAState{Phase: PhaseAct}
	lineage := NewLineage()
	lineage.Record(LineageEvent{
		Timestamp: time.Now(),
		Phase:     "initial",
		Skills:    []string{"code-analysis"},
		Trigger:   "analyze code quality",
	})
	proc.SetLineage(lineage)

	k.procTable.Store(proc.PID, proc)

	// When: specialize action loads "test-runner" skill
	decision := &OODADecision{
		Action: OODASpecialize,
		Target: "test-runner",
		Reason: "need test execution capability for coverage analysis",
	}
	result := k.oodaActSpecialize(proc, decision)

	// Then: result indicates success
	if strings.HasPrefix(result, "specialize error") {
		t.Fatalf("specialize failed: %s", result)
	}

	// And: lineage now has 2 events (initial + progressive)
	events := proc.GetLineage().Events()
	if len(events) != 2 {
		t.Fatalf("expected 2 lineage events, got %d", len(events))
	}

	progressive := events[1]
	if progressive.Phase != "progressive" {
		t.Errorf("expected second event phase='progressive', got %q", progressive.Phase)
	}
	if len(progressive.Skills) != 1 || progressive.Skills[0] != "test-runner" {
		t.Errorf("expected progressive skill=['test-runner'], got %v", progressive.Skills)
	}
}

func TestOODA_Specialize_LineageTriggerFromReason(t *testing.T) {
	// Given: a running stem agent with lineage
	// When: oodaActSpecialize is called with a specific reason
	// Then: the progressive lineage event's Trigger matches the decision.Reason
	llmFile := &mockLLMFile{}
	k, _, _ := newTestKernel(t, llmFile)

	k.SetSkillLoader(func(name string) (*skills.SkillInfo, error) {
		return &skills.SkillInfo{
			Manifest: skills.SkillManifest{Name: name, Description: "test"},
			Body:     "# " + name,
		}, nil
	})

	proc := NewProcess(0, "build project", []string{})
	proc.oodaEnabled = true
	proc.oodaState = &OODAState{Phase: PhaseAct}
	lineage := NewLineage()
	proc.SetLineage(lineage)
	k.procTable.Store(proc.PID, proc)

	expectedReason := "discovered test files that need compilation"
	decision := &OODADecision{
		Action: OODASpecialize,
		Target: "build-tools",
		Reason: expectedReason,
	}
	k.oodaActSpecialize(proc, decision)

	events := proc.GetLineage().Events()
	if len(events) == 0 {
		t.Fatal("expected lineage event after specialize")
	}

	last := events[len(events)-1]
	if last.Trigger != expectedReason {
		t.Errorf("expected trigger=%q, got %q", expectedReason, last.Trigger)
	}
}

// --- Task 4: Kernel GetLineage Method (AC: #1, #2) ---

func TestKernel_GetLineage_Success(t *testing.T) {
	// Given: a kernel with a process that has lineage
	// When: calling GetLineage with valid PID
	// Then: returns the lineage events
	llmFile := &mockLLMFile{}
	k, _, _ := newTestKernel(t, llmFile)

	proc := NewProcess(0, "analyze code", []string{"code-analysis"})
	lineage := NewLineage()
	lineage.Record(LineageEvent{
		Timestamp: time.Now(),
		Phase:     "initial",
		Skills:    []string{"code-analysis"},
		Trigger:   "analyze code",
	})
	proc.SetLineage(lineage)
	k.procTable.Store(proc.PID, proc)

	events, err := k.GetLineage(proc.PID)
	if err != nil {
		t.Fatalf("GetLineage returned error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Phase != "initial" {
		t.Errorf("expected phase='initial', got %q", events[0].Phase)
	}
}

func TestKernel_GetLineage_ProcessNotFound(t *testing.T) {
	// Given: a kernel with no processes
	// When: calling GetLineage with non-existent PID
	// Then: returns error
	llmFile := &mockLLMFile{}
	k, _, _ := newTestKernel(t, llmFile)

	_, err := k.GetLineage(99999)
	if err == nil {
		t.Fatal("expected error for non-existent PID, got nil")
	}
}

func TestKernel_GetLineage_NoLineage(t *testing.T) {
	// Given: a kernel with a non-stem process (no lineage)
	// When: calling GetLineage
	// Then: returns nil events without error
	llmFile := &mockLLMFile{}
	k, _, _ := newTestKernel(t, llmFile)

	proc := NewProcess(0, "regular task", []string{})
	k.procTable.Store(proc.PID, proc)

	events, err := k.GetLineage(proc.PID)
	if err != nil {
		t.Fatalf("expected no error for process without lineage, got: %v", err)
	}
	if events != nil {
		t.Errorf("expected nil events for non-differentiated process, got %v", events)
	}
}

// --- Task 6: End-to-End Integration (AC: #1, #2) ---

func TestE2E_Lineage_StemDifferentiation(t *testing.T) {
	// Given: full kernel setup with stem agent
	// When: spawning and waiting for differentiation
	// Then: lineage query returns initial event with correct skills and trigger
	llmFile := &mockLLMFile{}
	k, _, _ := newTestKernel(t, llmFile)

	mockSkills := []skills.SkillInfo{
		{Manifest: skills.SkillManifest{Name: "code-analysis", Description: "Analyze code"}},
	}
	discoverFn := func() ([]skills.SkillInfo, error) { return mockSkills, nil }
	k.SetStemMatcher(NewStemMatcherFromFunc(discoverFn))
	k.SetSkillLoader(func(name string) (*skills.SkillInfo, error) {
		return &skills.SkillInfo{
			Manifest: skills.SkillManifest{Name: name, Description: "test", AllowedToolsRaw: "/dev/fs"},
			Body:     "# " + name,
		}, nil
	})

	llmFile.mu.Lock()
	llmFile.readData = makeLLMResponse("done", 10)
	llmFile.mu.Unlock()

	pid, err := k.Spawn("analyze code", stemAgentInfo(), SpawnOpts{
		MaxTurns:  1,
		TimeoutMs: 5000,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	events, err := k.GetLineage(pid)
	if err != nil {
		t.Fatalf("GetLineage: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected lineage events for stem differentiated process")
	}
	if events[0].Phase != "initial" {
		t.Errorf("expected 'initial' phase, got %q", events[0].Phase)
	}
}

func TestE2E_Lineage_MemoryReuse(t *testing.T) {
	// Given: a kernel with diffMemory
	// When: second spawn with same intent reuses memory
	// Then: lineage records FromMemory=true
	llmFile := &mockLLMFile{}
	k, _, _ := newTestKernel(t, llmFile)

	mockSkills := []skills.SkillInfo{
		{Manifest: skills.SkillManifest{Name: "code-analysis", Description: "Analyze code"}},
	}
	discoverFn := func() ([]skills.SkillInfo, error) { return mockSkills, nil }
	k.SetStemMatcher(NewStemMatcherFromFunc(discoverFn))
	k.SetSkillLoader(func(name string) (*skills.SkillInfo, error) {
		return &skills.SkillInfo{
			Manifest: skills.SkillManifest{Name: name, Description: "test", AllowedToolsRaw: "/dev/fs"},
			Body:     "# " + name,
		}, nil
	})

	dm := NewDiffMemory(10)
	dm.Record("analyze code", []string{"code-analysis"})
	k.SetDiffMemory(dm)

	llmFile.mu.Lock()
	llmFile.readData = makeLLMResponse("done", 10)
	llmFile.mu.Unlock()

	pid, err := k.Spawn("analyze code", stemAgentInfo(), SpawnOpts{
		MaxTurns:  1,
		TimeoutMs: 5000,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	events, err := k.GetLineage(pid)
	if err != nil {
		t.Fatalf("GetLineage: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected lineage events")
	}
	if !events[0].FromMemory {
		t.Error("expected FromMemory=true for second spawn with same intent")
	}
}

func TestE2E_Lineage_MultiplePids(t *testing.T) {
	// Given: two stem agent processes spawned with different intents
	// When: querying lineage for each PID
	// Then: each has independent lineage with different triggers
	llmFile := &mockLLMFile{}
	k, _, _ := newTestKernel(t, llmFile)

	mockSkills := []skills.SkillInfo{
		{Manifest: skills.SkillManifest{Name: "code-analysis", Description: "Analyze code"}},
		{Manifest: skills.SkillManifest{Name: "deploy-tools", Description: "Deployment operations"}},
	}
	discoverFn := func() ([]skills.SkillInfo, error) { return mockSkills, nil }
	k.SetStemMatcher(NewStemMatcherFromFunc(discoverFn))
	k.SetSkillLoader(func(name string) (*skills.SkillInfo, error) {
		return &skills.SkillInfo{
			Manifest: skills.SkillManifest{Name: name, Description: "test", AllowedToolsRaw: "/dev/fs"},
			Body:     "# " + name,
		}, nil
	})

	llmFile.mu.Lock()
	llmFile.readData = makeLLMResponse("done", 10)
	llmFile.mu.Unlock()

	pid1, err := k.Spawn("analyze code", stemAgentInfo(), SpawnOpts{
		MaxTurns:  1,
		TimeoutMs: 5000,
	})
	if err != nil {
		t.Fatalf("Spawn 1: %v", err)
	}

	pid2, err := k.Spawn("deploy application", stemAgentInfo(), SpawnOpts{
		MaxTurns:  1,
		TimeoutMs: 5000,
	})
	if err != nil {
		t.Fatalf("Spawn 2: %v", err)
	}

	time.Sleep(150 * time.Millisecond)

	events1, err := k.GetLineage(pid1)
	if err != nil {
		t.Fatalf("GetLineage pid1: %v", err)
	}
	events2, err := k.GetLineage(pid2)
	if err != nil {
		t.Fatalf("GetLineage pid2: %v", err)
	}

	if len(events1) == 0 || len(events2) == 0 {
		t.Fatal("expected lineage events for both PIDs")
	}

	if events1[0].Trigger == events2[0].Trigger {
		t.Error("expected different triggers for different PIDs, but they match")
	}
}
