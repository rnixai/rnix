package ipc

import (
	"encoding/json"
	"testing"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
)

// ATDD Tests for Story 3.6: Step Output Streaming — IPC 层
//
// RED PHASE: These tests reference callbackMux.OnStepComplete and
// ProgressPayload.Action/Summary fields which do not exist yet.
//
// Tests verify:
// AC1/AC5: callbackMux.OnStepComplete 发送包含 action 和 summary 的 ProgressPayload
// AC5: ProgressPayload 序列化包含 action 和 summary 字段

// ============================================================
// AC1: callbackMux.OnStepComplete 发送正确的 StreamEvent
// ============================================================

func TestATDD_3_6_AC1_CallbackMux_OnStepComplete_SendsEvent(t *testing.T) {
	mux := newCallbackMux()

	// Register a listener for PID 1
	ch := make(chan StreamEvent, 10)
	mux.register(types.PID(1), ch)

	mux.OnStepComplete(types.PID(1), 3, "tool_call", "/dev/fs → read config.yaml")

	select {
	case ev := <-ch:
		if ev.Type != StreamProgress {
			t.Fatalf("AC1: expected StreamProgress, got %d", ev.Type)
		}

		var pp ProgressPayload
		if err := json.Unmarshal(ev.Payload, &pp); err != nil {
			t.Fatalf("AC1: failed to unmarshal payload: %v", err)
		}

		if pp.Event != "step_complete" {
			t.Errorf("AC1: expected event='step_complete', got %q", pp.Event)
		}
		if pp.PID != 1 {
			t.Errorf("AC1: expected PID=1, got %d", pp.PID)
		}
		if pp.Step != 3 {
			t.Errorf("AC1: expected Step=3, got %d", pp.Step)
		}
		if pp.Action != "tool_call" {
			t.Errorf("AC1: expected Action='tool_call', got %q", pp.Action)
		}
		if pp.Summary != "/dev/fs → read config.yaml" {
			t.Errorf("AC1: expected Summary='/dev/fs → read config.yaml', got %q", pp.Summary)
		}
	default:
		t.Fatal("AC1: no event sent by OnStepComplete")
	}
}

// ============================================================
// AC5: ProgressPayload JSON 序列化包含 action/summary 字段
// ============================================================

func TestATDD_3_6_AC5_ProgressPayload_JSONSerialization(t *testing.T) {
	pp := ProgressPayload{
		Event:   "step_complete",
		PID:     1,
		Step:    2,
		Action:  "plan",
		Summary: "plan (3 steps)",
	}

	data, err := json.Marshal(pp)
	if err != nil {
		t.Fatalf("AC5: json.Marshal failed: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("AC5: json.Unmarshal failed: %v", err)
	}

	if decoded["event"] != "step_complete" {
		t.Errorf("AC5: expected event='step_complete', got %v", decoded["event"])
	}
	if decoded["action"] != "plan" {
		t.Errorf("AC5: expected action='plan', got %v", decoded["action"])
	}
	if decoded["summary"] != "plan (3 steps)" {
		t.Errorf("AC5: expected summary='plan (3 steps)', got %v", decoded["summary"])
	}
	// pid should be present
	if decoded["pid"] != float64(1) {
		t.Errorf("AC5: expected pid=1, got %v", decoded["pid"])
	}
	// step should be present
	if decoded["step"] != float64(2) {
		t.Errorf("AC5: expected step=2, got %v", decoded["step"])
	}
}

// ============================================================
// AC5 补充: step_complete JSON omitempty — empty summary 不输出
// ============================================================

func TestATDD_3_6_AC5_ProgressPayload_OmitEmptySummary(t *testing.T) {
	pp := ProgressPayload{
		Event:  "step_complete",
		PID:    1,
		Step:   4,
		Action: "complete",
		// Summary is empty
	}

	data, err := json.Marshal(pp)
	if err != nil {
		t.Fatalf("AC5-omit: json.Marshal failed: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("AC5-omit: json.Unmarshal failed: %v", err)
	}

	// "summary" should be omitted when empty (due to omitempty tag)
	if _, hasSummary := decoded["summary"]; hasSummary {
		t.Errorf("AC5-omit: expected 'summary' to be omitted when empty, got %v", decoded["summary"])
	}
	// "action" should still be present
	if decoded["action"] != "complete" {
		t.Errorf("AC5-omit: expected action='complete', got %v", decoded["action"])
	}
}

// ============================================================
// 编译时接口检查: callbackMux 实现 KernelCallbacks（含 OnStepComplete）
// ============================================================

func TestATDD_3_6_CallbackMux_ImplementsKernelCallbacks(t *testing.T) {
	mux := newCallbackMux()
	var _ kernel.KernelCallbacks = mux
}
