package llm

import (
	"context"
	"sync"

	"github.com/rnixai/rnix/vfs"
)

// 56.2 「裁决 1 并发铁律」 的最小骨架（RED 阶段占位）：
//
// driver 是跨进程共享单例（drivers/llm/factory.go RegisterProviders 闭包），
// 而 raw capture 是 per-call 可变数据——若把 capture 存 driver 字段会同时
// 触发「跨进程数据串味」与「-race 必爆」。正解是经 ctx-scoped sink 在
// 调用栈内传递，driver 出口无状态填充，LLMFile 调用返回后归集到 per-Open
// `lastRawCapture` 字段。
//
// 本文件 56.2 dev 完成接线（writeCall/writeStream 注入 sink、各 API driver
// 在 buildParams 出口/HTTP middleware/RoundTripper 填充），现在仅落骨架让
// 测试 t.Skip RED 路径可编译；driver 实例**禁止**新增 capture 字段。

// rawCaptureSink 在 LLMFile.writeCall / writeStream 调用栈内创建，经
// context.Context 下传给 driver；driver 在 SDK middleware / RoundTripper /
// 手写 HTTP 出口取出并填入唯一一次完整 RawCapture。返回前 LLMFile 把
// sink.cap 归集到 f.lastRawCapture——共享的只有 driver 函数体（无状态）。
//
// mu 防御 streaming 路径下 SDK goroutine 写 vs LLMFile goroutine 读：虽然
// channel-close 已建立 happens-before，但加锁能让 -race 检测器对 set/get
// 的语义保持显式（设计裁决 1 第 2 步要求）。
type rawCaptureSink struct {
	mu  sync.Mutex
	cap *vfs.RawCapture
}

func (s *rawCaptureSink) set(c *vfs.RawCapture) {
	s.mu.Lock()
	s.cap = c
	s.mu.Unlock()
}

func (s *rawCaptureSink) get() *vfs.RawCapture {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cap
}

// rawSinkCtxKey 是包内私有 ctx key 类型——用未导出 struct 类型规避碰撞，
// 不让外部包能往同一 ctx 注入冒名顶替的 sink（drivers/llm 包内部唯一来源）。
type rawSinkCtxKey struct{}

// withRawSink 把 sink 挂到 ctx 上往下传。LLMFile.writeCall/writeStream 应在
// 调 driver.Call/Stream 之前调用；driver 出口经 rawSinkFromContext 取回。
//
// 56.2 dev 真正接线（vfsfile.go writeCall/writeStream）。
func withRawSink(ctx context.Context, sink *rawCaptureSink) context.Context {
	return context.WithValue(ctx, rawSinkCtxKey{}, sink)
}

// rawSinkFromContext 从 ctx 取回 sink；nil ctx / 未注入 sink / 类型不匹配
// 一律返回 nil（driver 检查 nil 即跳过捕获，零开销）。
func rawSinkFromContext(ctx context.Context) *rawCaptureSink {
	if ctx == nil {
		return nil
	}
	if v, ok := ctx.Value(rawSinkCtxKey{}).(*rawCaptureSink); ok {
		return v
	}
	return nil
}
