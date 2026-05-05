// Package event — error_nav_test.go (Story 38-5 PR11 Step 4(c))
package event

import (
	"testing"

	"github.com/rnixai/rnix/internal/dashboard/timeline"
	"github.com/rnixai/rnix/ipc"
)

func errStep() UnifiedEvent {
	return UnifiedEvent{StepEntry: &timeline.StepEntry{Summary: ipc.StepSummaryWire{HasError: true}}}
}
func okStep() UnifiedEvent {
	return UnifiedEvent{StepEntry: &timeline.StepEntry{Summary: ipc.StepSummaryWire{HasError: false}}}
}
func sysSev(s int) UnifiedEvent {
	return UnifiedEvent{Severity: s} // StepEntry nil
}

func TestIsError_StepHasError(t *testing.T) {
	if !IsError(errStep()) {
		t.Error("step with HasError=true 应判定为 error")
	}
	if IsError(okStep()) {
		t.Error("step with HasError=false 不应判定为 error")
	}
}

func TestIsError_SystemEventSeverityThreshold(t *testing.T) {
	if IsError(sysSev(SevInfo)) {
		t.Error("SevInfo 不应判定为 error")
	}
	if IsError(sysSev(SevWarn)) {
		t.Error("SevWarn 不应判定为 error")
	}
	if !IsError(sysSev(SevError)) {
		t.Error("SevError 应判定为 error")
	}
	if !IsError(sysSev(SevCritical)) {
		t.Error("SevCritical 应判定为 error")
	}
}

func TestFindNextErrorIdx_FromBeginning(t *testing.T) {
	events := []UnifiedEvent{okStep(), errStep(), okStep()}
	idx, ok := FindNextErrorIdx(events, -1)
	if !ok || idx != 1 {
		t.Errorf("got (%d, %v), want (1, true)", idx, ok)
	}
}

func TestFindNextErrorIdx_SkipsCurrentCursor(t *testing.T) {
	events := []UnifiedEvent{errStep(), errStep(), errStep()}
	idx, ok := FindNextErrorIdx(events, 0)
	if !ok || idx != 1 {
		t.Errorf("startIdx=0: got (%d, %v), want (1, true) — 必须跳过当前位置", idx, ok)
	}
}

func TestFindNextErrorIdx_NoMore(t *testing.T) {
	events := []UnifiedEvent{errStep(), okStep(), okStep()}
	idx, ok := FindNextErrorIdx(events, 0)
	if ok || idx != -1 {
		t.Errorf("got (%d, %v), want (-1, false)", idx, ok)
	}
}

func TestFindNextErrorIdx_StartBeyondLen(t *testing.T) {
	events := []UnifiedEvent{errStep()}
	idx, ok := FindNextErrorIdx(events, 10)
	if ok || idx != -1 {
		t.Errorf("startIdx 越界: got (%d, %v), want (-1, false)", idx, ok)
	}
}

func TestFindNextErrorIdx_EmptySlice(t *testing.T) {
	idx, ok := FindNextErrorIdx(nil, 0)
	if ok || idx != -1 {
		t.Errorf("nil slice: got (%d, %v), want (-1, false)", idx, ok)
	}
}

func TestFindNextErrorIdx_MixedStepAndSystem(t *testing.T) {
	events := []UnifiedEvent{okStep(), sysSev(SevWarn), sysSev(SevError), okStep()}
	idx, ok := FindNextErrorIdx(events, 0)
	if !ok || idx != 2 {
		t.Errorf("混合 step + system: got (%d, %v), want (2, true)", idx, ok)
	}
}

func TestFindPrevErrorIdx_FromEnd(t *testing.T) {
	events := []UnifiedEvent{okStep(), errStep(), okStep()}
	idx, ok := FindPrevErrorIdx(events, 2)
	if !ok || idx != 1 {
		t.Errorf("got (%d, %v), want (1, true)", idx, ok)
	}
}

func TestFindPrevErrorIdx_SkipsCurrentCursor(t *testing.T) {
	events := []UnifiedEvent{errStep(), errStep(), errStep()}
	idx, ok := FindPrevErrorIdx(events, 2)
	if !ok || idx != 1 {
		t.Errorf("startIdx=2: got (%d, %v), want (1, true) — 必须跳过当前位置", idx, ok)
	}
}

func TestFindPrevErrorIdx_NoMore(t *testing.T) {
	events := []UnifiedEvent{okStep(), okStep(), errStep()}
	idx, ok := FindPrevErrorIdx(events, 2)
	if ok || idx != -1 {
		t.Errorf("got (%d, %v), want (-1, false)", idx, ok)
	}
}

func TestFindPrevErrorIdx_StartZero_ReturnsNotFound(t *testing.T) {
	events := []UnifiedEvent{errStep()}
	idx, ok := FindPrevErrorIdx(events, 0)
	if ok || idx != -1 {
		t.Errorf("startIdx=0: got (%d, %v), want (-1, false) — 无前位", idx, ok)
	}
}

func TestFindPrevErrorIdx_EmptySlice(t *testing.T) {
	idx, ok := FindPrevErrorIdx(nil, 5)
	if ok || idx != -1 {
		t.Errorf("nil slice: got (%d, %v), want (-1, false)", idx, ok)
	}
}

func TestFindPrevErrorIdx_MixedStepAndSystem(t *testing.T) {
	events := []UnifiedEvent{sysSev(SevError), okStep(), errStep(), okStep()}
	idx, ok := FindPrevErrorIdx(events, 3)
	if !ok || idx != 2 {
		t.Errorf("got (%d, %v), want (2, true)", idx, ok)
	}
}
