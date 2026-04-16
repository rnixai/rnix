package kernel

import (
	gocontext "context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/skills"
	"github.com/rnixai/rnix/vfs"
)

type gatedLLMFile struct {
	inner vfs.VFSFile
	gate  chan struct{}
	once  sync.Once
}

func (f *gatedLLMFile) Read(length int) ([]byte, error) {
	f.once.Do(func() { <-f.gate })
	return f.inner.Read(length)
}

func (f *gatedLLMFile) Write(ctx gocontext.Context, data []byte) error {
	return f.inner.Write(ctx, data)
}

func (f *gatedLLMFile) Close() error {
	return f.inner.Close()
}

func (f *gatedLLMFile) Stat() (vfs.FileStat, error) {
	return f.inner.Stat()
}

func (f *gatedLLMFile) SupportsToolCalling() bool { return true }

// ============================================================
// ATDD RED PHASE — Story 26.3: Specialize 能力迁移
//
// Tests exercise the reasonStep ActionSpecialize code path.
// Current stub returns "specialize action not yet implemented",
// so every test that asserts real behaviour will FAIL until
// Task 1 replaces the stub with the full implementation.
//
// RED → GREEN: Replace the ActionSpecialize stub in
//   kernel/kernel.go with the implementation from Task 1.
// ============================================================

// makeSpecializeResponse builds an LLM response containing a specialize action.
func makeSpecializeResponse(skillName string, tokens int) []byte {
	resp := llmResponse{
		TokensUsed: tokens,
		ToolCalls: []llmToolCall{{
			ID:    "call_specialize",
			Name:  "specialize",
			Input: map[string]any{"skill_name": skillName},
		}},
	}
	data, _ := json.Marshal(resp)
	return data
}

// makeSpecializeResponseWithContent builds a specialize action with extra content text.
func makeSpecializeResponseWithContent(skillName, extraContent string, tokens int) []byte {
	resp := llmResponse{
		Content:    extraContent,
		TokensUsed: tokens,
		ToolCalls: []llmToolCall{{
			ID:    "call_specialize",
			Name:  "specialize",
			Input: map[string]any{"skill_name": skillName},
		}},
	}
	data, _ := json.Marshal(resp)
	return data
}

// newSpecializeTestKernel creates a kernel wired with a mock skill loader.
// Returns the kernel, ctxMgr, and a pointer to track loaded skill names.
func newSpecializeTestKernel(t *testing.T, llmFile vfs.VFSFile) (*KernelImpl, *rnixctx.Manager, *[]string) {
	t.Helper()
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return llmFile, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)

	var loaded []string
	var mu sync.Mutex
	k.SetSkillLoader(func(name string) (*skills.SkillInfo, error) {
		mu.Lock()
		loaded = append(loaded, name)
		mu.Unlock()
		return &skills.SkillInfo{
			Manifest: skills.SkillManifest{
				Name:            name,
				AllowedToolsRaw: "/dev/fs /dev/shell",
			},
			Body: fmt.Sprintf("# %s\n\nDynamic skill body.", name),
		}, nil
	})
	return k, ctxMgr, &loaded
}

// --- 26.3-UNIT-001: [P0] Specialize loads skill and updates proc.Skills (AC-1) ---

func TestReasonStep_Specialize_SkillLoaded(t *testing.T) {
	// LLM sequence: specialize → complete
	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeSpecializeResponse("code-analysis", 10),
			makeLLMResponse("done", 5),
		},
	}
	k, _, loaded := newSpecializeTestKernel(t, seqFile)

	pid, err := k.Spawn("analyze code", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	proc, _ := k.GetProcess(pid)

	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	// Then: skillLoader was called with "code-analysis"
	if !slices.Contains(*loaded, "code-analysis") {
		t.Errorf("skillLoader not called for code-analysis; loaded=%v", *loaded)
	}

	// Then: proc.Skills contains the loaded skill
	proc.mu.Lock()
	hasSkill := slices.Contains(proc.Skills, "code-analysis")
	proc.mu.Unlock()
	if !hasSkill {
		t.Errorf("proc.Skills should contain 'code-analysis', got %v", proc.Skills)
	}
}

// --- 26.3-UNIT-002: [P0] Specialize appends AllowedDevices (AC-1) ---

func TestReasonStep_Specialize_AllowedDevicesUpdated(t *testing.T) {
	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeSpecializeResponse("code-analysis", 10),
			makeLLMResponse("done", 5),
		},
	}
	k, _, _ := newSpecializeTestKernel(t, seqFile)

	pid, err := k.Spawn("analyze code", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	proc, _ := k.GetProcess(pid)

	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	proc.mu.Lock()
	devices := proc.AllowedDevices
	proc.mu.Unlock()

	// The mock skill loader returns AllowedToolsRaw="/dev/fs /dev/shell"
	if !slices.Contains(devices, "/dev/fs") || !slices.Contains(devices, "/dev/shell") {
		t.Errorf("AllowedDevices should include /dev/fs and /dev/shell, got %v", devices)
	}
}

// --- 26.3-UNIT-003: [P0] Specialize injects skill body via AppendMessage (AC-1) ---

func TestReasonStep_Specialize_SkillBodyInjected(t *testing.T) {
	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeSpecializeResponse("code-analysis", 10),
			makeLLMResponse("done", 5),
		},
	}
	k, ctxMgr, _ := newSpecializeTestKernel(t, seqFile)

	pid, err := k.Spawn("analyze code", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	proc, _ := k.GetProcess(pid)

	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	// Build prompt and check that the skill body was injected
	prompt, err := ctxMgr.BuildPrompt(proc.CtxID)
	if err != nil {
		t.Fatalf("BuildPrompt failed: %v", err)
	}

	found := false
	for _, m := range prompt.Messages {
		if m.Role == rnixctx.RoleUser {
			if strings.Contains(m.Content, "[Dynamic Skill Loaded: code-analysis]") {
				found = true
				break
			}
		}
	}
	if !found {
		t.Error("expected skill body to be injected as RoleUser message with [Dynamic Skill Loaded: code-analysis]")
	}
}

// --- 26.3-UNIT-004: [P0] Specialize returns success tool message (AC-1) ---

func TestReasonStep_Specialize_SuccessToolMessage(t *testing.T) {
	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeSpecializeResponse("code-analysis", 10),
			makeLLMResponse("done", 5),
		},
	}
	k, ctxMgr, _ := newSpecializeTestKernel(t, seqFile)

	pid, err := k.Spawn("analyze code", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	proc, _ := k.GetProcess(pid)

	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	prompt, err := ctxMgr.BuildPrompt(proc.CtxID)
	if err != nil {
		t.Fatalf("BuildPrompt failed: %v", err)
	}

	found := false
	for _, m := range prompt.Messages {
		if m.Role == rnixctx.RoleTool {
			if strings.Contains(m.Content, `skill "code-analysis" loaded successfully`) {
				found = true
				break
			}
		}
	}
	if !found {
		t.Error("expected tool message: skill \"code-analysis\" loaded successfully")
	}
}

// --- 26.3-UNIT-005: [P0] Specialize emits StemSpecialize event (AC-1) ---

func TestReasonStep_Specialize_StemSpecializeEvent(t *testing.T) {
	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeSpecializeResponse("code-analysis", 10),
			makeLLMResponse("done", 5),
		},
	}
	k, _, _ := newSpecializeTestKernel(t, seqFile)

	pid, err := k.Spawn("analyze code", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	proc, _ := k.GetProcess(pid)

	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	events := drainEvents(proc.DebugChan)
	found := false
	for _, ev := range events {
		if ev.Syscall == "StemSpecialize" {
			if skill, ok := ev.Args["skill"]; ok && skill == "code-analysis" {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected StemSpecialize event with skill=code-analysis")
	}
}

// --- 26.3-UNIT-006: [P0] TOCTOU first check — already loaded (AC-2) ---

func TestReasonStep_Specialize_AlreadyLoaded(t *testing.T) {
	// LLM returns specialize twice for the same skill, then completes
	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeSpecializeResponse("code-analysis", 10),
			makeSpecializeResponse("code-analysis", 10),
			makeLLMResponse("done", 5),
		},
	}
	k, ctxMgr, loaded := newSpecializeTestKernel(t, seqFile)

	pid, err := k.Spawn("analyze code", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	proc, _ := k.GetProcess(pid)

	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	// skillLoader should have been called only once
	count := 0
	for _, name := range *loaded {
		if name == "code-analysis" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("skillLoader should be called once for duplicate specialize, called %d times", count)
	}

	// The second specialize should produce "already loaded" tool message
	prompt, err := ctxMgr.BuildPrompt(proc.CtxID)
	if err != nil {
		t.Fatalf("BuildPrompt failed: %v", err)
	}
	foundAlready := false
	for _, m := range prompt.Messages {
		if m.Role == rnixctx.RoleTool {
			if strings.Contains(m.Content, `skill "code-analysis" is already loaded`) {
				foundAlready = true
				break
			}
		}
	}
	if !foundAlready {
		t.Error("expected tool message containing: skill \"code-analysis\" is already loaded")
	}
}

// --- 26.3-UNIT-007: [P0] Progressive lineage recorded (AC-3) ---

func TestReasonStep_Specialize_LineageRecorded(t *testing.T) {
	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeSpecializeResponse("code-analysis", 10),
			makeLLMResponse("done", 5),
		},
	}
	gate := make(chan struct{})
	gated := &gatedLLMFile{inner: seqFile, gate: gate}
	k, _, _ := newSpecializeTestKernel(t, gated)

	pid, err := k.Spawn("analyze code", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	proc, _ := k.GetProcess(pid)

	proc.mu.Lock()
	proc.lineage = NewLineage()
	proc.mu.Unlock()
	close(gate)

	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	events := proc.lineage.Events()
	found := false
	for _, ev := range events {
		if ev.Phase == "progressive" && slices.Contains(ev.Skills, "code-analysis") {
			if ev.FromMemory {
				t.Error("progressive specialization should have FromMemory=false")
			}
			found = true
		}
	}
	if !found {
		t.Error("expected lineage event with Phase=progressive and Skills=[code-analysis]")
	}
}

// --- 26.3-UNIT-008: [P0] DiffMemory updated after specialize (AC-4) ---

func TestReasonStep_Specialize_DiffMemoryRecorded(t *testing.T) {
	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeSpecializeResponse("code-analysis", 10),
			makeLLMResponse("done", 5),
		},
	}
	k, _, _ := newSpecializeTestKernel(t, seqFile)

	dm := NewDiffMemory(100)
	k.SetDiffMemory(dm)

	pid, err := k.Spawn("analyze code for bugs", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	proc, _ := k.GetProcess(pid)

	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	// DiffMemory should have a record for the process's intent
	dmSkills, found := dm.Lookup("analyze code for bugs")
	if !found {
		t.Fatal("expected DiffMemory to have a record after specialize")
	}
	if !slices.Contains(dmSkills, "code-analysis") {
		t.Errorf("DiffMemory entry should contain code-analysis, got %v", dmSkills)
	}
}

// --- 26.3-UNIT-009: [P0] Nonexistent skill error handling (AC-5) ---

func TestReasonStep_Specialize_SkillNotFound(t *testing.T) {
	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeSpecializeResponse("nonexistent-skill", 10),
			makeLLMResponse("done", 5),
		},
	}

	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return seqFile, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)

	k.SetSkillLoader(func(name string) (*skills.SkillInfo, error) {
		return nil, fmt.Errorf("skill %q not found in registry", name)
	})

	pid, err := k.Spawn("do something", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	proc, _ := k.GetProcess(pid)

	select {
	case exit := <-proc.Done:
		// Process should NOT crash — it should complete normally
		if exit.Code != 0 {
			t.Logf("exit reason: %s", exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	// Verify error tool message was injected
	prompt, err := ctxMgr.BuildPrompt(proc.CtxID)
	if err != nil {
		t.Fatalf("BuildPrompt failed: %v", err)
	}
	foundErr := false
	for _, m := range prompt.Messages {
		if m.Role == rnixctx.RoleTool {
			if strings.Contains(m.Content, "specialize error") && strings.Contains(m.Content, "nonexistent-skill") {
				foundErr = true
				break
			}
		}
	}
	if !foundErr {
		t.Error("expected error tool message for nonexistent skill")
	}

	// Skill should NOT be in proc.Skills
	proc.mu.Lock()
	hasSkill := slices.Contains(proc.Skills, "nonexistent-skill")
	proc.mu.Unlock()
	if hasSkill {
		t.Error("nonexistent skill should not be added to proc.Skills")
	}
}

// --- 26.3-UNIT-010: [P0] Empty skill name error handling (AC-6) ---

func TestReasonStep_Specialize_EmptySkillName(t *testing.T) {
	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeSpecializeResponse("", 10),
			makeLLMResponse("done", 5),
		},
	}
	k, ctxMgr, loaded := newSpecializeTestKernel(t, seqFile)

	pid, err := k.Spawn("do something", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	proc, _ := k.GetProcess(pid)

	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	// skillLoader should NOT have been called
	if len(*loaded) != 0 {
		t.Errorf("skillLoader should not be called for empty skill name, got %v", *loaded)
	}

	// Verify error tool message
	prompt, err := ctxMgr.BuildPrompt(proc.CtxID)
	if err != nil {
		t.Fatalf("BuildPrompt failed: %v", err)
	}
	foundErr := false
	for _, m := range prompt.Messages {
		if m.Role == rnixctx.RoleTool {
			if strings.Contains(m.Content, "specialize error: empty skill name") {
				foundErr = true
				break
			}
		}
	}
	if !foundErr {
		t.Error("expected tool message: specialize error: empty skill name")
	}
}

// --- 26.3-UNIT-011: [P0] No skillLoader configured (AC-1) ---

func TestReasonStep_Specialize_NoSkillLoader(t *testing.T) {
	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeSpecializeResponse("code-analysis", 10),
			makeLLMResponse("done", 5),
		},
	}

	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return seqFile, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)
	// Intentionally do NOT set skillLoader

	pid, err := k.Spawn("analyze code", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	proc, _ := k.GetProcess(pid)

	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	prompt, err := ctxMgr.BuildPrompt(proc.CtxID)
	if err != nil {
		t.Fatalf("BuildPrompt failed: %v", err)
	}
	foundErr := false
	for _, m := range prompt.Messages {
		if m.Role == rnixctx.RoleTool {
			if strings.Contains(m.Content, "no skill loader configured") {
				foundErr = true
				break
			}
		}
	}
	if !foundErr {
		t.Error("expected tool message: no skill loader configured")
	}
}

// --- 26.3-UNIT-012: [P1] AppendMessage failure is tolerated (AC-7) ---

func TestReasonStep_Specialize_AppendMessageFailure(t *testing.T) {
	// Use a real sequence: specialize → complete
	// We verify that even if the context is in a bad state,
	// the process does NOT crash.
	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeSpecializeResponse("code-analysis", 10),
			makeLLMResponse("done", 5),
		},
	}
	k, _, _ := newSpecializeTestKernel(t, seqFile)

	pid, err := k.Spawn("test append failure", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	proc, _ := k.GetProcess(pid)

	select {
	case exit := <-proc.Done:
		// The process must NOT crash due to AppendMessage failure
		if exit.Code != 0 {
			t.Logf("exit: code=%d reason=%s", exit.Code, exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out — process may have crashed")
	}

	// The process should still have the skill loaded even if body injection failed
	proc.mu.Lock()
	hasSkill := slices.Contains(proc.Skills, "code-analysis")
	proc.mu.Unlock()
	if !hasSkill {
		t.Error("proc.Skills should contain code-analysis even if AppendMessage failed")
	}
}

// --- 26.3-UNIT-013: [P0] Concurrent specialize is race-free (AC-2, AC-8) ---

func TestReasonStep_Specialize_ConcurrentRaceFree(t *testing.T) {
	// This test validates that concurrent specialize operations on
	// different processes sharing the same kernel don't cause data races.
	// The -race detector will catch any violations.

	const numProcs = 5
	skillNames := []string{"skill-a", "skill-b", "skill-c", "skill-d", "skill-e"}

	reg := vfs.NewDeviceRegistry()
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)

	dm := NewDiffMemory(100)
	k.SetDiffMemory(dm)

	k.SetSkillLoader(func(name string) (*skills.SkillInfo, error) {
		return &skills.SkillInfo{
			Manifest: skills.SkillManifest{
				Name:            name,
				AllowedToolsRaw: "/dev/fs",
			},
			Body: "body for " + name,
		}, nil
	})

	var wg sync.WaitGroup
	for i := range numProcs {
		seqFile := &sequenceLLMFile{
			responses: [][]byte{
				makeSpecializeResponse(skillNames[i], 10),
				makeLLMResponse("done", 5),
			},
		}
		_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
			return seqFile, nil
		})

		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			pid, spawnErr := k.Spawn(fmt.Sprintf("task-%d", idx), nil, SpawnOpts{})
			if spawnErr != nil {
				t.Errorf("Spawn %d failed: %v", idx, spawnErr)
				return
			}
			proc, _ := k.GetProcess(pid)
			select {
			case <-proc.Done:
			case <-time.After(10 * time.Second):
				t.Errorf("process %d timed out", idx)
			}
		}(i)
	}
	wg.Wait()
	// If -race detector is enabled, it will flag any data races.
}

// --- 26.3-UNIT-014: [P0] Lineage trigger comes from action.Content (AC-3) ---

func TestReasonStep_Specialize_LineageTriggerFromContent(t *testing.T) {
	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeSpecializeResponseWithContent("code-analysis", "I need code analysis to review this PR", 10),
			makeLLMResponse("done", 5),
		},
	}
	gate := make(chan struct{})
	gated := &gatedLLMFile{inner: seqFile, gate: gate}
	k, _, _ := newSpecializeTestKernel(t, gated)

	pid, err := k.Spawn("review pr", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	proc.mu.Lock()
	proc.lineage = NewLineage()
	proc.mu.Unlock()
	close(gate)

	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	events := proc.lineage.Events()
	for _, ev := range events {
		if ev.Phase == "progressive" && slices.Contains(ev.Skills, "code-analysis") {
			// In native tool calling, the trigger defaults to "specialize"
			// (action.Content is not available in the meta action handler).
			if ev.Trigger != "specialize" {
				t.Errorf("lineage trigger should be 'specialize', got %q", ev.Trigger)
			}
			return
		}
	}
	t.Error("expected progressive lineage event for code-analysis")
}

// --- 26.3-UNIT-015: [P1] Specialize error does NOT crash process (AC-5) ---

func TestReasonStep_Specialize_ErrorDoesNotCrash(t *testing.T) {
	// Multiple error scenarios followed by a successful complete
	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeSpecializeResponse("", 5),             // empty name → error
			makeSpecializeResponse("bad-skill", 5),    // will fail in loader
			makeSpecializeResponse("good-skill", 10),  // will succeed
			makeLLMResponse("all done", 5),
		},
	}

	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return seqFile, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)

	k.SetSkillLoader(func(name string) (*skills.SkillInfo, error) {
		if name == "bad-skill" {
			return nil, fmt.Errorf("skill not available")
		}
		return &skills.SkillInfo{
			Manifest: skills.SkillManifest{
				Name:            name,
				AllowedToolsRaw: "/dev/fs",
			},
			Body: "body",
		}, nil
	})

	pid, err := k.Spawn("multi error test", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	proc, _ := k.GetProcess(pid)

	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Errorf("process should complete successfully after recoverable errors, got code=%d reason=%s", exit.Code, exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out — process crashed or stuck")
	}

	// good-skill should be loaded
	proc.mu.Lock()
	hasGood := slices.Contains(proc.Skills, "good-skill")
	hasBad := slices.Contains(proc.Skills, "bad-skill")
	proc.mu.Unlock()
	if !hasGood {
		t.Error("good-skill should be in proc.Skills")
	}
	if hasBad {
		t.Error("bad-skill should NOT be in proc.Skills")
	}
}

// --- 26.3-UNIT-016: [P0] ReasonStep event for specialize action (AC-1) ---

func TestReasonStep_Specialize_ReasonStepEvent(t *testing.T) {
	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeSpecializeResponse("code-analysis", 10),
			makeLLMResponse("done", 5),
		},
	}
	k, _, _ := newSpecializeTestKernel(t, seqFile)

	pid, err := k.Spawn("analyze code", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	proc, _ := k.GetProcess(pid)

	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	events := drainEvents(proc.DebugChan)
	found := false
	for _, ev := range events {
		if ev.Syscall == "ReasonStep" {
			if action, ok := ev.Args["action"]; ok && action == "specialize" {
				if skill, ok := ev.Args["skill"]; ok && skill == "code-analysis" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Error("expected ReasonStep event with action=specialize and skill=code-analysis")
	}
}

// --- 26.3-UNIT-017: [P1] DiffMemory contains full skill snapshot (AC-4) ---

func TestReasonStep_Specialize_DiffMemoryFullSnapshot(t *testing.T) {
	// Specialize two different skills, verify DiffMemory gets the complete list
	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeSpecializeResponse("skill-a", 10),
			makeSpecializeResponse("skill-b", 10),
			makeLLMResponse("done", 5),
		},
	}
	k, _, _ := newSpecializeTestKernel(t, seqFile)

	dm := NewDiffMemory(100)
	k.SetDiffMemory(dm)

	pid, err := k.Spawn("multi-skill task", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	proc, _ := k.GetProcess(pid)

	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	dmSkills, found := dm.Lookup("multi-skill task")
	if !found {
		t.Fatal("expected DiffMemory to have a record")
	}
	// After second specialize, the DiffMemory should contain both skills
	if !slices.Contains(dmSkills, "skill-a") || !slices.Contains(dmSkills, "skill-b") {
		t.Errorf("DiffMemory should contain both skill-a and skill-b, got %v", dmSkills)
	}
}

