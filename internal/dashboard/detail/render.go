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
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/dashboard/timeline"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
)

// RenderContext 注入 cmd/rnix 端运行时上下文（Story 38-5 PR11 Step 4(c) · 与
// heatmap.RenderContext 同模式）。
//
// 字段语义：
//   - SelectedPID/SelectedUUID: 当前选中进程（用于 PID 显示 + Detail 字段对齐
//     校验 · 防 stale data 跨 PID 切换 · Story 28-4 AC-4 contract）；
//   - IsActive: 当前 pane 是否为 activePane（影响 border 颜色 · 由 wrapper 应用
//     · Render() 本身不输出 border，只输出 inner content）。
//   - HeartbeatStatus: render-time stall snapshot（Story 45.5 · 与
//     heatmap.RenderContext 同模式 render-time 注入运行时数据 · nil 时
//     renderStallSection skip · UUID-first / PID-fallback 匹配防止
//     PID 复用后的 stale stall 残留）。
type RenderContext struct {
	SelectedPID     types.PID
	SelectedUUID    string
	IsActive        bool
	HeartbeatStatus *ipc.HeartbeatStatusResponse
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
		uptime -= time.Duration(d.PausedTotalMs) * time.Millisecond
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
	fmt.Fprintf(&b, "    %d msgs | %s tok\n", d.ContextStats.MessageCount, timeline.FormatTokenCount(d.ContextStats.TokensUsed))
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

	// Section 5: Stall intensity heatmap (Story 45.5)
	// P4 daemon-passive supervision: daemon emits ProcessStalled events
	// but no longer drives cancel_step / suspend automatically. This
	// section surfaces stall intensity in the Detail pane so the user can
	// decide whether to press K to intervene. Section is omitted when the
	// selected process is not in HeartbeatStatus.CurrentStalled.
	renderStallSection(&b, ctx.SelectedPID, ctx.SelectedUUID, ctx.HeartbeatStatus, innerW)

	// Section 6: Resume Lineage (Story 42.3)
	// Only render when the process is the result of a resume/fork OR has fork
	// descendants. Skipping the section for plain spawn processes keeps the
	// Detail card noise-free.
	renderLineageSection(&b, d.UUID, d.OriginUUID, d.ResumedFromStep, state.LineageCache)

	return b.String()
}

// renderLineageSection emits the Story 42.3 Lineage block when the process is
// involved in a resume / fork chain. Section is omitted entirely when both
// OriginUUID and descendants are empty (avoids polluting plain spawn UI).
func renderLineageSection(b *strings.Builder, uuid, originUUID string, resumedFromStep int, cache map[string]*ipc.GetResumeLineageResponse) {
	// Resolve descendants (if any) from the lineage cache.
	descendants := lookupDescendants(uuid, cache)
	if originUUID == "" && len(descendants) == 0 {
		return
	}

	b.WriteString(divider("Lineage"))
	b.WriteString("\n")
	if originUUID != "" {
		short := TruncateUUID(originUUID)
		if resumedFromStep > 0 {
			fmt.Fprintf(b, "    Origin: %s (from step %d)\n", short, resumedFromStep)
		} else {
			fmt.Fprintf(b, "    Origin: %s\n", short)
		}
	}
	if n := len(descendants); n > 0 {
		// 渲染 "Forked: N descendants" 一行；后面再列出最多 3 个短哈希作为参考。
		fmt.Fprintf(b, "    Forked: %d descendants", n)
		if n <= 3 {
			hashes := make([]string, 0, n)
			for _, dst := range descendants {
				hashes = append(hashes, TruncateUUID(dst))
			}
			fmt.Fprintf(b, " (%s)", strings.Join(hashes, ", "))
		}
		b.WriteString("\n")
	}
}

// lookupDescendants returns the list of descendant UUIDs cached for the given
// current UUID. Returns empty slice when cache miss or no descendants.
func lookupDescendants(uuid string, cache map[string]*ipc.GetResumeLineageResponse) []string {
	if cache == nil || uuid == "" {
		return nil
	}
	entry, ok := cache[uuid]
	if !ok || entry == nil {
		return nil
	}
	if len(entry.Descendants) == 0 {
		return nil
	}
	out := make([]string, 0, len(entry.Descendants))
	for _, d := range entry.Descendants {
		if d.UUID != "" {
			out = append(out, d.UUID)
		}
	}
	return out
}

// renderStallSection emits the Story 45.5 Stall intensity block when the
// selected process appears in HeartbeatStatus.CurrentStalled. Borrows the
// fade-to-red intensity concept from cc-src Spinner/useStalledAnimation.ts
// (3s/2s thresholds → ConsecutiveStalls 4-level mapping aligned with daemon
// escalation: warn (<3) / cancel_step (==3) / suspend (>=4)).
//
// Skip cases (same skip-if-empty pattern as renderLineageSection):
//   - hb == nil
//   - len(hb.CurrentStalled) == 0
//   - selectedPID == 0
//   - no matching wire (UUID-first when selectedUUID != "", else PID fallback)
//
// Under P4 daemon-passive supervision (Epic 45) daemon no longer auto-cancels
// or auto-suspends — this section gives the user a direct visual signal so
// they can decide whether to intervene (press K) themselves.
func renderStallSection(b *strings.Builder, selectedPID types.PID, selectedUUID string, hb *ipc.HeartbeatStatusResponse, innerW int) {
	if hb == nil || len(hb.CurrentStalled) == 0 || selectedPID == 0 {
		return
	}

	// UUID-first matching prevents PID-reuse stale data leak (Story 28-4 AC-4
	// equivalent guard). When the caller did not supply a UUID (backward-
	// compatible fallback for older daemons), fall back to PID equality.
	var match *ipc.StalledProcWire
	for i := range hb.CurrentStalled {
		sp := &hb.CurrentStalled[i]
		if selectedUUID != "" {
			if sp.UUID == selectedUUID {
				match = sp
				break
			}
		} else if sp.PID == selectedPID {
			match = sp
			break
		}
	}
	if match == nil {
		return
	}

	// Clamp ConsecutiveStalls into the 4-level scale (Decision D5): daemon
	// accumulates ConsecutiveStalls without an upper bound (Story 45.2 D3),
	// but the UI scale tops out at 4 = "would suspend".
	levelN := min(match.ConsecutiveStalls, 4)

	stalledDur := time.Duration(match.StalledDurationMs) * time.Millisecond
	gap := time.Duration(match.HeartbeatGapMs) * time.Millisecond

	b.WriteString(divider("Stall"))
	b.WriteString("\n")
	// Summary spans three short lines so the section fits inside narrow
	// Detail panes (innerW 40 still common on split-screen layouts). Each
	// field surfaces independently (PID/level/action/idle/gap), so the
	// content-search assertions in AC1 work regardless of line wrapping.
	// time.Duration.String() produces "3m5s" / "4m0s" form which surfaces
	// both the minute and the residual seconds — denser signal than
	// ui.FormatDuration's "%.1fm" for stall durations near the minute mark.
	fmt.Fprintf(b, "    PID %d · level %d/4\n", match.PID, levelN)
	fmt.Fprintf(b, "    would %s\n", match.LastAction)
	fmt.Fprintf(b, "    idle %s · gap %s\n", stalledDur.String(), gap.String())

	// Intensity bar (Decision D2): fill ratio uses min(ConsecutiveStalls, 4) / 4
	// to align with the daemon's 4-level escalation mapping rather than the
	// per-process StepTimeout (which varies and would break cross-process
	// comparability). Width matches the Context budget bar (innerW-10).
	barWidth := max(innerW-10, 10)
	filled := int(float64(levelN) / 4.0 * float64(barWidth))
	filled = min(filled, barWidth)

	filledChar, unfilledChar := "█", "░"
	if asciiMode() {
		filledChar, unfilledChar = "#", "-"
	}

	filledStr := strings.Repeat(filledChar, filled)
	unfilledStr := strings.Repeat(unfilledChar, barWidth-filled)

	if !asciiMode() {
		// Color gradient (Decision D3): reuse project palette ColorWarning
		// (yellow) for warn / cancel_step levels, ColorError (red) for the
		// "would suspend" terminal level. Bold acts as the visual transition
		// between warn (1-2) and the cancel_step boundary (3). Only the
		// filled portion is styled — unfilled stays muted, matching the
		// Context budget bar's "color only the fill" pattern.
		color := ui.ColorWarning
		bold := false
		switch {
		case levelN >= 4:
			color = ui.ColorError
		case levelN == 3:
			bold = true
		}
		style := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
		if bold {
			style = style.Bold(true)
		}
		filledStr = style.Render(filledStr)
	}

	fmt.Fprintf(b, "    [%s%s] %d/4\n", filledStr, unfilledStr, levelN)
}

// divider builds the section divider line. Honors RNIX_ASCII=1 by falling back
// to ASCII '----' marks; otherwise uses the Unicode box-drawing '────' used by
// the rest of the Detail card.
func divider(label string) string {
	if asciiMode() {
		return "  ---- " + label + " ----"
	}
	return "  ──── " + label + " ────"
}

func asciiMode() bool {
	return os.Getenv("RNIX_ASCII") == "1"
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
