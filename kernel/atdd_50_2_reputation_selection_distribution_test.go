package kernel

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gocontext "context"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/skills"
	"github.com/rnixai/rnix/vfs"
)

// ======================================================================
// ATDD 50.2 — 声誉分布翻转结果级验证 · 累积改变选择分布（TR-2）
//
// 来源 Story：_bmad-output/implementation-artifacts/50-2-reputation-selection-distribution.md
// 审计来源：completion-audit-suspects-2026-05-30.md §A2 (TR-2: 21-EFFECT 声誉分布翻转)
// Story Key: 50-2-reputation-selection-distribution
//
// 验收标准（本文件覆盖项）：
//   AC1: synergy 驱动选择翻转 · 全反馈循环（核心 P0）
//   AC2: reputation 驱动 Score 分化 · 全反馈循环
//   AC3: 全循环可观测性 · 磁盘数据验证
//   AC4: 渐进收敛 · 累积越多分化越大
//   AC5: 对照组 · ε=1 纯探索跳过重排
//
// 与 50-1 AC3 / 51-4 INT_007 的关键区别：
//   50-1 AC3 和 51-4 INT_007 使用 seedSynergyRecords / seedReputationRecords 预填数据。
//   50-2 要求 **全反馈循环**：进程实际执行 → finishProcess 自然写入 → 数据累积
//   → 后续 spawn 的 reRankSkills 读到累积数据 → 选择分布翻转。
//   所有 synergy/reputation 数据必须来自 finishProcess 自然写入，禁止预填。
// ======================================================================

// --- failingLLMFile502: 每次 Read 返回 unknown tool_call, 触发 circuit breaker → exit 1 ---

type failingLLMFile502 struct {
	mu     sync.Mutex
	closed bool
}

func newFailingLLMFile502() *failingLLMFile502 {
	return &failingLLMFile502{}
}

func (f *failingLLMFile502) Read(_ int) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return makeToolCallResponse("nonexistent_tool_502", map[string]any{"input": "trigger_failure"}, 10), nil
}

func (f *failingLLMFile502) Write(_ gocontext.Context, _ []byte) error { return nil }
func (f *failingLLMFile502) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *failingLLMFile502) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{IsDevice: true, Name: "/dev/llm/claude"}, nil
}

func (f *failingLLMFile502) SupportsToolCalling() bool { return true }

// --- completeLLMFile502: 立即返回 complete → exit 0 ---

type completeLLMFile502 struct {
	mu       sync.Mutex
	response []byte
}

func newCompleteLLMFile502() *completeLLMFile502 {
	return &completeLLMFile502{
		response: makeCompleteResponse("done", 1),
	}
}

func (f *completeLLMFile502) Read(_ int) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.response, nil
}

func (f *completeLLMFile502) Write(_ gocontext.Context, _ []byte) error { return nil }
func (f *completeLLMFile502) Close() error                              { return nil }

func (f *completeLLMFile502) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{IsDevice: true, Name: "/dev/llm/claude"}, nil
}

func (f *completeLLMFile502) SupportsToolCalling() bool { return true }

// --- feedbackLoopKernel: 可切换 LLM 模式的 kernel 构造 ---

type feedbackLoopKernel struct {
	kernel  *KernelImpl
	repDir  string
	llmMode *atomic.Int32 // 0=success (complete), 1=failure (circuit breaker)
}

func newFeedbackLoopKernel(t *testing.T, discoverFn func() ([]skills.SkillInfo, error)) *feedbackLoopKernel {
	t.Helper()

	var llmMode atomic.Int32

	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		if llmMode.Load() == 0 {
			return newCompleteLLMFile502(), nil
		}
		return newFailingLLMFile502(), nil
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

	return &feedbackLoopKernel{
		kernel:  k,
		repDir:  repDir,
		llmMode: &llmMode,
	}
}

// spawnNamedAndWait502 spawns a named agent and waits for it to complete.
// Returns the process and its exit status.
func spawnNamedAndWait502(t *testing.T, k *KernelImpl, intent string, agentName string, skillNames []string, timeout time.Duration) (*Process, ExitStatus) {
	t.Helper()
	agent := testNamedAgentForReRank(agentName, skillNames...)
	pid, err := k.Spawn(intent, agent, SpawnOpts{TimeoutMs: timeout.Milliseconds()})
	if err != nil {
		t.Fatalf("Spawn(%s) failed: %v", agentName, err)
	}
	proc, ok := k.procTable.Load(pid)
	if !ok {
		t.Fatalf("process %d not found in procTable", pid)
	}
	select {
	case exit := <-proc.Done:
		return proc, exit
	case <-time.After(timeout + 2*time.Second):
		t.Fatalf("process %s did not complete within timeout", agentName)
		return proc, ExitStatus{}
	}
}

// spawnStemAndWait502 spawns a stem agent and waits for it to complete.
func spawnStemAndWait502(t *testing.T, k *KernelImpl, intent string, timeout time.Duration) *Process {
	t.Helper()
	agent := testStemAgentForReRank()
	pid, err := k.Spawn(intent, agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn(stem) failed: %v", err)
	}
	proc, ok := k.procTable.Load(pid)
	if !ok {
		t.Fatalf("process %d not found in procTable", pid)
	}
	waitProc514(t, proc, timeout)
	return proc
}

func defaultDiscoverFn502() func() ([]skills.SkillInfo, error) {
	return func() ([]skills.SkillInfo, error) {
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
}

func countJSONLLines(t *testing.T, filePath string) int {
	t.Helper()
	f, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("open %s: %v", filePath, err)
	}
	defer f.Close()

	count := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if len(scanner.Bytes()) > 0 {
			count++
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan %s: %v", filePath, err)
	}
	return count
}

// =============================================================================
// AC1: synergy 驱动选择翻转 · 全反馈循环（核心 P0）
// =============================================================================

func TestATDD_50_2_SynergyDrivenSelectionFlip(t *testing.T) {
	flk := newFeedbackLoopKernel(t, defaultDiscoverFn502())
	k := flk.kernel

	// --- 累积阶段：skill-A 成功 ×3 ---
	flk.llmMode.Store(0)
	for i := range 3 {
		_, exit := spawnNamedAndWait502(t, k, "accumulate success", fmt.Sprintf("acc-A-%d", i), []string{"skill-A"}, 5*time.Second)
		if exit.Code != 0 {
			t.Fatalf("acc-A-%d: exit code = %d, want 0", i, exit.Code)
		}
	}

	// --- 累积阶段：skill-B 失败 ×3 ---
	flk.llmMode.Store(1)
	for i := range 3 {
		_, exit := spawnNamedAndWait502(t, k, "accumulate failure", fmt.Sprintf("acc-B-%d", i), []string{"skill-B"}, 10*time.Second)
		if exit.Code == 0 {
			t.Fatalf("acc-B-%d: exit code = 0, want non-zero", i)
		}
	}

	// --- 验证阶段：ε=0 stem agent → 翻转 ---
	flk.llmMode.Store(0)
	k.SetStemEpsilon(0)

	proc := spawnStemAndWait502(t, k, "analyze code deeply", 5*time.Second)

	if len(proc.Skills) < 2 {
		t.Fatalf("expected ≥2 skills, got %d: %v", len(proc.Skills), proc.Skills)
	}
	if proc.Skills[0] != "skill-A" {
		t.Errorf("synergy-driven selection flip: first skill = %q, want %q "+
			"(skill-A should be promoted by accumulated synergy success rate)", proc.Skills[0], "skill-A")
	}
}

// =============================================================================
// AC2: reputation 驱动 Score 分化 · 全反馈循环
// =============================================================================

func TestATDD_50_2_ReputationDivergenceFromFinishProcess(t *testing.T) {
	flk := newFeedbackLoopKernel(t, defaultDiscoverFn502())
	k := flk.kernel
	repStore := NewReputationStore(flk.repDir)

	// --- agent-alpha 成功 ×3 ---
	flk.llmMode.Store(0)
	for i := range 3 {
		_, exit := spawnNamedAndWait502(t, k, "alpha task", "agent-alpha", []string{"skill-A"}, 5*time.Second)
		if exit.Code != 0 {
			t.Fatalf("agent-alpha run %d: exit code = %d, want 0", i, exit.Code)
		}
	}

	// --- agent-beta 失败 ×3 ---
	flk.llmMode.Store(1)
	for i := range 3 {
		_, exit := spawnNamedAndWait502(t, k, "beta task", "agent-beta", []string{"skill-B"}, 10*time.Second)
		if exit.Code == 0 {
			t.Fatalf("agent-beta run %d: exit code = 0, want non-zero", i)
		}
	}

	// --- 断言 Score 分化 ---
	summaryAlpha, err := repStore.GetSummary("agent-alpha")
	if err != nil {
		t.Fatalf("GetSummary(agent-alpha) failed: %v", err)
	}
	summaryBeta, err := repStore.GetSummary("agent-beta")
	if err != nil {
		t.Fatalf("GetSummary(agent-beta) failed: %v", err)
	}

	if summaryAlpha.Score <= summaryBeta.Score {
		t.Errorf("reputation divergence failed: agent-alpha (3 successes) Score=%v <= agent-beta (3 failures) Score=%v; "+
			"expected alpha > beta from finishProcess natural writes", summaryAlpha.Score, summaryBeta.Score)
	}
}

// =============================================================================
// AC3: 全循环可观测性 · 磁盘数据验证
// =============================================================================

func TestATDD_50_2_DiskPersistenceObservability(t *testing.T) {
	flk := newFeedbackLoopKernel(t, defaultDiscoverFn502())
	k := flk.kernel

	// --- 累积阶段：3 成功 + 3 失败 ---
	flk.llmMode.Store(0)
	for i := range 3 {
		spawnNamedAndWait502(t, k, "disk test success", fmt.Sprintf("disk-alpha-%d", i), []string{"skill-A"}, 5*time.Second)
	}

	flk.llmMode.Store(1)
	for i := range 3 {
		spawnNamedAndWait502(t, k, "disk test failure", fmt.Sprintf("disk-beta-%d", i), []string{"skill-B"}, 10*time.Second)
	}

	// --- 验证 synergy JSONL ---
	synergyPath := filepath.Join(flk.repDir, "synergy-matrix.json")
	synergyLines := countJSONLLines(t, synergyPath)
	if synergyLines < 6 {
		t.Errorf("synergy JSONL: got %d lines, want ≥6 (3 passed + 3 failed)", synergyLines)
	}

	// --- 验证 reputation JSONL ---
	for i := range 3 {
		name := fmt.Sprintf("disk-alpha-%d", i)
		p := filepath.Join(flk.repDir, name+".json")
		lines := countJSONLLines(t, p)
		if lines != 1 {
			t.Errorf("reputation(%s): got %d lines, want 1", name, lines)
		}
	}
	for i := range 3 {
		name := fmt.Sprintf("disk-beta-%d", i)
		p := filepath.Join(flk.repDir, name+".json")
		lines := countJSONLLines(t, p)
		if lines != 1 {
			t.Errorf("reputation(%s): got %d lines, want 1", name, lines)
		}
	}
}

// =============================================================================
// AC4: 渐进收敛 · 累积越多分化越大
// =============================================================================

func TestATDD_50_2_ProgressiveConvergence(t *testing.T) {
	flk := newFeedbackLoopKernel(t, defaultDiscoverFn502())
	k := flk.kernel

	// --- Phase 1: 0 条记录，原序保持 ---
	flk.llmMode.Store(0)
	k.SetStemEpsilon(0)

	proc0 := spawnStemAndWait502(t, k, "analyze code deeply", 5*time.Second)

	if len(proc0.Skills) < 2 {
		t.Fatalf("phase 0: expected ≥2 skills, got %d: %v", len(proc0.Skills), proc0.Skills)
	}
	if proc0.Skills[0] != "skill-B" {
		t.Errorf("phase 0 (no data): first skill = %q, want skill-B (original keyword order)", proc0.Skills[0])
	}

	// --- Phase 2: 累积 3 成功(A) + 3 失败(B) ---
	flk.llmMode.Store(0)
	for i := range 3 {
		spawnNamedAndWait502(t, k, "converge success", fmt.Sprintf("conv-A-%d", i), []string{"skill-A"}, 5*time.Second)
	}
	flk.llmMode.Store(1)
	for i := range 3 {
		spawnNamedAndWait502(t, k, "converge failure", fmt.Sprintf("conv-B-%d", i), []string{"skill-B"}, 10*time.Second)
	}

	// --- Phase 3: 累积后翻转 ---
	flk.llmMode.Store(0)
	proc3 := spawnStemAndWait502(t, k, "analyze code deeply", 5*time.Second)

	if len(proc3.Skills) < 2 {
		t.Fatalf("phase 3: expected ≥2 skills, got %d: %v", len(proc3.Skills), proc3.Skills)
	}
	if proc3.Skills[0] != "skill-A" {
		t.Errorf("phase 3 (after accumulation): first skill = %q, want skill-A (synergy flip)", proc3.Skills[0])
	}
}

// =============================================================================
// AC5: 对照组 · ε=1 纯探索跳过重排
// =============================================================================

func TestATDD_50_2_ControlGroup_EpsilonPureExploration(t *testing.T) {
	flk := newFeedbackLoopKernel(t, defaultDiscoverFn502())
	k := flk.kernel

	// --- 累积数据 ---
	flk.llmMode.Store(0)
	for i := range 3 {
		spawnNamedAndWait502(t, k, "ctrl success", fmt.Sprintf("ctrl-A-%d", i), []string{"skill-A"}, 5*time.Second)
	}
	flk.llmMode.Store(1)
	for i := range 3 {
		spawnNamedAndWait502(t, k, "ctrl failure", fmt.Sprintf("ctrl-B-%d", i), []string{"skill-B"}, 10*time.Second)
	}

	// --- ε=0: 翻转发生（正组）---
	flk.llmMode.Store(0)
	k.SetStemEpsilon(0)
	procGreedy := spawnStemAndWait502(t, k, "analyze code deeply", 5*time.Second)

	if len(procGreedy.Skills) < 2 {
		t.Fatalf("greedy: expected ≥2 skills, got %v", procGreedy.Skills)
	}
	if procGreedy.Skills[0] != "skill-A" {
		t.Errorf("ε=0 (greedy): first skill = %q, want skill-A (rerank flipped)", procGreedy.Skills[0])
	}

	// --- ε=1: 原序保持（对照组）---
	k.SetStemEpsilon(1.0)
	procExplore := spawnStemAndWait502(t, k, "analyze code deeply", 5*time.Second)

	if len(procExplore.Skills) < 2 {
		t.Fatalf("explore: expected ≥2 skills, got %v", procExplore.Skills)
	}
	if procExplore.Skills[0] != "skill-B" {
		t.Errorf("ε=1 (explore): first skill = %q, want skill-B (original keyword order, rerank skipped)", procExplore.Skills[0])
	}
}
