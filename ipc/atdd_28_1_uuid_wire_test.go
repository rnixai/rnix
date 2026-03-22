package ipc

// =============================================================================
// ATDD Story 28.1: IPC UUID Wire Protocol
// TDD RED PHASE — Tests reference fields not yet added to wire types.
//                  Compilation failure IS the red phase.
// =============================================================================
//
// Test Strategy:
//   AC-6: ProcInfoWire has UUID, ProcInfoToWire/WireToProcInfo preserve it
//   AC-6: SpawnResponse has UUID
//   AC-6: ProgressPayload has UUID for OnSpawn events
//   AC-7: JSON output includes uuid field
//
// Priority: P0 (IPC contract)
// Test Level: Unit

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

const testUUID = "019534a1-7c6b-7000-8abc-123456789012"

// ---------------------------------------------------------------------------
// AC-6: ProcInfoWire has UUID field
// ---------------------------------------------------------------------------

func TestATDD_28_1_AC6_ProcInfoWire_HasUUID(t *testing.T) {
	w := ProcInfoWire{
		PID:   1,
		State: types.StateRunning,
		UUID:  testUUID,
	}
	if w.UUID != testUUID {
		t.Fatalf("AC-6: ProcInfoWire.UUID = %q, want %q", w.UUID, testUUID)
	}
}

func TestATDD_28_1_AC6_ProcInfoWire_UUID_JSON_Roundtrip(t *testing.T) {
	w := ProcInfoWire{
		PID:    1,
		PPID:   0,
		State:  types.StateRunning,
		Intent: "test",
		UUID:   testUUID,
	}

	data, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("AC-6: marshal ProcInfoWire: %v", err)
	}

	var decoded ProcInfoWire
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("AC-6: unmarshal ProcInfoWire: %v", err)
	}
	if decoded.UUID != testUUID {
		t.Fatalf("AC-6: roundtrip UUID = %q, want %q", decoded.UUID, testUUID)
	}
}

func TestATDD_28_1_AC6_ProcInfoWire_UUID_OmitEmpty(t *testing.T) {
	w := ProcInfoWire{PID: 1, State: types.StateRunning}
	data, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("AC-6: marshal: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("AC-6: unmarshal raw: %v", err)
	}
	if _, ok := raw["uuid"]; ok {
		t.Fatal("AC-6: UUID field should be omitted when empty (omitempty)")
	}
}

// ---------------------------------------------------------------------------
// AC-6: ProcInfoToWire preserves UUID
// ---------------------------------------------------------------------------

func TestATDD_28_1_AC6_ProcInfoToWire_PreservesUUID(t *testing.T) {
	info := vfs.ProcInfo{
		PID:       1,
		PPID:      0,
		State:     types.StateRunning,
		Intent:    "test conversion",
		CreatedAt: time.Now(),
		UUID:      testUUID,
	}

	wire := ProcInfoToWire(info)
	if wire.UUID != testUUID {
		t.Fatalf("AC-6: ProcInfoToWire lost UUID: got %q, want %q", wire.UUID, testUUID)
	}
}

// ---------------------------------------------------------------------------
// AC-6: WireToProcInfo preserves UUID
// ---------------------------------------------------------------------------

func TestATDD_28_1_AC6_WireToProcInfo_PreservesUUID(t *testing.T) {
	wire := ProcInfoWire{
		PID:       1,
		State:     types.StateRunning,
		CreatedAt: time.Now().UnixMilli(),
		UUID:      testUUID,
	}

	info := WireToProcInfo(wire)
	if info.UUID != testUUID {
		t.Fatalf("AC-6: WireToProcInfo lost UUID: got %q, want %q", info.UUID, testUUID)
	}
}

// ---------------------------------------------------------------------------
// AC-6: ProcInfoToWire → WireToProcInfo roundtrip
// ---------------------------------------------------------------------------

func TestATDD_28_1_AC6_ProcInfo_WireRoundtrip_UUID(t *testing.T) {
	original := vfs.ProcInfo{
		PID:       42,
		PPID:      1,
		State:     types.StateZombie,
		Intent:    "roundtrip test",
		Skills:    []string{"skill-a"},
		CreatedAt: time.Now().Truncate(time.Millisecond),
		UUID:      testUUID,
		Provider:  "claude",
		Model:     "opus",
	}

	wire := ProcInfoToWire(original)
	restored := WireToProcInfo(wire)

	if restored.UUID != original.UUID {
		t.Fatalf("AC-6: roundtrip UUID = %q, want %q", restored.UUID, original.UUID)
	}
}

// ---------------------------------------------------------------------------
// AC-6: SpawnResponse has UUID field
// ---------------------------------------------------------------------------

func TestATDD_28_1_AC6_SpawnResponse_HasUUID(t *testing.T) {
	resp := SpawnResponse{PID: 1, UUID: testUUID}
	if resp.UUID != testUUID {
		t.Fatalf("AC-6: SpawnResponse.UUID = %q, want %q", resp.UUID, testUUID)
	}
}

func TestATDD_28_1_AC6_SpawnResponse_UUID_JSON(t *testing.T) {
	resp := SpawnResponse{PID: 1, UUID: testUUID}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("AC-6: marshal SpawnResponse: %v", err)
	}

	var decoded SpawnResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("AC-6: unmarshal SpawnResponse: %v", err)
	}
	if decoded.UUID != testUUID {
		t.Fatalf("AC-6: SpawnResponse roundtrip UUID = %q, want %q", decoded.UUID, testUUID)
	}
}

// ---------------------------------------------------------------------------
// AC-6: ProgressPayload includes UUID for spawn events
// ---------------------------------------------------------------------------

func TestATDD_28_1_AC6_ProgressPayload_SpawnEvent_HasUUID(t *testing.T) {
	pp := ProgressPayload{
		Event:    "spawn",
		PID:      1,
		Intent:   "test",
		Provider: "claude",
		Model:    "opus",
		UUID:     testUUID,
	}

	data, err := json.Marshal(pp)
	if err != nil {
		t.Fatalf("AC-6: marshal ProgressPayload: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("AC-6: unmarshal raw: %v", err)
	}
	uuidField, ok := raw["uuid"]
	if !ok {
		t.Fatal("AC-6: ProgressPayload JSON should contain 'uuid' field for spawn events")
	}
	var uuidVal string
	if err := json.Unmarshal(uuidField, &uuidVal); err != nil {
		t.Fatalf("AC-6: unmarshal uuid field: %v", err)
	}
	if uuidVal != testUUID {
		t.Fatalf("AC-6: ProgressPayload uuid = %q, want %q", uuidVal, testUUID)
	}
}

// ---------------------------------------------------------------------------
// AC-7: JSON process output includes uuid field
// ---------------------------------------------------------------------------

func TestATDD_28_1_AC7_ListProcsResponse_ContainsUUID(t *testing.T) {
	resp := ListProcsResponse{
		Processes: []ProcInfoWire{
			{PID: 1, State: types.StateRunning, UUID: testUUID},
			{PID: 2, State: types.StateZombie, UUID: "019534a1-7c6b-7000-8abc-999999999999"},
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("AC-7: marshal ListProcsResponse: %v", err)
	}

	var raw struct {
		Processes []map[string]json.RawMessage `json:"processes"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("AC-7: unmarshal raw: %v", err)
	}

	for i, proc := range raw.Processes {
		uuidField, ok := proc["uuid"]
		if !ok {
			t.Fatalf("AC-7: process %d missing 'uuid' field in JSON", i)
		}
		var uuidVal string
		if err := json.Unmarshal(uuidField, &uuidVal); err != nil {
			t.Fatalf("AC-7: process %d uuid unmarshal: %v", i, err)
		}
		if uuidVal == "" {
			t.Fatalf("AC-7: process %d uuid should not be empty", i)
		}
	}
}
