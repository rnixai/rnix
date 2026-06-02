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
// StemMatcher.MatchWithScores. The original match scores are used as the
// match factor (normalized to [0,1] by dividing by the top score).
// agentName is the stem agent template name for reputation lookup.
// Returns a new slice in reranked order. Original slice is not modified.
func reRankSkills(
	matchedSkills []StemMatchResult,
	agentName string,
	synergyMatrix *SynergyMatrix,
	reputationStore *ReputationStore,
	weights *StemReRankWeights,
) []StemMatchResult {
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

	reputationBoost := 0.5
	if reputationStore != nil && agentName != "" {
		if summary, err := reputationStore.GetSummary(agentName); err == nil && summary != nil {
			reputationBoost = summary.Score
		}
	}

	type scored struct {
		result StemMatchResult
		rank   float64
	}
	entries := make([]scored, len(matchedSkills))
	for i, skill := range matchedSkills {
		matchNorm := 1.0 - float64(i)/(n)

		synergyBoost := 0.5
		if cs, ok := synergyLookup[skill.Name]; ok {
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
		entries[i] = scored{result: skill, rank: total}
	}

	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].rank > entries[j].rank
	})

	result := make([]StemMatchResult, len(entries))
	for i, e := range entries {
		result[i] = e.result
	}
	return result
}
