package kernel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rnixai/rnix/agents"
	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/config"
	"github.com/rnixai/rnix/vfs"
)

// projectDocHeader is the markdown header wrapping injected AGENTS.md content.
const projectDocHeader = "# Project Instructions (AGENTS.md)"

// setupProjectDocProc returns a kernel + process wired with a project config
// rooted at a fresh temp dir, ready for project_doc section assertions.
func setupProjectDocProc(t *testing.T) (*KernelImpl, *Process, string) {
	t.Helper()
	k, _, proc := setupBackpressureKernel(t, 256)
	root := t.TempDir()
	proc.ProjectConfig = &config.ProjectConfig{ProjectDir: root}
	return k, proc, root
}

// AC1: AGENTS.md at the project root is injected as a project_doc section,
// visible in the assembled system prompt.
func TestATDD_35_7_AC1_InjectsRootAgentsMD(t *testing.T) {
	k, proc, root := setupProjectDocProc(t)
	body := "# Build\n\nRun `make all` before committing."
	if err := os.WriteFile(filepath.Join(root, config.AgentsMDFilename), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	result := registerSections(proc, k, "").Build()

	if !strings.Contains(result, projectDocHeader) {
		t.Errorf("expected project_doc header in system prompt, missing")
	}
	if !strings.Contains(result, "Run `make all` before committing.") {
		t.Errorf("expected AGENTS.md body injected, missing")
	}
}

// AC1: no AGENTS.md → empty section; Build succeeds without it.
func TestATDD_35_7_AC1_NoFileEmptySection(t *testing.T) {
	k, proc, _ := setupProjectDocProc(t) // root has no AGENTS.md

	result := registerSections(proc, k, "").Build()

	if strings.Contains(result, projectDocHeader) {
		t.Errorf("project_doc header must be absent when no AGENTS.md exists")
	}
}

// AC3: only AGENTS.md is recognized — a project-root CLAUDE.md is NOT injected.
func TestATDD_35_7_AC3_ExclusiveAgentsMD(t *testing.T) {
	k, proc, root := setupProjectDocProc(t)
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("Claude Code dev guide"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := registerSections(proc, k, "").Build()

	if strings.Contains(result, projectDocHeader) {
		t.Errorf("CLAUDE.md must not trigger project_doc injection")
	}
	if strings.Contains(result, "Claude Code dev guide") {
		t.Errorf("CLAUDE.md content must never leak into the system prompt")
	}
}

// AC6: per-agent disable switch → empty section even when AGENTS.md exists.
func TestATDD_35_7_AC6_DisableSwitch(t *testing.T) {
	k, proc, root := setupProjectDocProc(t)
	proc.ProjectDocInjection = false
	if err := os.WriteFile(filepath.Join(root, config.AgentsMDFilename), []byte("should be ignored"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := registerSections(proc, k, "").Build()

	if strings.Contains(result, projectDocHeader) {
		t.Errorf("disabled project_doc must produce an empty section")
	}
	if strings.Contains(result, "should be ignored") {
		t.Errorf("disabled project_doc must not inject AGENTS.md content")
	}
}

// AC5: oversized AGENTS.md is truncated with a marker (section-level wiring).
func TestATDD_35_7_AC5_Truncation(t *testing.T) {
	k, proc, root := setupProjectDocProc(t)
	big := strings.Repeat("x", config.MaxAgentsMDBytes+4096)
	if err := os.WriteFile(filepath.Join(root, config.AgentsMDFilename), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}

	result := registerSections(proc, k, "").Build()

	if !strings.Contains(result, projectDocHeader) {
		t.Fatalf("expected project_doc header for oversized file")
	}
	if !strings.Contains(result, "[truncated: AGENTS.md exceeded") {
		t.Errorf("expected truncation marker in injected content")
	}
}

// AC5: nil ProjectConfig (no project context) → empty section, no panic.
func TestATDD_35_7_AC5_NilProjectConfig(t *testing.T) {
	k, _, proc := setupBackpressureKernel(t, 256)
	proc.ProjectConfig = nil // no project context

	result := registerSections(proc, k, "").Build()

	if strings.Contains(result, projectDocHeader) {
		t.Errorf("nil ProjectConfig must produce an empty project_doc section")
	}
}

// AC4 ([Review][Patch] code review 2026-06-19): the eager-closure freeze must
// survive Invalidate(). A mid-run edit (or deletion) of the on-disk AGENTS.md
// must NOT change this process's snapshot — that is what protects the LLM
// prompt-cache hit rate (Story 35.2 lesson) and is the sole behavioral
// difference between the eager-closure capture and a lazy ComputeFn (which would
// re-read disk on Invalidate). Combination Matrix marks "project_doc × Invalidate"
// as 需验证; this closes that gap.
func TestATDD_35_7_AC4_FreezeSurvivesInvalidate(t *testing.T) {
	k, proc, root := setupProjectDocProc(t)
	agentsPath := filepath.Join(root, config.AgentsMDFilename)
	if err := os.WriteFile(agentsPath, []byte("ORIGINAL DOC"), 0o644); err != nil {
		t.Fatal(err)
	}

	reg := registerSections(proc, k, "")
	if first := reg.Build(); !strings.Contains(first, "ORIGINAL DOC") {
		t.Fatalf("expected ORIGINAL DOC in first build, missing")
	}

	// Mutate the on-disk file, then force a cached-section recompute (specialize
	// rebuilds the prompt via Invalidate). The frozen snapshot must be unaffected.
	if err := os.WriteFile(agentsPath, []byte("MUTATED DOC"), 0o644); err != nil {
		t.Fatal(err)
	}
	reg.Invalidate()

	second := reg.Build()
	if !strings.Contains(second, "ORIGINAL DOC") {
		t.Errorf("frozen snapshot must survive Invalidate: expected ORIGINAL DOC after recompute")
	}
	if strings.Contains(second, "MUTATED DOC") {
		t.Errorf("mid-run edit leaked into snapshot after Invalidate — eager freeze broken (breaks prompt cache)")
	}
}

// spawnWithProjectDocManifest spawns a process backed by a mock LLM carrying the
// given manifest ProjectDoc pointer, then returns the resulting process. Mirrors
// spawnWithAgentEffort — the agent has no provider, so the device resolves to the
// registered /dev/llm/claude default. No ProjectConfig is set: this isolates the
// AC6 manifest→proc transduction (the bool field) from actual injection (AC1).
func spawnWithProjectDocManifest(t *testing.T, projectDoc *bool) (*KernelImpl, *Process) {
	t.Helper()

	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockLLMFile{readData: makeLLMResponse("done", 10)}, nil
	})
	v := vfs.NewVFS(reg)
	k := NewKernel(v, rnixctx.NewManager(), nil)
	k.dataDir = t.TempDir()
	t.Cleanup(k.Shutdown)

	agent := &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Name:       "project-doc-agent",
			ProjectDoc: projectDoc,
		},
	}

	pid, err := k.Spawn("project doc manifest wiring", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d not found after spawn", pid)
	}
	waitEarlyEWDone(t, proc)
	return k, proc
}

// AC6 ([Review][Patch] code review 2026-06-19): the user-facing disable switch is
// wired through the agent manifest, not by setting proc.ProjectDocInjection
// directly. These three cases exercise the spawn.go transduction
// (`agent.Manifest.ProjectDoc != nil && !*ProjectDoc`) end-to-end, so a wiring
// regression cannot silently disable the feature.
func TestATDD_35_7_AC6_ManifestDisableWiring(t *testing.T) {
	disabled := false
	_, proc := spawnWithProjectDocManifest(t, &disabled)
	if proc.ProjectDocInjection {
		t.Errorf("manifest project_doc:false must set proc.ProjectDocInjection=false, got true")
	}
}

func TestATDD_35_7_AC6_ManifestNilDefaultsEnabled(t *testing.T) {
	_, proc := spawnWithProjectDocManifest(t, nil)
	if !proc.ProjectDocInjection {
		t.Errorf("nil manifest project_doc must default proc.ProjectDocInjection=true, got false")
	}
}

func TestATDD_35_7_AC6_ManifestExplicitTrue(t *testing.T) {
	enabled := true
	_, proc := spawnWithProjectDocManifest(t, &enabled)
	if !proc.ProjectDocInjection {
		t.Errorf("manifest project_doc:true must keep proc.ProjectDocInjection=true, got false")
	}
}
