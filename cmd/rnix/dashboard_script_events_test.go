// ATDD Story 43.3 - Timeline Renderer for Script Trace Events
//
// Red-phase tests for `scriptEventFromSyscall` + `formatScriptEventSummary`
// + `scriptGlyph` helpers in cmd/rnix/dashboard_script_events.go
// (file does not yet exist; dev-story 43-3 must create it).
//
// Acceptance Criteria covered:
//   - AC#2: 5 类 ScriptEvent → UnifiedEvent 转换契约（Type/Severity/PID/UUID/Summary/Detail）
//   - AC#3: Summary 文本契约（5 模板 + ASCII fallback + 长度截断 + missing-field fallback）
//
// RED 信号（dev-story 实施前 `go test -tags atdd_red ./cmd/rnix/...` 应失败）：
//   - undefined: scriptEventFromSyscall
//   - undefined: formatScriptEventSummary
//   - undefined: EventScript（cmd/rnix 端 re-export）
//
// 实施完成后应能编译并通过；最后移除 build tag 让 `make all` 接管。

package main

import (
	"os"
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
)

// =============================================================================
// scriptEventFromSyscall — AC#2 字段映射
// =============================================================================

// TestScriptEventFromSyscall_AllFiveKinds_HappyPath — 5 类 ScriptEvent 各 1 条
// 都能正确翻译成 UnifiedEvent，Type/PID/Summary/Detail 字段非空。
//
// AC#2 关键映射：
//   - Type == EventScript（新增常量 "script"）
//   - Severity == SevInfo（无 error / exit_code）
//   - PID == ev.PID
//   - Summary 非空（formatScriptEventSummary 已生成单行）
//   - Detail 非空（按 sortedKeys 拼接的 key=value 多行）
//   - StepEntry == nil（script trace 不对应 step）
//   - IsSynthetic == false（来自磁盘的真实事件）
//   - RawEvent != nil（指向 ev 的指针副本 · 与 strace 同模式）
//   - 返回的 bool == true
func TestScriptEventFromSyscall_AllFiveKinds_HappyPath(t *testing.T) {
	cases := []struct {
		name    string
		syscall string
		args    map[string]any
	}{
		{
			name:    "ScriptStmtBegin",
			syscall: "ScriptStmtBegin",
			args:    map[string]any{"line": 47, "stmt_kind": "spawn"},
		},
		{
			name:    "ScriptStmtEnd",
			syscall: "ScriptStmtEnd",
			args:    map[string]any{"line": 47, "stmt_kind": "spawn"},
		},
		{
			name:    "ScriptSpawn",
			syscall: "ScriptSpawn",
			args:    map[string]any{"line": 47, "intent": "build hello-world", "assign": "r"},
		},
		{
			name:    "ScriptWhileIter",
			syscall: "ScriptWhileIter",
			args:    map[string]any{"line": 12, "iteration": 3, "condition": "$N != 5"},
		},
		{
			name:    "ScriptCondition",
			syscall: "ScriptCondition",
			args:    map[string]any{"line": 12, "condition": "$project_done == true", "result": false},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wire := ipc.SyscallEventWire{
				TimestampMs: 1234567890,
				PID:         types.PID(101),
				Syscall:     tc.syscall,
				Args:        tc.args,
			}
			ue, ok := scriptEventFromSyscall(wire)
			if !ok {
				t.Fatalf("scriptEventFromSyscall(%q) returned ok=false (want true for known kind)", tc.syscall)
			}
			if ue.Type != EventScript {
				t.Errorf("Type = %q, want EventScript (%q)", ue.Type, EventScript)
			}
			if ue.Severity != SevInfo {
				t.Errorf("Severity = %d, want SevInfo (%d) — no error/exit_code in args", ue.Severity, SevInfo)
			}
			if ue.PID != types.PID(101) {
				t.Errorf("PID = %d, want 101", ue.PID)
			}
			if ue.Summary == "" {
				t.Error("Summary is empty (formatScriptEventSummary must produce a non-empty line)")
			}
			if ue.Detail == "" {
				t.Error("Detail is empty (must contain key=value lines from args)")
			}
			if ue.StepEntry != nil {
				t.Error("StepEntry must be nil for ScriptEvent (no associated step)")
			}
			if ue.IsSynthetic {
				t.Error("IsSynthetic must be false (event came from disk)")
			}
			if ue.RawEvent == nil {
				t.Error("RawEvent must be non-nil (pointer to copied wire · same as strace)")
			}
			if ue.Timestamp.UnixMilli() != 1234567890 {
				t.Errorf("Timestamp.UnixMilli() = %d, want 1234567890", ue.Timestamp.UnixMilli())
			}
		})
	}
}

// TestScriptEventFromSyscall_ErrorSeverity — AC#2 Severity 升级规则：
//   - args["error"] 非空字符串 → SevError
//   - args["exit_code"] != 0 (int 或 float64) → SevError
//   - args["exit_code"] == 0 → SevInfo
//   - args["stopped"] == true（自愿终止）→ SevInfo（break/return 不是错误）
func TestScriptEventFromSyscall_ErrorSeverity(t *testing.T) {
	cases := []struct {
		name    string
		args    map[string]any
		wantSev int
	}{
		{
			name:    "error_string_nonempty",
			args:    map[string]any{"line": 47, "stmt_kind": "pipeline", "error": "command not found"},
			wantSev: SevError,
		},
		{
			name:    "exit_code_nonzero_int",
			args:    map[string]any{"line": 47, "stmt_kind": "pipeline", "exit_code": 2},
			wantSev: SevError,
		},
		{
			name:    "exit_code_nonzero_float",
			args:    map[string]any{"line": 47, "stmt_kind": "pipeline", "exit_code": float64(127)},
			wantSev: SevError,
		},
		{
			name:    "exit_code_zero",
			args:    map[string]any{"line": 47, "stmt_kind": "pipeline", "exit_code": 0},
			wantSev: SevInfo,
		},
		{
			name:    "error_empty_string",
			args:    map[string]any{"line": 47, "stmt_kind": "pipeline", "error": ""},
			wantSev: SevInfo,
		},
		{
			name:    "stopped_true_no_error",
			args:    map[string]any{"line": 47, "stmt_kind": "if", "stopped": true},
			wantSev: SevInfo,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wire := ipc.SyscallEventWire{
				PID:     types.PID(101),
				Syscall: "ScriptStmtEnd",
				Args:    tc.args,
			}
			ue, ok := scriptEventFromSyscall(wire)
			if !ok {
				t.Fatalf("ok=false (want true)")
			}
			if ue.Severity != tc.wantSev {
				t.Errorf("Severity = %d, want %d (args=%v)", ue.Severity, tc.wantSev, tc.args)
			}
		})
	}
}

// TestScriptEventFromSyscall_UnknownSyscall_ReturnsFalse — AC#2 helper 边界：
// syscall 名不在 5 类内 → (UnifiedEvent{}, false)，让 caller 跳过非 Script 事件。
func TestScriptEventFromSyscall_UnknownSyscall_ReturnsFalse(t *testing.T) {
	cases := []string{"DriverToolCall", "Read", "Spawn", "Compact", "", "scriptstmtbegin" /* 大小写敏感 */}
	for _, syscall := range cases {
		t.Run(syscall, func(t *testing.T) {
			wire := ipc.SyscallEventWire{
				PID:     types.PID(101),
				Syscall: syscall,
				Args:    map[string]any{"foo": "bar"},
			}
			ue, ok := scriptEventFromSyscall(wire)
			if ok {
				t.Errorf("ok=true for %q (want false)", syscall)
			}
			// zero-value UnifiedEvent expected
			if ue.Type != "" || ue.Summary != "" {
				t.Errorf("expected zero UnifiedEvent, got Type=%q Summary=%q", ue.Type, ue.Summary)
			}
		})
	}
}

// TestScriptEventFromSyscall_Detail_ContainsAllArgs — AC#2 Detail 必须包含 args 中所有键
// （按 sortedKeys 顺序拼成 "key=value\nkey=value\n..."）。
func TestScriptEventFromSyscall_Detail_ContainsAllArgs(t *testing.T) {
	wire := ipc.SyscallEventWire{
		PID:     types.PID(101),
		Syscall: "ScriptSpawn",
		Args: map[string]any{
			"line":   47,
			"intent": "build hello",
			"agent":  "bmad-dev",
			"assign": "r",
		},
	}
	ue, ok := scriptEventFromSyscall(wire)
	if !ok {
		t.Fatal("ok=false (want true)")
	}
	for _, key := range []string{"line", "intent", "agent", "assign"} {
		if !strings.Contains(ue.Detail, key+"=") {
			t.Errorf("Detail missing %q=... line:\n%s", key, ue.Detail)
		}
	}
}

// =============================================================================
// formatScriptEventSummary — AC#3 Summary 文本契约
// =============================================================================

// TestFormatScriptEventSummary_StmtBeginTemplate — AC#3 表格 ScriptStmtBegin 行：
// `"L{line} ▸ {stmt_kind}"` (Unicode) or `"L{line} > {stmt_kind}"` (ASCII)
func TestFormatScriptEventSummary_StmtBeginTemplate(t *testing.T) {
	os.Unsetenv("RNIX_ASCII") // Unicode mode
	args := map[string]any{"line": 47, "stmt_kind": "spawn"}
	got := formatScriptEventSummary("ScriptStmtBegin", args)
	want := "L47 ▸ spawn"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestFormatScriptEventSummary_StmtEndSuccess — AC#3 ScriptStmtEnd 正常路径：
// `"L{line} ✓ {stmt_kind}"`
func TestFormatScriptEventSummary_StmtEndSuccess(t *testing.T) {
	os.Unsetenv("RNIX_ASCII")
	args := map[string]any{"line": 47, "stmt_kind": "spawn"}
	got := formatScriptEventSummary("ScriptStmtEnd", args)
	want := "L47 ✓ spawn"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestFormatScriptEventSummary_StmtEndError — AC#3 ScriptStmtEnd error 路径：
// `"L{line} ✗ {stmt_kind}: {error}"`
func TestFormatScriptEventSummary_StmtEndError(t *testing.T) {
	os.Unsetenv("RNIX_ASCII")
	args := map[string]any{
		"line":      99,
		"stmt_kind": "pipeline",
		"error":     "command not found",
	}
	got := formatScriptEventSummary("ScriptStmtEnd", args)
	if !strings.Contains(got, "L99") || !strings.Contains(got, "✗") || !strings.Contains(got, "pipeline") || !strings.Contains(got, "command not found") {
		t.Errorf("got %q, want format \"L99 ✗ pipeline: command not found\"", got)
	}
}

// TestFormatScriptEventSummary_StmtEndStopped — AC#3 ScriptStmtEnd 自愿终止：
// args["stopped"] == true → `"L{line} ⊘ {stmt_kind} (break/return)"`
func TestFormatScriptEventSummary_StmtEndStopped(t *testing.T) {
	os.Unsetenv("RNIX_ASCII")
	args := map[string]any{
		"line":      20,
		"stmt_kind": "if",
		"stopped":   true,
	}
	got := formatScriptEventSummary("ScriptStmtEnd", args)
	if !strings.Contains(got, "⊘") || !strings.Contains(got, "if") || !strings.Contains(got, "break/return") {
		t.Errorf("got %q, want format \"L20 ⊘ if (break/return)\"", got)
	}
}

// TestFormatScriptEventSummary_SpawnTemplate — AC#3 ScriptSpawn:
//   - 基础：`"L{line} ↳ spawn {intent}"`
//   - 带 assign：追加 ` → ${assign}`
//   - 带 parallel：追加 ` [parallel]`
func TestFormatScriptEventSummary_SpawnTemplate(t *testing.T) {
	os.Unsetenv("RNIX_ASCII")

	t.Run("basic_intent_with_assign", func(t *testing.T) {
		args := map[string]any{
			"line":   47,
			"intent": "build hello-world",
			"assign": "r",
		}
		got := formatScriptEventSummary("ScriptSpawn", args)
		if !strings.Contains(got, "L47") || !strings.Contains(got, "↳") || !strings.Contains(got, "build hello-world") || !strings.Contains(got, "$r") {
			t.Errorf("got %q, want format \"L47 ↳ spawn \\\"build hello-world\\\" → $r\"", got)
		}
	})

	t.Run("parallel_flag", func(t *testing.T) {
		args := map[string]any{
			"line":     51,
			"agent":    "bmad-help",
			"parallel": true,
		}
		got := formatScriptEventSummary("ScriptSpawn", args)
		if !strings.Contains(got, "L51") || !strings.Contains(got, "[parallel]") {
			t.Errorf("got %q, want suffix \"[parallel]\"", got)
		}
	})

	t.Run("fallback_to_agent_when_intent_empty", func(t *testing.T) {
		args := map[string]any{
			"line":  51,
			"agent": "bmad-help",
		}
		got := formatScriptEventSummary("ScriptSpawn", args)
		if !strings.Contains(got, "bmad-help") {
			t.Errorf("got %q, expected to contain agent \"bmad-help\" as fallback", got)
		}
	})
}

// TestFormatScriptEventSummary_WhileIterTemplate — AC#3 ScriptWhileIter:
// `"L{line} ↻ while iter={iteration} ({condition})"`
func TestFormatScriptEventSummary_WhileIterTemplate(t *testing.T) {
	os.Unsetenv("RNIX_ASCII")

	t.Run("with_condition", func(t *testing.T) {
		args := map[string]any{
			"line":      12,
			"iteration": 3,
			"condition": "$N != 5",
		}
		got := formatScriptEventSummary("ScriptWhileIter", args)
		want := "L12 ↻ while iter=3 ($N != 5)"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("condition_missing_omits_parens", func(t *testing.T) {
		args := map[string]any{"line": 12, "iteration": 1}
		got := formatScriptEventSummary("ScriptWhileIter", args)
		if strings.Contains(got, "()") {
			t.Errorf("got %q, condition-missing case should not emit empty parens", got)
		}
		if !strings.Contains(got, "L12") || !strings.Contains(got, "iter=1") {
			t.Errorf("got %q, want at least L12 + iter=1", got)
		}
	})
}

// TestFormatScriptEventSummary_ConditionTemplate — AC#3 ScriptCondition:
//   - true → `"L{line} ? {condition} → T"`
//   - false → `"L{line} ? {condition} → F"`
//   - error → `"? {condition} → ERR: {error}"`
//   - condition 缺省 → 取 args["left"]
func TestFormatScriptEventSummary_ConditionTemplate(t *testing.T) {
	os.Unsetenv("RNIX_ASCII")

	t.Run("result_true", func(t *testing.T) {
		args := map[string]any{
			"line":      31,
			"condition": "$r != \"\"",
			"result":    true,
		}
		got := formatScriptEventSummary("ScriptCondition", args)
		if !strings.Contains(got, "L31") || !strings.Contains(got, "→ T") {
			t.Errorf("got %q, want format \"L31 ? $r != \\\"\\\" → T\"", got)
		}
	})

	t.Run("result_false", func(t *testing.T) {
		args := map[string]any{
			"line":      12,
			"condition": "$project_done == true",
			"result":    false,
		}
		got := formatScriptEventSummary("ScriptCondition", args)
		if !strings.Contains(got, "→ F") {
			t.Errorf("got %q, want \"→ F\" suffix", got)
		}
	})

	t.Run("error_overrides_result", func(t *testing.T) {
		args := map[string]any{
			"line":      12,
			"condition": "$N",
			"error":     "variable not bound",
		}
		got := formatScriptEventSummary("ScriptCondition", args)
		if !strings.Contains(got, "ERR:") || !strings.Contains(got, "variable not bound") {
			t.Errorf("got %q, want ERR: prefix when error present", got)
		}
	})

	t.Run("falls_back_to_left_when_condition_missing", func(t *testing.T) {
		args := map[string]any{
			"line":   12,
			"left":   "$x",
			"result": true,
		}
		got := formatScriptEventSummary("ScriptCondition", args)
		if !strings.Contains(got, "$x") {
			t.Errorf("got %q, want fallback to left=\"$x\"", got)
		}
	})
}

// TestFormatScriptEventSummary_ASCIIFallback — AC#3 RNIX_ASCII=1 时所有 Unicode
// 字符替换为 ASCII（▸→> ✓→. ✗→x ⊘→/ ↳→> ↻→@ ? 保留）。
// 与 RenderUnifiedStepHeader Story 36-4 同模式。
func TestFormatScriptEventSummary_ASCIIFallback(t *testing.T) {
	t.Setenv("RNIX_ASCII", "1")

	unicodeChars := []string{"▸", "✓", "✗", "⊘", "↳", "↻"}

	cases := []struct {
		name    string
		syscall string
		args    map[string]any
	}{
		{"stmt_begin", "ScriptStmtBegin", map[string]any{"line": 47, "stmt_kind": "spawn"}},
		{"stmt_end_ok", "ScriptStmtEnd", map[string]any{"line": 47, "stmt_kind": "spawn"}},
		{"stmt_end_err", "ScriptStmtEnd", map[string]any{"line": 99, "stmt_kind": "pipe", "error": "x"}},
		{"stmt_end_stopped", "ScriptStmtEnd", map[string]any{"line": 20, "stmt_kind": "if", "stopped": true}},
		{"spawn", "ScriptSpawn", map[string]any{"line": 47, "intent": "build"}},
		{"while_iter", "ScriptWhileIter", map[string]any{"line": 12, "iteration": 3, "condition": "$N != 0"}},
		{"condition", "ScriptCondition", map[string]any{"line": 12, "condition": "$x", "result": true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := formatScriptEventSummary(tc.syscall, tc.args)
			for _, ch := range unicodeChars {
				if strings.Contains(got, ch) {
					t.Errorf("got %q contains forbidden Unicode %q under RNIX_ASCII=1", got, ch)
				}
			}
		})
	}
}

// TestFormatScriptEventSummary_TruncatesLongIntent — AC#3 长度上限 120 列
// （runewidth.StringWidth），超出用 "…" 截断。
func TestFormatScriptEventSummary_TruncatesLongIntent(t *testing.T) {
	os.Unsetenv("RNIX_ASCII")

	longIntent := strings.Repeat("a", 300)
	args := map[string]any{"line": 47, "intent": longIntent}
	got := formatScriptEventSummary("ScriptSpawn", args)

	w := runewidth.StringWidth(got)
	if w > 120 {
		t.Errorf("StringWidth(summary) = %d, want <= 120 (got %q)", w, got)
	}
	// 截断标记
	if !strings.Contains(got, "…") {
		t.Errorf("expected ellipsis '…' in truncated summary: %q", got)
	}
}

// TestFormatScriptEventSummary_StripsNewlines — AC#3 Summary 严格单行；
// args 中含 \n 必须 strip（与 shortenArgs 同模式）。
func TestFormatScriptEventSummary_StripsNewlines(t *testing.T) {
	os.Unsetenv("RNIX_ASCII")
	args := map[string]any{
		"line":      12,
		"condition": "line1\nline2\nline3",
		"result":    true,
	}
	got := formatScriptEventSummary("ScriptCondition", args)
	if strings.Contains(got, "\n") {
		t.Errorf("summary contains \\n (must be stripped to single line): %q", got)
	}
}

// TestFormatScriptEventSummary_MissingFields_NoPanic — AC#3 args 缺失关键字段
// 必须不 panic：line 缺省 → "L?"，stmt_kind/condition/left/error 缺省 → 各按
// 上表降级。
func TestFormatScriptEventSummary_MissingFields_NoPanic(t *testing.T) {
	os.Unsetenv("RNIX_ASCII")

	cases := []string{
		"ScriptStmtBegin",
		"ScriptStmtEnd",
		"ScriptSpawn",
		"ScriptWhileIter",
		"ScriptCondition",
	}
	for _, syscall := range cases {
		t.Run(syscall, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("formatScriptEventSummary(%q, empty args) panicked: %v", syscall, r)
				}
			}()
			got := formatScriptEventSummary(syscall, map[string]any{})
			if got == "" {
				t.Errorf("got empty summary for %q with missing args (expected fallback like \"L?\")", syscall)
			}
			if !strings.Contains(got, "L?") {
				t.Errorf("got %q, expected \"L?\" fallback when line key is missing", got)
			}
		})
	}
}

// TestFormatScriptEventSummary_NilArgs_NoPanic — args == nil 同样不 panic。
func TestFormatScriptEventSummary_NilArgs_NoPanic(t *testing.T) {
	os.Unsetenv("RNIX_ASCII")
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("nil args panicked: %v", r)
		}
	}()
	got := formatScriptEventSummary("ScriptStmtBegin", nil)
	if got == "" {
		t.Error("got empty summary with nil args (expected fallback string)")
	}
}
