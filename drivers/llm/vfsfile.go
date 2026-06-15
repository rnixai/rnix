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

	if err != nil {
		return err
	}
	return f.bufferResponse(resp)
}

// writeStream uses the streaming API, accumulating content and forwarding intermediate events.
func (f *LLMFile) writeStream(ctx context.Context, req LLMRequest) error {
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

	for evt := range ch {
		switch evt.Type {
		case "content":
			content.WriteString(evt.Content)
		case "reasoning":
			reasoning.WriteString(evt.Content)
		case "tool_call", "thinking", "system", "user", "assistant":
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
				return evt.Err
			}
			return fmt.Errorf("stream error: %s", evt.Content)
		}
	}

	finalContent := content.String()
	if finalContent == "" && reasoning.Len() > 0 {
		finalContent = reasoning.String()
	}

	// Guard: stream closed without a "done" event and no usable content
	if !receivedDone && finalContent == "" && len(toolCalls) == 0 {
		return fmt.Errorf("stream closed without result event: %w", ErrStreamIncomplete)
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
