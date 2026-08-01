package kernel

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/rnixai/rnix/drivers/llm"
	"github.com/rnixai/rnix/internal/config"
	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// ATDD 71.3 — compact 超时派生化（R2 + F3）。
//
// 🔴 AC1 的接线验证必须走 Spawn 真实路径断言 proc.CompactTimeout——现有 69.3 的
// 7 个用例无一执行 resolveCompactTimeout，只单测该函数本身会漏掉"忘了接线"
// （69.2 同型陷阱）。
//
// 🔴 AC1 项目级来源必须走 SpawnOpts.ProjectConfig（F2 红线）——用
// SetDriverTimeoutFunc 打桩会绕过它，缺陷在桩下不可见。
// =============================================================================

// projectConfigWithTimeout builds a ProjectConfig whose providers view declares
// a provider with the given timeout_sec — the project-level source that
// lookupProjectDriverTimeoutSec reads.
func projectConfigWithTimeout(provider string, timeoutSec int) *config.ProjectConfig {
	return &config.ProjectConfig{
		Providers: &llm.ProvidersConfig{
			Providers: []llm.ProviderConfig{{
				Name:       provider,
				Driver:     "openai-compat",
				TimeoutSec: timeoutSec,
			}},
		},
	}
}

// --- AC1: 派生接通 spawn 真实路径 ---

func TestATDD_71_3_AC1_ProjectTimeoutDerivesCompactTimeout(t *testing.T) {
	llmFile := &mockLLMFile{readData: makeLLMResponse("done", 10)}
	k, _ := newProjectWindowKernel(t, "proj-prov", llmFile)
	// 刻意不调 k.SetDriverTimeoutFunc —— 全局快照不认识这个 provider。

	pid, err := k.Spawn("project timeout", nil, SpawnOpts{
		Provider:      "proj-prov",
		ProjectConfig: projectConfigWithTimeout("proj-prov", 120), // 120s
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("pid %d missing", pid)
	}
	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	want := 120 * time.Second * compactTimeoutMultiplier // 480s = 8min
	if proc.CompactTimeout != want {
		t.Errorf("CompactTimeout = %v, want %v (project timeout_sec 120 × %d)",
			proc.CompactTimeout, want, compactTimeoutMultiplier)
	}
	if proc.compactTimeoutExplicit {
		t.Error("compactTimeoutExplicit = true, want false (derived, not explicit)")
	}
}

func TestATDD_71_3_AC1_ProjectMissFallsBackToGlobal(t *testing.T) {
	llmFile := &mockLLMFile{readData: makeLLMResponse("done", 10)}
	k, _ := newProjectWindowKernel(t, "proj-prov", llmFile)
	k.SetDriverTimeoutFunc(func(provider string) time.Duration {
		if provider == "proj-prov" {
			return 90 * time.Second
		}
		return 0
	})

	// 项目配置存在但该 provider 没有 timeout_sec —— 查表 miss。
	pc := &config.ProjectConfig{
		Providers: &llm.ProvidersConfig{
			Providers: []llm.ProviderConfig{{
				Name:   "proj-prov",
				Driver: "openai-compat",
				// TimeoutSec 未设 → 0 → miss
			}},
		},
	}

	pid, err := k.Spawn("project miss", nil, SpawnOpts{
		Provider:      "proj-prov",
		ProjectConfig: pc,
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	want := 90 * time.Second * compactTimeoutMultiplier // 360s = 6min
	if proc.CompactTimeout != want {
		t.Errorf("CompactTimeout = %v, want %v (global 90s × %d)",
			proc.CompactTimeout, want, compactTimeoutMultiplier)
	}
}

func TestATDD_71_3_AC1_BothEmptyFallsToFloor(t *testing.T) {
	llmFile := &mockLLMFile{readData: makeLLMResponse("done", 10)}
	k, _ := newProjectWindowKernel(t, "proj-prov", llmFile)
	// 无 ProjectConfig，无 driverTimeoutFunc → 落 llm.DefaultTimeout × 4。

	pid, err := k.Spawn("no source", nil, SpawnOpts{
		Provider: "proj-prov",
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	want := llm.DefaultTimeout * compactTimeoutMultiplier // 5min × 4 = 20min
	if proc.CompactTimeout != want {
		t.Errorf("CompactTimeout = %v, want %v (llm.DefaultTimeout × %d)",
			proc.CompactTimeout, want, compactTimeoutMultiplier)
	}
}

// --- AC1: 倍数常量 pin（69.4 先例）---

func TestATDD_71_3_AC1_MultiplierPinned(t *testing.T) {
	if compactTimeoutMultiplier != 4 {
		t.Errorf("compactTimeoutMultiplier = %d, want 4 (codex COMPACT_REQUEST_TIMEOUT_IDLE_MULTIPLIER)", compactTimeoutMultiplier)
	}
}

// --- AC1: 显式值优先于派生 ---

func TestATDD_71_3_AC1_ExplicitOptsBeatsDerivation(t *testing.T) {
	llmFile := &mockLLMFile{readData: makeLLMResponse("done", 10)}
	k, _ := newProjectWindowKernel(t, "proj-prov", llmFile)

	pid, err := k.Spawn("explicit wins", nil, SpawnOpts{
		Provider:       "proj-prov",
		ProjectConfig:  projectConfigWithTimeout("proj-prov", 300),
		CompactTimeout: 45 * time.Second,
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	if proc.CompactTimeout != 45*time.Second {
		t.Errorf("CompactTimeout = %v, want 45s (explicit opts must beat derivation)", proc.CompactTimeout)
	}
	if !proc.compactTimeoutExplicit {
		t.Error("compactTimeoutExplicit = false, want true (opts set it)")
	}
}

// --- AC2: 不倒挂三态 ---

func TestATDD_71_3_AC2_SmallTimeoutSecStillDerives(t *testing.T) {
	// timeout_sec调小：派生是乘法（×4），天然不倒挂。
	llmFile := &mockLLMFile{readData: makeLLMResponse("done", 10)}
	k, _ := newProjectWindowKernel(t, "prov", llmFile)

	pid, err := k.Spawn("small", nil, SpawnOpts{
		Provider:      "prov",
		ProjectConfig: projectConfigWithTimeout("prov", 10),
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	<-proc.Done

	driverTimeout := 10 * time.Second
	if proc.CompactTimeout < driverTimeout {
		t.Errorf("CompactTimeout %v < driver timeout %v (inversion)", proc.CompactTimeout, driverTimeout)
	}
	if proc.CompactTimeout != driverTimeout*compactTimeoutMultiplier {
		t.Errorf("CompactTimeout = %v, want %v", proc.CompactTimeout, driverTimeout*compactTimeoutMultiplier)
	}
}

func TestATDD_71_3_AC2_LargeTimeoutSecDerives(t *testing.T) {
	llmFile := &mockLLMFile{readData: makeLLMResponse("done", 10)}
	k, _ := newProjectWindowKernel(t, "prov", llmFile)

	pid, err := k.Spawn("large", nil, SpawnOpts{
		Provider:      "prov",
		ProjectConfig: projectConfigWithTimeout("prov", 600), // 10min
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	<-proc.Done

	want := 600 * time.Second * compactTimeoutMultiplier // 40min
	if proc.CompactTimeout != want {
		t.Errorf("CompactTimeout = %v, want %v", proc.CompactTimeout, want)
	}
}

// --- AC2-②: 显式值低于 driver 超时 → 值生效（不 clamp）---

func TestATDD_71_3_AC2_ExplicitBelowDriverNotClamped(t *testing.T) {
	llmFile := &mockLLMFile{readData: makeLLMResponse("done", 10)}
	k, _ := newProjectWindowKernel(t, "prov", llmFile)

	pid, err := k.Spawn("explicit low", nil, SpawnOpts{
		Provider:       "prov",
		ProjectConfig:  projectConfigWithTimeout("prov", 300), // driver 300s
		CompactTimeout: 5 * time.Second,                       // 显式 5s < 300s
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	<-proc.Done

	// 值生效，不被 clamp（警告走 log.Printf，不在返回值中）。
	if proc.CompactTimeout != 5*time.Second {
		t.Errorf("CompactTimeout = %v, want 5s (explicit value must not be clamped)", proc.CompactTimeout)
	}
}

// --- AC2-③: 溢出防御 ---

func TestATDD_71_3_AC2_OverflowClampedToPositive(t *testing.T) {
	// 极大 timeout_sec → driverTimeout ×4 溢出为负 → 必须钳到正数。
	llmFile := &mockLLMFile{readData: makeLLMResponse("done", 10)}
	k, _ := newProjectWindowKernel(t, "prov", llmFile)

	// 3e9 秒 ≈ 95 年 → time.Duration = 3e18 ns（正，但 > MaxInt64/4 ≈ 2.3e18）。
	hugeSec := int(math.MaxInt64 / int64(time.Second) / 2) // ≈ 4.6e9 秒，Duration 正但 ×4 溢出
	pid, err := k.Spawn("overflow", nil, SpawnOpts{
		Provider:      "prov",
		ProjectConfig: projectConfigWithTimeout("prov", hugeSec),
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	<-proc.Done

	if proc.CompactTimeout <= 0 {
		t.Fatalf("CompactTimeout = %v, must be positive (overflow must clamp, not wrap negative)", proc.CompactTimeout)
	}
	want := llm.DefaultTimeout * compactTimeoutMultiplier // 20min ceiling
	if proc.CompactTimeout != want {
		t.Errorf("CompactTimeout = %v, want %v (clamped to DefaultTimeout × multiplier)", proc.CompactTimeout, want)
	}
	// AC2: 派生值仍 ≥ driver 超时（在钳位场景下 driver 超时本身已溢出，
	// 但钳位值 20min 仍是一个合理的正数上界）。
	if proc.CompactTimeout < llm.DefaultTimeout {
		t.Errorf("CompactTimeout %v < llm.DefaultTimeout %v", proc.CompactTimeout, llm.DefaultTimeout)
	}
}

// --- AC5: 显式值落盘往返 ---

func TestATDD_71_3_AC5_ExplicitCompactTimeoutRoundTrip(t *testing.T) {
	info := vfs.ProcInfo{
		PID:            42,
		UUID:           "test-uuid",
		State:          3, // Dead
		CompactTimeout: 90 * time.Second,
	}
	disk := procInfoToDisk(info)
	if disk.CompactTimeoutMs != 90_000 {
		t.Fatalf("CompactTimeoutMs = %d, want 90000", disk.CompactTimeoutMs)
	}

	// JSON 往返（模拟真实落盘 + 读盘）。
	raw, err := json.Marshal(disk)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var disk2 procInfoDisk
	if err := json.Unmarshal(raw, &disk2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	restored := procInfoFromDisk(disk2)
	if restored.CompactTimeout != 90*time.Second {
		t.Errorf("round-trip CompactTimeout = %v, want 90s", restored.CompactTimeout)
	}
}

func TestATDD_71_3_AC5_DerivedValueNotPersisted(t *testing.T) {
	// 派生值（CompactTimeout > 0 但非显式）不应落盘——GetProcInfo 只在
	// compactTimeoutExplicit 时填 ProcInfo.CompactTimeout，故此处模拟
	// GetProcInfo 的输出：CompactTimeout = 0。
	info := vfs.ProcInfo{
		PID:            42,
		UUID:           "test-uuid",
		State:          3,
		CompactTimeout: 0, // 派生值不落盘
	}
	disk := procInfoToDisk(info)
	if disk.CompactTimeoutMs != 0 {
		t.Fatalf("CompactTimeoutMs = %d, want 0 (derived values must not persist)", disk.CompactTimeoutMs)
	}

	raw, _ := json.Marshal(disk)
	// omitempty → JSON 中无该键。
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if _, exists := m["compact_timeout_ms"]; exists {
		t.Error("compact_timeout_ms present in JSON for a derived-value process; omitempty must omit it")
	}
}

func TestATDD_71_3_AC5_LegacySnapshotOmitted(t *testing.T) {
	// 旧 proc-info.json 无 compact_timeout_ms 键 → 反序列化为 0 → resume 重新派生。
	raw := []byte(`{"pid":1,"uuid":"legacy","state":"dead","intent":"x","tokens_used":0,"created_at":"2026-01-01T00:00:00Z","ctx_id":0,"pipeline_index":0,"pipeline_total":0}`)
	var disk procInfoDisk
	if err := json.Unmarshal(raw, &disk); err != nil {
		t.Fatalf("unmarshal legacy: %v", err)
	}
	info := procInfoFromDisk(disk)
	if info.CompactTimeout != 0 {
		t.Errorf("legacy CompactTimeout = %v, want 0 (absent key → re-derive on resume)", info.CompactTimeout)
	}
}

// --- AC5: resume 重新派生（resolveCompactTimeout 单元验证）---

func TestATDD_71_3_AC5_ResolveDerivesWhenFieldZero(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	v := vfs.NewVFS(reg)
	k := NewKernel(v, nil, nil)
	t.Cleanup(k.Shutdown)

	proc := NewProcess(0, "resume derive", nil)
	proc.Provider = "prov"
	proc.ProjectConfig = projectConfigWithTimeout("prov", 200)
	// CompactTimeout == 0 → resolveCompactTimeout 应填派生值。

	k.resolveCompactTimeout(proc)

	want := 200 * time.Second * compactTimeoutMultiplier
	if proc.CompactTimeout != want {
		t.Errorf("CompactTimeout = %v, want %v (resume re-derivation)", proc.CompactTimeout, want)
	}
}

func TestATDD_71_3_AC5_ResolveSkipsExplicitValue(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	v := vfs.NewVFS(reg)
	k := NewKernel(v, nil, nil)
	t.Cleanup(k.Shutdown)

	proc := NewProcess(0, "resume explicit", nil)
	proc.Provider = "prov"
	proc.ProjectConfig = projectConfigWithTimeout("prov", 200)
	proc.CompactTimeout = 30 * time.Second // 显式值（已从 proc-info.json 重放）
	proc.compactTimeoutExplicit = true

	k.resolveCompactTimeout(proc)

	if proc.CompactTimeout != 30*time.Second {
		t.Errorf("CompactTimeout = %v, want 30s (explicit value must not be overridden by derivation)", proc.CompactTimeout)
	}
}

// --- AC6-②: 0 仍回落地板（不得禁用）---

func TestATDD_71_3_AC6_ZeroStillFallsBackToFloor(t *testing.T) {
	proc := NewProcess(0, "zero guard", nil)
	proc.CompactTimeout = 0
	if got := proc.effectiveCompactTimeout(); got != DefaultCompactTimeout {
		t.Errorf("effectiveCompactTimeout() = %v, want %v (0 must mean floor, not disabled)", got, DefaultCompactTimeout)
	}
}

// --- AC6-①: 5 个自由函数用例仍绿（由 atdd_69_3 文件覆盖，此处仅确认编译）---
// TestCompactTimeout_ManifestApplied / _OptsOverridesManifest /
// _InvalidManifestStringFallsBackToDefault / _ManifestZeroIsNoop /
// _NoAgentUsesDefault 均在 atdd_69_3_compact_timeout_test.go 中，
// 本 story 未改动它们。

// --- lookupProjectDriverTimeoutSec 降级（miss-never-panic）---

func TestATDD_71_3_LookupNilConfigIsMiss(t *testing.T) {
	if got := lookupProjectDriverTimeoutSec(nil, "prov"); got != 0 {
		t.Errorf("nil config → %d, want 0", got)
	}
}

func TestATDD_71_3_LookupWrongTypeIsMiss(t *testing.T) {
	pc := &config.ProjectConfig{Providers: "not a *llm.ProvidersConfig"}
	if got := lookupProjectDriverTimeoutSec(pc, "prov"); got != 0 {
		t.Errorf("wrong type → %d, want 0 (miss, not panic)", got)
	}
}

func TestATDD_71_3_LookupEmptyProviderIsMiss(t *testing.T) {
	pc := projectConfigWithTimeout("prov", 100)
	if got := lookupProjectDriverTimeoutSec(pc, ""); got != 0 {
		t.Errorf("empty provider → %d, want 0", got)
	}
}

func TestATDD_71_3_LookupProviderNotFoundIsMiss(t *testing.T) {
	pc := projectConfigWithTimeout("other", 100)
	if got := lookupProjectDriverTimeoutSec(pc, "prov"); got != 0 {
		t.Errorf("unknown provider → %d, want 0", got)
	}
}
