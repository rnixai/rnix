package kernel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// ============================================================
// ATDD RED PHASE — Story 22.2: 异常检测与威胁记忆
//
// AnomalyType, AnomalyAlert, ThreatSignature, AnomalyDetector,
// ImmuneStore 威胁持久化, ImmuneDaemon 异常检测集成的单元测试。
//
// 测试引用的类型和方法尚不存在，测试将无法编译直到实现完成。
//
// RED -> GREEN: 在 kernel/immune.go 中实现所有类型和方法。
// ============================================================

// --- 22.2-UNIT-001: [P0] AnomalyAlert JSON round-trip (AC1/AC4) ---

func TestAnomalyAlert_JSONRoundTrip(t *testing.T) {
	// Given: a fully populated AnomalyAlert
	original := AnomalyAlert{
		PID:           types.PID(42),
		AgentTemplate: "code-analyst",
		Type:          AnomalySyscallFreq,
		Detail:        "Open 频率 12.0 是基线 2.4 的 5.0 倍",
		Deviation:     5.0,
		Timestamp:     time.Date(2026, 3, 14, 10, 30, 0, 0, time.UTC),
	}

	// When: serializing to JSON and back
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var restored AnomalyAlert
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// Then: all fields are preserved
	if restored.PID != original.PID {
		t.Errorf("PID = %d, want %d", restored.PID, original.PID)
	}
	if restored.AgentTemplate != original.AgentTemplate {
		t.Errorf("AgentTemplate = %q, want %q", restored.AgentTemplate, original.AgentTemplate)
	}
	if restored.Type != AnomalySyscallFreq {
		t.Errorf("Type = %q, want %q", restored.Type, AnomalySyscallFreq)
	}
	if restored.Detail != original.Detail {
		t.Errorf("Detail = %q, want %q", restored.Detail, original.Detail)
	}
	if restored.Deviation != 5.0 {
		t.Errorf("Deviation = %f, want 5.0", restored.Deviation)
	}

	// And: JSON field names use snake_case
	jsonStr := string(data)
	expectedFields := []string{
		`"pid"`,
		`"agent_template"`,
		`"type"`,
		`"detail"`,
		`"deviation"`,
		`"timestamp"`,
	}
	for _, field := range expectedFields {
		if !searchString(jsonStr, field) {
			t.Errorf("JSON missing field %s in: %s", field, jsonStr)
		}
	}
}

// --- 22.2-UNIT-002: [P0] AnomalyType constants have correct values (AC4) ---

func TestAnomalyType_Constants(t *testing.T) {
	// Then: AnomalyType constants have expected string values
	if AnomalySyscallFreq != AnomalyType("syscall_freq") {
		t.Errorf("AnomalySyscallFreq = %q, want %q", AnomalySyscallFreq, "syscall_freq")
	}
	if AnomalyTokenRate != AnomalyType("token_rate") {
		t.Errorf("AnomalyTokenRate = %q, want %q", AnomalyTokenRate, "token_rate")
	}
	if AnomalyDeviceAccess != AnomalyType("device_access") {
		t.Errorf("AnomalyDeviceAccess = %q, want %q", AnomalyDeviceAccess, "device_access")
	}
}

// --- 22.2-UNIT-003: [P0] ThreatSignature JSON round-trip (AC2/AC5) ---

func TestThreatSignature_JSONRoundTrip(t *testing.T) {
	// Given: a fully populated ThreatSignature
	original := ThreatSignature{
		ID:            "threat-1710403800",
		Type:          AnomalySyscallFreq,
		AgentTemplate: "code-analyst",
		Metric:        "Open",
		Threshold:     5.0,
		CreatedAt:     time.Date(2026, 3, 14, 10, 30, 0, 0, time.UTC),
	}

	// When: serializing to JSON and back
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var restored ThreatSignature
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// Then: all fields are preserved
	if restored.ID != original.ID {
		t.Errorf("ID = %q, want %q", restored.ID, original.ID)
	}
	if restored.Type != AnomalySyscallFreq {
		t.Errorf("Type = %q, want %q", restored.Type, AnomalySyscallFreq)
	}
	if restored.AgentTemplate != original.AgentTemplate {
		t.Errorf("AgentTemplate = %q, want %q", restored.AgentTemplate, original.AgentTemplate)
	}
	if restored.Metric != "Open" {
		t.Errorf("Metric = %q, want %q", restored.Metric, "Open")
	}
	if restored.Threshold != 5.0 {
		t.Errorf("Threshold = %f, want 5.0", restored.Threshold)
	}

	// And: JSON field names use snake_case
	jsonStr := string(data)
	expectedFields := []string{
		`"id"`,
		`"type"`,
		`"agent_template"`,
		`"metric"`,
		`"threshold"`,
		`"created_at"`,
	}
	for _, field := range expectedFields {
		if !searchString(jsonStr, field) {
			t.Errorf("JSON missing field %s in: %s", field, jsonStr)
		}
	}
}

// --- 22.2-UNIT-004: [P0] ImmuneStore SaveThreat and LoadThreats (AC2/AC5) ---

func TestImmuneStore_SaveAndLoadThreats(t *testing.T) {
	// Given: an ImmuneStore with a temp directory
	dir := t.TempDir()
	store := NewImmuneStore(dir)

	// When: writing 3 threat signatures
	threats := []ThreatSignature{
		{
			ID:            "threat-001",
			Type:          AnomalySyscallFreq,
			AgentTemplate: "code-analyst",
			Metric:        "Open",
			Threshold:     5.0,
			CreatedAt:     time.Now(),
		},
		{
			ID:            "threat-002",
			Type:          AnomalyTokenRate,
			AgentTemplate: "code-analyst",
			Metric:        "token_rate",
			Threshold:     3.5,
			CreatedAt:     time.Now(),
		},
		{
			ID:            "threat-003",
			Type:          AnomalySyscallFreq,
			AgentTemplate: "reviewer",
			Metric:        "Write",
			Threshold:     4.0,
			CreatedAt:     time.Now(),
		},
	}

	for _, sig := range threats {
		if err := store.SaveThreat(sig); err != nil {
			t.Fatalf("SaveThreat failed: %v", err)
		}
	}

	// Then: reading back returns 3 threat signatures
	got, err := store.LoadThreats()
	if err != nil {
		t.Fatalf("LoadThreats failed: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 threats, got %d", len(got))
	}

	// And: first threat has correct data
	if got[0].ID != "threat-001" {
		t.Errorf("first threat ID = %q, want %q", got[0].ID, "threat-001")
	}
	if got[0].Type != AnomalySyscallFreq {
		t.Errorf("first threat Type = %q, want %q", got[0].Type, AnomalySyscallFreq)
	}
	if got[0].Metric != "Open" {
		t.Errorf("first threat Metric = %q, want %q", got[0].Metric, "Open")
	}
}

// --- 22.2-UNIT-005: [P0] ImmuneStore LoadThreats on empty returns empty slice (AC5) ---

func TestImmuneStore_LoadThreats_Empty(t *testing.T) {
	// Given: an ImmuneStore with no threat data
	dir := t.TempDir()
	store := NewImmuneStore(dir)

	// When: loading threats
	got, err := store.LoadThreats()

	// Then: no error, empty slice
	if err != nil {
		t.Fatalf("LoadThreats on empty should not error: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 threats, got %d", len(got))
	}
}

// --- 22.2-UNIT-006: [P0] ImmuneStore threats file uses JSON Lines format (AC5) ---

func TestImmuneStore_ThreatsJSONLinesFormat(t *testing.T) {
	// Given: an ImmuneStore with 2 threats saved
	dir := t.TempDir()
	store := NewImmuneStore(dir)

	for i := range 2 {
		sig := ThreatSignature{
			ID:            "threat-format-" + string(rune('0'+i)),
			Type:          AnomalySyscallFreq,
			AgentTemplate: "format-test",
			Metric:        "Open",
			Threshold:     3.0,
			CreatedAt:     time.Now(),
		}
		if err := store.SaveThreat(sig); err != nil {
			t.Fatalf("SaveThreat failed: %v", err)
		}
	}

	// Then: the file contains 2 lines (JSON Lines format)
	filePath := filepath.Join(dir, "threats.jsonl")
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
		t.Errorf("expected 2 lines in threats.jsonl, got %d", lines)
	}
}

// --- 22.2-UNIT-007: [P0] DefaultDeviationThreshold constant is 3.0 (AC1) ---

func TestDefaultDeviationThreshold_Value(t *testing.T) {
	// Then: DefaultDeviationThreshold equals 3.0
	if DefaultDeviationThreshold != 3.0 {
		t.Errorf("DefaultDeviationThreshold = %f, want 3.0", DefaultDeviationThreshold)
	}
}

// --- 22.2-UNIT-008: [P0] AnomalyDetector syscall normal range does not alert (AC1) ---

func TestAnomalyDetector_SyscallNormal(t *testing.T) {
	// Given: a detector with threshold 3.0 and a profile with Open mean=10, stddev=2
	detector := NewAnomalyDetector(3.0)
	profile := &NormalProfile{
		AgentTemplate: "test-agent",
		SampleCount:   10,
		SyscallMean:   map[string]float64{"Open": 10.0},
		SyscallStdDev: map[string]float64{"Open": 2.0},
	}

	// When: checking a normal count (15 < 10 + 3*2 = 16)
	alert := detector.CheckSyscallAnomaly(
		types.PID(1), "test-agent", "Open", 15, profile,
	)

	// Then: no alert
	if alert != nil {
		t.Errorf("expected nil alert for normal count, got %+v", alert)
	}
}

// --- 22.2-UNIT-009: [P0] AnomalyDetector syscall anomaly triggers alert (AC1/AC4) ---

func TestAnomalyDetector_SyscallAnomaly(t *testing.T) {
	// Given: a detector with threshold 3.0 and a profile with Open mean=10, stddev=2
	detector := NewAnomalyDetector(3.0)
	profile := &NormalProfile{
		AgentTemplate: "test-agent",
		SampleCount:   10,
		SyscallMean:   map[string]float64{"Open": 10.0},
		SyscallStdDev: map[string]float64{"Open": 2.0},
	}

	// When: checking an anomalous count (20 > 10 + 3*2 = 16)
	alert := detector.CheckSyscallAnomaly(
		types.PID(42), "test-agent", "Open", 20, profile,
	)

	// Then: alert is returned
	if alert == nil {
		t.Fatal("expected non-nil alert for anomalous count")
	}
	// And: alert has correct fields
	if alert.PID != types.PID(42) {
		t.Errorf("PID = %d, want 42", alert.PID)
	}
	if alert.Type != AnomalySyscallFreq {
		t.Errorf("Type = %q, want %q", alert.Type, AnomalySyscallFreq)
	}
	if alert.AgentTemplate != "test-agent" {
		t.Errorf("AgentTemplate = %q, want %q", alert.AgentTemplate, "test-agent")
	}
	// And: deviation is currentValue / mean = 20 / 10 = 2.0
	if alert.Deviation != 2.0 {
		t.Errorf("Deviation = %f, want 2.0", alert.Deviation)
	}
	// And: detail is human-readable
	if alert.Detail == "" {
		t.Error("Detail should not be empty")
	}
	// And: timestamp is set
	if alert.Timestamp.IsZero() {
		t.Error("Timestamp should not be zero")
	}
}

// --- 22.2-UNIT-010: [P0] AnomalyDetector token rate normal does not alert (AC1) ---

func TestAnomalyDetector_TokenRateNormal(t *testing.T) {
	// Given: a detector and profile with token rate mean=2.5, stddev=0.5
	detector := NewAnomalyDetector(3.0)
	profile := &NormalProfile{
		AgentTemplate:   "test-agent",
		SampleCount:     10,
		TokenRateMean:   2.5,
		TokenRateStdDev: 0.5,
	}

	// When: checking a normal rate (3.8 < 2.5 + 3*0.5 = 4.0)
	alert := detector.CheckTokenRateAnomaly(
		types.PID(1), "test-agent", 3.8, profile,
	)

	// Then: no alert
	if alert != nil {
		t.Errorf("expected nil alert for normal token rate, got %+v", alert)
	}
}

// --- 22.2-UNIT-011: [P0] AnomalyDetector token rate anomaly triggers alert (AC1/AC4) ---

func TestAnomalyDetector_TokenRateAnomaly(t *testing.T) {
	// Given: a detector and profile with token rate mean=2.5, stddev=0.5
	detector := NewAnomalyDetector(3.0)
	profile := &NormalProfile{
		AgentTemplate:   "test-agent",
		SampleCount:     10,
		TokenRateMean:   2.5,
		TokenRateStdDev: 0.5,
	}

	// When: checking an anomalous rate (5.0 > 2.5 + 3*0.5 = 4.0)
	alert := detector.CheckTokenRateAnomaly(
		types.PID(42), "test-agent", 5.0, profile,
	)

	// Then: alert is returned
	if alert == nil {
		t.Fatal("expected non-nil alert for anomalous token rate")
	}
	if alert.Type != AnomalyTokenRate {
		t.Errorf("Type = %q, want %q", alert.Type, AnomalyTokenRate)
	}
	// And: deviation = 5.0 / 2.5 = 2.0
	if alert.Deviation != 2.0 {
		t.Errorf("Deviation = %f, want 2.0", alert.Deviation)
	}
}

// --- 22.2-UNIT-012: [P1] AnomalyDetector nil profile returns nil (AC1) ---

func TestAnomalyDetector_NoProfile(t *testing.T) {
	// Given: a detector and nil profile
	detector := NewAnomalyDetector(3.0)

	// When: checking with nil profile
	syscallAlert := detector.CheckSyscallAnomaly(
		types.PID(1), "agent", "Open", 100, nil,
	)
	tokenAlert := detector.CheckTokenRateAnomaly(
		types.PID(1), "agent", 100.0, nil,
	)

	// Then: both return nil (cannot detect without profile)
	if syscallAlert != nil {
		t.Errorf("expected nil syscall alert with nil profile, got %+v", syscallAlert)
	}
	if tokenAlert != nil {
		t.Errorf("expected nil token alert with nil profile, got %+v", tokenAlert)
	}
}

// --- 22.2-UNIT-013: [P1] AnomalyDetector zero mean does not false positive (AC1) ---

func TestAnomalyDetector_ZeroMean(t *testing.T) {
	// Given: a detector and profile with zero mean for syscall
	detector := NewAnomalyDetector(3.0)
	profile := &NormalProfile{
		AgentTemplate: "test-agent",
		SampleCount:   10,
		SyscallMean:   map[string]float64{"Open": 0.0},
		SyscallStdDev: map[string]float64{"Open": 0.0},
		TokenRateMean: 0.0,
		TokenRateStdDev: 0.0,
	}

	// When: checking with a non-zero count on zero-mean profile
	syscallAlert := detector.CheckSyscallAnomaly(
		types.PID(1), "test-agent", "Open", 1, profile,
	)
	tokenAlert := detector.CheckTokenRateAnomaly(
		types.PID(1), "test-agent", 0.1, profile,
	)

	// Then: should not panic (may or may not alert, but must not divide by zero)
	// The key assertion is that we reach this line without panicking.
	_ = syscallAlert
	_ = tokenAlert
}

// --- 22.2-UNIT-014: [P0] AnomalyDetector MatchThreat finds matching signature (AC2) ---

func TestAnomalyDetector_MatchThreat(t *testing.T) {
	// Given: a detector and a list of known threats
	detector := NewAnomalyDetector(3.0)
	threats := []ThreatSignature{
		{
			ID:            "threat-001",
			Type:          AnomalySyscallFreq,
			AgentTemplate: "code-analyst",
			Metric:        "Open",
			Threshold:     5.0,
		},
		{
			ID:            "threat-002",
			Type:          AnomalyTokenRate,
			AgentTemplate: "reviewer",
			Metric:        "token_rate",
			Threshold:     3.0,
		},
	}

	// When: matching with same template+type+metric
	match := detector.MatchThreat("code-analyst", AnomalySyscallFreq, "Open", threats)

	// Then: the correct threat is returned
	if match == nil {
		t.Fatal("expected matching threat signature")
	}
	if match.ID != "threat-001" {
		t.Errorf("matched threat ID = %q, want %q", match.ID, "threat-001")
	}
}

// --- 22.2-UNIT-015: [P1] AnomalyDetector MatchThreat returns nil for no match (AC2) ---

func TestAnomalyDetector_NoMatchThreat(t *testing.T) {
	// Given: a detector and a list of known threats
	detector := NewAnomalyDetector(3.0)
	threats := []ThreatSignature{
		{
			ID:            "threat-001",
			Type:          AnomalySyscallFreq,
			AgentTemplate: "code-analyst",
			Metric:        "Open",
		},
	}

	// When: matching with different template
	match := detector.MatchThreat("reviewer", AnomalySyscallFreq, "Open", threats)

	// Then: no match returned
	if match != nil {
		t.Errorf("expected nil for non-matching threat, got %+v", match)
	}
}

// --- 22.2-UNIT-016: [P0] ImmuneDaemon anomaly detection auto-suspends (AC1/AC3) ---

func TestImmuneDaemon_AnomalyDetection(t *testing.T) {
	// Given: a running ImmuneDaemon with a profile and a suspendFn
	dir := t.TempDir()
	store := NewImmuneStore(dir)

	// Pre-save a profile with low Open mean for easy triggering
	profile := &NormalProfile{
		AgentTemplate: "anomaly-agent",
		SampleCount:   10,
		SyscallMean:   map[string]float64{"Open": 2.0},
		SyscallStdDev: map[string]float64{"Open": 0.5},
		TokenRateMean:   2.0,
		TokenRateStdDev: 0.3,
	}
	if err := store.SaveProfile(profile); err != nil {
		t.Fatalf("SaveProfile failed: %v", err)
	}

	daemon := NewImmuneDaemon(store)
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer daemon.Stop()

	// Track suspended PIDs
	var suspendedMu sync.Mutex
	suspendedPIDs := make([]types.PID, 0)
	daemon.SetSuspendFunc(func(pid types.PID) error {
		suspendedMu.Lock()
		defer suspendedMu.Unlock()
		suspendedPIDs = append(suspendedPIDs, pid)
		return nil
	})

	// When: a process generates far more Open calls than the baseline
	pid := types.PID(42)
	daemon.OnProcessStart(pid, "anomaly-agent")

	// Exceed threshold: mean=2.0, stddev=0.5, threshold=3.0 -> limit=2.0+3*0.5=3.5
	// Send enough events to exceed the threshold
	for i := range 10 {
		daemon.OnSyscallEvent(pid, types.SyscallEvent{
			PID:     pid,
			Syscall: "Open",
			Args:    map[string]any{"iter": i},
		})
	}

	// Then: the process should have been suspended
	suspendedMu.Lock()
	defer suspendedMu.Unlock()
	if len(suspendedPIDs) == 0 {
		t.Error("expected process to be suspended after exceeding anomaly threshold")
	}

	// And: an alert should exist for this PID
	alerts := daemon.GetAlerts()
	if alerts == nil {
		t.Fatal("GetAlerts returned nil")
	}
	alert, ok := alerts[pid]
	if !ok {
		t.Error("expected alert for PID 42")
	} else {
		if alert.Type != AnomalySyscallFreq {
			t.Errorf("alert Type = %q, want %q", alert.Type, AnomalySyscallFreq)
		}
	}
}

// --- 22.2-UNIT-017: [P0] ImmuneDaemon threat memory match triggers immediate suspend (AC2) ---

func TestImmuneDaemon_ThreatMemoryMatch(t *testing.T) {
	// Given: a running ImmuneDaemon with a known threat signature
	dir := t.TempDir()
	store := NewImmuneStore(dir)

	// Pre-save a threat signature
	sig := ThreatSignature{
		ID:            "threat-test-001",
		Type:          AnomalySyscallFreq,
		AgentTemplate: "known-threat-agent",
		Metric:        "Open",
		Threshold:     3.0,
		CreatedAt:     time.Now(),
	}
	if err := store.SaveThreat(sig); err != nil {
		t.Fatalf("SaveThreat failed: %v", err)
	}

	// Also save a profile so daemon starts with context
	profile := &NormalProfile{
		AgentTemplate: "known-threat-agent",
		SampleCount:   10,
		SyscallMean:   map[string]float64{"Open": 5.0},
		SyscallStdDev: map[string]float64{"Open": 1.0},
	}
	if err := store.SaveProfile(profile); err != nil {
		t.Fatalf("SaveProfile failed: %v", err)
	}

	daemon := NewImmuneDaemon(store)
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer daemon.Stop()

	var suspendedMu sync.Mutex
	suspendedPIDs := make([]types.PID, 0)
	daemon.SetSuspendFunc(func(pid types.PID) error {
		suspendedMu.Lock()
		defer suspendedMu.Unlock()
		suspendedPIDs = append(suspendedPIDs, pid)
		return nil
	})

	// When: a process with matching template sends any Open syscall
	pid := types.PID(99)
	daemon.OnProcessStart(pid, "known-threat-agent")
	// Just one event should trigger via threat memory match (no need to exceed statistical threshold)
	daemon.OnSyscallEvent(pid, types.SyscallEvent{
		PID:     pid,
		Syscall: "Open",
	})

	// Then: the process should be immediately suspended via threat match
	suspendedMu.Lock()
	defer suspendedMu.Unlock()
	if len(suspendedPIDs) == 0 {
		t.Error("expected process to be suspended via threat memory match")
	}
}

// --- 22.2-UNIT-018: [P1] ImmuneDaemon no profile skips detection (AC6) ---

func TestImmuneDaemon_NoProfileNoDetection(t *testing.T) {
	// Given: a running ImmuneDaemon with NO profile for the agent template
	dir := t.TempDir()
	store := NewImmuneStore(dir)

	daemon := NewImmuneDaemon(store)
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer daemon.Stop()

	suspended := false
	daemon.SetSuspendFunc(func(pid types.PID) error {
		suspended = true
		return nil
	})

	// When: a process sends many syscall events (but no profile exists)
	pid := types.PID(50)
	daemon.OnProcessStart(pid, "no-profile-agent")
	for i := range 100 {
		daemon.OnSyscallEvent(pid, types.SyscallEvent{
			PID:     pid,
			Syscall: "Open",
			Args:    map[string]any{"iter": i},
		})
	}

	// Then: process should NOT be suspended (no profile to compare against)
	if suspended {
		t.Error("expected no suspension when no profile exists for the agent template")
	}
}

// --- 22.2-UNIT-019: [P0] ImmuneDaemon ClearAlert removes alert (AC3) ---

func TestImmuneDaemon_ClearAlert(t *testing.T) {
	// Given: a daemon with an active alert
	dir := t.TempDir()
	store := NewImmuneStore(dir)
	daemon := NewImmuneDaemon(store)
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer daemon.Stop()

	// Simulate an alert by pre-populating (needs profile and anomaly triggering)
	profile := &NormalProfile{
		AgentTemplate: "clear-test-agent",
		SampleCount:   10,
		SyscallMean:   map[string]float64{"Open": 1.0},
		SyscallStdDev: map[string]float64{"Open": 0.1},
	}
	if err := store.SaveProfile(profile); err != nil {
		t.Fatalf("SaveProfile failed: %v", err)
	}
	// Restart to load profile
	daemon.Stop()
	daemon = NewImmuneDaemon(store)
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	daemon.SetSuspendFunc(func(pid types.PID) error { return nil })

	pid := types.PID(77)
	daemon.OnProcessStart(pid, "clear-test-agent")
	// Trigger anomaly
	for i := range 20 {
		daemon.OnSyscallEvent(pid, types.SyscallEvent{
			PID:     pid,
			Syscall: "Open",
			Args:    map[string]any{"iter": i},
		})
	}

	// Verify alert exists
	alerts := daemon.GetAlerts()
	if _, ok := alerts[pid]; !ok {
		t.Fatal("expected alert to exist before ClearAlert")
	}

	// When: clearing the alert
	daemon.ClearAlert(pid)

	// Then: alert is no longer present
	alerts = daemon.GetAlerts()
	if _, ok := alerts[pid]; ok {
		t.Error("expected alert to be cleared after ClearAlert")
	}
}

// --- 22.2-UNIT-020: [P0] ImmuneDaemon GetAlerts returns active alerts (AC4) ---

func TestImmuneDaemon_GetAlerts(t *testing.T) {
	// Given: a daemon (without alerts)
	dir := t.TempDir()
	store := NewImmuneStore(dir)
	daemon := NewImmuneDaemon(store)
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer daemon.Stop()

	// Then: GetAlerts returns non-nil empty map
	alerts := daemon.GetAlerts()
	if alerts == nil {
		t.Fatal("GetAlerts should return non-nil map")
	}
	if len(alerts) != 0 {
		t.Errorf("expected 0 alerts initially, got %d", len(alerts))
	}
}

// --- 22.2-UNIT-021: [P0] ImmuneDaemon GetThreats returns loaded threats (AC2) ---

func TestImmuneDaemon_GetThreats(t *testing.T) {
	// Given: a store with 2 threat signatures
	dir := t.TempDir()
	store := NewImmuneStore(dir)

	for i := range 2 {
		sig := ThreatSignature{
			ID:            "threat-get-" + string(rune('0'+i)),
			Type:          AnomalySyscallFreq,
			AgentTemplate: "threat-test",
			Metric:        "Open",
			Threshold:     3.0,
			CreatedAt:     time.Now(),
		}
		if err := store.SaveThreat(sig); err != nil {
			t.Fatalf("SaveThreat failed: %v", err)
		}
	}

	daemon := NewImmuneDaemon(store)
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer daemon.Stop()

	// When: getting threats
	threats := daemon.GetThreats()

	// Then: returns 2 threats
	if len(threats) != 2 {
		t.Errorf("expected 2 threats, got %d", len(threats))
	}
}

// --- 22.2-UNIT-022: [P0] ImmuneDaemon threat persistence across restart (AC5) ---

func TestImmuneDaemon_ThreatPersistence(t *testing.T) {
	// Given: a daemon that has saved threat signatures
	dir := t.TempDir()
	store := NewImmuneStore(dir)

	sig := ThreatSignature{
		ID:            "threat-persist",
		Type:          AnomalyTokenRate,
		AgentTemplate: "persist-agent",
		Metric:        "token_rate",
		Threshold:     4.0,
		CreatedAt:     time.Now(),
	}
	if err := store.SaveThreat(sig); err != nil {
		t.Fatalf("SaveThreat failed: %v", err)
	}

	// When: creating a new daemon (simulating restart)
	store2 := NewImmuneStore(dir)
	daemon2 := NewImmuneDaemon(store2)
	if err := daemon2.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer daemon2.Stop()

	// Then: the threats are loaded from disk
	threats := daemon2.GetThreats()
	if len(threats) != 1 {
		t.Fatalf("expected 1 threat after restart, got %d", len(threats))
	}
	if threats[0].ID != "threat-persist" {
		t.Errorf("threat ID = %q, want %q", threats[0].ID, "threat-persist")
	}
}

// --- 22.2-UNIT-023: [P1] ImmuneDaemon suspendFn nil does not panic (AC6) ---

func TestImmuneDaemon_SuspendFnNil(t *testing.T) {
	// Given: a running ImmuneDaemon without suspendFn set
	dir := t.TempDir()
	store := NewImmuneStore(dir)

	profile := &NormalProfile{
		AgentTemplate: "nil-suspend-agent",
		SampleCount:   10,
		SyscallMean:   map[string]float64{"Open": 1.0},
		SyscallStdDev: map[string]float64{"Open": 0.1},
	}
	if err := store.SaveProfile(profile); err != nil {
		t.Fatalf("SaveProfile failed: %v", err)
	}

	daemon := NewImmuneDaemon(store)
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer daemon.Stop()
	// Note: NOT setting suspendFn - it remains nil

	// When: triggering an anomaly
	pid := types.PID(88)
	daemon.OnProcessStart(pid, "nil-suspend-agent")

	// Then: should not panic even when anomaly detected
	for i := range 20 {
		daemon.OnSyscallEvent(pid, types.SyscallEvent{
			PID:     pid,
			Syscall: "Open",
			Args:    map[string]any{"iter": i},
		})
	}
	// If we reach here without panic, the test passes
}

// --- 22.2-UNIT-024: [P0] ImmuneDaemon nil-safe for new methods (AC6) ---

func TestImmuneDaemon_NilSafe_NewMethods(t *testing.T) {
	// Given: a nil ImmuneDaemon
	var daemon *ImmuneDaemon

	// Then: new methods should not panic
	alerts := daemon.GetAlerts()
	if alerts != nil {
		t.Error("GetAlerts on nil daemon should return nil")
	}

	threats := daemon.GetThreats()
	if threats != nil {
		t.Error("GetThreats on nil daemon should return nil")
	}

	// ClearAlert on nil should not panic
	daemon.ClearAlert(types.PID(1))

	// SetSuspendFunc on nil should not panic
	daemon.SetSuspendFunc(func(pid types.PID) error { return nil })
}

// --- 22.2-UNIT-025: [P1] ImmuneDaemon concurrent anomaly detection (AC1) ---

func TestImmuneDaemon_ConcurrentAnomalyDetection(t *testing.T) {
	// Given: a running ImmuneDaemon with a profile
	dir := t.TempDir()
	store := NewImmuneStore(dir)

	profile := &NormalProfile{
		AgentTemplate: "concurrent-anomaly",
		SampleCount:   10,
		SyscallMean:   map[string]float64{"Open": 2.0},
		SyscallStdDev: map[string]float64{"Open": 0.5},
	}
	if err := store.SaveProfile(profile); err != nil {
		t.Fatalf("SaveProfile failed: %v", err)
	}

	daemon := NewImmuneDaemon(store)
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer daemon.Stop()

	var suspendMu sync.Mutex
	suspendCount := 0
	daemon.SetSuspendFunc(func(pid types.PID) error {
		suspendMu.Lock()
		defer suspendMu.Unlock()
		suspendCount++
		return nil
	})

	// When: multiple processes trigger anomalies concurrently
	const goroutines = 5
	var wg sync.WaitGroup

	for i := range goroutines {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			pid := types.PID(500 + idx)
			daemon.OnProcessStart(pid, "concurrent-anomaly")
			for j := range 20 {
				daemon.OnSyscallEvent(pid, types.SyscallEvent{
					PID:     pid,
					Syscall: "Open",
					Args:    map[string]any{"iter": j},
				})
			}
		}(i)
	}
	wg.Wait()

	// Then: no data race (test runs with -race detector)
	// And: some processes should have been suspended
	suspendMu.Lock()
	defer suspendMu.Unlock()
	if suspendCount == 0 {
		t.Error("expected at least one process to be suspended during concurrent anomaly detection")
	}
}
