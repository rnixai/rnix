package kernel

import (
	"sort"
	"strings"
)

// StemReRankWeights controls the weighting of match score, synergy history,
// and reputation when reranking stem-matched skills (Story 51.4 AC1).
type StemReRankWeights struct {
	MatchWeight      float64
	SynergyWeight    float64
	ReputationWeight float64
}

// DefaultStemReRankWeights returns the default rerank weights:
// match=0.5, synergy=0.3, reputation=0.2.
func DefaultStemReRankWeights() StemReRankWeights {
	return StemReRankWeights{
		MatchWeight:      0.5,
		SynergyWeight:    0.3,
		ReputationWeight: 0.2,
	}
}

// reRankSkills reorders matchedSkills using synergy history and reputation data.
// Skills are assumed pre-sorted by keyword overlap score (descending) from
// StemMatcher.Match. Positional scores are derived: first skill = 1.0,
// last skill = 1/n (where n = len(matchedSkills)), linearly interpolated.
// agentName is the stem agent template name for reputation lookup.
// Returns a new slice in reranked order. Original slice is not modified.
func reRankSkills(
	matchedSkills []string,
	agentName string,
	synergyMatrix *SynergyMatrix,
	reputationStore *ReputationStore,
	weights *StemReRankWeights,
) []string {
	if len(matchedSkills) <= 1 {
		return matchedSkills
	}

	w := weights
	if w == nil {
		def := DefaultStemReRankWeights()
		w = &def
	}

	n := float64(len(matchedSkills))

	// Build synergy lookup: solo skill name → ComboSummary
	synergyLookup := map[string]*ComboSummary{}
	if synergyMatrix != nil {
		if summaries, err := synergyMatrix.GetComboSummaries(); err == nil {
			for i := range summaries {
				s := &summaries[i]
				if len(s.Skills) == 1 || !strings.Contains(string(s.ComboKey), ",") {
					if len(s.Skills) > 0 {
						synergyLookup[s.Skills[0]] = s
					}
				}
			}
		}
	}

	// Get reputation score for the agent
	reputationBoost := 0.5
	if reputationStore != nil && agentName != "" {
		if summary, err := reputationStore.GetSummary(agentName); err == nil && summary != nil {
			reputationBoost = summary.Score
		}
	}

	type scored struct {
		name  string
		score float64
	}
	entries := make([]scored, len(matchedSkills))
	for i, skill := range matchedSkills {
		// Positional match score: first=1.0, last=1/n, linear interpolation
		matchNorm := 1.0 - float64(i)/(n)

		synergyBoost := 0.5
		if cs, ok := synergyLookup[skill]; ok {
			synergyBoost = cs.SuccessRate
			if cs.Recommended {
				synergyBoost += 0.1
			}
			if synergyBoost > 1 {
				synergyBoost = 1
			}
			if synergyBoost < 0 {
				synergyBoost = 0
			}
		}

		total := matchNorm*w.MatchWeight + synergyBoost*w.SynergyWeight + reputationBoost*w.ReputationWeight
		entries[i] = scored{name: skill, score: total}
	}

	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].score > entries[j].score
	})

	result := make([]string, len(entries))
	for i, e := range entries {
		result[i] = e.name
	}
	return result
}
