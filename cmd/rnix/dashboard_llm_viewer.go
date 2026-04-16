package main

import (
	"fmt"
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

// --- LLM Viewer (Story 29.6) ---

// enterLLMViewer enters the full-screen LLM conversation viewer.
func (m dashboardModel) enterLLMViewer() (tea.Model, tea.Cmd) {
	if m.selectedPID == 0 {
		m.statusMsg = "No process selected"
		m.statusMsgTTL = statusMsgDefaultTTL
		return m, nil
	}
	m.llmViewerPrevMode = m.viewMode
	m.viewMode = viewLLM
	m.llmViewerPID = m.selectedPID
	m.llmViewerUUID = m.selectedUUID
	m.llmViewerStep = 0
	m.llmViewerStepMax = 0
	m.llmViewerSteps = nil
	m.llmViewerDetail = nil
	m.llmViewerContent = ""
	m.llmViewerFetching = false

	vp := viewport.New(viewport.WithWidth(m.width), viewport.WithHeight(max(m.height-4, 1)))
	m.llmViewerViewport = vp

	return m, fetchLLMStepListCmd(m.selectedPID, m.selectedUUID)
}

// fetchLLMStepListCmd fetches the step summary list for the LLM viewer.
func fetchLLMStepListCmd(pid types.PID, uuid string) tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return llmStepListMsg{pid: pid, err: err}
		}
		defer client.Close()

		var resp *ipc.ListStepsResponse
		if uuid != "" {
			resp, err = client.ListStepsByUUID(uuid, 0)
		} else {
			resp, err = client.ListSteps(pid, 0)
		}
		if err != nil {
			return llmStepListMsg{pid: pid, err: err}
		}
		return llmStepListMsg{pid: pid, steps: resp.Steps, total: resp.Total}
	}
}

// fetchLLMStepCmd fetches a specific step's detail for the LLM viewer.
func fetchLLMStepCmd(pid types.PID, uuid string, step int) tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return llmViewerMsg{pid: pid, step: step, err: err}
		}
		defer client.Close()
		_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))

		var detail *ipc.GetStepDetailResponse
		if uuid != "" {
			detail, err = client.GetStepDetailByUUID(uuid, step)
		} else {
			detail, err = client.GetStepDetail(pid, step)
		}
		return llmViewerMsg{pid: pid, step: step, detail: detail, err: err}
	}
}

// llmViewerKey handles key presses in the LLM viewer overlay.
func (m dashboardModel) llmViewerKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch key {
	case "q":
		return m, tea.Quit
	case "esc":
		m.viewMode = m.llmViewerPrevMode
		return m, nil
	case "h", "left":
		newStep := max(m.llmViewerStep-1, 0)
		if newStep == m.llmViewerStep || m.llmViewerFetching {
			return m, nil
		}
		m.llmViewerStep = newStep
		m.llmViewerFetching = true
		return m, fetchLLMStepCmd(m.llmViewerPID, m.llmViewerUUID, newStep)
	case "l", "right":
		if m.llmViewerStepMax == 0 || m.llmViewerFetching {
			return m, nil // step list not loaded yet or fetch in progress
		}
		newStep := min(m.llmViewerStep+1, m.llmViewerStepMax)
		if newStep == m.llmViewerStep {
			return m, nil
		}
		m.llmViewerStep = newStep
		m.llmViewerFetching = true
		return m, fetchLLMStepCmd(m.llmViewerPID, m.llmViewerUUID, newStep)
	case "y":
		return m, tea.SetClipboard(m.llmViewerContent)
	default:
		// j/k/PgUp/PgDn → viewport scroll
		var cmd tea.Cmd
		m.llmViewerViewport, cmd = m.llmViewerViewport.Update(msg)
		return m, cmd
	}
}

// renderLLMViewer renders the full-screen LLM viewer overlay.
func (m dashboardModel) renderLLMViewer(w, h int) string {
	content := m.llmViewerContent
	if content == "" {
		content = m.buildLLMViewerContent() // fallback for initial/test render
	}

	var b strings.Builder

	_ = w // width available for future line truncation

	// Title bar
	fmt.Fprintf(&b, " LLM Viewer | PID %d", m.llmViewerPID)
	// Show process start time
	for _, p := range m.processes {
		if p.PID == m.llmViewerPID && (m.llmViewerUUID == "" || p.UUID == m.llmViewerUUID) {
			if !p.CreatedAt.IsZero() {
				fmt.Fprintf(&b, " | %s", ui.FormatWallClock(p.CreatedAt))
			}
			break
		}
	}
	if m.llmViewerDetail != nil {
		fmt.Fprintf(&b, " | Step %d", m.llmViewerStep)
		if m.llmViewerDetail.Action != "" {
			fmt.Fprintf(&b, " | %s", m.llmViewerDetail.Action)
		}
	}
	b.WriteString("\n")

	// Content area
	contentH := max(h-3, 1) // title + step nav + margin
	if m.llmViewerViewport.Width() > 0 {
		// Viewport initialized — use for scrolling
		b.WriteString(m.llmViewerViewport.View())
	} else {
		// Viewport not initialized (test/first render) — render raw content
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			if i >= contentH {
				break
			}
			b.WriteString(line)
			b.WriteString("\n")
		}
	}

	// Step navigation bar
	b.WriteString(m.renderStepNavBar())

	return b.String()
}

// buildLLMViewerContent builds the main content for the LLM viewer.
func (m dashboardModel) buildLLMViewerContent() string {
	var b strings.Builder
	d := m.llmViewerDetail

	// REQUEST → block
	b.WriteString("═══ REQUEST → ═══\n\n")
	if d != nil {
		if d.SystemPrompt != "" {
			sysLen := utf8.RuneCountInString(d.SystemPrompt)
			fmt.Fprintf(&b, "%s (system prompt: %s chars)\n\n",
				promptRoleSystem.Render("[system]"), formatCharCount(sysLen))
		}

		toolCallNames := buildToolCallNameMap(d.Messages)
		for _, msg := range d.Messages {
			if msg.Role == "user" || msg.Role == "tool" || msg.Role == "system" {
				roleTag := formatRoleTag(msg, toolCallNames)
				content := truncateContent(msg.Content, promptContentTruncateLimit)
				fmt.Fprintf(&b, "%s %s\n\n", roleTag, content)
			}
		}
		fmt.Fprintf(&b, "  %d request tokens\n\n", d.RequestTokens)
	} else {
		b.WriteString("  (loading...)\n\n")
	}

	// RESPONSE ← block
	b.WriteString("═══ RESPONSE ← ═══\n\n")
	if d != nil {
		for _, msg := range d.Messages {
			if msg.Role == "assistant" {
				content := truncateContent(msg.Content, promptContentTruncateLimit)
				fmt.Fprintf(&b, "%s %s\n\n", promptRoleAssistant.Render("[assistant]"), content)
				for _, tc := range msg.ToolCalls {
					fmt.Fprintf(&b, "  tool_call: %s\n", tc.Name)
				}
			}
		}

		if d.Action != "" {
			fmt.Fprintf(&b, "  Action: %s\n", d.Action)
		}
		if d.Summary != "" {
			fmt.Fprintf(&b, "  Summary: %s\n", d.Summary)
		}

		if d.ToolPath != "" {
			fmt.Fprintf(&b, "  Tool: %s\n", d.ToolPath)
			if d.ToolInput != "" {
				fmt.Fprintf(&b, "  Input: %s\n", truncateContent(d.ToolInput, 200))
			}
			if d.ToolResult != "" {
				fmt.Fprintf(&b, "  Result: %s\n", truncateContent(d.ToolResult, 200))
			}
			if d.ToolError != "" {
				fmt.Fprintf(&b, "  Error: %s\n",
					lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError)).Render(d.ToolError))
			}
			fmt.Fprintf(&b, "  Duration: %.0fms\n", d.ToolDurationMs)
		}
		fmt.Fprintf(&b, "  %d response tokens\n", d.ResponseTokens)
	} else {
		b.WriteString("  (loading...)\n")
	}

	return b.String()
}

// renderStepNavBar renders the step navigation bar at the bottom.
func (m dashboardModel) renderStepNavBar() string {
	var b strings.Builder
	b.WriteString("Steps:")

	if len(m.llmViewerSteps) == 0 {
		b.WriteString(" (none)")
		return b.String()
	}

	for _, s := range m.llmViewerSteps {
		marker := ""
		if s.Step == m.llmViewerStep {
			marker = "*"
		}
		fmt.Fprintf(&b, " [%d%s:%s]", s.Step, marker, s.Action)
	}

	return b.String()
}

// --- helpers ---

func buildToolCallNameMap(msgs []ipc.MessageWire) map[string]string {
	names := make(map[string]string)
	for _, msg := range msgs {
		for _, tc := range msg.ToolCalls {
			names[tc.ID] = tc.Name
		}
	}
	return names
}

func truncateContent(s string, limit int) string {
	if utf8.RuneCountInString(s) > limit {
		runes := []rune(s)
		return string(runes[:limit]) + "..."
	}
	return s
}
