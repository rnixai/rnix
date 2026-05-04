// Package timeline — role.go (Story 38-5 PR11 Step 4(c) formatRoleTag +
// promptRoleForRole + promptRole* styles 迁出 · 第 N 个 helper 迁出延伸)
//
// 本文件迁出 cmd/rnix 端 conversation message role tag 渲染相关纯函数与 lipgloss
// styles，让 timeline 包独立持有 4 个角色的视觉规则，消除 cmd/rnix 端 promptRole*
// 共享 var 与 cmd/rnix-private 的耦合（形成完整的「pane 渲染规则全包内可见」边界）。
//
// 迁出范围：
//   - promptRole{System,User,Assistant,Tool} lipgloss.Style — 包内私有 styles
//     （hex 颜色字面量沿用 Story 27-4 / 38-3 落地的色板 · 与 cmd/rnix 端旧定义
//     完全等价）
//   - PromptRoleForRole(role) string — 简化版（无 Bold · 用于 timeline expand
//     mode debug detail block 渲染 · 与 cmd/rnix.promptRoleForRole 等价）
//   - FormatRoleTag(msg, toolCallNames) string — Story 38-3 AC#1 落地的
//     conversation 4-color role tag（system/user/assistant/tool_use/tool_result
//     5 种分支 · tool_use/tool_result 加 Bold + Story 38-3 review P22 颜色契约）
//
// **行为契约（不变性 · ATDD 27-4 + 36.1 + 38-3 AC#1 测试覆盖）**：
//   - system        → ColorMuted  灰     [system]
//   - user          → ColorSuccess 绿    [user]   / [user]   (短)
//   - assistant     → ColorAccent 蓝     [assistant] / [asst] (短)
//   - tool_use      → ColorSuccess 绿 + Bold  [tool_use]
//   - tool_result   → ColorReplay 橙 + Bold   [tool_result:Read]
//   - 默认 fallback → "[role]" 无样式
//
// **零 cmd/rnix 反向依赖**：本包只 import ipc + lipgloss + stdlib · 与 PR11 Step 4(c)
// 其他迁出包边界一致（render.go / helpers.go / state.go 等均零 cmd/rnix 反向 import）。
package timeline

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
)

// promptRole* — 4 个 conversation role 的 lipgloss style（cmd/rnix 端原 dashboard_types.go
// 全局 var 迁出 · 包内私有 · 与 cmd/rnix.promptRoleSystem/User/Assistant/Tool 完全等价）。
//
// 颜色字面量（Story 27-4 落地 · 38-3 AC#1 仅在 tool 分支拆分 tool_use / tool_result）：
//   - system    #888888 (灰 · 同 ui.ColorMuted hex)
//   - user      #6BCB77 (绿 · 同 ui.ColorSuccess hex)
//   - assistant #5B9BD5 (蓝 · Story 27-4 自定义不在 ui.Color* 常量中)
//   - tool      #FFD93D (黄 · Story 27-4 自定义 · 仅 PromptRoleForRole 用 ·
//     FormatRoleTag tool 分支用 ColorSuccess/ColorReplay)
var (
	promptRoleSystem    = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	promptRoleUser      = lipgloss.NewStyle().Foreground(lipgloss.Color("#6BCB77"))
	promptRoleAssistant = lipgloss.NewStyle().Foreground(lipgloss.Color("#5B9BD5"))
	promptRoleTool      = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFD93D"))
)

// PromptRoleForRole 返回 timeline expand mode 下 debug detail block 渲染时使用的
// 角色短标签（与 cmd/rnix.promptRoleForRole 等价 · 不加 Bold · 与 FormatRoleTag
// 区别：FormatRoleTag 用于 conversation lens 长标签 + 拆分 tool_use/tool_result）。
//
// 输出格式：
//   - "system"    → "[system]"   (不补空格)
//   - "user"      → "[user]  "   (补 2 空格对齐)
//   - "assistant" → "[asst]  "   (5 字符短名 + 2 空格)
//   - "tool"      → "[tool]  "
//   - 默认 fallback → "[" + role + "]"  (未知 role 不加样式)
//
// 调用方：cmd/rnix.dashboardModel.renderDebugDetail 通过 RenderDebugDetail
// roleStyle 依赖注入传入本函数（间接调用 · cmd/rnix.promptRoleForRole 是 thin
// wrapper · 与 timeline.RenderDebugDetail 接受 roleStyle func 参数协同）。
func PromptRoleForRole(role string) string {
	switch role {
	case "system":
		return promptRoleSystem.Render("[system]")
	case "user":
		return promptRoleUser.Render("[user]  ")
	case "assistant":
		return promptRoleAssistant.Render("[asst]  ")
	case "tool":
		return promptRoleTool.Render("[tool]  ")
	default:
		return "[" + role + "]"
	}
}

// FormatRoleTag 返回 conversation lens / step inspector 渲染时的长标签
// (Story 27-4 落地 · 38-3 AC#1 拆分 tool 分支为 tool_use 与 tool_result)。
//
// 输出格式（与 cmd/rnix.formatRoleTag 等价）：
//   - "system"     → "[system]"        (灰 · 不加 Bold)
//   - "user"       → "[user]"          (绿 + Bold)
//   - "assistant"  → "[assistant]"     (蓝 + Bold)
//   - "tool" 且 ToolCallID==""   → "[tool_use]"           (ColorSuccess 绿 + Bold)
//   - "tool" 且 ToolCallID!=""   → "[tool_result:<name>]" (ColorReplay 橙 + Bold)
//     - <name> = toolCallNames[ToolCallID] (映射存在则用名字 · 否则 fallback raw ID)
//   - 默认 fallback → "[" + role + "]"  (未知 role 不加样式)
//
// **Story 38-3 AC#1 关键契约**：
//   - tool_use   path  must contain literal "tool_use"   (regression-tested)
//   - tool_result path must contain literal "tool_result" + (mapped name | raw ID)
//   - user/assistant/system path must contain "[" + role + "]"  (zero behavior change)
//
// **lipgloss profile 兼容**：lipgloss 自动降级颜色 · ASCII 环境下 brackets / 文本
// 仍然可读（无色环境测试 t.Skip 颜色断言 · 仅断言文本字面量 · 见 dashboard_inspector_visual_test.go）。
func FormatRoleTag(msg ipc.MessageWire, toolCallNames map[string]string) string {
	switch msg.Role {
	case "system":
		return promptRoleSystem.Render("[system]")
	case "user":
		return promptRoleUser.Bold(true).Render("[user]")
	case "assistant":
		return promptRoleAssistant.Bold(true).Render("[assistant]")
	case "tool":
		// Story 38-3 AC#1: tool_use vs tool_result distinction by ToolCallID.
		if msg.ToolCallID == "" {
			toolUseStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorSuccess)).Bold(true)
			return toolUseStyle.Render("[tool_use]")
		}
		label := ""
		if name, ok := toolCallNames[msg.ToolCallID]; ok && name != "" {
			label = ":" + name
		} else {
			label = ":" + msg.ToolCallID
		}
		toolResultStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorReplay)).Bold(true)
		return toolResultStyle.Render("[tool_result" + label + "]")
	default:
		return "[" + msg.Role + "]"
	}
}
