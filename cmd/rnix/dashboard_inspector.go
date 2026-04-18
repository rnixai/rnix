package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/viewport"
	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
)

// --- Step Inspector (Story 36.1) ---

// enterStepInspector enters the unified Step Inspector overlay.
func (m dashboardModel) enterStepInspector() (tea.Model, tea.Cmd) {
	if m.selectedPID == 0 {
		m.statusMsg = "No process selected"
		m.statusMsgTTL = statusMsgDefaultTTL
		return m, nil
	}
	m.inspectorPrevMode = m.viewMode
	m.viewMode = viewStepInspector
	m.inspectorPID = m.selectedPID
	m.inspectorUUID = m.selectedUUID
	m.inspectorStep = 0
	m.inspectorStepMax = 0
	m.inspectorSteps = nil
	m.inspectorDetail = nil
	m.inspectorPrevDetail = nil
	m.inspectorPrevStep = 0
	m.inspectorCurDetailStep = 0
	m.inspectorLens = lensConversation
	m.inspectorFetching = false
	m.inspectorSystemExpanded = false
	// Story 36-5 fix: reset cross-pane search state when entering Inspector to
	// avoid stale searchQuery carried over from Timeline.
	m.searchMode = false
	m.searchQuery = ""
	m.searchMatches = nil
	m.searchMatchIdx = 0

	contentH := m.inspectorContentHeight()
	for i := range m.inspectorViewports {
		m.inspectorViewports[i] = viewport.New(
			viewport.WithWidth(m.width),
			viewport.WithHeight(contentH),
		)
		m.inspectorContents[i] = ""
	}

	return m, fetchInspectorStepListCmd(m.selectedPID, m.selectedUUID)
}

func (m dashboardModel) inspectorContentHeight() int {
	// stepRail(2) + lensTabs(1) + footer(1) = 4
	return max(m.height-4, 1)
}

// fetchInspectorStepListCmd fetches the step summary list for the Inspector.
func fetchInspectorStepListCmd(pid types.PID, uuid string) tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return inspectorStepListMsg{pid: pid, err: err}
		}
		defer client.Close()
		_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))

		var resp *ipc.ListStepsResponse
		if uuid != "" {
			resp, err = client.ListStepsByUUID(uuid, 0)
		} else {
			resp, err = client.ListSteps(pid, 0)
		}
		if err != nil {
			return inspectorStepListMsg{pid: pid, err: err}
		}
		return inspectorStepListMsg{pid: pid, steps: resp.Steps, total: resp.Total}
	}
}

// fetchInspectorDetailCmd fetches a specific step's detail for the Inspector.
func fetchInspectorDetailCmd(pid types.PID, uuid string, step int) tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return inspectorDetailMsg{pid: pid, step: step, err: err}
		}
		defer client.Close()
		_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))

		var detail *ipc.GetStepDetailResponse
		if uuid != "" {
			detail, err = client.GetStepDetailByUUID(uuid, step)
		} else {
			detail, err = client.GetStepDetail(pid, step)
		}
		return inspectorDetailMsg{pid: pid, step: step, detail: detail, err: err}
	}
}

// inspectorKey handles key presses in the Step Inspector overlay.
func (m dashboardModel) inspectorKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Story 36-5: search mode input handling takes priority.
	if m.searchMode {
		return m.handleInspectorSearchKey(key)
	}

	switch key {
	case "q":
		return m, tea.Quit
	case "esc":
		// Clear active search highlights before closing
		if m.searchQuery != "" {
			m.searchQuery = ""
			m.searchMatches = nil
			m.searchMatchIdx = 0
			m.rebuildInspectorContents()
			return m, nil
		}
		m.viewMode = m.inspectorPrevMode
		return m, nil

	// Lens switching: 1-5
	case "1":
		return m.switchInspectorLens(lensConversation), nil
	case "2":
		return m.switchInspectorLens(lensSystem), nil
	case "3":
		return m.switchInspectorLens(lensToolIO), nil
	case "4":
		return m.switchInspectorLens(lensMeta), nil
	case "5":
		return m.switchInspectorLens(lensRawJSON), nil

	// Step navigation (index-based for sparse step support)
	case "h", "left":
		if m.inspectorFetching || len(m.inspectorSteps) == 0 {
			return m, nil
		}
		idx := m.findStepIndex(m.inspectorStep)
		if idx <= 0 {
			return m, nil
		}
		newStep := m.inspectorSteps[idx-1].Step
		m.inspectorStep = newStep
		m.inspectorFetching = true
		return m, fetchInspectorDetailCmd(m.inspectorPID, m.inspectorUUID, newStep)
	case "l", "right":
		if m.inspectorFetching || len(m.inspectorSteps) == 0 {
			return m, nil
		}
		idx := m.findStepIndex(m.inspectorStep)
		if idx < 0 || idx >= len(m.inspectorSteps)-1 {
			return m, nil
		}
		newStep := m.inspectorSteps[idx+1].Step
		m.inspectorStep = newStep
		m.inspectorFetching = true
		return m, fetchInspectorDetailCmd(m.inspectorPID, m.inspectorUUID, newStep)
	case "H", "home":
		if len(m.inspectorSteps) == 0 || m.inspectorFetching {
			return m, nil
		}
		firstStep := m.inspectorSteps[0].Step
		if firstStep == m.inspectorStep {
			return m, nil
		}
		m.inspectorStep = firstStep
		m.inspectorFetching = true
		return m, fetchInspectorDetailCmd(m.inspectorPID, m.inspectorUUID, firstStep)
	case "L", "end":
		if len(m.inspectorSteps) == 0 || m.inspectorFetching {
			return m, nil
		}
		lastStep := m.inspectorSteps[len(m.inspectorSteps)-1].Step
		if lastStep == m.inspectorStep {
			return m, nil
		}
		m.inspectorStep = lastStep
		m.inspectorFetching = true
		return m, fetchInspectorDetailCmd(m.inspectorPID, m.inspectorUUID, lastStep)

	// Copy
	case "y":
		content := m.inspectorContents[m.inspectorLens]
		charCount := utf8.RuneCountInString(content)
		m.statusMsg = fmt.Sprintf("copied %s chars", formatCharCount(charCount))
		m.statusMsgTTL = statusMsgDefaultTTL
		return m, tea.SetClipboard(content)

	// Open full in $PAGER
	case "o":
		return m.openInspectorInPager()

	// Enter: expand collapsed system lens
	case "enter":
		if m.inspectorLens == lensSystem && !m.inspectorSystemExpanded {
			m.inspectorSystemExpanded = true
			if m.inspectorDetail != nil {
				content := m.buildLensContent(lensSystem, m.inspectorDetail, m.inspectorPrevDetail)
				m.inspectorContents[lensSystem] = content
				m.inspectorViewports[lensSystem].SetContent(content)
				m.inspectorViewports[lensSystem].GotoTop()
			}
			return m, nil
		}
		// Fall through to viewport scroll
		lens := m.inspectorLens
		var cmd tea.Cmd
		m.inspectorViewports[lens], cmd = m.inspectorViewports[lens].Update(msg)
		return m, cmd

	// Story 36-5: Search entry for Conversation / Tool I/O / System / Meta / Raw lenses
	case "/":
		m.searchMode = true
		m.searchQuery = ""
		m.searchMatches = nil
		m.searchMatchIdx = 0
		return m, nil

	// Story 36-5: Cycle through current search matches
	case "n":
		return m.inspectorJumpSearchMatch(1), nil
	case "N", "shift+N":
		return m.inspectorJumpSearchMatch(-1), nil

	default:
		// Story 36-5: j/k/PgUp/PgDn/Ctrl-d/u/g/G/home/end → viewport scroll.
		// Uses HandleListKey so behaviour matches other panes; falls back to
		// viewport.Update for any keys we don't recognise (e.g. mouse events).
		lens := m.inspectorLens
		vp := m.inspectorViewports[lens]
		if ui.HandleListKey(key, &vp, nil, 0, ui.ListNavOpts{}) {
			m.inspectorViewports[lens] = vp
			return m, nil
		}
		var cmd tea.Cmd
		m.inspectorViewports[lens], cmd = m.inspectorViewports[lens].Update(msg)
		return m, cmd
	}
}

// handleInspectorSearchKey handles keystrokes while the Inspector is in
// search-input mode. Story 36-5 AC-12.
func (m dashboardModel) handleInspectorSearchKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc":
		m.searchMode = false
		m.searchQuery = ""
		m.searchMatches = nil
		m.searchMatchIdx = 0
		m.rebuildInspectorContents()
		return m, nil
	case "enter":
		m.searchMode = false
		m.refreshInspectorSearchMatches()
		if len(m.searchMatches) == 0 {
			m.statusMsg = fmt.Sprintf("No matches for %q", m.searchQuery)
			m.statusMsgTTL = statusMsgDefaultTTL
			return m, nil
		}
		m.searchMatchIdx = 0
		m.rebuildInspectorContents()
		m.scrollInspectorToCurrentMatch()
		return m, nil
	case "backspace":
		runes := []rune(m.searchQuery)
		if len(runes) > 0 {
			m.searchQuery = string(runes[:len(runes)-1])
		}
		return m, nil
	case " ", "space":
		m.searchQuery += " "
		return m, nil
	default:
		if len([]rune(key)) == 1 {
			m.searchQuery += key
		}
		return m, nil
	}
}

func (m *dashboardModel) refreshInspectorSearchMatches() {
	content := m.inspectorContents[m.inspectorLens]
	m.searchMatches = ui.FindMatches(content, m.searchQuery)
}

func (m dashboardModel) inspectorJumpSearchMatch(dir int) dashboardModel {
	if len(m.searchMatches) == 0 {
		return m
	}
	n := len(m.searchMatches)
	m.searchMatchIdx = ((m.searchMatchIdx + dir) % n + n) % n
	m.scrollInspectorToCurrentMatch()
	return m
}

func (m *dashboardModel) scrollInspectorToCurrentMatch() {
	if len(m.searchMatches) == 0 {
		return
	}
	line := m.searchMatches[m.searchMatchIdx]
	vp := m.inspectorViewports[m.inspectorLens]
	vp.SetYOffset(line)
	m.inspectorViewports[m.inspectorLens] = vp
}

func (m dashboardModel) switchInspectorLens(lens inspectorLens) dashboardModel {
	m.inspectorLens = lens
	return m
}

// openInspectorInPager writes current lens full content to a temp file and opens $PAGER.
func (m dashboardModel) openInspectorInPager() (tea.Model, tea.Cmd) {
	detail := m.inspectorDetail
	if detail == nil {
		m.statusMsg = "No step data to open"
		m.statusMsgTTL = statusMsgDefaultTTL
		return m, nil
	}

	lensNames := [inspectorLensCount]string{"conversation", "system", "toolio", "meta", "rawjson"}
	content := m.buildFullLensContent(m.inspectorLens, detail, m.inspectorPrevDetail)

	tmpFile := fmt.Sprintf("/tmp/rnix-step-%s-%d-%s.txt",
		m.inspectorUUID, m.inspectorStep, lensNames[m.inspectorLens])

	if err := os.WriteFile(tmpFile, []byte(content), 0o600); err != nil {
		m.statusMsg = fmt.Sprintf("write error: %v", err)
		m.statusMsgTTL = statusMsgDefaultTTL
		return m, nil
	}

	pager := os.Getenv("PAGER")
	if pager == "" {
		pager = "less"
	}

	// Split PAGER to support flags (e.g. "less -R")
	parts := strings.Fields(pager)
	args := append(parts[1:], tmpFile)
	c := exec.Command(parts[0], args...)
	return m, tea.ExecProcess(c, func(err error) tea.Msg {
		os.Remove(tmpFile) // clean up temp file after pager exits
		return execResultMsg{err: err}
	})
}

// renderStepInspector renders the full-screen Step Inspector overlay.
func (m dashboardModel) renderStepInspector(w, h int) string {
	var b strings.Builder

	// Step Rail (top)
	b.WriteString(m.renderStepRail(w))
	b.WriteString("\n")

	// Lens Tabs
	b.WriteString(m.renderLensTabs(w))
	b.WriteString("\n")

	// Content area — current lens viewport
	lens := m.inspectorLens
	content := m.inspectorContents[lens]
	if content == "" && m.inspectorDetail == nil {
		if m.inspectorFetching {
			content = "  (loading...)"
		} else if len(m.inspectorSteps) == 0 {
			content = "  No step data recorded for this process.\n  (Process may have failed before completing any reasoning step)"
		}
		// Set viewport content so it renders through viewport.View()
		if content != "" && m.inspectorViewports[lens].Width() > 0 {
			m.inspectorViewports[lens].SetContent(content)
		}
	}

	if m.inspectorViewports[lens].Width() > 0 {
		b.WriteString(m.inspectorViewports[lens].View())
	} else {
		contentH := max(h-4, 1)
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			if i >= contentH {
				break
			}
			b.WriteString(line)
			b.WriteString("\n")
		}
	}

	// Footer
	b.WriteString("\n")
	b.WriteString(m.renderInspectorFooter())

	return b.String()
}

// renderStepRail renders the top step navigation rail.
func (m dashboardModel) renderStepRail(w int) string {
	var b strings.Builder

	ascii := ui.IsASCIIMode()

	fmt.Fprintf(&b, " Step Inspector | PID %d", m.inspectorPID)

	// Show process start time
	for _, p := range m.processes {
		if p.PID == m.inspectorPID && (m.inspectorUUID == "" || p.UUID == m.inspectorUUID) {
			if !p.CreatedAt.IsZero() {
				fmt.Fprintf(&b, " | %s", ui.FormatWallClock(p.CreatedAt))
			}
			break
		}
	}

	if m.inspectorDetail != nil {
		step := m.inspectorStep
		maxStep := m.inspectorStepMax
		fmt.Fprintf(&b, " | Step %d/%d", step, maxStep)

		if m.inspectorDetail.Action != "" {
			fmt.Fprintf(&b, " | %s", m.inspectorDetail.Action)
		}

		// Duration (from tool call)
		if m.inspectorDetail.ToolDurationMs > 0 {
			if ascii {
				fmt.Fprintf(&b, " | %.0fms", m.inspectorDetail.ToolDurationMs)
			} else {
				fmt.Fprintf(&b, " | ⧖%.0fms", m.inspectorDetail.ToolDurationMs)
			}
		}

		// Token counts
		reqTok := m.inspectorDetail.RequestTokens
		respTok := m.inspectorDetail.ResponseTokens
		if reqTok > 0 || respTok > 0 {
			if ascii {
				fmt.Fprintf(&b, " | %s->%s tok", formatTokenCount(reqTok), formatTokenCount(respTok))
			} else {
				fmt.Fprintf(&b, " | ⇅%s→%s tok", formatTokenCount(reqTok), formatTokenCount(respTok))
			}
		}
	}

	// Truncate to width
	result := b.String()
	if w > 0 && utf8.RuneCountInString(result) > w {
		runes := []rune(result)
		result = string(runes[:w])
	}

	return result
}

// renderLensTabs renders the lens selector tabs.
func (m dashboardModel) renderLensTabs(w int) string {
	ascii := ui.IsASCIIMode()

	type lensInfo struct {
		key   string
		label string
	}

	var lenses []lensInfo
	if ascii {
		lenses = []lensInfo{
			{"1", "Conv"},
			{"2", "Sys"},
			{"3", "Tool"},
			{"4", "Meta"},
			{"5", "JSON"},
		}
	} else {
		lenses = []lensInfo{
			{"1", "❶ Conversation"},
			{"2", "❷ System"},
			{"3", "❸ Tool I/O"},
			{"4", "❹ Meta"},
			{"5", "❺ Raw JSON"},
		}
	}

	activeBold := lipgloss.NewStyle().Bold(true)
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))

	var b strings.Builder
	b.WriteString(" ")
	for i, l := range lenses {
		if inspectorLens(i) == m.inspectorLens {
			b.WriteString(activeBold.Render("[" + l.label + "]"))
		} else {
			b.WriteString(dimStyle.Render(" " + l.label + " "))
		}
		b.WriteString(" ")
	}

	result := b.String()
	_ = w
	return result
}

// renderInspectorFooter renders the bottom shortcut hints.
func (m dashboardModel) renderInspectorFooter() string {
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	// Story 36-5: search overlay takes over the footer while active.
	if m.searchMode {
		return fmt.Sprintf(" Search: %s_", m.searchQuery)
	}
	if m.searchQuery != "" && len(m.searchMatches) > 0 {
		return dimStyle.Render(fmt.Sprintf(" /%s  [%d/%d]  n/N next/prev · Esc clear",
			m.searchQuery, m.searchMatchIdx+1, len(m.searchMatches)))
	}
	return dimStyle.Render(" h/l step · 1-5 lens · j/k scroll · / search · y copy · o open · Esc back")
}

// rebuildInspectorContents rebuilds all lens content from the current detail.
func (m *dashboardModel) rebuildInspectorContents() {
	reverse := lipgloss.NewStyle().Reverse(true)
	for i := range inspectorLensCount {
		content := m.buildLensContent(inspectorLens(i), m.inspectorDetail, m.inspectorPrevDetail)
		// Story 36-5: Apply reverse-video highlight to matched lines for the active lens.
		if inspectorLens(i) == m.inspectorLens && m.searchQuery != "" {
			lines := strings.Split(content, "\n")
			matchSet := make(map[int]struct{}, len(m.searchMatches))
			for _, ln := range m.searchMatches {
				matchSet[ln] = struct{}{}
			}
			for idx, line := range lines {
				if _, ok := matchSet[idx]; ok {
					lines[idx] = reverse.Render(line)
				}
			}
			content = strings.Join(lines, "\n")
		}
		m.inspectorContents[i] = content
		m.inspectorViewports[i].SetContent(content)
		// Story 36-5 fix: only reset scroll on the active lens; preserve per-lens
		// scroll position on other lenses (Story 36-1 invariant).
		if inspectorLens(i) == m.inspectorLens {
			m.inspectorViewports[i].GotoTop()
		}
	}
}

// buildLensContent builds display content for a specific lens.
func (m dashboardModel) buildLensContent(lens inspectorLens, detail, prevDetail *ipc.GetStepDetailResponse) string {
	if detail == nil {
		return "  (loading...)"
	}
	switch lens {
	case lensConversation:
		return m.buildConversationLens(detail)
	case lensSystem:
		return m.buildSystemLens(detail, prevDetail)
	case lensToolIO:
		return m.buildToolIOLens(detail)
	case lensMeta:
		return m.buildMetaLens(detail)
	case lensRawJSON:
		return m.buildRawJSONLens(detail)
	default:
		return ""
	}
}

// buildFullLensContent builds FULL (non-truncated) content for pager output.
func (m dashboardModel) buildFullLensContent(lens inspectorLens, detail, prevDetail *ipc.GetStepDetailResponse) string {
	if detail == nil {
		return ""
	}
	switch lens {
	case lensConversation:
		return m.buildConversationLensFull(detail)
	case lensSystem:
		return detail.SystemPrompt
	case lensToolIO:
		return m.buildToolIOLensFull(detail)
	case lensMeta:
		return m.buildMetaLens(detail)
	case lensRawJSON:
		return m.buildRawJSONLens(detail)
	default:
		return ""
	}
}

// buildConversationLens builds Lens ❶: full message flow with explicit truncation.
func (m dashboardModel) buildConversationLens(detail *ipc.GetStepDetailResponse) string {
	if len(detail.Messages) == 0 {
		dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
		return dimStyle.Render("No message history available.\n\n" +
			"CLI driver processes manage their conversation history internally.\n" +
			"For native-driver processes, this lens shows the complete message flow.")
	}

	var b strings.Builder
	toolCallNames := buildToolCallNameMap(detail.Messages)
	separator := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")).Render(strings.Repeat("─", 70))

	for i, msg := range detail.Messages {
		if i > 0 {
			b.WriteString(separator + "\n")
		}
		roleTag := formatRoleTag(msg, toolCallNames)
		b.WriteString(roleTag + "\n")

		content := msg.Content
		totalLen := utf8.RuneCountInString(content)
		if totalLen > inspectorTruncateThreshold {
			runes := []rune(content)
			content = string(runes[:inspectorTruncateThreshold])
			b.WriteString(content)
			b.WriteString(renderTruncationNotice(inspectorTruncateThreshold, totalLen))
		} else {
			b.WriteString(content)
		}
		b.WriteString("\n\n")
	}

	return b.String()
}

// buildConversationLensFull builds full (non-truncated) conversation for pager.
func (m dashboardModel) buildConversationLensFull(detail *ipc.GetStepDetailResponse) string {
	if len(detail.Messages) == 0 {
		return "No message history available."
	}
	var b strings.Builder
	for i, msg := range detail.Messages {
		if i > 0 {
			b.WriteString(strings.Repeat("─", 70) + "\n")
		}
		b.WriteString("[" + msg.Role + "]\n")
		b.WriteString(msg.Content)
		b.WriteString("\n\n")
	}
	return b.String()
}

// buildSystemLens builds Lens ❷: system prompt with diff indication.
func (m dashboardModel) buildSystemLens(detail, prevDetail *ipc.GetStepDetailResponse) string {
	var b strings.Builder

	isUnchanged := prevDetail != nil && m.inspectorStep > 0 && prevDetail.SystemPrompt == detail.SystemPrompt

	if isUnchanged && !m.inspectorSystemExpanded {
		dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
		b.WriteString(dimStyle.Render(fmt.Sprintf("unchanged from step %d [press Enter to expand]", m.inspectorPrevStep)))
		b.WriteString("\n")
		return b.String()
	}

	if isUnchanged {
		dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
		b.WriteString(dimStyle.Render(fmt.Sprintf("(unchanged from step %d)", m.inspectorPrevStep)))
		b.WriteString("\n\n")
	}

	sysLen := utf8.RuneCountInString(detail.SystemPrompt)
	fmt.Fprintf(&b, "═══ System Prompt (%s chars) ═══\n\n", formatCharCount(sysLen))

	content := detail.SystemPrompt
	if sysLen > inspectorTruncateThreshold {
		runes := []rune(content)
		content = string(runes[:inspectorTruncateThreshold])
		b.WriteString(content)
		b.WriteString(renderTruncationNotice(inspectorTruncateThreshold, sysLen))
	} else {
		b.WriteString(content)
	}

	return b.String()
}

// buildToolIOLens builds Lens ❸: tool call details.
func (m dashboardModel) buildToolIOLens(detail *ipc.GetStepDetailResponse) string {
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	nameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6BCB77")).Bold(true)

	if detail.Action == "" && detail.ToolPath == "" {
		return dimStyle.Render("No tool information for this step.")
	}

	var b strings.Builder

	if detail.Action != "" {
		b.WriteString(nameStyle.Render(detail.Action))
		if detail.Summary != "" {
			b.WriteString(" — " + detail.Summary)
		}
		b.WriteString("\n\n")
	}

	if detail.ToolPath != "" {
		b.WriteString(dimStyle.Render("Path: ") + detail.ToolPath + "\n\n")
	}

	if detail.ToolInput != "" {
		b.WriteString(dimStyle.Render("Input:") + "\n")
		m.writeWithTruncation(&b, detail.ToolInput)
		b.WriteString("\n")
	}

	if detail.ToolResult != "" {
		b.WriteString(dimStyle.Render("Result:") + "\n")
		m.writeWithTruncation(&b, detail.ToolResult)
		b.WriteString("\n")
	}

	if detail.ToolError != "" {
		errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError))
		b.WriteString(dimStyle.Render("Error:") + "\n")
		toolErr := detail.ToolError
		totalLen := utf8.RuneCountInString(toolErr)
		if totalLen > inspectorTruncateThreshold {
			runes := []rune(toolErr)
			toolErr = string(runes[:inspectorTruncateThreshold])
			b.WriteString(errStyle.Render(toolErr))
			b.WriteString(renderTruncationNotice(inspectorTruncateThreshold, totalLen))
		} else {
			b.WriteString(errStyle.Render(toolErr))
		}
		b.WriteString("\n\n")
	}

	if detail.ToolDurationMs > 0 {
		b.WriteString(dimStyle.Render(fmt.Sprintf("Duration: %.0fms", detail.ToolDurationMs)) + "\n")
	}

	return b.String()
}

// buildToolIOLensFull builds full (non-truncated) tool I/O for pager.
func (m dashboardModel) buildToolIOLensFull(detail *ipc.GetStepDetailResponse) string {
	var b strings.Builder

	if detail.Action != "" {
		b.WriteString("Action: " + detail.Action + "\n")
	}
	if detail.Summary != "" {
		b.WriteString("Summary: " + detail.Summary + "\n")
	}
	if detail.ToolPath != "" {
		b.WriteString("Path: " + detail.ToolPath + "\n")
	}
	if detail.ToolInput != "" {
		b.WriteString("\nInput:\n" + detail.ToolInput + "\n")
	}
	if detail.ToolResult != "" {
		b.WriteString("\nResult:\n" + detail.ToolResult + "\n")
	}
	if detail.ToolError != "" {
		b.WriteString("\nError:\n" + detail.ToolError + "\n")
	}
	if detail.ToolDurationMs > 0 {
		fmt.Fprintf(&b, "\nDuration: %.0fms\n", detail.ToolDurationMs)
	}

	return b.String()
}

// buildMetaLens builds Lens ❹: metadata.
func (m dashboardModel) buildMetaLens(detail *ipc.GetStepDetailResponse) string {
	var b strings.Builder
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))

	fmt.Fprintf(&b, "%s %d\n", dimStyle.Render("Request Tokens:"), detail.RequestTokens)
	fmt.Fprintf(&b, "%s %d\n", dimStyle.Render("Response Tokens:"), detail.ResponseTokens)
	fmt.Fprintf(&b, "%s %d\n", dimStyle.Render("Total Tokens:"), detail.TokenCount)
	b.WriteString("\n")

	if detail.Action != "" {
		fmt.Fprintf(&b, "%s %s\n", dimStyle.Render("Action:"), detail.Action)
	}
	if detail.Summary != "" {
		fmt.Fprintf(&b, "%s %s\n", dimStyle.Render("Summary:"), detail.Summary)
	}
	if detail.ToolPath != "" {
		fmt.Fprintf(&b, "%s %s\n", dimStyle.Render("Tool Path:"), detail.ToolPath)
	}
	if detail.ToolDurationMs > 0 {
		fmt.Fprintf(&b, "%s %.0fms\n", dimStyle.Render("Tool Duration:"), detail.ToolDurationMs)
	}
	fmt.Fprintf(&b, "%s %d\n", dimStyle.Render("Message Count:"), detail.MessageCount)

	return b.String()
}

// buildRawJSONLens builds Lens ❺: raw JSON with 2-space indent.
func (m dashboardModel) buildRawJSONLens(detail *ipc.GetStepDetailResponse) string {
	data, err := json.MarshalIndent(detail, "", "  ")
	if err != nil {
		return fmt.Sprintf("JSON marshal error: %v", err)
	}
	return string(data)
}

// writeWithTruncation writes content with truncation notice if it exceeds threshold.
func (m dashboardModel) writeWithTruncation(b *strings.Builder, content string) {
	totalLen := utf8.RuneCountInString(content)
	if totalLen > inspectorTruncateThreshold {
		runes := []rune(content)
		b.WriteString(string(runes[:inspectorTruncateThreshold]))
		b.WriteString(renderTruncationNotice(inspectorTruncateThreshold, totalLen))
	} else {
		b.WriteString(content)
	}
	b.WriteString("\n")
}

// renderTruncationNotice renders an explicit truncation notice.
func renderTruncationNotice(shown, total int) string {
	sep := " · "
	if ui.IsASCIIMode() {
		sep = " - "
	}
	return fmt.Sprintf("\n(truncated %s / total %s%so open full)",
		formatCharCount(shown), formatCharCount(total), sep)
}

// findStepIndex finds the index of a step number in inspectorSteps, or -1 if not found.
func (m dashboardModel) findStepIndex(step int) int {
	for i, s := range m.inspectorSteps {
		if s.Step == step {
			return i
		}
	}
	return -1
}

// buildToolCallNameMap maps tool call IDs to names.
func buildToolCallNameMap(msgs []ipc.MessageWire) map[string]string {
	names := make(map[string]string)
	for _, msg := range msgs {
		for _, tc := range msg.ToolCalls {
			names[tc.ID] = tc.Name
		}
	}
	return names
}
