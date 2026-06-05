package kernel

// ATDD Story 37.5 — 统一 spawn 设备继承语义（ActionSpawn 对齐 decompose）
//
// 红阶段（TDD RED）。本文件在实现前刻意失败，驱动 dev-story 落地以下改动：
//   Task 1 — 新增单点 helper：
//       var orchestrationOnlyDevices = []string{"/dev/intent"}
//       func stripOrchestrationDevices(devs []string) []string
//   Task 2 — kernel/tool_exec.go ActionSpawn 分支用 helper 构造 childOpts：
//       childOpts.AllowedDevices = stripOrchestrationDevices(proc.AllowedDevices)  // 剔除后为空 → nil（fail-open）
//       childOpts.DeniedDevices  = union(proc.DeniedDevices, orchestrationOnlyDevices)
//
// RED 形态（沿用项目 ATDD 范式，Go 不用 test.skip —— 参考 atdd-checklist-44-5.md）：
//   - 编译 RED：本文件引用尚不存在的 stripOrchestrationDevices / orchestrationOnlyDevices，
//     整个 kernel 包测试 build 失败（`undefined: stripOrchestrationDevices`）——最强 RED 信号。
//   - 行为 RED：实现 helper 后，若 ActionSpawn 仍原样继承（当前 tool_exec.go:789），
//     端到端断言失败（子进程 AllowedDevices == ['/dev/intent']）。
//
// 验证 behavioral RED 时可临时加 stub（验证后必须删除，恢复编译 RED）：
//   // kernel/atdd_37_5_stub_test.go (TEMPORARY)
//   var orchestrationOnlyDevices = []string{"/dev/intent"}
//   func stripOrchestrationDevices(devs []string) []string { return devs } // 原样返回 → RED 触发

import (
	gocontext "context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/vfs"
)

// ───────────────────────────── 测试基础设施 ─────────────────────────────

// spawnGateLLM 让首个读取它的进程（本测试里始终是 ActionSpawn 派生的子进程）
// 在首次 LLM Read 处停住，直到测试 close(release)。子进程因此停在 Running 状态、
// 仍在 procTable 中，使我们能稳定断言其 AllowedDevices/DeniedDevices——消除
// CLAUDE.md「Known Test Issues」记录的 reap 竞态（atdd_42_2 flaky 根因：进程秒退被
// reaper 移出 procTable）。这是 ipc/atdd_42_1 parkOnRead 握手 gate 的同构应用。
//
// 与 kernel 既有 parkingLLMFile（atdd_51_2）的区别：本 gate 额外暴露 reached 信号，
// 让测试精确知道子进程「何时」停在 Read，从而在子进程仍 Running 时断言其设备集。
type spawnGateLLM struct {
	mu       sync.Mutex
	readData []byte
	reached  chan struct{} // 子进程到达首次 Read 时关闭（仅一次）
	release  chan struct{} // 测试断言完毕后 close 放行
	parked   bool
}

func (f *spawnGateLLM) Read(_ int) ([]byte, error) {
	f.mu.Lock()
	first := !f.parked
	if first {
		f.parked = true
		close(f.reached)
	}
	f.mu.Unlock()
	if first {
		<-f.release // 仅首次 Read 阻塞，等测试放行；后续 Read 直接返回
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.readData, nil
}

func (f *spawnGateLLM) Write(_ gocontext.Context, _ []byte) error { return nil }
func (f *spawnGateLLM) Close() error                              { return nil }
func (f *spawnGateLLM) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{IsDevice: true, Name: "/dev/llm/claude"}, nil
}

// SupportsToolCalling 实现 vfs.ToolCapable，使 spawn 路径填充 toolMap（observe.go）。
func (f *spawnGateLLM) SupportsToolCalling() bool { return true }

// newSpawnGate 构造一个携带 reached/release 通道的 gate，readData 为放行后返回的
// 终止响应（complete），使子进程 reasonStep 干净退出、Shutdown 的 wg.Wait 能 join。
func newSpawnGate() *spawnGateLLM {
	return &spawnGateLLM{
		readData: makeLLMResponse("done", 1),
		reached:  make(chan struct{}),
		release:  make(chan struct{}),
	}
}

// newDeviceInheritanceKernel 构造一个注册了 spawn-gate LLM + /dev/shell + /dev/fs +
// /dev/intent 的 kernel（与 newMinimalKernel 同构，但 LLM 用 spawn gate）。
func newDeviceInheritanceKernel(t *testing.T, park *spawnGateLLM) (*KernelImpl, *rnixctx.Manager) {
	t.Helper()
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return park, nil
	})
	registerMockTool(reg, "/dev/fs", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockToolFile{readData: []byte("data")}, nil
	})
	registerMockTool(reg, "/dev/shell", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockToolFile{readData: []byte("ok")}, nil
	})
	registerMockTool(reg, "/dev/intent", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockToolFile{readData: []byte("{}")}, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	k.SetStepDataDir(t.TempDir())
	t.Cleanup(k.Shutdown)
	return k, ctxMgr
}

// driveActionSpawnAndCaptureChild 用受限设备的父进程驱动一次真实 ActionSpawn
// （executeMetaAction 的 ActionSpawn 分支），在子进程停在首次 LLM Read 时捕获它并返回。
// 返回的 release 必须在断言完成后调用，放行子进程并等待 executeMetaAction 收尾。
//
// 父进程 SkipReasonLoop=true（不自行跑 reasonStep，故不触碰 parking LLM）；
// 只有 ActionSpawn 派生的子进程（childOpts 无 SkipReasonLoop）会跑 reasonStep → Read → park。
func driveActionSpawnAndCaptureChild(
	t *testing.T, k *KernelImpl, parent *Process, ctxMgr *rnixctx.Manager, park *spawnGateLLM,
) (child *Process, release func()) {
	t.Helper()
	cid := parent.CtxID

	// 预填 assistant.tool_calls，使 executeMetaAction 收尾时的 appendToolResult 能配对
	// （避免 dangling tool result 破坏上下文）。
	if err := ctxMgr.AppendAssistantWithToolCalls(cid, "spawning child", "", nil, []rnixctx.ToolCall{
		{ID: "sp-1", Name: "Agent", Input: map[string]any{"intent": "run a shell task"}},
	}); err != nil {
		t.Fatalf("pre-fill assistant+tool_calls: %v", err)
	}

	mapping := toolMapping{Type: "meta", Action: ActionSpawn}
	tc := llmToolCall{ID: "sp-1", Name: "Agent", Input: map[string]any{"intent": "run a shell task"}}
	resp := llmResponse{Content: "spawning child"}
	var counter errFingerprintCounter
	seen := map[string]bool{}
	prompt := &rnixctx.PromptResult{}

	done := make(chan struct{})
	go func() {
		defer close(done)
		// executeMetaAction 在 WaitChildInReason 同步阻塞，直到子进程退出（被 release 放行后）。
		k.executeMetaAction(parent, tc, mapping, 1, time.Now(), &counter, seen, prompt, "", &resp)
	}()

	select {
	case <-park.reached:
		// 子进程已停在首次 LLM Read：Running、在 procTable、AllowedDevices 已定。
	case <-done:
		t.Fatal("executeMetaAction returned before child parked — 子进程未能 spawn 或未进入 reasonStep")
	case <-time.After(3 * time.Second):
		t.Fatal("timeout 等待子进程到达 LLM Read（park gate 未触发）")
	}

	// release 放行子进程并等待 executeMetaAction 收尾（带超时）。提前定义，使下面
	// 的失败路径也能 join 后台 goroutine，避免泄漏到后续测试（code-review P6）。
	release = func() {
		close(park.release)
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("executeMetaAction 在 release 后未返回（WaitChildInReason 卡住）")
		}
	}

	children := parent.GetChildren()
	if len(children) != 1 {
		release()
		t.Fatalf("expected exactly 1 child, got %d", len(children))
	}
	c, ok := k.GetProcess(children[0])
	if !ok {
		release()
		t.Fatalf("child PID %d not found in procTable", children[0])
	}
	return c, release
}

// spawnRestrictedParent 创建一个不自跑 reasonStep 的父进程，模拟纯编排 orchestrator
// （apply 顶层 spawn 只给编排设备）。返回的父进程已初始化 CtxID。
func spawnRestrictedParent(t *testing.T, k *KernelImpl, allowed []string) *Process {
	t.Helper()
	ppid, err := k.Spawn("orchestrator", nil, SpawnOpts{
		AllowedDevices: allowed,
		SkipReasonLoop: true,
	})
	if err != nil {
		t.Fatalf("spawn parent: %v", err)
	}
	parent, ok := k.GetProcess(ppid)
	if !ok {
		t.Fatal("parent process not found")
	}
	if !slices.Equal(parent.AllowedDevices, allowed) {
		t.Fatalf("setup: parent.AllowedDevices = %v, want %v", parent.AllowedDevices, allowed)
	}
	return parent
}

// ───────────────────── Layer A：helper 单元测试（编译 RED + 行为 RED）─────────────────────

// 010 — stripOrchestrationDevices 纯函数矩阵（AC1 剔除 + AC3 保留真实设备）。
//
// 编译 RED：stripOrchestrationDevices / orchestrationOnlyDevices 尚不存在 → 整个 kernel
// 包 build 失败。临时 stub `return devs` 下：剔除类用例行为 RED，保留类用例 green-guard。
func TestATDD_37_5_010_StripOrchestrationDevices_Matrix(t *testing.T) {
	// 守护单一真相源：orchestrationOnlyDevices 至少包含 /dev/intent（决定性死锁设备）。
	if !slices.Contains(orchestrationOnlyDevices, "/dev/intent") {
		t.Fatalf("orchestrationOnlyDevices = %v, must contain /dev/intent", orchestrationOnlyDevices)
	}

	tests := []struct {
		name string
		in   []string
		want []string
	}{
		// AC1：纯编排父 → 剔除后为空 → nil（触发 spawn.go fail-open 分支）。
		{"orchestration_only_to_nil", []string{"/dev/intent"}, nil},
		// AC3：父持真实执行设备 → 保留真实设备，仅剔除编排设备。
		{"keeps_real_drops_orchestration", []string{"/dev/fs", "/dev/intent"}, []string{"/dev/fs"}},
		{"keeps_multiple_real", []string{"/dev/intent", "/dev/fs", "/dev/shell"}, []string{"/dev/fs", "/dev/shell"}},
		// 边界：空输入保持空（fail-open 父无变化）。
		{"nil_stays_nil", nil, nil},
		{"empty_stays_empty", []string{}, nil},
		// 边界：无编排设备 → 原样返回（不误伤普通约束）。
		{"no_orchestration_unchanged", []string{"/dev/fs"}, []string{"/dev/fs"}},
		// 边界：重复编排设备全部剔除。
		{"duplicate_orchestration_all_stripped", []string{"/dev/intent", "/dev/intent"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripOrchestrationDevices(tt.in)
			if !slices.Equal(got, tt.want) {
				t.Errorf("stripOrchestrationDevices(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// 011 — helper 不得原地修改入参（父进程 AllowedDevices 不能被副作用篡改）。
func TestATDD_37_5_011_StripOrchestrationDevices_DoesNotMutateInput(t *testing.T) {
	in := []string{"/dev/fs", "/dev/intent"}
	orig := slices.Clone(in)
	_ = stripOrchestrationDevices(in)
	if !slices.Equal(in, orig) {
		t.Errorf("stripOrchestrationDevices mutated its input: got %v, want unchanged %v", in, orig)
	}
}

// ───────────────────── Layer B：ActionSpawn 端到端（行为 RED，AC5）─────────────────────

// 020 — 纯编排父（['/dev/intent']）经 ActionSpawn → 子进程 AllowedDevices 不含 /dev/intent
// 且为空（fail-open），从而放行 /dev/shell（AC1 + AC5 主断言）。
//
// 行为 RED：当前 tool_exec.go:789 原样继承 → 子 AllowedDevices == ['/dev/intent'] → 断言失败。
func TestATDD_37_5_020_ActionSpawn_StripsOrchestrationOnlyDevice(t *testing.T) {
	park := newSpawnGate()
	k, ctxMgr := newDeviceInheritanceKernel(t, park)
	parent := spawnRestrictedParent(t, k, []string{"/dev/intent"})

	child, release := driveActionSpawnAndCaptureChild(t, k, parent, ctxMgr, park)
	defer release()

	if slices.Contains(child.AllowedDevices, "/dev/intent") {
		t.Errorf("AC1: child.AllowedDevices = %v, 必须剔除纯编排设备 /dev/intent", child.AllowedDevices)
	}
	// 剔除后为空 → fail-open，子进程方能调用 /dev/shell（不再 unknown tool /dev/shell）。
	if len(child.AllowedDevices) != 0 {
		t.Errorf("AC5: child.AllowedDevices = %v, want empty/nil（fail-open 放行 /dev/shell）", child.AllowedDevices)
	}
}

// 021 — 父持真实设备（['/dev/fs','/dev/intent']）经 ActionSpawn → 子 == ['/dev/fs']
// （AC3 防误伤 + AC5 第二断言）。
//
// 行为 RED：当前原样继承 → 子 == ['/dev/fs','/dev/intent'] → 断言失败。
func TestATDD_37_5_021_ActionSpawn_PreservesRealDeviceDropsOrchestration(t *testing.T) {
	park := newSpawnGate()
	k, ctxMgr := newDeviceInheritanceKernel(t, park)
	parent := spawnRestrictedParent(t, k, []string{"/dev/fs", "/dev/intent"})

	child, release := driveActionSpawnAndCaptureChild(t, k, parent, ctxMgr, park)
	defer release()

	want := []string{"/dev/fs"}
	if !slices.Equal(child.AllowedDevices, want) {
		t.Errorf("AC3: child.AllowedDevices = %v, want %v（保留真实设备，仅剔除 /dev/intent）",
			child.AllowedDevices, want)
	}
}

// 022 — ActionSpawn 子进程 DeniedDevices 含 /dev/intent（AC2，防递归编排），
// 与 cmd/rnix/main.go SpawnFunc 对称。
//
// 行为 RED：当前 ActionSpawn 仅原样拷贝父 DeniedDevices（tool_exec.go:790），
// 父为空时子 DeniedDevices 不含 /dev/intent → 断言失败。
func TestATDD_37_5_022_ActionSpawn_ChildDeniesIntentDevice(t *testing.T) {
	park := newSpawnGate()
	k, ctxMgr := newDeviceInheritanceKernel(t, park)
	parent := spawnRestrictedParent(t, k, []string{"/dev/intent"})

	child, release := driveActionSpawnAndCaptureChild(t, k, parent, ctxMgr, park)
	defer release()

	if !slices.Contains(child.DeniedDevices, "/dev/intent") {
		t.Errorf("AC2: child.DeniedDevices = %v, 必须含 /dev/intent（防递归编排）",
			child.DeniedDevices)
	}
}

// 023 — 父原有 DeniedDevices 必须保留（AC2 后半：union 不能丢弃父黑名单）。
//
// 行为 RED：当前原样拷贝父 DeniedDevices 会保留父的 /dev/shell，但不会补 /dev/intent；
// 修复需 union 两者。本用例同时守护「父原有 denied 不丢」+「补 /dev/intent」。
func TestATDD_37_5_023_ActionSpawn_PreservesParentDeniedAndAddsIntent(t *testing.T) {
	park := newSpawnGate()
	k, ctxMgr := newDeviceInheritanceKernel(t, park)
	// 父：真实设备 /dev/fs + 原有 denied /dev/shell。
	ppid, err := k.Spawn("orchestrator", nil, SpawnOpts{
		AllowedDevices: []string{"/dev/fs", "/dev/intent"},
		DeniedDevices:  []string{"/dev/shell"},
		SkipReasonLoop: true,
	})
	if err != nil {
		t.Fatalf("spawn parent: %v", err)
	}
	parent, ok := k.GetProcess(ppid)
	if !ok {
		t.Fatal("parent not found")
	}

	child, release := driveActionSpawnAndCaptureChild(t, k, parent, ctxMgr, park)
	defer release()

	if !slices.Contains(child.DeniedDevices, "/dev/intent") {
		t.Errorf("AC2: child.DeniedDevices = %v, 必须补 /dev/intent", child.DeniedDevices)
	}
	if !slices.Contains(child.DeniedDevices, "/dev/shell") {
		t.Errorf("AC2: child.DeniedDevices = %v, 必须保留父原有 denied /dev/shell", child.DeniedDevices)
	}
}

// 024 — AC5 行为实证：纯编排父 ActionSpawn 的子进程能**实际**放行 /dev/shell 工具调用
// （fail-open 生效，而非仅断言字段为空），且 /dev/intent 被 DeniedDevices 先行拦截。
// 直接驱动 executeVFSTool 走权限判定（tool_exec.go:388 denied 先行 / :397 空白名单 fail-open），
// 补足 020 仅断言 child.AllowedDevices 字段值的覆盖盲区（code-review P1）。
func TestATDD_37_5_024_ActionSpawn_ChildReachesShellBlocksIntent(t *testing.T) {
	park := newSpawnGate()
	k, ctxMgr := newDeviceInheritanceKernel(t, park)
	parent := spawnRestrictedParent(t, k, []string{"/dev/intent"})

	child, release := driveActionSpawnAndCaptureChild(t, k, parent, ctxMgr, park)
	defer release()

	// 放行：子 AllowedDevices 为空（fail-open）→ /dev/shell 不应被权限拦截。
	// 可能因非权限原因失败，但绝不能是 "permission denied"（参考 spawn_recursion_test.go）。
	_, shellErr := k.executeVFSTool(child,
		llmToolCall{Name: "Bash", Input: map[string]any{}},
		toolMapping{Type: "vfs", VFSPath: "/dev/shell"})
	if shellErr != nil && strings.Contains(shellErr.Error(), "permission denied") {
		t.Errorf("AC5: 子进程应放行 /dev/shell（fail-open），却被权限拦截: %v", shellErr)
	}

	// 拦截：子 DeniedDevices 含 /dev/intent → 必须被黑名单先行拦截（防递归编排）。
	_, intentErr := k.executeVFSTool(child,
		llmToolCall{Name: "intent_decompose", Input: map[string]any{}},
		toolMapping{Type: "vfs", VFSPath: "/dev/intent/decompose"})
	if intentErr == nil || !strings.Contains(intentErr.Error(), "permission denied") {
		t.Errorf("AC2/AC5: 子进程必须拦截 /dev/intent（防递归编排），got err=%v", intentErr)
	}
}

// ───────────────────── Layer C：green-guard 回归（AC4 防误伤）─────────────────────

// 030 — 父无编排设备（['/dev/fs']）经 ActionSpawn → 子 == ['/dev/fs']（不变）。
//
// green-guard：修复前后均应通过（修复前原样继承；修复后 stripOrchestrationDevices(['/dev/fs'])
// == ['/dev/fs']）。守护「修复不削弱普通进程的合法设备约束」。
func TestATDD_37_5_030_ActionSpawn_NonOrchestrationParentUnaffected(t *testing.T) {
	park := newSpawnGate()
	k, ctxMgr := newDeviceInheritanceKernel(t, park)
	parent := spawnRestrictedParent(t, k, []string{"/dev/fs"})

	child, release := driveActionSpawnAndCaptureChild(t, k, parent, ctxMgr, park)
	defer release()

	want := []string{"/dev/fs"}
	if !slices.Equal(child.AllowedDevices, want) {
		t.Errorf("green-guard: child.AllowedDevices = %v, want %v（普通设备约束不应被改动）",
			child.AllowedDevices, want)
	}
}

// 注：AC4 的 depth-guard 回归由现有 kernel/spawn_recursion_test.go 守护
// （TestSpawnRecursion_DepthLimitRejected / _DepthLimitTripsBreakerAt3 /
// TestSpawn_DepthRejectInSpawn）——本 story 不得删除或弱化，故此处不重复实现。
