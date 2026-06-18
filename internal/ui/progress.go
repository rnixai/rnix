package ui

import (
	"fmt"
	"sync"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// thinkingThrottleWindow 是思考活动指示的最小刷新间隔。高频思考增量在同一窗口内
// 被聚合为一次可见反馈,避免逐 delta 刷屏(Story 60.1 AC3);窗口跨过后再刷一次,
// 使长思考期间界面持续「在动」。
const thinkingThrottleWindow = 400 * time.Millisecond

// thinkProgress 跟踪单个进程的思考活动节流状态。
type thinkProgress struct {
	lastEmit time.Time // 上次渲染时刻(零值=尚未渲染)
	chars    int       // 本进程累计思考字符数(随增量增长,体现「在动」)
}

// ProgressReporter outputs agent progress messages to the renderer.
type ProgressReporter struct {
	renderer *Renderer

	// 思考活动节流状态(Story 60.1)。OnThinking 可能来自 driver streaming
	// goroutine,多进程并发时共享同一 reporter,故用 mutex 保护 per-PID 状态。
	thinkMu    sync.Mutex
	thinkState map[types.PID]*thinkProgress
}

// NewProgressReporter creates a ProgressReporter attached to the given renderer.
func NewProgressReporter(r *Renderer) *ProgressReporter {
	return &ProgressReporter{renderer: r}
}

// KernelMessage outputs a kernel-prefixed message: [kernel] {message}
func (p *ProgressReporter) KernelMessage(format string, args ...any) {
	if p.renderer.OutputMode == ModeQuiet || p.renderer.OutputMode == ModeJSON {
		return
	}
	msg := fmt.Sprintf(format, args...)
	prefix := KernelStyle.Render("[kernel]")
	fmt.Fprintf(p.renderer.Writer, "%s %s\n", prefix, msg)
}

// StemMessage outputs a stem-prefixed message: [stem] {message}
func (p *ProgressReporter) StemMessage(format string, args ...any) {
	if p.renderer.OutputMode == ModeQuiet || p.renderer.OutputMode == ModeJSON {
		return
	}
	msg := fmt.Sprintf(format, args...)
	prefix := StemStyle.Render("[stem]")
	fmt.Fprintf(p.renderer.Writer, "%s   %s\n", prefix, msg)
}

// AgentMessage outputs an agent-prefixed message: [agent/{pid}] {message}
func (p *ProgressReporter) AgentMessage(pid types.PID, format string, args ...any) {
	if p.renderer.OutputMode == ModeQuiet || p.renderer.OutputMode == ModeJSON {
		return
	}
	msg := fmt.Sprintf(format, args...)
	prefix := AgentStyle.Render(fmt.Sprintf("[agent/%d]", pid))
	fmt.Fprintf(p.renderer.Writer, "%s %s\n", prefix, msg)
}

// AgentStep outputs a reasoning step progress line (verbose mode only after Story 3.6).
func (p *ProgressReporter) AgentStep(pid types.PID, step, total int) {
	if p.renderer.OutputMode != ModeVerbose {
		return
	}
	prefix := AgentStyle.Render(fmt.Sprintf("[agent/%d]", pid))
	fmt.Fprintf(p.renderer.Writer, "%s reasoning step %d...\n", prefix, step)
}

// AgentStepComplete outputs a step completion summary: [agent/{pid}] step {step}: {summary}
// When summary is non-empty, it is displayed directly (action type is omitted to avoid
// double-arrow with tool_call summaries that already contain "→").
// When summary is empty, only the action type is shown.
func (p *ProgressReporter) AgentStepComplete(pid types.PID, step int, action string, summary string) {
	if p.renderer.OutputMode == ModeQuiet || p.renderer.OutputMode == ModeJSON {
		return
	}
	prefix := AgentStyle.Render(fmt.Sprintf("[agent/%d]", pid))
	if summary != "" {
		fmt.Fprintf(p.renderer.Writer, "%s step %d: %s\n", prefix, step, summary)
	} else {
		fmt.Fprintf(p.renderer.Writer, "%s step %d: %s\n", prefix, step, action)
	}
}

// AgentThinking renders a throttled, concise "thinking in progress" indicator
// for the LLM long-reasoning phase (Story 60.1 AC2/AC3). It honors the
// output-mode contracts — no indicator under ModeQuiet, no unstructured thinking
// text under ModeJSON — and throttles high-frequency deltas (time window,
// thinkingThrottleWindow) rather than printing one line per delta.
//
// 默认呈现形态(Dev Notes 权衡 #2)：精简「thinking...」活动指示 + 累计字符数,
// 完整思考文本走 `rnix log`/dashboard 既有通道,不污染前台主输出。节流策略
// (权衡 #3)：时间窗口聚合——首个增量立即给反馈(确认在推进),其后同窗口内的
// 增量静默累积,跨窗口再刷一次。原始思考文本(text)只计入累计字节,不直接打印,
// 因此 ModeJSON 下也不会混入非结构化文本。
func (p *ProgressReporter) AgentThinking(pid types.PID, step int, text string) {
	// ModeQuiet: 完全静默; ModeJSON: 不混入非结构化思考文本(AC3 契约)。
	if p.renderer.OutputMode == ModeQuiet || p.renderer.OutputMode == ModeJSON {
		return
	}

	p.thinkMu.Lock()
	if p.thinkState == nil {
		p.thinkState = make(map[types.PID]*thinkProgress)
	}
	st := p.thinkState[pid]
	if st == nil {
		st = &thinkProgress{}
		p.thinkState[pid] = st
	}
	st.chars += len([]rune(text))
	now := time.Now()
	// 节流：非首次且仍在窗口内 → 累积但不刷屏。
	if !st.lastEmit.IsZero() && now.Sub(st.lastEmit) < thinkingThrottleWindow {
		p.thinkMu.Unlock()
		return
	}
	st.lastEmit = now
	chars := st.chars
	p.thinkMu.Unlock()

	prefix := AgentStyle.Render(fmt.Sprintf("[agent/%d]", pid))
	indicator := MutedStyle.Render(fmt.Sprintf("thinking... (%d chars)", chars))
	fmt.Fprintf(p.renderer.Writer, "%s %s\n", prefix, indicator)
}
