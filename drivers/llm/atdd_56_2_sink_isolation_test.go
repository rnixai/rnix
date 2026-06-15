package llm

// ATDD coverage for Story 56.2 — sink 抽象 + LLMFile 字段优先 + 并发隔离
// （AC #1 / #2 / #9）。
//
// 红灯机制（记忆 [[atdd-code-story-red-mechanism-preference]]）：
//   - 业务断言（CAP-1/2 真实落 sink、driver 出口接线）→ t.Skip 占位 RED；
//     56.2 dev 接线后移除 skip 并填充断言，验 RED → GREEN。
//   - GREEN-guard（field 优先 fallback / sink ctx 透传契约 / 不导入 kernel）
//     不 skip——sink 抽象骨架 + LLMFile.lastRawCapture 字段已就位即可立绿，
//     防止 dev 在动手时改坏 56.1 INT-001 或 sink 契约。

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/rnixai/rnix/vfs"
)

// ============================================================================
// 56-2-INT-001 — LLMFile.lastRawCapture 字段优先于 driver 委托（AC #1, #2）
//
// GREEN-guard：56.2 dev 接线前后都应保持
//   1. 字段非 nil → 直接返回字段；
//   2. 字段 nil + driver 实现 rawCaptureDriver → 委托（保 56.1 INT-001 全绿）；
//   3. 字段 nil + driver 不实现 → 返回 nil（noop driver 路径不变）。
// 三路同时验证防止 dev 改写 LastRawCapture 时回归。
// ============================================================================

func TestATDD_56_2_INT001_LLMFile_FieldFirstThenDelegate(t *testing.T) {
	// (a) 字段优先：lastRawCapture 非 nil 时无视 driver 委托返回字段值
	fieldCap := &vfs.RawCapture{Step: 99, Kind: "api"}
	delegateCap := &vfs.RawCapture{Step: 7, Kind: "api"}
	f := &LLMFile{
		driver:         &optInDriver{rc: delegateCap},
		lastRawCapture: fieldCap,
	}
	got := f.LastRawCapture()
	if got != fieldCap {
		t.Errorf("(a) field-first: got=%p want=%p (字段优先于 driver 委托)", got, fieldCap)
	}
	if got != nil && got.Step != 99 {
		t.Errorf("(a) field-first content mismatch: step=%d want=99", got.Step)
	}

	// (b) 字段 nil + opt-in driver → fallback 委托（保 56.1 INT-001 兼容）
	f2 := &LLMFile{driver: &optInDriver{rc: delegateCap}}
	got2 := f2.LastRawCapture()
	if got2 != delegateCap {
		t.Errorf("(b) fallback: got=%p want=%p (字段 nil 时应委托 driver)", got2, delegateCap)
	}

	// (c) 字段 nil + noop driver → nil（56.1 全部 driver 状态）
	f3 := &LLMFile{driver: noopDriver{}}
	if f3.LastRawCapture() != nil {
		t.Error("(c) noop driver: LastRawCapture() != nil, want nil")
	}

	// (d) vfs.RawCaptureProvider 接口编译期断言保持有效
	var _ vfs.RawCaptureProvider = (*LLMFile)(nil)
}

// ============================================================================
// 56-2-INT-002 — sink ctx 透传契约（AC #1, #9）
//
// GREEN-guard：rawCaptureSink + withRawSink + rawSinkFromContext 是 driver
// 出口取 sink 的唯一通道（裁决 1 第 2 步），契约一旦回退（如 ctx key 类型
// 改 string 起冲突、sink struct 漏锁、helper 函数签名变化）就让 56.2 全部
// driver 接线散架。本测试锁死契约：
//   - 未挂 sink 的 ctx → rawSinkFromContext 返回 nil；
//   - 挂 sink 后从 ctx 取出的就是同一个指针（设计裁决：包内私有 ctx key）；
//   - sink.set / sink.get 互通（防御 SDK 写 vs LLMFile 读的并发可见性）；
//   - nil ctx 也安全（driver 出口可零开销跳过）。
// ============================================================================

func TestATDD_56_2_INT002_RawCaptureSink_CtxContract(t *testing.T) {
	// (a) 空 ctx — 未注入 sink 应取回 nil（driver 出口零开销跳过）
	if rawSinkFromContext(context.Background()) != nil {
		t.Error("(a) bare ctx: rawSinkFromContext != nil, want nil")
	}
	// (b) nil-ish ctx — TODO ctx 也未挂 sink，应安全返回 nil（driver 出口
	// 即便意外拿到 TODO/Background 之外的 ctx 也不应 panic）
	if rawSinkFromContext(context.TODO()) != nil {
		t.Error("(b) TODO ctx: rawSinkFromContext != nil, want nil")
	}

	// (c) withRawSink + rawSinkFromContext 透传同一指针
	sink := &rawCaptureSink{}
	ctx := withRawSink(context.Background(), sink)
	got := rawSinkFromContext(ctx)
	if got != sink {
		t.Errorf("(c) ctx round-trip: got=%p want=%p (private ctx key 应保持身份)", got, sink)
	}

	// (d) sink set/get 互通
	rc := &vfs.RawCapture{Step: 1, Kind: "api"}
	sink.set(rc)
	if got := sink.get(); got != rc {
		t.Errorf("(d) sink set/get: got=%p want=%p", got, rc)
	}

	// (e) sink 默认零值 get → nil
	if (&rawCaptureSink{}).get() != nil {
		t.Error("(e) zero-value sink.get() != nil, want nil")
	}
}

// ============================================================================
// 56-2-INT-003 — sink 并发隔离（AC #2 铁律, -race）
//
// GREEN-guard：模拟「跨进程共享 driver」场景——两个独立 LLMFile 共用 sink
// 抽象，并行 set / get 不同 capture，验证：
//   - 各自取回自己的；
//   - 写入与读取在 -race 下干净（mu 守住 SDK goroutine 写 vs LLMFile 读）；
//   - 即便不同 sink 实例，driver 通过 ctx 取到的也只是「自己那次调用栈内」
//     的 sink，零跨实例串味。
// 这条 GREEN 不验 driver 接线本身（那是 INT-005..010 的事），仅锁 sink
// 抽象在并发下不退化。
// ============================================================================

func TestATDD_56_2_INT003_Sink_ConcurrentNoCrossTalk(t *testing.T) {
	const N = 64

	// 每个 worker 自有一个 sink + ctx，模拟 LLMFile per-Open 调用栈。
	// 共用 driver 的 fake：driver 函数体只 read ctx → write sink，无状态。
	fakeDriverCallEntry := func(ctx context.Context, want int64) {
		s := rawSinkFromContext(ctx)
		if s == nil {
			t.Errorf("worker: ctx 未挂 sink (want set)")
			return
		}
		s.set(&vfs.RawCapture{Step: int(want), Kind: "api"})
	}

	var wg sync.WaitGroup
	results := make([]*vfs.RawCapture, N)
	var mismatched atomic.Int64

	for i := range N {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sink := &rawCaptureSink{}
			ctx := withRawSink(context.Background(), sink)
			fakeDriverCallEntry(ctx, int64(idx))
			got := sink.get()
			results[idx] = got
			if got == nil || got.Step != idx {
				mismatched.Add(1)
			}
		}(i)
	}
	wg.Wait()

	if n := mismatched.Load(); n != 0 {
		t.Errorf("并发隔离破裂：%d/%d worker 取到的 capture 与自己写入的不匹配", n, N)
	}
	for i, r := range results {
		if r == nil {
			t.Errorf("worker[%d]: nil capture", i)
		}
	}
}

// ============================================================================
// 56-2-INT-004 — drivers/llm 不导入 kernel（Constraint #9 / AC #9）
//
// GREEN-guard：编译期 + 文件级双重断言。
//   - 编译期断言：raw_capture.go 仅依赖 vfs 与标准库——本测试 import 与
//     该包同布局，build 成功即证；
//   - 文件级 grep：扫 raw_capture.go 与 vfsfile.go 56.2 区域不出现
//     "kernel/" 字符串（保险绳，防 56.2 dev 反向加 import）。
//
// 这条 GREEN 让「drivers/ → kernel/ 反向依赖」回归在 lint 之前先撞测试。
// ============================================================================

func TestATDD_56_2_INT004_DriversLLMNoKernelImport(t *testing.T) {
	// 编译期：本测试文件能被 go test 编译通过即证 raw_capture.go 不间接拉
	// 进 kernel（go build 会沿 import 拓扑校验）。无需运行时断言。
	//
	// 文件级保险绳：扫描 56.2 关键源文件，若出现 "rnix/kernel" import 路径
	// 则提前在测试层兜底（lint 也会兜，但测试更快定位）。
	for _, path := range []string{"raw_capture.go", "vfsfile.go"} {
		src := mustReadFile(t, path)
		if strings.Contains(src, `"github.com/rnixai/rnix/kernel`) {
			t.Errorf("%s: 出现 kernel 反向 import (Constraint：drivers/llm 不导入 kernel)", path)
		}
	}
}

// ============================================================================
// 56-2-INT-005 — sink 写入流程实战（AC #1, RED）
//
// 业务断言：driver 在 ctx-scoped sink 出口填充 RawCapture，LLMFile 调用返回
// 后归集到 lastRawCapture 字段。56.2 dev 接线前 t.Skip；接线后移除 skip 即
// 自动覆盖 4 driver 的最小成功路径。
// ============================================================================

func TestATDD_56_2_INT005_SinkPopulationFlow_RED(t *testing.T) {
	t.Skip("56.2 dev 阶段移除：等 vfsfile.writeCall/writeStream 接线 sink + 任一 API driver 出口填 sink 后断言 LLMFile.LastRawCapture() 非 nil 且 Kind=='api'")
}
