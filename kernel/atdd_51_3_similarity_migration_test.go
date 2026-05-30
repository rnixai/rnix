package kernel

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rnixai/rnix/agents"
	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/skills"
	"github.com/rnixai/rnix/vfs"
)

// ATDD 51.3: 能力相似度迁移 — 破数据饥饿 + 结果级门禁闭环（EM-2）
//
// 来源 Story：_bmad-output/implementation-artifacts/51-3-similarity-migration-gated-closure.md
// 审计来源：completion-audit-suspects-2026-05-30.md §A2 (AUD-EM / EM-2)
// Story Key: 51-3-similarity-migration-gated-closure
//
// 验收标准（Acceptance Criteria，本文件覆盖项）：
//   AC1（矩阵 feed）     : 经真实 Spawn(带 agent+skills, immune 活跃) → similarity 矩阵非空
//   AC2（模板去重/守卫）  : RecordAgentSkills 空模板/空 skills 跳过；多 PID 同模板去重
//   AC4（CoopScore 接通） : feed + 51-2 coopHistory → CoopScore > 0
//   AC5（结果级净提升）   : 对照组(空矩阵→任务丢失) vs 实验组(相似 agent+真实 Spawn migrateFn→迁移续跑)
//   AC6（相似性选择）     : 相似 vs 无关候选 → TargetAgent 是相似者
//   AC7（nil-safe）       : nil daemon → RecordAgentSkills 无 panic、Spawn 行为不变
//   AC8（CLI 展示链）     : feed 后 GetSimilarAgents 返回非空
//   AC9（并发安全）       : 并发 spawn feed → go test -race 无数据竞争
//
// ============================== RED PHASE ==============================
// 本测试在 Story 51.3 实现前应 FAIL（红灯）。
// 预期失败原因：kernel 包无法编译——导出方法 RecordAgentSkills 尚未实现
// （undefined），且 spawn.go 尚未接通 similarity feed 注入点。实现后所有
// 用例应转绿（含 go test -race）。这是 Go ATDD 的标准红灯形态（缺失 API
// → 包编译失败），与 kernel 既有 atdd_* 集成测试的红灯模式一致。
//
// 注意：测试严格使用 Story Dev Notes 规定的契约：
//   - 新方法：func (d *ImmuneDaemon) RecordAgentSkills(template string, skills []string)
//     d==nil / 空模板 / 空 skills 早返回；d.mu.Lock 下写入内部累积器；
//     写入后调 UpdateSimilarityMatrix(累积 map) 全量重算。
//   - feed 注入点经 spawn.go 既有 `if k.immuneDaemon != nil` 守卫
//   - 结果级断言仅依赖既有公共 API (Spawn/SpawnSupervisor/GetSimilarity/
//     GetSimilarAgents/AttemptMigration) + 新 RecordAgentSkills
// ======================================================================

// --- Helpers ---

// testNamedAgentWithSkills builds a minimal agent with named skills so that
// spawn.go:61-67 populates proc.Skills, feeding the similarity matrix.
func testNamedAgentWithSkills(name string, skillNames ...string) *agents.AgentInfo {
	agent := &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Name: name,
			Models: agents.AgentModels{
				Provider:  "claude",
				Preferred: "sonnet",
				Fallback:  "haiku",
			},
			ContextBudget: 4096,
		},
		Instructions: "# " + name + "\n\nA minimal test agent.",
	}
	for _, sn := range skillNames {
		agent.Skills = append(agent.Skills, &skills.SkillInfo{
			Manifest: skills.SkillManifest{Name: sn},
		})
	}
	return agent
}

// newImmuneMigrationKernel wires a kernel with per-Open LLM factory and ACTIVE
// immune daemon for testing migration scenarios. The factory controls which mock
// LLM file each process gets (crash vs normal).
func newImmuneMigrationKernel(t *testing.T, factory func() vfs.VFSFile) (*KernelImpl, *ImmuneDaemon) {
	t.Helper()

	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return factory(), nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)

	store := NewImmuneStore(t.TempDir())
	d := NewImmuneDaemon(store, DefaultImmuneConfig())
	if err := d.Start(); err != nil {
		t.Fatalf("immune daemon Start: %v", err)
	}
	t.Cleanup(d.Stop)
	k.SetImmuneDaemon(d)

	return k, d
}

// =============================================================================
// AC7: nil daemon — RecordAgentSkills 无 panic
// =============================================================================

func TestATDD_51_3_AC7_NilDaemon_RecordAgentSkills(t *testing.T) {
	var d *ImmuneDaemon
	// Must not panic on nil receiver
	d.RecordAgentSkills("some-agent", []string{"skill-a", "skill-b"})
}

// =============================================================================
// AC2: RecordAgentSkills 守卫 — 空模板、空 skills、模板级去重
// =============================================================================

func TestATDD_51_3_AC2_RecordAgentSkills_EmptyTemplate(t *testing.T) {
	store := NewImmuneStore(t.TempDir())
	d := NewImmuneDaemon(store, DefaultImmuneConfig())
	if err := d.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer d.Stop()

	// Record a valid agent first
	d.RecordAgentSkills("real-agent", []string{"analysis"})
	// Empty template should be skipped — matrix should only contain "real-agent"
	d.RecordAgentSkills("", []string{"analysis", "coding"})

	// Verify: GetSimilarAgents for "" should return nil (empty template never entered)
	result := d.GetSimilarAgents("", 0.0)
	if len(result) != 0 {
		t.Errorf("expected no similar agents for empty template, got %d", len(result))
	}
}

func TestATDD_51_3_AC2_RecordAgentSkills_EmptySkills(t *testing.T) {
	store := NewImmuneStore(t.TempDir())
	d := NewImmuneDaemon(store, DefaultImmuneConfig())
	if err := d.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer d.Stop()

	// Record a valid agent first
	d.RecordAgentSkills("valid-agent", []string{"analysis"})
	// Empty skills should be skipped
	d.RecordAgentSkills("empty-skills-agent", nil)
	d.RecordAgentSkills("empty-skills-agent-2", []string{})

	// Verify: similarity between valid-agent and empty-skills-agent should be nil
	// (empty-skills-agent was never registered)
	sim := d.GetSimilarity("valid-agent", "empty-skills-agent")
	if sim != nil {
		t.Errorf("expected nil similarity for agent with empty skills, got %+v", sim)
	}
}

func TestATDD_51_3_AC2_RecordAgentSkills_TemplateDedupe(t *testing.T) {
	store := NewImmuneStore(t.TempDir())
	d := NewImmuneDaemon(store, DefaultImmuneConfig())
	if err := d.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer d.Stop()

	// Record same template multiple times (simulating multiple Spawns of same agent)
	d.RecordAgentSkills("code-analyst", []string{"analysis", "coding"})
	d.RecordAgentSkills("code-analyst", []string{"analysis", "coding"})
	d.RecordAgentSkills("code-analyst", []string{"analysis", "coding"})
	d.RecordAgentSkills("reviewer", []string{"analysis", "review"})

	// Verify: matrix should have exactly one entry pair (code-analyst, reviewer),
	// not three pairs from the three identical registrations
	sim := d.GetSimilarity("code-analyst", "reviewer")
	if sim == nil {
		t.Fatal("expected non-nil similarity between code-analyst and reviewer")
	}
	// Jaccard("analysis,coding", "analysis,review") = |{analysis}| / |{analysis,coding,review}| = 1/3
	expectedJaccard := 1.0 / 3.0
	if sim.SkillScore < expectedJaccard-0.01 || sim.SkillScore > expectedJaccard+0.01 {
		t.Errorf("SkillScore = %f, want ~%f (Jaccard of deduplicated skills)", sim.SkillScore, expectedJaccard)
	}
}

// =============================================================================
// AC1 + AC5: spawn feed → similarity 非空（经真实 Spawn 路径，非直接调 UpdateSimilarityMatrix）
// =============================================================================

func TestATDD_51_3_AC1_SpawnFeed_SimilarityNonEmpty(t *testing.T) {
	k, d, _ := newImmuneSpawnKernel(t)

	// Spawn two agents with overlapping skills → should feed similarity matrix
	pidA, err := k.Spawn("analyze task", testNamedAgentWithSkills("agent-alpha", "analysis", "coding"), SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn agent-alpha: %v", err)
	}
	waitForCollector(t, d, pidA)

	pidB, err := k.Spawn("review task", testNamedAgentWithSkills("agent-beta", "analysis", "coding", "review"), SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn agent-beta: %v", err)
	}
	waitForCollector(t, d, pidB)

	// After two Spawns with overlapping skills, the similarity matrix should be populated
	sim := d.GetSimilarity("agent-alpha", "agent-beta")
	if sim == nil {
		t.Fatal("expected non-nil similarity after spawning two agents with overlapping skills")
	}
	if sim.SkillScore <= 0 {
		t.Errorf("SkillScore = %f, want > 0 (skills overlap: analysis, coding)", sim.SkillScore)
	}
	// Jaccard("analysis,coding", "analysis,coding,review") = 2/3
	expectedJaccard := 2.0 / 3.0
	if sim.SkillScore < expectedJaccard-0.01 || sim.SkillScore > expectedJaccard+0.01 {
		t.Errorf("SkillScore = %f, want ~%f", sim.SkillScore, expectedJaccard)
	}
	if sim.Score <= 0 {
		t.Errorf("Score = %f, want > 0 (combined similarity)", sim.Score)
	}
}

// =============================================================================
// AC4: CoopScore 接通（51-2 coopHistory + 本 story feed → CoopScore > 0）
// =============================================================================

func TestATDD_51_3_AC4_CoopScore_PostFeed(t *testing.T) {
	k, d, _ := newImmuneSpawnKernel(t)

	// Spawn parent and child → triggers 51-2 cooperation topology feed
	parentPID, err := k.Spawn("parent task", testNamedAgentWithSkills("parent-agent", "analysis", "coding"), SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn parent: %v", err)
	}
	waitForCollector(t, d, parentPID)

	childPID, err := k.Spawn("child task", testNamedAgentWithSkills("child-agent", "analysis", "review"), SpawnOpts{ParentPID: parentPID})
	if err != nil {
		t.Fatalf("Spawn child: %v", err)
	}
	waitForCollector(t, d, childPID)

	// After parent→child spawn: 51-2 RecordCooperationTyped fires → coopHistory non-empty
	// Combined with this story's feed → Compute reads coopHistory → CoopScore > 0
	sim := d.GetSimilarity("parent-agent", "child-agent")
	if sim == nil {
		t.Fatal("expected non-nil similarity after parent→child spawn with skills")
	}
	if sim.CoopScore <= 0 {
		t.Errorf("CoopScore = %f, want > 0 (parent→child cooperation recorded by 51-2 feed)", sim.CoopScore)
	}
	// Score = 0.7*SkillScore + 0.3*CoopScore; both should contribute
	if sim.Score <= sim.SkillScore*0.7 {
		t.Errorf("Score = %f, want > SkillScore*0.7 (%f) — CoopScore should contribute", sim.Score, sim.SkillScore*0.7)
	}
}

// =============================================================================
// AC8: GetSimilarAgents 经 feed 后返回非空（CLI similarity 链间接覆盖）
// =============================================================================

func TestATDD_51_3_AC8_GetSimilarAgents_PostFeed(t *testing.T) {
	store := NewImmuneStore(t.TempDir())
	d := NewImmuneDaemon(store, DefaultImmuneConfig())
	if err := d.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer d.Stop()

	// Feed via RecordAgentSkills (unit test — bypasses spawn path)
	d.RecordAgentSkills("code-agent", []string{"analysis", "coding", "testing"})
	d.RecordAgentSkills("review-agent", []string{"analysis", "coding", "review"})
	d.RecordAgentSkills("cooking-agent", []string{"cooking"})

	// GetSimilarAgents for code-agent with threshold 0.3 should return review-agent
	similar := d.GetSimilarAgents("code-agent", 0.3)
	if len(similar) == 0 {
		t.Fatal("expected non-empty GetSimilarAgents after feed")
	}

	// Verify review-agent is in the results (skill overlap) but cooking-agent is not
	foundReview := false
	foundCooking := false
	for _, s := range similar {
		other := s.AgentB
		if other == "code-agent" {
			other = s.AgentA
		}
		if other == "review-agent" {
			foundReview = true
		}
		if other == "cooking-agent" {
			foundCooking = true
		}
	}
	if !foundReview {
		t.Error("expected review-agent in similar agents list")
	}
	if foundCooking {
		t.Error("cooking-agent (no skill overlap) should not be in similar agents above threshold 0.3")
	}
}

// =============================================================================
// AC5: 结果级净提升 — 对照组（空矩阵 → 任务丢失）
// =============================================================================

func TestATDD_51_3_AC5_ControlGroup_TaskLost(t *testing.T) {
	k := newSimpleTestKernel(t, &alwaysCrashFile{})

	// Setup immune daemon with NO similar agents (migration will fail)
	store := NewImmuneStore(t.TempDir())
	daemon := NewImmuneDaemon(store, DefaultImmuneConfig())
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer daemon.Stop()

	// Only the failing agent, no backup — migration will find no candidates
	daemon.RecordAgentSkills("crasher-agent", []string{"unique-skill"})

	daemon.SetMigrateFunc(func(intent, agentName string, contextMsgs []string) (types.PID, error) {
		t.Error("migrateFn should not be called when no similar candidates exist")
		return 0, nil
	})

	k.SetImmuneDaemon(daemon)

	agentInfo := testNamedAgentWithSkills("crasher-agent", "unique-skill")

	spec := SupervisorSpec{
		Strategy:    OneForOne,
		MaxRestarts: 2,
		MaxWindow:   10 * time.Second,
		Children: []ChildSpec{
			{Name: "crasher", Intent: "crash-task", Agent: agentInfo, Restart: RestartPermanent},
		},
	}

	supPID, err := k.SpawnSupervisor(spec)
	if err != nil {
		t.Fatalf("SpawnSupervisor: %v", err)
	}

	exit := waitSupervisor(k, supPID)

	// Control group: migration fails → supervisor shutdownAll → task lost
	if exit.Code != 1 {
		t.Errorf("exit code = %d, want 1 (task lost — no similar agent for migration)", exit.Code)
	}
	if exit.Reason != "max_restarts_exceeded" {
		t.Errorf("reason = %q, want %q", exit.Reason, "max_restarts_exceeded")
	}
}

// =============================================================================
// AC5: 结果级净提升 — 实验组（相似 agent + 真实 Spawn migrateFn → 迁移续跑）
// =============================================================================

func TestATDD_51_3_AC5_ExperimentGroup_MigrationSuccess(t *testing.T) {
	// Per-Open factory: first 2 opens → crash (initial spawn + 1 restart = 2 opens
	// before MaxRestarts=2 is reached), then normal "done" for the migrated backup-agent.
	var openCount atomic.Int32
	k, daemon := newImmuneMigrationKernel(t, func() vfs.VFSFile {
		if openCount.Add(1) <= 2 {
			return &alwaysCrashFile{}
		}
		return &normalFile{}
	})

	// Feed similarity matrix via RecordAgentSkills (production path, not direct
	// UpdateSimilarityMatrix). Pre-register backup-agent so AttemptMigration finds
	// it as a candidate. Crasher-agent spawn feed only adds/overwrites the same
	// entry — backup-agent survives because RecordAgentSkills accumulates.
	daemon.RecordAgentSkills("crasher-agent", []string{"analysis", "coding"})
	daemon.RecordAgentSkills("backup-agent", []string{"analysis", "coding", "review"})

	// Setup reputation store (default neutral score 0.5)
	repStore := NewReputationStore(t.TempDir())
	daemon.SetReputationStore(repStore)

	// Real migrateFn — uses k.Spawn to create a real process (not a mock PID)
	var migratedPID atomic.Uint64
	daemon.SetMigrateFunc(func(intent, agentName string, contextMsgs []string) (types.PID, error) {
		agent := testNamedAgentWithSkills(agentName, "analysis", "coding", "review")
		pid, err := k.Spawn(intent, agent, SpawnOpts{})
		if err == nil {
			migratedPID.Store(uint64(pid))
		}
		return pid, err
	})

	agentInfo := testNamedAgentWithSkills("crasher-agent", "analysis", "coding")

	spec := SupervisorSpec{
		Strategy:    OneForOne,
		MaxRestarts: 2,
		MaxWindow:   10 * time.Second,
		Children: []ChildSpec{
			{Name: "crasher", Intent: "crash-task", Agent: agentInfo, Restart: RestartPermanent},
		},
	}

	supPID, err := k.SpawnSupervisor(spec)
	if err != nil {
		t.Fatalf("SpawnSupervisor: %v", err)
	}

	exit := waitSupervisor(k, supPID)

	// Experiment group: migration succeeds → supervisor does NOT shutdownAll
	if exit.Code == 1 && exit.Reason == "max_restarts_exceeded" {
		t.Fatal("experiment group should NOT exit with max_restarts_exceeded — migration should have succeeded")
	}
	if exit.Code != 0 {
		t.Errorf("exit code = %d, want 0 (migration succeeded, all children done)", exit.Code)
	}

	// Verify migration actually happened via real Spawn
	newPID := types.PID(migratedPID.Load())
	if newPID == 0 {
		t.Fatal("migrateFn was never called — migration did not happen")
	}

	// Verify the migrated process completed (real Spawn, not fake PID)
	proc, ok := k.GetProcess(newPID)
	if !ok {
		// Process may have been reaped already — that's OK if it completed
		t.Logf("migrated process PID %d already reaped (completed)", newPID)
	} else {
		// Wait for the migrated process to complete (normalFile returns "done" quickly)
		select {
		case migExit := <-proc.Done:
			if migExit.Code != 0 {
				t.Errorf("migrated process exit code = %d, want 0 (backup agent should complete)", migExit.Code)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("migrated process did not complete within timeout")
		}
	}
}

// =============================================================================
// AC6: 迁移目标相似性断言 — 相似 vs 无关候选 → 选中相似者
// =============================================================================

func TestATDD_51_3_AC6_SimilarityTargetSelection(t *testing.T) {
	// Per-Open factory: first 2 crash (initial + 1 restart before MaxRestarts=2), then normal
	var openCount atomic.Int32
	k, daemon := newImmuneMigrationKernel(t, func() vfs.VFSFile {
		if openCount.Add(1) <= 2 {
			return &alwaysCrashFile{}
		}
		return &normalFile{}
	})

	// Feed via RecordAgentSkills (production path): crasher-agent has two candidates
	daemon.RecordAgentSkills("crasher-agent", []string{"analysis", "coding", "debugging"})
	daemon.RecordAgentSkills("similar-agent", []string{"analysis", "coding", "review"})     // skill overlap ≥ 0.3 (Jaccard = 2/4 = 0.5)
	daemon.RecordAgentSkills("unrelated-agent", []string{"cooking", "baking"})              // skill overlap 0 (Jaccard = 0/5 = 0)

	repStore := NewReputationStore(t.TempDir())
	daemon.SetReputationStore(repStore)

	var targetAgent atomic.Value
	daemon.SetMigrateFunc(func(intent, agentName string, contextMsgs []string) (types.PID, error) {
		targetAgent.Store(agentName)
		agent := testNamedAgentWithSkills(agentName, "analysis", "coding", "review")
		pid, err := k.Spawn(intent, agent, SpawnOpts{})
		return pid, err
	})

	agentInfo := testNamedAgentWithSkills("crasher-agent", "analysis", "coding", "debugging")

	spec := SupervisorSpec{
		Strategy:    OneForOne,
		MaxRestarts: 2,
		MaxWindow:   10 * time.Second,
		Children: []ChildSpec{
			{Name: "crasher", Intent: "crash-task", Agent: agentInfo, Restart: RestartPermanent},
		},
	}

	supPID, err := k.SpawnSupervisor(spec)
	if err != nil {
		t.Fatalf("SpawnSupervisor: %v", err)
	}

	_ = waitSupervisor(k, supPID)

	// Verify: migration target must be similar-agent, NOT unrelated-agent
	selected, ok := targetAgent.Load().(string)
	if !ok || selected == "" {
		t.Fatal("migrateFn was never called — no target agent selected")
	}
	if selected != "similar-agent" {
		t.Errorf("TargetAgent = %q, want %q (highest similarity score)", selected, "similar-agent")
	}
	if selected == "unrelated-agent" {
		t.Error("unrelated-agent (no skill overlap, below MinMigrationSimilarity=0.3) must NOT be selected")
	}
}

// =============================================================================
// AC7: nil daemon — Spawn 行为不变
// =============================================================================

func TestATDD_51_3_AC7_NilDaemon_SpawnUnchanged(t *testing.T) {
	park := newParkingLLMFile()

	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return park, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)
	t.Cleanup(park.releaseAll)

	// k.immuneDaemon is nil (default config — immune not enabled)
	// Spawn with agent+skills must work normally without panic
	pid, err := k.Spawn("test intent", testNamedAgentWithSkills("test-agent", "analysis", "coding"), SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn with nil immuneDaemon should succeed, got: %v", err)
	}
	if pid == 0 {
		t.Error("expected non-zero PID from Spawn")
	}

	// Similarity matrix should be empty (no daemon to feed)
	// This is expected behavior — matrix remains empty until immune is enabled (51-5)
}

// =============================================================================
// AC9: 并发安全 — 并发 spawn feed 无数据竞争
// =============================================================================

func TestATDD_51_3_AC9_ConcurrentSpawnFeed(t *testing.T) {
	k, d, _ := newImmuneSpawnKernel(t)

	const goroutines = 8
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := range goroutines {
		go func(idx int) {
			defer wg.Done()
			agentName := "agent-" + string(rune('A'+idx))
			agent := testNamedAgentWithSkills(agentName, "analysis", "coding")
			pid, err := k.Spawn("task-"+agentName, agent, SpawnOpts{})
			if err != nil {
				t.Errorf("concurrent Spawn %s failed: %v", agentName, err)
				return
			}
			waitForCollector(t, d, pid)
		}(i)
	}

	wg.Wait()

	// After concurrent spawns, the matrix should contain entries.
	// The exact count depends on how many unique templates were registered.
	// With 8 unique agents, we expect C(8,2) = 28 pairs.
	// Verify at least some entries exist (go test -race is the primary assertion).
	similar := d.GetSimilarAgents("agent-A", 0.0)
	if len(similar) == 0 {
		t.Error("expected non-empty similarity list after concurrent spawns")
	}
}
