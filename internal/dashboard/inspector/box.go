// Package inspector — box.go (Story 38-5 PR11 Step 4(a-2))
//
// Box-rendering helpers for Inspector Tool I/O lens (Story 38-3 AC#2):
//
//   - TruncateThreshold      （原 inspectorTruncateThreshold 常量）
//   - RenderTruncationNotice （原 renderTruncationNotice）
//   - BoxChar                （原 boxChar · 边框字符 / ASCII fallback）
//   - BoxWidth               （原 inspectorBoxWidth · 自适应 box 宽度）
//   - TruncateBoxContent     （原 truncateBoxContent · 内容硬截断）
//   - RenderBoxedSection     （原 renderBoxedSection · 主体 box 渲染）
//
// 全部函数纯净（不依赖 dashboardModel / InspectorState），仅依赖 ui.IsASCIIMode 和
// 同包内 StripANSIApprox/ChunkRunes（box 内换行 + 列对齐）。cmd/rnix 端保留同名
// thin wrapper 让现有 caller 调用契约不变。
package inspector

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/ui"
)

// TruncateThreshold 是 Inspector lens 内容渲染前的硬截断阈值（rune 数）。
// 用于 System / Tool I/O / Conversation 三个 lens 的内容超长保护，避免 lipgloss
// 渲染巨量字符串时延迟。原 cmd/rnix.inspectorTruncateThreshold 同值。
const TruncateThreshold = 10000

// RenderTruncationNotice 渲染 "(truncated X / total Y · o open full)" 提示行，
// 紧随被截断内容尾部。Story 27-4 落地行为保留。ASCII mode 中分隔符 " · " 降级为 " - "。
//
// 参数：shown — 已展示字符数；total — 原始字符总数。
func RenderTruncationNotice(shown, total int) string {
	sep := " · "
	if ui.IsASCIIMode() {
		sep = " - "
	}
	return fmt.Sprintf("\n(truncated %s / total %s%so open full)",
		formatCharCount(shown), formatCharCount(total), sep)
}

// BoxChar 把逻辑边框组件名映射为 rune（Unicode）/ ASCII 回退（RNIX_ASCII=1）。
//
// Story 38-3 Dev Notes 4: 此 helper 故意不复用 ui.AlertSeverityIcon（后者返回严重度
// 字形而非 box 字符）。
//
// 名字含义：tl/tr/bl/br = 四角；h/v = 水平/垂直边线。
func BoxChar(name string) string {
	if ui.IsASCIIMode() {
		switch name {
		case "tl", "tr", "bl", "br":
			return "+"
		case "h":
			return "-"
		case "v":
			return "|"
		}
		return "?"
	}
	switch name {
	case "tl":
		return "┌"
	case "tr":
		return "┐"
	case "bl":
		return "└"
	case "br":
		return "┘"
	case "h":
		return "─"
	case "v":
		return "│"
	}
	return "?"
}

// BoxWidth 返回 Inspector Tool I/O 的有效 box 宽度，窄屏幕自动缩窄。
// Story 38-3 AC#2 / AC#9：上限 70 列；mWidth - 4 留出左右各 2 列 padding。
//
// Story 38-3 review P12：当 mWidth ≤ 24 时下限 cap 在实际宽度而非旧的 20 列硬底
// （20 列 box 在 10 列 TTY 中仍会溢出）。
func BoxWidth(mWidth int) int {
	if mWidth <= 0 {
		return 70
	}
	if mWidth-4 < 70 {
		w := mWidth - 4
		if w < 20 {
			return min(20, mWidth)
		}
		return w
	}
	return 70
}

// TruncateBoxContent 对 box body 应用 Inspector 标准 rune-count 截断 + Story 27-4
// truncation notice。renderBoxedSection 调用方使用，让截断标记落入 box 框线之内。
func TruncateBoxContent(content string) string {
	totalLen := utf8.RuneCountInString(content)
	if totalLen <= TruncateThreshold {
		return content
	}
	runes := []rune(content)
	return string(runes[:TruncateThreshold]) + RenderTruncationNotice(TruncateThreshold, totalLen)
}

// RenderBoxedSection 在 content 周围画带标题的 box（Story 38-3 AC#2）。
//
// 布局（Unicode mode）：
//
//	┌─ <title> ──...──┐
//	│ <content line>  │
//	│ <content line>  │
//	└─────────────────┘
//
// 参数：
//   - title:     顶边缘短标题
//   - content:   预先截断的多行 body（caller 自行截断）
//   - color:     边框 hex 颜色；当 colorBody 为 true 时 body 行也用此颜色（Error box）
//   - colorBody: 是否着色 body 内容
//   - width:     box 外宽（含边框）；内可用宽度 = width-4
//
// ASCII mode：边框降级为 + - |。
func RenderBoxedSection(title, content, color string, colorBody bool, width int) string {
	if width < 8 {
		width = 8
	}
	innerWidth := width - 4 // 1 left edge + 1 space + content + 1 space + 1 right edge

	borderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
	bodyStyle := lipgloss.NewStyle()
	if colorBody {
		bodyStyle = bodyStyle.Foreground(lipgloss.Color(color))
	}

	tl := BoxChar("tl")
	tr := BoxChar("tr")
	bl := BoxChar("bl")
	br := BoxChar("br")
	h := BoxChar("h")
	v := BoxChar("v")

	// Top edge: `┌─ <title> ──...─┐`. Story 38-3 review P15: 当 title 比 inner
	// area 还宽时截断标题让顶边停留在 innerWidth+2 列内（与 body 行宽对齐），
	// 否则顶边会冲出右框，box 不再矩形。
	maxTitleRunes := max(innerWidth-2, 1) // reserve 2 cols for "h + space" prefix and trailing space
	titleRunes := []rune(title)
	if len(titleRunes) > maxTitleRunes {
		title = string(titleRunes[:maxTitleRunes])
	}
	titleSegment := h + " " + title + " "
	titleSegRunes := utf8.RuneCountInString(titleSegment)
	fillCount := max(innerWidth+2-titleSegRunes, 1)
	top := tl + titleSegment + strings.Repeat(h, fillCount) + tr

	var out strings.Builder
	out.WriteString(borderStyle.Render(top))
	out.WriteString("\n")

	// Body lines（每行按 innerWidth 列硬切，长行走 ChunkRunes）
	for line := range strings.SplitSeq(content, "\n") {
		if utf8.RuneCountInString(line) == 0 {
			out.WriteString(borderStyle.Render(v))
			out.WriteString(strings.Repeat(" ", innerWidth+2))
			out.WriteString(borderStyle.Render(v))
			out.WriteString("\n")
			continue
		}
		chunks := ChunkRunes(line, innerWidth)
		for _, chunk := range chunks {
			padCount := max(innerWidth-utf8.RuneCountInString(StripANSIApprox(chunk)), 0)
			out.WriteString(borderStyle.Render(v))
			out.WriteString(" ")
			if colorBody {
				out.WriteString(bodyStyle.Render(chunk))
			} else {
				out.WriteString(chunk)
			}
			out.WriteString(strings.Repeat(" ", padCount))
			out.WriteString(" ")
			out.WriteString(borderStyle.Render(v))
			out.WriteString("\n")
		}
	}

	// Bottom edge
	bottom := bl + strings.Repeat(h, innerWidth+2) + br
	out.WriteString(borderStyle.Render(bottom))
	return out.String()
}

// formatCharCount 是 inspector 包内私有 helper，将整型字符数格式化为 "1.5k" / "999"
// 形式（千分位精度 1 位）。与 timeline.FormatCharCount 实现等价；本地实现避免引入
// inspector → timeline 跨子包依赖（单向单 helper 不值得跨包耦合）。
func formatCharCount(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000.0)
	}
	return fmt.Sprintf("%d", n)
}
