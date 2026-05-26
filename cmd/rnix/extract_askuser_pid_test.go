package main

import (
	"context"
	"strings"
	"testing"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
)

// extractAskUserPID guards the ttyDriver.AskUserQuestion path: when the ctx
// passed into /dev/tty Write lacks kernel.PIDFromContext, it must surface a
// clear LLM-readable error instead of letting the request fall through to
// ipc/server_callback.go's PID=0 branch (which historically leaked the raw
// "cannot send to PID 0 (no process context)" string to the model).

func TestExtractAskUserPID_RejectsCtxWithoutPID(t *testing.T) {
	pid, err := extractAskUserPID(context.Background())
	if err == nil {
		t.Fatalf("expected error for ctx without PID, got pid=%d", pid)
	}
	if pid != 0 {
		t.Errorf("pid = %d, want 0 on error path", pid)
	}
	msg := err.Error()
	if !strings.Contains(msg, "AskUserQuestion unavailable") {
		t.Errorf("error %q missing user-facing 'AskUserQuestion unavailable' prefix", msg)
	}
	if !strings.Contains(msg, "no process context") {
		t.Errorf("error %q missing 'no process context' diagnostic", msg)
	}
}

func TestExtractAskUserPID_ReturnsPIDWhenPresent(t *testing.T) {
	want := types.PID(42)
	ctx := kernel.ContextWithPID(context.Background(), want)
	got, err := extractAskUserPID(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("pid = %d, want %d", got, want)
	}
}
