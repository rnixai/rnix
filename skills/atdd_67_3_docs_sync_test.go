package skills

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// ============================================================
// ATDD — Story 67.3: 正名批次收尾与文档同步
//
// 纯文档 story（机制 = atdd-doc-markdown-story-pattern）：测试读 living doc
// 文本，断言 67-1/67-2 删除的死符号在文档中零残留，且三处正名（SLA / init /
// 录制回放）的真实语义标志词已落地。改写前红（死符号在场 / 标志词缺失），
// 改写后绿；删除型/文档型无新增生产符号，不适用「骨架 + t.Skip」，同 67-1/67-2。
//
// 🔑 白名单式扫描（防 ATDD 自指）：测试只 os.ReadFile 显式列出的具体 living
// doc 文件，绝不 glob *.go——本测试文件自身含死符号字面量（"ProcGroupManager"
// 等作为断言搜索目标），glob 会误扫自身产生假红。根级文档用 runtime.Caller(0)
// 上溯仓库根定位，不依赖 go test 运行时 CWD（虽稳定为包目录，显式更稳健）。
//
// ⚠️ 纯文档 AC 只能近似断言（死符号零命中 + 正名标志词存在 + 节存在）；措辞
// 语义正确性需 dev 自查 + code-review 人工复核为准（纯文档 story 固有局限）。
// 活体守卫（immuneDaemon 字段行、README Token Economy 节）从第一天起绿且保持
// 绿——防改写误删整节（GREEN-stays-GREEN，不 skip）。
// ============================================================

// atdd673RepoRoot 用 runtime.Caller(0) 定位本测试文件，上溯两级到仓库根：
// <root>/skills/atdd_67_3_docs_sync_test.go → <root>/skills → <root>。
func atdd673RepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed — 无法定位仓库根")
	}
	return filepath.Dir(filepath.Dir(file))
}

// atdd673ReadDoc 读取仓库根相对路径的 living doc；缺文件即 Fatal（story 前提破坏）。
func atdd673ReadDoc(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(atdd673RepoRoot(t), rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}

// --- 67.3-AC5: CLAUDE.md 死符号零残留（ProcGroupManager / Team → ProcGroup / drift detection）---
func TestATDD_67_3_CLAUDEMd_NoDeadSymbols(t *testing.T) {
	content := atdd673ReadDoc(t, "CLAUDE.md")
	dead := []string{"ProcGroupManager", "Team → ProcGroup", "drift detection"}
	for _, sym := range dead {
		if strings.Contains(content, sym) {
			t.Errorf("CLAUDE.md 仍含死符号 %q（67-1/67-2 已删对应体系，AC5 要求正名）", sym)
		}
	}
}

// --- 67.3-AC4: project-context.md 死符号零残留（67-1 删除面同步）---
func TestATDD_67_3_ProjectContext_NoDeadSymbols(t *testing.T) {
	content := atdd673ReadDoc(t, "_bmad-output/project-context.md")
	dead := []string{"budget_status", "sla_status", "procGroups", "slaResults", "types.PGID"}
	for _, sym := range dead {
		if strings.Contains(content, sym) {
			t.Errorf("project-context.md 仍含死符号 %q（67-1 删除面，AC4 要求同步）", sym)
		}
	}
}

// --- 67.3-AC6: architecture.md 死方法名零残留（budget_status / sla_status 已随 67-1 删除）---
func TestATDD_67_3_ArchitectureMd_NoDeadMethods(t *testing.T) {
	content := atdd673ReadDoc(t, "lib/skills/using-rnix/references/architecture.md")
	dead := []string{"budget_status", "sla_status"}
	for _, sym := range dead {
		if strings.Contains(content, sym) {
			t.Errorf("architecture.md 仍含死方法名 %q（AC6 要求删该行两死方法名，其余活体方法保留）", sym)
		}
	}
}

// --- 67.3-AC6: .github/copilot-instructions.md 死符号零残留（ProcGroupManager）---
func TestATDD_67_3_CopilotInstructions_NoDeadSymbols(t *testing.T) {
	content := atdd673ReadDoc(t, ".github/copilot-instructions.md")
	if strings.Contains(content, "ProcGroupManager") {
		t.Error(".github/copilot-instructions.md 仍含死符号 ProcGroupManager（AC6，与 CLAUDE.md:87 同款）")
	}
}

// --- 67.3-AC1: kernel/sla.go 注释正名为合约评估（事后信誉输入）标志词 ---
// 近似断言：正名后 doc comment 应出现事后评估 / 信誉语义标志词之一。
func TestATDD_67_3_SLAGo_PostHocNaming(t *testing.T) {
	content := atdd673ReadDoc(t, "kernel/sla.go")
	markers := []string{"post-hoc", "reputation", "Reputation"}
	for _, m := range markers {
		if strings.Contains(content, m) {
			return // 命中任一即正名标志词落地
		}
	}
	t.Errorf("kernel/sla.go 注释未含事后/信誉语义标志词 %v（AC1: SLA 正名为合约评估-事后信誉输入，不终止不 gate）", markers)
}

// --- 67.3-AC2: project-context.md init 正名 PID 0（孤儿节不再含与实现矛盾的 PID=1）---
func TestATDD_67_3_ProjectContext_InitPID0(t *testing.T) {
	content := atdd673ReadDoc(t, "_bmad-output/project-context.md")
	if strings.Contains(content, "PID=1") {
		t.Error("project-context.md 仍含 'PID=1'（AC2: 与实现直接矛盾，代码 reparent 到 PID 0）")
	}
	if !strings.Contains(content, "PID 0") {
		t.Error("project-context.md 未含 'PID 0'（AC2: init 正名为 PID 0 被动哨兵语义）")
	}
}

// --- 67.3 活体守卫（GREEN-stays-GREEN，不 skip）：改写不得误删活体字段 / 整节 ---
func TestATDD_67_3_LiveGuards(t *testing.T) {
	pctx := atdd673ReadDoc(t, "_bmad-output/project-context.md")
	if !strings.Contains(pctx, "immuneDaemon") {
		t.Error("project-context.md 缺活体字段 immuneDaemon（护栏: 删死字段行不应波及活体字段）")
	}
	readme := atdd673ReadDoc(t, "README.md")
	if !strings.Contains(readme, "Token Economy") {
		t.Error("README.md 缺 Token Economy 节（护栏: Token Economy 行据实改写但保留节标题）")
	}
}
