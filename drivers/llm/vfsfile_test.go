package llm

import (
	"context"
	"encoding/json"
	"fmt"
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
	return nil, nil
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
	if err := f.Write(reqJSON); err != nil {
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
		err := f.Write([]byte(`{"intent":"test"}`))
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
	if err := f.Write(reqJSON); err != nil {
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
	err := f.Write(reqJSON)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestFileFactory(t *testing.T) {
	driver := &mockDriver{
		callResp: &LLMResponse{Content: "factory test", TokensUsed: 1},
	}
	factory := FileFactory(driver, "/dev/llm/claude")

	file, err := factory("", 0)
	if err != nil {
		t.Fatalf("FileFactory creation failed: %v", err)
	}

	// Verify it's a working VFSFile
	reqJSON, _ := json.Marshal(LLMRequest{Intent: "test"})
	if err := file.Write(reqJSON); err != nil {
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
