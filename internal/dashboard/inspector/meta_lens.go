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
// Story 41.2 新增（2026-05-14）·  cache hit rate 一等指标 + driver 语义分支：
//
//   - RenderRateLine — 通用 "label %  (num / denom suffix)" 渲染原语
//   - ComputeCacheHitRate — 按 driver 类型分支计算 hit rate + 分母
//   - FirstStepWarmHitRateThreshold — 首步 prefix 共享 / 冷启动判定阈值
//   - driverAnthropic / driverOpenAI / ... — 本地字符串常量
//     （与 drivers/llm/config.go 同步 · 测试守门防漂移）
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
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/dashboard/timeline"
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

// knownModelContextWindows maps model name prefixes to their known context window sizes.
// Longer prefixes MUST appear before shorter ones (first-match-wins linear scan).
var knownModelContextWindows = []struct {
	prefix string
	window int
}{
	{"deepseek-v4", 1_000_000},
	{"deepseek-r1", 128_000},
	{"deepseek-v3", 128_000},
	{"gpt-5", 400_000},
	{"gpt-4o", 128_000},
	{"gpt-4.1", 1_048_576},
	{"gpt-4-turbo", 128_000},
	{"gpt-4", 128_000},
	{"o4-mini", 200_000},
	{"o3", 200_000},
	{"o1", 200_000},
	{"claude-opus-4", 200_000},
	{"claude-sonnet-4", 200_000},
	{"claude-haiku-4", 200_000},
	{"gemini-2.5", 1_048_576},
	{"gemini-3", 1_048_576},
}

// ResolveContextWindow returns the effective context window size for a model.
//
// Resolution order:
//  1. If configured > 0, return configured (explicit per-model config from providers.yaml)
//  2. Prefix match against known model context windows
//  3. MetaTokenContextWindow (200k) as fallback
func ResolveContextWindow(model string, configured int) int {
	if configured > 0 {
		return configured
	}
	for _, entry := range knownModelContextWindows {
		if strings.HasPrefix(model, entry.prefix) {
			return entry.window
		}
	}
	return MetaTokenContextWindow
}

// Story 41.2 ：Cache Hit Rate 一等指标 + driver 语义分支。
// =============================================================================

// FirstStepWarmHitRateThreshold 是首步判定 "prefix 共享" vs "冷启动" 的阈值。
//
// 选 0.05 = 5%：在 EchoMatrix 真实数据（DeepSeek 长进程）上，首步 prefix
// 共享时 cache 命中率通常落在 20-30%（即跨进程 system prompt + skills 复用），
// 冷启动则在 0-2%。5% 阈值给一个安全 buffer。
//
// 抽常量便于未来按 driver 微调（如 Anthropic 因接口语义首步 hit rate 可能落在
// 不同区间）。
const FirstStepWarmHitRateThreshold = 0.05

// driver type 字符串常量（与 drivers/llm/config.go 同步 · 反向 import 仅在
// _test.go 守门测试中，生产代码保持 dashboard → drivers 单向依赖禁止）。
const (
	driverOpenAI    = "openai"
	driverDeepSeek  = "deepseek"
	driverAnthropic = "anthropic"
	driverClaudeCLI = "claude-cli"
	driverCursorCLI = "cursor-cli"
	driverCodexCLI  = "codex-cli"
	driverQwenCLI   = "qwen-cli"
	driverGemini    = "gemini"
)

// IsAnthropicDriver 判断 driver 是否为 Anthropic 原生接口（cached 不含在 input
// 内 · hit rate 公式分母为 input + cached）。
func IsAnthropicDriver(driverType string) bool {
	return driverType == driverAnthropic
}

// IsCLIDriver 判断 driver 是否为 CLI 族（claude-cli / cursor-cli / codex-cli /
// qwen-cli）。CLI driver 整个 agent 会话只 exec 一次，原始请求记录仅落在 step 1
// （raw.jsonl 只有一条），Raw I/O lens 据此对非 step-1 给出文案引导而非泛化占位串。
func IsCLIDriver(driverType string) bool {
	switch driverType {
	case driverClaudeCLI, driverCursorCLI, driverCodexCLI, driverQwenCLI:
		return true
	default:
		return false
	}
}

// RenderRateLine 通用 "rate 行" 渲染原语，与 RenderTokenLine 同 label 对齐宽度。
//
// 输出形态（normal case）：
//
//	<label-10>  <pct.1f>%  (<num> / <denom> <suffix>)
//
// 边界 case：
//
//   - numerator == 0 && denominator == 0 → 返回空串，调用方决定不输出（避免空行）
//   - denominator == 0 && numerator > 0 → "<label> —  (cached <num>)" 不 panic
//   - pct > 100 → clamp 显示 ">100%" + 行尾追加 "(check driver semantics)"
//
// 数值用 timeline.FormatTokenCount（k/m 后缀）保证与 dashboard 全局单位一致。
//
// Story 41.2 AC#1.
func RenderRateLine(label string, numerator, denominator int, suffix string) string {
	if numerator == 0 && denominator == 0 {
		return ""
	}
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))
	labelPadded := fmt.Sprintf("%10s", label)
	if denominator == 0 {
		// numerator > 0 但 denom == 0 — 显示 cached 数避免 div-by-zero
		return fmt.Sprintf("%s %s",
			dim.Render(labelPadded),
			dim.Render(fmt.Sprintf("—  (cached %s)", timeline.FormatTokenCount(numerator))))
	}
	pct := float64(numerator) / float64(denominator) * 100
	pctStr := fmt.Sprintf("%.1f%%", pct)
	extra := ""
	if pct > 100 {
		pctStr = ">100%"
		extra = "  (check driver semantics)"
	}
	return fmt.Sprintf("%s %s  (%s / %s %s)%s",
		dim.Render(labelPadded),
		dim.Render(pctStr),
		timeline.FormatTokenCount(numerator),
		timeline.FormatTokenCount(denominator),
		suffix,
		extra,
	)
}

// ComputeCacheHitRate 按 driver 类型分支计算 cache hit rate。
//
// 公式表：
//
//	driverType                                 公式               分母
//	─────────────────────────────────────────────────────────────────
//	openai / deepseek                         cached / input     input
//	anthropic                                  cached/(in+cached) input+cached
//	claude-cli / cursor-cli / codex-cli /      cached / input     input   (OpenAI fallback)
//	qwen-cli / gemini
//	空 / 未知                                  cached / input     input   (OpenAI fallback)
//
// 边界：input == 0 时返回 (0, 0)，调用方决定不渲染（与 RenderRateLine 配套）。
// cached > denom（hit rate >100%）原值返回，由渲染层 clamp。
//
// 返回值：
//   - rate: 命中率（0.0 ~ N · 不 clamp · 渲染层处理 >100%）
//   - denom: 实际使用的分母（input 或 input+cached）
//
// Story 41.2 AC#2.
func ComputeCacheHitRate(driverType string, input, cached int) (rate float64, denom int) {
	if input == 0 {
		return 0, 0
	}
	switch driverType {
	case driverAnthropic:
		denom = input + cached
	default:
		// openai / deepseek / CLI drivers / 空 / unknown
		// → OpenAI 语义（input 已含 cached）
		denom = input
	}
	if denom == 0 {
		return 0, 0
	}
	return float64(cached) / float64(denom), denom
}

// dashboard-model-info：Meta lens ── Model ── 段渲染。
// =============================================================================

// friendlyMetaLabels maps known DriverMeta keys to human-friendly labels.
// Unknown keys fall back to the raw key name (see RenderModelSectionLines).
var friendlyMetaLabels = map[string]string{
	"resolved_bin":        "Binary:",
	"permission_mode":     "Permission:",
	"fallback_candidates": "Fallback:",
	"probe_duration_ms":   "Probe:",
}

// DashIfEmpty returns s, or a dash placeholder when s is empty. ASCII mode
// degrades the em-dash "—" to "-" so RNIX_ASCII=1 stays glyph-free. Shared by
// the Meta lens Model section and the detail card so the "missing value"
// convention is identical everywhere.
func DashIfEmpty(s string) string {
	if s != "" {
		return s
	}
	if ui.IsASCIIMode() {
		return "-"
	}
	return "—"
}

// RenderModelSectionLines renders the body lines of the Meta lens ── Model ──
// section: Provider / Model / Driver type, followed by every DriverMeta runtime
// diagnostic field. Labels are right-aligned to 10 columns and dimmed, matching
// the existing Tokens/Counts sections (RenderTokenLine's "%10s").
//
// DriverMeta handling:
//   - cap_* keys are aggregated onto a single "Cap:" line as
//     "partial-messages=true add-dir=true ..." (underscores → dashes);
//   - known keys use friendlyMetaLabels; unknown keys fall back to "<key>:";
//   - non-cap keys are emitted in sorted order for deterministic output;
//   - an empty meta map appends a single "(无运行时诊断)" line so non-CLI
//     drivers (which expose no DriverMeta) render cleanly without blank lines.
//
// Pure function: inputs are strings/maps, output is a slice of styled lines.
func RenderModelSectionLines(provider, model, effort, driverType string, meta map[string]string) []string {
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))
	line := func(label, value string) string {
		return fmt.Sprintf("%s %s", dim.Render(fmt.Sprintf("%10s", label)), value)
	}

	out := []string{
		line("Provider:", DashIfEmpty(provider)),
		line("Model:", DashIfEmpty(model)),
		line("Effort:", DashIfEmpty(effort)),
		line("Driver:", DashIfEmpty(driverType)),
	}

	if len(meta) == 0 {
		out = append(out, line("", "(no runtime diagnostics)"))
		return out
	}

	keys := make([]string, 0, len(meta))
	for k := range meta {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var caps []string
	for _, k := range keys {
		if name, ok := strings.CutPrefix(k, "cap_"); ok {
			if name == "" {
				continue // 跳过退化键 "cap_"（无能力名），避免渲染出 "=value"
			}
			name = strings.ReplaceAll(name, "_", "-")
			caps = append(caps, name+"="+meta[k])
			continue
		}
		label, ok := friendlyMetaLabels[k]
		if !ok {
			label = k + ":"
		}
		out = append(out, line(label, meta[k]))
	}
	if len(caps) > 0 {
		out = append(out, line("Cap:", strings.Join(caps, " ")))
	}
	return out
}
