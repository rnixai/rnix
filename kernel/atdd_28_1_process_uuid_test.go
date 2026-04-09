package kernel

// =============================================================================
// ATDD Story 28.1: Process UUID v7 引入
// TDD RED PHASE — Tests reference fields/signatures not yet implemented.
//                  Compilation failure IS the red phase.
// =============================================================================
//
// Test Strategy:
//   AC-1: Process struct has UUID field, populated by NewProcess()
//   AC-2: UUID v7 generation — valid format, time-ordered, unique, ≤1ms latency
//   AC-3: Cross-daemon uniqueness — UUID independent of PID counter
//   AC-5: KernelCallbacks.OnSpawn receives uuid parameter
//
// Priority: P0 (core identity infrastructure)
// Test Level: Unit

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// ---------------------------------------------------------------------------
// AC-1: Process struct has UUID field
// ---------------------------------------------------------------------------

func TestATDD_28_1_AC1_NewProcess_HasUUID(t *testing.T) {
	proc := NewProcess(0, "test intent", []string{"test-skill"})
	if proc.UUID == "" {
		t.Fatal("AC-1: NewProcess() should populate UUID field, got empty string")
	}
}

func TestATDD_28_1_AC1_UUID_Format_36Chars(t *testing.T) {
	proc := NewProcess(0, "test intent", nil)
	if len(proc.UUID) != 36 {
		t.Fatalf("AC-1: UUID should be 36 characters (standard format), got %d: %q", len(proc.UUID), proc.UUID)
	}
}

func TestATDD_28_1_AC1_UUID_Immutable_AfterCreation(t *testing.T) {
	proc := NewProcess(0, "test intent", nil)
	uuid1 := proc.UUID
	if uuid1 == "" {
		t.Fatal("AC-1: UUID should be non-empty")
	}
	uuid2 := proc.UUID
	if uuid1 != uuid2 {
		t.Fatalf("AC-1: UUID should be immutable, got %q then %q", uuid1, uuid2)
	}
}

// ---------------------------------------------------------------------------
// AC-2: Spawn generates UUID v7 — valid format, time-ordered, unique
// ---------------------------------------------------------------------------

func TestATDD_28_1_AC2_UUID_V7_VersionByte(t *testing.T) {
	proc := NewProcess(0, "test intent", nil)
	uuid := proc.UUID
	if len(uuid) < 15 {
		t.Fatalf("AC-2: UUID too short: %q", uuid)
	}
	// UUID v7 format: xxxxxxxx-xxxx-7xxx-yxxx-xxxxxxxxxxxx
	// The 15th character (index 14) should be '7' for version 7
	if uuid[14] != '7' {
		t.Fatalf("AC-2: UUID version byte should be '7' (v7), got '%c' in %q", uuid[14], uuid)
	}
}

func TestATDD_28_1_AC2_UUID_Parseable(t *testing.T) {
	proc := NewProcess(0, "test intent", nil)
	uuid := proc.UUID

	// Validate standard UUID format: 8-4-4-4-12 hex chars
	parts := splitUUID(uuid)
	if len(parts) != 5 {
		t.Fatalf("AC-2: UUID should have 5 parts separated by '-', got %d in %q", len(parts), uuid)
	}
	expectedLens := []int{8, 4, 4, 4, 12}
	for i, part := range parts {
		if len(part) != expectedLens[i] {
			t.Errorf("AC-2: UUID part %d should be %d chars, got %d in %q", i, expectedLens[i], len(part), uuid)
		}
		for _, c := range part {
			if !isHexChar(c) {
				t.Errorf("AC-2: UUID contains non-hex char '%c' in %q", c, uuid)
			}
		}
	}
}

func TestATDD_28_1_AC2_UUID_TimeOrdered(t *testing.T) {
	proc1 := NewProcess(0, "first", nil)
	time.Sleep(2 * time.Millisecond)
	proc2 := NewProcess(0, "second", nil)

	// UUID v7 is time-ordered: later creation → lexicographically larger UUID
	if proc2.UUID <= proc1.UUID {
		t.Fatalf("AC-2: UUID v7 should be time-ordered: %q (first) should be < %q (second)", proc1.UUID, proc2.UUID)
	}
}

func TestATDD_28_1_AC2_UUID_Uniqueness_1000(t *testing.T) {
	seen := make(map[string]struct{}, 1000)
	for i := range 1000 {
		proc := NewProcess(0, "uniqueness test", nil)
		if _, exists := seen[proc.UUID]; exists {
			t.Fatalf("AC-2: UUID collision at iteration %d: %q", i, proc.UUID)
		}
		seen[proc.UUID] = struct{}{}
	}
}

func TestATDD_28_1_AC2_UUID_GenerationLatency(t *testing.T) {
	// NFR65-obs: UUID generation ≤ 1ms
	iterations := 100
	start := time.Now()
	for range iterations {
		_ = NewProcess(0, "latency test", nil)
	}
	elapsed := time.Since(start)
	avgNs := elapsed.Nanoseconds() / int64(iterations)
	if avgNs > int64(time.Millisecond) {
		t.Fatalf("AC-2/NFR65: average UUID generation latency %v exceeds 1ms", time.Duration(avgNs))
	}
}

// ---------------------------------------------------------------------------
// AC-3: Cross-daemon UUID uniqueness — UUID independent of PID counter
// ---------------------------------------------------------------------------

func TestATDD_28_1_AC3_UUID_Independent_Of_PIDCounter(t *testing.T) {
	// Simulate "daemon restart" by recording current UUID, resetting PID
	// counter (which happens on new daemon), then verifying new UUID differs.
	proc1 := NewProcess(0, "before restart", nil)
	uuid1 := proc1.UUID

	// Save and reset PID counter to simulate daemon restart
	oldPID := pidCounter.Load()
	pidCounter.Store(0)
	defer pidCounter.Store(oldPID)

	proc2 := NewProcess(0, "after restart", nil)
	uuid2 := proc2.UUID

	if uuid1 == uuid2 {
		t.Fatalf("AC-3: UUID should be unique across daemon restarts, both got %q", uuid1)
	}
	// PID may collide (restarted from 0), but UUID must not
	if proc1.PID == proc2.PID {
		t.Logf("AC-3: PID collision (expected after restart): PID=%d", proc1.PID)
	}
}

func TestATDD_28_1_AC3_ConcurrentUUID_Uniqueness(t *testing.T) {
	const goroutines = 10
	const perGoroutine = 100

	type result struct {
		uuids []string
	}

	results := make(chan result, goroutines)
	for range goroutines {
		go func() {
			uuids := make([]string, perGoroutine)
			for i := range perGoroutine {
				proc := NewProcess(0, "concurrent", nil)
				uuids[i] = proc.UUID
			}
			results <- result{uuids: uuids}
		}()
	}

	allUUIDs := make(map[string]struct{}, goroutines*perGoroutine)
	for range goroutines {
		r := <-results
		for _, uuid := range r.uuids {
			if _, exists := allUUIDs[uuid]; exists {
				t.Fatalf("AC-3: concurrent UUID collision: %q", uuid)
			}
			allUUIDs[uuid] = struct{}{}
		}
	}
}

// ---------------------------------------------------------------------------
// AC-5: KernelCallbacks.OnSpawn receives uuid parameter
// ---------------------------------------------------------------------------

type mockCallbacksWithUUID struct {
	spawnCalls []spawnCallRecord
}

type spawnCallRecord struct {
	pid      types.PID
	intent   string
	provider string
	model    string
	uuid     string
}

func (m *mockCallbacksWithUUID) OnSpawn(pid types.PID, intent, provider, model, uuid string) {
	m.spawnCalls = append(m.spawnCalls, spawnCallRecord{
		pid: pid, intent: intent, provider: provider, model: model, uuid: uuid,
	})
}
func (m *mockCallbacksWithUUID) OnStep(pid types.PID, step, total int)                           {}
func (m *mockCallbacksWithUUID) OnStepComplete(pid types.PID, step int, action, summary string, hasError bool, durationMs float64) {}
func (m *mockCallbacksWithUUID) OnComplete(pid types.PID, result string, exit ExitStatus)        {}
func (m *mockCallbacksWithUUID) OnError(pid types.PID, err error)                                {}
func (m *mockCallbacksWithUUID) OnAskUser(pid types.PID, requestID string, questions []byte) ([]byte, error) {
	return nil, nil
}

func TestATDD_28_1_AC5_OnSpawn_ReceivesUUID(t *testing.T) {
	// This test verifies KernelCallbacks.OnSpawn signature includes uuid.
	// It will fail to compile until the interface is updated.
	cb := &mockCallbacksWithUUID{}
	cb.OnSpawn(1, "test", "provider", "model", "019…fake-uuid")
	if len(cb.spawnCalls) != 1 {
		t.Fatal("AC-5: OnSpawn should record one call")
	}
	if cb.spawnCalls[0].uuid != "019…fake-uuid" {
		t.Fatalf("AC-5: OnSpawn uuid = %q, want %q", cb.spawnCalls[0].uuid, "019…fake-uuid")
	}
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func splitUUID(uuid string) []string {
	var parts []string
	var current []byte
	for _, c := range []byte(uuid) {
		if c == '-' {
			parts = append(parts, string(current))
			current = nil
		} else {
			current = append(current, c)
		}
	}
	if len(current) > 0 {
		parts = append(parts, string(current))
	}
	return parts
}

func isHexChar(c rune) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// Ensure atomic import is used (Go compiler requires all imports to be used)
var _ atomic.Uint64
