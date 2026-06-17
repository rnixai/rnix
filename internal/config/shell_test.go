package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Story 57.1 AC1: ResolveShellTimeout reads shell.command_timeout_seconds from
// config.yaml and maps it to a Duration with a lenient contract mirroring
// ResolveFeatures — a missing file/section or a non-positive value yields 0
// ("unset — let the driver fall back to its default") and never a hard error.

// writeShellConfig writes content to tmpDir/config.yaml and returns the full
// path. ResolveShellTimeout takes the file path directly (main.go passes
// filepath.Join(globalDir, "config.yaml")), so there is no .rnix nesting here.
func writeShellConfig(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	return p
}

func TestResolveShellTimeout_MissingFile(t *testing.T) {
	d, warn := ResolveShellTimeout(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if d != 0 {
		t.Errorf("missing file: got %v, want 0", d)
	}
	if warn != "" {
		t.Errorf("missing file: got warning %q, want none", warn)
	}
}

func TestResolveShellTimeout_PositiveValue(t *testing.T) {
	p := writeShellConfig(t, "shell:\n  command_timeout_seconds: 90\n")
	d, warn := ResolveShellTimeout(p)
	if d != 90*time.Second {
		t.Errorf("got %v, want 90s", d)
	}
	if warn != "" {
		t.Errorf("got warning %q, want none", warn)
	}
}

func TestResolveShellTimeout_MissingSection(t *testing.T) {
	// File exists but has no shell section → treated as unset.
	p := writeShellConfig(t, "features:\n  profile: full\n")
	d, warn := ResolveShellTimeout(p)
	if d != 0 || warn != "" {
		t.Errorf("missing section: got (%v, %q), want (0, \"\")", d, warn)
	}
}

func TestResolveShellTimeout_NonPositive(t *testing.T) {
	for _, v := range []string{"0", "-5"} {
		p := writeShellConfig(t, "shell:\n  command_timeout_seconds: "+v+"\n")
		d, warn := ResolveShellTimeout(p)
		if d != 0 || warn != "" {
			t.Errorf("value %s: got (%v, %q), want (0, \"\")", v, d, warn)
		}
	}
}

func TestResolveShellTimeout_ParseError(t *testing.T) {
	// Malformed YAML must yield 0 + a non-empty warning, never a hard failure
	// (lenient contract). The int field given a non-numeric scalar forces a
	// type/parse error from the unmarshaler.
	p := writeShellConfig(t, "shell:\n  command_timeout_seconds: not-a-number\n")
	d, warn := ResolveShellTimeout(p)
	if d != 0 {
		t.Errorf("parse error: got %v, want 0", d)
	}
	if warn == "" {
		t.Error("parse error: expected non-empty warning")
	}
}

func TestResolveShellTimeout_LargeValueNotClamped(t *testing.T) {
	// The driver-default (config) timeout is operator-trusted and is NOT clamped;
	// the 600s hard ceiling applies only to per-call overrides (AC3, driver
	// layer). Config layer must pass large values through verbatim.
	p := writeShellConfig(t, "shell:\n  command_timeout_seconds: 9999\n")
	d, _ := ResolveShellTimeout(p)
	if d != 9999*time.Second {
		t.Errorf("got %v, want 9999s (config layer must not clamp)", d)
	}
}
