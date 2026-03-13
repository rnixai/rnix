package kernel

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// SynergyComboKey represents a deterministic identifier for a set of skills.
// Skills are sorted alphabetically and joined with commas.
type SynergyComboKey string

// NewComboKey creates a deterministic combo key from a list of skill names.
// Names are sorted alphabetically and joined with commas, ensuring {A,B} and {B,A}
// produce the same key.
func NewComboKey(skills []string) SynergyComboKey {
	if len(skills) == 0 {
		return ""
	}
	sorted := make([]string, len(skills))
	copy(sorted, skills)
	sort.Strings(sorted)
	return SynergyComboKey(strings.Join(sorted, ","))
}

// SynergyRecord records a single skill combination execution result.
type SynergyRecord struct {
	ComboKey   SynergyComboKey `json:"combo_key"`
	Skills     []string        `json:"skills"`
	Passed     bool            `json:"passed"`
	TokensUsed int             `json:"tokens_used"`
	DurationMs int64           `json:"duration_ms"`
	Timestamp  time.Time       `json:"timestamp"`
}

// SynergyMatrix manages skill combination historical performance data.
// Data is persisted as JSON Lines in $PROJECT/.rnix/reputation/synergy-matrix.json.
type SynergyMatrix struct {
	mu       sync.Mutex
	filePath string
}

// NewSynergyMatrix creates a new SynergyMatrix using the given reputation directory.
func NewSynergyMatrix(reputationDir string) *SynergyMatrix {
	return &SynergyMatrix{
		filePath: filepath.Join(reputationDir, "synergy-matrix.json"),
	}
}

// RecordCombo appends a combo execution record to the synergy matrix file.
func (m *SynergyMatrix) RecordCombo(record SynergyRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	dir := filepath.Dir(m.filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	f, err := os.OpenFile(m.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = f.Write(data)
	return err
}

// GetAllRecords reads all historical records from the synergy matrix file.
// Returns an empty (non-nil) slice if no file exists.
func (m *SynergyMatrix) GetAllRecords() ([]SynergyRecord, error) {
	f, err := os.Open(m.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []SynergyRecord{}, nil
		}
		return nil, err
	}
	defer f.Close()

	var records []SynergyRecord
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec SynergyRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			return nil, err
		}
		records = append(records, rec)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if records == nil {
		records = []SynergyRecord{}
	}
	return records, nil
}

// ComboSummary holds aggregated statistics for a single skill combination.
type ComboSummary struct {
	ComboKey         SynergyComboKey `json:"combo_key"`
	Skills           []string        `json:"skills"`
	SuccessRate      float64         `json:"success_rate"`
	AvgTokens        int             `json:"avg_tokens"`
	TotalExecutions  int             `json:"total_executions"`
	AvgSoloRate      float64         `json:"avg_solo_rate"`
	TokenImprovement float64         `json:"token_improvement"`
	Recommended      bool            `json:"recommended"`
}

// GetComboSummaries computes aggregated statistics for all skill combinations.
// Results are sorted: recommended combos first, then by success rate descending.
func (m *SynergyMatrix) GetComboSummaries() ([]ComboSummary, error) {
	records, err := m.GetAllRecords()
	if err != nil {
		return nil, err
	}

	if len(records) == 0 {
		return []ComboSummary{}, nil
	}

	// Group records by ComboKey
	type comboGroup struct {
		skills     []string
		passed     int
		total      int
		totalToken int
	}
	groups := make(map[SynergyComboKey]*comboGroup)

	for _, rec := range records {
		g, ok := groups[rec.ComboKey]
		if !ok {
			g = &comboGroup{skills: rec.Skills}
			groups[rec.ComboKey] = g
		}
		g.total++
		g.totalToken += rec.TokensUsed
		if rec.Passed {
			g.passed++
		}
	}

	// Build solo baselines: success rate and avg tokens for single-skill combos
	soloRates := make(map[string]float64)
	soloTokens := make(map[string]int)
	for key, g := range groups {
		if !strings.Contains(string(key), ",") {
			// Single skill
			soloRates[string(key)] = float64(g.passed) / float64(g.total)
			soloTokens[string(key)] = g.totalToken / g.total
		}
	}

	// Compute summaries
	summaries := make([]ComboSummary, 0, len(groups))
	for key, g := range groups {
		successRate := float64(g.passed) / float64(g.total)
		avgTokens := g.totalToken / g.total

		summary := ComboSummary{
			ComboKey:        key,
			Skills:          g.skills,
			SuccessRate:     successRate,
			AvgTokens:       avgTokens,
			TotalExecutions: g.total,
		}

		// For multi-skill combos, compute comparison with solo baselines
		if strings.Contains(string(key), ",") {
			skillNames := strings.Split(string(key), ",")
			soloCount := 0
			soloRateSum := 0.0
			soloTokenSum := 0

			for _, s := range skillNames {
				if rate, ok := soloRates[s]; ok {
					soloRateSum += rate
					soloCount++
				}
				if tokens, ok := soloTokens[s]; ok {
					soloTokenSum += tokens
				}
			}

			if soloCount > 0 {
				summary.AvgSoloRate = soloRateSum / float64(soloCount)

				// Token improvement = (soloAvg - comboAvg) / soloAvg * 100
				soloAvgTokensF := float64(soloTokenSum) / float64(soloCount)
				if soloAvgTokensF > 0 {
					summary.TokenImprovement = (soloAvgTokensF - float64(avgTokens)) / soloAvgTokensF * 100
				}

				// Recommended if combo success rate > solo avg + 10%
				if successRate > summary.AvgSoloRate+0.10 {
					summary.Recommended = true
				}
			}
		}

		summaries = append(summaries, summary)
	}

	// Sort: recommended first, then by success rate descending
	sort.Slice(summaries, func(i, j int) bool {
		if summaries[i].Recommended != summaries[j].Recommended {
			return summaries[i].Recommended
		}
		return summaries[i].SuccessRate > summaries[j].SuccessRate
	})

	return summaries, nil
}
