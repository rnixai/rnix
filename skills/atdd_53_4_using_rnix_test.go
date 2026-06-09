package skills

import (
	"slices"
	"strings"
	"testing"
)

// ============================================================
// ATDD — Story 53.4: rnix 能力与使用指南 skill（using-rnix）
//
// 验证新建的 using-rnix 能力 skill 可加载、frontmatter 合规（仅
// agentskills.io 标准字段，无 rnix-only 扩展）、body 覆盖 rnix 能力维度，
// 且 SKILL.md body 不含设备路径。
//
// 🔑 设备路径切分（本 story 独有）：本 skill 的主题是「文档化 rnix（含其
// VFS 设备模型）」，设备路径作为被描述的架构事实合法存在——但仅在
// references/architecture.md（不被 53.3 INT-012/INT-013 扫描）。SKILL.md
// body 本身严格无设备路径：既守 53.3 的 blanket grep 闸，又满足 53.4 AC3
// 「教调用工具用 Bash，描述架构的设备路径下沉 references」的切分。
//
// 这些是 GREEN-stays-GREEN 护栏：skill 文件已就位，测试实时锁定其可发现性
// + 可移植性范围红线，防后续 drift。语义正确性（命令无杜撰 / 外部 agent 能
// 驱动 rnix）以 skill-creator eval + 人工复核为准（纯文档 skill 固有局限，
// 对齐 MEMORY atdd-doc-markdown-story-pattern）。
// ============================================================

const atdd534UsingRnixName = "using-rnix"

func atdd534Load(t *testing.T) *SkillInfo {
	t.Helper()
	loader := NewSkillLoader([]string{"../lib/skills"})
	info, err := loader.LoadFull(atdd534UsingRnixName)
	if err != nil {
		t.Fatalf("LoadFull(%q) error: %v", atdd534UsingRnixName, err)
	}
	return info
}

func atdd534DevicePathTokens() []string {
	return []string{"/dev/", "/mnt/mcp/", "/proc/"}
}

// --- 53.4-AC1: skill 可被 SkillLoader.LoadFull 加载，Name == "using-rnix" ---
func TestATDD_53_4_AC1_UsingRnix_Loadable(t *testing.T) {
	info := atdd534Load(t)
	if info.Manifest.Name != atdd534UsingRnixName {
		t.Errorf("Manifest.Name = %q, want %q", info.Manifest.Name, atdd534UsingRnixName)
	}
	if strings.TrimSpace(info.Manifest.Description) == "" {
		t.Error("Manifest.Description is empty (AC1: description 是触发主力，不能为空)")
	}
}

// --- 53.4-AC1: allowed-tools 仅 /dev/shell（Layer 1 设备路径，驱动 rnix 经 shell 跑 CLI）---
func TestATDD_53_4_AC1_UsingRnix_AllowedToolsIsDevShell(t *testing.T) {
	info := atdd534Load(t)
	got := info.Manifest.AllowedTools()
	want := []string{"/dev/shell"}
	if !slices.Equal(got, want) {
		t.Errorf("allowed-tools = %v, want %v (AC1: 驱动 rnix = 经 shell 跑 rnix CLI)", got, want)
	}
}

// --- 53.4-AC3: frontmatter 无 rnix-only 扩展字段（synergy/search_hint），保跨平台可移植 ---
func TestATDD_53_4_AC3_UsingRnix_NoRnixOnlyFrontmatter(t *testing.T) {
	info := atdd534Load(t)
	if len(info.Manifest.Synergies) != 0 {
		t.Errorf("Synergies = %v, want empty (AC3: 可移植 skill 不用 rnix-only synergy 字段)", info.Manifest.Synergies)
	}
	if strings.TrimSpace(info.Manifest.SearchHint) != "" {
		t.Errorf("SearchHint = %q, want empty (AC3: 可移植 skill 不用 rnix-only search_hint 字段)", info.Manifest.SearchHint)
	}
}

// --- 53.4-AC1: Body 排除 frontmatter（parseSKILLMD 剥离 allowed-tools 等行）---
func TestATDD_53_4_AC1_UsingRnix_BodyExcludesFrontmatter(t *testing.T) {
	info := atdd534Load(t)
	if strings.Contains(info.Body, "allowed-tools:") {
		t.Error("Body contains 'allowed-tools:' — frontmatter not stripped (AC1: Body 须自动排除 frontmatter)")
	}
	if !strings.HasPrefix(strings.TrimSpace(info.Body), "#") {
		t.Errorf("Body should start with a markdown heading after frontmatter strip, got: %.40q", info.Body)
	}
}

// --- 53.4-AC2: body 覆盖 rnix 能力地图关键维度（关键词级可检）---
func TestATDD_53_4_AC2_UsingRnix_BodyCoversCapabilityMap(t *testing.T) {
	info := atdd534Load(t)
	required := []string{
		"spawn", "ps", "kill", "strace", "wait", "suspend", "resume",
		"compose", "intent", "skill", "daemon", "VFS",
	}
	for _, kw := range required {
		if !strings.Contains(info.Body, kw) {
			t.Errorf("body missing capability keyword %q (AC2: body 须覆盖 rnix 能力地图)", kw)
		}
	}
}

// --- 53.4-AC3 护栏: SKILL.md body 无设备路径（切分：架构描述设备路径下沉 references/）---
// 锁定本 skill 通过 53.3 的 INT-012/INT-013 blanket grep，同时满足 AC3 切分。
func TestATDD_53_4_AC3_UsingRnix_BodyNoDevicePaths(t *testing.T) {
	info := atdd534Load(t)
	for _, tok := range atdd534DevicePathTokens() {
		if strings.Contains(info.Body, tok) {
			t.Errorf("SKILL.md body contains device path %q (AC3 切分: body 教调用工具用 Bash，设备路径作架构描述须下沉 references/architecture.md)", tok)
		}
	}
}

// --- 53.4-AC3 护栏: body 行数 <500（progressive disclosure，深度外移 references/）---
func TestATDD_53_4_AC3_UsingRnix_BodyUnderLineLimit(t *testing.T) {
	info := atdd534Load(t)
	lines := strings.Count(info.Body, "\n") + 1
	if lines >= 500 {
		t.Errorf("body has %d lines, want <500 (AC3: progressive disclosure，深度拆到 references/)", lines)
	}
}
