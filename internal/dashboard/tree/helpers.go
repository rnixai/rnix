// Package tree — helpers.go (Story 38-5 PR2 Step 3a)
//
// Tree pane 渲染所依赖的纯 helper 函数集合，从 cmd/rnix/dashboard_tree.go
// 与 cmd/rnix/dashboard_history.go 迁入。这些函数仅依赖 vfs.ProcInfo / TreeState /
// 标准库 / internal/ui，**禁止反向依赖** cmd/rnix。
//
// 迁出来源：
//   - cmd/rnix/dashboard_tree.go::reusedPIDs / renderCtxBar / orchestrationAnnotation /
//     suspendReasonAbbrev / collapseCommonPrefix / longestCommonPrefix / truncateToWordBoundary /
//     filteredExpandedRows (method on dashboardModel) / buildCollapsedIntents (method)
//   - cmd/rnix/dashboard_history.go::agentLabel
//
// cmd/rnix 端保留 thin wrapper 以兼容现有调用点 + ATDD 测试 grep（如 dispatcher 调
// `m.filteredExpandedRows()`）。
package tree

import (
	"fmt"
	"strings"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/vfs"
)

// AgentLabel 返回 ProcInfo 的显示标签：优先 Model > Provider > 截断 Intent > "—"。
//
// 用于 Tree pane / History view 的进程行 label 显示。Intent 超过 20 rune 时截断为 17+省略号。
//
// 示例：
//
//	AgentLabel(p) => "claude-opus-4-7"  (Model 非空)
//	AgentLabel(p) => "anthropic"         (Model 空 · Provider="anthropic")
//	AgentLabel(p) => "build the auth..." (Model/Provider 都空 · Intent="build the authentication system")
//	AgentLabel(p) => "—"                 (全部为空 · 占位符)
func AgentLabel(p vfs.ProcInfo) string {
	if p.Model != "" {
		return p.Model
	}
	if p.Provider != "" {
		return p.Provider
	}
	if p.Intent != "" {
		runes := []rune(p.Intent)
		if len(runes) > 20 {
			return string(runes[:17]) + "..."
		}
		return p.Intent
	}
	return "—"
}

// ReusedPIDs 返回一个 PID → 计数映射，仅包含**复用**的 PID（出现 ≥ 2 次）。
//
// 用于 Tree pane 渲染时区分被复用的 PID（在显示时附加 UUID 短缀以避免歧义）。
// PID 复用通常发生在跨 daemon 重启或 OS 层 PID 回滚时。
//
// 性能上界：O(n)，n=进程数。typical n=10-100，调用成本可忽略。
func ReusedPIDs(procs []vfs.ProcInfo) map[types.PID]int {
	counts := make(map[types.PID]int)
	for _, p := range procs {
		counts[p.PID]++
	}
	reused := make(map[types.PID]int)
	for pid, count := range counts {
		if count > 1 {
			reused[pid] = count
		}
	}
	return reused
}

// RenderCtxBar 渲染 context 使用率进度条："ctx ████████░░ 78%"。
//
// 参数：
//   - tokensUsed:    当前已用 token 数
//   - contextBudget: context 总预算（≤ 0 时返回空字符串）
//   - barWidth:      条形字符数（典型值 10）
//
// ASCII 模式（RNIX_ASCII=1）：filled='#'  empty='.'
// Unicode 模式：filled='█'  empty='░'
//
// 颜色规则：
//   - pct > 80:  ErrorStyle（红色）
//   - pct ≥ 50:  WarningStyle（橙色）
//   - 默认:      SuccessStyle（绿色）
//
// 返回：lipgloss 着色后的进度条字符串；contextBudget ≤ 0 时返回 ""。
func RenderCtxBar(tokensUsed, contextBudget, barWidth int) string {
	if contextBudget <= 0 {
		return ""
	}
	pct := max(0, min(int(int64(tokensUsed)*100/int64(contextBudget)), 100))
	filled := max(0, barWidth*pct/100)

	ascii := ui.IsASCIIMode()
	var filledChar, emptyChar string
	if ascii {
		filledChar = "#"
		emptyChar = "."
	} else {
		filledChar = "█"
		emptyChar = "░"
	}

	bar := strings.Repeat(filledChar, filled) + strings.Repeat(emptyChar, barWidth-filled)
	label := fmt.Sprintf("ctx %s %d%%", bar, pct)

	switch {
	case pct > 80:
		return ui.ErrorStyle.Render(label)
	case pct >= 50:
		return ui.WarningStyle.Render(label)
	default:
		return ui.SuccessStyle.Render(label)
	}
}

// OrchestrationAnnotation 返回进程行的简要编排注解（compose / pipeline）。
//
// 编排类型：
//   - Compose 节点带依赖：    "╌╌►dep1,dep2"  / ASCII: "-->dep1,dep2"
//   - Compose 节点无依赖：    "◆node-name"    / ASCII: "[C:node-name]"
//   - Pipeline:               "│►[2/5]"        / ASCII: "|>[2/5]"
//   - 其他:                   ""（无注解）
//
// 依赖列表超过 30 字符时截断为 27+"..."。
func OrchestrationAnnotation(p vfs.ProcInfo) string {
	if p.ComposeNode != "" {
		if len(p.ComposeDeps) > 0 {
			deps := strings.Join(p.ComposeDeps, ",")
			if len(deps) > 30 {
				deps = deps[:27] + "..."
			}
			if ui.IsASCIIMode() {
				return "-->" + deps
			}
			return "╌╌►" + deps
		}
		if ui.IsASCIIMode() {
			return "[C:" + p.ComposeNode + "]"
		}
		return "◆" + p.ComposeNode
	}
	if p.PipelineTotal > 0 {
		label := fmt.Sprintf("[%d/%d]", p.PipelineIndex+1, p.PipelineTotal)
		if ui.IsASCIIMode() {
			return "|>" + label
		}
		return "│►" + label
	}
	return ""
}

// SuspendReasonAbbrev 把 SuspendReason 字符串映射为 Tree pane 行尾的简短 tag。
//
// Story 30.8 AC#6 落地。映射规则：
//
//	""                              → ""（无 tag）
//	"budget_exhausted*"             → "[budget]"
//	"heartbeat_timeout"             → "[stalled]"
//	"loop_detected"                 → "[loop]"
//	其他（含用户主动 suspend）         → "[user]"
func SuspendReasonAbbrev(reason string) string {
	switch {
	case reason == "":
		return ""
	case strings.HasPrefix(reason, "budget_exhausted"):
		return "[budget]"
	case reason == "heartbeat_timeout":
		return "[stalled]"
	case reason == "loop_detected":
		return "[loop]"
	default:
		return "[user]"
	}
}

// FilteredRows 返回 TreeState.Rows 中标签或 intent 文本（不区分大小写）匹配 SearchQuery 的子集。
//
// SearchQuery 为空时返回 state.Rows 不变（零拷贝）。
// 匹配 label = AgentLabel(row.Proc) 或 row.Proc.Intent。
//
// 用于 viewExpanded + paneTree 下的实时搜索过滤。每次按键调用一次，O(n)。
func FilteredRows(state TreeState) []FlatRow {
	if state.SearchQuery == "" {
		return state.Rows
	}
	q := strings.ToLower(state.SearchQuery)
	result := make([]FlatRow, 0, len(state.Rows))
	for _, row := range state.Rows {
		label := strings.ToLower(AgentLabel(row.Proc))
		intent := strings.ToLower(row.Proc.Intent)
		if strings.Contains(label, q) || strings.Contains(intent, q) {
			result = append(result, row)
		}
	}
	return result
}

// BuildCollapsedIntents 把同父 UUID 下的子进程 intent 公共前缀折叠为 "…/" 显示。
//
// AC4 (Story 34.3) 落地：当兄弟进程 intent 共享一段较长前缀（> avgLen/2）时，
// 渲染时把前缀替换为 "…/"（ASCII: ".../"）以让用户聚焦差异部分。
//
// 算法：
//  1. 按 ParentUUID（或 PPID 回退）分组；
//  2. 同组 intent 取最长公共前缀；
//  3. 若前缀长度 > 平均长度的一半，则替换为 "…/"；
//  4. 返回 UUID → collapsed intent 的映射（仅包含被折叠的进程）。
//
// 性能上界：O(n×m)，n=进程数，m=intent 平均长度。typical n=10-50，m=20-200，可忽略。
func BuildCollapsedIntents(state TreeState) map[string]string {
	result := make(map[string]string)
	if len(state.Rows) == 0 {
		return result
	}

	// reusedPID counts how many displayed rows hold each PID. A PID held by >1
	// row was recycled across daemon generations; the PPID fallback below must
	// not group siblings by such a PID, or unrelated trees' children would be
	// folded together (spec-agent-tree-uuid-build). Counted inline over the rows
	// to avoid copying ProcInfo structs on the per-tick render path.
	pidCount := make(map[types.PID]int, len(state.Rows))
	for _, row := range state.Rows {
		pidCount[row.Proc.PID]++
	}

	// Build parent UUID → child rows mapping from the tree structure
	parentChildren := make(map[string][]int) // parentUUID → indices in Rows
	for i, row := range state.Rows {
		puuid := row.Proc.ParentUUID
		if puuid != "" {
			parentChildren[puuid] = append(parentChildren[puuid], i)
			continue
		}
		// Fallback: PPID → UUID lookup for old processes without ParentUUID.
		// Skip when the parent PID is reused (ambiguous across daemon
		// generations) — grouping by it would merge unrelated siblings.
		if row.Proc.PPID > 0 && row.Proc.PPID != row.Proc.PID && pidCount[row.Proc.PPID] <= 1 {
			for _, other := range state.Rows {
				if other.Proc.PID == row.Proc.PPID && other.Proc.UUID != "" {
					parentChildren[other.Proc.UUID] = append(parentChildren[other.Proc.UUID], i)
					break
				}
			}
		}
	}

	// For each parent group with >1 children, try prefix collapse
	for _, indices := range parentChildren {
		if len(indices) <= 1 {
			continue
		}
		intents := make([]string, len(indices))
		for j, idx := range indices {
			intents[j] = state.Rows[idx].Proc.Intent
		}
		collapsed := collapseCommonPrefix(intents)
		// If collapse changed anything, store the results
		for j, idx := range indices {
			if collapsed[j] != intents[j] {
				result[state.Rows[idx].Proc.UUID] = collapsed[j]
			}
		}
	}

	return result
}

// CollapseCommonPrefix 把多个 intent 共享的前缀替换为 "…/"（ASCII: ".../"）。
//
// 公开 API（Story 38-5 PR2 Step 4）：替代 cmd/rnix/dashboard_tree.go 端的同名实现，
// 让 dashboard_test.go::34.3-UNIT-007/008 可以通过 cmd/rnix wrapper 调用。
//
// 仅当公共前缀长度 > 平均 intent 长度的 50% 时才折叠（避免短前缀过度折叠）。
// 前缀按 word boundary 截断（' ' / '/' / '-' / '_'）。
//
// 由 BuildCollapsedIntents 内部调用 + cmd/rnix/dashboard_tree.go::collapseCommonPrefix wrapper 调用。
func CollapseCommonPrefix(intents []string) []string {
	if len(intents) <= 1 {
		return intents
	}
	prefix := LongestCommonPrefix(intents)
	prefix = TruncateToWordBoundary(prefix)
	if len(prefix) == 0 {
		return intents
	}
	totalLen := 0
	for _, s := range intents {
		totalLen += len(s)
	}
	avgLen := totalLen / len(intents)
	if len(prefix) <= avgLen/2 {
		return intents
	}

	ellipsis := "…/"
	if ui.IsASCIIMode() {
		ellipsis = ".../"
	}

	result := make([]string, len(intents))
	changed := false
	for i, s := range intents {
		if len(prefix) >= len(s) {
			result[i] = s // prefix covers entire intent, don't collapse to empty
		} else {
			result[i] = ellipsis + s[len(prefix):]
			changed = true
		}
	}
	if !changed {
		return intents
	}
	return result
}

// collapseCommonPrefix 是 CollapseCommonPrefix 的包私有别名（向后兼容内部调用方）。
//
// 保留原签名以让 BuildCollapsedIntents / 内部 helper 测试继续直接调用。
func collapseCommonPrefix(intents []string) []string {
	return CollapseCommonPrefix(intents)
}

// LongestCommonPrefix 找出多个字符串的最长公共前缀（按 byte，不按 rune）。
//
// 公开 API（Story 38-5 PR2 Step 4）：替代 cmd/rnix 端同名 helper。
// 空输入返回 ""。
func LongestCommonPrefix(strs []string) string {
	if len(strs) == 0 {
		return ""
	}
	prefix := strs[0]
	for _, s := range strs[1:] {
		for len(prefix) > 0 && !strings.HasPrefix(s, prefix) {
			prefix = prefix[:len(prefix)-1]
		}
	}
	return prefix
}

// longestCommonPrefix 是 LongestCommonPrefix 的包私有别名。
func longestCommonPrefix(strs []string) string {
	return LongestCommonPrefix(strs)
}

// TruncateToWordBoundary 把字符串按最后一个 word boundary（'/' / ' ' / '-' / '_'）截断。
//
// 公开 API（Story 38-5 PR2 Step 4）：替代 cmd/rnix 端同名 helper。
// 无 boundary 则返回 ""（强制保持词完整性）。
//
// 示例：
//
//	TruncateToWordBoundary("foo/bar/baz")     => "foo/bar/"
//	TruncateToWordBoundary("foo bar baz")     => "foo bar "
//	TruncateToWordBoundary("nopunctuation")   => ""    （无边界，返回空）
func TruncateToWordBoundary(s string) string {
	if s == "" {
		return ""
	}
	lastBound := -1
	for i, c := range s {
		if c == '/' || c == ' ' || c == '-' || c == '_' {
			lastBound = i + 1 // include the boundary character
		}
	}
	if lastBound <= 0 {
		return ""
	}
	return s[:lastBound]
}

// truncateToWordBoundary 是 TruncateToWordBoundary 的包私有别名。
func truncateToWordBoundary(s string) string {
	return TruncateToWordBoundary(s)
}

// effectiveResult 返回 render 层失败判定应使用的 result 文本（Story 64.3 D5）。
//
// Story 64.1 归一化把 daemon-restart 杀死的非终态快照转为 dead + ExitReason=="interrupted" +
// Result=""；空 Result 会驱动 ui.isFailedResult("")→true（render.go 行标红 / badge 走失败分支），
// 与统计条 Interrupted 段（黄）矛盾。代入 "interrupted" 字面量让 ui.isFailedResult 既有
// interrupted 豁免（internal/ui/symbols.go）生效 → 行改 dim（非红）· badge 走 success 分支。
//
// 锚点：kernel/history_reconcile.go exitReasonInterrupted（写入源①64.1 归一化；
// ②kernel/cli_subagent.go synthetic 子代理 stream-end 兜底 finalize）。
// 边界：ui.IsProcessFailed 权威路径（ExitCodeSet=true）优先于 result 文本——本 fallback 仅对
// ExitCodeSet=false 条目有影响，恰是 interrupted 归一化条目形态（created/running 起源，未退出即落盘）；
// zombie 起源保留真实 ExitReason + ExitCodeSet=true，不匹配本 helper 也不受影响（按真实 exit code 归桶）。
func effectiveResult(p vfs.ProcInfo) string {
	if p.Result == "" && p.ExitReason == exitReasonInterrupted {
		return exitReasonInterrupted
	}
	return p.Result
}
