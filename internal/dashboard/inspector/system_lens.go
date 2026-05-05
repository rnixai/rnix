// Package inspector — system_lens.go (Story 38-5 PR11 Step 4(c) System lens
// helpers 迁出 · 续 box.go pattern · 同 PR11 Step 4(a-2) inspector ANSI/rune
// helpers + thumbnail helpers + search helpers + diff helpers 节奏)
//
// 本文件迁出 cmd/rnix/dashboard_inspector.go::formatSignedCharCount + renderSystemPromptBody
// 两个 System lens 共享纯 helper · 二者紧密协作（renderSystemPromptBody 输出含
// formatSignedCharCount 等字符数标签 · 整体迁入保持 cohesion）。
//
// **迁移动机**（PR11 Step 4(c) · 2026-05-05）：
//
//   - 二者都是 (m dashboardModel) 无关的 pure helper · 仅依赖输入 string + delta int；
//   - 二者依赖的底层格式化 helper（formatCharCount + RenderTruncationNotice +
//     TruncateThreshold）已在 box.go 内（PR11 Step 4(a-2) 第 9 个 commit f8e61ef
//     落地）· 本 commit 是 box.go helpers 集合的自然延伸；
//   - System lens 全部 3 处调用点（buildSystemLens 内 first-step / unchanged / changed
//     三分支）均通过 cmd/rnix wrapper 委托新公开 API · 0 行为变化 · 与 spec 38-3
//     System lens 行为契约（═══ System Prompt (X chars) ═══ + truncation notice）
//     完全等价。
//
// 包边界（spec § 04 风险 3 缓解）：
//   - 不 import cmd/rnix（go module 边界已强制）；
//   - 仅依赖 fmt + strings + utf8 + 本包内 box.go::formatCharCount/RenderTruncationNotice/TruncateThreshold；
//   - **零** cmd/rnix-private 类型引用。

package inspector

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// FormatSignedCharCount returns "+1.2k" / "-272" / "+0" style strings for
// representing a signed delta in character counts (used by System lens header
// when displaying step-to-step prompt deltas).
//
// Story 38-5 PR11 Step 4(c) (2026-05-05): Migrated from
// cmd/rnix/dashboard_inspector.go::formatSignedCharCount. Pure function.
//
// Behavior contract (preserved verbatim from cmd/rnix):
//   - delta < 0 → "-" prefix + formatCharCount of abs(delta)
//   - delta >= 0 → "+" prefix + formatCharCount(delta) (zero shows as "+0")
func FormatSignedCharCount(delta int) string {
	if delta < 0 {
		return "-" + formatCharCount(-delta)
	}
	return "+" + formatCharCount(delta)
}

// RenderSystemPromptBody emits the canonical "═══ System Prompt (X chars) ═══"
// header followed by the prompt body, applying inspector truncation. Extracted
// so all three System-lens code paths (first-step / unchanged-expanded /
// changed) share the same body rendering.
//
// Story 38-5 PR11 Step 4(c) (2026-05-05): Migrated from
// cmd/rnix/dashboard_inspector.go::renderSystemPromptBody. Pure function.
//
// Behavior contract (preserved verbatim from cmd/rnix · Story 38-3 AC):
//   - Header includes formatted char count: "═══ System Prompt (1.5k chars) ═══\n\n"
//   - When prompt length > TruncateThreshold (10000):
//     truncate to TruncateThreshold runes + append RenderTruncationNotice
//   - Otherwise: emit full prompt as-is
//   - utf8.RuneCountInString preserves CJK/emoji width semantics
func RenderSystemPromptBody(prompt string) string {
	var b strings.Builder
	sysLen := utf8.RuneCountInString(prompt)
	fmt.Fprintf(&b, "═══ System Prompt (%s chars) ═══\n\n", formatCharCount(sysLen))
	if sysLen > TruncateThreshold {
		runes := []rune(prompt)
		b.WriteString(string(runes[:TruncateThreshold]))
		b.WriteString(RenderTruncationNotice(TruncateThreshold, sysLen))
	} else {
		b.WriteString(prompt)
	}
	return b.String()
}
