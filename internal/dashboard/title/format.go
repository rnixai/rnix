// Package title — Dashboard Title Bar 共享纯 helper（Story 38-5 PR11 Step 4(c)）
//
// 本包从 cmd/rnix/dashboard_title.go 迁出 Title Bar 渲染所需的纯格式化 / 颜色
// 工具，无 dashboardModel 状态依赖。Title Bar 是 App Model 顶层渲染（不属于
// 任何 PaneModel/OverlayModel），但其内部使用的 percent clamp / 时长格式化 /
// 颜色阈值 helper 都是无状态纯函数 · 适合独立子包承载。
//
// **迁移动机**（PR11 Step 4(c) · 2026-05-05 · 第 9 个会话第 8 个 commit）：
//
//   - cmd/rnix/dashboard_title.go::clampPercent / formatElapsedHHMMSS / pctColorStyle
//     全部 (无 receiver) 自由函数 · 与本会话 PR11 Step 4(c) inspector layout /
//     System lens / debug helpers 同 pure-helper 迁出模式；
//   - Story 38.2 落地的 pctColorStyle 行为契约（< 60% Muted / 60-79% Warning /
//     ≥ 80% Error+Bold）已被 dashboard_test.go::TestPctColorStyle_Thresholds
//     直接断言（38.2-UNIT-001 · AC#1）· 通过 wrapper 委托保持零回归；
//   - clampPercent 用于 ctx / budget percent 显示（多处调用 · 5 处主代码）·
//     公开为 ClampPercent 让所有 percent 计算共享同一防御边界（[0, 999]）；
//   - formatElapsedHHMMSS 是 Title Bar 进程运行时长展示的核心格式 · 公开
//     便于未来 detail card / inspector 等其他 pane 复用。
//
// 包边界（spec § 04 风险 3 缓解）：
//   - 不 import cmd/rnix（go module 边界已强制）；
//   - 仅依赖 fmt + time + lipgloss + internal/ui::ColorMuted/Warning/Error；
//   - **零** cmd/rnix-private 类型引用；
//   - 与既有 internal/dashboard/* 子包同抽象级别。
package title

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/ui"
)

// ClampPercent restricts a percentage value to the displayable range [0, 999].
// Used by Title Bar ctx / budget percent rendering to defend against extreme
// values (e.g. miscomputed percentages or upstream API bugs sending oversized
// numbers).
//
// Story 38-5 PR11 Step 4(c) (2026-05-05): Migrated from
// cmd/rnix/dashboard_title.go::clampPercent. Pure function · zero state
// dependency.
//
// Behavior contract (preserved verbatim from cmd/rnix):
//   - v < 0   → 0   (negative percentages are nonsensical · floor at 0)
//   - v > 999 → 999 (clamp to 3-digit display ceiling)
//   - else    → v   (passthrough)
func ClampPercent(v int) int {
	if v < 0 {
		return 0
	}
	if v > 999 {
		return 999
	}
	return v
}

// FormatElapsedHHMMSS formats a duration as HH:MM:SS string. Used by Title Bar
// for the per-process elapsed-time segment.
//
// Story 38-5 PR11 Step 4(c) (2026-05-05): Migrated from
// cmd/rnix/dashboard_title.go::formatElapsedHHMMSS. Pure function · zero state
// dependency.
//
// Behavior contract (preserved verbatim from cmd/rnix):
//   - d < 0 → treated as 0 (no negative elapsed display)
//   - Hours: full hour count (no day rollover · 25:00:00 displays as 25:00:00)
//   - Minutes/Seconds: modulo 60 within the hour/minute boundary
//   - Format: "%02d:%02d:%02d" (zero-padded · always 8 chars wide)
func FormatElapsedHHMMSS(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

// PctColorStyle returns a lipgloss style coloured by usage percentage thresholds
// for the Title Bar ctx / budget segments (Story 38.2 AC#1).
//
// Story 38-5 PR11 Step 4(c) (2026-05-05): Migrated from
// cmd/rnix/dashboard_title.go::pctColorStyle. Pure function · zero state
// dependency. Story 38.2 AC#1 行为契约（38.2-UNIT-001 测试覆盖）保留。
//
// Behavior contract (preserved verbatim from cmd/rnix · Story 38.2 AC#1):
//
//	< 60%  → ColorMuted (dim grey)
//	60-79% → ColorWarning (yellow)
//	≥ 80%  → ColorError (red, bold)
//
// 阈值与 styleProviderName 的三级健康逻辑保持一致 · 用户在 "approaching limit"
// vs "over limit" 上看到统一的颜色语义（38.2 落地的颜色一致性原则）。
func PctColorStyle(pct int) lipgloss.Style {
	switch {
	case pct >= 80:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError)).Bold(true)
	case pct >= 60:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorWarning))
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))
	}
}
