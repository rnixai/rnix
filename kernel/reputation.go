package kernel

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
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
