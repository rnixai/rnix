package kernel

import (
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/agents"
	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/config"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// ATDD Tests for Story 3.5: ConfigResolve strace 事件
//
// Tests verify:
// 1. resolveLLMDevice returns (device, source, error) with correct source values
// 2. Spawn emits ConfigResolve strace event with provider/model source tracking
// 3. ConfigResolve event args contain complete provider/model/source fields
// 4. Override relationship (project_default) is visible when applicable

// ============================================================
// AC1: ConfigResolve 事件发射 — provider source 追踪
// ============================================================

func TestATDD_3_5_AC1_ResolveLLMDevice_ReturnsSource_CLI(t *testing.T) {
	t.Parallel()
	k := atddKernelWithProviders("claude", "cursor", "openrouter")
	agent := &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Models: agents.AgentModels{Provider: "claude"},
		},
	}

	// CLI override should produce source="cli"
	device, source, err := k.resolveLLMDevice(agent, "openrouter")
	if err != nil {
		t.Fatalf("AC1: expected no error, got: %v", err)
	}
	if device != "/dev/llm/openrouter" {
		t.Errorf("AC1: expected /dev/llm/openrouter, got %q", device)
	}
	if source != "cli" {
		t.Errorf("AC1: expected source='cli' for CLI override, got %q", source)
	}
}

func TestATDD_3_5_AC1_ResolveLLMDevice_ReturnsSource_Agent(t *testing.T) {
	t.Parallel()
	k := atddKernelWithProviders("claude", "cursor", "openrouter")
	agent := &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Models: agents.AgentModels{Provider: "openrouter"},
		},
	}

	// No CLI override, agent specifies provider -> source="agent"
	device, source, err := k.resolveLLMDevice(agent, "")
	if err != nil {
		t.Fatalf("AC1: expected no error, got: %v", err)
	}
	if device != "/dev/llm/openrouter" {
		t.Errorf("AC1: expected /dev/llm/openrouter, got %q", device)
	}
	if source != "agent" {
		t.Errorf("AC1: expected source='agent' for agent manifest, got %q", source)
	}
}

func TestATDD_3_5_AC1_ResolveLLMDevice_ReturnsSource_Project(t *testing.T) {
	t.Parallel()
	k := atddKernelWithProviders("claude", "cursor")
	k.defaultProvider = "cursor"

	// No CLI, no agent provider -> falls back to defaultProvider -> source="project"
	device, source, err := k.resolveLLMDevice(nil, "")
	if err != nil {
		t.Fatalf("AC1: expected no error, got: %v", err)
	}
	if device != "/dev/llm/cursor" {
		t.Errorf("AC1: expected /dev/llm/cursor, got %q", device)
	}
	if source != "project" {
		t.Errorf("AC1: expected source='project' for defaultProvider, got %q", source)
	}
}

func TestATDD_3_5_AC1_ResolveLLMDevice_ReturnsSource_Default(t *testing.T) {
	t.Parallel()
	k := atddKernelWithProviders("claude", "cursor")
	// No defaultProvider set, no CLI, no agent -> hardcoded "claude" -> source="default"

	device, source, err := k.resolveLLMDevice(nil, "")
	if err != nil {
		t.Fatalf("AC1: expected no error, got: %v", err)
	}
	if device != "/dev/llm/claude" {
		t.Errorf("AC1: expected /dev/llm/claude, got %q", device)
	}
	if source != "default" {
		t.Errorf("AC1: expected source='default' for hardcoded fallback, got %q", source)
	}
}

// ============================================================
// AC2: Model source 追踪
// ============================================================

func TestATDD_3_5_AC2_Spawn_EmitsConfigResolve_ModelSource_CLI(t *testing.T) {
	llmFile := &mockLLMFile{
		readData: makeLLMResponse("model source test", 10),
	}
	k, _, _ := newTestKernel(t, llmFile)

	k.SetProviderResolver(
		func() []string { return []string{"claude", "cursor"} },
		func(name string) bool { return name == "claude" || name == "cursor" },
	)

	agent := &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Name: "test-model-source-agent",
		},
	}

	pid, err := k.Spawn("test model source", agent, SpawnOpts{Model: "hunter-alpha"})
	if err != nil {
		t.Fatalf("AC2: Spawn should succeed, got error: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("AC2: process not found")
	}

	// Drain DebugChan to find ConfigResolve event
	configResolveEvent := drainForConfigResolve(t, proc, "AC2")

	// model_source should be "cli" since opts.Model was set
	modelSource, ok := configResolveEvent.Args["model_source"]
	if !ok {
		t.Fatal("AC2: ConfigResolve event missing 'model_source' arg")
	}
	if modelSource != "cli" {
		t.Errorf("AC2: expected model_source='cli', got %q", modelSource)
	}

	// model should be "hunter-alpha"
	model, ok := configResolveEvent.Args["model"]
	if !ok {
		t.Fatal("AC2: ConfigResolve event missing 'model' arg")
	}
	if model != "hunter-alpha" {
		t.Errorf("AC2: expected model='hunter-alpha', got %q", model)
	}
}

func TestATDD_3_5_AC2_Spawn_EmitsConfigResolve_ModelSource_Driver(t *testing.T) {
	llmFile := &mockLLMFile{
		readData: makeLLMResponse("driver model test", 10),
	}
	k, _, _ := newTestKernel(t, llmFile)

	k.SetProviderResolver(
		func() []string { return []string{"claude"} },
		func(name string) bool { return name == "claude" },
	)

	agent := &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Name: "test-driver-model-agent",
		},
	}

	// No explicit model -> model_source should be "driver"
	pid, err := k.Spawn("test driver model", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("AC2: Spawn should succeed, got error: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("AC2: process not found")
	}

	configResolveEvent := drainForConfigResolve(t, proc, "AC2")

	modelSource, ok := configResolveEvent.Args["model_source"]
	if !ok {
		t.Fatal("AC2: ConfigResolve event missing 'model_source' arg")
	}
	if modelSource != "driver" {
		t.Errorf("AC2: expected model_source='driver' when no explicit model, got %q", modelSource)
	}
}

// ============================================================
// AC3: 覆盖关系可见 — project_default 字段
// ============================================================

func TestATDD_3_5_AC3_Spawn_EmitsConfigResolve_OverrideVisible(t *testing.T) {
	llmFile := &mockLLMFile{
		readData: makeLLMResponse("override test", 10),
	}
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/openrouter", func(_ string, _ vfs.OpenFlag) (vfs.VFSFile, error) {
		return llmFile, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	k.SetProviderResolver(
		func() []string { return []string{"claude", "cursor", "openrouter"} },
		func(name string) bool {
			return name == "claude" || name == "cursor" || name == "openrouter"
		},
	)
	k.SetDefaultProvider("cursor") // project default

	agent := &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Name:   "test-override-agent",
			Models: agents.AgentModels{Provider: "openrouter"}, // agent overrides project default
		},
	}

	pid, err := k.Spawn("test override visibility", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("AC3: Spawn should succeed, got error: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("AC3: process not found")
	}

	configResolveEvent := drainForConfigResolve(t, proc, "AC3")

	// Verify provider is the agent's override
	provider, ok := configResolveEvent.Args["provider"]
	if !ok {
		t.Fatal("AC3: ConfigResolve missing 'provider' arg")
	}
	if provider != "openrouter" {
		t.Errorf("AC3: expected provider='openrouter', got %q", provider)
	}

	// Verify provider_source is "agent"
	providerSource, ok := configResolveEvent.Args["provider_source"]
	if !ok {
		t.Fatal("AC3: ConfigResolve missing 'provider_source' arg")
	}
	if providerSource != "agent" {
		t.Errorf("AC3: expected provider_source='agent', got %q", providerSource)
	}

	// Verify project_default is present since agent overrides it
	projectDefault, ok := configResolveEvent.Args["project_default"]
	if !ok {
		t.Fatal("AC3: ConfigResolve should include 'project_default' when agent overrides project default provider")
	}
	if projectDefault != "cursor" {
		t.Errorf("AC3: expected project_default='cursor', got %q", projectDefault)
	}
}

func TestATDD_3_5_AC3_Spawn_NoProjectDefault_WhenNotOverridden(t *testing.T) {
	llmFile := &mockLLMFile{
		readData: makeLLMResponse("no override test", 10),
	}
	k, _, _ := newTestKernel(t, llmFile)

	k.SetProviderResolver(
		func() []string { return []string{"claude"} },
		func(name string) bool { return name == "claude" },
	)

	agent := &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Name: "test-no-override-agent",
		},
	}

	pid, err := k.Spawn("test no override", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("AC3: Spawn should succeed, got error: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("AC3: process not found")
	}

	configResolveEvent := drainForConfigResolve(t, proc, "AC3")

	// When no override, project_default should NOT be present
	if _, hasProjectDefault := configResolveEvent.Args["project_default"]; hasProjectDefault {
		t.Error("AC3: project_default should NOT be present when provider matches project default")
	}
}

// ============================================================
// AC1 补充: Spawn 发射 ConfigResolve 事件 — 完整字段验证
// ============================================================

func TestATDD_3_5_AC1_Spawn_EmitsConfigResolve_AllFields(t *testing.T) {
	llmFile := &mockLLMFile{
		readData: makeLLMResponse("config resolve test", 10),
	}
	k, _, _ := newTestKernel(t, llmFile)

	k.SetProviderResolver(
		func() []string { return []string{"claude", "cursor"} },
		func(name string) bool { return name == "claude" || name == "cursor" },
	)

	agent := &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Name: "test-config-resolve-agent",
		},
	}

	pid, err := k.Spawn("test config resolve", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("AC1: Spawn should succeed, got error: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("AC1: process not found")
	}

	configResolveEvent := drainForConfigResolve(t, proc, "AC1")

	// Verify required fields exist
	requiredFields := []string{"provider", "provider_source", "model", "model_source"}
	for _, field := range requiredFields {
		if _, ok := configResolveEvent.Args[field]; !ok {
			t.Errorf("AC1: ConfigResolve event missing required field %q", field)
		}
	}

	// Verify provider defaults
	if configResolveEvent.Args["provider"] != "claude" {
		t.Errorf("AC1: expected provider='claude' (default), got %q", configResolveEvent.Args["provider"])
	}
	if configResolveEvent.Args["provider_source"] != "default" {
		t.Errorf("AC1: expected provider_source='default', got %q", configResolveEvent.Args["provider_source"])
	}
}

// ============================================================
// AC6: JSON 模式兼容 — args 字段结构化
// ============================================================

func TestATDD_3_5_AC6_ConfigResolve_ArgsStructured(t *testing.T) {
	llmFile := &mockLLMFile{
		readData: makeLLMResponse("json mode test", 10),
	}
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/openrouter", func(_ string, _ vfs.OpenFlag) (vfs.VFSFile, error) {
		return llmFile, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	k.SetProviderResolver(
		func() []string { return []string{"claude", "openrouter"} },
		func(name string) bool { return name == "claude" || name == "openrouter" },
	)
	k.SetDefaultProvider("claude")

	agent := &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Name:   "test-json-agent",
			Models: agents.AgentModels{Provider: "openrouter"},
		},
	}

	pid, err := k.Spawn("json mode test", agent, SpawnOpts{Model: "test-model"})
	if err != nil {
		t.Fatalf("AC6: Spawn should succeed, got error: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("AC6: process not found")
	}

	configResolveEvent := drainForConfigResolve(t, proc, "AC6")

	args := configResolveEvent.Args

	// provider should be a non-empty string
	if p, ok := args["provider"].(string); !ok || p == "" {
		t.Errorf("AC6: 'provider' should be a non-empty string, got %T=%v", args["provider"], args["provider"])
	}

	// provider_source should be a non-empty string
	if ps, ok := args["provider_source"].(string); !ok || ps == "" {
		t.Errorf("AC6: 'provider_source' should be a non-empty string, got %T=%v", args["provider_source"], args["provider_source"])
	}

	// model should be a string
	if _, ok := args["model"].(string); !ok {
		t.Errorf("AC6: 'model' should be a string, got %T=%v", args["model"], args["model"])
	}

	// model_source should be a non-empty string
	if ms, ok := args["model_source"].(string); !ok || ms == "" {
		t.Errorf("AC6: 'model_source' should be a non-empty string, got %T=%v", args["model_source"], args["model_source"])
	}

	// project_default should be present since agent overrides project default
	if pd, ok := args["project_default"].(string); !ok || pd == "" {
		t.Errorf("AC6: 'project_default' should be a non-empty string when agent overrides, got %T=%v", args["project_default"], args["project_default"])
	}

	// Minimum 4 args
	if len(args) < 4 {
		t.Errorf("AC6: expected at least 4 args, got %d", len(args))
	}
}

// ============================================================
// AC1 补充: ProjectConfig 路径也发射 ConfigResolve 事件
// ============================================================

func TestATDD_3_5_AC1_Spawn_ProjectConfigPath_EmitsConfigResolve(t *testing.T) {
	llmFile := &mockLLMFile{
		readData: makeLLMResponse("project config test", 10),
	}
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/mycloud", func(_ string, _ vfs.OpenFlag) (vfs.VFSFile, error) {
		return llmFile, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	agent := &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Name: "test-project-config-agent",
		},
	}

	// Spawn with ProjectConfig.LLMFileOpener -- this takes the ProjectConfig code path
	pid, err := k.Spawn("test project config path", agent, SpawnOpts{
		ProjectConfig: &config.ProjectConfig{
			DefaultProvider: "mycloud",
			LLMFileOpener: func(provider string, flags int) (any, error) {
				return llmFile, nil
			},
		},
	})
	if err != nil {
		t.Fatalf("AC1: Spawn with ProjectConfig should succeed, got error: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("AC1: process not found")
	}

	configResolveEvent := drainForConfigResolve(t, proc, "AC1-ProjectConfig")

	// Verify provider resolved correctly
	if configResolveEvent.Args["provider"] != "mycloud" {
		t.Errorf("AC1: expected provider='mycloud', got %q", configResolveEvent.Args["provider"])
	}

	// Verify provider_source (ProjectConfig.DefaultProvider -> "project")
	if configResolveEvent.Args["provider_source"] != "project" {
		t.Errorf("AC1: expected provider_source='project' for ProjectConfig.DefaultProvider, got %q",
			configResolveEvent.Args["provider_source"])
	}
}

// ============================================================
// AC7: 签名兼容性回归守护
// ============================================================

func TestATDD_3_5_AC7_ResolveLLMDevice_BackwardCompat(t *testing.T) {
	t.Parallel()
	k := atddKernelWithProviders("claude", "cursor")

	// Verify the 3-return-value form works correctly.
	device, source, err := k.resolveLLMDevice(nil, "")
	if err != nil {
		t.Fatalf("AC7: backward compat test failed: %v", err)
	}
	if device != "/dev/llm/claude" {
		t.Errorf("AC7: expected /dev/llm/claude, got %q", device)
	}
	// source should be "default" since no override and no defaultProvider
	if source != "default" {
		t.Errorf("AC7: expected source='default', got %q", source)
	}
	_ = strings.TrimSpace("") // use strings import
}

// ============================================================
// Helper: drain DebugChan looking for ConfigResolve event
// ============================================================

func drainForConfigResolve(t *testing.T, proc *Process, acLabel string) types.SyscallEvent {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case event, ok := <-proc.DebugChan:
			if !ok {
				t.Fatalf("%s: DebugChan closed before ConfigResolve event found", acLabel)
			}
			if event.Syscall == "ConfigResolve" {
				return event
			}
		case <-timeout:
			t.Fatalf("%s: timed out waiting for ConfigResolve event", acLabel)
		}
	}
}
