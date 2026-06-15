package kernel

import (
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// Story 55.2 — ReasoningEffort must survive procInfoDisk persistence so a
// daemon-restart / resume-from-disk process still reports its effort to the
// dashboard and proc-info.json. Value is passed through verbatim (gemini
// uppercase is NOT normalized — same passthrough rule as 55.1).

func TestProcInfoDisk_ReasoningEffortRoundTrip(t *testing.T) {
	original := vfs.ProcInfo{
		PID:             7,
		UUID:            "effort-disk-uuid",
		State:           types.StateRunning,
		Intent:          "disk roundtrip",
		Provider:        "gemini",
		Model:           "gemini-3-pro",
		ReasoningEffort: "HIGH", // gemini uppercase — passthrough, not normalized
		CreatedAt:       time.Now(),
		CtxID:           3,
	}
	d := procInfoToDisk(original)
	if d.ReasoningEffort != "HIGH" {
		t.Errorf("disk.ReasoningEffort = %q, want 'HIGH'", d.ReasoningEffort)
	}
	back := procInfoFromDisk(d)
	if back.ReasoningEffort != "HIGH" {
		t.Errorf("roundtrip ReasoningEffort = %q, want 'HIGH'", back.ReasoningEffort)
	}
}

func TestProcInfoDisk_ReasoningEffort_EmptyOmitted(t *testing.T) {
	// Legacy / non-effort processes persist no reasoning_effort key (omitempty)
	// and load back as empty without error.
	d := procInfoToDisk(vfs.ProcInfo{PID: 1, UUID: "no-effort", State: types.StateRunning, CreatedAt: time.Now()})
	if d.ReasoningEffort != "" {
		t.Errorf("disk.ReasoningEffort = %q, want empty", d.ReasoningEffort)
	}
	if back := procInfoFromDisk(d); back.ReasoningEffort != "" {
		t.Errorf("roundtrip ReasoningEffort = %q, want empty", back.ReasoningEffort)
	}
}

// TestGetProcInfo_IncludesReasoningEffort is a regression guard for the Story
// 55.2 review finding: GetProcInfo — the ProcInfo snapshot source for reap
// persistence (proc-info.json + procHistory) and /proc/{pid}/status — must
// mirror Process.ReasoningEffort onto the returned ProcInfo. The disk/wire
// roundtrip tests take a ProcInfo directly, so they cannot catch a missed
// assignment in the Process→ProcInfo construction (which is exactly how the
// original miss slipped past: ListProcs got the field, GetProcInfo did not).
func TestGetProcInfo_IncludesReasoningEffort(t *testing.T) {
	k, _ := setupResumeKernel(t)
	proc := NewProcess(0, "effort snapshot regression", nil)
	proc.ReasoningEffort = "high"
	_ = proc.Start()
	k.AddProcess(proc)

	info, err := k.GetProcInfo(proc.PID)
	if err != nil {
		t.Fatalf("GetProcInfo: %v", err)
	}
	if info.ReasoningEffort != "high" {
		t.Errorf("GetProcInfo ReasoningEffort = %q, want 'high'", info.ReasoningEffort)
	}
}
