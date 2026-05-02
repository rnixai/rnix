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

func TestApplyCmd_UpdateFlag(t *testing.T) {
	f := applyCmd.Flags().Lookup("update")
	if f == nil {
		t.Fatal("expected --update flag to be defined")
	}
	if f.Shorthand != "u" {
		t.Fatalf("expected shorthand 'u', got %q", f.Shorthand)
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

// TestRootCmd_FallbackFlags verifies the persistent CLI flags for fallback
// model/provider override are registered on the root command and visible
// to apply (and any other spawn-issuing subcommand).
func TestRootCmd_FallbackFlags(t *testing.T) {
	for _, name := range []string{"fallback-model", "fallback-provider"} {
		f := rootCmd.PersistentFlags().Lookup(name)
		if f == nil {
			t.Fatalf("expected --%s persistent flag to be defined", name)
		}
		if f.Value.Type() != "string" {
			t.Errorf("--%s expected string flag, got %s", name, f.Value.Type())
		}
	}
}
