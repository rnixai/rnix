package kernel

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/agents"
	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/vfs"
)

// === Init Bootstrap Test Infrastructure ===

// mockServiceInitializer is a controllable ServiceInitializer for testing.
type mockServiceInitializer struct {
	name string
	err  error // if non-nil, Init returns this error
}

func (m *mockServiceInitializer) Name() string                  { return m.name }
func (m *mockServiceInitializer) Init(cfg map[string]any) error { return m.err }

// newInitTestKernel creates a kernel suitable for init bootstrap tests.
// It uses a normalFile LLM device so SpawnSupervisor children can run.
func newInitTestKernel(t testing.TB) *KernelImpl {
	return newSimpleTestKernel(t, &normalFile{})
}

// mockAgentLoader returns an AgentLoaderFunc that resolves known agent names.
func mockAgentLoader(known map[string]*agents.AgentInfo) AgentLoaderFunc {
	return func(name string) (*agents.AgentInfo, error) {
		if info, ok := known[name]; ok {
			return info, nil
		}
		return nil, fmt.Errorf("agent not found: %s", name)
	}
}

// === Unit Tests (P0) ===

// 10.5-UNIT-001: 默认配置（无服务无 Supervisor）→ Bootstrap 成功，InitResult 为空
func TestBootstrap_DefaultConfig_EmptyResult(t *testing.T) {
	k := newInitTestKernel(t)
	cfg := DefaultInitConfig()

	result, err := Bootstrap(k, cfg, mockAgentLoader(nil))
	if err != nil {
		t.Fatalf("Bootstrap with default config should succeed, got error: %v", err)
	}
	if len(result.Started) != 0 {
		t.Fatalf("Expected empty Started list, got %v", result.Started)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("Expected empty Warnings list, got %v", result.Warnings)
	}
	if len(result.Failed) != 0 {
		t.Fatalf("Expected empty Failed list, got %v", result.Failed)
	}
}

// 10.5-UNIT-002: required 服务失败 → Bootstrap 返回 error，含服务名和恢复建议
func TestBootstrap_RequiredServiceFailure_ReturnsError(t *testing.T) {
	k := newInitTestKernel(t)
	cfg := &InitConfig{
		Services: []ServiceConfig{
			{
				Name:     "critical-service",
				Type:     "skill_registry",
				Required: true,
				Config:   map[string]any{"scan_path": "/nonexistent"},
			},
		},
	}

	// Override service registry to inject a failing service.
	origRegistry := serviceRegistry
	serviceRegistry = map[string]func() ServiceInitializer{
		"skill_registry": func() ServiceInitializer {
			return &mockServiceInitializer{
				name: "critical-service",
				err:  fmt.Errorf("scan_path does not exist"),
			}
		},
	}
	defer func() { serviceRegistry = origRegistry }()

	_, err := Bootstrap(k, cfg, mockAgentLoader(nil))
	if err == nil {
		t.Fatal("Expected error when required service fails, got nil")
	}
	if !strings.Contains(err.Error(), "critical-service") {
		t.Fatalf("Error should contain service name 'critical-service', got: %v", err)
	}
}

// 10.5-UNIT-003: optional 服务失败 → Bootstrap 成功，Warnings 含警告
func TestBootstrap_OptionalServiceFailure_SucceedsWithWarnings(t *testing.T) {
	k := newInitTestKernel(t)
	cfg := &InitConfig{
		Services: []ServiceConfig{
			{
				Name:     "optional-service",
				Type:     "mcp_manager",
				Required: false,
				Config:   map[string]any{"config_path": "/nonexistent"},
			},
		},
	}

	origRegistry := serviceRegistry
	serviceRegistry = map[string]func() ServiceInitializer{
		"mcp_manager": func() ServiceInitializer {
			return &mockServiceInitializer{
				name: "optional-service",
				err:  fmt.Errorf("config file not found"),
			}
		},
	}
	defer func() { serviceRegistry = origRegistry }()

	result, err := Bootstrap(k, cfg, mockAgentLoader(nil))
	if err != nil {
		t.Fatalf("Bootstrap should succeed with optional failure, got error: %v", err)
	}
	if len(result.Warnings) == 0 {
		t.Fatal("Expected at least one warning for optional service failure")
	}

	found := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "optional-service") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Warnings should mention 'optional-service', got: %v", result.Warnings)
	}
}

// 10.5-UNIT-004: Supervisor 树从 config 构建 → SpawnSupervisor 成功
func TestBootstrap_SupervisorTreeConstruction_Succeeds(t *testing.T) {
	k := newInitTestKernel(t)
	cfg := &InitConfig{
		Supervisors: []SupervisorConfig{
			{
				Name:        "test-supervisor",
				Strategy:    "one_for_one",
				MaxRestarts: 3,
				MaxWindow:   10 * time.Second,
				Required:    true,
				Children: []ChildConfig{
					{
						Name:          "worker-1",
						Intent:        "test worker",
						Model:         "haiku",
						ContextBudget: 1000,
						Restart:       "temporary",
					},
				},
			},
		},
	}

	result, err := Bootstrap(k, cfg, mockAgentLoader(nil))
	if err != nil {
		t.Fatalf("Bootstrap with supervisor config should succeed, got error: %v", err)
	}
	if len(result.Started) == 0 {
		t.Fatal("Expected at least one started entry for the supervisor")
	}
}

// 10.5-UNIT-005: required Supervisor 构建失败 → Bootstrap 返回 error
func TestBootstrap_RequiredSupervisorFailure_ReturnsError(t *testing.T) {
	// Create a kernel where VFS Open always fails, so SpawnSupervisor fails.
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag) (vfs.VFSFile, error) {
		return nil, fmt.Errorf("device unavailable")
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)

	cfg := &InitConfig{
		Supervisors: []SupervisorConfig{
			{
				Name:        "failing-supervisor",
				Strategy:    "one_for_one",
				MaxRestarts: 3,
				MaxWindow:   10 * time.Second,
				Required:    true,
				Children: []ChildConfig{
					{
						Name:    "child-1",
						Intent:  "will fail",
						Restart: "permanent",
					},
				},
			},
		},
	}

	_, err := Bootstrap(k, cfg, mockAgentLoader(nil))
	if err == nil {
		t.Fatal("Expected error when required supervisor fails, got nil")
	}
	if !strings.Contains(err.Error(), "failing-supervisor") {
		t.Fatalf("Error should contain supervisor name, got: %v", err)
	}
}

// === Integration Tests (P1) ===

// 10.5-INT-001: 混合场景：2 required + 1 optional 服务 + 1 Supervisor → 全部成功
func TestBootstrap_MixedScenario_AllSucceed(t *testing.T) {
	k := newInitTestKernel(t)

	cfg := &InitConfig{
		Services: []ServiceConfig{
			{Name: "svc-required-1", Type: "log_aggregator", Required: true},
			{Name: "svc-required-2", Type: "skill_registry", Required: true, Config: map[string]any{"scan_path": "lib/skills"}},
			{Name: "svc-optional-1", Type: "mcp_manager", Required: false, Config: map[string]any{"config_path": "mcp.yaml"}},
		},
		Supervisors: []SupervisorConfig{
			{
				Name:        "system-supervisor",
				Strategy:    "one_for_one",
				MaxRestarts: 5,
				MaxWindow:   60 * time.Second,
				Required:    true,
				Children: []ChildConfig{
					{Name: "monitor", Intent: "系统监控", Model: "haiku", ContextBudget: 1000, Restart: "temporary"},
				},
			},
		},
	}

	// Use real (no-op) service implementations for integration test.
	// The default service registry should handle these types.
	result, err := Bootstrap(k, cfg, mockAgentLoader(nil))
	if err != nil {
		t.Fatalf("Mixed scenario should succeed, got error: %v", err)
	}

	// Verify all required services started
	if len(result.Started) < 2 {
		t.Fatalf("Expected at least 2 started services, got %d: %v", len(result.Started), result.Started)
	}
}

// 10.5-INT-002: AgentLoaderFunc 加载 agent → ChildSpec.Agent 正确设置
func TestBootstrap_AgentLoaderFunc_SetsChildAgent(t *testing.T) {
	k := newInitTestKernel(t)

	testAgent := &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Name: "test-agent",
		},
		Instructions: "test instructions",
	}

	loader := mockAgentLoader(map[string]*agents.AgentInfo{
		"test-agent": testAgent,
	})

	cfg := &InitConfig{
		Supervisors: []SupervisorConfig{
			{
				Name:        "agent-supervisor",
				Strategy:    "one_for_one",
				MaxRestarts: 3,
				MaxWindow:   10 * time.Second,
				Required:    true,
				Children: []ChildConfig{
					{
						Name:    "agent-child",
						Intent:  "test with agent",
						Agent:   "test-agent",
						Model:   "haiku",
						Restart: "temporary",
					},
				},
			},
		},
	}

	result, err := Bootstrap(k, cfg, loader)
	if err != nil {
		t.Fatalf("Bootstrap with agent loader should succeed, got error: %v", err)
	}
	if len(result.Started) == 0 {
		t.Fatal("Expected at least one started entry")
	}
}

// === LoadInitConfig Tests (CR fix: H1) ===

// TestLoadInitConfig_FileNotExist: 文件不存在时返回 DefaultInitConfig
func TestLoadInitConfig_FileNotExist(t *testing.T) {
	cfg, err := LoadInitConfig("/nonexistent/path/rnix-init.yaml")
	if err != nil {
		t.Fatalf("expected nil error for missing file, got: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if len(cfg.Services) != 0 {
		t.Fatalf("expected empty services, got %d", len(cfg.Services))
	}
	if len(cfg.Supervisors) != 0 {
		t.Fatalf("expected empty supervisors, got %d", len(cfg.Supervisors))
	}
}

// TestLoadInitConfig_ValidYAML: 有效 YAML 正确解析
func TestLoadInitConfig_ValidYAML(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/rnix-init.yaml"

	content := `services:
  - name: test-svc
    type: log_aggregator
    required: true
    config:
      key: value
supervisors:
  - name: test-sup
    strategy: one_for_one
    max_restarts: 5
    max_window: 1m0s
    required: false
    children:
      - name: child-1
        intent: test
        model: haiku
        context_budget: 1000
        restart: permanent
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadInitConfig(path)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if len(cfg.Services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(cfg.Services))
	}
	svc := cfg.Services[0]
	if svc.Name != "test-svc" || svc.Type != "log_aggregator" || !svc.Required {
		t.Fatalf("service parsed incorrectly: %+v", svc)
	}
	if len(cfg.Supervisors) != 1 {
		t.Fatalf("expected 1 supervisor, got %d", len(cfg.Supervisors))
	}
	sup := cfg.Supervisors[0]
	if sup.Name != "test-sup" || sup.Strategy != "one_for_one" || sup.MaxRestarts != 5 {
		t.Fatalf("supervisor parsed incorrectly: %+v", sup)
	}
	if sup.MaxWindow != time.Minute {
		t.Fatalf("expected MaxWindow 1m0s, got %v", sup.MaxWindow)
	}
	if len(sup.Children) != 1 || sup.Children[0].Name != "child-1" {
		t.Fatalf("children parsed incorrectly: %+v", sup.Children)
	}
}

// TestLoadInitConfig_InvalidYAML: 无效 YAML 返回 error
func TestLoadInitConfig_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/rnix-init.yaml"

	if err := os.WriteFile(path, []byte("{{invalid yaml content"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadInitConfig(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
	if !strings.Contains(err.Error(), "parse init config") {
		t.Fatalf("error should mention 'parse init config', got: %v", err)
	}
}

// === Regression Tests (P2) ===

// 10.5-REG-001: 编译时类型检查 — 确保公共 API 类型存在
func TestInit_TypesExist(t *testing.T) {
	// Verify InitConfig and related types compile correctly
	_ = InitConfig{}
	_ = ServiceConfig{}
	_ = SupervisorConfig{}
	_ = ChildConfig{}
	_ = InitResult{}
	_ = ServiceError{}

	// Verify ServiceInitializer interface exists
	var _ ServiceInitializer = &mockServiceInitializer{}

	// Verify AgentLoaderFunc type exists
	var _ AgentLoaderFunc = func(name string) (*agents.AgentInfo, error) {
		return nil, nil
	}

	// Verify Bootstrap function signature
	var _ = Bootstrap //nolint:staticcheck // compile-time type existence check

	// Verify DefaultInitConfig function signature
	var _ = DefaultInitConfig //nolint:staticcheck // compile-time type existence check
}
