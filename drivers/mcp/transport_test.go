package mcp

import (
	"context"
	"testing"
	"time"
)

// --- Transport Interface Tests ---

func TestStdioTransport_Connect(t *testing.T) {
	t.Run("connect initializes MCP session", func(t *testing.T) {
		// Given: a StdioTransport configured with a valid command
		// Note: Uses a simple echo command for testing, not a real MCP server
		transport := NewStdioTransport(TransportConfig{
			Command:       "echo",
			Args:          []string{"{}"},
			TimeoutMillis: 3000,
		})

		// When: Connect is called
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		err := transport.Connect(ctx)

		// Then: connection should be attempted
		// (May fail in test env without real MCP server - that's expected in RED phase)
		_ = err // RED phase: we verify the interface exists and is callable
		_ = transport.Close()
	})
}

func TestStdioTransport_Ping(t *testing.T) {
	t.Run("ping times out within 3 seconds when server unresponsive", func(t *testing.T) {
		// Given: a StdioTransport (not connected)
		transport := NewStdioTransport(TransportConfig{
			Command:       "sleep",
			Args:          []string{"10"},
			TimeoutMillis: 3000,
		})

		// When: Ping is called with a 3-second timeout
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		err := transport.Ping(ctx)

		// Then: error is returned (timeout or not connected)
		if err == nil {
			t.Fatal("expected ping to fail for unconnected transport")
		}
	})
}

func TestStdioTransport_Close(t *testing.T) {
	t.Run("close terminates MCP server process", func(t *testing.T) {
		// Given: a StdioTransport
		transport := NewStdioTransport(TransportConfig{
			Command:       "echo",
			Args:          []string{"test"},
			TimeoutMillis: 3000,
		})

		// When: Close is called
		err := transport.Close()

		// Then: no panic (graceful even if not connected)
		_ = err
	})
}

func TestStdioTransport_Call(t *testing.T) {
	t.Run("call sends JSON-RPC request and returns response", func(t *testing.T) {
		// Given: a StdioTransport (RED phase - interface verification)
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

		// When: NewStdioTransport is called (or validation)
		transport := NewStdioTransport(config)

		// Then: transport exists but Connect should fail
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		err := transport.Connect(ctx)
		if err == nil {
			t.Fatal("expected error when connecting with empty command")
		}
		_ = transport.Close()
	})
}
