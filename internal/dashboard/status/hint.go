// Package status — hint.go (Story 38-5 PR11 Step 4(c))
//
// Hint 渲染 status bar 底部的 "key+desc" 提示对，例如 "j/k nav"、"q quit"。
// 把 cmd/rnix.dashboard_status.go::hint / hintGroup / initHintStyles 三个
// 强耦合的 status-bar helper 整体迁出到 status 子包，与 RenderModeLabel
// 形成同包共渲染单元。
//
// 迁出动机（spec § 04 风险 5 中断保险原则）：
//
//   - 三个 helper 都是 status bar 内部使用，没有 dashboardModel 共享状态
//     依赖（仅消费 string 参数 + 包内 lipgloss.Style 单例）；
//   - cmd/rnix 端 8+ 处 hint(...) callsite + 1 处 hintDescStyle.Render(...)
//     全部可以通过 thin wrapper 委托零修改；
//   - 与 mode_label.go 同模式（RenderModeLabel pure helper · 消费 ui.Color*
//     + IsASCIIMode），保持 status 子包的「无状态纯函数」一致性。
//
// nil safety：所有公开函数无 receiver，输入为 string，零值安全（空 key 或
// 空 desc 仍正常工作）。

package status

import (
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/ui"
)

// 包内私有 lipgloss.Style 单例。延迟初始化以避免在 lipgloss profile 检测
// 完成前固化颜色（与 cmd/rnix.initHintStyles 同模式）。
var (
	hintInitOnce sync.Once
	keyStyle     lipgloss.Style
	descStyle    lipgloss.Style
)

func initHintStyles() {
	hintInitOnce.Do(func() {
		keyStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAgent)).Bold(true)
		descStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))
	})
}

// Hint 渲染单个 key+desc 对：高亮 key（ColorAgent + Bold），暗淡 desc
// （ColorMuted）。返回拼接后的字符串，无分隔符（caller 自行决定）。
//
// 与 cmd/rnix.dashboard_status.go::hint 完全等价（仅迁移位置）。
func Hint(key, desc string) string {
	initHintStyles()
	return keyStyle.Render(key) + descStyle.Render(desc)
}

// HintGroup 用双空格连接一组 hints。空切片返回空字符串。
//
// 与 cmd/rnix.dashboard_status.go::hintGroup 完全等价（仅迁移位置）。
func HintGroup(hints ...string) string {
	return strings.Join(hints, "  ")
}

// RenderDescStyle 用 desc 风格（ColorMuted）渲染给定文本。
//
// 用于 cmd/rnix.dashboard_status.go::renderDashboardStatus line 99 的
// "(PID %d)" 后缀渲染（Dead process filter indicator）。提供单一入口让
// cmd/rnix 端无需直接持有 lipgloss.Style 全局 var。
func RenderDescStyle(text string) string {
	initHintStyles()
	return descStyle.Render(text)
}
