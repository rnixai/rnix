// Package main is the ATDD 48.2 AC4 fixture: an MCP server that masks SIGTERM.
//
// Purpose: validate that drivers/mcp/transport.go::Close performs the
// SIGTERM → 5s wait → SIGKILL two-phase escalation. The fixture:
//
//   1. Performs the MCP initialize JSON-RPC handshake exactly once
//      (so transport.Connect succeeds and we reach the test body).
//   2. Installs signal.Notify(c, SIGTERM) and DISCARDS every SIGTERM it
//      receives. SIGKILL cannot be masked at userland so the upgrade path
//      eventually wins.
//   3. Blocks forever on a select{} (no reads from stdin after the handshake).
//
// Story §易错点 #10: A bash `trap "" TERM` would only mask the shell itself,
// not its descendants. A native Go program with signal.Notify is the
// portable way to truly ignore SIGTERM across PID namespaces.
//
// Built by the test via `go build -o <tmp>/mcp_ignore_sigterm .` at runtime
// (see drivers/mcp/transport_close_test.go::TestATDD_48_2_004_*).
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Result  any    `json:"result,omitempty"`
}

func main() {
	// Mask SIGTERM (and SIGINT for good measure) — the discard goroutine
	// drains the channel forever.
	sigCh := make(chan os.Signal, 8)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		for range sigCh {
			// intentionally discard
		}
	}()

	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	out := json.NewEncoder(os.Stdout)

	// 1) Read initialize request.
	if !in.Scan() {
		fmt.Fprintln(os.Stderr, "[fixture] unexpected EOF before initialize")
		os.Exit(1)
	}
	var initReq rpcRequest
	if err := json.Unmarshal(in.Bytes(), &initReq); err != nil {
		fmt.Fprintf(os.Stderr, "[fixture] parse initialize: %v\n", err)
		os.Exit(1)
	}
	if initReq.Method != "initialize" {
		fmt.Fprintf(os.Stderr, "[fixture] expected method=initialize, got %q\n", initReq.Method)
		os.Exit(1)
	}

	// 2) Reply with a minimal valid initialize result.
	_ = out.Encode(rpcResponse{
		JSONRPC: "2.0",
		ID:      initReq.ID,
		Result: map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"serverInfo": map[string]string{
				"name":    "ignore-sigterm-fixture",
				"version": "1.0.0",
			},
		},
	})

	// 3) Read the initialized notification (id-less, no response expected).
	if !in.Scan() {
		fmt.Fprintln(os.Stderr, "[fixture] unexpected EOF before initialized notif")
		os.Exit(1)
	}

	// 4) Block forever. SIGTERM is silently discarded; only SIGKILL ends us.
	select {}
}
