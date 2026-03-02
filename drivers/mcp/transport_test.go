package mcp

import (
	"context"
	"testing"
	"time"
)

// mockMCPServer is a bash script that simulates an MCP server:
// 1. Reads the initialize JSON-RPC request → responds with a valid result
// 2. Reads the initialized notification → discards (no response)
// 3. For each subsequent request line, extracts the id and echoes back a success response
const mockMCPServer = `
read req
echo '{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"mock","version":"1.0.0"}}}'
read notif
while IFS= read -r line; do
  id="${line#*\"id\":}"
  id="${id%%,*}"
  id="${id%%\}*}"
  echo "{\"jsonrpc\":\"2.0\",\"id\":${id},\"result\":{}}"
done
`

// --- Transport Interface Tests ---

func TestStdioTransport_Connect(t *testing.T) {
	t.Run("connect performs MCP initialize handshake", func(t *testing.T) {
		// Given: a StdioTransport configured with a mock MCP server
		transport := NewStdioTransport(TransportConfig{
			Command:       "bash",
			Args:          []string{"-c", mockMCPServer},
			TimeoutMillis: 3000,
		})

		// When: Connect is called
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		err := transport.Connect(ctx)

		// Then: connection succeeds with valid handshake
		if err != nil {
			t.Fatalf("Connect failed: %v", err)
		}
		_ = transport.Close()
	})

	t.Run("connect fails when server does not respond to initialize", func(t *testing.T) {
		// Given: a StdioTransport with a server that exits immediately (no response)
		transport := NewStdioTransport(TransportConfig{
			Command:       "true",
			TimeoutMillis: 3000,
		})

		// When: Connect is called
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		err := transport.Connect(ctx)

		// Then: error is returned (handshake failure)
		if err == nil {
			t.Fatal("expected error when server does not respond to initialize")
		}
		_ = transport.Close()
	})

	t.Run("connect cleans up process on handshake failure", func(t *testing.T) {
		// Given: a StdioTransport that fails during handshake
		transport := NewStdioTransport(TransportConfig{
			Command:       "true",
			TimeoutMillis: 3000,
		})

		// When: Connect fails
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = transport.Connect(ctx)

		// Then: transport is not marked as connected
		if transport.connected {
			t.Fatal("expected transport to NOT be connected after failed handshake")
		}
	})
}

func TestStdioTransport_Ping(t *testing.T) {
	t.Run("ping succeeds on connected transport", func(t *testing.T) {
		// Given: a connected StdioTransport
		transport := NewStdioTransport(TransportConfig{
			Command:       "bash",
			Args:          []string{"-c", mockMCPServer},
			TimeoutMillis: 3000,
		})
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := transport.Connect(ctx); err != nil {
			t.Fatalf("Connect failed: %v", err)
		}
		defer transport.Close()

		// When: Ping is called
		err := transport.Ping(ctx)

		// Then: ping succeeds (server responds to ping method)
		if err != nil {
			t.Fatalf("Ping failed: %v", err)
		}
	})

	t.Run("ping fails on unconnected transport", func(t *testing.T) {
		// Given: a StdioTransport (not connected)
		transport := NewStdioTransport(TransportConfig{
			Command:       "sleep",
			Args:          []string{"10"},
			TimeoutMillis: 3000,
		})

		// When: Ping is called without Connect
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		err := transport.Ping(ctx)

		// Then: error is returned (not connected)
		if err == nil {
			t.Fatal("expected ping to fail for unconnected transport")
		}
	})
}

func TestStdioTransport_Close(t *testing.T) {
	t.Run("close terminates MCP server process", func(t *testing.T) {
		// Given: a connected StdioTransport
		transport := NewStdioTransport(TransportConfig{
			Command:       "bash",
			Args:          []string{"-c", mockMCPServer},
			TimeoutMillis: 3000,
		})
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = transport.Connect(ctx)

		// When: Close is called
		err := transport.Close()

		// Then: no error
		if err != nil {
			t.Fatalf("Close failed: %v", err)
		}
	})

	t.Run("close is safe when not connected", func(t *testing.T) {
		// Given: a StdioTransport (never connected)
		transport := NewStdioTransport(TransportConfig{
			Command:       "echo",
			Args:          []string{"test"},
			TimeoutMillis: 3000,
		})

		// When: Close is called
		err := transport.Close()

		// Then: no panic, no error
		if err != nil {
			t.Fatalf("Close failed: %v", err)
		}
	})
}

func TestStdioTransport_Call(t *testing.T) {
	t.Run("call returns response from connected server", func(t *testing.T) {
		// Given: a connected StdioTransport
		transport := NewStdioTransport(TransportConfig{
			Command:       "bash",
			Args:          []string{"-c", mockMCPServer},
			TimeoutMillis: 3000,
		})
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := transport.Connect(ctx); err != nil {
			t.Fatalf("Connect failed: %v", err)
		}
		defer transport.Close()

		// When: Call is invoked with tools/list
		result, err := transport.Call(ctx, "tools/list", nil)

		// Then: result is returned
		if err != nil {
			t.Fatalf("Call failed: %v", err)
		}
		if result == nil {
			t.Fatal("expected non-nil result")
		}
	})

	t.Run("call fails when not connected", func(t *testing.T) {
		// Given: a StdioTransport (not connected)
		transport := NewStdioTransport(TransportConfig{
			Command:       "echo",
			Args:          []string{"{}"},
			TimeoutMillis: 3000,
		})

		// When: Call is invoked without connection
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		_, err := transport.Call(ctx, "tools/list", nil)

		// Then: error is expected (not connected)
		if err == nil {
			t.Fatal("expected error when calling without connection")
		}
	})
}

// --- TransportConfig Tests ---

func TestTransportConfig_Validation(t *testing.T) {
	t.Run("empty command is invalid", func(t *testing.T) {
		// Given: a TransportConfig with empty command
		config := TransportConfig{
			Command: "",
		}

		// When: Connect is called
		transport := NewStdioTransport(config)
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		err := transport.Connect(ctx)

		// Then: error is returned
		if err == nil {
			t.Fatal("expected error when connecting with empty command")
		}
		_ = transport.Close()
	})
}
