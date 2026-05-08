// Package detail — render.go (Story 38-5 PR11 Step 4(c) · 第 2 个 pane render
// 主体迁出 · 同 alertstrip Step 4(a-2) 模式)
//
// Render() 主体迁出自 cmd/rnix/dashboard_detail.go::renderDetailPane · 1:1 行为
// 等价（Story 27-6 / 28-4 / 34-5 落地行为契约保留）。
//
// 接口签名变化（与 alertstrip / heatmap 同模式）：
//   - cmd/rnix renderDetailPane(*dashboardModel, width, height) string
//   - detail Render(state DetailState, ctx RenderContext, innerW int) string
//
// 解耦理由：renderDetailPane 原依赖 m.activePane/m.selectedPID/m.selectedUUID/
// m.detail.Detail 4 个 dashboardModel 字段。本 Render() 通过 RenderContext
// 显式注入前 3 个字段（SelectedPID/SelectedUUID/IsActive），第 4 个 detail.Detail
// 已在 PR5 Step 1 落地（DetailState.Detail）。
//
// renderFixedPanel 外层 border 由 cmd/rnix wrapper 包裹（同 heatmap pattern ·
// border / activePane 是 cmd/rnix 内部状态，不属于本 pane 内容渲染职责）。
package detail

import (
	"fmt"
	"strings"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
)

// RenderContext 注入 cmd/rnix 端运行时上下文（Story 38-5 PR11 Step 4(c) · 与
// heatmap.RenderContext 同模式）。
//
// 字段语义：
//   - SelectedPID/SelectedUUID: 当前选中进程（用于 PID 显示 + Detail 字段对齐
//     校验 · 防 stale data 跨 PID 切换 · Story 28-4 AC-4 contract）；
//   - IsActive: 当前 pane 是否为 activePane（影响 border 颜色 · 由 wrapper 应用
//     · Render() 本身不输出 border，只输出 inner content）。
type RenderContext struct {
	SelectedPID  types.PID
	SelectedUUID string
	IsActive     bool
}

// Render renders the Detail pane inner content (excluding outer border).
//
// Behaviour contract (preserved from cmd/rnix.renderDetailPane · zero behavior
// change · spec § AC5 detail pane preserved):
//   - SelectedPID == 0 → "Select a process to view detail"
//   - state.Detail == nil OR Detail.PID != SelectedPID OR
//     (SelectedUUID != "" AND Detail.UUID != SelectedUUID) → "Loading..."
//     （Story 28-4 AC-4 stale-data guard · UUID-keyed cache validation）
//   - 否则按 4 个 sections 渲染：Basic info / Allowed devices / Skills / FD Table
//     / Context stats（含 budget bar · pct 计算保留）。
//
// Performance: O(N) over Skills + FD Table + AllowedDevices · 实际 N ≤ ~30。
//
// Return value: inner content string · caller (cmd/rnix wrapper) 用
// renderFixedPanel(content, width, height, borderColor) 包裹外层 border。
func Render(state DetailState, ctx RenderContext, innerW int) string {
	var b strings.Builder

	b.WriteString(" Detail")
	if ctx.SelectedPID > 0 {
		fmt.Fprintf(&b, " | PID %d", ctx.SelectedPID)
	}
	b.WriteString("\n")

	if ctx.SelectedPID == 0 {
		b.WriteString("\n    Select a process to view detail")
		return b.String()
	}

	d := state.Detail
	if d == nil || d.PID != ctx.SelectedPID || (ctx.SelectedUUID != "" && d.UUID != ctx.SelectedUUID) {
		// Story 28-4 AC-4: stale-data guard · UUID-keyed cache validation prevents
		// cross-PID rendering when reused PID still holds previous process's Detail.
		b.WriteString("\n    Loading...")
		return b.String()
	}

	// Section 1: Basic info
	var uptime time.Duration
	if d.CreatedAtMs > 0 {
		created := time.UnixMilli(d.CreatedAtMs)
		if d.DeadAtMs > 0 {
			uptime = time.UnixMilli(d.DeadAtMs).Sub(created)
		} else {
			uptime = time.Since(created)
		}
	}
	fmt.Fprintf(&b, "  PID: %d  UUID: %s\n", d.PID, TruncateUUID(d.UUID))
	fmt.Fprintf(&b, "  State: %s  Intent: %s\n", d.State, TruncateStr(d.Intent, 40))
	fmt.Fprintf(&b, "  Provider: %s  Model: %s\n", d.Provider, d.Model)
	fmt.Fprintf(&b, "  Uptime: %s\n", ui.FormatDuration(uptime))

	// Section 1b: Allowed devices
	if len(d.AllowedDevices) > 0 {
		fmt.Fprintf(&b, "  Devices: %s\n", strings.Join(d.AllowedDevices, ", "))
	}

	// Section 2: Skills
	b.WriteString("  ──── Skills ────\n")
	if len(d.Skills) == 0 {
		b.WriteString("    (none)\n")
	}
	for _, sk := range d.Skills {
		tools := strings.Join(sk.AllowedTools, ", ")
		if tools == "" {
			tools = "—"
		}
		fmt.Fprintf(&b, "    %s → %s\n", sk.Name, tools)
	}

	// Section 3: FD table
	b.WriteString("  ──── FD Table ────\n")
	if len(d.FDTable) == 0 {
		if d.State == "dead" {
			b.WriteString("    (closed)\n")
		} else {
			b.WriteString("    (empty)\n")
		}
	}
	for _, fd := range d.FDTable {
		fmt.Fprintf(&b, "    %d: %s\n", fd.FD, fd.DevicePath)
	}

	// Section 4: Context stats
	b.WriteString("  ──── Context ────\n")
	fmt.Fprintf(&b, "    %d msgs | %s tok\n", d.ContextStats.MessageCount, ui.FormatTokens(d.ContextStats.TokensUsed))
	if d.ContextStats.SlotMax > 0 {
		fmt.Fprintf(&b, "    %d/%d slots (%.0f%%)\n",
			d.ContextStats.SlotUsed, d.ContextStats.SlotMax, d.ContextStats.SlotPercentage)
	}
	if d.ContextStats.ContextBudget > 0 {
		barWidth := max(innerW-10, 10)
		filled := int(d.ContextStats.UsagePct / 100.0 * float64(barWidth))
		filled = min(filled, barWidth)
		bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
		pct := min(d.ContextStats.UsagePct, 100.0)
		fmt.Fprintf(&b, "    [%s] %.0f%%\n", bar, pct)
	}

	return b.String()
}

// TruncateUUID returns the first 8 characters of a UUID string for display.
//
// Migrated from cmd/rnix/dashboard_detail.go::truncateUUID (Story 38-5 PR11
// Step 4(c)). Public so other dashboard sub-packages can reuse if needed.
func TruncateUUID(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

// TruncateStr truncates a string to maxLen runes (UTF-8 safe), appending "..."
// when truncated. maxLen < 4 disables truncation (the "..." would consume all
// budget so falling back to the original string is more useful).
//
// Migrated from cmd/rnix/dashboard_detail.go::truncateStr (Story 38-5 PR11
// Step 4(c)).
func TruncateStr(s string, maxLen int) string {
	if maxLen < 4 {
		return s
	}
	runes := []rune(s)
	if len(runes) > maxLen {
		return string(runes[:maxLen-3]) + "..."
	}
	return s
}
