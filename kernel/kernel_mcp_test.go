package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// Story 48.3 — kernel MCP registry + probe contract tests
//
// **TRUE RED PHASE (compile-failing).**
//
// This file deliberately references kernel API surface that does not yet
// exist on the 48.2 baseline:
//
//   - (*KernelImpl).SetMCPRegistry(map[string]vfs.MCPConfig)
//   - (*KernelImpl).MCPRegistry() map[string]vfs.MCPConfig
//   - (*KernelImpl).SetTransportFactory(vfs.TransportFactory)
//   - (*KernelImpl).RunMCPProbe(ctx, vfs.MCPConfig) MCPProbeResult
//   - kernel.MCPProbeResult{Stages, Tools, Resources, Prompts, OK,
//                            ServerInfo}
//   - kernel.MCPProbeStage{Name, OK, DurationMs, Error}
//
// Until Story 48.3 Task 6 lands these symbols, `go test ./kernel/` WILL
// FAIL TO COMPILE — which is the entire point of the RED phase: anyone
// running the suite immediately sees the gap. The 48.1 / 48.2 ATDD tests
// in this same package will also be blocked from running until the symbols
// exist; this is the intentional cost of contract-first ATDD (user choice
// 2026-05-28).
//
// dev-story Task 6 → just add the methods on KernelImpl; this file then
// turns GREEN with no edits.
//
// References:
//   - Story 48.3 §AC4 (registry injection), §AC2 (RunMCPProbe contract)
//   - Story 48.3 Source Tree: `kernel/kernel.go` UPDATE + `kernel/init.go` UPDATE
//   - Story 48.3 §RunMCPProbe 时序图
// =============================================================================

// -----------------------------------------------------------------------------
// Contract 1: SetMCPRegistry / MCPRegistry round-trip
// -----------------------------------------------------------------------------

func TestKernel_SetMCPRegistry_RoundTrip(t *testing.T) {
	k := newKernelOnly(t)

	// Pre-condition: no registry set yet → MCPRegistry() must not panic and
	// must return either nil or an empty map. AC4 §"kernel.MCPRegistry() 返回
	// nil 还是空 map: IPC handler 必须容忍 nil".
	pre := k.MCPRegistry()
	if len(pre) != 0 {
		t.Errorf("pre-Set MCPRegistry should be nil/empty, got %d entries", len(pre))
	}

	servers := map[string]vfs.MCPConfig{
		"playwright": {ServerName: "playwright", Command: "npx", Args: []string{"@playwright/mcp"}, TransportType: "stdio"},
		"deepwiki":   {ServerName: "deepwiki", Command: "deepwiki-mcp", TransportType: "stdio"},
	}
	k.SetMCPRegistry(servers)

	got := k.MCPRegistry()
	if got == nil {
		t.Fatal("MCPRegistry returned nil after SetMCPRegistry")
	}
	if len(got) != 2 {
		t.Fatalf("MCPRegistry size = %d, want 2", len(got))
	}
	if got["playwright"].Command != "npx" {
		t.Errorf("playwright.Command = %q, want %q", got["playwright"].Command, "npx")
	}
	if got["deepwiki"].ServerName != "deepwiki" {
		t.Errorf("deepwiki.ServerName = %q, want %q", got["deepwiki"].ServerName, "deepwiki")
	}
}

// -----------------------------------------------------------------------------
// Contract 2: SetTransportFactory + RunMCPProbe happy path
// -----------------------------------------------------------------------------

func TestKernel_RunMCPProbe_HappyPath_RecordsStages(t *testing.T) {
	k := newKernelOnly(t)

	tp := &probeMockTransport{
		toolsResp:     json.RawMessage(`{"tools":[{"name":"echo"},{"name":"goto"},{"name":"screenshot"}]}`),
		resourcesResp: json.RawMessage(`{"resources":[{"uri":"page://current"}]}`),
		promptsResp:   json.RawMessage(`{"prompts":[]}`),
	}
	k.SetTransportFactory(func(_ vfs.MCPConfig) (vfs.MCPTransport, error) { return tp, nil })

	cfg := vfs.MCPConfig{ServerName: "playwright", Command: "npx", TransportType: "stdio"}
	ctx, cancel := context.WithTimeout(context.Background(), 11*time.Second)
	defer cancel()
	res := k.RunMCPProbe(ctx, cfg)

	if !res.OK {
		t.Fatalf("RunMCPProbe res.OK=false, want true; stages=%+v", res.Stages)
	}
	if len(res.Stages) < 4 {
		t.Fatalf("RunMCPProbe stages=%d, want ≥ 4 (connect/tools_list/resources_list/prompts_list)", len(res.Stages))
	}
	if res.Stages[0].Name != "connect" {
		t.Errorf("stage[0].Name = %q, want %q", res.Stages[0].Name, "connect")
	}
	if !res.Stages[0].OK {
		t.Errorf("stage[0].OK = false, want true; err = %q", res.Stages[0].Error)
	}
	if res.Tools != 3 {
		t.Errorf("res.Tools = %d, want 3", res.Tools)
	}
	if res.Resources != 1 {
		t.Errorf("res.Resources = %d, want 1", res.Resources)
	}
	if !tp.closed {
		t.Error("transport.Close() not called by RunMCPProbe defer — leak risk")
	}
}

// -----------------------------------------------------------------------------
// Contract 3: RunMCPProbe — connect failure short-circuits remaining stages
// -----------------------------------------------------------------------------

func TestKernel_RunMCPProbe_ConnectFails_ShortCircuits(t *testing.T) {
	k := newKernelOnly(t)

	tp := &probeMockTransport{connectErr: errors.New("exec: \"nonexistent\": executable file not found in $PATH")}
	k.SetTransportFactory(func(_ vfs.MCPConfig) (vfs.MCPTransport, error) { return tp, nil })

	cfg := vfs.MCPConfig{ServerName: "broken", Command: "nonexistent"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res := k.RunMCPProbe(ctx, cfg)

	if res.OK {
		t.Error("res.OK = true, want false (connect failed)")
	}
	if len(res.Stages) == 0 {
		t.Fatal("res.Stages empty, want at least connect")
	}
	if res.Stages[0].OK {
		t.Error("stage[0].OK = true, want false")
	}
	if res.Stages[0].Error == "" {
		t.Error("stage[0].Error empty, want connect error message")
	}
	// tools/list etc must NOT have run after connect failure.
	for i := 1; i < len(res.Stages); i++ {
		if res.Stages[i].OK {
			t.Errorf("stage[%d]=%q.OK = true after connect failure — short-circuit broken", i, res.Stages[i].Name)
		}
	}
	if tp.toolsListCalls != 0 {
		t.Errorf("transport.Call(\"tools/list\") = %d, want 0 (short-circuit broken)", tp.toolsListCalls)
	}
	if !tp.closed {
		t.Error("transport.Close() not called even on connect-failure path — leak risk")
	}
}

// -----------------------------------------------------------------------------
// Helpers — kept local so the file is self-contained.
// -----------------------------------------------------------------------------

// newKernelOnly returns a bare KernelImpl. Story 48.3 contract tests only
// exercise the new MCP registry + probe surface, so we skip vfs/ctxMgr
// construction. If a future contract test needs them, factor up.
func newKernelOnly(t *testing.T) *KernelImpl {
	t.Helper()
	return &KernelImpl{}
}

// probeMockTransport satisfies vfs.MCPTransport. Each method records call
// accounting so RunMCPProbe can be verified by stage rather than by output
// string. Lives only in this file — 48.1 / 48.2 mocks have different shape
// constraints (concurrency-safe accounting, closeDelay semantics) and we
// don't want this file to depend on their helpers.
type probeMockTransport struct {
	connectErr error

	toolsResp     json.RawMessage
	resourcesResp json.RawMessage
	promptsResp   json.RawMessage
	callErr       map[string]error // optional per-method error

	toolsListCalls     int
	resourcesListCalls int
	promptsListCalls   int
	closed             bool
}

func (m *probeMockTransport) Connect(ctx context.Context) error { return m.connectErr }

func (m *probeMockTransport) Call(ctx context.Context, method string, _ json.RawMessage) (json.RawMessage, error) {
	if err, ok := m.callErr[method]; ok && err != nil {
		return nil, err
	}
	switch method {
	case "tools/list":
		m.toolsListCalls++
		if m.toolsResp == nil {
			return json.RawMessage(`{"tools":[]}`), nil
		}
		return m.toolsResp, nil
	case "resources/list":
		m.resourcesListCalls++
		if m.resourcesResp == nil {
			return json.RawMessage(`{"resources":[]}`), nil
		}
		return m.resourcesResp, nil
	case "prompts/list":
		m.promptsListCalls++
		if m.promptsResp == nil {
			return json.RawMessage(`{"prompts":[]}`), nil
		}
		return m.promptsResp, nil
	default:
		return json.RawMessage(`{}`), nil
	}
}

func (m *probeMockTransport) Close() error           { m.closed = true; return nil }
func (m *probeMockTransport) Ping(_ context.Context) error { return nil }
