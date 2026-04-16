package kernel

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// maxDiscoverResults is the maximum number of results returned by discover_skill.
const maxDiscoverResults = 5

type discoverResult struct {
	Query   string        `json:"query"`
	Matches []discoverHit `json:"matches"`
}

type discoverHit struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Score       float64 `json:"score"`
}

// discoverSkills searches all registered skills (deferred + loaded metadata)
// and returns the top maxDiscoverResults matches.
// Already-loaded skills are excluded from results.
func discoverSkills(proc *Process, query string) ([]discoverHit, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("empty query")
	}

	proc.mu.Lock()
	deferred := make([]DeferredSkillMeta, len(proc.DeferredSkills))
	copy(deferred, proc.DeferredSkills)
	loadedSkills := make([]string, len(proc.Skills))
	copy(loadedSkills, proc.Skills)
	proc.mu.Unlock()

	queryLower := strings.ToLower(query)

	var matches []discoverHit
	for _, ds := range deferred {
		if slices.Contains(loadedSkills, ds.Name) {
			continue
		}
		score := scoreSkillMatch(ds, queryLower)
		if score > 0 {
			matches = append(matches, discoverHit{
				Name:        ds.Name,
				Description: ds.Description,
				Score:       float64(score),
			})
		}
	}

	slices.SortFunc(matches, func(a, b discoverHit) int {
		if a.Score > b.Score {
			return -1
		}
		if a.Score < b.Score {
			return 1
		}
		return 0
	})

	if len(matches) > maxDiscoverResults {
		matches = matches[:maxDiscoverResults]
	}

	return matches, nil
}

// discoverResultJSON marshals a discover_skill result to JSON with error fallback.
func discoverResultJSON(query string, matches []discoverHit) string {
	result := discoverResult{Query: query, Matches: matches}
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return fmt.Sprintf(`{"query":%q,"matches":[],"error":"marshal failed: %v"}`, query, err)
	}
	return string(resultJSON)
}

// scoreSkillMatch scores a deferred skill against a query string using discrete scoring:
//   - Exact name match: 12 points
//   - Partial name match: 6 points
//   - SearchHint match: 4 points
//   - Description match: 2 points
func scoreSkillMatch(ds DeferredSkillMeta, queryLower string) int {
	if queryLower == "" {
		return 0
	}
	nameLower := strings.ToLower(ds.Name)
	descLower := strings.ToLower(ds.Description)
	hintLower := strings.ToLower(ds.SearchHint)

	score := 0

	if nameLower == queryLower {
		score += 12
	} else if strings.Contains(nameLower, queryLower) || strings.Contains(queryLower, nameLower) {
		score += 6
	}

	if hintLower != "" && strings.Contains(hintLower, queryLower) {
		score += 4
	}

	if strings.Contains(descLower, queryLower) {
		score += 2
	}

	return score
}
