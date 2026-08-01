package ui

import (
	"bytes"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// thinking 原地动效（点律动）—— 覆盖 spec-thinking-animated-indicator 的
// I/O & Edge-Case Matrix 全部 8 个场景。
//
// 既有 Story 60.1 断言（atdd_60_1_thinking_render_test.go）不设 IsTTY/ColorLevel，
// 故走降级路径，本次改动后须保持 GREEN 且不被修改。此文件补齐动效路径、能力降级、
// 显式收尾与恢复起跳。

// newThinkingReporter 构造指定动效能力的 reporter。
//
// animate=true 需同时满足 IsTTY + ColorLevel>0（见 animationEnabled）。ColorLevel>0
// 会让 lipgloss 真的上色，故这里统一 InitStyles(ColorLevel:0) 保持断言可读——样式
// 渲染是否带色不属本 spec 关注点，animationEnabled 只读 Profile 不读全局样式。
func newThinkingReporter(t *testing.T, animate bool, mode OutputMode) (*ProgressReporter, *bytes.Buffer) {
	t.Helper()
	withPlainStyles(t)

	// animationEnabled 的第三个条件读进程级 TERM,故必须固定下来:否则在 Emacs
	// shell-mode / TERM 未设的容器 / dumb 终端下,animate=true 的用例会静默走降级
	// 路径并全线失败(实测 TERM=dumb 时 6 个测试 FAIL)。
	t.Setenv("TERM", "xterm-256color")

	profile := TerminalProfile{IsTTY: animate, ColorLevel: 0}
	if animate {
		profile.ColorLevel = 3
	}
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: mode, Profile: profile}
	return NewProgressReporter(r), &buf
}

// withPlainStyles 安装无色样式并在测试结束后还原，避免包级样式全局量被跨测试污染
// （InitStyles 改写包级变量且无还原机制）。
func withPlainStyles(t *testing.T) {
	t.Helper()
	oldAgent, oldMuted, oldKernel, oldStem := AgentStyle, MutedStyle, KernelStyle, StemStyle
	InitStyles(TerminalProfile{ColorLevel: 0})
	t.Cleanup(func() {
		AgentStyle, MutedStyle, KernelStyle, StemStyle = oldAgent, oldMuted, oldKernel, oldStem
	})
}

// advanceNow 安装可手动推进的时钟，复用既有 nowFunc seam（table.go:415）。
func advanceNow(t *testing.T) func(time.Duration) {
	t.Helper()
	cur := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	old := nowFunc
	nowFunc = func() time.Time { return cur }
	t.Cleanup(func() { nowFunc = old })
	return func(d time.Duration) { cur = cur.Add(d) }
}

// -----------------------------------------------------------------------------
// 场景 1：动效路径连续思考 → 单行原地覆盖，点数循环，输出不含 \n
// -----------------------------------------------------------------------------

func TestThinkingAnimation_InPlaceDotCycle(t *testing.T) {
	advance := advanceNow(t)
	p, buf := newThinkingReporter(t, true, ModeDefault)

	for range 4 {
		p.AgentThinking(7, 1, "reasoning")
		advance(thinkingThrottleWindow)
	}

	out := buf.String()
	if strings.Contains(out, "\n") {
		t.Errorf("动效必须原地刷新、不写换行，实得 %q", out)
	}

	frames := strings.Split(out, ansiClearLine)[1:] // 首段为空
	if len(frames) != 4 {
		t.Fatalf("应有 4 帧原地刷新，实得 %d 帧：%q", len(frames), out)
	}
	want := []string{"thinking.", "thinking..", "thinking...", "thinking."}
	for i, w := range want {
		if !strings.HasSuffix(frames[i], w) {
			t.Errorf("第 %d 帧应以 %q 结尾，实得 %q", i, w, frames[i])
		}
		if !strings.Contains(frames[i], "[agent/7]") {
			t.Errorf("第 %d 帧应含 agent 前缀，实得 %q", i, frames[i])
		}
	}
}

// -----------------------------------------------------------------------------
// 场景 2：降级路径连续思考 → 心跳整行，无 \r/ANSI，行数远小于 delta 数
// -----------------------------------------------------------------------------

func TestThinkingAnimation_Fallback_HeartbeatLines(t *testing.T) {
	advanceNow(t) // 冻结时钟：50 次调用发生在同一瞬间
	p, buf := newThinkingReporter(t, false, ModeDefault)

	const n = 50
	for range n {
		p.AgentThinking(1, 1, "delta")
	}

	out := buf.String()
	assertNoTerminalControl(t, out)
	if got := strings.Count(out, "\n"); got != 1 {
		t.Errorf("冻结时钟下 %d 次增量应只落 1 行心跳，实得 %d 行：%q", n, got, out)
	}
	if !strings.Contains(out, thinkingFallbackFrame) {
		t.Errorf("降级行应含 %q，实得 %q", thinkingFallbackFrame, out)
	}
}

func TestThinkingAnimation_Fallback_HeartbeatWindowElapses(t *testing.T) {
	advance := advanceNow(t)
	p, buf := newThinkingReporter(t, false, ModeDefault)

	p.AgentThinking(1, 1, "delta")
	advance(thinkingHeartbeatWindow / 2)
	p.AgentThinking(1, 1, "delta") // 窗内 → 静默
	advance(thinkingHeartbeatWindow)
	p.AgentThinking(1, 1, "delta") // 跨窗 → 再落一行

	if got := strings.Count(buf.String(), "\n"); got != 2 {
		t.Errorf("跨过心跳窗应共 2 行，实得 %d 行：%q", got, buf.String())
	}
}

// 钉住两个窗口的具体取值。上面的测试用符号本身推进时钟，故常量取任何值它都过
// （自我豁免）；心跳窗是本次对非 TTY 用户最可感知的行为变更（相对 Story 60.1
// 的 400ms 放大 25 倍），必须有字面量守卫。
func TestThinkingAnimation_WindowConstants(t *testing.T) {
	if thinkingThrottleWindow != 400*time.Millisecond {
		t.Errorf("动效帧间隔应为 400ms，实得 %v", thinkingThrottleWindow)
	}
	if thinkingHeartbeatWindow != 10*time.Second {
		t.Errorf("降级心跳窗应为 10s（Story 60.1 原为 400ms，本次刻意放大 25 倍），实得 %v",
			thinkingHeartbeatWindow)
	}
}

// -----------------------------------------------------------------------------
// 场景 3：动效路径高频 delta → 窗内只刷一帧，帧序号不前进
// -----------------------------------------------------------------------------

func TestThinkingAnimation_ThrottledWithinWindow(t *testing.T) {
	advance := advanceNow(t)
	p, buf := newThinkingReporter(t, true, ModeDefault)

	p.AgentThinking(1, 1, "delta")
	first := buf.String()

	advance(thinkingThrottleWindow / 4)
	for range 20 {
		p.AgentThinking(1, 1, "delta")
	}
	if buf.String() != first {
		t.Errorf("节流窗内不应有新输出，之前 %q，之后 %q", first, buf.String())
	}

	// 跨窗后推进到第 2 帧，证明窗内调用没有消耗帧序号。
	advance(thinkingThrottleWindow)
	p.AgentThinking(1, 1, "delta")
	if !strings.HasSuffix(buf.String(), "thinking..") {
		t.Errorf("跨窗后应推进到第 2 帧 (thinking..)，实得 %q", buf.String())
	}
}

// -----------------------------------------------------------------------------
// 场景 4：动效不可用降级 —— IsTTY 为真但 ANSI 能力缺失
// -----------------------------------------------------------------------------

func TestThinkingAnimation_CapabilityDowngrade(t *testing.T) {
	cases := []struct {
		name    string
		profile TerminalProfile
		termEnv string
	}{
		{"NO_COLOR 使 ColorLevel 归零", TerminalProfile{IsTTY: true, ColorLevel: 0}, "xterm-256color"},
		{"TERM=dumb", TerminalProfile{IsTTY: true, ColorLevel: 3}, "dumb"},
		{"TERM=DUMB 大小写不敏感", TerminalProfile{IsTTY: true, ColorLevel: 3}, "DUMB"},
		{"非 TTY", TerminalProfile{IsTTY: false, ColorLevel: 3}, "xterm-256color"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withPlainStyles(t)
			advanceNow(t)
			t.Setenv("TERM", c.termEnv)

			var buf bytes.Buffer
			r := &Renderer{Writer: &buf, OutputMode: ModeDefault, Profile: c.profile}
			p := NewProgressReporter(r)

			p.AgentThinking(7, 1, "reasoning")
			// 后续常规输出也不得写清行序列。
			p.AgentStepComplete(7, 1, "tool_call", "read file")

			assertNoTerminalControl(t, buf.String())
		})
	}
}

// 反向守卫：能力齐备时必须真的走动效路径，否则上面的降级用例会真空 PASS。
func TestThinkingAnimation_CapabilityEnabled(t *testing.T) {
	withPlainStyles(t)
	advanceNow(t)
	t.Setenv("TERM", "xterm-256color")

	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeDefault,
		Profile: TerminalProfile{IsTTY: true, ColorLevel: 3}}
	p := NewProgressReporter(r)

	p.AgentThinking(7, 1, "reasoning")

	if !strings.Contains(buf.String(), ansiClearLine) {
		t.Errorf("能力齐备时应走动效路径（含清行序列），实得 %q", buf.String())
	}
}

// -----------------------------------------------------------------------------
// 场景 5：thinking 后接常规输出 → 先清行，常规行从行首干净起
// -----------------------------------------------------------------------------

func TestThinkingAnimation_ClearedByRegularOutput(t *testing.T) {
	emitters := map[string]func(p *ProgressReporter){
		"AgentStepComplete": func(p *ProgressReporter) { p.AgentStepComplete(7, 3, "tool_call", "read file") },
		"KernelMessage":     func(p *ProgressReporter) { p.KernelMessage("spawning") },
		"StemMessage":       func(p *ProgressReporter) { p.StemMessage("recall") },
		"AgentMessage":      func(p *ProgressReporter) { p.AgentMessage(7, "hello") },
	}
	for name, emit := range emitters {
		t.Run(name, func(t *testing.T) {
			advanceNow(t)
			p, buf := newThinkingReporter(t, true, ModeDefault)
			p.AgentThinking(7, 1, "reasoning")
			buf.Reset() // 只观察后继输出
			emit(p)

			out := buf.String()
			if !strings.HasPrefix(out, ansiClearLine) {
				t.Errorf("%s 应先清掉动效行再输出，实得 %q", name, out)
			}
			if strings.Contains(strings.TrimPrefix(out, ansiClearLine), "thinking") {
				t.Errorf("%s 输出中不应残留 thinking 痕迹，实得 %q", name, out)
			}
		})
	}

	// AgentStep 只在 verbose 模式输出，单独验证。
	t.Run("AgentStep", func(t *testing.T) {
		advanceNow(t)
		p, buf := newThinkingReporter(t, true, ModeVerbose)
		p.AgentThinking(7, 1, "reasoning")
		buf.Reset()
		p.AgentStep(7, 3, 10)
		if !strings.HasPrefix(buf.String(), ansiClearLine) {
			t.Errorf("AgentStep 应先清掉动效行再输出，实得 %q", buf.String())
		}
	})
}

// -----------------------------------------------------------------------------
// 场景 6：显式收尾 Finish() —— 幂等，且降级路径不写控制序列
// -----------------------------------------------------------------------------

func TestThinkingAnimation_Finish(t *testing.T) {
	t.Run("清掉活动动效行", func(t *testing.T) {
		advanceNow(t)
		p, buf := newThinkingReporter(t, true, ModeDefault)
		p.AgentThinking(7, 1, "reasoning")
		buf.Reset()

		p.Finish()

		if buf.String() != ansiClearLine {
			t.Errorf("Finish 应恰好写一次清行序列，实得 %q", buf.String())
		}
	})

	t.Run("幂等：重复调用不再写出", func(t *testing.T) {
		advanceNow(t)
		p, buf := newThinkingReporter(t, true, ModeDefault)
		p.AgentThinking(7, 1, "reasoning")
		p.Finish()
		buf.Reset()

		p.Finish()
		p.Finish()

		if buf.Len() != 0 {
			t.Errorf("重复 Finish 应为 no-op，实得 %q", buf.String())
		}
	})

	t.Run("无活动行时为 no-op", func(t *testing.T) {
		advanceNow(t)
		p, buf := newThinkingReporter(t, true, ModeDefault)

		p.Finish()

		if buf.Len() != 0 {
			t.Errorf("无活动行时 Finish 应静默，实得 %q", buf.String())
		}
	})

	t.Run("降级路径行已自闭合，Finish 不写控制序列", func(t *testing.T) {
		advanceNow(t)
		p, buf := newThinkingReporter(t, false, ModeDefault)
		p.AgentThinking(7, 1, "reasoning")
		buf.Reset()

		p.Finish()

		if buf.Len() != 0 {
			t.Errorf("降级路径 Finish 应为 no-op，实得 %q", buf.String())
		}
	})
}

// -----------------------------------------------------------------------------
// 场景 7：常规输出后恢复思考 → 立即重开且从第 1 帧起跳
// -----------------------------------------------------------------------------

// -----------------------------------------------------------------------------
// 场景 7：常规输出后恢复思考 → 帧相位与节流窗继续推进（不复位）
//
// review-2 修正：早期实现在清行时复位相位与节流窗，实测有两处严重后果——
// 相位归零使律动在生产主导模式（thinking / step_complete 交错）下恒为第 1 帧；
// 节流窗归零使 400ms 限频被击穿。故改为「只抹物理行」。
// -----------------------------------------------------------------------------

func TestThinkingAnimation_ResumeAfterRegularOutput(t *testing.T) {
	advance := advanceNow(t)
	p, buf := newThinkingReporter(t, true, ModeDefault)

	// 推进 2 帧，使 st.frame 停在 2。刻意避开 len(thinkingFrames) 的整数倍——
	// 若推进 3 帧，3%3==0 会让「复位」与「不复位」产生相同的下一帧，测试自我豁免。
	p.AgentThinking(7, 1, "reasoning")
	advance(thinkingThrottleWindow)
	p.AgentThinking(7, 1, "reasoning")
	if !strings.HasSuffix(buf.String(), "thinking..") {
		t.Fatalf("前置条件：应已推进到第 2 帧，实得 %q", buf.String())
	}

	p.AgentStepComplete(7, 1, "tool_call", "read file")
	buf.Reset()

	// 时钟停在第 2 帧的时刻，未跨窗：节流窗未被清行复位，故本次应被吞掉。
	p.AgentThinking(7, 2, "reasoning again")
	if buf.Len() != 0 {
		t.Errorf("清行不得复位节流窗（否则 400ms 限频被击穿），实得 %q", buf.String())
	}

	// 跨过节流窗后恢复出帧，且相位接着第 3 帧而非从头。
	advance(thinkingThrottleWindow)
	p.AgentThinking(7, 2, "reasoning again")
	out := buf.String()
	if out == "" {
		t.Fatal("跨过节流窗后应重开动效行，实得空输出")
	}
	if !strings.HasSuffix(out, "thinking...") {
		t.Errorf("清行不得复位帧相位（否则律动恒为第 1 帧），应接第 3 帧，实得 %q", out)
	}
}

// 生产主导模式回归：thinking 与 step_complete 交错时，点律动必须真的循环。
// 这是 review-2 抓到的核心 bug——早期实现在此模式下恒为 "thinking."。
func TestThinkingAnimation_CyclesUnderInterleavedOutput(t *testing.T) {
	advance := advanceNow(t)
	p, buf := newThinkingReporter(t, true, ModeDefault)

	// 6 轮「思考一下 → 打一条 step_complete」，即真实 agent 推理循环。
	for range 6 {
		p.AgentThinking(7, 1, "reasoning")
		p.AgentStepComplete(7, 1, "tool_call", "read file")
		advance(thinkingThrottleWindow)
	}

	dist := countThinkingFrames(buf.String())
	for dots := 1; dots <= 3; dots++ {
		if dist[dots] == 0 {
			t.Errorf("交错输出下 %d 点帧从未出现，律动失效。完整分布 %v", dots, dist)
		}
	}
}

// countThinkingFrames 按点数统计各帧出现次数。
func countThinkingFrames(s string) map[int]int {
	dist := map[int]int{}
	for _, m := range thinkingFrameRe.FindAllStringSubmatch(s, -1) {
		dist[len(m[1])]++
	}
	return dist
}

var thinkingFrameRe = regexp.MustCompile(`thinking(\.{1,3})`)

// 能力中途消失时，降级分支须先清掉残留动效行，且不得因此绕过节流。
// （review-2：原实现直接写整行，产生 "thinking.[agent/7] thinking..." 粘连，
// 且 hasThinkLine 永久为真，此后每次 Finish/emitLine 都白写一次清行序列。）
func TestThinkingAnimation_CapabilityLostMidRun(t *testing.T) {
	advance := advanceNow(t)
	withPlainStyles(t)
	t.Setenv("TERM", "xterm-256color")

	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeDefault,
		Profile: TerminalProfile{IsTTY: true, ColorLevel: 3}}
	p := NewProgressReporter(r)

	p.AgentThinking(7, 1, "x") // 动效路径，留下未闭合行
	r.Profile.ColorLevel = 0   // 能力消失
	buf.Reset()

	advance(thinkingHeartbeatWindow)
	p.AgentThinking(7, 1, "x") // 降级路径

	out := buf.String()
	if !strings.HasPrefix(out, ansiClearLine) {
		t.Errorf("降级分支应先清掉残留动效行，实得 %q", out)
	}
	if strings.Contains(strings.TrimPrefix(out, ansiClearLine), ansiClearLine) {
		t.Errorf("降级行本身不应再含清行序列，实得 %q", out)
	}

	// 状态须已复位：Finish 应为 no-op，而非再写一次清行序列。
	buf.Reset()
	p.Finish()
	if buf.Len() != 0 {
		t.Errorf("降级写出后 hasThinkLine 应已复位，Finish 应静默，实得 %q", buf.String())
	}

	// 降级写出同样要记账，否则下一次调用会绕过心跳窗。
	buf.Reset()
	p.AgentThinking(7, 1, "x")
	if buf.Len() != 0 {
		t.Errorf("紧邻的下一次调用应被心跳窗节流，实得 %q", buf.String())
	}
}

// -----------------------------------------------------------------------------
// 场景 8：ModeQuiet / ModeJSON 在两种能力状态下均守约
// -----------------------------------------------------------------------------

func TestThinkingAnimation_ModeGuards(t *testing.T) {
	const sentinel = "RAW_THINKING_SENTINEL"

	for _, animate := range []bool{true, false} {
		for _, mode := range []OutputMode{ModeQuiet, ModeJSON} {
			advanceNow(t)
			p, buf := newThinkingReporter(t, animate, mode)
			p.AgentThinking(1, 1, sentinel)
			if buf.Len() != 0 {
				t.Errorf("animate=%v mode=%v 下思考指示必须静默，实得 %q", animate, mode, buf.String())
			}
		}
	}
}

// -----------------------------------------------------------------------------
// 并发安全：driver goroutine 与前台输出并发，-race 下不得报 data race
// -----------------------------------------------------------------------------

func TestThinkingAnimation_ConcurrentOutput(t *testing.T) {
	withPlainStyles(t)
	t.Setenv("TERM", "xterm-256color") // 见 newThinkingReporter：不能依赖宿主 TERM

	// 用真实时钟但把节流窗压到 0 —— 若仍用冻结时钟，除首次外全部调用会在节流处
	// 提前 return，写出路径根本执行不到（真空 PASS）。
	cur := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	var clockMu sync.Mutex
	old := nowFunc
	nowFunc = func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		cur = cur.Add(thinkingThrottleWindow) // 每次取时间都跨窗，保证真的写出
		return cur
	}
	t.Cleanup(func() { nowFunc = old })

	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeDefault,
		Profile: TerminalProfile{IsTTY: true, ColorLevel: 3}}
	p := NewProgressReporter(r)

	const perGoroutine = 25
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range perGoroutine {
				p.AgentThinking(types.PID(7), 1, "delta")
			}
		})
	}
	wg.Go(func() {
		for range perGoroutine {
			p.KernelMessage("tick")
		}
	})
	wg.Go(func() {
		for range perGoroutine {
			p.Finish()
		}
	})
	wg.Wait()

	// 实质断言：常规行必须全部落盘，且每行都从行首起（前面要么是清行序列，
	// 要么是上一行的换行）——即动效帧不会把 [kernel] 顶到行中间。
	out := buf.String()
	if got := strings.Count(out, "[kernel] tick"); got != perGoroutine {
		t.Errorf("常规输出应全部落盘：期望 %d 条，实得 %d 条", perGoroutine, got)
	}
	for line := range strings.SplitSeq(out, "\n") {
		if idx := strings.Index(line, "[kernel]"); idx > 0 {
			before := line[:idx]
			if !strings.HasSuffix(before, ansiClearLine) {
				t.Errorf("[kernel] 行未从行首起，其前缀为 %q（完整行 %q）", before, line)
				break
			}
		}
	}
}

// assertNoTerminalControl 断言输出不含任何原地刷新/ANSI 控制序列。
func assertNoTerminalControl(t *testing.T, out string) {
	t.Helper()
	if strings.Contains(out, "\r") {
		t.Errorf("降级路径不得写 \\r（管道中会成为垃圾字符），实得 %q", out)
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("降级路径不得写 ANSI 控制序列，实得 %q", out)
	}
}
