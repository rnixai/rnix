package agtest

import (
	"path/filepath"
	"testing"
)

// tier1SuiteDir is the production Tier1 suite, relative to this package
// directory (agtest/ → repo root → tests/agtest/tier1).
const tier1SuiteDir = "../tests/agtest/tier1"

// TestTier1Suite_GuardConformance is the repository guard that keeps the real
// Tier1 golden suite honest. It runs inside `make test` / CI, so a
// non-conforming case (or a deleted/renamed suite directory) can never land on
// main. Story 68.2 裁决 2 + 关键防错 #3: "zero cases == FAIL" — if the directory
// is silently emptied the guard must go red rather than pass vacuously.
func TestTier1Suite_GuardConformance(t *testing.T) {
	dir, err := filepath.Abs(tier1SuiteDir)
	if err != nil {
		t.Fatalf("resolve tier1 suite dir: %v", err)
	}

	suite, err := ParseDir(dir)
	if err != nil {
		t.Fatalf("Tier1 suite must parse (dir %s): %v", dir, err)
	}

	// AC2 lower bound + anti-silent-emptying guard.
	if len(suite.Tests) < 10 {
		t.Fatalf("Tier1 suite must contain >= 10 cases (AC2), got %d in %s", len(suite.Tests), dir)
	}

	if err := ValidateTier1(suite); err != nil {
		t.Fatalf("Tier1 suite violates ValidateTier1 discipline:\n%v", err)
	}
}
