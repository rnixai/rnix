package kernel

import (
	"sync"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// =============================================================================
// ATDD 45.2 — HeartbeatMonitor warn-only refactor (P4 daemon-passive supervision)
//
// Story 45.2 删除 `case "suspend"` / `case "cancel_step"` 分支体内的破坏性动作
// (`hm.kernel.Suspend(pid)` + `SuspendReason="heartbeat_timeout"` 写入
//  + `delete(stalledProcs, pid)` + `proc.CancelStep()`)，
// 保留：case 标签 / emit ProcessStalled 块（含 action 字段）/ ConsecutiveStalls 累积
// / stallRecord.LastActionAt 更新 / Status() 接口 / 4 级 action 映射。
//
// RED-phase 信号：本文件 4 个 TestATDD_45_2_00X 用例在 `heartbeat_monitor.go`
// 改动**之前** 运行必然失败——当前源码仍调用 Suspend(pid) / CancelStep()，新
// 断言（`State == Running` / `cancelCalled == false` / `stallRecord 仍存在`）
// 不能成立。改动落地后转绿。
//
// 引用：
//   - _bmad-output/implementation-artifacts/45-2-heartbeat-monitor-warn-only.md
//     §Acceptance-Criteria AC1-AC4
//   - kernel/heartbeat_monitor.go#149-220 handleStalled（改动主体）
//   - kernel/heartbeat_monitor_unit_test.go#322-329 newHeartbeatTestKernel（复用）
//   - kernel/atdd_44_3_helpers_test.go#181 drainAllEvents（同包跨文件可见，
//     用于断言 DebugChan 上的 ProcessStalled event）
//
// 与既有 unit test 的关系：
//   - `TestHeartbeatMonitor_Level3CancelStep` / `TestHeartbeatMonitor_Level4Suspend`
//     的改写（AC5 / Task 5）与代码改动原子绑定，**留给 dev-story Task 5**——
//     不在本 ATDD 阶段产出。本文件仅承载 4 个**新增**正向断言，与改写后的
//     既有 unit test 形成"正向 + 双重防护"。
//   - `TestHeartbeatMonitor_Level1WarnOnly` 已覆盖 AC3 的等价语义；本文件的
//     `TestATDD_45_2_003_DefaultLevelWarn` 作为 epic-45 P4 哲学锚定再写一份，
//     允许重叠（dev notes 4.4 明确允许）。
// =============================================================================

// findStalledEventAction scans drained DebugChan events and returns the
// `action` value from the most-recent ProcessStalled emit. Returns empty
// string + false if no ProcessStalled event was found in the slice.
//
// This helper is colocated in this file (rather than atdd_44_3_helpers_test.go)
// because it bakes in the Story 45.2 schema assumption: the action field is
// the discriminator that the dashboard / 45.5 stall heatmap downstream
// consume — pinning it here documents the contract surface.
func findStalledEventAction(events []types.SyscallEvent) (string, int, bool) {
	var lastAction string
	count := 0
	for _, ev := range events {
		if ev.Syscall != "HeartbeatMonitor" {
			continue
		}
		evName, _ := ev.Args["event"].(string)
		if evName != "ProcessStalled" {
			continue
		}
		count++
		if action, ok := ev.Args["action"].(string); ok {
			lastAction = action
		}
	}
	return lastAction, count, count > 0
}

// -----------------------------------------------------------------------------
// AC1: case "suspend" 分支体不再 Suspend / 不再写 SuspendReason / 不再 delete stallRecord
// -----------------------------------------------------------------------------

// TestATDD_45_2_001_LevelSuspend_NoStateChange
//
// 预置 ConsecutiveStalls=3 让本次 scan 触发 Level 4 (`>=4`)，断言：
//  1. proc.State == Running       (不再被 hm.kernel.Suspend(pid) 转 Suspended)
//  2. proc.SuspendReason == ""    (不再被写为 "heartbeat_timeout")
//  3. hm.stalledProcs[pid] 仍存在 (不再被 delete)，ConsecutiveStalls == 4
//  4. emit 一次 ProcessStalled event，action == "suspend"
//
// RED 断言：当前 heartbeat_monitor.go line 204-209 仍调 Suspend + delete →
// (1)(2)(3) 必然失败。
func TestATDD_45_2_001_LevelSuspend_NoStateChange(t *testing.T) {
	k := newHeartbeatTestKernel(t)
	proc := NewProcess(0, "test-atdd-45-2-001", nil)
	proc.mu.Lock()
	proc.State = types.StateRunning
	proc.StepTimeout = 100 * time.Millisecond
	proc.LastHeartbeat = time.Now().Add(-500 * time.Millisecond)
	proc.mu.Unlock()
	k.procTable.Store(proc.PID, proc)

	// SetStepCancel ensures we'd also notice an unwanted CancelStep call,
	// though AC1 nominally only forbids Suspend; defense in depth.
	var cancelCalled bool
	var cmu sync.Mutex
	proc.SetStepCancel(func() {
		cmu.Lock()
		cancelCalled = true
		cmu.Unlock()
	})

	hm := NewHeartbeatMonitor(k, 50*time.Millisecond)

	// Pre-seed ConsecutiveStalls=3 so the next scan() crosses the >=4 threshold
	// and resolves action="suspend".
	hm.mu.Lock()
	hm.stalledProcs[proc.PID] = &stallRecord{
		PID:               proc.PID,
		UUID:              proc.UUID,
		ConsecutiveStalls: 3,
		FirstStalledAt:    time.Now().Add(-2 * time.Second),
		LastActionAt:      time.Now().Add(-500 * time.Millisecond),
	}
	hm.mu.Unlock()

	hm.scan()

	// (1) State must remain Running — daemon-passive mode forbids auto-Suspend.
	if got := proc.GetState(); got != types.StateRunning {
		t.Errorf("Story 45.2 AC1: process State after Level 4 scan = %s, want StateRunning (passive mode — Suspend(pid) MUST NOT be called)", got)
	}

	// (2) SuspendReason must NOT be written by HeartbeatMonitor.
	if got := proc.GetSuspendReason(); got != "" {
		t.Errorf("Story 45.2 AC1: SuspendReason after Level 4 scan = %q, want \"\" (passive mode — heartbeat_timeout reason MUST NOT be written)", got)
	}

	// (3) stallRecord MUST persist so that subsequent scans keep accumulating
	// ConsecutiveStalls (data source for the 45.5 stall heatmap).
	hm.mu.Lock()
	rec, exists := hm.stalledProcs[proc.PID]
	cs := 0
	if exists {
		cs = rec.ConsecutiveStalls
	}
	hm.mu.Unlock()
	if !exists {
		t.Error("Story 45.2 AC1: stallRecord MUST persist after Level 4 scan (passive mode — delete(stalledProcs) MUST NOT run)")
	}
	if cs != 4 {
		t.Errorf("Story 45.2 AC1: ConsecutiveStalls after Level 4 scan = %d, want 4 (3 pre-seed + 1 this scan)", cs)
	}

	// (4) emit ProcessStalled with action="suspend" must still fire.
	events := drainAllEvents(t, proc, 100*time.Millisecond)
	action, count, found := findStalledEventAction(events)
	if !found {
		t.Error("Story 45.2 AC1: expected ProcessStalled event emit, got none")
	}
	if action != "suspend" {
		t.Errorf("Story 45.2 AC1: ProcessStalled.action = %q, want \"suspend\" (action map preserves 4-level semantics for dashboard)", action)
	}
	if count != 1 {
		t.Errorf("Story 45.2 AC1: ProcessStalled emit count = %d, want 1 (single scan triggers single emit)", count)
	}

	// Defense in depth: CancelStep MUST NOT have run either at Level 4.
	cmu.Lock()
	called := cancelCalled
	cmu.Unlock()
	if called {
		t.Error("Story 45.2 AC1: CancelStep MUST NOT be called at Level 4 (passive mode)")
	}
}

// -----------------------------------------------------------------------------
// AC2: case "cancel_step" 分支体不再 proc.CancelStep() — 仅保留 LastActionAt + emit
// -----------------------------------------------------------------------------

// TestATDD_45_2_002_LevelCancelStep_NoCancelCall
//
// 预置 ConsecutiveStalls=2 让本次 scan 触发 Level 3 (`>=3 && <4`)，断言：
//  1. proc.SetStepCancel 注入的 cancel 回调**不被调用** (proc.CancelStep() 未 invoke)
//  2. hm.stalledProcs[pid].ConsecutiveStalls == 3
//  3. record.LastActionAt 非零 (保留更新便于 dashboard "上次告警时间")
//  4. emit 一次 ProcessStalled event，action == "cancel_step"
//
// RED 断言：当前 heartbeat_monitor.go line 212 仍调 proc.CancelStep() → (1) 失败。
func TestATDD_45_2_002_LevelCancelStep_NoCancelCall(t *testing.T) {
	k := newHeartbeatTestKernel(t)
	proc := NewProcess(0, "test-atdd-45-2-002", nil)
	proc.mu.Lock()
	proc.State = types.StateRunning
	proc.StepTimeout = 100 * time.Millisecond
	proc.LastHeartbeat = time.Now().Add(-500 * time.Millisecond)
	proc.mu.Unlock()
	k.procTable.Store(proc.PID, proc)

	// Inject step-cancel callback; AC2 asserts it must NOT fire at Level 3.
	var cancelCalled bool
	var cmu sync.Mutex
	proc.SetStepCancel(func() {
		cmu.Lock()
		cancelCalled = true
		cmu.Unlock()
	})

	hm := NewHeartbeatMonitor(k, 50*time.Millisecond)

	// Pre-seed ConsecutiveStalls=2 so the next scan() resolves action="cancel_step".
	hm.mu.Lock()
	hm.stalledProcs[proc.PID] = &stallRecord{
		PID:               proc.PID,
		UUID:              proc.UUID,
		ConsecutiveStalls: 2,
		FirstStalledAt:    time.Now().Add(-1 * time.Second),
	}
	hm.mu.Unlock()

	hm.scan()

	// (1) CancelStep callback MUST NOT be invoked — driver / skill / user-K
	// owns the decision to abort the LLM call, not the daemon.
	cmu.Lock()
	called := cancelCalled
	cmu.Unlock()
	if called {
		t.Error("Story 45.2 AC2: passive mode — CancelStep MUST NOT be called at Level 3 (proc.CancelStep() removed from case \"cancel_step\")")
	}

	// (2)(3) stallRecord state advances normally — LastActionAt updates so
	// dashboard can render "last warned at" timestamps.
	hm.mu.Lock()
	rec, exists := hm.stalledProcs[proc.PID]
	hm.mu.Unlock()
	if !exists {
		t.Fatal("Story 45.2 AC2: stallRecord MUST persist after Level 3 scan")
	}
	if rec.ConsecutiveStalls != 3 {
		t.Errorf("Story 45.2 AC2: ConsecutiveStalls = %d, want 3 (2 pre-seed + 1 this scan)", rec.ConsecutiveStalls)
	}
	if rec.LastActionAt.IsZero() {
		t.Error("Story 45.2 AC2: LastActionAt MUST be set after Level 3 scan (kept for dashboard 'last warned' timestamp)")
	}

	// (4) emit ProcessStalled with action="cancel_step" must still fire.
	events := drainAllEvents(t, proc, 100*time.Millisecond)
	action, count, found := findStalledEventAction(events)
	if !found {
		t.Error("Story 45.2 AC2: expected ProcessStalled event emit, got none")
	}
	if action != "cancel_step" {
		t.Errorf("Story 45.2 AC2: ProcessStalled.action = %q, want \"cancel_step\"", action)
	}
	if count != 1 {
		t.Errorf("Story 45.2 AC2: ProcessStalled emit count = %d, want 1", count)
	}
}

// -----------------------------------------------------------------------------
// AC3: default 分支 (consecutiveStalls < 3) 行为不变 — warn-only
// -----------------------------------------------------------------------------

// TestATDD_45_2_003_DefaultLevelWarn
//
// 首次 scan（无预置 stallRecord），断言：
//  1. hm.stalledProcs[pid].ConsecutiveStalls == 1
//  2. cancelCalled == false (默认分支不做破坏性动作)
//  3. proc.State == Running (整个 epic-45 不再自动 transition)
//  4. emit 一次 ProcessStalled event，action == "warn"
//
// 与既有 TestHeartbeatMonitor_Level1WarnOnly 重叠允许（epic-45 P4 锚定，与
// dev notes Task 4.4 一致）。
//
// RED 信号：此用例在当前源码下也应能 pass — 列入文件是为了形成 4 级语义的
// 完整正向断言矩阵，让 dev-story 完成后 Level 1/3/4 三个动作映射都有同源
// 测试可索引。
func TestATDD_45_2_003_DefaultLevelWarn(t *testing.T) {
	k := newHeartbeatTestKernel(t)
	proc := NewProcess(0, "test-atdd-45-2-003", nil)
	proc.mu.Lock()
	proc.State = types.StateRunning
	proc.StepTimeout = 100 * time.Millisecond
	proc.LastHeartbeat = time.Now().Add(-500 * time.Millisecond)
	proc.mu.Unlock()
	k.procTable.Store(proc.PID, proc)

	var cancelCalled bool
	var cmu sync.Mutex
	proc.SetStepCancel(func() {
		cmu.Lock()
		cancelCalled = true
		cmu.Unlock()
	})

	hm := NewHeartbeatMonitor(k, 50*time.Millisecond)
	hm.scan()

	// (1) ConsecutiveStalls increments to 1 on first detection.
	hm.mu.Lock()
	rec, exists := hm.stalledProcs[proc.PID]
	hm.mu.Unlock()
	if !exists {
		t.Fatal("Story 45.2 AC3: expected stallRecord after first scan")
	}
	if rec.ConsecutiveStalls != 1 {
		t.Errorf("Story 45.2 AC3: ConsecutiveStalls = %d, want 1 (first detection)", rec.ConsecutiveStalls)
	}

	// (2) Default branch is warn-only — no CancelStep invocation.
	cmu.Lock()
	called := cancelCalled
	cmu.Unlock()
	if called {
		t.Error("Story 45.2 AC3: CancelStep MUST NOT be called at default (warn) level")
	}

	// (3) State stays Running.
	if got := proc.GetState(); got != types.StateRunning {
		t.Errorf("Story 45.2 AC3: process State after default scan = %s, want StateRunning", got)
	}

	// (4) emit ProcessStalled with action="warn".
	events := drainAllEvents(t, proc, 100*time.Millisecond)
	action, count, found := findStalledEventAction(events)
	if !found {
		t.Error("Story 45.2 AC3: expected ProcessStalled event emit, got none")
	}
	if action != "warn" {
		t.Errorf("Story 45.2 AC3: ProcessStalled.action = %q, want \"warn\"", action)
	}
	if count != 1 {
		t.Errorf("Story 45.2 AC3: ProcessStalled emit count = %d, want 1", count)
	}
}

// -----------------------------------------------------------------------------
// AC4: 持续 stalled — ConsecutiveStalls 无上限累积，emit 持续触发，State 永不变
// -----------------------------------------------------------------------------

// TestATDD_45_2_004_PersistentStalled_ConsecutiveAccumulates
//
// 连续调用 hm.scan() 5 次（同一 stalled proc，每次 LastHeartbeat 仍远过期），
// 断言：
//  1. hm.stalledProcs[pid].ConsecutiveStalls == 5  (Level 4 后不再被 reset)
//  2. 最后一次 emit 的 action == "suspend"          (>=4 持续映射 suspend)
//  3. proc.State == Running                         (始终未被自动 transition)
//  4. hm.stalledProcs[pid] 仍存在                   (passive mode 不再 delete)
//  5. emit count == 5                                (每次 scan 一次 emit)
//
// RED 断言：当前源码在 scan #4（ConsecutiveStalls=4 → action=suspend）会
// delete(stalledProcs) + Suspend(pid) → scan #5 时 proc.State != Running、
// stallRecord 已被清空 → (1)(3)(4)(5) 必然失败。
func TestATDD_45_2_004_PersistentStalled_ConsecutiveAccumulates(t *testing.T) {
	k := newHeartbeatTestKernel(t)
	proc := NewProcess(0, "test-atdd-45-2-004", nil)
	proc.mu.Lock()
	proc.State = types.StateRunning
	proc.StepTimeout = 100 * time.Millisecond
	// LastHeartbeat 设置一次，5 次 scan 期间都保持远过期 (testing 不依赖
	// 真实时钟推进——scan() 用 time.Since(lastHB) 比较)。
	proc.LastHeartbeat = time.Now().Add(-10 * time.Minute)
	proc.mu.Unlock()
	k.procTable.Store(proc.PID, proc)

	hm := NewHeartbeatMonitor(k, 50*time.Millisecond)

	const totalScans = 5
	for i := range totalScans {
		hm.scan()
		// Sanity: after each scan State stays Running (defense in depth).
		if got := proc.GetState(); got != types.StateRunning {
			t.Fatalf("Story 45.2 AC4: process State after scan #%d = %s, want StateRunning (passive mode forbids auto-Suspend)", i+1, got)
		}
	}

	// (1)(4) ConsecutiveStalls == 5, stallRecord still present.
	hm.mu.Lock()
	rec, exists := hm.stalledProcs[proc.PID]
	hm.mu.Unlock()
	if !exists {
		t.Fatal("Story 45.2 AC4: stallRecord MUST persist after Level 4 scan (passive mode — delete(stalledProcs) removed)")
	}
	if rec.ConsecutiveStalls != totalScans {
		t.Errorf("Story 45.2 AC4: ConsecutiveStalls after %d scans = %d, want %d (no auto-reset after Level 4)",
			totalScans, rec.ConsecutiveStalls, totalScans)
	}

	// (5) Five emits — one per scan. Drain after the loop to avoid racing
	// with the (synchronous) scan() — emitEvent writes are non-blocking
	// (DebugChan buffer = 256) so a single drain post-loop catches all.
	events := drainAllEvents(t, proc, 200*time.Millisecond)
	action, count, found := findStalledEventAction(events)
	if !found {
		t.Fatal("Story 45.2 AC4: expected at least one ProcessStalled event")
	}
	if count != totalScans {
		t.Errorf("Story 45.2 AC4: ProcessStalled emit count = %d, want %d (one per scan)", count, totalScans)
	}

	// (2) After ConsecutiveStalls crosses 4, action stays at "suspend" — the
	// 4-level action map (warn / cancel_step / suspend) keeps "suspend" as
	// the ceiling so dashboard renders a stable stall-intensity signal.
	if action != "suspend" {
		t.Errorf("Story 45.2 AC4: last ProcessStalled.action = %q, want \"suspend\" (ceiling action after ConsecutiveStalls>=4)", action)
	}

	// (3) State assertion (post-loop) — passive mode invariant.
	if got := proc.GetState(); got != types.StateRunning {
		t.Errorf("Story 45.2 AC4: process State after %d scans = %s, want StateRunning", totalScans, got)
	}

	// SuspendReason MUST remain empty even after sustained stall — daemon
	// never writes "heartbeat_timeout" to this field anymore.
	if got := proc.GetSuspendReason(); got != "" {
		t.Errorf("Story 45.2 AC4: SuspendReason after sustained stall = %q, want \"\"", got)
	}
}
