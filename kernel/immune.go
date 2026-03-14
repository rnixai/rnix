package kernel

import (
	"bufio"
	"encoding/json"
	"fmt"
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

// SaveThreat appends a threat signature to the threat memory (threats.jsonl).
func (s *ImmuneStore) SaveThreat(sig ThreatSignature) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.baseDir, 0o755); err != nil {
		return err
	}

	filePath := filepath.Join(s.baseDir, "threats.jsonl")
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	data, err := json.Marshal(sig)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = f.Write(data)
	return err
}

// LoadThreats loads all threat signatures from the threat memory file.
// Returns an empty slice if the file does not exist.
func (s *ImmuneStore) LoadThreats() ([]ThreatSignature, error) {
	filePath := filepath.Join(s.baseDir, "threats.jsonl")
	f, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []ThreatSignature{}, nil
		}
		return nil, err
	}
	defer f.Close()

	var threats []ThreatSignature
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var sig ThreatSignature
		if err := json.Unmarshal(line, &sig); err != nil {
			return nil, err
		}
		threats = append(threats, sig)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if threats == nil {
		threats = []ThreatSignature{}
	}
	return threats, nil
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

// GetSyscallCount returns the current cumulative count for the given syscall name.
func (c *BehaviorCollector) GetSyscallCount(syscallName string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.syscallCounts[syscallName]
}

// GetAgentTemplate returns the agent template name for this collector.
func (c *BehaviorCollector) GetAgentTemplate() string {
	return c.agentTemplate
}

// --- Anomaly Detection Types (Story 22.2) ---

// AnomalyType enumerates the kinds of anomalies the immune system can detect.
type AnomalyType string

const (
	AnomalySyscallFreq  AnomalyType = "syscall_freq"  // syscall invocation frequency anomaly
	AnomalyTokenRate    AnomalyType = "token_rate"     // token consumption rate anomaly
	AnomalyDeviceAccess AnomalyType = "device_access"  // unexpected device access anomaly
)

// AnomalyAlert records a single anomaly detection event.
type AnomalyAlert struct {
	PID           types.PID   `json:"pid"`
	AgentTemplate string      `json:"agent_template"`
	Type          AnomalyType `json:"type"`
	Detail        string      `json:"detail"`    // human-readable description
	Deviation     float64     `json:"deviation"` // deviation multiplier (actual / mean)
	Timestamp     time.Time   `json:"timestamp"`
}

// ThreatSignature describes a known anomalous behavior pattern (antibody memory).
type ThreatSignature struct {
	ID            string      `json:"id"`
	Type          AnomalyType `json:"type"`
	AgentTemplate string      `json:"agent_template"`
	Metric        string      `json:"metric"`    // specific metric name (e.g., syscall name "Open", or "token_rate")
	Threshold     float64     `json:"threshold"` // deviation multiplier that triggered this signature
	CreatedAt     time.Time   `json:"created_at"`
}

// --- AnomalyDetector: statistical anomaly detection engine (Story 22.2) ---

// DefaultDeviationThreshold is the default deviation threshold (standard deviation multiplier).
// When actual value > mean + threshold * stddev, the behavior is considered anomalous.
// 3.0 corresponds to 99.7% confidence interval of a normal distribution.
const DefaultDeviationThreshold = 3.0

// AnomalyDetector detects behavioral anomalies by comparing runtime metrics against NormalProfiles.
type AnomalyDetector struct {
	threshold float64 // standard deviation multiplier
}

// NewAnomalyDetector creates a new AnomalyDetector with the given threshold.
func NewAnomalyDetector(threshold float64) *AnomalyDetector {
	return &AnomalyDetector{threshold: threshold}
}

// CheckSyscallAnomaly checks whether the current syscall count is anomalous.
// Returns nil if normal or if profile is nil / has no data for this syscall.
func (d *AnomalyDetector) CheckSyscallAnomaly(
	pid types.PID,
	agentTemplate string,
	syscallName string,
	currentCount int,
	profile *NormalProfile,
) *AnomalyAlert {
	if profile == nil {
		return nil
	}

	mean, ok := profile.SyscallMean[syscallName]
	if !ok || mean == 0 {
		return nil
	}

	stddev := profile.SyscallStdDev[syscallName]
	upperBound := mean + d.threshold*stddev
	cur := float64(currentCount)

	if cur <= upperBound {
		return nil
	}

	deviation := cur / mean
	return &AnomalyAlert{
		PID:           pid,
		AgentTemplate: agentTemplate,
		Type:          AnomalySyscallFreq,
		Detail:        fmt.Sprintf("%s count %.0f exceeds baseline %.1f by %.1fx", syscallName, cur, mean, deviation),
		Deviation:     deviation,
		Timestamp:     time.Now(),
	}
}

// CheckTokenRateAnomaly checks whether the current token rate is anomalous.
// Returns nil if normal or if profile is nil / has zero mean.
func (d *AnomalyDetector) CheckTokenRateAnomaly(
	pid types.PID,
	agentTemplate string,
	currentRate float64,
	profile *NormalProfile,
) *AnomalyAlert {
	if profile == nil {
		return nil
	}

	mean := profile.TokenRateMean
	if mean == 0 {
		return nil
	}

	stddev := profile.TokenRateStdDev
	upperBound := mean + d.threshold*stddev

	if currentRate <= upperBound {
		return nil
	}

	deviation := currentRate / mean
	return &AnomalyAlert{
		PID:           pid,
		AgentTemplate: agentTemplate,
		Type:          AnomalyTokenRate,
		Detail:        fmt.Sprintf("token rate %.1f tok/s exceeds baseline %.1f tok/s by %.1fx", currentRate, mean, deviation),
		Deviation:     deviation,
		Timestamp:     time.Now(),
	}
}

// MatchThreat checks if the current behavior matches a known threat signature.
// Match criteria: same agent_template + same anomaly_type + same metric.
// Returns the first matching threat, or nil if no match.
func (d *AnomalyDetector) MatchThreat(
	agentTemplate string,
	anomalyType AnomalyType,
	metric string,
	threats []ThreatSignature,
) *ThreatSignature {
	for i := range threats {
		t := &threats[i]
		if t.AgentTemplate == agentTemplate && t.Type == anomalyType && t.Metric == metric {
			return t
		}
	}
	return nil
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

	// Story 22.2: anomaly detection and threat memory
	detector  *AnomalyDetector
	threats   []ThreatSignature
	alerts    map[types.PID]*AnomalyAlert
	suspendFn func(pid types.PID) error
}

// NewImmuneDaemon creates a new ImmuneDaemon backed by the given store.
func NewImmuneDaemon(store *ImmuneStore) *ImmuneDaemon {
	return &ImmuneDaemon{
		store:      store,
		profiles:   make(map[string]*NormalProfile),
		collectors: make(map[types.PID]*BehaviorCollector),
		alerts:     make(map[types.PID]*AnomalyAlert),
		stopCh:     make(chan struct{}),
	}
}

// SetDetector sets the anomaly detector engine.
func (d *ImmuneDaemon) SetDetector(detector *AnomalyDetector) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.detector = detector
}

// SetSuspendFunc sets the callback used to suspend (SIGPAUSE) a process.
// If nil, anomalies are recorded but processes are not suspended.
func (d *ImmuneDaemon) SetSuspendFunc(fn func(pid types.PID) error) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.suspendFn = fn
}

// Start initializes the ImmuneDaemon and loads existing NormalProfiles and threat signatures.
func (d *ImmuneDaemon) Start() error {
	if d == nil {
		return nil
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if d.running {
		return nil
	}

	// Initialize default anomaly detector if none set (Story 22.2)
	if d.detector == nil {
		d.detector = NewAnomalyDetector(DefaultDeviationThreshold)
	}

	// Load existing profiles from disk
	profiles, err := d.store.LoadAllProfiles()
	if err != nil {
		return err
	}
	d.profiles = profiles

	// Load threat signatures from disk (Story 22.2)
	threats, err := d.store.LoadThreats()
	if err != nil {
		// Non-fatal: continue without threat memory
		threats = []ThreatSignature{}
	}
	d.threats = threats

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

// OnSyscallEvent forwards a SyscallEvent to the corresponding BehaviorCollector
// and performs anomaly detection (Story 22.2).
func (d *ImmuneDaemon) OnSyscallEvent(pid types.PID, event types.SyscallEvent) {
	if d == nil {
		return
	}

	d.mu.RLock()
	collector, ok := d.collectors[pid]
	if !ok {
		d.mu.RUnlock()
		return
	}
	detector := d.detector
	agentTpl := collector.GetAgentTemplate()
	profile := d.profiles[agentTpl]
	threats := d.threats
	suspendFn := d.suspendFn
	d.mu.RUnlock()

	collector.Observe(event)

	// Skip anomaly detection if no detector or no profile
	if detector == nil || profile == nil {
		return
	}

	// Fast path: check threat signature match first
	if matched := detector.MatchThreat(agentTpl, AnomalySyscallFreq, event.Syscall, threats); matched != nil {
		alert := &AnomalyAlert{
			PID:           pid,
			AgentTemplate: agentTpl,
			Type:          AnomalySyscallFreq,
			Detail:        fmt.Sprintf("known threat: %s (signature %s)", event.Syscall, matched.ID),
			Deviation:     matched.Threshold,
			Timestamp:     time.Now(),
		}
		d.mu.Lock()
		d.alerts[pid] = alert
		d.mu.Unlock()
		if suspendFn != nil {
			_ = suspendFn(pid)
		}
		return
	}

	// Statistical anomaly detection: check syscall frequency
	currentCount := collector.GetSyscallCount(event.Syscall)
	alert := detector.CheckSyscallAnomaly(pid, agentTpl, event.Syscall, currentCount, profile)
	if alert == nil {
		return
	}

	// Anomaly detected: record alert, persist threat, suspend process
	d.mu.Lock()
	d.alerts[pid] = alert

	// Create and persist threat signature
	sig := ThreatSignature{
		ID:            fmt.Sprintf("threat-%d", time.Now().UnixNano()),
		Type:          AnomalySyscallFreq,
		AgentTemplate: agentTpl,
		Metric:        event.Syscall,
		Threshold:     alert.Deviation,
		CreatedAt:     time.Now(),
	}
	d.threats = append(d.threats, sig)
	d.mu.Unlock()

	// Persist threat to disk (synchronous, per 22.1 lesson)
	_ = d.store.SaveThreat(sig)

	// Suspend the process
	if suspendFn != nil {
		_ = suspendFn(pid)
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

// GetAlerts returns a copy of all active anomaly alerts (keyed by PID).
func (d *ImmuneDaemon) GetAlerts() map[types.PID]*AnomalyAlert {
	if d == nil {
		return nil
	}

	d.mu.RLock()
	defer d.mu.RUnlock()

	result := make(map[types.PID]*AnomalyAlert, len(d.alerts))
	maps.Copy(result, d.alerts)
	return result
}

// ClearAlert removes the anomaly alert for the given PID.
// The actual SIGRESUME is sent by the caller (CLI/IPC handler).
func (d *ImmuneDaemon) ClearAlert(pid types.PID) {
	if d == nil {
		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.alerts, pid)
}

// GetThreats returns a copy of all known threat signatures.
func (d *ImmuneDaemon) GetThreats() []ThreatSignature {
	if d == nil {
		return nil
	}

	d.mu.RLock()
	defer d.mu.RUnlock()

	result := make([]ThreatSignature, len(d.threats))
	copy(result, d.threats)
	return result
}
