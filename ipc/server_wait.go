package ipc

import (
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

const maxWaitTimeoutMs = int64(1<<63-1) / int64(time.Millisecond)

// handleWait implements MethodWait (Story 63.1). It blocks until the target
// process reaches a terminal state (Zombie/Dead), then propagates its exit
// code — or returns TimedOut=true after req.TimeoutMs (0 = wait forever).
//
// Design rulings (story §核心设计裁决):
//   - 裁决 1: waits on Process.TerminatedCh() — a broadcast channel closed
//     exactly once at both terminal transitions (Terminate /
//     killSuspendedProcess). Exit is assigned under p.mu before the close,
//     so a woken waiter always reads ExitCodeSet=true.
//   - 裁决 2: pure observer — never consumes proc.Done, never calls Reap.
//     The same PID can be waited concurrently and repeatedly (closed
//     channel always returns immediately; after the Dead-TTL sweep the
//     procHistory fallback answers).
//   - 裁决 3: dispatch does `return` after this handler (long-blocking
//     method, mirror MethodSpawn), so the handler owns the connection. A
//     watchdog goroutine probes conn with a 1-byte Read to detect client
//     disconnect (Ctrl-C / shell timeout) and unblock the select — no
//     goroutine leak, daemon shutdown doesn't burn the 10s force-exit.
//   - 裁决 4: timeout is a business result inside an OK envelope, not a
//     protocol error (mirror MCPTest). The target process is untouched.
//   - 裁决 6: Suspended is not terminal — terminated is only closed at
//     Zombie/Dead, so a suspended process keeps the waiter blocked
//     (Unix wait(2) semantics). Callers bound that risk with --timeout.
func (s *Server) handleWait(conn net.Conn, payload json.RawMessage) {
	var req WaitRequest
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &req); err != nil {
			writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: fmt.Sprintf("parse wait request: %v", err)}})
			return
		}
	}
	if req.PID == 0 {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "pid required"}})
		return
	}
	if s.kern == nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "internal", Message: "kernel not wired"}})
		return
	}
	if req.TimeoutMs < 0 {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "timeout_ms must be non-negative"}})
		return
	}
	if req.TimeoutMs > maxWaitTimeoutMs {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: fmt.Sprintf("timeout_ms exceeds maximum %d", maxWaitTimeoutMs)}})
		return
	}

	proc, ok := s.kern.GetProcess(req.PID)
	if !ok {
		// Not in the procTable: either already reaped past the Dead-TTL
		// window (answer from history, AC2) or never existed (NOT_FOUND, AC5).
		s.respondWaitFromHistory(conn, req.PID)
		return
	}

	// Watchdog (裁决 3): dispatch already returned, so this handler owns the
	// conn read side. A waiting client sends nothing more on this
	// connection; a Read only returns when the peer closes (EOF/err) — or,
	// on protocol violation, delivers bytes we deliberately ignore. The
	// deferred conn.Close in handleConn unblocks the residual Read on the
	// normal return path, so the goroutine never leaks.
	clientGone := make(chan struct{})
	go func() {
		buf := make([]byte, 1)
		for {
			if _, err := conn.Read(buf); err != nil {
				close(clientGone)
				return
			}
		}
	}()

	var timeoutCh <-chan time.Time
	if req.TimeoutMs > 0 {
		t := time.NewTimer(time.Duration(req.TimeoutMs) * time.Millisecond)
		defer t.Stop()
		timeoutCh = t.C
	}

	select {
	case <-proc.TerminatedCh():
		// Exit happens-before close: GetProcInfo now reads ExitCodeSet=true.
		info, err := s.kern.GetProcInfo(req.PID)
		if err != nil {
			// Dead-TTL sweep raced us out of the procTable. cleanupExpiredDead
			// adds to procHistory before removing, so the fallback is seamless.
			s.respondWaitFromHistory(conn, req.PID)
			return
		}
		writeWaitResponse(conn, waitResponseFromInfo(req.PID, info))
	case <-timeoutCh:
		// 裁决 4: business result, not a protocol error. Process untouched —
		// the client may immediately wait the same PID again (pollable).
		writeWaitResponse(conn, WaitResponse{PID: req.PID, TimedOut: true})
	case <-clientGone:
		// Peer already hung up — nothing to write.
	}
}

// respondWaitFromHistory answers a wait for a PID absent from the procTable:
// a procHistory hit means the process already terminated (answer immediately,
// AC2); a miss is NOT_FOUND (AC5).
func (s *Server) respondWaitFromHistory(conn net.Conn, pid types.PID) {
	info := s.kern.FindHistoryByPID(pid)
	if info == nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: fmt.Sprintf("process %d not found", pid)}})
		return
	}
	writeWaitResponse(conn, waitResponseFromInfo(pid, info))
}

// waitResponseFromInfo projects a ProcInfo snapshot into the wire response.
//
// 裁决 5: an entry with ExitCodeSet=false is a daemon-crash leftover or
// legacy data — the process is gone but its exit code was never recorded.
// Never propagate the zero value 0 (reporting a crash as success is a safety
// hazard): degrade to exit code 1 and label the reason unknown.
func waitResponseFromInfo(pid types.PID, info *vfs.ProcInfo) WaitResponse {
	if !info.ExitCodeSet {
		reason := info.ExitReason
		if reason == "" {
			reason = "unknown (no exit code recorded)"
		}
		return WaitResponse{PID: pid, ExitCode: 1, ExitReason: reason}
	}
	return WaitResponse{PID: pid, ExitCode: info.ExitCode, ExitReason: info.ExitReason}
}

// writeWaitResponse marshals a WaitResponse into an OK envelope.
func writeWaitResponse(conn net.Conn, resp WaitResponse) {
	body, err := json.Marshal(resp)
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "internal", Message: fmt.Sprintf("marshal wait: %v", err)}})
		return
	}
	writeResponse(conn, Response{OK: true, Payload: body})
}
