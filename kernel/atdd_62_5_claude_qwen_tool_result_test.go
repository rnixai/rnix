package kernel

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/rnixai/rnix/internal/types"
)

func findStepByAction625(t *testing.T, recs []types.StepRecord, action string) types.StepRecord {
	t.Helper()
	for _, r := range recs {
		if r.Action == action || strings.Contains(r.ToolPath, action) || strings.Contains(r.Summary, action) {
			return r
		}
	}
	t.Fatalf("no step record found for action %q in %d records", action, len(recs))
	return types.StepRecord{}
}

func TestATDD_62_5_INT_001_ClaudeToolResultBackfilledAfterAssistantInput(t *testing.T) {
	h := newStreamHarness(t)

	h.feed(evtStarted("Read", "call_A"))
	h.feed(evtInputDelta(`{"file_p`))
	h.feed(evtAssistantToolUse("call_A", "Read", map[string]any{"file_path": "/tmp/a.txt"}))
	h.feed(evtUserToolResult("call_A", "file contents"))

	recs := h.flushAndReadSteps(t)
	rec := findStepByAction625(t, recs, "Read")
	if !jsonHasField(t, rec.ToolInput, "file_path", "/tmp/a.txt") {
		t.Fatalf("ToolInput should still use authoritative assistant input, got %q", rec.ToolInput)
	}
	if rec.ToolResult != "file contents" {
		t.Fatalf("ToolResult = %q, want %q", rec.ToolResult, "file contents")
	}
}

func TestATDD_62_5_INT_002_UserResultFlushesMatchingCallNotCurrentTool(t *testing.T) {
	h := newStreamHarness(t)

	h.feed(evtStarted("Read", "call_A"))
	h.feed(evtInputDelta(`{"file_p`))
	h.feed(evtAssistantToolUse("call_A", "Read", map[string]any{"file_path": "a.txt"}))
	h.feed(evtStarted("Bash", "call_B"))
	h.feed(evtInputDelta(`{"comm`))
	h.feed(evtUserToolResult("call_A", "contents of a"))
	h.feed(evtAssistantToolUse("call_B", "Bash", map[string]any{"command": "pwd"}))
	h.feed(evtUserToolResult("call_B", "workspace"))

	recs := h.flushAndReadSteps(t)
	readRec := findStepByAction625(t, recs, "Read")
	bashRec := findStepByAction625(t, recs, "Bash")
	if readRec.ToolResult != "contents of a" {
		t.Fatalf("Read.ToolResult = %q, want result from call_A", readRec.ToolResult)
	}
	if bashRec.ToolResult != "workspace" {
		t.Fatalf("Bash.ToolResult = %q, want result from call_B", bashRec.ToolResult)
	}
}

func TestATDD_62_5_INT_003_ParallelToolResultsNoCrosstalk(t *testing.T) {
	h := newStreamHarness(t)

	h.feed(evtStarted("Read", "call_A"))
	h.feed(evtInputDelta(`{"file_p`))
	h.feed(evtStarted("Bash", "call_B"))
	h.feed(evtInputDelta(`{"comm`))
	h.feed(map[string]any{
		"type": "assistant", "role": "assistant",
		"content": []map[string]any{
			{"type": "tool_use", "id": "call_A", "name": "Read", "input": map[string]any{"file_path": "a.txt"}},
			{"type": "tool_use", "id": "call_B", "name": "Bash", "input": map[string]any{"command": "pwd"}},
		},
	})
	h.feed(map[string]any{
		"type": "user", "role": "user",
		"content": []map[string]any{
			{"type": "tool_result", "tool_use_id": "call_A", "content": "contents of a"},
			{"type": "tool_result", "tool_use_id": "call_B", "content": "workspace"},
		},
	})

	recs := h.flushAndReadSteps(t)
	readRec := findStepByAction625(t, recs, "Read")
	bashRec := findStepByAction625(t, recs, "Bash")
	if readRec.ToolResult != "contents of a" {
		t.Fatalf("Read.ToolResult = %q, want contents of a", readRec.ToolResult)
	}
	if bashRec.ToolResult != "workspace" {
		t.Fatalf("Bash.ToolResult = %q, want workspace", bashRec.ToolResult)
	}
}

func TestATDD_62_5_INT_004_TerminalToolWithoutResultStillFlushesOnDone(t *testing.T) {
	h := newStreamHarness(t)

	h.feed(evtStarted("Read", "call_A"))
	h.feed(evtInputDelta(`{"file_path":"tail.txt"}`))
	h.feed(map[string]any{"type": "done"})

	recs := h.flushAndReadSteps(t)
	if len(recs) != 1 {
		t.Fatalf("expected one terminal step record, got %d", len(recs))
	}
	if rec := recs[0]; rec.ToolResult != "" || !jsonHasField(t, rec.ToolInput, "file_path", "tail.txt") {
		t.Fatalf("terminal record mismatch: input=%q result=%q", rec.ToolInput, rec.ToolResult)
	}
}

func TestATDD_62_5_INT_005_LongToolResultTruncatedUTF8Safe(t *testing.T) {
	h := newStreamHarness(t)
	longResult := strings.Repeat("界", 70*1024)

	h.feed(evtStarted("Read", "call_A"))
	h.feed(evtAssistantToolUse("call_A", "Read", map[string]any{"file_path": "large.txt"}))
	h.feed(evtUserToolResult("call_A", longResult))

	recs := h.flushAndReadSteps(t)
	rec := findStepByAction625(t, recs, "Read")
	if len(rec.ToolResult) > 64*1024 {
		t.Fatalf("ToolResult len = %d bytes, want <= 65536", len(rec.ToolResult))
	}
	if !utf8.ValidString(rec.ToolResult) {
		t.Fatalf("ToolResult is not valid UTF-8 after truncation")
	}
	if !strings.Contains(rec.ToolResult, "[truncated:") {
		t.Fatalf("ToolResult missing truncation marker: len=%d", len(rec.ToolResult))
	}
}

func TestATDD_62_5_INT_006_QwenAnyContentBlocksBackfillToolResult(t *testing.T) {
	h := newStreamHarness(t)

	h.feed(evtStarted("Read", "call_qwen"))
	h.feed(map[string]any{
		"type": "assistant",
		"role": "assistant",
		"content": []any{
			map[string]any{
				"type":  "tool_use",
				"id":    "call_qwen",
				"name":  "Read",
				"input": map[string]any{"file_path": "qwen.txt"},
			},
		},
	})
	h.feed(map[string]any{
		"type": "user",
		"role": "user",
		"content": []any{
			map[string]any{
				"type":        "tool_result",
				"tool_use_id": "call_qwen",
				"content": []any{
					map[string]any{"type": "text", "text": "qwen contents"},
				},
			},
		},
	})

	recs := h.flushAndReadSteps(t)
	rec := findStepByAction625(t, recs, "Read")
	if !jsonHasField(t, rec.ToolInput, "file_path", "qwen.txt") {
		t.Fatalf("ToolInput should accept qwen []any assistant blocks, got %q", rec.ToolInput)
	}
	if rec.ToolResult != "qwen contents" {
		t.Fatalf("ToolResult = %q, want qwen contents", rec.ToolResult)
	}
}
