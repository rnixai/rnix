package kernel

import (
	gocontext "context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rnixai/rnix/agents"
	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// ATDD Story 60.1 — AC2 + AC4 (kernel/observe 层): 思考事件触发 OnThinking 回调。
//
// AC2: observe.go 的 stream handler 收到思考事件(AC1 归一后的 thinking)时,除
// emitLog 外须调用 k.callbacks.OnThinking(pid, step, text),使默认前台 spawn 在
// 长思考阶段有实时反馈。
//
// 红灯机制 [[atdd-code-story-red-mechanism-preference]]:
//   - atdd60Callbacks 自带 OnThinking 方法(额外方法,不改生产 KernelCallbacks
//     接口),故本文件可编译,make all 保持绿。
//   - 60.1-INT-001(AC2) 用 t.Skip 标 RED: 现状 handler 只 emitLog/emitEvent,
//     OnThinking 未入接口/未被调用 → 回调永不触发。dev 加接口方法 + observe
//     handler 调用 + 6 处实现补全后,移 skip 验 RED→GREEN。
//   - 60.1-UNIT-004(AC4) 是 green-guard(不 skip): 钉住 AC1 归一策略①依赖的
//     driverEventToLog "thinking"→LogThink 分支不回归。
//
// 同构先例: kernel/atdd_3_6_step_output_streaming_test.go(向 KernelCallbacks
// 加 OnStepComplete 的同款模式) + kernel/fallback_stream_handler_test.go
// (streamingMockLLMFile 经 SetStreamHandler 驱动 stream handler 的范式)。

// ---------------------------------------------------------------------------
// mock: 捕获 OnThinking 调用的 KernelCallbacks 实现
// ---------------------------------------------------------------------------

// thinkingRecord 捕获一次 OnThinking 调用。
type thinkingRecord struct {
	PID  types.PID
	Step int
	Text string
}

// atdd60Callbacks 满足 KernelCallbacks 既有 7 方法,并额外实现 Story 60.1 拟新增
// 的 OnThinking。OnThinking 此刻仅是本 mock 的"额外方法"——KernelCallbacks 接口
// 尚未声明它,故内核暂无法调用(这正是 60.1-INT-001 的 RED 来源)。
type atdd60Callbacks struct {
	mu        sync.Mutex
	thinkings []thinkingRecord
}

func (c *atdd60Callbacks) OnSpawn(_ types.PID, _, _, _, _ string) {}
func (c *atdd60Callbacks) OnStep(_ types.PID, _ int, _ int)       {}
func (c *atdd60Callbacks) OnStepComplete(_ types.PID, _ int, _ string, _ string, _ bool, _ float64) {
}
func (c *atdd60Callbacks) OnComplete(_ types.PID, _ string, _ ExitStatus) {}
func (c *atdd60Callbacks) OnError(_ types.PID, _ error)                   {}
func (c *atdd60Callbacks) OnAskUser(_ types.PID, _ string, _ []byte) ([]byte, error) {
	return nil, nil
}
func (c *atdd60Callbacks) OnStemDiff(_ types.PID, _ []StemMatchResult, _ []string, _ bool) {}

// OnThinking 是 Story 60.1 AC2 拟新增的回调。dev 实现时须把它加入
// kernel/kernel.go 的 KernelCallbacks 接口,并补全全仓 6 个实现点。
func (c *atdd60Callbacks) OnThinking(pid types.PID, step int, text string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.thinkings = append(c.thinkings, thinkingRecord{PID: pid, Step: step, Text: text})
}

func (c *atdd60Callbacks) waitForThinking(timeout time.Duration) *thinkingRecord {
	deadline := time.After(timeout)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-deadline:
			return nil
		case <-ticker.C:
			c.mu.Lock()
			if len(c.thinkings) > 0 {
				r := c.thinkings[0]
				c.mu.Unlock()
				return &r
			}
			c.mu.Unlock()
		}
	}
}

// ---------------------------------------------------------------------------
// mock: 在 Write 时经 stream handler 发 thinking 事件的 LLM 设备
// ---------------------------------------------------------------------------

// atdd60ThinkingLLMFile 实现 vfs.VFSFile + vfs.StreamObserver。内核在该设备上
// 注册 stream handler 后,首次 Write 触发一个 thinking 事件(模拟 AC1 归一后
// driver 转发的思考增量),随后 Read 返回一个完成响应让进程干净退出。
type atdd60ThinkingLLMFile struct {
	mu           sync.Mutex
	readData     []byte
	handler      func(event map[string]any)
	thinkingText string
}

func (f *atdd60ThinkingLLMFile) Write(_ gocontext.Context, _ []byte) error {
	f.mu.Lock()
	h := f.handler
	text := f.thinkingText
	f.mu.Unlock()
	if h != nil {
		h(map[string]any{"type": "thinking", "content": text})
	}
	return nil
}

func (f *atdd60ThinkingLLMFile) Read(_ int) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.readData, nil
}

func (f *atdd60ThinkingLLMFile) Close() error { return nil }

func (f *atdd60ThinkingLLMFile) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{IsDevice: true, Name: "/dev/llm/claude"}, nil
}

func (f *atdd60ThinkingLLMFile) SupportsToolCalling() bool { return true }

// SetStreamHandler implements vfs.StreamObserver.
func (f *atdd60ThinkingLLMFile) SetStreamHandler(fn func(event map[string]any)) {
	f.mu.Lock()
	f.handler = fn
	f.mu.Unlock()
}

// ---------------------------------------------------------------------------
// 60.1-INT-001 (AC2, RED): thinking 事件 → OnThinking 回调被触发
// ---------------------------------------------------------------------------

func TestATDD_60_1_INT_001_ThinkingEventTriggersOnThinking(t *testing.T) {
	const thinkingText = "deliberating over the optimal approach"
	llm := &atdd60ThinkingLLMFile{
		readData:     makeLLMResponse("final answer", 5),
		thinkingText: thinkingText,
	}

	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return llm, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()

	cb := &atdd60Callbacks{}
	k := NewKernel(v, ctxMgr, cb)
	defer k.Shutdown()

	agent := &agents.AgentInfo{Manifest: agents.AgentManifest{Name: "atdd60-thinking"}}
	pid, err := k.Spawn("trigger thinking phase", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	rec := cb.waitForThinking(3 * time.Second)
	if rec == nil {
		t.Fatal("AC2: 超时 — OnThinking 从未被调用(长思考阶段前台无实时反馈)")
	}
	if rec.PID != pid {
		t.Errorf("AC2: 期望 PID %d,实得 %d", pid, rec.PID)
	}
	if !strings.Contains(rec.Text, "deliberating") {
		t.Errorf("AC2: OnThinking 文本应携带思考内容,实得 %q", rec.Text)
	}
}

// ---------------------------------------------------------------------------
// 60.1-UNIT-004 (AC4, green-guard): driverEventToLog "thinking"→LogThink 不回归
// ---------------------------------------------------------------------------

func TestATDD_60_1_AC4_ThinkingMapsToLogThink(t *testing.T) {
	// green-guard(不 skip): AC1 归一策略①(reasoning→thinking)依赖 driverEventToLog
	// 既有 "thinking" 分支映射 LogThink。该分支不得回归,否则 API driver 归一后
	// 反而丢失 [think] 日志。
	cat, content, _, ok := driverEventToLog(map[string]any{
		"type":    "thinking",
		"content": "some reasoning text",
	})
	if !ok {
		t.Fatal("AC4: driverEventToLog 应处理 type=thinking 事件")
	}
	if cat != types.LogThink {
		t.Errorf("AC4: 期望 LogThink 类别,实得 %v", cat)
	}
	if content != "some reasoning text" {
		t.Errorf("AC4: 期望透传内容,实得 %q", content)
	}
}
