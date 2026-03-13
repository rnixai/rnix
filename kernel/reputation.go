package kernel

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ReputationRecord records a single SLA evaluation for persistence.
type ReputationRecord struct {
	SLAResult *SLAResult `json:"sla_result"`
	Timestamp time.Time  `json:"timestamp"`
}

// ReputationStore manages agent reputation data via JSON Lines files.
type ReputationStore struct {
	mu      sync.Mutex
	baseDir string // $PROJECT/.rnix/reputation/
}

// NewReputationStore creates a new ReputationStore rooted at baseDir.
func NewReputationStore(baseDir string) *ReputationStore {
	return &ReputationStore{baseDir: baseDir}
}

// RecordResult appends an SLA evaluation result to the agent's reputation file.
// The file is created (with parent directories) if it does not exist.
// Format: JSON Lines (one JSON object per line).
func (rs *ReputationStore) RecordResult(agentName string, result *SLAResult) error {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	if err := os.MkdirAll(rs.baseDir, 0o755); err != nil {
		return err
	}

	filePath := filepath.Join(rs.baseDir, agentName+".json")
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	record := ReputationRecord{
		SLAResult: result,
		Timestamp: time.Now(),
	}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = f.Write(data)
	return err
}

// GetHistory reads the reputation history for the given agent.
// Returns an empty (non-nil) slice if no file exists.
func (rs *ReputationStore) GetHistory(agentName string) ([]ReputationRecord, error) {
	filePath := filepath.Join(rs.baseDir, agentName+".json")
	f, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []ReputationRecord{}, nil
		}
		return nil, err
	}
	defer f.Close()

	var records []ReputationRecord
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec ReputationRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			return nil, err
		}
		records = append(records, rec)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if records == nil {
		records = []ReputationRecord{}
	}
	return records, nil
}

// ReputationSummary holds a computed reputation summary for an agent.
type ReputationSummary struct {
	AgentName     string  `json:"agent_name"`
	Score         float64 `json:"score"`           // 综合声誉分 0.0~1.0
	SuccessRate   float64 `json:"success_rate"`     // SLA 通过率
	AvgTokens     int     `json:"avg_tokens"`       // 平均 token 消耗
	AvgDurationMs int64   `json:"avg_duration_ms"`  // 平均执行时长
	TotalRecords  int     `json:"total_records"`    // 总评估次数
	RecentTrend   string  `json:"recent_trend"`     // "improving" | "declining" | "stable"
}

// GetSummary computes a reputation summary for the given agent.
// Returns a default neutral summary (Score=0.5) if no records exist.
func (rs *ReputationStore) GetSummary(agentName string) (*ReputationSummary, error) {
	records, err := rs.GetHistory(agentName)
	if err != nil {
		return nil, err
	}

	if len(records) == 0 {
		return &ReputationSummary{
			AgentName:   agentName,
			Score:       0.5,
			RecentTrend: "stable",
		}, nil
	}

	total := len(records)
	passedCount := 0
	totalTokens := 0
	maxTokens := 0
	var totalDuration int64

	for _, rec := range records {
		if rec.SLAResult != nil {
			if rec.SLAResult.Passed {
				passedCount++
			}
			totalTokens += rec.SLAResult.TokensUsed
			if rec.SLAResult.TokensUsed > maxTokens {
				maxTokens = rec.SLAResult.TokensUsed
			}
			totalDuration += rec.SLAResult.DurationMs
		}
	}

	successRate := float64(passedCount) / float64(total)
	avgTokens := totalTokens / total
	avgDuration := totalDuration / int64(total)

	// Token efficiency: 1.0 - (avgTokens / maxTokens), normalized to [0, 1]
	tokenEfficiency := 1.0
	if maxTokens > 0 && avgTokens != maxTokens {
		tokenEfficiency = 1.0 - float64(avgTokens)/float64(maxTokens)
	}

	// Score = 0.7 * SuccessRate + 0.3 * TokenEfficiency
	score := 0.7*successRate + 0.3*tokenEfficiency

	// RecentTrend: compare last 5 records' pass rate vs overall
	recentCount := min(5, total)
	recentPassed := 0
	for i := total - recentCount; i < total; i++ {
		if records[i].SLAResult != nil && records[i].SLAResult.Passed {
			recentPassed++
		}
	}
	recentRate := float64(recentPassed) / float64(recentCount)

	trend := "stable"
	if recentRate > successRate+0.1 {
		trend = "improving"
	} else if recentRate < successRate-0.1 {
		trend = "declining"
	}

	return &ReputationSummary{
		AgentName:     agentName,
		Score:         score,
		SuccessRate:   successRate,
		AvgTokens:     avgTokens,
		AvgDurationMs: avgDuration,
		TotalRecords:  total,
		RecentTrend:   trend,
	}, nil
}

// ListAgents returns agent names from all .json files in the reputation store directory.
// Returns an empty slice if the directory does not exist.
func (rs *ReputationStore) ListAgents() ([]string, error) {
	entries, err := os.ReadDir(rs.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}

	var agents []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if agentName, ok := strings.CutSuffix(name, ".json"); ok {
			agents = append(agents, agentName)
		}
	}
	if agents == nil {
		agents = []string{}
	}
	return agents, nil
}

// GetAllSummaries returns reputation summaries for all known agents, sorted by Score descending.
func (rs *ReputationStore) GetAllSummaries() ([]ReputationSummary, error) {
	agents, err := rs.ListAgents()
	if err != nil {
		return nil, err
	}

	summaries := make([]ReputationSummary, 0, len(agents))
	for _, name := range agents {
		summary, err := rs.GetSummary(name)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, *summary)
	}

	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].Score > summaries[j].Score
	})

	return summaries, nil
}

// SelectBest returns the agent name with the highest reputation score from the given candidates.
// If all candidates have the same default score, the first candidate is returned (deterministic).
// Returns an error if the candidates list is empty.
func (rs *ReputationStore) SelectBest(candidates []string) (string, error) {
	if len(candidates) == 0 {
		return "", fmt.Errorf("reputation: empty candidates list")
	}

	bestName := candidates[0]
	bestScore := -1.0

	for _, name := range candidates {
		summary, err := rs.GetSummary(name)
		if err != nil {
			continue
		}
		if summary.Score > bestScore {
			bestScore = summary.Score
			bestName = name
		}
	}

	return bestName, nil
}
