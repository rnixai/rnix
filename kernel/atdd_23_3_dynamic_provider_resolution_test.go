package kernel

import (
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/agents"
	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/vfs"
)

// ATDD Tests for Story 23.3: Provider 动态解析与白名单移除
//
// GREEN PHASE: All tests updated to use KernelImpl.resolveLLMDevice() method
// with SetProviderResolver callback injection. t.Skip() removed.

// atddKernelWithProviders creates a minimal KernelImpl with mock provider resolver for ATDD tests.
func atddKernelWithProviders(providers ...string) *KernelImpl {
	k := &KernelImpl{}
	if len(providers) > 0 {
		providerSet := make(map[string]bool)
		for _, p := range providers {
			providerSet[p] = true
		}
		k.SetProviderResolver(
			func() []string {
				names := make([]string, 0, len(providerSet))
				for n := range providerSet {
					names = append(names, n)
				}
				sort.Strings(names)
				return names
			},
			func(name string) bool { return providerSet[name] },
		)
	}
	return k
}

// ============================================================
// AC1: 移除硬编码白名单，改为查询 DriverRegistry
// ============================================================

func TestATDD_23_3_AC1_NoHardcodedWhitelist_DynamicProvider(t *testing.T) {
	t.Parallel()
	k := atddKernelWithProviders("claude", "cursor", "ollama")
	agent := &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Models: agents.AgentModels{Provider: "ollama"},
		},
	}

	device, _, err := k.resolveLLMDevice(agent, "")
	if err != nil {
		t.Fatalf("AC1: registered provider 'ollama' should be accepted by dynamic resolution, got error: %v", err)
	}
	if device != "/dev/llm/ollama" {
		t.Errorf("AC1: expected /dev/llm/ollama, got %q", device)
	}
}

func TestATDD_23_3_AC1_NilResolverAllowsAll(t *testing.T) {
	t.Parallel()
	k := &KernelImpl{} // No resolver injected — backward compat
	agent := &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Models: agents.AgentModels{Provider: "anydriver"},
		},
	}

	device, _, err := k.resolveLLMDevice(agent, "")
	if err != nil {
		t.Fatalf("AC1: nil resolver should allow all providers (backward compat), got error: %v", err)
	}
	if device != "/dev/llm/anydriver" {
		t.Errorf("AC1: expected /dev/llm/anydriver, got %q", device)
	}
}

// ============================================================
// AC2: Agent 指定 provider: ollama → 返回 /dev/llm/ollama
// ============================================================

func TestATDD_23_3_AC2_RegisteredProviderReturnsCorrectPath(t *testing.T) {
	llmFile := &mockLLMFile{
		readData: makeLLMResponse("ollama response", 10),
	}
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/ollama", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return llmFile, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	k.SetProviderResolver(
		func() []string { return []string{"claude", "cursor", "ollama"} },
		func(name string) bool {
			return name == "claude" || name == "cursor" || name == "ollama"
		},
	)

	agent := &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Name:   "test-ollama-agent",
			Models: agents.AgentModels{Provider: "ollama"},
		},
	}

	pid, err := k.Spawn("test ollama provider", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("AC2: Spawn with provider 'ollama' should succeed when registered, got error: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("AC2: process not found after spawn")
	}

	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("AC2: expected exit code 0, got %d: %s", exit.Code, exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("AC2: timed out waiting for process completion")
	}

	if proc.Result != "ollama response" {
		t.Errorf("AC2: expected 'ollama response', got %q", proc.Result)
	}
}

func TestATDD_23_3_AC2_DirectResolve_RegisteredProvider(t *testing.T) {
	t.Parallel()
	k := atddKernelWithProviders("claude", "cursor", "ollama")
	agent := &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Models: agents.AgentModels{Provider: "ollama"},
		},
	}

	device, _, err := k.resolveLLMDevice(agent, "")
	if err != nil {
		t.Fatalf("AC2: expected no error for registered provider 'ollama', got: %v", err)
	}
	if device != "/dev/llm/ollama" {
		t.Errorf("AC2: expected /dev/llm/ollama, got %q", device)
	}
}

// ============================================================
// AC3: 不存在的 provider → 清晰错误，列出所有已注册 provider
// ============================================================

func TestATDD_23_3_AC3_UnregisteredProviderClearError(t *testing.T) {
	t.Parallel()
	k := atddKernelWithProviders("claude", "cursor", "ollama")
	agent := &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Models: agents.AgentModels{Provider: "nonexist"},
		},
	}

	_, _, err := k.resolveLLMDevice(agent, "")
	if err == nil {
		t.Fatal("AC3: expected error for unregistered provider 'nonexist'")
	}

	errMsg := err.Error()

	if !strings.Contains(errMsg, `"nonexist"`) {
		t.Errorf("AC3: error should quote the provider name, got: %s", errMsg)
	}

	if !strings.Contains(errMsg, "(available:") {
		t.Errorf("AC3: error should contain '(available:' listing registered providers, got: %s", errMsg)
	}

	if !strings.Contains(errMsg, "claude") {
		t.Errorf("AC3: error should list 'claude' as available provider, got: %s", errMsg)
	}
}

func TestATDD_23_3_AC3_ErrorListsSortedProviders(t *testing.T) {
	t.Parallel()
	k := atddKernelWithProviders("claude", "cursor", "ollama")
	agent := &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Models: agents.AgentModels{Provider: "nonexist"},
		},
	}

	_, _, err := k.resolveLLMDevice(agent, "")
	if err == nil {
		t.Fatal("AC3: expected error for unregistered provider")
	}

	errMsg := err.Error()

	// DriverRegistry.Names() returns sorted names.
	// Error format: unsupported LLM provider: "nonexist" (available: claude, cursor, ollama)
	for _, expected := range []string{"claude", "cursor", "ollama"} {
		if !strings.Contains(errMsg, expected) {
			t.Errorf("AC3: error should list '%s' as available, got: %s", expected, errMsg)
		}
	}
}

func TestATDD_23_3_AC3_SpawnReturnsDriverError(t *testing.T) {
	llmFile := &mockLLMFile{
		readData: makeLLMResponse("should not reach", 10),
	}
	k, _, _ := newTestKernel(t, llmFile)

	k.SetProviderResolver(
		func() []string { return []string{"claude", "cursor"} },
		func(name string) bool { return name == "claude" || name == "cursor" },
	)

	agent := &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Name:   "bad-provider-agent",
			Models: agents.AgentModels{Provider: "nonexist"},
		},
	}

	_, err := k.Spawn("test nonexist provider", agent, SpawnOpts{})
	if err == nil {
		t.Fatal("AC3: Spawn with unregistered provider should return error")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "(available:") {
		t.Errorf("AC3: Spawn error should contain available providers list, got: %s", errMsg)
	}
}

// ============================================================
// AC4: Agent 未指定 provider（空字符串）→ 使用默认 "claude"
// ============================================================

func TestATDD_23_3_AC4_EmptyProviderUsesDefault(t *testing.T) {
	t.Parallel()
	k := atddKernelWithProviders("claude", "cursor")

	device, _, err := k.resolveLLMDevice(nil, "")
	if err != nil {
		t.Fatalf("AC4: empty provider with nil agent should default to claude, got error: %v", err)
	}
	if device != "/dev/llm/claude" {
		t.Errorf("AC4: expected /dev/llm/claude as default, got %q", device)
	}

	agent := &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Models: agents.AgentModels{Provider: ""},
		},
	}
	device, _, err = k.resolveLLMDevice(agent, "")
	if err != nil {
		t.Fatalf("AC4: agent with empty provider should default to claude, got error: %v", err)
	}
	if device != "/dev/llm/claude" {
		t.Errorf("AC4: expected /dev/llm/claude, got %q", device)
	}
}

func TestATDD_23_3_AC4_DefaultThroughSpawn(t *testing.T) {
	llmFile := &mockLLMFile{
		readData: makeLLMResponse("default provider response", 10),
	}
	k, _, _ := newTestKernel(t, llmFile)

	k.SetProviderResolver(
		func() []string { return []string{"claude", "cursor"} },
		func(name string) bool { return name == "claude" || name == "cursor" },
	)

	agent := &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Name:   "default-agent",
			Models: agents.AgentModels{Provider: ""},
		},
	}

	pid, err := k.Spawn("test default provider", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("AC4: Spawn with empty provider should succeed, got error: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("AC4: process not found")
	}

	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("AC4: expected exit code 0, got %d: %s", exit.Code, exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("AC4: timed out")
	}

	if proc.Result != "default provider response" {
		t.Errorf("AC4: expected 'default provider response', got %q", proc.Result)
	}
}

// ============================================================
// AC5: CLI --provider=groq 覆盖 agent.yaml 的 provider 配置
// ============================================================

func TestATDD_23_3_AC5_CLIProviderOverridesAgent(t *testing.T) {
	t.Parallel()
	k := atddKernelWithProviders("claude", "cursor", "groq")
	agent := &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Models: agents.AgentModels{Provider: "claude"},
		},
	}

	// CLI override: --provider=groq
	device, _, err := k.resolveLLMDevice(agent, "groq")
	if err != nil {
		t.Fatalf("AC5: CLI provider override 'groq' should be accepted when registered, got error: %v", err)
	}
	if device != "/dev/llm/groq" {
		t.Errorf("AC5: expected /dev/llm/groq, got %q", device)
	}
}

func TestATDD_23_3_AC5_CLIOverrideThroughSpawn(t *testing.T) {
	llmFile := &mockLLMFile{
		readData: makeLLMResponse("groq response", 10),
	}
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/groq", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return llmFile, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	k.SetProviderResolver(
		func() []string { return []string{"claude", "cursor", "groq"} },
		func(name string) bool {
			return name == "claude" || name == "cursor" || name == "groq"
		},
	)

	agent := &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Name:   "test-agent",
			Models: agents.AgentModels{Provider: "claude"},
		},
	}

	// SpawnOpts.Provider = "groq" simulates CLI --provider=groq
	pid, err := k.Spawn("test CLI override", agent, SpawnOpts{Provider: "groq"})
	if err != nil {
		t.Fatalf("AC5: Spawn with --provider=groq should succeed when registered, got error: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("AC5: process not found")
	}

	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("AC5: expected exit code 0, got %d: %s", exit.Code, exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("AC5: timed out")
	}

	if proc.Result != "groq response" {
		t.Errorf("AC5: expected 'groq response', got %q", proc.Result)
	}
}

func TestATDD_23_3_AC5_OverridePrecedence(t *testing.T) {
	t.Parallel()
	k := atddKernelWithProviders("claude", "cursor", "ollama")
	agent := &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Models: agents.AgentModels{Provider: "ollama"},
		},
	}

	// Agent says "ollama", CLI override says "cursor"
	// Override should win.
	device, _, err := k.resolveLLMDevice(agent, "cursor")
	if err != nil {
		t.Fatalf("AC5: override 'cursor' should take precedence over agent 'ollama', got error: %v", err)
	}
	if device != "/dev/llm/cursor" {
		t.Errorf("AC5: expected /dev/llm/cursor (override wins), got %q", device)
	}
}
