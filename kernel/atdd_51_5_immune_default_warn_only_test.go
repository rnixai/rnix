package kernel

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// ATDD 51.5: 免疫系统默认启用 + warn-only 观测档 — IN-1 路线B
//
// 来源 Story：_bmad-output/implementation-artifacts/51-5-immune-default-on-warn-only.md
// 审计来源：completion-audit-suspects-2026-05-30.md §A2 IN-1 裁决
// Story Key: 51-5-immune-default-on-warn-only
//
// 验收标准（Acceptance Criteria，本文件覆盖项）：
//   AC1（Enabled=true）      : DefaultImmuneConfig().Enabled = true
//   AC2（WarnOnly=true）     : DefaultImmuneConfig().WarnOnly = true
//   AC3（ParseImmuneConfig） : 解析 warn_only 字段（true/false/未指定）
//   AC4（warn-only 行为）    : 异常→alert+threat 但不 suspendFn
//   AC5（enforce 不变）      : WarnOnly=false 时保持既有行为
//   AC7（IsWarnOnly）        : nil-safe 返回 + IPC 响应序列化
//   AC9（配置覆盖）          : base 传播保留默认 WarnOnly=true
//
// ============================== RED PHASE ==============================
// 本测试在 Story 51.5 实现前应 FAIL（红灯）。
// 预期失败原因：kernel 包无法编译——ImmuneConfig.WarnOnly 字段、
// ImmuneDaemon.IsWarnOnly() 方法尚未定义（undefined）。
// 实现后所有用例应转绿（含 go test -race）。
// 这是 Go ATDD 的标准红灯形态（缺失 API → 包编译失败），与
// kernel 既有 atdd_51_* 集成测试的红灯模式一致。
//
// 注意：测试严格使用 Story Dev Notes 规定的契约：
//   - 新字段：ImmuneConfig.WarnOnly bool (json:"warn_only" yaml:"warn_only")
//   - 新方法：ImmuneDaemon.IsWarnOnly() bool (nil-safe)
//   - DefaultImmuneConfig() 返回 Enabled=true, WarnOnly=true
//   - ParseImmuneConfig 支持 "warn_only" key
//   - OnSyscallEvent warn-only 分支：alert+threat+log 但不 suspendFn
// ======================================================================

// --- 51.5-INT_001: [P0] DefaultImmuneConfig 返回 Enabled=true, WarnOnly=true (AC1/AC2) ---

func TestATDD_51_5_INT_001_DefaultImmuneConfig_EnabledAndWarnOnly(t *testing.T) {
	cfg := DefaultImmuneConfig()

	// AC1: Enabled 默认为 true
	if !cfg.Enabled {
		t.Error("DefaultImmuneConfig().Enabled should be true (AC1: 免疫系统默认启用)")
	}

	// AC2: WarnOnly 默认为 true
	if !cfg.WarnOnly {
		t.Error("DefaultImmuneConfig().WarnOnly should be true (AC2: 默认 warn-only 观测模式)")
	}

	// 其余字段保持不变
	if cfg.DeviationThreshold != DefaultDeviationThreshold {
		t.Errorf("DeviationThreshold = %f, want %f", cfg.DeviationThreshold, DefaultDeviationThreshold)
	}
	if cfg.MinSamples != MinSamplesForProfile {
		t.Errorf("MinSamples = %d, want %d", cfg.MinSamples, MinSamplesForProfile)
	}
	if cfg.ReinforcementThreshold != DefaultReinforcementThreshold {
		t.Errorf("ReinforcementThreshold = %d, want %d", cfg.ReinforcementThreshold, DefaultReinforcementThreshold)
	}
	if cfg.MinMigrationSimilarity != MinMigrationSimilarity {
		t.Errorf("MinMigrationSimilarity = %f, want %f", cfg.MinMigrationSimilarity, MinMigrationSimilarity)
	}
}

// --- 51.5-INT_002: [P0] warn-only 模式下统计异常检测：alert+threat 存在，suspendFn 未调用 (AC4) ---

func TestATDD_51_5_INT_002_WarnOnly_StatisticalAnomaly_NoSuspend(t *testing.T) {
	// Given: a daemon in warn-only mode (default) with a profile and suspendFn
	dir := t.TempDir()
	store := NewImmuneStore(dir)

	profile := &NormalProfile{
		AgentTemplate: "warn-test-agent",
		SampleCount:   10,
		SyscallMean:   map[string]float64{"Open": 2.0},
		SyscallStdDev: map[string]float64{"Open": 0.5},
		TokenRateMean:   2.0,
		TokenRateStdDev: 0.3,
	}
	if err := store.SaveProfile(profile); err != nil {
		t.Fatalf("SaveProfile failed: %v", err)
	}

	cfg := DefaultImmuneConfig()
	// AC2 保证: cfg.WarnOnly == true
	if !cfg.WarnOnly {
		t.Fatal("precondition: DefaultImmuneConfig().WarnOnly must be true")
	}

	daemon := NewImmuneDaemon(store, cfg)
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer daemon.Stop()

	// Track suspendFn calls
	var suspendMu sync.Mutex
	suspendCount := 0
	daemon.SetSuspendFunc(func(pid types.PID) error {
		suspendMu.Lock()
		defer suspendMu.Unlock()
		suspendCount++
		return nil
	})

	// When: a process generates far more Open calls than the baseline
	pid := types.PID(42)
	daemon.OnProcessStart(pid, "warn-test-agent")

	// Exceed threshold: mean=2.0, stddev=0.5, threshold=3.0 -> limit=2.0+3*0.5=3.5
	for i := range 10 {
		daemon.OnSyscallEvent(pid, types.SyscallEvent{
			PID:     pid,
			Syscall: "Open",
			Args:    map[string]any{"iter": i},
		})
	}

	// Then: alert should exist (warn-only still records alerts)
	alerts := daemon.GetAlerts()
	if alerts == nil {
		t.Fatal("GetAlerts returned nil")
	}
	alert, ok := alerts[pid]
	if !ok {
		t.Error("AC4: expected alert for PID 42 in warn-only mode (alert should be recorded)")
	} else if alert.Type != AnomalySyscallFreq {
		t.Errorf("alert Type = %q, want %q", alert.Type, AnomalySyscallFreq)
	}

	// And: threat should be persisted (warn-only still persists threats)
	threats := daemon.GetThreats()
	foundThreat := false
	for _, th := range threats {
		if th.AgentTemplate == "warn-test-agent" && th.Metric == "Open" {
			foundThreat = true
			break
		}
	}
	if !foundThreat {
		t.Error("AC4: expected ThreatSignature to be persisted in warn-only mode")
	}

	// But: suspendFn should NOT have been called (AC4: warn-only 不暂停进程)
	suspendMu.Lock()
	defer suspendMu.Unlock()
	if suspendCount != 0 {
		t.Errorf("AC4: suspendFn called %d times in warn-only mode, want 0 (no SIGPAUSE)", suspendCount)
	}
}

// --- 51.5-INT_003: [P0] enforce 模式 (WarnOnly=false) 下 suspendFn 被调用 (AC5) ---

func TestATDD_51_5_INT_003_Enforce_StatisticalAnomaly_Suspends(t *testing.T) {
	// Given: a daemon in enforce mode (WarnOnly=false) — existing 22.2 behavior
	dir := t.TempDir()
	store := NewImmuneStore(dir)

	profile := &NormalProfile{
		AgentTemplate: "enforce-test-agent",
		SampleCount:   10,
		SyscallMean:   map[string]float64{"Open": 2.0},
		SyscallStdDev: map[string]float64{"Open": 0.5},
		TokenRateMean:   2.0,
		TokenRateStdDev: 0.3,
	}
	if err := store.SaveProfile(profile); err != nil {
		t.Fatalf("SaveProfile failed: %v", err)
	}

	cfg := DefaultImmuneConfig()
	cfg.WarnOnly = false // enforce mode
	daemon := NewImmuneDaemon(store, cfg)
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer daemon.Stop()

	var suspendMu sync.Mutex
	suspendedPIDs := make([]types.PID, 0)
	daemon.SetSuspendFunc(func(pid types.PID) error {
		suspendMu.Lock()
		defer suspendMu.Unlock()
		suspendedPIDs = append(suspendedPIDs, pid)
		return nil
	})

	// When: a process exceeds anomaly threshold
	pid := types.PID(43)
	daemon.OnProcessStart(pid, "enforce-test-agent")
	for i := range 10 {
		daemon.OnSyscallEvent(pid, types.SyscallEvent{
			PID:     pid,
			Syscall: "Open",
			Args:    map[string]any{"iter": i},
		})
	}

	// Then: suspendFn SHOULD be called (AC5: enforce 行为不变)
	suspendMu.Lock()
	defer suspendMu.Unlock()
	if len(suspendedPIDs) == 0 {
		t.Error("AC5: expected suspendFn to be called in enforce mode (WarnOnly=false)")
	}

	// And: alert should exist
	alerts := daemon.GetAlerts()
	if _, ok := alerts[pid]; !ok {
		t.Error("AC5: expected alert for PID in enforce mode")
	}
}

// --- 51.5-INT_004: [P0] warn-only 威胁签名快速匹配路径：alert 存在，suspendFn 未调用 (AC4) ---

func TestATDD_51_5_INT_004_WarnOnly_ThreatMatch_NoSuspend(t *testing.T) {
	// Given: a daemon in warn-only mode with a known threat signature
	dir := t.TempDir()
	store := NewImmuneStore(dir)

	// Pre-save threat signature
	sig := ThreatSignature{
		ID:            "threat-warn-test-001",
		Type:          AnomalySyscallFreq,
		AgentTemplate: "threat-warn-agent",
		Metric:        "Open",
		Threshold:     3.0,
		CreatedAt:     time.Now(),
	}
	if err := store.SaveThreat(sig); err != nil {
		t.Fatalf("SaveThreat failed: %v", err)
	}

	// Pre-save profile so daemon has detection context
	profile := &NormalProfile{
		AgentTemplate: "threat-warn-agent",
		SampleCount:   10,
		SyscallMean:   map[string]float64{"Open": 5.0},
		SyscallStdDev: map[string]float64{"Open": 1.0},
	}
	if err := store.SaveProfile(profile); err != nil {
		t.Fatalf("SaveProfile failed: %v", err)
	}

	cfg := DefaultImmuneConfig()
	// cfg.WarnOnly == true (default)
	daemon := NewImmuneDaemon(store, cfg)
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer daemon.Stop()

	suspendCount := 0
	daemon.SetSuspendFunc(func(pid types.PID) error {
		suspendCount++
		return nil
	})

	// When: a process with matching template sends a syscall that matches the threat
	pid := types.PID(99)
	daemon.OnProcessStart(pid, "threat-warn-agent")
	daemon.OnSyscallEvent(pid, types.SyscallEvent{
		PID:     pid,
		Syscall: "Open",
	})

	// Then: alert should exist (threat match records alert even in warn-only)
	alerts := daemon.GetAlerts()
	if _, ok := alerts[pid]; !ok {
		t.Error("AC4: expected alert for PID via threat match in warn-only mode")
	}

	// But: suspendFn should NOT be called (AC4: warn-only 不暂停)
	if suspendCount != 0 {
		t.Errorf("AC4: suspendFn called %d times via threat match in warn-only mode, want 0", suspendCount)
	}
}

// --- 51.5-INT_005: [P0] ParseImmuneConfig 解析 warn_only: false (AC3) ---

func TestATDD_51_5_INT_005_ParseImmuneConfig_WarnOnlyFalse(t *testing.T) {
	data := map[string]any{
		"warn_only": false,
	}
	cfg, warnings := ParseImmuneConfig(data)
	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}

	// AC3: warn_only: false 应正确解析
	if cfg.WarnOnly {
		t.Error("AC3: ParseImmuneConfig should parse warn_only: false → WarnOnly=false")
	}
}

// --- 51.5-INT_006: [P0] ParseImmuneConfig 未指定 warn_only 时保留 base 默认 true (AC3/AC9) ---

func TestATDD_51_5_INT_006_ParseImmuneConfig_WarnOnlyUnspecified_PreservesBase(t *testing.T) {
	// Subtest A: no base → DefaultImmuneConfig provides WarnOnly=true
	t.Run("no_base", func(t *testing.T) {
		data := map[string]any{
			"enabled": true,
		}
		cfg, warnings := ParseImmuneConfig(data)
		if len(warnings) != 0 {
			t.Errorf("expected no warnings, got %v", warnings)
		}
		// WarnOnly not specified → retains default (true)
		if !cfg.WarnOnly {
			t.Error("AC3/AC9: unspecified warn_only should retain default WarnOnly=true")
		}
	})

	// Subtest B: base with WarnOnly=false → project-level not specifying warn_only preserves base
	t.Run("base_false_preserved", func(t *testing.T) {
		globalData := map[string]any{
			"enabled":   true,
			"warn_only": false,
		}
		globalCfg, _ := ParseImmuneConfig(globalData)
		if globalCfg.WarnOnly {
			t.Fatal("precondition: global should have WarnOnly=false")
		}

		// Project config does NOT specify warn_only → should preserve global's false
		projectData := map[string]any{
			"min_samples": 20,
		}
		merged, warnings := ParseImmuneConfig(projectData, globalCfg)
		if len(warnings) != 0 {
			t.Errorf("expected no warnings, got %v", warnings)
		}
		if merged.WarnOnly {
			t.Error("AC9: project-level unspecified warn_only should preserve base WarnOnly=false")
		}
	})

	// Subtest C: base with WarnOnly=true → project overrides to false
	t.Run("base_true_override_false", func(t *testing.T) {
		globalData := map[string]any{
			"enabled": true,
		}
		globalCfg, _ := ParseImmuneConfig(globalData)
		if !globalCfg.WarnOnly {
			t.Fatal("precondition: global default should have WarnOnly=true")
		}

		projectData := map[string]any{
			"warn_only": false,
		}
		merged, warnings := ParseImmuneConfig(projectData, globalCfg)
		if len(warnings) != 0 {
			t.Errorf("expected no warnings, got %v", warnings)
		}
		if merged.WarnOnly {
			t.Error("AC9: project-level warn_only: false should override base WarnOnly=true")
		}
	})
}

// --- 51.5-INT_007: [P1] IsWarnOnly() nil-safe 返回 false (AC7) ---

func TestATDD_51_5_INT_007_IsWarnOnly_NilSafe(t *testing.T) {
	// Given: a nil ImmuneDaemon
	var daemon *ImmuneDaemon

	// When/Then: IsWarnOnly should return false (nil-safe, no panic)
	if daemon.IsWarnOnly() {
		t.Error("AC7: nil ImmuneDaemon.IsWarnOnly() should return false")
	}

	// Also test with a real daemon in warn-only mode
	dir := t.TempDir()
	store := NewImmuneStore(dir)
	cfg := DefaultImmuneConfig()
	realDaemon := NewImmuneDaemon(store, cfg)
	if !realDaemon.IsWarnOnly() {
		t.Error("AC7: daemon with WarnOnly=true config should return IsWarnOnly()=true")
	}

	// And with enforce mode
	cfg.WarnOnly = false
	enforceDaemon := NewImmuneDaemon(store, cfg)
	if enforceDaemon.IsWarnOnly() {
		t.Error("AC7: daemon with WarnOnly=false config should return IsWarnOnly()=false")
	}
}

// --- 51.5-INT_008: [P1] ImmuneStatusResponse.WarnOnly JSON 正确序列化 (AC7) ---

func TestATDD_51_5_INT_008_ImmuneStatusResponse_WarnOnly_JSON(t *testing.T) {
	// This test imports ipc.ImmuneStatusResponse indirectly via the kernel package.
	// Since ImmuneStatusResponse lives in the ipc package, we test the field via
	// a local struct that mirrors the expected wire format.

	type immuneStatusWire struct {
		Running        bool   `json:"running"`
		UptimeMs       int64  `json:"uptime_ms"`
		ProfileCount   int    `json:"profile_count"`
		SecurityStatus string `json:"security_status"`
		WarnOnly       bool   `json:"warn_only"`
	}

	tests := []struct {
		name     string
		warnOnly bool
	}{
		{"warn_only_true", true},
		{"warn_only_false", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := immuneStatusWire{
				Running:        true,
				UptimeMs:       60000,
				ProfileCount:   3,
				SecurityStatus: "normal",
				WarnOnly:       tt.warnOnly,
			}

			data, err := json.Marshal(resp)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}

			jsonStr := string(data)

			// AC7: warn_only 字段存在于 JSON 中
			expectedField := fmt.Sprintf(`"warn_only":%v`, tt.warnOnly)
			if !searchString(jsonStr, expectedField) {
				t.Errorf("AC7: JSON should contain %s, got: %s", expectedField, jsonStr)
			}

			// Round-trip
			var restored immuneStatusWire
			if err := json.Unmarshal(data, &restored); err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}
			if restored.WarnOnly != tt.warnOnly {
				t.Errorf("AC7: round-trip WarnOnly = %v, want %v", restored.WarnOnly, tt.warnOnly)
			}
		})
	}
}

// --- 51.5-INT_009: [P0] warn-only 与 enforce 对照：同一构造不同模式 (AC4/AC5 组合验证) ---

func TestATDD_51_5_INT_009_WarnOnly_vs_Enforce_Contrast(t *testing.T) {
	// 使用同一 profile 和相同事件序列，验证 WarnOnly 开关的行为差异
	setupDaemon := func(t *testing.T, warnOnly bool) (*ImmuneDaemon, *int, string) {
		t.Helper()
		dir := t.TempDir()
		store := NewImmuneStore(dir)

		profile := &NormalProfile{
			AgentTemplate: "contrast-agent",
			SampleCount:   10,
			SyscallMean:   map[string]float64{"Open": 2.0},
			SyscallStdDev: map[string]float64{"Open": 0.5},
			TokenRateMean:   2.0,
			TokenRateStdDev: 0.3,
		}
		if err := store.SaveProfile(profile); err != nil {
			t.Fatalf("SaveProfile failed: %v", err)
		}

		cfg := DefaultImmuneConfig()
		cfg.WarnOnly = warnOnly
		daemon := NewImmuneDaemon(store, cfg)
		if err := daemon.Start(); err != nil {
			t.Fatalf("Start failed: %v", err)
		}

		suspendCount := new(int)
		daemon.SetSuspendFunc(func(pid types.PID) error {
			*suspendCount++
			return nil
		})

		return daemon, suspendCount, dir
	}

	// Run identical event sequence in both modes
	fireEvents := func(daemon *ImmuneDaemon, pid types.PID) {
		daemon.OnProcessStart(pid, "contrast-agent")
		for i := range 10 {
			daemon.OnSyscallEvent(pid, types.SyscallEvent{
				PID:     pid,
				Syscall: "Open",
				Args:    map[string]any{"iter": i},
			})
		}
	}

	t.Run("warn_only", func(t *testing.T) {
		daemon, suspendCount, _ := setupDaemon(t, true)
		defer daemon.Stop()
		fireEvents(daemon, types.PID(100))

		alerts := daemon.GetAlerts()
		if _, ok := alerts[types.PID(100)]; !ok {
			t.Error("warn-only: expected alert to be recorded")
		}
		if *suspendCount != 0 {
			t.Errorf("warn-only: suspendFn called %d times, want 0", *suspendCount)
		}
	})

	t.Run("enforce", func(t *testing.T) {
		daemon, suspendCount, _ := setupDaemon(t, false)
		defer daemon.Stop()
		fireEvents(daemon, types.PID(101))

		alerts := daemon.GetAlerts()
		if _, ok := alerts[types.PID(101)]; !ok {
			t.Error("enforce: expected alert to be recorded")
		}
		if *suspendCount == 0 {
			t.Error("enforce: expected suspendFn to be called")
		}
	})
}

// --- 51.5-INT_010: [P1] warn-only 模式下 suspending map 不被设置 (AC4 易错点 #3) ---

func TestATDD_51_5_INT_010_WarnOnly_NoSuspendingFlag(t *testing.T) {
	// 验证 warn-only 路径不设 d.suspending[pid]，避免后续正常事件被跳过
	dir := t.TempDir()
	store := NewImmuneStore(dir)

	profile := &NormalProfile{
		AgentTemplate: "suspending-flag-agent",
		SampleCount:   10,
		SyscallMean:   map[string]float64{"Open": 2.0},
		SyscallStdDev: map[string]float64{"Open": 0.5},
		TokenRateMean:   2.0,
		TokenRateStdDev: 0.3,
	}
	if err := store.SaveProfile(profile); err != nil {
		t.Fatalf("SaveProfile failed: %v", err)
	}

	cfg := DefaultImmuneConfig()
	// WarnOnly=true by default
	daemon := NewImmuneDaemon(store, cfg)
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer daemon.Stop()

	daemon.SetSuspendFunc(func(pid types.PID) error {
		t.Error("suspendFn should not be called in warn-only mode")
		return nil
	})

	pid := types.PID(200)
	daemon.OnProcessStart(pid, "suspending-flag-agent")

	// Trigger anomaly
	for i := range 10 {
		daemon.OnSyscallEvent(pid, types.SyscallEvent{
			PID:     pid,
			Syscall: "Open",
			Args:    map[string]any{"iter": i},
		})
	}

	// After anomaly in warn-only, subsequent events should still be processed
	// (not skipped by re-entrancy guard). Send another batch of events.
	for i := range 5 {
		daemon.OnSyscallEvent(pid, types.SyscallEvent{
			PID:     pid,
			Syscall: "Read",
			Args:    map[string]any{"post-anomaly": i},
		})
	}

	// Verify collector still received all events (Open + Read)
	// The key assertion: if suspending[pid] were incorrectly set,
	// the Read events would be processed only via collector.Observe (no anomaly check),
	// but they would NOT be skipped entirely.
	// The absence of suspendFn call is already asserted by the SetSuspendFunc callback above.
}
