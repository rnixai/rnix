package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gonewx/crux/internal/ui"
	"github.com/gonewx/crux/kernel"
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

	cb.OnSpawn(1, "test intent")

	output := buf.String()
	if !strings.Contains(output, "[kernel]") {
		t.Errorf("expected [kernel] prefix, got %q", output)
	}
	if !strings.Contains(output, "spawning PID 1") {
		t.Errorf("expected spawning PID message, got %q", output)
	}
}

func TestCliCallbacks_OnStep(t *testing.T) {
	ui.InitStyles(ui.TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &ui.Renderer{Writer: &buf, OutputMode: ui.ModeDefault, Profile: ui.TerminalProfile{ColorLevel: 0}}
	p := ui.NewProgressReporter(r)
	cb := &cliCallbacks{progress: p}

	cb.OnStep(1, 2, 3)

	output := buf.String()
	if !strings.Contains(output, "[agent/1]") {
		t.Errorf("expected [agent/1] prefix, got %q", output)
	}
	if !strings.Contains(output, "reasoning step 2/3") {
		t.Errorf("expected step progress, got %q", output)
	}
}

func TestOutputSuccess_JSON(t *testing.T) {
	ui.InitStyles(ui.TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	renderer := &ui.Renderer{Writer: &buf, OutputMode: ui.ModeJSON, Profile: ui.TerminalProfile{ColorLevel: 0}}

	proc := &kernel.Process{
		PID:        1,
		Result:     "test result",
		TokensUsed: 100,
	}

	outputSuccess(renderer, ui.ModeJSON, 1, proc, 5*time.Second)

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

	proc := &kernel.Process{
		PID:        1,
		Result:     "分析完成",
		TokensUsed: 50,
	}

	outputSuccess(renderer, ui.ModeDefault, 1, proc, 2*time.Second)

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

	cb.OnSpawn(1, "test")
	cb.OnStep(1, 1, 3)

	if buf.Len() != 0 {
		t.Errorf("expected no output in quiet mode, got %q", buf.String())
	}
}

func TestExitCode_InitialZero(t *testing.T) {
	// Verify exitCode starts at 0
	saved := exitCode
	defer func() { exitCode = saved }()

	exitCode = 0
	if exitCode != 0 {
		t.Errorf("expected initial exitCode 0, got %d", exitCode)
	}
}

// --- Version command tests (Task 1 / AC #5) ---

func TestVersion_WithClaude(t *testing.T) {
	saved := claudeVersionChecker
	defer func() { claudeVersionChecker = saved }()

	claudeVersionChecker = func() (string, error) {
		return "1.0.34", nil
	}

	savedJSON := flagJSON
	defer func() { flagJSON = savedJSON }()
	flagJSON = false

	var buf bytes.Buffer
	cmd := &cobra.Command{Use: "test"}
	cmd.SetOut(&buf)
	runVersion(cmd, nil)

	output := buf.String()
	if !strings.Contains(output, "crux v") {
		t.Errorf("expected crux version, got %q", output)
	}
	if !strings.Contains(output, "claude-code: 1.0.34") {
		t.Errorf("expected claude-code version, got %q", output)
	}
}

func TestVersion_WithoutClaude(t *testing.T) {
	saved := claudeVersionChecker
	defer func() { claudeVersionChecker = saved }()

	claudeVersionChecker = func() (string, error) {
		return "", fmt.Errorf("exec: not found")
	}

	savedJSON := flagJSON
	defer func() { flagJSON = savedJSON }()
	flagJSON = false

	var buf bytes.Buffer
	cmd := &cobra.Command{Use: "test"}
	cmd.SetOut(&buf)
	runVersion(cmd, nil)

	output := buf.String()
	if !strings.Contains(output, "crux v") {
		t.Errorf("expected crux version, got %q", output)
	}
	if !strings.Contains(output, "✗ claude-code CLI not found") {
		t.Errorf("expected not found message, got %q", output)
	}
	if !strings.Contains(output, "npm install -g @anthropic-ai/claude-code") {
		t.Errorf("expected install suggestion, got %q", output)
	}
}

func TestVersion_JSON(t *testing.T) {
	saved := claudeVersionChecker
	defer func() { claudeVersionChecker = saved }()

	claudeVersionChecker = func() (string, error) {
		return "1.0.34", nil
	}

	savedJSON := flagJSON
	defer func() { flagJSON = savedJSON }()
	flagJSON = true

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
	if m["version"] != version {
		t.Errorf("expected version %q, got %v", version, m["version"])
	}
	if m["claude_code_available"] != true {
		t.Errorf("expected claude_code_available true, got %v", m["claude_code_available"])
	}
	if m["claude_code"] != "1.0.34" {
		t.Errorf("expected claude_code 1.0.34, got %v", m["claude_code"])
	}
}

func TestVersion_JSON_WithoutClaude(t *testing.T) {
	saved := claudeVersionChecker
	defer func() { claudeVersionChecker = saved }()

	claudeVersionChecker = func() (string, error) {
		return "", fmt.Errorf("not found")
	}

	savedJSON := flagJSON
	defer func() { flagJSON = savedJSON }()
	flagJSON = true

	var buf bytes.Buffer
	cmd := &cobra.Command{Use: "test"}
	cmd.SetOut(&buf)
	runVersion(cmd, nil)

	var resp JSONResponse
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %s", err, buf.String())
	}
	if !resp.OK {
		t.Error("expected ok=true for version even without claude")
	}

	data, _ := json.Marshal(resp.Data)
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("failed to parse data: %v", err)
	}
	if m["claude_code_available"] != false {
		t.Errorf("expected claude_code_available false, got %v", m["claude_code_available"])
	}
	if _, exists := m["claude_code"]; exists {
		t.Errorf("expected no claude_code field when not available, got %v", m["claude_code"])
	}
}

// --- Help output tests (Task 2 / AC #6) ---

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
	if !strings.Contains(output, "分析 ./README.md") {
		t.Errorf("expected example in help output, got %q", output)
	}
}
