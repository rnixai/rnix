package ui

// =============================================================================
// ATDD Story 28.1: RenderProcessTable UUID Column
// TDD RED PHASE — Tests reference the showUUID parameter not yet added.
//                  Compilation failure IS the red phase.
// =============================================================================
//
// Test Strategy:
//   AC-4: RenderProcessTable(showUUID=true) shows UUID column
//   AC-4: RenderProcessTable(showUUID=false) hides UUID column (backward compat)
//   AC-4: UUID column truncation display
//
// Priority: P0 (user-visible output)
// Test Level: Unit

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

const atddTestUUID1 = "019534a1-7c6b-7000-8abc-123456789012"
const atddTestUUID2 = "019534a1-7c6b-7000-8abc-abcdef000001"
const atddTestUUID3 = "019534a1-7c6b-7000-8abc-abcdef000002"

func atddSampleProcsWithUUID() []vfs.ProcInfo {
	base := time.Now().Add(-6200 * time.Millisecond)
	return []vfs.ProcInfo{
		{PID: 1, PPID: 0, State: types.StateRunning, Intent: "分析代码", Skills: []string{"code-analyst"}, TokensUsed: 1847, CreatedAt: base, UUID: atddTestUUID1},
		{PID: 2, PPID: 1, State: types.StateZombie, Intent: "审查 PR", Skills: nil, TokensUsed: 423, CreatedAt: base.Add(-500 * time.Millisecond), UUID: atddTestUUID2},
		{PID: 3, PPID: 0, State: types.StateDead, Intent: "重构", Skills: []string{"pr-reviewer"}, TokensUsed: 3201, CreatedAt: base.Add(-10 * time.Second), UUID: atddTestUUID3},
	}
}

// ---------------------------------------------------------------------------
// AC-4: showUUID=true shows UUID column header
// ---------------------------------------------------------------------------

func TestATDD_28_1_AC4_ShowUUID_True_HasUUIDHeader(t *testing.T) {
	InitStyles(defaultTestProfile())
	r, buf := testRenderer(defaultTestProfile(), ModeDefault)

	RenderProcessTable(r, atddSampleProcsWithUUID(), false, true)
	out := buf.String()

	if !strings.Contains(out, "UUID") {
		t.Fatalf("AC-4: output should contain 'UUID' column header when showUUID=true, got:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// AC-4: showUUID=true shows UUID values in rows
// ---------------------------------------------------------------------------

func TestATDD_28_1_AC4_ShowUUID_True_ContainsUUIDValues(t *testing.T) {
	InitStyles(defaultTestProfile())
	r, buf := testRenderer(defaultTestProfile(), ModeDefault)

	RenderProcessTable(r, atddSampleProcsWithUUID(), false, true)
	out := buf.String()

	// UUID renders in short suffix form: "…" + last 6 chars — check the tail.
	suffix1 := atddTestUUID1[len(atddTestUUID1)-6:]
	if !strings.Contains(out, suffix1) {
		t.Fatalf("AC-4: output should contain UUID suffix %q, got:\n%s", suffix1, out)
	}
}

// ---------------------------------------------------------------------------
// AC-4: showUUID=false hides UUID column (backward compatibility)
// ---------------------------------------------------------------------------

func TestATDD_28_1_AC4_ShowUUID_False_NoUUIDHeader(t *testing.T) {
	InitStyles(defaultTestProfile())
	r, buf := testRenderer(defaultTestProfile(), ModeDefault)

	RenderProcessTable(r, atddSampleProcsWithUUID(), false, false)
	out := buf.String()

	if strings.Contains(out, "UUID") {
		t.Fatalf("AC-4: output should NOT contain 'UUID' header when showUUID=false, got:\n%s", out)
	}
}

func TestATDD_28_1_AC4_ShowUUID_False_NoUUIDValues(t *testing.T) {
	InitStyles(defaultTestProfile())
	r, buf := testRenderer(defaultTestProfile(), ModeDefault)

	RenderProcessTable(r, atddSampleProcsWithUUID(), false, false)
	out := buf.String()

	suffix1 := atddTestUUID1[len(atddTestUUID1)-6:]
	if strings.Contains(out, suffix1) {
		t.Fatalf("AC-4: output should NOT contain UUID values when showUUID=false, got:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// AC-4: Verbose mode + showUUID
// ---------------------------------------------------------------------------

func TestATDD_28_1_AC4_Verbose_And_ShowUUID(t *testing.T) {
	InitStyles(defaultTestProfile())
	r, buf := testRenderer(defaultTestProfile(), ModeDefault)

	RenderProcessTable(r, atddSampleProcsWithUUID(), true, true)
	out := buf.String()

	if !strings.Contains(out, "UUID") {
		t.Fatal("AC-4: verbose+showUUID should show UUID column")
	}
	if !strings.Contains(out, "PPID") {
		t.Fatal("AC-4: verbose mode should still show PPID column")
	}
}

// ---------------------------------------------------------------------------
// AC-4: Empty process list with showUUID
// ---------------------------------------------------------------------------

func TestATDD_28_1_AC4_EmptyProcs_ShowUUID(t *testing.T) {
	InitStyles(defaultTestProfile())
	r, buf := testRenderer(defaultTestProfile(), ModeDefault)

	RenderProcessTable(r, nil, false, true)
	out := buf.String()

	if !strings.Contains(out, "No active processes") {
		t.Fatal("AC-4: empty process list should still show 'No active processes.'")
	}
}

// ---------------------------------------------------------------------------
// AC-4: Backward compat — existing callers pass showUUID=false
// ---------------------------------------------------------------------------

func TestATDD_28_1_AC4_BackwardCompat_DefaultOutput(t *testing.T) {
	InitStyles(defaultTestProfile())
	r, buf := testRenderer(defaultTestProfile(), ModeDefault)

	procs := atddSampleProcsWithUUID()
	RenderProcessTable(r, procs, false, false)
	out := buf.String()

	// Standard columns should still be present
	if !strings.Contains(out, "PID") {
		t.Fatal("AC-4: backward compat — PID column missing")
	}
	if !strings.Contains(out, "STATE") {
		t.Fatal("AC-4: backward compat — STATE column missing")
	}
	if !strings.Contains(out, "running") {
		t.Fatal("AC-4: backward compat — should show 'running' state")
	}
}

// Suppress unused import warning for bytes
var _ = bytes.Compare
