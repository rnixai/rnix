package rnix

import (
	"os"
	"strings"
	"testing"
)

// ============================================================
// ATDD RED PHASE — Story 26.5: 文档更新——统一推理循环
//
// Tests verify that documentation files have been updated to
// replace OODA references with Unified Reasoning Loop content.
//
// All tests will FAIL until the documentation is updated
// (Tasks 1–8 in the story spec).
//
// RED → GREEN: Apply documentation changes per story spec.
// ============================================================

func readDocFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	return string(data)
}

func assertContains(t *testing.T, content, substr, msg string) {
	t.Helper()
	if !strings.Contains(content, substr) {
		t.Errorf("%s: expected to find %q", msg, substr)
	}
}

func assertNotContains(t *testing.T, content, substr, msg string) {
	t.Helper()
	if strings.Contains(content, substr) {
		t.Errorf("%s: should NOT contain %q", msg, substr)
	}
}

// --- AC-1: PRD 功能需求更新 [P0] ---

func TestDoc_PRD_FR_UnifiedReasoningLoop(t *testing.T) {
	content := readDocFile(t, "_bmad-output/planning-artifacts/prd/functional-requirements.md")

	assertContains(t, content,
		"## Unified Reasoning Loop（统一推理循环，Phase 3）",
		"[AC-1] FR section heading must be updated")

	assertNotContains(t, content,
		"## OODA Autonomous Decision（OODA 自主决策，Phase 3）",
		"[AC-1] old OODA section heading must be removed")

	assertContains(t, content,
		"FR112:** 系统提供统一推理循环，LLM 每步自主决策行为类型",
		"[AC-1] FR112 must describe unified reasoning loop")

	assertContains(t, content,
		"FR113:** 统一推理循环每步仅调用一次 LLM",
		"[AC-1] FR113 must describe single LLM call per step")

	assertContains(t, content,
		"FR114:** 系统提供 planning 配置开关",
		"[AC-1] FR114 must describe planning config switch")

	assertContains(t, content,
		"FR115:** 统一推理循环内置熔断机制",
		"[AC-1] FR115 must describe circuit breaker")

	assertContains(t, content,
		"FR116:** 统一推理循环中工具调用错误必须以 tool message 格式注入",
		"[AC-1] FR116 must describe tool error injection")

	assertContains(t, content,
		"FR117:** 统一推理循环中智能体可自主决定 spawn 子智能体",
		"[AC-1] FR117 must describe autonomous spawn")

	assertContains(t, content,
		"最基础的推理能力和统一推理循环",
		"[AC-1] FR118 must reference unified reasoning loop instead of OODA")

	assertNotContains(t, content,
		"最基础的推理能力和 OODA 循环",
		"[AC-1] FR118 old OODA reference must be removed")
}

// --- AC-2: PRD 非功能需求更新 [P0] ---

func TestDoc_PRD_NFR44_Updated(t *testing.T) {
	content := readDocFile(t, "_bmad-output/planning-artifacts/prd/non-functional-requirements.md")

	assertContains(t, content,
		"NFR44:** 统一推理循环单步框架开销（不含 LLM 调用时间）≤ 50ms",
		"[AC-2] NFR44 must describe unified reasoning loop with ≤50ms")

	assertNotContains(t, content,
		"OODA 单轮循环（Observe→Orient→Decide→Act）",
		"[AC-2] old OODA NFR44 text must be removed")
}

// --- AC-3: PRD 项目范围更新 [P1] ---

func TestDoc_PRD_Scoping_Phase3(t *testing.T) {
	content := readDocFile(t, "_bmad-output/planning-artifacts/prd/project-scoping-phased-development.md")

	assertContains(t, content,
		"统一推理循环",
		"[AC-3] Phase 3 table must contain unified reasoning loop")

	assertContains(t, content,
		"单一 reasonStep 循环",
		"[AC-3] Phase 3 description must mention single reasonStep")

	assertNotContains(t, content,
		"OODA 自主决策",
		"[AC-3] old OODA entry must be removed from Phase 3 table")

	assertNotContains(t, content,
		"Observe/Orient/Decide/Act 循环",
		"[AC-3] old OODA description must be removed")
}

// --- AC-4: PRD 索引更新 [P1] ---

func TestDoc_PRD_Index_TOCLink(t *testing.T) {
	content := readDocFile(t, "_bmad-output/planning-artifacts/prd/index.md")

	assertContains(t, content,
		"Unified Reasoning Loop（统一推理循环，Phase 3）",
		"[AC-4] TOC link text must be updated")

	assertContains(t, content,
		"#unified-reasoning-loop",
		"[AC-4] TOC anchor must use new heading slug")

	assertNotContains(t, content,
		"OODA Autonomous Decision（OODA 自主决策，Phase 3）",
		"[AC-4] old OODA TOC entry must be removed")
}

// --- AC-5: 架构决策文档——新增 Decision 23 [P0] ---

func TestDoc_Arch_Decision23(t *testing.T) {
	content := readDocFile(t, "_bmad-output/planning-artifacts/architecture/core-architectural-decisions.md")

	assertContains(t, content,
		"Decision 23: 统一推理循环",
		"[AC-5] Decision 23 heading must exist")

	assertContains(t, content,
		"废弃 linear/OODA 双推理模式，统一为单一 reasonStep 循环",
		"[AC-5] Decision 23 must describe the unified approach")

	assertContains(t, content,
		"ActionType 枚举（7 种）",
		"[AC-5] Decision 23 must include ActionType enum table")

	for _, at := range []string{"text", "tool_call", "plan", "spawn", "complete", "replan", "specialize"} {
		assertContains(t, content, "`"+at+"`",
			"[AC-5] Decision 23 must list ActionType: "+at)
	}

	assertContains(t, content,
		"内置安全机制",
		"[AC-5] Decision 23 must describe safety mechanisms")

	assertContains(t, content,
		"连续 3 次 tool_call/spawn 失败触发熔断退出",
		"[AC-5] Decision 23 must describe circuit breaker rule")
}

// --- AC-6: 架构验证结果更新 [P1] ---

func TestDoc_Arch_ValidationResults_NoOODA(t *testing.T) {
	content := readDocFile(t, "_bmad-output/planning-artifacts/architecture/architecture-validation-results.md")

	assertContains(t, content,
		"统一推理循环（Epic 26 已实现）",
		"[AC-6] validation results must reference unified reasoning loop")

	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "ooda") &&
			!strings.Contains(lower, "统一推理循环") {
			t.Errorf("[AC-6] line %d still contains bare OODA reference: %s", i+1, strings.TrimSpace(line))
		}
	}
}

// --- AC-7: project-context.md 推理循环段落重写 [P0] ---

func TestDoc_ProjectContext_ReasoningLoop(t *testing.T) {
	content := readDocFile(t, "_bmad-output/project-context.md")

	assertContains(t, content,
		"#### 统一推理循环",
		"[AC-7] section heading must be updated to unified reasoning loop")

	assertNotContains(t, content,
		"两种推理模式通过 `SpawnOpts.ReasoningMode` 选择",
		"[AC-7] old dual-mode description must be removed")

	assertNotContains(t, content,
		"`\"ooda\"` (OODA 模式)",
		"[AC-7] old OODA mode description must be removed")

	for _, at := range []string{"tool_call", "plan", "spawn", "specialize", "replan", "complete", "text"} {
		assertContains(t, content, "`"+at+"`",
			"[AC-7] reasoning loop section must list ActionType: "+at)
	}

	assertContains(t, content,
		"planning: true|false",
		"[AC-7] must describe planning config switch")

	assertContains(t, content,
		"连续 3 次 tool_call/spawn 失败触发熔断退出",
		"[AC-7] must describe circuit breaker mechanism")

	assertNotContains(t, content,
		"OODA specialize action",
		"[AC-7] 'OODA specialize action' in context propagation must be updated")

	assertNotContains(t, content,
		"ooda.go",
		"[AC-7] ooda.go must be removed from file listing")
}

// --- AC-8: CLAUDE.md 架构描述更新 [P1] ---

func TestDoc_CLAUDE_MD_UnifiedLoop(t *testing.T) {
	content := readDocFile(t, "CLAUDE.md")

	assertContains(t, content,
		"Unified Reasoning Loop",
		"[AC-8] CLAUDE.md must mention Unified Reasoning Loop")

	assertContains(t, content,
		"reasonStep",
		"[AC-8] CLAUDE.md must mention reasonStep")

	assertNotContains(t, content,
		"OODA",
		"[AC-8] CLAUDE.md must not contain OODA references")

	assertNotContains(t, content,
		"ooda",
		"[AC-8] CLAUDE.md must not contain ooda references")
}
