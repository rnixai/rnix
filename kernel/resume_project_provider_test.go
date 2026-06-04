package kernel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/config"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// Resume 路径项目级 provider 校验对称化
// (spec-resume-project-provider-fix.md, 目标 1).
//
// Spawn (spawn.go:588) skips the global DriverRegistry validation when the
// ProjectConfig carries an LLMFileOpener, so a provider defined only in
// .rnix/providers.yaml (e.g. opencodego) can spawn. The two resume paths used
// to call resolveLLMDevice(nil, provider) unconditionally and reject such a
// provider before ever reaching openLLMDeviceForResume. These tests pin the
// symmetric behaviour across all three ProjectConfig sources (opts > oldProc >
// projectConfigLoader) on both the history and checkpoint paths, and guard the
// global / unknown-provider fail-fast against regression.
// =============================================================================

// projectOnlyOpener returns a fresh mock LLM file so openLLMDeviceForResume can
// RegisterFD it; the resumed reasonStep loop then reads a "complete" response.
func projectOnlyOpener() func(string, int) (any, error) {
	return func(_ string, _ int) (any, error) {
		return &mockLLMFile{readData: makeCompleteResponse("resumed via project provider", 10)}, nil
	}
}

// setupProjectProviderKernel builds a kernel whose GLOBAL registry/validator
// knows only "claude" and REJECTS the project-only provider, mirroring a daemon
// whose ~/.config/rnix/providers.yaml lacks opencodego.
func setupProjectProviderKernel(t *testing.T) (*KernelImpl, string) {
	t.Helper()
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockLLMFile{readData: makeCompleteResponse("global fallback", 10)}, nil
	})
	v := vfs.NewVFS(reg)
	k := NewKernel(v, rnixctx.NewManager(), nil)
	t.Cleanup(k.Shutdown)
	_, projBaseDir := TestSetupDataDir(t, k)
	k.SetProviderResolver(
		func() []string { return []string{"claude"} },
		func(name string) bool { return name == "claude" },
	)
	return k, projBaseDir
}

func projectProviderConfig() *config.ProjectConfig {
	return &config.ProjectConfig{
		ProjectDir:      testProjectDir,
		DefaultProvider: "opencodego",
		LLMFileOpener:   projectOnlyOpener(),
	}
}

// writeProjHistoryFixture writes a Dead history snapshot whose provider is the
// project-only one and stamps project_dir, plus the steps.jsonl + meta needed
// by the history rehydrate path.
func writeProjHistoryFixture(t *testing.T, baseDir, uuid, provider, projectDir string) {
	t.Helper()
	writeTestStepsAndMeta(t, baseDir, uuid, 3, "")
	overwriteProcInfoFields(t, baseDir, uuid, map[string]any{
		"provider":    provider,
		"project_dir": projectDir,
	})
}

func assertResumedProvider(t *testing.T, k *KernelImpl, pid types.PID, wantProvider string, wantOpener bool) {
	t.Helper()
	proc, ok := k.GetProcess(pid)
	if !ok {
		// Process may have already completed+reaped via the mock "complete"
		// response; the no-error return from Resume is the primary assertion.
		return
	}
	if proc.Provider != wantProvider {
		t.Errorf("proc.Provider = %q, want %q", proc.Provider, wantProvider)
	}
	if wantOpener {
		if proc.ProjectConfig == nil {
			t.Error("proc.ProjectConfig = nil, want project config carried into resume")
		} else if proc.ProjectConfig.LLMFileOpener == nil {
			t.Error("proc.ProjectConfig.LLMFileOpener = nil, want project opener carried into resume")
		}
	}
}

// --- history + opts.ProjectConfig (apply 重连场景) ---
func TestResume_ProjectProvider_History_OptsProjectConfig(t *testing.T) {
	k, baseDir := setupProjectProviderKernel(t)
	uuid := "projprov-hist-opts-000000000001"
	writeProjHistoryFixture(t, baseDir, uuid, "opencodego", testProjectDir)

	result, err := k.ResumeWithOpts(uuid, ResumeOpts{ProjectConfig: projectProviderConfig()})
	if err != nil {
		t.Fatalf("resume project-only provider via opts.ProjectConfig should succeed, got: %v", err)
	}
	if result.PID == 0 {
		t.Fatal("expected non-zero PID")
	}
	assertResumedProvider(t, k, result.PID, "opencodego", true)
	cleanupResumedProc(t, k, result.PID)
}

// --- history + empty opts + oldProc placeholder (auto-resume 复用 oldProc) ---
func TestResume_ProjectProvider_History_ReuseOldProcConfig(t *testing.T) {
	k, baseDir := setupProjectProviderKernel(t)
	uuid := "projprov-hist-oldproc-00000001"
	writeProjHistoryFixture(t, baseDir, uuid, "opencodego", testProjectDir)

	// Suspended placeholder carrying the rebuilt ProjectConfig, exactly as
	// LoadSuspendedFromDisk produces it before AutoResumeDaemonShutdown.
	placeholder := NewProcess(0, "placeholder", nil)
	placeholder.UUID = uuid
	placeholder.Provider = "opencodego"
	placeholder.ProjectConfig = projectProviderConfig()
	_ = placeholder.Start()
	_ = placeholder.Suspend()
	k.AddProcess(placeholder)

	// Empty ResumeOpts → must fall back to oldProc.ProjectConfig, NOT fail the
	// global validation (this is the AutoResumeDaemonShutdown path).
	result, err := k.ResumeWithOpts(uuid, ResumeOpts{})
	if err != nil {
		t.Fatalf("auto-resume (empty opts) of project-only provider should reuse placeholder config, got: %v", err)
	}
	assertResumedProvider(t, k, result.PID, "opencodego", true)
	cleanupResumedProc(t, k, result.PID)
}

// --- history + empty opts + projectConfigLoader fallback (no oldProc) ---
func TestResume_ProjectProvider_History_ProjectConfigLoaderFallback(t *testing.T) {
	k, baseDir := setupProjectProviderKernel(t)
	uuid := "projprov-hist-loader-00000001"
	writeProjHistoryFixture(t, baseDir, uuid, "opencodego", testProjectDir)

	loaderCalls := 0
	k.SetProjectConfigLoader(func(pd string) (*config.ProjectConfig, error) {
		loaderCalls++
		if pd != testProjectDir {
			t.Errorf("loader called with %q, want %q", pd, testProjectDir)
		}
		return projectProviderConfig(), nil
	})

	// No procTable entry, empty opts → only diskInfo.project_dir +
	// projectConfigLoader can supply the opener.
	result, err := k.ResumeWithOpts(uuid, ResumeOpts{})
	if err != nil {
		t.Fatalf("resume should rebuild ProjectConfig via projectConfigLoader, got: %v", err)
	}
	if loaderCalls != 1 {
		t.Errorf("projectConfigLoader called %d times, want 1", loaderCalls)
	}
	assertResumedProvider(t, k, result.PID, "opencodego", true)
	cleanupResumedProc(t, k, result.PID)
}

// --- checkpoint + opts.ProjectConfig ---
func TestResume_ProjectProvider_Checkpoint_OptsProjectConfig(t *testing.T) {
	k, baseDir := setupProjectProviderKernel(t)
	uuid := "projprov-ckpt-opts-0000000001"

	stepsDir := filepath.Join(baseDir, "steps", uuid)
	if err := os.MkdirAll(stepsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	ctxSnap := json.RawMessage(`{"system_prompt":"proj agent","messages":[{"role":"user","content":"hi"}],"max_size":64}`)
	cp := &CheckpointData{
		Version:         CheckpointVersion,
		UUID:            uuid,
		LastStep:        3,
		Timestamp:       time.Now(),
		ContextSnapshot: ctxSnap,
		ProcState: CheckpointProcState{
			Provider: "opencodego",
			Model:    "deepseek-v4-flash",
			Intent:   "checkpoint project provider",
		},
	}
	if err := writeCheckpoint(stepsDir, cp); err != nil {
		t.Fatalf("writeCheckpoint: %v", err)
	}

	result, err := k.ResumeWithOpts(uuid, ResumeOpts{ProjectConfig: projectProviderConfig()})
	if err != nil {
		t.Fatalf("checkpoint resume of project-only provider should succeed, got: %v", err)
	}
	assertResumedProvider(t, k, result.PID, "opencodego", true)
	cleanupResumedProc(t, k, result.PID)
}

// --- regression: unknown provider + no project opener still fails fast ---
func TestResume_UnknownProvider_History_FailFast(t *testing.T) {
	k, baseDir := setupProjectProviderKernel(t)
	uuid := "unknownprov-hist-0000000001"
	writeProjHistoryFixture(t, baseDir, uuid, "ghostprovider", "")

	// No opts.ProjectConfig, no projectConfigLoader, empty project_dir → the
	// global validation must still reject the unknown provider (no regression).
	_, err := k.ResumeWithOpts(uuid, ResumeOpts{})
	if err == nil {
		t.Fatal("expected ErrDriver for unknown provider without project opener")
	}
	se, ok := err.(*SyscallError)
	if !ok {
		t.Fatalf("expected *SyscallError, got %T", err)
	}
	if se.Code != types.ErrDriver {
		t.Errorf("error code = %q, want %q", se.Code, types.ErrDriver)
	}
}
