package llm

import (
	"context"
	"testing"

	"github.com/rnixai/rnix/vfs"
)

// ============================================================================
// ATDD Story 56.1 — LLMFile.LastRawCapture 委托 (AC#2)
//
// 56-1-INT-001. 56.1 全部 driver 均未实现 rawCaptureDriver — LLMFile 应恒返回 nil。
// 56.2/56.3 才填充实际的 raw capture。
//
// 这是终态实现（不需 t.Skip）：源码骨架 LastRawCapture() 已正确委托并在
// type-assert 失败时返回 nil，dev 阶段不应改动这个行为，仅由 56.2/56.3 通过
// 实现 rawCaptureDriver 来"激活"非 nil 路径。
// ============================================================================

// noopDriver implements LLMDriver but NOT rawCaptureDriver — 模拟 56.1 的
// 全部 driver 状态。
type noopDriver struct{}

func (noopDriver) Call(_ context.Context, _ LLMRequest) (*LLMResponse, error) {
	return &LLMResponse{}, nil
}

func (noopDriver) Stream(_ context.Context, _ LLMRequest) (<-chan StreamEvent, error) {
	ch := make(chan StreamEvent)
	close(ch)
	return ch, nil
}

func (noopDriver) Info() DriverInfo { return DriverInfo{Name: "noop", Provider: "test"} }

// optInDriver 实现 rawCaptureDriver — 模拟 56.2/56.3 之后的状态，
// 用于反向断言"委托确实生效"（56.1 实现正确则 opt-in 时返回非 nil）。
type optInDriver struct {
	rc *vfs.RawCapture
}

func (o *optInDriver) Call(_ context.Context, _ LLMRequest) (*LLMResponse, error) {
	return &LLMResponse{}, nil
}

func (o *optInDriver) Stream(_ context.Context, _ LLMRequest) (<-chan StreamEvent, error) {
	ch := make(chan StreamEvent)
	close(ch)
	return ch, nil
}

func (o *optInDriver) Info() DriverInfo                { return DriverInfo{Name: "optin", Provider: "test"} }
func (o *optInDriver) LastRawCapture() *vfs.RawCapture { return o.rc }

// 56-1-INT-001: driver 未实现 rawCaptureDriver → LLMFile.LastRawCapture() 必须返回 nil。
func TestATDD_56_1_INT001_LLMFile_LastRawCapture_NilWhenDriverUnimplemented(t *testing.T) {
	// (a) noop driver — 56.1 状态：所有 driver 都不实现 rawCaptureDriver
	f := &LLMFile{driver: noopDriver{}}
	if got := f.LastRawCapture(); got != nil {
		t.Errorf("LastRawCapture() = %+v, want nil (56.1: no driver implements rawCaptureDriver)", got)
	}

	// (b) opt-in driver — 反向断言：56.2/56.3 时委托应当生效
	want := &vfs.RawCapture{Step: 7, Kind: "api"}
	f2 := &LLMFile{driver: &optInDriver{rc: want}}
	got := f2.LastRawCapture()
	if got == nil {
		t.Fatal("opt-in driver: LastRawCapture() = nil, want non-nil "+
			"(委托未生效会让 56.2/56.3 直接退化)")
	}
	if got.Step != want.Step || got.Kind != want.Kind {
		t.Errorf("opt-in capture mismatch: got=%+v want=%+v", got, want)
	}

	// (c) opt-in driver 但本次未 capture — LastRawCapture() 返回 nil 应当透传
	f3 := &LLMFile{driver: &optInDriver{rc: nil}}
	if got := f3.LastRawCapture(); got != nil {
		t.Errorf("opt-in driver returning nil: LLMFile.LastRawCapture() = %+v, want nil", got)
	}

	// (d) LLMFile 必须满足 vfs.RawCaptureProvider 接口（编译期断言）
	var _ vfs.RawCaptureProvider = (*LLMFile)(nil)
}
