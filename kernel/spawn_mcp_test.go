package kernel

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rnixai/rnix/agents"
	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// --- Mock MountManager with call tracking for Spawn MCP tests ---

// mountCall records a single Mount invocation.
type mountCall struct {
	Path   string
	Config vfs.MCPConfig
}

// spawnMockMountManager extends mockMountManager with call tracking.
type spawnMockMountManager struct {
	mu           sync.Mutex
	mountCalls   []mountCall
	unmountCalls []string
	mounted      map[string]bool
	mountFn      func(path string, config vfs.MCPConfig) error
	unmountFn    func(path string) error
}

func newSpawnMockMountManager() *spawnMockMountManager {
	return &spawnMockMountManager{
		mounted: make(map[string]bool),
	}
}

func (m *spawnMockMountManager) Mount(path string, config vfs.MCPConfig) error {
	m.mu.Lock()
	m.mountCalls = append(m.mountCalls, mountCall{Path: path, Config: config})
	m.mu.Unlock()

	if m.mountFn != nil {
		return m.mountFn(path, config)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.mounted[path] {
		return fmt.Errorf("already mounted: %s", path)
	}
	m.mounted[path] = true
	return nil
}

func (m *spawnMockMountManager) Unmount(path string) error {
	m.mu.Lock()
	m.unmountCalls = append(m.unmountCalls, path)
	m.mu.Unlock()

	if m.unmountFn != nil {
		return m.unmountFn(path)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.mounted, path)
	return nil
}

func (m *spawnMockMountManager) UnmountAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k := range m.mounted {
		delete(m.mounted, k)
	}
	return nil
}

func (m *spawnMockMountManager) getMountCalls() []mountCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]mountCall, len(m.mountCalls))
	copy(result, m.mountCalls)
	return result
}

func (m *spawnMockMountManager) getUnmountCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]string, len(m.unmountCalls))
	copy(result, m.unmountCalls)
	return result
}

// --- Test helpers ---

// newSpawnTestKernel creates a Kernel with a mock LLM device and the given MountManager.
func newSpawnTestKernel(t testing.TB, mountMgr MountManager) *KernelImpl {
	t.Helper()
	reg := vfs.NewDeviceRegistry()
	llmFile := &mockLLMFile{
		readData: []byte(`{"content":"test","tokens_used":1}`),
	}
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return llmFile, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	if mountMgr != nil {
		k.mountMgr = mountMgr
	}
	t.Cleanup(func() { k.Shutdown() })
	return k
}

// testAgentWithMCP creates an AgentInfo with the given MCPConfigs.
func testAgentWithMCP(mcpConfigs []vfs.MCPConfig) *agents.AgentInfo {
	return &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Name:        "test-mcp-agent",
			Description: "test agent with MCP",
			Models: agents.AgentModels{
				Provider:  "claude",
				Preferred: "sonnet",
			},
		},
		Instructions: "Test agent.",
		MCPConfigs:   mcpConfigs,
	}
}

// testAgentWithoutMCP creates an AgentInfo without MCPConfigs.
func testAgentWithoutMCP() *agents.AgentInfo {
	return &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Name:        "test-agent",
			Description: "test agent without MCP",
			Models: agents.AgentModels{
				Provider:  "claude",
				Preferred: "sonnet",
			},
		},
		Instructions: "Test agent.",
	}
}

// --- Spawn Auto-Mount MCP Tests ---

func TestSpawn_AutoMountMCP(t *testing.T) {
	t.Run("spawn with mcp configs mounts all", func(t *testing.T) {
		// Given: a Kernel with a MountManager and an agent with 2 MCP configs
		mm := newSpawnMockMountManager()
		k := newSpawnTestKernel(t, mm)

		mcpConfigs := []vfs.MCPConfig{
			{ServerName: "github", Command: "npx", TransportType: "stdio"},
			{ServerName: "slack", Command: "npx", TransportType: "stdio"},
		}
		agent := testAgentWithMCP(mcpConfigs)

		// When: Spawn is called
		pid, err := k.Spawn("test intent", agent, SpawnOpts{})

		// Then: no error, both MCP servers are mounted
		if err != nil {
			t.Fatalf("Spawn returned error: %v", err)
		}
		if pid == 0 {
			t.Fatal("Spawn returned PID 0")
		}

		calls := mm.getMountCalls()
		if len(calls) != 2 {
			t.Fatalf("Mount called %d times, want 2", len(calls))
		}
		if calls[0].Config.ServerName != "github" {
			t.Errorf("Mount call[0].Config.ServerName = %q, want %q", calls[0].Config.ServerName, "github")
		}
		if calls[1].Config.ServerName != "slack" {
			t.Errorf("Mount call[1].Config.ServerName = %q, want %q", calls[1].Config.ServerName, "slack")
		}
	})

	t.Run("spawn with mcp configs records mount paths", func(t *testing.T) {
		// Given: a Kernel with MountManager and an agent with MCP configs
		mm := newSpawnMockMountManager()
		k := newSpawnTestKernel(t, mm)

		mcpConfigs := []vfs.MCPConfig{
			{ServerName: "github", Command: "npx", TransportType: "stdio"},
		}
		agent := testAgentWithMCP(mcpConfigs)

		// When: Spawn is called
		pid, err := k.Spawn("test intent", agent, SpawnOpts{})
		if err != nil {
			t.Fatalf("Spawn returned error: %v", err)
		}

		// Then: Process.MCPMounts records the mount path
		proc, ok := k.GetProcess(pid)
		if !ok {
			t.Fatal("process not found in table")
		}
		proc.mu.Lock()
		mounts := append([]string(nil), proc.MCPMounts...)
		proc.mu.Unlock()

		if len(mounts) != 1 {
			t.Fatalf("MCPMounts length = %d, want 1", len(mounts))
		}
	})

	t.Run("spawn mount path format is pid-name", func(t *testing.T) {
		// Given: a Kernel with MountManager
		mm := newSpawnMockMountManager()
		k := newSpawnTestKernel(t, mm)

		mcpConfigs := []vfs.MCPConfig{
			{ServerName: "github", Command: "npx", TransportType: "stdio"},
		}
		agent := testAgentWithMCP(mcpConfigs)

		// When: Spawn is called
		pid, err := k.Spawn("test intent", agent, SpawnOpts{})
		if err != nil {
			t.Fatalf("Spawn returned error: %v", err)
		}

		// Then: mount path follows /mnt/mcp/{pid}-{server-name}/ format
		calls := mm.getMountCalls()
		if len(calls) != 1 {
			t.Fatalf("Mount called %d times, want 1", len(calls))
		}
		expectedPrefix := fmt.Sprintf("/mnt/mcp/%d-github", pid)
		if !strings.HasPrefix(calls[0].Path, expectedPrefix) {
			t.Errorf("Mount path = %q, want prefix %q", calls[0].Path, expectedPrefix)
		}
	})

	t.Run("spawn mount failure rolls back previous mounts", func(t *testing.T) {
		// Given: a MountManager that fails on the second Mount
		callCount := 0
		mm := newSpawnMockMountManager()
		mm.mountFn = func(path string, config vfs.MCPConfig) error {
			callCount++
			if callCount == 2 {
				return fmt.Errorf("mount failed: connection timeout")
			}
			mm.mu.Lock()
			mm.mounted[path] = true
			mm.mu.Unlock()
			return nil
		}
		k := newSpawnTestKernel(t, mm)

		mcpConfigs := []vfs.MCPConfig{
			{ServerName: "github", Command: "npx", TransportType: "stdio"},
			{ServerName: "slack", Command: "npx", TransportType: "stdio"},
		}
		agent := testAgentWithMCP(mcpConfigs)

		// When: Spawn is called
		_, err := k.Spawn("test intent", agent, SpawnOpts{})

		// Then: error is returned
		if err == nil {
			t.Fatal("expected error for mount failure, got nil")
		}

		// And: previously mounted paths are unmounted (rollback)
		unmountCalls := mm.getUnmountCalls()
		if len(unmountCalls) != 1 {
			t.Fatalf("Unmount called %d times for rollback, want 1", len(unmountCalls))
		}
	})

	t.Run("spawn mount failure returns syscall error", func(t *testing.T) {
		// Given: a MountManager that always fails
		mm := newSpawnMockMountManager()
		mm.mountFn = func(path string, config vfs.MCPConfig) error {
			return fmt.Errorf("connection refused")
		}
		k := newSpawnTestKernel(t, mm)

		mcpConfigs := []vfs.MCPConfig{
			{ServerName: "github", Command: "npx", TransportType: "stdio"},
		}
		agent := testAgentWithMCP(mcpConfigs)

		// When: Spawn is called
		_, err := k.Spawn("test intent", agent, SpawnOpts{})

		// Then: *SyscallError is returned
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var syscallErr *SyscallError
		if !containsSyscallError(err, &syscallErr) {
			t.Fatalf("expected *SyscallError, got %T: %v", err, err)
		}
	})

	t.Run("spawn without mcp configs skips mount", func(t *testing.T) {
		// Given: a Kernel with MountManager and an agent without MCP
		mm := newSpawnMockMountManager()
		k := newSpawnTestKernel(t, mm)

		agent := testAgentWithoutMCP()

		// When: Spawn is called
		pid, err := k.Spawn("test intent", agent, SpawnOpts{})

		// Then: no error, Mount is not called
		if err != nil {
			t.Fatalf("Spawn returned error: %v", err)
		}
		if pid == 0 {
			t.Fatal("Spawn returned PID 0")
		}
		calls := mm.getMountCalls()
		if len(calls) != 0 {
			t.Errorf("Mount called %d times, want 0 for agent without MCP", len(calls))
		}
	})

	t.Run("spawn with nil mount manager and mcp returns error", func(t *testing.T) {
		// Given: a Kernel without MountManager (nil) and an agent with MCP configs
		k := newSpawnTestKernel(t, nil)

		mcpConfigs := []vfs.MCPConfig{
			{ServerName: "github", Command: "npx", TransportType: "stdio"},
		}
		agent := testAgentWithMCP(mcpConfigs)

		// When: Spawn is called
		_, err := k.Spawn("test intent", agent, SpawnOpts{})

		// Then: error is returned (ErrInternal)
		if err == nil {
			t.Fatal("expected error for nil mountMgr with MCP configs, got nil")
		}
		var syscallErr *SyscallError
		if !containsSyscallError(err, &syscallErr) {
			t.Fatalf("expected *SyscallError, got %T: %v", err, err)
		}
		if syscallErr.Code != types.ErrInternal {
			t.Errorf("SyscallError.Code = %v, want %v", syscallErr.Code, types.ErrInternal)
		}
	})
}

// --- Process Exit Auto-Unmount MCP Tests ---

func TestFinishProcess_AutoUnmountMCP(t *testing.T) {
	t.Run("process exit unmounts all mcp mounts", func(t *testing.T) {
		// Given: a Kernel with MountManager and a process with MCPMounts
		mm := newSpawnMockMountManager()
		k := newSpawnTestKernel(t, mm)

		mcpConfigs := []vfs.MCPConfig{
			{ServerName: "github", Command: "npx", TransportType: "stdio"},
			{ServerName: "slack", Command: "npx", TransportType: "stdio"},
		}
		agent := testAgentWithMCP(mcpConfigs)

		pid, err := k.Spawn("test intent", agent, SpawnOpts{})
		if err != nil {
			t.Fatalf("Spawn returned error: %v", err)
		}

		// When: the process completes (wait for it to finish)
		proc, ok := k.GetProcess(pid)
		if !ok {
			t.Fatal("process not found")
		}
		select {
		case <-proc.Done:
			// Process finished
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for process to finish")
		}

		// Then: Unmount is called for each MCPMount path
		unmountCalls := mm.getUnmountCalls()
		if len(unmountCalls) < 2 {
			t.Fatalf("Unmount called %d times, want >= 2", len(unmountCalls))
		}
	})

	t.Run("unmount failure does not block process exit", func(t *testing.T) {
		// Given: a MountManager where Unmount always fails
		mm := newSpawnMockMountManager()
		mm.unmountFn = func(path string) error {
			return fmt.Errorf("unmount failed: device busy")
		}
		k := newSpawnTestKernel(t, mm)

		mcpConfigs := []vfs.MCPConfig{
			{ServerName: "github", Command: "npx", TransportType: "stdio"},
		}
		agent := testAgentWithMCP(mcpConfigs)

		pid, err := k.Spawn("test intent", agent, SpawnOpts{})
		if err != nil {
			t.Fatalf("Spawn returned error: %v", err)
		}

		// When: the process completes
		proc, ok := k.GetProcess(pid)
		if !ok {
			t.Fatal("process not found")
		}
		select {
		case exit := <-proc.Done:
			// Then: process exits despite Unmount failure (exit is not blocked)
			// The exit code should reflect the reasoning result, not the Unmount failure
			_ = exit // Process completed
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for process to finish - Unmount failure blocked exit")
		}

		// And: Unmount was still attempted
		unmountCalls := mm.getUnmountCalls()
		if len(unmountCalls) == 0 {
			t.Error("Unmount was not called despite process having MCPMounts")
		}
	})
}

// --- Story 9.3: AllowedDevices MCP Path Tests (AC #8) ---

func TestSpawn_AllowedDevices_IncludesMCPPaths(t *testing.T) {
	t.Run("spawn appends mcp mount paths to AllowedDevices", func(t *testing.T) {
		// Given: a Kernel with MountManager and an agent with MCP configs
		mm := newSpawnMockMountManager()
		k := newSpawnTestKernel(t, mm)

		mcpConfigs := []vfs.MCPConfig{
			{ServerName: "github", Command: "npx", TransportType: "stdio"},
			{ServerName: "slack", Command: "npx", TransportType: "stdio"},
		}
		agent := testAgentWithMCP(mcpConfigs)

		// When: Spawn is called
		pid, err := k.Spawn("test intent", agent, SpawnOpts{})
		if err != nil {
			t.Fatalf("Spawn returned error: %v", err)
		}

		// Then: AllowedDevices contains MCP mount paths
		proc, ok := k.GetProcess(pid)
		if !ok {
			t.Fatal("process not found in table")
		}
		proc.mu.Lock()
		devices := append([]string(nil), proc.AllowedDevices...)
		proc.mu.Unlock()

		// Check that MCP paths are in AllowedDevices
		githubPath := fmt.Sprintf("/mnt/mcp/%d-github", pid)
		slackPath := fmt.Sprintf("/mnt/mcp/%d-slack", pid)

		foundGithub := false
		foundSlack := false
		for _, d := range devices {
			if d == githubPath {
				foundGithub = true
			}
			if d == slackPath {
				foundSlack = true
			}
		}
		if !foundGithub {
			t.Errorf("AllowedDevices missing github MCP path %q, got %v", githubPath, devices)
		}
		if !foundSlack {
			t.Errorf("AllowedDevices missing slack MCP path %q, got %v", slackPath, devices)
		}
	})

	t.Run("spawn without mcp does not add mcp paths to AllowedDevices", func(t *testing.T) {
		// Given: a Kernel with MountManager and an agent without MCP
		mm := newSpawnMockMountManager()
		k := newSpawnTestKernel(t, mm)

		agent := testAgentWithoutMCP()

		// When: Spawn is called
		pid, err := k.Spawn("test intent", agent, SpawnOpts{})
		if err != nil {
			t.Fatalf("Spawn returned error: %v", err)
		}

		// Then: AllowedDevices does not contain any /mnt/mcp/ paths
		proc, ok := k.GetProcess(pid)
		if !ok {
			t.Fatal("process not found")
		}
		proc.mu.Lock()
		devices := append([]string(nil), proc.AllowedDevices...)
		proc.mu.Unlock()

		for _, d := range devices {
			if strings.HasPrefix(d, "/mnt/mcp/") {
				t.Errorf("AllowedDevices should not contain MCP paths for agent without MCP, found %q", d)
			}
		}
	})

	t.Run("mcp subpath matches AllowedDevices prefix check", func(t *testing.T) {
		// Given: a process with MCP mount path in AllowedDevices
		mm := newSpawnMockMountManager()
		k := newSpawnTestKernel(t, mm)

		mcpConfigs := []vfs.MCPConfig{
			{ServerName: "github", Command: "npx", TransportType: "stdio"},
		}
		agent := testAgentWithMCP(mcpConfigs)

		pid, err := k.Spawn("test intent", agent, SpawnOpts{})
		if err != nil {
			t.Fatalf("Spawn returned error: %v", err)
		}

		proc, ok := k.GetProcess(pid)
		if !ok {
			t.Fatal("process not found")
		}
		proc.mu.Lock()
		devices := append([]string(nil), proc.AllowedDevices...)
		proc.mu.Unlock()

		// When: checking if MCP tool subpath matches via prefix
		mcpToolPath := fmt.Sprintf("/mnt/mcp/%d-github/tools/create-issue", pid)
		mcpResourcePath := fmt.Sprintf("/mnt/mcp/%d-github/resources/repo://a/b", pid)
		mcpRootPath := fmt.Sprintf("/mnt/mcp/%d-github/", pid)

		// Then: all MCP subpaths match the base mount path prefix
		checkPaths := []string{mcpToolPath, mcpResourcePath, mcpRootPath}
		for _, checkPath := range checkPaths {
			matched := false
			for _, dev := range devices {
				if checkPath == dev || strings.HasPrefix(checkPath, dev+"/") {
					matched = true
					break
				}
			}
			if !matched {
				t.Errorf("path %q did not match any AllowedDevices %v", checkPath, devices)
			}
		}
	})
}

// containsSyscallError is a helper to unwrap and check for *SyscallError.
func containsSyscallError(err error, target **SyscallError) bool {
	for err != nil {
		if se, ok := err.(*SyscallError); ok {
			*target = se
			return true
		}
		// Check if error implements Unwrap
		if u, ok := err.(interface{ Unwrap() error }); ok {
			err = u.Unwrap()
		} else {
			return false
		}
	}
	return false
}
