package kernel

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// ATDD 48.1 — shared test helpers
//
// These helpers underpin atdd_48_1_rehydrate_mcp_test.go (AC1-AC7) and the
// process_history schema roundtrip tests for the proc-info.json mcp_mounts
// field expected by Story 48.1 Task 1.1-1.3.
//
// Design choices:
//   - The mock MCPTransport lives in this file (kernel package) so we avoid
//     a kernel <-> drivers/mcp dependency edge (project-context.md §依赖方向).
//     The mock satisfies vfs.MCPTransport using only types from vfs + stdlib.
//   - setupResumeKernelWithMockMCP layers on top of resume_test.go's
//     setupResumeKernel — it adds a MountManager wired to a mock factory so
//     AC3 (partial failure) can inject one healthy + one failing transport
//     into the same manager.
//   - writeProcInfoWithMCPMounts writes proc-info.json with the *new*
//     mcp_mounts field. Until Task 1.1-1.3 ships, this field is forward-compat
//     only — procInfoFromDisk silently ignores unknown JSON keys, so the
//     fixture is safe pre-implementation.
// =============================================================================

// ----- mockMCPTransport -------------------------------------------------------

// mockMCPTransport is a configurable vfs.MCPTransport for ATDD-48.1.
// Each instance can be tuned to fail Connect (AC3 server-B) or succeed (AC1
// happy path). Call accounting lets tests assert the transport was actually
// invoked — without this, a test that "passes" because the mount silently
// short-circuited would look identical to a real implementation.
type mockMCPTransport struct {
	serverName string

	mu sync.Mutex

	// Behavior knobs — set BEFORE the test calls mountMgr.Mount.
	connectErr error
	callErr    error
	pingErr    error

	// Call accounting.
	connectCount int
	closed       bool
	calls        []mockCall
}

type mockCall struct {
	Method string
	Params json.RawMessage
}

func (m *mockMCPTransport) Connect(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connectCount++
	return m.connectErr
}

func (m *mockMCPTransport) Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, mockCall{Method: method, Params: params})
	if m.callErr != nil {
		return nil, m.callErr
	}
	// Minimal, valid responses so AC1's Open + Write + Read flow does not
	// crash on JSON parsing in vfs/mcp.go. Concrete shape is irrelevant —
	// AC1 only asserts "no ErrNotFound".
	switch method {
	case "tools/list":
		return json.RawMessage(`{"tools":[{"name":"echo","description":"test echo"}]}`), nil
	case "tools/call":
		return json.RawMessage(`{"content":[{"type":"text","text":"ok"}]}`), nil
	case "resources/list":
		return json.RawMessage(`{"resources":[]}`), nil
	case "resources/read":
		return json.RawMessage(`{"contents":[]}`), nil
	default:
		return json.RawMessage(`{}`), nil
	}
}

func (m *mockMCPTransport) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func (m *mockMCPTransport) Ping(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pingErr
}

// snapshot returns a copy of the mock's call accounting for assertions.
// Avoids races between the test goroutine and the transport's Call goroutine
// (vfs/mcp.go reads the response asynchronously inside mcpToolListFile.Read).
func (m *mockMCPTransport) snapshot() (connectCount int, calls []mockCall, closed bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	calls = append([]mockCall(nil), m.calls...)
	return m.connectCount, calls, m.closed
}

// ----- mock TransportFactory --------------------------------------------------

// newMockTransportFactory returns a vfs.TransportFactory keyed by
// MCPConfig.ServerName. Unknown servers get a default healthy transport so a
// test that forgets to register a mock still produces a deterministic result
// (succeeds) instead of crashing on a nil deref.
func newMockTransportFactory(transports map[string]*mockMCPTransport) vfs.TransportFactory {
	return func(cfg vfs.MCPConfig) (vfs.MCPTransport, error) {
		if t, ok := transports[cfg.ServerName]; ok {
			t.serverName = cfg.ServerName
			return t, nil
		}
		fallback := &mockMCPTransport{serverName: cfg.ServerName}
		transports[cfg.ServerName] = fallback
		return fallback, nil
	}
}

// ----- kernel setup with mounted MCP -----------------------------------------

// setupResumeKernelWithMockMCP wraps setupResumeKernel (resume_test.go) and
// additionally injects a MountManager built from a mock TransportFactory.
// Returns (kernel, baseDir, mountManager, transports). The transports map is
// pre-shared so the caller can configure connectErr / callErr per server BEFORE
// triggering resume.
func setupResumeKernelWithMockMCP(t *testing.T, transports map[string]*mockMCPTransport) (*KernelImpl, string, *vfs.MountManager) {
	t.Helper()
	k, baseDir := setupResumeKernel(t)
	if transports == nil {
		transports = map[string]*mockMCPTransport{}
	}
	devReg := k.vfs.DeviceRegistry()
	factory := newMockTransportFactory(transports)
	mgr := vfs.NewMountManager(devReg, factory)
	k.SetMountManager(mgr)
	return k, baseDir, mgr
}

// ----- disk fixture: proc-info.json with mcp_mounts --------------------------

// procMountFixture mirrors the on-disk JSON shape Story 48.1 Task 1.2
// specifies for the new mcp_mounts field. Using a typed struct (instead of
// map[string]any) catches typos at compile time and stays in lockstep with
// vfs.MCPConfig serialization.
type procMountFixture struct {
	Path   string         `json:"path"`
	Config vfs.MCPConfig  `json:"config"`
}

// writeProcInfoWithMCPMounts writes a complete on-disk fixture (steps.jsonl +
// process-meta.json + proc-info.json) for a process that had `mounts` MCP
// servers mounted. The mcp_mounts JSON field is written verbatim — pre-Task-1
// the field is ignored by procInfoFromDisk; post-Task-1 it round-trips.
//
// pid is the on-disk PID (resume must use this PID for mount path
// reconstruction, NOT the new PID assigned by NewProcess).
func writeProcInfoWithMCPMounts(t *testing.T, baseDir, uuid string, pid uint64,
	state, exitReason string, allowedDevices []string, mounts []procMountFixture) {
	t.Helper()
	stepsDir := filepath.Join(baseDir, "data", "steps", uuid)
	if err := os.MkdirAll(stepsDir, 0o755); err != nil {
		t.Fatalf("mkdir steps dir: %v", err)
	}

	// Minimal steps.jsonl — one record so rehydrate's parseStepsJSONL has
	// something to load.
	stepsLine := `{"step":1,"action":"tool_call","request":{"messages":[{"role":"user","content":"hi"}]},"response":{"content":"ok"}}` + "\n"
	if err := os.WriteFile(filepath.Join(stepsDir, "steps.jsonl"),
		[]byte(stepsLine), 0o600); err != nil {
		t.Fatalf("write steps.jsonl: %v", err)
	}

	// process-meta.json with non-empty SystemPrompt so the fallback synthesis
	// branch in rehydrate.go is NOT hit (we want to validate the happy path).
	if err := os.WriteFile(filepath.Join(stepsDir, "process-meta.json"),
		[]byte(`{"system_prompt":"You are a test agent for MCP resume","tool_defs":[]}`),
		0o600); err != nil {
		t.Fatalf("write process-meta.json: %v", err)
	}

	procInfo := map[string]any{
		"pid":             pid,
		"uuid":            uuid,
		"state":           state,
		"intent":          "test resume with MCP",
		"provider":        "claude",
		"model":           "claude-4",
		"skills":          []string{},
		"allowed_devices": allowedDevices,
		"exit_reason":     exitReason,
		"tokens_used":     1000,
		"primary_device":  "/dev/llm/claude",
		"created_at":      time.Now().Format(time.RFC3339Nano),
		// Forward-compat: the mcp_mounts field is consumed by the
		// procInfoDisk schema extension expected from Story 48.1 Task 1.2.
		"mcp_mounts": mounts,
	}
	data, err := json.Marshal(procInfo)
	if err != nil {
		t.Fatalf("marshal proc-info.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stepsDir, "proc-info.json"), data, 0o600); err != nil {
		t.Fatalf("write proc-info.json: %v", err)
	}
}

// makeMCPConfig builds a vfs.MCPConfig suitable for stdio mock transport.
// Default Command is "/bin/echo" — never executed because the mock factory
// short-circuits Connect. Tests override Command (e.g. /nonexistent/binary)
// only when they want a deterministic transport-construction or connect path.
func makeMCPConfig(serverName, command, workDir string) vfs.MCPConfig {
	if command == "" {
		command = "/bin/echo"
	}
	return vfs.MCPConfig{
		ServerName:    serverName,
		Command:       command,
		Args:          []string{"hello"},
		Env:           map[string]string{"MCP_TEST": "1"},
		TransportType: "stdio",
		WorkDir:       workDir,
		Instructions:  "test mcp server",
	}
}

// mcpMountPath returns the canonical MCP mount path for a (pid, serverName)
// pair, mirroring spawn.go:619 ("/mnt/mcp/<pid>-<server>"). Resume MUST use
// the ORIGINAL pid (the one persisted to disk), not the new PID assigned to
// the resumed process — see story 48.1 Dev Notes §易错点 #1.
func mcpMountPath(pid uint64, serverName string) string {
	return "/mnt/mcp/" + uintToString(pid) + "-" + serverName
}

func uintToString(u uint64) string {
	// strconv.FormatUint would work too; this avoids an extra import in
	// helpers that mostly do not need strconv.
	if u == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for u > 0 {
		i--
		buf[i] = byte('0' + u%10)
		u /= 10
	}
	return string(buf[i:])
}

// ----- DebugChan drain --------------------------------------------------------

// drainMountEvents collects every SyscallEvent named "Mount" emitted on
// proc.DebugChan within the timeout window. Returns the matched events plus
// the full chronological log (for ordering assertions). Stops draining when
// the deadline expires or DebugChan closes.
//
// Important: rehydrate.go currently runs BEFORE k.AddProcess(proc) in
// resumeFromHistory, so any Mount events emitted from inside rehydrate are
// silently dropped (no EventWriter, no DebugChan reader). Story 48.1 Dev
// Notes §易错点 #8 calls this out as the highest-risk edge case — the new
// reattachMCPMounts helper MUST be invoked AFTER AddProcess for these events
// to reach DebugChan.
func drainMountEvents(t *testing.T, proc *Process, timeout time.Duration) (mountEvents, allEvents []types.SyscallEvent) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case evt, ok := <-proc.DebugChan:
			if !ok {
				return mountEvents, allEvents
			}
			allEvents = append(allEvents, evt)
			if evt.Syscall == "Mount" {
				mountEvents = append(mountEvents, evt)
			}
		case <-time.After(20 * time.Millisecond):
			// Tight poll — return early if channel is idle for one
			// poll interval and we already have at least one Mount.
			if len(mountEvents) > 0 {
				return mountEvents, allEvents
			}
		}
	}
	return mountEvents, allEvents
}

// mountEventArg looks up a named argument from a Mount syscall event's Args
// map. Returns the value plus an `ok` bool so tests can distinguish absent
// from zero-valued. Tests assert presence of `source="resume"`, `cwd_fallback`,
// `daemon_restart_recovery`, `reused`, etc. per AC1-AC7.
func mountEventArg(evt types.SyscallEvent, key string) (value any, ok bool) {
	if evt.Args == nil {
		return nil, false
	}
	v, ok := evt.Args[key]
	return v, ok
}
