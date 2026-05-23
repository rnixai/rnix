package kernel

import (
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// =============================================================================
// ATDD 44.3 — AC#5: HeartbeatMonitor does NOT escalate placeholders
// reloaded from disk into "stalled" alerts, even though their LastHeartbeat
// values are arbitrarily old (the daemon-restart event itself does not
// touch heartbeat).
//
// Story spec (44-3 §AC5):
//   - 44.1 already lets heartbeat_monitor.go skip State==StateSuspended in
//     the outer scan() guard (line 102-107).
//   - LoadSuspendedFromDisk places placeholders with State=StateSuspended
//     into procTable, so the existing guard is sufficient.
//   - This AC is REGRESSION coverage: pin the invariant that the guard
//     applies to reload placeholders too (no special-case interaction with
//     the disk-loaded LastHeartbeat field).
//
// RED phase signal: depends on LoadSuspendedFromDisk being defined
// (compile-fail on Task 3.1 absence). Runtime behaviour is already correct
// today thanks to 44.1's guard; this test pins it as protection against
// a future refactor that might inadvertently regress.
//
// Story 45.4 P4 引用 (Epic 45): 本测试守护的 "outer-scan guard skip-Suspended"
// 不变量在 Epic 45 P4 daemon-passive 框架后**仍然有效**——passive mode 仅改
// handleStalled case body，未动 outer scan() guard。等价语义在
// kernel/atdd_45_4_30_6_continuity_test.go (AC2 用例) 与
// kernel/atdd_45_2_warn_only_test.go (45.2 既有) 中显式指名锚定。
// =============================================================================

// TestATDD_44_3_050_HeartbeatMonitor_SkipsReloadedSuspendedPlaceholder
//
// AC#5: A placeholder with LastHeartbeat set to "1 hour ago" must not be
// escalated to "stalled" by HeartbeatMonitor.scan(). We verify this by:
//   1. Reload a placeholder via LoadSuspendedFromDisk (state=Suspended).
//   2. Force-set LastHeartbeat = now - 1h.
//   3. Run one scan() pass.
//   4. Assert the process is still Suspended AND no Heartbeat stall event
//      was emitted.
func TestATDD_44_3_050_HeartbeatMonitor_SkipsReloadedSuspendedPlaceholder(t *testing.T) {
	k, dataDir := newReloadKernel(t)
	uuid := uuidForTest("hbreload")

	writeSuspendProcInfoFixture(t, dataDir, suspendDiskInfo{
		PID:           21,
		UUID:          uuid,
		State:         "suspended",
		Intent:        "heartbeat reload regression",
		Provider:      "claude",
		Model:         "claude-opus-4-7",
		CreatedAt:     staticTime(t, -3*time.Hour).Format(time.RFC3339Nano),
		CtxID:         1,
		SuspendReason: "user_paused",
		IsPaused:      true,
	}, true, true)

	if _, err := k.LoadSuspendedFromDisk(); err != nil {
		t.Fatalf("LoadSuspendedFromDisk: %v", err)
	}
	proc, ok := k.GetProcessByUUID(uuid)
	if !ok {
		t.Fatal("placeholder not loaded")
	}

	// Force-set LastHeartbeat to "1 hour ago" so a faulty implementation
	// (one that scanned Suspended placeholders) would definitely fire stall.
	proc.mu.Lock()
	proc.LastHeartbeat = time.Now().Add(-1 * time.Hour)
	proc.StepTimeout = 1 * time.Minute // any non-zero value enables stall logic
	proc.mu.Unlock()

	hm := NewHeartbeatMonitor(k, 10*time.Millisecond)
	// We invoke scan() directly rather than Start() to make the test
	// deterministic (no race between the scan goroutine and the assertion).
	hm.scan()

	// Drain a short window to catch any heartbeat-escalation events.
	events := drainAllEvents(t, proc, 100*time.Millisecond)
	for _, ev := range events {
		if ev.Syscall == "HeartbeatMonitor" {
			if action, _ := ev.Args["action"].(string); action == "stalled" || action == "escalation" {
				t.Errorf("HeartbeatMonitor escalated reload placeholder: ev=%+v", ev)
			}
		}
	}
	if got := proc.GetState(); got != types.StateSuspended {
		t.Errorf("placeholder state after scan() = %s, want Suspended", got)
	}
}
