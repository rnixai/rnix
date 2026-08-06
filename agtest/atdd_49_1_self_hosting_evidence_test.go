package agtest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Story 49.1 (self-hosting-validation-evidence) regression guards. The Tier1
// case replays a static script, so a silently edited fixture or a script that
// no longer contains the asserted keywords would still go green — these tests
// pin the feature's source-of-truth files so the evidence cannot drift.

const (
	selfHostingCaseName  = "self-hosting-validation"
	selfHostingTier1Dir  = "../tests/agtest/tier1"
	selfHostingFixture   = "../tests/agtest/tier1/testdata/self-hosting-demo.go"
	selfHostingScript    = "../tests/agtest/tier1/scripts/13-self-hosting-validation.responses.yaml"
	selfHostingTier2Case = "../tests/agtest/tier2/02-self-hosting-live.yaml"
	evidenceArtifactsDir = "../_bmad-output/test-artifacts"
)

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// TestSelfHostingEvidence_PlantedFixtureIntact pins the three planted defects
// (Story 49.1 Task 1). If anyone "fixes" the fixture, the replay script keeps
// passing while the case silently stops exercising real content.
func TestSelfHostingEvidence_PlantedFixtureIntact(t *testing.T) {
	content := mustReadFile(t, selfHostingFixture)

	if !strings.Contains(content, "Planted fixture for Story 49.1") {
		t.Error("fixture must retain the Story 49.1 planted-fixture header comment")
	}
	if !strings.Contains(content, "users[i+1]") {
		t.Error("planted [Critical] defect users[i+1] (out-of-bounds) missing from fixture")
	}
	if !strings.Contains(content, "data, _ := os.ReadFile(cfgPath)") {
		t.Error("planted [Warning] defect (unchecked os.ReadFile error) missing from fixture")
	}
	if !strings.Contains(content, "os.Open(logPath)") {
		t.Error("planted [Warning] defect (resource leak: os.Open without Close) missing from fixture")
	}
	if strings.Contains(content, "\tdefer ") {
		t.Error("resource-leak fixture must NOT contain a defer statement — leak is the planted defect")
	}
}

// TestSelfHostingEvidence_CaseScriptContract verifies the Tier1 case is
// present, replay-driven, and that every output assertion keyword appears
// verbatim in its paired script (Story 49.1 关键防错 #3) — otherwise the case
// would pass without ever exercising the code-analyst report contract.
func TestSelfHostingEvidence_CaseScriptContract(t *testing.T) {
	suite, err := ParseDir(selfHostingTier1Dir)
	if err != nil {
		t.Fatalf("parse tier1 suite: %v", err)
	}

	var tc *TestCaseSpec
	for i := range suite.Tests {
		if suite.Tests[i].Name == selfHostingCaseName {
			tc = &suite.Tests[i]
			break
		}
	}
	if tc == nil {
		t.Fatalf("Tier1 suite must contain case %q", selfHostingCaseName)
	}
	if tc.Agent.Provider != "replay" {
		t.Errorf("case %q must use provider replay, got %q", selfHostingCaseName, tc.Agent.Provider)
	}
	if tc.Assert == nil || tc.Assert.Output == nil || len(tc.Assert.Output.Contains) == 0 {
		t.Fatalf("case %q must have output.contains assertions", selfHostingCaseName)
	}
	if tc.Assert.Syscalls == nil || len(tc.Assert.Syscalls.Includes) == 0 {
		t.Fatalf("case %q must have syscalls.includes assertions", selfHostingCaseName)
	}

	script := mustReadFile(t, selfHostingScript)
	for _, kw := range tc.Assert.Output.Contains {
		if !strings.Contains(script, kw) {
			t.Errorf("script %s must contain output assertion keyword %q verbatim (逐字一致)", selfHostingScript, kw)
		}
	}
	// Read is a scripted tool_call (the fixture read); ReasonStep is emitted by
	// the real reasoning loop and is never scripted, so only Read is checkable here.
	if !strings.Contains(script, "name: Read") {
		t.Error("script must open with a Read tool_call — proves the agent read the fixture (AC2)")
	}
}

// TestSelfHostingEvidence_Tier2Isolation pins the isolation discipline (AC6):
// the live-LLM Tier2 case must exist as advisory but never be parsed as part
// of the deterministic Tier1 suite.
func TestSelfHostingEvidence_Tier2Isolation(t *testing.T) {
	if _, err := os.Stat(selfHostingTier2Case); err != nil {
		t.Fatalf("Tier2 advisory case %s must exist: %v", selfHostingTier2Case, err)
	}
	tier2Content := mustReadFile(t, selfHostingTier2Case)
	if strings.Contains(tier2Content, "provider: replay") {
		t.Error("Tier2 case must not use the replay provider (it is a live-LLM advisory case)")
	}

	suite, err := ParseDir(selfHostingTier1Dir)
	if err != nil {
		t.Fatalf("parse tier1 suite: %v", err)
	}
	for _, c := range suite.Tests {
		if strings.Contains(c.Name, "live") {
			t.Errorf("Tier2 case %q leaked into the deterministic Tier1 suite (AC6 isolation)", c.Name)
		}
	}
}

// TestSelfHostingEvidence_ArtifactReviewed pins the evidence-of-record (AC4/5):
// the Tier2 artifact must exist with a manual review conclusion marking at
// least one finding as confirmed.
func TestSelfHostingEvidence_ArtifactReviewed(t *testing.T) {
	matches, err := filepath.Glob(filepath.Join(evidenceArtifactsDir, "self-hosting-evidence-*.md"))
	if err != nil {
		t.Fatalf("glob evidence artifacts: %v", err)
	}
	if len(matches) == 0 {
		t.Fatalf("no self-hosting-evidence-*.md artifact under %s", evidenceArtifactsDir)
	}
	content := mustReadFile(t, matches[0])
	for _, marker := range []string{"人工复核结论", "已确认", "NR-1 判定"} {
		if !strings.Contains(content, marker) {
			t.Errorf("evidence artifact %s must contain review marker %q", matches[0], marker)
		}
	}
}
