package kernel

import (
	gocontext "context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rnixai/rnix/agents"
	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/skills"
	"github.com/rnixai/rnix/vfs"
)

// ATDD 51.4: 声誉/Synergy 接入 StemMatcher 主调度 — 最高杠杆涌现闭环（AR-1 + EM-1）
//
// 来源 Story：_bmad-output/implementation-artifacts/51-4-reputation-synergy-into-stem-match.md
// 审计来源：completion-audit-suspects-2026-05-30.md §A2 (AR-1 + EM-1)
// Story Key: 51-4-reputation-synergy-into-stem-match
//
// 验收标准（Acceptance Criteria，本文件覆盖项）：
//   AC1（重排逻辑）     : StemMatcher.Match 返回候选后，用 synergy/声誉重排 skills 优先级
//   AC2（ε 探索）       : ε=0 纯贪心重排；ε=1 纯探索保留原序
//   AC3（finishProcess RecordResult）: 进程退出时补声誉记录
//   AC4（finishProcess RecordCombo）: 进程退出时补 synergy 记录
//   AC5（kernel setter）: 新字段 + setter 注入可用
//   AC7（重排生效断言） : skill-A 历史好 + skill-B keyword 高 → 重排后 A 升首
//   AC8（声誉累积）     : 连续失败 vs 成功 → Score 分化
//   AC9（并发安全）     : 多 goroutine 并发 spawn，-race 无报警
//   AC10（确定性策略）  : ε=0 / ε=1 消除随机性
//
// ============================== RED PHASE ==============================
// 本测试在 Story 51.4 实现前应 FAIL（红灯）。
// 预期失败原因：kernel 包无法编译——导出方法 SetReputationStoreForStem、
// SetSynergyMatrixForStem、SetStemEpsilon、SetStemReRankWeights 尚未实现
// （undefined），StemReRankWeights 类型未定义，Process.AgentTemplate 字段
// 不存在。实现后所有用例应转绿（含 go test -race）。
// 这是 Go ATDD 的标准红灯形态（缺失 API → 包编译失败），与
// kernel 既有 atdd_51_* 集成测试的红灯模式一致。
//
// 注意：测试严格使用 Story Dev Notes 规定的契约：
//   - 新类型：StemReRankWeights{MatchWeight, SynergyWeight, ReputationWeight float64}
//   - 新方法：SetReputationStoreForStem / SetSynergyMatrixForStem / SetStemEpsilon / SetStemReRankWeights
//   - 新文件：kernel/stem_rerank.go 含 reRankSkills 函数
//   - Process 新字段：AgentTemplate string（spawn 时赋值 agent.Manifest.Name）
//   - finishProcess 新逻辑：reputationStore.RecordResult + synergyMatrix.RecordCombo
//   - 断言只依赖既有公共 API (Spawn/ReputationStore/SynergyMatrix/GetSummary/
//     GetHistory/GetAllRecords/GetComboSummaries) + 新 setter
// ======================================================================

// --- completeLLMFile: 立即返回 complete 的 mock LLM ---

type completeLLMFile514 struct {
	mu       sync.Mutex
	response []byte
}

func newCompleteLLMFile514() *completeLLMFile514 {
	return &completeLLMFile514{
		response: makeLLMResponse("done", 1),
	}
}

func (f *completeLLMFile514) Read(_ int) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.response, nil
}

func (f *completeLLMFile514) Write(_ gocontext.Context, _ []byte) error { return nil }
func (f *completeLLMFile514) Close() error                              { return nil }

func (f *completeLLMFile514) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{IsDevice: true, Name: "/dev/llm/claude"}, nil
}

func (f *completeLLMFile514) SupportsToolCalling() bool { return true }

// --- Helpers ---

// newStemReRankKernel creates a kernel wired for stem rerank testing with mock LLM,
// StemMatcher (custom discovery func), ReputationStore, and SynergyMatrix.
func newStemReRankKernel(t *testing.T, discoverFn func() ([]skills.SkillInfo, error)) (*KernelImpl, string) {
	t.Helper()

	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return newCompleteLLMFile514(), nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)

	k.stemMatcher = NewStemMatcherFromFunc(discoverFn)
	k.skillLoader = func(name string) (*skills.SkillInfo, error) {
		return &skills.SkillInfo{
			Manifest: skills.SkillManifest{Name: name},
			Body:     "# " + name + "\nTest skill body.",
		}, nil
	}

	repDir := t.TempDir()
	reputationStore := NewReputationStore(repDir)
	synergyMatrix := NewSynergyMatrix(repDir)

	k.SetReputationStoreForStem(reputationStore)
	k.SetSynergyMatrixForStem(synergyMatrix)

	return k, repDir
}

// testStemAgentForReRank builds a minimal "stem" agent (triggers differentiation).
func testStemAgentForReRank() *agents.AgentInfo {
	return &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Name: "stem",
			Models: agents.AgentModels{
				Provider:  "claude",
				Preferred: "sonnet",
				Fallback:  "haiku",
			},
			ContextBudget: 4096,
		},
		Instructions: "# stem\nMinimal test agent for rerank testing.",
	}
}

// testNamedAgentForReRank builds a named non-stem agent with optional skills.
func testNamedAgentForReRank(name string, skillNames ...string) *agents.AgentInfo {
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
		Instructions: "# " + name + "\nA minimal test agent for rerank testing.",
	}
	for _, sn := range skillNames {
		agent.Skills = append(agent.Skills, &skills.SkillInfo{
			Manifest: skills.SkillManifest{Name: sn},
		})
	}
	return agent
}

// seedSynergyRecords writes synergy records to set up combo history.
func seedSynergyRecords(t *testing.T, matrix *SynergyMatrix, skillName string, passed int, failed int) {
	t.Helper()
	for range passed {
		err := matrix.RecordCombo(SynergyRecord{
			ComboKey:   NewComboKey([]string{skillName}),
			Skills:     []string{skillName},
			Passed:     true,
			TokensUsed: 1000,
			DurationMs: 500,
			Timestamp:  time.Now(),
		})
		if err != nil {
			t.Fatalf("seed synergy record (passed) failed: %v", err)
		}
	}
	for range failed {
		err := matrix.RecordCombo(SynergyRecord{
			ComboKey:   NewComboKey([]string{skillName}),
			Skills:     []string{skillName},
			Passed:     false,
			TokensUsed: 2000,
			DurationMs: 1000,
			Timestamp:  time.Now(),
		})
		if err != nil {
			t.Fatalf("seed synergy record (failed) failed: %v", err)
		}
	}
}

// seedReputationRecords writes reputation records for an agent.
func seedReputationRecords(t *testing.T, store *ReputationStore, agentName string, passed int, failed int) {
	t.Helper()
	for range passed {
		err := store.RecordResult(agentName, &SLAResult{
			AgentName:   agentName,
			Passed:      true,
			TokensUsed:  1000,
			DurationMs:  500,
			EvaluatedAt: time.Now(),
		})
		if err != nil {
			t.Fatalf("seed reputation record (passed) failed: %v", err)
		}
	}
	for range failed {
		err := store.RecordResult(agentName, &SLAResult{
			AgentName:   agentName,
			Passed:      false,
			TokensUsed:  2000,
			DurationMs:  1000,
			EvaluatedAt: time.Now(),
		})
		if err != nil {
			t.Fatalf("seed reputation record (failed) failed: %v", err)
		}
	}
}

// waitProc waits for a process to reach Done within timeout.
func waitProc514(t *testing.T, proc *Process, timeout time.Duration) {
	t.Helper()
	select {
	case <-proc.Done:
	case <-time.After(timeout):
		t.Fatal("process did not complete within timeout")
	}
}

// =============================================================================
// INT_005: Kernel setter 注入（AC5）
// =============================================================================

func TestATDD_51_4_INT_005_KernelSetterInjection(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)

	repDir := t.TempDir()
	reputationStore := NewReputationStore(repDir)
	synergyMatrix := NewSynergyMatrix(repDir)

	k.SetReputationStoreForStem(reputationStore)
	k.SetSynergyMatrixForStem(synergyMatrix)
	k.SetStemEpsilon(0.1)

	weights := DefaultStemReRankWeights()
	k.SetStemReRankWeights(&weights)

	if weights.MatchWeight != 0.5 {
		t.Errorf("DefaultStemReRankWeights().MatchWeight = %v, want 0.5", weights.MatchWeight)
	}
	if weights.SynergyWeight != 0.3 {
		t.Errorf("DefaultStemReRankWeights().SynergyWeight = %v, want 0.3", weights.SynergyWeight)
	}
	if weights.ReputationWeight != 0.2 {
		t.Errorf("DefaultStemReRankWeights().ReputationWeight = %v, want 0.2", weights.ReputationWeight)
	}
}

// =============================================================================
// INT_006: 重排生效（AC7/AC1）
// =============================================================================

func TestATDD_51_4_INT_006_ReRankSkillOrder(t *testing.T) {
	// skill-A has 100% synergy success rate, skill-B has 0%.
	// Match returns [skill-B, skill-A] (B has higher keyword overlap).
	// After rerank (ε=0), skill-A should be first.

	discoverFn := func() ([]skills.SkillInfo, error) {
		return []skills.SkillInfo{
			{Manifest: skills.SkillManifest{
				Name:        "skill-B",
				Description: "deeply code analysis",
			}},
			{Manifest: skills.SkillManifest{
				Name:        "skill-A",
				Description: "code analysis",
			}},
		}, nil
	}

	k, repDir := newStemReRankKernel(t, discoverFn)

	// Seed synergy: skill-A has 100% success, skill-B has 0%
	synergyMatrix := NewSynergyMatrix(repDir)
	seedSynergyRecords(t, synergyMatrix, "skill-A", 10, 0)
	seedSynergyRecords(t, synergyMatrix, "skill-B", 0, 10)

	k.SetStemEpsilon(0) // pure greedy

	agent := testStemAgentForReRank()
	pid, err := k.Spawn("analyze code deeply", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := k.procTable.Load(pid)
	if !ok {
		t.Fatal("process not found in procTable")
	}
	waitProc514(t, proc, 5*time.Second)

	if len(proc.Skills) < 2 {
		t.Fatalf("expected at least 2 skills, got %d: %v", len(proc.Skills), proc.Skills)
	}
	if proc.Skills[0] != "skill-A" {
		t.Errorf("after rerank, first skill = %q, want %q (skill-A should be promoted by synergy history)", proc.Skills[0], "skill-A")
	}
}

// =============================================================================
// INT_002: ε 探索（AC2/AC10）
// =============================================================================

func TestATDD_51_4_INT_002_EpsilonPureGreedy(t *testing.T) {
	// ε=0: always rerank (pure greedy, deterministic)
	discoverFn := func() ([]skills.SkillInfo, error) {
		return []skills.SkillInfo{
			{Manifest: skills.SkillManifest{
				Name:        "skill-B",
				Description: "deeply code analysis",
			}},
			{Manifest: skills.SkillManifest{
				Name:        "skill-A",
				Description: "code analysis",
			}},
		}, nil
	}

	k, repDir := newStemReRankKernel(t, discoverFn)

	synergyMatrix := NewSynergyMatrix(repDir)
	seedSynergyRecords(t, synergyMatrix, "skill-A", 10, 0)
	seedSynergyRecords(t, synergyMatrix, "skill-B", 0, 10)

	k.SetStemEpsilon(0)

	agent := testStemAgentForReRank()
	pid, err := k.Spawn("analyze code deeply", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := k.procTable.Load(pid)
	if !ok {
		t.Fatal("process not found")
	}
	waitProc514(t, proc, 5*time.Second)

	if len(proc.Skills) < 2 {
		t.Fatalf("expected >=2 skills, got %v", proc.Skills)
	}
	if proc.Skills[0] != "skill-A" {
		t.Errorf("ε=0: first skill = %q, want skill-A (pure greedy rerank)", proc.Skills[0])
	}
}

func TestATDD_51_4_INT_002_EpsilonPureExplore(t *testing.T) {
	// ε=1: never rerank (pure explore, preserve original Match order)
	discoverFn := func() ([]skills.SkillInfo, error) {
		return []skills.SkillInfo{
			{Manifest: skills.SkillManifest{
				Name:        "skill-B",
				Description: "deeply code analysis",
			}},
			{Manifest: skills.SkillManifest{
				Name:        "skill-A",
				Description: "code analysis",
			}},
		}, nil
	}

	k, repDir := newStemReRankKernel(t, discoverFn)

	synergyMatrix := NewSynergyMatrix(repDir)
	seedSynergyRecords(t, synergyMatrix, "skill-A", 10, 0)
	seedSynergyRecords(t, synergyMatrix, "skill-B", 0, 10)

	k.SetStemEpsilon(1.0) // pure explore — skip rerank

	agent := testStemAgentForReRank()
	pid, err := k.Spawn("analyze code deeply", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := k.procTable.Load(pid)
	if !ok {
		t.Fatal("process not found")
	}
	waitProc514(t, proc, 5*time.Second)

	if len(proc.Skills) < 2 {
		t.Fatalf("expected >=2 skills, got %v", proc.Skills)
	}
	// ε=1: rerank skipped → original keyword order (skill-B first)
	if proc.Skills[0] != "skill-B" {
		t.Errorf("ε=1: first skill = %q, want skill-B (original keyword order)", proc.Skills[0])
	}
}

// =============================================================================
// INT_003: finishProcess 补 RecordResult（AC3）
// =============================================================================

func TestATDD_51_4_INT_003_FinishProcessRecordResult(t *testing.T) {
	// Named agent process completes → reputationStore should have a new record.
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return newCompleteLLMFile514(), nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)

	repDir := t.TempDir()
	reputationStore := NewReputationStore(repDir)
	k.SetReputationStoreForStem(reputationStore)

	agent := testNamedAgentForReRank("code-analyst", "skill-X")
	pid, err := k.Spawn("analyze this code", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := k.procTable.Load(pid)
	if !ok {
		t.Fatal("process not found")
	}
	waitProc514(t, proc, 5*time.Second)

	history, err := reputationStore.GetHistory("code-analyst")
	if err != nil {
		t.Fatalf("GetHistory failed: %v", err)
	}
	if len(history) == 0 {
		t.Fatal("finishProcess should have called RecordResult — no reputation records for 'code-analyst'")
	}
	if history[0].SLAResult == nil {
		t.Fatal("reputation record has nil SLAResult")
	}
	if !history[0].SLAResult.Passed {
		t.Error("process completed successfully but SLAResult.Passed = false")
	}
}

func TestATDD_51_4_INT_003_FinishProcessRecordResult_NoTemplate(t *testing.T) {
	// Agent with empty name → finishProcess should NOT write reputation record
	// (guard: agentTemplate != "")
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return newCompleteLLMFile514(), nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)

	repDir := t.TempDir()
	reputationStore := NewReputationStore(repDir)
	k.SetReputationStoreForStem(reputationStore)

	agent := testNamedAgentForReRank("")
	pid, err := k.Spawn("do work", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := k.procTable.Load(pid)
	if !ok {
		t.Fatal("process not found")
	}
	waitProc514(t, proc, 5*time.Second)

	// Should have no records for empty template
	agentNames, err := reputationStore.ListAgents()
	if err != nil {
		t.Fatalf("ListAgents failed: %v", err)
	}
	if len(agentNames) != 0 {
		t.Errorf("expected no reputation records for empty template, got agents: %v", agentNames)
	}
}

// =============================================================================
// INT_004: finishProcess 补 RecordCombo（AC4）
// =============================================================================

func TestATDD_51_4_INT_004_FinishProcessRecordCombo(t *testing.T) {
	// Stem agent with skills → after process completes, synergyMatrix has new record.
	discoverFn := func() ([]skills.SkillInfo, error) {
		return []skills.SkillInfo{
			{Manifest: skills.SkillManifest{
				Name:        "skill-X",
				Description: "code analysis tool",
			}},
			{Manifest: skills.SkillManifest{
				Name:        "skill-Y",
				Description: "test runner tool",
			}},
		}, nil
	}

	k, repDir := newStemReRankKernel(t, discoverFn)
	k.SetStemEpsilon(1.0) // skip rerank — we only care about RecordCombo

	agent := testStemAgentForReRank()
	pid, err := k.Spawn("analyze code and run tests", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := k.procTable.Load(pid)
	if !ok {
		t.Fatal("process not found")
	}
	waitProc514(t, proc, 5*time.Second)

	synergyMatrix := NewSynergyMatrix(repDir)
	records, err := synergyMatrix.GetAllRecords()
	if err != nil {
		t.Fatalf("GetAllRecords failed: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("finishProcess should have called RecordCombo — no synergy records found")
	}
	if len(records[0].Skills) == 0 {
		t.Error("synergy record has empty skills list")
	}
}

func TestATDD_51_4_INT_004_FinishProcessRecordCombo_NoSkills(t *testing.T) {
	// Agent with no skills → finishProcess should NOT write synergy record
	// (guard: len(proc.Skills) > 0)
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return newCompleteLLMFile514(), nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)

	repDir := t.TempDir()
	synergyMatrix := NewSynergyMatrix(repDir)
	k.SetSynergyMatrixForStem(synergyMatrix)

	agent := testNamedAgentForReRank("no-skills-agent") // no skills
	pid, err := k.Spawn("do something", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := k.procTable.Load(pid)
	if !ok {
		t.Fatal("process not found")
	}
	waitProc514(t, proc, 5*time.Second)

	records, err := synergyMatrix.GetAllRecords()
	if err != nil {
		t.Fatalf("GetAllRecords failed: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("expected no synergy records for agent without skills, got %d", len(records))
	}
}

// =============================================================================
// INT_007: 声誉累积改变选择（AC8）— 直接预铺 50-2
// =============================================================================

func TestATDD_51_4_INT_007_ReputationAccumulation(t *testing.T) {
	// agent-X fails 3 times, agent-Y succeeds 3 times.
	// GetSummary(X).Score < GetSummary(Y).Score.
	repDir := t.TempDir()
	store := NewReputationStore(repDir)

	seedReputationRecords(t, store, "agent-X", 0, 3)
	seedReputationRecords(t, store, "agent-Y", 3, 0)

	summaryX, err := store.GetSummary("agent-X")
	if err != nil {
		t.Fatalf("GetSummary(agent-X) failed: %v", err)
	}
	summaryY, err := store.GetSummary("agent-Y")
	if err != nil {
		t.Fatalf("GetSummary(agent-Y) failed: %v", err)
	}

	if summaryX.Score >= summaryY.Score {
		t.Errorf("reputation accumulation failed: agent-X (3 failures) Score=%v >= agent-Y (3 successes) Score=%v; "+
			"expected X < Y to validate dynamic reputation affects selection distribution", summaryX.Score, summaryY.Score)
	}
}

// =============================================================================
// INT_008: 并发安全（AC9）
// =============================================================================

func TestATDD_51_4_INT_008_ConcurrentSpawnReRank(t *testing.T) {
	var callCount atomic.Int32
	discoverFn := func() ([]skills.SkillInfo, error) {
		return []skills.SkillInfo{
			{Manifest: skills.SkillManifest{
				Name:        "skill-alpha",
				Description: "code analysis",
			}},
			{Manifest: skills.SkillManifest{
				Name:        "skill-beta",
				Description: "test runner",
			}},
		}, nil
	}

	k, repDir := newStemReRankKernel(t, discoverFn)

	synergyMatrix := NewSynergyMatrix(repDir)
	seedSynergyRecords(t, synergyMatrix, "skill-alpha", 5, 1)
	seedSynergyRecords(t, synergyMatrix, "skill-beta", 1, 5)

	k.SetStemEpsilon(0)

	const concurrency = 5
	var wg sync.WaitGroup
	wg.Add(concurrency)

	for i := range concurrency {
		go func(idx int) {
			defer wg.Done()
			agent := testStemAgentForReRank()
			pid, err := k.Spawn("analyze code", agent, SpawnOpts{})
			if err != nil {
				t.Errorf("goroutine %d: Spawn failed: %v", idx, err)
				return
			}
			callCount.Add(1)

			proc, ok := k.procTable.Load(pid)
			if !ok {
				t.Errorf("goroutine %d: process not found", idx)
				return
			}
			select {
			case <-proc.Done:
			case <-time.After(10 * time.Second):
				t.Errorf("goroutine %d: timeout", idx)
			}
		}(i)
	}
	wg.Wait()

	if callCount.Load() != concurrency {
		t.Errorf("expected %d successful spawns, got %d", concurrency, callCount.Load())
	}
}

// =============================================================================
// INT_009: 无数据退化中性（AC1 退化分支）
// =============================================================================

func TestATDD_51_4_INT_009_ReRankNoDataNeutral(t *testing.T) {
	// No synergy/reputation data → rerank should not disturb original order
	// (all scores collapse to neutral 0.5 × w, matchScore dominates).
	discoverFn := func() ([]skills.SkillInfo, error) {
		return []skills.SkillInfo{
			{Manifest: skills.SkillManifest{
				Name:        "skill-B",
				Description: "deeply code analysis",
			}},
			{Manifest: skills.SkillManifest{
				Name:        "skill-A",
				Description: "code analysis",
			}},
		}, nil
	}

	k, _ := newStemReRankKernel(t, discoverFn)
	k.SetStemEpsilon(0)

	agent := testStemAgentForReRank()
	pid, err := k.Spawn("analyze code deeply", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := k.procTable.Load(pid)
	if !ok {
		t.Fatal("process not found")
	}
	waitProc514(t, proc, 5*time.Second)

	if len(proc.Skills) < 2 {
		t.Fatalf("expected >=2 skills, got %v", proc.Skills)
	}
	// No data → synergy/rep boost = 0.5 for all → matchScore dominates → original order
	if proc.Skills[0] != "skill-B" {
		t.Errorf("no-data rerank: first skill = %q, want skill-B (keyword overlap dominates)", proc.Skills[0])
	}
}

// =============================================================================
// INT_010: Process.AgentTemplate 赋值（AC3 前置）
// =============================================================================

func TestATDD_51_4_INT_010_ProcessAgentTemplate(t *testing.T) {
	// Spawn a named agent → proc.AgentTemplate should equal agent.Manifest.Name.
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return newCompleteLLMFile514(), nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)

	agent := testNamedAgentForReRank("my-custom-agent")
	pid, err := k.Spawn("do work", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := k.procTable.Load(pid)
	if !ok {
		t.Fatal("process not found")
	}

	if proc.AgentTemplate != "my-custom-agent" {
		t.Errorf("proc.AgentTemplate = %q, want %q", proc.AgentTemplate, "my-custom-agent")
	}

	waitProc514(t, proc, 5*time.Second)
}

// =============================================================================
// INT_011: Nil guard — reputation/synergy 为 nil 时不 panic（AC1 守卫）
// =============================================================================

func TestATDD_51_4_INT_011_NilGuardNoPanic(t *testing.T) {
	// Kernel has stemMatcher but no reputationStore/synergyMatrix.
	// Spawn should work without rerank and without panic.
	discoverFn := func() ([]skills.SkillInfo, error) {
		return []skills.SkillInfo{
			{Manifest: skills.SkillManifest{
				Name:        "skill-X",
				Description: "code analysis",
			}},
		}, nil
	}

	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return newCompleteLLMFile514(), nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)

	k.stemMatcher = NewStemMatcherFromFunc(discoverFn)
	k.skillLoader = func(name string) (*skills.SkillInfo, error) {
		return &skills.SkillInfo{
			Manifest: skills.SkillManifest{Name: name},
			Body:     "# " + name,
		}, nil
	}
	// Deliberately NOT setting reputationStore/synergyMatrix

	agent := testStemAgentForReRank()
	pid, err := k.Spawn("analyze code", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn should succeed without reputation/synergy: %v", err)
	}

	proc, ok := k.procTable.Load(pid)
	if !ok {
		t.Fatal("process not found")
	}
	waitProc514(t, proc, 5*time.Second)

	if len(proc.Skills) == 0 {
		t.Error("expected at least one skill matched even without rerank")
	}
}
