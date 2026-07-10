package kernel

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/drivers/llm"
	"github.com/rnixai/rnix/internal/config"
	"github.com/rnixai/rnix/vfs"
)

// ============================================================================
// ATDD Tests for Story 66.4: fallback provider 解析支持 project 级 provider
//
// Root cause: primary resolution merges global + project providers.yaml
// (ipc/server_spawn.go resolveProjectContext → ProjectConfig.LLMFileOpener),
// but fallback resolution went through resolveLLMDevice(nil, fbProvider) →
// k.hasProvider (the daemon-global DriverRegistry closure), so a project-only
// provider always failed to resolve and FallbackDevice was left empty. When
// primary quota then ran out, attemptFallback's `FallbackDevice == ""` guard
// short-circuited and the whole process tree died with no fallback.
//
// Fix: resolveFallbackDevice honors the merged project name list (Task 1),
// same-provider fallback aligns its implicit-provider chain with primary
// (Task 2), and resolution failure is now surfaced on proc.FallbackResolveError
// + a delayed events.jsonl event (Task 3).
//
// RED validity note (see Dev Notes 测试标准): every helper below registers a
// global provider resolver that does NOT contain the project-only provider.
// Without SetProviderResolver, resolveLLMDevice admits ANY provider when
// hasProvider is nil (spawn.go back-compat) → pre-fix code would also pass and
// the RED would be void. To reproduce the RED, temporarily revert
// resolveFallbackDevice to a pure `k.resolveLLMDevice(nil, fbProvider)`
// delegate: the project-only / same-provider-default / visibility cases flip to
// FAIL.
// ============================================================================

// newProjectFallbackKernel builds a kernel whose GLOBAL resolver knows only
// globalNames, plus a project ProjectConfig whose merged providers view lists
// projNames and whose LLMFileOpener dispatches per provider name via openFor.
// dataDir, when non-empty, enables the events.jsonl auto-attach path.
func newProjectFallbackKernel(t testing.TB, globalNames []string, dataDir string,
	registerDevices []string, openFor func(provider string) *mockLLMFile) *KernelImpl {
	t.Helper()
	reg := vfs.NewDeviceRegistry()
	for _, name := range registerDevices {
		n := name
		_ = reg.Register("/dev/llm/"+n, func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
			if openFor != nil {
				if f := openFor(n); f != nil {
					return f, nil
				}
			}
			return &mockLLMFile{}, nil
		})
	}
	v := vfs.NewVFS(reg)
	k := NewKernel(v, rnixctx.NewManager(), nil)
	if dataDir != "" {
		k.dataDir = dataDir
	}
	t.Cleanup(k.Shutdown)

	globalSet := make(map[string]bool, len(globalNames))
	for _, n := range globalNames {
		globalSet[n] = true
	}
	k.SetProviderResolver(
		func() []string { return append([]string(nil), globalNames...) },
		func(name string) bool { return globalSet[name] },
	)
	return k
}

// projectConfigWithProviders builds a ProjectConfig carrying a merged providers
// view (names) + an LLMFileOpener that dispatches per provider via openFor.
func projectConfigWithProviders(defaultProvider string, names []string, openFor func(provider string) *mockLLMFile) *config.ProjectConfig {
	providers := make([]llm.ProviderConfig, 0, len(names))
	for _, n := range names {
		providers = append(providers, llm.ProviderConfig{Name: n, Driver: "openai-compat", BaseURL: "http://x"})
	}
	return &config.ProjectConfig{
		DefaultProvider: defaultProvider,
		Providers:       &llm.ProvidersConfig{Providers: providers},
		LLMFileOpener: func(provider string, _ int) (any, error) {
			if openFor != nil {
				if f := openFor(provider); f != nil {
					return f, nil
				}
			}
			return &mockLLMFile{}, nil
		},
	}
}

// ---------------------------------------------------------------------------
// AC1 / AC6: project-only provider resolves as a fallback device.
// Global resolver knows only "claude"; "projonly" exists solely in the merged
// project view. Pre-fix: FallbackDevice == "" (resolveLLMDevice rejects it).
// ---------------------------------------------------------------------------

func TestATDD_66_4_ProjectOnlyProviderFallback(t *testing.T) {
	k := newProjectFallbackKernel(t, []string{"claude"}, "", []string{"claude"}, nil)

	projConfig := projectConfigWithProviders("", []string{"claude", "projonly"}, nil)
	// agent: primary claude, explicit fallback provider "projonly" (project-only)
	agent := fallbackAgentInfo("claude", "sonnet", "fb-model", "projonly")

	pid, err := k.Spawn("project-only fallback resolve", agent, SpawnOpts{
		SkipReasonLoop: true,
		ProjectConfig:  projConfig,
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d missing after spawn", pid)
	}
	if proc.FallbackDevice != "/dev/llm/projonly" {
		t.Errorf("FallbackDevice = %q, want /dev/llm/projonly (project-only fallback resolved)", proc.FallbackDevice)
	}
	if proc.FallbackProvider != "projonly" {
		t.Errorf("FallbackProvider = %q, want projonly", proc.FallbackProvider)
	}
	if proc.FallbackResolveError != "" {
		t.Errorf("FallbackResolveError = %q, want empty (resolve succeeded)", proc.FallbackResolveError)
	}
}

// ---------------------------------------------------------------------------
// AC2: runtime takeover. primary write returns 429/quota; fallback (project-only
// provider) opens via LLMFileOpener and completes → process exits 0. End-to-end,
// not just the resolution layer. Pre-fix: FallbackDevice == "" so attemptFallback
// short-circuits and the process dies non-zero.
// ---------------------------------------------------------------------------

func TestATDD_66_4_RuntimeTakeoverProjectFallback(t *testing.T) {
	primaryFile := &mockLLMFile{
		writeErr: llm.NewLLMError("claude", 429, fmt.Errorf("quota exhausted")),
	}
	fallbackFile := &mockLLMFile{
		readData: makeLLMResponse("fallback via projonly", 12),
	}
	openFor := func(provider string) *mockLLMFile {
		if provider == "projonly" {
			return fallbackFile
		}
		return primaryFile
	}

	k := newProjectFallbackKernel(t, []string{"claude"}, "", []string{"claude"}, openFor)
	projConfig := projectConfigWithProviders("", []string{"claude", "projonly"}, openFor)
	agent := fallbackAgentInfo("claude", "sonnet", "fb-model", "projonly")

	pid, err := k.Spawn("runtime takeover via project fallback", agent, SpawnOpts{
		ProjectConfig: projConfig,
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d missing after spawn", pid)
	}
	if proc.FallbackDevice != "/dev/llm/projonly" {
		t.Fatalf("FallbackDevice = %q, want /dev/llm/projonly (needed for takeover)", proc.FallbackDevice)
	}

	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("expected exit 0 (project fallback took over), got %d: %s (err: %v)",
				exit.Code, exit.Reason, exit.Err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for process completion")
	}
	if proc.Result != "fallback via projonly" {
		t.Errorf("Result = %q, want 'fallback via projonly' (fallback device produced output)", proc.Result)
	}
}

// ---------------------------------------------------------------------------
// AC4: same-provider fallback (fallback set, fallback_provider empty) resolves
// the implicit provider using the primary chain. When primary is a project-level
// default_provider ("reclaude"), same-provider fallback must resolve to it too,
// not wrongly default to "claude". Pre-fix chain lacked the ProjectConfig
// DefaultProvider level → FallbackProvider == "claude" (unresolvable).
// ---------------------------------------------------------------------------

func TestATDD_66_4_SameProviderProjectDefault(t *testing.T) {
	k := newProjectFallbackKernel(t, []string{"reclaude"}, "", []string{"reclaude"}, nil)
	projConfig := projectConfigWithProviders("reclaude", []string{"reclaude"}, nil)

	// agent declares ONLY fallback (no provider, no fallback_provider)
	agent := fallbackAgentInfo("", "", "fb-model", "")

	pid, err := k.Spawn("same-provider project default fallback", agent, SpawnOpts{
		SkipReasonLoop: true,
		ProjectConfig:  projConfig,
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d missing after spawn", pid)
	}
	if proc.FallbackProvider != "reclaude" {
		t.Errorf("FallbackProvider = %q, want reclaude (project default_provider; pre-fix == claude)", proc.FallbackProvider)
	}
	if proc.FallbackDevice != "/dev/llm/reclaude" {
		t.Errorf("FallbackDevice = %q, want /dev/llm/reclaude", proc.FallbackDevice)
	}
	if proc.FallbackResolveError != "" {
		t.Errorf("FallbackResolveError = %q, want empty", proc.FallbackResolveError)
	}
}

// ---------------------------------------------------------------------------
// AC3: resolution failure is visible. A fallback provider that exists in NEITHER
// the global resolver NOR the project view leaves FallbackDevice empty, sets
// FallbackResolveError, and (on the !SkipReasonLoop path) emits a delayed
// events.jsonl ReasonStep{action:"fallback_resolve_failed"} AFTER EventWriter
// attach. spawn itself does NOT fail (non-blocking, fallback disabled).
// ---------------------------------------------------------------------------

func TestATDD_66_4_ResolveFailedVisibleInEvents(t *testing.T) {
	tmpDir := t.TempDir()
	primaryFile := &mockLLMFile{readData: makeLLMResponse("primary done", 5)}
	openFor := func(_ string) *mockLLMFile { return primaryFile }

	// Global resolver knows only "claude"; fallback provider "ghost" is unknown
	// everywhere. No ProjectConfig → resolveFallbackDevice delegates to the
	// global validator, which rejects "ghost".
	k := newProjectFallbackKernel(t, []string{"claude"}, tmpDir, []string{"claude"}, openFor)
	agent := fallbackAgentInfo("claude", "sonnet", "fb-model", "ghost")

	pid, err := k.Spawn("fallback resolve failure visibility", agent, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn should not fail (resolution failure is non-blocking): %v", err)
	}
	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d missing after spawn", pid)
	}
	// proc field set synchronously during Spawn (IPC backfill channel).
	if proc.FallbackResolveError == "" {
		t.Error("FallbackResolveError should be non-empty (ghost provider unresolvable)")
	}
	if proc.FallbackDevice != "" {
		t.Errorf("FallbackDevice = %q, want empty (resolve failed)", proc.FallbackDevice)
	}

	// Wait for the single reasonStep (primary completes) to terminate.
	select {
	case <-proc.terminated:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for pid=%d to terminate", proc.PID)
	}

	eventsPath := filepath.Join(k.ResolveStepBaseDir(proc), "steps", proc.UUID, "events.jsonl")
	rows, err := ReadAllEvents(eventsPath)
	if err != nil {
		t.Fatalf("events.jsonl unreadable: %v", err)
	}
	found := false
	for i := range rows {
		if rows[i].Syscall != "ReasonStep" {
			continue
		}
		action, _ := rows[i].Args["action"].(string)
		if action != "fallback_resolve_failed" {
			continue
		}
		found = true
		if fp, _ := rows[i].Args["fallback_provider"].(string); fp != "ghost" {
			t.Errorf("fallback_provider = %q, want ghost", fp)
		}
		if reason, _ := rows[i].Args["reason"].(string); reason == "" {
			t.Error("reason should be non-empty")
		}
		break
	}
	if !found {
		t.Error("events.jsonl should contain ReasonStep action=fallback_resolve_failed (delayed emit after EventWriter attach)")
	}
}

// ---------------------------------------------------------------------------
// AC6 (ordering guard): project name-list MISS still falls through to the global
// resolver. "deepseek" is registered globally but absent from the project view;
// resolution must succeed via the global path. This proves the project list only
// ADDS a path — it never narrows the global one. GREEN both pre- and post-fix;
// it guards against the helper accidentally shadowing the global fallthrough.
// ---------------------------------------------------------------------------

func TestATDD_66_4_ListMissGlobalFallthrough(t *testing.T) {
	// Global resolver knows claude + deepseek.
	k := newProjectFallbackKernel(t, []string{"claude", "deepseek"}, "", []string{"claude", "deepseek"}, nil)
	// Project view lists ONLY claude (deepseek missing) but has an opener.
	projConfig := projectConfigWithProviders("", []string{"claude"}, nil)
	agent := fallbackAgentInfo("claude", "sonnet", "fb-model", "deepseek")

	pid, err := k.Spawn("project list miss → global fallthrough", agent, SpawnOpts{
		SkipReasonLoop: true,
		ProjectConfig:  projConfig,
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d missing after spawn", pid)
	}
	if proc.FallbackDevice != "/dev/llm/deepseek" {
		t.Errorf("FallbackDevice = %q, want /dev/llm/deepseek (global fallthrough on project-list miss)", proc.FallbackDevice)
	}
	if proc.FallbackResolveError != "" {
		t.Errorf("FallbackResolveError = %q, want empty (global resolution succeeded)", proc.FallbackResolveError)
	}
}
