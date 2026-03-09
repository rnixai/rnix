package main

import "testing"

func TestIntentCmd_Registered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "intent" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected 'intent' subcommand to be registered")
	}
}

func TestIntentStatusCmd_Registered(t *testing.T) {
	found := false
	for _, cmd := range intentCmd.Commands() {
		if cmd.Name() == "status" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected 'status' subcommand to be registered under 'intent'")
	}
}

func TestIntentStatusCmd_UsageAndDescription(t *testing.T) {
	if intentStatusCmd.Use != "status [intent-id]" {
		t.Fatalf("expected Use=%q, got %q", "status [intent-id]", intentStatusCmd.Use)
	}
	if intentStatusCmd.Short == "" {
		t.Fatal("expected non-empty Short description")
	}
}
