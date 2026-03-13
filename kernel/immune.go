package kernel

import (
	"bufio"
	"encoding/json"
	"maps"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// BehaviorSample records a single agent execution's behavioral summary.
type BehaviorSample struct {
	AgentTemplate string         `json:"agent_template"`
	SyscallCounts map[string]int `json:"syscall_counts"`
	DeviceAccess  []string       `json:"device_access"`
	TokensUsed    int            `json:"tokens_used"`
	TokenRate     float64        `json:"token_rate"`
	DurationMs    int64          `json:"duration_ms"`
	ExitNormal    bool           `json:"exit_normal"`
	Timestamp     time.Time      `json:"timestamp"`
}

// NormalProfile describes the normal behavior range for an agent template.
// Computed from historical BehaviorSample data using statistical methods.
type NormalProfile struct {
	AgentTemplate    string             `json:"agent_template"`
	SampleCount      int                `json:"sample_count"`
	SyscallMean      map[string]float64 `json:"syscall_mean"`
	SyscallStdDev    map[string]float64 `json:"syscall_std_dev"`
	TokenRateMean    float64            `json:"token_rate_mean"`
	TokenRateStdDev  float64            `json:"token_rate_std_dev"`
	DurationMeanMs   float64            `json:"duration_mean_ms"`
	DurationStdDevMs float64            `json:"duration_std_dev_ms"`
	LastUpdated      time.Time          `json:"last_updated"`
}

// MinSamplesForProfile is the minimum number of samples required to build a NormalProfile.
const MinSamplesForProfile = 5

// ComputeProfile builds a NormalProfile from historical behavior samples.
// Returns nil if fewer than MinSamplesForProfile samples are provided.
func ComputeProfile(agentTemplate string, samples []BehaviorSample) *NormalProfile {
	if len(samples) < MinSamplesForProfile {
		return nil
	}

	n := float64(len(samples))

	// Aggregate syscall names across all samples
	syscallSums := make(map[string]float64)
	for _, s := range samples {
		for name, count := range s.SyscallCounts {
			syscallSums[name] += float64(count)
		}
	}

	// Compute syscall means
	syscallMean := make(map[string]float64, len(syscallSums))
	for name, sum := range syscallSums {
		syscallMean[name] = sum / n
	}

	// Compute syscall standard deviations
	syscallStdDev := make(map[string]float64, len(syscallMean))
	for name, mean := range syscallMean {
		var sumSqDiff float64
		for _, s := range samples {
			diff := float64(s.SyscallCounts[name]) - mean
			sumSqDiff += diff * diff
		}
		syscallStdDev[name] = math.Sqrt(sumSqDiff / n)
	}

	// Compute token rate mean and stddev
	var tokenRateSum float64
	for _, s := range samples {
		tokenRateSum += s.TokenRate
	}
	tokenRateMean := tokenRateSum / n

	var tokenRateSqSum float64
	for _, s := range samples {
		diff := s.TokenRate - tokenRateMean
		tokenRateSqSum += diff * diff
	}
	tokenRateStdDev := math.Sqrt(tokenRateSqSum / n)

	// Compute duration mean and stddev
	var durationSum float64
	for _, s := range samples {
		durationSum += float64(s.DurationMs)
	}
	durationMean := durationSum / n

	var durationSqSum float64
	for _, s := range samples {
		diff := float64(s.DurationMs) - durationMean
		durationSqSum += diff * diff
	}
	durationStdDev := math.Sqrt(durationSqSum / n)

	return &NormalProfile{
		AgentTemplate:    agentTemplate,
		SampleCount:      len(samples),
		SyscallMean:      syscallMean,
		SyscallStdDev:    syscallStdDev,
		TokenRateMean:    tokenRateMean,
		TokenRateStdDev:  tokenRateStdDev,
		DurationMeanMs:   durationMean,
		DurationStdDevMs: durationStdDev,
		LastUpdated:      time.Now(),
	}
}

// --- ImmuneStore: persistence engine ---

// ImmuneStore manages behavior sample persistence and NormalProfile read/write.
// Data is stored in baseDir (typically $PROJECT/.rnix/immune/).
type ImmuneStore struct {
	mu      sync.Mutex
	baseDir string
}

// NewImmuneStore creates a new ImmuneStore rooted at baseDir.
func NewImmuneStore(baseDir string) *ImmuneStore {
	return &ImmuneStore{baseDir: baseDir}
}

// RecordSample appends a behavior sample to the agent template's JSONL file.
func (s *ImmuneStore) RecordSample(sample BehaviorSample) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.baseDir, 0o755); err != nil {
		return err
	}

	filePath := filepath.Join(s.baseDir, sample.AgentTemplate+".jsonl")
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	data, err := json.Marshal(sample)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = f.Write(data)
	return err
}

// GetSamples reads all historical behavior samples for the given agent template.
// Returns an empty slice if no file exists.
func (s *ImmuneStore) GetSamples(agentTemplate string) ([]BehaviorSample, error) {
	filePath := filepath.Join(s.baseDir, agentTemplate+".jsonl")
	f, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []BehaviorSample{}, nil
		}
		return nil, err
	}
	defer f.Close()

	var samples []BehaviorSample
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var sample BehaviorSample
		if err := json.Unmarshal(line, &sample); err != nil {
			return nil, err
		}
		samples = append(samples, sample)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if samples == nil {
		samples = []BehaviorSample{}
	}
	return samples, nil
}

// SaveProfile saves a NormalProfile to disk as a complete JSON file.
func (s *ImmuneStore) SaveProfile(profile *NormalProfile) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	profileDir := filepath.Join(s.baseDir, "profiles")
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		return err
	}

	filePath := filepath.Join(profileDir, profile.AgentTemplate+"-profile.json")
	data, err := json.Marshal(profile)
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, data, 0o644)
}

// LoadProfile loads a NormalProfile from disk.
// Returns nil, nil if the file does not exist (not an error).
func (s *ImmuneStore) LoadProfile(agentTemplate string) (*NormalProfile, error) {
	filePath := filepath.Join(s.baseDir, "profiles", agentTemplate+"-profile.json")
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var profile NormalProfile
	if err := json.Unmarshal(data, &profile); err != nil {
		return nil, err
	}
	return &profile, nil
}

// LoadAllProfiles loads all saved NormalProfiles from the profiles directory.
func (s *ImmuneStore) LoadAllProfiles() (map[string]*NormalProfile, error) {
	profileDir := filepath.Join(s.baseDir, "profiles")
	entries, err := os.ReadDir(profileDir)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]*NormalProfile{}, nil
		}
		return nil, err
	}

	profiles := make(map[string]*NormalProfile)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if len(name) <= len("-profile.json") {
			continue
		}
		if name[len(name)-len("-profile.json"):] != "-profile.json" {
			continue
		}

		filePath := filepath.Join(profileDir, name)
		data, err := os.ReadFile(filePath)
		if err != nil {
			return nil, err
		}
		var profile NormalProfile
		if err := json.Unmarshal(data, &profile); err != nil {
			return nil, err
		}
		profiles[profile.AgentTemplate] = &profile
	}
	return profiles, nil
}

// --- BehaviorCollector: runtime behavior aggregation ---

// BehaviorCollector monitors a single process and aggregates SyscallEvent data.
type BehaviorCollector struct {
	mu            sync.Mutex
	pid           types.PID
	agentTemplate string
	startTime     time.Time
	syscallCounts map[string]int
	deviceAccess  map[string]struct{}
}

// NewBehaviorCollector creates a new BehaviorCollector for the given process.
func NewBehaviorCollector(pid types.PID, agentTemplate string) *BehaviorCollector {
	return &BehaviorCollector{
		pid:           pid,
		agentTemplate: agentTemplate,
		startTime:     time.Now(),
		syscallCounts: make(map[string]int),
		deviceAccess:  make(map[string]struct{}),
	}
}

// Observe processes a SyscallEvent and updates behavior statistics.
func (c *BehaviorCollector) Observe(event types.SyscallEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.syscallCounts[event.Syscall]++

	// Extract device path from Args if present
	if args := event.Args; args != nil {
		if path, ok := args["path"].(string); ok && path != "" {
			c.deviceAccess[path] = struct{}{}
		}
		if device, ok := args["device"].(string); ok && device != "" {
			c.deviceAccess[device] = struct{}{}
		}
	}
}

// Finalize produces the final BehaviorSample when the process exits.
func (c *BehaviorCollector) Finalize(tokensUsed int, exitNormal bool) BehaviorSample {
	c.mu.Lock()
	defer c.mu.Unlock()

	duration := time.Since(c.startTime)
	durationMs := duration.Milliseconds()
	if durationMs == 0 && duration > 0 {
		durationMs = 1
	}

	var tokenRate float64
	if durationMs > 0 {
		tokenRate = float64(tokensUsed) / (float64(durationMs) / 1000.0)
	}

	// Convert device access set to sorted slice
	devices := make([]string, 0, len(c.deviceAccess))
	for d := range c.deviceAccess {
		devices = append(devices, d)
	}

	// Copy syscall counts
	counts := make(map[string]int, len(c.syscallCounts))
	maps.Copy(counts, c.syscallCounts)

	return BehaviorSample{
		AgentTemplate: c.agentTemplate,
		SyscallCounts: counts,
		DeviceAccess:  devices,
		TokensUsed:    tokensUsed,
		TokenRate:     tokenRate,
		DurationMs:    durationMs,
		ExitNormal:    exitNormal,
		Timestamp:     time.Now(),
	}
}

// --- ImmuneDaemon: core monitoring engine ---

// ImmuneDaemon is the security monitoring daemon.
// It passively monitors agent behavior through event-driven hooks (no polling).
type ImmuneDaemon struct {
	mu         sync.RWMutex
	store      *ImmuneStore
	profiles   map[string]*NormalProfile
	collectors map[types.PID]*BehaviorCollector
	running    bool
	stopCh     chan struct{}
}

// NewImmuneDaemon creates a new ImmuneDaemon backed by the given store.
func NewImmuneDaemon(store *ImmuneStore) *ImmuneDaemon {
	return &ImmuneDaemon{
		store:      store,
		profiles:   make(map[string]*NormalProfile),
		collectors: make(map[types.PID]*BehaviorCollector),
		stopCh:     make(chan struct{}),
	}
}

// Start initializes the ImmuneDaemon and loads existing NormalProfiles.
func (d *ImmuneDaemon) Start() error {
	if d == nil {
		return nil
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if d.running {
		return nil
	}

	// Load existing profiles from disk
	profiles, err := d.store.LoadAllProfiles()
	if err != nil {
		return err
	}
	d.profiles = profiles
	d.running = true
	return nil
}

// Stop shuts down the ImmuneDaemon.
func (d *ImmuneDaemon) Stop() {
	if d == nil {
		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.running {
		return
	}
	d.running = false
	close(d.stopCh)
}

// OnProcessStart creates a BehaviorCollector for the new process.
func (d *ImmuneDaemon) OnProcessStart(pid types.PID, agentTemplate string) {
	if d == nil {
		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.running {
		return
	}

	d.collectors[pid] = NewBehaviorCollector(pid, agentTemplate)
}

// OnSyscallEvent forwards a SyscallEvent to the corresponding BehaviorCollector.
func (d *ImmuneDaemon) OnSyscallEvent(pid types.PID, event types.SyscallEvent) {
	if d == nil {
		return
	}

	d.mu.RLock()
	collector, ok := d.collectors[pid]
	d.mu.RUnlock()

	if ok {
		collector.Observe(event)
	}
}

// OnProcessExit finalizes the behavior sample and updates the NormalProfile.
func (d *ImmuneDaemon) OnProcessExit(pid types.PID, tokensUsed int, exitNormal bool) {
	if d == nil {
		return
	}

	d.mu.Lock()
	collector, ok := d.collectors[pid]
	if ok {
		delete(d.collectors, pid)
	}
	d.mu.Unlock()

	if !ok {
		return
	}

	sample := collector.Finalize(tokensUsed, exitNormal)

	_ = d.store.RecordSample(sample)
	d.updateProfile(sample.AgentTemplate)
}

// updateProfile recomputes and saves the NormalProfile for an agent template.
func (d *ImmuneDaemon) updateProfile(agentTemplate string) {
	samples, err := d.store.GetSamples(agentTemplate)
	if err != nil {
		return
	}

	profile := ComputeProfile(agentTemplate, samples)
	if profile == nil {
		return
	}

	if err := d.store.SaveProfile(profile); err != nil {
		return
	}

	d.mu.Lock()
	d.profiles[agentTemplate] = profile
	d.mu.Unlock()
}

// GetProfile returns the NormalProfile for the given agent template, or nil if none exists.
func (d *ImmuneDaemon) GetProfile(agentTemplate string) *NormalProfile {
	if d == nil {
		return nil
	}

	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.profiles[agentTemplate]
}

// GetAllProfiles returns a copy of all established NormalProfiles.
func (d *ImmuneDaemon) GetAllProfiles() map[string]*NormalProfile {
	if d == nil {
		return nil
	}

	d.mu.RLock()
	defer d.mu.RUnlock()

	result := make(map[string]*NormalProfile, len(d.profiles))
	maps.Copy(result, d.profiles)
	return result
}

// ActivePIDs returns the PIDs of all processes currently being monitored.
func (d *ImmuneDaemon) ActivePIDs() []types.PID {
	if d == nil {
		return nil
	}

	d.mu.RLock()
	defer d.mu.RUnlock()

	pids := make([]types.PID, 0, len(d.collectors))
	for pid := range d.collectors {
		pids = append(pids, pid)
	}
	return pids
}

// IsRunning reports whether the ImmuneDaemon is currently running.
func (d *ImmuneDaemon) IsRunning() bool {
	if d == nil {
		return false
	}

	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.running
}
