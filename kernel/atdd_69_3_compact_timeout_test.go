package kernel

import (
	"testing"
	"time"

	"github.com/rnixai/rnix/agents"
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
	if DefaultCompactTimeout != 30*time.Second {
		t.Errorf("DefaultCompactTimeout = %v, want 30s (AC5: the default is deliberately unchanged)", DefaultCompactTimeout)
	}
}

// TestCompactTimeout_ZeroFallsBackToDefault pins the deliberate asymmetry with
// StepTimeout: 0 means "default", never "disabled". Flipping this would hand
// gocontext.WithTimeout a zero deadline and permanently break compaction.
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
