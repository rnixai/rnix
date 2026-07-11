package kernel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rnixai/rnix/internal/types"
)

// Story 67.1 — 内核死配额体系删除。
//
// 删除型 story 无新增生产符号，不适用「骨架 + t.Skip」模式。RED 机制是
// 源码内容扫描断言（[[atdd-doc-markdown-story-pattern]] 文件合规断言）：
// 测试读取 kernel 包 .go 生产源文件（排除 _test.go 与本 ATDD 文件本身，
// 避免自指命中），断言死符号零命中。删除前 FAIL（红），删除后 GREEN。
// 活体守卫从第一天起绿且保持绿（GREEN-stays-GREEN，不 skip）。

// deadKernelQuotaSymbols are identifiers whose presence in kernel package
// production source proves the dead BudgetPool 注册记账 / ProcGroup 管理 /
// slaResults 通道 still exists. Story 67.1 deletes all of them.
var deadKernelQuotaSymbols = []string{
	// BudgetPool 注册记账面（compose 绕开，budgetPools map 恒空）
	"RegisterBudgetPool",
	"UnregisterBudgetPool",
	"GetBudgetStatus",
	"budgetPools",
	// slaResults 通道（零生产写入，sla_status 永远返回空）
	"RecordSLAResult",
	"GetSLAResults",
	"slaResults",
	// ProcGroup 管理（组杀已由 66-5 OS pgid 承担）
	"ProcGroupManager",
	"procGroups",
	"JoinGroup",
	"SignalGroup",
	"removeFromAllGroups",
	"newProcGroup",
	"findGroupEventSource",
}

// TestATDD_67_1_DeadKernelQuotaSymbolsRemoved scans kernel package production
// .go source and fails if any dead symbol from the removed 死配额体系 remains.
func TestATDD_67_1_DeadKernelQuotaSymbolsRemoved(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob kernel sources: %v", err)
	}
	scanned := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue // exclude tests (and this ATDD file) — avoid self-hit
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		scanned++
		src := string(data)
		for _, sym := range deadKernelQuotaSymbols {
			if strings.Contains(src, sym) {
				t.Errorf("dead symbol %q still present in %s — Story 67.1 requires removal", sym, f)
			}
		}
	}
	if scanned == 0 {
		t.Fatal("scanned 0 kernel source files — glob or CWD wrong")
	}
}

// TestATDD_67_1_LiveComposeQuotaChainStillCallable is a GREEN-stays-GREEN guard:
// the BudgetPool 类型本体 (compose engine.go's private quota engine:
// NewBudgetPool → AllocateQuota → RecordUsage → GetStatus) must remain callable
// after the kernel 注册面 is deleted.
func TestATDD_67_1_LiveComposeQuotaChainStillCallable(t *testing.T) {
	pool := NewBudgetPool(1000)
	if pool == nil {
		t.Fatal("NewBudgetPool(1000) returned nil — live BudgetPool type broken")
	}
	quota := pool.AllocateQuota(types.PID(1), "agent-a", PriorityHigh)
	if quota <= 0 {
		t.Errorf("AllocateQuota returned %d, want > 0", quota)
	}
	if err := pool.RecordUsage(types.PID(1), 100); err != nil {
		t.Errorf("RecordUsage failed: %v", err)
	}
	status := pool.GetStatus()
	if status.TotalBudget != 1000 {
		t.Errorf("BudgetPool total budget = %d, want 1000", status.TotalBudget)
	}
}

// TestATDD_67_1_LiveSLAEvaluateStillCallable is a GREEN-stays-GREEN guard: the
// SLASpec.Evaluate 合约评估 (compose engine.go:262 → reputation 链) must remain
// callable after the kernel slaResults 通道 is deleted.
func TestATDD_67_1_LiveSLAEvaluateStillCallable(t *testing.T) {
	spec := SLASpec{MaxTokens: 100}
	result := spec.Evaluate("agent-a", 50, 1000, "")
	if result == nil {
		t.Fatal("SLASpec.Evaluate returned nil — live SLA type broken")
	}
	if !result.Passed {
		t.Errorf("expected SLA passed for tokensUsed=50 <= MaxTokens=100")
	}
}
