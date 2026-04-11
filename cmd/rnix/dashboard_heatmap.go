package main

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/debug"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"

	tea "charm.land/bubbletea/v2"
)

// --- Heatmap logic (Story 17-3) ---

func segmentKindLabel(kind segmentKind) string {
	switch kind {
	case segSystem:
		return "System Prompt"
	case segSkill:
		return "Skill"
	case segTool:
		return "Tool Results"
	case segUser:
		return "User Messages"
	case segAssistant:
		return "Assistant"
	case segLeaked:
		return "Leaked"
	default:
		return "Unknown"
	}
}

func activityLabel(a activityLevel) string {
	switch a {
	case actActive:
		return "Active"
	case actWarm:
		return "Warm"
	case actCold:
		return "Cold"
	case actLeaked:
		return "Leaked"
	default:
		return "Unknown"
	}
}

func mapConsumerKind(kind string) segmentKind {
	switch {
	case kind == "system_prompt":
		return segSystem
	case kind == "user":
		return segUser
	case kind == "assistant":
		return segAssistant
	case strings.HasPrefix(kind, "tool:"):
		return segTool
	case kind == "skill" || strings.HasPrefix(kind, "skill:"):
		return segSkill
	default:
		return segAssistant
	}
}

func dim(hexColor string) string {
	switch hexColor {
	case "#5B9BD5":
		return "#3A6B94"
	case "#9B59B6":
		return "#6A3D7E"
	case "#6BCB77":
		return "#4A8C52"
	case "#FFD93D":
		return "#B3982B"
	case "#888888":
		return "#5E5E5E"
	default:
		return "#666666"
	}
}

func segmentColor(kind segmentKind, activity activityLevel) string {
	if activity == actLeaked || kind == segLeaked {
		return "#FF6B6B"
	}
	var base string
	switch kind {
	case segSystem:
		base = "#5B9BD5"
	case segSkill:
		base = colorIPC
	case segTool:
		base = "#6BCB77"
	case segUser:
		base = "#FFD93D"
	case segAssistant:
		base = "#888888"
	default:
		base = "#888888"
	}
	if activity == actCold {
		return dim(base)
	}
	return base
}

func estimateActivity(profile *debug.CtxProfileResult, kind segmentKind, rank int) activityLevel {
	if kind == segSystem {
		return actActive
	}
	if kind == segLeaked {
		return actLeaked
	}
	cl := profile.Classification
	if cl.Active.Pct > cl.Cold.Pct {
		if rank <= 2 {
			return actActive
		}
		return actWarm
	}
	if cl.Cold.Pct > cl.Active.Pct {
		return actCold
	}
	return actWarm
}

func buildHeatmapSegments(profile *debug.CtxProfileResult) []heatmapSegment {
	if profile == nil || len(profile.TopConsumers) == 0 {
		return nil
	}

	type kindBucket struct {
		tokens   int
		pct      float64
		kind     segmentKind
		activity activityLevel
		bestRank int
	}
	merged := make(map[segmentKind]*kindBucket)

	for _, c := range profile.TopConsumers {
		kind := mapConsumerKind(c.Kind)
		activity := estimateActivity(profile, kind, c.Rank)
		if b, ok := merged[kind]; ok {
			b.tokens += c.Tokens
			b.pct += c.Pct
			if c.Rank < b.bestRank {
				b.bestRank = c.Rank
				b.activity = activity
			}
		} else {
			merged[kind] = &kindBucket{
				tokens: c.Tokens, pct: c.Pct,
				kind: kind, activity: activity, bestRank: c.Rank,
			}
		}
	}

	var segments []heatmapSegment
	var otherTokens int
	var otherPct float64

	for _, b := range merged {
		if b.pct < 3.0 {
			otherTokens += b.tokens
			otherPct += b.pct
			continue
		}
		segments = append(segments, heatmapSegment{
			label:    segmentKindLabel(b.kind),
			tokens:   b.tokens,
			pct:      b.pct,
			kind:     b.kind,
			activity: b.activity,
		})
	}

	if profile.Classification.Leaked.Tokens > 0 {
		if profile.Classification.Leaked.Pct < 3.0 {
			otherTokens += profile.Classification.Leaked.Tokens
			otherPct += profile.Classification.Leaked.Pct
		} else {
			segments = append(segments, heatmapSegment{
				label:    "Leaked",
				tokens:   profile.Classification.Leaked.Tokens,
				pct:      profile.Classification.Leaked.Pct,
				kind:     segLeaked,
				activity: actLeaked,
			})
		}
	}

	if otherTokens > 0 {
		segments = append(segments, heatmapSegment{
			label:    "Other",
			tokens:   otherTokens,
			pct:      otherPct,
			kind:     segAssistant,
			activity: actCold,
		})
	}

	sort.Slice(segments, func(i, j int) bool {
		return segments[i].tokens > segments[j].tokens
	})

	return segments
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
	if m.selectedPID == m.heatmapPID {
		return m
	}
	m.heatmapProfile = nil
	m.heatmapSegments = nil
	m.heatmapCursor = 0
	m.heatmapExpanded = false
	m.heatmapErr = nil
	return m
}

func (m dashboardModel) handleHeatmapKey(key string) dashboardModel {
	switch key {
	case "up", "k":
		if m.heatmapCursor > 0 {
			m.heatmapCursor--
		}
	case "down", "j":
		if m.heatmapCursor < len(m.heatmapSegments)-1 {
			m.heatmapCursor++
		}
	case "enter":
		m.heatmapExpanded = !m.heatmapExpanded
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
	if m.selectedPID > 0 && m.heatmapProfile != nil {
		fmt.Fprintf(&b, " | PID %d", m.selectedPID)
		pct := 0
		if m.heatmapProfile.ContextBudget > 0 {
			pct = m.heatmapProfile.TotalTokens * 100 / m.heatmapProfile.ContextBudget
		}
		fmt.Fprintf(&b, " | ~%d tok / %d budget (%d%%)",
			m.heatmapProfile.TotalTokens, m.heatmapProfile.ContextBudget, pct)
	}
	b.WriteString("\n")

	if m.selectedPID == 0 {
		b.WriteString("\n    Select an agent to view heatmap")
		return renderFixedPanel(b.String(), width, height, borderColor)
	}
	if m.heatmapErr != nil {
		fmt.Fprintf(&b, "\n    ✗ %v", m.heatmapErr)
		return renderFixedPanel(b.String(), width, height, borderColor)
	}
	if len(m.heatmapSegments) == 0 {
		b.WriteString("\n    Loading context profile...")
		return renderFixedPanel(b.String(), width, height, borderColor)
	}

	barWidth := max(innerW-2, 10)
	for _, seg := range m.heatmapSegments {
		segW := max(int(seg.pct/100.0*float64(barWidth)), 1)
		color := segmentColor(seg.kind, seg.activity)
		catStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
		b.WriteString(catStyle.Render(strings.Repeat("█", segW)))
	}
	b.WriteString("\n")

	for _, seg := range m.heatmapSegments {
		fmt.Fprintf(&b, "%s(%.0f%%) ", seg.label, seg.pct)
	}
	b.WriteString("\n")

	for i, seg := range m.heatmapSegments {
		cursor := "  "
		if i == m.heatmapCursor {
			cursor = "▸ "
		}
		actStr := activityLabel(seg.activity)
		fmt.Fprintf(&b, "%s%-15s %4d tok  %5.1f%%  %s\n",
			cursor, seg.label, seg.tokens, seg.pct, actStr)
	}

	if m.heatmapExpanded && m.heatmapCursor < len(m.heatmapSegments) {
		seg := m.heatmapSegments[m.heatmapCursor]
		actStr := activityLabel(seg.activity)
		fmt.Fprintf(&b, "\n── Selected: %s ──\n", seg.label)
		fmt.Fprintf(&b, "%d tokens | %.1f%% | %s\n", seg.tokens, seg.pct, actStr)
		if seg.summary != "" {
			if utf8.RuneCountInString(seg.summary) > 60 {
				runes := []rune(seg.summary)
				b.WriteString("\"" + string(runes[:57]) + "...\"\n")
			} else {
				b.WriteString("\"" + seg.summary + "\"\n")
			}
		}
	}

	return renderFixedPanel(b.String(), width, height, borderColor)
}
