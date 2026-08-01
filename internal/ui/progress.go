package ui

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// thinkingThrottleWindow 是动效路径下的帧间隔。高频思考增量在同一窗口内聚合为
// 一帧,避免逐 delta 刷屏(Story 60.1 AC3);跨窗后推进一帧,使长思考期间界面「在动」。
const thinkingThrottleWindow = 400 * time.Millisecond

// thinkingHeartbeatWindow 是降级路径(非 TTY / 无 ANSI 能力)的心跳行间隔。该场景
// 不能写 `\r` 原地覆盖(在管道里就是垃圾字符),故退化为低频整行:既给出「仍在推进」
// 的活性证据,又不把日志刷满。
//
// ⚠️ 相对 Story 60.1 的原行为(非 TTY 亦用 400ms)放大了 25 倍,是刻意变更。
const thinkingHeartbeatWindow = 10 * time.Second

// thinkingFrames 是动效路径的帧序列——点数循环 1→2→3→1,体现律动。纯 ASCII,
// 故不依赖 Profile.IsUnicode / RNIX_ASCII。
//
// ⚠️ 降级路径取 thinkingFallbackFrame 而非本数组的某个下标,两者刻意解耦:
// 调整帧序列不应静默改变管道日志的文本。
var thinkingFrames = [...]string{"thinking.", "thinking..", "thinking..."}

// thinkingFallbackFrame 是降级路径的固定文本(与 Story 60.1 原输出一致)。
const thinkingFallbackFrame = "thinking..."

// ansiClearLine 把光标移回行首并清到行尾。帧变短时(3 点→1 点)裸 `\r` 会残留旧
// 字符,故每帧前整行清除。仅在动效可用时写出。
const ansiClearLine = "\r\x1b[K"

// thinkProgress 跟踪单个进程的思考活动节流状态。
type thinkProgress struct {
	lastEmit time.Time // 上次渲染时刻(零值=尚未渲染,下次调用立即出帧)
	frame    int       // 下一帧在 thinkingFrames 中的下标(动效路径用)
}

// ProgressReporter outputs agent progress messages to the renderer.
type ProgressReporter struct {
	renderer *Renderer

	// outMu 保护「动效状态 + 对 renderer.Writer 的写出」这一整体。OnThinking 来自
	// driver streaming goroutine,与前台其他输出并发。
	//
	// 状态与写出必须在同一临界区:hasThinkLine 描述的是「终端上物理存在什么」,
	// 若先在锁内改状态、再在锁外写出,两者会脱钩(A 记录自己占有动效行,B 抢先写出
	// → ANSI 序列错位);同理「先清行再写正文」若不原子,中间插入一帧动效会把正文
	// 顶到残帧之后。
	outMu      sync.Mutex
	thinkState map[types.PID]*thinkProgress

	// hasThinkLine 表示存在一条未闭合的动效行(原地刷新不写 `\n`)。它必须在打常规
	// 行前、以及思考终止时(Finish)被清除。
	hasThinkLine bool
}

// NewProgressReporter creates a ProgressReporter attached to the given renderer.
func NewProgressReporter(r *Renderer) *ProgressReporter {
	return &ProgressReporter{renderer: r}
}

// animationEnabled 判定能否使用原地动效(即能否安全写 `\r` 与 ANSI 控制序列)。
//
// isatty 并不等同于「支持 ANSI」,也不等同于「用户想要动效」,故三重判定:
//   - IsTTY: 管道/重定向下 `\r` 只是垃圾字符。
//   - ColorLevel > 0: DetectProfile 在 NO_COLOR 时把它归零,借此一并尊重「用户显式
//     要求无装饰输出」的诉求。
//   - TERM != "dumb": Emacs shell-mode 等 isatty 为真却不解析 ANSI 的终端。
//
// 对齐 Codex 的 MotionMode::Reduced 与 Claude Code 的 prefersReducedMotion。
func (p *ProgressReporter) animationEnabled() bool {
	return p.renderer.Profile.IsTTY &&
		p.renderer.Profile.ColorLevel > 0 &&
		!strings.EqualFold(os.Getenv("TERM"), "dumb")
}

// emitLine 输出一条常规进度行(format 须自带换行),并在同一临界区内先抹掉未闭合的
// 动效行,使本行从行首干净起行。所有换行型输出方法都必须经由此处。
//
// ⚠️ 这只是 ProgressReporter 内部的不变量。同一个 Writer 在 cmd/rnix 下另有大量
// 直写点(RenderSummary/RenderResult/RenderError/ask_user 等),它们绕过本方法,
// 故收尾另需显式 Finish()。
func (p *ProgressReporter) emitLine(format string, args ...any) {
	p.outMu.Lock()
	defer p.outMu.Unlock()

	p.clearThinkLineLocked()
	fmt.Fprintf(p.renderer.Writer, format, args...)
}

// clearThinkLineLocked 抹掉未闭合的动效行。调用方须持有 outMu。
//
// ⚠️ 只抹物理行,**不得**复位 thinkProgress 的 lastEmit / frame。曾经复位过,实测
// 有两处严重后果(review-2):
//   - 帧相位归零使点律动在真实场景下完全失效。生产主导模式是「思考几下 → 打一条
//     step_complete」交错,每条常规行都经 emitLine 走到这里;复位后每次都从第 1 帧
//     起,实测 6 轮交错「1点=6, 2点=0, 3点=0」——恒为 thinking.,律动看不见。
//   - 节流窗归零使 400ms 限频被击穿:下个 delta 命中 lastEmit.IsZero() 短路立即出
//     帧。冻结时钟下 10 轮交错写出 10 帧(本应 1 帧),交错越密放大越严重。
//
// 代价是清行后最长等一个节流窗才出下一帧,但触发清行的那条常规行本身就是可见反馈,
// 空窗并不可感知。
func (p *ProgressReporter) clearThinkLineLocked() {
	if !p.hasThinkLine {
		return
	}
	p.hasThinkLine = false

	// 降级路径的行自带 `\n` 已闭合,不会置 hasThinkLine,故这里必然是动效路径。
	fmt.Fprint(p.renderer.Writer, ansiClearLine)
}

// Finish 闭合尚未终止的思考动效行,使其后的输出(包括绕过 ProgressReporter 的
// RenderSummary/RenderError、ask_user 提问,以及进程退出后的 shell 提示符)从行首
// 干净起行。幂等:无活动行时为 no-op。
//
// 之所以需要显式调用而非自动收尾:OnThinking 只有增量、没有结束事件,
// AgentThinking 自身无从得知思考何时终止。
func (p *ProgressReporter) Finish() {
	p.outMu.Lock()
	defer p.outMu.Unlock()
	p.clearThinkLineLocked()
}

// KernelMessage outputs a kernel-prefixed message: [kernel] {message}
func (p *ProgressReporter) KernelMessage(format string, args ...any) {
	if p.renderer.OutputMode == ModeQuiet || p.renderer.OutputMode == ModeJSON {
		return
	}
	msg := fmt.Sprintf(format, args...)
	prefix := KernelStyle.Render("[kernel]")
	p.emitLine("%s %s\n", prefix, msg)
}

// StemMessage outputs a stem-prefixed message: [stem] {message}
func (p *ProgressReporter) StemMessage(format string, args ...any) {
	if p.renderer.OutputMode == ModeQuiet || p.renderer.OutputMode == ModeJSON {
		return
	}
	msg := fmt.Sprintf(format, args...)
	prefix := StemStyle.Render("[stem]")
	p.emitLine("%s   %s\n", prefix, msg)
}

// AgentMessage outputs an agent-prefixed message: [agent/{pid}] {message}
func (p *ProgressReporter) AgentMessage(pid types.PID, format string, args ...any) {
	if p.renderer.OutputMode == ModeQuiet || p.renderer.OutputMode == ModeJSON {
		return
	}
	msg := fmt.Sprintf(format, args...)
	prefix := AgentStyle.Render(fmt.Sprintf("[agent/%d]", pid))
	p.emitLine("%s %s\n", prefix, msg)
}

// AgentStep outputs a reasoning step progress line (verbose mode only after Story 3.6).
func (p *ProgressReporter) AgentStep(pid types.PID, step, total int) {
	if p.renderer.OutputMode != ModeVerbose {
		return
	}
	prefix := AgentStyle.Render(fmt.Sprintf("[agent/%d]", pid))
	p.emitLine("%s reasoning step %d...\n", prefix, step)
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
		p.emitLine("%s step %d: %s\n", prefix, step, summary)
	} else {
		p.emitLine("%s step %d: %s\n", prefix, step, action)
	}
}

// AgentThinking renders a throttled, concise "thinking in progress" indicator
// for the LLM long-reasoning phase (Story 60.1 AC2/AC3). It honors the
// output-mode contracts — no indicator under ModeQuiet, no unstructured thinking
// text under ModeJSON — and throttles high-frequency deltas rather than printing
// one line per delta.
//
// 呈现形态：纯活动指示——不显示思考字符计数,回避「累计 vs 每轮」语义争议;完整
// 思考文本走 `rnix log`/dashboard 既有通道,不污染前台主输出。text/step 形参为
// OnThinking 接口对称保留,此形态下不直接使用(原始思考文本从不进前台,故 ModeJSON
// 下也不会混入非结构化文本)。
//
// 两条路径按 animationEnabled() 分岔：
//   - 动效：单行原地覆盖(`\r\x1b[K`),点数循环 1→2→3→1,不写 `\n`。行的收尾由
//     emitLine 或 Finish 负责。
//   - 降级：thinkingHeartbeatWindow 间隔的整行输出,不含任何控制序列。
//
// 帧推进只由本方法被动调用驱动(driver delta 触发),不起独立 ticker goroutine:
// 匀速动画需要明确的生命周期起止信号,而当前回调接口只有增量。代价是 delta 稀疏
// 时动效变慢——这本身正确反映了「LLM 输出确实慢」。
func (p *ProgressReporter) AgentThinking(pid types.PID, step int, text string) {
	// ModeQuiet: 完全静默; ModeJSON: 不混入非结构化思考文本(AC3 契约)。
	if p.renderer.OutputMode == ModeQuiet || p.renderer.OutputMode == ModeJSON {
		return
	}

	animate := p.animationEnabled()
	window := thinkingHeartbeatWindow
	if animate {
		window = thinkingThrottleWindow
	}

	p.outMu.Lock()
	defer p.outMu.Unlock()

	if p.thinkState == nil {
		p.thinkState = make(map[types.PID]*thinkProgress)
	}
	st := p.thinkState[pid]
	if st == nil {
		st = &thinkProgress{}
		p.thinkState[pid] = st
	}
	now := nowFunc()
	// 节流：非首次且仍在窗口内 → 不刷屏,帧也不前进。
	if !st.lastEmit.IsZero() && now.Sub(st.lastEmit) < window {
		return
	}

	prefix := AgentStyle.Render(fmt.Sprintf("[agent/%d]", pid))
	st.lastEmit = now

	if !animate {
		// 先清掉可能残留的动效行:能力判定依赖 Profile 与进程级 TERM,原则上不该
		// 在一次运行内变化,但一旦变化(或未来有人让它可变),不清行会把心跳行接在
		// 残帧后,且 hasThinkLine 会永久为真——此后每次 emitLine/Finish 都白写一次
		// 清行序列,擦掉本不该擦的内容。清行是幂等的,无残留时为 no-op。
		p.clearThinkLineLocked()
		fmt.Fprintf(p.renderer.Writer, "%s %s\n", prefix, MutedStyle.Render(thinkingFallbackFrame))
		return
	}

	frame := thinkingFrames[st.frame%len(thinkingFrames)]
	st.frame++
	fmt.Fprintf(p.renderer.Writer, "%s%s %s", ansiClearLine, prefix, MutedStyle.Render(frame))
	p.hasThinkLine = true
}
