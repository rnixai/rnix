package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/agtest"
	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/drivers/llm"
	drivershell "github.com/rnixai/rnix/drivers/shell"
	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/kernel"
	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// E2E — Story 68.2: golden 套件基建与首批用例
//
// cmd/rnix/agtest_test.go's existing 8 tests only exercise CLI report
// formatting against canned agtest.SuiteResult values — none of them dial a
// real daemon. That leaves everything Story 68.2 actually changed in
// ipcExecutor.Execute (Provider/ProjectDir passthrough, resolveScriptModel,
// and the AttachDebug → ListEvents syscall-collection rewrite, 裁决 4) with
// zero automated coverage; the story's own Task 5 closed that gap with a
// *manual* `rnix agtest tests/agtest/tier1/` run instead (see Completion
// Notes). This file automates that verification so a regression here is
// caught by `make test` / CI instead of the next manual run.
//
// Setup mirrors ipc/atdd_68_1_replay_e2e_test.go's setupReplayE2E: a real
// socket server + real kernel + a real *llm.ReplayDriver at /dev/llm/replay +
// a real /dev/shell so scripted Bash tool_calls actually execute (a no-agent
// spawn exposes every registered base device as a tool — see that file's own
// comment). Every test here chdirs into an empty temp dir first —
// ipcExecutor.Execute always resolves req.ProjectDir from os.Getwd() (裁决
// 5), and this repository's real .rnix/providers.yaml (gitignored,
// machine-local — see Story 68.2's Completion Notes) declares its own
// "replay" provider. Without the chdir, config.ProjectDir would walk up from
// cmd/rnix/ to the repo root and pull that local file into the project-level
// provider merge, making the test's outcome depend on what a given developer
// happens to have configured locally instead of the fixture below.
// Chdir'ing into a directory with no .rnix ancestor makes config.ProjectDir
// return "" deterministically (internal/config/paths.go), i.e. the same
// "pure global mode" the 68.1 E2E file relies on.
// =============================================================================

// setupAgtestE2E starts a real socket server backed by a real kernel with a
// real *llm.ReplayDriver mounted at /dev/llm/replay and a real /dev/shell.
// It also neutralizes ipcExecutor.Execute's cwd-based ProjectDir resolution
// (see file header) and returns the socket path.
func setupAgtestE2E(t *testing.T) (sockPath string) {
	t.Helper()

	driver := llm.NewReplayDriver("replay")
	devReg := vfs.NewDeviceRegistry()
	if err := devReg.Register("/dev/llm/replay", llm.FileFactory(driver, "/dev/llm/replay", "")); err != nil {
		t.Fatalf("register /dev/llm/replay: %v", err)
	}
	shellDriver := drivershell.NewDriver()
	if err := devReg.RegisterWithDriver("/dev/shell", drivershell.FileFactory(shellDriver, "/dev/shell"), shellDriver); err != nil {
		t.Fatalf("register /dev/shell: %v", err)
	}
	vfsInst := vfs.NewVFS(devReg)
	ctxMgr := rnixctx.NewManager()

	srv := ipc.NewServer(nil, nil, "0.1.0-test", "", "")
	kern := kernel.NewKernel(vfsInst, ctxMgr, srv.CallbackMux())
	srv.SetKernel(kern)
	kernel.TestSetupDataDir(t, kern)

	sockPath = filepath.Join(t.TempDir(), "test.sock")
	if err := srv.ListenAndServe(sockPath); err != nil {
		t.Fatalf("ListenAndServe: %v", err)
	}
	t.Cleanup(func() {
		srv.Shutdown()
		srv.Wait()
		kern.Shutdown()
	})

	t.Chdir(t.TempDir())

	return sockPath
}

func agtestE2EExecutor(sockPath string) *ipcExecutor {
	return &ipcExecutor{dialFunc: func() (*ipc.Client, error) { return ipc.Dial(sockPath) }}
}

// writeReplayScript writes a replay response script under <tempdir>/scripts/
// and returns the tempdir, so callers can set TestCaseSpec.SourceDir to it
// and Agent.Model to "scripts/"+name — the same relative-path shape the real
// tests/agtest/tier1/ suite uses — exercising resolveScriptModel's relative
// resolution for real rather than only unit-testing the pure function.
func writeReplayScript(t *testing.T, name, content string) (sourceDir string) {
	t.Helper()
	sourceDir = t.TempDir()
	scriptsDir := filepath.Join(sourceDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return sourceDir
}

// -----------------------------------------------------------------------------
// resolveScriptModel — pure function, previously zero coverage (no existing
// test references it). Table-driven; no daemon required.
// -----------------------------------------------------------------------------

func TestResolveScriptModel(t *testing.T) {
	dir := t.TempDir()
	scriptsDir := filepath.Join(dir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	realScript := filepath.Join(scriptsDir, "case.responses.yaml")
	if err := os.WriteFile(realScript, []byte("version: \"1\"\nresponses: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		tc   agtest.TestCaseSpec
		want string
	}{
		{
			name: "empty provider passes through unchanged",
			tc:   agtest.TestCaseSpec{Agent: agtest.AgentConfig{Model: "scripts/case.responses.yaml"}, SourceDir: dir},
			want: "scripts/case.responses.yaml",
		},
		{
			name: "empty model passes through unchanged",
			tc:   agtest.TestCaseSpec{Agent: agtest.AgentConfig{Provider: "replay"}, SourceDir: dir},
			want: "",
		},
		{
			name: "empty SourceDir (ParseBytes origin) passes through unchanged",
			tc:   agtest.TestCaseSpec{Agent: agtest.AgentConfig{Provider: "replay", Model: "scripts/case.responses.yaml"}},
			want: "scripts/case.responses.yaml",
		},
		{
			name: "already-absolute model passes through unchanged",
			tc:   agtest.TestCaseSpec{Agent: agtest.AgentConfig{Provider: "replay", Model: realScript}, SourceDir: dir},
			want: realScript,
		},
		{
			name: "ordinary model name with no matching file passes through unchanged",
			tc:   agtest.TestCaseSpec{Agent: agtest.AgentConfig{Provider: "claude", Model: "haiku"}, SourceDir: dir},
			want: "haiku",
		},
		{
			name: "relative path resolving to an existing file is absolutized",
			tc:   agtest.TestCaseSpec{Agent: agtest.AgentConfig{Provider: "replay", Model: "scripts/case.responses.yaml"}, SourceDir: dir},
			want: realScript,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveScriptModel(&tt.tc); got != tt.want {
				t.Errorf("resolveScriptModel() = %q, want %q", got, tt.want)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// ipcExecutor.Execute against a real daemon — Story 68.2 裁决 4: syscall
// collection is a fresh-connection, post-completion ListEvents read, not a
// live AttachDebug stream. This wiring broke twice during dev (connection
// reset by peer reusing the streaming client; then the text-terminal-action
// discovery below) — exactly the class of bug a canned-SuiteResult unit test
// cannot catch.
// -----------------------------------------------------------------------------

const twoStepReplayScript = `version: "1"
responses:
  - tool_calls:
      - name: Bash
        input:
          command: "echo hi-from-e2e"
  - content: "done"
    tool_calls:
      - name: Complete
        input:
          result: "68-2 e2e tool-chain result"
    usage:
      input_tokens: 10
      output_tokens: 5
    stop_reason: "tool_use"
`

func TestE2E_68_2_IpcExecutor_SyscallCollectionViaListEvents(t *testing.T) {
	sockPath := setupAgtestE2E(t)
	sourceDir := writeReplayScript(t, "case.responses.yaml", twoStepReplayScript)

	tc := &agtest.TestCaseSpec{
		Intent:    "e2e 68.2 — syscall collection via ListEvents",
		Agent:     agtest.AgentConfig{Provider: "replay", Model: "scripts/case.responses.yaml"},
		SourceDir: sourceDir,
		Assert: &agtest.AssertConfig{
			Output:   &agtest.OutputAssert{Contains: []string{"68-2 e2e tool-chain result"}},
			Syscalls: &agtest.SyscallAssert{Includes: []string{"ReasonStep", "Spawn"}, Excludes: []string{"Kill"}},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := agtestE2EExecutor(sockPath).Execute(ctx, tc)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("result.Error = %q, want empty", result.Error)
	}
	if !strings.Contains(result.Output, "68-2 e2e tool-chain result") {
		t.Errorf("Output = %q, want it to contain the scripted Complete result (resolveScriptModel must resolve the relative script path for the run to ever reach it)", result.Output)
	}
	if len(result.Syscalls) == 0 {
		t.Fatal("Syscalls is empty — ListEvents-based collection returned nothing for a completed run")
	}

	for _, r := range agtest.EvalOutput(result.Output, tc.Assert.Output) {
		if !r.Passed {
			t.Errorf("output assertion failed against real data: %s", r.Message)
		}
	}
	for _, r := range agtest.EvalSyscalls(result.Syscalls, tc.Assert.Syscalls) {
		if !r.Passed {
			t.Errorf("syscalls assertion failed against real data: %s (syscalls=%v)", r.Message, result.Syscalls)
		}
	}
}

// textThenCompleteReplayScript reproduces the exact shape the story's own
// "用例设计蓝图" originally specified for cases 03/06 (text response, then a
// second Complete response) — a shape that turned out to be unreachable in
// practice (Completion Notes 实现偏离 #2) and had to be replaced with a
// single-response script in the real suite. Scripting it here, deliberately,
// pins that discovery down as a regression test instead of leaving it as
// prose: a content-only response with no tool_calls is itself a terminal
// "text" action, so the second scripted response must never be consumed.
const textThenCompleteReplayScript = `version: "1"
responses:
  - content: "68-2 e2e text-only terminal result"
    usage:
      input_tokens: 8
      output_tokens: 5
    stop_reason: "end_turn"
  - content: "second response must never run"
    tool_calls:
      - name: Complete
        input:
          result: "should never be reached"
    usage:
      input_tokens: 10
      output_tokens: 5
    stop_reason: "tool_use"
`

func TestE2E_68_2_IpcExecutor_TextResponseTerminatesLoop(t *testing.T) {
	sockPath := setupAgtestE2E(t)
	sourceDir := writeReplayScript(t, "case.responses.yaml", textThenCompleteReplayScript)

	tc := &agtest.TestCaseSpec{
		Intent:    "e2e 68.2 — pure-text response is a terminal action",
		Agent:     agtest.AgentConfig{Provider: "replay", Model: "scripts/case.responses.yaml"},
		SourceDir: sourceDir,
		Assert: &agtest.AssertConfig{
			Output: &agtest.OutputAssert{
				Contains:    []string{"68-2 e2e text-only terminal result"},
				NotContains: []string{"should never be reached"},
			},
			Syscalls: &agtest.SyscallAssert{Includes: []string{"ReasonStep"}, Excludes: []string{"Kill"}},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := agtestE2EExecutor(sockPath).Execute(ctx, tc)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("result.Error = %q, want empty", result.Error)
	}
	if strings.Contains(result.Output, "should never be reached") {
		t.Fatalf("Output = %q — the script's second (Complete) response was consumed; a content-only response must terminate the loop immediately instead of waiting for a following step", result.Output)
	}
	if !strings.Contains(result.Output, "68-2 e2e text-only terminal result") {
		t.Errorf("Output = %q, want it to contain the first scripted response's own text", result.Output)
	}

	for _, r := range agtest.EvalOutput(result.Output, tc.Assert.Output) {
		if !r.Passed {
			t.Errorf("output assertion failed against real data: %s", r.Message)
		}
	}
	for _, r := range agtest.EvalSyscalls(result.Syscalls, tc.Assert.Syscalls) {
		if !r.Passed {
			t.Errorf("syscalls assertion failed against real data: %s (syscalls=%v)", r.Message, result.Syscalls)
		}
	}
}

// -----------------------------------------------------------------------------
// The centerpiece: run the real tests/agtest/tier1/ golden suite through the
// real ipcExecutor against a real daemon — automating Story 68.2 AC5's manual
// `rnix agtest tests/agtest/tier1/` acceptance run (Task 5 / Completion
// Notes). agtest/tier1_guard_test.go already guards the suite's *shape*
// (ValidateTier1 discipline, >= 10 cases); this guards that the suite
// actually still passes end to end.
// -----------------------------------------------------------------------------

func TestE2E_68_2_Tier1Suite_RunSuiteAgainstRealDaemon(t *testing.T) {
	// Resolve before setupAgtestE2E's t.Chdir takes effect below.
	tier1Dir, err := filepath.Abs("../../tests/agtest/tier1")
	if err != nil {
		t.Fatalf("resolve tier1 suite dir: %v", err)
	}
	suite, err := agtest.ParseDir(tier1Dir)
	if err != nil {
		t.Fatalf("ParseDir(%s): %v", tier1Dir, err)
	}
	if len(suite.Tests) == 0 {
		t.Fatalf("tier1 suite at %s parsed zero cases", tier1Dir)
	}

	sockPath := setupAgtestE2E(t)
	runner := &agtest.Runner{
		Executor: agtestE2EExecutor(sockPath),
		Timeout:  30 * time.Second,
	}

	result := runner.RunSuite(context.Background(), suite)

	if result.Total != len(suite.Tests) {
		t.Errorf("Total = %d, want %d", result.Total, len(suite.Tests))
	}
	if result.Errors != 0 || result.Failed != 0 {
		for _, c := range result.Cases {
			if c.Status == agtest.StatusPassed {
				continue
			}
			t.Logf("case %q: status=%s error=%q", c.Name, c.Status, c.Error)
			for _, a := range c.Assertions {
				if !a.Passed {
					t.Logf("  assertion[%s]: %s (expected=%v actual=%v)", a.Type, a.Message, a.Expected, a.Actual)
				}
			}
		}
		t.Fatalf("tier1 suite run: %d passed, %d failed, %d errors out of %d — see per-case detail above",
			result.Passed, result.Failed, result.Errors, result.Total)
	}
}
