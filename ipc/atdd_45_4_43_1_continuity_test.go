package ipc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
)

// =============================================================================
// ATDD 45.4 — Story 43.1 P4 等价语义守护
//             (HB-1 keeper 删除后 script-runner 活性信号真诚化)
//
// 本文件锚定 Story 43.1（heartbeat-lifecycle coverage）原 AC1-AC5 在 epic-45
// P4 daemon-passive 框架 + Story 45.3（HB-1 keeper 删除）落地后的等价断言。
// Story 43.1 原 AC 关键守护项：
//   AC1 — script-runner 通过 HB-1 keeper 每 10s 推进 LastHeartbeat
//   AC2 — keeper 启动/停止生命周期由 handleExecScript 管理
//   AC3 — 不影响 SpawnAndWait 现有行为
//   AC4 — keeper 退出时清理资源
//   AC5 — keeper 引用进程对象时取强引用，不让 GC 提前回收
//
// 在 epic-45 P4 框架下，43.1 上述承诺**完全推翻**——HB-1 keeper 本身被 45.3
// 删除（D1 决策：5 个原 keeper 测试都断言 keeper 工作正确，符号删除后无等价
// 语义可改写）。本文件锚定 43.1 原 AC 在 P4 框架下的**等价正面语义**：
//   - AC1 等价：script-runner 活性信号由 events.jsonl 流密度天然提供
//             （`AttachEventWriter` + `EmitScriptEvent` 路径），不再依赖 daemon
//             代替业务进程伪造心跳
//   - "P4 修复 HB-1 hack 性质" 正面证据：HB-1 删除后，**真挂死可被 HeartbeatMonitor
//             检测** —— problem-solution-2026-05-22 line 345 "HB-1 是 hack
//             而非 fix"论断的可机器验证证据
//   - AC2/AC3 等价 + epic line 69 "step_timeout 保留作为 dashboard 显示信息"：
//             step_timeout 字段在 P4 下保留可见作为 dashboard 数据源，未被
//             错误地彻底删除
//
// 与 ipc/atdd_45_3_no_hb1_test.go 的关系：
//   - 45.3 ATDD 聚焦"HB-1 符号删除 + daemon 不再推进 LastHeartbeat"（符号 + 不变量）
//   - 本文件 ATDD 聚焦"43.1 原 AC 的等价正面承诺 + epic line 69 step_timeout 保留"
//   - 双层覆盖："删除证据 + 等价行为" 双向防护
//
// 引用：
//   - _bmad-output/implementation-artifacts/45-4-atdd-rewrite-for-warn-only-semantics.md
//     §AC3 + §Decision D2 (helper 新建 vs 复用 — 本文件新建独立 helper)
//   - _bmad-output/implementation-artifacts/43-1-heartbeat-lifecycle-coverage.md
//     §Acceptance-Criteria AC1-AC5
//   - _bmad-output/planning-artifacts/epics/epic-45-heartbeat-subsystem-reform.md
//     §"不在本 Epic 范围内" line 69 (step_timeout 字段保留)
//     §Background line 50-53 (HB-1 是 hack 而非 fix)
//   - _bmad-output/problem-solution-2026-05-22-heartbeat-verification.md
//     line 343-346 ("HB-1 是 hack 而非 fix 的证据")
//   - kernel/observe.go#77-97 EmitScriptEvent (script-runner 活性信号入口)
//   - kernel/process.go#789-793 TouchHeartbeat (合法 writer 之一)
//   - kernel/process.go#808-817 AttachEventWriter
//   - kernel/event_writer.go#52-66 NewEventWriter
//   - ipc/protocol.go#213-214 ProcInfoWire.StepTimeoutMs (dashboard 字段)
//   - ipc/atdd_45_3_no_hb1_test.go#173-187 newSkipReasonLoopProcForATDD45_3 (参考模式)
// =============================================================================

// newSkipReasonLoopProcForATDD45_4 spawns a SkipReasonLoop process via
// kernel.Spawn（命中 spawn.go:466 / :692 / :717 三处 SkipReasonLoop 分支）
// + 准备 stepDataDir 以便 AttachEventWriter / events.jsonl 写盘。
//
// D2 决策（参考 spec line 362-371）：本 helper **新建** 不复用 45.3 同名
// helper，避免跨 ATDD 文件耦合。45.3 helper 用途是"验证 LastHeartbeat 不被
// 自我推进"（无需 stepDataDir）；本 helper 额外提供 stepDataDir 以验证
// events.jsonl 流密度作为活性信号源。
//
// 返回 stepDataDir 让调用方拼接 events.jsonl 路径做断言。
func newSkipReasonLoopProcForATDD45_4(t *testing.T) (*kernel.KernelImpl, *kernel.Process, string) {
	t.Helper()
	stepDataDir := t.TempDir()

	kern := kernel.NewKernel(nil, rnixctx.NewManager(), nil)
	kern.SetStepDataDir(stepDataDir)
	t.Cleanup(func() { kern.Shutdown() })

	pid, err := kern.Spawn("run: atdd-45-4.ash", nil, kernel.SpawnOpts{SkipReasonLoop: true})
	if err != nil {
		t.Fatalf("Story 45.4 AC3: Spawn(SkipReasonLoop:true) failed: %v", err)
	}
	proc, ok := kern.GetProcess(pid)
	if !ok {
		t.Fatalf("Story 45.4 AC3: GetProcess(%d) vanished after Spawn", pid)
	}
	return kern, proc, stepDataDir
}

// -----------------------------------------------------------------------------
// AC3 用例 001 — 活性信号由 events.jsonl 流密度天然提供
// -----------------------------------------------------------------------------

// TestATDD_45_4_43_1_001_ScriptRunnerActivityViaEventsJSONLOnly
//
// 守护 Story 43.1 AC1 在 P4 + 45.3 框架下的等价语义："script-runner 通过 HB-1
// keeper 每 10s 推进 LastHeartbeat" 已被 "活性由 events.jsonl 流密度天然提供 —
// HeartbeatMonitor 看到 LastHeartbeat 停滞 emit warn-only ProcessStalled，
// dashboard 通过 events 流密度 + 45.5 heatmap UI 给出真实活性可视化" 替代。
//
// 关键不变量：EmitScriptEvent / events.jsonl 写盘 **不推进** LastHeartbeat —
// 这是 P4 下 events 流密度作为"侧道"活性信号的设计：dashboard 通过 events
// density 推断活性，而不是把活性信号收纳到 LastHeartbeat 字段。
//
// 这与 Story 30.5 字段语义一致：LastHeartbeat 的合法 writer 仅来自 reasonStep /
// driver streaming / Spawn / Resume / load_suspended — events.jsonl 写盘
// （来自 script-runner / SkipReasonLoop 路径）不在 writer 列表里。
func TestATDD_45_4_43_1_001_ScriptRunnerActivityViaEventsJSONLOnly(t *testing.T) {
	kern, proc, stepDataDir := newSkipReasonLoopProcForATDD45_4(t)

	// Attach EventWriter — script-runner 路径不会自动 init writer（不像 reasonStep
	// 在 reason.go 自我 init），由 handleExecScript 显式 attach（参考
	// ipc/atdd_43_2_script_trace_test.go:87-93 同款模式）。
	ew, err := kernel.NewEventWriter(stepDataDir, proc.UUID)
	if err != nil {
		t.Fatalf("Story 45.4 AC3.001: NewEventWriter: %v", err)
	}
	proc.AttachEventWriter(ew)
	t.Cleanup(func() { _ = ew.Close() })

	// 记录初始 LastHeartbeat（spawn-time seed，30.5 AC#1 合法 writer）。
	initialHB := proc.LastHeartbeatSnapshot()
	if initialHB.IsZero() {
		t.Fatal("Story 45.4 AC3.001: Spawn did not seed LastHeartbeat (30.5 AC#1 precondition broken)")
	}

	// 通过 EmitScriptEvent 触发一条 events.jsonl 写盘 — 模拟 script-runner
	// 执行 while/spawn 时的 OnEvent 回调路径（参考 atdd_43_2_script_trace_test.go
	// runScriptRunnerLikeHandleExecScript:107）。
	kern.EmitScriptEvent(proc, "ScriptStmtBegin", map[string]any{
		"line":      1,
		"stmt_kind": "while",
		"intent":    "atdd-45-4-43-1-001",
	})

	// 等一个最小窗口让 EventWriter flush 落盘。EmitScriptEvent → emitEvent →
	// EventWriter.WriteEvent 内部已 Flush，但跨 goroutine 写仍需保险。
	time.Sleep(50 * time.Millisecond)

	// (1) events.jsonl 必须存在且包含此事件
	eventsPath := filepath.Join(stepDataDir, "data", "steps", proc.UUID, "events.jsonl")
	rows, err := kernel.ReadAllEvents(eventsPath)
	if err != nil {
		t.Fatalf("Story 45.4 AC3.001: ReadAllEvents(%s): %v (P4 下 events.jsonl 是 script-runner 活性信号源)", eventsPath, err)
	}
	var found bool
	for _, r := range rows {
		if r.Syscall == "ScriptStmtBegin" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Story 45.4 AC3.001: events.jsonl missing ScriptStmtBegin (%d rows total) — events 流密度作为活性信号的设计被破坏", len(rows))
	}

	// (2) LastHeartbeat **必须未被 EmitScriptEvent / events.jsonl 写盘推进**
	// — P4 设计哲学：events 流密度是侧道信号，不收纳到 LastHeartbeat 字段
	currentHB := proc.LastHeartbeatSnapshot()
	if !currentHB.Equal(initialHB) {
		t.Errorf("Story 45.4 AC3.001: LastHeartbeat advanced by EmitScriptEvent: initial=%v current=%v delta=%v — P4 violation: events.jsonl write must NOT bridge to LastHeartbeat (events 流密度是侧道信号源，与 30.5 字段合法 writer 列表正交)",
			initialHB, currentHB, currentHB.Sub(initialHB))
	}
}

// -----------------------------------------------------------------------------
// AC3 用例 002 — HB-1 删除后真挂死可被 HeartbeatMonitor 检测
// -----------------------------------------------------------------------------

// TestATDD_45_4_43_1_002_GenuineDeadlockNoLongerMaskedByFakeHeartbeat
//
// "HB-1 是 hack 而非 fix" 的正面机器证据（参考 problem-solution-2026-05-22
// line 343-346）：Story 43.1 时 HB-1 keeper 让 LastHeartbeat 每 10s 被假更新 →
// "父进程真挂死时 ticker 仍每 10s 打卡，永远检测不出来"。
//
// 本测试锚定 P4 框架下的等价正面承诺：HB-1 删除后真挂死可被 HeartbeatMonitor
// 检测并 emit warn 信号 — 这是 epic-45 §Background line 50-53 "HB-1 是 hack
// 而非 fix" 论断的可机器验证证据。
//
// 模拟"父进程真挂死"：spawn SkipReasonLoop 进程（无 reasonStep 自动推进心跳，
// 也不调用 TouchHeartbeat），让 LastHeartbeat 自然过期 → HeartbeatMonitor.scan
// 应 emit ProcessStalled with action="warn"，证明假心跳路径已断、daemon 看到
// 真实静默状态。
func TestATDD_45_4_43_1_002_GenuineDeadlockNoLongerMaskedByFakeHeartbeat(t *testing.T) {
	// 自构 spawn 流程：需要设置 StepTimeout=100ms 启用 stall 检测，
	// 与 helper 默认 spawn 不同（helper 不设 StepTimeout）。
	stepDataDir := t.TempDir()
	kern := kernel.NewKernel(nil, rnixctx.NewManager(), nil)
	kern.SetStepDataDir(stepDataDir)
	t.Cleanup(func() { kern.Shutdown() })

	// StepTimeout=100ms — 让 sleep 300ms 后 stalledDuration (~300ms) > 100ms。
	pid, err := kern.Spawn("run: atdd-45-4-43-1-002.ash", nil, kernel.SpawnOpts{
		SkipReasonLoop: true,
		StepTimeout:    100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Story 45.4 AC3.002: Spawn(SkipReasonLoop+StepTimeout=100ms): %v", err)
	}
	proc, ok := kern.GetProcess(pid)
	if !ok {
		t.Fatalf("Story 45.4 AC3.002: GetProcess vanished after Spawn")
	}

	// 模拟"父进程真挂死"：不调用任何 TouchHeartbeat 路径，让 LastHeartbeat
	// 自然过期。Sleep 300ms — 远长于 StepTimeout 100ms。
	//
	// 关键：若 HB-1 keeper 仍存在，会在此期间每 10s 假更新 LastHeartbeat（虽然
	// 300ms 内 ticker 还来不及 fire，但 keeper 一旦存在 SpawnAndWait 也会注入
	// 同样的伪心跳路径——本测试通过 SkipReasonLoop 避开了 handleExecScript /
	// SpawnAndWait 启动 keeper 的入口，与 45.3 ATDD 共同守护"daemon 不再有任何
	// 路径主动推进 LastHeartbeat"不变量）。
	time.Sleep(300 * time.Millisecond)

	// 启动 HeartbeatMonitor 通过 ticker 触发 scan — `scan()` 是 unexported，
	// ipc 包外只能通过 Start/Stop + checkInterval ticker 间接触发（kernel 包内
	// 单测如 heartbeat_monitor_unit_test.go 直接调 hm.scan(); 跨包测试只能走
	// Start ticker 路径，这与生产 daemon 启动 HeartbeatMonitor 的真实路径完全
	// 一致——即同时也验证了 Start/Stop 生命周期不退步）。
	hm := kernel.NewHeartbeatMonitor(kern, 50*time.Millisecond)
	hm.Start()
	defer hm.Stop()

	// (1) Drain events 寻找 ProcessStalled with action="warn"
	// 等 ticker fire 一次 (50ms checkInterval) + buffer
	deadline := time.After(500 * time.Millisecond)
	var stalledFound bool
	var stalledAction string
collectLoop:
	for {
		select {
		case ev := <-proc.DebugChan:
			if ev.Syscall != "HeartbeatMonitor" {
				continue
			}
			if name, _ := ev.Args["event"].(string); name == "ProcessStalled" {
				stalledFound = true
				if a, _ := ev.Args["action"].(string); a != "" {
					stalledAction = a
				}
				break collectLoop
			}
		case <-deadline:
			break collectLoop
		}
	}

	if !stalledFound {
		t.Error("Story 45.4 AC3.002: expected ProcessStalled emit after sleep > StepTimeout — P4 violation: HB-1 删除后真挂死必须可被 HeartbeatMonitor 检测，否则 epic-45 §Background line 50-53 \"HB-1 是 hack 而非 fix\" 论断无机器验证")
	}
	if stalledAction != "warn" {
		t.Errorf("Story 45.4 AC3.002: first ProcessStalled.action = %q, want \"warn\" (Level 1 — ConsecutiveStalls=1 触发 warn 默认分支)", stalledAction)
	}

	// (2) State 必须仍是 Running — passive mode 不破坏进程
	if got := proc.GetState(); got != types.StateRunning {
		t.Errorf("Story 45.4 AC3.002: process State after stall scan = %s, want StateRunning (passive mode 不应自动 Suspend)", got)
	}
}

// -----------------------------------------------------------------------------
// AC3 用例 003 — step_timeout 字段保留作为 dashboard 数据源 (wire schema 不变)
// -----------------------------------------------------------------------------

// TestATDD_45_4_43_1_003_StepTimeoutFieldStillUsedForDashboard
//
// 守护 epic-45 §"不在本 Epic 范围内" line 69 "step_timeout 保留作为 dashboard
// 显示信息，不再触发动作；字段彻底移除留待后续" — 本 ATDD 锚定 step_timeout
// 字段在 P4 下保留可见作为 dashboard 数据源，未被错误地彻底删除。
//
// 验证路径：
//   (1) Spawn 一个进程并设置 StepTimeout=8min
//   (2) 通过 proc.StepTimeout 直接访问失败（包外不可见），改为通过
//       proc.LastHeartbeatSnapshot 不能验证 timeout — 改为读 ipc/protocol.go
//       源码确认 ProcInfoWire.StepTimeoutMs json tag 仍存在
//   (3) 验证 ipc/protocol.go:254 ProcInfoToWire 仍把 StepTimeout 转 wire
//       (`StepTimeoutMs: p.StepTimeout.Milliseconds()`)
func TestATDD_45_4_43_1_003_StepTimeoutFieldStillUsedForDashboard(t *testing.T) {
	// (1) Spawn — 验证 SpawnOpts.StepTimeout 透传完成（30.5 AC#3 沿用）。
	// 这里仅用于编译契约：若 SpawnOpts.StepTimeout 字段被错误删除，Spawn 调用
	// 失败 → 本测试不能编译 → CI 立即可见。
	stepDataDir := t.TempDir()
	kern := kernel.NewKernel(nil, rnixctx.NewManager(), nil)
	kern.SetStepDataDir(stepDataDir)
	t.Cleanup(func() { kern.Shutdown() })

	pid, err := kern.Spawn("run: atdd-45-4-43-1-003.ash", nil, kernel.SpawnOpts{
		SkipReasonLoop: true,
		StepTimeout:    8 * time.Minute,
	})
	if err != nil {
		t.Fatalf("Story 45.4 AC3.003: Spawn(SkipReasonLoop+StepTimeout=8m): %v", err)
	}
	_, ok := kern.GetProcess(pid)
	if !ok {
		t.Fatal("Story 45.4 AC3.003: GetProcess vanished after Spawn")
	}

	// (2) Verify wire schema field still exists in ipc/protocol.go
	// (epic-45 §不在本 Epic 范围内 line 69 — step_timeout 字段彻底删除留待后续)
	protocolPath := "protocol.go" // 测试 cwd 为 ipc 包目录
	body, err := os.ReadFile(protocolPath)
	if err != nil {
		t.Fatalf("Story 45.4 AC3.003: read ipc/protocol.go: %v", err)
	}
	text := string(body)

	// ProcInfoWire 必含 StepTimeoutMs 字段（dashboard 数据源）
	expectedTokens := []string{
		`StepTimeoutMs`,             // Go 字段名
		`"step_timeout_ms,omitempty"`, // wire JSON 字段名
		`p.StepTimeout.Milliseconds()`, // ProcInfoToWire 转换路径
	}
	for _, tok := range expectedTokens {
		if !strings.Contains(text, tok) {
			t.Errorf("Story 45.4 AC3.003: ipc/protocol.go missing dashboard token %q — epic-45 line 69 \"step_timeout 保留作为 dashboard 显示信息\" 退步: 字段彻底删除未在本 epic 范围内",
				tok)
		}
	}
}
