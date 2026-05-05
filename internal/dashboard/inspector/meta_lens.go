// Package inspector — meta_lens.go (Story 38-5 PR11 Step 4(c))
//
// Meta lens（Lens ❹）的 5 个 pure helper + 1 个常量：
//
//   - MetaTokenContextWindow — 默认上下文窗口大小（Claude 3.5 / Sonnet 4.x = 200K）
//   - RenderMetaSectionHeader — "── Title ────...───" 分隔标题渲染
//   - MetaSectionHeaderWidth — 动态 header 宽度（cap 70 / shrink mWidth-2 / floor 16）
//   - RenderTokenLine — Tokens section 单行（label + count + bar + percent · total 形态特殊）
//   - FormatThousands — 千位分隔符插入（"200000" → "200,000"）
//   - RenderTokenBar — 固定宽度 unicode block 字符进度条 + ASCII 降级
//
// 迁出动机（spec § 04 风险 5 中断保险原则）：
//
//   - 5 个 helper 全部 caller 都在 cmd/rnix.dashboard_inspector.go::buildMetaLens
//     内部，没有外部 caller 也没有 ATDD grep 契约保留点；
//   - 全部 pure function（仅消费 string/int 参数 + ui.ColorMuted + IsASCIIMode），
//     无 dashboardModel 状态依赖；
//   - 与 system_lens.go (FormatSignedCharCount / RenderSystemPromptBody) 同模式 ·
//     完成 Meta lens 的 pure helper 全部抽离 · 为剩余 buildMetaLens receiver
//     方法迁出（依赖 detail + m.width + m.inspector.StepMax）铺路；
//   - Story 38-3 AC#4 行为契约（标签右对齐 10 字符 / 进度条 20 字符 / 千位分隔
//     200,000 / ASCII 降级）通过 11 项契约测试显式保留.
//
// nil safety：所有公开函数无 receiver，输入参数为 string/int 零值安全.
package inspector

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/ui"
)

// MetaTokenContextWindow 是 Meta lens Tokens 段假设的默认上下文窗口大小。
//
// Claude 3.5 / Sonnet 4.x 默认 200K tokens；超过该窗口时进度条会 clamp 到 100%
// （RenderTokenBar 内部的 ratio > 1 防御）。如果未来需要按 provider/model
// 动态查找，应在 buildMetaLens caller 处计算后通过参数传入，而不是修改这里
// 的硬编码常量（保持 helper 纯函数性）。
const MetaTokenContextWindow = 200000

// RenderMetaSectionHeader 返回 "── <title> ────...───" 形式的分隔标题，按
// width 填充至给定列宽。最小填充字符数 3（保证视觉边界存在）。
//
// width 单位为显示列；prefix 长度按 utf8.RuneCountInString 计算（不是 byte），
// 让 CJK 等多字节标题也能正确对齐。
func RenderMetaSectionHeader(title string, width int) string {
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))
	prefix := "── " + title + " "
	used := utf8.RuneCountInString(prefix)
	fillCount := max(width-used, 3)
	return dim.Render(prefix + strings.Repeat("─", fillCount))
}

// MetaSectionHeaderWidth 返回 Meta lens 三个分隔标题（Tokens / Action / Counts）
// 的动态宽度。
//
// Story 38-3 review P11：之前固定 70 字面量在 sub-70 列终端会溢出。规则：
//   - mWidth <= 0 → 70（fallback · 调用前未拿到 dashboard 宽度）
//   - 否则 → min(70, max(mWidth-2, 16))（cap 70 / shrink mWidth-2 / floor 16）
//
// 减 2 是给 inspector 1 列左 padding + 边距留余量；floor 16 保证标题至少能
// 容纳 "── XYZ ────────"（标题 + 空格 + 至少 8 字符填充）.
func MetaSectionHeaderWidth(mWidth int) int {
	if mWidth <= 0 {
		return 70
	}
	return min(70, max(mWidth-2, 16))
}

// RenderTokenLine 输出 Tokens section 单行，两种形态：
//
//	totalLine == false:
//	   <label-padded-10> <count>  <bar 20 cells>  <pct>%
//	totalLine == true:
//	   <label-padded-10> <count> of <total-formatted> context
//
// Story 38-3 AC#4 + code review fixes:
//   - P7：label 用 "%10s"（右对齐）让 "Request:" / "Response:" / "Total:"
//     的冒号对齐；
//   - P8：total 形态的 total 用 FormatThousands（"200,000"）；
//   - P13：total 形态去掉冗余的前置 count（count 仅在 "<count> of <total>"
//     处出现一次）.
//
// 渲染原始整数 count 而不是 FormatTokenCount："1500" 仍保留让 27-4 现有
// regression 测试匹配（条形图已传达视觉 scale）.
func RenderTokenLine(label string, count, total int, totalLine bool) string {
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))
	labelPadded := fmt.Sprintf("%10s", label)
	if totalLine {
		return fmt.Sprintf("%s %s",
			dim.Render(labelPadded),
			dim.Render(fmt.Sprintf("%d of %s context", count, FormatThousands(total))))
	}
	pct := 0.0
	if total > 0 {
		pct = float64(count) / float64(total) * 100
	}
	return fmt.Sprintf("%s %d  %s  %.1f%%",
		dim.Render(labelPadded),
		count,
		RenderTokenBar(count, total, 20),
		pct)
}

// FormatThousands 在非负整数中插入千位分隔符（US locale: ","）。
//
// Story 38-3 review P8：用于 Meta lens Total 行渲染 "200,000" 而非 "200000".
//
// 负数：递归处理（"-200,000"）.
// |n| <= 999：直接返回 "%d"（无分隔符）.
func FormatThousands(n int) string {
	if n < 0 {
		return "-" + FormatThousands(-n)
	}
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	pre := len(s) % 3
	if pre > 0 {
		b.WriteString(s[:pre])
		if len(s) > pre {
			b.WriteByte(',')
		}
	}
	for i := pre; i < len(s); i += 3 {
		b.WriteString(s[i : i+3])
		if i+3 < len(s) {
			b.WriteByte(',')
		}
	}
	return b.String()
}

// RenderTokenBar 返回固定宽度的 unicode block-char 进度条，反映 count/total
// 比例。width 单位是显示列。ASCII 降级：'█' → '#'、'░' → '.'。Story 38-3 AC#4.
//
// Story 38-3 review P20：ratio 在浮点空间计算并 clamp 到 [0, 1] 再缩放到 width，
// 避免：
//  1. count*width 32-bit overflow（count 极大时整型溢出）
//  2. count > total 时 bar 画出 > width 的格子（例如 token 超过默认 200K 上下文窗口）
//
// width < 1 时强制 1（保护性 fallback · 不返回空字符串）.
func RenderTokenBar(count, total, width int) string {
	if width < 1 {
		width = 1
	}
	filled := 0
	if total > 0 {
		ratio := float64(count) / float64(total)
		if ratio < 0 {
			ratio = 0
		} else if ratio > 1 {
			ratio = 1
		}
		filled = int(ratio * float64(width))
		filled = min(filled, width)
		filled = max(filled, 0)
	}
	full := "█"
	empty := "░"
	if ui.IsASCIIMode() {
		full = "#"
		empty = "."
	}
	return strings.Repeat(full, filled) + strings.Repeat(empty, width-filled)
}
