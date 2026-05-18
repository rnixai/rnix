package kernel

import (
	"testing"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// ATDD 42.5: ProcessHistory.HasEverSeen 三态 (AC#6 基础设施)
//
// Acceptance criteria covered:
//   - AC#6  UNIT-030  HasEverSeen returns true for currently-present UUID
//   - AC#6  UNIT-031  HasEverSeen returns true for previously-added then
//                     RemoveByUUID'd UUID (the gc'd marker)
//   - AC#6  UNIT-032  HasEverSeen returns false for never-Add'd UUID
//
// RED PHASE: HasEverSeen stub only checks current entries. UNIT-030 and
// UNIT-032 pass even in RED phase; UNIT-031 is wrapped in t.Skip until
// dev-story extends RemoveByUUID + adds the `removedUUIDs` field.
// =============================================================================

const (
	hesUUIDExisting = "existing-aaaaaaaa-bbbb-cccc-dddd-000000000001"
	hesUUIDRemoved  = "removed-aaaaaaaa-bbbb-cccc-dddd-000000000002"
	hesUUIDUnknown  = "unknown-aaaaaaaa-bbbb-cccc-dddd-000000000003"
)

// hesProcInfo builds a minimal vfs.ProcInfo so we can Add() into ProcessHistory.
func hesProcInfo(uuid string) vfs.ProcInfo {
	return vfs.ProcInfo{
		PID:   types.PID(1),
		UUID:  uuid,
		State: types.StateDead,
	}
}

// --- UNIT-030 (AC#6): currently-present UUID returns true ---

func TestATDD_42_5_030_HasEverSeen_CurrentlyPresent(t *testing.T) {
	h := NewProcessHistory(10)
	h.Add(hesProcInfo(hesUUIDExisting))

	if !h.HasEverSeen(hesUUIDExisting) {
		t.Errorf("HasEverSeen(%q) = false, want true (currently in entries)", hesUUIDExisting)
	}
}

// --- UNIT-031 (AC#6): post-RemoveByUUID still returns true ---

func TestATDD_42_5_031_HasEverSeen_AfterRemove(t *testing.T) {
	h := NewProcessHistory(10)
	h.Add(hesProcInfo(hesUUIDRemoved))

	if !h.RemoveByUUID(hesUUIDRemoved) {
		t.Fatal("RemoveByUUID returned false for an Add'd UUID")
	}

	// FindByUUID must say no (entry is gone from the ring buffer).
	if h.FindByUUID(hesUUIDRemoved) != nil {
		t.Error("FindByUUID must return nil after RemoveByUUID")
	}

	// HasEverSeen must still say yes (Add'd at some point, even if since removed).
	// This is the AC#6 distinguishing signal between "garbage collected" and
	// "never spawned" in resume error messages.
	if !h.HasEverSeen(hesUUIDRemoved) {
		t.Errorf("HasEverSeen(%q) = false, want true (was Add'd then RemoveByUUID'd; AC#6)", hesUUIDRemoved)
	}
}

// --- UNIT-032 (AC#6): never-Add'd UUID returns false ---

func TestATDD_42_5_032_HasEverSeen_NeverAdded(t *testing.T) {
	h := NewProcessHistory(10)
	// Add a different UUID, never touch hesUUIDUnknown.
	h.Add(hesProcInfo(hesUUIDExisting))

	if h.HasEverSeen(hesUUIDUnknown) {
		t.Errorf("HasEverSeen(%q) = true, want false (never Add'd)", hesUUIDUnknown)
	}
	// Empty string is also never-added.
	if h.HasEverSeen("") {
		t.Errorf("HasEverSeen(\"\") = true, want false (defensive)")
	}
}
