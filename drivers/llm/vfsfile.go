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

// Write accepts a JSON-encoded LLMRequest, invokes the driver, and buffers the response.
func (f *LLMFile) Write(ctx context.Context, data []byte) error {
	if f.closed {
		return fmt.Errorf("write to closed llm file")
	}

	var req LLMRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return fmt.Errorf("failed to parse llm request: %w", err)
	}

	if f.mode == ModeCall {
		return f.writeCall(ctx, req)
	}
	return f.writeStream(ctx, req)
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
