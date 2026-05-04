// Package timeline — render.go (Story 38-5 PR11 Step 4(c) timeline render body
// 主体迁出的第一个 commit · renderStepFilterBar 子 render block)
//
// 本文件迁出 timeline pane 的 render body 子块。第一个迁出目标 RenderStepFilterBar
// 是 timeline 中最独立的 render block：
//   - 仅依赖 TimelineState.StepFilters map[string]bool（通过参数传入 · 不需要 dashboardModel）
//   - 其他依赖均为已迁出的公开 helper / 常量：ActionColor / ActionAbbrev /
//     TruncateAnsi（本包） + EventCompact / EventBudget / EventExit / EventStall /
//     EventImmune（已迁至 internal/dashboard/event）
//   - 64 行函数体（含 row 1 + row 2 + 末尾 done 提示） · 行为契约固化在 38-3 落地
//
// 后续 commit 将逐步迁出更复杂的 render block：
//   - RenderUnifiedStepHeader（依赖 m.processes / m.unifiedEvents / m.selectedPID 共享 EventStream
//     字段 · 需要通过 RenderContext struct 注入运行时数据）
//   - RenderTimelinePane（顶层入口 · 调用所有子 render block）
//   - RenderStepTimeline（最大块 · ~480 行 · 依赖大量共享字段 · 留至最后）
//   - RenderAggregatedTimeline（aggregation mode · 依赖 step entries + filter）
//   - RenderExpandedDetail / RenderDebugDetail（expand mode 详情 · 依赖 detail cache）
//
// 与 PR11 Step 4(c) 其他 pane render 主体迁出（detail/security/intent/trace/eval）
// 同模式：先迁最独立的子 render block · 让 timeline 包逐步累积 render 能力 · 最终
// dashboard_timeline.go 瘦身为 thin wrapper · 不破坏 38-1/2/3/4 行为契约。
//
// **零 cmd/rnix 反向依赖**：本包只 import internal/dashboard/event + ui + lipgloss + stdlib。
package timeline

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/vfs"
)

// System event type 常量字符串字面量（与 internal/dashboard/event.Event* 等价 ·
// 自包含 · 避免反向 import event 包形成循环依赖）：
//
//   - event 包 import timeline（UnifiedEvent.StepEntry *timeline.StepEntry）·
//     timeline 反向 import event 会形成循环 · Go module 边界硬约束
//   - 6 个字符串字面量复制成本远低于循环依赖的工程成本（与 FormatDurationMs
//     在 timeline / trace / eval 三包各 8 行复制同模式）
//   - 行为契约保证：值与 event.EventCompact / EventBudget / EventExit / EventStall /
//     EventImmune 完全等价（"compact"/"budget"/"exit"/"stall"/"immune"）·
//     event 包变更时本地复制需同步更新（grep "compact\\|budget\\|exit\\|stall\\|immune" 检查）
const (
	stepEventCompact = "compact"
	stepEventBudget  = "budget"
	stepEventExit    = "exit"
	stepEventStall   = "stall"
	stepEventImmune  = "immune"
)

// SlowStepThresholdMs 定义"慢 step"的判定阈值（毫秒 · 与 cmd/rnix
// .slowStepThresholdMs 等价 · Story 38-5 PR11 Step 4(c) RenderAggregatedTimeline
// 迁出时一并公开化）。
//
// 用途：duration 超过该阈值的 step 在 timeline 渲染中以警告色（#E5C07B 黄色 ·
// duration 列前景色覆盖）显示，提示用户关注潜在性能瓶颈。
//
// 行为契约（不变性 · 与 cmd/rnix 端 const 完全等价）：
//   - 单位：毫秒（float64 · 与 ipc.StepSummaryWire.DurationMs 类型一致）
//   - 阈值：1000.0（即 1s · 经验值 · 落地 Story 27-3 / 36-3）
//   - 比较语义：strict greater than（s.DurationMs > SlowStepThresholdMs）
//   - 等于阈值时不上色（边界 case · 与 cmd/rnix 等价）
const SlowStepThresholdMs = 1000.0

// RenderStepFilterBar 渲染 timeline filter editing mode 的双行 header（与 cmd/rnix
// .renderStepFilterBar 等价 · Story 38-5 PR11 Step 4(c) 第一个 timeline render
// block 迁出 · 行为契约固化在 38-3 落地）。
//
// **行为契约（不变性 · ATDD 27-3 + 36-3 + 38-3 测试覆盖）**：
//   - Row 1 "Step:"（行首加 2 空格）：7 个 step action 类型 [t/p/a/c/s/r/z] 一行展示
//     - 每个 type 显示 "[<key>]<abbrev> <mark>" 格式
//     - mark = ✓（filter on · ActionColor 上色）/ · （filter off · 灰色）
//     - filter on 判定：filters == nil（默认全开）OR filters[action] == true
//   - Row 2 "Events:"（行首加 1 空格）：6 个 system event 类型 [C/b/x/X/T/i] 一行展示
//     - 每个 type 显示 "[<key>]<label> <mark>" 格式
//     - mark = ✓（filter on · 青色 #00CED1 默认 system event 颜色）/ ·（灰色）
//     - 相同 filter on 判定逻辑（含 nil 防御）
//   - 末尾固定提示 "  [*]all  f/Esc:done"（用 ColorMuted 灰色样式）
//   - 整体 TruncateAnsi 截断到 maxW（防止超宽 · profile-tolerant）
//
// **filters == nil 行为**：所有类型显示为 on（与 cmd/rnix 等价 · 防御默认空 map 渲染）。
// **maxW <= 0 行为**：返回空字符串（TruncateAnsi 边界保护）。
//
// 不再返回 (b strings.Builder) · 直接返回 string · 与其他 RenderXxx helper 同模式。
func RenderStepFilterBar(filters map[string]bool, maxW int) string {
	var b strings.Builder
	b.WriteString(" Step:  ")

	// Row 1: Step action types
	stepTypes := []struct {
		key    string
		label  string
		action string
	}{
		{"t", ActionAbbrev("tool_call"), "tool_call"},
		{"p", ActionAbbrev("plan"), "plan"},
		{"a", ActionAbbrev("text"), "text"},
		{"c", ActionAbbrev("complete"), "complete"},
		{"s", ActionAbbrev("spawn"), "spawn"},
		{"r", ActionAbbrev("replan"), "replan"},
		{"z", ActionAbbrev("specialize"), "specialize"},
	}

	for _, t := range stepTypes {
		on := filters == nil || filters[t.action]
		mark := "✓"
		color := ActionColor(t.action)
		if !on {
			mark = "·"
			color = lipgloss.Color(ui.ColorMuted)
		}
		catStyle := lipgloss.NewStyle().Foreground(color)
		fmt.Fprintf(&b, " [%s]%s %s", t.key, catStyle.Render(t.label), mark)
	}

	// Row 2: System event types
	b.WriteString("\n Events:")

	sysTypes := []struct {
		key       string
		label     string
		eventType string
	}{
		{"C", "compact", stepEventCompact},
		{"b", "budget", stepEventBudget},
		{"x", "spawn", "sys_spawn"},
		{"X", "exit", stepEventExit},
		{"T", "stall", stepEventStall},
		{"i", "immune", stepEventImmune},
	}

	for _, t := range sysTypes {
		on := filters == nil || filters[t.eventType]
		mark := "✓"
		color := lipgloss.Color("#00CED1") // default system event color
		if !on {
			mark = "·"
			color = lipgloss.Color(ui.ColorMuted)
		}
		catStyle := lipgloss.NewStyle().Foreground(color)
		fmt.Fprintf(&b, " [%s]%s %s", t.key, catStyle.Render(t.label), mark)
	}
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))
	b.WriteString("  [*]all  " + dimStyle.Render("f/Esc:done"))
	return TruncateAnsi(b.String(), maxW)
}

// RenderDebugDetail 渲染 timeline expand mode 下 step 对应的 debug detail block（与
// cmd/rnix.renderDebugDetail 等价 · Story 38-5 PR11 Step 4(c) 第二个 timeline render
// block 迁出 · Story 27-3/27-4 落地 · CLI driver no-history fallback 27-4 落地）。
//
// **行为契约（不变性 · ATDD 27-3 + 27-4 + 36-3 测试覆盖）**：
//   - 顶部分隔线 "┊─────"（最多 60 个 ─）
//   - 无 Messages 时：CLI driver 提示 "CLI driver — no message history" + 操作提示
//     "↳ 按 p 查看 system prompt"（对齐尾部 38 列 · 27-4 落地）
//   - 有 Messages 时：
//     - "Messages (N)"  header + 右对齐的 "累计 X tok"（formatTokenCount k 后缀）
//     - 末尾 N 条 message preview（rune-width 60 列截断 · 用 roleStyle 渲染 role tag）
//     - 末尾操作提示 "↳ 按 p 查看完整 prompt"
//   - 全部受 maxLines 约束（每行写入前先检查 lines < maxLines · 防溢出）
//   - 所有 ┊ 边线 / 提示 / role tag 用 dimStyle 灰色渲染
//
// **依赖注入**：roleStyle func(role string) string 由调用方提供（cmd/rnix 端
// promptRoleForRole · 避免 timeline 包持有 4 个 promptRole* lipgloss.Style 拷贝
// 跨 inspector / debug 共享 · 与 PR11 Step 4(c) 其他 RenderXxx 同模式）。
//
// **副作用**：对传入的 b *strings.Builder 进行 append（caller 复用 buffer）·
// 返回值为本块写入的行数（caller 用于 lines 计数 · 与 RenderExpandedDetail 同模式）。
//
// detail == nil 行为：caller 必须保证 detail 非 nil（cmd/rnix renderStepTimeline 在
// 调用前已守卫 · 与原函数行为一致 · 不引入 nil-safety 改变契约）。
func RenderDebugDetail(b *strings.Builder, detail *ipc.GetStepDetailResponse, maxW, maxLines int, roleStyle func(role string) string) int {
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	lines := 0

	// Separator
	if lines < maxLines {
		sep := strings.Repeat("─", min(maxW-6, 60))
		fmt.Fprintf(b, "   %s\n", dimStyle.Render("┊"+sep))
		lines++
	}

	msgCount := len(detail.Messages)

	if msgCount == 0 {
		// CLI driver process: no message history available
		if lines < maxLines {
			fmt.Fprintf(b, "   %s %s\n", dimStyle.Render("┊"), dimStyle.Render("CLI driver — no message history"))
			lines++
		}
		if lines < maxLines {
			fmt.Fprintf(b, "   %s %s\n", dimStyle.Render("┊"), dimStyle.Render("                                      ↳ 按 p 查看 system prompt"))
			lines++
		}
		return lines
	}

	// Messages header
	if lines < maxLines {
		totalTok := FormatTokenCount(detail.TokenCount)
		fmt.Fprintf(b, "   %s Messages (%d)%s%s\n",
			dimStyle.Render("┊"),
			detail.MessageCount,
			strings.Repeat(" ", max(maxW-30-len(totalTok), 2)),
			dimStyle.Render("累计 "+totalTok+" tok"))
		lines++
	}

	// Message preview
	showCount := min(msgCount, min(maxLines-lines, 6))
	showCount = max(showCount, min(msgCount, 2))
	// Show the last N messages
	startMsg := max(msgCount-showCount, 0)
	for i := startMsg; i < msgCount && lines < maxLines; i++ {
		msg := detail.Messages[i]
		rs := roleStyle(msg.Role)
		content := msg.Content
		if runewidth.StringWidth(content) > 60 {
			content = runewidth.Truncate(content, 57, "…")
		}
		fmt.Fprintf(b, "   %s  %s %s\n", dimStyle.Render("┊"), rs, content)
		lines++
	}

	// Hint
	if lines < maxLines {
		fmt.Fprintf(b, "   %s %s\n", dimStyle.Render("┊"), dimStyle.Render("                                      ↳ 按 p 查看完整 prompt"))
		lines++
	}

	return lines
}

// HeaderContext 聚合 RenderUnifiedStepHeader 渲染所需的运行时数据 · 避免参数列表过长。
//
// **共享 EventStream 字段**：Processes / SelectedPID / SelectedUUID / TotalEvents 是
// dashboardModel App-level 共享状态（spec § Dev Notes 关键架构决策 6 保留在 App Model）·
// 通过参数传入而不是把 timeline 包反向 import cmd/rnix · 与 RenderDebugDetail
// roleStyle 依赖注入同模式。
type HeaderContext struct {
	State        TimelineState  // Timeline 自身状态（StepEntries/SortAsc/ExpandMode/StepCursor/StepFilters）
	Processes    []vfs.ProcInfo // 用于查找 selectedPID 对应的 CreatedAt（wall-clock display）· 与 tree 包共享 vfs.ProcInfo
	SelectedPID  types.PID      // 当前选中 PID（0 表示无选择）
	SelectedUUID string         // 当前选中 UUID（空字符串表示按 PID 匹配 · 28-4 PID 复用契约）
	TotalEvents  int            // len(m.unifiedEvents) 共享 EventStream 总事件数（用于 filter 指示）
}

// RenderUnifiedStepHeader 渲染 timeline pane 的统一 step header（与
// cmd/rnix.renderUnifiedStepHeader 等价 · Story 38-5 PR11 Step 4(c) 第四个 timeline render
// block 迁出 · Story 27-3 / 36-3 / 36-4 落地的复合 header）。
//
// **行为契约（不变性 · ATDD 27-3 + 36-3 + 36-4 测试覆盖）**：
//   - 标题 " Timeline" + " │ PID N" 当 SelectedPID > 0
//   - " │ HH:MM:SS" wall-clock（从 Processes 查 CreatedAt · 28-4 UUID-keyed 优先 · PID fallback）
//   - " │ N steps" + " + N events" 当 sysCount > 0
//   - **Story 36-4 排序方向 + expandMode 指示**（dim 颜色）：
//     - SortAsc=true: "↑ 旧→新"（ASCII: "^ old->new"）
//     - SortAsc=false: "↓ 新→旧"（ASCII: "v new->old"）
//     - ExpandMode=Expanded: "· all"（ASCII: "- all"）
//     - ExpandMode=ErrorsOnly: "· errors"（ASCII: "- errors"）
//   - " │ X tok" 总 token（FormatTokenCount k 后缀 · 当 totalTok > 0）
//   - **Stage statistics**（maxW >= 100 时）：按 plan/tool_call/spawn/specialize/replan/text/complete
//     顺序显示 "abbrev:N"（ActionColor 上色）+ "err:N"（红色 · 当 errCount > 0）
//   - " │ pos/total" 滚动位置（maxW >= 80 时）
//   - **Filter 指示**（filteredCount < TotalEvents 时）："filter: N/M -hidden_types"
//     hidden labels 列表：tool/plan/txt/done/spn/rpl/spec/cmp/bgt/sspn/exit/stl/imm
//   - 整体 TruncateAnsi 截断到 maxW
//
// 参数：HeaderContext + maxW + totalSteps + filteredCount + sysCount。
func RenderUnifiedStepHeader(ctx HeaderContext, maxW, totalSteps, filteredCount, sysCount int) string {
	var b strings.Builder
	b.WriteString(" Timeline")
	if ctx.SelectedPID > 0 {
		fmt.Fprintf(&b, " │ PID %d", ctx.SelectedPID)
	}
	// Wall-clock start time for selected process
	for _, p := range ctx.Processes {
		if p.PID == ctx.SelectedPID && (ctx.SelectedUUID == "" || p.UUID == ctx.SelectedUUID) {
			if !p.CreatedAt.IsZero() {
				fmt.Fprintf(&b, " │ %s", ui.FormatWallClock(p.CreatedAt))
			}
			break
		}
	}
	fmt.Fprintf(&b, " │ %d steps", totalSteps)
	if sysCount > 0 {
		fmt.Fprintf(&b, " + %d events", sysCount)
	}

	// Story 36-4: 排序方向 & expandMode 指示（dim 颜色，放在 steps 数量之后）
	{
		dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))
		ascii := ui.IsASCIIMode()
		var dirText string
		if ctx.State.SortAsc {
			if ascii {
				dirText = "^ old->new"
			} else {
				dirText = "↑ 旧→新"
			}
		} else {
			if ascii {
				dirText = "v new->old"
			} else {
				dirText = "↓ 新→旧"
			}
		}
		fmt.Fprintf(&b, " %s", dimStyle.Render("│ "+dirText))
		switch ctx.State.ExpandMode {
		case ExpandModeExpanded:
			sep := "·"
			if ascii {
				sep = "-"
			}
			fmt.Fprintf(&b, " %s", dimStyle.Render(sep+" all"))
		case ExpandModeErrorsOnly:
			sep := "·"
			if ascii {
				sep = "-"
			}
			fmt.Fprintf(&b, " %s", dimStyle.Render(sep+" errors"))
		}
	}

	// Total tokens from step summaries
	totalTok := 0
	for _, e := range ctx.State.StepEntries {
		totalTok += e.Summary.TokenCount
	}
	if totalTok > 0 {
		fmt.Fprintf(&b, " │ %s tok", FormatTokenCount(totalTok))
	}

	// Stage statistics (wide screens only)
	if maxW >= 100 && totalSteps > 0 {
		counts := make(map[string]int)
		errCount := 0
		for _, e := range ctx.State.StepEntries {
			counts[e.Summary.Action]++
			if e.Summary.HasError {
				errCount++
			}
		}
		b.WriteString(" │")
		for _, action := range []string{"plan", "tool_call", "spawn", "specialize", "replan", "text", "complete"} {
			if c, ok := counts[action]; ok && c > 0 {
				color := ActionColor(action)
				label := ActionAbbrev(action)
				fmt.Fprintf(&b, " %s", lipgloss.NewStyle().Foreground(color).Render(fmt.Sprintf("%s:%d", label, c)))
			}
		}
		if errCount > 0 {
			fmt.Fprintf(&b, " %s", lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError)).Render(fmt.Sprintf("err:%d", errCount)))
		}
	}

	// Scroll position (medium+ screens)
	if maxW >= 80 && filteredCount > 0 {
		pos := min(ctx.State.StepCursor+1, filteredCount)
		fmt.Fprintf(&b, " │ %d/%d", pos, filteredCount)
	}

	// Filter indicator
	if filteredCount < ctx.TotalEvents {
		// Build list of disabled type names for quick insight
		allTypes := []struct{ key, label string }{
			{"tool_call", "tool"}, {"plan", "plan"}, {"text", "txt"},
			{"complete", "done"}, {"spawn", "spn"}, {"replan", "rpl"}, {"specialize", "spec"},
			{stepEventCompact, "cmp"}, {stepEventBudget, "bgt"}, {"sys_spawn", "sspn"},
			{stepEventExit, "exit"}, {stepEventStall, "stl"}, {stepEventImmune, "imm"},
		}
		var hidden []string
		for _, t := range allTypes {
			if !ctx.State.StepFilters[t.key] {
				hidden = append(hidden, t.label)
			}
		}
		dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))
		label := fmt.Sprintf("filter: %d/%d", filteredCount, ctx.TotalEvents)
		if len(hidden) > 0 {
			label += " -" + strings.Join(hidden, ",")
		}
		fmt.Fprintf(&b, "  %s", dimStyle.Render(label))
	}

	return TruncateAnsi(b.String(), maxW)
}

// RenderExpandedDetail 渲染 timeline expand mode 下 step 对应的详细信息块（与
// cmd/rnix.renderExpandedDetail 等价 · Story 38-5 PR11 Step 4(c) 第三个 timeline render
// block 迁出 · Story 27-3 落地 · 三级 detail 控制）。
//
// **行为契约（不变性 · ATDD 27-3 测试覆盖 · 与 hasExpandableContent / EstimateExpandedLines
// 判断逻辑等价）**：
//   - ToolPath：当 detail.ToolPath != Level 1 displaySummary 时显示（去重 Level 1 已展示）
//   - ToolInput：tool_call 类型 · 超 contentW 则 runewidth 截断 + "(N bytes)" 后缀
//   - ToolError 或 ToolResult（互斥 · error 优先）：
//     - 多行（>3 行）截断显示 "… (N more lines)" hint
//     - error 用 ui.ColorError 红色样式 · result 默认色
//   - RawResponse：fallback · 当无 Input/Error/Result 时展示前 3 行 + 省略 hint
//   - Token breakdown："%d req → %d resp" · 当 RequestTokens/ResponseTokens != TokenCount 时显示
//   - Fallback fall-through：当所有字段去重或空时尝试展示完整 ToolPath / RawResponse
//   - 全部受 maxLines 约束（每行写入前先检查 lines < maxLines · 防溢出）
//
// **零依赖注入**：本函数为完全 pure helper · 不依赖 dashboardModel 状态 · 无 receiver。
//
// **副作用**：对传入的 b *strings.Builder 进行 append（caller 复用 buffer）·
// 返回值为本块写入的行数（caller 用于 lines 计数 · 与 RenderDebugDetail 同模式）。
//
// **样式 hex 字面量**：
//   - #888888 ↔ ui.ColorMuted（避免引入 ui 常量耦合）· 与 cmd/rnix.renderExpandedDetail 等价
//   - #4EC9B0 ↔ pathStyle 青色 · 沿用 27-3 落地的调色板（spec 38-2 之前已固化）
//   - ui.ColorError ↔ errStyle 红色（公开常量复用）
func RenderExpandedDetail(b *strings.Builder, detail *ipc.GetStepDetailResponse, s ipc.StepSummaryWire, maxW, maxLines int) int {
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError))
	lines := 0
	contentW := max(maxW-14, 20) // 3 indent + "┊" + " " + 8 label + " " padding

	// ToolPath — skip when Level 1 already shows it as displaySummary
	displayedAsSummary := s.Summary
	if s.ToolPath != "" && len(s.Summary) < 8 {
		displayedAsSummary = s.ToolPath
	}
	if detail.ToolPath != "" && detail.ToolPath != displayedAsSummary && lines < maxLines {
		pathLabel := detail.ToolPath
		if runewidth.StringWidth(pathLabel) > contentW {
			pathLabel = runewidth.Truncate(pathLabel, contentW-1, "…")
		}
		pathStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#4EC9B0"))
		fmt.Fprintf(b, "   %s %s %s\n", dimStyle.Render("┊"), dimStyle.Render("   Path"), pathStyle.Render(pathLabel))
		lines++
	}

	// Input (tool_call only)
	if detail.ToolInput != "" && lines < maxLines {
		input := detail.ToolInput
		if runewidth.StringWidth(input) > contentW {
			totalBytes := len(detail.ToolInput)
			input = runewidth.Truncate(input, contentW-15, "") + fmt.Sprintf("… (%d bytes)", totalBytes)
		}
		fmt.Fprintf(b, "   %s %s %s\n", dimStyle.Render("┊"), dimStyle.Render("  Input"), input)
		lines++
	}

	// Result or Error
	if detail.ToolError != "" && lines < maxLines {
		errMsg := detail.ToolError
		errLines := strings.Split(errMsg, "\n")
		if len(errLines) > 3 {
			errMsg = strings.Join(errLines[:3], "\n") + fmt.Sprintf("\n… (%d more lines)", len(errLines)-3)
		}
		for i, el := range strings.Split(errMsg, "\n") {
			if lines >= maxLines {
				break
			}
			if i == 0 {
				fmt.Fprintf(b, "   %s %s %s\n", dimStyle.Render("┊"), dimStyle.Render("  Error"), errStyle.Render(el))
			} else {
				fmt.Fprintf(b, "   %s %s %s\n", dimStyle.Render("┊"), dimStyle.Render("       "), errStyle.Render(el))
			}
			lines++
		}
	} else if detail.ToolResult != "" && lines < maxLines {
		result := detail.ToolResult
		resultLines := strings.Split(result, "\n")
		if len(resultLines) > 3 {
			result = strings.Join(resultLines[:3], "\n") + fmt.Sprintf("\n… (%d more lines)", len(resultLines)-3)
		}
		for i, rl := range strings.Split(result, "\n") {
			if lines >= maxLines {
				break
			}
			if i == 0 {
				fmt.Fprintf(b, "   %s %s %s\n", dimStyle.Render("┊"), dimStyle.Render(" Result"), rl)
			} else {
				fmt.Fprintf(b, "   %s %s %s\n", dimStyle.Render("┊"), dimStyle.Render("       "), rl)
			}
			lines++
		}
	} else if detail.RawResponse != "" && lines < maxLines {
		// Show RawResponse snippet as fallback for any action type
		rawLines := strings.Split(detail.RawResponse, "\n")
		showLines := min(3, len(rawLines))
		for i := range showLines {
			if lines >= maxLines {
				break
			}
			fmt.Fprintf(b, "   %s %s\n", dimStyle.Render("┊"), rawLines[i])
			lines++
		}
		if len(rawLines) > 3 && lines < maxLines {
			fmt.Fprintf(b, "   %s %s\n", dimStyle.Render("┊"), dimStyle.Render(fmt.Sprintf("… (%d more lines)", len(rawLines)-3)))
			lines++
		}
	}

	// Token breakdown — skip when total already shown in Level 1 and breakdown matches
	if (detail.RequestTokens > 0 || detail.ResponseTokens > 0) && (s.TokenCount == 0 || detail.RequestTokens+detail.ResponseTokens != s.TokenCount) && lines < maxLines {
		fmt.Fprintf(b, "   %s %s %d req → %d resp\n", dimStyle.Render("┊"), dimStyle.Render("  Token"), detail.RequestTokens, detail.ResponseTokens)
		lines++
	}

	// Fallback: if all fields were deduped or empty (should be rare — hasExpandableContent prevents most cases)
	if lines == 0 {
		// Try full ToolPath if Level 1 showed a truncated Summary
		if detail.ToolPath != "" && detail.ToolPath != displayedAsSummary {
			tp := detail.ToolPath
			if runewidth.StringWidth(tp) > contentW {
				tp = runewidth.Truncate(tp, contentW-1, "…")
			}
			fmt.Fprintf(b, "   %s %s\n", dimStyle.Render("┊"), dimStyle.Render(tp))
			lines++
		}
		// Try RawResponse as last resort
		if lines == 0 && detail.RawResponse != "" {
			raw := detail.RawResponse
			if runewidth.StringWidth(raw) > contentW {
				raw = runewidth.Truncate(raw, contentW-1, "…")
			}
			fmt.Fprintf(b, "   %s %s\n", dimStyle.Render("┊"), dimStyle.Render(raw))
			lines++
		}
	}

	return lines
}

// AggGroupSize 定义 aggregation mode 的 step 分组大小（每 50 个连续 step 聚合为
// 一个 group · 与 cmd/rnix.renderAggregatedTimeline 等价 · Story 27-3 落地）。
//
// 行为契约：固定值 50（不可调整 · 经验值 · 平衡渲染密度与上下文跳跃成本）。
const AggGroupSize = 50

// RenderAggregatedTimeline 渲染 timeline 的 aggregation mode（每 50 个 step
// 聚合为 group · 折叠态显示 group 概要 · 展开态显示 group header + 个别 step 行）。
//
// **行为契约（不变性 · ATDD 27-3 + 36-3 + 36-4 测试覆盖）**：
//   - Group 划分：按 filtered 数组顺序每 AggGroupSize=50 个连续 entry 切一个 group
//     - g.firstStep / g.lastStep 取自 group 内第一个 / 最后一个 entry 的 step number
//     - g.actionCounts 累计 group 内每种 action 出现次数
//     - g.errCount / g.totalTokens 累计 HasError / TokenCount
//   - Cursor group 计算：cursorFilterIdx = min(state.StepCursor, len(filtered)-1)
//     · cursorGroupIdx = cursorFilterIdx / AggGroupSize · 越界 clamp 到 len(groups)-1
//   - 滚动起点 startGi：从 0 开始递增 · 直到 [startGi..cursorGroupIdx] 总高度 ≤ listLines
//     · 保证 cursor group 始终在视野内（与 cmd/rnix 等价）
//   - 折叠态行（!ExpandedAggGroups[gi]）：1 行
//     · cursorMark "▸ " 或 "  " · marker "▸"（cursor 在该 group 时退避为空格避免双 ▸）
//     · "Steps {first}-{last}: <action-summary>[, {N} error(s)]<token>"
//     · action-summary 按固定顺序 [tool_call/plan/text/spawn/specialize/replan/complete]
//       渲染（每个 type 用 ActionColor 上色 · 跳过 count==0）
//     · cursor 在 group 内 → 整行 #2D2D3D 背景 + #FFFFFF 前景高亮
//   - 展开态（ExpandedAggGroups[gi]==true）：1 行 header + N 行 entries
//     · header "  ▾ Steps {first}-{last}"（Muted 灰色）
//     · entry 行同 RenderStepTimeline 的 collapsed entry 格式（步号 + abbrev +
//       optional token + optional duration + optional ✗ + summary）
//     · summary 优先 ToolPath（当 Summary 长度 < 8 时）
//     · duration 列：超过 SlowStepThresholdMs 用 #E5C07B 黄色提示
//     · HasError → 整行 #3D1F1F 背景 · cursor → #2D2D3D 背景 + #FFFFFF 前景
//   - linesUsed 上限 listLines · 每写一行后先检查再继续
//
// **依赖（全部已迁出至 timeline 包）**：ActionColor / ActionAbbrev /
// FormatTokenCount / FormatDurationMs / TruncateAnsi / TruncateRuneWidth /
// SlowStepThresholdMs · 跨包仅依赖 internal/ui (ColorError / ColorMuted) +
// lipgloss · 零 cmd/rnix 反向依赖。
//
// **签名设计**：
//   - 接受 TimelineState（值类型 · 仅读 StepEntries / StepCursor /
//     ExpandedAggGroups 三个字段 · 不修改）
//   - 返回 linesUsed（实际写入行数 · 调用方用于后续布局计算）
//   - 写入 *strings.Builder（与 cmd/rnix.renderAggregatedTimeline 同模式 ·
//     避免大字符串拷贝 · 调用方负责 b 的生命周期）
func RenderAggregatedTimeline(b *strings.Builder, state TimelineState, filtered []int, truncW, listLines int, showToken, showDuration bool) int {
	// dimStyle 使用 #888888 而非 ui.ColorMuted (#666666) · 与 cmd/rnix
	// .renderAggregatedTimeline 1:1 行为等价（aggregation mode 用稍亮的灰色 ·
	// 与 step entry 行 dim style 区分 · ATDD 27-3 视觉契约）。
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))

	type aggGroup struct {
		startIdx     int // index in filtered array
		endIdx       int // exclusive
		firstStep    int // step number of first entry
		lastStep     int // step number of last entry
		actionCounts map[string]int
		errCount     int
		totalTokens  int
	}

	// Build groups
	var groups []aggGroup
	for i := 0; i < len(filtered); i += AggGroupSize {
		end := min(i+AggGroupSize, len(filtered))
		g := aggGroup{
			startIdx:     i,
			endIdx:       end,
			actionCounts: make(map[string]int),
		}
		for fi := i; fi < end; fi++ {
			entry := state.StepEntries[filtered[fi]]
			s := entry.Summary
			if fi == i {
				g.firstStep = s.Step
			}
			g.lastStep = s.Step
			g.actionCounts[s.Action]++
			if s.HasError {
				g.errCount++
			}
			g.totalTokens += s.TokenCount
		}
		groups = append(groups, g)
	}

	linesUsed := 0
	cursorFilterIdx := min(state.StepCursor, len(filtered)-1)

	// F3: Calculate group heights and find start group for viewport scrolling
	cursorGroupIdx := 0
	if cursorFilterIdx >= 0 && AggGroupSize > 0 {
		cursorGroupIdx = cursorFilterIdx / AggGroupSize
	}
	if cursorGroupIdx >= len(groups) {
		cursorGroupIdx = max(len(groups)-1, 0)
	}

	groupHeights := make([]int, len(groups))
	for gi, g := range groups {
		if state.ExpandedAggGroups[gi] {
			groupHeights[gi] = 1 + (g.endIdx - g.startIdx) // header + entries
		} else {
			groupHeights[gi] = 1
		}
	}

	// Find start group: scroll forward until cursor group fits in view
	startGi := 0
	for startGi < cursorGroupIdx {
		linesFromStart := 0
		for gi := startGi; gi <= cursorGroupIdx; gi++ {
			linesFromStart += groupHeights[gi]
		}
		if linesFromStart <= listLines {
			break
		}
		startGi++
	}

	for gi := startGi; gi < len(groups) && linesUsed < listLines; gi++ {
		g := groups[gi]
		if linesUsed >= listLines {
			break
		}

		isExpanded := state.ExpandedAggGroups[gi]
		// Check if cursor is in this group
		cursorInGroup := cursorFilterIdx >= g.startIdx && cursorFilterIdx < g.endIdx

		if !isExpanded {
			// Render aggregation summary line
			marker := "▸"
			cursorMark := "  "
			if cursorInGroup {
				cursorMark = "▸ "
				marker = " " // cursor mark replaces fold marker to avoid double ▸
			}

			// Build action summary
			var actionParts []string
			for _, action := range []string{"tool_call", "plan", "text", "spawn", "specialize", "replan", "complete"} {
				if c, ok := g.actionCounts[action]; ok && c > 0 {
					color := ActionColor(action)
					label := ActionAbbrev(action)
					actionParts = append(actionParts, lipgloss.NewStyle().Foreground(color).Render(fmt.Sprintf("%d %s", c, label)))
				}
			}
			actionSummary := strings.Join(actionParts, ", ")

			errPart := ""
			if g.errCount > 0 {
				noun := "error"
				if g.errCount > 1 {
					noun = "errors"
				}
				errPart = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError)).Render(fmt.Sprintf(", %d %s", g.errCount, noun))
			}

			line := fmt.Sprintf("%s%s Steps %d-%d: %s%s",
				cursorMark, marker, g.firstStep, g.lastStep, actionSummary, errPart)

			if showToken && g.totalTokens > 0 {
				line += dimStyle.Render(fmt.Sprintf("  %s", FormatTokenCount(g.totalTokens)))
			}

			if cursorInGroup {
				line = lipgloss.NewStyle().
					Background(lipgloss.Color("#2D2D3D")).
					Foreground(lipgloss.Color("#FFFFFF")).
					Render(line)
			}

			b.WriteString(TruncateAnsi(line, truncW))
			b.WriteString("\n")
			linesUsed++
		} else {
			// Render expanded: group header + individual steps
			header := fmt.Sprintf("  ▾ Steps %d-%d", g.firstStep, g.lastStep)
			b.WriteString(dimStyle.Render(header))
			b.WriteString("\n")
			linesUsed++

			for fi := g.startIdx; fi < g.endIdx && linesUsed < listLines; fi++ {
				idx := filtered[fi]
				entry := state.StepEntries[idx]
				s := entry.Summary

				cursorMark := "  "
				if fi == state.StepCursor {
					cursorMark = "▸ "
				}

				stepAction := dimStyle.Render(fmt.Sprintf("%d %s", s.Step, ActionAbbrev(s.Action)))

				var tokenLabel string
				if showToken {
					if s.TokenCount == 0 {
						tokenLabel = dimStyle.Render("    —")
					} else {
						tokenLabel = dimStyle.Render(fmt.Sprintf("%5s", FormatTokenCount(s.TokenCount)))
					}
				}

				var durLabel string
				if showDuration {
					dur := FormatDurationMs(s.DurationMs)
					durStyle := dimStyle
					if s.DurationMs > SlowStepThresholdMs {
						durStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#E5C07B"))
					}
					durLabel = durStyle.Render(fmt.Sprintf("%6s", dur))
				}

				errMark := ""
				if s.HasError {
					errMark = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError)).Render(" ✗")
				}

				displaySummary := s.Summary
				if s.ToolPath != "" && len(s.Summary) < 8 {
					displaySummary = s.ToolPath
				}
				fixedWidth := 2 + 1 + 1 + 5
				if showToken {
					fixedWidth += 6
				}
				if showDuration {
					fixedWidth += 7
				}
				if s.HasError {
					fixedWidth += 2
				}
				summaryW := max(truncW-fixedWidth, 10)
				summaryText := TruncateRuneWidth(displaySummary, summaryW)

				var parts []string
				parts = append(parts, cursorMark, " ", summaryText)
				if showDuration {
					parts = append(parts, tokenLabel, durLabel, stepAction, errMark)
				} else if showToken {
					parts = append(parts, tokenLabel, stepAction, errMark)
				} else {
					parts = append(parts, stepAction, errMark)
				}
				line := strings.Join(parts, " ")

				if s.HasError {
					line = lipgloss.NewStyle().Background(lipgloss.Color("#3D1F1F")).Render(line)
				} else if fi == state.StepCursor {
					line = lipgloss.NewStyle().
						Background(lipgloss.Color("#2D2D3D")).
						Foreground(lipgloss.Color("#FFFFFF")).
						Render(line)
				}

				b.WriteString(TruncateAnsi(line, truncW))
				b.WriteString("\n")
				linesUsed++
			}
		}
	}

	return linesUsed
}
