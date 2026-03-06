package llm

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/usecrux/crux/vfs"
)

// LLMFile implements vfs.VFSFile for LLM device access via write-then-read semantics.
type LLMFile struct {
	driver     LLMDriver
	devicePath string
	response   []byte
	offset     int
	closed     bool
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

	resp, err := f.driver.Call(ctx, req)
	if err != nil {
		return err
	}

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
func FileFactory(driver LLMDriver, basePath string) vfs.VFSFileFactory {
	return func(subpath string, flags vfs.OpenFlag) (vfs.VFSFile, error) {
		return &LLMFile{
			driver:     driver,
			devicePath: basePath + subpath,
		}, nil
	}
}
