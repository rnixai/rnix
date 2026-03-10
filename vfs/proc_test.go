package vfs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// mockProcessInfoProvider is a test double for ProcessInfoProvider.
type mockProcessInfoProvider struct {
	procs map[types.PID]*ProcInfo
}

func (m *mockProcessInfoProvider) GetProcInfo(pid types.PID) (*ProcInfo, error) {
	info, ok := m.procs[pid]
	if !ok {
		return nil, &VFSError{
			Op:     "GetProcInfo",
			Device: "/proc",
			Err:    fmt.Errorf("process %d not found", pid),
			Code:   types.ErrNotFound,
		}
	}
	return info, nil
}

func (m *mockProcessInfoProvider) ListProcs() []ProcInfo {
	var result []ProcInfo
	for _, info := range m.procs {
		result = append(result, *info)
	}
	return result
}

// mockContextSummaryProvider is a test double for ContextSummaryProvider.
type mockContextSummaryProvider struct {
	summaries map[types.CtxID]string
}

func (m *mockContextSummaryProvider) GetContextSummary(ctxID types.CtxID) (string, error) {
	summary, ok := m.summaries[ctxID]
	if !ok {
		return "", fmt.Errorf("context %d not found", ctxID)
	}
	return summary, nil
}

func newTestProcFS() (*ProcFS, *mockProcessInfoProvider, *mockContextSummaryProvider) {
	provider := &mockProcessInfoProvider{
		procs: map[types.PID]*ProcInfo{
			1: {
				PID:            1,
				PPID:           0,
				State:          types.StateRunning,
				Intent:         "分析代码",
				Skills:         []string{"code-analysis"},
				TokensUsed:     1500,
				CreatedAt:      time.Now().Add(-6 * time.Second),
				CtxID:          10,
				Result:         "",
				AllowedDevices: []string{"/dev/fs", "/dev/shell"},
			},
			2: {
				PID:        2,
				PPID:       1,
				State:      types.StateZombie,
				Intent:     "读取文件",
				Skills:     nil,
				TokensUsed: 500,
				CreatedAt:  time.Now().Add(-3 * time.Second),
				CtxID:      20,
				Result:     "done",
			},
		},
	}
	ctxProvider := &mockContextSummaryProvider{
		summaries: map[types.CtxID]string{
			10: "Messages: 5 (system: 1, user: 2, assistant: 2)\nSystem Prompt: 1200 chars\nLast Message: [assistant] 分析完成，发现 3 个问题...",
			20: "Messages: 2 (system: 0, user: 1, assistant: 1)\nSystem Prompt: 0 chars\nLast Message: [assistant] done",
		},
	}
	procFS := NewProcFS(provider, ctxProvider)
	return procFS, provider, ctxProvider
}

// Task 8.1: Test ProcFS with mock ProcessInfoProvider
func TestProcFS_FileFactory(t *testing.T) {
	procFS, _, _ := newTestProcFS()
	factory := procFS.FileFactory()

	t.Run("creates file for valid path", func(t *testing.T) {
		file, err := factory("/1/status", O_RDONLY)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if file == nil {
			t.Fatal("expected file, got nil")
		}
		_ = file.Close()
	})
}

// Task 8.2: Test /proc/{pid}/status — verify JSON format and field completeness
func TestProcFS_Status(t *testing.T) {
	procFS, _, _ := newTestProcFS()
	factory := procFS.FileFactory()

	t.Run("returns valid JSON with all fields", func(t *testing.T) {
		file, err := factory("/1/status", O_RDONLY)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer file.Close()

		data, err := file.Read(1 << 20)
		if err != nil {
			t.Fatalf("Read failed: %v", err)
		}

		var status statusJSON
		if err := json.Unmarshal(data, &status); err != nil {
			t.Fatalf("invalid JSON: %v\nraw: %s", err, data)
		}

		if status.PID != 1 {
			t.Errorf("pid: got %d, want 1", status.PID)
		}
		if status.PPID != 0 {
			t.Errorf("ppid: got %d, want 0", status.PPID)
		}
		if status.State != "running" {
			t.Errorf("state: got %q, want %q", status.State, "running")
		}
		if status.Intent != "分析代码" {
			t.Errorf("intent: got %q, want %q", status.Intent, "分析代码")
		}
		if len(status.Skills) != 1 || status.Skills[0] != "code-analysis" {
			t.Errorf("skills: got %v, want [code-analysis]", status.Skills)
		}
		if status.TokensUsed != 1500 {
			t.Errorf("tokens_used: got %d, want 1500", status.TokensUsed)
		}
		if status.ElapsedMs < 6000 {
			t.Errorf("elapsed_ms: got %d, want >= 6000", status.ElapsedMs)
		}
		if len(status.AllowedDevices) != 2 {
			t.Errorf("allowed_devices: got %v, want 2 items", status.AllowedDevices)
		}
	})

	t.Run("zombie process returns correct state", func(t *testing.T) {
		file, err := factory("/2/status", O_RDONLY)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer file.Close()

		data, _ := file.Read(1 << 20)
		var status statusJSON
		_ = json.Unmarshal(data, &status)

		if status.State != "zombie" {
			t.Errorf("state: got %q, want %q", status.State, "zombie")
		}
		if status.PID != 2 {
			t.Errorf("pid: got %d, want 2", status.PID)
		}
		if status.PPID != 1 {
			t.Errorf("ppid: got %d, want 1", status.PPID)
		}
	})

	t.Run("nil skills and allowed_devices become empty arrays", func(t *testing.T) {
		file, err := factory("/2/status", O_RDONLY)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer file.Close()

		data, _ := file.Read(1 << 20)
		var raw map[string]json.RawMessage
		_ = json.Unmarshal(data, &raw)

		// skills should be [] not null
		if string(raw["skills"]) == "null" {
			t.Error("skills should be [] not null")
		}
		// allowed_devices should be [] not null
		if string(raw["allowed_devices"]) == "null" {
			t.Error("allowed_devices should be [] not null")
		}
	})
}

// Task 8.3: Test /proc/{pid}/intent — verify raw intent text
func TestProcFS_Intent(t *testing.T) {
	procFS, _, _ := newTestProcFS()
	factory := procFS.FileFactory()

	t.Run("returns raw intent text", func(t *testing.T) {
		file, err := factory("/1/intent", O_RDONLY)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer file.Close()

		data, err := file.Read(1 << 20)
		if err != nil {
			t.Fatalf("Read failed: %v", err)
		}

		if string(data) != "分析代码" {
			t.Errorf("got %q, want %q", string(data), "分析代码")
		}
	})
}

// Task 8.4: Test /proc/{pid}/context — verify context summary
func TestProcFS_Context(t *testing.T) {
	procFS, _, _ := newTestProcFS()
	factory := procFS.FileFactory()

	t.Run("returns context summary", func(t *testing.T) {
		file, err := factory("/1/context", O_RDONLY)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer file.Close()

		data, err := file.Read(1 << 20)
		if err != nil {
			t.Fatalf("Read failed: %v", err)
		}

		got := string(data)
		if got == "" {
			t.Fatal("expected non-empty context summary")
		}
		// Should contain "Messages: 5"
		if !strings.Contains(got, "Messages: 5") {
			t.Errorf("expected summary to contain 'Messages: 5', got: %s", got)
		}
	})
}

// Task 8.5: Test PID not found — verify ErrNotFound
func TestProcFS_PIDNotFound(t *testing.T) {
	procFS, _, _ := newTestProcFS()
	factory := procFS.FileFactory()

	t.Run("returns ErrNotFound for nonexistent PID", func(t *testing.T) {
		_, err := factory("/999/status", O_RDONLY)
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		var vfsErr *VFSError
		if !errors.As(err, &vfsErr) {
			t.Fatalf("expected *VFSError, got %T: %v", err, err)
		}
		if vfsErr.Code != types.ErrNotFound {
			t.Errorf("code: got %q, want %q", vfsErr.Code, types.ErrNotFound)
		}
	})
}

// Task 8.6: Test invalid path formats
func TestProcFS_InvalidPaths(t *testing.T) {
	procFS, _, _ := newTestProcFS()
	factory := procFS.FileFactory()

	tests := []struct {
		name    string
		subpath string
	}{
		{"empty path", ""},
		{"root only", "/"},
		{"no file", "/1"},
		{"no file with slash", "/1/"},
		{"non-numeric PID", "/abc/status"},
		{"unsupported file", "/1/cmdline"},
		{"extra segments", "/1/status/extra"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := factory(tt.subpath, O_RDONLY)
			if err == nil {
				t.Fatalf("expected error for subpath %q, got nil", tt.subpath)
			}

			var vfsErr *VFSError
			if !errors.As(err, &vfsErr) {
				t.Fatalf("expected *VFSError, got %T: %v", err, err)
			}
			if vfsErr.Code != types.ErrNotFound {
				t.Errorf("code: got %q, want %q", vfsErr.Code, types.ErrNotFound)
			}
		})
	}
}

// Task 8.7: Test Write rejection — /proc is read-only
func TestProcFS_WriteRejected(t *testing.T) {
	procFS, _, _ := newTestProcFS()
	factory := procFS.FileFactory()

	file, err := factory("/1/status", O_RDONLY)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer file.Close()

	err = file.Write(context.Background(), []byte("data"))
	if err == nil {
		t.Fatal("expected error on Write to /proc, got nil")
	}

	var vfsErr *VFSError
	if !errors.As(err, &vfsErr) {
		t.Fatalf("expected *VFSError, got %T: %v", err, err)
	}
	if vfsErr.Code != types.ErrPermission {
		t.Errorf("code: got %q, want %q", vfsErr.Code, types.ErrPermission)
	}
}

// Test Read offset behavior (partial reads and EOF)
func TestProcFile_ReadOffset(t *testing.T) {
	procFS, _, _ := newTestProcFS()
	factory := procFS.FileFactory()

	file, err := factory("/1/intent", O_RDONLY)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer file.Close()

	// First read: partial
	data1, err := file.Read(3)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if len(data1) == 0 {
		t.Fatal("expected non-empty first read")
	}

	// Continue reading
	data2, err := file.Read(1 << 20)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	full := string(append(data1, data2...))
	if full != "分析代码" {
		t.Errorf("combined reads: got %q, want %q", full, "分析代码")
	}

	// EOF
	data3, err := file.Read(1)
	if err != nil {
		t.Fatalf("Read at EOF failed: %v", err)
	}
	if data3 != nil {
		t.Errorf("expected nil at EOF, got %v", data3)
	}
}

// Test Stat returns correct metadata
func TestProcFile_Stat(t *testing.T) {
	procFS, _, _ := newTestProcFS()
	factory := procFS.FileFactory()

	file, err := factory("/1/intent", O_RDONLY)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}

	if stat.Name != "/proc/1/intent" {
		t.Errorf("Name: got %q, want %q", stat.Name, "/proc/1/intent")
	}
	if stat.Size <= 0 {
		t.Errorf("Size: got %d, want > 0", stat.Size)
	}
}

// Test Close is idempotent
func TestProcFile_Close(t *testing.T) {
	procFS, _, _ := newTestProcFS()
	factory := procFS.FileFactory()

	file, err := factory("/1/status", O_RDONLY)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := file.Close(); err != nil {
		t.Fatalf("first Close failed: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("second Close failed: %v", err)
	}
}

// Task 8.9: Test concurrent reads — multiple goroutines reading /proc without races
func TestProcFS_ConcurrentReads(t *testing.T) {
	procFS, _, _ := newTestProcFS()
	factory := procFS.FileFactory()

	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			file, err := factory("/1/status", O_RDONLY)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			defer file.Close()

			data, err := file.Read(1 << 20)
			if err != nil {
				t.Errorf("Read failed: %v", err)
				return
			}
			if len(data) == 0 {
				t.Error("expected non-empty data")
			}
		})
	}
	wg.Wait()
}

// ============================================================
// ATDD RED PHASE — Story 10.3: Token 预算管理 (AC5)
//
// Tests reference ProcInfo.ContextBudget and statusJSON.ContextBudget
// which do NOT exist yet → compile failure = RED phase.
// ============================================================

// --- 10.3-UNIT-050: [P1] statusJSON includes context_budget when set ---

func TestProcFS_Status_IncludesContextBudget(t *testing.T) {
	provider := &mockProcessInfoProvider{
		procs: map[types.PID]*ProcInfo{
			1: {
				PID:           1,
				PPID:          0,
				State:         types.StateRunning,
				Intent:        "budget test",
				Skills:        []string{"test"},
				TokensUsed:    3000,
				CreatedAt:     time.Now(),
				ContextBudget: 5000,
			},
		},
	}
	ctxProvider := &mockContextSummaryProvider{summaries: map[types.CtxID]string{}}
	procFS := NewProcFS(provider, ctxProvider)
	factory := procFS.FileFactory()

	file, err := factory("/1/status", O_RDONLY)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer file.Close()

	data, err := file.Read(1 << 20)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	var status statusJSON
	if err := json.Unmarshal(data, &status); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %s", err, data)
	}

	if status.ContextBudget != 5000 {
		t.Errorf("context_budget: got %d, want 5000", status.ContextBudget)
	}
}

// --- 10.3-UNIT-051: [P1] statusJSON omits context_budget when 0 ---

func TestProcFS_Status_OmitsContextBudgetWhenZero(t *testing.T) {
	provider := &mockProcessInfoProvider{
		procs: map[types.PID]*ProcInfo{
			1: {
				PID:           1,
				State:         types.StateRunning,
				Intent:        "no budget",
				Skills:        []string{},
				TokensUsed:    500,
				CreatedAt:     time.Now(),
				ContextBudget: 0,
			},
		},
	}
	ctxProvider := &mockContextSummaryProvider{summaries: map[types.CtxID]string{}}
	procFS := NewProcFS(provider, ctxProvider)
	factory := procFS.FileFactory()

	file, err := factory("/1/status", O_RDONLY)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer file.Close()

	data, err := file.Read(1 << 20)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if _, exists := raw["context_budget"]; exists {
		t.Error("context_budget should be omitted when 0 (omitempty)")
	}
}

// --- 10.3-UNIT-052: [P2] ProcInfo.ContextBudget field exists and serializes ---

func TestProcInfo_ContextBudget_FieldExists(t *testing.T) {
	info := ProcInfo{
		PID:           1,
		State:         types.StateRunning,
		ContextBudget: 10000,
	}
	if info.ContextBudget != 10000 {
		t.Errorf("expected ContextBudget 10000, got %d", info.ContextBudget)
	}
}

// --- 10.3-UNIT-053: [P2] ListProcs returns ContextBudget for all processes ---

func TestProcFS_ListProcs_ContextBudget(t *testing.T) {
	provider := &mockProcessInfoProvider{
		procs: map[types.PID]*ProcInfo{
			1: {PID: 1, State: types.StateRunning, ContextBudget: 3000},
			2: {PID: 2, State: types.StateRunning, ContextBudget: 0},
			3: {PID: 3, State: types.StateRunning, ContextBudget: 8000},
		},
	}
	procs := provider.ListProcs()
	budgets := make(map[types.PID]int)
	for _, p := range procs {
		budgets[p.PID] = p.ContextBudget
	}
	if budgets[1] != 3000 {
		t.Errorf("PID 1 budget: got %d, want 3000", budgets[1])
	}
	if budgets[2] != 0 {
		t.Errorf("PID 2 budget: got %d, want 0", budgets[2])
	}
	if budgets[3] != 8000 {
		t.Errorf("PID 3 budget: got %d, want 8000", budgets[3])
	}
}

// Test stateToString mapping
func TestStateToString(t *testing.T) {
	tests := []struct {
		state types.ProcessState
		want  string
	}{
		{types.StateCreated, "created"},
		{types.StateRunning, "running"},
		{types.StateZombie, "zombie"},
		{types.StateDead, "dead"},
		{types.ProcessState(99), "unknown(99)"},
	}
	for _, tt := range tests {
		got := stateToString(tt.state)
		if got != tt.want {
			t.Errorf("stateToString(%d): got %q, want %q", tt.state, got, tt.want)
		}
	}
}

// Test parseProcPath
func TestParseProcPath(t *testing.T) {
	tests := []struct {
		subpath string
		pid     types.PID
		file    string
		wantErr bool
	}{
		{"/1/status", 1, "status", false},
		{"/42/intent", 42, "intent", false},
		{"/100/context", 100, "context", false},
		{"", 0, "", true},
		{"/", 0, "", true},
		{"/1", 0, "", true},
		{"/abc/status", 0, "", true},
		{"/1/cmdline", 0, "", true},
	}
	for _, tt := range tests {
		pid, file, err := parseProcPath(tt.subpath)
		if tt.wantErr {
			if err == nil {
				t.Errorf("parseProcPath(%q): expected error", tt.subpath)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseProcPath(%q): unexpected error: %v", tt.subpath, err)
			continue
		}
		if pid != tt.pid {
			t.Errorf("parseProcPath(%q): pid: got %d, want %d", tt.subpath, pid, tt.pid)
		}
		if file != tt.file {
			t.Errorf("parseProcPath(%q): file: got %q, want %q", tt.subpath, file, tt.file)
		}
	}
}
