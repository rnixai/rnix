package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// mockDriver implements LLMDriver for testing VFSFile.
type mockDriver struct {
	callResp *LLMResponse
	callErr  error
}

func (m *mockDriver) Call(_ context.Context, _ LLMRequest) (*LLMResponse, error) {
	return m.callResp, m.callErr
}

func (m *mockDriver) Stream(_ context.Context, _ LLMRequest) (<-chan StreamEvent, error) {
	// Convert Call result to a stream for testing
	ch := make(chan StreamEvent, 1)
	if m.callErr != nil {
		ch <- StreamEvent{Type: "error", Err: m.callErr}
	} else if m.callResp != nil {
		ch <- StreamEvent{
			Type:         "done",
			Content:      m.callResp.Content,
			TokensUsed:   m.callResp.TokensUsed,
			InputTokens:  m.callResp.InputTokens,
			OutputTokens: m.callResp.OutputTokens,
		}
	}
	close(ch)
	return ch, nil
}

func (m *mockDriver) Info() DriverInfo {
	return DriverInfo{Name: "mock", Provider: "test", DefaultModel: "test-model"}
}

func TestLLMFile_WriteRead(t *testing.T) {
	driver := &mockDriver{
		callResp: &LLMResponse{Content: "hello world", TokensUsed: 5},
	}
	f := &LLMFile{driver: driver, devicePath: "/dev/llm/claude"}

	reqJSON, _ := json.Marshal(LLMRequest{Intent: "test"})
	if err := f.Write(context.Background(), reqJSON); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	data, err := f.Read(0)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	var resp LLMResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp.Content != "hello world" {
		t.Errorf("expected content 'hello world', got %q", resp.Content)
	}
	if resp.TokensUsed != 5 {
		t.Errorf("expected tokens_used 5, got %d", resp.TokensUsed)
	}
}

func TestLLMFile_ReadBeforeWrite(t *testing.T) {
	driver := &mockDriver{}
	f := &LLMFile{driver: driver, devicePath: "/dev/llm/claude"}

	_, err := f.Read(100)
	if err == nil {
		t.Fatal("expected error on read before write, got nil")
	}
}

func TestLLMFile_ClosedAccess(t *testing.T) {
	driver := &mockDriver{
		callResp: &LLMResponse{Content: "test", TokensUsed: 1},
	}
	f := &LLMFile{driver: driver, devicePath: "/dev/llm/claude"}

	if err := f.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	t.Run("WriteAfterClose", func(t *testing.T) {
		err := f.Write(context.Background(), []byte(`{"intent":"test"}`))
		if err == nil {
			t.Error("expected error on write after close")
		}
	})

	t.Run("ReadAfterClose", func(t *testing.T) {
		_, err := f.Read(100)
		if err == nil {
			t.Error("expected error on read after close")
		}
	})

	t.Run("DoubleClose", func(t *testing.T) {
		err := f.Close()
		if err == nil {
			t.Error("expected error on double close")
		}
	})
}

func TestLLMFile_Stat(t *testing.T) {
	driver := &mockDriver{}
	f := &LLMFile{driver: driver, devicePath: "/dev/llm/claude"}

	stat, err := f.Stat()
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if stat.Name != "/dev/llm/claude" {
		t.Errorf("expected name '/dev/llm/claude', got %q", stat.Name)
	}
	if !stat.IsDevice {
		t.Error("expected IsDevice=true")
	}
	if stat.DevicePath != "/dev/llm/claude" {
		t.Errorf("expected device path '/dev/llm/claude', got %q", stat.DevicePath)
	}
}

func TestLLMFile_ReadPartial(t *testing.T) {
	driver := &mockDriver{
		callResp: &LLMResponse{Content: "hello", TokensUsed: 1},
	}
	f := &LLMFile{driver: driver, devicePath: "/dev/llm/claude"}

	reqJSON, _ := json.Marshal(LLMRequest{Intent: "test"})
	if err := f.Write(context.Background(), reqJSON); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Read partial
	data, err := f.Read(5)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if len(data) != 5 {
		t.Errorf("expected 5 bytes, got %d", len(data))
	}

	// Read remaining
	data2, err := f.Read(0)
	if err != nil {
		t.Fatalf("second Read failed: %v", err)
	}

	// Combine and verify
	full := append(data, data2...)
	var resp LLMResponse
	if err := json.Unmarshal(full, &resp); err != nil {
		t.Fatalf("failed to parse combined response: %v", err)
	}
	if resp.Content != "hello" {
		t.Errorf("expected content 'hello', got %q", resp.Content)
	}
}

func TestLLMFile_WriteDriverError(t *testing.T) {
	driver := &mockDriver{
		callErr: fmt.Errorf("driver error"),
	}
	f := &LLMFile{driver: driver, devicePath: "/dev/llm/claude"}

	reqJSON, _ := json.Marshal(LLMRequest{Intent: "test"})
	err := f.Write(context.Background(), reqJSON)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestFileFactory(t *testing.T) {
	driver := &mockDriver{
		callResp: &LLMResponse{Content: "factory test", TokensUsed: 1},
	}
	factory := FileFactory(driver, "/dev/llm/claude", "")

	file, err := factory("", 0, "")
	if err != nil {
		t.Fatalf("FileFactory creation failed: %v", err)
	}

	// Verify it's a working VFSFile
	reqJSON, _ := json.Marshal(LLMRequest{Intent: "test"})
	if err := file.Write(context.Background(), reqJSON); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	data, err := file.Read(0)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	var resp LLMResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp.Content != "factory test" {
		t.Errorf("expected content 'factory test', got %q", resp.Content)
	}
}

// contextDriver captures the context passed to Call for verification.
type contextDriver struct {
	callCtx context.Context
}

func (d *contextDriver) Call(ctx context.Context, _ LLMRequest) (*LLMResponse, error) {
	d.callCtx = ctx
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		return &LLMResponse{Content: "ok", TokensUsed: 1}, nil
	}
}

func (d *contextDriver) Stream(ctx context.Context, req LLMRequest) (<-chan StreamEvent, error) {
	resp, err := d.Call(ctx, req)
	ch := make(chan StreamEvent, 1)
	if err != nil {
		ch <- StreamEvent{Type: "error", Err: err}
	} else {
		ch <- StreamEvent{Type: "done", Content: resp.Content, TokensUsed: resp.TokensUsed}
	}
	close(ch)
	return ch, nil
}

func (d *contextDriver) Info() DriverInfo {
	return DriverInfo{Name: "ctx-test", Provider: "test", DefaultModel: "test"}
}

func TestLLMFile_Write_ContextCancelled(t *testing.T) {
	driver := &contextDriver{}
	f := &LLMFile{driver: driver, devicePath: "/dev/llm/claude"}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	reqJSON, _ := json.Marshal(LLMRequest{Intent: "test"})
	err := f.Write(ctx, reqJSON)
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled error, got: %v", err)
	}
}

func TestLLMFile_Write_ContextPropagated(t *testing.T) {
	driver := &contextDriver{}
	f := &LLMFile{driver: driver, devicePath: "/dev/llm/claude"}

	ctx := context.WithValue(context.Background(), contextKey("test"), "value")
	reqJSON, _ := json.Marshal(LLMRequest{Intent: "test"})
	err := f.Write(ctx, reqJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if driver.callCtx != ctx {
		t.Error("expected driver.Call to receive the same context passed to Write")
	}
}

type contextKey string

// streamMockDriver lets tests specify exactly which stream events to send.
type streamMockDriver struct {
	events []StreamEvent
}

func (m *streamMockDriver) Call(_ context.Context, _ LLMRequest) (*LLMResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *streamMockDriver) Stream(_ context.Context, _ LLMRequest) (<-chan StreamEvent, error) {
	ch := make(chan StreamEvent, len(m.events))
	for _, evt := range m.events {
		ch <- evt
	}
	close(ch)
	return ch, nil
}

func (m *streamMockDriver) Info() DriverInfo {
	return DriverInfo{Name: "stream-mock", Provider: "test"}
}

func TestWriteStream_NoResultEvent(t *testing.T) {
	// Simulate stream that sends system+user but no done event (Cursor CLI silent exit)
	driver := &streamMockDriver{
		events: []StreamEvent{
			{Type: "system", Content: "init"},
			{Type: "user"},
		},
	}
	f := &LLMFile{driver: driver, devicePath: "/dev/llm/cursor"}

	reqJSON, _ := json.Marshal(LLMRequest{Intent: "test"})
	err := f.Write(context.Background(), reqJSON)
	if err == nil {
		t.Fatal("expected ErrStreamIncomplete, got nil")
	}
	if !errors.Is(err, ErrStreamIncomplete) {
		t.Errorf("expected error wrapping ErrStreamIncomplete, got: %v", err)
	}
}

func TestWriteStream_EmptyDoneEvent(t *testing.T) {
	// Stream sends done event but with empty result — should still succeed
	// (the empty content is a valid response; reasonStep handles it)
	driver := &streamMockDriver{
		events: []StreamEvent{
			{Type: "system", Content: "init"},
			{Type: "done", Content: ""},
		},
	}
	f := &LLMFile{driver: driver, devicePath: "/dev/llm/cursor"}

	reqJSON, _ := json.Marshal(LLMRequest{Intent: "test"})
	err := f.Write(context.Background(), reqJSON)
	if err != nil {
		t.Fatalf("expected nil error for empty done event, got: %v", err)
	}
}

func TestWriteStream_ContentWithoutDone(t *testing.T) {
	// Stream sends content events but no done — should NOT error
	// because accumulated content is usable
	driver := &streamMockDriver{
		events: []StreamEvent{
			{Type: "content", Content: "partial result"},
		},
	}
	f := &LLMFile{driver: driver, devicePath: "/dev/llm/test"}

	reqJSON, _ := json.Marshal(LLMRequest{Intent: "test"})
	err := f.Write(context.Background(), reqJSON)
	if err != nil {
		t.Fatalf("expected nil error when content exists without done, got: %v", err)
	}

	data, _ := f.Read(0)
	var resp LLMResponse
	_ = json.Unmarshal(data, &resp)
	if resp.Content != "partial result" {
		t.Errorf("expected content 'partial result', got %q", resp.Content)
	}
}

func TestIsTransient_StreamIncomplete(t *testing.T) {
	if !IsTransient(ErrStreamIncomplete) {
		t.Error("ErrStreamIncomplete should be classified as transient")
	}
	// Wrapped version
	wrapped := fmt.Errorf("stream closed: %w", ErrStreamIncomplete)
	if !IsTransient(wrapped) {
		t.Error("wrapped ErrStreamIncomplete should be classified as transient")
	}
}

// mockBundleDriver is a mockDriver that also implements SkillsBundleCapable.
type mockBundleDriver struct{ mockDriver }

func (m *mockBundleDriver) UsesSkillsBundle() {}

// TestLLMFile_MergeSkills_NonBundleDriver verifies R5 fallback: when the
// driver does NOT implement SkillsBundleCapable, skill bodies are merged into
// req.SystemPrompt before invoking the driver, and req.Skills is cleared.
func TestLLMFile_MergeSkills_NonBundleDriver(t *testing.T) {
	f := &LLMFile{driver: &mockDriver{}, devicePath: "/dev/llm/test"}

	req := LLMRequest{
		SystemPrompt: "base system prompt",
		Skills: []Skill{
			{Name: "s1", Body: "body-one", Dir: "/tmp/s1"},
			{Name: "s2", Body: "body-two"},
		},
	}
	f.maybeMergeSkillsIntoPrompt(&req)

	if len(req.Skills) != 0 {
		t.Errorf("expected Skills cleared after merge, got %d entries", len(req.Skills))
	}
	if !strings.Contains(req.SystemPrompt, "base system prompt") {
		t.Errorf("lost original SystemPrompt: %q", req.SystemPrompt)
	}
	if !strings.Contains(req.SystemPrompt, "## s1") || !strings.Contains(req.SystemPrompt, "body-one") {
		t.Errorf("s1 body not merged: %q", req.SystemPrompt)
	}
	if !strings.Contains(req.SystemPrompt, "Base directory for this skill: /tmp/s1") {
		t.Errorf("s1 dir hint not merged: %q", req.SystemPrompt)
	}
	if !strings.Contains(req.SystemPrompt, "## s2") || !strings.Contains(req.SystemPrompt, "body-two") {
		t.Errorf("s2 body not merged: %q", req.SystemPrompt)
	}
}

// TestLLMFile_MergeSkills_BundleDriver verifies R5 fallback is skipped when
// the driver implements SkillsBundleCapable — the driver will consume Skills
// directly via its own bundle mechanism.
func TestLLMFile_MergeSkills_BundleDriver(t *testing.T) {
	f := &LLMFile{driver: &mockBundleDriver{}, devicePath: "/dev/llm/bundle"}

	req := LLMRequest{
		SystemPrompt: "base",
		Skills:       []Skill{{Name: "s1", Body: "body-one", Dir: "/tmp/s1"}},
	}
	f.maybeMergeSkillsIntoPrompt(&req)

	if len(req.Skills) != 1 {
		t.Errorf("expected Skills preserved for bundle driver, got %d entries", len(req.Skills))
	}
	if req.SystemPrompt != "base" {
		t.Errorf("SystemPrompt should not change, got %q", req.SystemPrompt)
	}
}

// TestLLMFile_MergeSkills_Empty ensures merge is a no-op when no skills.
func TestLLMFile_MergeSkills_Empty(t *testing.T) {
	f := &LLMFile{driver: &mockDriver{}, devicePath: "/dev/llm/test"}
	req := LLMRequest{SystemPrompt: "sp"}
	f.maybeMergeSkillsIntoPrompt(&req)
	if req.SystemPrompt != "sp" {
		t.Errorf("expected unchanged SystemPrompt, got %q", req.SystemPrompt)
	}
}

