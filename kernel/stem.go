package kernel

import (
	"sort"
	"strings"
	"unicode"

	"github.com/rnixai/rnix/skills"
)

// StemMatchResult pairs a skill name with its keyword overlap score.
type StemMatchResult struct {
	Name  string
	Score float64
}

// StemMatcher matches intents to skill combinations using keyword analysis.
type StemMatcher struct {
	discoverAll func() ([]skills.SkillInfo, error)
}

// NewStemMatcher creates a StemMatcher using a SkillDiscovery instance.
func NewStemMatcher(discovery *skills.SkillDiscovery) *StemMatcher {
	return &StemMatcher{discoverAll: discovery.DiscoverAll}
}

// NewStemMatcherFromFunc creates a StemMatcher with a custom discovery function (for testing).
func NewStemMatcherFromFunc(discoverFn func() ([]skills.SkillInfo, error)) *StemMatcher {
	return &StemMatcher{discoverAll: discoverFn}
}

// MatchWithScores analyzes the intent and returns skill matches with scores,
// ordered by relevance (descending score).
// Returns an empty list (not an error) if no skills match.
func (m *StemMatcher) MatchWithScores(intent string) ([]StemMatchResult, error) {
	if intent == "" {
		return nil, nil
	}

	allSkills, err := m.discoverAll()
	if err != nil {
		return nil, err
	}

	if len(allSkills) == 0 {
		return nil, nil
	}

	intentKeywords := tokenize(intent)
	if len(intentKeywords) == 0 {
		return nil, nil
	}

	var matches []StemMatchResult
	for _, skill := range allSkills {
		skillKeywords := tokenize(skill.Manifest.Name + " " + skill.Manifest.Description)
		score := overlapScore(intentKeywords, skillKeywords)
		if score > 0 {
			matches = append(matches, StemMatchResult{Name: skill.Manifest.Name, Score: score})
		}
	}

	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Score != matches[j].Score {
			return matches[i].Score > matches[j].Score
		}
		return matches[i].Name < matches[j].Name
	})

	return matches, nil
}

// AvailableCount returns the number of currently discoverable skills.
func (m *StemMatcher) AvailableCount() (int, error) {
	allSkills, err := m.discoverAll()
	if err != nil {
		return 0, err
	}
	return len(allSkills), nil
}

// Match analyzes the intent and returns skill names ordered by relevance (descending score).
// Returns an empty list (not an error) if no skills match.
func (m *StemMatcher) Match(intent string) ([]string, error) {
	matches, err := m.MatchWithScores(intent)
	if err != nil {
		return nil, err
	}
	result := make([]string, len(matches))
	for i, mr := range matches {
		result[i] = mr.Name
	}
	return result, nil
}

// tokenize splits text into lowercase keyword tokens.
// Splits on whitespace, punctuation, and hyphens.
func tokenize(text string) []string {
	text = strings.ToLower(text)
	tokens := strings.FieldsFunc(text, func(r rune) bool {
		return unicode.IsSpace(r) || r == '-' || r == '_' || unicode.IsPunct(r)
	})
	// Deduplicate
	seen := make(map[string]struct{}, len(tokens))
	result := make([]string, 0, len(tokens))
	for _, t := range tokens {
		if _, ok := seen[t]; !ok && len(t) > 0 {
			seen[t] = struct{}{}
			result = append(result, t)
		}
	}
	return result
}

// overlapScore calculates the keyword overlap ratio.
// Score = |intersection(intentKeywords, skillKeywords)| / |intentKeywords|
func overlapScore(intentKeywords, skillKeywords []string) float64 {
	skillSet := make(map[string]struct{}, len(skillKeywords))
	for _, kw := range skillKeywords {
		skillSet[kw] = struct{}{}
	}

	var overlap int
	for _, kw := range intentKeywords {
		if _, ok := skillSet[kw]; ok {
			overlap++
		}
	}

	return float64(overlap) / float64(len(intentKeywords))
}
