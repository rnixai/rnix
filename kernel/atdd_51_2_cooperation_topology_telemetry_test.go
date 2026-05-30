package kernel

import (
	gocontext "context"
	"sync"
	"testing"
	"time"

	"github.com/rnixai/rnix/agents"
	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// ATDD 51.2: 协作拓扑遥测补活 + reinforced_paths 正名（EM-3）
//
// 来源 Story：_bmad-output/implementation-artifacts/51-2-cooperation-topology-telemetry.md
// 审计来源：completion-audit-suspects-2026-05-30.md §A2 (AUD-EM / EM-3)
// Story Key: 51-2-cooperation-topology-telemetry
//
// 验收标准（Acceptance Criteria，节选 unit/集成可测项）：
//   AC1（spawn feed）   : Spawn 子进程(ParentPID>0, immune 活跃) → RecordCooperationTyped(parent,child,"spawn")
//   AC2（msg feed）     : Send 成功 enqueue(immune 活跃) → RecordCooperationTyped(from,to,"msg")
//   AC3（PID→模板解析器）: 新增 AgentTemplateForPID(pid) — 命中返回模板名；未知 PID / nil daemon 返回 ""
//   AC4（空名守卫）     : 任一模板名为 "" → 跳过记录，topology 不含空节点边
//   AC5（结果级·核心）  : 经真实 Spawn(父→子)/Send(agent→agent) 后 GetTopology 非空，
//                         SpawnCount/MsgCount/Total 计数正确（经 Spawn/Send 而非直接调 RecordCooperationTyped）
//   AC6（默认禁用零回归）: k.immuneDaemon==nil 时 Spawn/Send 正常、无 panic
//   AC7（相似度副作用）  : feed 后 GetSimilarity 的 CoopScore > 0（RecordCooperationTyped→RecordCooperation）
//   AC9（并发安全）     : 并发 Send 注入路径 -race 干净
//   （AC8 文档正名 / AC10 make all 门禁为非 unit 范围，不在此文件覆盖）
//
// ============================== RED PHASE ==============================
// 本测试在 Story 51.2 实现前应 FAIL（红灯）。
// 预期失败原因：kernel 包无法编译——导出方法 AgentTemplateForPID 尚未实现
// （undefined），且 spawn.go/ipc.go 尚未接通 feed 注入点。实现后所有用例
// 应转绿（含 go test -race）。这是 Go ATDD 的标准红灯形态（缺失 API →
// 包编译失败），与 kernel 既有 atdd_* 集成测试的红灯模式一致。
//
// 注意：测试严格使用 Story Dev Notes 规定的契约：
//   - 新方法：func (d *ImmuneDaemon) AgentTemplateForPID(pid types.PID) string
//   - feed 注入点经既有 `if k.immuneDaemon != nil` 守卫，immune 活跃时才记录
//   - 模板名来源 = ImmuneDaemon.collectors[pid].agentTemplate（OnProcessStart 注册）
//   - 断言只依赖既有公共 API（Spawn/Send/GetTopology/GetSimilarity）+ 新 AgentTemplateForPID
// ======================================================================

// --- parkingLLMFile: 让 spawned 进程驻留 Running 的握手 mock ---
//
// kernel 既有 mockLLMFile 的 Read 立即返回，进程会迅速 complete→reap，
// 导致 collector 在子进程 Spawn 时已被 OnProcessExit 删除（时序竞态）。
// parkingLLMFile.Read 阻塞在 release channel 上，使进程在首次 LLM Read 处
// 停住（已 past proc.Start() → Running、collector 已注册），从而稳健地
// 验证「真实 Spawn/Send 路径」而非依赖异步 reap 时序。
//
// 生命周期：releaseAll 必须在 k.Shutdown 之前执行（否则 Shutdown 的
// wg.Wait 会因 Read 永久阻塞而死锁）——helper 用 t.Cleanup LIFO 顺序保证。
type parkingLLMFile struct {
	release chan struct{}
	once    sync.Once
}

func newParkingLLMFile() *parkingLLMFile {
	return &parkingLLMFile{release: make(chan struct{})}
}

func (f *parkingLLMFile) Write(_ gocontext.Context, _ []byte) error { return nil }

func (f *parkingLLMFile) Read(_ int) ([]byte, error) {
	<-f.release // park until released at cleanup
	// After release, return a terminal "complete" response so reasonStep exits
	// cleanly and Shutdown's wg.Wait can join the goroutine.
	return makeLLMResponse("done", 1), nil
}

func (f *parkingLLMFile) Close() error { return nil }

func (f *parkingLLMFile) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{IsDevice: true, Name: "/dev/llm/claude"}, nil
}

// SupportsToolCalling lets the kernel populate native tool defs during spawn.
func (f *parkingLLMFile) SupportsToolCalling() bool { return true }

func (f *parkingLLMFile) releaseAll() { f.once.Do(func() { close(f.release) }) }

// testNamedAgent builds a minimal agent (no skills → no device requirements)
// whose Manifest.Name becomes the immune collector's agentTemplate.
func testNamedAgent(name string) *agents.AgentInfo {
	return &agents.AgentInfo{
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
}

// newImmuneSpawnKernel wires a kernel (parking LLM) with an ACTIVE immune
// daemon so that real Spawn/Send drive OnProcessStart → collector registration
// → feed injection. Cleanup order (LIFO): d.Stop → park.releaseAll → k.Shutdown.
func newImmuneSpawnKernel(t *testing.T) (*KernelImpl, *ImmuneDaemon, *parkingLLMFile) {
	t.Helper()
	park := newParkingLLMFile()

	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return park, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)

	// Register Shutdown FIRST so it runs LAST; releaseAll runs before it to
	// unblock parked Reads and let goroutines exit cleanly (avoid wg deadlock).
	t.Cleanup(k.Shutdown)
	t.Cleanup(park.releaseAll)

	store := NewImmuneStore(t.TempDir())
	d := NewImmuneDaemon(store, DefaultImmuneConfig())
	if err := d.Start(); err != nil {
		t.Fatalf("immune daemon Start failed: %v", err)
	}
	t.Cleanup(d.Stop)
	k.SetImmuneDaemon(d)

	return k, d, park
}

// waitForCollector polls until the immune daemon has registered a collector for
// pid (OnProcessStart runs synchronously inside Spawn, but proc.Start ordering
// is defensive-polled to avoid flakiness).
func waitForCollector(t *testing.T, d *ImmuneDaemon, pid types.PID) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if d.AgentTemplateForPID(pid) != "" {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("collector for pid %d not registered within deadline", pid)
}

// findEdge returns the topology edge between agents a and b (order-insensitive),
// or nil if absent.
func findEdge(topo *CollaborationTopology, a, b string) *CooperationEdge {
	if topo == nil {
		return nil
	}
	for i := range topo.Edges {
		e := topo.Edges[i]
		if (e.From == a && e.To == b) || (e.From == b && e.To == a) {
			return &topo.Edges[i]
		}
	}
	return nil
}

// =============================================================================
// AC3: AgentTemplateForPID 解析器
// =============================================================================

func TestATDD_51_2_AC3_AgentTemplateForPID_Hit(t *testing.T) {
	store := NewImmuneStore(t.TempDir())
	d := NewImmuneDaemon(store, DefaultImmuneConfig())
	if err := d.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer d.Stop()

	d.OnProcessStart(types.PID(42), "code-analyst")

	if got := d.AgentTemplateForPID(types.PID(42)); got != "code-analyst" {
		t.Errorf("AgentTemplateForPID(42) = %q, want %q", got, "code-analyst")
	}
}

func TestATDD_51_2_AC3_AgentTemplateForPID_UnknownPID(t *testing.T) {
	store := NewImmuneStore(t.TempDir())
	d := NewImmuneDaemon(store, DefaultImmuneConfig())
	if err := d.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer d.Stop()

	if got := d.AgentTemplateForPID(types.PID(9999)); got != "" {
		t.Errorf("AgentTemplateForPID(unknown) = %q, want empty", got)
	}
}

func TestATDD_51_2_AC3_AgentTemplateForPID_NilDaemon(t *testing.T) {
	var d *ImmuneDaemon
	if got := d.AgentTemplateForPID(types.PID(1)); got != "" {
		t.Errorf("nil daemon AgentTemplateForPID = %q, want empty", got)
	}
}

// =============================================================================
// AC1 + AC5: spawn parent→child feed → topology 非空、SpawnCount 正确
// =============================================================================

func TestATDD_51_2_AC5_SpawnFeed_TopologyNonEmpty(t *testing.T) {
	k, d, _ := newImmuneSpawnKernel(t)

	// Spawn parent (top-level, ParentPID == 0).
	parentPID, err := k.Spawn("parent intent", testNamedAgent("agent-parent"), SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn parent failed: %v", err)
	}
	waitForCollector(t, d, parentPID)

	// Spawn child with ParentPID set → triggers spawn feed.
	childPID, err := k.Spawn("child intent", testNamedAgent("agent-child"), SpawnOpts{ParentPID: parentPID})
	if err != nil {
		t.Fatalf("Spawn child failed: %v", err)
	}
	waitForCollector(t, d, childPID)

	topo := d.GetTopology()
	if topo == nil {
		t.Fatal("GetTopology returned nil")
	}
	if len(topo.Edges) == 0 {
		t.Fatal("expected non-empty topology after real Spawn(parent→child), got 0 edges")
	}

	edge := findEdge(topo, "agent-parent", "agent-child")
	if edge == nil {
		t.Fatalf("expected edge between agent-parent and agent-child, edges=%+v", topo.Edges)
	}
	if edge.SpawnCount < 1 {
		t.Errorf("SpawnCount = %d, want >= 1", edge.SpawnCount)
	}
	if edge.Total != edge.SpawnCount+edge.MsgCount {
		t.Errorf("Total = %d, want SpawnCount(%d)+MsgCount(%d)", edge.Total, edge.SpawnCount, edge.MsgCount)
	}
}

// AC1: 多次 spawn 同一 parent→child 模板对，SpawnCount 单调递增。
func TestATDD_51_2_AC1_SpawnFeed_Monotonic(t *testing.T) {
	k, d, _ := newImmuneSpawnKernel(t)

	parentPID, err := k.Spawn("parent intent", testNamedAgent("agent-parent"), SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn parent failed: %v", err)
	}
	waitForCollector(t, d, parentPID)

	for i := 0; i < 2; i++ {
		childPID, err := k.Spawn("child intent", testNamedAgent("agent-child"), SpawnOpts{ParentPID: parentPID})
		if err != nil {
			t.Fatalf("Spawn child #%d failed: %v", i, err)
		}
		waitForCollector(t, d, childPID)
	}

	edge := findEdge(d.GetTopology(), "agent-parent", "agent-child")
	if edge == nil {
		t.Fatal("expected agent-parent↔agent-child edge after two spawns")
	}
	if edge.SpawnCount < 2 {
		t.Errorf("SpawnCount = %d, want >= 2 after two parent→child spawns", edge.SpawnCount)
	}
}

// =============================================================================
// AC2 + AC5: agent→agent msg feed → MsgCount 正确
// =============================================================================

func TestATDD_51_2_AC5_MsgFeed_TopologyNonEmpty(t *testing.T) {
	k, d, _ := newImmuneSpawnKernel(t)

	p1, err := k.Spawn("agent a intent", testNamedAgent("agent-a"), SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn agent-a failed: %v", err)
	}
	waitForCollector(t, d, p1)

	p2, err := k.Spawn("agent b intent", testNamedAgent("agent-b"), SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn agent-b failed: %v", err)
	}
	waitForCollector(t, d, p2)

	if err := k.Send(p1, p2, []byte("hello")); err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	edge := findEdge(d.GetTopology(), "agent-a", "agent-b")
	if edge == nil {
		t.Fatalf("expected agent-a↔agent-b edge after Send, edges=%+v", d.GetTopology().Edges)
	}
	if edge.MsgCount < 1 {
		t.Errorf("MsgCount = %d, want >= 1 after Send", edge.MsgCount)
	}
}

// AC2: 多次 Send 同一对，MsgCount 单调递增。
func TestATDD_51_2_AC2_MsgFeed_Monotonic(t *testing.T) {
	k, d, _ := newImmuneSpawnKernel(t)

	p1, err := k.Spawn("agent a intent", testNamedAgent("agent-a"), SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn agent-a failed: %v", err)
	}
	waitForCollector(t, d, p1)
	p2, err := k.Spawn("agent b intent", testNamedAgent("agent-b"), SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn agent-b failed: %v", err)
	}
	waitForCollector(t, d, p2)

	for i := 0; i < 3; i++ {
		if err := k.Send(p1, p2, []byte("ping")); err != nil {
			t.Fatalf("Send #%d failed: %v", i, err)
		}
	}

	edge := findEdge(d.GetTopology(), "agent-a", "agent-b")
	if edge == nil {
		t.Fatal("expected agent-a↔agent-b edge after sends")
	}
	if edge.MsgCount < 3 {
		t.Errorf("MsgCount = %d, want >= 3 after three Sends", edge.MsgCount)
	}
}

// =============================================================================
// AC4: 空名守卫 — ad-hoc intent spawn (agent==nil → agentName=="") 不产生空节点边
// =============================================================================

func TestATDD_51_2_AC4_EmptyNameGuard_AdHocSpawn(t *testing.T) {
	k, d, _ := newImmuneSpawnKernel(t)

	parentPID, err := k.Spawn("parent intent", testNamedAgent("agent-parent"), SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn parent failed: %v", err)
	}
	waitForCollector(t, d, parentPID)

	// Ad-hoc spawn: no agent → child agentName == "" → spawn feed must skip.
	childPID, err := k.Spawn("ad-hoc child intent", nil, SpawnOpts{ParentPID: parentPID})
	if err != nil {
		t.Fatalf("ad-hoc Spawn failed: %v", err)
	}
	_ = childPID

	topo := d.GetTopology()
	// No edge should involve an empty-string node.
	for _, e := range topo.Edges {
		if e.From == "" || e.To == "" {
			t.Errorf("topology contains empty-node edge: %+v", e)
		}
	}
	for _, n := range topo.Nodes {
		if n.Agent == "" {
			t.Errorf("topology contains empty-named node: %+v", n)
		}
	}
}

// =============================================================================
// AC6: 默认禁用零回归 — k.immuneDaemon == nil 时 Spawn/Send 正常、无 panic
// =============================================================================

func TestATDD_51_2_AC6_NilDaemon_SpawnSendNoRegression(t *testing.T) {
	// Build a kernel WITHOUT SetImmuneDaemon (default config → nil daemon).
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

	parentPID, err := k.Spawn("parent intent", testNamedAgent("agent-parent"), SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn parent failed with nil immune daemon: %v", err)
	}
	childPID, err := k.Spawn("child intent", testNamedAgent("agent-child"), SpawnOpts{ParentPID: parentPID})
	if err != nil {
		t.Fatalf("Spawn child failed with nil immune daemon: %v", err)
	}
	if err := k.Send(parentPID, childPID, []byte("hi")); err != nil {
		t.Fatalf("Send failed with nil immune daemon: %v", err)
	}
	// Reaching here without panic satisfies the nil-safe feed requirement.
}

// =============================================================================
// AC7: 向后兼容副作用 — feed 后 GetSimilarity.CoopScore > 0
// =============================================================================

func TestATDD_51_2_AC7_FeedDrivesCoopScore(t *testing.T) {
	k, d, _ := newImmuneSpawnKernel(t)

	parentPID, err := k.Spawn("parent intent", testNamedAgent("agent-parent"), SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn parent failed: %v", err)
	}
	waitForCollector(t, d, parentPID)
	childPID, err := k.Spawn("child intent", testNamedAgent("agent-child"), SpawnOpts{ParentPID: parentPID})
	if err != nil {
		t.Fatalf("Spawn child failed: %v", err)
	}
	waitForCollector(t, d, childPID)

	// RecordCooperationTyped (via spawn feed) also feeds coopHistory → CoopScore.
	d.UpdateSimilarityMatrix(map[string][]string{
		"agent-parent": {"skill-1"},
		"agent-child":  {"skill-2"},
	})

	sim := d.GetSimilarity("agent-parent", "agent-child")
	if sim == nil {
		t.Fatal("expected non-nil similarity")
	}
	if sim.CoopScore <= 0.0 {
		t.Errorf("CoopScore = %f, want > 0 (spawn feed should populate coopHistory)", sim.CoopScore)
	}
}

// =============================================================================
// AC9: 并发 Send 注入路径 -race 干净
// =============================================================================

func TestATDD_51_2_AC9_ConcurrentSendFeed_RaceClean(t *testing.T) {
	k, d, _ := newImmuneSpawnKernel(t)

	p1, err := k.Spawn("agent a intent", testNamedAgent("agent-a"), SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn agent-a failed: %v", err)
	}
	waitForCollector(t, d, p1)
	p2, err := k.Spawn("agent b intent", testNamedAgent("agent-b"), SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn agent-b failed: %v", err)
	}
	waitForCollector(t, d, p2)

	const goroutines = 8
	const sends = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < sends; i++ {
				_ = k.Send(p1, p2, []byte("concurrent"))
			}
		}()
	}
	wg.Wait()

	edge := findEdge(d.GetTopology(), "agent-a", "agent-b")
	if edge == nil {
		t.Fatal("expected agent-a↔agent-b edge after concurrent sends")
	}
	if edge.MsgCount < goroutines*sends {
		t.Errorf("MsgCount = %d, want >= %d after concurrent sends", edge.MsgCount, goroutines*sends)
	}
}
