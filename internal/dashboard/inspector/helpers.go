// Package inspector — helpers.go (Story 38-5 PR11 Step 4(a-2))
//
// 纯 ANSI / Rune 字符串处理 helpers，迁出自 cmd/rnix/dashboard_inspector.go：
//   - StripANSIApprox       （原 stripANSIApprox · 测试 ANSI 剥离）
//   - TruncateANSIRunes     （原 truncateANSIRunes · 按可见列截断 + SGR 关闭）
//   - ChunkRunes            （原 chunkRunes · 按显示宽度切块 + SGR 跨块续色）
//
// 这些函数完全纯（不依赖 dashboardModel / InspectorState），因此独立可测可迁。
// cmd/rnix 端保留同名小写 wrapper 以兼容现有 dashboard_inspector_visual_test.go
// （该测试文件在 stripANSIApprox 上有 ~30+ 个调用点，是 38-3 视觉契约的关键证据）。
//
// 设计与 PR2 helpers / PR3 segmentColor / PR11 timeline pure helpers 同模式。
package inspector

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// StripANSIApprox 移除字符串中的所有 ANSI 转义序列，返回纯文本。
//
// 实现：状态机扫描 — 检测 0x1b（ESC），进入 inEsc 状态后吞掉所有字符直到 'm'。
// 不严格识别 CSI / OSC 等高级序列，只针对 dashboard 中常见的 SGR 着色码足够用，
// 故名 "Approx"。已被 dashboard_inspector_visual_test.go 大量使用作为 ANSI byte
// 比较的 normalizer（profile-tolerant 视觉测试）。
//
// 性能：O(n) 单次扫描；无分配（仅内部 strings.Builder）。
func StripANSIApprox(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		if r == 0x1b {
			inEsc = true
			continue
		}
		if inEsc {
			if r == 'm' {
				inEsc = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// TruncateANSIRunes 按可见列数 maxCols 截断字符串，跳过 ANSI SGR 序列字节，
// 当截断时若仍有未关闭的 SGR 着色，自动追加 "\x1b[0m" reset，避免颜色泄漏。
//
// 用途：Step Rail / Thumbnail Bar 等需要按显示列数硬截断含 ANSI 着色的内容时
// 调用。空 / 0 / 负 maxCols 返回空串。
//
// 注意：不识别宽字符（CJK/emoji 仍按 1 列计算），这是 38-3 落地行为，CJK 多列
// 显示由调用方通过更宽的 maxCols 兜底；专门的宽字符切块走 ChunkRunes。
func TruncateANSIRunes(s string, maxCols int) string {
	if maxCols <= 0 {
		return ""
	}
	visible := 0
	inEsc := false
	hasOpenSGR := false
	var b strings.Builder
	for _, r := range s {
		if r == 0x1b {
			inEsc = true
			b.WriteRune(r)
			continue
		}
		if inEsc {
			b.WriteRune(r)
			if r == 'm' {
				inEsc = false
				snapshot := b.String()
				if idx := strings.LastIndex(snapshot, "\x1b["); idx >= 0 {
					seq := snapshot[idx:]
					if seq == "\x1b[0m" || seq == "\x1b[m" {
						hasOpenSGR = false
					} else {
						hasOpenSGR = true
					}
				}
			}
			continue
		}
		if visible >= maxCols {
			if hasOpenSGR {
				b.WriteString("\x1b[0m")
			}
			return b.String()
		}
		b.WriteRune(r)
		visible++
	}
	if hasOpenSGR {
		b.WriteString("\x1b[0m")
	}
	return b.String()
}

// ChunkRunes 把 s 按显示宽度切块（每块 ≤ maxCols 列），跳过 ANSI 转义并跨块续色。
//
// 用途：renderBoxedSection 在 box 内换行长行时调用，确保 box 右边线列对齐。
//
// Story 38-3 review fixes 保留：
//   - P16: SGR 跨块时在块尾发 "\x1b[0m" reset、块首再开同 SGR，颜色不泄漏。
//   - P17: 用 lipgloss.Width 统计宽度，CJK / emoji 占 2 列正确生效。
//
// 边界：
//   - maxCols ≤ 0 / lipgloss.Width(s) ≤ maxCols → 返回单元素 [s]
//   - 空串 s 走第二个分支返回 [""]（保留原行为）
func ChunkRunes(s string, maxCols int) []string {
	if maxCols <= 0 {
		return []string{s}
	}
	if lipgloss.Width(s) <= maxCols {
		return []string{s}
	}
	var out []string
	var cur strings.Builder
	cols := 0
	inEsc := false
	openSGR := "" // last open SGR sequence, to re-open on next chunk
	for _, r := range s {
		if r == 0x1b {
			inEsc = true
			cur.WriteRune(r)
			continue
		}
		if inEsc {
			cur.WriteRune(r)
			if r == 'm' {
				inEsc = false
				snapshot := cur.String()
				if idx := strings.LastIndex(snapshot, "\x1b["); idx >= 0 {
					seq := snapshot[idx:]
					if seq == "\x1b[0m" || seq == "\x1b[m" {
						openSGR = ""
					} else {
						openSGR = seq
					}
				}
			}
			continue
		}
		cur.WriteRune(r)
		cols += lipgloss.Width(string(r))
		if cols >= maxCols {
			if openSGR != "" {
				cur.WriteString("\x1b[0m")
			}
			out = append(out, cur.String())
			cur.Reset()
			if openSGR != "" {
				cur.WriteString(openSGR)
			}
			cols = 0
		}
	}
	if cur.Len() > 0 {
		if openSGR != "" {
			cur.WriteString("\x1b[0m")
		}
		out = append(out, cur.String())
	}
	return out
}
