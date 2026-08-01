package kernel

import (
	"testing"
	"time"

	"github.com/rnixai/rnix/agents"
	"github.com/rnixai/rnix/drivers/llm"
)

// Story 69.3 AC5 — CompactTimeout config entry: opts > agent manifest > 30s.
//
// resolveCompactTimeout mirrors the spawn.go priority block; these tests drive
// the real spawn path where possible and the effective getter everywhere, so a
// regression in either half shows up.

func TestCompactTimeout_DefaultWhenUnset(t *testing.T) {
	proc := NewProcess(0, "no compact timeout configured", nil)
	if got := proc.effectiveCompactTimeout(); got != DefaultCompactTimeout {
		t.Errorf("effectiveCompactTimeout() = %v, want %v", got, DefaultCompactTimeout)
	}
	// Story 71.3 AC4 / F5 — the 69.3 ruling "the default is deliberately
	// unchanged" is OVERRULED by post-69.1/69.4 data (cache warm yet 308/342 =
	// 90.1% of compactions still saturated the 30s ceiling). The production
	// default is now derived (driverTimeout × compactTimeoutMultiplier);
	// DefaultCompactTimeout survives as the FLOOR for when every lookup misses,
	// and that floor value stays pinned.
	if DefaultCompactTimeout != 30*time.Second {
		t.Errorf("DefaultCompactTimeout floor = %v, want 30s (floor pinned; production default is derived)", DefaultCompactTimeout)
	}
	// Derivation relationship: driver family default × 4 = 20 minutes.
	if want := llm.DefaultTimeout * compactTimeoutMultiplier; want != 20*time.Minute {
		t.Errorf("derived default = %v, want 20m (llm.DefaultTimeout %v × %d)", want, llm.DefaultTimeout, compactTimeoutMultiplier)
	}
}

// TestCompactTimeout_ZeroFallsBackToDefault pins the deliberate asymmetry with
// StepTimeout: 0 means "default", never "disabled". Flipping this would hand
// gocontext.WithTimeout a zero deadline and permanently break compaction.
// Story 71.3: "default" at this getter is the FLOOR (DefaultCompactTimeout);
// the production default is derived upstream by resolveCompactTimeout, which
// this bare-process path never reaches — so the getter must still return the
// floor for a zero field.
func TestCompactTimeout_ZeroFallsBackToDefault(t *testing.T) {
	proc := NewProcess(0, "explicit zero", nil)
	proc.CompactTimeout = 0
	if got := proc.effectiveCompactTimeout(); got != DefaultCompactTimeout {
		t.Errorf("CompactTimeout=0 → %v, want %v (0 must mean default, not disabled)", got, DefaultCompactTimeout)
	}
}

func TestCompactTimeout_ManifestApplied(t *testing.T) {
	k, _, _, _ := setupCompactKernel(t, 32)

	agent := &agents.AgentInfo{Manifest: agents.AgentManifest{
		Name:           "slow-provider",
		CompactTimeout: "60s",
	}}

	proc := NewProcess(0, "manifest compact timeout", nil)
	applyCompactTimeout(proc, agent, SpawnOpts{})
	_ = k

	if got := proc.effectiveCompactTimeout(); got != 60*time.Second {
		t.Errorf("effectiveCompactTimeout() = %v, want 60s (manifest compact_timeout)", got)
	}
}

func TestCompactTimeout_OptsOverridesManifest(t *testing.T) {
	agent := &agents.AgentInfo{Manifest: agents.AgentManifest{
		Name:           "slow-provider",
		CompactTimeout: "60s",
	}}

	proc := NewProcess(0, "opts wins", nil)
	applyCompactTimeout(proc, agent, SpawnOpts{CompactTimeout: 90 * time.Second})

	if got := proc.effectiveCompactTimeout(); got != 90*time.Second {
		t.Errorf("effectiveCompactTimeout() = %v, want 90s (opts must beat manifest)", got)
	}
}

func TestCompactTimeout_InvalidManifestStringFallsBackToDefault(t *testing.T) {
	agent := &agents.AgentInfo{Manifest: agents.AgentManifest{
		Name:           "typo",
		CompactTimeout: "sixty seconds",
	}}

	proc := NewProcess(0, "invalid duration", nil)
	applyCompactTimeout(proc, agent, SpawnOpts{})

	if got := proc.effectiveCompactTimeout(); got != DefaultCompactTimeout {
		t.Errorf("effectiveCompactTimeout() = %v, want %v (invalid duration is ignored)", got, DefaultCompactTimeout)
	}
}

// TestCompactTimeout_ManifestZeroIsNoop: a manifest "0" parses fine but must not
// land in the field, or effectiveCompactTimeout would still return 30s while
// the recorded intent looked like "disabled". Asserting the observable result.
func TestCompactTimeout_ManifestZeroIsNoop(t *testing.T) {
	agent := &agents.AgentInfo{Manifest: agents.AgentManifest{
		Name:           "zero",
		CompactTimeout: "0",
	}}

	proc := NewProcess(0, "manifest zero", nil)
	applyCompactTimeout(proc, agent, SpawnOpts{})

	if got := proc.effectiveCompactTimeout(); got != DefaultCompactTimeout {
		t.Errorf("effectiveCompactTimeout() = %v, want %v", got, DefaultCompactTimeout)
	}
}

func TestCompactTimeout_NoAgentUsesDefault(t *testing.T) {
	proc := NewProcess(0, "bare spawn", nil)
	applyCompactTimeout(proc, nil, SpawnOpts{})

	if got := proc.effectiveCompactTimeout(); got != DefaultCompactTimeout {
		t.Errorf("effectiveCompactTimeout() = %v, want %v", got, DefaultCompactTimeout)
	}
}
