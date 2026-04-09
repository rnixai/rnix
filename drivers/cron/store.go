package cron

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/rnixai/rnix/internal/xsync"
)

// CronJob represents a scheduled cron job.
type CronJob struct {
	ID        string    `json:"id"`
	Schedule  string    `json:"schedule"`
	Prompt    string    `json:"prompt"`
	Agent     string    `json:"agent"`
	Durable   bool      `json:"durable"`
	CreatedAt time.Time `json:"created_at"`
	NextRun   time.Time `json:"next_run"`
	LastRun   time.Time `json:"last_run,omitzero"`
	RunCount  int       `json:"run_count"`
}

// JobStore manages cron job persistence and lookup.
type JobStore struct {
	jobs    *xsync.SyncMap[string, *CronJob]
	counter atomic.Int64
	dataDir string
}

// MaxActiveJobs is the maximum number of concurrent cron jobs.
const MaxActiveJobs = 50

// NonDurableExpiry is how long non-durable jobs live before auto-expiry.
const NonDurableExpiry = 7 * 24 * time.Hour

// NewJobStore creates a new job store.
// dataDir is the path for persisting durable jobs (e.g., ".rnix/data/cron").
func NewJobStore(dataDir string) *JobStore {
	return &JobStore{
		jobs:    xsync.NewSyncMap[string, *CronJob](),
		dataDir: dataDir,
	}
}

// Count returns the number of active jobs.
func (s *JobStore) Count() int {
	count := 0
	s.jobs.Range(func(_ string, _ *CronJob) bool {
		count++
		return true
	})
	return count
}

// Add creates a new cron job and persists if durable.
func (s *JobStore) Add(schedule, prompt, agent string, durable bool, nextRun time.Time) (*CronJob, error) {
	if s.Count() >= MaxActiveJobs {
		return nil, fmt.Errorf("maximum %d active cron jobs reached", MaxActiveJobs)
	}

	id := fmt.Sprintf("cron-%d", s.counter.Add(1))
	job := &CronJob{
		ID:        id,
		Schedule:  schedule,
		Prompt:    prompt,
		Agent:     agent,
		Durable:   durable,
		CreatedAt: time.Now(),
		NextRun:   nextRun,
	}

	s.jobs.Store(id, job)

	if durable {
		if err := s.persist(); err != nil {
			return job, fmt.Errorf("persist failed: %w", err)
		}
	}

	return job, nil
}

// Get returns a job by ID.
func (s *JobStore) Get(id string) *CronJob {
	job, ok := s.jobs.Load(id)
	if !ok {
		return nil
	}
	return job
}

// Delete removes a job by ID.
func (s *JobStore) Delete(id string) bool {
	_, ok := s.jobs.Load(id)
	if !ok {
		return false
	}
	s.jobs.Delete(id)
	// Best-effort persist
	_ = s.persist()
	return true
}

// List returns all active jobs.
func (s *JobStore) List() []*CronJob {
	var result []*CronJob
	s.jobs.Range(func(_ string, job *CronJob) bool {
		result = append(result, job)
		return true
	})
	return result
}

// UpdateNextRun updates the next run time and increments run count.
func (s *JobStore) UpdateNextRun(id string, nextRun time.Time) {
	job, ok := s.jobs.Load(id)
	if !ok {
		return
	}
	job.LastRun = time.Now()
	job.NextRun = nextRun
	job.RunCount++
	s.jobs.Store(id, job)
	if job.Durable {
		_ = s.persist()
	}
}

// PruneExpired removes non-durable jobs older than NonDurableExpiry.
func (s *JobStore) PruneExpired() int {
	now := time.Now()
	var expired []string
	s.jobs.Range(func(id string, job *CronJob) bool {
		if !job.Durable && now.Sub(job.CreatedAt) > NonDurableExpiry {
			expired = append(expired, id)
		}
		return true
	})
	for _, id := range expired {
		s.jobs.Delete(id)
	}
	return len(expired)
}

// Load reads durable jobs from disk.
func (s *JobStore) Load() error {
	path := filepath.Join(s.dataDir, "jobs.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read jobs file: %w", err)
	}

	var jobs []*CronJob
	if err := json.Unmarshal(data, &jobs); err != nil {
		return fmt.Errorf("parse jobs file: %w", err)
	}

	var maxID int64
	for _, job := range jobs {
		s.jobs.Store(job.ID, job)
		// Extract numeric suffix to restore counter
		var num int64
		if _, err := fmt.Sscanf(job.ID, "cron-%d", &num); err == nil && num > maxID {
			maxID = num
		}
	}
	s.counter.Store(maxID)

	return nil
}

// persist writes all durable jobs to disk.
func (s *JobStore) persist() error {
	if s.dataDir == "" {
		return nil
	}

	var durableJobs []*CronJob
	s.jobs.Range(func(_ string, job *CronJob) bool {
		if job.Durable {
			durableJobs = append(durableJobs, job)
		}
		return true
	})

	if err := os.MkdirAll(s.dataDir, 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(durableJobs, "", "  ")
	if err != nil {
		return err
	}

	path := filepath.Join(s.dataDir, "jobs.json")
	return os.WriteFile(path, data, 0o644)
}
