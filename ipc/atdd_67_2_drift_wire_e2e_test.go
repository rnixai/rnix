package ipc

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rnixai/rnix/intent"
	"github.com/rnixai/rnix/internal/types"
)

// Story 67.2 — Intent 漂移死代码删除的「跨包端到端回归」套件。
//
// 与 intent/atdd_67_2_drift_dead_code_removal_test.go 的区别：
//   - intent 包 ATDD 是 *源码内容扫描*（死符号从 .go 消失）+ *反射/单元* GREEN
//     守卫（DriftItem json tag、活体常量值、config 字段）——属静态/单元。
//   - 本文件是 *跨包运行时端到端*：真实 intent.Reconciler 被失败节点驱动，走
//     activate → AddDrift(node_failed) 活体路径，随后经 ipc 层 intentTreeToWire/
//     driftItemToWire 转换为 DriftItemWire，断言活体 drift 一路流到 wire 字段完整。
//
// 删除型 story（67-2 删的全是 intent 包内部符号：ComputeDrifts 死方法 + 两个空壳
// 漂移常量 + 两个死配置字段，零 IPC/wire 暴达面）的 E2E 价值不在「测新功能」，
// 而在「回归」——证明删除死骨架后，活体 drift 编排（node_failed/node_timeout →
// AddDrift → intentTreeToWire → DriftItemWire）端到端仍自洽，AC4「wire 面零改动」
// 有跨包运行时证据。67-1 那套「已删 IPC 方法优雅降级」三断言在此不适用：67-2
// 未删任何 IPC 方法，无 method 可触发 default 分支。
//
// 唯一未被现有单一测试端到端串起的正是这条 intent(真实 reconciler 失败) → ipc(wire
// 转换) 跨包链路——reconciler_test.go 覆盖到 OnDriftDetected 回调为止，未跨到 wire。

// e2eDriftSpawner drives intent.Reconciler through IntentKernelSpawner's injection
// points. SpawnIntent hands out monotonically increasing PIDs (mirroring the intent
// package's own mock); Wait returns a per-PID ExitStatus so a node can fail on its
// first attempt and (optionally) succeed on retry.
func e2eDriftSpawner(waitByPID func(pid types.PID) intent.ExitStatus) *IntentKernelSpawner {
	var nextPID atomic.Int64
	return &IntentKernelSpawner{
		SpawnFunc: func(_ context.Context, _ *intent.IntentNode) (types.PID, error) {
			return types.PID(nextPID.Add(1)), nil
		},
		WaitFunc: func(pid types.PID) (intent.ExitStatus, error) {
			return waitByPID(pid), nil
		},
		KillFunc: func(_ types.PID) error { return nil },
	}
}

// TestE2E_67_2_DriftFlowsToWireAfterNodeFailure 验证：删除死漂移骨架后，一个节点
// 失败驱动的活体 drift（DriftNodeFailed）经 reconciler AddDrift 累积到 tree.Drifts，
// 再经 ipc 层 intentTreeToWire 转换为 DriftItemWire 时字段逐一完整——这是 AC4
// 「DriftItem wire 暴露面零改动」的运行时端到端证据。
//
// MaxRetries=0 → CanRetry()==false → 失败即 MarkFailed（不 ClearDrift），drift 留存。
func TestE2E_67_2_DriftFlowsToWireAfterNodeFailure(t *testing.T) {
	tree := &intent.IntentTree{
		ID:         "intent-67-2-e2e-fail",
		RootIntent: "drift flows to wire",
		State:      intent.IntentAwaitConfirm,
		Nodes: map[string]*intent.IntentNode{
			"a": {ID: "a", Intent: "always fails", State: intent.IntentPending, MaxRetries: 0},
		},
		CreatedAt: time.Now(),
	}

	// 恒失败（Code!=0）→ reconciler emit evNodeFailed → AddDrift(node_failed)。
	spawner := e2eDriftSpawner(func(_ types.PID) intent.ExitStatus {
		return intent.ExitStatus{Code: 1, Reason: "boom", Err: context.Canceled}
	})

	reconciler, err := intent.NewReconciler(tree, spawner, intent.DefaultReconcilerConfig(), intent.ReconcilerCallbacks{})
	if err != nil {
		t.Fatalf("NewReconciler: %v", err)
	}
	_ = reconciler.Execute(context.Background())

	// 活体断言 1：节点失败后 drift 留在 tree.Drifts（重试耗尽未 ClearDrift）。
	if got := len(tree.Drifts); got != 1 {
		t.Fatalf("tree.Drifts after node failure = %d, want 1 (drift should persist when retries exhausted)", got)
	}

	// 端到端断言：intentTreeToWire → DriftItemWire 字段逐一完整（AC4 wire 红线）。
	wire := intentTreeToWire(tree)
	if wire == nil {
		t.Fatal("intentTreeToWire returned nil")
	}
	if len(wire.Drifts) != 1 {
		t.Fatalf("wire.Drifts = %d, want 1 — 活体 drift 未流到 wire", len(wire.Drifts))
	}
	dw := wire.Drifts[0]
	if dw.NodeID != "a" {
		t.Errorf("DriftItemWire.NodeID = %q, want %q", dw.NodeID, "a")
	}
	// Type 必须是活体常量 node_failed（DriftNodeFailed），证明死常量删除未误伤活体值。
	if dw.Type != string(intent.DriftNodeFailed) {
		t.Errorf("DriftItemWire.Type = %q, want %q", dw.Type, string(intent.DriftNodeFailed))
	}
	if dw.Type != "node_failed" {
		t.Errorf("DriftItemWire.Type wire 字面量 = %q, want \"node_failed\"", dw.Type)
	}
	if dw.Message == "" {
		t.Error("DriftItemWire.Message 应携带失败原因，得到空串")
	}
	if dw.DetectedAtMs <= 0 {
		t.Errorf("DriftItemWire.DetectedAtMs = %d, want > 0（DetectedAt.UnixMilli 应被填充）", dw.DetectedAtMs)
	}
}

// TestE2E_67_2_DriftClearedFromWireAfterRetrySuccess 验证活体 drift *生命周期* 在
// wire 面正确反映：节点首次失败记 drift → 重试成功后 ClearDrift → tree.Drifts 归零
// → intentTreeToWire 的 wire.Drifts 也为空。证明删除死骨架后，drift 的记录与消解
// 活体编排端到端自洽（ClearDrift 链未被连带破坏）。
func TestE2E_67_2_DriftClearedFromWireAfterRetrySuccess(t *testing.T) {
	tree := &intent.IntentTree{
		ID:         "intent-67-2-e2e-resolve",
		RootIntent: "drift cleared from wire",
		State:      intent.IntentAwaitConfirm,
		Nodes: map[string]*intent.IntentNode{
			"a": {ID: "a", Intent: "fails once then succeeds", State: intent.IntentPending, MaxRetries: 3},
		},
		CreatedAt: time.Now(),
	}

	// PID 1（首次 spawn）失败 → AddDrift；PID≥2（重试 spawn）成功 → ClearDrift。
	spawner := e2eDriftSpawner(func(pid types.PID) intent.ExitStatus {
		if pid == types.PID(1) {
			return intent.ExitStatus{Code: 1, Reason: "transient", Err: context.Canceled}
		}
		return intent.ExitStatus{Code: 0, Result: "ok"}
	})

	reconciler, err := intent.NewReconciler(tree, spawner, intent.DefaultReconcilerConfig(), intent.ReconcilerCallbacks{})
	if err != nil {
		t.Fatalf("NewReconciler: %v", err)
	}
	_ = reconciler.Execute(context.Background())

	// 节点最终完成（重试成功）。
	if node := tree.Nodes["a"]; node.State != intent.IntentCompleted {
		t.Fatalf("node 'a' state = %q, want %q (retry should have succeeded)", node.State, intent.IntentCompleted)
	}

	// 活体断言：drift 被 ClearDrift 消解，tree.Drifts 归零。
	if got := len(tree.Drifts); got != 0 {
		t.Fatalf("tree.Drifts after retry success = %d, want 0 (ClearDrift should have removed it)", got)
	}

	// 端到端断言：wire.Drifts 同步为空（nil 或空切片皆可——无残留 DriftItemWire）。
	wire := intentTreeToWire(tree)
	if wire == nil {
		t.Fatal("intentTreeToWire returned nil")
	}
	if len(wire.Drifts) != 0 {
		t.Errorf("wire.Drifts after drift resolved = %d, want 0", len(wire.Drifts))
	}
}
