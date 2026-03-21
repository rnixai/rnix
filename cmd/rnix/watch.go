package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
	"github.com/spf13/cobra"
)

var watchCmd = &cobra.Command{
	Use:   "watch <pid>",
	Short: "Observe agent reasoning steps in real time",
	Long:  "Attach to a running process and display each reasoning step as it completes.",
	Args:  cobra.ExactArgs(1),
	RunE:  runWatch,
}

func runWatch(_ *cobra.Command, args []string) error {
	pidVal, err := strconv.ParseUint(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid pid: %s", args[0])
	}
	pid := types.PID(pidVal)

	streamClient, err := ipc.EnsureDaemon()
	if err != nil {
		return err
	}

	queryClient, err := ipc.Dial(ipc.SocketPath())
	if err != nil {
		streamClient.Close()
		return fmt.Errorf("watch: dial query connection: %w", err)
	}

	profile := ui.DetectProfile(os.Stdout)
	model := newWatchModel(pid, streamClient, queryClient, profile)
	p := tea.NewProgram(model)
	final, runErr := p.Run()
	if fm, ok := final.(watchModel); ok {
		fm.streamClient.Close()
		fm.queryClient.Close()
	} else {
		streamClient.Close()
		queryClient.Close()
	}
	if runErr != nil {
		return fmt.Errorf("watch: %w", runErr)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Legacy line-by-line rendering (used by spawn --watch)
// ---------------------------------------------------------------------------

func renderWatchEvent(ev ipc.StreamEvent, profile ui.TerminalProfile) {
	if ev.Type != ipc.StreamProgress && ev.Type != ipc.StreamComplete && ev.Type != ipc.StreamError {
		return
	}

	var pp ipc.ProgressPayload
	if err := json.Unmarshal(ev.Payload, &pp); err != nil {
		return
	}

	switch pp.Event {
	case "spawn":
		if pp.Provider != "" && pp.Model != "" {
			fmt.Printf("\r\033[K  PID %d (%s/%s)\n", pp.PID, pp.Provider, pp.Model)
		} else {
			fmt.Printf("\r\033[K  PID %d spawned\n", pp.PID)
		}
	case "step":
		if pp.Total > 0 {
			fmt.Printf("\r\033[K  [step %d/%d] thinking...", pp.Step, pp.Total)
		} else {
			fmt.Printf("\r\033[K  [step %d] thinking...", pp.Step)
		}
	case "step_complete":
		icon := watchSuccessIcon(profile)
		if pp.HasError {
			icon = watchErrorIcon(profile)
		}
		dur := watchFormatDuration(pp.DurationMs)
		fmt.Printf("\r\033[K  [step %d] %s → %s  %s  %s\n", pp.Step, pp.Action, pp.Summary, dur, icon)
	case "complete":
		icon := watchSuccessIcon(profile)
		if pp.ExitCode != 0 {
			icon = watchErrorIcon(profile)
		}
		sep := "───────────────────────────"
		if !profile.IsUnicode {
			sep = "---------------------------"
		}
		fmt.Printf("\r\033[K  %s\n", sep)
		fmt.Printf("  %s PID %d completed (exit=%d)\n", icon, pp.PID, pp.ExitCode)
	case "error":
		icon := watchErrorIcon(profile)
		fmt.Printf("\r\033[K  %s error: %s\n", icon, pp.ErrorMessage)
	}
}

func watchSuccessIcon(p ui.TerminalProfile) string {
	if p.IsUnicode {
		return "✓"
	}
	return "OK"
}

func watchErrorIcon(p ui.TerminalProfile) string {
	if p.IsUnicode {
		return "✗"
	}
	return "ERR"
}

func watchFormatDuration(ms float64) string {
	if ms < 1000 {
		return fmt.Sprintf("%.1fs", ms/1000)
	}
	sec := ms / 1000
	if sec < 60 {
		return fmt.Sprintf("%.0fs", sec)
	}
	min := int(sec) / 60
	remSec := int(sec) % 60
	return fmt.Sprintf("%dm%ds", min, remSec)
}

// ---------------------------------------------------------------------------
// BubbleTea TUI (Story 27.4)
// ---------------------------------------------------------------------------

type watchState int

const (
	watchStateNormal   watchState = iota
	watchStateExpanded
	watchStatePager
)

type watchStepInfo struct {
	step       int
	action     string
	summary    string
	durationMs float64
	hasError   bool
}

// BubbleTea messages

type watchEventMsg struct {
	event ipc.StreamEvent
	ch    <-chan ipc.StreamEvent
}

type watchDoneMsg struct{}

type watchDetailMsg struct {
	step   int
	detail *ipc.GetStepDetailResponse
	err    error
}

type watchStartedMsg struct {
	eventCh <-chan ipc.StreamEvent
}

type watchModel struct {
	pid          types.PID
	streamClient *ipc.Client
	queryClient  *ipc.Client
	steps        []watchStepInfo
	cursor       int
	state        watchState
	expandLevel  int // 2 or 3

	detailCache map[int]*ipc.GetStepDetailResponse

	pagerLines  []string
	pagerOffset int
	pagerTitle  string

	width  int
	height int

	completed     bool
	exitCode      int
	errorMsg      string
	profile       ui.TerminalProfile
	providerModel string

	thinkingStep  int
	thinkingTotal int

	embeddedInTop bool // true when launched from top; q returns to top instead of quitting
}

func newWatchModel(pid types.PID, streamClient *ipc.Client, queryClient *ipc.Client, profile ui.TerminalProfile) watchModel {
	return watchModel{
		pid:          pid,
		streamClient: streamClient,
		queryClient:  queryClient,
		detailCache:  make(map[int]*ipc.GetStepDetailResponse),
		profile:      profile,
	}
}

func (m watchModel) Init() tea.Cmd {
	return m.startWatchStream
}

func (m watchModel) startWatchStream() tea.Msg {
	eventCh := make(chan ipc.StreamEvent, 64)
	go func() {
		defer close(eventCh)
		_, _ = m.streamClient.WatchProcess(m.pid, func(ev ipc.StreamEvent) {
			eventCh <- ev
		})
	}()
	return watchStartedMsg{eventCh: eventCh}
}

func waitForEvent(ch <-chan ipc.StreamEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return watchDoneMsg{}
		}
		return watchEventMsg{event: ev, ch: ch}
	}
}

func fetchDetailCmd(client *ipc.Client, pid types.PID, step int) tea.Cmd {
	return func() tea.Msg {
		detail, err := client.GetStepDetail(pid, step)
		return watchDetailMsg{step: step, detail: detail, err: err}
	}
}

func (m watchModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case watchStartedMsg:
		return m, waitForEvent(msg.eventCh)

	case watchEventMsg:
		cmd := m.handleStreamEvent(msg.event)
		return m, tea.Batch(cmd, waitForEvent(msg.ch))

	case watchDoneMsg:
		m.completed = true
		return m, nil

	case watchDetailMsg:
		return m.handleDetailResponse(msg), nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	}
	return m, nil
}

func (m *watchModel) handleStreamEvent(ev ipc.StreamEvent) tea.Cmd {
	if ev.Type != ipc.StreamProgress && ev.Type != ipc.StreamComplete && ev.Type != ipc.StreamError {
		return nil
	}

	var pp ipc.ProgressPayload
	if err := json.Unmarshal(ev.Payload, &pp); err != nil {
		return nil
	}

	switch pp.Event {
	case "spawn":
		if pp.Provider != "" && pp.Model != "" {
			m.providerModel = fmt.Sprintf("%s/%s", pp.Provider, pp.Model)
		}

	case "step":
		m.thinkingStep = pp.Step
		m.thinkingTotal = pp.Total

	case "step_complete":
		info := watchStepInfo{
			step:       pp.Step,
			action:     pp.Action,
			summary:    pp.Summary,
			durationMs: pp.DurationMs,
			hasError:   pp.HasError,
		}
		m.steps = append(m.steps, info)
		m.thinkingStep = 0

		if m.state == watchStateNormal {
			m.cursor = len(m.steps) - 1
		}

		if pp.HasError || pp.DurationMs > 1000 {
			m.cursor = len(m.steps) - 1
			m.state = watchStateExpanded
			m.expandLevel = 2
			return fetchDetailCmd(m.queryClient, m.pid, pp.Step)
		}

	case "complete":
		m.completed = true
		m.exitCode = pp.ExitCode

	case "error":
		m.errorMsg = pp.ErrorMessage
	}
	return nil
}

func (m watchModel) handleDetailResponse(msg watchDetailMsg) watchModel {
	if msg.err != nil {
		m.errorMsg = fmt.Sprintf("detail fetch error: %v", msg.err)
		return m
	}
	if msg.detail != nil {
		m.detailCache[msg.step] = msg.detail
		if m.state == watchStatePager && m.cursor >= 0 && m.cursor < len(m.steps) && m.steps[m.cursor].step == msg.step {
			m.pagerLines = formatPromptForPager(msg.detail)
			m.pagerOffset = 0
		}
	}
	return m
}

func (m watchModel) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch m.state {
	case watchStatePager:
		return m.handlePagerKey(key)
	default:
		return m.handleNormalKey(key)
	}
}

func (m watchModel) handleNormalKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
			if m.state == watchStateExpanded {
				m.state = watchStateNormal
				m.expandLevel = 0
			}
		}
		return m, nil

	case "down", "j":
		if m.cursor < len(m.steps)-1 {
			m.cursor++
			if m.state == watchStateExpanded {
				m.state = watchStateNormal
				m.expandLevel = 0
			}
		}
		return m, nil

	case "v":
		if len(m.steps) == 0 {
			return m, nil
		}
		if m.state == watchStateExpanded {
			m.state = watchStateNormal
			m.expandLevel = 0
			return m, nil
		}
		m.state = watchStateExpanded
		m.expandLevel = 2
		step := m.steps[m.cursor].step
		if _, ok := m.detailCache[step]; ok {
			return m, nil
		}
		return m, fetchDetailCmd(m.queryClient, m.pid, step)

	case "V", "shift+V":
		if len(m.steps) == 0 {
			return m, nil
		}
		if m.state == watchStateExpanded && m.expandLevel == 2 {
			m.expandLevel = 3
			step := m.steps[m.cursor].step
			if _, ok := m.detailCache[step]; ok {
				return m, nil
			}
			return m, fetchDetailCmd(m.queryClient, m.pid, step)
		} else if m.state == watchStateExpanded && m.expandLevel == 3 {
			m.expandLevel = 2
		}
		return m, nil

	case "p":
		if len(m.steps) == 0 {
			return m, nil
		}
		step := m.steps[m.cursor].step
		if detail, ok := m.detailCache[step]; ok {
			m.pagerLines = formatPromptForPager(detail)
			m.pagerOffset = 0
			m.pagerTitle = fmt.Sprintf("Prompt for step %d", step)
			m.state = watchStatePager
			return m, nil
		}
		m.state = watchStatePager
		m.pagerTitle = fmt.Sprintf("Prompt for step %d", step)
		m.pagerLines = []string{"Loading..."}
		m.pagerOffset = 0
		return m, fetchDetailCmd(m.queryClient, m.pid, step)
	}
	return m, nil
}

func (m watchModel) handlePagerKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q", "esc":
		m.state = watchStateNormal
		m.expandLevel = 0
		return m, nil

	case "ctrl+c":
		return m, tea.Quit

	case "up", "k":
		if m.pagerOffset > 0 {
			m.pagerOffset--
		}
	case "down", "j":
		maxOffset := m.pagerMaxOffset()
		if m.pagerOffset < maxOffset {
			m.pagerOffset++
		}
	case "pgup":
		visible := m.pagerVisibleLines()
		m.pagerOffset -= visible
		if m.pagerOffset < 0 {
			m.pagerOffset = 0
		}
	case "pgdown":
		visible := m.pagerVisibleLines()
		maxOffset := m.pagerMaxOffset()
		m.pagerOffset += visible
		if m.pagerOffset > maxOffset {
			m.pagerOffset = maxOffset
		}
	case "g":
		m.pagerOffset = 0
	case "G", "shift+G":
		m.pagerOffset = m.pagerMaxOffset()
	}
	return m, nil
}

func (m watchModel) pagerVisibleLines() int {
	h := m.height - 4
	if h < 1 {
		h = 20
	}
	return h
}

func (m watchModel) pagerMaxOffset() int {
	visible := m.pagerVisibleLines()
	maxOff := len(m.pagerLines) - visible
	if maxOff < 0 {
		return 0
	}
	return maxOff
}

// ---------------------------------------------------------------------------
// View
// ---------------------------------------------------------------------------

func (m watchModel) View() tea.View {
	var b strings.Builder

	switch m.state {
	case watchStatePager:
		m.renderPagerView(&b)
	default:
		m.renderNormalView(&b)
	}

	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

func (m watchModel) renderNormalView(b *strings.Builder) {
	treeLine := m.treeLine()

	if m.providerModel != "" {
		fmt.Fprintf(b, "  PID %d (%s)\n\n", m.pid, m.providerModel)
	} else {
		fmt.Fprintf(b, "  PID %d\n\n", m.pid)
	}

	for i, s := range m.steps {
		cursor := "  "
		if i == m.cursor {
			cursor = "▸ "
		}

		icon := watchSuccessIcon(m.profile)
		if s.hasError {
			icon = watchErrorIcon(m.profile)
		}
		dur := watchFormatDuration(s.durationMs)

		fmt.Fprintf(b, "%s[step %d] %s → %s  %s  %s\n", cursor, s.step, s.action, s.summary, dur, icon)

		if m.state == watchStateExpanded && i == m.cursor {
			m.renderExpandedDetail(b, s.step, treeLine)
		}
	}

	if m.thinkingStep > 0 {
		if m.thinkingTotal > 0 {
			fmt.Fprintf(b, "  [step %d/%d] thinking...\n", m.thinkingStep, m.thinkingTotal)
		} else {
			fmt.Fprintf(b, "  [step %d] thinking...\n", m.thinkingStep)
		}
	}

	if m.completed {
		sep := "───────────────────────────"
		if !m.profile.IsUnicode {
			sep = "---------------------------"
		}
		icon := watchSuccessIcon(m.profile)
		if m.exitCode != 0 {
			icon = watchErrorIcon(m.profile)
		}
		fmt.Fprintf(b, "\n  %s\n", sep)
		fmt.Fprintf(b, "  %s PID %d completed (exit=%d)\n", icon, m.pid, m.exitCode)
	}

	if m.errorMsg != "" {
		fmt.Fprintf(b, "\n  %s %s\n", watchErrorIcon(m.profile), m.errorMsg)
	}

	b.WriteString("\n")
	switch m.state {
	case watchStateExpanded:
		if m.expandLevel == 2 {
			b.WriteString("  [v] Collapse  [V] Debug  [p] Prompt  [q] Quit  [↑↓] Navigate")
		} else {
			b.WriteString("  [v] Collapse  [V] Level 2  [p] Prompt  [q] Quit  [↑↓] Navigate")
		}
	default:
		b.WriteString("  [v] Expand  [p] Prompt  [q] Quit  [↑↓] Navigate")
	}
	b.WriteString("\n")
}

func (m watchModel) renderExpandedDetail(b *strings.Builder, step int, treeLine string) {
	detail, ok := m.detailCache[step]
	if !ok {
		fmt.Fprintf(b, "  %s Loading...\n", treeLine)
		return
	}

	rawResp := truncateStr(detail.RawResponse, 500)
	if rawResp != "" {
		fmt.Fprintf(b, "  %s Response: %s\n", treeLine, rawResp)
	}

	if detail.ToolPath != "" {
		fmt.Fprintf(b, "  %s Tool: %s\n", treeLine, detail.ToolPath)
	}
	if detail.ToolInput != "" {
		fmt.Fprintf(b, "  %s Input: %s\n", treeLine, truncateStr(detail.ToolInput, 300))
	}
	if detail.ToolResult != "" {
		fmt.Fprintf(b, "  %s Result: %s\n", treeLine, truncateStr(detail.ToolResult, 300))
	}
	if detail.ToolError != "" {
		fmt.Fprintf(b, "  %s Error: %s\n", treeLine, truncateStr(detail.ToolError, 300))
	}
	fmt.Fprintf(b, "  %s Tokens: req=%d resp=%d\n", treeLine, detail.RequestTokens, detail.ResponseTokens)

	if m.expandLevel >= 3 {
		sep := "── Debug ──"
		fmt.Fprintf(b, "  %s %s\n", treeLine, sep)
		fmt.Fprintf(b, "  %s Messages: %d (est. %d tokens)\n", treeLine, detail.MessageCount, detail.TokenCount)

		firstUser := extractFirstUserMessage(detail.Messages, 200)
		if firstUser != "" {
			fmt.Fprintf(b, "  %s First user: %s\n", treeLine, firstUser)
		}
	}
}

func (m watchModel) renderPagerView(b *strings.Builder) {
	totalLines := len(m.pagerLines)
	var lineInfo string
	if totalLines == 0 {
		lineInfo = "empty"
	} else {
		lineInfo = fmt.Sprintf("line %d/%d", m.pagerOffset+1, totalLines)
	}
	fmt.Fprintf(b, "  ── %s ── (%s)\n\n", m.pagerTitle, lineInfo)

	visible := m.pagerVisibleLines()
	end := m.pagerOffset + visible
	end = min(end, totalLines)

	for i := m.pagerOffset; i < end; i++ {
		fmt.Fprintf(b, "  %s\n", m.pagerLines[i])
	}

	b.WriteString("\n  [q/Esc] Back  [↑↓/jk] Scroll  [PgUp/PgDn] Page  [g/G] Top/Bottom\n")
}

func (m watchModel) treeLine() string {
	if m.profile.IsUnicode {
		return "┊"
	}
	return "|"
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func truncateStr(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	runes := []rune(s)
	if len(runes) > maxLen {
		return string(runes[:maxLen]) + "..."
	}
	return s
}

func extractFirstUserMessage(messages []ipc.MessageWire, maxLen int) string {
	for _, msg := range messages {
		if msg.Role == "user" {
			content := strings.ReplaceAll(msg.Content, "\n", " ")
			runes := []rune(content)
			if len(runes) > maxLen {
				return string(runes[:maxLen]) + "..."
			}
			return content
		}
	}
	return ""
}

func formatPromptForPager(detail *ipc.GetStepDetailResponse) []string {
	if detail == nil {
		return []string{"(no data)"}
	}

	var lines []string

	lines = append(lines, "[System Prompt]")
	if detail.SystemPrompt != "" {
		lines = append(lines, strings.Split(detail.SystemPrompt, "\n")...)
	} else {
		lines = append(lines, "(empty)")
	}
	lines = append(lines, "")

	lines = append(lines, fmt.Sprintf("[Messages (%d)]", len(detail.Messages)))
	for _, msg := range detail.Messages {
		header := fmt.Sprintf("[%s]", msg.Role)
		contentLines := strings.Split(msg.Content, "\n")
		if len(contentLines) > 0 {
			lines = append(lines, fmt.Sprintf("%s %s", header, contentLines[0]))
			lines = append(lines, contentLines[1:]...)
		} else {
			lines = append(lines, header)
		}
		if len(msg.ToolCalls) > 0 {
			for _, tc := range msg.ToolCalls {
				inputJSON, _ := json.Marshal(tc.Input)
				lines = append(lines, fmt.Sprintf("  → tool_call: %s(%s)", tc.Name, truncateStr(string(inputJSON), 100)))
			}
		}
	}
	lines = append(lines, "")

	lines = append(lines, fmt.Sprintf("[Tools (%d)]", len(detail.Tools)))
	for _, t := range detail.Tools {
		desc := t.Description
		if len(desc) > 80 {
			desc = desc[:80] + "..."
		}
		lines = append(lines, fmt.Sprintf("%s: %s", t.Name, desc))
	}

	return lines
}
