package kernel

// IN-3 免疫误报退化修复（F1-F6）验收测试。
// 来源：_bmad-output/implementation-artifacts/spec-in-3-immune-false-positive-fix.md
// 覆盖 spec I/O & Edge-Case Matrix 的全部场景 + AC。

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// --- F2: 三重门（统计门 + 绝对地板 + 历史最大值增长门） ---

// 稀有 syscall 首调不报警：复现卷宗 mean=1/60 形态（Shutdown 60 个样本出现 1 次）。
// 旧逻辑单次调用即报 60x 偏差；新地板门（cur<=5）拦下。
func TestCheckSyscallAnomaly_RareSyscallFirstCall_NoAlert(t *testing.T) {
	d := NewAnomalyDetector(3.0)
	profile := &NormalProfile{
		AgentTemplate: "sa-orchestrator",
		SampleCount:   60,
		SyscallMean:   map[string]float64{"Shutdown": 1.0 / 60.0},
		SyscallStdDev: map[string]float64{"Shutdown": 0.128},
		SyscallMax:    map[string]float64{"Shutdown": 1},
	}

	if alert := d.CheckSyscallAnomaly(types.PID(1), "sa-orchestrator", "Shutdown", 1, profile); alert != nil {
		t.Errorf("first call of a rare syscall must not alert (base-rate fallacy), got deviation %.1fx", alert.Deviation)
	}
}

// 长任务重尾不报警：复现卷宗 ReasonStep mean=6.31/σ=15.12 形态，
// 88 步越过 mean+3σ=51.66 但未越过 histMax×1.5=90。
func TestCheckSyscallAnomaly_HeavyTailLongTask_NoAlert(t *testing.T) {
	d := NewAnomalyDetector(3.0)
	profile := &NormalProfile{
		AgentTemplate: "sa-orchestrator",
		SampleCount:   81,
		SyscallMean:   map[string]float64{"ReasonStep": 6.31},
		SyscallStdDev: map[string]float64{"ReasonStep": 15.12},
		SyscallMax:    map[string]float64{"ReasonStep": 60},
	}

	if alert := d.CheckSyscallAnomaly(types.PID(1), "sa-orchestrator", "ReasonStep", 88, profile); alert != nil {
		t.Errorf("88 steps with histMax=60 must not alert (88 <= 60*1.5), got deviation %.1fx", alert.Deviation)
	}
}

// 真异常：三门全过（统计门 + 地板 + 增长门）才报警，Deviation 为当前实测。
func TestCheckSyscallAnomaly_GenuineAnomaly_Alerts(t *testing.T) {
	d := NewAnomalyDetector(3.0)
	profile := &NormalProfile{
		AgentTemplate: "sa-orchestrator",
		SampleCount:   81,
		SyscallMean:   map[string]float64{"ReasonStep": 6.31},
		SyscallStdDev: map[string]float64{"ReasonStep": 15.12},
		SyscallMax:    map[string]float64{"ReasonStep": 60},
	}

	alert := d.CheckSyscallAnomaly(types.PID(1), "sa-orchestrator", "ReasonStep", 500, profile)
	if alert == nil {
		t.Fatal("500 calls (all three gates exceeded) must alert")
	}
	wantDev := 500.0 / 6.31
	if alert.Deviation < wantDev-0.1 || alert.Deviation > wantDev+0.1 {
		t.Errorf("Deviation = %.2f, want measured ratio ≈ %.2f", alert.Deviation, wantDev)
	}
}

// 旧 profile 无 syscall_max 字段：nil map 安全，退化为统计门+地板。
func TestCheckSyscallAnomaly_LegacyProfileNoSyscallMax_NilSafe(t *testing.T) {
	d := NewAnomalyDetector(3.0)
	profile := &NormalProfile{
		AgentTemplate: "legacy-agent",
		SampleCount:   10,
		SyscallMean:   map[string]float64{"Open": 2.0},
		SyscallStdDev: map[string]float64{"Open": 0.5},
		// SyscallMax 缺失（legacy 磁盘 profile 反序列化后为 nil）
	}

	// 增长门跳过：cur=10 > mean+3σ=3.5 且 > 地板 5 → 报警，不 panic
	if alert := d.CheckSyscallAnomaly(types.PID(1), "legacy-agent", "Open", 10, profile); alert == nil {
		t.Error("legacy profile without syscall_max should fall back to statistical+floor gates and alert at cur=10")
	}
	// 地板仍生效
	if alert := d.CheckSyscallAnomaly(types.PID(1), "legacy-agent", "Open", 4, profile); alert != nil {
		t.Error("cur=4 <= floor must not alert even above mean+3σ")
	}
}

// ComputeProfile 计算 SyscallMax。
func TestComputeProfile_SyscallMax(t *testing.T) {
	samples := []BehaviorSample{
		{AgentTemplate: "a", SyscallCounts: map[string]int{"Open": 3}},
		{AgentTemplate: "a", SyscallCounts: map[string]int{"Open": 9}},
		{AgentTemplate: "a", SyscallCounts: map[string]int{"Open": 5}},
		{AgentTemplate: "a", SyscallCounts: map[string]int{"Open": 1}},
		{AgentTemplate: "a", SyscallCounts: map[string]int{"Open": 2}},
	}
	profile := ComputeProfile("a", samples, 5)
	if profile == nil {
		t.Fatal("expected profile from 5 samples")
	}
	if got := profile.SyscallMax["Open"]; got != 9 {
		t.Errorf("SyscallMax[Open] = %.0f, want 9", got)
	}
}

// --- F1: 签名不再独立报警 + 不重复铸签名 ---

// 签名命中且统计越界时不重复铸签名（威胁库大小不变）。
func TestOnSyscallEvent_MatchedSignature_NotReMinted(t *testing.T) {
	dir := t.TempDir()
	seed := NewImmuneStore(filepath.Join(dir, "global"))

	sig := ThreatSignature{
		ID: "threat-fixed", Type: AnomalySyscallFreq,
		AgentTemplate: "agent-x", Metric: "Open",
		Threshold: 3.0, CreatedAt: time.Now(),
	}
	if err := seed.RewriteThreats([]ThreatSignature{sig}); err != nil {
		t.Fatalf("RewriteThreats: %v", err)
	}
	profile := &NormalProfile{
		AgentTemplate: "agent-x", SampleCount: 10,
		SyscallMean:   map[string]float64{"Open": 2.0},
		SyscallStdDev: map[string]float64{"Open": 0.5},
	}
	if err := seed.SaveProfile(profile); err != nil {
		t.Fatalf("SaveProfile: %v", err)
	}

	daemon := NewImmuneDaemon(NewImmuneStore(dir), DefaultImmuneConfig())
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer daemon.Stop()

	pid := types.PID(7)
	daemon.OnProcessStart(pid, "agent-x", "global")
	for range 10 {
		daemon.OnSyscallEvent(pid, types.SyscallEvent{PID: pid, Syscall: "Open"})
	}

	threats := daemon.GetThreats()
	if len(threats) != 1 {
		t.Fatalf("matched signature must not be re-minted: want 1 threat, got %d", len(threats))
	}
	if threats[0].ID != "threat-fixed" {
		t.Errorf("threat ID = %q, want the original threat-fixed", threats[0].ID)
	}
}

// 未命中签名时铸签名走 upsert：同键第二次报警不追加。
func TestOnSyscallEvent_MintedSignature_UpsertNotAppend(t *testing.T) {
	dir := t.TempDir()
	seed := NewImmuneStore(filepath.Join(dir, "global"))
	profile := &NormalProfile{
		AgentTemplate: "agent-y", SampleCount: 10,
		SyscallMean:   map[string]float64{"Open": 2.0},
		SyscallStdDev: map[string]float64{"Open": 0.5},
	}
	if err := seed.SaveProfile(profile); err != nil {
		t.Fatalf("SaveProfile: %v", err)
	}

	daemon := NewImmuneDaemon(NewImmuneStore(dir), DefaultImmuneConfig())
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer daemon.Stop()

	pid := types.PID(8)
	daemon.OnProcessStart(pid, "agent-y", "global")
	// 第 6 发首次报警并铸签名；后续每发均命中签名（统计仍越界）但不再追加
	for range 20 {
		daemon.OnSyscallEvent(pid, types.SyscallEvent{PID: pid, Syscall: "Open"})
	}

	if got := len(daemon.GetThreats()); got != 1 {
		t.Errorf("same-key signature must be upserted, not appended: want 1, got %d", got)
	}

	// 磁盘一致：threats.jsonl 只有一行
	loaded, err := seed.LoadThreats()
	if err != nil {
		t.Fatalf("LoadThreats: %v", err)
	}
	if len(loaded) != 1 {
		t.Errorf("on-disk threats = %d, want 1", len(loaded))
	}
}

// 并发报警不丢签名：写盘在 d.mu 之外，flushMu 保证「快照+写盘」串行，
// 否则旧世代快照可能后落盘覆盖掉新签名。
func TestOnSyscallEvent_ConcurrentFlush_NoSignatureLoss(t *testing.T) {
	dir := t.TempDir()
	seed := NewImmuneStore(filepath.Join(dir, "global"))

	const syscallCount = 8
	mean := map[string]float64{}
	stddev := map[string]float64{}
	names := make([]string, 0, syscallCount)
	for i := range syscallCount {
		name := "Syscall" + string(rune('A'+i))
		names = append(names, name)
		mean[name] = 2.0
		stddev[name] = 0.5
	}
	if err := seed.SaveProfile(&NormalProfile{
		AgentTemplate: "concurrent-agent", SampleCount: 10,
		SyscallMean: mean, SyscallStdDev: stddev,
	}); err != nil {
		t.Fatalf("SaveProfile: %v", err)
	}

	daemon := NewImmuneDaemon(NewImmuneStore(dir), DefaultImmuneConfig())
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer daemon.Stop()

	// 每个 syscall 一个 goroutine，各自打到越过三重门（mean+3σ=3.5、地板 5）
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Add(1)
		go func(pid types.PID, syscallName string) {
			defer wg.Done()
			daemon.OnProcessStart(pid, "concurrent-agent", "global")
			for range 8 {
				daemon.OnSyscallEvent(pid, types.SyscallEvent{PID: pid, Syscall: syscallName})
			}
		}(types.PID(500+i), name)
	}
	wg.Wait()

	// 内存与磁盘都应有全部 8 条签名（每个 syscall 一个键）
	if got := len(daemon.GetThreats()); got != syscallCount {
		t.Errorf("in-memory threats = %d, want %d", got, syscallCount)
	}
	loaded, err := seed.LoadThreats()
	if err != nil {
		t.Fatalf("LoadThreats: %v", err)
	}
	if len(loaded) != syscallCount {
		t.Errorf("on-disk threats = %d, want %d (a stale snapshot overwrote newer signatures)", len(loaded), syscallCount)
	}
}

// --- F4: 进程退出清 alert ---

func TestOnProcessExit_ClearsAlert(t *testing.T) {
	dir := t.TempDir()
	seed := NewImmuneStore(filepath.Join(dir, "global"))
	profile := &NormalProfile{
		AgentTemplate: "agent-z", SampleCount: 10,
		SyscallMean:   map[string]float64{"Open": 2.0},
		SyscallStdDev: map[string]float64{"Open": 0.5},
	}
	if err := seed.SaveProfile(profile); err != nil {
		t.Fatalf("SaveProfile: %v", err)
	}

	daemon := NewImmuneDaemon(NewImmuneStore(dir), DefaultImmuneConfig())
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer daemon.Stop()

	pid := types.PID(9)
	daemon.OnProcessStart(pid, "agent-z", "global")
	for range 10 {
		daemon.OnSyscallEvent(pid, types.SyscallEvent{PID: pid, Syscall: "Open"})
	}
	if _, ok := daemon.GetAlerts()[pid]; !ok {
		t.Fatal("precondition: expected alert before exit")
	}

	daemon.OnProcessExit(pid, 100, true)

	if _, ok := daemon.GetAlerts()[pid]; ok {
		t.Error("alert must be cleared when the process exits (IN-3 F4)")
	}
}

// --- F6: 空 template 跳过采集 ---

func TestOnProcessStart_EmptyTemplate_Skipped(t *testing.T) {
	dir := t.TempDir()
	daemon := NewImmuneDaemon(NewImmuneStore(dir), DefaultImmuneConfig())
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer daemon.Stop()

	pid := types.PID(10)
	daemon.OnProcessStart(pid, "", "global")

	if got := daemon.AgentTemplateForPID(pid); got != "" {
		t.Errorf("empty-template process must not be collected, got template %q", got)
	}
	if pids := daemon.ActivePIDs(); len(pids) != 0 {
		t.Errorf("expected 0 active monitors for empty-template spawn, got %d", len(pids))
	}
	// exit 不 panic 且不落样本
	daemon.OnProcessExit(pid, 100, true)
}

// --- F6: 分桶隔离 + legacy 顶层文件忽略 ---

func TestImmuneDaemon_ProjectBucketIsolation(t *testing.T) {
	dir := t.TempDir()
	daemon := NewImmuneDaemon(NewImmuneStore(dir), DefaultImmuneConfig())
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer daemon.Stop()

	// 项目 A 与项目 B 各自跑同名 template 6 次（越过 MinSamples=5）
	pid := types.PID(100)
	for _, project := range []string{"proj-a-11111111", "proj-b-22222222"} {
		for range 6 {
			pid++
			daemon.OnProcessStart(pid, "sa-orchestrator", project)
			daemon.OnSyscallEvent(pid, types.SyscallEvent{PID: pid, Syscall: "Open"})
			daemon.OnProcessExit(pid, 100, true)
		}
	}

	// 样本/​profile 落各自桶
	for _, project := range []string{"proj-a-11111111", "proj-b-22222222"} {
		samplePath := filepath.Join(dir, project, "sa-orchestrator.jsonl")
		if _, err := os.Stat(samplePath); err != nil {
			t.Errorf("expected samples in bucket %s: %v", project, err)
		}
		profilePath := filepath.Join(dir, project, "profiles", "sa-orchestrator-profile.json")
		if _, err := os.Stat(profilePath); err != nil {
			t.Errorf("expected profile in bucket %s: %v", project, err)
		}
	}

	// 内存键 scoped，互不可见
	if daemon.GetProfile("proj-a-11111111", "sa-orchestrator") == nil {
		t.Error("expected profile for project A")
	}
	if daemon.GetProfile("proj-c-33333333", "sa-orchestrator") != nil {
		t.Error("project C must not see other projects' profiles")
	}
}

func TestImmuneDaemon_LegacyTopLevelFilesIgnored(t *testing.T) {
	dir := t.TempDir()

	// 铺 legacy 布局：顶层 threats.jsonl + profiles/（IN-3 前的污染存量）
	legacy := NewImmuneStore(dir)
	if err := legacy.RewriteThreats([]ThreatSignature{{
		ID: "threat-legacy", Type: AnomalySyscallFreq,
		AgentTemplate: "sa-orchestrator", Metric: "Shutdown",
		Threshold: 60.0, CreatedAt: time.Now(),
	}}); err != nil {
		t.Fatalf("RewriteThreats: %v", err)
	}
	if err := legacy.SaveProfile(&NormalProfile{
		AgentTemplate: "sa-orchestrator", SampleCount: 60,
		SyscallMean: map[string]float64{"Shutdown": 1.0 / 60.0},
	}); err != nil {
		t.Fatalf("SaveProfile: %v", err)
	}

	daemon := NewImmuneDaemon(NewImmuneStore(dir), DefaultImmuneConfig())
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start must not fail on legacy layout: %v", err)
	}
	defer daemon.Stop()

	if got := len(daemon.GetThreats()); got != 0 {
		t.Errorf("legacy top-level threats must be ignored: want 0, got %d", got)
	}
	if got := len(daemon.GetAllProfiles()); got != 0 {
		t.Errorf("legacy top-level profiles must be ignored: want 0, got %d", got)
	}
}

// --- F3: Start 清洗（去重 + TTL + 上限） ---

func TestImmuneDaemon_StartHygiene_DedupTTLCap(t *testing.T) {
	dir := t.TempDir()
	seed := NewImmuneStore(filepath.Join(dir, "global"))

	now := time.Now()
	sigs := []ThreatSignature{
		// 同键重复：留 CreatedAt 最新的 threat-dup-new
		{ID: "threat-dup-old", Type: AnomalySyscallFreq, AgentTemplate: "a", Metric: "Open", Threshold: 2.0, CreatedAt: now.Add(-48 * time.Hour)},
		{ID: "threat-dup-new", Type: AnomalySyscallFreq, AgentTemplate: "a", Metric: "Open", Threshold: 4.0, CreatedAt: now.Add(-1 * time.Hour)},
		// 过期（TTL 默认 30 天）
		{ID: "threat-expired", Type: AnomalySyscallFreq, AgentTemplate: "a", Metric: "Write", Threshold: 3.0, CreatedAt: now.AddDate(0, 0, -45)},
		// 存活
		{ID: "threat-live", Type: AnomalySyscallFreq, AgentTemplate: "b", Metric: "Read", Threshold: 3.0, CreatedAt: now.Add(-2 * time.Hour)},
	}
	if err := seed.RewriteThreats(sigs); err != nil {
		t.Fatalf("RewriteThreats: %v", err)
	}

	daemon := NewImmuneDaemon(NewImmuneStore(dir), DefaultImmuneConfig())
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer daemon.Stop()

	threats := daemon.GetThreats()
	ids := make(map[string]bool, len(threats))
	for _, th := range threats {
		ids[th.ID] = true
	}
	if len(threats) != 2 || !ids["threat-dup-new"] || !ids["threat-live"] {
		t.Errorf("hygiene pass: want exactly {threat-dup-new, threat-live}, got %v", ids)
	}

	// 清洗结果已回写磁盘
	loaded, err := seed.LoadThreats()
	if err != nil {
		t.Fatalf("LoadThreats: %v", err)
	}
	if len(loaded) != 2 {
		t.Errorf("hygiene pass must rewrite disk: want 2 signatures, got %d", len(loaded))
	}
}

func TestCleanThreats_CapEvictsOldest(t *testing.T) {
	now := time.Now()
	var sigs []ThreatSignature
	for i := range 10 {
		sigs = append(sigs, ThreatSignature{
			ID: "threat-" + string(rune('a'+i)), Type: AnomalySyscallFreq,
			AgentTemplate: "a", Metric: "M" + string(rune('a'+i)),
			CreatedAt: now.Add(time.Duration(i) * time.Minute),
		})
	}
	out, changed := cleanThreats(sigs, 0, 3, now.Add(time.Hour))
	if !changed || len(out) != 3 {
		t.Fatalf("cap: want 3 newest kept, got %d (changed=%v)", len(out), changed)
	}
	for _, th := range out {
		if th.ID == "threat-a" || th.ID == "threat-b" {
			t.Errorf("oldest signatures must be evicted first, found %s", th.ID)
		}
	}
}

// --- F3: ForgetThreats 纠错口 ---

func TestForgetThreats_ByTemplateMetricAndAll(t *testing.T) {
	dir := t.TempDir()
	seed := NewImmuneStore(filepath.Join(dir, "global"))
	now := time.Now()
	sigs := []ThreatSignature{
		{ID: "t1", Type: AnomalySyscallFreq, AgentTemplate: "a", Metric: "Open", CreatedAt: now},
		{ID: "t2", Type: AnomalySyscallFreq, AgentTemplate: "a", Metric: "Write", CreatedAt: now},
		{ID: "t3", Type: AnomalySyscallFreq, AgentTemplate: "b", Metric: "Open", CreatedAt: now},
	}
	if err := seed.RewriteThreats(sigs); err != nil {
		t.Fatalf("RewriteThreats: %v", err)
	}

	daemon := NewImmuneDaemon(NewImmuneStore(dir), DefaultImmuneConfig())
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer daemon.Stop()

	// 指定 (template, metric)：删 1
	if got := daemon.ForgetThreats("a", "Open", false); got != 1 {
		t.Errorf("forget (a, Open): removed = %d, want 1", got)
	}
	// 指定 template：删 a 剩余 1 条
	if got := daemon.ForgetThreats("a", "", false); got != 1 {
		t.Errorf("forget (a, *): removed = %d, want 1", got)
	}
	// 无匹配：0
	if got := daemon.ForgetThreats("nonexistent", "", false); got != 0 {
		t.Errorf("forget nonexistent: removed = %d, want 0", got)
	}
	// 全量：删剩余 1 条
	if got := daemon.ForgetThreats("", "", true); got != 1 {
		t.Errorf("forget --all: removed = %d, want 1", got)
	}

	// 磁盘同步为空
	loaded, err := seed.LoadThreats()
	if err != nil {
		t.Fatalf("LoadThreats: %v", err)
	}
	if len(loaded) != 0 {
		t.Errorf("disk after forget --all: want 0, got %d", len(loaded))
	}
}

// --- F1+F5 组合回归：卷宗完整复现（签名存在 + 稀有 baseline + 新进程首调） ---

func TestIN3_DossierReproduction_NoFalsePositive(t *testing.T) {
	dir := t.TempDir()
	seed := NewImmuneStore(filepath.Join(dir, "global"))

	// 卷宗形态：60x 签名（数周前铸）+ 已自愈 baseline
	if err := seed.RewriteThreats([]ThreatSignature{{
		ID: "threat-1783474043358057557", Type: AnomalySyscallFreq,
		AgentTemplate: "sa-orchestrator", Metric: "Shutdown",
		Threshold: 60.0, CreatedAt: time.Now().AddDate(0, 0, -20),
	}}); err != nil {
		t.Fatalf("RewriteThreats: %v", err)
	}
	if err := seed.SaveProfile(&NormalProfile{
		AgentTemplate: "sa-orchestrator", SampleCount: 81,
		SyscallMean:   map[string]float64{"Shutdown": 0.148},
		SyscallStdDev: map[string]float64{"Shutdown": 0.50},
		SyscallMax:    map[string]float64{"Shutdown": 2},
	}); err != nil {
		t.Fatalf("SaveProfile: %v", err)
	}

	cfg := DefaultImmuneConfig()
	cfg.WarnOnly = false // enforce：卷宗判定的 P0 场景
	daemon := NewImmuneDaemon(NewImmuneStore(dir), cfg)
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer daemon.Stop()

	suspended := false
	daemon.SetSuspendFunc(func(pid types.PID) error { suspended = true; return nil })

	// 新进程首次（正常）调用 Shutdown
	pid := types.PID(2389)
	daemon.OnProcessStart(pid, "sa-orchestrator", "global")
	daemon.OnSyscallEvent(pid, types.SyscallEvent{PID: pid, Syscall: "Shutdown"})

	if suspended {
		t.Error("P0: first normal Shutdown must NOT suspend in enforce mode")
	}
	if len(daemon.GetAlerts()) != 0 {
		t.Error("first normal Shutdown must not produce an alert")
	}
	if got := len(daemon.GetThreats()); got != 1 {
		t.Errorf("no new signature must be minted, want 1 got %d", got)
	}
}

// Detail 标注含签名 ID 与创建日期（F5 显示诚实化）。
func TestAlertDetail_KnownThreatAnnotationFormat(t *testing.T) {
	dir := t.TempDir()
	seed := NewImmuneStore(filepath.Join(dir, "global"))
	created := time.Date(2026, 7, 8, 10, 0, 0, 0, time.UTC)
	if err := seed.RewriteThreats([]ThreatSignature{{
		ID: "threat-anno", Type: AnomalySyscallFreq,
		AgentTemplate: "agent-w", Metric: "Open",
		Threshold: 60.0, CreatedAt: created,
	}}); err != nil {
		t.Fatalf("RewriteThreats: %v", err)
	}
	if err := seed.SaveProfile(&NormalProfile{
		AgentTemplate: "agent-w", SampleCount: 10,
		SyscallMean:   map[string]float64{"Open": 2.0},
		SyscallStdDev: map[string]float64{"Open": 0.5},
	}); err != nil {
		t.Fatalf("SaveProfile: %v", err)
	}

	cfg := DefaultImmuneConfig()
	cfg.ThreatTTLDays = 0 // 关闭 TTL，签名日期久远仍保留
	daemon := NewImmuneDaemon(NewImmuneStore(dir), cfg)
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer daemon.Stop()

	pid := types.PID(11)
	daemon.OnProcessStart(pid, "agent-w", "global")
	for range 10 {
		daemon.OnSyscallEvent(pid, types.SyscallEvent{PID: pid, Syscall: "Open"})
	}

	alert, ok := daemon.GetAlerts()[pid]
	if !ok {
		t.Fatal("expected alert")
	}
	if !strings.Contains(alert.Detail, "matches known threat threat-anno (created 2026-07-08)") {
		t.Errorf("Detail must carry signature ID + creation date, got %q", alert.Detail)
	}
	if alert.Deviation == 60.0 {
		t.Error("Deviation must be the current measurement, not the signature's frozen 60.0")
	}
}
