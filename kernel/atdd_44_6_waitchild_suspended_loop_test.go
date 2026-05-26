package kernel

import (
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// =============================================================================
// ATDD 44.6 — WaitChildInReason Suspended-loop（v2 AC7 对称收尾）
//
// Spec: _bmad-output/implementation-artifacts/spec-fix-pause-child-reaped-as-dead.md
//
// 背景:Story 44.5 v2 AC7 修了 ipc/server_pipeline.go::SpawnAndWait 的 receive
// 侧:收到 proc.Done 后用 GetState() 判别终态/mid-state suspend,Suspended
// continue 重 select。但 kernel/reap.go::WaitChildInReason(LLM agent 父进程
// 等子进程的内部 spawn syscall 路径)漏了相同治理,把 Suspended child 当成
// completed 立即 reap,导致父 reasonStep 拿 Code=2 错误推进(EchoMatrix 现场
// 症状:Tree 中 child 红色 ✗ + 父进程继续 spawn "上一轮执行异常..." 进程)。
//
// 本测试套件锁定 receive 侧治理协议:
//   - 030: Pause-child 后 WaitChildInReason **不**返回(default case continue)
//   - 031: Resume happy-exit 后 WaitChildInReason 返回 Code=0,reap 后 invariant 一致
//   - 032: Parent ctx cancel 不退化(Suspended child 不被 reap,留给 SuspendSubtree
//          walker)
// =============================================================================

// waitChildResult 用于 fork goroutine 跑 WaitChildInReason 时回传结果。
type waitChildResult struct {
	exit      ExitStatus
	cancelled bool
}

// runWaitChildInReason 在 goroutine 中跑 WaitChildInReason,返回结果 channel。
// 调用者负责通过 SuspendSubtree / Cancel / Finish 等方式让函数最终返回。
func runWaitChildInReason(k *KernelImpl, parent *Process, childPID types.PID) <-chan waitChildResult {
	resultCh := make(chan waitChildResult, 1)
	go func() {
		exit, cancelled := k.WaitChildInReason(parent, childPID)
		resultCh <- waitChildResult{exit: exit, cancelled: cancelled}
	}()
	return resultCh
}

// simulateNotifySuspendDone 模拟 kernel/suspend.go:106-121 notifySuspendDone 在
// reasonStep defer 路径写 mid-state Done 信号。production 路径需要 reasonStep
// goroutine 跑 defer,但 makeRunningProc44_1 fixture 没有 reasonStep,故这里
// 直接向 child.Done 写入等价信号,用于单元测试 receive 侧逻辑。
func simulateNotifySuspendDone(t *testing.T, child *Process) {
	t.Helper()
	exit := ExitStatus{Code: ExitSuspended, Reason: "suspended"}
	select {
	case child.Done <- exit:
	default:
		t.Fatalf("child.Done already full — fixture leaked an earlier write")
	}
}

// TestATDD_44_6_030_PauseChild_WaitChildInReason_DoesNotReturn 锁定主修复:
// child 被 SuspendSubtree 转 Suspended + notifySuspendDone 写 mid-state Done
// 后,父 WaitChildInReason 必须 **不**返回(走 default case continue),否则
// 父 reasonStep 会拿 Code=2 错误推进(EchoMatrix 现场症状)。
func TestATDD_44_6_030_PauseChild_WaitChildInReason_DoesNotReturn(t *testing.T) {
	k := newSubtreeKernel(t)

	parent := makeRunningProc44_1(t, k, 0, "parent")
	child := makeRunningProc44_1(t, k, parent.PID, "child")

	resultCh := runWaitChildInReason(k, parent, child.PID)

	// child 子树暂停:state Running → Suspended
	if _, err := k.SuspendSubtree(child.PID); err != nil {
		t.Fatalf("SuspendSubtree: %v", err)
	}
	assertProcState44_1(t, child, types.StateSuspended, "child after SuspendSubtree")

	// 模拟 notifySuspendDone 写 mid-state suspend 信号到 child.Done
	simulateNotifySuspendDone(t, child)

	// 锁定核心契约:200ms 内 WaitChildInReason **不**返回(default case continue)
	select {
	case got := <-resultCh:
		t.Fatalf("WaitChildInReason returned prematurely on mid-state Done: exit=%+v cancelled=%v",
			got.exit, got.cancelled)
	case <-time.After(200 * time.Millisecond):
		// PASS: WaitChildInReason 仍在 select 等下一个真正的 finishProcess Done
	}

	// child 仍 Suspended(没被误 reap)
	if got := child.GetState(); got != types.StateSuspended {
		t.Errorf("child.State = %s, want Suspended (must not be reaped to Dead)", got)
	}

	// 收尾:Cancel parent 让 WaitChildInReason 通过 parentCtxDone 退出,避免 goroutine leak
	parent.Cancel()
	select {
	case <-resultCh:
	case <-time.After(1 * time.Second):
		t.Fatal("WaitChildInReason did not exit after parent.Cancel()")
	}
}

// TestATDD_44_6_031_ResumeChild_WaitChildInReason_ReturnsHappyExit 锁定 Resume
// 路径:Pause 后 Resume + finishProcess(Code=0) 让 WaitChildInReason 收到
// **真正的**终态 Done,正常 reap+return Code=0;reap 后的 ProcInfo 满足
// ValidateProcInfoInvariant Dead 行约束(SuspendReason="")。
func TestATDD_44_6_031_ResumeChild_WaitChildInReason_ReturnsHappyExit(t *testing.T) {
	k := newSubtreeKernel(t)

	parent := makeRunningProc44_1(t, k, 0, "parent")
	child := makeRunningProc44_1(t, k, parent.PID, "child")

	resultCh := runWaitChildInReason(k, parent, child.PID)

	// 同 030: SuspendSubtree + 模拟 mid-state Done
	if _, err := k.SuspendSubtree(child.PID); err != nil {
		t.Fatalf("SuspendSubtree: %v", err)
	}
	simulateNotifySuspendDone(t, child)

	// 给 WaitChildInReason 一段时间消费 mid-state Done 并进入 continue 等待
	time.Sleep(50 * time.Millisecond)

	// ResumeSubtree: Suspended → Running,清 SuspendReason + drain stale Done
	if _, _, err := k.ResumeSubtree(child.PID); err != nil {
		t.Fatalf("ResumeSubtree: %v", err)
	}
	assertProcState44_1(t, child, types.StateRunning, "child after ResumeSubtree")

	// 模拟 reasonStep 完成 finishProcess(Code=0):Running → Zombie + 写 Done
	// (Process.Finish 是 kernel test convenience,等价于 finishProcess happy path)
	child.Finish("ok", 0, nil)

	// 断言 WaitChildInReason 2s 内返回 cancelled=false + exit.Code=0
	select {
	case got := <-resultCh:
		if got.cancelled {
			t.Errorf("cancelled = true, want false (happy exit)")
		}
		if got.exit.Code != 0 {
			t.Errorf("exit.Code = %d, want 0", got.exit.Code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("WaitChildInReason did not return after Resume + Finish within 2s")
	}

	// child 已被 reap → Dead
	assertProcState44_1(t, child, types.StateDead, "child after reap")

	// Invariant 一致性: state=Dead × SuspendReason="" × ExitReason!=""
	info, err := k.GetProcInfo(child.PID)
	if err != nil {
		t.Fatalf("GetProcInfo: %v", err)
	}
	if err := ValidateProcInfoInvariant(info); err != nil {
		t.Errorf("ValidateProcInfoInvariant violated after reap: %v\nProcInfo: %+v", err, info)
	}
}

// TestATDD_44_6_032_ParentCancel_WaitChildInReason_DoesNotReapSuspended 锁定
// 边缘场景:父 ctx 被取消(例如 SuspendSubtree 也作用于父)时,WaitChildInReason
// 走 parentCtxDone case 返回 cancelled=true,**但**已 Suspended 的 child 不能
// 被 reap(留给 SuspendSubtree walker 维持其 Suspended state)。
func TestATDD_44_6_032_ParentCancel_WaitChildInReason_DoesNotReapSuspended(t *testing.T) {
	k := newSubtreeKernel(t)

	parent := makeRunningProc44_1(t, k, 0, "parent")
	child := makeRunningProc44_1(t, k, parent.PID, "child")

	resultCh := runWaitChildInReason(k, parent, child.PID)

	// child Suspended
	if _, err := k.SuspendSubtree(child.PID); err != nil {
		t.Fatalf("SuspendSubtree: %v", err)
	}
	assertProcState44_1(t, child, types.StateSuspended, "child after SuspendSubtree")

	// 写 mid-state Done(模拟 production notifySuspendDone)— 这次 default case
	// 会 continue,但下一轮 select 会被 parentCtxDone 命中
	simulateNotifySuspendDone(t, child)

	// 给 WaitChildInReason 时间消费 Done 并 continue
	time.Sleep(50 * time.Millisecond)

	// parent.Cancel() 触发 parent.CancelledCh() → parentCtxDone case
	parent.Cancel()

	// 断言 WaitChildInReason 1s 内返回 cancelled=true
	select {
	case got := <-resultCh:
		if !got.cancelled {
			t.Errorf("cancelled = false, want true (parent ctx done)")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("WaitChildInReason did not return after parent.Cancel()")
	}

	// 关键不变量:child 仍 Suspended(没被 WaitChildInReason 误 reap 成 Dead)
	if got := child.GetState(); got != types.StateSuspended {
		t.Errorf("child.State = %s, want Suspended (parentCtxDone path must NOT reap)", got)
	}
}
