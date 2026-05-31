package kernel

import (
	gocontext "context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rnixai/rnix/agents"
	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/skills"
	"github.com/rnixai/rnix/vfs"
)

// ATDD 50.3: 免疫端到端 suspend 闭环 — 结果级断言
//
// 来源 Story：_bmad-output/implementation-artifacts/50-3-immune-suspend-e2e-closure.md
// 审计来源：completion-audit-suspects-2026-05-30.md §A2 TR-3
// Story Key: 50-3-immune-suspend-e2e-closure
//
// 验收标准（本文件覆盖项）：
//   AC1（统计异常→StateSuspended）    : 50.3-INT-001
//   AC2（威胁匹配→StateSuspended）    : 50.3-INT-002
//   AC3（warn-only 对照组→Running）   : 50.3-INT-003
//   AC4（suspend 后 Exit 可观测性）   : 并入 50.3-INT-001
//   AC5（配置门接线仿 main.go）       : 50.3-INT-004
//   AC6（纯 ATDD 不改生产代码）       : 文件级约束
//   AC7（go test -race 通过）         : CI 验证
//   AC8（make all 全绿）              : CI 验证
//
// 核心差异 vs 22.2/51.5：
//   - 22.2/51.5 使用 mock suspendFn（slice append）→ 断言"被调用" = 机制
//   - 50.3 使用真 k.Kill(SIGPAUSE) → 断言 proc.GetState()==StateSuspended = 结果

// immuneParkingLLM is a mock LLM device that blocks on Write until the
// process context is cancelled. This allows the test to hold a process in
// Running state while poking the immune system, then let Suspend → Cancel
// unblock the Write and allow the reasoning goroutine to exit cleanly.
//
// Different from 51-2's parkingLLMFile which blocks on Read.
type immuneParkingLLM struct {
	mu        sync.Mutex
	readData  []byte
	reachedCh chan struct{}
	reached   bool
}

func newImmuneParkingLLM() *immuneParkingLLM {
	return &immuneParkingLLM{
		readData:  makeLLMResponse("parked", 1),
		reachedCh: make(chan struct{}),
	}
}

func (f *immuneParkingLLM) Write(ctx gocontext.Context, _ []byte) error {
	f.mu.Lock()
	if !f.reached {
		f.reached = true
		close(f.reachedCh)
	}
	f.mu.Unlock()
	<-ctx.Done()
	return ctx.Err()
}

func (f *immuneParkingLLM) Read(_ int) ([]byte, error) {
	return f.readData, nil
}

func (f *immuneParkingLLM) Close() error                { return nil }
func (f *immuneParkingLLM) Stat() (vfs.FileStat, error) { return vfs.FileStat{IsDevice: true, Name: "/dev/llm/claude"}, nil }
func (f *immuneParkingLLM) SupportsToolCalling() bool   { return true }

// Reached returns a channel that is closed when the process enters Write
// (guaranteeing it is Running and the LLM request is in flight).
func (f *immuneParkingLLM) Reached() <-chan struct{} { return f.reachedCh }

// newImmuneTestKernel creates a kernel with an immuneParkingLLM for immune E2E tests.
func newImmuneTestKernel(t testing.TB, llmFile *immuneParkingLLM) (*KernelImpl, *vfs.VFS, *rnixctx.Manager) {
	t.Helper()
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return llmFile, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)
	return k, v, ctxMgr
}

// setupImmuneE2E assembles ImmuneDaemon with real k.Kill(SIGPAUSE) suspend,
// faithfully replicating the main.go:1780-1801 assembly sequence (AC5).
func setupImmuneE2E(t *testing.T, k *KernelImpl, warnOnly bool, agentTemplate string) *ImmuneDaemon {
	t.Helper()

	dir := t.TempDir()
	store := NewImmuneStore(dir)

	profile := &NormalProfile{
		AgentTemplate: agentTemplate,
		SampleCount:   10,
		SyscallMean:   map[string]float64{"Open": 2.0},
		SyscallStdDev: map[string]float64{"Open": 0.5},
		TokenRateMean:   2.0,
		TokenRateStdDev: 0.3,
	}
	if err := store.SaveProfile(profile); err != nil {
		t.Fatalf("SaveProfile: %v", err)
	}

	cfg := DefaultImmuneConfig()
	cfg.WarnOnly = warnOnly
	cfg.DeviationThreshold = 3.0
	daemon := NewImmuneDaemon(store, cfg)
	if err := daemon.Start(); err != nil {
		t.Fatalf("daemon.Start: %v", err)
	}
	t.Cleanup(func() { daemon.Stop() })

	// AC5: assembly order matches main.go
	k.SetImmuneDaemon(daemon)
	daemon.SetSuspendFunc(func(pid types.PID) error {
		return k.Kill(pid, types.SIGPAUSE)
	})

	return daemon
}

// immuneTestAgent creates a minimal AgentInfo for immune E2E tests.
func immuneTestAgent(name string) *agents.AgentInfo {
	return &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Name: name,
			Models: agents.AgentModels{
				Provider:  "claude",
				Preferred: "sonnet",
				Fallback:  "haiku",
			},
			ContextBudget: 4096,
			Skills:        []string{"mock-skill"},
		},
		Instructions: "You are a test agent for immune E2E testing.",
		Skills: []*skills.SkillInfo{
			{
				Manifest: skills.SkillManifest{
					Name:            "mock-skill",
					Description:     "A mock skill for testing",
					AllowedToolsRaw: "/dev/fs",
				},
				Body: "# Mock Skill\nTest skill.",
			},
		},
	}
}

// feedSyscalls sends N Open syscall events to the daemon for the given PID.
func feedSyscalls(daemon *ImmuneDaemon, pid types.PID, syscall string, count int) {
	for i := range count {
		daemon.OnSyscallEvent(pid, types.SyscallEvent{
			PID:     pid,
			Syscall: syscall,
			Args:    map[string]any{"iter": i},
		})
	}
}

// --- 50.3-INT-001: [P0] 统计异常端到端 suspend 闭环 (AC1 + AC4) ---

func TestATDD_50_3_INT_001_E2E_StatisticalAnomaly_SuspendsClosure(t *testing.T) {
	const agentTpl = "e2e-stat-agent"
	llmFile := newImmuneParkingLLM()
	k, _, _ := newImmuneTestKernel(t, llmFile)
	daemon := setupImmuneE2E(t, k, false, agentTpl)

	pid, err := k.Spawn("immune e2e test", immuneTestAgent(agentTpl), SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d not found", pid)
	}

	// Wait until process reaches Write (Running state guaranteed)
	select {
	case <-llmFile.Reached():
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for process to reach LLM Write")
	}
	if proc.GetState() != types.StateRunning {
		t.Fatalf("expected Running before suspend, got %v", proc.GetState())
	}

	// Register collector for this PID (simulates kernel's OnProcessStart callback)
	daemon.OnProcessStart(pid, agentTpl)

	// Feed 10 Open events — threshold = mean+3*stddev = 2.0+1.5 = 3.5, so 10 >> 3.5
	// OnSyscallEvent → anomaly detected → suspendFn → k.Kill(SIGPAUSE) →
	// suspendSubtree → Cancel + wg.Wait → suspendProcess → StateSuspended
	// This is synchronous: when OnSyscallEvent returns, the process is already Suspended.
	feedSyscalls(daemon, pid, "Open", 10)

	// AC1: result-level assertion — process state is Suspended (not just "suspendFn called")
	if got := proc.GetState(); got != types.StateSuspended {
		t.Errorf("AC1: proc.GetState() = %v, want StateSuspended", got)
	}

	// AC4: suspend observability — Exit recorded correctly
	proc.mu.Lock()
	exit := proc.Exit
	proc.mu.Unlock()
	if exit == nil {
		t.Fatal("AC4: proc.Exit is nil after suspend")
	}
	if exit.Code != ExitSuspended {
		t.Errorf("AC4: Exit.Code = %d, want ExitSuspended (%d)", exit.Code, ExitSuspended)
	}
	if !strings.Contains(exit.Reason, "suspended") {
		t.Errorf("AC4: Exit.Reason = %q, want substring \"suspended\"", exit.Reason)
	}

	// AC1: alert exists with correct type
	alerts := daemon.GetAlerts()
	alert, ok := alerts[pid]
	if !ok {
		t.Fatal("AC1: expected alert for PID after statistical anomaly")
	}
	if alert.Type != AnomalySyscallFreq {
		t.Errorf("AC1: alert.Type = %q, want %q", alert.Type, AnomalySyscallFreq)
	}
}

// --- 50.3-INT-002: [P0] 威胁匹配端到端 suspend 闭环 (AC2) ---

func TestATDD_50_3_INT_002_E2E_ThreatMatch_SuspendsClosure(t *testing.T) {
	const agentTpl = "e2e-threat-agent"
	llmFile := newImmuneParkingLLM()
	k, _, _ := newImmuneTestKernel(t, llmFile)

	// Setup immune with a pre-stored threat signature
	dir := t.TempDir()
	store := NewImmuneStore(dir)

	profile := &NormalProfile{
		AgentTemplate: agentTpl,
		SampleCount:   10,
		SyscallMean:   map[string]float64{"Open": 2.0},
		SyscallStdDev: map[string]float64{"Open": 0.5},
		TokenRateMean:   2.0,
		TokenRateStdDev: 0.3,
	}
	if err := store.SaveProfile(profile); err != nil {
		t.Fatalf("SaveProfile: %v", err)
	}

	sig := ThreatSignature{
		ID:            "threat-e2e-001",
		Type:          AnomalySyscallFreq,
		AgentTemplate: agentTpl,
		Metric:        "Open",
		Threshold:     3.0,
		CreatedAt:     time.Now(),
	}
	if err := store.SaveThreat(sig); err != nil {
		t.Fatalf("SaveThreat: %v", err)
	}

	cfg := DefaultImmuneConfig()
	cfg.WarnOnly = false
	daemon := NewImmuneDaemon(store, cfg)
	if err := daemon.Start(); err != nil {
		t.Fatalf("daemon.Start: %v", err)
	}
	t.Cleanup(func() { daemon.Stop() })
	k.SetImmuneDaemon(daemon)
	daemon.SetSuspendFunc(func(pid types.PID) error {
		return k.Kill(pid, types.SIGPAUSE)
	})

	pid, err := k.Spawn("immune threat e2e test", immuneTestAgent(agentTpl), SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d not found", pid)
	}

	select {
	case <-llmFile.Reached():
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for process to reach LLM Write")
	}

	daemon.OnProcessStart(pid, agentTpl)

	// Single matching syscall should trigger threat fast-path
	daemon.OnSyscallEvent(pid, types.SyscallEvent{
		PID:     pid,
		Syscall: "Open",
		Args:    map[string]any{"threat": true},
	})

	// AC2: process suspended via threat match
	if got := proc.GetState(); got != types.StateSuspended {
		t.Errorf("AC2: proc.GetState() = %v, want StateSuspended", got)
	}

	// AC2: alert detail contains "known threat"
	alerts := daemon.GetAlerts()
	alert, ok := alerts[pid]
	if !ok {
		t.Fatal("AC2: expected alert for PID after threat match")
	}
	if !strings.Contains(alert.Detail, "known threat") {
		t.Errorf("AC2: alert.Detail = %q, want substring 'known threat'", alert.Detail)
	}
}

// --- 50.3-INT-003: [P0] warn-only 对照组 — 不 suspend (AC3) ---

func TestATDD_50_3_INT_003_E2E_WarnOnly_NoSuspend(t *testing.T) {
	const agentTpl = "e2e-warnonly-agent"
	llmFile := newImmuneParkingLLM()
	k, _, _ := newImmuneTestKernel(t, llmFile)
	daemon := setupImmuneE2E(t, k, true, agentTpl)

	pid, err := k.Spawn("immune warnonly e2e test", immuneTestAgent(agentTpl), SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d not found", pid)
	}

	select {
	case <-llmFile.Reached():
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for process to reach LLM Write")
	}

	daemon.OnProcessStart(pid, agentTpl)

	// Feed the same 10 Open events that would trigger suspend in enforce mode
	feedSyscalls(daemon, pid, "Open", 10)

	// Short grace period to confirm no async suspend
	time.Sleep(100 * time.Millisecond)

	// AC3: process STILL Running (not suspended)
	if got := proc.GetState(); got != types.StateRunning {
		t.Errorf("AC3: proc.GetState() = %v, want StateRunning (warn-only should not suspend)", got)
	}

	// AC3: alert recorded (anomaly detected but not acted upon)
	alerts := daemon.GetAlerts()
	if _, ok := alerts[pid]; !ok {
		t.Error("AC3: expected alert for PID even in warn-only mode")
	}
}

// --- 50.3-INT-004: [P1] 配置门接线验证 — 仿 main.go 装配 (AC5) ---

func TestATDD_50_3_INT_004_ConfigGateWiring(t *testing.T) {
	const agentTpl = "e2e-wiring-agent"
	llmFile := newImmuneParkingLLM()
	k, _, _ := newImmuneTestKernel(t, llmFile)

	// Verify kernel starts with nil immuneDaemon
	if k.immuneDaemon != nil {
		t.Error("AC5: kernel should start with nil immuneDaemon")
	}

	// Assembly sequence replicating main.go:1780-1801
	daemon := setupImmuneE2E(t, k, false, agentTpl)

	// AC5: after setup, immuneDaemon is injected
	if k.immuneDaemon == nil {
		t.Fatal("AC5: k.immuneDaemon is nil after SetImmuneDaemon")
	}

	// AC5: daemon is the exact instance we injected
	if k.immuneDaemon != daemon {
		t.Error("AC5: k.immuneDaemon is not the daemon we set")
	}

	// AC5: suspendFn is wired (not nil)
	daemon.mu.RLock()
	hasSuspendFn := daemon.suspendFn != nil
	daemon.mu.RUnlock()
	if !hasSuspendFn {
		t.Error("AC5: daemon.suspendFn is nil after SetSuspendFunc")
	}
}
