package kernel

import (
	gocontext "context"
	"regexp"
	"sync"
	"testing"
	"time"
)

// ============================================================
// ATDD RED PHASE — Story 13.2: 断点系统
//
// Tests reference BreakpointType, Breakpoint, BreakpointCondition,
// BreakpointContext, BPSyscall, BPReasoning, BPQuality, BPBudget,
// Process.AddBreakpoint, Process.RemoveBreakpoint,
// Process.ListBreakpoints, Process.CheckBreakpoint,
// Process.GdbPause, Process.GdbResume, Process.IsGdbPaused,
// Process.GdbPauseCh
// which do NOT exist yet → compile failure = RED phase.
// ============================================================

// newBreakpointTestProcess creates a Running test process for breakpoint tests.
func newBreakpointTestProcess(t *testing.T, k *KernelImpl) *Process {
	t.Helper()
	proc := NewProcess(0, "bp-test", nil)
	ctx, cancel := gocontext.WithCancel(gocontext.Background())
	proc.ctx = ctx
	proc.cancel = cancel
	_ = proc.Start()
	k.AddProcess(proc)
	k.msgQueues.Store(proc.PID, newMessageQueue())
	return proc
}

// --- 13.2-UNIT-001: [P0] BreakpointType 枚举值存在且互不相同 ---

func TestBreakpointType_Constants(t *testing.T) {
	bpTypes := []BreakpointType{BPSyscall, BPReasoning, BPQuality, BPBudget}
	seen := make(map[BreakpointType]bool)
	for _, bt := range bpTypes {
		if bt == 0 {
			t.Errorf("BreakpointType should not be zero value: %v", bt)
		}
		if seen[bt] {
			t.Errorf("duplicate BreakpointType: %v", bt)
		}
		seen[bt] = true
	}
}

// --- 13.2-UNIT-002: [P0] Breakpoint 结构体包含必要字段 ---

func TestBreakpoint_Fields(t *testing.T) {
	bp := &Breakpoint{
		ID:       1,
		Type:     BPSyscall,
		Enabled:  true,
		HitCount: 0,
	}
	if bp.ID != 1 {
		t.Errorf("ID = %d, want 1", bp.ID)
	}
	if bp.Type != BPSyscall {
		t.Errorf("Type = %v, want BPSyscall", bp.Type)
	}
	if !bp.Enabled {
		t.Error("Enabled should be true")
	}
	if bp.HitCount != 0 {
		t.Errorf("HitCount = %d, want 0", bp.HitCount)
	}
}

// --- 13.2-UNIT-003: [P0] Breakpoint 支持 Condition 接口 ---

func TestBreakpoint_SyscallCondition(t *testing.T) {
	bp := &Breakpoint{
		ID:        1,
		Type:      BPSyscall,
		Enabled:   true,
		Condition: &SyscallCondition{Name: "Read"},
	}

	sc, ok := bp.Condition.(*SyscallCondition)
	if !ok {
		t.Fatal("Condition should be *SyscallCondition")
	}
	if sc.Name != "Read" {
		t.Errorf("Name = %q, want %q", sc.Name, "Read")
	}
}

func TestBreakpoint_ReasoningCondition(t *testing.T) {
	bp := &Breakpoint{
		ID:        2,
		Type:      BPReasoning,
		Enabled:   true,
		Condition: &ReasoningCondition{},
	}

	_, ok := bp.Condition.(*ReasoningCondition)
	if !ok {
		t.Fatal("Condition should be *ReasoningCondition")
	}
}

func TestBreakpoint_QualityPatternCondition(t *testing.T) {
	bp := &Breakpoint{
		ID:   3,
		Type: BPQuality,
		Condition: &QualityCondition{
			Mode:    QualityModePattern,
			Pattern: regexp.MustCompile("安全漏洞"),
		},
	}

	qc, ok := bp.Condition.(*QualityCondition)
	if !ok {
		t.Fatal("Condition should be *QualityCondition")
	}
	if qc.Mode != QualityModePattern {
		t.Errorf("Mode = %v, want QualityModePattern", qc.Mode)
	}
	if qc.Pattern == nil {
		t.Fatal("Pattern should not be nil")
	}
}

func TestBreakpoint_QualityEvalCondition(t *testing.T) {
	bp := &Breakpoint{
		ID:   4,
		Type: BPQuality,
		Condition: &QualityCondition{
			Mode:     QualityModeEval,
			EvalExpr: "输出必须包含代码示例",
		},
	}

	qc, ok := bp.Condition.(*QualityCondition)
	if !ok {
		t.Fatal("Condition should be *QualityCondition")
	}
	if qc.Mode != QualityModeEval {
		t.Errorf("Mode = %v, want QualityModeEval", qc.Mode)
	}
	if qc.EvalExpr != "输出必须包含代码示例" {
		t.Errorf("EvalExpr = %q, want %q", qc.EvalExpr, "输出必须包含代码示例")
	}
}

func TestBreakpoint_BudgetCondition(t *testing.T) {
	bp := &Breakpoint{
		ID:        5,
		Type:      BPBudget,
		Enabled:   true,
		Condition: &BudgetCondition{Threshold: 5000},
	}

	bc, ok := bp.Condition.(*BudgetCondition)
	if !ok {
		t.Fatal("Condition should be *BudgetCondition")
	}
	if bc.Threshold != 5000 {
		t.Errorf("Threshold = %d, want 5000", bc.Threshold)
	}
}

// --- 13.2-UNIT-004: [P0] Process.AddBreakpoint 返回断点 ID ---

func TestProcess_AddBreakpoint(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newBreakpointTestProcess(t, k)

	bp := &Breakpoint{
		Type:      BPSyscall,
		Enabled:   true,
		Condition: &SyscallCondition{Name: "Read"},
	}

	id := proc.AddBreakpoint(bp)
	if id <= 0 {
		t.Errorf("AddBreakpoint should return positive ID, got %d", id)
	}
	if bp.ID != id {
		t.Errorf("bp.ID = %d, want %d", bp.ID, id)
	}
}

// --- 13.2-UNIT-005: [P0] 连续添加断点 ID 递增 ---

func TestProcess_AddBreakpoint_IncrementingIDs(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newBreakpointTestProcess(t, k)

	id1 := proc.AddBreakpoint(&Breakpoint{Type: BPSyscall, Enabled: true, Condition: &SyscallCondition{Name: "Read"}})
	id2 := proc.AddBreakpoint(&Breakpoint{Type: BPReasoning, Enabled: true, Condition: &ReasoningCondition{}})
	id3 := proc.AddBreakpoint(&Breakpoint{Type: BPBudget, Enabled: true, Condition: &BudgetCondition{Threshold: 1000}})

	if id2 <= id1 || id3 <= id2 {
		t.Errorf("IDs should be incrementing: %d, %d, %d", id1, id2, id3)
	}
}

// --- 13.2-UNIT-006: [P0] Process.RemoveBreakpoint 成功删除返回 true ---

func TestProcess_RemoveBreakpoint(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newBreakpointTestProcess(t, k)

	id := proc.AddBreakpoint(&Breakpoint{Type: BPSyscall, Enabled: true, Condition: &SyscallCondition{Name: "Read"}})

	if !proc.RemoveBreakpoint(id) {
		t.Error("RemoveBreakpoint should return true for existing breakpoint")
	}

	// Second removal should return false
	if proc.RemoveBreakpoint(id) {
		t.Error("RemoveBreakpoint should return false for already-removed breakpoint")
	}
}

// --- 13.2-UNIT-007: [P0] Process.RemoveBreakpoint 不存在 ID 返回 false ---

func TestProcess_RemoveBreakpoint_NotFound(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newBreakpointTestProcess(t, k)

	if proc.RemoveBreakpoint(9999) {
		t.Error("RemoveBreakpoint should return false for non-existent ID")
	}
}

// --- 13.2-UNIT-008: [P0] Process.ListBreakpoints 返回所有断点 ---

func TestProcess_ListBreakpoints(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newBreakpointTestProcess(t, k)

	proc.AddBreakpoint(&Breakpoint{Type: BPSyscall, Enabled: true, Condition: &SyscallCondition{Name: "Read"}})
	proc.AddBreakpoint(&Breakpoint{Type: BPReasoning, Enabled: true, Condition: &ReasoningCondition{}})
	proc.AddBreakpoint(&Breakpoint{Type: BPBudget, Enabled: true, Condition: &BudgetCondition{Threshold: 5000}})

	bps := proc.ListBreakpoints()
	if len(bps) != 3 {
		t.Fatalf("ListBreakpoints count = %d, want 3", len(bps))
	}
}

// --- 13.2-UNIT-009: [P0] ListBreakpoints 返回副本，不暴露内部 slice ---

func TestProcess_ListBreakpoints_ReturnsCopy(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newBreakpointTestProcess(t, k)

	proc.AddBreakpoint(&Breakpoint{Type: BPSyscall, Enabled: true, Condition: &SyscallCondition{Name: "Read"}})

	bps1 := proc.ListBreakpoints()
	bps2 := proc.ListBreakpoints()

	// Modifying one slice should not affect the other
	if len(bps1) == 0 {
		t.Fatal("expected at least 1 breakpoint")
	}
	bps1[0] = nil

	if bps2[0] == nil {
		t.Error("ListBreakpoints should return independent copies")
	}
}

// --- 13.2-UNIT-010: [P0] CheckBreakpoint 匹配 syscall 断点 ---

func TestProcess_CheckBreakpoint_Syscall(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newBreakpointTestProcess(t, k)

	proc.AddBreakpoint(&Breakpoint{Type: BPSyscall, Enabled: true, Condition: &SyscallCondition{Name: "Read"}})

	// Should match
	ctx := BreakpointContext{
		BPType:      BPSyscall,
		SyscallName: "Read",
	}
	hit := proc.CheckBreakpoint(ctx)
	if hit == nil {
		t.Fatal("CheckBreakpoint should return matching breakpoint")
	}
	if hit.Type != BPSyscall {
		t.Errorf("hit.Type = %v, want BPSyscall", hit.Type)
	}

	// Should NOT match
	ctx2 := BreakpointContext{
		BPType:      BPSyscall,
		SyscallName: "Write",
	}
	hit2 := proc.CheckBreakpoint(ctx2)
	if hit2 != nil {
		t.Error("CheckBreakpoint should return nil for non-matching syscall")
	}
}

// --- 13.2-UNIT-011: [P0] CheckBreakpoint 匹配 reasoning 断点 ---

func TestProcess_CheckBreakpoint_Reasoning(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newBreakpointTestProcess(t, k)

	proc.AddBreakpoint(&Breakpoint{Type: BPReasoning, Enabled: true, Condition: &ReasoningCondition{}})

	ctx := BreakpointContext{BPType: BPReasoning}
	hit := proc.CheckBreakpoint(ctx)
	if hit == nil {
		t.Fatal("CheckBreakpoint should return matching reasoning breakpoint")
	}
}

// --- 13.2-UNIT-012: [P0] CheckBreakpoint 匹配 quality --pattern 断点 ---

func TestProcess_CheckBreakpoint_QualityPattern(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newBreakpointTestProcess(t, k)

	proc.AddBreakpoint(&Breakpoint{
		Type:    BPQuality,
		Enabled: true,
		Condition: &QualityCondition{
			Mode:    QualityModePattern,
			Pattern: regexp.MustCompile("安全漏洞"),
		},
	})

	// Should match
	ctx := BreakpointContext{
		BPType:      BPQuality,
		LLMResponse: "这段代码存在安全漏洞，需要修复",
	}
	hit := proc.CheckBreakpoint(ctx)
	if hit == nil {
		t.Fatal("CheckBreakpoint should match quality pattern")
	}

	// Should NOT match
	ctx2 := BreakpointContext{
		BPType:      BPQuality,
		LLMResponse: "代码质量良好",
	}
	hit2 := proc.CheckBreakpoint(ctx2)
	if hit2 != nil {
		t.Error("CheckBreakpoint should not match non-matching pattern")
	}
}

// --- 13.2-UNIT-013: [P0] CheckBreakpoint 匹配 quality --eval 断点 ---

func TestProcess_CheckBreakpoint_QualityEval(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newBreakpointTestProcess(t, k)

	proc.AddBreakpoint(&Breakpoint{
		Type:    BPQuality,
		Enabled: true,
		Condition: &QualityCondition{
			Mode:     QualityModeEval,
			EvalExpr: "代码示例",
		},
	})

	// Should NOT match (eval checks content DOES NOT contain the expression)
	ctx := BreakpointContext{
		BPType:      BPQuality,
		LLMResponse: "这是一个代码示例: func main() {}",
	}
	hit := proc.CheckBreakpoint(ctx)
	if hit != nil {
		t.Error("CheckBreakpoint should not trigger when eval criteria is met (content contains expression)")
	}

	// SHOULD match (content does NOT contain the eval expression)
	ctx2 := BreakpointContext{
		BPType:      BPQuality,
		LLMResponse: "这是一段纯文本描述",
	}
	hit2 := proc.CheckBreakpoint(ctx2)
	if hit2 == nil {
		t.Fatal("CheckBreakpoint should trigger when eval criteria is NOT met")
	}
}

// --- 13.2-UNIT-014: [P0] CheckBreakpoint 匹配 budget 断点 ---

func TestProcess_CheckBreakpoint_Budget(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newBreakpointTestProcess(t, k)

	proc.AddBreakpoint(&Breakpoint{
		Type:      BPBudget,
		Enabled:   true,
		Condition: &BudgetCondition{Threshold: 5000},
	})

	// Should NOT match (below threshold)
	ctx := BreakpointContext{
		BPType:     BPBudget,
		TokensUsed: 4999,
	}
	hit := proc.CheckBreakpoint(ctx)
	if hit != nil {
		t.Error("CheckBreakpoint should not trigger below budget threshold")
	}

	// Should match (at threshold)
	ctx2 := BreakpointContext{
		BPType:     BPBudget,
		TokensUsed: 5000,
	}
	hit2 := proc.CheckBreakpoint(ctx2)
	if hit2 == nil {
		t.Fatal("CheckBreakpoint should trigger at budget threshold")
	}

	// Should NOT match again (budget breakpoint is one-shot to prevent infinite re-trigger)
	ctx3 := BreakpointContext{
		BPType:     BPBudget,
		TokensUsed: 5001,
	}
	hit3 := proc.CheckBreakpoint(ctx3)
	if hit3 != nil {
		t.Error("CheckBreakpoint should not re-trigger budget breakpoint after first hit (one-shot)")
	}
}

// --- 13.2-UNIT-015: [P1] CheckBreakpoint 跳过禁用的断点 ---

func TestProcess_CheckBreakpoint_DisabledSkipped(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newBreakpointTestProcess(t, k)

	proc.AddBreakpoint(&Breakpoint{
		Type:      BPSyscall,
		Enabled:   false, // disabled
		Condition: &SyscallCondition{Name: "Read"},
	})

	ctx := BreakpointContext{
		BPType:      BPSyscall,
		SyscallName: "Read",
	}
	hit := proc.CheckBreakpoint(ctx)
	if hit != nil {
		t.Error("CheckBreakpoint should skip disabled breakpoints")
	}
}

// --- 13.2-UNIT-016: [P1] CheckBreakpoint 命中时递增 HitCount ---

func TestProcess_CheckBreakpoint_IncrementHitCount(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newBreakpointTestProcess(t, k)

	proc.AddBreakpoint(&Breakpoint{Type: BPReasoning, Enabled: true, Condition: &ReasoningCondition{}})

	ctx := BreakpointContext{BPType: BPReasoning}

	hit1 := proc.CheckBreakpoint(ctx)
	if hit1 == nil {
		t.Fatal("expected hit")
	}
	if hit1.HitCount != 1 {
		t.Errorf("HitCount = %d, want 1", hit1.HitCount)
	}

	hit2 := proc.CheckBreakpoint(ctx)
	if hit2 == nil {
		t.Fatal("expected hit")
	}
	if hit2.HitCount != 2 {
		t.Errorf("HitCount = %d, want 2", hit2.HitCount)
	}
}

// --- 13.2-UNIT-017: [P0] GdbPause 阻塞 / GdbResume 解除阻塞 ---

func TestProcess_GdbPause_GdbResume(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newBreakpointTestProcess(t, k)

	bp := &Breakpoint{ID: 1, Type: BPReasoning, Enabled: true}

	// GdbPause blocks, so run it in a goroutine
	pauseDone := make(chan struct{})
	go func() {
		proc.GdbPause("test pause", bp)
		close(pauseDone)
	}()

	// Wait for pause to take effect (event emitted to DebugChan)
	select {
	case <-proc.DebugChan:
		// GdbPause event received, process is now paused
	case <-time.After(2 * time.Second):
		t.Fatal("no GdbPause event received on DebugChan")
	}

	if !proc.IsGdbPaused() {
		t.Fatal("expected IsGdbPaused() == true after GdbPause")
	}

	ch := proc.GdbPauseCh()
	if ch == nil {
		t.Fatal("GdbPauseCh should return non-nil channel when paused")
	}

	// Verify channel blocks
	select {
	case <-ch:
		t.Fatal("channel should be blocking")
	default:
	}

	// Resume
	proc.GdbResume()

	select {
	case <-pauseDone:
		// GdbPause returned after resume
	case <-time.After(2 * time.Second):
		t.Fatal("GdbPause did not unblock after GdbResume")
	}

	if proc.IsGdbPaused() {
		t.Error("should not be gdb-paused after GdbResume")
	}
}

// --- 13.2-UNIT-018: [P1] GdbResume 未暂停时为 noop ---

func TestProcess_GdbResume_NotPaused(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newBreakpointTestProcess(t, k)

	// Should not panic
	proc.GdbResume()

	if proc.IsGdbPaused() {
		t.Error("should not be paused")
	}
}

// --- 13.2-UNIT-019: [P1] GdbPauseCh 未暂停时返回 nil ---

func TestProcess_GdbPauseCh_NilWhenNotPaused(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newBreakpointTestProcess(t, k)

	ch := proc.GdbPauseCh()
	if ch != nil {
		t.Error("GdbPauseCh should return nil when not paused")
	}
}

// --- 13.2-UNIT-020: [P0] GdbPause 与 Signal Pause 互不干扰 ---

func TestProcess_GdbPause_IndependentOfSignalPause(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newBreakpointTestProcess(t, k)

	// Signal pause
	proc.Pause()
	if !proc.IsPaused() {
		t.Fatal("should be signal-paused")
	}

	// GDB pause (blocks, so run in goroutine)
	pauseDone := make(chan struct{})
	go func() {
		proc.GdbPause("test", &Breakpoint{ID: 1, Type: BPReasoning})
		close(pauseDone)
	}()

	// Wait for GdbPause event
	select {
	case <-proc.DebugChan:
	case <-time.After(2 * time.Second):
		t.Fatal("no GdbPause event received")
	}

	if !proc.IsGdbPaused() {
		t.Fatal("should be gdb-paused")
	}

	// Resume signal — should NOT affect gdb pause
	proc.Resume()
	if proc.IsPaused() {
		t.Error("should not be signal-paused after Resume")
	}
	if !proc.IsGdbPaused() {
		t.Error("should still be gdb-paused after signal Resume")
	}

	// Resume gdb
	proc.GdbResume()

	select {
	case <-pauseDone:
	case <-time.After(2 * time.Second):
		t.Fatal("GdbPause did not unblock after GdbResume")
	}

	if proc.IsGdbPaused() {
		t.Error("should not be gdb-paused after GdbResume")
	}
}

// --- 13.2-UNIT-021: [P1] 并发安全测试 ---

func TestProcess_Breakpoint_Concurrent(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newBreakpointTestProcess(t, k)

	const n = 100
	var wg sync.WaitGroup

	for range n {
		wg.Go(func() {
			id := proc.AddBreakpoint(&Breakpoint{
				Type:      BPSyscall,
				Enabled:   true,
				Condition: &SyscallCondition{Name: "Read"},
			})
			_ = proc.ListBreakpoints()
			_ = proc.CheckBreakpoint(BreakpointContext{BPType: BPSyscall, SyscallName: "Read"})
			proc.RemoveBreakpoint(id)
		})
	}

	wg.Wait()
	// No race detected = pass (test runs with -race flag)
}

// --- 13.2-UNIT-022: [P0] GdbPause 发送 gdb_prompt 事件到 DebugChan ---

func TestProcess_GdbPause_EmitsDebugEvent(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newBreakpointTestProcess(t, k)

	bp := &Breakpoint{ID: 1, Type: BPSyscall, Enabled: true, Condition: &SyscallCondition{Name: "Read"}}

	// GdbPause in background (it blocks)
	go proc.GdbPause("syscall Read hit", bp)

	// Should receive a gdb_prompt event on DebugChan
	select {
	case ev := <-proc.DebugChan:
		if ev.Syscall != "GdbPause" {
			t.Errorf("Syscall = %q, want GdbPause", ev.Syscall)
		}
		if ev.Args["reason"] != "syscall Read hit" {
			t.Errorf("reason = %v, want %q", ev.Args["reason"], "syscall Read hit")
		}
		if ev.Args["bp_id"] != bp.ID {
			t.Errorf("bp_id = %v, want %d", ev.Args["bp_id"], bp.ID)
		}
		bpType, ok := ev.Args["bp_type"]
		if !ok || bpType != BPSyscall {
			t.Errorf("bp_type = %v, want BPSyscall", bpType)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no GdbPause event received on DebugChan")
	}

	// Cleanup: resume to unblock the goroutine
	proc.GdbResume()
}

// --- 13.2-UNIT-023: [P0] 无断点时 CheckBreakpoint O(1) 跳过 ---

func TestProcess_CheckBreakpoint_NoBreakpoints(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newBreakpointTestProcess(t, k)

	ctx := BreakpointContext{BPType: BPSyscall, SyscallName: "Read"}
	hit := proc.CheckBreakpoint(ctx)
	if hit != nil {
		t.Error("CheckBreakpoint should return nil when no breakpoints registered")
	}
}

// --- 13.2-UNIT-024: [P1] 删除后断点不再被 CheckBreakpoint 匹配 ---

func TestProcess_CheckBreakpoint_AfterRemove(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newBreakpointTestProcess(t, k)

	id := proc.AddBreakpoint(&Breakpoint{
		Type:      BPSyscall,
		Enabled:   true,
		Condition: &SyscallCondition{Name: "Read"},
	})

	// Verify it matches
	ctx := BreakpointContext{BPType: BPSyscall, SyscallName: "Read"}
	if proc.CheckBreakpoint(ctx) == nil {
		t.Fatal("should match before removal")
	}

	// Remove and verify no match
	proc.RemoveBreakpoint(id)
	if proc.CheckBreakpoint(ctx) != nil {
		t.Error("should not match after removal")
	}
}

// --- 13.2-UNIT-025: [P1] BreakpointContext 结构体字段完整 ---

func TestBreakpointContext_Fields(t *testing.T) {
	ctx := BreakpointContext{
		BPType:      BPQuality,
		SyscallName: "Read",
		SyscallArgs: map[string]any{"path": "/dev/llm/claude"},
		LLMResponse: "test content",
		TokensUsed:  500,
		StepNumber:  3,
	}
	if ctx.BPType != BPQuality {
		t.Errorf("BPType = %v, want BPQuality", ctx.BPType)
	}
	if ctx.SyscallName != "Read" {
		t.Errorf("SyscallName = %q, want %q", ctx.SyscallName, "Read")
	}
	if ctx.LLMResponse != "test content" {
		t.Errorf("LLMResponse = %q, want %q", ctx.LLMResponse, "test content")
	}
	if ctx.TokensUsed != 500 {
		t.Errorf("TokensUsed = %d, want 500", ctx.TokensUsed)
	}
	if ctx.StepNumber != 3 {
		t.Errorf("StepNumber = %d, want 3", ctx.StepNumber)
	}
}

// --- 13.2-UNIT-026: [P2] QualityMode 枚举值存在 ---

func TestQualityMode_Constants(t *testing.T) {
	if QualityModePattern == QualityModeEval {
		t.Error("QualityModePattern and QualityModeEval should be distinct")
	}
}

// --- 13.2-PERF-001: [P1] 断点触发延迟 <= 100ms (NFR31) ---

func TestProcess_CheckBreakpoint_PerformanceSub100ms(t *testing.T) {
	k := newSimpleKernel(t)
	proc := newBreakpointTestProcess(t, k)

	// Add 10 breakpoints (max expected in typical usage)
	for i := range 10 {
		proc.AddBreakpoint(&Breakpoint{
			Type:    BPSyscall,
			Enabled: true,
			Condition: &SyscallCondition{
				Name: "Syscall" + string(rune('A'+i)),
			},
		})
	}

	ctx := BreakpointContext{BPType: BPSyscall, SyscallName: "SyscallJ"}
	start := time.Now()
	hit := proc.CheckBreakpoint(ctx)
	elapsed := time.Since(start)

	if hit == nil {
		t.Fatal("should match last breakpoint")
	}
	if elapsed > 100*time.Millisecond {
		t.Errorf("CheckBreakpoint took %v, want <= 100ms (NFR31)", elapsed)
	}
}
