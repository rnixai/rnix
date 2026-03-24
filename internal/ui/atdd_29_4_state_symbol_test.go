package ui

// =============================================================================
// ATDD Story 29.4: 状态符号体系
// TDD RED PHASE — Tests reference StateSymbol function not yet implemented.
//                  Compilation failure IS the red phase.
// =============================================================================
//
// Test Strategy:
//   AC-6: StateSymbol(state, result) returns correct Unicode/ASCII symbols
//         Running→●/*  Created→○/o  Done(exit0)→✓/+  Failed(exit≠0)→✕/x  Paused→⏸/=
//
// Priority: P1 (UI consistency)
// Test Level: Unit

import (
	"testing"

	"github.com/rnixai/rnix/internal/types"
)

// ---------------------------------------------------------------------------
// AC-6: Unicode symbols (default mode)
// ---------------------------------------------------------------------------

func TestATDD_29_4_AC6_StateSymbol_Running_Unicode(t *testing.T) {
	got := StateSymbol(types.StateRunning, "")
	if got != "●" {
		t.Fatalf("AC-6: Running should be ●, got %q", got)
	}
}

func TestATDD_29_4_AC6_StateSymbol_Created_Unicode(t *testing.T) {
	got := StateSymbol(types.StateCreated, "")
	if got != "○" {
		t.Fatalf("AC-6: Created should be ○, got %q", got)
	}
}

func TestATDD_29_4_AC6_StateSymbol_Dead_Success_Unicode(t *testing.T) {
	got := StateSymbol(types.StateDead, "completed: task done")
	if got != "✓" {
		t.Fatalf("AC-6: Dead+success should be ✓, got %q", got)
	}
}

func TestATDD_29_4_AC6_StateSymbol_Dead_EmptyResult_Unicode(t *testing.T) {
	// Empty result for Dead process → Failed
	got := StateSymbol(types.StateDead, "")
	if got != "✕" {
		t.Fatalf("AC-6: Dead+empty result should be ✕ (failed), got %q", got)
	}
}

func TestATDD_29_4_AC6_StateSymbol_Dead_Error_Unicode(t *testing.T) {
	got := StateSymbol(types.StateDead, "error: timeout exceeded")
	if got != "✕" {
		t.Fatalf("AC-6: Dead+error should be ✕, got %q", got)
	}
}

func TestATDD_29_4_AC6_StateSymbol_Dead_Fail_Unicode(t *testing.T) {
	got := StateSymbol(types.StateDead, "fail: bad input")
	if got != "✕" {
		t.Fatalf("AC-6: Dead+fail should be ✕, got %q", got)
	}
}

func TestATDD_29_4_AC6_StateSymbol_Dead_Timeout_Unicode(t *testing.T) {
	got := StateSymbol(types.StateDead, "timeout after 30s")
	if got != "✕" {
		t.Fatalf("AC-6: Dead+timeout should be ✕, got %q", got)
	}
}

func TestATDD_29_4_AC6_StateSymbol_Zombie_Unicode(t *testing.T) {
	got := StateSymbol(types.StateZombie, "")
	if got != "⏸" {
		t.Fatalf("AC-6: Zombie (paused) should be ⏸, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// AC-6: ASCII symbols (RNIX_ASCII=1)
// ---------------------------------------------------------------------------

func TestATDD_29_4_AC6_StateSymbol_Running_ASCII(t *testing.T) {
	t.Setenv("RNIX_ASCII", "1")
	got := StateSymbol(types.StateRunning, "")
	if got != "*" {
		t.Fatalf("AC-6: Running ASCII should be *, got %q", got)
	}
}

func TestATDD_29_4_AC6_StateSymbol_Created_ASCII(t *testing.T) {
	t.Setenv("RNIX_ASCII", "1")
	got := StateSymbol(types.StateCreated, "")
	if got != "o" {
		t.Fatalf("AC-6: Created ASCII should be o, got %q", got)
	}
}

func TestATDD_29_4_AC6_StateSymbol_Dead_Success_ASCII(t *testing.T) {
	t.Setenv("RNIX_ASCII", "1")
	got := StateSymbol(types.StateDead, "completed: all good")
	if got != "+" {
		t.Fatalf("AC-6: Dead+success ASCII should be +, got %q", got)
	}
}

func TestATDD_29_4_AC6_StateSymbol_Dead_Error_ASCII(t *testing.T) {
	t.Setenv("RNIX_ASCII", "1")
	got := StateSymbol(types.StateDead, "error: crashed")
	if got != "x" {
		t.Fatalf("AC-6: Dead+error ASCII should be x, got %q", got)
	}
}

func TestATDD_29_4_AC6_StateSymbol_Zombie_ASCII(t *testing.T) {
	t.Setenv("RNIX_ASCII", "1")
	got := StateSymbol(types.StateZombie, "")
	if got != "=" {
		t.Fatalf("AC-6: Zombie ASCII should be =, got %q", got)
	}
}
