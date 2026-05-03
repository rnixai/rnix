package main

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/debug"
	"github.com/rnixai/rnix/internal/dashboard/heatmap"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"

	tea "charm.land/bubbletea/v2"
)

// --- Heatmap logic (Story 17-3) ---
//
// Story 38-5 PR3 Step 1: 全部 helper 迁出至 internal/dashboard/heatmap，cmd/rnix 端保留 thin wrapper
// 让旧 caller / 测试 grep 字符串零变化。详见 internal/dashboard/heatmap/builder.go。

// segmentKindLabel — thin wrapper · 见 internal/dashboard/heatmap.SegmentKindLabel
//
//nolint:unused // 保留供潜在外部 caller / 测试 grep 使用
func segmentKindLabel(kind segmentKind) string {
	return heatmap.SegmentKindLabel(kind)
}

// activityLabel — thin wrapper · 见 internal/dashboard/heatmap.ActivityLabel
func activityLabel(a activityLevel) string {
	return heatmap.ActivityLabel(a)
}

// dim — thin wrapper · 见 internal/dashboard/heatmap.Dim
//
//nolint:unused // 保留供潜在外部 caller 使用
func dim(hexColor string) string {
	return heatmap.Dim(hexColor)
}

// segmentColor — thin wrapper · 见 internal/dashboard/heatmap.SegmentColor
func segmentColor(kind segmentKind, activity activityLevel) string {
	return heatmap.SegmentColor(kind, activity)
}

// mapConsumerKind — thin wrapper · 见 internal/dashboard/heatmap.MapConsumerKind
//
// 保留供 dashboard_test.go::17.3-UNIT-012 / CR-FIX-004 测试调用。
func mapConsumerKind(kind string) segmentKind {
	return heatmap.MapConsumerKind(kind)
}

// buildHeatmapSegments — thin wrapper · 见 internal/dashboard/heatmap.BuildSegments
//
// heatmapSegment 是 heatmap.Segment 的 type alias（见 dashboard_types.go），可直接返回 BuildSegments
// 的结果而无需类型转换。
func buildHeatmapSegments(profile *debug.CtxProfileResult) []heatmapSegment {
	return heatmap.BuildSegments(profile)
}

func fetchHeatmapCmd(pid types.PID) tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return heatmapProfileMsg{err: err}
		}
		defer client.Close()
		profile, err := client.CtxProfile(pid)
		return heatmapProfileMsg{profile: profile, err: err}
	}
}

func (m dashboardModel) handleHeatmapPIDChange() dashboardModel {
	if m.selectedPID == m.heatmap.PID {
		return m
	}
	m.heatmap.Profile = nil
	m.heatmap.Segments = nil
	m.heatmap.Cursor = 0
	m.heatmap.Expanded = false
	m.heatmap.Err = nil
	return m
}

func (m dashboardModel) handleHeatmapKey(key string) dashboardModel {
	// Story 36-5: 统一导航键集合（补齐 pgdn/pgup/ctrl+d/u/g/G/home/end）
	if ui.HandleListKey(key, nil, &m.heatmap.Cursor, len(m.heatmap.Segments), ui.ListNavOpts{PageSize: 8}) {
		return m
	}
	// Story 36-5 P-8: 当前面板（Heatmap）不支持搜索；按 / 提示用户
	if key == "/" {
		m.statusMsg = "Search not available in this pane"
		m.statusMsgTTL = statusMsgDefaultTTL
		return m
	}
	if key == "enter" {
		// Story 36-5 P-5: 空列表时不切换 expanded 状态，避免无意义的状态翻转
		if len(m.heatmap.Segments) == 0 {
			return m
		}
		m.heatmap.Expanded = !m.heatmap.Expanded
	}
	return m
}

func (m dashboardModel) renderHeatmapPane(width, height int) string {
	isActive := m.activePane == paneHeatmap

	borderColor := lipgloss.Color(ui.ColorMuted)
	if isActive {
		borderColor = lipgloss.Color(ui.ColorAgent)
	}

	innerW := max(width-2, 1)

	var b strings.Builder

	b.WriteString(" Heatmap")
	if m.selectedPID > 0 && m.heatmap.Profile != nil {
		fmt.Fprintf(&b, " | PID %d", m.selectedPID)
		pct := 0
		if m.heatmap.Profile.ContextBudget > 0 {
			pct = m.heatmap.Profile.TotalTokens * 100 / m.heatmap.Profile.ContextBudget
		}
		fmt.Fprintf(&b, " | ~%d tok / %d budget (%d%%)",
			m.heatmap.Profile.TotalTokens, m.heatmap.Profile.ContextBudget, pct)
	}
	b.WriteString("\n")

	if m.selectedPID == 0 {
		b.WriteString("\n    Select an agent to view heatmap")
		return renderFixedPanel(b.String(), width, height, borderColor)
	}
	if m.heatmap.Err != nil {
		fmt.Fprintf(&b, "\n    ✗ %v", m.heatmap.Err)
		return renderFixedPanel(b.String(), width, height, borderColor)
	}
	if len(m.heatmap.Segments) == 0 {
		b.WriteString("\n    Loading context profile...")
		return renderFixedPanel(b.String(), width, height, borderColor)
	}

	barWidth := max(innerW-2, 10)
	for _, seg := range m.heatmap.Segments {
		segW := max(int(seg.Pct/100.0*float64(barWidth)), 1)
		color := segmentColor(seg.Kind, seg.Activity)
		catStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
		b.WriteString(catStyle.Render(strings.Repeat("█", segW)))
	}
	b.WriteString("\n")

	for _, seg := range m.heatmap.Segments {
		fmt.Fprintf(&b, "%s(%.0f%%) ", seg.Label, seg.Pct)
	}
	b.WriteString("\n")

	for i, seg := range m.heatmap.Segments {
		cursor := "  "
		if i == m.heatmap.Cursor {
			cursor = "▸ "
		}
		actStr := activityLabel(seg.Activity)
		fmt.Fprintf(&b, "%s%-15s %4d tok  %5.1f%%  %s\n",
			cursor, seg.Label, seg.Tokens, seg.Pct, actStr)
	}

	if m.heatmap.Expanded && m.heatmap.Cursor < len(m.heatmap.Segments) {
		seg := m.heatmap.Segments[m.heatmap.Cursor]
		actStr := activityLabel(seg.Activity)
		fmt.Fprintf(&b, "\n── Selected: %s ──\n", seg.Label)
		fmt.Fprintf(&b, "%d tokens | %.1f%% | %s\n", seg.Tokens, seg.Pct, actStr)
		if seg.Summary != "" {
			if utf8.RuneCountInString(seg.Summary) > 60 {
				runes := []rune(seg.Summary)
				b.WriteString("\"" + string(runes[:57]) + "...\"\n")
			} else {
				b.WriteString("\"" + seg.Summary + "\"\n")
			}
		}
	}

	return renderFixedPanel(b.String(), width, height, borderColor)
}
