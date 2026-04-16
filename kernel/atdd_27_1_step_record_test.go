package kernel

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// ============================================================
// Story 27.1: StepRecord 类型定义与磁盘写入器
// ============================================================

// marshalMessages is a test helper to serialize context.Message slice to json.RawMessage.
func marshalMessages(msgs []rnixctx.Message) json.RawMessage {
	data, _ := json.Marshal(msgs)
	return data
}

// --- AC-1: StepRecord 类型定义 ---

func TestATDD27_1_StepRecord_TypeDefinition(t *testing.T) {
	msgs := marshalMessages([]rnixctx.Message{{Role: rnixctx.RoleUser, Content: "hello"}})
	rec := types.StepRecord{
		Step:           1,
		Timestamp:      time.Duration(100 * time.Millisecond),
		Messages:       msgs,
		MessageCount:   1,
		TokenCount:     50,
		RawResponse:    `{"content":"test","tokens_used":50}`,
		Action:         "tool_call",
		Summary:        "called echo tool",
		ToolPath:       "/dev/tools/echo",
		ToolInput:      `{"msg":"hello"}`,
		ToolResult:     "echo-result",
		ToolError:      "",
		ToolDuration:   time.Duration(5 * time.Millisecond),
		RequestTokens:  100,
		ResponseTokens: 50,
	}

	if rec.Step != 1 {
		t.Fatalf("expected Step=1, got %d", rec.Step)
	}
	if rec.Action != "tool_call" {
		t.Fatalf("expected Action='tool_call', got %q", rec.Action)
	}
	if len(rec.Messages) == 0 {
		t.Fatal("Messages should not be empty")
	}
}

func TestATDD27_1_StepRecord_JSONRoundTrip(t *testing.T) {
	msgs2 := marshalMessages([]rnixctx.Message{{Role: rnixctx.RoleAssistant, Content: "I will help"}})
	rec := types.StepRecord{
		Step:           2,
		Timestamp:      time.Duration(200 * time.Millisecond),
		Messages:       msgs2,
		MessageCount:   3,
		TokenCount:     150,
		RawResponse:    `{"content":"response","tokens_used":50}`,
		Action:         "complete",
		Summary:        "task finished",
		RequestTokens:  100,
		ResponseTokens: 50,
	}

	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded types.StepRecord
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.Step != rec.Step {
		t.Fatalf("Step mismatch: %d vs %d", decoded.Step, rec.Step)
	}
	if decoded.Action != rec.Action {
		t.Fatalf("Action mismatch: %q vs %q", decoded.Action, rec.Action)
	}
	if decoded.RawResponse != rec.RawResponse {
		t.Fatalf("RawResponse mismatch")
	}
}

// --- AC-2: StepWriter 实现 ---

func TestATDD27_1_StepWriter_AppendAndRead(t *testing.T) {
	baseDir := t.TempDir()

	sw, err := NewStepWriter(baseDir, "test-uuid-42")
	if err != nil {
		t.Fatalf("NewStepWriter: %v", err)
	}
	defer sw.Close()

	for i := range 3 {
		rec := types.StepRecord{
			Step:        i + 1,
			Action:      "tool_call",
			Summary:     fmt.Sprintf("step %d", i+1),
			RawResponse: fmt.Sprintf(`{"content":"resp-%d","tokens_used":10}`, i+1),
		}
		if err := sw.WriteStep(rec); err != nil {
			t.Fatalf("WriteStep(%d): %v", i+1, err)
		}
	}

	stepsFile := filepath.Join(baseDir, "data", "steps", "test-uuid-42", "steps.jsonl")
	for i := range 3 {
		rec, err := ReadStep(stepsFile, i+1)
		if err != nil {
			t.Fatalf("ReadStep(%d): %v", i+1, err)
		}
		if rec.Step != i+1 {
			t.Fatalf("expected Step=%d, got %d", i+1, rec.Step)
		}
		expected := fmt.Sprintf("step %d", i+1)
		if rec.Summary != expected {
			t.Fatalf("expected Summary=%q, got %q", expected, rec.Summary)
		}
	}
}

func TestATDD27_1_StepWriter_ConcurrentSafety(t *testing.T) {
	baseDir := t.TempDir()

	sw, err := NewStepWriter(baseDir, "test-uuid-99")
	if err != nil {
		t.Fatalf("NewStepWriter: %v", err)
	}
	defer sw.Close()

	const goroutines = 10
	const stepsPerGoroutine = 5
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := range goroutines {
		go func(gIdx int) {
			defer wg.Done()
			for s := range stepsPerGoroutine {
				rec := types.StepRecord{
					Step:    gIdx*stepsPerGoroutine + s + 1,
					Action:  "tool_call",
					Summary: fmt.Sprintf("g%d-s%d", gIdx, s),
				}
				if err := sw.WriteStep(rec); err != nil {
					t.Errorf("WriteStep g%d-s%d: %v", gIdx, s, err)
				}
			}
		}(g)
	}
	wg.Wait()

	stepsFile := filepath.Join(baseDir, "data", "steps", "test-uuid-99", "steps.jsonl")
	f, err := os.Open(stepsFile)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer f.Close()

	lineCount := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lineCount++
		var rec types.StepRecord
		if err := json.Unmarshal(scanner.Bytes(), &rec); err != nil {
			t.Fatalf("invalid JSON on line %d: %v", lineCount, err)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("Scanner: %v", err)
	}

	expected := goroutines * stepsPerGoroutine
	if lineCount != expected {
		t.Fatalf("expected %d lines, got %d", expected, lineCount)
	}
}

func TestATDD27_1_StepWriter_FlushGuarantee(t *testing.T) {
	baseDir := t.TempDir()

	sw, err := NewStepWriter(baseDir, "test-uuid-77")
	if err != nil {
		t.Fatalf("NewStepWriter: %v", err)
	}
	defer sw.Close()

	rec := types.StepRecord{
		Step:    1,
		Action:  "complete",
		Summary: "flush-test",
	}
	if err := sw.WriteStep(rec); err != nil {
		t.Fatalf("WriteStep: %v", err)
	}

	stepsFile := filepath.Join(baseDir, "data", "steps", "test-uuid-77", "steps.jsonl")
	data, err := os.ReadFile(stepsFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("file should not be empty after WriteStep (flush guarantee violated)")
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var decoded types.StepRecord
	if err := json.Unmarshal([]byte(lines[0]), &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Summary != "flush-test" {
		t.Fatalf("expected Summary='flush-test', got %q", decoded.Summary)
	}
}

func TestATDD27_1_StepWriter_CreatesDirStructure(t *testing.T) {
	baseDir := t.TempDir()

	sw, err := NewStepWriter(baseDir, "test-uuid-123")
	if err != nil {
		t.Fatalf("NewStepWriter: %v", err)
	}
	defer sw.Close()

	expectedDir := filepath.Join(baseDir, "data", "steps", "test-uuid-123")
	info, err := os.Stat(expectedDir)
	if err != nil {
		t.Fatalf("directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("expected directory, got file")
	}

	expectedFile := filepath.Join(expectedDir, "steps.jsonl")
	if _, err := os.Stat(expectedFile); err != nil {
		t.Fatalf("steps.jsonl not created: %v", err)
	}
}

// --- AC-7: 写入性能 ≤ 1ms ---

func TestATDD27_1_StepWriter_WritePerformance(t *testing.T) {
	baseDir := t.TempDir()

	sw, err := NewStepWriter(baseDir, "test-uuid-88")
	if err != nil {
		t.Fatalf("NewStepWriter: %v", err)
	}
	defer sw.Close()

	perfMsgs := marshalMessages(make([]rnixctx.Message, 5))
	rec := types.StepRecord{
		Step:           1,
		Messages:       perfMsgs,
		MessageCount:   5,
		TokenCount:     500,
		RawResponse:    strings.Repeat("y", 2000),
		Action:         "tool_call",
		Summary:        "performance test",
		ToolPath:       "/dev/tools/perf",
		ToolInput:      strings.Repeat("z", 500),
		ToolResult:     strings.Repeat("w", 1000),
		RequestTokens:  300,
		ResponseTokens: 200,
	}

	// 预热
	if err := sw.WriteStep(rec); err != nil {
		t.Fatalf("warmup WriteStep: %v", err)
	}

	const iterations = 100
	start := time.Now()
	for range iterations {
		rec.Step++
		if err := sw.WriteStep(rec); err != nil {
			t.Fatalf("WriteStep: %v", err)
		}
	}
	elapsed := time.Since(start)
	avgMs := float64(elapsed.Milliseconds()) / float64(iterations)

	if avgMs > 1.0 {
		t.Fatalf("average write time %.3fms exceeds 1ms NFR62 limit", avgMs)
	}
	t.Logf("average write time: %.3fms over %d iterations", avgMs, iterations)
}

// --- AC-3 & AC-5 & AC-6: 集成测试 — reasonStep 自动创建并写入 StepRecord ---

func TestATDD27_1_Integration_StepRecordAutoCreatedOnSpawn(t *testing.T) {
	baseDir := t.TempDir()

	reg := vfs.NewDeviceRegistry()
	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeToolCallResponse("/dev/tools/echo", map[string]any{"msg": "hi"}, 50),
			makeCompleteResponse("all done", 30),
		},
	}
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return seqFile, nil
	})
	registerMockTool(reg, "/dev/tools/echo", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockToolFile{readData: []byte("echo-result")}, nil
	})

	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	k.SetStepDataDir(baseDir)

	pid, err := k.Spawn("step record auto test", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("exit code %d: %s", exit.Code, exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	// AC-3: StepWriter 应已自动创建并写入 steps.jsonl (UUID path since Story 28-2)
	stepsFile := filepath.Join(baseDir, "data", "steps", proc.UUID, "steps.jsonl")
	data, err := os.ReadFile(stepsFile)
	if err != nil {
		t.Fatalf("steps.jsonl not found — StepWriter not auto-created: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("steps.jsonl is empty — no StepRecords written")
	}

	// 应至少 2 条 StepRecord（tool_call + complete）
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected ≥2 StepRecords, got %d", len(lines))
	}

	// 解析第一条记录
	var firstRec types.StepRecord
	if err := json.Unmarshal([]byte(lines[0]), &firstRec); err != nil {
		t.Fatalf("Unmarshal first record: %v", err)
	}
	if firstRec.Step != 1 {
		t.Fatalf("expected first record Step=1, got %d", firstRec.Step)
	}
	if firstRec.Action != "tool_call" {
		t.Fatalf("expected first record Action='tool_call', got %q", firstRec.Action)
	}

	// AC-6: Messages 字段不为空
	if len(firstRec.Messages) == 0 {
		t.Fatal("first record Messages should not be empty (BuildPrompt snapshot)")
	}

	// AC-6: RawResponse 不应被截断
	if firstRec.RawResponse == "" {
		t.Fatal("first record RawResponse should not be empty")
	}

	// AC-6: ToolResult 应包含工具返回
	if firstRec.ToolPath != "/dev/tools/echo" {
		t.Fatalf("expected ToolPath='/dev/tools/echo', got %q", firstRec.ToolPath)
	}
	if !strings.Contains(firstRec.ToolResult, "echo-result") {
		t.Fatalf("expected ToolResult to contain 'echo-result', got %q", firstRec.ToolResult)
	}
}

// --- AC-4 & AC-5: FinalSystemPrompt ---

func TestATDD27_1_Integration_FinalSystemPromptCaptured(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeCompleteResponse("done", 30),
		},
	}
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return seqFile, nil
	})

	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	pid, err := k.Spawn("finalsysprompt test", nil, SpawnOpts{SystemPrompt: "You are a test assistant."})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("exit code %d: %s", exit.Code, exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	// AC-4 & AC-5: FinalSystemPrompt 应在首次 reasonStep 后被捕获
	proc.mu.Lock()
	fsp := proc.FinalSystemPrompt
	proc.mu.Unlock()
	if fsp == "" {
		t.Fatal("FinalSystemPrompt should be captured after first reasonStep")
	}
}

// --- AC-8: 进程退出清理 ---

func TestATDD27_1_Integration_ProcessMetaWrittenOnExit(t *testing.T) {
	baseDir := t.TempDir()

	reg := vfs.NewDeviceRegistry()
	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeCompleteResponse("exit meta test", 20),
		},
	}
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return seqFile, nil
	})

	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	k.SetStepDataDir(baseDir)

	pid, err := k.Spawn("meta exit test", nil, SpawnOpts{SystemPrompt: "You are a test assistant."})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	// Wait triggers reapProcess for top-level processes (PPID=0)
	exit, waitErr := k.Wait(pid)
	if waitErr != nil {
		t.Fatalf("Wait: %v", waitErr)
	}
	if exit.Code != 0 {
		t.Fatalf("exit code %d: %s", exit.Code, exit.Reason)
	}

	proc, _ := k.GetProcess(pid)
	// AC-8: process-meta.json 应被写入 (uses UUID path since Story 28-2)
	metaFile := filepath.Join(baseDir, "data", "steps", proc.UUID, "process-meta.json")
	data, err := os.ReadFile(metaFile)
	if err != nil {
		t.Fatalf("process-meta.json not found: %v", err)
	}

	var meta struct {
		SystemPrompt string `json:"system_prompt"`
		ToolDefs     []any  `json:"tool_defs"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("Unmarshal meta: %v", err)
	}

	if meta.SystemPrompt == "" {
		t.Fatal("process-meta.json system_prompt should not be empty")
	}
}

// --- AC-6: StepRecord 写入在 AppendMessage 之前 ---

func TestATDD27_1_Integration_WrittenBeforeAppendMessage(t *testing.T) {
	baseDir := t.TempDir()

	reg := vfs.NewDeviceRegistry()
	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeToolCallResponse("/dev/tools/echo", map[string]any{"msg": "order"}, 50),
			makeCompleteResponse("order done", 30),
		},
	}
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return seqFile, nil
	})
	registerMockTool(reg, "/dev/tools/echo", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockToolFile{readData: []byte("order-result")}, nil
	})

	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	k.SetStepDataDir(baseDir)

	pid, err := k.Spawn("write order test", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("exit code %d: %s", exit.Code, exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	stepsFile := filepath.Join(baseDir, "data", "steps", proc.UUID, "steps.jsonl")
	data, err := os.ReadFile(stepsFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 1 {
		t.Fatal("expected at least 1 StepRecord")
	}

	var rec types.StepRecord
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	// StepRecord.Messages 应该是 AppendMessage 之前的快照
	// 即不包含当前步骤的工具结果
	var msgs []rnixctx.Message
	if err := json.Unmarshal(rec.Messages, &msgs); err != nil {
		t.Fatalf("Unmarshal Messages: %v", err)
	}
	for _, m := range msgs {
		if m.Role == rnixctx.RoleTool && strings.Contains(m.Content, "order-result") {
			t.Fatal("StepRecord.Messages should NOT contain current step's tool result (must be snapshot before AppendMessage)")
		}
	}
}
