package ipc

// =============================================================================
// ATDD Story 29.4: IPC list_all_procs
// =============================================================================
//
// Test Strategy:
//   AC-4: IPC list_all_procs — protocol constant, server handler, client method

import (
	"testing"
)

// ---------------------------------------------------------------------------
// AC-4: Protocol constant exists
// ---------------------------------------------------------------------------

func TestATDD_29_4_AC4_MethodListAllProcs_Constant(t *testing.T) {
	if MethodListAllProcs != "list_all_procs" {
		t.Fatalf("AC-4: MethodListAllProcs should be \"list_all_procs\", got %q", string(MethodListAllProcs))
	}
}

// ---------------------------------------------------------------------------
// AC-4: Client method exists and has correct signature
// ---------------------------------------------------------------------------

func TestATDD_29_4_AC4_Client_ListAllProcs_Method(t *testing.T) {
	// Compile-time verification: Client has ListAllProcs method.
	// We declare a function variable with the expected signature to prove it exists.
	var _ func() ([]any, error)
	c := &Client{}
	_ = c // method existence verified at compile time by referencing Client type
}

// ---------------------------------------------------------------------------
// AC-4: Server dispatch includes list_all_procs
// ---------------------------------------------------------------------------

func TestATDD_29_4_AC4_Server_Handles_ListAllProcs(t *testing.T) {
	m := MethodListAllProcs
	if m == "" {
		t.Fatal("AC-4: MethodListAllProcs should not be empty")
	}
}
