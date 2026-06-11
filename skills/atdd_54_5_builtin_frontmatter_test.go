package skills

import (
	"os"
	"slices"
	"strings"
	"testing"
)

// ATDD Story 54.5 — 三个内置 skill frontmatter allowed-tools 语义工具名化 +
// docs/skills.md 推翻旧 Decision 44 论述 + skills/types.go 注释同步。
//
// 覆盖 AC4（code-analysis）、AC5（decompose）、AC6（using-rnix）、AC7（docs + types.go）。
// kernel 声明值归一化机制见 kernel/atdd_54_5_skill_frontmatter_normalize_test.go。
//
// ── RED 形态：t.Skip（Decker 2026-06-11 拍板，混合 story 统一全绿提交）──────────
// 内容断言本可直接运行时失败（53.3 模式），但 54.5 是 kernel+文本混合 story；为与
// kernel 主题统一并保 ATDD 提交期 `make all` 全绿，RED 用例标 t.Skip；dev-story 改
// frontmatter / docs / types.go 后移除 Skip 验 RED→GREEN。
// 唯一例外：AC7b「docs 保留节」是 GREEN 护栏（防 dev 改 docs 时误删 body 五原则 /
// using-rnix 切分节），当前即绿、不 skip、实时拦截。
//
// ⚠️ dev-story 注意（ATDD 不碰已有回归测试，此处仅上报）：改 frontmatter 后须同步
// 更新 skills/atdd_53_3_skill_body_naming_test.go 的 INT-006 / INT-011（断言
// allowed-tools == 旧设备路径值的 GREEN 护栏），否则本包红灯。详见 atdd-checklist Step 5。

const (
	atdd545LibSkillsDir = "../lib/skills"
	atdd545DocsSkillsMd = "../docs/skills.md"
	atdd545TypesGo      = "types.go" // 同包，相对包目录 skills/
)

func atdd545LoadSkill(t *testing.T, name string) *SkillInfo {
	t.Helper()
	loader := NewSkillLoader([]string{atdd545LibSkillsDir})
	info, err := loader.LoadFull(name)
	if err != nil {
		t.Fatalf("LoadFull(%q): %v", name, err)
	}
	return info
}

func atdd545ReadFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// assertAllowedTools 断言 allowed-tools 集合相等（顺序无关）且不含设备路径 token。
func assertAllowedTools(t *testing.T, skill string, got, want []string) {
	t.Helper()
	gotSorted := slices.Clone(got)
	wantSorted := slices.Clone(want)
	slices.Sort(gotSorted)
	slices.Sort(wantSorted)
	if !slices.Equal(gotSorted, wantSorted) {
		t.Errorf("%s allowed-tools = %v, want %v（语义工具名集合）", skill, got, want)
	}
	for _, tok := range got {
		if strings.HasPrefix(tok, "/dev/") || strings.HasPrefix(tok, "/mnt/mcp/") {
			t.Errorf("%s allowed-tools 含设备路径 %q（54.5 须语义工具名，设备路径已内化）", skill, tok)
		}
	}
}

// 54.5-UNIT-001 [RED] AC4：code-analysis frontmatter allowed-tools = [Read Write Edit Glob Grep Bash]。
func TestATDD_54_5_AC4_CodeAnalysisFrontmatter_ToolNames(t *testing.T) {
	info := atdd545LoadSkill(t, "code-analysis")
	assertAllowedTools(t, "code-analysis", info.Manifest.AllowedTools(),
		[]string{"Read", "Write", "Edit", "Glob", "Grep", "Bash"})
}

// 54.5-UNIT-002 [RED] AC5：decompose frontmatter allowed-tools = intent 四工具 + Bash + fs 全集。
func TestATDD_54_5_AC5_DecomposeFrontmatter_ToolNames(t *testing.T) {
	info := atdd545LoadSkill(t, "decompose")
	assertAllowedTools(t, "decompose", info.Manifest.AllowedTools(),
		[]string{"IntentDecompose", "IntentConfirm", "IntentExecute", "IntentStatus", "Bash", "Read", "Write", "Edit", "Glob", "Grep"})
}

// 54.5-UNIT-003 [RED] AC6：using-rnix frontmatter allowed-tools = [Bash]。
func TestATDD_54_5_AC6_UsingRnixFrontmatter_ToolName(t *testing.T) {
	info := atdd545LoadSkill(t, "using-rnix")
	assertAllowedTools(t, "using-rnix", info.Manifest.AllowedTools(), []string{"Bash"})
}

// 54.5-UNIT-004 [RED] AC4/5/6 grep 合规：三个内置 skill 的 allowed-tools 行不含设备路径 token。
// 精确等价 story 验收：`grep "/dev/" lib/skills/{code-analysis,decompose,using-rnix}/SKILL.md`
// 在 allowed-tools 行零命中（设备路径已全部语义名化）。
func TestATDD_54_5_AC456_FrontmatterAllowedToolsLine_NoDevicePaths(t *testing.T) {
	for _, name := range []string{"code-analysis", "decompose", "using-rnix"} {
		data := atdd545ReadFile(t, atdd545LibSkillsDir+"/"+name+"/SKILL.md")
		sawAllowedTools := false
		for line := range strings.SplitSeq(data, "\n") {
			trimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(trimmed, "allowed-tools:") {
				continue
			}
			sawAllowedTools = true
			for _, tok := range []string{"/dev/", "/mnt/mcp/"} {
				if strings.Contains(trimmed, tok) {
					t.Errorf("%s allowed-tools 行含设备路径 %q: %q（54.5 须语义工具名）", name, tok, trimmed)
				}
			}
		}
		if !sawAllowedTools {
			t.Errorf("%s SKILL.md 未找到 allowed-tools 行（夹具/搜索目录可能错）", name)
		}
	}
}

// 54.5-UNIT-005 [RED] AC7a：docs/skills.md 推翻旧 Decision 44 论述、立 Decision 45 语义名立场。
func TestATDD_54_5_AC7_DocsAdoptsDecision45(t *testing.T) {
	content := atdd545ReadFile(t, atdd545DocsSkillsMd)
	// 旧 Decision 44 论断须消失（baseline 501cee6 的 :462 表述）。
	stale := []string{
		"allowed-tools` 的值是 Layer 1 资源路径，不是工具语义名",
		"写成 `Read` 会被直接拒绝",
	}
	for _, s := range stale {
		if strings.Contains(content, s) {
			t.Errorf("docs/skills.md 仍含旧 Decision 44 论断 %q（54.5 须改写为 Decision 45 语义名立场）", s)
		}
	}
	// 新立场须引用 Decision 45。
	if !strings.Contains(content, "Decision 45") {
		t.Error("docs/skills.md 缺 Decision 45 引用（AC7：allowed-tools 语义名化 + 工具级 enforcement）")
	}
}

// 54.5-UNIT-006 [RED] AC7c：skills/types.go AllowedTools() 注释从「device paths」改为「语义工具名」。
func TestATDD_54_5_AC7_TypesGoCommentSemanticToolNames(t *testing.T) {
	content := atdd545ReadFile(t, atdd545TypesGo)
	if strings.Contains(content, "allowed tool device paths") {
		t.Error(`types.go AllowedTools() 注释仍为 "allowed tool device paths"（AC7c：须改为语义工具名）`)
	}
}

// 54.5-UNIT-007 GREEN 护栏 AC7b：docs/skills.md 改写时须保留 body 工具引用规范节（53.3）+
// using-rnix 设备路径切分节（53.4）——与本 story 正交，不得删。当前即绿，不 skip，实时拦截。
func TestATDD_54_5_AC7_DocsPreservesBodyConventions_GreenGuard(t *testing.T) {
	content := atdd545ReadFile(t, atdd545DocsSkillsMd)
	for _, s := range []string{
		"Skill body 工具引用规范", // 53.3 body 五原则节标题
		"设备路径切分",             // 53.4 using-rnix 切分说明
	} {
		if !strings.Contains(content, s) {
			t.Errorf("docs/skills.md 缺应保留章节锚点 %q（AC7：改写 allowed-tools 论述时不得删 body 五原则 / 切分说明）", s)
		}
	}
}
