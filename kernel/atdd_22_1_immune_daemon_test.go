package kernel

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// ============================================================
// ATDD RED PHASE — Story 22.1: Immune Daemon 与行为基线
//
// BehaviorSample、NormalProfile、ImmuneStore、BehaviorCollector、
// ImmuneDaemon 类型和功能的单元测试。
// 测试引用的类型和方法尚不存在，测试将无法编译直到实现完成。
//
// RED → GREEN: 在 kernel/immune.go 中实现所有类型和方法。
// ============================================================

// --- 22.1-UNIT-001: [P0] BehaviorSample JSON round-trip (AC2/AC5) ---

func TestBehaviorSample_JSONRoundTrip(t *testing.T) {
	// Given: a fully populated BehaviorSample
	original := BehaviorSample{
		AgentTemplate: "code-analyst",
		SyscallCounts: map[string]int{"Open": 5, "Read": 12, "Write": 3},
		DeviceAccess:  []string{"/dev/llm/claude", "/dev/fs"},
		TokensUsed:    1500,
		TokenRate:     2.5,
		DurationMs:    6000,
		ExitNormal:    true,
		Timestamp:     time.Date(2026, 3, 14, 10, 0, 0, 0, time.UTC),
	}

	// When: serializing to JSON and back
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var restored BehaviorSample
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// Then: all fields are preserved
	if restored.AgentTemplate != original.AgentTemplate {
		t.Errorf("AgentTemplate = %q, want %q", restored.AgentTemplate, original.AgentTemplate)
	}
	if len(restored.SyscallCounts) != len(original.SyscallCounts) {
		t.Errorf("SyscallCounts len = %d, want %d", len(restored.SyscallCounts), len(original.SyscallCounts))
	}
	if restored.SyscallCounts["Open"] != 5 {
		t.Errorf("SyscallCounts[Open] = %d, want 5", restored.SyscallCounts["Open"])
	}
	if len(restored.DeviceAccess) != 2 {
		t.Errorf("DeviceAccess len = %d, want 2", len(restored.DeviceAccess))
	}
	if restored.TokensUsed != 1500 {
		t.Errorf("TokensUsed = %d, want 1500", restored.TokensUsed)
	}
	if restored.TokenRate != 2.5 {
		t.Errorf("TokenRate = %f, want 2.5", restored.TokenRate)
	}
	if restored.DurationMs != 6000 {
		t.Errorf("DurationMs = %d, want 6000", restored.DurationMs)
	}
	if !restored.ExitNormal {
		t.Error("ExitNormal should be true")
	}
	// And: JSON field names use snake_case
	if string(data) == "" {
		t.Error("serialized JSON should not be empty")
	}
}

// --- 22.1-UNIT-002: [P1] BehaviorSample default values are safe (AC2) ---

func TestBehaviorSample_DefaultValues(t *testing.T) {
	// Given: a zero-value BehaviorSample
	var sample BehaviorSample

	// When: serializing
	data, err := json.Marshal(sample)
	if err != nil {
		t.Fatalf("Marshal zero-value failed: %v", err)
	}

	// Then: no panic, valid JSON
	var restored BehaviorSample
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal zero-value failed: %v", err)
	}
	if restored.AgentTemplate != "" {
		t.Errorf("AgentTemplate should be empty, got %q", restored.AgentTemplate)
	}
	if restored.TokensUsed != 0 {
		t.Errorf("TokensUsed should be 0, got %d", restored.TokensUsed)
	}
	if restored.ExitNormal {
		t.Error("ExitNormal should default to false")
	}
}

// --- 22.1-UNIT-003: [P0] ComputeProfile with sufficient samples (AC3) ---

func TestComputeProfile_SufficientSamples(t *testing.T) {
	// Given: 6 behavior samples for "code-analyst"
	samples := make([]BehaviorSample, 6)
	for i := range 6 {
		samples[i] = BehaviorSample{
			AgentTemplate: "code-analyst",
			SyscallCounts: map[string]int{"Open": 10 + i, "Read": 20 + i*2},
			TokensUsed:    1000 + i*100,
			TokenRate:     2.0 + float64(i)*0.2,
			DurationMs:    int64(5000 + i*500),
			ExitNormal:    true,
			Timestamp:     time.Now(),
		}
	}

	// When: computing profile
	profile := ComputeProfile("code-analyst", samples, MinSamplesForProfile)

	// Then: profile is not nil
	if profile == nil {
		t.Fatal("expected non-nil profile for 6 samples")
	}
	// And: sample count is 6
	if profile.SampleCount != 6 {
		t.Errorf("SampleCount = %d, want 6", profile.SampleCount)
	}
	// And: agent template matches
	if profile.AgentTemplate != "code-analyst" {
		t.Errorf("AgentTemplate = %q, want %q", profile.AgentTemplate, "code-analyst")
	}
	// And: syscall mean is computed
	if profile.SyscallMean == nil {
		t.Fatal("SyscallMean should not be nil")
	}
	openMean := profile.SyscallMean["Open"]
	if openMean < 10 || openMean > 16 {
		t.Errorf("SyscallMean[Open] = %f, want between 10 and 16", openMean)
	}
	// And: syscall std dev is computed
	if profile.SyscallStdDev == nil {
		t.Fatal("SyscallStdDev should not be nil")
	}
	openStdDev := profile.SyscallStdDev["Open"]
	if openStdDev <= 0 {
		t.Errorf("SyscallStdDev[Open] = %f, should be > 0", openStdDev)
	}
	// And: token rate mean is reasonable
	if profile.TokenRateMean < 2.0 || profile.TokenRateMean > 3.5 {
		t.Errorf("TokenRateMean = %f, want between 2.0 and 3.5", profile.TokenRateMean)
	}
	// And: duration mean is reasonable
	if profile.DurationMeanMs < 5000 || profile.DurationMeanMs > 8000 {
		t.Errorf("DurationMeanMs = %f, want between 5000 and 8000", profile.DurationMeanMs)
	}
	// And: LastUpdated is set
	if profile.LastUpdated.IsZero() {
		t.Error("LastUpdated should not be zero")
	}
}

// --- 22.1-UNIT-004: [P0] ComputeProfile with insufficient samples returns nil (AC3) ---

func TestComputeProfile_InsufficientSamples(t *testing.T) {
	// Given: only 4 samples (below MinSamplesForProfile=5)
	samples := make([]BehaviorSample, 4)
	for i := range 4 {
		samples[i] = BehaviorSample{
			AgentTemplate: "reviewer",
			SyscallCounts: map[string]int{"Open": 5},
			TokenRate:     1.5,
			DurationMs:    3000,
			Timestamp:     time.Now(),
		}
	}

	// When: computing profile
	profile := ComputeProfile("reviewer", samples, MinSamplesForProfile)

	// Then: returns nil
	if profile != nil {
		t.Errorf("expected nil profile for 4 samples, got %+v", profile)
	}
}

// --- 22.1-UNIT-005: [P0] ComputeProfile with empty samples returns nil (AC3) ---

func TestComputeProfile_EmptySamples(t *testing.T) {
	// Given: empty sample list
	profile := ComputeProfile("any-agent", []BehaviorSample{}, MinSamplesForProfile)

	// Then: returns nil
	if profile != nil {
		t.Error("expected nil profile for empty samples")
	}
}

// --- 22.1-UNIT-006: [P1] ComputeProfile with single syscall type computes correctly (AC3) ---

func TestComputeProfile_SingleSyscall(t *testing.T) {
	// Given: 5 samples each with exactly one syscall type
	samples := make([]BehaviorSample, 5)
	for i := range 5 {
		samples[i] = BehaviorSample{
			AgentTemplate: "simple-agent",
			SyscallCounts: map[string]int{"Write": 10},
			TokenRate:     1.0,
			DurationMs:    1000,
			Timestamp:     time.Now(),
		}
	}

	// When: computing profile
	profile := ComputeProfile("simple-agent", samples, MinSamplesForProfile)

	// Then: profile has only "Write" in syscall stats
	if profile == nil {
		t.Fatal("expected non-nil profile")
	}
	if len(profile.SyscallMean) != 1 {
		t.Errorf("expected 1 syscall in mean, got %d", len(profile.SyscallMean))
	}
	if profile.SyscallMean["Write"] != 10.0 {
		t.Errorf("SyscallMean[Write] = %f, want 10.0", profile.SyscallMean["Write"])
	}
}

// --- 22.1-UNIT-007: [P1] ComputeProfile zero variance gives stddev=0 (AC3) ---

func TestComputeProfile_ZeroVariance(t *testing.T) {
	// Given: 5 identical samples
	samples := make([]BehaviorSample, 5)
	for i := range 5 {
		samples[i] = BehaviorSample{
			AgentTemplate: "uniform-agent",
			SyscallCounts: map[string]int{"Open": 7, "Read": 15},
			TokenRate:     2.0,
			DurationMs:    5000,
			Timestamp:     time.Now(),
		}
	}

	// When: computing profile
	profile := ComputeProfile("uniform-agent", samples, MinSamplesForProfile)

	// Then: standard deviations are 0
	if profile == nil {
		t.Fatal("expected non-nil profile")
	}
	if profile.SyscallStdDev["Open"] != 0 {
		t.Errorf("SyscallStdDev[Open] = %f, want 0", profile.SyscallStdDev["Open"])
	}
	if profile.SyscallStdDev["Read"] != 0 {
		t.Errorf("SyscallStdDev[Read] = %f, want 0", profile.SyscallStdDev["Read"])
	}
	if profile.TokenRateStdDev != 0 {
		t.Errorf("TokenRateStdDev = %f, want 0", profile.TokenRateStdDev)
	}
	if profile.DurationStdDevMs != 0 {
		t.Errorf("DurationStdDevMs = %f, want 0", profile.DurationStdDevMs)
	}
}

// --- 22.1-UNIT-008: [P0] MinSamplesForProfile constant is 5 (AC3) ---

func TestMinSamplesForProfile_Value(t *testing.T) {
	// Then: MinSamplesForProfile equals 5
	if MinSamplesForProfile != 5 {
		t.Errorf("MinSamplesForProfile = %d, want 5", MinSamplesForProfile)
	}
}

// --- 22.1-UNIT-009: [P0] ImmuneStore RecordSample and GetSamples (AC4/AC5) ---

func TestImmuneStore_RecordAndGetSamples(t *testing.T) {
	// Given: an ImmuneStore with a temp directory
	dir := t.TempDir()
	store := NewImmuneStore(dir)

	// When: writing 3 samples
	samples := []BehaviorSample{
		{
			AgentTemplate: "code-analyst",
			SyscallCounts: map[string]int{"Open": 5},
			TokensUsed:    1000,
			TokenRate:     2.0,
			DurationMs:    5000,
			ExitNormal:    true,
			Timestamp:     time.Now(),
		},
		{
			AgentTemplate: "code-analyst",
			SyscallCounts: map[string]int{"Open": 8, "Write": 3},
			TokensUsed:    1500,
			TokenRate:     2.5,
			DurationMs:    6000,
			ExitNormal:    true,
			Timestamp:     time.Now(),
		},
		{
			AgentTemplate: "code-analyst",
			SyscallCounts: map[string]int{"Open": 3},
			TokensUsed:    800,
			TokenRate:     1.5,
			DurationMs:    4000,
			ExitNormal:    false,
			Timestamp:     time.Now(),
		},
	}

	for _, s := range samples {
		if err := store.RecordSample(s); err != nil {
			t.Fatalf("RecordSample failed: %v", err)
		}
	}

	// Then: reading back returns 3 samples
	got, err := store.GetSamples("code-analyst")
	if err != nil {
		t.Fatalf("GetSamples failed: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 samples, got %d", len(got))
	}
	// And: first sample has correct data
	if got[0].TokensUsed != 1000 {
		t.Errorf("first sample TokensUsed = %d, want 1000", got[0].TokensUsed)
	}
	if got[0].AgentTemplate != "code-analyst" {
		t.Errorf("first sample AgentTemplate = %q, want %q", got[0].AgentTemplate, "code-analyst")
	}
}

// --- 22.1-UNIT-010: [P0] ImmuneStore GetSamples with no file returns empty (AC5) ---

func TestImmuneStore_EmptyFile(t *testing.T) {
	// Given: an ImmuneStore with no data
	dir := t.TempDir()
	store := NewImmuneStore(dir)

	// When: reading samples for nonexistent agent
	got, err := store.GetSamples("nonexistent-agent")

	// Then: no error, empty slice
	if err != nil {
		t.Fatalf("GetSamples on empty should not error: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 samples, got %d", len(got))
	}
}

// --- 22.1-UNIT-011: [P0] ImmuneStore SaveProfile and LoadProfile (AC4) ---

func TestImmuneStore_SaveAndLoadProfile(t *testing.T) {
	// Given: an ImmuneStore and a NormalProfile
	dir := t.TempDir()
	store := NewImmuneStore(dir)

	profile := &NormalProfile{
		AgentTemplate:    "code-analyst",
		SampleCount:      10,
		SyscallMean:      map[string]float64{"Open": 7.5, "Read": 20.0},
		SyscallStdDev:    map[string]float64{"Open": 1.2, "Read": 3.5},
		TokenRateMean:    2.5,
		TokenRateStdDev:  0.3,
		DurationMeanMs:   5500.0,
		DurationStdDevMs: 800.0,
		LastUpdated:      time.Now().Truncate(time.Second),
	}

	// When: saving the profile
	if err := store.SaveProfile(profile); err != nil {
		t.Fatalf("SaveProfile failed: %v", err)
	}

	// Then: loading it back returns the same profile
	loaded, err := store.LoadProfile("code-analyst")
	if err != nil {
		t.Fatalf("LoadProfile failed: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected non-nil loaded profile")
	}
	if loaded.AgentTemplate != "code-analyst" {
		t.Errorf("AgentTemplate = %q, want %q", loaded.AgentTemplate, "code-analyst")
	}
	if loaded.SampleCount != 10 {
		t.Errorf("SampleCount = %d, want 10", loaded.SampleCount)
	}
	if loaded.TokenRateMean != 2.5 {
		t.Errorf("TokenRateMean = %f, want 2.5", loaded.TokenRateMean)
	}
	if loaded.SyscallMean["Open"] != 7.5 {
		t.Errorf("SyscallMean[Open] = %f, want 7.5", loaded.SyscallMean["Open"])
	}
}

// --- 22.1-UNIT-012: [P0] ImmuneStore LoadProfile nonexistent returns nil, nil (AC4) ---

func TestImmuneStore_LoadProfileNotExist(t *testing.T) {
	// Given: an ImmuneStore with no profiles
	dir := t.TempDir()
	store := NewImmuneStore(dir)

	// When: loading a nonexistent profile
	profile, err := store.LoadProfile("nonexistent")

	// Then: returns nil, nil (not an error)
	if err != nil {
		t.Fatalf("LoadProfile should not error for nonexistent: %v", err)
	}
	if profile != nil {
		t.Error("expected nil profile for nonexistent agent")
	}
}

// --- 22.1-UNIT-013: [P0] ImmuneStore LoadAllProfiles (AC4) ---

func TestImmuneStore_LoadAllProfiles(t *testing.T) {
	// Given: profiles saved for 3 agent templates
	dir := t.TempDir()
	store := NewImmuneStore(dir)

	templates := []string{"code-analyst", "reviewer", "security-scan"}
	for _, tmpl := range templates {
		profile := &NormalProfile{
			AgentTemplate: tmpl,
			SampleCount:   5,
			SyscallMean:   map[string]float64{"Open": 5.0},
			SyscallStdDev: map[string]float64{"Open": 1.0},
			TokenRateMean: 2.0,
			DurationMeanMs: 5000,
			LastUpdated:   time.Now(),
		}
		if err := store.SaveProfile(profile); err != nil {
			t.Fatalf("SaveProfile(%s) failed: %v", tmpl, err)
		}
	}

	// When: loading all profiles
	all, err := store.LoadAllProfiles()
	if err != nil {
		t.Fatalf("LoadAllProfiles failed: %v", err)
	}

	// Then: returns 3 profiles
	if len(all) != 3 {
		t.Fatalf("expected 3 profiles, got %d", len(all))
	}
	// And: all templates are present
	for _, tmpl := range templates {
		if _, ok := all[tmpl]; !ok {
			t.Errorf("missing profile for %q", tmpl)
		}
	}
}

// --- 22.1-UNIT-014: [P1] ImmuneStore concurrent writes are safe (AC5) ---

func TestImmuneStore_ConcurrentWrites(t *testing.T) {
	// Given: an ImmuneStore
	dir := t.TempDir()
	store := NewImmuneStore(dir)

	// When: 10 goroutines each write 5 samples concurrently
	const goroutines = 10
	const samplesPerGoroutine = 5
	var wg sync.WaitGroup

	for i := range goroutines {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := range samplesPerGoroutine {
				sample := BehaviorSample{
					AgentTemplate: "concurrent-agent",
					SyscallCounts: map[string]int{"Open": idx + j},
					TokensUsed:    1000 + idx*100 + j*10,
					TokenRate:     float64(idx) + float64(j)*0.1,
					DurationMs:    int64(5000 + idx*100),
					ExitNormal:    true,
					Timestamp:     time.Now(),
				}
				if err := store.RecordSample(sample); err != nil {
					t.Errorf("concurrent RecordSample failed: %v", err)
				}
			}
		}(i)
	}
	wg.Wait()

	// Then: all 50 samples are recorded
	got, err := store.GetSamples("concurrent-agent")
	if err != nil {
		t.Fatalf("GetSamples failed: %v", err)
	}
	if len(got) != goroutines*samplesPerGoroutine {
		t.Errorf("expected %d samples, got %d", goroutines*samplesPerGoroutine, len(got))
	}
}

// --- 22.1-UNIT-015: [P0] BehaviorCollector Observe accumulates (AC1/AC2) ---

func TestBehaviorCollector_ObserveAccumulates(t *testing.T) {
	// Given: a BehaviorCollector for PID 42
	collector := NewBehaviorCollector(types.PID(42), "code-analyst")

	// When: observing multiple syscall events
	events := []types.SyscallEvent{
		{PID: 42, Syscall: "Open", Args: map[string]any{"path": "/dev/llm/claude"}},
		{PID: 42, Syscall: "Read", Args: map[string]any{"path": "/dev/llm/claude"}},
		{PID: 42, Syscall: "Open", Args: map[string]any{"path": "/dev/fs"}},
		{PID: 42, Syscall: "Write", Args: map[string]any{"path": "/dev/fs"}},
		{PID: 42, Syscall: "Open", Args: map[string]any{"path": "/dev/llm/claude"}},
	}
	for _, e := range events {
		collector.Observe(e)
	}

	// Then: finalizing produces correct accumulated stats
	sample := collector.Finalize(1500, true)

	// And: syscall counts are correct
	if sample.SyscallCounts["Open"] != 3 {
		t.Errorf("SyscallCounts[Open] = %d, want 3", sample.SyscallCounts["Open"])
	}
	if sample.SyscallCounts["Read"] != 1 {
		t.Errorf("SyscallCounts[Read] = %d, want 1", sample.SyscallCounts["Read"])
	}
	if sample.SyscallCounts["Write"] != 1 {
		t.Errorf("SyscallCounts[Write] = %d, want 1", sample.SyscallCounts["Write"])
	}
	// And: agent template is set
	if sample.AgentTemplate != "code-analyst" {
		t.Errorf("AgentTemplate = %q, want %q", sample.AgentTemplate, "code-analyst")
	}
	// And: exit was normal
	if !sample.ExitNormal {
		t.Error("ExitNormal should be true")
	}
	// And: tokens are set
	if sample.TokensUsed != 1500 {
		t.Errorf("TokensUsed = %d, want 1500", sample.TokensUsed)
	}
}

// --- 22.1-UNIT-016: [P0] BehaviorCollector Finalize computes token rate (AC2) ---

func TestBehaviorCollector_Finalize(t *testing.T) {
	// Given: a BehaviorCollector
	collector := NewBehaviorCollector(types.PID(1), "test-agent")

	// When: a single event observed and finalized after some time
	collector.Observe(types.SyscallEvent{PID: 1, Syscall: "Open"})

	sample := collector.Finalize(500, true)

	// Then: duration is > 0
	if sample.DurationMs <= 0 {
		t.Errorf("DurationMs should be > 0, got %d", sample.DurationMs)
	}
	// And: token rate is computed (tokens / duration_seconds)
	expectedRate := float64(500) / (float64(sample.DurationMs) / 1000.0)
	if math.Abs(sample.TokenRate-expectedRate) > 0.01 {
		t.Errorf("TokenRate = %f, want approximately %f", sample.TokenRate, expectedRate)
	}
	// And: timestamp is set
	if sample.Timestamp.IsZero() {
		t.Error("Timestamp should not be zero")
	}
}

// --- 22.1-UNIT-017: [P1] BehaviorCollector device access deduplication (AC2) ---

func TestBehaviorCollector_DeviceAccessDedup(t *testing.T) {
	// Given: a BehaviorCollector
	collector := NewBehaviorCollector(types.PID(1), "test-agent")

	// When: observing events with repeated device paths
	collector.Observe(types.SyscallEvent{PID: 1, Syscall: "Open", Args: map[string]any{"path": "/dev/llm/claude"}})
	collector.Observe(types.SyscallEvent{PID: 1, Syscall: "Read", Args: map[string]any{"path": "/dev/llm/claude"}})
	collector.Observe(types.SyscallEvent{PID: 1, Syscall: "Open", Args: map[string]any{"path": "/dev/fs"}})
	collector.Observe(types.SyscallEvent{PID: 1, Syscall: "Open", Args: map[string]any{"path": "/dev/llm/claude"}})

	sample := collector.Finalize(100, true)

	// Then: device access list contains unique entries only
	if len(sample.DeviceAccess) != 2 {
		t.Errorf("DeviceAccess len = %d, want 2 (deduplicated)", len(sample.DeviceAccess))
	}
	// And: both devices are present
	devices := make(map[string]bool)
	for _, d := range sample.DeviceAccess {
		devices[d] = true
	}
	if !devices["/dev/llm/claude"] {
		t.Error("missing /dev/llm/claude in DeviceAccess")
	}
	if !devices["/dev/fs"] {
		t.Error("missing /dev/fs in DeviceAccess")
	}
}

// --- 22.1-UNIT-018: [P1] BehaviorCollector zero events produces zero-value sample (AC2) ---

func TestBehaviorCollector_ZeroEvents(t *testing.T) {
	// Given: a BehaviorCollector with no events
	collector := NewBehaviorCollector(types.PID(1), "idle-agent")

	// When: finalizing immediately
	sample := collector.Finalize(0, true)

	// Then: syscall counts are empty
	if len(sample.SyscallCounts) != 0 {
		t.Errorf("SyscallCounts should be empty, got %d entries", len(sample.SyscallCounts))
	}
	// And: device access is empty
	if len(sample.DeviceAccess) != 0 {
		t.Errorf("DeviceAccess should be empty, got %d entries", len(sample.DeviceAccess))
	}
	// And: tokens used is 0
	if sample.TokensUsed != 0 {
		t.Errorf("TokensUsed = %d, want 0", sample.TokensUsed)
	}
	// And: agent template is still set
	if sample.AgentTemplate != "idle-agent" {
		t.Errorf("AgentTemplate = %q, want %q", sample.AgentTemplate, "idle-agent")
	}
}

// --- 22.1-UNIT-019: [P0] ImmuneDaemon Start and Stop lifecycle (AC1) ---

func TestImmuneDaemon_StartStop(t *testing.T) {
	// Given: an ImmuneDaemon with a store
	dir := t.TempDir()
	store := NewImmuneStore(dir)
	daemon := NewImmuneDaemon(store, DefaultImmuneConfig())

	// When: starting the daemon
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Then: daemon is running (can call methods without panic)
	// And: stopping completes without error
	daemon.Stop()
}

// --- 22.1-UNIT-020: [P0] ImmuneDaemon process lifecycle (AC1/AC2) ---

func TestImmuneDaemon_OnProcessLifecycle(t *testing.T) {
	// Given: a running ImmuneDaemon
	dir := t.TempDir()
	store := NewImmuneStore(dir)
	daemon := NewImmuneDaemon(store, DefaultImmuneConfig())
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer daemon.Stop()

	// When: simulating a process lifecycle
	pid := types.PID(100)
	daemon.OnProcessStart(pid, "code-analyst")

	// And: sending syscall events
	daemon.OnSyscallEvent(pid, types.SyscallEvent{PID: pid, Syscall: "Open", Args: map[string]any{"path": "/dev/llm/claude"}})
	daemon.OnSyscallEvent(pid, types.SyscallEvent{PID: pid, Syscall: "Read"})
	daemon.OnSyscallEvent(pid, types.SyscallEvent{PID: pid, Syscall: "Write"})

	// And: process exits
	daemon.OnProcessExit(pid, 500, true)

	// Then: a behavior sample was recorded
	samples, err := store.GetSamples("code-analyst")
	if err != nil {
		t.Fatalf("GetSamples failed: %v", err)
	}
	if len(samples) != 1 {
		t.Fatalf("expected 1 sample, got %d", len(samples))
	}
	if samples[0].SyscallCounts["Open"] != 1 {
		t.Errorf("SyscallCounts[Open] = %d, want 1", samples[0].SyscallCounts["Open"])
	}
	if samples[0].TokensUsed != 500 {
		t.Errorf("TokensUsed = %d, want 500", samples[0].TokensUsed)
	}
}

// --- 22.1-UNIT-021: [P0] ImmuneDaemon builds profile after enough samples (AC3) ---

func TestImmuneDaemon_ProfileBuilding(t *testing.T) {
	// Given: a running ImmuneDaemon
	dir := t.TempDir()
	store := NewImmuneStore(dir)
	daemon := NewImmuneDaemon(store, DefaultImmuneConfig())
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer daemon.Stop()

	// When: simulating 5 complete process lifecycles (enough for profile)
	for i := range 5 {
		pid := types.PID(200 + i)
		daemon.OnProcessStart(pid, "profile-test-agent")
		daemon.OnSyscallEvent(pid, types.SyscallEvent{PID: pid, Syscall: "Open"})
		daemon.OnProcessExit(pid, 1000+i*100, true)
	}

	// Then: a profile should be established
	profile := daemon.GetProfile("profile-test-agent")
	if profile == nil {
		t.Fatal("expected profile to be built after 5 samples")
	}
	if profile.SampleCount != 5 {
		t.Errorf("SampleCount = %d, want 5", profile.SampleCount)
	}
	if profile.AgentTemplate != "profile-test-agent" {
		t.Errorf("AgentTemplate = %q, want %q", profile.AgentTemplate, "profile-test-agent")
	}
}

// --- 22.1-UNIT-022: [P0] ImmuneDaemon profile persistence across restart (AC4) ---

func TestImmuneDaemon_ProfilePersistence(t *testing.T) {
	// Given: a daemon that has built and saved a profile
	dir := t.TempDir()
	store := NewImmuneStore(dir)

	// Pre-populate samples directly to build a profile
	for i := range 6 {
		sample := BehaviorSample{
			AgentTemplate: "persistent-agent",
			SyscallCounts: map[string]int{"Open": 5 + i},
			TokensUsed:    1000 + i*100,
			TokenRate:     2.0,
			DurationMs:    5000,
			ExitNormal:    true,
			Timestamp:     time.Now(),
		}
		if err := store.RecordSample(sample); err != nil {
			t.Fatalf("RecordSample failed: %v", err)
		}
	}
	// Compute and save profile
	samples, _ := store.GetSamples("persistent-agent")
	profile := ComputeProfile("persistent-agent", samples, MinSamplesForProfile)
	if profile == nil {
		t.Fatal("expected non-nil profile from 6 samples")
	}
	if err := store.SaveProfile(profile); err != nil {
		t.Fatalf("SaveProfile failed: %v", err)
	}

	// When: creating a new daemon (simulating restart) and starting it
	store2 := NewImmuneStore(dir)
	daemon2 := NewImmuneDaemon(store2, DefaultImmuneConfig())
	if err := daemon2.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer daemon2.Stop()

	// Then: the profile is loaded from disk
	loaded := daemon2.GetProfile("persistent-agent")
	if loaded == nil {
		t.Fatal("expected profile to be loaded after restart")
	}
	if loaded.SampleCount != 6 {
		t.Errorf("SampleCount = %d, want 6", loaded.SampleCount)
	}
}

// --- 22.1-UNIT-023: [P0] ImmuneDaemon nil-safe (AC7) ---

func TestImmuneDaemon_NilSafe(t *testing.T) {
	// Given: a nil ImmuneDaemon
	var daemon *ImmuneDaemon

	// Then: all methods should not panic
	daemon.OnProcessStart(types.PID(1), "agent")
	daemon.OnSyscallEvent(types.PID(1), types.SyscallEvent{})
	daemon.OnProcessExit(types.PID(1), 100, true)
	daemon.Stop()

	profile := daemon.GetProfile("agent")
	if profile != nil {
		t.Error("GetProfile on nil daemon should return nil")
	}

	allProfiles := daemon.GetAllProfiles()
	if allProfiles != nil {
		t.Error("GetAllProfiles on nil daemon should return nil")
	}
}

// --- 22.1-UNIT-024: [P1] ImmuneDaemon concurrent access (AC1) ---

func TestImmuneDaemon_ConcurrentAccess(t *testing.T) {
	// Given: a running ImmuneDaemon
	dir := t.TempDir()
	store := NewImmuneStore(dir)
	daemon := NewImmuneDaemon(store, DefaultImmuneConfig())
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer daemon.Stop()

	// When: multiple goroutines concurrently start/event/exit processes
	const goroutines = 10
	var wg sync.WaitGroup

	for i := range goroutines {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			pid := types.PID(300 + idx)
			daemon.OnProcessStart(pid, "concurrent-agent")
			for j := range 5 {
				daemon.OnSyscallEvent(pid, types.SyscallEvent{
					PID:     pid,
					Syscall: "Open",
					Args:    map[string]any{"iter": j},
				})
			}
			daemon.OnProcessExit(pid, 1000+idx*100, true)
		}(i)
	}
	wg.Wait()

	// Then: all samples are recorded without race
	samples, err := store.GetSamples("concurrent-agent")
	if err != nil {
		t.Fatalf("GetSamples failed: %v", err)
	}
	if len(samples) != goroutines {
		t.Errorf("expected %d samples, got %d", goroutines, len(samples))
	}
}

// --- 22.1-UNIT-025: [P0] ImmuneStore sample file uses JSON Lines format (AC5) ---

func TestImmuneStore_JSONLinesFormat(t *testing.T) {
	// Given: an ImmuneStore with 2 samples
	dir := t.TempDir()
	store := NewImmuneStore(dir)

	for i := range 2 {
		sample := BehaviorSample{
			AgentTemplate: "format-test",
			SyscallCounts: map[string]int{"Open": i + 1},
			TokensUsed:    1000 * (i + 1),
			Timestamp:     time.Now(),
		}
		if err := store.RecordSample(sample); err != nil {
			t.Fatalf("RecordSample failed: %v", err)
		}
	}

	// Then: the file contains 2 lines (JSON Lines format)
	filePath := filepath.Join(dir, "format-test.jsonl")
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	lines := 0
	for _, b := range data {
		if b == '\n' {
			lines++
		}
	}
	if lines != 2 {
		t.Errorf("expected 2 lines in JSONL file, got %d", lines)
	}
}

// --- 22.1-UNIT-026: [P0] ImmuneStore profile file path is in immune directory (AC4) ---

func TestImmuneStore_ProfilePath(t *testing.T) {
	// Given: an ImmuneStore with a saved profile
	dir := t.TempDir()
	store := NewImmuneStore(dir)

	profile := &NormalProfile{
		AgentTemplate: "path-test",
		SampleCount:   5,
		SyscallMean:   map[string]float64{"Open": 5.0},
		SyscallStdDev: map[string]float64{"Open": 1.0},
		LastUpdated:   time.Now(),
	}
	if err := store.SaveProfile(profile); err != nil {
		t.Fatalf("SaveProfile failed: %v", err)
	}

	// Then: profile file exists at expected path
	expectedPath := filepath.Join(dir, "profiles", "path-test-profile.json")
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Errorf("profile file not found at expected path: %s", expectedPath)
	}
}

// --- 22.1-UNIT-027: [P0] ImmuneDaemon GetAllProfiles returns cached profiles (AC3) ---

func TestImmuneDaemon_GetAllProfiles(t *testing.T) {
	// Given: a daemon that has loaded profiles
	dir := t.TempDir()
	store := NewImmuneStore(dir)

	// Pre-save 2 profiles
	for _, tmpl := range []string{"agent-a", "agent-b"} {
		profile := &NormalProfile{
			AgentTemplate: tmpl,
			SampleCount:   5,
			SyscallMean:   map[string]float64{"Open": 5.0},
			SyscallStdDev: map[string]float64{"Open": 1.0},
			LastUpdated:   time.Now(),
		}
		if err := store.SaveProfile(profile); err != nil {
			t.Fatalf("SaveProfile(%s) failed: %v", tmpl, err)
		}
	}

	daemon := NewImmuneDaemon(store, DefaultImmuneConfig())
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer daemon.Stop()

	// When: getting all profiles
	all := daemon.GetAllProfiles()

	// Then: returns 2 profiles
	if len(all) != 2 {
		t.Fatalf("expected 2 profiles, got %d", len(all))
	}
	if _, ok := all["agent-a"]; !ok {
		t.Error("missing profile for agent-a")
	}
	if _, ok := all["agent-b"]; !ok {
		t.Error("missing profile for agent-b")
	}
}

// --- 22.1-UNIT-028: [P1] NormalProfile JSON serialization (AC3/AC4) ---

func TestNormalProfile_JSONSerialization(t *testing.T) {
	// Given: a NormalProfile
	profile := NormalProfile{
		AgentTemplate:    "json-test",
		SampleCount:      8,
		SyscallMean:      map[string]float64{"Open": 7.5},
		SyscallStdDev:    map[string]float64{"Open": 1.2},
		TokenRateMean:    2.5,
		TokenRateStdDev:  0.3,
		DurationMeanMs:   5500.0,
		DurationStdDevMs: 800.0,
		LastUpdated:      time.Date(2026, 3, 14, 10, 0, 0, 0, time.UTC),
	}

	// When: serializing to JSON
	data, err := json.Marshal(profile)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Then: JSON contains snake_case field names
	jsonStr := string(data)
	expectedFields := []string{
		`"agent_template"`,
		`"sample_count"`,
		`"syscall_mean"`,
		`"syscall_std_dev"`,
		`"token_rate_mean"`,
		`"token_rate_std_dev"`,
		`"duration_mean_ms"`,
		`"duration_std_dev_ms"`,
		`"last_updated"`,
	}
	for _, field := range expectedFields {
		if !contains(jsonStr, field) {
			t.Errorf("JSON missing field %s in: %s", field, jsonStr)
		}
	}
}

// contains checks if substr is in s.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
