package kernel

import (
	"errors"
	"fmt"
	"testing"

	"github.com/usecrux/crux/internal/types"
)

func TestSyscallError_Error(t *testing.T) {
	inner := fmt.Errorf("connection refused")
	e := NewSyscallError("open", 42, "/dev/llm", inner, types.ErrDriver)
	want := "[DRIVER] PID 42 open: /dev/llm (connection refused)"
	if got := e.Error(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSyscallError_Unwrap(t *testing.T) {
	inner := fmt.Errorf("not found")
	e := NewSyscallError("read", 1, "/dev/fs", inner, types.ErrNotFound)
	if !errors.Is(e, inner) {
		t.Fatal("errors.Is should match inner error")
	}
}

func TestSyscallError_ErrorsAs(t *testing.T) {
	inner := fmt.Errorf("timeout")
	e := NewSyscallError("write", 7, "/dev/shell", inner, types.ErrTimeout)
	wrapped := fmt.Errorf("wrapped: %w", e)

	var se *SyscallError
	if !errors.As(wrapped, &se) {
		t.Fatal("errors.As should find SyscallError")
	}
	if se.Code != types.ErrTimeout {
		t.Fatalf("got code %q, want %q", se.Code, types.ErrTimeout)
	}
	if se.PID != 7 {
		t.Fatalf("got PID %d, want 7", se.PID)
	}
}

func TestNewSyscallError_Fields(t *testing.T) {
	inner := fmt.Errorf("denied")
	e := NewSyscallError("mount", 100, "/dev/fs", inner, types.ErrPermission)
	if e.Syscall != "mount" {
		t.Fatalf("Syscall: got %q, want \"mount\"", e.Syscall)
	}
	if e.PID != 100 {
		t.Fatalf("PID: got %d, want 100", e.PID)
	}
	if e.Device != "/dev/fs" {
		t.Fatalf("Device: got %q, want \"/dev/fs\"", e.Device)
	}
	if e.Code != types.ErrPermission {
		t.Fatalf("Code: got %q, want %q", e.Code, types.ErrPermission)
	}
}

func TestSyscallError_AllErrCodes(t *testing.T) {
	codes := []types.ErrCode{
		types.ErrTimeout,
		types.ErrNotFound,
		types.ErrPermission,
		types.ErrInternal,
		types.ErrDriver,
	}
	for _, code := range codes {
		e := NewSyscallError("test", 1, "dev", fmt.Errorf("err"), code)
		if e.Code != code {
			t.Errorf("expected code %q, got %q", code, e.Code)
		}
	}
}
