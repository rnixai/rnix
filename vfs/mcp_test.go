package vfs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// --- mcpFile Tests (existing, regression) ---

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

// --- Story 9.3: mcpFileFactory routing tests (AC #1-#7) ---

func TestMCPFileFactory_Routing(t *testing.T) {
	transport := &mockMCPTransport{}
	factory := mcpFileFactory(transport)

	tests := []struct {
		name        string
		subpath     string
		wantType    string // expected concrete type name
		wantErr     bool
		wantErrCode types.ErrCode
	}{
		// AC5: Root path
		{name: "empty subpath routes to mcpRootFile", subpath: "", wantType: "*vfs.mcpRootFile"},
		{name: "slash routes to mcpRootFile", subpath: "/", wantType: "*vfs.mcpRootFile"},

		// AC2: Tool list
		{name: "/tools routes to mcpToolListFile", subpath: "/tools", wantType: "*vfs.mcpToolListFile"},
		{name: "/tools/ trailing slash routes to mcpToolListFile", subpath: "/tools/", wantType: "*vfs.mcpToolListFile"},

		// AC1: Tool call (existing mcpFile)
		{name: "/tools/create-issue routes to mcpFile", subpath: "/tools/create-issue", wantType: "*vfs.mcpFile"},
		{name: "/tools/search routes to mcpFile", subpath: "/tools/search", wantType: "*vfs.mcpFile"},

		// AC4: Resource list
		{name: "/resources routes to mcpResourceListFile", subpath: "/resources", wantType: "*vfs.mcpResourceListFile"},
		{name: "/resources/ trailing slash routes to mcpResourceListFile", subpath: "/resources/", wantType: "*vfs.mcpResourceListFile"},

		// AC3: Resource read
		{name: "/resources/repo://owner/repo routes to mcpResourceFile", subpath: "/resources/repo://owner/repo", wantType: "*vfs.mcpResourceFile"},
		{name: "/resources/file:///path routes to mcpResourceFile", subpath: "/resources/file:///path/to/file", wantType: "*vfs.mcpResourceFile"},
		{name: "/resources/https://example.com routes to mcpResourceFile", subpath: "/resources/https://example.com/data", wantType: "*vfs.mcpResourceFile"},

		// AC7: Invalid paths
		{name: "/invalid returns ErrNotFound", subpath: "/invalid", wantErr: true, wantErrCode: types.ErrNotFound},
		{name: "/foo/bar returns ErrNotFound", subpath: "/foo/bar", wantErr: true, wantErrCode: types.ErrNotFound},
		{name: "/toolsx returns ErrNotFound", subpath: "/toolsx", wantErr: true, wantErrCode: types.ErrNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// When: factory is called with the subpath
			file, err := factory(tt.subpath, O_RDONLY, "")

			// Then: check expected results
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for subpath %q, got nil", tt.subpath)
				}
				var drvErr *types.DriverError
				if !errors.As(err, &drvErr) {
					t.Fatalf("expected *types.DriverError, got %T: %v", err, err)
				}
				if drvErr.Code != tt.wantErrCode {
					t.Fatalf("expected error code %v, got %v", tt.wantErrCode, drvErr.Code)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for subpath %q: %v", tt.subpath, err)
			}
			gotType := fmt.Sprintf("%T", file)
			if gotType != tt.wantType {
				t.Fatalf("subpath %q: expected type %s, got %s", tt.subpath, tt.wantType, gotType)
			}
		})
	}
}

// --- Story 9.3: readFromBuffer tests (AC #1-#5) ---

func TestReadFromBuffer(t *testing.T) {
	t.Run("empty buffer returns nil nil", func(t *testing.T) {
		// Given: an empty buffer
		// When: readFromBuffer is called
		data, remaining := readFromBuffer(nil, 10)

		// Then: both are nil
		if data != nil {
			t.Fatalf("expected nil data, got %v", data)
		}
		if remaining != nil {
			t.Fatalf("expected nil remaining, got %v", remaining)
		}
	})

	t.Run("zero length buffer returns nil nil", func(t *testing.T) {
		// Given: a zero-length buffer
		data, remaining := readFromBuffer([]byte{}, 10)

		// Then: both are nil
		if data != nil {
			t.Fatalf("expected nil data, got %v", data)
		}
		if remaining != nil {
			t.Fatalf("expected nil remaining, got %v", remaining)
		}
	})

	t.Run("length zero returns all data", func(t *testing.T) {
		// Given: a buffer with data
		buf := []byte("hello world")

		// When: readFromBuffer is called with length=0
		data, remaining := readFromBuffer(buf, 0)

		// Then: all data is returned, remaining is empty
		if string(data) != "hello world" {
			t.Fatalf("expected 'hello world', got %q", data)
		}
		if len(remaining) != 0 {
			t.Fatalf("expected empty remaining, got %d bytes", len(remaining))
		}
	})

	t.Run("length exceeding buffer returns all data", func(t *testing.T) {
		// Given: a buffer with 5 bytes
		buf := []byte("hello")

		// When: readFromBuffer is called with length=100
		data, remaining := readFromBuffer(buf, 100)

		// Then: all data is returned
		if string(data) != "hello" {
			t.Fatalf("expected 'hello', got %q", data)
		}
		if len(remaining) != 0 {
			t.Fatalf("expected empty remaining, got %d bytes", len(remaining))
		}
	})

	t.Run("correct chunking and remaining", func(t *testing.T) {
		// Given: a buffer with 10 bytes
		buf := []byte("0123456789")

		// When: readFromBuffer is called with length=4
		data, remaining := readFromBuffer(buf, 4)

		// Then: first 4 bytes returned, remaining has 6
		if string(data) != "0123" {
			t.Fatalf("expected '0123', got %q", data)
		}
		if string(remaining) != "456789" {
			t.Fatalf("expected '456789', got %q", remaining)
		}
	})

	t.Run("multiple reads drain buffer", func(t *testing.T) {
		// Given: a buffer with data
		buf := []byte("abcdef")

		// When: read in chunks of 2
		d1, rem := readFromBuffer(buf, 2)
		d2, rem := readFromBuffer(rem, 2)
		d3, rem := readFromBuffer(rem, 2)
		d4, rem := readFromBuffer(rem, 2)

		// Then: all chunks correct, final read returns nil
		if string(d1) != "ab" {
			t.Fatalf("chunk 1: expected 'ab', got %q", d1)
		}
		if string(d2) != "cd" {
			t.Fatalf("chunk 2: expected 'cd', got %q", d2)
		}
		if string(d3) != "ef" {
			t.Fatalf("chunk 3: expected 'ef', got %q", d3)
		}
		if d4 != nil {
			t.Fatalf("chunk 4: expected nil, got %v", d4)
		}
		if rem != nil {
			t.Fatalf("final remaining: expected nil, got %v", rem)
		}
	})
}

// --- Story 9.3: mcpToolListFile tests (AC #2) ---

func TestMCPToolListFile_Read(t *testing.T) {
	t.Run("read calls tools/list via transport", func(t *testing.T) {
		// Given: a mcpToolListFile with a mock transport
		var capturedMethod string
		transport := &mockMCPTransport{
			callFn: func(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
				capturedMethod = method
				return json.RawMessage(`{"tools":[{"name":"create-issue"}]}`), nil
			},
		}
		file := newMCPToolListFile(transport)

		// When: Read is called
		data, err := file.Read(1 << 20)

		// Then: transport.Call is invoked with "tools/list"
		if err != nil {
			t.Fatalf("Read failed: %v", err)
		}
		if capturedMethod != "tools/list" {
			t.Fatalf("expected method 'tools/list', got %q", capturedMethod)
		}
		if len(data) == 0 {
			t.Fatal("expected non-empty read data")
		}
	})

	t.Run("read passes nil params to tools/list", func(t *testing.T) {
		// Given: a mcpToolListFile
		var capturedParams json.RawMessage
		transport := &mockMCPTransport{
			callFn: func(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
				capturedParams = params
				return json.RawMessage(`{"tools":[]}`), nil
			},
		}
		file := newMCPToolListFile(transport)

		// When: Read is called
		_, _ = file.Read(1 << 20)

		// Then: params passed to transport is nil
		if capturedParams != nil {
			t.Fatalf("expected nil params, got %s", capturedParams)
		}
	})

	t.Run("subsequent reads use cache without calling transport again", func(t *testing.T) {
		// Given: a mcpToolListFile
		callCount := 0
		transport := &mockMCPTransport{
			callFn: func(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
				callCount++
				return json.RawMessage(`{"tools":[{"name":"tool1"}]}`), nil
			},
		}
		file := newMCPToolListFile(transport)

		// When: Read is called twice (first loads, second returns remaining)
		data1, err1 := file.Read(5)
		if err1 != nil {
			t.Fatalf("first Read failed: %v", err1)
		}

		data2, err2 := file.Read(1 << 20)
		if err2 != nil {
			t.Fatalf("second Read failed: %v", err2)
		}

		// Then: transport.Call is only invoked once
		if callCount != 1 {
			t.Fatalf("expected 1 transport call, got %d", callCount)
		}
		// Combined data should equal the full response
		combined := append(data1, data2...)
		if string(combined) != `{"tools":[{"name":"tool1"}]}` {
			t.Fatalf("combined reads = %q, want full response", combined)
		}
	})

	t.Run("read returns ErrServiceUnavailable when transport fails", func(t *testing.T) {
		// Given: a mcpToolListFile with a failing transport
		transport := &mockMCPTransport{
			callFn: func(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
				return nil, fmt.Errorf("connection refused")
			},
		}
		file := newMCPToolListFile(transport)

		// When: Read is called
		_, err := file.Read(1 << 20)

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

func TestMCPToolListFile_Write(t *testing.T) {
	t.Run("write returns ErrInvalid for read-only tool list", func(t *testing.T) {
		// Given: a mcpToolListFile (read-only)
		transport := &mockMCPTransport{}
		file := newMCPToolListFile(transport)

		// When: Write is called
		err := file.Write(context.Background(), []byte(`{"test":"data"}`))

		// Then: error contains ErrInvalid
		if err == nil {
			t.Fatal("expected error from write to read-only file, got nil")
		}
		var drvErr *types.DriverError
		if !errors.As(err, &drvErr) {
			t.Fatalf("expected *types.DriverError, got %T: %v", err, err)
		}
		if drvErr.Code != types.ErrInvalid {
			t.Fatalf("expected ErrInvalid, got %v", drvErr.Code)
		}
	})
}

func TestMCPToolListFile_Close(t *testing.T) {
	t.Run("close then read returns error", func(t *testing.T) {
		// Given: a mcpToolListFile that has been closed
		transport := &mockMCPTransport{}
		file := newMCPToolListFile(transport)
		_ = file.Close()

		// When: Read is called after Close
		_, err := file.Read(1 << 20)

		// Then: error is returned
		if err == nil {
			t.Fatal("expected error reading closed file, got nil")
		}
	})

	t.Run("close then write returns error", func(t *testing.T) {
		// Given: a mcpToolListFile that has been closed
		transport := &mockMCPTransport{}
		file := newMCPToolListFile(transport)
		_ = file.Close()

		// When: Write is called after Close
		err := file.Write(context.Background(), []byte(`{}`))

		// Then: error is returned
		if err == nil {
			t.Fatal("expected error writing closed file, got nil")
		}
	})

	t.Run("double close returns error", func(t *testing.T) {
		// Given: a mcpToolListFile
		transport := &mockMCPTransport{}
		file := newMCPToolListFile(transport)

		// When: Close is called twice
		err1 := file.Close()
		err2 := file.Close()

		// Then: first close succeeds, second returns error
		if err1 != nil {
			t.Fatalf("first Close failed: %v", err1)
		}
		if err2 == nil {
			t.Fatal("expected error on double close, got nil")
		}
	})
}

func TestMCPToolListFile_Stat(t *testing.T) {
	t.Run("stat returns tool list metadata", func(t *testing.T) {
		// Given: a mcpToolListFile
		transport := &mockMCPTransport{}
		file := newMCPToolListFile(transport)

		// When: Stat is called
		stat, err := file.Stat()

		// Then: returns FileStat with device info
		if err != nil {
			t.Fatalf("Stat failed: %v", err)
		}
		if !stat.IsDevice {
			t.Fatal("expected IsDevice=true for tool list file")
		}
	})
}

// --- Story 9.3: mcpResourceFile tests (AC #3) ---

func TestMCPResourceFile_Read(t *testing.T) {
	t.Run("read calls resources/read with correct URI", func(t *testing.T) {
		// Given: a mcpResourceFile for repo://owner/repo
		var capturedMethod string
		var capturedParams json.RawMessage
		transport := &mockMCPTransport{
			callFn: func(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
				capturedMethod = method
				capturedParams = params
				return json.RawMessage(`{"contents":[{"text":"file content"}]}`), nil
			},
		}
		file := newMCPResourceFile("/resources/repo://owner/repo", transport)

		// When: Read is called
		data, err := file.Read(1 << 20)

		// Then: transport.Call is invoked with "resources/read" and correct URI
		if err != nil {
			t.Fatalf("Read failed: %v", err)
		}
		if capturedMethod != "resources/read" {
			t.Fatalf("expected method 'resources/read', got %q", capturedMethod)
		}
		// Verify URI in params
		var paramsMap map[string]string
		if err := json.Unmarshal(capturedParams, &paramsMap); err != nil {
			t.Fatalf("failed to unmarshal params: %v", err)
		}
		if paramsMap["uri"] != "repo://owner/repo" {
			t.Fatalf("expected URI 'repo://owner/repo', got %q", paramsMap["uri"])
		}
		if len(data) == 0 {
			t.Fatal("expected non-empty read data")
		}
	})

	t.Run("read returns ErrServiceUnavailable when transport fails", func(t *testing.T) {
		// Given: a mcpResourceFile with a failing transport
		transport := &mockMCPTransport{
			callFn: func(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
				return nil, fmt.Errorf("server timeout")
			},
		}
		file := newMCPResourceFile("/resources/repo://a/b", transport)

		// When: Read is called
		_, err := file.Read(1 << 20)

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

func TestMCPResourceFile_Write(t *testing.T) {
	t.Run("write returns ErrInvalid for read-only resource", func(t *testing.T) {
		// Given: a mcpResourceFile (read-only)
		transport := &mockMCPTransport{}
		file := newMCPResourceFile("/resources/repo://a/b", transport)

		// When: Write is called
		err := file.Write(context.Background(), []byte(`{}`))

		// Then: error contains ErrInvalid
		if err == nil {
			t.Fatal("expected error from write to read-only resource, got nil")
		}
		var drvErr *types.DriverError
		if !errors.As(err, &drvErr) {
			t.Fatalf("expected *types.DriverError, got %T: %v", err, err)
		}
		if drvErr.Code != types.ErrInvalid {
			t.Fatalf("expected ErrInvalid, got %v", drvErr.Code)
		}
	})
}

func TestMCPResourceFile_Close(t *testing.T) {
	t.Run("close then read returns error", func(t *testing.T) {
		// Given: a closed mcpResourceFile
		transport := &mockMCPTransport{}
		file := newMCPResourceFile("/resources/repo://a/b", transport)
		_ = file.Close()

		// When: Read is called after Close
		_, err := file.Read(1 << 20)

		// Then: error is returned
		if err == nil {
			t.Fatal("expected error reading closed file, got nil")
		}
	})

	t.Run("close then write returns error", func(t *testing.T) {
		// Given: a closed mcpResourceFile
		transport := &mockMCPTransport{}
		file := newMCPResourceFile("/resources/repo://a/b", transport)
		_ = file.Close()

		// When: Write is called after Close
		err := file.Write(context.Background(), []byte(`{}`))

		// Then: error is returned
		if err == nil {
			t.Fatal("expected error writing closed file, got nil")
		}
	})

	t.Run("double close returns error", func(t *testing.T) {
		// Given: a mcpResourceFile
		transport := &mockMCPTransport{}
		file := newMCPResourceFile("/resources/repo://a/b", transport)

		// When: Close is called twice
		err1 := file.Close()
		err2 := file.Close()

		// Then: first close succeeds, second returns error
		if err1 != nil {
			t.Fatalf("first Close failed: %v", err1)
		}
		if err2 == nil {
			t.Fatal("expected error on double close, got nil")
		}
	})
}

// --- Story 9.3: parseResourceURI tests (AC #3) ---

func TestParseResourceURI(t *testing.T) {
	tests := []struct {
		name    string
		subpath string
		wantURI string
	}{
		{name: "repo scheme", subpath: "/resources/repo://owner/repo", wantURI: "repo://owner/repo"},
		{name: "file scheme", subpath: "/resources/file:///path/to/file", wantURI: "file:///path/to/file"},
		{name: "https scheme", subpath: "/resources/https://example.com/data", wantURI: "https://example.com/data"},
		{name: "simple name", subpath: "/resources/my-resource", wantURI: "my-resource"},
		{name: "nested path", subpath: "/resources/org/repo/branch/file.txt", wantURI: "org/repo/branch/file.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// When: parseResourceURI is called
			got := parseResourceURI(tt.subpath)

			// Then: URI is correctly extracted
			if got != tt.wantURI {
				t.Fatalf("parseResourceURI(%q) = %q, want %q", tt.subpath, got, tt.wantURI)
			}
		})
	}
}

// --- Story 9.3: mcpResourceListFile tests (AC #4) ---

func TestMCPResourceListFile_Read(t *testing.T) {
	t.Run("read calls resources/list via transport", func(t *testing.T) {
		// Given: a mcpResourceListFile with a mock transport
		var capturedMethod string
		transport := &mockMCPTransport{
			callFn: func(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
				capturedMethod = method
				return json.RawMessage(`{"resources":[{"uri":"repo://a/b","name":"repo"}]}`), nil
			},
		}
		file := newMCPResourceListFile(transport)

		// When: Read is called
		data, err := file.Read(1 << 20)

		// Then: transport.Call is invoked with "resources/list"
		if err != nil {
			t.Fatalf("Read failed: %v", err)
		}
		if capturedMethod != "resources/list" {
			t.Fatalf("expected method 'resources/list', got %q", capturedMethod)
		}
		if len(data) == 0 {
			t.Fatal("expected non-empty read data")
		}
	})

	t.Run("read passes nil params to resources/list", func(t *testing.T) {
		// Given: a mcpResourceListFile
		var capturedParams json.RawMessage
		transport := &mockMCPTransport{
			callFn: func(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
				capturedParams = params
				return json.RawMessage(`{"resources":[]}`), nil
			},
		}
		file := newMCPResourceListFile(transport)

		// When: Read is called
		_, _ = file.Read(1 << 20)

		// Then: params is nil
		if capturedParams != nil {
			t.Fatalf("expected nil params, got %s", capturedParams)
		}
	})

	t.Run("subsequent reads use cache", func(t *testing.T) {
		// Given: a mcpResourceListFile
		callCount := 0
		transport := &mockMCPTransport{
			callFn: func(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
				callCount++
				return json.RawMessage(`{"resources":[]}`), nil
			},
		}
		file := newMCPResourceListFile(transport)

		// When: Read is called twice
		_, _ = file.Read(5)
		_, _ = file.Read(1 << 20)

		// Then: transport.Call invoked only once
		if callCount != 1 {
			t.Fatalf("expected 1 transport call, got %d", callCount)
		}
	})

	t.Run("read returns ErrServiceUnavailable when transport fails", func(t *testing.T) {
		// Given: a mcpResourceListFile with failing transport
		transport := &mockMCPTransport{
			callFn: func(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
				return nil, fmt.Errorf("EOF")
			},
		}
		file := newMCPResourceListFile(transport)

		// When: Read is called
		_, err := file.Read(1 << 20)

		// Then: error contains ErrServiceUnavailable
		if err == nil {
			t.Fatal("expected error, got nil")
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

func TestMCPResourceListFile_Write(t *testing.T) {
	t.Run("write returns ErrInvalid for read-only resource list", func(t *testing.T) {
		// Given: a mcpResourceListFile (read-only)
		transport := &mockMCPTransport{}
		file := newMCPResourceListFile(transport)

		// When: Write is called
		err := file.Write(context.Background(), []byte(`{}`))

		// Then: error contains ErrInvalid
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var drvErr *types.DriverError
		if !errors.As(err, &drvErr) {
			t.Fatalf("expected *types.DriverError, got %T: %v", err, err)
		}
		if drvErr.Code != types.ErrInvalid {
			t.Fatalf("expected ErrInvalid, got %v", drvErr.Code)
		}
	})
}

func TestMCPResourceListFile_Close(t *testing.T) {
	t.Run("close then read returns error", func(t *testing.T) {
		// Given: a closed mcpResourceListFile
		transport := &mockMCPTransport{}
		file := newMCPResourceListFile(transport)
		_ = file.Close()

		// When: Read is called after Close
		_, err := file.Read(1 << 20)

		// Then: error is returned
		if err == nil {
			t.Fatal("expected error reading closed resource list file, got nil")
		}
	})

	t.Run("close then write returns error", func(t *testing.T) {
		// Given: a closed mcpResourceListFile
		transport := &mockMCPTransport{}
		file := newMCPResourceListFile(transport)
		_ = file.Close()

		// When: Write is called after Close
		err := file.Write(context.Background(), []byte(`{}`))

		// Then: error is returned
		if err == nil {
			t.Fatal("expected error writing closed resource list file, got nil")
		}
	})

	t.Run("double close returns error", func(t *testing.T) {
		// Given: a mcpResourceListFile
		transport := &mockMCPTransport{}
		file := newMCPResourceListFile(transport)

		// When: Close is called twice
		err1 := file.Close()
		err2 := file.Close()

		// Then: first close succeeds, second returns error
		if err1 != nil {
			t.Fatalf("first Close failed: %v", err1)
		}
		if err2 == nil {
			t.Fatal("expected error on double close, got nil")
		}
	})
}

// --- Story 9.3: mcpRootFile tests (AC #5) ---

func TestMCPRootFile_Read(t *testing.T) {
	t.Run("read returns tools and resources namespace list", func(t *testing.T) {
		// Given: a mcpRootFile
		file := newMCPRootFile()

		// When: Read is called
		data, err := file.Read(1 << 20)

		// Then: returns JSON array ["tools","resources"]
		if err != nil {
			t.Fatalf("Read failed: %v", err)
		}
		var namespaces []string
		if err := json.Unmarshal(data, &namespaces); err != nil {
			t.Fatalf("response is not valid JSON array: %v", err)
		}
		if len(namespaces) != 2 {
			t.Fatalf("expected 2 namespaces, got %d", len(namespaces))
		}
		if namespaces[0] != "tools" || namespaces[1] != "resources" {
			t.Fatalf("expected [tools, resources], got %v", namespaces)
		}
	})

	t.Run("read returns valid JSON", func(t *testing.T) {
		// Given: a mcpRootFile
		file := newMCPRootFile()

		// When: Read is called
		data, err := file.Read(1 << 20)

		// Then: response is valid JSON
		if err != nil {
			t.Fatalf("Read failed: %v", err)
		}
		if !json.Valid(data) {
			t.Fatalf("response is not valid JSON: %q", data)
		}
	})

	t.Run("read drains on second call", func(t *testing.T) {
		// Given: a mcpRootFile that has been fully read
		file := newMCPRootFile()

		// When: Read is called twice
		data1, err1 := file.Read(1 << 20)
		data2, err2 := file.Read(1 << 20)

		// Then: first returns data, second returns nil (drained)
		if err1 != nil {
			t.Fatalf("first Read failed: %v", err1)
		}
		if len(data1) == 0 {
			t.Fatal("expected non-empty first read")
		}
		if err2 != nil {
			t.Fatalf("second Read failed: %v", err2)
		}
		if data2 != nil {
			t.Fatalf("expected nil on drained read, got %q", data2)
		}
	})
}

func TestMCPRootFile_Write(t *testing.T) {
	t.Run("write returns ErrInvalid for read-only root", func(t *testing.T) {
		// Given: a mcpRootFile (read-only)
		file := newMCPRootFile()

		// When: Write is called
		err := file.Write(context.Background(), []byte(`{}`))

		// Then: error contains ErrInvalid
		if err == nil {
			t.Fatal("expected error from write to read-only root, got nil")
		}
		var drvErr *types.DriverError
		if !errors.As(err, &drvErr) {
			t.Fatalf("expected *types.DriverError, got %T: %v", err, err)
		}
		if drvErr.Code != types.ErrInvalid {
			t.Fatalf("expected ErrInvalid, got %v", drvErr.Code)
		}
	})
}

func TestMCPRootFile_Close(t *testing.T) {
	t.Run("close then read returns error", func(t *testing.T) {
		// Given: a closed mcpRootFile
		file := newMCPRootFile()
		_ = file.Close()

		// When: Read is called after Close
		_, err := file.Read(1 << 20)

		// Then: error is returned
		if err == nil {
			t.Fatal("expected error reading closed root file, got nil")
		}
	})
}

// --- Story 9.3: mcpCallTimeout constant test ---

func TestMCPCallTimeout(t *testing.T) {
	t.Run("mcpCallTimeout is 30 seconds", func(t *testing.T) {
		// Given/When: the constant is defined
		// Then: it equals 30 seconds
		if mcpCallTimeout != 30*time.Second {
			t.Fatalf("expected mcpCallTimeout=30s, got %v", mcpCallTimeout)
		}
	})
}

// --- Story 9.3: mcpFile regression (AC #1) ---

func TestMCPFile_WriteReadRoundTrip(t *testing.T) {
	t.Run("write then read complete round trip unchanged", func(t *testing.T) {
		// Given: an mcpFile with a transport that echoes response
		expectedResponse := `{"content":[{"type":"text","text":"result data"}]}`
		transport := &mockMCPTransport{
			callFn: func(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
				return json.RawMessage(expectedResponse), nil
			},
		}
		file := newMCPFile("/tools/my-tool", transport)

		// When: Write then Read
		err := file.Write(context.Background(), []byte(`{"arg":"val"}`))
		if err != nil {
			t.Fatalf("Write failed: %v", err)
		}
		data, err := file.Read(1 << 20)
		if err != nil {
			t.Fatalf("Read failed: %v", err)
		}

		// Then: Read returns exact response from Write
		if string(data) != expectedResponse {
			t.Fatalf("round trip data mismatch: got %q, want %q", data, expectedResponse)
		}
	})
}

func TestParseToolName(t *testing.T) {
	t.Run("extracts tool name from subpath", func(t *testing.T) {
		// Given/When/Then: parseToolName extracts correctly
		tests := []struct {
			subpath  string
			wantName string
		}{
			{"/tools/create-issue", "create-issue"},
			{"/tools/search", "search"},
			{"/tools/complex-tool-name", "complex-tool-name"},
		}
		for _, tt := range tests {
			got := parseToolName(tt.subpath)
			if got != tt.wantName {
				t.Errorf("parseToolName(%q) = %q, want %q", tt.subpath, got, tt.wantName)
			}
		}
	})
}

// --- Closed file DriverError assertions (bare error → DriverError fix) ---

func TestMCPFile_ClosedReturnsDriverError(t *testing.T) {
	assertDriverError := func(t *testing.T, err error, wantOp string) {
		t.Helper()
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var drvErr *types.DriverError
		if !errors.As(err, &drvErr) {
			t.Fatalf("expected *types.DriverError, got %T: %v", err, err)
		}
		if drvErr.Code != types.ErrDriver {
			t.Fatalf("expected ErrDriver, got %v", drvErr.Code)
		}
		if drvErr.Op != wantOp {
			t.Fatalf("expected Op=%q, got %q", wantOp, drvErr.Op)
		}
	}

	t.Run("mcpFile closed returns DriverError", func(t *testing.T) {
		transport := &mockMCPTransport{}
		file := newMCPFile("/tools/test", transport)
		_ = file.Close()

		_, readErr := file.Read(1 << 20)
		assertDriverError(t, readErr, "Read")

		writeErr := file.Write(context.Background(), []byte(`{}`))
		assertDriverError(t, writeErr, "Write")

		closeErr := file.Close()
		assertDriverError(t, closeErr, "Close")

		_, statErr := file.Stat()
		assertDriverError(t, statErr, "Stat")
	})

	t.Run("mcpRootFile closed returns DriverError", func(t *testing.T) {
		file := newMCPRootFile()
		_ = file.Close()

		_, readErr := file.Read(1 << 20)
		assertDriverError(t, readErr, "Read")

		closeErr := file.Close()
		assertDriverError(t, closeErr, "Close")

		_, statErr := file.Stat()
		assertDriverError(t, statErr, "Stat")
	})

	t.Run("mcpToolListFile closed returns DriverError", func(t *testing.T) {
		transport := &mockMCPTransport{}
		file := newMCPToolListFile(transport)
		_ = file.Close()

		_, readErr := file.Read(1 << 20)
		assertDriverError(t, readErr, "Read")

		writeErr := file.Write(context.Background(), []byte(`{}`))
		assertDriverError(t, writeErr, "Write")

		closeErr := file.Close()
		assertDriverError(t, closeErr, "Close")

		_, statErr := file.Stat()
		assertDriverError(t, statErr, "Stat")
	})

	t.Run("mcpResourceFile closed returns DriverError", func(t *testing.T) {
		transport := &mockMCPTransport{}
		file := newMCPResourceFile("/resources/repo://a/b", transport)
		_ = file.Close()

		_, readErr := file.Read(1 << 20)
		assertDriverError(t, readErr, "Read")

		writeErr := file.Write(context.Background(), []byte(`{}`))
		assertDriverError(t, writeErr, "Write")

		closeErr := file.Close()
		assertDriverError(t, closeErr, "Close")

		_, statErr := file.Stat()
		assertDriverError(t, statErr, "Stat")
	})

	t.Run("mcpResourceListFile closed returns DriverError", func(t *testing.T) {
		transport := &mockMCPTransport{}
		file := newMCPResourceListFile(transport)
		_ = file.Close()

		_, readErr := file.Read(1 << 20)
		assertDriverError(t, readErr, "Read")

		writeErr := file.Write(context.Background(), []byte(`{}`))
		assertDriverError(t, writeErr, "Write")

		closeErr := file.Close()
		assertDriverError(t, closeErr, "Close")

		_, statErr := file.Stat()
		assertDriverError(t, statErr, "Stat")
	})

	t.Run("mcpFile no-response read returns DriverError ErrInvalid", func(t *testing.T) {
		transport := &mockMCPTransport{}
		file := newMCPFile("/tools/test", transport)

		// Read without Write should return ErrInvalid
		_, err := file.Read(1 << 20)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var drvErr *types.DriverError
		if !errors.As(err, &drvErr) {
			t.Fatalf("expected *types.DriverError, got %T: %v", err, err)
		}
		if drvErr.Code != types.ErrInvalid {
			t.Fatalf("expected ErrInvalid, got %v", drvErr.Code)
		}
	})
}
