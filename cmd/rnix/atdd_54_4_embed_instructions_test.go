// =============================================================================
// Story 54.4 — 内置 agent instructions 去设备路径 · embed 链 ATDD
//
// 辅测试（agents 包主测试经 AgentLoader.Load 读磁盘；本文件经 rnix.EmbeddedAgents
// 读 embed.FS）。两条链 cache 行为不同（[[dev-story-make-all-verification-gotchas]]）：
//   - agents 包读磁盘 .md → go test cache 不感知 → 须 -count=1
//   - cmd/rnix 经 //go:embed lib/agents 读 embed.FS → 改 .md 触发重编译 + cache 失效
//     （cache-safe）→ 自动重跑，与 agents 主测试形成交叉保护，并验证「打包进二进制
//     的 instructions 也干净」（用户实际加载的是 embed 版）。
//
//   🔴 RED  (实现前失败): EMBED-001/002/003/004
//   🟢 护栏 (实现前已通过): EMBED-005
//
// RED → GREEN：dev-story 改 lib/agents/{orchestrator,playwright-demo}/instructions.md
// 后转绿。详见 _bmad-output/test-artifacts/atdd-checklist-54-4-agent-instructions-rewrite.md。
// =============================================================================

package main

import (
	"io/fs"
	"strings"
	"testing"

	rnix "github.com/rnixai/rnix"
)

// atdd544EmbedDeviceTokens 是 embedded instructions 中禁止出现的设备路径前缀。
var atdd544EmbedDeviceTokens = []string{"/dev/", "/mnt/mcp/"}

// atdd544ReadEmbedInstructions 从 embed.FS 读取指定 agent 的 instructions.md 原文。
func atdd544ReadEmbedInstructions(t *testing.T, agent string) string {
	t.Helper()
	data, err := fs.ReadFile(rnix.EmbeddedAgents, "lib/agents/"+agent+"/instructions.md")
	if err != nil {
		t.Fatalf("read embedded instructions for %q: %v", agent, err)
	}
	return string(data)
}

// --- 54.4-EMBED-001: [P0] AC1+AC2 orchestrator embed instructions 无设备路径 ---
// 🔴 RED: 当前 embed 的 orchestrator instructions 含 /dev/intent*、/dev/shell、/dev/fs。

func TestATDD_54_4_Embed_OrchestratorInstructions_NoDevicePaths(t *testing.T) {
	content := atdd544ReadEmbedInstructions(t, "orchestrator")
	for _, tok := range atdd544EmbedDeviceTokens {
		if strings.Contains(content, tok) {
			t.Errorf("embedded orchestrator instructions contains device path token %q (打包进二进制的 instructions 须工具中立)", tok)
		}
	}
}

// --- 54.4-EMBED-002: [P0] AC2 orchestrator embed 无 :61 自相矛盾反指引 ---
// 🔴 RED: 当前 embed 含「可用 VFS 设备」「以设备路径作为工具名」「不存在的工具名」。

func TestATDD_54_4_Embed_OrchestratorInstructions_NoSelfContradictoryGuidance(t *testing.T) {
	content := atdd544ReadEmbedInstructions(t, "orchestrator")
	for _, phrase := range []string{"VFS 设备", "以设备路径作为工具名", "不存在的工具名"} {
		if strings.Contains(content, phrase) {
			t.Errorf("embedded orchestrator instructions still contains self-contradictory guidance %q (AC2 最高风险点)", phrase)
		}
	}
}

// --- 54.4-EMBED-003: [P0] AC3 playwright-demo embed instructions 无设备路径 ---
// 🔴 RED: 当前 embed 含 /dev/fs、/dev/mcp/playwright/。

func TestATDD_54_4_Embed_PlaywrightDemoInstructions_NoDevicePaths(t *testing.T) {
	content := atdd544ReadEmbedInstructions(t, "playwright-demo")
	for _, tok := range atdd544EmbedDeviceTokens {
		if strings.Contains(content, tok) {
			t.Errorf("embedded playwright-demo instructions contains device path token %q (打包进二进制的 instructions 须工具中立)", tok)
		}
	}
}

// --- 54.4-EMBED-004: [P0] 全量 embed agent instructions 设备路径合规（grep 等价）---
// 🔴 RED: 当前 orchestrator + playwright-demo embed 含设备路径 → 失败；改后全绿。

func TestATDD_54_4_Embed_AllBuiltinAgents_InstructionsNoDevicePaths(t *testing.T) {
	matches, err := fs.Glob(rnix.EmbeddedAgents, "lib/agents/*/instructions.md")
	if err != nil {
		t.Fatalf("glob embedded agent instructions: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("no embedded agent instructions found — embed glob may be wrong")
	}
	for _, path := range matches {
		data, rerr := fs.ReadFile(rnix.EmbeddedAgents, path)
		if rerr != nil {
			t.Errorf("read embedded %s: %v", path, rerr)
			continue
		}
		for _, tok := range atdd544EmbedDeviceTokens {
			if strings.Contains(string(data), tok) {
				t.Errorf("embedded %s contains device path token %q (所有内置 agent instructions 须工具中立)", path, tok)
			}
		}
	}
}

// --- 54.4-EMBED-005: [P1] AC3 护栏：playwright-demo embed 保留真实路径 / CLI / MCP 真实工具名 ---
// 🟢 护栏 (GREEN-stays-GREEN): 当前即绿，改后须保持（与 agents 包 INT-008 跨链一致）。

func TestATDD_54_4_Embed_PlaywrightDemoInstructions_PreservesGuardrails(t *testing.T) {
	content := atdd544ReadEmbedInstructions(t, "playwright-demo")
	for _, a := range []string{".rnix/data/screenshots", "rnix check mcp", "mcp__playwright__", "browser_navigate"} {
		if !strings.Contains(content, a) {
			t.Errorf("embedded playwright-demo instructions missing guardrail anchor %q (护栏 AC3/AC6: 真实路径 / CLI / MCP 真实工具名须保留)", a)
		}
	}
}
