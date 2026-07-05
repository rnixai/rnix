package kernel

import (
	"slices"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/skills"
)

// Regression tests for the specialize privilege-escalation guard (deferred-work:
// code review of spawn-privilege-escalation-fix, 2026-06-01): a permission-
// constrained process must not widen proc.AllowedDevices / proc.AllowedTools by
// loading a skill — the child ⊆ parent invariant established at spawn has to
// survive specialize. Only-narrow semantics: the skill body still loads
// (knowledge), but declared tools/devices outside the whitelists are withheld.
// Unconstrained processes (both lists empty) keep the historical behavior of
// adopting the skill's toolset verbatim (covered by TestATDD_54_5_120).

// specializeSkillLoader installs a loader returning a skill whose frontmatter
// declares raw. Reuses the newNormalizeKernel registry (/dev/fs 5 tools +
// /dev/shell Bash + /dev/intent) so normalizeDeclaredAllowedTools resolves
// semantic tool names to device roots.
func specializeSkillLoader(k *KernelImpl, raw string) {
	k.SetSkillLoader(func(name string) (*skills.SkillInfo, error) {
		return &skills.SkillInfo{
			Manifest: skills.SkillManifest{Name: name, AllowedToolsRaw: raw},
			Body:     "skill body for " + name,
			Dir:      "/tmp/skills/" + name,
		}, nil
	})
}

func runSpecialize(t *testing.T, k *KernelImpl, proc *Process, skillName string) bool {
	t.Helper()
	resp := llmResponse{Content: "specializing"}
	tc := llmToolCall{ID: "sp-1", Name: "Skill", Input: map[string]any{"skill": skillName}}
	var consec errFingerprintCounter
	prompt := &rnixctx.PromptResult{}
	return k.executeMetaAction(proc, tc, toolMapping{Type: "meta", Action: ActionSpecialize}, 1, time.Now(), &consec, map[string]bool{}, prompt, "", &resp)
}

// A process narrowed to /dev/fs + [Read] loads a skill declaring Bash: the
// skill must load, but neither Bash nor /dev/shell may be granted.
func TestSpecialize_ConstrainedProc_NoPrivilegeEscalation(t *testing.T) {
	k := newNormalizeKernel(t)
	specializeSkillLoader(k, "Bash")

	cid, _ := k.ctxMgr.CtxAlloc(16)
	proc := NewProcess(0, "specialize-escalation", nil)
	proc.CtxID = cid
	proc.AllowedDevices = []string{"/dev/fs"}
	proc.AllowedTools = []string{"Read"}
	proc.toolMap = map[string]toolMapping{"Skill": {Type: "meta", Action: ActionSpecialize}}

	if !runSpecialize(t, k, proc, "shell-skill") {
		t.Fatal("expected true from specialize")
	}

	proc.mu.Lock()
	gotTools := slices.Clone(proc.AllowedTools)
	gotDevs := slices.Clone(proc.AllowedDevices)
	loaded := slices.Contains(proc.Skills, "shell-skill")
	proc.mu.Unlock()

	if !loaded {
		t.Fatal("skill should still load (knowledge) — only permission widening is blocked")
	}
	if !slices.Equal(gotTools, []string{"Read"}) {
		t.Errorf("AllowedTools widened by specialize: got %v, want [Read]", gotTools)
	}
	if !slices.Equal(gotDevs, []string{"/dev/fs"}) {
		t.Errorf("AllowedDevices widened by specialize: got %v, want [/dev/fs]", gotDevs)
	}
}

// A device-only constrained process (AllowedDevices set, AllowedTools empty)
// loads a skill declaring Bash. Appending Bash to the empty AllowedTools would
// activate the first-match tool-name gate in executeVFSTool and grant Bash
// despite the device whitelist — the guard must intersect both lists as a pair.
func TestSpecialize_DeviceOnlyConstraint_NoToolWhitelistBypass(t *testing.T) {
	k := newNormalizeKernel(t)
	specializeSkillLoader(k, "Bash")

	cid, _ := k.ctxMgr.CtxAlloc(16)
	proc := NewProcess(0, "specialize-device-only", nil)
	proc.CtxID = cid
	proc.AllowedDevices = []string{"/dev/fs"}
	proc.toolMap = map[string]toolMapping{"Skill": {Type: "meta", Action: ActionSpecialize}}

	if !runSpecialize(t, k, proc, "shell-skill") {
		t.Fatal("expected true from specialize")
	}

	proc.mu.Lock()
	gotTools := slices.Clone(proc.AllowedTools)
	gotDevs := slices.Clone(proc.AllowedDevices)
	proc.mu.Unlock()

	if len(gotTools) != 0 {
		t.Errorf("AllowedTools must stay empty (device-gated proc): got %v", gotTools)
	}
	if !slices.Equal(gotDevs, []string{"/dev/fs"}) {
		t.Errorf("AllowedDevices widened by specialize: got %v, want [/dev/fs]", gotDevs)
	}
}

// Context-full rollback on a constrained process whose whitelist overlaps the
// skill's declarations must not strip pre-existing permissions (deferred-work
// 54-1: rollback over-deletion). The rollback removes exactly what the append
// contributed — here nothing, since the intersection withheld every declared
// value the process did not already hold.
func TestSpecialize_RollbackPreservesPreexistingPermissions(t *testing.T) {
	k := newNormalizeKernel(t)
	specializeSkillLoader(k, "/dev/fs /dev/shell")

	// 4 slots, fill 3, then the assistant message uses the 4th → AvailableSlots
	// < 2 forces the context-full rollback (mirrors TestSpecialize_AppendMessageFail_Rollback).
	cid, _ := k.ctxMgr.CtxAlloc(4)
	fillContext(t, k.ctxMgr, cid, 3)

	proc := NewProcess(0, "specialize-rollback-preexisting", nil)
	proc.CtxID = cid
	proc.AllowedDevices = []string{"/dev/fs"}
	proc.AllowedTools = []string{"Read"}
	proc.toolMap = map[string]toolMapping{"Skill": {Type: "meta", Action: ActionSpecialize}}
	if err := proc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := k.ctxMgr.AppendMessage(cid, rnixctx.RoleAssistant, "I will specialize"); err != nil {
		t.Fatalf("pre-fill: %v", err)
	}

	if !runSpecialize(t, k, proc, "fs-skill") {
		t.Fatal("expected true from specialize rollback")
	}

	proc.mu.Lock()
	gotTools := slices.Clone(proc.AllowedTools)
	gotDevs := slices.Clone(proc.AllowedDevices)
	stillLoaded := slices.Contains(proc.Skills, "fs-skill")
	proc.mu.Unlock()

	if stillLoaded {
		t.Error("skill should be rolled back (context full)")
	}
	if !slices.Equal(gotDevs, []string{"/dev/fs"}) {
		t.Errorf("rollback stripped pre-existing AllowedDevices: got %v, want [/dev/fs]", gotDevs)
	}
	if !slices.Equal(gotTools, []string{"Read"}) {
		t.Errorf("rollback stripped pre-existing AllowedTools: got %v, want [Read]", gotTools)
	}
}
