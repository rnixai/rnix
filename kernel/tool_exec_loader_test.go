package kernel

import (
	"fmt"
	"testing"

	"github.com/rnixai/rnix/agents"
	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/config"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/skills"
	"github.com/rnixai/rnix/vfs"
)

// Regression for the "subagent uses wrong provider/model" bug. Before the fix
// kernel/tool_exec.go ActionSpawn called k.agentLoader directly — bypassing
// any .rnix/agents/ override carried on proc.ProjectConfig. Symptom: stem
// subagent spawned inside a project still resolved provider=claude,
// model=sonnet from ~/.config/rnix/agents/stem/agent.yaml even after the
// project pinned `models: {}`. The helpers resolveSpawnAgentLoader /
// resolveSpecializeSkillLoader are the choke point — these tests lock the
// project-first contract.

func newKernelForLoaderTest(t *testing.T) *KernelImpl {
	t.Helper()
	reg := vfs.NewDeviceRegistry()
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)
	return k
}

func TestResolveSpawnAgentLoader_ProjectLoaderWins(t *testing.T) {
	k := newKernelForLoaderTest(t)

	var globalCalls, projectCalls int
	k.SetAgentLoader(func(name string) (*agents.AgentInfo, error) {
		globalCalls++
		return &agents.AgentInfo{Manifest: agents.AgentManifest{Name: "global-stem"}}, nil
	})

	proc := NewProcess(1, "intent", nil)
	proc.ProjectConfig = &config.ProjectConfig{
		AgentLoader: func(name string) (any, error) {
			projectCalls++
			return &agents.AgentInfo{Manifest: agents.AgentManifest{Name: "project-stem"}}, nil
		},
	}

	loader, ok := k.resolveSpawnAgentLoader(proc)
	if !ok {
		t.Fatal("resolveSpawnAgentLoader returned hasLoader=false; expected project loader to qualify")
	}
	ai, err := loader("stem")
	if err != nil {
		t.Fatalf("loader returned error: %v", err)
	}
	if ai.Manifest.Name != "project-stem" {
		t.Errorf("loaded agent name = %q, want %q (project loader should win over global)", ai.Manifest.Name, "project-stem")
	}
	if projectCalls != 1 {
		t.Errorf("project loader called %d times, want 1", projectCalls)
	}
	if globalCalls != 0 {
		t.Errorf("global loader called %d times, want 0 (project loader present must shadow global)", globalCalls)
	}
}

func TestResolveSpawnAgentLoader_FallbackToGlobalWhenNoProject(t *testing.T) {
	k := newKernelForLoaderTest(t)

	var globalCalls int
	k.SetAgentLoader(func(name string) (*agents.AgentInfo, error) {
		globalCalls++
		return &agents.AgentInfo{Manifest: agents.AgentManifest{Name: "global-stem"}}, nil
	})

	proc := NewProcess(1, "intent", nil) // no ProjectConfig

	loader, ok := k.resolveSpawnAgentLoader(proc)
	if !ok {
		t.Fatal("expected global loader to be used when ProjectConfig is nil")
	}
	ai, err := loader("stem")
	if err != nil {
		t.Fatalf("loader returned error: %v", err)
	}
	if ai.Manifest.Name != "global-stem" {
		t.Errorf("loaded agent name = %q, want %q", ai.Manifest.Name, "global-stem")
	}
	if globalCalls != 1 {
		t.Errorf("global loader called %d times, want 1", globalCalls)
	}
}

func TestResolveSpawnAgentLoader_FallbackWhenProjectConfigHasNilLoader(t *testing.T) {
	k := newKernelForLoaderTest(t)
	k.SetAgentLoader(func(name string) (*agents.AgentInfo, error) {
		return &agents.AgentInfo{Manifest: agents.AgentManifest{Name: "global-stem"}}, nil
	})

	proc := NewProcess(1, "intent", nil)
	proc.ProjectConfig = &config.ProjectConfig{} // explicit empty config — AgentLoader is nil

	loader, ok := k.resolveSpawnAgentLoader(proc)
	if !ok {
		t.Fatal("expected global loader fallback when ProjectConfig.AgentLoader is nil")
	}
	ai, _ := loader("stem")
	if ai.Manifest.Name != "global-stem" {
		t.Errorf("agent name = %q, want %q (must fall through to global)", ai.Manifest.Name, "global-stem")
	}
}

func TestResolveSpawnAgentLoader_NoLoadersAvailable(t *testing.T) {
	k := newKernelForLoaderTest(t)
	// Deliberately do NOT call SetAgentLoader and leave ProjectConfig nil.
	proc := NewProcess(1, "intent", nil)

	if _, ok := k.resolveSpawnAgentLoader(proc); ok {
		t.Fatal("expected hasLoader=false when neither project nor global loader configured")
	}
}

func TestResolveSpawnAgentLoader_ProjectLoaderReturnsWrongType_FallsBackToGlobal(t *testing.T) {
	k := newKernelForLoaderTest(t)

	var globalCalls int
	k.SetAgentLoader(func(name string) (*agents.AgentInfo, error) {
		globalCalls++
		return &agents.AgentInfo{Manifest: agents.AgentManifest{Name: "global-stem"}}, nil
	})

	proc := NewProcess(1, "intent", nil)
	proc.ProjectConfig = &config.ProjectConfig{
		AgentLoader: func(name string) (any, error) {
			return "not-an-agent", nil // type assertion will fail
		},
	}

	loader, ok := k.resolveSpawnAgentLoader(proc)
	if !ok {
		t.Fatal("expected hasLoader=true")
	}
	ai, err := loader("stem")
	if err != nil {
		t.Fatalf("expected fallback to global loader instead of error: %v", err)
	}
	if ai.Manifest.Name != "global-stem" {
		t.Errorf("name = %q, want %q (fallback should reach global)", ai.Manifest.Name, "global-stem")
	}
	if globalCalls != 1 {
		t.Errorf("global loader call count = %d, want 1", globalCalls)
	}
}

// Regression for the typed-nil footgun: a project loader that returns
// `return (*agents.AgentInfo)(nil), nil` passes the type assertion with
// ai == nil. Without an explicit nil guard the wrapper would pass agentInfo=nil
// to k.Spawn, silently dropping the agent the LLM asked for.
func TestResolveSpawnAgentLoader_ProjectLoaderReturnsTypedNil_FallsBackToGlobal(t *testing.T) {
	k := newKernelForLoaderTest(t)

	var globalCalls int
	k.SetAgentLoader(func(name string) (*agents.AgentInfo, error) {
		globalCalls++
		return &agents.AgentInfo{Manifest: agents.AgentManifest{Name: "global-stem"}}, nil
	})

	proc := NewProcess(1, "intent", nil)
	proc.ProjectConfig = &config.ProjectConfig{
		AgentLoader: func(_ string) (any, error) {
			// Typed nil: assertion to *agents.AgentInfo succeeds with ai==nil.
			var typedNil *agents.AgentInfo
			return typedNil, nil
		},
	}

	loader, _ := k.resolveSpawnAgentLoader(proc)
	ai, err := loader("stem")
	if err != nil {
		t.Fatalf("expected fallback to global, got error: %v", err)
	}
	if ai == nil {
		t.Fatal("expected non-nil fallback agent info; got nil (typed-nil leaked through)")
	}
	if ai.Manifest.Name != "global-stem" {
		t.Errorf("name = %q, want %q", ai.Manifest.Name, "global-stem")
	}
	if globalCalls != 1 {
		t.Errorf("global loader call count = %d, want 1", globalCalls)
	}
}

func TestResolveSpawnAgentLoader_ProjectLoaderError_Propagates(t *testing.T) {
	k := newKernelForLoaderTest(t)
	k.SetAgentLoader(func(name string) (*agents.AgentInfo, error) {
		t.Fatal("global loader must not be called when project loader fails with a real error")
		return nil, nil
	})

	proc := NewProcess(1, "intent", nil)
	wantErr := fmt.Errorf("not found in project agents dir")
	proc.ProjectConfig = &config.ProjectConfig{
		AgentLoader: func(name string) (any, error) { return nil, wantErr },
	}

	loader, _ := k.resolveSpawnAgentLoader(proc)
	if _, err := loader("stem"); err == nil {
		t.Fatal("expected project loader error to propagate, got nil")
	}
}

func TestResolveSpecializeSkillLoader_ProjectLoaderWins(t *testing.T) {
	k := newKernelForLoaderTest(t)

	var globalCalls, projectCalls int
	k.SetSkillLoader(func(name string) (*skills.SkillInfo, error) {
		globalCalls++
		return &skills.SkillInfo{Manifest: skills.SkillManifest{Name: "global-skill"}}, nil
	})

	proc := NewProcess(1, "intent", nil)
	proc.ProjectConfig = &config.ProjectConfig{
		SkillLoader: func(name string) (any, error) {
			projectCalls++
			return &skills.SkillInfo{Manifest: skills.SkillManifest{Name: "project-skill"}}, nil
		},
	}

	loader, ok := k.resolveSpecializeSkillLoader(proc)
	if !ok {
		t.Fatal("hasLoader=false; expected project loader to qualify")
	}
	si, err := loader("foo")
	if err != nil {
		t.Fatalf("loader: %v", err)
	}
	if si.Manifest.Name != "project-skill" {
		t.Errorf("skill name = %q, want %q", si.Manifest.Name, "project-skill")
	}
	if projectCalls != 1 || globalCalls != 0 {
		t.Errorf("call counts: project=%d global=%d; want project=1 global=0", projectCalls, globalCalls)
	}
}

func TestResolveSpecializeSkillLoader_NoLoadersAvailable(t *testing.T) {
	k := newKernelForLoaderTest(t)
	proc := NewProcess(1, "intent", nil)
	if _, ok := k.resolveSpecializeSkillLoader(proc); ok {
		t.Fatal("expected hasLoader=false when no loaders configured")
	}
}

// Compile-time assertion that types.AgentLoaderFunc / SkillLoaderFunc accept
// the natural wrap functions produced in ipc.resolveProjectContext, so a
// future change to either signature blows up here instead of silently at runtime.
var _ types.AgentLoaderFunc = func(_ string) (any, error) { return (*agents.AgentInfo)(nil), nil }
var _ types.SkillLoaderFunc = func(_ string) (any, error) { return (*skills.SkillInfo)(nil), nil }
