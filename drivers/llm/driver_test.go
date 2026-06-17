package llm

import (
	"context"
	"os/exec"
	"testing"
)

// TestConfigureCommandDir covers the I/O matrix from
// spec-fix-cli-driver-cwd-projectdir: only an absolute projectDir locks
// cmd.Dir; empty / relative / nil are left untouched so the child inherits the
// parent cwd (prior behavior) rather than resolving against a stale daemon cwd.
func TestConfigureCommandDir(t *testing.T) {
	t.Run("absolute path sets cmd.Dir", func(t *testing.T) {
		cmd := exec.CommandContext(context.Background(), "echo")
		configureCommandDir(cmd, "/a/apex")
		if cmd.Dir != "/a/apex" {
			t.Fatalf("expected cmd.Dir=/a/apex, got %q", cmd.Dir)
		}
	})

	t.Run("empty path leaves cmd.Dir unset", func(t *testing.T) {
		cmd := exec.CommandContext(context.Background(), "echo")
		configureCommandDir(cmd, "")
		if cmd.Dir != "" {
			t.Fatalf("expected cmd.Dir unset, got %q", cmd.Dir)
		}
	})

	t.Run("relative path is rejected (leaves cmd.Dir unset)", func(t *testing.T) {
		cmd := exec.CommandContext(context.Background(), "echo")
		configureCommandDir(cmd, "../apex")
		if cmd.Dir != "" {
			t.Fatalf("expected relative path rejected (cmd.Dir unset), got %q", cmd.Dir)
		}
	})

	t.Run("nil cmd does not panic", func(t *testing.T) {
		configureCommandDir(nil, "/a/apex")
	})
}
