// =============================================================================
// Story 48.4 — playwright-demo agent 资源 ATDD (GREEN) — AC5
//
// Asserts:
//   1. embed.FS 含 lib/agents/playwright-demo/agent.yaml + instructions.md
//   2. agent.yaml 解析为 AgentManifest 且 MCP=["playwright"]
//   3. instructions.md 含 4 个工作流锚点 (navigate / wait / screenshot / report)
// =============================================================================

package main

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	rnix "github.com/rnixai/rnix"
	"github.com/rnixai/rnix/agents"
)

// -----------------------------------------------------------------------------
// _005 (AC5): embed.FS 含 playwright-demo agent 的两个核心文件
// -----------------------------------------------------------------------------
func TestATDD_48_4_005_PlaywrightDemoAgent_EmbedExtracted(t *testing.T) {
	for _, path := range []string{
		"lib/agents/playwright-demo/agent.yaml",
		"lib/agents/playwright-demo/instructions.md",
	} {
		data, err := fs.ReadFile(rnix.EmbeddedAgents, path)
		if err != nil {
			t.Errorf("missing embedded asset %s: %v", path, err)
			continue
		}
		if len(data) == 0 {
			t.Errorf("embedded asset %s is empty", path)
		}
	}

	// 完整目录确认 (defensive: 防止意外把 agent.yaml 放在错误层级,
	// 但也要拒绝任何意外混入的文件 — 例如 .DS_Store / swp / 备份).
	matches, err := fs.Glob(rnix.EmbeddedAgents, "lib/agents/playwright-demo/*")
	if err != nil {
		t.Fatalf("fs.Glob embedded playwright-demo failed: %v", err)
	}
	want := map[string]bool{
		"lib/agents/playwright-demo/agent.yaml":      false,
		"lib/agents/playwright-demo/instructions.md": false,
	}
	for _, m := range matches {
		if _, ok := want[m]; !ok {
			t.Errorf("unexpected file embedded under playwright-demo: %q", m)
			continue
		}
		want[m] = true
	}
	for path, found := range want {
		if !found {
			t.Errorf("expected embedded file missing: %q", path)
		}
	}
}

// -----------------------------------------------------------------------------
// _006 (AC5): agent.yaml 解析为合法 AgentManifest 且字段满足约束
// -----------------------------------------------------------------------------
func TestATDD_48_4_006_PlaywrightDemoAgent_ManifestParses(t *testing.T) {
	data, err := fs.ReadFile(rnix.EmbeddedAgents, "lib/agents/playwright-demo/agent.yaml")
	if err != nil {
		t.Fatalf("read embedded agent.yaml: %v", err)
	}

	var manifest agents.AgentManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("manifest YAML parse failed: %v\nraw=%s", err, string(data))
	}

	// 必备字段
	if manifest.Name != "playwright-demo" {
		t.Errorf("Name=%q, want %q", manifest.Name, "playwright-demo")
	}
	if strings.TrimSpace(manifest.Description) == "" {
		t.Error("Description must be non-empty")
	}
	if manifest.Models.Provider != "claude" {
		t.Errorf("Models.Provider=%q, want %q (与 code-analyst 一致)",
			manifest.Models.Provider, "claude")
	}
	if manifest.Models.Preferred == "" {
		t.Error("Models.Preferred required (建议 sonnet)")
	}

	// MCP 字段 = ["playwright"] (大小写敏感,key 必须与 mcp.yaml 中 server name 匹配)
	if len(manifest.MCP) != 1 || manifest.MCP[0] != "playwright" {
		t.Errorf("MCP=%v, want [\"playwright\"]", manifest.MCP)
	}

	// 不引用 skills (Story §"playwright-demo agent.yaml 设计")
	if len(manifest.Skills) > 0 {
		t.Errorf("Skills should be empty (demo agent self-contained), got %v",
			manifest.Skills)
	}

	// context_budget 合理范围 (8192-16384)
	if manifest.ContextBudget < 8192 || manifest.ContextBudget > 16384 {
		t.Errorf("ContextBudget=%d, want in [8192, 16384]", manifest.ContextBudget)
	}

	// max_steps 不应过大 (demo 不应消耗过多 step)
	if manifest.MaxSteps > 30 || (manifest.MaxSteps > 0 && manifest.MaxSteps < 5) {
		t.Errorf("MaxSteps=%d, want in [5, 30] (or 0=default)", manifest.MaxSteps)
	}
}

// -----------------------------------------------------------------------------
// _013 (AC5): instructions.md 含 4 个工作流锚点 + 失败 hint + 输出约定
// -----------------------------------------------------------------------------
func TestATDD_48_4_013_PlaywrightDemoAgent_InstructionsHasWorkflowAnchors(t *testing.T) {
	data, err := fs.ReadFile(rnix.EmbeddedAgents, "lib/agents/playwright-demo/instructions.md")
	if err != nil {
		t.Fatalf("read embedded instructions.md: %v", err)
	}
	body := string(data)
	bodyLower := strings.ToLower(body)

	// 长度 ≥ 30 行 (AC5 acceptance)
	lines := strings.Count(body, "\n")
	if lines < 30 {
		t.Errorf("instructions.md too short (%d lines), want ≥ 30", lines)
	}

	// 4 个工作流锚点 (case-insensitive,中英文均可)
	anchors := map[string][]string{
		"navigate":   {"navigate", "打开", "open url", "browser_navigate"},
		"wait":       {"wait", "等待", "load", "browser_wait"},
		"screenshot": {"screenshot", "截图", "browser_take_screenshot"},
		"report":     {"report", "报告", "summary", "/dev/fs"},
	}
	for anchor, alts := range anchors {
		found := false
		for _, a := range alts {
			if strings.Contains(bodyLower, strings.ToLower(a)) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("workflow anchor %q missing — looked for any of %v", anchor, alts)
		}
	}

	// 失败 hint: 引导用户跑 rnix check mcp (AC7)
	if !strings.Contains(body, "rnix check mcp") {
		t.Error("instructions.md missing 'rnix check mcp' failure hint")
	}

	// 截图保存约定: .rnix/data/screenshots/ 路径
	if !strings.Contains(body, ".rnix/data/screenshots") {
		t.Error("instructions.md missing '.rnix/data/screenshots' storage convention")
	}

	// 角色边界: 必须自我声明仅用于演示 (避免被当 production agent)
	hasBoundary := strings.Contains(bodyLower, "demo") ||
		strings.Contains(body, "演示") ||
		strings.Contains(bodyLower, "demonstration")
	if !hasBoundary {
		t.Error("instructions.md missing demo/演示 self-description (role boundary)")
	}
}
