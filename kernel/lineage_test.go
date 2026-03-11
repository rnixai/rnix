package kernel

// =============================================================================
// ATDD Story 20.5: Differentiation Lineage Graph -- Unit Tests
// TDD RED PHASE - All tests designed to FAIL until implementation exists
// =============================================================================
//
// Test Strategy:
// - Task 1: Lineage data model (Record, Events, concurrency, empty state)
//
// Priority: P0 (core data model for lineage tracking)
// Test Level: Unit

import (
	"sync"
	"testing"
	"time"
)

// --- Task 1: Lineage Unit Tests ---

func TestLineage_RecordAndEvents(t *testing.T) {
	// Given: a new Lineage
	// When: recording two events
	// Then: Events() returns them in insertion order
	l := NewLineage()

	ev1 := LineageEvent{
		Timestamp: time.Now(),
		Phase:     "initial",
		Skills:    []string{"code-analysis", "git-tools"},
		Trigger:   "analyze code quality",
	}
	ev2 := LineageEvent{
		Timestamp: time.Now().Add(time.Second),
		Phase:     "progressive",
		Skills:    []string{"test-runner"},
		Trigger:   "need test execution capability",
	}

	l.Record(ev1)
	l.Record(ev2)

	events := l.Events()
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Phase != "initial" {
		t.Errorf("expected first event phase='initial', got %q", events[0].Phase)
	}
	if events[1].Phase != "progressive" {
		t.Errorf("expected second event phase='progressive', got %q", events[1].Phase)
	}
	if len(events[0].Skills) != 2 {
		t.Errorf("expected first event to have 2 skills, got %d", len(events[0].Skills))
	}
	if events[0].Skills[0] != "code-analysis" {
		t.Errorf("expected first skill='code-analysis', got %q", events[0].Skills[0])
	}
	if events[1].Trigger != "need test execution capability" {
		t.Errorf("expected second trigger, got %q", events[1].Trigger)
	}
}

func TestLineage_EmptyEvents(t *testing.T) {
	// Given: a new Lineage with no records
	// When: calling Events()
	// Then: returns empty (non-nil) slice
	l := NewLineage()

	events := l.Events()
	if events == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}
	if len(events) != 0 {
		t.Fatalf("expected 0 events, got %d", len(events))
	}
}

func TestLineage_ConcurrentAccess(t *testing.T) {
	// Given: a Lineage shared among multiple goroutines
	// When: 100 goroutines concurrently Record and read Events
	// Then: no data race occurs (verified by -race flag)
	l := NewLineage()

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	// Half write, half read
	for i := range goroutines {
		go func(n int) {
			defer wg.Done()
			l.Record(LineageEvent{
				Timestamp: time.Now(),
				Phase:     "progressive",
				Skills:    []string{"skill-" + string(rune('a'+n%26))},
				Trigger:   "concurrent test",
			})
		}(i)

		go func() {
			defer wg.Done()
			_ = l.Events()
		}()
	}

	wg.Wait()

	events := l.Events()
	if len(events) != goroutines {
		t.Errorf("expected %d events, got %d", goroutines, len(events))
	}
}

func TestLineage_FromMemoryFlag(t *testing.T) {
	// Given: a Lineage
	// When: recording an event with FromMemory=true
	// Then: the flag is preserved when reading Events()
	l := NewLineage()

	l.Record(LineageEvent{
		Timestamp:  time.Now(),
		Phase:      "initial",
		Skills:     []string{"code-analysis"},
		Trigger:    "analyze code",
		FromMemory: true,
	})

	events := l.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if !events[0].FromMemory {
		t.Error("expected FromMemory=true, got false")
	}
}

func TestLineage_EventsReturnsCopy(t *testing.T) {
	// Given: a Lineage with one event
	// When: modifying the returned slice
	// Then: the original Lineage is not affected
	l := NewLineage()

	l.Record(LineageEvent{
		Timestamp: time.Now(),
		Phase:     "initial",
		Skills:    []string{"code-analysis"},
		Trigger:   "test",
	})

	events := l.Events()
	events[0].Phase = "tampered"

	original := l.Events()
	if original[0].Phase == "tampered" {
		t.Error("Events() should return a copy, but modification affected the original")
	}
}
