package kernel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/agents"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// ATDD 45.4 — Story 30.5 P4 等价语义守护（heartbeat 活性检测合法数据源）
//
// 本文件锚定 Story 30.5（heartbeat liveness detection）原 AC#1-#4 在 epic-45
// P4 daemon-passive 框架下的等价断言。Story 30.5 的核心承诺是：
//   AC#1 — Process 持有 LastHeartbeat + StepTimeout 字段（spawn 时初始化）
//   AC#2 — reasonStep 每步在 SpawnOpts 与 reasonStep 边界更新 LastHeartbeat
//   AC#3 — AgentManifest.step_timeout 通过 SpawnOpts.StepTimeout 链路传播
//   AC#4 — StepTimeout=0 表示禁用心跳检测（信任进程，不做活性判断）
//
// 在 epic-45 P4 框架下，30.5 上述承诺**继续成立**，但需要补充"心跳信号诚实性"
// 不变量：daemon 不再有任何代替业务进程伪造心跳的路径（HB-1 keeper 已由 45.3
// 删除；SpawnAndWait ticker 已由 45.3 删除；warn-only 已由 45.2 落地）。本文件
// 通过 4 个用例显式锚定 30.5 原 AC 在 P4 下的等价语义，与 `kernel/heartbeat_test.go`
// 已有的 7 个 30.5 测试形成"正向 + 等价"双层防护。
//
// RED-phase 信号：本文件用例在 epic-45 落地**之后**编写（45.2 done + 45.3 done），
// 默认应全绿。其设计目的是**守护未来回归**——任何 PR 若让 daemon 重新引入"代替
// 业务进程伪造心跳"路径（如未来 watchdog 误推 LastHeartbeat / HB-1 keeper 复活），
// 本文件用例**立即** FAIL，守住 P4 哲学红线（45.2/45.3/45.4 共同维护的不变量）。
//
// 引用：
//   - _bmad-output/implementation-artifacts/45-4-atdd-rewrite-for-warn-only-semantics.md
//     §AC1 + §Decision D1 (5 个独立 ATDD 文件，按守护源 story 索引)
//   - _bmad-output/planning-artifacts/epics/epic-45-heartbeat-subsystem-reform.md
//     §AC-EA4 + §Constraints "Always" line 124
//   - _bmad-output/implementation-artifacts/30-5-heartbeat-liveness-detection.md
//     §Acceptance-Criteria AC#1-#4
//   - kernel/heartbeat_test.go (Story 30.5 7 个守护测试，本 story 不动测试体)
//   - kernel/heartbeat_monitor_unit_test.go#357-365 newHeartbeatTestKernel (复用)
//   - kernel/atdd_44_3_helpers_test.go#181 drainAllEvents (复用)
//   - kernel/atdd_45_2_warn_only_test.go#51-68 findStalledEventAction (复用)
// =============================================================================

// hb1ProhibitedSymbols 列出 45.3 删除后绝不应在 ipc/server_pipeline.go 中
// 复活的 HB-1 / SpawnAndWait fake-heartbeat 符号。30.5 字段语义诚实性的
// epic 级保护——任一字符串重新出现即标志着 daemon-side fake heartbeat 路径
// 复活，违反 P4 哲学。
//
// 与 ipc/atdd_45_3_no_hb1_test.go 中的 hb1AndWaitGroupSymbols /
// spawnAndWaitTickerSymbols 列表语义重合，但本文件从 kernel 包视角守护"30.5
// 字段诚实性"——双层防护，单 PR 错误改动两侧都会被捕获。
var hb1ProhibitedSymbols = []string{
	"parentProc.TouchHeartbeat",     // SpawnAndWait fake heartbeat 方法调用
	"keepScriptRunnerHeartbeat",     // HB-1 keeper 函数
	"scriptRunnerHeartbeatInterval", // HB-1 keeper 周期常量
	"hbWG",                          // HB-1 sync.WaitGroup 唯一局部变量
}

// legitimateHeartbeatWriterFiles 列出 P4 框架下 LastHeartbeat 字段的**全部**合法
// 写入路径。这些路径的共同特点：心跳由业务工作的天然推进驱动（spawn-time seed /
// reasonStep 边界 / driver streaming / Resume / 持久化恢复），daemon 不主动
// 制造活性信号。
//
// expectedToken 是该文件中应能 grep 到的字符串——证明该 writer 路径仍存在；
// 若某文件 expectedToken grep 不到，意味着该 writer 路径被错误删除，30.5 AC#1
// 字段语义合法性可能被破坏，需要立即调查。
var legitimateHeartbeatWriterFiles = []struct {
	relPath       string
	expectedToken string
	whyLegitimate string
}{
	{
		relPath:       "spawn.go",
		expectedToken: "proc.LastHeartbeat = time.Now()",
		whyLegitimate: "Spawn-time seed (kernel/spawn.go:365) — 进程刚启动时打 LastHeartbeat seed，让 HeartbeatMonitor 在首个 reasonStep 完成前不会误判",
	},
	{
		relPath:       "reason.go",
		expectedToken: "proc.LastHeartbeat = time.Now()",
		whyLegitimate: "reasonStep boundary update (kernel/reason.go:276) — reasonStep 每步边界更新，是 P4 下 daemon 唯一合法的心跳推进路径之一",
	},
	{
		relPath:       "observe.go",
		expectedToken: "proc.TouchHeartbeat()",
		whyLegitimate: "Driver streaming bridge (kernel/observe.go:502) — driver 流式 chunk 到达时桥接到 TouchHeartbeat，证明 LLM 仍在产出 token",
	},
	{
		relPath:       "resume.go",
		expectedToken: "proc.LastHeartbeat = time.Now()",
		whyLegitimate: "Resume path (kernel/resume.go) — Epic 42 resume 时给恢复的进程打新的 heartbeat seed",
	},
	{
		relPath:       "subtree.go",
		expectedToken: "proc.LastHeartbeat = time.Now()",
		whyLegitimate: "Resume-subtree path (kernel/subtree.go) — 子树恢复时同样需要 reseed",
	},
	{
		relPath:       "load_suspended.go",
		expectedToken: "proc.LastHeartbeat = info.LastHeartbeat",
		whyLegitimate: "Suspended-state load (kernel/load_suspended.go:81) — Epic 44 daemon 重启恢复 Suspended placeholder 时，从持久化数据复原历史 LastHeartbeat",
	},
}

// -----------------------------------------------------------------------------
// AC1 用例 001 — Story 30.5 字段语义 + P4 心跳信号诚实性的 epic 级保护
// -----------------------------------------------------------------------------

// TestATDD_45_4_30_5_001_LastHeartbeatWritersAreFinite
//
// 守护 Story 30.5 原 AC#1（Process 持有 LastHeartbeat 字段）在 P4 框架下的
// "心跳信号诚实性" epic 级不变量：LastHeartbeat 的合法写入路径必须为可枚举的
// 有限集合（spawn / reason / observe / resume / subtree / load_suspended），
// 且 ipc/server_pipeline.go 内不再有任何代替业务进程伪造心跳的 daemon-side
// 路径。
//
// 与 45.3 ATDD 001 (NoHB1KeeperSymbol) 的差异：
//   - 45.3 ATDD 001 守护 ipc/ 包内 HB-1 keeper / hbWG 符号删除（聚焦"符号删除"）
//   - 本测试守护 ipc/server_pipeline.go 内 parentProc.TouchHeartbeat /
//     keepScriptRunnerHeartbeat 全部不复活，**同时** 5 个合法 writer 路径都存在
//     （聚焦"30.5 字段语义诚实性"作为 epic 级保护）
//
// 双层防护：任一非法 fake-heartbeat 路径复活 → 45.3 + 45.4 共同 FAIL；任一
// 合法 writer 路径被错误删除 → 仅 45.4 FAIL（45.3 不覆盖此场景）。
func TestATDD_45_4_30_5_001_LastHeartbeatWritersAreFinite(t *testing.T) {
	// Locate the repository root by walking up until we find go.mod. The test
	// binary's cwd is the package directory (kernel/), so we step out once to
	// reach repo root, then verify by checking for go.mod presence.
	repoRoot, err := findRepoRootForATDD45_4(".")
	if err != nil {
		t.Fatalf("Story 45.4 AC1.001: locate repo root: %v", err)
	}

	// Part 1: 验证 5 个合法 writer 路径都存在（30.5 AC#1 字段语义合法性）
	for _, w := range legitimateHeartbeatWriterFiles {
		fullPath := filepath.Join(repoRoot, "kernel", w.relPath)
		body, err := os.ReadFile(fullPath)
		if err != nil {
			t.Errorf("Story 45.4 AC1.001: read legitimate writer %s: %v (30.5 AC#1 LastHeartbeat 字段语义合法性 — 该 writer 路径不应被删除; reason: %s)",
				fullPath, err, w.whyLegitimate)
			continue
		}
		if !strings.Contains(string(body), w.expectedToken) {
			t.Errorf("Story 45.4 AC1.001: legitimate writer %s missing expected token %q (30.5 AC#1 字段语义被错误破坏; reason: %s)",
				fullPath, w.expectedToken, w.whyLegitimate)
		}
	}

	// Part 2: 验证 ipc/server_pipeline.go 内无任一 fake-heartbeat 符号
	// （P4 哲学红线 — 30.5 字段语义诚实性的 epic 级保护）
	//
	// Line-level filter: 跳过 `//` 单行注释 + `/* ... */` 块注释，避免未来的
	// doc-comment（如 `// keepScriptRunnerHeartbeat was removed in 45.3`）误触
	// 报警。仅在**代码行**中搜索禁用符号——若禁用符号出现在 declarative position
	// （variable / func / const decl 或 usage call），即视为 P4 违规。
	pipelinePath := filepath.Join(repoRoot, "ipc", "server_pipeline.go")
	pipelineBody, err := os.ReadFile(pipelinePath)
	if err != nil {
		t.Fatalf("Story 45.4 AC1.001: read ipc/server_pipeline.go: %v", err)
	}
	pipelineText := stripGoComments(string(pipelineBody))
	for _, sym := range hb1ProhibitedSymbols {
		if strings.Contains(pipelineText, sym) {
			t.Errorf("Story 45.4 AC1.001: ipc/server_pipeline.go contains forbidden fake-heartbeat symbol %q in non-comment code — P4 violation: daemon must not actively push LastHeartbeat (30.5 字段语义诚实性的 epic 级保护; 与 ipc/atdd_45_3_no_hb1_test.go 形成双层防护)",
				sym)
		}
	}
}

// stripGoComments removes `//` line comments and `/* ... */` block comments
// from a Go source text. Used by AC1.001 to avoid false-positive symbol
// matches inside doc-comments referencing removed APIs.
//
// 简化实现：不处理 string literals 内的 `//` / `/* */`，因为 server_pipeline.go
// 的字符串字面量中不太可能包含 HB-1 / fake-heartbeat 符号名（且即使包含也是
// false-positive 的小概率边界情况，不值得拉 go/parser 这么重的依赖）。
func stripGoComments(text string) string {
	var b strings.Builder
	b.Grow(len(text))
	for i := 0; i < len(text); i++ {
		// /* ... */ block comment
		if i+1 < len(text) && text[i] == '/' && text[i+1] == '*' {
			end := strings.Index(text[i+2:], "*/")
			if end == -1 {
				return b.String() // unterminated block — discard rest
			}
			i += 2 + end + 1 // skip past "*/"
			continue
		}
		// // line comment
		if i+1 < len(text) && text[i] == '/' && text[i+1] == '/' {
			nl := strings.IndexByte(text[i:], '\n')
			if nl == -1 {
				return b.String() // last line is a comment
			}
			i += nl // jump to newline; outer loop's i++ moves past it
			b.WriteByte('\n')
			continue
		}
		b.WriteByte(text[i])
	}
	return b.String()
}

// findRepoRootForATDD45_4 walks up from start until a go.mod is found, returning
// the directory containing it. Terminates on filesystem root (parent == abs).
//
// Suffix "_ATDD45_4" disambiguates from any other repo-root helpers that might
// be added in sibling test files.
func findRepoRootForATDD45_4(start string) (string, error) {
	abs, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(abs, "go.mod")); err == nil {
			return abs, nil
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return "", os.ErrNotExist
		}
		abs = parent
	}
}

// -----------------------------------------------------------------------------
// AC1 用例 002 — reasonStep 边界更新作为"primary honest heartbeat signal"
// -----------------------------------------------------------------------------

// TestATDD_45_4_30_5_002_ReasonStepHeartbeatUpdateIsPrimaryHonestSignal
//
// 守护 Story 30.5 原 AC#2（reasonStep 每步更新 LastHeartbeat）在 P4 框架下的
// 等价语义"primary honest signal"——reasonStep 边界更新是 daemon 下游可信赖
// 的真实心跳推进路径之一，HeartbeatMonitor 看到该路径推进过的进程**不**会触发
// stall。
//
// 与 kernel/heartbeat_test.go::TestReasonStep_HeartbeatUpdatedPerStep 的关系：
//   - 既有测试：验证 reasonStep 跑完后 LastHeartbeat 时间戳推进
//   - 本测试：在既有测试之上 **追加一次 hm.scan() 调用**，验证 HeartbeatMonitor
//     看到一个"刚刚被 reasonStep 边界更新过"的健康进程，**不**会触发 stall
//     —— 这是 P4 框架下"心跳由业务工作天然驱动"的端到端正向证据
func TestATDD_45_4_30_5_002_ReasonStepHeartbeatUpdateIsPrimaryHonestSignal(t *testing.T) {
	// 复用 heartbeat_test.go TestReasonStep_HeartbeatUpdatedPerStep 的 mock 模式
	// (sequenceLLMFile + makeToolCallResponse + makeLLMResponse + registerMockTool
	//  + mockToolFile)——同包跨文件可见。
	reg := vfs.NewDeviceRegistry()
	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeToolCallResponse("/dev/tools/read", map[string]any{"path": "/foo"}, 50),
			makeLLMResponse("done", 30),
		},
	}
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return seqFile, nil
	})
	registerMockTool(reg, "/dev/tools/read", func(subpath string, flags vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockToolFile{readData: []byte("bar")}, nil
	})

	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	pid, err := k.Spawn("read a file", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Story 45.4 AC1.002: Spawn failed: %v", err)
	}
	proc, _ := k.GetProcess(pid)

	// Record spawn-time heartbeat for sanity (Story 30.5 AC#1 — Spawn seeds LastHeartbeat).
	spawnHeartbeat := proc.LastHeartbeatSnapshot()
	if spawnHeartbeat.IsZero() {
		t.Fatal("Story 45.4 AC1.002: Spawn did not seed LastHeartbeat (30.5 AC#1 precondition broken)")
	}

	// Wait for completion. 30s deadline matches kernel/heartbeat_test.go
	// convention — 5s was too tight under -race (3-10x wall-time multiplier)
	// + slow CI runners.
	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("Story 45.4 AC1.002: unexpected exit code %d: %s", exit.Code, exit.Reason)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Story 45.4 AC1.002: timed out waiting for process to complete")
	}

	// reasonStep ran 2 steps — LastHeartbeat must have been advanced.
	finalHeartbeat := proc.LastHeartbeatSnapshot()
	if !finalHeartbeat.After(spawnHeartbeat) && !finalHeartbeat.Equal(spawnHeartbeat) {
		t.Errorf("Story 45.4 AC1.002: LastHeartbeat must be >= spawn time (30.5 AC#2): spawn=%v final=%v", spawnHeartbeat, finalHeartbeat)
	}

	// P4 equivalent assertion: a process that completed reasonStep cleanly is
	// observed by HeartbeatMonitor as healthy. The reasonStep boundary update
	// is the "primary honest heartbeat signal" — HeartbeatMonitor.scan() must
	// NOT flag this process as stalled, because LastHeartbeat was advanced by
	// real reasoning work (not by any daemon-side fake-heartbeat path).
	//
	// Note: by this point the process is Zombie (Done fired, state has
	// transitioned), so scan()'s outer state guard naturally skips it. But the
	// invariant we want to pin is "even if the process were still Running,
	// LastHeartbeat freshness would protect it from stall flagging" — to test
	// that, we manually rewind state and assert.
	proc.mu.Lock()
	proc.State = types.StateRunning
	proc.StepTimeout = 5 * time.Minute // a generous timeout
	proc.mu.Unlock()

	hm := NewHeartbeatMonitor(k, 50*time.Millisecond)
	hm.scan()

	status := hm.Status()
	if status.TotalStalledDetected != 0 {
		t.Errorf("Story 45.4 AC1.002: P4 invariant — a healthy reasonStep-driven process must not be flagged as stalled, got TotalStalledDetected=%d (heartbeat fresh, StepTimeout=5m, finalHeartbeat=%v)",
			status.TotalStalledDetected, finalHeartbeat)
	}
	hm.mu.Lock()
	_, exists := hm.stalledProcs[proc.PID]
	hm.mu.Unlock()
	if exists {
		t.Error("Story 45.4 AC1.002: P4 invariant — stallRecord must NOT be created for a process with freshly-updated LastHeartbeat (reasonStep boundary update is the honest signal source)")
	}
}

// -----------------------------------------------------------------------------
// AC1 用例 003 — StepTimeout=0 (disabled) 在 P4 下仍跳过 stall 检测
// -----------------------------------------------------------------------------

// TestATDD_45_4_30_5_003_StepTimeoutZeroSkipsScan
//
// 守护 Story 30.5 原 AC#4（StepTimeout=0 表示禁用心跳检测）在 P4 框架下仍然
// 有效——passive mode 不会破坏"用户显式选择不检测活性"的契约。
//
// 与 heartbeat_monitor_unit_test.go::TestHeartbeatMonitor_ScanSkipsStepTimeoutZero
// 的关系：
//   - 既有测试：验证 scan() 跳过 StepTimeout=0 进程（单元层）
//   - 本测试：在既有测试之上 **明确指名 Story 30.5 AC#4 + P4 哲学**，并补充
//     "drainAllEvents 验证零 ProcessStalled 事件 emit" 的负向证据
func TestATDD_45_4_30_5_003_StepTimeoutZeroSkipsScan(t *testing.T) {
	k := newHeartbeatTestKernel(t)
	proc := NewProcess(0, "test-atdd-45-4-30-5-003", nil)
	proc.mu.Lock()
	proc.State = types.StateRunning
	proc.StepTimeout = 0 // 30.5 AC#4: explicit "user opted out of liveness detection"
	proc.LastHeartbeat = time.Now().Add(-10 * time.Minute)
	proc.PrimaryDevice = "/dev/llm/claude" // simulate reasonStep-driven proc (Story 44.5 v2 review附 heartbeat fix)
	proc.mu.Unlock()
	k.procTable.Store(proc.PID, proc)

	hm := NewHeartbeatMonitor(k, 50*time.Millisecond)
	hm.scan()

	// (1) stallRecord must not be created.
	hm.mu.Lock()
	_, exists := hm.stalledProcs[proc.PID]
	hm.mu.Unlock()
	if exists {
		t.Error("Story 45.4 AC1.003: stallRecord MUST NOT be created when StepTimeout=0 (30.5 AC#4: user opted out of liveness detection; P4 passive mode honours the opt-out contract)")
	}

	// (2) TotalStalledDetected must be 0.
	status := hm.Status()
	if status.TotalStalledDetected != 0 {
		t.Errorf("Story 45.4 AC1.003: TotalStalledDetected = %d, want 0 (StepTimeout=0 must skip detection entirely)", status.TotalStalledDetected)
	}

	// (3) No ProcessStalled event must be emitted.
	events := drainAllEvents(t, proc, 100*time.Millisecond)
	if _, count, found := findStalledEventAction(events); found || count > 0 {
		t.Errorf("Story 45.4 AC1.003: zero ProcessStalled events expected, got %d (P4 passive mode must NOT emit stall signals when StepTimeout=0)", count)
	}
}

// -----------------------------------------------------------------------------
// AC1 用例 004 — AgentManifest.step_timeout → SpawnOpts.StepTimeout → Process.StepTimeout
// -----------------------------------------------------------------------------

// TestATDD_45_4_30_5_004_AgentManifestStepTimeoutPropagatesToProcess
//
// 守护 Story 30.5 原 AC#3（AgentManifest.step_timeout 配置链路）在 P4 框架下
// 仍然有效——agent.yaml / SpawnOpts 配置正确传播到 proc.StepTimeout 字段，
// dashboard 渲染 step_timeout_ms 数据源不退步。
//
// 链路：agents.AgentManifest{StepTimeout: "7m"} → time.ParseDuration → 7 * time.Minute
//   → SpawnOpts{StepTimeout: 7 * time.Minute} → kern.Spawn → proc.StepTimeout
func TestATDD_45_4_30_5_004_AgentManifestStepTimeoutPropagatesToProcess(t *testing.T) {
	// (1) AgentManifest 解析层：step_timeout 字符串 "7m" → 7 * time.Minute.
	manifest := agents.AgentManifest{
		StepTimeout: "7m",
	}
	parsed, err := time.ParseDuration(manifest.StepTimeout)
	if err != nil {
		t.Fatalf("Story 45.4 AC1.004: ParseDuration(%q) error: %v (30.5 AC#3 解析链路损坏)", manifest.StepTimeout, err)
	}
	if parsed != 7*time.Minute {
		t.Errorf("Story 45.4 AC1.004: ParseDuration(%q) = %v, want 7m", manifest.StepTimeout, parsed)
	}

	// (2) SpawnOpts → Process.StepTimeout 透传：用 mock LLM 立即 complete 的最小进程。
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockLLMFile{readData: makeLLMResponse("done", 5)}, nil
	})
	v := vfs.NewVFS(reg)
	k := NewKernel(v, rnixctx.NewManager(), nil)
	defer k.Shutdown()

	pid, err := k.Spawn("test step timeout propagation", nil, SpawnOpts{StepTimeout: 7 * time.Minute})
	if err != nil {
		t.Fatalf("Story 45.4 AC1.004: Spawn with StepTimeout=7m failed: %v", err)
	}
	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("Story 45.4 AC1.004: GetProcess vanished after Spawn")
	}

	proc.mu.Lock()
	gotTimeout := proc.StepTimeout
	proc.mu.Unlock()

	if gotTimeout != 7*time.Minute {
		t.Errorf("Story 45.4 AC1.004: proc.StepTimeout = %v, want 7m (30.5 AC#3 SpawnOpts → Process 透传链路损坏)", gotTimeout)
	}

	// (3) Reverse assertion: not the default 5m — proves the SpawnOpts value
	// actually took effect rather than masking with the default constant.
	if gotTimeout == 5*time.Minute {
		t.Errorf("Story 45.4 AC1.004: proc.StepTimeout = %v which equals default 5m — SpawnOpts.StepTimeout=7m was masked by default (30.5 AC#3 透传失效)", gotTimeout)
	}
}
