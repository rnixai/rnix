package kernel

import (
	"fmt"
	"strings"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// ValidateProcInfoInvariant enforces the (State, SuspendReason, ExitReason,
// ResumedFromStep) tuple invariant matrix defined in Story 44.5 AC6. The
// matrix codifies which field combinations are meaningful at each lifecycle
// stage; violations indicate a race or stale field that would surface to the
// dashboard as contradictory information. Callers can use this on persisted
// ProcInfo snapshots (proc-info.json) or in-memory ProcInfo values returned by
// GetProcInfo.
//
// Allowed combinations (see Story 44.5 AC6):
//
//	| State     | SuspendReason     | ExitReason           | ResumedFromStep |
//	| --------- | ----------------- | -------------------- | --------------- |
//	| Created   | ""                | ""                   | 0               |
//	| Running   | ""                | ""                   | 0 or N          |
//	| Suspended | non-empty         | "" or "suspended: …" | 0 or N          |
//	| Zombie    | ""                | non-empty            | preserved       |
//	| Dead      | "" (load-bearing) | non-empty            | 0 or N          |
//
// The load-bearing Dead × SuspendReason cell is exactly the EchoMatrix
// PID 2/4/6 contradiction Story 44.5 fixes — finishProcess clears
// SuspendReason on failed exits so this invariant holds.
func ValidateProcInfoInvariant(info *vfs.ProcInfo) error {
	if info == nil {
		return fmt.Errorf("ValidateProcInfoInvariant: nil info")
	}

	switch info.State {
	case types.StateCreated:
		if info.SuspendReason != "" {
			return fmt.Errorf("state=Created must have empty SuspendReason, got %q", info.SuspendReason)
		}
		if info.ExitReason != "" {
			return fmt.Errorf("state=Created must have empty ExitReason, got %q", info.ExitReason)
		}
		if info.ResumedFromStep != 0 {
			return fmt.Errorf("state=Created must have ResumedFromStep=0, got %d", info.ResumedFromStep)
		}
	case types.StateRunning:
		if info.SuspendReason != "" {
			return fmt.Errorf("state=Running must have empty SuspendReason, got %q "+
				"(Suspended is a mutually exclusive lifecycle stage)", info.SuspendReason)
		}
		if info.ExitReason != "" {
			return fmt.Errorf("state=Running must have empty ExitReason, got %q "+
				"(a live process has not exited)", info.ExitReason)
		}
	case types.StateSuspended:
		if info.SuspendReason == "" {
			return fmt.Errorf("state=Suspended must have non-empty SuspendReason " +
				"(every legitimate suspend path stamps a reason)")
		}
		if info.ExitReason != "" && !strings.HasPrefix(info.ExitReason, "suspended") {
			return fmt.Errorf("state=Suspended ExitReason=%q must be empty or start with \"suspended\"",
				info.ExitReason)
		}
	case types.StateZombie:
		if info.ExitReason == "" {
			return fmt.Errorf("state=Zombie must have non-empty ExitReason " +
				"(Terminate records exit before transition)")
		}
	case types.StateDead:
		if info.SuspendReason != "" {
			return fmt.Errorf("state=Dead must have empty SuspendReason, got %q "+
				"(Story 44.5 AC6 — finishProcess clears stale SuspendReason on failed exit; "+
				"a Dead snapshot with SuspendReason set is the EchoMatrix contradiction)",
				info.SuspendReason)
		}
		if info.ExitReason == "" {
			return fmt.Errorf("state=Dead must have non-empty ExitReason " +
				"(a reaped process has exited)")
		}
	default:
		return fmt.Errorf("unknown state %v", info.State)
	}
	return nil
}
