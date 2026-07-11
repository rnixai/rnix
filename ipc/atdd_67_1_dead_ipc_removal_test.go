package ipc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Story 67.1 — IPC 死端删除（budget_status + sla_status 全链）。
//
// 源码内容扫描断言（同 kernel/atdd_67_1_dead_quota_removal_test.go 机制）：
// 扫描 ipc/*.go 与 ipc/wire/*.go 生产源文件（排除 _test.go 与本 ATDD 文件），
// 断言 budget_status / sla_status 相关死符号零命中。删除前 FAIL，删除后 GREEN。
// budget_status 有 wire 双镜像（protocol.go + wire/protocol.go），两侧同步删。

// deadIPCQuotaTokens are tokens whose presence in ipc production source proves
// the dead budget_status / sla_status IPC 面 still exists. Story 67.1 removes them.
var deadIPCQuotaTokens = []string{
	// method 常量与线路串
	"budget_status",
	"sla_status",
	"MethodBudgetStatus",
	"MethodSLAStatus",
	// wire / protocol 类型（budget_status 有 wire 双镜像）
	"BudgetStatusRequest",
	"BudgetStatusResponse",
	"AgentQuotaWire",
	"SLAStatusRequest",
	"SLAStatusResponse",
	"SLAResultWire",
	// server handlers（Client.BudgetStatus/SLAStatus 零消费者一并删）
	"handleBudgetStatus",
	"handleSLAStatus",
}

// TestATDD_67_1_DeadIPCQuotaSymbolsRemoved scans ipc + ipc/wire production
// source and fails if any dead budget_status / sla_status token remains.
func TestATDD_67_1_DeadIPCQuotaSymbolsRemoved(t *testing.T) {
	top, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob ipc sources: %v", err)
	}
	wireFiles, err := filepath.Glob("wire/*.go")
	if err != nil {
		t.Fatalf("glob ipc/wire sources: %v", err)
	}
	files := append(top, wireFiles...)

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
		for _, tok := range deadIPCQuotaTokens {
			if strings.Contains(src, tok) {
				t.Errorf("dead IPC symbol %q still present in %s — Story 67.1 requires removal", tok, f)
			}
		}
	}
	if scanned == 0 {
		t.Fatal("scanned 0 ipc source files — glob or CWD wrong")
	}
}

// TestATDD_67_1_LiveIPCStatusNeighborsIntact is a GREEN-stays-GREEN guard: the
// sibling status methods (provider_status / reputation_status) that share
// server_status.go with the deleted budget/sla handlers must NOT be removed.
func TestATDD_67_1_LiveIPCStatusNeighborsIntact(t *testing.T) {
	if MethodProviderStatus != "provider_status" {
		t.Errorf("MethodProviderStatus = %q, want provider_status — live neighbor damaged", MethodProviderStatus)
	}
	if MethodReputationStatus != "reputation_status" {
		t.Errorf("MethodReputationStatus = %q, want reputation_status — live neighbor damaged", MethodReputationStatus)
	}
}
