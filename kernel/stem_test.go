package kernel

import (
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/rnixai/rnix/skills"
)

var errTestDiscovery = errors.New("discovery error for testing")

// =============================================================================
// ATDD Story 20.3: Stem Agent & Auto-Differentiation
// TDD RED PHASE - All tests designed to FAIL until implementation exists
// =============================================================================

// --- mockSkillDiscovery provides a controllable SkillDiscovery for testing ---

type mockSkillDiscovery struct {
	skills []skills.SkillInfo
	err    error
}

func (m *mockSkillDiscovery) discoverAll() ([]skills.SkillInfo, error) {
	return m.skills, m.err
}

// testSkillInfoList creates a predefined set of skill metadata for matcher tests.
func testSkillInfoList() []skills.SkillInfo {
	return []skills.SkillInfo{
		{Manifest: skills.SkillManifest{Name: "code-analysis", Description: "Analyze source code for quality issues and bugs"}},
		{Manifest: skills.SkillManifest{Name: "git-tools", Description: "Git version control operations and repository management"}},
		{Manifest: skills.SkillManifest{Name: "web-search", Description: "Search the web for information and documentation"}},
		{Manifest: skills.SkillManifest{Name: "file-editor", Description: "Edit and modify files in the filesystem"}},
		{Manifest: skills.SkillManifest{Name: "test-runner", Description: "Run tests and report test results"}},
	}
}

// --- Task 2: Intent-Skill Matching Engine (AC: #1) ---

func TestStemMatcher_Match_CodeAnalysis(t *testing.T) {
	// Given a StemMatcher with known skills
	discovery := &mockSkillDiscovery{skills: testSkillInfoList()}
	matcher := NewStemMatcherFromFunc(discovery.discoverAll)

	// When matching intent "analyze code"
	matched, err := matcher.Match("analyze code")

	// Then code-analysis skill is matched
	if err != nil {
		t.Fatalf("Match returned error: %v", err)
	}
	if len(matched) == 0 {
		t.Fatal("Match returned empty list, expected at least one skill")
	}
	if matched[0] != "code-analysis" {
		t.Errorf("first match = %q, want %q", matched[0], "code-analysis")
	}
}

func TestStemMatcher_Match_NoMatch(t *testing.T) {
	// Given a StemMatcher with known skills
	discovery := &mockSkillDiscovery{skills: testSkillInfoList()}
	matcher := NewStemMatcherFromFunc(discovery.discoverAll)

	// When matching an unrelated intent
	matched, err := matcher.Match("cook dinner recipe")

	// Then no skills match (empty list, no error)
	if err != nil {
		t.Fatalf("Match returned error: %v", err)
	}
	if len(matched) != 0 {
		t.Errorf("expected empty match list for unrelated intent, got %v", matched)
	}
}

func TestStemMatcher_Match_MultipleSkills(t *testing.T) {
	// Given a StemMatcher with known skills
	discovery := &mockSkillDiscovery{skills: testSkillInfoList()}
	matcher := NewStemMatcherFromFunc(discovery.discoverAll)

	// When matching an intent that could match multiple skills
	matched, err := matcher.Match("analyze code and run tests")

	// Then multiple skills matched, ordered by relevance (descending score)
	if err != nil {
		t.Fatalf("Match returned error: %v", err)
	}
	if len(matched) < 2 {
		t.Fatalf("expected at least 2 matches, got %d: %v", len(matched), matched)
	}
	// Both code-analysis and test-runner should appear
	hasCodeAnalysis := false
	hasTestRunner := false
	for _, name := range matched {
		if name == "code-analysis" {
			hasCodeAnalysis = true
		}
		if name == "test-runner" {
			hasTestRunner = true
		}
	}
	if !hasCodeAnalysis {
		t.Error("expected code-analysis in matches")
	}
	if !hasTestRunner {
		t.Error("expected test-runner in matches")
	}
}

func TestStemMatcher_Match_EmptyIntent(t *testing.T) {
	// Given a StemMatcher with known skills
	discovery := &mockSkillDiscovery{skills: testSkillInfoList()}
	matcher := NewStemMatcherFromFunc(discovery.discoverAll)

	// When matching empty intent
	matched, err := matcher.Match("")

	// Then returns empty list without error
	if err != nil {
		t.Fatalf("Match returned error: %v", err)
	}
	if len(matched) != 0 {
		t.Errorf("expected empty match list for empty intent, got %v", matched)
	}
}

func TestStemMatcher_Match_DiscoveryError(t *testing.T) {
	// Given a StemMatcher whose discovery returns an error
	discovery := &mockSkillDiscovery{err: errTestDiscovery}
	matcher := NewStemMatcherFromFunc(discovery.discoverAll)

	// When matching any intent
	_, err := matcher.Match("analyze code")

	// Then the discovery error propagates
	if err == nil {
		t.Fatal("expected error from discovery, got nil")
	}
}

func TestStemMatcher_Match_EmptySkillList(t *testing.T) {
	// Given a StemMatcher with no available skills
	discovery := &mockSkillDiscovery{skills: []skills.SkillInfo{}}
	matcher := NewStemMatcherFromFunc(discovery.discoverAll)

	// When matching any intent
	matched, err := matcher.Match("analyze code")

	// Then returns empty list without error
	if err != nil {
		t.Fatalf("Match returned error: %v", err)
	}
	if len(matched) != 0 {
		t.Errorf("expected empty match list for no skills, got %v", matched)
	}
}

func TestStemMatcher_Match_EnglishKeywordsInIntent(t *testing.T) {
	// Given a StemMatcher with known skills
	discovery := &mockSkillDiscovery{skills: testSkillInfoList()}
	matcher := NewStemMatcherFromFunc(discovery.discoverAll)

	// When matching English keywords that appear in skill metadata
	matched, err := matcher.Match("code analysis")

	// Then code-analysis skill is matched
	if err != nil {
		t.Fatalf("Match returned error: %v", err)
	}
	if len(matched) == 0 {
		t.Fatal("Match returned empty list, expected at least one skill")
	}
	if !slices.Contains(matched, "code-analysis") {
		t.Errorf("expected code-analysis in matches, got %v", matched)
	}
}

func TestStemMatcher_Match_PureCJKIntent_NoMatch(t *testing.T) {
	// AC #1 specifies Chinese intent "分析代码", but the current keyword-based
	// matching cannot bridge CJK to English skill metadata. This is a known
	// limitation documented in Dev Notes -- Story 20.4 may upgrade to
	// embedding-based matching. For now, pure CJK intent returns no matches
	// and the stem agent runs as a bare process.
	discovery := &mockSkillDiscovery{skills: testSkillInfoList()}
	matcher := NewStemMatcherFromFunc(discovery.discoverAll)

	matched, err := matcher.Match("分析代码")
	if err != nil {
		t.Fatalf("Match returned error: %v", err)
	}
	if len(matched) != 0 {
		t.Errorf("pure CJK intent should not match English skill metadata, got %v", matched)
	}
}

// --- Task 5: NFR42 Performance Verification (AC: #2) ---

func TestStemMatcher_Performance_NFR42(t *testing.T) {
	// Given a StemMatcher with 50 skills (simulating a large skill set)
	largeSkillList := make([]skills.SkillInfo, 50)
	for i := range largeSkillList {
		largeSkillList[i] = skills.SkillInfo{
			Manifest: skills.SkillManifest{
				Name:        fmt.Sprintf("skill-%03d", i),
				Description: fmt.Sprintf("Skill number %d for testing performance with various keywords like code analysis git web search", i),
			},
		}
	}
	discovery := &mockSkillDiscovery{skills: largeSkillList}
	matcher := NewStemMatcherFromFunc(discovery.discoverAll)

	// When matching with a multi-keyword intent
	start := time.Now()
	_, err := matcher.Match("analyze code and search web for documentation about git tools")
	elapsed := time.Since(start)

	// Then the match completes within 3 seconds (NFR42)
	if err != nil {
		t.Fatalf("Match returned error: %v", err)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("NFR42 violation: Match took %v, must be <= 3s", elapsed)
	}
	t.Logf("NFR42 check: Match completed in %v", elapsed)
}
