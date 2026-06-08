package skills

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// ============================================================
// ATDD RED PHASE — Story 53.3: skill-authoring 命名规范 + 修内置 skill body
//
// 验证两个内置 skill 的 body / description 工具中立化（VFS 设备路径 →
// ToolDef.Name / 方法论描述），而 frontmatter allowed-tools（Layer 1 设备
// 权限）保持不变。规范依据 ADR Decision 44（53.2）Layer 1/Layer 2 双层原则
// + 铁律「skill 必须可移植，无 Rnix 专用豁免」。
//
// 关键技术杠杆：SkillInfo.Body 由 parseSKILLMD 剥离 frontmatter 后返回，故
// 断言 info.Body 不含 "/dev/" 会自动排除 allowed-tools 行——无需手动过滤。
//
// 测试分两类（详见 _bmad-output/test-artifacts/
//   atdd-checklist-53-3-skill-body-naming-fix-builtin.md）：
//   🔴 RED  (实现前失败，驱动开发): INT-001/002/003/004/005/007/008/012/013
//   🟢 护栏 (实现前已通过，锁定不变契约): INT-006/009/010/011
//
// RED → GREEN（dev-story 实现后转绿）：
//   - AC1: docs/skills.md 增「Skill body 工具引用规范」节 + 引用 Decision 44 +
//          厘清 :459 旧错误表述
//   - AC2: lib/skills/code-analysis/SKILL.md body 设备路径 → Read/Grep/Glob/Bash
//   - AC3: lib/skills/decompose/SKILL.md body 工具中立化 + description 去 /dev/intent
//   - 改完后 `go test -run TestATDD_53_3 ./skills/` 全绿
//
// 设计说明（不使用 t.Skip）：本项目 ATDD 惯例为直接生效的运行时断言（参见
// atdd_21_4_synergy_decl_test.go）；且护栏测试须始终运行以实时拦截范围红线违规
// （误改 allowed-tools / 替换独有工具名 / 删方法论）。所有断言均针对真实预期
// 行为，无占位断言。
// ============================================================

// 路径相对包目录（skills/）——与 atdd_21_4_synergy_decl_test.go:146 既有惯例一致。
const (
	atdd533LibSkillsDir = "../lib/skills"
	atdd533DocsSkillsMd = "../docs/skills.md"
)

// atdd533DeviceTokens 是 body / description 中禁止出现的 Layer 1 设备路径前缀。
// 这些路径只允许出现在 frontmatter 的 allowed-tools 行（不进入 SkillInfo.Body）。
var atdd533DeviceTokens = []string{"/dev/", "/mnt/mcp/"}

// atdd533LoadSkill 用真实搜索目录加载内置 skill（含 body）。
func atdd533LoadSkill(t *testing.T, name string) *SkillInfo {
	t.Helper()
	loader := NewSkillLoader([]string{atdd533LibSkillsDir})
	info, err := loader.LoadFull(name)
	if err != nil {
		t.Fatalf("LoadFull(%q) returned error: %v", name, err)
	}
	return info
}

// atdd533ReadDocs 读取 docs/skills.md 全文。
func atdd533ReadDocs(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(atdd533DocsSkillsMd)
	if err != nil {
		t.Fatalf("read %s: %v", atdd533DocsSkillsMd, err)
	}
	return string(data)
}

// --- 53.3-INT-001: [P2] AC1 docs/skills.md 含「Skill body 工具引用规范」节 ---
// 🔴 RED: 当前 docs/skills.md 无此节（仅有「跨工具共享的 skill 编写建议」:457）。

func TestATDD_53_3_AC1_DocsHasBodyConventionSection(t *testing.T) {
	content := atdd533ReadDocs(t)
	if !strings.Contains(content, "Skill body 工具引用规范") {
		t.Error("docs/skills.md missing 'Skill body 工具引用规范' section (AC1: 须新增 skill body 工具引用规范节)")
	}
}

// --- 53.3-INT-002: [P2] AC1 规范节显式引用 ADR Decision 44 ---
// 🔴 RED: 当前 docs/skills.md 无 Decision 44 引用（:459 误引 Decision 7）。

func TestATDD_53_3_AC1_DocsReferencesDecision44(t *testing.T) {
	content := atdd533ReadDocs(t)
	if !strings.Contains(content, "Decision 44") {
		t.Error("docs/skills.md missing 'Decision 44' reference (AC1: 须引用 ADR Decision 44 说明 Layer 1 设备路径 / Layer 2 ToolDef.Name)")
	}
}

// --- 53.3-INT-003: [P2] AC1 厘清 :459 矛盾表述（allowed-tools 非抽象标签） ---
// 🔴 RED: 当前 :459 含错误表述「allowed-tools 用规范的抽象标签...映射到 /dev/fs」。
// 厘清后该错误标志词「抽象标签」应消失（allowed-tools = Layer 1 设备路径）。

func TestATDD_53_3_AC1_DocsClarifiesAllowedToolsNotAbstractLabel(t *testing.T) {
	content := atdd533ReadDocs(t)
	if strings.Contains(content, "抽象标签") {
		t.Error("docs/skills.md still contains incorrect '抽象标签' wording (AC1: 须厘清 :459 —— allowed-tools 是 Layer 1 设备路径，非抽象标签，无 Read→/dev/fs 映射层)")
	}
}

// --- 53.3-INT-004: [P0] AC2 code-analysis body 无设备路径 ---
// 🔴 RED: 当前 body 含 /dev/fs(:21/28/46)、/dev/shell(:23/35)。

func TestATDD_53_3_AC2_CodeAnalysisBody_NoDevicePaths(t *testing.T) {
	info := atdd533LoadSkill(t, "code-analysis")
	for _, tok := range atdd533DeviceTokens {
		if strings.Contains(info.Body, tok) {
			t.Errorf("code-analysis body contains device path token %q (AC2: body 须用 ToolDef.Name 如 Read/Grep/Glob/Bash，不得用设备路径)", tok)
		}
	}
}

// --- 53.3-INT-005: [P1] AC2 code-analysis body 用 Bash 工具名（/dev/shell→Bash 正向验证）---
// 🔴 RED: 当前 body 用 /dev/shell 描述命令执行，无 "Bash" 工具名。

func TestATDD_53_3_AC2_CodeAnalysisBody_UsesBashToolName(t *testing.T) {
	info := atdd533LoadSkill(t, "code-analysis")
	if !strings.Contains(info.Body, "Bash") {
		t.Error("code-analysis body should reference 'Bash' tool name (AC2: /dev/shell 命令执行场景 → Bash)")
	}
}

// --- 53.3-INT-006: [P1] AC2 code-analysis allowed-tools 不变（范围红线护栏）---
// 🟢 护栏: 当前即绿。allowed-tools 是 Layer 1 设备权限，53.2 已定，改之破坏 enforcement。

func TestATDD_53_3_AC2_CodeAnalysisAllowedTools_Unchanged(t *testing.T) {
	info := atdd533LoadSkill(t, "code-analysis")
	got := info.Manifest.AllowedTools()
	want := []string{"/dev/fs", "/dev/shell"}
	if !slices.Equal(got, want) {
		t.Errorf("code-analysis allowed-tools = %v, want %v (范围红线: Layer 1 设备路径必须保持不变，本 story 只改 body)", got, want)
	}
}

// --- 53.3-INT-007: [P0] AC3 decompose body 无设备路径 ---
// 🔴 RED: 当前 body 含 /dev/intent/*(:50/62/69/76/85/87/88/89)。

func TestATDD_53_3_AC3_DecomposeBody_NoDevicePaths(t *testing.T) {
	info := atdd533LoadSkill(t, "decompose")
	for _, tok := range atdd533DeviceTokens {
		if strings.Contains(info.Body, tok) {
			t.Errorf("decompose body contains device path token %q (AC3: body 须工具中立化，删工具使用指南节 + 重写工作流程为方法论)", tok)
		}
	}
}

// --- 53.3-INT-008: [P1] AC3 decompose description 无设备路径 ---
// 🔴 RED: 当前 description(:4) 含「通过 /dev/intent 设备进行意图分解」。

func TestATDD_53_3_AC3_DecomposeDescription_NoDevicePaths(t *testing.T) {
	info := atdd533LoadSkill(t, "decompose")
	for _, tok := range atdd533DeviceTokens {
		if strings.Contains(info.Manifest.Description, tok) {
			t.Errorf("decompose description contains device path token %q (AC3: description 是 LLM 可见的匹配文本，须去除 /dev/intent 设备引用改为方法论描述)", tok)
		}
	}
}

// --- 53.3-INT-009: [P1] AC3 decompose body 不点名 intent_* 独有工具名（铁律③护栏）---
// 🟢 护栏: 当前即绿（body 用 /dev/intent/* 设备路径，无下划线工具名）。
// 防「好心办坏事」：dev 不得把删掉的设备路径替换成 intent_decompose 等 Rnix 独有名。

func TestATDD_53_3_AC3_DecomposeBody_NoUniqueIntentToolNames(t *testing.T) {
	info := atdd533LoadSkill(t, "decompose")
	forbidden := []string{"intent_decompose", "intent_confirm", "intent_execute", "intent_status"}
	for _, name := range forbidden {
		if strings.Contains(info.Body, name) {
			t.Errorf("decompose body contains Rnix-unique tool name %q (铁律③: body 须工具中立，禁止硬绑 intent_* 独有工具名——改为方法论描述)", name)
		}
	}
}

// --- 53.3-INT-010: [P1] AC3 decompose body 保留可迁移方法论锚点（防删过头护栏）---
// 🟢 护栏: 当前即绿。分解规则 / DAG / depends_on 是工具中立的领域方法论，须保留。

func TestATDD_53_3_AC3_DecomposeBody_PreservesMethodology(t *testing.T) {
	info := atdd533LoadSkill(t, "decompose")
	anchors := []string{"depends_on", "DAG", "分解规则"}
	for _, a := range anchors {
		if !strings.Contains(info.Body, a) {
			t.Errorf("decompose body missing methodology anchor %q (AC3: 须保留可迁移方法论——分解规则/DAG/增量更新，不得删过头)", a)
		}
	}
}

// --- 53.3-INT-011: [P1] AC3 decompose allowed-tools 不变（范围红线护栏）---
// 🟢 护栏: 当前即绿。

func TestATDD_53_3_AC3_DecomposeAllowedTools_Unchanged(t *testing.T) {
	info := atdd533LoadSkill(t, "decompose")
	got := info.Manifest.AllowedTools()
	want := []string{"/dev/intent", "/dev/shell", "/dev/fs"}
	if !slices.Equal(got, want) {
		t.Errorf("decompose allowed-tools = %v, want %v (范围红线: Layer 1 设备路径必须保持不变，本 story 只改 body + description)", got, want)
	}
}

// --- 53.3-INT-012: [P0] AC4 全量内置 skill 的 body + description 工具中立 ---
// 🔴 RED: 当前 code-analysis + decompose 的 body（及 decompose description）含设备路径。
// 遍历 lib/skills 所有 skill，结构化断言 body 与 description 均无设备路径。

func TestATDD_53_3_AC4_AllBuiltinSkills_BodyAndDescriptionToolNeutral(t *testing.T) {
	loader := NewSkillLoader([]string{atdd533LibSkillsDir})
	entries, err := os.ReadDir(atdd533LibSkillsDir)
	if err != nil {
		t.Fatalf("read dir %s: %v", atdd533LibSkillsDir, err)
	}

	checked := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// 仅处理真正的 skill 目录（含 SKILL.md），跳过 .registry.yaml 等。
		if _, statErr := os.Stat(filepath.Join(atdd533LibSkillsDir, e.Name(), "SKILL.md")); statErr != nil {
			continue
		}
		info, loadErr := loader.LoadFull(e.Name())
		if loadErr != nil {
			t.Errorf("LoadFull(%q) error: %v", e.Name(), loadErr)
			continue
		}
		checked++
		for _, tok := range atdd533DeviceTokens {
			if strings.Contains(info.Body, tok) {
				t.Errorf("skill %q body contains device path token %q (AC4: 所有内置 skill body 须工具中立)", e.Name(), tok)
			}
			if strings.Contains(info.Manifest.Description, tok) {
				t.Errorf("skill %q description contains device path token %q (AC4: 所有内置 skill description 须工具中立)", e.Name(), tok)
			}
		}
	}
	if checked == 0 {
		t.Fatalf("no builtin skills with SKILL.md found under %s — search dir may be wrong", atdd533LibSkillsDir)
	}
}

// --- 53.3-INT-013: [P0] AC4 命令级 grep 合规：设备路径仅出现在 allowed-tools 行 ---
// 🔴 RED: 当前多行 body / description 含设备路径。
// 逐行读 SKILL.md 原文，精确等价 story AC4 验收：
//   `grep -rn "/dev/" lib/skills/*/SKILL.md` 仅剩 frontmatter allowed-tools 行命中。

func TestATDD_53_3_AC4_GrepCompliance_DevicePathsOnlyInAllowedTools(t *testing.T) {
	entries, err := os.ReadDir(atdd533LibSkillsDir)
	if err != nil {
		t.Fatalf("read dir %s: %v", atdd533LibSkillsDir, err)
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		skillMd := filepath.Join(atdd533LibSkillsDir, e.Name(), "SKILL.md")
		data, statErr := os.ReadFile(skillMd)
		if statErr != nil {
			continue // 非 skill 目录
		}
		for i, line := range strings.Split(string(data), "\n") {
			for _, tok := range atdd533DeviceTokens {
				if !strings.Contains(line, tok) {
					continue
				}
				if !strings.HasPrefix(strings.TrimSpace(line), "allowed-tools:") {
					t.Errorf("%s:%d device path %q appears outside allowed-tools line: %q\n  (AC4: 设备路径只允许出现在 frontmatter allowed-tools 行，body/description 须工具中立)",
						skillMd, i+1, tok, strings.TrimSpace(line))
				}
			}
		}
	}
}
