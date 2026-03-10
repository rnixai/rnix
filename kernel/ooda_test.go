package kernel

import (
	gocontext "context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// ATDD Story 20.1: OODA Loop Core Implementation
// TDD RED PHASE - All tests designed to FAIL until implementation exists
// =============================================================================

// --- AC #1-#4: OODA Type & Phase Constants ---

func TestOODAPhase_Constants(t *testing.T) {
	// Verify OODA phase constants are defined with correct values
	if PhaseObserve != OODAPhase("observe") {
		t.Fatalf("expected PhaseObserve = \"observe\", got %q", PhaseObserve)
	}
	if PhaseOrient != OODAPhase("orient") {
		t.Fatalf("expected PhaseOrient = \"orient\", got %q", PhaseOrient)
	}
	if PhaseDecide != OODAPhase("decide") {
		t.Fatalf("expected PhaseDecide = \"decide\", got %q", PhaseDecide)
	}
	if PhaseAct != OODAPhase("act") {
		t.Fatalf("expected PhaseAct = \"act\", got %q", PhaseAct)
	}
}

func TestOODADecision_Types(t *testing.T) {
	// Verify all OODAActionType constants are defined
	if OODAToolCall != OODAActionType("tool_call") {
		t.Fatalf("expected OODAToolCall = \"tool_call\", got %q", OODAToolCall)
	}
	if OODASpawn != OODAActionType("spawn") {
		t.Fatalf("expected OODASpawn = \"spawn\", got %q", OODASpawn)
	}
	if OODAComplete != OODAActionType("complete") {
		t.Fatalf("expected OODAComplete = \"complete\", got %q", OODAComplete)
	}
	if OODAReplan != OODAActionType("replan") {
		t.Fatalf("expected OODAReplan = \"replan\", got %q", OODAReplan)
	}
}

func TestOODAState_Struct(t *testing.T) {
	// Verify OODAState struct can be created with all required fields
	state := &OODAState{
		Phase:       PhaseObserve,
		Cycle:       1,
		Observations: "test observations",
		Orientation:  "test orientation",
		Decision: &OODADecision{
			Action: OODAToolCall,
			Target: "/dev/fs",
			Data:   []byte("{}"),
			Reason: "test reason",
		},
	}
	if state.Phase != PhaseObserve {
		t.Fatalf("expected Phase = PhaseObserve, got %q", state.Phase)
	}
	if state.Cycle != 1 {
		t.Fatalf("expected Cycle = 1, got %d", state.Cycle)
	}
	if state.Decision == nil {
		t.Fatal("expected Decision to be non-nil")
	}
	if state.Decision.Action != OODAToolCall {
		t.Fatalf("expected Decision.Action = OODAToolCall, got %q", state.Decision.Action)
	}
}

// --- AC #1-#4: Process OODA State (Thread Safety) ---

func TestProcess_OODAState(t *testing.T) {
	proc := NewProcess(0, "ooda test", nil)

	// Initially not OODA-enabled
	if proc.IsOODA() {
		t.Fatal("new process should not be OODA-enabled by default")
	}

	state := proc.GetOODAState()
	if state != nil {
		t.Fatal("expected nil OODAState for non-OODA process")
	}
}

func TestProcess_OODAState_SetPhase(t *testing.T) {
	proc := NewProcess(0, "ooda test", nil)

	// Enable OODA - this should be done via SetOODAPhase or initialization
	proc.SetOODAPhase(PhaseObserve)

	if !proc.IsOODA() {
		t.Fatal("process should be OODA-enabled after SetOODAPhase")
	}

	state := proc.GetOODAState()
	if state == nil {
		t.Fatal("expected non-nil OODAState after SetOODAPhase")
	}
	if state.Phase != PhaseObserve {
		t.Fatalf("expected Phase = PhaseObserve, got %q", state.Phase)
	}
}

func TestProcess_OODAState_ConcurrentAccess(t *testing.T) {
	proc := NewProcess(0, "ooda concurrent", nil)

	var wg sync.WaitGroup
	phases := []OODAPhase{PhaseObserve, PhaseOrient, PhaseDecide, PhaseAct}

	// Concurrent writes
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			proc.SetOODAPhase(phases[idx%len(phases)])
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = proc.IsOODA()
			_ = proc.GetOODAState()
		}()
	}

	wg.Wait()

	// After all goroutines, the process should be OODA-enabled
	if !proc.IsOODA() {
		t.Fatal("process should be OODA-enabled after concurrent SetOODAPhase calls")
	}
}

// --- AC #1-#4: OODA Reasoning Loop Integration ---

// newOODATestKernel creates a test kernel with a dynamically-responding mock LLM.
// The responseFunc is called for each LLM Write to determine the Read response.
func newOODATestKernel(t testing.TB, responseFunc func(writeData []byte) []byte) (*KernelImpl, *vfs.VFS, *rnixctx.Manager) {
	llmFile := &mockDynamicLLMFile{responseFunc: responseFunc}
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag) (vfs.VFSFile, error) {
		return llmFile, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)
	return k, v, ctxMgr
}

// mockDynamicLLMFile responds dynamically based on write data.
type mockDynamicLLMFile struct {
	mu           sync.Mutex
	writeData    []byte
	responseFunc func(writeData []byte) []byte
	closed       bool
}

func (f *mockDynamicLLMFile) Write(_ gocontext.Context, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writeData = data
	return nil
}

func (f *mockDynamicLLMFile) Read(length int) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.responseFunc != nil {
		return f.responseFunc(f.writeData), nil
	}
	return makeLLMResponse("default", 10), nil
}

func (f *mockDynamicLLMFile) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *mockDynamicLLMFile) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{IsDevice: true, Name: "/dev/llm/claude"}, nil
}

func TestOODAReasonStep_SingleCycle(t *testing.T) {
	// AC #1-#4: Single OODA cycle completing with OODAComplete action
	// Observe -> Orient -> Decide(complete) -> Act
	callCount := 0
	responseFunc := func(writeData []byte) []byte {
		callCount++
		switch {
		case callCount == 1:
			// Orient response
			return makeLLMResponse("orientation: state is aligned with goals", 20)
		case callCount == 2:
			// Decide response - choose complete
			decision := map[string]any{
				"action": "complete",
				"target": "",
				"data":   map[string]any{},
				"reason": "goal achieved",
			}
			decisionJSON, _ := json.Marshal(decision)
			return makeLLMResponse(string(decisionJSON), 20)
		default:
			return makeLLMResponse("unexpected call", 5)
		}
	}

	k, _, _ := newOODATestKernel(t, responseFunc)

	pid, err := k.Spawn("ooda single cycle test", nil, SpawnOpts{
		ReasoningMode: "ooda",
		MaxTurns:      5,
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d not found", pid)
	}

	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("expected exit code 0 (complete), got %d: %s", exit.Code, exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for OODA process completion")
	}

	if proc.GetState() != types.StateZombie {
		t.Fatalf("expected Zombie state, got %d", proc.GetState())
	}
}

func TestOODAReasonStep_MultipleCycles(t *testing.T) {
	// AC #1-#4: Multiple OODA cycles with tool_call then complete
	callCount := 0
	responseFunc := func(writeData []byte) []byte {
		callCount++
		// Cycle 1: Orient -> Decide(tool_call) -> tool result
		// Cycle 2: Orient -> Decide(complete)
		switch {
		case callCount == 1:
			// Cycle 1 Orient
			return makeLLMResponse("need more data from filesystem", 15)
		case callCount == 2:
			// Cycle 1 Decide: tool_call
			decision := map[string]any{
				"action": "tool_call",
				"target": "/dev/fs",
				"data":   map[string]any{"path": "/tmp/test"},
				"reason": "need file contents",
			}
			decisionJSON, _ := json.Marshal(decision)
			return makeLLMResponse(string(decisionJSON), 15)
		case callCount == 3:
			// Cycle 2 Orient
			return makeLLMResponse("data collected, goals met", 15)
		case callCount == 4:
			// Cycle 2 Decide: complete
			decision := map[string]any{
				"action": "complete",
				"target": "",
				"data":   map[string]any{},
				"reason": "all data collected",
			}
			decisionJSON, _ := json.Marshal(decision)
			return makeLLMResponse(string(decisionJSON), 15)
		default:
			return makeLLMResponse("unexpected", 5)
		}
	}

	k, _, _ := newOODATestKernel(t, responseFunc)

	pid, err := k.Spawn("ooda multi-cycle test", nil, SpawnOpts{
		ReasoningMode: "ooda",
		MaxTurns:      10,
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d not found", pid)
	}

	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("expected exit code 0, got %d: %s", exit.Code, exit.Reason)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for multi-cycle OODA completion")
	}
}

func TestOODAReasonStep_SpawnAction(t *testing.T) {
	// AC #3: Decide chooses spawn, verify child process creation
	callCount := 0
	responseFunc := func(writeData []byte) []byte {
		callCount++
		switch {
		case callCount == 1:
			return makeLLMResponse("need sub-agent for specialized task", 15)
		case callCount == 2:
			decision := map[string]any{
				"action": "spawn",
				"target": "analyze sub-task",
				"data":   map[string]any{},
				"reason": "delegate specialized analysis",
			}
			decisionJSON, _ := json.Marshal(decision)
			return makeLLMResponse(string(decisionJSON), 15)
		case callCount == 3:
			// Child process linear reasonStep response (text = completes child)
			return makeLLMResponse("child analysis result", 10)
		case callCount == 4:
			// Parent cycle 2 Orient
			return makeLLMResponse("spawn completed, goals met", 15)
		case callCount == 5:
			// Parent cycle 2 Decide: complete
			decision := map[string]any{
				"action": "complete",
				"target": "",
				"data":   map[string]any{},
				"reason": "sub-task completed",
			}
			decisionJSON, _ := json.Marshal(decision)
			return makeLLMResponse(string(decisionJSON), 15)
		default:
			return makeLLMResponse("unexpected", 5)
		}
	}

	k, _, _ := newOODATestKernel(t, responseFunc)

	pid, err := k.Spawn("ooda spawn test", nil, SpawnOpts{
		ReasoningMode: "ooda",
		MaxTurns:      10,
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d not found", pid)
	}

	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("expected exit code 0, got %d: %s", exit.Code, exit.Reason)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for OODA spawn test completion")
	}
}

func TestOODAReasonStep_ReplanAction(t *testing.T) {
	// AC #3: Decide chooses replan, verify no external actions, next cycle re-evaluates
	callCount := 0
	responseFunc := func(writeData []byte) []byte {
		callCount++
		switch {
		case callCount == 1:
			return makeLLMResponse("initial assessment unclear", 15)
		case callCount == 2:
			// Replan - no external action should occur
			decision := map[string]any{
				"action": "replan",
				"target": "",
				"data":   map[string]any{},
				"reason": "need to adjust approach",
			}
			decisionJSON, _ := json.Marshal(decision)
			return makeLLMResponse(string(decisionJSON), 15)
		case callCount == 3:
			return makeLLMResponse("replan complete, now aligned", 15)
		case callCount == 4:
			decision := map[string]any{
				"action": "complete",
				"target": "",
				"data":   map[string]any{},
				"reason": "replanned and completed",
			}
			decisionJSON, _ := json.Marshal(decision)
			return makeLLMResponse(string(decisionJSON), 15)
		default:
			return makeLLMResponse("unexpected", 5)
		}
	}

	k, _, _ := newOODATestKernel(t, responseFunc)

	pid, err := k.Spawn("ooda replan test", nil, SpawnOpts{
		ReasoningMode: "ooda",
		MaxTurns:      10,
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d not found", pid)
	}

	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("expected exit code 0, got %d: %s", exit.Code, exit.Reason)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for OODA replan test")
	}
}

func TestOODAReasonStep_MaxCyclesExceeded(t *testing.T) {
	// AC #4: Exceed max cycles, verify exit code != 0
	responseFunc := func(writeData []byte) []byte {
		// Always return tool_call decision, never complete
		decision := map[string]any{
			"action": "tool_call",
			"target": "/dev/fs",
			"data":   map[string]any{},
			"reason": "still working",
		}
		decisionJSON, _ := json.Marshal(decision)
		return makeLLMResponse(string(decisionJSON), 10)
	}

	k, _, _ := newOODATestKernel(t, responseFunc)

	pid, err := k.Spawn("ooda max cycles test", nil, SpawnOpts{
		ReasoningMode: "ooda",
		MaxTurns:      2, // Very low to force max cycles
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d not found", pid)
	}

	select {
	case exit := <-proc.Done:
		if exit.Code == 0 {
			t.Fatal("expected non-zero exit code when max cycles exceeded")
		}
		if exit.Reason != "max ooda cycles exceeded" {
			t.Fatalf("expected reason 'max ooda cycles exceeded', got %q", exit.Reason)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for max cycles exceeded")
	}
}

func TestOODAReasonStep_ContextCancellation(t *testing.T) {
	// AC #4: Context cancelled mid-cycle, verify clean exit
	responseFunc := func(writeData []byte) []byte {
		// Slow response - give time for cancellation
		time.Sleep(100 * time.Millisecond)
		return makeLLMResponse("orientation", 10)
	}

	k, _, _ := newOODATestKernel(t, responseFunc)

	pid, err := k.Spawn("ooda cancel test", nil, SpawnOpts{
		ReasoningMode: "ooda",
		MaxTurns:      100,
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d not found", pid)
	}

	// Cancel the process after a short delay
	time.AfterFunc(200*time.Millisecond, func() {
		_ = k.Kill(pid, types.SIGKILL)
	})

	select {
	case <-proc.Done:
		// Process should exit due to cancellation
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for cancelled OODA process")
	}
}

func TestOODAReasonStep_ObserveError(t *testing.T) {
	// AC #1: Observe phase encounters VFS read failure, verify error handling
	// This test verifies graceful degradation when observation sources are unavailable
	responseFunc := func(writeData []byte) []byte {
		decision := map[string]any{
			"action": "complete",
			"target": "",
			"data":   map[string]any{},
			"reason": "completed despite observe errors",
		}
		decisionJSON, _ := json.Marshal(decision)
		return makeLLMResponse(string(decisionJSON), 10)
	}

	k, _, _ := newOODATestKernel(t, responseFunc)

	pid, err := k.Spawn("ooda observe error test", nil, SpawnOpts{
		ReasoningMode: "ooda",
		MaxTurns:      5,
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d not found", pid)
	}

	select {
	case exit := <-proc.Done:
		// Should handle gracefully - either complete or exit with error
		_ = exit
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for OODA observe error handling")
	}
}

// --- NFR41: Framework Overhead Performance ---

func TestOODAReasonStep_FrameworkOverhead(t *testing.T) {
	// NFR41: Single OODA cycle framework overhead <= 200ms
	// Uses instant LLM mock to isolate pure framework code timing
	responseFunc := func(writeData []byte) []byte {
		// Instant response - no simulated delay
		decision := map[string]any{
			"action": "complete",
			"target": "",
			"data":   map[string]any{},
			"reason": "instant completion for perf test",
		}
		decisionJSON, _ := json.Marshal(decision)
		return makeLLMResponse(string(decisionJSON), 5)
	}

	k, _, _ := newOODATestKernel(t, responseFunc)

	start := time.Now()
	pid, err := k.Spawn("ooda perf test", nil, SpawnOpts{
		ReasoningMode: "ooda",
		MaxTurns:      1,
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d not found", pid)
	}

	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	elapsed := time.Since(start)
	// NFR41: framework overhead should be well under 200ms with instant LLM
	if elapsed > 200*time.Millisecond {
		t.Fatalf("OODA framework overhead %v exceeds 200ms NFR41 limit", elapsed)
	}
}

// --- Spawn Compatibility Tests ---

func TestSpawn_DefaultReasoningMode(t *testing.T) {
	// Verify default SpawnOpts uses linear reasoning (not OODA)
	llmFile := &mockLLMFile{
		readData: makeLLMResponse("linear response", 42),
	}
	k, _, _ := newTestKernel(t, llmFile)

	pid, err := k.Spawn("default mode test", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d not found", pid)
	}

	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("expected exit code 0 for default linear mode, got %d: %s", exit.Code, exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	// Verify process did NOT use OODA
	if proc.IsOODA() {
		t.Fatal("default ReasoningMode should not enable OODA")
	}
}

func TestSpawn_OODAReasoningMode(t *testing.T) {
	// Verify ReasoningMode: "ooda" enables OODA loop
	responseFunc := func(writeData []byte) []byte {
		decision := map[string]any{
			"action": "complete",
			"target": "",
			"data":   map[string]any{},
			"reason": "ooda mode confirmed",
		}
		decisionJSON, _ := json.Marshal(decision)
		return makeLLMResponse(string(decisionJSON), 10)
	}

	k, _, _ := newOODATestKernel(t, responseFunc)

	pid, err := k.Spawn("ooda mode test", nil, SpawnOpts{
		ReasoningMode: "ooda",
		MaxTurns:      5,
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d not found", pid)
	}

	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("expected exit code 0, got %d: %s", exit.Code, exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	// Verify OODA was enabled
	if !proc.IsOODA() {
		t.Fatal("expected OODA to be enabled with ReasoningMode 'ooda'")
	}
}

// --- AC #1-#4: SyscallEvent & Log Integration ---

func TestOODAReasonStep_SyscallEvents(t *testing.T) {
	// Verify OODA phases emit correct SyscallEvent types
	responseFunc := func(writeData []byte) []byte {
		decision := map[string]any{
			"action": "complete",
			"target": "",
			"data":   map[string]any{},
			"reason": "emit test",
		}
		decisionJSON, _ := json.Marshal(decision)
		return makeLLMResponse(string(decisionJSON), 10)
	}

	k, _, _ := newOODATestKernel(t, responseFunc)

	pid, err := k.Spawn("ooda events test", nil, SpawnOpts{
		ReasoningMode: "ooda",
		MaxTurns:      1,
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d not found", pid)
	}

	// Collect events from DebugChan
	var events []types.SyscallEvent
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case evt, ok := <-proc.DebugChan:
				if !ok {
					return
				}
				events = append(events, evt)
			case <-time.After(3 * time.Second):
				return
			}
		}
	}()

	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	<-done

	// Verify OODA-specific events were emitted
	oodaEventTypes := map[string]bool{
		"OODAObserve": false,
		"OODAOrient":  false,
		"OODADecide":  false,
		"OODAAct":     false,
		"OODACycle":   false,
	}

	for _, evt := range events {
		if _, ok := oodaEventTypes[evt.Syscall]; ok {
			oodaEventTypes[evt.Syscall] = true
		}
	}

	for evtType, found := range oodaEventTypes {
		if !found {
			t.Errorf("expected OODA event %q was not emitted", evtType)
		}
	}
}

func TestOODAReasonStep_LogEntries(t *testing.T) {
	// Verify OODA phases produce correct LogEntry with LogOODA category
	responseFunc := func(writeData []byte) []byte {
		decision := map[string]any{
			"action": "complete",
			"target": "",
			"data":   map[string]any{},
			"reason": "log test",
		}
		decisionJSON, _ := json.Marshal(decision)
		return makeLLMResponse(string(decisionJSON), 10)
	}

	k, _, _ := newOODATestKernel(t, responseFunc)

	pid, err := k.Spawn("ooda logs test", nil, SpawnOpts{
		ReasoningMode: "ooda",
		MaxTurns:      1,
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d not found", pid)
	}

	// Collect log entries
	var logs []types.LogEntry
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case entry, ok := <-proc.LogChan:
				if !ok {
					return
				}
				logs = append(logs, entry)
			case <-time.After(3 * time.Second):
				return
			}
		}
	}()

	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	<-done

	// Verify LogOODA category entries exist
	foundOODALog := false
	for _, entry := range logs {
		if entry.Category == types.LogOODA {
			foundOODALog = true
			break
		}
	}
	if !foundOODALog {
		t.Error("expected at least one LogEntry with LogOODA category")
	}
}

// --- AC #1-#4: LogOODA Category ---

func TestLogOODA_Category(t *testing.T) {
	// Verify LogOODA constant exists in types package
	if types.LogOODA != types.LogCategory("ooda") {
		t.Fatalf("expected LogOODA = \"ooda\", got %q", types.LogOODA)
	}
}
