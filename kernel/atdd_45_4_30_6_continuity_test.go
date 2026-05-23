package kernel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// =============================================================================
// ATDD 45.4 — Story 30.6 P4 等价语义守护（4 级 escalation + IPC wire + 配置链路）
//
// 本文件锚定 Story 30.6（supervisor heartbeat monitor and auto-recovery）原 AC#4
// 三级恢复策略 + AC#3/#1 配置链路 + AC#10/#11 IPC + CLI 行为 在 epic-45 P4
// daemon-passive 框架下的等价断言。Story 30.6 原 AC 关键守护项：
//   AC#1  — NewHeartbeatMonitor 构造接受 checkInterval 入参
//   AC#3  — 默认 checkInterval 30s（cmd/rnix/main.go 注入）
//   AC#4  — 三级恢复策略：Level 1 retry / Level 2 Suspend / Level 3 通知
//   AC#5  — 进程退出后从 stalledProcs 移除
//   AC#7  — 恢复检测 + ProcessRecovered emit
//   AC#10 — IPC heartbeat_status wire schema
//   AC#11 — CLI rnix heartbeat status
//
// 在 epic-45 P4 框架下，30.6 原 AC#4 的三级语义重新映射为 4 级 action 字段
// （warn / cancel_step / suspend），但**所有动作都降级为 emit only**——`action`
// 字段保留作为 dashboard / 45.5 stall intensity heatmap 的强度信号源。AC#10
// IPC wire schema 完全不动（下游消费者零风险）。
//
// 与 kernel/atdd_45_2_warn_only_test.go 既有 4 个 ATDD 的关系：
//   - 45.2 ATDD 聚焦"case 分支体动作降级"（passive mode 不再调 Suspend/CancelStep）
//   - 本文件 ATDD 聚焦"4 级语义传承到 dashboard + IPC wire 不变 + 配置链路无退步"
//   - 双层覆盖："正向 + 等价"防护
//
// 引用：
//   - _bmad-output/implementation-artifacts/45-4-atdd-rewrite-for-warn-only-semantics.md
//     §AC2 + §Decision D1 (按守护源 story 拆分独立文件)
//   - _bmad-output/implementation-artifacts/30-6-supervisor-heartbeat-monitor-and-auto-recovery.md
//     §Acceptance-Criteria AC#1-#12
//   - kernel/heartbeat_monitor.go#149-220 handleStalled (45.2 后 warn-only)
//   - kernel/heartbeat_monitor.go#255-305 Status / HeartbeatStatusInfo / StalledProcInfo
//   - ipc/protocol.go#1322-1343 HeartbeatStatusResponse / StalledProcWire (AC#10)
//   - kernel/heartbeat_monitor_unit_test.go#357-365 newHeartbeatTestKernel (复用)
//   - kernel/atdd_44_3_helpers_test.go#181 drainAllEvents (复用)
//   - kernel/atdd_45_2_warn_only_test.go#51-68 findStalledEventAction (复用)
// =============================================================================

// -----------------------------------------------------------------------------
// AC2 用例 001 — 4 级 action 字段映射 (warn/cancel_step/suspend) 在 passive mode 保留
// -----------------------------------------------------------------------------

// TestATDD_45_4_30_6_001_EscalationActionFieldMappingPreserved
//
// 守护 Story 30.6 原 AC#4（三级恢复策略）在 P4 框架下的等价语义：
// `action` 字段保留 4 级映射作为 dashboard / 45.5 stall intensity heatmap 的
// 强度信号源；所有动作降级为 emit only，State 始终 Running，SuspendReason
// 始终空。
//
// 对同一 stalled 进程，通过 4 次预置 ConsecutiveStalls 触发 4 个不同等级的 scan：
//   ConsecutiveStalls 0 (首次 scan 后变 1) → action="warn"
//   ConsecutiveStalls 1 (变 2)             → action="warn"  (仍 < 3)
//   ConsecutiveStalls 2 (变 3)             → action="cancel_step"
//   ConsecutiveStalls 3 (变 4)             → action="suspend"
//
// 关键不变量：4 次 scan 全程 proc.State == Running 且 SuspendReason == ""
//（passive mode 不破坏进程状态）。
func TestATDD_45_4_30_6_001_EscalationActionFieldMappingPreserved(t *testing.T) {
	k := newHeartbeatTestKernel(t)
	proc := NewProcess(0, "test-atdd-45-4-30-6-001", nil)
	proc.mu.Lock()
	proc.State = types.StateRunning
	proc.StepTimeout = 100 * time.Millisecond
	proc.LastHeartbeat = time.Now().Add(-10 * time.Minute) // 远过期，保持 stalled
	proc.mu.Unlock()
	k.procTable.Store(proc.PID, proc)

	hm := NewHeartbeatMonitor(k, 50*time.Millisecond)

	expectedActions := []string{"warn", "warn", "cancel_step", "suspend"}

	for i, want := range expectedActions {
		hm.scan()

		// Drain immediately to inspect the emit produced by THIS scan.
		events := drainAllEvents(t, proc, 50*time.Millisecond)
		action, count, found := findStalledEventAction(events)
		if !found {
			t.Fatalf("Story 45.4 AC2.001: scan #%d emitted no ProcessStalled event (30.6 AC#4 — 每次 stall scan 必须 emit)", i+1)
		}
		if count != 1 {
			t.Errorf("Story 45.4 AC2.001: scan #%d emit count = %d, want 1", i+1, count)
		}
		if action != want {
			t.Errorf("Story 45.4 AC2.001: scan #%d action = %q, want %q (30.6 AC#4 → P4 4 级映射: warn/warn/cancel_step/suspend)",
				i+1, action, want)
		}

		// P4 invariant: State must remain Running after every scan.
		if got := proc.GetState(); got != types.StateRunning {
			t.Fatalf("Story 45.4 AC2.001: scan #%d — State = %s, want StateRunning (passive mode forbids auto-Suspend)", i+1, got)
		}
	}

	// Final invariants after all 4 scans.
	if reason := proc.GetSuspendReason(); reason != "" {
		t.Errorf("Story 45.4 AC2.001: SuspendReason after 4 escalation scans = %q, want \"\" (P4: daemon never writes \"heartbeat_timeout\")", reason)
	}

	hm.mu.Lock()
	rec, exists := hm.stalledProcs[proc.PID]
	cs := 0
	if exists {
		cs = rec.ConsecutiveStalls
	}
	hm.mu.Unlock()
	if !exists {
		t.Error("Story 45.4 AC2.001: stallRecord MUST persist through all 4 scans (passive mode never deletes)")
	}
	if cs != 4 {
		t.Errorf("Story 45.4 AC2.001: ConsecutiveStalls after 4 scans = %d, want 4 (累积无 reset)", cs)
	}
}

// -----------------------------------------------------------------------------
// AC2 用例 002 — stallRecord 持续到 (a) 心跳恢复 或 (b) 进程退出 State!=Running
// -----------------------------------------------------------------------------

// TestATDD_45_4_30_6_002_StalledRecordPersistsUntilRecoveryOrExit
//
// 守护 Story 30.6 原 AC#7（恢复检测 + ProcessRecovered emit） + AC#5 子句
// （进程退出后从 stalledProcs 移除）在 P4 框架下行为不变——passive mode 不影响
// recovery 和 cleanup 路径。
//
// 子用例 (a) — 心跳恢复路径：
//   1. 进程 stalled 一次 (ConsecutiveStalls=1)
//   2. 调 proc.TouchHeartbeat() 模拟心跳恢复（推进 LastHeartbeat 到当前时间）
//   3. scan 一次 → checkRecovery 路径触发
//   4. 断言：emit ProcessRecovered (with recovered_after_ms 非零 +
//      consecutive_stalls=1) + stallRecord 已清理
//
// 子用例 (b) — 进程退出 (State != Running) 路径：
//   1. 另一进程 stalled 一次
//   2. 强制 proc.State = StateDead
//   3. scan 一次 → cleanupIfTracked 路径触发
//   4. 断言：stallRecord 已清理（不依赖 ProcessRecovered emit，Dead 进程不再
//      Running）
func TestATDD_45_4_30_6_002_StalledRecordPersistsUntilRecoveryOrExit(t *testing.T) {
	t.Run("RecoveryPath_ProcessRecoveredEmit", func(t *testing.T) {
		k := newHeartbeatTestKernel(t)
		proc := NewProcess(0, "test-atdd-45-4-30-6-002-recovery", nil)
		proc.mu.Lock()
		proc.State = types.StateRunning
		proc.StepTimeout = 100 * time.Millisecond
		proc.LastHeartbeat = time.Now().Add(-500 * time.Millisecond)
		proc.mu.Unlock()
		k.procTable.Store(proc.PID, proc)

		hm := NewHeartbeatMonitor(k, 50*time.Millisecond)

		// First scan → ConsecutiveStalls=1, stallRecord exists.
		hm.scan()
		hm.mu.Lock()
		_, ok := hm.stalledProcs[proc.PID]
		hm.mu.Unlock()
		if !ok {
			t.Fatal("Story 45.4 AC2.002 recovery: precondition — stallRecord must exist after first stalled scan")
		}

		// Drain warn emit so we don't conflate it with the ProcessRecovered emit.
		_ = drainAllEvents(t, proc, 50*time.Millisecond)

		// Recover heartbeat — simulates reasonStep / driver streaming making real progress.
		proc.TouchHeartbeat()

		// Second scan → checkRecovery path triggered.
		hm.scan()

		// stallRecord must be cleared.
		hm.mu.Lock()
		_, stillTracked := hm.stalledProcs[proc.PID]
		hm.mu.Unlock()
		if stillTracked {
			t.Error("Story 45.4 AC2.002 recovery: stallRecord MUST be cleared after heartbeat recovery (30.6 AC#7 — checkRecovery cleanup)")
		}

		// Verify ProcessRecovered event emitted with expected fields.
		events := drainAllEvents(t, proc, 100*time.Millisecond)
		var recoveredFound bool
		var recoveredAfterMs int64
		var consecutiveStalls int
		for _, ev := range events {
			if ev.Syscall != "HeartbeatMonitor" {
				continue
			}
			if name, _ := ev.Args["event"].(string); name == "ProcessRecovered" {
				recoveredFound = true
				if v, ok := ev.Args["recovered_after_ms"].(int64); ok {
					recoveredAfterMs = v
				}
				if v, ok := ev.Args["consecutive_stalls"].(int); ok {
					consecutiveStalls = v
				}
			}
		}
		if !recoveredFound {
			t.Error("Story 45.4 AC2.002 recovery: expected ProcessRecovered emit, got none (30.6 AC#7 — recovery event 不退步)")
		}
		if recoveredAfterMs <= 0 {
			t.Errorf("Story 45.4 AC2.002 recovery: recovered_after_ms = %d, want > 0 (30.6 AC#7 wire field)", recoveredAfterMs)
		}
		if consecutiveStalls != 1 {
			t.Errorf("Story 45.4 AC2.002 recovery: consecutive_stalls = %d, want 1 (pre-recovery stall count)", consecutiveStalls)
		}
	})

	t.Run("ExitPath_CleanupIfTracked", func(t *testing.T) {
		k := newHeartbeatTestKernel(t)
		proc := NewProcess(0, "test-atdd-45-4-30-6-002-exit", nil)
		proc.mu.Lock()
		proc.State = types.StateRunning
		proc.StepTimeout = 100 * time.Millisecond
		proc.LastHeartbeat = time.Now().Add(-500 * time.Millisecond)
		proc.mu.Unlock()
		k.procTable.Store(proc.PID, proc)

		hm := NewHeartbeatMonitor(k, 50*time.Millisecond)

		// First scan → stallRecord exists.
		hm.scan()
		hm.mu.Lock()
		_, ok := hm.stalledProcs[proc.PID]
		hm.mu.Unlock()
		if !ok {
			t.Fatal("Story 45.4 AC2.002 exit: precondition — stallRecord must exist after first stalled scan")
		}

		// Process exits (Dead).
		proc.mu.Lock()
		proc.State = types.StateDead
		proc.mu.Unlock()

		// Second scan → cleanupIfTracked path triggered.
		hm.scan()

		hm.mu.Lock()
		_, stillTracked := hm.stalledProcs[proc.PID]
		hm.mu.Unlock()
		if stillTracked {
			t.Error("Story 45.4 AC2.002 exit: stallRecord MUST be removed when proc transitions out of StateRunning (30.6 AC#5 — Dead/Zombie/Suspended cleanup)")
		}
	})
}

// -----------------------------------------------------------------------------
// AC2 用例 003 — 已迁移至 ipc/atdd_45_4_30_6_wire_schema_test.go
// -----------------------------------------------------------------------------
//
// AC2.003 wire schema 不变量 ATDD 已迁移到 ipc 包：直接 instance 化
// ipc.HeartbeatStatusResponse{} + ipc.StalledProcWire{} 并 json.Marshal 验证
// 字段集精确匹配 spec 列出的 4+6 snake_case key（含负向断言：marshal 结果中
// 无意外字段）。该路径避免 kernel→ipc 反向 import 循环，且证据强度优于 kernel
// 包内的 source-grep。
//
// 详见: ipc/atdd_45_4_30_6_wire_schema_test.go (TestATDD_45_4_30_6_003_*)
// 决策来源: code review 2026-05-24 — Decision-needed 选项 A 解决

// -----------------------------------------------------------------------------
// AC2 用例 004 — checkInterval 可配置 + 默认 30s (cmd/rnix/main.go)
// -----------------------------------------------------------------------------

// TestATDD_45_4_30_6_004_CheckIntervalIsConfigurableAndDefaults30s
//
// 守护 Story 30.6 原 AC#3 (心跳检测延迟 ≤5s, checkInterval 可配置, 默认 30s)
// + AC#1 (NewHeartbeatMonitor 入参) 在 P4 框架下行为不变——passive mode 仅改
// handleStalled case body，构造 / 配置路径不动。
//
// (1) 三个 NewHeartbeatMonitor 实例 → 验证 checkInterval 透传
// (2) 读 cmd/rnix/main.go → 验证默认 30s 注入存在
func TestATDD_45_4_30_6_004_CheckIntervalIsConfigurableAndDefaults30s(t *testing.T) {
	k := newHeartbeatTestKernel(t)

	cases := []time.Duration{
		30 * time.Second,
		5 * time.Second,
		100 * time.Millisecond,
	}
	for _, want := range cases {
		hm := NewHeartbeatMonitor(k, want)
		got := hm.Status().CheckInterval
		if got != want {
			t.Errorf("Story 45.4 AC2.004: NewHeartbeatMonitor(k, %v).Status().CheckInterval = %v, want %v (30.6 AC#1 入参透传)", want, got, want)
		}
	}

	// Verify cmd/rnix/main.go still injects default 30s.
	repoRoot, err := findRepoRootForATDD45_4(".")
	if err != nil {
		t.Fatalf("Story 45.4 AC2.004: locate repo root: %v", err)
	}
	mainPath := filepath.Join(repoRoot, "cmd", "rnix", "main.go")
	body, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatalf("Story 45.4 AC2.004: read cmd/rnix/main.go: %v", err)
	}
	text := string(body)

	// Look for the production injection site: `NewHeartbeatMonitor(k, 30*time.Second)`
	// (whitespace-tolerant — we accept any of "30 * time.Second" / "30*time.Second").
	wantTokens := []string{"NewHeartbeatMonitor", "30 * time.Second"}
	for _, tok := range wantTokens {
		if !strings.Contains(text, tok) {
			t.Errorf("Story 45.4 AC2.004: cmd/rnix/main.go missing token %q — default checkInterval 30s injection 退步 (30.6 AC#3)", tok)
		}
	}
}

