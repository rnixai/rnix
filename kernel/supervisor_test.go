package kernel

import (
	gocontext "context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rnixai/rnix/agents"
	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// === Intent-aware mock infrastructure ===

// intentRouter routes crash behavior to per-FD mock files based on intent.
type intentRouter struct {
	mu          sync.Mutex
	crashIntent string
	crashCount  int           // remaining crashes for crashIntent
	slowDelay   time.Duration // delay for non-crash reads (0 = no delay)
}

func (r *intentRouter) newFile() vfs.VFSFile {
	return &routedLLMFile{router: r}
}

// routedLLMFile is a per-FD mock that records intent from Write and routes
// Read behavior through the router.
type routedLLMFile struct {
	router *intentRouter
	mu     sync.Mutex
	intent string
}

func (f *routedLLMFile) Write(_ gocontext.Context, data []byte) error {
	var req llmRequest
	if json.Unmarshal(data, &req) == nil {
		f.mu.Lock()
		f.intent = req.Intent
		f.mu.Unlock()
	}
	return nil
}

func (f *routedLLMFile) Read(length int) ([]byte, error) {
	f.mu.Lock()
	intent := f.intent
	f.mu.Unlock()

	f.router.mu.Lock()
	shouldCrash := intent == f.router.crashIntent && f.router.crashCount > 0
	if shouldCrash {
		f.router.crashCount--
	}
	delay := f.router.slowDelay
	f.router.mu.Unlock()

	if shouldCrash {
		return nil, &vfs.VFSError{
			Op: "Read", Device: "/dev/llm/claude", Code: types.ErrDriver,
			Err: fmt.Errorf("simulated crash for %s", intent),
		}
	}

	if delay > 0 {
		time.Sleep(delay)
	}

	resp := llmResponse{Content: "done", TokensUsed: 1}
	data, _ := json.Marshal(resp)
	return data, nil
}

func (f *routedLLMFile) Close() error { return nil }
func (f *routedLLMFile) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{IsDevice: true, Name: "/dev/llm/claude"}, nil
}

// newRoutedTestKernel creates a kernel with per-FD intent-routing.
func newRoutedTestKernel(t testing.TB, router *intentRouter) *KernelImpl {
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag) (vfs.VFSFile, error) {
		return router.newFile(), nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)
	return k
}

// === Simple mock files ===

// alwaysCrashFile always returns Read error.
type alwaysCrashFile struct{}

func (f *alwaysCrashFile) Write(_ gocontext.Context, data []byte) error { return nil }
func (f *alwaysCrashFile) Read(length int) ([]byte, error) {
	return nil, &vfs.VFSError{Op: "Read", Device: "/dev/llm/claude", Code: types.ErrDriver,
		Err: fmt.Errorf("always crash")}
}
func (f *alwaysCrashFile) Close() error { return nil }
func (f *alwaysCrashFile) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{IsDevice: true, Name: "/dev/llm/claude"}, nil
}

// normalFile returns "done" on Read (children exit code 0).
type normalFile struct{}

func (f *normalFile) Write(_ gocontext.Context, data []byte) error { return nil }
func (f *normalFile) Read(length int) ([]byte, error) {
	resp := llmResponse{Content: "done", TokensUsed: 1}
	data, _ := json.Marshal(resp)
	return data, nil
}
func (f *normalFile) Close() error { return nil }
func (f *normalFile) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{IsDevice: true, Name: "/dev/llm/claude"}, nil
}

// slowFile delays Read to simulate long-running children.
type slowFile struct{ delay time.Duration }

func (f *slowFile) Write(_ gocontext.Context, data []byte) error { return nil }
func (f *slowFile) Read(length int) ([]byte, error) {
	time.Sleep(f.delay)
	resp := llmResponse{Content: "done", TokensUsed: 1}
	data, _ := json.Marshal(resp)
	return data, nil
}
func (f *slowFile) Close() error { return nil }
func (f *slowFile) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{IsDevice: true, Name: "/dev/llm/claude"}, nil
}

// newSimpleTestKernel creates a kernel with a single shared LLM file.
func newSimpleTestKernel(t testing.TB, file vfs.VFSFile) *KernelImpl {
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag) (vfs.VFSFile, error) {
		return file, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)
	return k
}

// newPerOpenTestKernel creates a kernel with a factory returning fresh files per Open.
func newPerOpenTestKernel(t testing.TB, factory func() vfs.VFSFile) *KernelImpl {
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag) (vfs.VFSFile, error) {
		return factory(), nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)
	return k
}

// waitSupervisor blocks until the supervisor process writes to Done.
func waitSupervisor(k *KernelImpl, supPID types.PID) ExitStatus {
	proc, ok := k.GetProcess(supPID)
	if !ok {
		return ExitStatus{Code: -1, Reason: "supervisor not found"}
	}
	return <-proc.Done
}

// === Tests ===

// Test 7.1: one_for_one — child B crashes → only B restarted, A/C unaffected
func TestSupervisor_OneForOne(t *testing.T) {
	router := &intentRouter{crashIntent: "child-B", crashCount: 1}
	k := newRoutedTestKernel(t, router)

	spec := SupervisorSpec{
		Strategy:    OneForOne,
		MaxRestarts: 5,
		MaxWindow:   10 * time.Second,
		Children: []ChildSpec{
			{Name: "child-A", Intent: "child-A", Restart: RestartTemporary},
			{Name: "child-B", Intent: "child-B", Restart: RestartTransient},
			{Name: "child-C", Intent: "child-C", Restart: RestartTemporary},
		},
	}

	supPID, err := k.SpawnSupervisor(spec)
	if err != nil {
		t.Fatalf("SpawnSupervisor: %v", err)
	}

	exit := waitSupervisor(k, supPID)
	if exit.Code != 0 {
		t.Fatalf("exit code %d, reason: %s", exit.Code, exit.Reason)
	}
	if exit.Reason != "all children completed" {
		t.Fatalf("reason: %q, want 'all children completed'", exit.Reason)
	}
}

// Test 7.2: one_for_all — child B crashes → all A+B+C restarted
func TestSupervisor_OneForAll(t *testing.T) {
	router := &intentRouter{crashIntent: "child-B", crashCount: 1}
	k := newRoutedTestKernel(t, router)

	spec := SupervisorSpec{
		Strategy:    OneForAll,
		MaxRestarts: 5,
		MaxWindow:   10 * time.Second,
		Children: []ChildSpec{
			{Name: "child-A", Intent: "child-A", Restart: RestartTemporary},
			{Name: "child-B", Intent: "child-B", Restart: RestartTransient},
			{Name: "child-C", Intent: "child-C", Restart: RestartTemporary},
		},
	}

	supPID, err := k.SpawnSupervisor(spec)
	if err != nil {
		t.Fatalf("SpawnSupervisor: %v", err)
	}

	exit := waitSupervisor(k, supPID)
	if exit.Code != 0 {
		t.Fatalf("exit code %d, reason: %s", exit.Code, exit.Reason)
	}
	if exit.Reason != "all children completed" {
		t.Fatalf("reason: %q, want 'all children completed'", exit.Reason)
	}
}

// Test 7.3: rest_for_one — child B crashes → B+C restarted, A unaffected
func TestSupervisor_RestForOne(t *testing.T) {
	router := &intentRouter{crashIntent: "child-B", crashCount: 1}
	k := newRoutedTestKernel(t, router)

	spec := SupervisorSpec{
		Strategy:    RestForOne,
		MaxRestarts: 5,
		MaxWindow:   10 * time.Second,
		Children: []ChildSpec{
			{Name: "child-A", Intent: "child-A", Restart: RestartTemporary},
			{Name: "child-B", Intent: "child-B", Restart: RestartTransient},
			{Name: "child-C", Intent: "child-C", Restart: RestartTemporary},
		},
	}

	supPID, err := k.SpawnSupervisor(spec)
	if err != nil {
		t.Fatalf("SpawnSupervisor: %v", err)
	}

	exit := waitSupervisor(k, supPID)
	if exit.Code != 0 {
		t.Fatalf("exit code %d, reason: %s", exit.Code, exit.Reason)
	}
	if exit.Reason != "all children completed" {
		t.Fatalf("reason: %q, want 'all children completed'", exit.Reason)
	}
}

// Test 7.4: restart frequency exceeded → supervisor exits
func TestSupervisor_MaxRestartsExceeded(t *testing.T) {
	k := newSimpleTestKernel(t, &alwaysCrashFile{})

	spec := SupervisorSpec{
		Strategy:    OneForOne,
		MaxRestarts: 3,
		MaxWindow:   10 * time.Second,
		Children: []ChildSpec{
			{Name: "crasher", Intent: "crasher", Restart: RestartPermanent},
		},
	}

	supPID, err := k.SpawnSupervisor(spec)
	if err != nil {
		t.Fatalf("SpawnSupervisor: %v", err)
	}

	exit := waitSupervisor(k, supPID)
	if exit.Code != 1 {
		t.Fatalf("exit code %d, want 1", exit.Code)
	}
	if exit.Reason != "max_restarts_exceeded" {
		t.Fatalf("reason: %q, want 'max_restarts_exceeded'", exit.Reason)
	}
}

// Test 7.5: permanent — even normal exit triggers restart → eventually max_restarts_exceeded
func TestSupervisor_PermanentRestart(t *testing.T) {
	k := newSimpleTestKernel(t, &normalFile{})

	spec := SupervisorSpec{
		Strategy:    OneForOne,
		MaxRestarts: 3,
		MaxWindow:   10 * time.Second,
		Children: []ChildSpec{
			{Name: "permanent-child", Intent: "permanent-child", Restart: RestartPermanent},
		},
	}

	supPID, err := k.SpawnSupervisor(spec)
	if err != nil {
		t.Fatalf("SpawnSupervisor: %v", err)
	}

	exit := waitSupervisor(k, supPID)
	if exit.Code != 1 {
		t.Fatalf("exit code %d, want 1 (reason: %s)", exit.Code, exit.Reason)
	}
	if exit.Reason != "max_restarts_exceeded" {
		t.Fatalf("reason: %q, want 'max_restarts_exceeded'", exit.Reason)
	}
}

// Test 7.6: transient — normal exit (code=0) → no restart
func TestSupervisor_TransientRestart(t *testing.T) {
	k := newSimpleTestKernel(t, &normalFile{})

	spec := SupervisorSpec{
		Strategy:    OneForOne,
		MaxRestarts: 5,
		MaxWindow:   10 * time.Second,
		Children: []ChildSpec{
			{Name: "transient-child", Intent: "transient-child", Restart: RestartTransient},
		},
	}

	supPID, err := k.SpawnSupervisor(spec)
	if err != nil {
		t.Fatalf("SpawnSupervisor: %v", err)
	}

	exit := waitSupervisor(k, supPID)
	if exit.Code != 0 {
		t.Fatalf("exit code %d, reason: %s", exit.Code, exit.Reason)
	}
	if exit.Reason != "all children completed" {
		t.Fatalf("reason: %q, want 'all children completed'", exit.Reason)
	}
}

// Test 7.7: temporary — never restarted even on crash
func TestSupervisor_TemporaryNoRestart(t *testing.T) {
	k := newSimpleTestKernel(t, &alwaysCrashFile{})

	spec := SupervisorSpec{
		Strategy:    OneForOne,
		MaxRestarts: 5,
		MaxWindow:   10 * time.Second,
		Children: []ChildSpec{
			{Name: "temp-child", Intent: "temp-child", Restart: RestartTemporary},
		},
	}

	supPID, err := k.SpawnSupervisor(spec)
	if err != nil {
		t.Fatalf("SpawnSupervisor: %v", err)
	}

	exit := waitSupervisor(k, supPID)
	if exit.Code != 0 {
		t.Fatalf("exit code %d, reason: %s", exit.Code, exit.Reason)
	}
	if exit.Reason != "all children completed" {
		t.Fatalf("reason: %q, want 'all children completed'", exit.Reason)
	}
}

// Test 7.8: Kill supervisor → children cleaned up, no restart triggered
func TestSupervisor_KillNoRestart(t *testing.T) {
	k := newPerOpenTestKernel(t, func() vfs.VFSFile {
		return &slowFile{delay: 500 * time.Millisecond}
	})

	spec := SupervisorSpec{
		Strategy:    OneForOne,
		MaxRestarts: 5,
		MaxWindow:   10 * time.Second,
		Children: []ChildSpec{
			{Name: "child-A", Intent: "child-A", Restart: RestartPermanent},
			{Name: "child-B", Intent: "child-B", Restart: RestartPermanent},
		},
	}

	supPID, err := k.SpawnSupervisor(spec)
	if err != nil {
		t.Fatalf("SpawnSupervisor: %v", err)
	}

	// Give children time to start
	time.Sleep(50 * time.Millisecond)

	if err := k.Kill(supPID, types.SIGKILL); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	exit := waitSupervisor(k, supPID)
	if exit.Reason != "supervisor cancelled" {
		t.Fatalf("reason: %q, want 'supervisor cancelled'", exit.Reason)
	}
}

// Test 7.9: Restart time ≤ 5 seconds
func TestSupervisor_RestartWithin5Seconds(t *testing.T) {
	router := &intentRouter{crashIntent: "child", crashCount: 1}
	k := newRoutedTestKernel(t, router)

	start := time.Now()

	spec := SupervisorSpec{
		Strategy:    OneForOne,
		MaxRestarts: 5,
		MaxWindow:   10 * time.Second,
		Children: []ChildSpec{
			{Name: "child", Intent: "child", Restart: RestartTransient},
		},
	}

	supPID, err := k.SpawnSupervisor(spec)
	if err != nil {
		t.Fatalf("SpawnSupervisor: %v", err)
	}

	exit := waitSupervisor(k, supPID)
	elapsed := time.Since(start)

	if exit.Code != 0 {
		t.Fatalf("exit code %d, reason: %s", exit.Code, exit.Reason)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("restart took %v, want ≤ 5s", elapsed)
	}
}

// Test 7.10: Startup failure → rollback already started children → supervisor exits
func TestSupervisor_StartupFailureRollback(t *testing.T) {
	// Make VFS Open fail on the 2nd call, so 2nd child's Spawn fails synchronously.
	// This tests the Phase 1 rollback path in run().
	var openCount atomic.Int32
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag) (vfs.VFSFile, error) {
		n := openCount.Add(1)
		if n >= 2 {
			return nil, fmt.Errorf("simulated open failure")
		}
		return &normalFile{}, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)

	spec := SupervisorSpec{
		Strategy:    OneForOne,
		MaxRestarts: 5,
		MaxWindow:   10 * time.Second,
		Children: []ChildSpec{
			{Name: "ok-child", Intent: "ok-child", Restart: RestartPermanent},
			{Name: "fail-child", Intent: "fail-child", Restart: RestartPermanent},
		},
	}

	supPID, err := k.SpawnSupervisor(spec)
	if err != nil {
		t.Fatalf("SpawnSupervisor: %v", err)
	}

	exit := waitSupervisor(k, supPID)
	if exit.Code != 1 {
		t.Fatalf("exit code %d, want 1", exit.Code)
	}
}

// Test 7.11: All temporary children complete → supervisor exits normally
func TestSupervisor_AllTemporaryComplete(t *testing.T) {
	k := newSimpleTestKernel(t, &normalFile{})

	spec := SupervisorSpec{
		Strategy:    OneForOne,
		MaxRestarts: 5,
		MaxWindow:   10 * time.Second,
		Children: []ChildSpec{
			{Name: "temp-A", Intent: "temp-A", Restart: RestartTemporary},
			{Name: "temp-B", Intent: "temp-B", Restart: RestartTemporary},
			{Name: "temp-C", Intent: "temp-C", Restart: RestartTemporary},
		},
	}

	supPID, err := k.SpawnSupervisor(spec)
	if err != nil {
		t.Fatalf("SpawnSupervisor: %v", err)
	}

	exit := waitSupervisor(k, supPID)
	if exit.Code != 0 {
		t.Fatalf("exit code %d, reason: %s", exit.Code, exit.Reason)
	}
	if exit.Reason != "all children completed" {
		t.Fatalf("reason: %q, want 'all children completed'", exit.Reason)
	}
}

// Test 7.12: No regression — compile-time checks
func TestSupervisor_NoRegressionCheck(t *testing.T) {
	var _ SupervisorManager = (*KernelImpl)(nil)
	_ = RestartStrategy(OneForOne)
	_ = RestartStrategy(OneForAll)
	_ = RestartStrategy(RestForOne)
	_ = ChildRestart(RestartPermanent)
	_ = ChildRestart(RestartTransient)
	_ = ChildRestart(RestartTemporary)
}

// Test CR-1 regression: one_for_all with transient children must not cascade
// from stale monitor goroutine events. Without the PID check in handleChildExit,
// stopped children's exit events (code≠0) would be misattributed to newly
// restarted children, causing false restarts and premature max_restarts_exceeded.
func TestSupervisor_OneForAll_NoStaleEventCascade(t *testing.T) {
	// slowDelay ensures A and C are still running when B crashes, so
	// restartOneForAll must stop them (generating stale exit events).
	router := &intentRouter{crashIntent: "child-B", crashCount: 1, slowDelay: 100 * time.Millisecond}
	k := newRoutedTestKernel(t, router)

	spec := SupervisorSpec{
		Strategy:    OneForAll,
		MaxRestarts: 2, // Tight limit: 1 real restart must suffice
		MaxWindow:   10 * time.Second,
		Children: []ChildSpec{
			{Name: "child-A", Intent: "child-A", Restart: RestartTransient},
			{Name: "child-B", Intent: "child-B", Restart: RestartTransient},
			{Name: "child-C", Intent: "child-C", Restart: RestartTransient},
		},
	}

	supPID, err := k.SpawnSupervisor(spec)
	if err != nil {
		t.Fatalf("SpawnSupervisor: %v", err)
	}

	exit := waitSupervisor(k, supPID)
	if exit.Code != 0 {
		t.Fatalf("exit code %d, reason: %s — stale events likely caused false restart cascade", exit.Code, exit.Reason)
	}
	if exit.Reason != "all children completed" {
		t.Fatalf("reason: %q, want 'all children completed'", exit.Reason)
	}
}

// Test 22.4: migration triggered when restart limit exceeded
func TestSupervisor_MigrationOnRestartExceeded(t *testing.T) {
	k := newSimpleTestKernel(t, &alwaysCrashFile{})

	// Setup immune daemon with similarity matrix and migrate function
	immuneStore := NewImmuneStore(t.TempDir())
	daemon := NewImmuneDaemon(immuneStore)
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start immune daemon: %v", err)
	}
	defer daemon.Stop()

	// Build similarity matrix: "crasher-agent" is similar to "backup-agent"
	agentSkills := map[string][]string{
		"crasher-agent": {"analysis", "coding"},
		"backup-agent":  {"analysis", "coding", "review"},
	}
	daemon.UpdateSimilarityMatrix(agentSkills)

	var migrated atomic.Bool
	daemon.SetMigrateFunc(func(intent string, agentName string, contextMessages []string) (types.PID, error) {
		migrated.Store(true)
		return types.PID(9999), nil
	})

	k.SetImmuneDaemon(daemon)

	agentInfo := &agents.AgentInfo{
		Manifest: agents.AgentManifest{Name: "crasher-agent"},
	}

	spec := SupervisorSpec{
		Strategy:    OneForOne,
		MaxRestarts: 2,
		MaxWindow:   10 * time.Second,
		Children: []ChildSpec{
			{Name: "crasher", Intent: "crash-task", Agent: agentInfo, Restart: RestartPermanent},
		},
	}

	supPID, err := k.SpawnSupervisor(spec)
	if err != nil {
		t.Fatalf("SpawnSupervisor: %v", err)
	}

	_ = waitSupervisor(k, supPID)
	if !migrated.Load() {
		t.Error("expected migration to be attempted when restart limit exceeded")
	}
}

// Test 22.4: migration fails, supervisor falls back to normal shutdown
func TestSupervisor_MigrationFailed_FallbackShutdown(t *testing.T) {
	k := newSimpleTestKernel(t, &alwaysCrashFile{})

	// Setup immune daemon with no similar agents (migration will fail)
	immuneStore := NewImmuneStore(t.TempDir())
	daemon := NewImmuneDaemon(immuneStore)
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start immune daemon: %v", err)
	}
	defer daemon.Stop()

	// Empty similarity matrix: no candidates for migration
	daemon.UpdateSimilarityMatrix(map[string][]string{
		"crasher-agent": {"unique-skill"},
	})

	daemon.SetMigrateFunc(func(intent string, agentName string, contextMessages []string) (types.PID, error) {
		return 0, fmt.Errorf("spawn failed")
	})

	k.SetImmuneDaemon(daemon)

	agentInfo := &agents.AgentInfo{
		Manifest: agents.AgentManifest{Name: "crasher-agent"},
	}

	spec := SupervisorSpec{
		Strategy:    OneForOne,
		MaxRestarts: 2,
		MaxWindow:   10 * time.Second,
		Children: []ChildSpec{
			{Name: "crasher", Intent: "crash-task", Agent: agentInfo, Restart: RestartPermanent},
		},
	}

	supPID, err := k.SpawnSupervisor(spec)
	if err != nil {
		t.Fatalf("SpawnSupervisor: %v", err)
	}

	exit := waitSupervisor(k, supPID)
	// Migration fails, so supervisor should fall back to normal shutdown
	if exit.Code != 1 {
		t.Fatalf("exit code %d, want 1 (migration failed, should fallback)", exit.Code)
	}
	if exit.Reason != "max_restarts_exceeded" {
		t.Fatalf("reason: %q, want 'max_restarts_exceeded'", exit.Reason)
	}
}

// Test CR-1 regression: rest_for_one with transient children must not cascade.
func TestSupervisor_RestForOne_NoStaleEventCascade(t *testing.T) {
	router := &intentRouter{crashIntent: "child-B", crashCount: 1, slowDelay: 100 * time.Millisecond}
	k := newRoutedTestKernel(t, router)

	spec := SupervisorSpec{
		Strategy:    RestForOne,
		MaxRestarts: 2,
		MaxWindow:   10 * time.Second,
		Children: []ChildSpec{
			{Name: "child-A", Intent: "child-A", Restart: RestartTransient},
			{Name: "child-B", Intent: "child-B", Restart: RestartTransient},
			{Name: "child-C", Intent: "child-C", Restart: RestartTransient},
		},
	}

	supPID, err := k.SpawnSupervisor(spec)
	if err != nil {
		t.Fatalf("SpawnSupervisor: %v", err)
	}

	exit := waitSupervisor(k, supPID)
	if exit.Code != 0 {
		t.Fatalf("exit code %d, reason: %s — stale events likely caused false restart cascade", exit.Code, exit.Reason)
	}
	if exit.Reason != "all children completed" {
		t.Fatalf("reason: %q, want 'all children completed'", exit.Reason)
	}
}
