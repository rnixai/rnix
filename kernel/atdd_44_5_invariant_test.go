package kernel

import (
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// ATDD 44.5 — Process state × field invariant matrix.
//
// AC6 specifies a tuple invariant on persisted ProcInfo snapshots:
//
//   | State     | SuspendReason     | ExitReason          | ResumedFromStep |
//   | --------- | ----------------- | ------------------- | --------------- |
//   | Created   | ""                | ""                  | 0               |
//   | Running   | ""                | ""                  | 0 or N          |
//   | Suspended | non-empty         | "" or "suspended:…" | 0 or N          |
//   | Zombie    | ""                | non-empty           | preserved       |
//   | Dead      | "" (load-bearing) | non-empty           | 0 or N          |
//
// The load-bearing cell is Dead × SuspendReason — Story 44.5's whole reason
// for existing is that the EchoMatrix repro produced a Dead snapshot carrying
// suspend_reason="user_paused" alongside exit_reason="llm write failed". AC6
// pins this invariant in code so a future race regression surfaces here
// instead of in a user's terminal.
//
// RED-phase shape: all 5 tests below reference the unexported function
// kernel.ValidateProcInfoInvariant which Story 44.5 Task 3.2 introduces.
// Without that function the entire file fails to compile, which is the
// strongest possible RED signal — `go vet ./kernel/...` will report
// "undefined: ValidateProcInfoInvariant". Once the helper lands the failure
// shifts to behavioural RED until the implementation captures every row of
// the matrix.
// =============================================================================

// procInfoCase bundles a fixture ProcInfo with the expectation for the
// invariant verdict. wantErrSubstring being empty means the case is expected
// to pass invariant validation (no error returned).
type procInfoCase struct {
	name              string
	info              vfs.ProcInfo
	wantErrSubstring string // empty = expect nil error; non-empty = expect error containing this substring
}

// runInvariantCases evaluates each case against ValidateProcInfoInvariant and
// reports per-case failures so a single broken row in the matrix does not
// mask others (table-driven Go idiom).
func runInvariantCases(t *testing.T, cases []procInfoCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateProcInfoInvariant(&tc.info)
			if tc.wantErrSubstring == "" {
				if err != nil {
					t.Errorf("ValidateProcInfoInvariant returned %v, want nil "+
						"(case `%s` should satisfy the invariant matrix)", err, tc.name)
				}
				return
			}
			if err == nil {
				t.Errorf("ValidateProcInfoInvariant returned nil, want error containing %q "+
					"(case `%s` violates the invariant matrix and must be flagged)",
					tc.wantErrSubstring, tc.name)
				return
			}
			if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.wantErrSubstring)) {
				t.Errorf("ValidateProcInfoInvariant error = %v, want substring %q "+
					"(case `%s`)", err, tc.wantErrSubstring, tc.name)
			}
		})
	}
}

// TestATDD_44_5_040_ValidateProcInfoInvariant_AllStatesAllowedCases
//
// Happy-path matrix: every State has a representative ProcInfo combination
// that the invariant must accept. Each subtest documents the legitimate field
// shape for that lifecycle stage.
func TestATDD_44_5_040_ValidateProcInfoInvariant_AllStatesAllowedCases(t *testing.T) {
	runInvariantCases(t, []procInfoCase{
		{
			name: "created-fresh",
			info: vfs.ProcInfo{
				PID:   1,
				State: types.StateCreated,
			},
		},
		{
			name: "running-fresh",
			info: vfs.ProcInfo{
				PID:   2,
				State: types.StateRunning,
			},
		},
		{
			name: "running-after-resume",
			info: vfs.ProcInfo{
				PID:             3,
				State:           types.StateRunning,
				ResumedFromStep: 5,
			},
		},
		{
			name: "suspended-user-paused",
			info: vfs.ProcInfo{
				PID:           4,
				State:         types.StateSuspended,
				SuspendReason: "user_paused",
			},
		},
		{
			name: "suspended-with-exit-string",
			info: vfs.ProcInfo{
				PID:           5,
				State:         types.StateSuspended,
				SuspendReason: "user_paused",
				ExitReason:    "suspended: user_paused",
			},
		},
		{
			name: "suspended-after-resume-cycle",
			info: vfs.ProcInfo{
				PID:             6,
				State:           types.StateSuspended,
				SuspendReason:   "user_paused",
				ResumedFromStep: 5,
			},
		},
		{
			name: "zombie-complete",
			info: vfs.ProcInfo{
				PID:        7,
				State:      types.StateZombie,
				ExitReason: "complete",
			},
		},
		{
			name: "dead-clean-complete",
			info: vfs.ProcInfo{
				PID:        8,
				State:      types.StateDead,
				ExitReason: "complete",
				DeadAt:     time.Now(),
			},
		},
		{
			name: "dead-after-real-resume",
			info: vfs.ProcInfo{
				PID:             9,
				State:           types.StateDead,
				ExitReason:      "complete",
				ResumedFromStep: 5,
				DeadAt:          time.Now(),
			},
		},
	})
}

// TestATDD_44_5_041_ValidateProcInfoInvariant_DeadWithSuspendReason_Violates
//
// The flagship reverse case. Reproduces the EchoMatrix PID 2/4/6 disk state
// exactly: Dead + suspend_reason="user_paused" + exit_reason="llm write
// failed" + resumed_from_step preset by the deleted pre-fill block. The
// invariant must reject this tuple with a message that mentions the offending
// state/field so future regressions are diagnosable.
//
// RED-phase failure: file does not compile until
// kernel.ValidateProcInfoInvariant exists; once it does, this case fails until
// the helper implements the "Dead → SuspendReason must be empty" rule.
func TestATDD_44_5_041_ValidateProcInfoInvariant_DeadWithSuspendReason_Violates(t *testing.T) {
	runInvariantCases(t, []procInfoCase{
		{
			name: "echo-matrix-pid-2-style",
			info: vfs.ProcInfo{
				PID:             2,
				State:           types.StateDead,
				SuspendReason:   "user_paused",
				ExitReason:      "llm write failed",
				ResumedFromStep: 6,
				DeadAt:          time.Now(),
			},
			wantErrSubstring: "suspendreason",
		},
		{
			name: "dead-with-clean-exit-but-stale-suspend-reason",
			info: vfs.ProcInfo{
				PID:           10,
				State:         types.StateDead,
				SuspendReason: "user_paused",
				ExitReason:    "complete",
				DeadAt:        time.Now(),
			},
			wantErrSubstring: "suspendreason",
		},
	})
}

// TestATDD_44_5_042_ValidateProcInfoInvariant_CreatedWithExitReason_Violates
//
// A Created process by definition has not started, so it cannot have an
// ExitReason. This case catches early-pipeline state corruption where a
// failure during Spawn was attributed to the proc before it ran.
func TestATDD_44_5_042_ValidateProcInfoInvariant_CreatedWithExitReason_Violates(t *testing.T) {
	runInvariantCases(t, []procInfoCase{
		{
			name: "created-with-early-error",
			info: vfs.ProcInfo{
				PID:        11,
				State:      types.StateCreated,
				ExitReason: "spawn_failed",
			},
			wantErrSubstring: "exitreason",
		},
		{
			name: "created-with-suspend-reason",
			info: vfs.ProcInfo{
				PID:           12,
				State:         types.StateCreated,
				SuspendReason: "user_paused",
			},
			wantErrSubstring: "suspendreason",
		},
	})
}

// TestATDD_44_5_043_ValidateProcInfoInvariant_RunningWithSuspendReason_Violates
//
// Running and Suspended are mutually exclusive lifecycle stages — a Running
// process must not advertise a SuspendReason. This case guards against a race
// where SetSuspendReason was called but the subsequent state transition was
// rolled back without clearing the field.
func TestATDD_44_5_043_ValidateProcInfoInvariant_RunningWithSuspendReason_Violates(t *testing.T) {
	runInvariantCases(t, []procInfoCase{
		{
			name: "running-with-stale-suspend-reason",
			info: vfs.ProcInfo{
				PID:           13,
				State:         types.StateRunning,
				SuspendReason: "budget_exhausted",
			},
			wantErrSubstring: "suspendreason",
		},
		{
			name: "running-with-exit-reason",
			info: vfs.ProcInfo{
				PID:        14,
				State:      types.StateRunning,
				ExitReason: "complete",
			},
			wantErrSubstring: "exitreason",
		},
	})
}

// TestATDD_44_5_044_ValidateProcInfoInvariant_SuspendedWithEmptyReason_Violates
//
// Suspended state is meaningless without a reason — every legitimate suspend
// site (suspend.go:20, signal.go) writes one. A Suspended snapshot with an
// empty SuspendReason indicates either a buggy write path or a snapshot
// captured mid-race.
func TestATDD_44_5_044_ValidateProcInfoInvariant_SuspendedWithEmptyReason_Violates(t *testing.T) {
	runInvariantCases(t, []procInfoCase{
		{
			name: "suspended-empty-reason",
			info: vfs.ProcInfo{
				PID:   15,
				State: types.StateSuspended,
			},
			wantErrSubstring: "suspendreason",
		},
	})
}
