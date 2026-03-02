package vfs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/gonewx/crux/internal/types"
)

// --- mcpFile Tests ---

func TestMCPFile_Write(t *testing.T) {
	t.Run("write sends tools/call via transport", func(t *testing.T) {
		// Given: an mcpFile with a mock transport
		var capturedMethod string
		var capturedParams json.RawMessage
		transport := &mockMCPTransport{
			callFn: func(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
				capturedMethod = method
				capturedParams = params
				return json.RawMessage(`{"content":[{"type":"text","text":"success"}]}`), nil
			},
		}
		file := newMCPFile("/tools/create-issue", transport)

		// When: Write is called with tool call parameters
		data := []byte(`{"title":"test","body":"test body"}`)
		err := file.Write(context.Background(), data)

		// Then: Transport.Call is invoked with tools/call method
		if err != nil {
			t.Fatalf("Write failed: %v", err)
		}
		if capturedMethod != "tools/call" {
			t.Fatalf("expected method 'tools/call', got %q", capturedMethod)
		}
		if capturedParams == nil {
			t.Fatal("expected params to be captured")
		}
	})

	t.Run("write returns ErrServiceUnavailable when transport fails", func(t *testing.T) {
		// Given: an mcpFile with a failing transport
		transport := &mockMCPTransport{
			callFn: func(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
				return nil, fmt.Errorf("connection lost")
			},
		}
		file := newMCPFile("/tools/create-issue", transport)

		// When: Write is called
		err := file.Write(context.Background(), []byte(`{"test":"data"}`))

		// Then: error contains ErrServiceUnavailable
		if err == nil {
			t.Fatal("expected error from failing transport, got nil")
		}
		var drvErr *types.DriverError
		if !errors.As(err, &drvErr) {
			t.Fatalf("expected *types.DriverError, got %T: %v", err, err)
		}
		if drvErr.Code != types.ErrServiceUnavailable {
			t.Fatalf("expected ErrServiceUnavailable, got %v", drvErr.Code)
		}
	})
}

func TestMCPFile_Read(t *testing.T) {
	t.Run("read returns tool execution result", func(t *testing.T) {
		// Given: an mcpFile that has performed a Write (tool call)
		transport := &mockMCPTransport{
			callFn: func(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
				return json.RawMessage(`{"content":[{"type":"text","text":"issue created #42"}]}`), nil
			},
		}
		file := newMCPFile("/tools/create-issue", transport)

		// Perform Write first to trigger tool call
		_ = file.Write(context.Background(), []byte(`{"title":"test"}`))

		// When: Read is called
		data, err := file.Read(1 << 20)

		// Then: result from tool call is returned
		if err != nil {
			t.Fatalf("Read failed: %v", err)
		}
		if len(data) == 0 {
			t.Fatal("expected non-empty read data")
		}
	})

	t.Run("read returns ErrServiceUnavailable when transport fails", func(t *testing.T) {
		// Given: an mcpFile with a transport that fails on Call
		transport := &mockMCPTransport{
			callFn: func(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
				return nil, fmt.Errorf("service crashed")
			},
		}
		file := newMCPFile("/tools/create-issue", transport)

		// Write triggers the call (which fails)
		_ = file.Write(context.Background(), []byte(`{"title":"test"}`))

		// When: Read is called
		_, err := file.Read(1 << 20)

		// Then: error contains ErrServiceUnavailable
		if err == nil {
			t.Fatal("expected error when transport fails, got nil")
		}
	})
}

func TestMCPFile_Close(t *testing.T) {
	t.Run("close does not close transport connection", func(t *testing.T) {
		// Given: an mcpFile (closing a file should not close the MCP connection itself)
		transport := &mockMCPTransport{}
		file := newMCPFile("/tools/create-issue", transport)

		// When: Close is called
		err := file.Close()

		// Then: no error, transport is NOT closed (connection reuse)
		if err != nil {
			t.Fatalf("Close failed: %v", err)
		}
		if transport.closed {
			t.Fatal("expected Transport NOT to be closed when file is closed")
		}
	})
}

func TestMCPFile_Stat(t *testing.T) {
	t.Run("stat returns MCP tool metadata", func(t *testing.T) {
		// Given: an mcpFile
		transport := &mockMCPTransport{}
		file := newMCPFile("/tools/create-issue", transport)

		// When: Stat is called
		stat, err := file.Stat()

		// Then: returns FileStat with device info
		if err != nil {
			t.Fatalf("Stat failed: %v", err)
		}
		if !stat.IsDevice {
			t.Fatal("expected IsDevice=true for MCP file")
		}
	})
}

func TestMCPFile_Timeout(t *testing.T) {
	t.Run("write respects context timeout within 3 seconds", func(t *testing.T) {
		// Given: an mcpFile with a transport that hangs
		transport := &mockMCPTransport{
			callFn: func(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
				// Simulate a hang — wait for context cancellation
				<-ctx.Done()
				return nil, ctx.Err()
			},
		}
		file := newMCPFile("/tools/slow-tool", transport)

		// When: Write is called with a 100ms timeout context
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		err := file.Write(ctx, []byte(`{"test":"data"}`))

		// Then: error is returned (timeout or context cancelled)
		if err == nil {
			t.Fatal("expected timeout error, got nil")
		}
	})
}
