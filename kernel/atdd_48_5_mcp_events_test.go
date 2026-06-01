//go:build unix

package kernel

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// ATDD 48.5 AC1/AC3 — kernel emits mcp.error / mcp.reconnect at the tool-call
// boundary (Task 8).
//
// Story: _bmad-output/implementation-artifacts/48-5-mcp-health-check-stderr-reconnect.md
// Phase: 🔴 RED — `//go:build atdd_48_5_red && unix`. Run:
//            go test -tags=atdd_48_5_red -race -run TestATDD_48_5_07 ./kernel/
//        Pre-impl: compile fails on undefined types.ErrDeviceDisconnected; once
//        that lands, the tests fail BEHAVIORALLY (no mcp.error/mcp.reconnect in
//        events.jsonl) until Task 8 wires the emission in kernel/tool_exec.go.
//
// Architecture (Story §决策 1): drivers/mcp never imports kernel, so the
// PID-attributed events.jsonl entries are emitted only by the kernel at the
// VFS tool-call boundary (vfsWriteWithEvent / vfsReadWithEvent), observing the
// transport's ErrDeviceDisconnected sentinel and its ReconnectCount() delta.
// This mirrors the 48.2 ErrForceKilled → Unmount forced=true pattern.
//
// We drive the boundary directly via k.vfsOpenWithEvent + k.vfsWriteWithEvent
// (same package), avoiding the AllowedDevices gate that lives in executeToolCalls.
//
// Injection points (dev-story Task 8):
//   types.ErrDeviceDisconnected
//   kernel emits SyscallEvent "mcp.error"   {server, reason}
//   kernel emits SyscallEvent "mcp.reconnect" {server, attempt|reconnect_count}
//   detection: *types.DriverError Code==ErrDeviceDisconnected + ReconnectCount delta
// =============================================================================

// healthMockTransport implements the FULL post-48.5 vfs.MCPTransport surface
// for kernel-side event tests. callErr (when set) is returned from Call so the
// boundary observes the ErrDeviceDisconnected sentinel; reconnectCount is a
// settable counter the kernel compares across tool calls.
type healthMockTransport struct {
	callErr        error
	status         vfs.MCPStatus
	reconnectCount atomic.Int32
}

func (m *healthMockTransport) Connect(_ context.Context) error { return nil }
func (m *healthMockTransport) Call(_ context.Context, method string, _ json.RawMessage) (json.RawMessage, error) {
	if m.callErr != nil {
		return nil, m.callErr
	}
	switch method {
	case "tools/call":
		return json.RawMessage(`{"content":[{"type":"text","text":"ok"}]}`), nil
	default:
		return json.RawMessage(`{}`), nil
	}
}
func (m *healthMockTransport) Close() error                  { return nil }
func (m *healthMockTransport) Ping(_ context.Context) error { return nil }

func (m *healthMockTransport) Status() vfs.MCPStatus {
	if m.status == 0 && m.callErr == nil {
		return vfs.MCPStatusConnected
	}
	return m.status
}
func (m *healthMockTransport) Alive() bool          { return m.callErr == nil }
func (m *healthMockTransport) ToolCount() int       { return 0 }
func (m *healthMockTransport) ResourceCount() int   { return 0 }
func (m *healthMockTransport) LastCheck() time.Time { return time.Time{} }
func (m *healthMockTransport) ReconnectCount() int  { return int(m.reconnectCount.Load()) }
func (m *healthMockTransport) StderrTail() []string { return nil }

// setupMCPEventKernel builds a kernel with a mounted MCP transport + a proc
// wired with an EventWriter, returning everything the test needs to drive the
// tool-call boundary and read back events.jsonl.
func setupMCPEventKernel(t *testing.T, server string, tr vfs.MCPTransport) (*KernelImpl, *Process, string, string) {
	t.Helper()

	llmFile := &mockLLMFile{readData: makeLLMResponse("mcp-evt", 1)}
	k, _, _ := newTestKernel(t, llmFile)

	baseDir := t.TempDir()
	k.SetStepDataDir(baseDir)

	devReg := k.vfs.DeviceRegistry()
	factory := func(_ vfs.MCPConfig) (vfs.MCPTransport, error) { return tr, nil }
	mgr := vfs.NewMountManager(devReg, factory)
	k.SetMountManager(mgr)

	path := "/mnt/mcp/100-" + server
	if err := mgr.Mount(path, vfs.MCPConfig{ServerName: server, Command: "/bin/echo", TransportType: "stdio"}); err != nil {
		t.Fatalf("Mount: %v", err)
	}

	proc := NewProcess(0, "mcp event test", nil)
	if err := proc.Start(); err != nil {
		t.Fatalf("proc.Start: %v", err)
	}
	proc.MCPMounts = []string{path}

	ew, err := NewEventWriter(baseDir, proc.UUID)
	if err != nil {
		t.Fatalf("NewEventWriter: %v", err)
	}
	t.Cleanup(func() { _ = ew.Close() })
	proc.AttachEventWriter(ew)

	return k, proc, baseDir, path
}

func countEventsBySyscall(t *testing.T, baseDir, uuid, syscall string) int {
	t.Helper()
	rows, err := ReadAllEvents(filepath.Join(baseDir, "steps", uuid, "events.jsonl"))
	if err != nil {
		t.Fatalf("ReadAllEvents: %v", err)
	}
	n := 0
	for i := range rows {
		if rows[i].Syscall == syscall {
			n++
		}
	}
	return n
}

// driveMCPToolCall performs one Open → Write tool-call round through the kernel
// boundary on the given mcp tool path.
func driveMCPToolCall(t *testing.T, k *KernelImpl, proc *Process, mountPath string) error {
	t.Helper()
	fd, err := k.vfsOpenWithEvent(proc, mountPath+"/tools/echo", vfs.O_RDWR)
	if err != nil {
		return err
	}
	defer func() { _ = k.vfsCloseWithEvent(proc, fd) }()
	return k.vfsWriteWithEvent(proc, fd, []byte(`{}`))
}

// -----------------------------------------------------------------------------
// _070: tool call against a disconnected transport (Call → ErrDeviceDisconnected)
//        → kernel emits an mcp.error event with server + reason. (AC1)
// -----------------------------------------------------------------------------
func TestATDD_48_5_070_McpError_EmittedOnDeviceDisconnected(t *testing.T) {
	tr := &healthMockTransport{
		status: vfs.MCPStatusError,
		callErr: types.NewDriverError("Call", "tools/call",
			context.DeadlineExceeded, types.ErrDeviceDisconnected),
	}
	k, proc, baseDir, path := setupMCPEventKernel(t, "disconn", tr)

	// Boundary sees the sentinel; the Write itself returns an error (expected).
	_ = driveMCPToolCall(t, k, proc, path)

	if err := proc.eventWriter.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	rows, err := ReadAllEvents(filepath.Join(baseDir, "steps", proc.UUID, "events.jsonl"))
	if err != nil {
		t.Fatalf("ReadAllEvents: %v", err)
	}
	var evt *SyscallEventDisk
	for i := range rows {
		if rows[i].Syscall == "mcp.error" {
			e := rows[i]
			evt = &e
			break
		}
	}
	if evt == nil {
		t.Fatalf("no mcp.error event emitted; got %d events (Task 8 not landed)", len(rows))
	}
	if got, _ := evt.Args["server"].(string); got != "disconn" {
		t.Errorf("mcp.error args.server = %q, want %q", got, "disconn")
	}
	if _, ok := evt.Args["reason"]; !ok {
		t.Errorf("mcp.error missing args.reason; got %v", evt.Args)
	}
}

// -----------------------------------------------------------------------------
// _071: a reconnect that happened between tool calls (ReconnectCount delta) →
//        kernel emits exactly one mcp.reconnect event. (AC3)
// -----------------------------------------------------------------------------
func TestATDD_48_5_071_McpReconnect_EmittedOnReconnectDelta(t *testing.T) {
	tr := &healthMockTransport{status: vfs.MCPStatusConnected}
	k, proc, baseDir, path := setupMCPEventKernel(t, "playwright", tr)

	// Call 1: healthy, ReconnectCount = 0 → kernel records baseline.
	if err := driveMCPToolCall(t, k, proc, path); err != nil {
		t.Fatalf("call 1: %v", err)
	}
	// Simulate a reconnect having occurred behind the scenes.
	tr.reconnectCount.Store(1)
	// Call 2: kernel observes the delta → emits mcp.reconnect.
	if err := driveMCPToolCall(t, k, proc, path); err != nil {
		t.Fatalf("call 2: %v", err)
	}

	if err := proc.eventWriter.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	if n := countEventsBySyscall(t, baseDir, proc.UUID, "mcp.reconnect"); n != 1 {
		t.Errorf("mcp.reconnect event count = %d, want exactly 1 (one per ReconnectCount delta)", n)
	}
}

// -----------------------------------------------------------------------------
// _072: anti-noise — all-healthy tool calls emit NO mcp.error / mcp.reconnect
//        (Story §易错点 13: events only on state-transition boundaries). (AC6)
// -----------------------------------------------------------------------------
func TestATDD_48_5_072_McpEvents_NoNoiseOnHealthy(t *testing.T) {
	tr := &healthMockTransport{status: vfs.MCPStatusConnected}
	k, proc, baseDir, path := setupMCPEventKernel(t, "healthy", tr)

	for i := range 3 {
		if err := driveMCPToolCall(t, k, proc, path); err != nil {
			t.Fatalf("healthy call #%d: %v", i, err)
		}
	}
	if err := proc.eventWriter.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	if n := countEventsBySyscall(t, baseDir, proc.UUID, "mcp.error"); n != 0 {
		t.Errorf("mcp.error count = %d on healthy path, want 0 (events.jsonl noise)", n)
	}
	if n := countEventsBySyscall(t, baseDir, proc.UUID, "mcp.reconnect"); n != 0 {
		t.Errorf("mcp.reconnect count = %d on healthy path, want 0", n)
	}
}
