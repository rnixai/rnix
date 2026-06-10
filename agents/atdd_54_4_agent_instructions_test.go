package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rnixai/rnix/drivers/mcp"
	"github.com/rnixai/rnix/skills"
)

// ============================================================
// ATDD RED PHASE — Story 54.4: 内置 agent instructions 去设备路径
//
// 验证 orchestrator 与 playwright-demo 两个内置 agent 的 instructions.md
// （LLM 直读的人类可读文本）里的 Rnix 设备路径换成 54.2/54.3 落定的语义
// 工具名 / 工具中立描述（/dev/intent* → Intent*, /dev/shell → Bash,
// /dev/fs → Read/Write, /dev/mcp/playwright/ → mcp__playwright__*）。规范
// 依据 ADR Decision 45 模块⑤（agent instructions 去设备路径），与 53.3
// 把 decompose skill body 工具中立化为同构操作（对象换成 agent instructions）。
//
// 关键技术杠杆：AgentInfo.Instructions（loader.go:161）= instructions.md
// 原文（agent 的 instructions.md 无 frontmatter，整个文件即 instructions），
// 故断言 info.Instructions 不含 "/dev/" 精确等价于「instructions 文本去设备路径」。
//
// MCP 配置注意（loader.go:147）：playwright-demo 声明 mcp:[playwright]，若
// mcpCfg==nil 则 Load 报错。故 atdd544MCPConfig 提供含 playwright server 的
// 最小配置（仅需 config map 有 key，Load 不真正连接 server）。
//
// 测试分两类（详见 _bmad-output/test-artifacts/
//   atdd-checklist-54-4-agent-instructions-rewrite.md）：
//   🔴 RED  (实现前失败，驱动开发): INT-001/002/003/004/005/006/010/011
//   🟢 护栏 (实现前已通过，锁定不变契约): INT-007/008/009
//
// RED → GREEN（dev-story 实现后转绿）：
//   - AC1: orchestrator :3/:11/:21/:29/:37/:51/:73/:74/:76 的 /dev/intent* → Intent*
//   - AC2: orchestrator :59 章节标题 / :61 自相矛盾反指引重写 / :63-67 工具清单
//   - AC3: playwright-demo :8/:14/:23/:33/:35 的 /dev/fs、/dev/mcp → Write、mcp__playwright__*
//   - AC7: orchestrator/agent.yaml:2 description /dev/intent → 工具中立（默认纳入）
//   - 改完后 `go test -count=1 -race -run TestATDD_54_4 ./agents/` 全绿
//
// 设计说明（不使用 t.Skip）：本项目文档型 ATDD 惯例为直接生效的运行时断言
// （参见 skills/atdd_53_3_skill_body_naming_test.go），红灯=运行时断言失败
// （能编译）优于编译失败；且护栏测试须始终运行以实时拦截范围红线违规（误删
// Orchestrator 标题 / 误改 code-analyst·stem / 误删真实路径锚点）。所有断言
// 均针对真实预期行为，无占位断言。
//
// cache 纪律：agents 包经 AgentLoader.Load 读磁盘 lib/agents/*.md，go test
// cache 不感知 .md 变化 —— 验证 RED→GREEN 必须 `-count=1 -race`。
// ============================================================

// 路径相对包目录（agents/）——与 loader_test.go:207-208 既有惯例一致。
const (
	atdd544LibAgentsDir = "../lib/agents"
	atdd544LibSkillsDir = "../lib/skills"
)

// atdd544DeviceTokens 是 instructions / description 中禁止出现的设备路径前缀。
var atdd544DeviceTokens = []string{"/dev/", "/mnt/mcp/"}

// atdd544MCPConfig 提供含 playwright server 的最小 MCPGlobalConfig，使声明
// mcp:[playwright] 的 playwright-demo 能经 Load 加载（规避 loader.go:147 的
// "no mcp.yaml configuration was loaded" 报错）。仅需 config map 含该 key，
// Load 路径只做存在性校验 + ToMCPConfig 转换，不真正启动/连接子进程。
func atdd544MCPConfig() *mcp.MCPGlobalConfig {
	return &mcp.MCPGlobalConfig{
		Servers: map[string]mcp.MCPServerConfig{
			"playwright": {
				Command:       "npx",
				Args:          []string{"-y", "@playwright/mcp"},
				TransportType: "stdio",
			},
		},
	}
}

// atdd544LoadAgent 用真实搜索目录加载内置 agent（含 instructions + 合并 skill）。
func atdd544LoadAgent(t *testing.T, name string) *AgentInfo {
	t.Helper()
	sl := skills.NewSkillLoader([]string{atdd544LibSkillsDir})
	al := NewAgentLoader([]string{atdd544LibAgentsDir}, sl, atdd544MCPConfig())
	info, err := al.Load(name)
	if err != nil {
		t.Fatalf("Load(%q) returned error: %v", name, err)
	}
	return info
}

// --- 54.4-INT-001: [P0] AC1+AC2 orchestrator instructions 无设备路径 ---
// 🔴 RED: 当前含 /dev/intent*(:3/11/21/29/37/51/73/74/76)、/dev/shell(:64/76)、/dev/fs(:65)。

func TestATDD_54_4_AC1_OrchestratorInstructions_NoDevicePaths(t *testing.T) {
	info := atdd544LoadAgent(t, "orchestrator")
	for _, tok := range atdd544DeviceTokens {
		if strings.Contains(info.Instructions, tok) {
			t.Errorf("orchestrator instructions contains device path token %q (AC1/AC2: 须用 IntentDecompose/IntentConfirm/IntentExecute/IntentStatus/Bash/Read/Write 工具名，不得用设备路径)", tok)
		}
	}
}

// --- 54.4-INT-002: [P1] AC1 orchestrator 含 Intent* 工具名（正向）---
// 🔴 RED: 当前 :11/:51/:73 用 /dev/intent/decompose，无 "IntentDecompose" 字面量
// （:57 已含 IntentConfirm/Execute/Status，但 IntentDecompose 缺失 → 整体 RED）。

func TestATDD_54_4_AC1_OrchestratorInstructions_UsesIntentToolNames(t *testing.T) {
	info := atdd544LoadAgent(t, "orchestrator")
	for _, name := range []string{"IntentDecompose", "IntentConfirm", "IntentExecute", "IntentStatus"} {
		if !strings.Contains(info.Instructions, name) {
			t.Errorf("orchestrator instructions missing intent tool name %q (AC1: /dev/intent/* → 54.2 PascalCase 工具名)", name)
		}
	}
}

// --- 54.4-INT-003: [P1] AC2 orchestrator 含 Read 和 Write（正向）---
// 🔴 RED: 当前 :65 是中文「读写宿主文件」，无 "Read"/"Write" 字面量。
// 注：不正向断言 "Bash"——当前 :61 反指引「不要使用 Bash」已含该串，无法区分 RED/GREEN
// （改由 INT-004 驱动 :61 重写、INT-007 护栏锁定 Bash 保留）。

func TestATDD_54_4_AC2_OrchestratorInstructions_UsesReadWriteToolNames(t *testing.T) {
	info := atdd544LoadAgent(t, "orchestrator")
	// 锚定为反引号工具名形式（instructions 中工具名均以 `Read` / `Write` 反引号呈现），
	// 而非裸子串 "Read"/"Write"——后者会被未来无关词（Ready/thread/Writeup 等）假阳命中，
	// 致真工具名被删时护栏失效（code-review 2026-06-11 P1 加固）。
	for _, name := range []string{"`Read`", "`Write`"} {
		if !strings.Contains(info.Instructions, name) {
			t.Errorf("orchestrator instructions missing backtick-quoted tool name %q (AC2: /dev/fs 读写宿主文件 → `Read` / `Write`)", name)
		}
	}
}

// --- 54.4-INT-004: [P0] AC2 orchestrator 无 :61 自相矛盾反指引 ---  ⚠️ 本 story 最高风险点
// 🔴 RED: 当前 :59「## 可用 VFS 设备」、:61「直接以设备路径作为工具名调用…也不要使用
// `Bash`、`shell`、`fs_write` 等不存在的工具名」——54.3 后 Bash 是真实工具名，此句教
// LLM 拒用正确工具、改用设备路径，必须删除/反转。

func TestATDD_54_4_AC2_OrchestratorInstructions_NoSelfContradictoryGuidance(t *testing.T) {
	info := atdd544LoadAgent(t, "orchestrator")
	forbidden := []string{
		"VFS 设备",           // :59 章节标题 + :61 框架词（Decision 45 弃用「设备」框架）
		"以设备路径作为工具名", // :61 错误指引①：与 Decision 45 完全相反
		"不存在的工具名",       // :61 错误指引③：教 LLM 拒用真实工具名 Bash/Read/Write
	}
	for _, phrase := range forbidden {
		if strings.Contains(info.Instructions, phrase) {
			t.Errorf("orchestrator instructions still contains self-contradictory guidance %q (AC2 最高风险: :61 教 LLM「以设备路径作为工具名调用」并把真实工具名 Bash 称为「不存在的工具名」——必须删除/反转，让工具清单自己说话)", phrase)
		}
	}
}

// --- 54.4-INT-005: [P0] AC3 playwright-demo instructions 无设备路径 ---
// 🔴 RED: 当前 :8/:23/:33/:35 含 /dev/fs，:14 含 /dev/mcp/playwright/。

func TestATDD_54_4_AC3_PlaywrightDemoInstructions_NoDevicePaths(t *testing.T) {
	info := atdd544LoadAgent(t, "playwright-demo")
	for _, tok := range atdd544DeviceTokens {
		if strings.Contains(info.Instructions, tok) {
			t.Errorf("playwright-demo instructions contains device path token %q (AC3: /dev/fs → Write，/dev/mcp/playwright/ → mcp__playwright__*)", tok)
		}
	}
}

// --- 54.4-INT-006: [P1] AC3 playwright-demo 含 Write 工具名（正向）---
// 🔴 RED: 当前 :8/:23/:35 是中文「写文件」「写报告」「写一条 markdown」+ /dev/fs，无 "Write" 字面量。
// 注：不正向断言 "mcp__playwright__"——当前 :8 已含该串（真实工具名形式），无法区分 RED/GREEN
// （改由 INT-005 驱动 :14 的 /dev/mcp 替换、INT-008 护栏锁定 mcp__playwright__ 保留）。

func TestATDD_54_4_AC3_PlaywrightDemoInstructions_UsesWriteToolName(t *testing.T) {
	info := atdd544LoadAgent(t, "playwright-demo")
	if !strings.Contains(info.Instructions, "Write") {
		t.Error("playwright-demo instructions missing 'Write' tool name (AC3: /dev/fs 写文件/写报告 → Write)")
	}
}

// --- 54.4-INT-007: [P1] AC2 orchestrator 护栏：保留 Orchestrator 标题 + Bash 工具名 ---
// 🟢 护栏 (GREEN-stays-GREEN): 当前即绿。
//   - "Orchestrator"(:1 标题) — loader_test.go:573 硬断言 SystemPrompt 含 "Orchestrator"，删则破坏回归。
//   - "Bash"(:64/:67/:76 改后) — /dev/shell → Bash 是 54.3 真实工具名，AC2 须保留（不得连同反指引一起删光）。

func TestATDD_54_4_AC2_OrchestratorInstructions_PreservesGuardrails(t *testing.T) {
	info := atdd544LoadAgent(t, "orchestrator")
	if !strings.Contains(info.Instructions, "Orchestrator") {
		t.Error("orchestrator instructions missing 'Orchestrator' title (护栏: loader_test:573 硬依赖，AC2 不得删 :1 标题)")
	}
	if !strings.Contains(info.Instructions, "Bash") {
		t.Error("orchestrator instructions missing 'Bash' tool name (护栏: /dev/shell → Bash 须保留，不得连反指引一起删光)")
	}
}

// --- 54.4-INT-008: [P1] AC3/AC6 playwright-demo 护栏：保留真实路径 / CLI / MCP 真实工具名 / demo 自述 ---
// 🟢 护栏 (GREEN-stays-GREEN): 当前即绿。这些非设备路径，是真实用户数据目录 / CLI 命令 /
// MCP 工具真实名 / 角色自述，改了会破坏 48.4 测试或教坏 LLM（类比 53.3 的 /dev/null 护栏）。

func TestATDD_54_4_AC3_PlaywrightDemoInstructions_PreservesGuardrails(t *testing.T) {
	info := atdd544LoadAgent(t, "playwright-demo")
	anchors := []string{
		".rnix/data/screenshots", // 真实用户数据目录约定（atdd_48_4:159 依赖）
		"/tmp/rnix-",             // fallback 真实路径（按 UID 隔离，与 IPC socket 约定一致）
		"rnix check mcp",         // CLI 诊断命令（atdd_48_4:154 依赖）
		"mcp__playwright__",      // MCP 工具 LLM 可见名形式（:8 保留 + :14 改后），非设备路径
		"browser_navigate",       // MCP 工具真实名锚点（atdd_48_4:135 依赖）
		"演示",                    // demo 角色自述（atdd_48_4:168 依赖）
	}
	for _, a := range anchors {
		if !strings.Contains(info.Instructions, a) {
			t.Errorf("playwright-demo instructions missing guardrail anchor %q (护栏 AC3/AC6: 真实路径 / CLI / MCP 真实工具名 / demo 自述须保留，非设备路径)", a)
		}
	}
}

// --- 54.4-INT-009: [P0] AC5 code-analyst + stem instructions 持续无设备路径 ---
// 🟢 护栏 (GREEN-stays-GREEN): 当前即绿（已核实 clean）。本 story 不改这两个 agent，
// 加此护栏防止未来回归（与 orchestrator/playwright 的 RED 断言同文件并列）。

func TestATDD_54_4_AC5_CleanAgentsInstructions_StayClean(t *testing.T) {
	for _, name := range []string{"code-analyst", "stem"} {
		info := atdd544LoadAgent(t, name)
		for _, tok := range atdd544DeviceTokens {
			if strings.Contains(info.Instructions, tok) {
				t.Errorf("%s instructions contains device path token %q (护栏 AC5: 本 story 不改此 agent，须持续无设备路径)", name, tok)
			}
		}
	}
}

// --- 54.4-INT-010: [P0] AC1/2/3/5 全量内置 agent instructions 设备路径合规（grep 等价）---
// 🔴 RED: 当前 orchestrator + playwright-demo 含设备路径 → 失败；改后全绿。
// 遍历 lib/agents 所有含 instructions.md 的 agent，结构化断言 instructions 无设备路径，
// 精确等价 story 验收：`grep -rn "/dev/\|/mnt/mcp" lib/agents/*/instructions.md` 零命中。

func TestATDD_54_4_AllBuiltinAgents_InstructionsNoDevicePaths(t *testing.T) {
	sl := skills.NewSkillLoader([]string{atdd544LibSkillsDir})
	al := NewAgentLoader([]string{atdd544LibAgentsDir}, sl, atdd544MCPConfig())

	entries, err := os.ReadDir(atdd544LibAgentsDir)
	if err != nil {
		t.Fatalf("read dir %s: %v", atdd544LibAgentsDir, err)
	}

	checked := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// 仅处理真正的 agent 目录（含 instructions.md）。
		if _, statErr := os.Stat(filepath.Join(atdd544LibAgentsDir, e.Name(), "instructions.md")); statErr != nil {
			continue
		}
		info, loadErr := al.Load(e.Name())
		if loadErr != nil {
			t.Errorf("Load(%q) error: %v", e.Name(), loadErr)
			continue
		}
		checked++
		for _, tok := range atdd544DeviceTokens {
			if strings.Contains(info.Instructions, tok) {
				t.Errorf("agent %q instructions contains device path token %q (所有内置 agent instructions 须工具中立)", e.Name(), tok)
			}
		}
	}
	// 兜底（对齐 53.3 INT-012 code-review patch）：防 lib/agents 为空 / 全部 Load 失败时
	// 测试零检查 vacuous pass。
	if checked == 0 {
		t.Fatalf("no builtin agents with instructions.md found under %s — search dir may be wrong", atdd544LibAgentsDir)
	}
}

// --- 54.4-INT-011: [P3] AC7 orchestrator agent.yaml description 无设备路径 ---
// 🔴 RED: 当前 description(:2)「…通过 /dev/intent 设备完成分解、确认和执行」含 /dev/intent。
// description 不进 system prompt（CLI/生态可见元数据），Decision 45 模块③精神 + 与
// playwright-demo description 一致性，默认纳入工具中立化（story AC7 默认执行）。

func TestATDD_54_4_AC7_OrchestratorDescription_NoDevicePaths(t *testing.T) {
	info := atdd544LoadAgent(t, "orchestrator")
	for _, tok := range atdd544DeviceTokens {
		if strings.Contains(info.Manifest.Description, tok) {
			t.Errorf("orchestrator agent.yaml description contains device path token %q (AC7: description 是 CLI/生态可见元数据，默认纳入工具中立化)", tok)
		}
	}
}
