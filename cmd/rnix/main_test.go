package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/kernel"
	"github.com/rnixai/rnix/vfs"
	"github.com/spf13/cobra"
)

func TestResolveOutputMode(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		flagJSON, flagVerbose, flagQuiet = false, false, false
		if m := resolveOutputMode(); m != ui.ModeDefault {
			t.Errorf("expected ModeDefault, got %d", m)
		}
	})

	t.Run("json", func(t *testing.T) {
		flagJSON, flagVerbose, flagQuiet = true, false, false
		if m := resolveOutputMode(); m != ui.ModeJSON {
			t.Errorf("expected ModeJSON, got %d", m)
		}
	})

	t.Run("verbose", func(t *testing.T) {
		flagJSON, flagVerbose, flagQuiet = false, true, false
		if m := resolveOutputMode(); m != ui.ModeVerbose {
			t.Errorf("expected ModeVerbose, got %d", m)
		}
	})

	t.Run("quiet", func(t *testing.T) {
		flagJSON, flagVerbose, flagQuiet = false, false, true
		if m := resolveOutputMode(); m != ui.ModeQuiet {
			t.Errorf("expected ModeQuiet, got %d", m)
		}
	})

	t.Run("json_takes_precedence", func(t *testing.T) {
		flagJSON, flagVerbose, flagQuiet = true, true, true
		if m := resolveOutputMode(); m != ui.ModeJSON {
			t.Errorf("expected ModeJSON when all flags set, got %d", m)
		}
	})
}

func TestCliCallbacks_OnSpawn(t *testing.T) {
	ui.InitStyles(ui.TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &ui.Renderer{Writer: &buf, OutputMode: ui.ModeDefault, Profile: ui.TerminalProfile{ColorLevel: 0}}
	p := ui.NewProgressReporter(r)
	cb := &cliCallbacks{progress: p}

	cb.OnSpawn(1, "test intent", "claude", "sonnet", "019534a1-7c6b-7000-8abc-123456789012")

	output := buf.String()
	if !strings.Contains(output, "[kernel]") {
		t.Errorf("expected [kernel] prefix, got %q", output)
	}
	if !strings.Contains(output, "spawning PID 1") {
		t.Errorf("expected spawning PID message, got %q", output)
	}
	if !strings.Contains(output, "claude/sonnet") {
		t.Errorf("expected provider/model in output, got %q", output)
	}
	if !strings.Contains(output, "uuid:") {
		t.Errorf("expected uuid in output, got %q", output)
	}
}

func TestCliCallbacks_OnStep(t *testing.T) {
	ui.InitStyles(ui.TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &ui.Renderer{Writer: &buf, OutputMode: ui.ModeVerbose, Profile: ui.TerminalProfile{ColorLevel: 0}}
	p := ui.NewProgressReporter(r)
	cb := &cliCallbacks{progress: p}

	cb.OnStep(1, 2, 3)

	output := buf.String()
	if !strings.Contains(output, "[agent/1]") {
		t.Errorf("expected [agent/1] prefix, got %q", output)
	}
	if !strings.Contains(output, "reasoning step 2") {
		t.Errorf("expected step progress, got %q", output)
	}
}

func TestCliCallbacks_OnStepComplete(t *testing.T) {
	ui.InitStyles(ui.TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &ui.Renderer{Writer: &buf, OutputMode: ui.ModeDefault, Profile: ui.TerminalProfile{ColorLevel: 0}}
	p := ui.NewProgressReporter(r)
	cb := &cliCallbacks{progress: p}

	cb.OnStepComplete(1, 2, "tool_call", "/dev/fs → read config.yaml", false, 123.4)

	output := buf.String()
	if !strings.Contains(output, "[agent/1]") {
		t.Errorf("expected [agent/1] prefix, got %q", output)
	}
	if !strings.Contains(output, "step 2:") {
		t.Errorf("expected 'step 2:' in output, got %q", output)
	}
	if !strings.Contains(output, "/dev/fs") {
		t.Errorf("expected tool path in output, got %q", output)
	}
}

func TestCliCallbacks_OnStep_DefaultMode_Silent(t *testing.T) {
	ui.InitStyles(ui.TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &ui.Renderer{Writer: &buf, OutputMode: ui.ModeDefault, Profile: ui.TerminalProfile{ColorLevel: 0}}
	p := ui.NewProgressReporter(r)
	cb := &cliCallbacks{progress: p}

	cb.OnStep(1, 2, 3)

	if buf.Len() != 0 {
		t.Errorf("expected no output in default mode, got %q", buf.String())
	}
}

func TestOutputSuccess_JSON(t *testing.T) {
	ui.InitStyles(ui.TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	renderer := &ui.Renderer{Writer: &buf, OutputMode: ui.ModeJSON, Profile: ui.TerminalProfile{ColorLevel: 0}}

	outputSuccess(renderer, ui.ModeJSON, 1, "test result", 100, 5*time.Second, "claude", "sonnet", "")

	var resp JSONResponse
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse JSON output: %v\nraw: %s", err, buf.String())
	}
	if !resp.OK {
		t.Error("expected ok=true")
	}
	if resp.Data == nil {
		t.Fatal("expected non-nil data")
	}

	data, _ := json.Marshal(resp.Data)
	var success jsonSuccessData
	if err := json.Unmarshal(data, &success); err != nil {
		t.Fatalf("failed to parse data: %v", err)
	}
	if success.PID != 1 {
		t.Errorf("expected PID 1, got %d", success.PID)
	}
	if success.Result != "test result" {
		t.Errorf("expected result 'test result', got %q", success.Result)
	}
	if success.TokensUsed != 100 {
		t.Errorf("expected tokens 100, got %d", success.TokensUsed)
	}
	if success.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", success.ExitCode)
	}
}

func TestOutputSuccess_Default(t *testing.T) {
	ui.InitStyles(ui.TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	renderer := &ui.Renderer{Writer: &buf, OutputMode: ui.ModeDefault, Profile: ui.TerminalProfile{Width: 80, ColorLevel: 0}}

	outputSuccess(renderer, ui.ModeDefault, 1, "分析完成", 50, 2*time.Second, "claude", "haiku", "")

	output := buf.String()
	if !strings.Contains(output, "分析完成") {
		t.Errorf("missing result content, got %q", output)
	}
	if !strings.Contains(output, "PID 1 exited(0)") {
		t.Errorf("missing summary footer, got %q", output)
	}
	if !strings.Contains(output, "tokens: 50") {
		t.Errorf("missing token count, got %q", output)
	}
}

func TestOutputError_JSON(t *testing.T) {
	ui.InitStyles(ui.TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	renderer := &ui.Renderer{Writer: &buf, OutputMode: ui.ModeJSON, Profile: ui.TerminalProfile{ColorLevel: 0}}

	outputError(renderer, ui.ModeJSON, "/dev/llm/claude", "connection refused", "impact", "suggestion")

	var resp JSONResponse
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse JSON: %v\nraw: %s", err, buf.String())
	}
	if resp.OK {
		t.Error("expected ok=false")
	}

	data, _ := json.Marshal(resp.Error)
	var errData jsonErrorData
	if err := json.Unmarshal(data, &errData); err != nil {
		t.Fatalf("failed to parse error data: %v", err)
	}
	if errData.Message != "connection refused" {
		t.Errorf("expected message 'connection refused', got %q", errData.Message)
	}
	if errData.Device != "/dev/llm/claude" {
		t.Errorf("expected device '/dev/llm/claude', got %q", errData.Device)
	}
}

func TestOutputError_Default(t *testing.T) {
	ui.InitStyles(ui.TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	renderer := &ui.Renderer{Writer: &buf, OutputMode: ui.ModeDefault, Profile: ui.TerminalProfile{Width: 80, ColorLevel: 0, IsUnicode: true}}

	outputError(renderer, ui.ModeDefault, "/dev/llm/claude", "timeout", "影响描述", "建议操作")

	output := buf.String()
	if !strings.Contains(output, "✗") {
		t.Errorf("expected error prefix, got %q", output)
	}
	if !strings.Contains(output, "/dev/llm/claude") {
		t.Errorf("expected device path, got %q", output)
	}
	if !strings.Contains(output, "timeout") {
		t.Errorf("expected reason, got %q", output)
	}
}

func TestJSONResponse_Structure(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		resp := JSONResponse{
			OK: true,
			Data: jsonSuccessData{
				PID:        1,
				Result:     "hello",
				TokensUsed: 42,
				ElapsedMs:  6200,
				ExitCode:   0,
			},
		}
		data, err := json.Marshal(resp)
		if err != nil {
			t.Fatal(err)
		}
		output := string(data)
		if !strings.Contains(output, `"ok":true`) {
			t.Error("missing ok:true")
		}
		if !strings.Contains(output, `"pid":1`) {
			t.Error("missing pid")
		}
		if !strings.Contains(output, `"tokens_used":42`) {
			t.Error("missing tokens_used")
		}
		if !strings.Contains(output, `"elapsed_ms":6200`) {
			t.Error("missing elapsed_ms")
		}
	})

	t.Run("error_with_device", func(t *testing.T) {
		resp := JSONResponse{
			OK: false,
			Error: jsonErrorData{
				Code:    "TIMEOUT",
				Message: "request timeout (30s)",
				Syscall: "Write",
				Device:  "/dev/llm/claude",
			},
		}
		data, err := json.Marshal(resp)
		if err != nil {
			t.Fatal(err)
		}
		output := string(data)
		if !strings.Contains(output, `"ok":false`) {
			t.Error("missing ok:false")
		}
		if !strings.Contains(output, `"code":"TIMEOUT"`) {
			t.Error("missing error code")
		}
		if !strings.Contains(output, `"device":"/dev/llm/claude"`) {
			t.Error("missing device field")
		}
		if !strings.Contains(output, `"syscall":"Write"`) {
			t.Error("missing syscall field")
		}
	})
}

func TestCLICallbacks_ImplementsInterface(t *testing.T) {
	var _ kernel.KernelCallbacks = (*cliCallbacks)(nil)
}

func TestCLICallbacks_QuietMode(t *testing.T) {
	ui.InitStyles(ui.TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &ui.Renderer{Writer: &buf, OutputMode: ui.ModeQuiet, Profile: ui.TerminalProfile{ColorLevel: 0}}
	p := ui.NewProgressReporter(r)
	cb := &cliCallbacks{progress: p}

	cb.OnSpawn(1, "test", "claude", "sonnet", "019534a1-7c6b-7000-8abc-123456789012")
	cb.OnStep(1, 1, 3)

	if buf.Len() != 0 {
		t.Errorf("expected no output in quiet mode, got %q", buf.String())
	}
}

func TestExitCode_InitialZero(t *testing.T) {
	saved := exitCode
	defer func() { exitCode = saved }()

	exitCode = 0
	if exitCode != 0 {
		t.Errorf("expected initial exitCode 0, got %d", exitCode)
	}
}

// --- Version command tests (Task 1 / AC #5) ---
// Story 45.1 rewrote the version system to three-source fallback (ldflags >
// BuildInfo > hardcoded "0.0.0"). The legacy "-dev" suffix is gone; runVersion
// now sources its values from buildVersionInfo() instead of reading
// package-level vars directly. These tests still modify package-level
// ldflags-injection vars (ldVersion, ldGitCommit, ldBuildDate, flagJSON,
// claudeVersionChecker) via save/restore. Do NOT add t.Parallel().

func TestVersion_Basic(t *testing.T) {
	savedJSON := flagJSON
	savedV, savedC, savedD := ldVersion, ldGitCommit, ldBuildDate
	defer func() {
		flagJSON = savedJSON
		ldVersion, ldGitCommit, ldBuildDate = savedV, savedC, savedD
	}()
	flagJSON = false
	// Pin ldflags so the test does not depend on the test binary's BuildInfo.
	ldVersion, ldGitCommit, ldBuildDate = "v9.9.9-test", "deadbee", "2026-05-23T00:00:00Z"

	var buf bytes.Buffer
	cmd := &cobra.Command{Use: "test"}
	cmd.SetOut(&buf)
	runVersion(cmd, nil)

	output := buf.String()
	// Story 45.1 §AC#1 dropped the "-dev" suffix — version is just SemVer now.
	if !strings.Contains(output, "rnix v9.9.9-test") {
		t.Errorf("expected version line, got %q", output)
	}
	if strings.Contains(output, "-dev") {
		t.Errorf("legacy '-dev' suffix must not appear in new version output, got %q", output)
	}
	if strings.Contains(output, "claude-code") {
		t.Errorf("version should not contain claude-code info, got %q", output)
	}
}

func TestVersion_JSON_Basic(t *testing.T) {
	savedJSON := flagJSON
	savedV, savedC, savedD := ldVersion, ldGitCommit, ldBuildDate
	defer func() {
		flagJSON = savedJSON
		ldVersion, ldGitCommit, ldBuildDate = savedV, savedC, savedD
	}()
	flagJSON = true
	ldVersion, ldGitCommit, ldBuildDate = "v9.9.9-test", "deadbee", "2026-05-23T00:00:00Z"

	var buf bytes.Buffer
	cmd := &cobra.Command{Use: "test"}
	cmd.SetOut(&buf)
	runVersion(cmd, nil)

	var resp JSONResponse
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %s", err, buf.String())
	}
	if !resp.OK {
		t.Error("expected ok=true")
	}

	data, _ := json.Marshal(resp.Data)
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("failed to parse data: %v", err)
	}
	if m["version"] != "v9.9.9-test" {
		t.Errorf("expected version %q, got %v", "v9.9.9-test", m["version"])
	}
	if _, ok := m["git_commit"]; !ok {
		t.Error("expected git_commit field in JSON output")
	}
	if _, ok := m["build_date"]; !ok {
		t.Error("expected build_date field in JSON output")
	}
	// Story 45.1 AC#1 added the dirty bool — must be present and boolean.
	if _, ok := m["dirty"]; !ok {
		t.Error("expected dirty field in JSON output (Story 45.1 AC#1)")
	}
	if _, ok := m["dirty"].(bool); !ok {
		t.Errorf("dirty field must be bool, got %T (%v)", m["dirty"], m["dirty"])
	}
	if _, exists := m["claude_code_available"]; exists {
		t.Errorf("version JSON should not contain claude_code_available field")
	}
}

// TestVersionString_NoLdflags_FallbackToBuildInfo (was TestVersionString_Dev).
// Story 45.1 §AC#1 dropped the "-dev" suffix; when ldflags are not injected,
// versionString falls back to BuildInfo (or the hardcoded floor when BuildInfo
// has no VCS data). The test only asserts that the legacy "-dev" suffix is
// gone — the exact fallback value depends on the test binary's BuildInfo.
func TestVersionString_NoLdflags_FallbackToBuildInfo(t *testing.T) {
	savedV, savedC, savedD := ldVersion, ldGitCommit, ldBuildDate
	defer func() {
		ldVersion, ldGitCommit, ldBuildDate = savedV, savedC, savedD
	}()
	ldVersion, ldGitCommit, ldBuildDate = "", "", ""

	got := versionString()
	if strings.HasSuffix(got, "-dev") {
		t.Errorf("versionString() = %q, legacy '-dev' suffix must be gone (Story 45.1 AC#1)", got)
	}
	if got == "" {
		t.Errorf("versionString() returned empty string — must fall back to BuildInfo or hardcoded floor")
	}
}

// TestVersionString_Release covers the ldflags-injection path (Story 45.1 §AC#1
// priority 1). When the Makefile injects ldVersion, versionString must return
// it verbatim (with no "-dev" suffix).
func TestVersionString_Release(t *testing.T) {
	savedV, savedC, savedD := ldVersion, ldGitCommit, ldBuildDate
	defer func() {
		ldVersion, ldGitCommit, ldBuildDate = savedV, savedC, savedD
	}()
	ldVersion, ldGitCommit, ldBuildDate = "v0.8.0", "abc1234", "2026-05-23T00:00:00Z"

	got := versionString()
	if got != "v0.8.0" {
		t.Errorf("versionString() = %q, want %q (ldflags path)", got, "v0.8.0")
	}
}

// TestVersion_WithBuildInfo: when ldflags inject commit/buildDate, runVersion
// prints the commit/built lines (no '-dev' suffix). Story 45.1 §AC#1 routes
// these through buildVersionInfo() rather than reading gitCommit/buildDate
// directly.
func TestVersion_WithBuildInfo(t *testing.T) {
	savedV, savedC, savedD := ldVersion, ldGitCommit, ldBuildDate
	savedJSON := flagJSON
	defer func() {
		ldVersion, ldGitCommit, ldBuildDate = savedV, savedC, savedD
		flagJSON = savedJSON
	}()

	ldVersion = "v0.8.0"
	ldGitCommit = "abc1234"
	ldBuildDate = "2026-03-15T00:00:00Z"
	flagJSON = false

	var buf bytes.Buffer
	cmd := &cobra.Command{Use: "test"}
	cmd.SetOut(&buf)
	runVersion(cmd, nil)

	output := buf.String()
	if !strings.Contains(output, "rnix v0.8.0") {
		t.Errorf("expected rnix v0.8.0, got %q", output)
	}
	if strings.Contains(output, "-dev") {
		t.Errorf("expected no -dev suffix with ldGitCommit set, got %q", output)
	}
	if !strings.Contains(output, "commit:  abc1234") {
		t.Errorf("expected commit line, got %q", output)
	}
	if !strings.Contains(output, "built:   2026-03-15T00:00:00Z") {
		t.Errorf("expected built line, got %q", output)
	}
}

// --- Help output tests ---

func TestHelp_ContainsUsage(t *testing.T) {
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetArgs([]string{"--help"})
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetArgs(nil)
	})
	_ = rootCmd.Execute()

	output := buf.String()
	if !strings.Contains(output, "Usage:") && !strings.Contains(output, "Usage") {
		t.Errorf("expected Usage in help output, got %q", output)
	}
	if !strings.Contains(output, "version") {
		t.Errorf("expected version subcommand in help, got %q", output)
	}
	if !strings.Contains(output, "--json") {
		t.Errorf("expected --json flag in help, got %q", output)
	}
	if !strings.Contains(output, "--verbose") {
		t.Errorf("expected --verbose flag in help, got %q", output)
	}
	if !strings.Contains(output, "--quiet") {
		t.Errorf("expected --quiet flag in help, got %q", output)
	}
	if !strings.Contains(output, "--intent") {
		t.Errorf("expected --intent flag in help, got %q", output)
	}
}

func TestHelp_ContainsExample(t *testing.T) {
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetArgs([]string{"--help"})
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetArgs(nil)
	})
	_ = rootCmd.Execute()

	output := buf.String()
	if !strings.Contains(output, "rnix -i") {
		t.Errorf("expected 'rnix -i' in example, got %q", output)
	}
	if !strings.Contains(output, "analyze ./README.md") {
		t.Errorf("expected example in help output, got %q", output)
	}
}

// --- rnix ps tests via IPC ---

func TestRenderPsJSON_EmptyList(t *testing.T) {
	ui.InitStyles(ui.TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &ui.Renderer{Writer: &buf, OutputMode: ui.ModeJSON, Profile: ui.TerminalProfile{ColorLevel: 0}}

	renderPsJSON(r, nil)

	var resp JSONResponse
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse JSON: %v\nraw: %s", err, buf.String())
	}
	if !resp.OK {
		t.Error("expected ok=true")
	}

	data, _ := json.Marshal(resp.Data)
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("failed to parse data: %v", err)
	}
	if string(m["processes"]) != "[]" {
		t.Errorf("expected empty processes array, got %s", m["processes"])
	}
}

func TestRenderPsJSON_WithProcs(t *testing.T) {
	ui.InitStyles(ui.TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &ui.Renderer{Writer: &buf, OutputMode: ui.ModeJSON, Profile: ui.TerminalProfile{ColorLevel: 0}}

	procs := []vfs.ProcInfo{
		{PID: 1, PPID: 0, State: types.StateRunning, Intent: "分析代码", Skills: []string{"code-analyst"}, TokensUsed: 1847, CreatedAt: time.Now().Add(-6 * time.Second)},
		{PID: 2, PPID: 1, State: types.StateZombie, Intent: "审查", Skills: nil, TokensUsed: 423, CreatedAt: time.Now().Add(-2 * time.Second)},
	}
	renderPsJSON(r, procs)

	var resp JSONResponse
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse JSON: %v\nraw: %s", err, buf.String())
	}
	if !resp.OK {
		t.Error("expected ok=true")
	}

	raw := buf.String()
	for _, field := range []string{"pid", "ppid", "state", "intent", "skills", "tokens_used", "elapsed_ms"} {
		if !strings.Contains(raw, fmt.Sprintf(`"%s"`, field)) {
			t.Errorf("missing field %q in: %s", field, raw)
		}
	}
	if strings.Contains(raw, `"skills":null`) {
		t.Error("nil skills should serialize as [] not null")
	}
}

func TestRenderPsQuiet(t *testing.T) {
	ui.InitStyles(ui.TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &ui.Renderer{Writer: &buf, OutputMode: ui.ModeQuiet, Profile: ui.TerminalProfile{ColorLevel: 0}}

	procs := []vfs.ProcInfo{
		{PID: 1, State: types.StateRunning},
		{PID: 5, State: types.StateZombie},
		{PID: 12, State: types.StateDead},
	}
	renderPsQuiet(r, procs)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %q", len(lines), buf.String())
	}
	if strings.TrimSpace(lines[0]) != "1" {
		t.Errorf("line 0: expected '1', got %q", lines[0])
	}
	if strings.TrimSpace(lines[1]) != "5" {
		t.Errorf("line 1: expected '5', got %q", lines[1])
	}
	if strings.TrimSpace(lines[2]) != "12" {
		t.Errorf("line 2: expected '12', got %q", lines[2])
	}
}

func TestRenderPsQuiet_Empty(t *testing.T) {
	ui.InitStyles(ui.TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &ui.Renderer{Writer: &buf, OutputMode: ui.ModeQuiet, Profile: ui.TerminalProfile{ColorLevel: 0}}

	renderPsQuiet(r, nil)

	if buf.Len() != 0 {
		t.Errorf("expected empty output for quiet empty list, got %q", buf.String())
	}
}

func TestJsonProcess_SnakeCase(t *testing.T) {
	jp := jsonProcess{
		PID:        1,
		PPID:       0,
		State:      "running",
		Intent:     "test",
		Skills:     []string{"a"},
		TokensUsed: 100,
		ElapsedMs:  5000,
	}
	data, err := json.Marshal(jp)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, field := range []string{"pid", "ppid", "state", "intent", "skills", "tokens_used", "elapsed_ms"} {
		if !strings.Contains(s, fmt.Sprintf(`"%s"`, field)) {
			t.Errorf("missing snake_case field %q in: %s", field, s)
		}
	}
}

func TestHelp_ContainsPsSubcommand(t *testing.T) {
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetArgs([]string{"--help"})
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetArgs(nil)
	})
	_ = rootCmd.Execute()

	output := buf.String()
	if !strings.Contains(output, "ps") {
		t.Errorf("expected 'ps' subcommand in help, got %q", output)
	}
	if !strings.Contains(output, "kill") {
		t.Errorf("expected 'kill' subcommand in help, got %q", output)
	}
}

// --- IPC-based CLI tests ---

// setupTestIPCServer creates a server+kernel for IPC-based CLI tests.
func setupTestIPCServer(t *testing.T) (string, *kernel.KernelImpl) {
	t.Helper()

	devReg := vfs.NewDeviceRegistry()
	vfsInst := vfs.NewVFS(devReg)
	ctxMgr := rnixctx.NewManager()

	srv := ipc.NewServer(nil, nil, "0.1.0-test", "", "")
	kern := kernel.NewKernel(vfsInst, ctxMgr, srv.CallbackMux())
	srv.SetKernel(kern)

	sockDir := t.TempDir()
	sockPath := filepath.Join(sockDir, "test.sock")

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

func TestRunKill_InvalidPID(t *testing.T) {
	saved := exitCode
	defer func() { exitCode = saved }()
	exitCode = 0

	err := runKill(&cobra.Command{}, []string{"abc"})
	if err != nil {
		t.Fatalf("runKill should return nil (errors handled internally), got %v", err)
	}
	if exitCode != 1 {
		t.Errorf("expected exitCode 1 for invalid PID, got %d", exitCode)
	}
}

func TestRunKill_PIDNotFound_ViaIPC(t *testing.T) {
	sockPath, _ := setupTestIPCServer(t)

	saved := exitCode
	defer func() { exitCode = saved }()
	exitCode = 0

	origSocketPath := ipc.SocketPath
	ipc.SocketPathOverride = sockPath
	t.Cleanup(func() {
		ipc.SocketPathOverride = ""
		_ = origSocketPath
	})

	err := runKill(&cobra.Command{}, []string{"999"})
	if err != nil {
		t.Fatalf("runKill should return nil, got %v", err)
	}
	if exitCode != 1 {
		t.Errorf("expected exitCode 1 for non-existent PID, got %d", exitCode)
	}
}

func TestRunKill_Success_ViaIPC(t *testing.T) {
	sockPath, kern := setupTestIPCServer(t)

	saved := exitCode
	defer func() { exitCode = saved }()
	exitCode = 0

	ipc.SocketPathOverride = sockPath
	t.Cleanup(func() { ipc.SocketPathOverride = "" })

	proc := kernel.NewProcess(0, "test intent", []string{"test-skill"})
	_ = proc.Start()
	kern.AddProcess(proc)

	err := runKill(&cobra.Command{}, []string{fmt.Sprintf("%d", proc.PID)})
	if err != nil {
		t.Fatalf("runKill should return nil, got %v", err)
	}
	if exitCode != 0 {
		t.Errorf("expected exitCode 0 for successful kill, got %d", exitCode)
	}
}

func TestDaemonCmd_HasSubcommands(t *testing.T) {
	stopFound := false
	statusFound := false
	for _, sub := range daemonCmd.Commands() {
		switch sub.Name() {
		case "stop":
			stopFound = true
		case "status":
			statusFound = true
		}
	}
	if !stopFound {
		t.Error("daemon command should have 'stop' subcommand")
	}
	if !statusFound {
		t.Error("daemon command should have 'status' subcommand")
	}
}

func TestDaemonCmd_ShowsHelpWithoutInternalFlag(t *testing.T) {
	saved := flagDaemonInternal
	defer func() { flagDaemonInternal = saved }()
	flagDaemonInternal = false

	err := runDaemon(daemonCmd, nil)
	if err != nil {
		t.Fatalf("expected help (no error), got: %v", err)
	}
}

func TestWireToSyscallEvent(t *testing.T) {
	sew := ipc.SyscallEventWire{
		TimestampMs: 3000,
		PID:         2,
		Syscall:     "Spawn",
		Args:        map[string]any{"intent": "test"},
		DurationMs:  15.5,
		Error:       "something failed",
	}

	event := wireToSyscallEvent(sew)
	if event.PID != 2 {
		t.Errorf("pid = %d, want 2", event.PID)
	}
	if event.Syscall != "Spawn" {
		t.Errorf("syscall = %q, want %q", event.Syscall, "Spawn")
	}
	if event.Err == nil || event.Err.Error() != "something failed" {
		t.Errorf("err = %v, want 'something failed'", event.Err)
	}
}

// --- Intent flag tests (Task 9) ---

func TestRunRoot_IntentFlag(t *testing.T) {
	savedIntent := flagIntent
	savedExit := exitCode
	defer func() {
		flagIntent = savedIntent
		exitCode = savedExit
	}()

	flagIntent = "test"
	exitCode = 0

	sockPath, _ := setupTestIPCServer(t)
	ipc.SocketPathOverride = sockPath
	t.Cleanup(func() { ipc.SocketPathOverride = "" })

	_ = runRoot(rootCmd, []string{})

	if exitCode != 1 {
		t.Errorf("expected exitCode 1 (spawn fails without LLM driver), got %d", exitCode)
	}
}

func TestRunRoot_NoArgsNoIntent_ShowsHelp(t *testing.T) {
	savedIntent := flagIntent
	defer func() { flagIntent = savedIntent }()
	flagIntent = ""

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	defer rootCmd.SetOut(nil)

	err := runRoot(rootCmd, []string{})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(buf.String(), "Usage:") {
		t.Errorf("expected help output with Usage:, got %q", buf.String())
	}
}

func TestRunRoot_IntentWithMultipleFlags(t *testing.T) {
	savedIntent := flagIntent
	savedJSON := flagJSON
	savedModel := flagModel
	savedExit := exitCode
	defer func() {
		flagIntent = savedIntent
		flagJSON = savedJSON
		flagModel = savedModel
		exitCode = savedExit
	}()

	flagIntent = "test"
	flagJSON = true
	flagModel = "opus"
	exitCode = 0

	sockPath, _ := setupTestIPCServer(t)
	ipc.SocketPathOverride = sockPath
	t.Cleanup(func() { ipc.SocketPathOverride = "" })

	_ = runRoot(rootCmd, []string{})

	if exitCode != 1 {
		t.Errorf("expected exitCode 1 (spawn fails without LLM driver), got %d", exitCode)
	}
}

// --- Bare args rejection tests (Task 10) ---

func TestRejectPositionalArgs_Empty(t *testing.T) {
	err := rejectPositionalArgs(rootCmd, []string{})
	if err != nil {
		t.Errorf("expected nil for empty args, got %v", err)
	}
}

func TestRejectPositionalArgs_UnknownWord(t *testing.T) {
	err := rejectPositionalArgs(rootCmd, []string{"foobar"})
	if err == nil {
		t.Fatal("expected error for unknown word")
	}
	msg := err.Error()
	if !strings.Contains(msg, `unknown command "foobar"`) {
		t.Errorf("expected 'unknown command \"foobar\"', got %q", msg)
	}
	if !strings.Contains(msg, "rnix -i <intent>") {
		t.Errorf("expected usage hint, got %q", msg)
	}
	if strings.Contains(msg, "did you mean") {
		t.Errorf("should not suggest for unrelated word, got %q", msg)
	}
}

func TestRejectPositionalArgs_SuggestsClose(t *testing.T) {
	err := rejectPositionalArgs(rootCmd, []string{"ver"})
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, `did you mean "version"`) {
		t.Errorf("expected suggestion for 'ver', got %q", msg)
	}
}

func TestRejectPositionalArgs_NoSuggestion(t *testing.T) {
	err := rejectPositionalArgs(rootCmd, []string{"zzz"})
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if strings.Contains(msg, "did you mean") {
		t.Errorf("should not suggest for 'zzz', got %q", msg)
	}
	if !strings.Contains(msg, `unknown command "zzz"`) {
		t.Errorf("expected unknown command message, got %q", msg)
	}
}

func TestRejectPositionalArgs_MultipleArgs(t *testing.T) {
	err := rejectPositionalArgs(rootCmd, []string{"foo", "bar"})
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, `unknown command "foo bar"`) {
		t.Errorf("expected joined display, got %q", msg)
	}
}

func TestRejectPositionalArgs_IntentPlusBareArgs(t *testing.T) {
	savedIntent := flagIntent
	defer func() { flagIntent = savedIntent }()
	flagIntent = "foo"

	err := rejectPositionalArgs(rootCmd, []string{"bar"})
	if err == nil {
		t.Fatal("expected error even with flagIntent set")
	}
}

// --- --json does not affect bare args error (Task 11) ---

func TestBareArgs_JsonFlagIgnored(t *testing.T) {
	savedJSON := flagJSON
	defer func() { flagJSON = savedJSON }()
	flagJSON = true

	// rejectPositionalArgs returns a plain fmt.Errorf regardless of flagJSON,
	// because Cobra's Args validation runs before RunE (which has JSON logic).
	err := rejectPositionalArgs(rootCmd, []string{"top"})
	if err == nil {
		t.Fatal("expected error for bare args")
	}
	msg := err.Error()
	if !strings.Contains(msg, `unknown command "top"`) {
		t.Errorf("expected plain text error containing 'unknown command', got %q", msg)
	}
	if strings.Contains(msg, `{"ok":`) {
		t.Errorf("expected non-JSON output, got %q", msg)
	}
}

// --- Fuzzy matching tests (Task 12) ---

func TestSuggestCommand_ExactMatch(t *testing.T) {
	if got := suggestCommand(rootCmd, "ps"); got != "ps" {
		t.Errorf("exact match: got %q, want %q", got, "ps")
	}
}

func TestSuggestCommand_Prefix(t *testing.T) {
	if got := suggestCommand(rootCmd, "ver"); got != "version" {
		t.Errorf("prefix match: got %q, want %q", got, "version")
	}
}

func TestSuggestCommand_Levenshtein(t *testing.T) {
	if got := suggestCommand(rootCmd, "strce"); got != "strace" {
		t.Errorf("levenshtein match: got %q, want %q", got, "strace")
	}
}

func TestSuggestCommand_Hidden(t *testing.T) {
	if got := suggestCommand(rootCmd, "deamon"); got != "daemon" {
		t.Errorf("hidden command match: got %q, want %q", got, "daemon")
	}
}

func TestSuggestCommand_ShortInputSkipsLevenshtein(t *testing.T) {
	if got := suggestCommand(rootCmd, "is"); got != "" {
		t.Errorf("short input should skip levenshtein: got %q, want empty", got)
	}
}

func TestSuggestCommand_ShortInputPrefixStillWorks(t *testing.T) {
	if got := suggestCommand(rootCmd, "ki"); got != "kill" {
		t.Errorf("short input prefix: got %q, want %q", got, "kill")
	}
}

func TestSuggestCommand_Len3PrefixMatch(t *testing.T) {
	if got := suggestCommand(rootCmd, "kil"); got != "kill" {
		t.Errorf("len=3 prefix match: got %q, want %q", got, "kill")
	}
}

func TestSuggestCommand_NoMatch(t *testing.T) {
	if got := suggestCommand(rootCmd, "zzzzz"); got != "" {
		t.Errorf("no match: got %q, want empty", got)
	}
}

func TestLevenshtein(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"ps", "ps", 0},
		{"kil", "kill", 1},
		{"", "abc", 3},
		{"abc", "xyz", 3},
		{"deamon", "daemon", 2},
	}
	for _, tc := range tests {
		if got := levenshtein(tc.a, tc.b); got != tc.want {
			t.Errorf("levenshtein(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

// ============================================================
// ATDD RED PHASE — Story 11.1: 管道语法 (Pipe Syntax)
//
// Tests reference isPipelineSyntax which does NOT exist yet
// → compile failure = RED phase.
// ============================================================

// --- 11.1-REG-001: [P2] 管道语法检测——正确识别 spawn 管道 ---

func TestIsPipelineSyntax_BasicPipe(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{`spawn "分析代码" | spawn "写文档"`, true},
		{`spawn "A" | spawn "B" | spawn "C"`, true},
		{`spawn "分析" --agent=analyst | spawn "写文档" --agent=writer`, true},
	}
	for _, tc := range tests {
		if got := isPipelineSyntax(tc.input); got != tc.want {
			t.Errorf("isPipelineSyntax(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

// --- 11.1-REG-002: [P2] 非管道 intent 不误判 ---

func TestIsPipelineSyntax_NonPipe(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{`分析 A | B 的差异`, false},
		{`比较 left|right 分支`, false},
		{`spawn "分析代码"`, false},
		{`echo "hello | world"`, false},
		{`|`, false},
		{``, false},
	}
	for _, tc := range tests {
		if got := isPipelineSyntax(tc.input); got != tc.want {
			t.Errorf("isPipelineSyntax(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

// ============================================================
// ATDD RED PHASE — Story 11.2: 变量与环境传递
//
// Tests reference isScriptSyntax which does NOT exist yet
// → compile failure = RED phase.
// ============================================================

// --- 11.2-REG-001: [P1] isScriptSyntax 检测 ---

func TestIsScriptSyntax_Positive(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		// Multi-line script (contains \n)
		{"export TARGET=./src\nspawn \"分析 $TARGET\"", true},
		// Single-line export
		{"export KEY=value", true},
		// Export case insensitive
		{"Export KEY=value", true},
		{"EXPORT KEY=value", true},
		// Export with tab separator
		{"export\tKEY=value", true},
		// Multi-line with pipeline
		{"export OUT=./reports\nspawn \"分析\" | spawn \"保存到 $OUT\"", true},
		// Multi-line with comments
		{"# comment\nexport A=1\nspawn \"test\"", true},
		// Multi-line without export
		{"spawn \"A\"\nspawn \"B\"", true},
	}
	for _, tc := range tests {
		if got := isScriptSyntax(tc.input); got != tc.want {
			t.Errorf("isScriptSyntax(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestIsScriptSyntax_Negative(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		// Single spawn (not a script)
		{`spawn "分析代码"`, false},
		// Pipeline (not a script — handled by isPipelineSyntax)
		{`spawn "A" | spawn "B"`, false},
		// Plain intent
		{`分析这段代码`, false},
		// Empty
		{``, false},
		// Word "export" in intent but not as command
		{`spawn "export the data to CSV"`, false},
	}
	for _, tc := range tests {
		if got := isScriptSyntax(tc.input); got != tc.want {
			t.Errorf("isScriptSyntax(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

// --- 11.2-REG-002: [P2] 现有单 spawn/管道路径不受影响 ---

func TestScriptDetection_PriorityOverPipeline(t *testing.T) {
	// Script with export + pipeline should be detected as script, not pipeline
	input := "export A=1\nspawn \"X\" | spawn \"Y\""
	if !isScriptSyntax(input) {
		t.Error("multi-line with export should be script syntax")
	}
}

func TestExistingPaths_Unchanged(t *testing.T) {
	// Single spawn — not script, not pipeline
	single := `spawn "分析代码"`
	if isScriptSyntax(single) {
		t.Error("single spawn should NOT be script syntax")
	}
	if isPipelineSyntax(single) {
		t.Error("single spawn should NOT be pipeline syntax")
	}

	// Pipeline — not script
	pipe := `spawn "A" | spawn "B"`
	if isScriptSyntax(pipe) {
		t.Error("pipeline should NOT be script syntax")
	}
	if !isPipelineSyntax(pipe) {
		t.Error("pipeline should be pipeline syntax")
	}
}

// ============================================================
// ATDD RED PHASE — Story 11.3: 最小控制结构 (Minimal Control Structures)
//
// isScriptSyntax does NOT detect on-error yet → runtime failure = RED.
// ============================================================

// --- 11.3-REG-003: [P1] isScriptSyntax on-error 检测 ---

func TestIsScriptSyntax_OnError(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{`spawn "A" on-error spawn "B"`, true},
		{"spawn \"A\" on-error spawn \"B\"\nspawn \"C\"", true},
	}
	for _, tc := range tests {
		if got := isScriptSyntax(tc.input); got != tc.want {
			t.Errorf("isScriptSyntax(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

// ============================================================
// ATDD RED PHASE — Story 13.1: gdb 调试会话管理 (Attach/Detach)
//
// Tests reference gdbCmd and runGdb which do NOT exist yet
// → compile failure = RED phase.
// ============================================================

// --- 13.1-CLI-001: [P0] gdb 子命令注册且可见 ---

func TestHelp_ContainsGdbSubcommand(t *testing.T) {
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetArgs([]string{"--help"})
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetArgs(nil)
	})
	_ = rootCmd.Execute()

	output := buf.String()
	if !strings.Contains(output, "gdb") {
		t.Errorf("expected 'gdb' subcommand in help, got %q", output)
	}
}

// --- 13.1-CLI-002: [P0] gdb 要求恰好 1 个 PID 参数 ---

func TestGdbCmd_RequiresExactlyOnePID(t *testing.T) {
	if gdbCmd.Args == nil {
		t.Fatal("gdbCmd.Args should be set (expected ExactArgs(1))")
	}
	// No args should fail
	if err := gdbCmd.Args(gdbCmd, []string{}); err == nil {
		t.Error("expected error with 0 args")
	}
	// 1 arg should pass
	if err := gdbCmd.Args(gdbCmd, []string{"1"}); err != nil {
		t.Errorf("expected success with 1 arg, got %v", err)
	}
	// 2 args should fail
	if err := gdbCmd.Args(gdbCmd, []string{"1", "2"}); err == nil {
		t.Error("expected error with 2 args")
	}
}

// --- 13.1-CLI-003: [P0] runGdb 无效 PID 返回错误 ---

func TestRunGdb_InvalidPID(t *testing.T) {
	saved := exitCode
	defer func() { exitCode = saved }()
	exitCode = 0

	err := runGdb(&cobra.Command{}, []string{"abc"})
	if err != nil {
		t.Fatalf("runGdb should return nil (errors handled internally), got %v", err)
	}
	if exitCode != 1 {
		t.Errorf("expected exitCode 1 for invalid PID, got %d", exitCode)
	}
}

// --- 13.1-CLI-004: [P0] runGdb PID 不存在 返回错误 ---

func TestRunGdb_PIDNotFound_ViaIPC(t *testing.T) {
	sockPath, _ := setupTestIPCServer(t)

	saved := exitCode
	defer func() { exitCode = saved }()
	exitCode = 0

	ipc.SocketPathOverride = sockPath
	t.Cleanup(func() { ipc.SocketPathOverride = "" })

	err := runGdb(&cobra.Command{}, []string{"999"})
	if err != nil {
		t.Fatalf("runGdb should return nil, got %v", err)
	}
	if exitCode != 1 {
		t.Errorf("expected exitCode 1 for non-existent PID, got %d", exitCode)
	}
}

// --- 13.1-CLI-005: [P1] gdbCmd 有正确的 Use 和 Short 描述 ---

func TestGdbCmd_UsageAndDescription(t *testing.T) {
	if !strings.Contains(gdbCmd.Use, "gdb") {
		t.Errorf("gdbCmd.Use should contain 'gdb', got %q", gdbCmd.Use)
	}
	if !strings.Contains(gdbCmd.Use, "<pid>") && !strings.Contains(gdbCmd.Use, "pid") {
		t.Errorf("gdbCmd.Use should mention pid, got %q", gdbCmd.Use)
	}
	if gdbCmd.Short == "" {
		t.Error("gdbCmd.Short should not be empty")
	}
}

// --- 13.1-CLI-006: [P1] gdbCmd 支持 --json flag ---

func TestGdbCmd_SupportsJSONFlag(t *testing.T) {
	// The --json flag should be inherited from root or registered on gdb
	// Verify gdb can be run with --json (parsing doesn't fail)
	f := gdbCmd.Flags().Lookup("json")
	if f == nil {
		// Check inherited flags from root
		f = gdbCmd.InheritedFlags().Lookup("json")
	}
	if f == nil {
		t.Error("gdb command should support --json flag (either local or inherited)")
	}
}
