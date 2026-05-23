package kernel

import (
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// =============================================================================
// ATDD 45.4 — epic AC-EA2 长跑业务端到端 ATDD
//             (dev-auto.ash 类业务跑 1 小时不被自动 suspend)
//
// 本文件独立锚定 epic-45 AC-EA2 line 98 "dev-auto.ash 类长跑业务跑 1 小时不会
// 因 step_timeout 被自动 suspend" 的端到端承诺。当前该 epic 级 AC 仅通过
// `kernel/atdd_45_2_warn_only_test.go::TestATDD_45_2_004_PersistentStalled_ConsecutiveAccumulates`
// 的 5 次连续 scan 在 unit 层覆盖 — 5 × 30s checkInterval = 2.5 分钟模拟，
// 远低于 1 小时承诺。
//
// 本文件 2 个用例补足该层：
//   - 001 — 30 次连续 scan 基线（默认）≈ 15 分钟模拟（30 × 30s checkInterval）
//   - 002 — RNIX_45_4_LONG_RUN_SCANS 环境变量参数化（支持 100+ 次模拟真实 1 小时）
//
// EchoMatrix 联动（epic AC-EA6 跨仓库验证链）：
//   1. EchoMatrix 通过 `rnix daemon status` 查询 daemon_commit 确认 P4 框架版本
//   2. EchoMatrix 跑 dev-auto.ash 主循环（765 min 长跑），父 PID 25 spawn 子任务
//   3. 本文件 002 用例通过 `RNIX_45_4_LONG_RUN_SCANS=120 go test ...` 在 daemon
//      侧模拟 1 小时长跑，作为 daemon-side 官方机器证据
//   4. dashboard 显示 PID 25 stall heatmap 颜色递进（45.5 落地后），用户选择
//      是否手动按 K 干预
//
// 5 次纠偏决策史背景（参考 problem-solution-2026-05-22-heartbeat-verification.md
// §决策演化时间线）：
//   方案 1 (5min step_timeout) → 业务被错杀
//   方案 2 (15min step_timeout) → 仍被错杀
//   方案 3 (HB-1 keeper 续命) → hack 而非 fix
//   方案 4 (SpawnAndWait ticker) → 同样 hack
//   ...
//   方案 8 (P4 daemon-passive) → 收敛 — daemon 任何时间窗口都不做破坏性动作
//
// 本文件用例即"方案 8 P4 收敛"的可机器验证落地证据。
//
// 引用：
//   - _bmad-output/implementation-artifacts/45-4-atdd-rewrite-for-warn-only-semantics.md
//     §AC4 + §Decision D3 (环境变量参数化 vs 编译期常量 vs build tag)
//   - _bmad-output/planning-artifacts/epics/epic-45-heartbeat-subsystem-reform.md
//     §AC-EA2 (line 98) + §AC-EA6 (EchoMatrix 联动)
//   - _bmad-output/problem-solution-2026-05-22-heartbeat-verification.md
//     §决策演化时间线 (5 次纠偏 → 方案 8 P4 收敛)
//   - kernel/heartbeat_monitor_unit_test.go#357-365 newHeartbeatTestKernel (复用)
//   - kernel/atdd_44_3_helpers_test.go#181 drainAllEvents (复用)
//   - kernel/atdd_45_2_warn_only_test.go#51-68 findStalledEventAction (复用)
//   - kernel/atdd_45_2_warn_only_test.go#342-430 TestATDD_45_2_004 (5 次 scan
//     unit 层 baseline；本文件用例扩展到 30+ 次)
// =============================================================================

// longRunScanCount returns the number of scans to run for parameterized tests.
// Default 30 (≈15 minutes simulation at 30s checkInterval). Override via
// RNIX_45_4_LONG_RUN_SCANS env var; values <= 0 or > 200 → t.Skipf.
//
// Upper bound rationale: DebugChan buffer is 256 (kernel/process.go:240) and
// emit is non-blocking with drop-on-full (debug/event.go:12-20). Since the
// scan loop emits without intermediate drain, totalScans must stay safely
// below 256 — 200 gives a 56-event headroom for any out-of-band emits from
// other code paths (e.g. event_writer flushes, recordMgr). Going higher would
// silently drop events and break the `count == totalScans` assertion.
//
// For 1-hour simulation coverage (epic AC-EA2 line 98), 120 × 30s = 60min is
// well within 200. EchoMatrix cross-repo verification scenarios up to ~1.5h
// fit; longer runs require a redesign (drain mid-loop or increase buffer).
func longRunScanCount(t *testing.T) int {
	t.Helper()
	const (
		defaultScans = 30
		// maxScans must stay below DebugChan buffer (256) to avoid silent
		// drop-on-full corrupting the emit-count assertion.
		maxScans = 200
	)
	v := os.Getenv("RNIX_45_4_LONG_RUN_SCANS")
	if v == "" {
		return defaultScans
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		t.Skipf("Story 45.4 AC4: invalid RNIX_45_4_LONG_RUN_SCANS=%q: %v (skipping parameterized run)", v, err)
		return 0
	}
	if n <= 0 || n > maxScans {
		t.Skipf("Story 45.4 AC4: RNIX_45_4_LONG_RUN_SCANS=%d out of bounds (1..%d); skipping parameterized run", n, maxScans)
		return 0
	}
	return n
}

// -----------------------------------------------------------------------------
// AC4 用例 001 — 30 次连续 scan 基线（默认）
// -----------------------------------------------------------------------------

// TestATDD_45_4_LongRun_001_30ScansNeverAutoSuspend
//
// 基线长跑覆盖：30 次连续 scan ≈ 15 分钟模拟（30 × 30s checkInterval）。
// 通过本测试为 epic AC-EA2 "长跑业务跑 1 小时不被自动 suspend" 提供 unit 层
// 之上的 integration 层证据。
//
// 完整断言矩阵：
//   (1) proc.GetState() == StateRunning（每次 scan 后立即断言，任一次失败即 fatal）
//   (2) proc.GetSuspendReason() == ""（30 次 scan 后）
//   (3) hm.stalledProcs[pid] 存在 + ConsecutiveStalls == 30
//   (4) cancelCalled == false（cancelCallback 从未被触发——defense in depth）
//   (5) emit count == 30（每次 scan 一次 emit）
//   (6) 最后一次 emit 的 action == "suspend"（ConsecutiveStalls >= 4 后稳定在 ceiling）
func TestATDD_45_4_LongRun_001_30ScansNeverAutoSuspend(t *testing.T) {
	k := newHeartbeatTestKernel(t)
	proc := NewProcess(0, "test-atdd-45-4-longrun-001", nil)
	proc.mu.Lock()
	proc.State = types.StateRunning
	proc.StepTimeout = 100 * time.Millisecond
	// LastHeartbeat 远过期，30 次 scan 期间不推进 — 模拟"daemon 视角下业务持续
	// 静默"（实际生产中 dev-auto.ash 父进程可能在等子任务长时间执行 LLM call）
	proc.LastHeartbeat = time.Now().Add(-10 * time.Minute)
	proc.mu.Unlock()
	k.procTable.Store(proc.PID, proc)

	// Defense in depth — cancelCallback 设置但不应被触发
	var cancelCalled bool
	var cmu sync.Mutex
	proc.SetStepCancel(func() {
		cmu.Lock()
		cancelCalled = true
		cmu.Unlock()
	})

	hm := NewHeartbeatMonitor(k, 50*time.Millisecond)

	const totalScans = 30

	for i := range totalScans {
		hm.scan()

		// (1) State 每次 scan 后立即断言 — 任一次失败立即 fatal，避免后续 scan
		// 在错误状态上累积无效证据
		if got := proc.GetState(); got != types.StateRunning {
			// drain stallRecord 以便错误消息提供诊断信息
			hm.mu.Lock()
			rec := hm.stalledProcs[proc.PID]
			hm.mu.Unlock()
			cs := 0
			if rec != nil {
				cs = rec.ConsecutiveStalls
			}
			t.Fatalf("Story 45.4 AC4.001: scan #%d — State = %s, want StateRunning (epic AC-EA2 长跑业务承诺破坏; ConsecutiveStalls=%d at failure)",
				i+1, got, cs)
		}
	}

	// (2) SuspendReason 始终空
	if reason := proc.GetSuspendReason(); reason != "" {
		t.Errorf("Story 45.4 AC4.001: SuspendReason after %d scans = %q, want \"\" (P4: daemon 永不写 heartbeat_timeout)",
			totalScans, reason)
	}

	// (3) stallRecord 仍存在 + ConsecutiveStalls 准确累积
	hm.mu.Lock()
	rec, exists := hm.stalledProcs[proc.PID]
	cs := 0
	if exists {
		cs = rec.ConsecutiveStalls
	}
	hm.mu.Unlock()
	if !exists {
		t.Fatal("Story 45.4 AC4.001: stallRecord MUST persist through all 30 scans (passive mode 永不 delete)")
	}
	if cs != totalScans {
		t.Errorf("Story 45.4 AC4.001: ConsecutiveStalls after %d scans = %d, want %d (累积无 reset)",
			totalScans, cs, totalScans)
	}

	// (4) cancelCallback 从未被触发
	cmu.Lock()
	called := cancelCalled
	cmu.Unlock()
	if called {
		t.Error("Story 45.4 AC4.001: cancelCallback MUST NOT fire during 30-scan long-run (passive mode forbids CancelStep)")
	}

	// (5)(6) emit count == 30 + 最后一次 action == "suspend"
	events := drainAllEvents(t, proc, 500*time.Millisecond)
	action, count, found := findStalledEventAction(events)
	if !found {
		t.Fatal("Story 45.4 AC4.001: expected at least one ProcessStalled emit, got none")
	}
	if count != totalScans {
		t.Errorf("Story 45.4 AC4.001: ProcessStalled emit count = %d, want %d (one per scan)",
			count, totalScans)
	}
	if action != "suspend" {
		t.Errorf("Story 45.4 AC4.001: last ProcessStalled.action = %q, want \"suspend\" (ConsecutiveStalls >= 4 之后 action 稳定在 ceiling)",
			action)
	}

	t.Logf("Story 45.4 AC4.001: %d scans completed; ConsecutiveStalls=%d; emit count=%d; final action=%q; cancelCalled=%v; SuspendReason=%q (≈%.1f min simulation at 30s checkInterval)",
		totalScans, cs, count, action, called, proc.GetSuspendReason(), float64(totalScans)*30.0/60.0)
}

// -----------------------------------------------------------------------------
// AC4 用例 002 — 可参数化覆盖到 1 小时（RNIX_45_4_LONG_RUN_SCANS env var）
// -----------------------------------------------------------------------------

// TestATDD_45_4_LongRun_002_ParameterizedLongRunViaEnvVar
//
// 同 001 但 scan 次数由 RNIX_45_4_LONG_RUN_SCANS 环境变量控制（默认 30，CI 可
// 设 100/120 等覆盖真实 50-60 分钟模拟）。
//
// 使用示例：
//   RNIX_45_4_LONG_RUN_SCANS=120 go test -race -count=1 -run TestATDD_45_4_LongRun_002 ./kernel/...
//   → 120 × 30s checkInterval = 60 分钟模拟（epic AC-EA2 真实 1 小时承诺）
//
// 设计决策（参考 spec §Decision D3）：
//   - 环境变量 vs 编译期常量：环境变量可在不重新编译测试二进制的情况下切换两种
//     模式；编译期常量需要 CI / 手工 verify 重新编译
//   - 环境变量 vs build tag：build tag 适合"切换不同代码路径"，本场景仅是"切换
//     循环次数"——过度工程
//   - 默认安全：未设环境变量时使用 default 30 次（同 001），CI 默认通过
//   - 边界值防护：解析失败 / <= 0 / > 1000 → t.Skipf，防止恶意大值阻塞 CI
func TestATDD_45_4_LongRun_002_ParameterizedLongRunViaEnvVar(t *testing.T) {
	totalScans := longRunScanCount(t)
	if totalScans == 0 {
		return // Skipped by longRunScanCount; defensive — t.Skipf already returned
	}

	k := newHeartbeatTestKernel(t)
	proc := NewProcess(0, "test-atdd-45-4-longrun-002", nil)
	proc.mu.Lock()
	proc.State = types.StateRunning
	proc.StepTimeout = 100 * time.Millisecond
	proc.LastHeartbeat = time.Now().Add(-10 * time.Minute)
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

	for i := range totalScans {
		hm.scan()
		if got := proc.GetState(); got != types.StateRunning {
			hm.mu.Lock()
			rec := hm.stalledProcs[proc.PID]
			hm.mu.Unlock()
			cs := 0
			if rec != nil {
				cs = rec.ConsecutiveStalls
			}
			t.Fatalf("Story 45.4 AC4.002: scan #%d/%d — State = %s, want StateRunning (epic AC-EA2 长跑业务承诺破坏; ConsecutiveStalls=%d)",
				i+1, totalScans, got, cs)
		}
	}

	if reason := proc.GetSuspendReason(); reason != "" {
		t.Errorf("Story 45.4 AC4.002: SuspendReason after %d scans = %q, want \"\"",
			totalScans, reason)
	}

	hm.mu.Lock()
	rec, exists := hm.stalledProcs[proc.PID]
	cs := 0
	if exists {
		cs = rec.ConsecutiveStalls
	}
	hm.mu.Unlock()
	if !exists {
		t.Fatal("Story 45.4 AC4.002: stallRecord MUST persist through long-run scans")
	}
	if cs != totalScans {
		t.Errorf("Story 45.4 AC4.002: ConsecutiveStalls after %d scans = %d, want %d", totalScans, cs, totalScans)
	}

	cmu.Lock()
	called := cancelCalled
	cmu.Unlock()
	if called {
		t.Error("Story 45.4 AC4.002: cancelCallback MUST NOT fire during long-run")
	}

	// Drain window — DebugChan buffer is 256 (kernel/process.go:240) and emit
	// is non-blocking + drop-on-full (debug/event.go:12-20). The scan loop
	// emits without intermediate drain, so totalScans must stay below 256 to
	// avoid silent drops corrupting the emit-count assertion. longRunScanCount
	// caps the env-var input at 200, giving a 56-event headroom for any
	// out-of-band emits from other code paths.
	//
	// The drain window scales linearly with scan count to give drainAllEvents
	// enough idle-tick budget to read all queued events; once the channel
	// empties, drainAllEvents returns immediately on the next deadline tick.
	drainWindow := time.Duration(50+totalScans*2) * time.Millisecond
	events := drainAllEvents(t, proc, drainWindow)
	action, count, found := findStalledEventAction(events)
	if !found {
		t.Fatal("Story 45.4 AC4.002: expected at least one ProcessStalled emit")
	}
	if count != totalScans {
		t.Errorf("Story 45.4 AC4.002: ProcessStalled emit count = %d, want %d (one per scan)",
			count, totalScans)
	}
	if action != "suspend" {
		t.Errorf("Story 45.4 AC4.002: last ProcessStalled.action = %q, want \"suspend\"", action)
	}

	t.Logf("Story 45.4 AC4.002: parameterized run completed — totalScans=%d; ConsecutiveStalls=%d; emit count=%d; final action=%q; cancelCalled=%v; SuspendReason=%q (≈%.1f min simulation at 30s checkInterval; set RNIX_45_4_LONG_RUN_SCANS=120 for full 1-hour coverage)",
		totalScans, cs, count, action, called, proc.GetSuspendReason(), float64(totalScans)*30.0/60.0)
}
