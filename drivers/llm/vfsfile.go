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
	resp, err := f.driver.Call(ctx, req)
	if err != nil {
		return err
	}
	return f.bufferResponse(resp)
}

// writeStream uses the streaming API, accumulating content and forwarding intermediate events.
func (f *LLMFile) writeStream(ctx context.Context, req LLMRequest) error {
	ch, err := f.driver.Stream(ctx, req)
	if err != nil {
		return err
	}

	var content strings.Builder
	var reasoning strings.Builder
	var tokens, inputTokens, outputTokens int

	for evt := range ch {
		switch evt.Type {
		case "content":
			content.WriteString(evt.Content)
		case "reasoning":
			reasoning.WriteString(evt.Content)
		case "tool_call":
			if f.onEvent != nil {
				evtData := map[string]any{
					"type":    "tool_call",
					"subtype": evt.Content,
				}
				// Merge driver-specific metadata (tool name, description, command, etc.)
				maps.Copy(evtData, evt.Data)
				f.onEvent(evtData)
			}
		case "done":
			// Use result content if available (CLI drivers put final result here)
			if evt.Content != "" {
				content.Reset()
				content.WriteString(evt.Content)
			}
			tokens = evt.TokensUsed
			inputTokens = evt.InputTokens
			outputTokens = evt.OutputTokens
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

	resp := &LLMResponse{
		Content:      finalContent,
		Reasoning:    reasoning.String(),
		TokensUsed:   tokens,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
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
