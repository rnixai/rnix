package ipc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/gonewx/crux/internal/types"
	"github.com/gonewx/crux/vfs"
)

// Client connects to the crux daemon over a Unix socket.
type Client struct {
	conn    net.Conn
	scanner *bufio.Scanner
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
	return &Client{conn: conn, scanner: scanner}, nil
}

// Close closes the client connection.
func (c *Client) Close() error {
	return c.conn.Close()
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

// Kill sends a kill signal to the specified process.
func (c *Client) Kill(pid types.PID, signal types.Signal) error {
	_, err := c.call(MethodKill, KillRequest{PID: pid, Signal: signal})
	return err
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
