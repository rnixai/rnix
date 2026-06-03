package intentdriver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/rnixai/rnix/vfs"
)

// mockLLMFile simulates a /dev/llm/* VFS device for testing VFSCaller.
type mockLLMFile struct {
	responseJSON []byte
	writeErr     error
	readErr      error
	offset       int
	closed       bool
}

func (f *mockLLMFile) Write(_ context.Context, data []byte) error {
	if f.writeErr != nil {
		return f.writeErr
	}
	var req llmRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return fmt.Errorf("mock: invalid request: %w", err)
	}
	return nil
}

func (f *mockLLMFile) Read(length int) ([]byte, error) {
	if f.readErr != nil {
		return nil, f.readErr
	}
	remaining := f.responseJSON[f.offset:]
	if len(remaining) == 0 {
		return nil, nil
	}
	if length <= 0 || length > len(remaining) {
		length = len(remaining)
	}
	out := make([]byte, length)
	copy(out, remaining[:length])
	f.offset += length
	return out, nil
}

func (f *mockLLMFile) Close() error {
	f.closed = true
	return nil
}

func (f *mockLLMFile) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{Name: "/dev/llm/mock", IsDevice: true, DevicePath: "/dev/llm/mock"}, nil
}

func newMockDeviceRegistry(provider string, file *mockLLMFile) *vfs.DeviceRegistry {
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/"+provider, func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return file, nil
	})
	return reg
}

func TestVFSCaller_Call_Success(t *testing.T) {
	resp := llmResponse{Content: `[{"id":"a","intent":"task a","depends_on":[]}]`}
	respJSON, _ := json.Marshal(resp)
	mock := &mockLLMFile{responseJSON: respJSON}
	reg := newMockDeviceRegistry("test", mock)

	caller := NewVFSCaller(reg, "test")
	result, err := caller.Call(context.Background(), "decompose this", "claude-3", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != resp.Content {
		t.Errorf("got %q, want %q", result, resp.Content)
	}
	if !mock.closed {
		t.Error("expected file to be closed")
	}
}

func TestVFSCaller_Call_DeviceNotFound(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	caller := NewVFSCaller(reg, "nonexistent")

	_, err := caller.Call(context.Background(), "test", "", "")
	if err == nil {
		t.Fatal("expected error for missing device")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Errorf("error should mention provider name, got: %v", err)
	}
}

func TestVFSCaller_Call_WriteError(t *testing.T) {
	mock := &mockLLMFile{writeErr: fmt.Errorf("connection refused")}
	reg := newMockDeviceRegistry("failing", mock)

	caller := NewVFSCaller(reg, "failing")
	_, err := caller.Call(context.Background(), "test", "", "")
	if err == nil {
		t.Fatal("expected error on write failure")
	}
	if !strings.Contains(err.Error(), "failing") {
		t.Errorf("error should mention provider name, got: %v", err)
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("error should wrap underlying error, got: %v", err)
	}
}

func TestVFSCaller_Call_InvalidResponse(t *testing.T) {
	mock := &mockLLMFile{responseJSON: []byte("not valid json")}
	reg := newMockDeviceRegistry("broken", mock)

	caller := NewVFSCaller(reg, "broken")
	_, err := caller.Call(context.Background(), "test", "", "")
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
	if !strings.Contains(err.Error(), "invalid response JSON") {
		t.Errorf("expected 'invalid response JSON' in error, got: %v", err)
	}
}

func TestVFSCaller_Call_EmptyModel(t *testing.T) {
	resp := llmResponse{Content: "result"}
	respJSON, _ := json.Marshal(resp)
	mock := &mockLLMFile{responseJSON: respJSON}
	reg := newMockDeviceRegistry("test", mock)

	caller := NewVFSCaller(reg, "test")
	result, err := caller.Call(context.Background(), "prompt", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "result" {
		t.Errorf("got %q, want %q", result, "result")
	}
}

func TestVFSCaller_Call_ReadError(t *testing.T) {
	mock := &mockLLMFile{readErr: fmt.Errorf("device read failure")}
	reg := newMockDeviceRegistry("broken-read", mock)

	caller := NewVFSCaller(reg, "broken-read")
	_, err := caller.Call(context.Background(), "test", "", "")
	if err == nil {
		t.Fatal("expected error on read failure")
	}
	if !strings.Contains(err.Error(), "read response") {
		t.Errorf("expected 'read response' in error, got: %v", err)
	}
}

func TestVFSCaller_Call_ProviderOverride(t *testing.T) {
	resp := llmResponse{Content: "override result"}
	respJSON, _ := json.Marshal(resp)
	overrideMock := &mockLLMFile{responseJSON: respJSON}

	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/default-prov", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		t.Fatal("should not open the default provider device")
		return nil, nil
	})
	_ = reg.Register("/dev/llm/override-prov", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return overrideMock, nil
	})

	caller := NewVFSCaller(reg, "default-prov")
	result, err := caller.Call(context.Background(), "test", "", "override-prov")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "override result" {
		t.Errorf("got %q, want %q", result, "override result")
	}
}

func TestVFSCaller_Call_EmptyResponse(t *testing.T) {
	mock := &mockLLMFile{responseJSON: []byte{}}
	reg := newMockDeviceRegistry("empty", mock)

	caller := NewVFSCaller(reg, "empty")
	_, err := caller.Call(context.Background(), "test", "", "")
	if err == nil {
		t.Fatal("expected error for empty response")
	}
	if !strings.Contains(err.Error(), "empty response") {
		t.Errorf("expected 'empty response' in error, got: %v", err)
	}
}
