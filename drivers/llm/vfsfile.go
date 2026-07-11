package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strings"

	"github.com/rnixai/rnix/vfs"
)

// LLMFile implements vfs.VFSFile for LLM device access via write-then-read semantics.
type LLMFile struct {
	driver     LLMDriver
	devicePath string
	mode       string // "call" or "" / "stream" (default)
	response   []byte
	offset     int
	closed     bool
	onEvent    func(event map[string]any) // stream event handler (set via SetStreamHandler)
	// lastRawCapture 是 56.2 「裁决 1 并发铁律」 的 per-Open 落点：driver 是
	// 跨进程共享单例，不可在 driver 字段存 per-call 数据；改由 LLMFile 在
	// writeCall/writeStream 调用栈内经 ctx-scoped sink 注入、driver 出口填充、
	// 调用返回后归集到本字段。56.2 dev 接线后填充；56.1/未开通的 driver 留 nil。
	lastRawCapture *vfs.RawCapture
}

// SetStreamHandler sets a callback for intermediate stream events (e.g., tool_call).
// Implements vfs.StreamObserver.
func (f *LLMFile) SetStreamHandler(fn func(event map[string]any)) {
	f.onEvent = fn
}

// SupportsToolCalling reports whether the underlying driver supports native tool calling.
// Implements vfs.ToolCapable.
func (f *LLMFile) SupportsToolCalling() bool {
	_, ok := f.driver.(ToolCallingDriver)
	return ok
}

// DefaultModel returns the driver's configured default model.
// Implements vfs.ModelInfoProvider.
func (f *LLMFile) DefaultModel() string {
	return f.driver.Info().DefaultModel
}

// ReasoningEffort returns the driver's configured reasoning-effort/level string.
// Implements vfs.ReasoningEffortProvider (Story 55.2). Empty for drivers without
// an effort concept (cursor-cli/qwen-cli) or when unset.
func (f *LLMFile) ReasoningEffort() string {
	return f.driver.Info().ReasoningEffort
}

// DriverType returns the underlying driver type (e.g. DriverClaudeCLI).
// Implements vfs.DriverTypeProvider (Story 56.6) — the kernel gates CLI-subagent
// synthesis on this rather than the provider-named device path.
func (f *LLMFile) DriverType() string {
	return f.driver.Info().DriverType
}

// DriverMeta returns runtime metadata from the underlying driver, if it
// implements DriverMetaProvider. Returns nil otherwise.
func (f *LLMFile) DriverMeta() map[string]string {
	if dmp, ok := f.driver.(DriverMetaProvider); ok {
		return dmp.DriverMeta()
	}
	return nil
}

// LastRawCapture returns the most recent raw request/response capture.
// Implements vfs.RawCaptureProvider (Story 56.1).
//
// 56.2 接线后采用 field-first / fallback-委托：
//
//  1. 优先返回 per-Open `f.lastRawCapture`（API driver 经 raw_capture.go 的
//     ctx-scoped sink 在调用出口填充，调用返回后归集到此字段——「裁决 1 并发
//     铁律」：跨进程共享 driver 不可存 per-call 数据）。
//  2. 字段为 nil 时 fallback 委托 `rawCaptureDriver`——保 56.1 委托语义
//     与 INT-001 全绿不破：56.1/未开通的 driver 走原路径返回 nil，opt-in
//     test 的 fake driver 仍能通过反射 verify 委托正确性。
func (f *LLMFile) LastRawCapture() *vfs.RawCapture {
	if f.lastRawCapture != nil {
		return f.lastRawCapture
	}
	if rcd, ok := f.driver.(rawCaptureDriver); ok {
		return rcd.LastRawCapture()
	}
	return nil
}

// rawCaptureDriver is the drivers/llm package-internal counterpart to
// vfs.RawCaptureProvider (Story 56.1). LLMFile.LastRawCapture() type-asserts
// against this interface; drivers that opt into raw capture (56.2/56.3)
// implement it, all others return nil for free.
type rawCaptureDriver interface {
	LastRawCapture() *vfs.RawCapture
}

// Write accepts a JSON-encoded LLMRequest, invokes the driver, and buffers the response.
func (f *LLMFile) Write(ctx context.Context, data []byte) error {
	if f.closed {
		return fmt.Errorf("write to closed llm file")
	}
	// Per-open raw capture is per Write attempt. Clear any previous attempt
	// before parsing/dispatch so a startup or no-capture failure cannot be
	// persisted as the current call by the kernel failure hook.
	f.lastRawCapture = nil

	var req LLMRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return fmt.Errorf("failed to parse llm request: %w", err)
	}

	f.maybeMergeSkillsIntoPrompt(&req)

	if f.mode == ModeCall {
		return f.writeCall(ctx, req)
	}
	return f.writeStream(ctx, req)
}

// maybeMergeSkillsIntoPrompt lets non-bundle drivers still receive skill
// content by appending each skill's body onto req.SystemPrompt. Drivers that
// implement SkillsBundleCapable (e.g. claude-cli) consume req.Skills directly
// and this function is a no-op for them.
//
// After merging, req.Skills is cleared so the driver sees a single source of
// truth and doesn't accidentally double-process the content.
func (f *LLMFile) maybeMergeSkillsIntoPrompt(req *LLMRequest) {
	if len(req.Skills) == 0 {
		return
	}
	if _, ok := f.driver.(SkillsBundleCapable); ok {
		return
	}
	var b strings.Builder
	if req.SystemPrompt != "" {
		b.WriteString(req.SystemPrompt)
		b.WriteString("\n\n")
	}
	b.WriteString("# Loaded Skills\n")
	for _, s := range req.Skills {
		fmt.Fprintf(&b, "\n## %s\n", s.Name)
		if s.Dir != "" {
			fmt.Fprintf(&b, "Base directory for this skill: %s\n\n", s.Dir)
		}
		b.WriteString(s.Body)
		b.WriteString("\n")
	}
	req.SystemPrompt = b.String()
	req.Skills = nil
}

// writeCall uses the synchronous Call API.
func (f *LLMFile) writeCall(ctx context.Context, req LLMRequest) error {
	// 56.2 裁决 1：在调用栈内挂 ctx-scoped sink；driver 出口（手写 HTTP /
	// SDK middleware / RoundTripper）经 rawSinkFromContext 取出填充。
	// 共享 driver 函数体本身保持无状态——sink 是 per-call 容器。
	sink := &rawCaptureSink{}
	ctx = withRawSink(ctx, sink)

	var resp *LLMResponse
	var err error

	if len(req.Tools) > 0 {
		if tcd, ok := f.driver.(ToolCallingDriver); ok {
			resp, err = tcd.CallWithTools(ctx, req, req.Tools)
		} else {
			resp, err = f.driver.Call(ctx, req)
		}
	} else {
		resp, err = f.driver.Call(ctx, req)
	}

	// 调用返回后归集 sink → per-Open 字段（即便 err != nil 也保留 Request
	// 形态，便于审计排错；err 路径下 Response 可能缺失，那就只有 Request）。
	if c := sink.get(); c != nil {
		f.lastRawCapture = c
	}

	if err != nil {
		return err
	}
	return f.bufferResponse(resp)
}

// writeStream uses the streaming API, accumulating content and forwarding intermediate events.
func (f *LLMFile) writeStream(ctx context.Context, req LLMRequest) error {
	// 56.2 裁决 1：sink 经 ctx 下传给 driver；driver goroutine 在 stream
	// 全部读完（resp.Body Close 时刻）通过 captureResponseBody.Close 写入
	// sink.cap.Response。LLMFile 在「range ch 结束」之后归集——channel
	// close 与 Body Close 都 happens-before 那一点，sink 已为终态。
	sink := &rawCaptureSink{}
	ctx = withRawSink(ctx, sink)

	var ch <-chan StreamEvent
	var err error

	if len(req.Tools) > 0 {
		if tcd, ok := f.driver.(ToolCallingDriver); ok {
			ch, err = tcd.StreamWithTools(ctx, req, req.Tools)
		} else {
			ch, err = f.driver.Stream(ctx, req)
		}
	} else {
		ch, err = f.driver.Stream(ctx, req)
	}
	if err != nil {
		// Stream 启动失败也尽量保留 sink 中已落入的 Request 形态。
		if c := sink.get(); c != nil {
			f.lastRawCapture = c
		}
		return err
	}

	var content strings.Builder
	var reasoning strings.Builder
	var tokens, inputTokens, outputTokens, cachedInputTokens int
	var costUSD float64
	var stopReason string
	var toolCalls []ToolCall
	var reasoningBlocks []ReasoningBlock
	var receivedDone bool
	var streamErr error

	for evt := range ch {
		// 56.7 裁决 1（G1）：error 事件不再立即 return——CLI driver 的 sink.set
		// 发生在 error 事件之后、close(ch) 之前（56.3 Stream 时序铁律），提前
		// return 会拿到过早态 sink。改为记住首个 error，继续 drain 到 channel
		// close 再归集。drain 不会永久阻塞：driver 后续 send 均有 ctx.Done
		// 兜底，close(ch) 由 driver goroutine 的 defer 保证。错误已定局，
		// drain 期间忽略后续 content/done 事件（不回填 response buffer）。
		if streamErr != nil {
			continue
		}
		switch evt.Type {
		case "content":
			content.WriteString(evt.Content)
			// 调查 codex-cli-observability-parity R3（apex-pid517 同症根修）：
			// content 也转发 onEvent，使 kernel 能在长内容流期间刷新
			// heartbeat——此前纯 content 阶段（codex agent_message、openai
			// 长文本生成）handler 全程静默，HeartbeatMonitor 误报 STALL。
			// API driver 的 token 级 delta 高频，kernel 侧仅 touch 不落盘；
			// CLI driver 的消息级 content（Data.subtype=agent_message）会被
			// 记录到 events.jsonl。
			if f.onEvent != nil {
				evtData := map[string]any{}
				maps.Copy(evtData, evt.Data)
				evtData["type"] = "content"
				if evt.Content != "" {
					evtData["content"] = evt.Content
				}
				f.onEvent(evtData)
			}
		case "reasoning":
			reasoning.WriteString(evt.Content)
			// Story 60.1 AC1: 把 reasoning 增量也转发到 onEvent,归一为
			// "thinking" 事件类型,使 API driver(anthropic/gemini/openai-compat)
			// 的思考阶段成为实时可观测事件——与 CLI driver 的 "thinking" 路径对齐,
			// 下游 driverEventToLog "thinking"→LogThink 分支零改动即命中。
			// 这是额外旁路: 上面的 reasoning.WriteString 仍独立驱动
			// LLMResponse.Reasoning 累积,落盘/round-trip(AC4)完全不变。
			if f.onEvent != nil {
				// 先合并 driver 元数据,再赋权威归一字段——确保 type:"thinking"
				// 与 content 不被 evt.Data 的同名键覆盖(Code Review #P2 防御:
				// 未来 driver 若给 reasoning 附 Data["type"] 会静默破坏归一,
				// 致下游 observe.go 的 thinking 判断 miss、OnThinking 不触发)。
				evtData := map[string]any{}
				maps.Copy(evtData, evt.Data)
				evtData["type"] = "thinking"
				if evt.Content != "" {
					evtData["content"] = evt.Content
				}
				f.onEvent(evtData)
			}
		case "tool_call", "thinking", "system", "user", "assistant", "item":
			if f.onEvent != nil {
				evtData := map[string]any{
					"type": evt.Type,
				}
				if evt.Content != "" {
					evtData["content"] = evt.Content
				}
				// Merge driver-specific metadata (tool name, description, subtype, etc.)
				maps.Copy(evtData, evt.Data)
				f.onEvent(evtData)
			}
		case "usage":
			// Story 66.6: forward the mid-stream usage delta to the kernel so ps /
			// proc-info can preview token growth during a long CLI session. This
			// deliberately does NOT touch the `content`/`tokens` accumulators — the
			// `done` event remains the sole authoritative source (AC4), and the
			// kernel discards these deltas at the step boundary.
			if f.onEvent != nil {
				evtData := map[string]any{
					"type":                "usage",
					"tokens_used":         evt.TokensUsed,
					"input_tokens":        evt.InputTokens,
					"output_tokens":       evt.OutputTokens,
					"cached_input_tokens": evt.CachedInputTokens,
				}
				maps.Copy(evtData, evt.Data)
				f.onEvent(evtData)
			}
		case "done":
			receivedDone = true
			// Use result content if available (CLI drivers put final result here)
			if evt.Content != "" {
				content.Reset()
				content.WriteString(evt.Content)
			}
			tokens = evt.TokensUsed
			inputTokens = evt.InputTokens
			outputTokens = evt.OutputTokens
			cachedInputTokens = evt.CachedInputTokens
			costUSD = evt.CostUSD
			stopReason = evt.StopReason
			// Collect ToolCalls from done event (OpenAI stream flushes them here)
			if len(evt.ToolCalls) > 0 {
				toolCalls = evt.ToolCalls
			}
			// Collect ReasoningBlocks from done event so thinking-mode
			// signatures survive the stream → LLMResponse → context
			// round-trip (review finding H1).
			if len(evt.ReasoningBlocks) > 0 {
				reasoningBlocks = evt.ReasoningBlocks
			}
		case "error":
			if evt.Err != nil {
				streamErr = evt.Err
			} else {
				streamErr = fmt.Errorf("stream error: %s", evt.Content)
			}
		}
	}

	// 56.7 裁决 1（G1/G2）：sink 归集前移——失败路径（error 事件 /
	// ErrStreamIncomplete）也归集到 per-Open 字段，对齐 writeCall 的「即便
	// err != nil 也保留 Request 形态」语义。range ch 已结束 = close(ch) 已
	// 发生，happens-after driver 的最后一次 sink.set，get() 是终态。
	collected := sink.get()
	if collected != nil {
		f.lastRawCapture = collected
	}

	// Story 66.2 (code-review P1): a process/step-level cancel dominates any
	// streamErr that is merely the cancellation surfacing as a driver "error"
	// event. On a kill, drivers race `ch <- StreamEvent{Type:"error"}` against
	// `<-ctx.Done()` (claude_cli.go / anthropic.go), so streamErr may or may
	// not be set — without this precedence the partial content would be
	// discarded ~half the time. Return the accumulated partial (text, or
	// reasoning when no visible text arrived) so the kernel can tag [partial].
	// Must precede the streamErr return.
	if ctx.Err() != nil && !receivedDone {
		partial := content.String()
		if partial == "" {
			partial = reasoning.String()
		}
		return &StreamInterruptedError{Partial: partial}
	}

	if streamErr != nil {
		return streamErr
	}

	finalContent := content.String()
	if finalContent == "" && reasoning.Len() > 0 {
		finalContent = reasoning.String()
	}

	// Guard: stream closed without a "done" event and no usable content.
	// 56.7 (G7): append stderr tail + exit code from the collected capture
	// (when present) so a silent CLI break is no longer undiagnosable.
	if !receivedDone && finalContent == "" && len(toolCalls) == 0 {
		return fmt.Errorf("stream closed without result event%s: %w", streamIncompleteDiag(collected), ErrStreamIncomplete)
	}

	resp := &LLMResponse{
		Content:           finalContent,
		Reasoning:         reasoning.String(),
		ReasoningBlocks:   reasoningBlocks,
		TokensUsed:        tokens,
		InputTokens:       inputTokens,
		OutputTokens:      outputTokens,
		CachedInputTokens: cachedInputTokens,
		CostUSD:           costUSD,
		StopReason:        stopReason,
		ToolCalls:         toolCalls,
	}
	return f.bufferResponse(resp)
}

// bufferResponse serializes and stores the response for subsequent Read calls.
func (f *LLMFile) bufferResponse(resp *LLMResponse) error {
	respJSON, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("failed to serialize llm response: %w", err)
	}
	f.response = respJSON
	f.offset = 0
	return nil
}

// Read returns buffered response data up to the requested length.
func (f *LLMFile) Read(length int) ([]byte, error) {
	if f.closed {
		return nil, fmt.Errorf("read from closed llm file")
	}
	if f.response == nil {
		return nil, fmt.Errorf("no response available: write a request first")
	}

	remaining := f.response[f.offset:]
	if len(remaining) == 0 {
		return nil, nil
	}

	if length <= 0 || length > len(remaining) {
		length = len(remaining)
	}

	data := make([]byte, length)
	copy(data, remaining[:length])
	f.offset += length
	return data, nil
}

// Close marks the file as closed and releases buffers.
func (f *LLMFile) Close() error {
	if f.closed {
		return fmt.Errorf("llm file already closed")
	}
	f.closed = true
	f.response = nil
	f.offset = 0
	return nil
}

// Stat returns metadata about this LLM device file.
func (f *LLMFile) Stat() (vfs.FileStat, error) {
	if f.closed {
		return vfs.FileStat{}, fmt.Errorf("stat on closed llm file")
	}
	return vfs.FileStat{
		Name:       f.devicePath,
		IsDevice:   true,
		DevicePath: f.devicePath,
	}, nil
}

// FileFactory returns a VFSFileFactory that creates LLMFile instances for the given driver.
// basePath is the device mount path (e.g., "/dev/llm/claude").
// mode is "call" or "" / "stream" (default).
func FileFactory(driver LLMDriver, basePath string, mode string) vfs.VFSFileFactory {
	return func(subpath string, flags vfs.OpenFlag, workDir string) (vfs.VFSFile, error) {
		return &LLMFile{
			driver:     driver,
			devicePath: basePath + subpath,
			mode:       mode,
		}, nil
	}
}
