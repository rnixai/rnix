package main

import "testing"

func TestApplyCmd_Registered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "apply" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected 'apply' subcommand to be registered")
	}
}

func TestApplyCmd_NoArgs(t *testing.T) {
	if applyCmd.Args == nil {
		t.Fatal("expected Args validator to be set")
	}
	err := applyCmd.Args(applyCmd, []string{})
	if err == nil {
		t.Fatal("expected error when no args provided")
	}
}

func TestApplyCmd_YesFlag(t *testing.T) {
	f := applyCmd.Flags().Lookup("yes")
	if f == nil {
		t.Fatal("expected --yes flag to be defined")
	}
	if f.Shorthand != "y" {
		t.Fatalf("expected shorthand 'y', got %q", f.Shorthand)
	}
}

func TestApplyCmd_UsageAndDescription(t *testing.T) {
	if applyCmd.Use != "apply <intent>" {
		t.Fatalf("expected Use=%q, got %q", "apply <intent>", applyCmd.Use)
	}
	if applyCmd.Short == "" {
		t.Fatal("expected non-empty Short description")
	}
}
