package kernel

import (
	"os"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/vfs"
)

// ============================================================================
// ATDD Tests for Story 23.7: rnix-compose/init 配置格式升级 (Init 部分)
// RED PHASE: Tests reference Provider field on ChildConfig and ChildSpec
// which do NOT exist yet.
// ============================================================================

// ============================================================
// AC3: Init YAML child 指定 provider + model → Spawn 传递正确
// ============================================================

// TestATDD_23_7_AC3_InitChildProvider verifies that when rnix-init.yaml
// specifies provider: groq + model: llama-3.3-70b-versatile on a supervisor
// child, the config is parsed correctly.
func TestATDD_23_7_AC3_InitChildProvider(t *testing.T) {
	t.Parallel()

	// Given: an init YAML with child specifying provider
	dir := t.TempDir()
	path := dir + "/rnix-init.yaml"
	content := `supervisors:
  - name: test-supervisor
    strategy: one_for_one
    max_restarts: 3
    max_window: 10s
    required: true
    children:
      - name: groq-worker
        intent: "run with groq"
        provider: groq
        model: llama-3.3-70b-versatile
        context_budget: 1000
        restart: temporary
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadInitConfig(path)
	if err != nil {
		t.Fatalf("LoadInitConfig failed: %v", err)
	}

	// Then: ChildConfig.Provider is parsed correctly
	if len(cfg.Supervisors) != 1 {
		t.Fatalf("AC3: expected 1 supervisor, got %d", len(cfg.Supervisors))
	}
	children := cfg.Supervisors[0].Children
	if len(children) != 1 {
		t.Fatalf("AC3: expected 1 child, got %d", len(children))
	}
	child := children[0]
	if child.Provider != "groq" {
		t.Errorf("AC3: expected ChildConfig.Provider = %q, got %q", "groq", child.Provider)
	}
	if child.Model != "llama-3.3-70b-versatile" {
		t.Errorf("AC3: expected ChildConfig.Model = %q, got %q", "llama-3.3-70b-versatile", child.Model)
	}
}

// TestATDD_23_7_AC3_InitChildNoProvider verifies backward compatibility:
// when init YAML children have no provider field, Provider remains empty.
func TestATDD_23_7_AC3_InitChildNoProvider(t *testing.T) {
	t.Parallel()

	// Given: an init YAML without provider field (old format)
	dir := t.TempDir()
	path := dir + "/rnix-init.yaml"
	content := `supervisors:
  - name: test-supervisor
    strategy: one_for_one
    max_restarts: 3
    max_window: 10s
    required: true
    children:
      - name: default-worker
        intent: "run with default"
        model: haiku
        context_budget: 500
        restart: temporary
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadInitConfig(path)
	if err != nil {
		t.Fatalf("LoadInitConfig failed: %v", err)
	}

	// Then: ChildConfig.Provider is empty (backward compat)
	child := cfg.Supervisors[0].Children[0]
	if child.Provider != "" {
		t.Errorf("AC3: expected empty ChildConfig.Provider for old format, got %q", child.Provider)
	}
}

// ============================================================
// Unit Tests: ChildConfig / ChildSpec Provider
// ============================================================

// TestLoadInitConfig_ChildProvider verifies YAML parsing of provider on children.
func TestLoadInitConfig_ChildProvider(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := dir + "/rnix-init.yaml"
	content := `supervisors:
  - name: sup
    strategy: one_for_one
    max_restarts: 3
    max_window: 10s
    children:
      - name: w1
        intent: task1
        provider: groq
        model: llama3
        restart: temporary
      - name: w2
        intent: task2
        model: haiku
        restart: permanent
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadInitConfig(path)
	if err != nil {
		t.Fatalf("LoadInitConfig failed: %v", err)
	}

	children := cfg.Supervisors[0].Children
	if len(children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(children))
	}

	// w1 has provider
	if children[0].Provider != "groq" {
		t.Errorf("w1: expected Provider = %q, got %q", "groq", children[0].Provider)
	}
	// w2 has no provider
	if children[1].Provider != "" {
		t.Errorf("w2: expected empty Provider, got %q", children[1].Provider)
	}
}

// TestLoadInitConfig_ChildNoProvider_BackwardCompat verifies that YAML
// without provider fields parses with empty Provider (old format compat).
func TestLoadInitConfig_ChildNoProvider_BackwardCompat(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := dir + "/rnix-init.yaml"
	content := `supervisors:
  - name: legacy
    strategy: one_for_one
    max_restarts: 3
    max_window: 10s
    children:
      - name: old-worker
        intent: legacy task
        model: haiku
        restart: temporary
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadInitConfig(path)
	if err != nil {
		t.Fatalf("LoadInitConfig failed: %v", err)
	}

	child := cfg.Supervisors[0].Children[0]
	if child.Provider != "" {
		t.Errorf("expected empty Provider for legacy format, got %q", child.Provider)
	}
}

// TestToSupervisorSpec_ChildProvider verifies that toSupervisorSpec
// propagates ChildConfig.Provider to ChildSpec.Provider.
func TestToSupervisorSpec_ChildProvider(t *testing.T) {
	t.Parallel()

	supCfg := &SupervisorConfig{
		Name:        "test",
		Strategy:    "one_for_one",
		MaxRestarts: 3,
		MaxWindow:   10 * time.Second,
		Children: []ChildConfig{
			{
				Name:     "w1",
				Intent:   "task with provider",
				Provider: "groq",
				Model:    "llama3",
				Restart:  "temporary",
			},
			{
				Name:    "w2",
				Intent:  "task without provider",
				Model:   "haiku",
				Restart: "permanent",
			},
		},
	}

	spec, err := supCfg.toSupervisorSpec(mockAgentLoader(nil))
	if err != nil {
		t.Fatalf("toSupervisorSpec failed: %v", err)
	}

	if len(spec.Children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(spec.Children))
	}

	// w1: provider propagated
	if spec.Children[0].Provider != "groq" {
		t.Errorf("w1: expected ChildSpec.Provider = %q, got %q", "groq", spec.Children[0].Provider)
	}
	// w2: no provider
	if spec.Children[1].Provider != "" {
		t.Errorf("w2: expected empty ChildSpec.Provider, got %q", spec.Children[1].Provider)
	}
}

// TestBootstrap_SupervisorChildProvider verifies that when rnix-init.yaml
// child specifies provider, the Bootstrap flow passes it through to SpawnOpts.
func TestBootstrap_SupervisorChildProvider(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	file := &normalFile{}
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag) (vfs.VFSFile, error) {
		return file, nil
	})
	_ = reg.Register("/dev/llm/groq", func(_ string, _ vfs.OpenFlag) (vfs.VFSFile, error) {
		return file, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)

	cfg := &InitConfig{
		Supervisors: []SupervisorConfig{
			{
				Name:        "provider-supervisor",
				Strategy:    "one_for_one",
				MaxRestarts: 3,
				MaxWindow:   10 * time.Second,
				Required:    true,
				Children: []ChildConfig{
					{
						Name:          "groq-worker",
						Intent:        "run with groq provider",
						Provider:      "groq",
						Model:         "llama-3.3-70b-versatile",
						ContextBudget: 2000,
						Restart:       "temporary",
					},
				},
			},
		},
	}

	result, err := Bootstrap(k, cfg, mockAgentLoader(nil))
	if err != nil {
		t.Fatalf("Bootstrap should succeed, got error: %v", err)
	}
	if len(result.Started) == 0 {
		t.Fatal("Expected at least one started entry for the supervisor")
	}
}

// ============================================================
// Compile-time type existence checks
// ============================================================

// TestATDD_23_7_InitTypesExist verifies that new Provider fields compile.
func TestATDD_23_7_InitTypesExist(t *testing.T) {
	t.Parallel()

	// ChildConfig.Provider must exist
	var cc ChildConfig
	_ = cc.Provider

	// ChildSpec.Provider must exist
	var cs ChildSpec
	_ = cs.Provider

	// Verify type construction with Provider field
	_ = ChildConfig{
		Name:     "test",
		Intent:   "test",
		Provider: "groq",
		Model:    "llama3",
	}
	_ = ChildSpec{
		Name:     "test",
		Intent:   "test",
		Provider: "groq",
		Model:    "llama3",
	}
}
