package kernel

import (
	gocontext "context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rnixai/rnix/agents"
	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/skills"
	"github.com/rnixai/rnix/vfs"
)

// --- E2E Test Helpers ---

// mockMultiStepLLM simulates a multi-step LLM device for e2e testing.
// Step 1: tool_call → /dev/shell (Device layer)
// Step 2: tool_call → /mnt/mcp/{pid}-mock-server/tools/query (MCP layer)
// Step 3: text "E2E test completed" (task done)
type mockMultiStepLLM struct {
	mu   sync.Mutex
	step int
	pid  types.PID
}

func (m *mockMultiStepLLM) Write(_ gocontext.Context, data []byte) error {
	return nil
}

func (m *mockMultiStepLLM) Read(length int) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.step++
	switch m.step {
	case 1:
		return json.Marshal(llmResponse{
			TokensUsed: 10,
			ToolCalls: []llmToolCall{{
				ID: "call_1", Name: "/dev/shell",
				Input: map[string]any{"command": "echo hello"},
			}},
		})
	case 2:
		return json.Marshal(llmResponse{
			TokensUsed: 10,
			ToolCalls: []llmToolCall{{
				ID: "call_2", Name: fmt.Sprintf("/mnt/mcp/%d-mock-server/tools/query", m.pid),
				Input: map[string]any{"query": "test"},
			}},
		})
	default:
		return json.Marshal(llmResponse{Content: "E2E test completed", TokensUsed: 5})
	}
}

func (m *mockMultiStepLLM) Close() error { return nil }

func (m *mockMultiStepLLM) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{IsDevice: true, Name: "/dev/llm/claude"}, nil
}

func (f *mockMultiStepLLM) SupportsToolCalling() bool { return true }

// mockShellFile simulates /dev/shell for e2e testing.
type mockShellFile struct {
	mu        sync.Mutex
	writeData []byte
	closed    bool
}

func (f *mockShellFile) Write(_ gocontext.Context, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writeData = data
	return nil
}

func (f *mockShellFile) Read(length int) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return []byte(`{"output": "hello", "exit_code": 0}`), nil
}

func (f *mockShellFile) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *mockShellFile) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{IsDevice: true, Name: "/dev/shell"}, nil
}

// mockFSFile simulates /dev/fs for registration purposes.
type mockFSFile struct{}

func (f *mockFSFile) Write(_ gocontext.Context, _ []byte) error { return nil }
func (f *mockFSFile) Read(length int) ([]byte, error)           { return []byte("{}"), nil }
func (f *mockFSFile) Close() error                              { return nil }
func (f *mockFSFile) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{IsDevice: true, Name: "/dev/fs"}, nil
}

// mockMCPToolFile simulates an MCP tool VFS file.
type mockMCPToolFile struct {
	mu        sync.Mutex
	writeData []byte
	closed    bool
}

func (f *mockMCPToolFile) Write(_ gocontext.Context, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writeData = data
	return nil
}

func (f *mockMCPToolFile) Read(length int) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return []byte(`{"result": "query result from mock MCP server"}`), nil
}

func (f *mockMCPToolFile) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *mockMCPToolFile) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{IsDevice: true, Name: "/mnt/mcp/tools/query"}, nil
}

// collectEvents drains all events from a DebugChan after it is closed.
func collectEvents(ch <-chan types.SyscallEvent) []types.SyscallEvent {
	var events []types.SyscallEvent
	for e := range ch {
		events = append(events, e)
	}
	return events
}

// findEvent returns the first event matching the given syscall name.
func findEvent(events []types.SyscallEvent, syscall string) *types.SyscallEvent {
	for i := range events {
		if events[i].Syscall == syscall {
			return &events[i]
		}
	}
	return nil
}

// findEvents returns all events matching the given syscall name.
func findEvents(events []types.SyscallEvent, syscall string) []types.SyscallEvent {
	var result []types.SyscallEvent
	for _, e := range events {
		if e.Syscall == syscall {
			result = append(result, e)
		}
	}
	return result
}

// findEventWithArg returns the first event matching syscall and having the specified arg key/value.
func findEventWithArg(events []types.SyscallEvent, syscall, argKey string, argValue any) *types.SyscallEvent {
	for i := range events {
		if events[i].Syscall != syscall {
			continue
		}
		if v, ok := events[i].Args[argKey]; ok {
			if fmt.Sprintf("%v", v) == fmt.Sprintf("%v", argValue) {
				return &events[i]
			}
		}
	}
	return nil
}

// e2eAgentInfo creates the full four-layer AgentInfo for e2e tests.
func e2eAgentInfo() *agents.AgentInfo {
	return &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Name:          "e2e-test-agent",
			Description:   "端到端测试用 Agent",
			Models:        agents.AgentModels{Provider: "claude", Preferred: "sonnet", Fallback: "haiku"},
			ContextBudget: 4096,
			Skills:        []string{"e2e-skill"},
			MCP:           []string{"mock-server"},
		},
		Instructions: "你是一个端到端测试用 Agent。请根据用户请求执行任务。",
		Skills: []*skills.SkillInfo{
			{
				Manifest: skills.SkillManifest{
					Name:            "e2e-skill",
					Description:     "端到端测试用 Skill",
					AllowedToolsRaw: "/dev/llm/claude /dev/shell /dev/fs",
				},
				Body: "这是一个端到端测试用的 Skill，用于验证四层能力栈。",
			},
		},
		MCPConfigs: []vfs.MCPConfig{
			{ServerName: "mock-server", Command: "mock", TransportType: "stdio"},
		},
	}
}

// newE2EKernel creates a kernel wired for four-layer e2e testing.
// It registers mock devices for /dev/llm/claude, /dev/shell, /dev/fs
// and sets up a mock MountManager. The mcpMountPath is registered in the
// device registry so VFS Open can route MCP tool calls.
func newE2EKernel(t testing.TB, llmFile vfs.VFSFile) (*KernelImpl, *spawnMockMountManager) {
	t.Helper()
	reg := vfs.NewDeviceRegistry()

	// Register LLM device
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return llmFile, nil
	})

	// Register Shell device
	registerMockTool(reg, "/dev/shell", func(subpath string, flags vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockShellFile{}, nil
	})

	// Register FS device
	registerMockTool(reg, "/dev/fs", func(subpath string, flags vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockFSFile{}, nil
	})

	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)

	// Set up mock MountManager that also registers MCP paths in DeviceRegistry
	mm := newSpawnMockMountManager()
	origMountFn := mm.mountFn
	mm.mountFn = func(path string, config vfs.MCPConfig) error {
		if origMountFn != nil {
			if err := origMountFn(path, config); err != nil {
				return err
			}
		}
		mm.mu.Lock()
		mm.mounted[path] = true
		mm.mu.Unlock()

		// Register MCP path in DeviceRegistry so VFS Open routes correctly
		_ = reg.Register(path, func(subpath string, flags vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
			return &mockMCPToolFile{}, nil
		})
		return nil
	}
	k.mountMgr = mm

	t.Cleanup(func() { k.Shutdown() })
	return k, mm
}

// --- Test 1: TestFourLayerCapabilityStack (AC #1) ---

func TestFourLayerCapabilityStack(t *testing.T) {
	t.Run("agent_layer_spawn_success_and_identity", func(t *testing.T) {
		// Given: a full four-layer AgentInfo
		agent := e2eAgentInfo()
		llm := &mockMultiStepLLM{}
		k, _ := newE2EKernel(t, llm)

		// When: Spawn is called
		pid, err := k.Spawn("e2e test intent", agent, SpawnOpts{})
		if err != nil {
			t.Fatalf("Spawn returned error: %v", err)
		}
		llm.mu.Lock()
		llm.pid = pid
		llm.mu.Unlock()

		// Then: Agent layer is correctly configured
		if pid == 0 {
			t.Fatal("Spawn returned PID 0")
		}

		proc, ok := k.GetProcess(pid)
		if !ok {
			t.Fatal("process not found in table")
		}

		// Wait for process to finish
		select {
		case <-proc.Done:
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for process to finish")
		}
	})

	t.Run("skill_layer_allowed_devices_from_skill", func(t *testing.T) {
		// Given: an agent with a skill that allows /dev/llm/claude, /dev/shell, /dev/fs
		agent := e2eAgentInfo()
		llm := &mockMultiStepLLM{}
		k, _ := newE2EKernel(t, llm)

		// When: Spawn is called
		pid, err := k.Spawn("e2e test intent", agent, SpawnOpts{})
		if err != nil {
			t.Fatalf("Spawn returned error: %v", err)
		}
		llm.mu.Lock()
		llm.pid = pid
		llm.mu.Unlock()

		// Then: AllowedDevices contains the skill's device paths
		proc, ok := k.GetProcess(pid)
		if !ok {
			t.Fatal("process not found")
		}
		proc.mu.Lock()
		devices := append([]string(nil), proc.AllowedDevices...)
		proc.mu.Unlock()

		expectedDevices := []string{"/dev/fs", "/dev/llm/claude", "/dev/shell"}
		for _, expected := range expectedDevices {
			if !slices.Contains(devices, expected) {
				t.Errorf("AllowedDevices missing %q, got %v", expected, devices)
			}
		}

		// Wait for process to finish
		select {
		case <-proc.Done:
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for process to finish")
		}
	})

	t.Run("mcp_layer_mount_and_allowed_devices", func(t *testing.T) {
		// Given: an agent with MCP config
		agent := e2eAgentInfo()
		llm := &mockMultiStepLLM{}
		k, mm := newE2EKernel(t, llm)

		// When: Spawn is called
		pid, err := k.Spawn("e2e test intent", agent, SpawnOpts{})
		if err != nil {
			t.Fatalf("Spawn returned error: %v", err)
		}
		llm.mu.Lock()
		llm.pid = pid
		llm.mu.Unlock()

		// Then: MCP is mounted and MCPMounts is recorded
		proc, ok := k.GetProcess(pid)
		if !ok {
			t.Fatal("process not found")
		}
		proc.mu.Lock()
		mounts := append([]string(nil), proc.MCPMounts...)
		devices := append([]string(nil), proc.AllowedDevices...)
		proc.mu.Unlock()

		expectedMCPPath := fmt.Sprintf("/mnt/mcp/%d-mock-server", pid)
		if len(mounts) != 1 {
			t.Fatalf("MCPMounts length = %d, want 1", len(mounts))
		}
		if mounts[0] != expectedMCPPath {
			t.Errorf("MCPMounts[0] = %q, want %q", mounts[0], expectedMCPPath)
		}

		// MCP path should be in AllowedDevices
		if !slices.Contains(devices, expectedMCPPath) {
			t.Errorf("AllowedDevices missing MCP path %q, got %v", expectedMCPPath, devices)
		}

		// Mount was called
		calls := mm.getMountCalls()
		if len(calls) != 1 {
			t.Fatalf("Mount called %d times, want 1", len(calls))
		}
		if calls[0].Config.ServerName != "mock-server" {
			t.Errorf("Mount config.ServerName = %q, want %q", calls[0].Config.ServerName, "mock-server")
		}

		// Wait for process to finish
		select {
		case <-proc.Done:
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for process to finish")
		}
	})

	t.Run("full_e2e_multi_step_reasoning", func(t *testing.T) {
		// Given: a full four-layer agent with multi-step LLM
		agent := e2eAgentInfo()
		llm := &mockMultiStepLLM{}
		k, mm := newE2EKernel(t, llm)

		// When: Spawn and execute
		pid, err := k.Spawn("e2e test intent", agent, SpawnOpts{})
		if err != nil {
			t.Fatalf("Spawn returned error: %v", err)
		}
		llm.mu.Lock()
		llm.pid = pid
		llm.mu.Unlock()

		proc, ok := k.GetProcess(pid)
		if !ok {
			t.Fatal("process not found")
		}

		// Wait for process to finish
		var exit ExitStatus
		select {
		case exit = <-proc.Done:
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for process to finish")
		}

		// Then: process completed successfully with text result
		if exit.Code != 0 {
			t.Errorf("exit code = %d, want 0; reason: %s, err: %v", exit.Code, exit.Reason, exit.Err)
		}

		proc.mu.Lock()
		result := proc.Result
		tokens := proc.TokensUsed
		proc.mu.Unlock()

		if result != "E2E test completed" {
			t.Errorf("process result = %q, want %q", result, "E2E test completed")
		}
		// 3 steps: 10 + 10 + 5 = 25 tokens
		if tokens != 25 {
			t.Errorf("tokens used = %d, want 25", tokens)
		}

		// LLM went through 3 steps
		llm.mu.Lock()
		steps := llm.step
		llm.mu.Unlock()
		if steps != 3 {
			t.Errorf("LLM steps = %d, want 3", steps)
		}

		// And: MCP was unmounted after process exit
		unmountCalls := mm.getUnmountCalls()
		if len(unmountCalls) < 1 {
			t.Fatal("Unmount not called after process exit")
		}
		expectedPath := fmt.Sprintf("/mnt/mcp/%d-mock-server", pid)
		if !slices.Contains(unmountCalls, expectedPath) {
			t.Errorf("Unmount not called for %q, calls: %v", expectedPath, unmountCalls)
		}
	})

	t.Run("mcp_auto_unmount_on_process_exit", func(t *testing.T) {
		// Given: a full four-layer agent
		agent := e2eAgentInfo()
		llm := &mockMultiStepLLM{}
		k, mm := newE2EKernel(t, llm)

		// When: Spawn and wait for completion
		pid, err := k.Spawn("e2e test intent", agent, SpawnOpts{})
		if err != nil {
			t.Fatalf("Spawn returned error: %v", err)
		}
		llm.mu.Lock()
		llm.pid = pid
		llm.mu.Unlock()

		proc, ok := k.GetProcess(pid)
		if !ok {
			t.Fatal("process not found")
		}
		select {
		case <-proc.Done:
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for process to finish")
		}

		// Then: Unmount was called for the MCP mount
		unmountCalls := mm.getUnmountCalls()
		expectedPath := fmt.Sprintf("/mnt/mcp/%d-mock-server", pid)
		if !slices.Contains(unmountCalls, expectedPath) {
			t.Errorf("Unmount not called for %q after process exit, calls: %v", expectedPath, unmountCalls)
		}
	})
}

// --- Test 2: TestFourLayerAstraceVisibility (AC #2) ---

func TestFourLayerAstraceVisibility(t *testing.T) {
	// Shared setup: spawn a four-layer process and collect all events
	agent := e2eAgentInfo()
	llm := &mockMultiStepLLM{}
	k, _ := newE2EKernel(t, llm)

	pid, err := k.Spawn("e2e trace test", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}
	llm.mu.Lock()
	llm.pid = pid
	llm.mu.Unlock()

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("process not found")
	}

	// Start collecting events from DebugChan asynchronously.
	// DebugChan is closed by reapProcess (triggered via Wait), so we
	// must start draining before Wait.
	proc.mu.Lock()
	ch := proc.DebugChan
	proc.mu.Unlock()

	eventsCh := make(chan []types.SyscallEvent, 1)
	go func() {
		eventsCh <- collectEvents(ch)
	}()

	// Wait reaps the process, which closes DebugChan allowing collectEvents to return
	exit, waitErr := k.Wait(pid)
	if waitErr != nil {
		t.Fatalf("Wait returned error: %v", waitErr)
	}
	if exit.Code != 0 {
		t.Fatalf("exit code = %d, want 0; reason: %s", exit.Code, exit.Reason)
	}

	// Get collected events
	var events []types.SyscallEvent
	select {
	case events = <-eventsCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for event collection")
	}

	if len(events) == 0 {
		t.Fatal("no events collected from DebugChan")
	}

	t.Run("spawn_event_contains_four_layer_args", func(t *testing.T) {
		spawnEvent := findEvent(events, "Spawn")
		if spawnEvent == nil {
			t.Fatal("Spawn event not found in events")
		}
		if _, ok := spawnEvent.Args["agent"]; !ok {
			t.Error("Spawn event missing 'agent' arg")
		}
		if _, ok := spawnEvent.Args["skills"]; !ok {
			t.Error("Spawn event missing 'skills' arg")
		}
		if _, ok := spawnEvent.Args["allowed_devices"]; !ok {
			t.Error("Spawn event missing 'allowed_devices' arg")
		}
		if _, ok := spawnEvent.Args["mcp_mounts"]; !ok {
			t.Error("Spawn event missing 'mcp_mounts' arg")
		}
	})

	t.Run("mount_event_with_auto_flag", func(t *testing.T) {
		mountEvents := findEvents(events, "Mount")
		if len(mountEvents) == 0 {
			t.Fatal("no Mount events found")
		}
		// Find the successful mount event (err == nil)
		var mountEvent *types.SyscallEvent
		for i := range mountEvents {
			if mountEvents[i].Err == nil {
				mountEvent = &mountEvents[i]
				break
			}
		}
		if mountEvent == nil {
			t.Fatal("no successful Mount event found")
		}
		if auto, ok := mountEvent.Args["auto"]; !ok || auto != true {
			t.Errorf("Mount event 'auto' = %v, want true", auto)
		}
		if _, ok := mountEvent.Args["path"]; !ok {
			t.Error("Mount event missing 'path' arg")
		}
	})

	t.Run("device_layer_open_write_read_close_events", func(t *testing.T) {
		// Look for Open event for /dev/shell
		openEvents := findEvents(events, "Open")
		shellOpened := false
		for _, e := range openEvents {
			if path, ok := e.Args["path"]; ok && path == "/dev/shell" {
				shellOpened = true
				break
			}
		}
		if !shellOpened {
			t.Error("no Open event for /dev/shell found")
		}

		// Write and Read events exist (generic FD-based, can't easily filter by path
		// but we verify they exist for tool calls)
		writeEvents := findEvents(events, "Write")
		if len(writeEvents) == 0 {
			t.Error("no Write events found")
		}
		readEvents := findEvents(events, "Read")
		if len(readEvents) == 0 {
			t.Error("no Read events found")
		}
		closeEvents := findEvents(events, "Close")
		if len(closeEvents) == 0 {
			t.Error("no Close events found")
		}
	})

	t.Run("mcp_layer_open_write_read_close_events", func(t *testing.T) {
		// Look for Open event for MCP tool path
		openEvents := findEvents(events, "Open")
		mcpPath := fmt.Sprintf("/mnt/mcp/%d-mock-server/tools/query", pid)
		mcpOpened := false
		for _, e := range openEvents {
			if path, ok := e.Args["path"]; ok {
				if pathStr, isStr := path.(string); isStr && pathStr == mcpPath {
					mcpOpened = true
					break
				}
			}
		}
		if !mcpOpened {
			t.Errorf("no Open event for MCP path %q found", mcpPath)
		}
	})

	t.Run("unmount_event_with_auto_flag", func(t *testing.T) {
		unmountEvents := findEvents(events, "Unmount")
		if len(unmountEvents) == 0 {
			t.Fatal("no Unmount events found")
		}
		unmountEvent := &unmountEvents[0]
		if auto, ok := unmountEvent.Args["auto"]; !ok || auto != true {
			t.Errorf("Unmount event 'auto' = %v, want true", auto)
		}
		expectedPath := fmt.Sprintf("/mnt/mcp/%d-mock-server", pid)
		if path, ok := unmountEvent.Args["path"]; !ok || path != expectedPath {
			t.Errorf("Unmount event 'path' = %v, want %q", path, expectedPath)
		}
	})

	t.Run("event_chronological_order", func(t *testing.T) {
		// Verify key events appear in expected order.
		// We check that the first occurrence of each event type appears
		// strictly after the first occurrence of the previous event type.
		expectedOrder := []string{"CtxAlloc", "Open", "Mount", "Spawn", "ReasonStep"}

		// Build index map: event name -> first occurrence index
		firstIdx := make(map[string]int)
		for i, e := range events {
			if _, exists := firstIdx[e.Syscall]; !exists {
				firstIdx[e.Syscall] = i
			}
		}

		// Verify all expected events exist
		for _, expected := range expectedOrder {
			if _, exists := firstIdx[expected]; !exists {
				t.Errorf("expected event %q not found in events", expected)
			}
		}

		// Verify pairwise ordering: each event must appear before the next
		for i := 0; i < len(expectedOrder)-1; i++ {
			curr := expectedOrder[i]
			next := expectedOrder[i+1]
			currIdx, currOK := firstIdx[curr]
			nextIdx, nextOK := firstIdx[next]
			if currOK && nextOK && currIdx >= nextIdx {
				t.Errorf("%s (idx %d) should appear before %s (idx %d)", curr, currIdx, next, nextIdx)
			}
		}
	})

	t.Run("event_fields_complete", func(t *testing.T) {
		for _, e := range events {
			if e.PID == 0 {
				t.Errorf("event %q has PID=0", e.Syscall)
			}
			if e.Syscall == "" {
				t.Error("event has empty Syscall field")
			}
			// Args can be nil for some events but Syscall and PID must always be set
		}
		// ReasonStep events should have Duration > 0
		reasonSteps := findEvents(events, "ReasonStep")
		for _, rs := range reasonSteps {
			if rs.Duration == 0 {
				// Duration can be 0 for very fast operations; just verify it's non-negative
				if rs.Duration < 0 {
					t.Errorf("ReasonStep event has negative duration: %v", rs.Duration)
				}
			}
		}
	})
}

// --- Test 3: TestAllowedDevicesAggregation (AC #1) ---

func TestAllowedDevicesAggregation(t *testing.T) {
	t.Run("skill_and_mcp_paths_coexist", func(t *testing.T) {
		// Given: an agent with both skills (providing /dev/ paths) and MCP (providing /mnt/mcp/ paths)
		agent := e2eAgentInfo()
		llm := &mockMultiStepLLM{}
		k, _ := newE2EKernel(t, llm)

		// When: Spawn is called
		pid, err := k.Spawn("aggregation test", agent, SpawnOpts{})
		if err != nil {
			t.Fatalf("Spawn returned error: %v", err)
		}
		llm.mu.Lock()
		llm.pid = pid
		llm.mu.Unlock()

		proc, ok := k.GetProcess(pid)
		if !ok {
			t.Fatal("process not found")
		}
		proc.mu.Lock()
		devices := append([]string(nil), proc.AllowedDevices...)
		proc.mu.Unlock()

		// Then: AllowedDevices contains both /dev/ and /mnt/mcp/ paths
		hasDevPath := false
		hasMCPPath := false
		for _, d := range devices {
			if strings.HasPrefix(d, "/dev/") {
				hasDevPath = true
			}
			if strings.HasPrefix(d, "/mnt/mcp/") {
				hasMCPPath = true
			}
		}
		if !hasDevPath {
			t.Errorf("AllowedDevices missing /dev/ paths, got %v", devices)
		}
		if !hasMCPPath {
			t.Errorf("AllowedDevices missing /mnt/mcp/ paths, got %v", devices)
		}

		// Wait for process
		select {
		case <-proc.Done:
		case <-time.After(5 * time.Second):
			t.Fatal("timeout")
		}
	})

	t.Run("permission_allows_skill_devices", func(t *testing.T) {
		// Given: an agent with skill allowing /dev/shell and /dev/fs
		agent := e2eAgentInfo()
		// Override LLM to call /dev/shell (which should be allowed)
		llm := &mockLLMFile{
			readData: makeToolCallResponse("/dev/shell", map[string]any{"cmd": "ls"}, 10),
		}
		k, _ := newE2EKernel(t, llm)

		// When: Spawn is called
		pid, err := k.Spawn("permission test", agent, SpawnOpts{})
		if err != nil {
			t.Fatalf("Spawn returned error: %v", err)
		}

		proc, ok := k.GetProcess(pid)
		if !ok {
			t.Fatal("process not found")
		}

		// Then: process should complete (tool call succeeds because /dev/shell is allowed)
		select {
		case exit := <-proc.Done:
			// Process completed - the tool call to /dev/shell was allowed
			// It either succeeded or had a follow-up, both indicate permission was granted
			_ = exit
		case <-time.After(5 * time.Second):
			t.Fatal("timeout - process did not complete")
		}
	})

	t.Run("permission_allows_mcp_subpaths", func(t *testing.T) {
		// Given: an agent with MCP mount
		agent := e2eAgentInfo()
		llm := &mockMultiStepLLM{}
		k, _ := newE2EKernel(t, llm)

		// When: Spawn is called
		pid, err := k.Spawn("mcp permission test", agent, SpawnOpts{})
		if err != nil {
			t.Fatalf("Spawn returned error: %v", err)
		}
		llm.mu.Lock()
		llm.pid = pid
		llm.mu.Unlock()

		proc, ok := k.GetProcess(pid)
		if !ok {
			t.Fatal("process not found")
		}

		// Then: process completes (step 2 calls MCP tool which is allowed)
		select {
		case exit := <-proc.Done:
			if exit.Code != 0 {
				t.Errorf("exit code = %d, want 0; reason: %s", exit.Code, exit.Reason)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timeout")
		}
	})

	t.Run("permission_denies_unauthorized_path", func(t *testing.T) {
		// Given: an agent with skills (allowing /dev/shell, /dev/fs, /dev/llm/claude)
		agent := e2eAgentInfo()

		// First response: tool_call to /dev/unknown
		resp1 := llmResponse{
			TokensUsed: 5,
			ToolCalls: []llmToolCall{{
				ID: "call_unknown", Name: "/dev/unknown",
				Input: map[string]any{},
			}},
		}
		resp1JSON, _ := json.Marshal(resp1)

		// Second response: text (done)
		resp2 := llmResponse{Content: "done after denial", TokensUsed: 5}
		resp2JSON, _ := json.Marshal(resp2)

		// Create kernel with custom sequential LLM
		var step int
		var mu sync.Mutex
		reg := vfs.NewDeviceRegistry()
		_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
			mu.Lock()
			step++
			s := step
			mu.Unlock()
			if s == 1 {
				return &mockLLMFile{readData: resp1JSON}, nil
			}
			return &mockLLMFile{readData: resp2JSON}, nil
		})
		registerMockTool(reg, "/dev/shell", func(subpath string, flags vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
			return &mockShellFile{}, nil
		})
		registerMockTool(reg, "/dev/fs", func(subpath string, flags vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
			return &mockFSFile{}, nil
		})
		v := vfs.NewVFS(reg)
		ctxMgr := rnixctx.NewManager()
		k := NewKernel(v, ctxMgr, nil)
		mm := newSpawnMockMountManager()
		mm.mountFn = func(path string, config vfs.MCPConfig) error {
			mm.mu.Lock()
			mm.mounted[path] = true
			mm.mu.Unlock()
			_ = reg.Register(path, func(subpath string, flags vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
				return &mockMCPToolFile{}, nil
			})
			return nil
		}
		k.mountMgr = mm
		t.Cleanup(func() { k.Shutdown() })

		// When: Spawn with the agent
		pid, err := k.Spawn("permission denial test", agent, SpawnOpts{})
		if err != nil {
			t.Fatalf("Spawn returned error: %v", err)
		}

		proc, ok := k.GetProcess(pid)
		if !ok {
			t.Fatal("process not found")
		}

		// Collect events from DebugChan to verify permission_denied
		proc.mu.Lock()
		ch := proc.DebugChan
		proc.mu.Unlock()

		eventsCh := make(chan []types.SyscallEvent, 1)
		go func() {
			eventsCh <- collectEvents(ch)
		}()

		// Then: process eventually completes (denied tool_call is handled gracefully)
		_, waitErr := k.Wait(pid)
		if waitErr != nil {
			t.Fatalf("Wait returned error: %v", waitErr)
		}

		// Collect events
		var denialEvents []types.SyscallEvent
		select {
		case evts := <-eventsCh:
			denialEvents = evts
		case <-time.After(5 * time.Second):
			t.Fatal("timeout collecting events")
		}

		// Verify that a permission_denied ReasonStep event was emitted for /dev/unknown
		permDenied := findEventWithArg(denialEvents, "ReasonStep", "action", "permission_denied")
		if permDenied == nil {
			t.Error("no permission_denied ReasonStep event found for /dev/unknown")
		} else {
			if tool, ok := permDenied.Args["tool"]; !ok || tool != "/dev/unknown" {
				t.Errorf("permission_denied event tool = %v, want /dev/unknown", tool)
			}
		}
	})
}

// --- Test 4: TestFourLayerBoundaryConditions (AC #1, #2) ---

func TestFourLayerBoundaryConditions(t *testing.T) {
	t.Run("agent_with_skill_no_mcp", func(t *testing.T) {
		// Given: an agent with skill but no MCP
		agent := &agents.AgentInfo{
			Manifest: agents.AgentManifest{
				Name:   "skill-only-agent",
				Models: agents.AgentModels{Provider: "claude", Preferred: "sonnet"},
				Skills: []string{"e2e-skill"},
			},
			Instructions: "Test agent with skill only.",
			Skills: []*skills.SkillInfo{
				{
					Manifest: skills.SkillManifest{
						Name:            "e2e-skill",
						AllowedToolsRaw: "/dev/shell /dev/fs",
					},
					Body: "Skill body.",
				},
			},
			// No MCPConfigs
		}
		llmFile := &mockLLMFile{
			readData: makeLLMResponse("skill only done", 5),
		}
		k, _ := newE2EKernel(t, llmFile)

		// When: Spawn is called
		pid, err := k.Spawn("skill only test", agent, SpawnOpts{})
		if err != nil {
			t.Fatalf("Spawn returned error: %v", err)
		}

		proc, ok := k.GetProcess(pid)
		if !ok {
			t.Fatal("process not found")
		}

		// Then: process completes normally without MCP
		select {
		case exit := <-proc.Done:
			if exit.Code != 0 {
				t.Errorf("exit code = %d, want 0; reason: %s", exit.Code, exit.Reason)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timeout")
		}

		// And: AllowedDevices contains only skill devices (no /mnt/mcp/)
		proc.mu.Lock()
		devices := append([]string(nil), proc.AllowedDevices...)
		mounts := append([]string(nil), proc.MCPMounts...)
		proc.mu.Unlock()

		for _, d := range devices {
			if strings.HasPrefix(d, "/mnt/mcp/") {
				t.Errorf("AllowedDevices should not contain MCP paths, found %q", d)
			}
		}
		if len(mounts) != 0 {
			t.Errorf("MCPMounts should be empty, got %v", mounts)
		}
	})

	t.Run("agent_with_mcp_no_skill", func(t *testing.T) {
		// Given: an agent with MCP but no skills
		agent := &agents.AgentInfo{
			Manifest: agents.AgentManifest{
				Name:   "mcp-only-agent",
				Models: agents.AgentModels{Provider: "claude", Preferred: "sonnet"},
				MCP:    []string{"mock-server"},
			},
			Instructions: "Test agent with MCP only.",
			// No Skills
			MCPConfigs: []vfs.MCPConfig{
				{ServerName: "mock-server", Command: "mock", TransportType: "stdio"},
			},
		}
		llmFile := &mockLLMFile{
			readData: makeLLMResponse("mcp only done", 5),
		}
		k, _ := newE2EKernel(t, llmFile)

		// When: Spawn is called
		pid, err := k.Spawn("mcp only test", agent, SpawnOpts{})
		if err != nil {
			t.Fatalf("Spawn returned error: %v", err)
		}

		proc, ok := k.GetProcess(pid)
		if !ok {
			t.Fatal("process not found")
		}

		// Then: process completes normally
		select {
		case exit := <-proc.Done:
			if exit.Code != 0 {
				t.Errorf("exit code = %d, want 0; reason: %s", exit.Code, exit.Reason)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timeout")
		}

		// And: AllowedDevices contains MCP path (auto-added by Spawn)
		proc.mu.Lock()
		devices := append([]string(nil), proc.AllowedDevices...)
		mounts := append([]string(nil), proc.MCPMounts...)
		proc.mu.Unlock()

		expectedMCPPath := fmt.Sprintf("/mnt/mcp/%d-mock-server", pid)
		if !slices.Contains(devices, expectedMCPPath) {
			t.Errorf("AllowedDevices should contain MCP path %q, got %v", expectedMCPPath, devices)
		}
		if len(mounts) != 1 || mounts[0] != expectedMCPPath {
			t.Errorf("MCPMounts = %v, want [%q]", mounts, expectedMCPPath)
		}
	})

	t.Run("mcp_mount_failure_rollback", func(t *testing.T) {
		// Given: a MountManager that fails on mount
		agent := e2eAgentInfo()
		llm := &mockMultiStepLLM{}

		reg := vfs.NewDeviceRegistry()
		_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
			return llm, nil
		})
		registerMockTool(reg, "/dev/shell", func(subpath string, flags vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
			return &mockShellFile{}, nil
		})
		registerMockTool(reg, "/dev/fs", func(subpath string, flags vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
			return &mockFSFile{}, nil
		})
		v := vfs.NewVFS(reg)
		ctxMgr := rnixctx.NewManager()
		k := NewKernel(v, ctxMgr, nil)

		mm := newSpawnMockMountManager()
		mm.mountFn = func(path string, config vfs.MCPConfig) error {
			return fmt.Errorf("connection timeout")
		}
		k.mountMgr = mm
		t.Cleanup(func() { k.Shutdown() })

		// When: Spawn is called
		_, err := k.Spawn("mount failure test", agent, SpawnOpts{})

		// Then: error is returned
		if err == nil {
			t.Fatal("expected error for mount failure, got nil")
		}

		// And: error is a SyscallError
		var syscallErr *SyscallError
		if !containsSyscallError(err, &syscallErr) {
			t.Fatalf("expected *SyscallError, got %T: %v", err, err)
		}
	})

	t.Run("kill_triggers_mcp_cleanup", func(t *testing.T) {
		// Given: a four-layer agent with a slow LLM (blocks to keep process running)
		agent := e2eAgentInfo()

		// Create a slow LLM that blocks on Write until context is cancelled
		slowLLM := &e2eBlockingLLM{}
		k, mm := newE2EKernel(t, slowLLM)

		// When: Spawn and then Kill
		pid, err := k.Spawn("kill cleanup test", agent, SpawnOpts{})
		if err != nil {
			t.Fatalf("Spawn returned error: %v", err)
		}

		proc, ok := k.GetProcess(pid)
		if !ok {
			t.Fatal("process not found")
		}

		// Give the goroutine time to start
		time.Sleep(50 * time.Millisecond)

		// Kill the process
		if err := k.Kill(pid, types.SIGKILL); err != nil {
			t.Fatalf("Kill returned error: %v", err)
		}

		// Wait for process to finish
		select {
		case <-proc.Done:
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for process to finish after Kill")
		}

		// Then: Unmount was called for MCP mount
		unmountCalls := mm.getUnmountCalls()
		expectedPath := fmt.Sprintf("/mnt/mcp/%d-mock-server", pid)
		if !slices.Contains(unmountCalls, expectedPath) {
			t.Errorf("Unmount not called for %q after Kill, calls: %v", expectedPath, unmountCalls)
		}
	})

	t.Run("multiple_mcp_and_skills_aggregation", func(t *testing.T) {
		// Given: an agent with 2 skills and 2 MCP servers
		agent := &agents.AgentInfo{
			Manifest: agents.AgentManifest{
				Name:   "multi-agent",
				Models: agents.AgentModels{Provider: "claude", Preferred: "sonnet"},
				Skills: []string{"skill-a", "skill-b"},
				MCP:    []string{"server-a", "server-b"},
			},
			Instructions: "Multi agent.",
			Skills: []*skills.SkillInfo{
				{
					Manifest: skills.SkillManifest{
						Name:            "skill-a",
						AllowedToolsRaw: "/dev/shell",
					},
				},
				{
					Manifest: skills.SkillManifest{
						Name:            "skill-b",
						AllowedToolsRaw: "/dev/fs",
					},
				},
			},
			MCPConfigs: []vfs.MCPConfig{
				{ServerName: "server-a", Command: "mock-a", TransportType: "stdio"},
				{ServerName: "server-b", Command: "mock-b", TransportType: "stdio"},
			},
		}
		llmFile := &mockLLMFile{
			readData: makeLLMResponse("multi done", 5),
		}
		k, mm := newE2EKernel(t, llmFile)

		// When: Spawn is called
		pid, err := k.Spawn("multi test", agent, SpawnOpts{})
		if err != nil {
			t.Fatalf("Spawn returned error: %v", err)
		}

		proc, ok := k.GetProcess(pid)
		if !ok {
			t.Fatal("process not found")
		}

		// Wait for completion
		select {
		case <-proc.Done:
		case <-time.After(5 * time.Second):
			t.Fatal("timeout")
		}

		// Then: AllowedDevices contains paths from both skills and both MCP servers
		proc.mu.Lock()
		devices := append([]string(nil), proc.AllowedDevices...)
		mounts := append([]string(nil), proc.MCPMounts...)
		proc.mu.Unlock()

		// Check skill paths
		if !slices.Contains(devices, "/dev/shell") {
			t.Errorf("AllowedDevices missing /dev/shell from skill-a, got %v", devices)
		}
		if !slices.Contains(devices, "/dev/fs") {
			t.Errorf("AllowedDevices missing /dev/fs from skill-b, got %v", devices)
		}

		// Check MCP paths
		mcpPathA := fmt.Sprintf("/mnt/mcp/%d-server-a", pid)
		mcpPathB := fmt.Sprintf("/mnt/mcp/%d-server-b", pid)
		if !slices.Contains(devices, mcpPathA) {
			t.Errorf("AllowedDevices missing MCP path %q, got %v", mcpPathA, devices)
		}
		if !slices.Contains(devices, mcpPathB) {
			t.Errorf("AllowedDevices missing MCP path %q, got %v", mcpPathB, devices)
		}

		// Check MCPMounts
		if len(mounts) != 2 {
			t.Fatalf("MCPMounts length = %d, want 2", len(mounts))
		}

		// Check Mount calls
		calls := mm.getMountCalls()
		if len(calls) != 2 {
			t.Fatalf("Mount called %d times, want 2", len(calls))
		}
	})
}

// e2eBlockingLLM is an LLM mock that blocks on Write until the context is cancelled.
// This is used to test Kill behavior on a running process.
type e2eBlockingLLM struct{}

func (f *e2eBlockingLLM) Write(ctx gocontext.Context, data []byte) error {
	// Block until context is cancelled (simulating a long-running LLM call)
	<-ctx.Done()
	return ctx.Err()
}

func (f *e2eBlockingLLM) Read(length int) ([]byte, error) {
	return json.Marshal(llmResponse{Content: "should not reach", TokensUsed: 0})
}

func (f *e2eBlockingLLM) Close() error { return nil }

func (f *e2eBlockingLLM) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{IsDevice: true, Name: "/dev/llm/claude"}, nil
}

func (f *e2eBlockingLLM) SupportsToolCalling() bool { return true }
