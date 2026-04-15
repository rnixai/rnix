package ipc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/rnixai/rnix/debug"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// Client connects to the rnix daemon over a Unix socket.
type Client struct {
	conn       net.Conn
	scanner    *bufio.Scanner
	socketPath string // saved for SendDetach (separate connection)
}

// Dial connects to the daemon at the given socket path.
func Dial(socketPath string) (*Client, error) {
	return DialTimeout(socketPath, 3*time.Second)
}

// DialTimeout connects to the daemon with a timeout.
func DialTimeout(socketPath string, timeout time.Duration) (*Client, error) {
	conn, err := net.DialTimeout("unix", socketPath, timeout)
	if err != nil {
		return nil, fmt.Errorf("ipc: dial %s: %w", socketPath, err)
	}
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<20)
	return &Client{conn: conn, scanner: scanner, socketPath: socketPath}, nil
}

// Close closes the client connection.
func (c *Client) Close() error {
	return c.conn.Close()
}

// SetReadDeadline sets a deadline for future read operations on the connection.
// A zero time value clears the deadline.
func (c *Client) SetReadDeadline(t time.Time) error {
	return c.conn.SetReadDeadline(t)
}

// Ping checks if the daemon is alive and returns its version.
func (c *Client) Ping() (string, error) {
	resp, err := c.call(MethodPing, nil)
	if err != nil {
		return "", err
	}
	var pr PingResponse
	if err := json.Unmarshal(resp.Payload, &pr); err != nil {
		return "", fmt.Errorf("ipc: unmarshal ping: %w", err)
	}
	return pr.Version, nil
}

// ListProcs returns all processes visible to the daemon.
func (c *Client) ListProcs() ([]vfs.ProcInfo, error) {
	resp, err := c.call(MethodListProcs, nil)
	if err != nil {
		return nil, err
	}
	var lr ListProcsResponse
	if err := json.Unmarshal(resp.Payload, &lr); err != nil {
		return nil, fmt.Errorf("ipc: unmarshal list_procs: %w", err)
	}
	result := make([]vfs.ProcInfo, len(lr.Processes))
	for i, w := range lr.Processes {
		result[i] = WireToProcInfo(w)
	}
	return result, nil
}

// ListAllProcs returns all processes (active + historical) from the daemon.
func (c *Client) ListAllProcs() ([]vfs.ProcInfo, error) {
	resp, err := c.call(MethodListAllProcs, nil)
	if err != nil {
		return nil, err
	}
	var lr ListProcsResponse
	if err := json.Unmarshal(resp.Payload, &lr); err != nil {
		return nil, fmt.Errorf("ipc: unmarshal list_all_procs: %w", err)
	}
	result := make([]vfs.ProcInfo, len(lr.Processes))
	for i, w := range lr.Processes {
		result[i] = WireToProcInfo(w)
	}
	return result, nil
}

// CtxProfile returns context profiling results for the given PID.
func (c *Client) CtxProfile(pid types.PID) (*debug.CtxProfileResult, error) {
	resp, err := c.call(MethodCtxProfile, CtxProfileRequest{PID: pid})
	if err != nil {
		return nil, err
	}
	var result debug.CtxProfileResult
	if err := json.Unmarshal(resp.Payload, &result); err != nil {
		return nil, fmt.Errorf("ipc: unmarshal ctx_profile: %w", err)
	}
	return &result, nil
}

// CtxGrowth returns context growth prediction for the given PID.
func (c *Client) CtxGrowth(pid types.PID) (*debug.GrowthPrediction, error) {
	resp, err := c.call(MethodCtxGrowth, CtxGrowthRequest{PID: pid})
	if err != nil {
		return nil, err
	}
	var result debug.GrowthPrediction
	if err := json.Unmarshal(resp.Payload, &result); err != nil {
		return nil, fmt.Errorf("ipc: unmarshal ctx_growth: %w", err)
	}
	return &result, nil
}

// Kill sends a kill signal to the specified process.
func (c *Client) Kill(pid types.PID, signal types.Signal) error {
	_, err := c.call(MethodKill, KillRequest{PID: pid, Signal: signal})
	return err
}

// SignalTree sends a signal to the target process and all its living descendants.
func (c *Client) SignalTree(pid types.PID, signal types.Signal) (*SignalTreeResponse, error) {
	resp, err := c.call(MethodSignalTree, SignalTreeRequest{PID: pid, Signal: signal})
	if err != nil {
		return nil, err
	}
	var result SignalTreeResponse
	if err := json.Unmarshal(resp.Payload, &result); err != nil {
		return nil, fmt.Errorf("ipc: unmarshal signal_tree response: %w", err)
	}
	return &result, nil
}

// Suspend suspends the specified running process.
func (c *Client) Suspend(pid types.PID) (*SuspendResponse, error) {
	resp, err := c.call(MethodSuspend, SuspendRequest{PID: pid})
	if err != nil {
		return nil, err
	}
	var result SuspendResponse
	if err := json.Unmarshal(resp.Payload, &result); err != nil {
		return nil, fmt.Errorf("ipc: unmarshal suspend response: %w", err)
	}
	return &result, nil
}

// Resume resumes a suspended process from its checkpoint.
func (c *Client) Resume(uuid string) (*ResumeResponse, error) {
	resp, err := c.call(MethodResume, ResumeRequest{UUID: uuid})
	if err != nil {
		return nil, err
	}
	var result ResumeResponse
	if err := json.Unmarshal(resp.Payload, &result); err != nil {
		return nil, fmt.Errorf("ipc: unmarshal resume response: %w", err)
	}
	return &result, nil
}

// SpawnAndWatch spawns a process and streams events until completion.
// The onEvent callback is called for each StreamEvent. Returns the final SpawnResponse PID
// and the complete ProgressPayload (from the complete/error event).
func (c *Client) SpawnAndWatch(req SpawnRequest, onEvent func(StreamEvent)) (types.PID, *ProgressPayload, error) {
	if err := c.sendRequest(MethodSpawn, req); err != nil {
		return 0, nil, err
	}

	if !c.scanner.Scan() {
		return 0, nil, fmt.Errorf("ipc: no initial spawn response")
	}
	var resp Response
	if err := json.Unmarshal(c.scanner.Bytes(), &resp); err != nil {
		return 0, nil, fmt.Errorf("ipc: unmarshal spawn response: %w", err)
	}
	if !resp.OK {
		msg := "spawn failed"
		if resp.Error != nil {
			msg = resp.Error.Message
		}
		return 0, nil, fmt.Errorf("ipc: %s", msg)
	}

	var sr SpawnResponse
	if err := json.Unmarshal(resp.Payload, &sr); err != nil {
		return 0, nil, fmt.Errorf("ipc: unmarshal spawn payload: %w", err)
	}

	var finalPayload *ProgressPayload
	for c.scanner.Scan() {
		var ev StreamEvent
		if err := json.Unmarshal(c.scanner.Bytes(), &ev); err != nil {
			continue
		}

		if onEvent != nil {
			onEvent(ev)
		}

		if ev.Type == StreamComplete || ev.Type == StreamError {
			var pp ProgressPayload
			if err := json.Unmarshal(ev.Payload, &pp); err == nil {
				finalPayload = &pp
			}
			break
		}
	}

	return sr.PID, finalPayload, nil
}

// SpawnPipelineAndWatch spawns a pipeline of processes and streams events until completion.
// The onEvent callback is called for each StreamEvent (progress per stage).
// Returns the SpawnPipelineResponse with all stage results.
func (c *Client) SpawnPipelineAndWatch(req SpawnPipelineRequest, onEvent func(StreamEvent)) (*SpawnPipelineResponse, error) {
	if err := c.sendRequest(MethodSpawnPipeline, req); err != nil {
		return nil, err
	}

	if !c.scanner.Scan() {
		return nil, fmt.Errorf("ipc: no initial pipeline response")
	}
	var resp Response
	if err := json.Unmarshal(c.scanner.Bytes(), &resp); err != nil {
		return nil, fmt.Errorf("ipc: unmarshal pipeline response: %w", err)
	}
	if !resp.OK {
		msg := "pipeline spawn failed"
		if resp.Error != nil {
			msg = resp.Error.Message
		}
		return nil, fmt.Errorf("ipc: %s", msg)
	}

	var finalResp *SpawnPipelineResponse
	for c.scanner.Scan() {
		var ev StreamEvent
		if err := json.Unmarshal(c.scanner.Bytes(), &ev); err != nil {
			continue
		}

		if onEvent != nil {
			onEvent(ev)
		}

		if ev.Type == StreamComplete {
			var pr SpawnPipelineResponse
			if err := json.Unmarshal(ev.Payload, &pr); err == nil {
				finalResp = &pr
			}
			break
		}
		if ev.Type == StreamError {
			var ep ErrorPayload
			if err := json.Unmarshal(ev.Payload, &ep); err == nil {
				return nil, fmt.Errorf("ipc: pipeline error: %s", ep.Message)
			}
			return nil, fmt.Errorf("ipc: pipeline failed")
		}
	}

	return finalResp, nil
}

// ExecScriptAndWatch sends a script to the daemon for execution and streams events.
// The onEvent callback is called for each StreamEvent (progress per script step).
// Returns the ExecScriptResponse with final results.
func (c *Client) ExecScriptAndWatch(req ExecScriptRequest, onEvent func(StreamEvent)) (*ExecScriptResponse, error) {
	if err := c.sendRequest(MethodExecScript, req); err != nil {
		return nil, err
	}

	if !c.scanner.Scan() {
		return nil, fmt.Errorf("ipc: no initial exec_script response")
	}
	var resp Response
	if err := json.Unmarshal(c.scanner.Bytes(), &resp); err != nil {
		return nil, fmt.Errorf("ipc: unmarshal exec_script response: %w", err)
	}
	if !resp.OK {
		msg := "exec_script failed"
		if resp.Error != nil {
			msg = resp.Error.Message
		}
		return nil, fmt.Errorf("ipc: %s", msg)
	}

	var finalResp *ExecScriptResponse
	for c.scanner.Scan() {
		var ev StreamEvent
		if err := json.Unmarshal(c.scanner.Bytes(), &ev); err != nil {
			continue
		}

		if onEvent != nil {
			onEvent(ev)
		}

		if ev.Type == StreamComplete {
			var sr ExecScriptResponse
			if err := json.Unmarshal(ev.Payload, &sr); err == nil {
				finalResp = &sr
			}
			break
		}
		if ev.Type == StreamError {
			var ep ErrorPayload
			if err := json.Unmarshal(ev.Payload, &ep); err == nil {
				return nil, fmt.Errorf("ipc: script error: %s", ep.Message)
			}
			return nil, fmt.Errorf("ipc: script failed")
		}
	}

	return finalResp, nil
}

// AttachDebug streams SyscallEvents from the specified process.
// The onEvent callback is called for each SyscallEventWire. Blocks until the stream ends.
func (c *Client) AttachDebug(pid types.PID, onEvent func(SyscallEventWire)) error {
	if err := c.sendRequest(MethodAttachDebug, AttachDebugRequest{PID: pid}); err != nil {
		return err
	}

	if !c.scanner.Scan() {
		return fmt.Errorf("ipc: no attach_debug response")
	}
	var resp Response
	if err := json.Unmarshal(c.scanner.Bytes(), &resp); err != nil {
		return fmt.Errorf("ipc: unmarshal attach_debug response: %w", err)
	}
	if !resp.OK {
		msg := "attach failed"
		if resp.Error != nil {
			msg = resp.Error.Message
		}
		return fmt.Errorf("ipc: %s", msg)
	}

	for c.scanner.Scan() {
		var ev StreamEvent
		if err := json.Unmarshal(c.scanner.Bytes(), &ev); err != nil {
			continue
		}

		if ev.Type == StreamEOF {
			break
		}

		if ev.Type == StreamSyscallEvent && onEvent != nil {
			var sew SyscallEventWire
			if err := json.Unmarshal(ev.Payload, &sew); err == nil {
				onEvent(sew)
			}
		}
	}

	return nil
}

// AttachLog streams LogEntries from the specified process.
// The onEntry callback is called for each LogEntryWire. Blocks until the stream ends.
func (c *Client) AttachLog(pid types.PID, onEntry func(LogEntryWire)) error {
	if err := c.sendRequest(MethodAttachLog, AttachLogRequest{PID: pid}); err != nil {
		return err
	}

	if !c.scanner.Scan() {
		return fmt.Errorf("ipc: no attach_log response")
	}
	var resp Response
	if err := json.Unmarshal(c.scanner.Bytes(), &resp); err != nil {
		return fmt.Errorf("ipc: unmarshal attach_log response: %w", err)
	}
	if !resp.OK {
		msg := "attach failed"
		if resp.Error != nil {
			msg = resp.Error.Message
		}
		return fmt.Errorf("ipc: %s", msg)
	}

	for c.scanner.Scan() {
		var ev StreamEvent
		if err := json.Unmarshal(c.scanner.Bytes(), &ev); err != nil {
			continue
		}

		if ev.Type == StreamEOF {
			break
		}

		if ev.Type == StreamLogEntry && onEntry != nil {
			var lew LogEntryWire
			if err := json.Unmarshal(ev.Payload, &lew); err == nil {
				onEntry(lew)
			}
		}
	}

	return nil
}

// Shutdown requests the daemon to shut down gracefully.
func (c *Client) Shutdown() error {
	_, err := c.call(MethodShutdown, nil)
	return err
}

// AnswerUser sends an answer for a pending ask_user request.
// Uses a separate connection to avoid interfering with the spawn stream.
func AnswerUser(socketPath string, requestID string, answers json.RawMessage) error {
	client, err := Dial(socketPath)
	if err != nil {
		return fmt.Errorf("dial for answer_user: %w", err)
	}
	defer client.Close()

	_, err = client.call(MethodAnswerUser, AnswerUserRequest{
		RequestID: requestID,
		Answers:   answers,
	})
	return err
}

// RecordStart starts execution recording for a process.
// Returns the recording ID.
func (c *Client) RecordStart(pid types.PID) (string, error) {
	resp, err := c.call(MethodRecordStart, RecordStartRequest{PID: pid})
	if err != nil {
		return "", err
	}
	var rr RecordStartResponse
	if err := json.Unmarshal(resp.Payload, &rr); err != nil {
		return "", fmt.Errorf("ipc: unmarshal record_start: %w", err)
	}
	return rr.RecordID, nil
}

// RecordStop stops execution recording for a process.
// Returns the number of events captured.
func (c *Client) RecordStop(pid types.PID) (uint64, error) {
	resp, err := c.call(MethodRecordStop, RecordStopRequest{PID: pid})
	if err != nil {
		return 0, err
	}
	var rr RecordStopResponse
	if err := json.Unmarshal(resp.Payload, &rr); err != nil {
		return 0, fmt.Errorf("ipc: unmarshal record_stop: %w", err)
	}
	return rr.EventCount, nil
}

// RecordList returns metadata for all recording sessions.
func (c *Client) RecordList() ([]RecordMetadataWire, error) {
	resp, err := c.call(MethodRecordList, nil)
	if err != nil {
		return nil, err
	}
	var rr RecordListResponse
	if err := json.Unmarshal(resp.Payload, &rr); err != nil {
		return nil, fmt.Errorf("ipc: unmarshal record_list: %w", err)
	}
	return rr.Records, nil
}

// ReplayLoad loads a recording's metadata for replay.
func (c *Client) ReplayLoad(recordID string) (*ReplayLoadResponse, error) {
	resp, err := c.call(MethodReplayLoad, ReplayLoadRequest{RecordID: recordID})
	if err != nil {
		return nil, err
	}
	var rr ReplayLoadResponse
	if err := json.Unmarshal(resp.Payload, &rr); err != nil {
		return nil, fmt.Errorf("ipc: unmarshal replay_load: %w", err)
	}
	return &rr, nil
}

// ForkContinue sends a fork-continue request to the daemon, which creates a new
// process from the given fork context and streams events until completion.
// Returns the new process PID and PPID info.
func (c *Client) ForkContinue(req ForkContinueRequest, onEvent func(StreamEvent)) (*ForkContinueResponse, error) {
	if err := c.sendRequest(MethodForkContinue, req); err != nil {
		return nil, err
	}

	if !c.scanner.Scan() {
		return nil, fmt.Errorf("ipc: no fork_continue response")
	}
	var resp Response
	if err := json.Unmarshal(c.scanner.Bytes(), &resp); err != nil {
		return nil, fmt.Errorf("ipc: unmarshal fork_continue response: %w", err)
	}
	if !resp.OK {
		msg := "fork_continue failed"
		if resp.Error != nil {
			msg = resp.Error.Message
		}
		return nil, fmt.Errorf("ipc: %s", msg)
	}

	var fcr ForkContinueResponse
	if err := json.Unmarshal(resp.Payload, &fcr); err != nil {
		return nil, fmt.Errorf("ipc: unmarshal fork_continue payload: %w", err)
	}

	// Stream events in background until completion (non-blocking for caller)
	if onEvent != nil {
		go func() {
			for c.scanner.Scan() {
				var ev StreamEvent
				if err := json.Unmarshal(c.scanner.Bytes(), &ev); err != nil {
					continue
				}
				onEvent(ev)
				if ev.Type == StreamComplete || ev.Type == StreamError {
					break
				}
			}
		}()
	}

	return &fcr, nil
}

// AttachGdb attaches to a process for interactive debugging, receiving both
// syscall events and log entries via a unified stream. Returns the initial
// process metadata snapshot. The onEvent callback is called for each GdbEvent
// in a background goroutine.
//
// AttachGdb waits for the event stream to end naturally (EOF from server).
// If the stream does not end within a short window, it returns immediately
// so the caller can interact with the session (e.g., call SendDetach).
func (c *Client) AttachGdb(pid types.PID, onEvent func(GdbEvent)) (*AttachGdbResponse, error) {
	if err := c.sendRequest(MethodAttachGdb, AttachGdbRequest{PID: pid}); err != nil {
		return nil, err
	}

	if !c.scanner.Scan() {
		return nil, fmt.Errorf("ipc: no attach_gdb response")
	}
	var resp Response
	if err := json.Unmarshal(c.scanner.Bytes(), &resp); err != nil {
		return nil, fmt.Errorf("ipc: unmarshal attach_gdb response: %w", err)
	}
	if !resp.OK {
		msg := "attach failed"
		if resp.Error != nil {
			msg = resp.Error.Message
		}
		return nil, fmt.Errorf("ipc: %s", msg)
	}

	var info AttachGdbResponse
	if err := json.Unmarshal(resp.Payload, &info); err != nil {
		return nil, fmt.Errorf("ipc: unmarshal attach_gdb payload: %w", err)
	}

	// Stream events in background goroutine
	done := make(chan struct{})
	go func() {
		defer close(done)
		for c.scanner.Scan() {
			var ev GdbEvent
			if err := json.Unmarshal(c.scanner.Bytes(), &ev); err != nil {
				continue
			}

			if ev.Type == StreamEOF {
				break
			}

			if onEvent != nil {
				onEvent(ev)
			}
		}
	}()

	// Wait for the stream to end, or return early for interactive sessions.
	// Short-lived streams (process exit, channel close) complete before the
	// timeout and all events are guaranteed delivered. Long-lived streams
	// (interactive attach) return after the timeout to allow SendDetach calls.
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
	}

	return &info, nil
}

// SendDetach sends a detach request to the server for the given PID.
// This opens a separate connection to send the detach command, which
// triggers server-side EOF on the attach stream and unblocks AttachGdb.
func (c *Client) SendDetach(pid types.PID) error {
	detachConn, err := DialTimeout(c.socketPath, 3*time.Second)
	if err != nil {
		return fmt.Errorf("ipc: detach dial: %w", err)
	}
	defer detachConn.Close()
	_, err = detachConn.call(MethodDetachGdb, DetachGdbRequest{PID: pid})
	return err
}

// SendGdbCommand sends a gdb command to a process via a separate connection.
// This allows sending commands (break, delete, continue, info) while the
// gdb attach event stream remains active on the primary connection.
func (c *Client) SendGdbCommand(pid types.PID, command string, args []string) (*GdbCommandResponse, error) {
	cmdConn, err := DialTimeout(c.socketPath, 3*time.Second)
	if err != nil {
		return nil, fmt.Errorf("ipc: gdb_command dial: %w", err)
	}
	defer cmdConn.Close()
	resp, err := cmdConn.call(MethodGdbCommand, GdbCommandRequest{PID: pid, Command: command, Args: args})
	if err != nil {
		return nil, err
	}
	var gcr GdbCommandResponse
	if err := json.Unmarshal(resp.Payload, &gcr); err != nil {
		return nil, fmt.Errorf("ipc: unmarshal gdb_command response: %w", err)
	}
	return &gcr, nil
}

// call sends a request and reads a single response.
func (c *Client) call(method Method, payload any) (*Response, error) {
	if err := c.sendRequest(method, payload); err != nil {
		return nil, err
	}
	return c.readResponse()
}

func (c *Client) sendRequest(method Method, payload any) error {
	var rawPayload json.RawMessage
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("ipc: marshal payload: %w", err)
		}
		rawPayload = data
	}
	req := Request{Method: method, Payload: rawPayload}
	enc := json.NewEncoder(c.conn)
	if err := enc.Encode(req); err != nil {
		return fmt.Errorf("ipc: send request: %w", err)
	}
	return nil
}

func (c *Client) readResponse() (*Response, error) {
	if !c.scanner.Scan() {
		if err := c.scanner.Err(); err != nil {
			return nil, fmt.Errorf("ipc: read response: %w", err)
		}
		return nil, fmt.Errorf("ipc: connection closed")
	}
	var resp Response
	if err := json.Unmarshal(c.scanner.Bytes(), &resp); err != nil {
		return nil, fmt.Errorf("ipc: unmarshal response: %w", err)
	}
	if !resp.OK {
		msg := "request failed"
		if resp.Error != nil {
			msg = fmt.Sprintf("[%s] %s", resp.Error.Code, resp.Error.Message)
		}
		return nil, fmt.Errorf("ipc: %s", msg)
	}
	return &resp, nil
}

// --- Intent Client Methods ---

// ApplyIntentAndWatch sends an apply_intent request and streams events.
func (c *Client) ApplyIntentAndWatch(req ApplyIntentRequest, onEvent func(StreamEvent)) (*ApplyIntentResponse, error) {
	if err := c.sendRequest(MethodApplyIntent, req); err != nil {
		return nil, err
	}

	resp, err := c.readResponse()
	if err != nil {
		return nil, err
	}

	var applyResp ApplyIntentResponse
	if err := json.Unmarshal(resp.Payload, &applyResp); err != nil {
		return nil, fmt.Errorf("ipc: unmarshal apply_intent response: %w", err)
	}

	for c.scanner.Scan() {
		var ev StreamEvent
		if err := json.Unmarshal(c.scanner.Bytes(), &ev); err != nil {
			continue
		}
		if onEvent != nil {
			onEvent(ev)
		}
		if ev.Type == StreamIntentComplete || ev.Type == StreamError {
			break
		}
	}

	return &applyResp, nil
}

// ConfirmIntent sends a confirmation for an intent awaiting confirmation.
func (c *Client) ConfirmIntent(intentID string, confirm bool) error {
	req := Request{
		Method:  MethodIntentConfirm,
		Payload: marshalPayload(IntentConfirmRequest{IntentID: intentID, Confirm: confirm}),
	}
	enc := json.NewEncoder(c.conn)
	return enc.Encode(req)
}

// IntentStatus queries the status of an intent or all active intents.
func (c *Client) IntentStatus(intentID string) (*IntentStatusResponse, error) {
	resp, err := c.call(MethodIntentStatus, IntentStatusRequest{IntentID: intentID})
	if err != nil {
		return nil, err
	}
	var statusResp IntentStatusResponse
	if err := json.Unmarshal(resp.Payload, &statusResp); err != nil {
		return nil, fmt.Errorf("ipc: unmarshal intent_status response: %w", err)
	}
	return &statusResp, nil
}

// ApplyIncrementalIntent sends an incremental update to an existing intent.
func (c *Client) ApplyIncrementalIntent(req ApplyIncrementalIntentRequest) (*ApplyIncrementalIntentResponse, error) {
	resp, err := c.call(MethodApplyIncrementalIntent, req)
	if err != nil {
		return nil, err
	}
	var result ApplyIncrementalIntentResponse
	if err := json.Unmarshal(resp.Payload, &result); err != nil {
		return nil, fmt.Errorf("ipc: unmarshal apply_incremental_intent response: %w", err)
	}
	return &result, nil
}

// IntentList queries all intents (active + completed).
func (c *Client) IntentList() (*IntentStatusResponse, error) {
	resp, err := c.call(MethodIntentList, nil)
	if err != nil {
		return nil, err
	}
	var statusResp IntentStatusResponse
	if err := json.Unmarshal(resp.Payload, &statusResp); err != nil {
		return nil, fmt.Errorf("ipc: unmarshal intent_list response: %w", err)
	}
	return &statusResp, nil
}

// GetStepDetail returns the full prompt and step detail for a specific process step (Story 27.2).
func (c *Client) GetStepDetail(pid types.PID, step int) (*GetStepDetailResponse, error) {
	resp, err := c.call(MethodGetStepDetail, GetStepDetailRequest{PID: pid, Step: step})
	if err != nil {
		return nil, err
	}
	var result GetStepDetailResponse
	if err := json.Unmarshal(resp.Payload, &result); err != nil {
		return nil, fmt.Errorf("ipc: unmarshal get_step_detail: %w", err)
	}
	return &result, nil
}

// SpawnDetached sends a spawn request and returns the PID without watching events.
// The process runs in the daemon; events are not consumed.
func (c *Client) SpawnDetached(req SpawnRequest) (types.PID, error) {
	if err := c.sendRequest(MethodSpawn, req); err != nil {
		return 0, err
	}

	if !c.scanner.Scan() {
		return 0, fmt.Errorf("ipc: no initial spawn response")
	}
	var resp Response
	if err := json.Unmarshal(c.scanner.Bytes(), &resp); err != nil {
		return 0, fmt.Errorf("ipc: unmarshal spawn response: %w", err)
	}
	if !resp.OK {
		msg := "spawn failed"
		if resp.Error != nil {
			msg = resp.Error.Message
		}
		return 0, fmt.Errorf("ipc: %s", msg)
	}

	var sr SpawnResponse
	if err := json.Unmarshal(resp.Payload, &sr); err != nil {
		return 0, fmt.Errorf("ipc: unmarshal spawn payload: %w", err)
	}
	return sr.PID, nil
}

// GetStepDetailByUUID returns the full prompt and step detail for a specific process step by UUID (Story 28.3).
func (c *Client) GetStepDetailByUUID(uuid string, step int) (*GetStepDetailResponse, error) {
	resp, err := c.call(MethodGetStepDetail, GetStepDetailRequest{UUID: uuid, Step: step})
	if err != nil {
		return nil, err
	}
	var result GetStepDetailResponse
	if err := json.Unmarshal(resp.Payload, &result); err != nil {
		return nil, fmt.Errorf("ipc: unmarshal get_step_detail: %w", err)
	}
	return &result, nil
}

// ListSteps returns step summaries for a process (Story 27.3).
// If afterStep > 0, only steps after that step number are returned.
func (c *Client) ListSteps(pid types.PID, afterStep int) (*ListStepsResponse, error) {
	resp, err := c.call(MethodListSteps, ListStepsRequest{PID: pid, AfterStep: afterStep})
	if err != nil {
		return nil, err
	}
	var result ListStepsResponse
	if err := json.Unmarshal(resp.Payload, &result); err != nil {
		return nil, fmt.Errorf("ipc: unmarshal list_steps: %w", err)
	}
	return &result, nil
}

// ListStepsByUUID returns step summaries for a process by UUID (Story 28.3).
func (c *Client) ListStepsByUUID(uuid string, afterStep int) (*ListStepsResponse, error) {
	resp, err := c.call(MethodListSteps, ListStepsRequest{UUID: uuid, AfterStep: afterStep})
	if err != nil {
		return nil, err
	}
	var result ListStepsResponse
	if err := json.Unmarshal(resp.Payload, &result); err != nil {
		return nil, fmt.Errorf("ipc: unmarshal list_steps: %w", err)
	}
	return &result, nil
}

// Lineage returns the differentiation lineage for the given PID.
func (c *Client) Lineage(pid types.PID) (*LineageResponse, error) {
	resp, err := c.call(MethodLineage, LineageRequest{PID: pid})
	if err != nil {
		return nil, err
	}
	var result LineageResponse
	if err := json.Unmarshal(resp.Payload, &result); err != nil {
		return nil, fmt.Errorf("ipc: unmarshal lineage response: %w", err)
	}
	return &result, nil
}

// ProviderStatus returns the health status of all registered providers.
func (c *Client) ProviderStatus() ([]ProviderStatusWire, error) {
	resp, err := c.call(MethodProviderStatus, nil)
	if err != nil {
		return nil, err
	}
	var pr ProviderStatusResponse
	if err := json.Unmarshal(resp.Payload, &pr); err != nil {
		return nil, fmt.Errorf("ipc: unmarshal provider_status: %w", err)
	}
	return pr.Providers, nil
}

// BudgetStatus returns the budget pool status for the given process group.
func (c *Client) BudgetStatus(groupID types.PGID) (*BudgetStatusResponse, error) {
	resp, err := c.call(MethodBudgetStatus, BudgetStatusRequest{GroupID: groupID})
	if err != nil {
		return nil, err
	}
	var result BudgetStatusResponse
	if err := json.Unmarshal(resp.Payload, &result); err != nil {
		return nil, fmt.Errorf("ipc: unmarshal budget_status: %w", err)
	}
	return &result, nil
}

// SLAStatus returns the SLA evaluation results for the given process group.
func (c *Client) SLAStatus(groupID types.PGID) (*SLAStatusResponse, error) {
	resp, err := c.call(MethodSLAStatus, SLAStatusRequest{GroupID: groupID})
	if err != nil {
		return nil, err
	}
	var result SLAStatusResponse
	if err := json.Unmarshal(resp.Payload, &result); err != nil {
		return nil, fmt.Errorf("ipc: unmarshal sla_status: %w", err)
	}
	return &result, nil
}

// ReputationStatus returns reputation summaries for the specified agent or all agents.
func (c *Client) ReputationStatus(agentName string) (*ReputationStatusResponse, error) {
	resp, err := c.call(MethodReputationStatus, ReputationStatusRequest{AgentName: agentName})
	if err != nil {
		return nil, err
	}
	var result ReputationStatusResponse
	if err := json.Unmarshal(resp.Payload, &result); err != nil {
		return nil, fmt.Errorf("ipc: unmarshal reputation_status: %w", err)
	}
	return &result, nil
}

// SynergyList returns the skill combination matrix summaries (Story 21.5).
func (c *Client) SynergyList() (*SynergyListResponse, error) {
	resp, err := c.call(MethodSynergyList, SynergyListRequest{})
	if err != nil {
		return nil, err
	}
	var result SynergyListResponse
	if err := json.Unmarshal(resp.Payload, &result); err != nil {
		return nil, fmt.Errorf("ipc: unmarshal synergy_list: %w", err)
	}
	return &result, nil
}

// ImmuneStatus returns the immune daemon status and behavior profiles (Story 22.1).
func (c *Client) ImmuneStatus() (*ImmuneStatusResponse, error) {
	resp, err := c.call(MethodImmuneStatus, ImmuneStatusRequest{})
	if err != nil {
		return nil, err
	}
	var result ImmuneStatusResponse
	if err := json.Unmarshal(resp.Payload, &result); err != nil {
		return nil, fmt.Errorf("ipc: unmarshal immune_status: %w", err)
	}
	return &result, nil
}

// ImmuneResume resumes a suspended process and clears its alert (Story 22.2).
func (c *Client) ImmuneResume(pid uint64) (*ImmuneResumeResponse, error) {
	resp, err := c.call(MethodImmuneResume, ImmuneResumeRequest{PID: pid})
	if err != nil {
		return nil, err
	}
	var result ImmuneResumeResponse
	if err := json.Unmarshal(resp.Payload, &result); err != nil {
		return nil, fmt.Errorf("ipc: unmarshal immune_resume: %w", err)
	}
	return &result, nil
}

// SimilarityQuery returns similar agents for the given agent (Story 22.4).
func (c *Client) SimilarityQuery(agentName string, minScore float64) (*SimilarityQueryResponse, error) {
	resp, err := c.call(MethodSimilarityQuery, SimilarityQueryRequest{AgentName: agentName, MinScore: minScore})
	if err != nil {
		return nil, err
	}
	var result SimilarityQueryResponse
	if err := json.Unmarshal(resp.Payload, &result); err != nil {
		return nil, fmt.Errorf("ipc: unmarshal similarity_query: %w", err)
	}
	return &result, nil
}

// TopologyQuery returns the collaboration topology (Story 22.5).
func (c *Client) TopologyQuery() (*TopologyQueryResponse, error) {
	resp, err := c.call(MethodTopologyQuery, TopologyQueryRequest{})
	if err != nil {
		return nil, err
	}
	var result TopologyQueryResponse
	if err := json.Unmarshal(resp.Payload, &result); err != nil {
		return nil, fmt.Errorf("ipc: unmarshal topology_query: %w", err)
	}
	return &result, nil
}

// GetProcDetail returns full process detail including env, skills, FD table, context stats (Story 27.6).
func (c *Client) GetProcDetail(pid types.PID, uuid ...string) (*GetProcDetailResponse, error) {
	req := GetProcDetailRequest{PID: pid}
	if len(uuid) > 0 {
		req.UUID = uuid[0]
	}
	resp, err := c.call(MethodGetProcDetail, req)
	if err != nil {
		return nil, err
	}
	var result GetProcDetailResponse
	if err := json.Unmarshal(resp.Payload, &result); err != nil {
		return nil, fmt.Errorf("ipc: unmarshal get_proc_detail: %w", err)
	}
	return &result, nil
}

// ListEvents returns persisted syscall events from disk for a process.
func (c *Client) ListEvents(pid types.PID, uuid string) ([]SyscallEventWire, error) {
	resp, err := c.call(MethodListEvents, ListEventsRequest{PID: pid, UUID: uuid})
	if err != nil {
		return nil, err
	}
	var result ListEventsResponse
	if err := json.Unmarshal(resp.Payload, &result); err != nil {
		return nil, fmt.Errorf("ipc: unmarshal list_events: %w", err)
	}
	return result.Events, nil
}

// TraceList returns all trace summaries (Story 27.9).
func (c *Client) TraceList() ([]TraceSummaryWire, error) {
	resp, err := c.call(MethodTraceList, nil)
	if err != nil {
		return nil, err
	}
	var result TraceListResponse
	if err := json.Unmarshal(resp.Payload, &result); err != nil {
		return nil, fmt.Errorf("ipc: unmarshal trace_list: %w", err)
	}
	return result.Traces, nil
}

// TraceTree returns the span tree for a given trace ID (Story 27.9).
func (c *Client) TraceTree(traceID string) (*SpanTreeWire, error) {
	resp, err := c.call(MethodTraceTree, TraceTreeRequest{TraceID: traceID})
	if err != nil {
		return nil, err
	}
	var result TraceTreeResponse
	if err := json.Unmarshal(resp.Payload, &result); err != nil {
		return nil, fmt.Errorf("ipc: unmarshal trace_tree: %w", err)
	}
	return result.Tree, nil
}

func marshalPayload(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}

// HeartbeatStatus returns the heartbeat monitor status from the daemon (Story 30.6).
func (c *Client) HeartbeatStatus() (*HeartbeatStatusResponse, error) {
	resp, err := c.call(MethodHeartbeatStatus, HeartbeatStatusRequest{})
	if err != nil {
		return nil, err
	}
	var result HeartbeatStatusResponse
	if err := json.Unmarshal(resp.Payload, &result); err != nil {
		return nil, fmt.Errorf("ipc: unmarshal heartbeat_status: %w", err)
	}
	return &result, nil
}

// Compact triggers manual context compaction for a running process (Story 31.2).
func (c *Client) Compact(pid types.PID, instructions string) (*CompactResponse, error) {
	resp, err := c.call(MethodCompact, CompactRequest{PID: pid, CustomInstructions: instructions})
	if err != nil {
		return nil, err
	}
	var result CompactResponse
	if err := json.Unmarshal(resp.Payload, &result); err != nil {
		return nil, fmt.Errorf("ipc: unmarshal compact: %w", err)
	}
	return &result, nil
}
