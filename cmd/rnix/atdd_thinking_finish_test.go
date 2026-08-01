package main

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/ui"
)

// runRoot 里 progress.Finish() 的调用点回归护栏。
//
// 背景（review-2）：这些调用点是本次改动在 cmd 侧的全部内容，而审查证明把四处全
// 替换成 no-op 后测试仍全绿——零回归保护。runRoot 本体需要真 daemon 难以单测，
// 故此处复刻其收尾结构，钉住「绕过 ProgressReporter 的直写之前必须先 Finish」。

// ttyReporter 构造一个动效可用的 reporter，并返回累积输出。
func ttyReporter(t *testing.T) (*ui.ProgressReporter, *ui.Renderer, *syncWriter) {
	t.Helper()
	t.Setenv("TERM", "xterm-256color")
	w := &syncWriter{}
	renderer := &ui.Renderer{
		Writer:     w,
		OutputMode: ui.ModeDefault,
		Profile:    ui.TerminalProfile{Width: 80, IsTTY: true, ColorLevel: 3, IsUnicode: true},
	}
	ui.InitStyles(ui.TerminalProfile{ColorLevel: 0}) // 无色，便于断言
	return ui.NewProgressReporter(renderer), renderer, w
}

// 终态汇聚点：Finish 之后 RenderSummary 必须从行首起，不与动效帧粘连。
// 这是 review-1 实测到的 IG-2（"thinking.[kernel] PID 7 exited(0)..."）。
func TestThinkingFinish_BeforeTerminalOutput(t *testing.T) {
	progress, renderer, w := ttyReporter(t)

	progress.AgentThinking(7, 1, "reasoning")
	progress.Finish() // main.go: spawnedPID.Store 之后、所有终态分支之前
	ui.RenderSummary(renderer, 7, 0, 1234, 2*time.Second, "claude", "opus", "")

	out := w.String()
	idx := strings.Index(out, "[kernel]")
	if idx < 0 {
		t.Fatalf("summary 未输出：%q", out)
	}
	if !strings.HasSuffix(out[:idx], "\r\x1b[K") {
		t.Errorf("summary 必须紧跟清行序列（否则与动效帧粘连），其前缀为 %q", out[:idx])
	}
	if strings.Contains(out[idx:], "thinking") {
		t.Errorf("summary 之后不应有 thinking 残留：%q", out[idx:])
	}
}

// 二次 SIGINT 强退：Finish 必须在 forceExitFunc 之前，否则 shell 提示符画在
// 「thinking..」后面。复刻 main.go 的 SIGINT goroutine 结构。
func TestThinkingFinish_BeforeForceExit(t *testing.T) {
	progress, _, w := ttyReporter(t)

	exitCh := make(chan int, 1)
	saved := forceExitFunc
	forceExitFunc = func(code int) { exitCh <- code }
	defer func() { forceExitFunc = saved }()

	progress.AgentThinking(7, 1, "reasoning")
	beforeLen := len(w.String())

	sigCh := make(chan os.Signal, 2)
	var wg sync.WaitGroup
	wg.Go(func() {
		<-sigCh
		progress.KernelMessage("interrupted (SIGINT)")
		select {
		case <-sigCh:
			progress.Finish()
			forceExitFunc(130)
		case <-time.After(2 * time.Second):
		}
	})

	sigCh <- os.Interrupt
	time.Sleep(20 * time.Millisecond)
	sigCh <- os.Interrupt

	select {
	case code := <-exitCh:
		if code != 130 {
			t.Errorf("强退码应为 130，实得 %d", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("二次 SIGINT 未触发强退")
	}
	wg.Wait()

	// 真正的不变量：强退时屏幕上不得留下未闭合的动效行。
	// 本例中首次 SIGINT 的 KernelMessage 已经过 emitLine 清掉了动效行，故二次强退
	// 的 Finish 是幂等 no-op——它守的是「KernelMessage 未能执行」等其它路径。
	tail := w.String()[beforeLen:]
	if !strings.HasSuffix(tail, "\n") {
		t.Errorf("强退前最后一次写出应是已闭合的整行，实得尾部 %q", tail)
	}
	if strings.Contains(tail, "thinking") {
		t.Errorf("强退前不应残留动效帧，实得尾部 %q", tail)
	}
}

// 无 KernelMessage 兜底时，Finish 是唯一的收尾手段。
func TestThinkingFinish_ForceExitWithoutPriorOutput(t *testing.T) {
	progress, _, w := ttyReporter(t)

	progress.AgentThinking(7, 1, "reasoning")
	if !strings.HasSuffix(w.String(), "thinking.") {
		t.Fatalf("前置条件：应留下未闭合动效行，实得 %q", w.String())
	}

	progress.Finish()

	if !strings.HasSuffix(w.String(), "\r\x1b[K") {
		t.Errorf("Finish 应闭合动效行，实得 %q", w.String())
	}
}

// ask_user 弹问前收尾：否则提问文本接在动效帧后，且此后任一次清行会擦掉
// 用户的问答内容而非残留动效行（review-1 IG-2a）。
func TestThinkingFinish_BeforeAskUser(t *testing.T) {
	progress, _, w := ttyReporter(t)

	progress.AgentThinking(7, 1, "reasoning")
	progress.Finish() // main.go: StreamAskUser 分支，handleAskUserEvent 之前
	// handleAskUserEvent 会裸写 os.Stdout，此处只验证收尾已发生。

	if !strings.HasSuffix(w.String(), "\r\x1b[K") {
		t.Errorf("ask_user 之前应已闭合动效行，实得 %q", w.String())
	}
}
