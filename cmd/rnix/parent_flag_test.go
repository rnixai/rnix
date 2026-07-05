package main

import (
	"context"
	"strconv"
	"strings"
	"testing"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/kernel"
	"github.com/rnixai/rnix/vfs"
)

type parentFlagLLMFile struct{}

func (f *parentFlagLLMFile) Write(context.Context, []byte) error { return nil }
func (f *parentFlagLLMFile) Read(int) ([]byte, error) {
	return []byte(`{"action":"complete","summary":"done","content":"done"}`), nil
}
func (f *parentFlagLLMFile) Close() error { return nil }
func (f *parentFlagLLMFile) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{IsDevice: true, Name: "/dev/llm/claude"}, nil
}
func (f *parentFlagLLMFile) SupportsToolCalling() bool { return true }

func resetParentFlagTestGlobals(t *testing.T) {
	t.Helper()

	savedIntent := flagIntent
	savedParentPID := flagParentPID
	savedQuiet := flagQuiet
	savedJSON := flagJSON
	savedVerbose := flagVerbose
	savedModel := flagModel
	savedAgent := flagAgent
	savedProvider := flagProvider
	savedFallbackModel := flagFallbackModel
	savedFallbackProvider := flagFallbackProvider
	savedReasoningEffort := flagReasoningEffort
	savedMaxSteps := flagMaxSteps
	savedExit := exitCode
	savedSocketOverride := ipc.SocketPathOverride
	t.Cleanup(func() {
		flagIntent = savedIntent
		flagParentPID = savedParentPID
		flagQuiet = savedQuiet
		flagJSON = savedJSON
		flagVerbose = savedVerbose
		flagModel = savedModel
		flagAgent = savedAgent
		flagProvider = savedProvider
		flagFallbackModel = savedFallbackModel
		flagFallbackProvider = savedFallbackProvider
		flagReasoningEffort = savedReasoningEffort
		flagMaxSteps = savedMaxSteps
		exitCode = savedExit
		ipc.SocketPathOverride = savedSocketOverride
	})

	flagIntent = ""
	flagParentPID = 0
	flagQuiet = false
	flagJSON = false
	flagVerbose = false
	flagModel = ""
	flagAgent = ""
	flagProvider = ""
	flagFallbackModel = ""
	flagFallbackProvider = ""
	flagReasoningEffort = ""
	flagMaxSteps = 0
	exitCode = 0
	ipc.SocketPathOverride = ""
}

func setupParentFlagIPCServer(t *testing.T) (string, *kernel.KernelImpl) {
	t.Helper()

	devReg := vfs.NewDeviceRegistry()
	_ = devReg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &parentFlagLLMFile{}, nil
	})
	vfsInst := vfs.NewVFS(devReg)
	ctxMgr := rnixctx.NewManager()

	srv := ipc.NewServer(nil, nil, "0.1.0-test", "", "")
	kern := kernel.NewKernel(vfsInst, ctxMgr, srv.CallbackMux())
	srv.SetKernel(kern)
	kernel.TestSetupDataDir(t, kern)

	sockPath := t.TempDir() + "/test.sock"
	if err := srv.ListenAndServe(sockPath); err != nil {
		t.Fatalf("ListenAndServe: %v", err)
	}
	t.Cleanup(func() {
		srv.Shutdown()
		srv.Wait()
		kern.Shutdown()
	})
	return sockPath, kern
}

func TestParentFlag_Registered(t *testing.T) {
	flag := rootCmd.Flags().Lookup("parent")
	if flag == nil {
		t.Fatal("expected root --parent flag to be registered")
	}
	if flag.Shorthand != "" {
		t.Fatalf("--parent should not have a shorthand, got %q", flag.Shorthand)
	}
	if !strings.Contains(flag.Usage, "RNIX_PARENT_PID") {
		t.Fatalf("--parent help should mention RNIX_PARENT_PID fallback, got %q", flag.Usage)
	}
}

func TestResolveParentPIDForSpawn_EnvFallback(t *testing.T) {
	got := resolveParentPIDForSpawn(0, func(key string) (string, bool) {
		if key != "RNIX_PARENT_PID" {
			t.Fatalf("unexpected env key %q", key)
		}
		return "42", true
	})
	if got != types.PID(42) {
		t.Fatalf("ParentPID = %d, want 42", got)
	}
}

func TestResolveParentPIDForSpawn_FlagOverridesEnv(t *testing.T) {
	got := resolveParentPIDForSpawn(7, func(string) (string, bool) { return "42", true })
	if got != types.PID(7) {
		t.Fatalf("ParentPID = %d, want explicit flag 7", got)
	}
}

func TestResolveParentPIDForSpawn_InvalidEnvDegradesToZero(t *testing.T) {
	got := resolveParentPIDForSpawn(0, func(string) (string, bool) { return "not-a-pid", true })
	if got != 0 {
		t.Fatalf("ParentPID = %d, want 0 for invalid RNIX_PARENT_PID", got)
	}
}

func TestRunRoot_ParentFlagSentToSpawnRequest(t *testing.T) {
	resetParentFlagTestGlobals(t)
	sockPath, kern := setupParentFlagIPCServer(t)
	ipc.SocketPathOverride = sockPath

	parent := kernel.NewProcess(0, "cmd parent", []string{"test"})
	if err := parent.Start(); err != nil {
		t.Fatalf("parent Start: %v", err)
	}
	kern.AddProcess(parent)

	flagIntent = "cmd parent child"
	flagParentPID = uint64(parent.PID)
	flagQuiet = true

	if err := runRoot(rootCmd, []string{}); err != nil {
		t.Fatalf("runRoot: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0", exitCode)
	}

	children := parent.GetChildren()
	if len(children) != 1 {
		t.Fatalf("parent children = %v, want exactly one child from --parent", children)
	}
	child, ok := kern.GetProcess(children[0])
	if !ok {
		t.Fatalf("child PID %d not found", children[0])
	}
	if child.PPID != parent.PID {
		t.Fatalf("child PPID = %d, want %d", child.PPID, parent.PID)
	}
	if child.ParentUUID != parent.UUID {
		t.Fatalf("child ParentUUID = %q, want %q", child.ParentUUID, parent.UUID)
	}
}

func TestRunRoot_ParentPIDEnvSentToSpawnRequest(t *testing.T) {
	resetParentFlagTestGlobals(t)
	sockPath, kern := setupParentFlagIPCServer(t)
	ipc.SocketPathOverride = sockPath

	parent := kernel.NewProcess(0, "cmd env parent", []string{"test"})
	if err := parent.Start(); err != nil {
		t.Fatalf("parent Start: %v", err)
	}
	kern.AddProcess(parent)
	t.Setenv("RNIX_PARENT_PID", strconv.FormatUint(uint64(parent.PID), 10))

	flagIntent = "cmd env parent child"
	flagParentPID = 0
	flagQuiet = true

	if err := runRoot(rootCmd, []string{}); err != nil {
		t.Fatalf("runRoot: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0", exitCode)
	}

	children := parent.GetChildren()
	if len(children) != 1 {
		t.Fatalf("parent children = %v, want exactly one child from RNIX_PARENT_PID", children)
	}
	child, ok := kern.GetProcess(children[0])
	if !ok {
		t.Fatalf("child PID %d not found", children[0])
	}
	if child.PPID != parent.PID {
		t.Fatalf("child PPID = %d, want %d", child.PPID, parent.PID)
	}
	if child.ParentUUID != parent.UUID {
		t.Fatalf("child ParentUUID = %q, want %q", child.ParentUUID, parent.UUID)
	}
}
