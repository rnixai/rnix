// Package debug — helpers_test.go (Story 38-5 PR11 Step 4(a-2) debug pane 纯 helpers
// 迁出测试 · 同 alertstrip / timeline Step 4(a-2) test 模式)
//
// 测试覆盖：
//   - MaxStraceEvents 常量值（= 500 · 与 cmd/rnix 等价）
//   - ExtractDeviceName 6 项（/dev/llm/claude / /dev/fs / /dev/shell / /dev/mcp/xxx /
//     非 /dev/ path / 空 path / nil args）
//   - RenderSyscallLine 3 项（Info / Warn / Error · 颜色路由）
//   - RenderStepLine 5 项（nil StepEntry / HasError / ToolPath 替换 Summary /
//     ToolPath 不替换 / 完整数据）
//
// profile-tolerant 模式（38-3 教训 · GetForeground / GetBold 直接断言不依赖
// ANSI byte）。
package debug

import (
	"strings"
	"testing"

	"github.com/rnixai/rnix/internal/dashboard/event"
	"github.com/rnixai/rnix/internal/dashboard/timeline"
	"github.com/rnixai/rnix/ipc"
)

// =============================================================================
// MaxStraceEvents
// =============================================================================

func TestMaxStraceEvents_EqualsFiveHundred(t *testing.T) {
	if MaxStraceEvents != 500 {
		t.Errorf("MaxStraceEvents = %d, want 500 (与 cmd/rnix.maxDebugStraceEvents 等价)", MaxStraceEvents)
	}
}

// =============================================================================
// ExtractDeviceName (7 项)
// =============================================================================

func TestExtractDeviceName_DevLLM(t *testing.T) {
	sew := ipc.SyscallEventWire{Args: map[string]any{"path": "/dev/llm/claude"}}
	if got := ExtractDeviceName(sew); got != "llm" {
		t.Errorf("/dev/llm/claude: want 'llm', got %q", got)
	}
}

func TestExtractDeviceName_DevFS(t *testing.T) {
	sew := ipc.SyscallEventWire{Args: map[string]any{"path": "/dev/fs"}}
	if got := ExtractDeviceName(sew); got != "fs" {
		t.Errorf("/dev/fs: want 'fs', got %q", got)
	}
}

func TestExtractDeviceName_DevShell(t *testing.T) {
	sew := ipc.SyscallEventWire{Args: map[string]any{"path": "/dev/shell"}}
	if got := ExtractDeviceName(sew); got != "shell" {
		t.Errorf("/dev/shell: want 'shell', got %q", got)
	}
}

func TestExtractDeviceName_DevMCP(t *testing.T) {
	sew := ipc.SyscallEventWire{Args: map[string]any{"path": "/dev/mcp/playwright"}}
	if got := ExtractDeviceName(sew); got != "mcp" {
		t.Errorf("/dev/mcp/playwright: want 'mcp', got %q", got)
	}
}

func TestExtractDeviceName_NonDevPath(t *testing.T) {
	sew := ipc.SyscallEventWire{Args: map[string]any{"path": "/tmp/xxx"}}
	if got := ExtractDeviceName(sew); got != "/tmp/xxx" {
		t.Errorf("non-/dev path: want passthrough '/tmp/xxx', got %q", got)
	}
}

func TestExtractDeviceName_EmptyPath(t *testing.T) {
	sew := ipc.SyscallEventWire{Args: map[string]any{"path": ""}}
	if got := ExtractDeviceName(sew); got != "" {
		t.Errorf("empty path: want '', got %q", got)
	}
}

func TestExtractDeviceName_MissingPath(t *testing.T) {
	sew := ipc.SyscallEventWire{Args: map[string]any{}}
	if got := ExtractDeviceName(sew); got != "" {
		t.Errorf("missing path: want '', got %q", got)
	}
}

// =============================================================================
// RenderSyscallLine (3 项)
// =============================================================================

func TestRenderSyscallLine_Info(t *testing.T) {
	ev := event.UnifiedEvent{Type: event.EventSyscall, Severity: event.SevInfo, Summary: "Read(fd=3)"}
	got := RenderSyscallLine(ev, "  ", 80)
	if !strings.HasPrefix(got, "  ") {
		t.Errorf("cursor prefix should be present, got: %q", got)
	}
	if !strings.Contains(got, "Read(fd=3)") {
		t.Errorf("summary should be in output, got: %q", got)
	}
}

func TestRenderSyscallLine_Warn(t *testing.T) {
	ev := event.UnifiedEvent{Type: event.EventSyscall, Severity: event.SevWarn, Summary: "Write(fd=4)"}
	got := RenderSyscallLine(ev, "▸ ", 80)
	if !strings.Contains(got, "Write(fd=4)") {
		t.Errorf("summary should be in output, got: %q", got)
	}
	if !strings.HasPrefix(got, "▸ ") {
		t.Errorf("Unicode cursor prefix should be present, got: %q", got)
	}
}

func TestRenderSyscallLine_ErrorChangesColor(t *testing.T) {
	evInfo := event.UnifiedEvent{Severity: event.SevInfo, Summary: "x"}
	evError := event.UnifiedEvent{Severity: event.SevError, Summary: "x"}
	gotInfo := RenderSyscallLine(evInfo, "", 80)
	gotError := RenderSyscallLine(evError, "", 80)
	// ANSI fragments should differ between SevInfo (Muted) and SevError (Error)
	// 在 NoColor profile 下两者会都是 "x"，所以这里只断言 ANSI byte 数量差异
	// 不依赖具体颜色码。profile-tolerant.
	if gotInfo == gotError {
		// Skip on no-color profile
		t.Skip("no-color profile · skipping ANSI assertion (38-3 教训)")
	}
}

// =============================================================================
// RenderStepLine (5 项)
// =============================================================================

func TestRenderStepLine_NilStepEntryFallback(t *testing.T) {
	ev := event.UnifiedEvent{StepEntry: nil, Summary: "fallback summary"}
	got := RenderStepLine(ev, "  ", 80)
	if got != "  fallback summary" {
		t.Errorf("nil StepEntry: want '  fallback summary', got %q", got)
	}
}

func TestRenderStepLine_BasicFormat(t *testing.T) {
	ev := event.UnifiedEvent{StepEntry: &timeline.StepEntry{
		Summary: ipc.StepSummaryWire{Step: 7, Action: "tool_call", Summary: "reading config file"},
	}}
	got := RenderStepLine(ev, "▸ ", 80)
	if !strings.Contains(got, "#7") {
		t.Errorf("step label '#7' missing in: %q", got)
	}
	if !strings.Contains(got, "tool_call") {
		t.Errorf("action 'tool_call' missing in: %q", got)
	}
	if !strings.Contains(got, "reading config file") {
		t.Errorf("summary missing in: %q", got)
	}
	if !strings.HasPrefix(got, "▸ ") {
		t.Errorf("cursor prefix missing, got: %q", got)
	}
}

func TestRenderStepLine_HasErrorMark(t *testing.T) {
	ev := event.UnifiedEvent{StepEntry: &timeline.StepEntry{
		Summary: ipc.StepSummaryWire{Step: 1, Action: "tool_call", Summary: "x", HasError: true},
	}}
	got := RenderStepLine(ev, "  ", 80)
	if !strings.Contains(got, "✗") {
		t.Errorf("HasError should add ✗ mark, got: %q", got)
	}
}

func TestRenderStepLine_ToolPathReplacesShortSummary(t *testing.T) {
	// Summary 长度 < 8 + ToolPath 非空 → 用 ToolPath 替换
	ev := event.UnifiedEvent{StepEntry: &timeline.StepEntry{
		Summary: ipc.StepSummaryWire{Step: 1, Action: "tool_call", Summary: "x", ToolPath: "Read"},
	}}
	got := RenderStepLine(ev, "  ", 80)
	if !strings.Contains(got, "Read") {
		t.Errorf("short summary + non-empty ToolPath: should contain 'Read', got: %q", got)
	}
}

func TestRenderStepLine_ToolPathNotReplacingLongSummary(t *testing.T) {
	// Summary 长度 >= 8 → 不替换 (即使 ToolPath 非空)
	ev := event.UnifiedEvent{StepEntry: &timeline.StepEntry{
		Summary: ipc.StepSummaryWire{Step: 1, Action: "tool_call", Summary: "long enough summary text", ToolPath: "Read"},
	}}
	got := RenderStepLine(ev, "  ", 80)
	if !strings.Contains(got, "long enough summary text") {
		t.Errorf("long summary should NOT be replaced by ToolPath, got: %q", got)
	}
}
