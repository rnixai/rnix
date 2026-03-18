package kernel

// =============================================================================
// ATDD Story 20.4: Progressive Specialization & Differentiation Memory
// TDD RED PHASE - All tests designed to FAIL until implementation exists
// =============================================================================
//
// Test Strategy:
// - Task 1: DiffMemory data structure unit tests (Record, Lookup, normalize, eviction, concurrency)
// - Task 2: Spawn integration with differentiation memory (Lookup, Record, fallback)
//
// Priority: P0/P1 (core differentiation memory and progressive specialization)
// Test Level: Unit (DiffMemory) + Integration (Spawn)

import (
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- Task 1: DiffMemory Unit Tests ---

func TestDiffMemory_RecordAndLookup(t *testing.T) {
	// Given: a new DiffMemory with maxSize=10
	// When: recording a differentiation path for "analyze code"
	// Then: looking up the same intent returns the recorded skills
	dm := NewDiffMemory(10)

	dm.Record("analyze code", []string{"code-analysis", "git-tools"})

	skills, ok := dm.Lookup("analyze code")
	if !ok {
		t.Fatal("expected Lookup to find recorded intent")
	}
	if len(skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(skills))
	}
	if skills[0] != "code-analysis" || skills[1] != "git-tools" {
		t.Fatalf("expected [code-analysis, git-tools], got %v", skills)
	}
}

func TestDiffMemory_NormalizedIntent(t *testing.T) {
	// Given: a DiffMemory with a recorded entry for "analyze code"
	// When: looking up "code analyze" (same tokens, different order)
	// Then: it matches the same entry because normalizeIntent sorts tokens
	dm := NewDiffMemory(10)

	dm.Record("analyze code", []string{"code-analysis"})

	// Same tokens reordered should match
	skills, ok := dm.Lookup("code analyze")
	if !ok {
		t.Fatal("expected Lookup to find normalized intent (reordered tokens)")
	}
	if len(skills) != 1 || skills[0] != "code-analysis" {
		t.Fatalf("expected [code-analysis], got %v", skills)
	}
}

func TestDiffMemory_NormalizedIntent_CaseInsensitive(t *testing.T) {
	// Given: a recorded entry for "Analyze Code"
	// When: looking up "analyze code" (lowercase)
	// Then: it matches because normalizeIntent lowercases tokens
	dm := NewDiffMemory(10)

	dm.Record("Analyze Code", []string{"code-analysis"})

	skills, ok := dm.Lookup("analyze code")
	if !ok {
		t.Fatal("expected case-insensitive match")
	}
	if skills[0] != "code-analysis" {
		t.Fatalf("expected code-analysis, got %s", skills[0])
	}
}

func TestDiffMemory_UpdateExisting_SameSkills(t *testing.T) {
	// Given: a recorded entry for "analyze code" with skills [code-analysis]
	// When: recording the same intent with the same skills again
	// Then: Timestamp updates, skills unchanged (Record does not increment HitCount)
	dm := NewDiffMemory(10)

	dm.Record("analyze code", []string{"code-analysis"})
	time.Sleep(10 * time.Millisecond) // ensure timestamp difference
	dm.Record("analyze code", []string{"code-analysis"})

	skills, ok := dm.Lookup("analyze code")
	if !ok {
		t.Fatal("expected Lookup to find recorded intent")
	}
	if len(skills) != 1 || skills[0] != "code-analysis" {
		t.Fatalf("expected [code-analysis], got %v", skills)
	}
}

func TestDiffMemory_UpdateExisting_DifferentSkills(t *testing.T) {
	// Given: a recorded entry for "analyze code" with skills [code-analysis]
	// When: recording the same intent with DIFFERENT skills [code-analysis, git-tools]
	// Then: skill list is replaced with the new one (latest wins)
	dm := NewDiffMemory(10)

	dm.Record("analyze code", []string{"code-analysis"})
	dm.Record("analyze code", []string{"code-analysis", "git-tools"})

	skills, ok := dm.Lookup("analyze code")
	if !ok {
		t.Fatal("expected Lookup to find updated intent")
	}
	if len(skills) != 2 {
		t.Fatalf("expected 2 skills after update, got %d", len(skills))
	}
	if skills[0] != "code-analysis" || skills[1] != "git-tools" {
		t.Fatalf("expected [code-analysis, git-tools], got %v", skills)
	}
}

func TestDiffMemory_EvictionPolicy(t *testing.T) {
	// Given: a DiffMemory with maxSize=3
	// When: recording 4 intents (exceeding capacity)
	// Then: the least-used and oldest entry is evicted
	dm := NewDiffMemory(3)

	// Record 3 entries
	dm.Record("intent-a", []string{"skill-a"})
	dm.Record("intent-b", []string{"skill-b"})
	dm.Record("intent-c", []string{"skill-c"})

	// Boost intent-a and intent-c hit counts via Lookup
	dm.Lookup("intent-a")
	dm.Lookup("intent-c")

	// Record a 4th entry - should evict intent-b (lowest hit count)
	dm.Record("intent-d", []string{"skill-d"})

	// intent-b should be evicted
	_, ok := dm.Lookup("intent-b")
	if ok {
		t.Fatal("expected intent-b to be evicted (lowest HitCount)")
	}

	// Others should still exist
	if _, ok := dm.Lookup("intent-a"); !ok {
		t.Fatal("expected intent-a to still exist")
	}
	if _, ok := dm.Lookup("intent-c"); !ok {
		t.Fatal("expected intent-c to still exist")
	}
	if _, ok := dm.Lookup("intent-d"); !ok {
		t.Fatal("expected intent-d to exist (just added)")
	}
}

func TestDiffMemory_LookupNotFound(t *testing.T) {
	// Given: an empty DiffMemory
	// When: looking up any intent
	// Then: returns nil, false
	dm := NewDiffMemory(10)

	skills, ok := dm.Lookup("nonexistent intent")
	if ok {
		t.Fatal("expected Lookup to return false for nonexistent intent")
	}
	if skills != nil {
		t.Fatalf("expected nil skills, got %v", skills)
	}
}

func TestDiffMemory_LookupNotFound_AfterEviction(t *testing.T) {
	// Given: a DiffMemory with maxSize=1
	// When: recording "intent-a" then "intent-b"
	// Then: "intent-a" is evicted
	dm := NewDiffMemory(1)

	dm.Record("intent-a", []string{"skill-a"})
	dm.Record("intent-b", []string{"skill-b"})

	_, ok := dm.Lookup("intent-a")
	if ok {
		t.Fatal("expected intent-a to be evicted when maxSize=1")
	}
}

func TestDiffMemory_ConcurrentAccess(t *testing.T) {
	// Given: a DiffMemory accessed by 100 concurrent goroutines
	// When: half write and half read simultaneously
	// Then: no races or panics (verified by -race flag)
	dm := NewDiffMemory(256)

	var wg sync.WaitGroup
	const goroutines = 100

	// Writers
	for i := range goroutines / 2 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			intent := strings.Repeat("word", n%5+1)
			dm.Record(intent, []string{"skill-" + strings.Repeat("x", n%3+1)})
		}(i)
	}

	// Readers
	for i := range goroutines / 2 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			intent := strings.Repeat("word", n%5+1)
			dm.Lookup(intent)
		}(i)
	}

	wg.Wait()
	// If we get here without panic or race, test passes
}

func TestDiffMemory_EmptyIntent(t *testing.T) {
	// Given: a DiffMemory
	// When: recording an empty intent
	// Then: it should handle gracefully (record with empty normalized key)
	dm := NewDiffMemory(10)

	dm.Record("", []string{"skill-a"})

	// Empty intent should still be findable (or gracefully handled)
	skills, ok := dm.Lookup("")
	if !ok {
		t.Fatal("expected empty intent to be recorded and found")
	}
	if len(skills) != 1 || skills[0] != "skill-a" {
		t.Fatalf("expected [skill-a], got %v", skills)
	}
}

func TestDiffMemory_EmptySkillList(t *testing.T) {
	// Given: a DiffMemory
	// When: recording an intent with empty skill list
	// Then: Lookup returns empty list, true (entry exists but no skills)
	dm := NewDiffMemory(10)

	dm.Record("some intent", []string{})

	skills, ok := dm.Lookup("some intent")
	if !ok {
		t.Fatal("expected Lookup to find intent with empty skills")
	}
	if len(skills) != 0 {
		t.Fatalf("expected empty skill list, got %v", skills)
	}
}

// TestNormalizeIntent_TokenSort verifies that normalizeIntent produces
// sorted token signatures, consistent with the tokenize() function in stem.go.
func TestNormalizeIntent_TokenSort(t *testing.T) {
	// normalizeIntent should tokenize and sort, so "code analyze review"
	// and "review analyze code" produce the same signature.
	sig1 := normalizeIntent("code analyze review")
	sig2 := normalizeIntent("review analyze code")

	if sig1 != sig2 {
		t.Fatalf("expected same signature for reordered tokens, got %q vs %q", sig1, sig2)
	}

	// Verify the signature is sorted tokens joined by space
	tokens := tokenize("code analyze review")
	sort.Strings(tokens)
	expected := strings.Join(tokens, " ")
	if sig1 != expected {
		t.Fatalf("expected signature %q, got %q", expected, sig1)
	}
}

func TestNormalizeIntent_Deduplication(t *testing.T) {
	// normalizeIntent should deduplicate tokens (inherited from tokenize)
	sig := normalizeIntent("code code analysis analysis")

	tokens := strings.Fields(sig)
	seen := make(map[string]bool)
	for _, tok := range tokens {
		if seen[tok] {
			t.Fatalf("found duplicate token %q in normalized signature %q", tok, sig)
		}
		seen[tok] = true
	}
}
