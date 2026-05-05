// Package status — mode_label.go (Story 38-5 PR11 Step 4(c))
//
// RenderModeLabel 把 cmd/rnix.dashboardModel.renderModeLabel 的 mode-label
// 渲染主体迁出到这里。这是 Story 38.2 AC#2 的视觉契约入口（在 status bar
// 与 replay status bar 左侧统一注入 [MONITOR] / [EXPANDED] / [INSPECTOR] /
// [DEBUG] / [REPLAY] 标签）。
//
// 迁出动机（spec § 04 风险 5 中断保险原则）：
//
//   - renderModeLabel 仅依赖 4 个 cmd/rnix 端字面量（replayMode bool +
//     viewMode int + ASCII flag + colorIPC 常量）；没有 dashboardModel
//     共享状态依赖；
//   - 与 title 包同模式（format / health / health_counts / provider 全部
//     pure helper），可以创建第 17 个 internal/dashboard 子包承载 status
//     bar 共享渲染单元；
//   - 行为契约 1:1 等价；cmd/rnix wrapper 保留同名 receiver 让现有 9 项
//     38.2-UNIT-005..009 行为测试 + atdd_29_1 文件拆分契约 grep 字符串通过.
//
// 包归属决策：
//   - status 子包独立于 title（title 渲染顶栏 / status 渲染底栏）；
//   - 共享 ui.Color* + IsASCIIMode + 不依赖 cmd/rnix-private viewMode 类型
//     （改用 int + 公开常量集 · 解耦 cmd/rnix.viewMode 类型 cascade · 与 PR10
//     PrevMode int 决策同模式）.
package status

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/ui"
)

// ColorIPC 是 [INSPECTOR] mode label 使用的紫色（与 cmd/rnix.colorIPC
// 一致 · #9B59B6 · Story 38.2 AC#2 视觉契约）。
//
// 不直接 import cmd/rnix（go module 边界 + 反向依赖防御），因此这里
// 单独导出常量与 cmd/rnix 端保持一致。两个常量值偏离时通过编译期手工
// 验证（cmd/rnix 端 colorIPC 改动需同步更新此处）。
const ColorIPC = "#9B59B6"

// ViewMode int constants — 与 cmd/rnix.viewMode iota 取值范围对齐。
//
// 解耦决策（与 PR10 InspectorState.PrevMode int 同模式）：
//   - status 包不依赖 cmd/rnix.viewMode 类型，避免 cmd/rnix → internal
//     反向 import；
//   - cmd/rnix wrapper 通过 int(viewMode) cast 调用本函数；
//   - cmd/rnix 端 viewMode iota 顺序变化时需同步更新这里的常量值
//     （编译期不会报错 · 但 38.2-UNIT-005 行为测试会立刻捕获）.
const (
	ViewDefault       = 0 // viewDefault
	ViewExpanded      = 1 // viewExpanded
	ViewStepInspector = 2 // viewStepInspector
	ViewHistory       = 3 // viewHistory（已不使用 · iota 占位）
	ViewDebug         = 4 // viewDebug
)

// RenderModeLabel returns the styled "[<MODE>] │ " mode-label prefix that
// gets injected at the left of the status bar (Story 38.2 AC#2).
//
// Inputs:
//   - replayMode: 优先级最高 · true → "[REPLAY]" 强制覆盖 viewMode.
//   - viewMode: cmd/rnix.viewMode iota 的整数值（见 ViewDefault 等常量）.
//
// Output 形态：
//
//	"[MODE]  │  " （UTF-8 模式 · '│' 为 U+2502 box drawings light vertical）
//	"[MODE]  |  " （ASCII 模式 · 通过 ui.IsASCIIMode() 判定）
//
// 颜色与 Bold（与 cmd/rnix.renderModeLabel 完全等价）：
//
//	[REPLAY]    → ColorReplay (#D08770) · regular weight
//	[INSPECTOR] → ColorIPC (#9B59B6 紫) · regular weight
//	[DEBUG]     → ColorWarning (#FFD93D 黄) · Bold
//	[EXPANDED]  → ColorAgent (#5B9BD5 蓝) · regular weight
//	[MONITOR]   → ColorMuted (#666666 灰) · regular weight
//
// 优先级（高 → 低）：replayMode → viewStepInspector → viewDebug →
// viewExpanded → {viewDefault, viewHistory}（viewHistory 已不再使用 iota
// 占位 · 落入 [MONITOR]）.
//
// 未识别 viewMode 同样降级到 [MONITOR]（防御性 fallback）.
//
// nil 安全：函数无 receiver · 输入参数全是值类型 · 不会 panic.
func RenderModeLabel(replayMode bool, viewMode int) string {
	ascii := ui.IsASCIIMode()
	sep := "│"
	if ascii {
		sep = "|"
	}
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))

	var label string
	var color string
	var bold bool
	switch {
	case replayMode:
		label = "[REPLAY]"
		color = ui.ColorReplay
	case viewMode == ViewStepInspector:
		label = "[INSPECTOR]"
		color = ColorIPC
	case viewMode == ViewDebug:
		label = "[DEBUG]"
		color = ui.ColorWarning
		bold = true
	case viewMode == ViewExpanded:
		label = "[EXPANDED]"
		color = ui.ColorAgent
	case viewMode == ViewDefault, viewMode == ViewHistory:
		label = "[MONITOR]"
		color = ui.ColorMuted
	default:
		label = "[MONITOR]"
		color = ui.ColorMuted
	}

	style := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
	if bold {
		style = style.Bold(true)
	}
	return style.Render(label) + "  " + dim.Render(sep) + "  "
}
