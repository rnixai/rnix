package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	kernelmemory "github.com/rnixai/rnix/kernel/memory"
)

// =============================================================================
// ATDD Tests for Story 35.4: /dev/memory/recall VFS Device
// =============================================================================

// --- Mock RecallSearcher ---

type mockRecallSearcher struct {
	results []kernelmemory.RecallResult
	ready   bool
}

func (m *mockRecallSearcher) Search(query string, maxResults int) []kernelmemory.RecallResult {
	if maxResults > 0 && len(m.results) > maxResults {
		return m.results[:maxResults]
	}
	return m.results
}

func (m *mockRecallSearcher) Ready() bool {
	return m.ready
}

// --- Mock LLMCaller ---

type mockDeviceRecallLLMCaller struct {
	mu       sync.Mutex
	response string
	err      error
	calls    int
}

func (m *mockDeviceRecallLLMCaller) Call(ctx context.Context, systemPrompt, userPrompt string, maxTokens int) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	return m.response, m.err
}

func (m *mockDeviceRecallLLMCaller) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// =============================================================================
// ToolDef Tests (AC-5)
// =============================================================================

// 35.4-VFS-001: MemoryRecallDriver implements ToolDescriptor
func TestMemoryRecallDriver_ToolDefs(t *testing.T) {
	driver := NewRecallDriver(&mockRecallSearcher{ready: true}, nil)
	defs := driver.ToolDefs()

	if len(defs) != 1 {
		t.Fatalf("expected 1 ToolDef, got %d", len(defs))
	}

	def := defs[0]
	if def.Name != "MemoryRecall" {
		t.Errorf("expected Name='MemoryRecall', got %q", def.Name)
	}
	if !def.IsReadOnly {
		t.Error("expected IsReadOnly=true")
	}
	if !def.IsConcurrencySafe {
		t.Error("expected IsConcurrencySafe=true")
	}
	if def.IsDestructive {
		t.Error("expected IsDestructive=false")
	}
	if !def.ShouldDefer {
		t.Error("expected ShouldDefer=true")
	}
	if def.SearchHint == "" {
		t.Error("expected non-empty SearchHint")
	}
}

// =============================================================================
// Write/Read Search Tests (AC-3)
// =============================================================================

// 35.4-VFS-002: Write with valid query returns search results
func TestRecallFile_Write_Search(t *testing.T) {
	searcher := &mockRecallSearcher{
		ready: true,
		results: []kernelmemory.RecallResult{
			{UUID: "uuid-1", Summary: "yaml config merge", Timestamp: time.Now()},
		},
	}
	driver := NewRecallDriver(searcher, nil)
	file := &MemoryRecallFile{driver: driver}

	req, _ := json.Marshal(recallRequest{Query: "yaml config"})
	err := file.Write(context.Background(), req)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	data, err := file.Read(0)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	var resp recallResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if !resp.OK {
		t.Errorf("expected OK=true, got error: %s", resp.Error)
	}
	if resp.Count != 1 {
		t.Errorf("expected Count=1, got %d", resp.Count)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(resp.Results))
	}
	if resp.Results[0].UUID != "uuid-1" {
		t.Errorf("expected UUID='uuid-1', got %q", resp.Results[0].UUID)
	}
}

// 35.4-VFS-003: Write with empty query returns error
func TestRecallFile_Write_EmptyQuery(t *testing.T) {
	driver := NewRecallDriver(&mockRecallSearcher{ready: true}, nil)
	file := &MemoryRecallFile{driver: driver}

	req, _ := json.Marshal(recallRequest{Query: ""})
	_ = file.Write(context.Background(), req)

	data, _ := file.Read(0)
	var resp recallResponse
	json.Unmarshal(data, &resp)

	if resp.OK {
		t.Error("expected OK=false for empty query")
	}
	if resp.Error == "" {
		t.Error("expected non-empty error message")
	}
}

// 35.4-VFS-004: Write with invalid JSON returns error
func TestRecallFile_Write_InvalidJSON(t *testing.T) {
	driver := NewRecallDriver(&mockRecallSearcher{ready: true}, nil)
	file := &MemoryRecallFile{driver: driver}

	_ = file.Write(context.Background(), []byte("not json"))

	data, _ := file.Read(0)
	var resp recallResponse
	json.Unmarshal(data, &resp)

	if resp.OK {
		t.Error("expected OK=false for invalid JSON")
	}
}

// 35.4-VFS-005: Write when index not ready returns error
func TestRecallFile_Write_IndexNotReady(t *testing.T) {
	driver := NewRecallDriver(&mockRecallSearcher{ready: false}, nil)
	file := &MemoryRecallFile{driver: driver}

	req, _ := json.Marshal(recallRequest{Query: "test"})
	_ = file.Write(context.Background(), req)

	data, _ := file.Read(0)
	var resp recallResponse
	json.Unmarshal(data, &resp)

	if resp.OK {
		t.Error("expected OK=false when index not ready")
	}
	if resp.Error == "" {
		t.Error("expected non-empty error about index building")
	}
}

// 35.4-VFS-006: Read before Write returns error response
func TestRecallFile_Read_BeforeWrite(t *testing.T) {
	driver := NewRecallDriver(&mockRecallSearcher{ready: true}, nil)
	file := &MemoryRecallFile{driver: driver}

	data, err := file.Read(0)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	var resp recallResponse
	json.Unmarshal(data, &resp)

	if resp.OK {
		t.Error("expected OK=false before any Write")
	}
}

// =============================================================================
// LLM Summary Tests (AC-4)
// =============================================================================

// 35.4-VFS-007: Write with summarize=true calls LLM and includes summary
func TestRecallFile_Write_WithSummarize(t *testing.T) {
	caller := &mockDeviceRecallLLMCaller{response: "concise summary of yaml config results"}
	searcher := &mockRecallSearcher{
		ready: true,
		results: []kernelmemory.RecallResult{
			{UUID: "uuid-1", Summary: "yaml config merge", Timestamp: time.Now()},
		},
	}
	driver := NewRecallDriver(searcher, caller)
	file := &MemoryRecallFile{driver: driver}

	req, _ := json.Marshal(recallRequest{Query: "yaml", Summarize: true})
	_ = file.Write(context.Background(), req)

	data, _ := file.Read(0)
	var resp recallResponse
	json.Unmarshal(data, &resp)

	if !resp.OK {
		t.Errorf("expected OK=true, error: %s", resp.Error)
	}
	if resp.Summary == "" {
		t.Error("expected non-empty Summary when summarize=true")
	}
	if caller.CallCount() != 1 {
		t.Errorf("expected 1 LLM call, got %d", caller.CallCount())
	}
}

// 35.4-VFS-008: Write with summarize=true and LLM failure still returns results
func TestRecallFile_Write_SummarizeFallback(t *testing.T) {
	caller := &mockDeviceRecallLLMCaller{err: fmt.Errorf("network error")}
	searcher := &mockRecallSearcher{
		ready: true,
		results: []kernelmemory.RecallResult{
			{UUID: "uuid-1", Summary: "yaml config", Timestamp: time.Now()},
		},
	}
	driver := NewRecallDriver(searcher, caller)
	file := &MemoryRecallFile{driver: driver}

	req, _ := json.Marshal(recallRequest{Query: "yaml", Summarize: true})
	_ = file.Write(context.Background(), req)

	data, _ := file.Read(0)
	var resp recallResponse
	json.Unmarshal(data, &resp)

	// Should still return OK with results, just no summary
	if !resp.OK {
		t.Errorf("expected OK=true even when LLM fails, error: %s", resp.Error)
	}
	if resp.Count != 1 {
		t.Errorf("expected Count=1, got %d", resp.Count)
	}
}

// 35.4-VFS-009: Write with summarize=false does not call LLM
func TestRecallFile_Write_NoSummarize(t *testing.T) {
	caller := &mockDeviceRecallLLMCaller{response: "should not be called"}
	searcher := &mockRecallSearcher{
		ready: true,
		results: []kernelmemory.RecallResult{
			{UUID: "uuid-1", Summary: "yaml config", Timestamp: time.Now()},
		},
	}
	driver := NewRecallDriver(searcher, caller)
	file := &MemoryRecallFile{driver: driver}

	req, _ := json.Marshal(recallRequest{Query: "yaml", Summarize: false})
	_ = file.Write(context.Background(), req)

	if caller.CallCount() != 0 {
		t.Errorf("expected 0 LLM calls when summarize=false, got %d", caller.CallCount())
	}
}

// =============================================================================
// File Stat Tests
// =============================================================================

// 35.4-VFS-010: Stat returns correct device metadata
func TestRecallFile_Stat(t *testing.T) {
	driver := NewRecallDriver(&mockRecallSearcher{ready: true}, nil)
	file := &MemoryRecallFile{driver: driver}

	stat, err := file.Stat()
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if !stat.IsDevice {
		t.Error("expected IsDevice=true")
	}
	if stat.DevicePath != "/dev/memory/recall" {
		t.Errorf("expected DevicePath='/dev/memory/recall', got %q", stat.DevicePath)
	}
}

// =============================================================================
// RecallFileFactory Tests
// =============================================================================

// 35.4-VFS-011: RecallFileFactory produces working files
func TestRecallFileFactory(t *testing.T) {
	driver := NewRecallDriver(&mockRecallSearcher{ready: true}, nil)
	factory := RecallFileFactory(driver)

	file, err := factory("", 0, "/tmp")
	if err != nil {
		t.Fatalf("factory failed: %v", err)
	}
	if file == nil {
		t.Fatal("expected non-nil file")
	}

	stat, err := file.Stat()
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if stat.DevicePath != "/dev/memory/recall" {
		t.Errorf("expected DevicePath='/dev/memory/recall', got %q", stat.DevicePath)
	}
}

// =============================================================================
// Integration: End-to-End with Real RecallIndex
// =============================================================================

// 35.4-VFS-012: Full pipeline — build index, search via VFS device
func TestRecallFile_EndToEnd(t *testing.T) {
	dir := t.TempDir()

	// Create process step data
	stepsDir := filepath.Join(dir, "data", "steps", "uuid-e2e")
	if err := os.MkdirAll(stepsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(stepsDir, "steps.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	steps := []map[string]any{
		{"step": 1, "action": "tool_call", "summary": "reading yaml configuration file"},
		{"step": 2, "action": "tool_call", "summary": "merging yaml config with defaults"},
		{"step": 3, "action": "complete", "summary": "yaml config merge completed successfully"},
	}
	for _, s := range steps {
		data, _ := json.Marshal(s)
		fmt.Fprintf(f, "%s\n", data)
	}
	f.Close()

	// Build recall index
	idx := kernelmemory.NewRecallIndex()
	if err := idx.BuildFromDisk(filepath.Join(dir, "data", "steps")); err != nil {
		t.Fatal(err)
	}
	idx.WaitReady(0) // sync build is already done
	// Mark ready since we used sync BuildFromDisk
	// Need to use BuildFromDiskAsync for Ready() to be true
	// Use the index directly instead of checking Ready

	// Create VFS driver with a ready-reporting wrapper
	readySearcher := &readyRecallSearcherWrapper{idx: idx}
	driver2 := NewRecallDriver(readySearcher, nil)
	file := &MemoryRecallFile{driver: driver2}

	req, _ := json.Marshal(recallRequest{Query: "yaml config"})
	_ = file.Write(context.Background(), req)

	data, _ := file.Read(0)
	var resp recallResponse
	json.Unmarshal(data, &resp)

	if !resp.OK {
		t.Errorf("expected OK=true, error: %s", resp.Error)
	}
	if resp.Count == 0 {
		t.Error("expected at least 1 result in end-to-end test")
	}
}

// readyRecallSearcherWrapper wraps a RecallIndex and always reports Ready=true.
type readyRecallSearcherWrapper struct {
	idx *kernelmemory.RecallIndex
}

func (w *readyRecallSearcherWrapper) Search(query string, maxResults int) []kernelmemory.RecallResult {
	return w.idx.Search(query, maxResults)
}

func (w *readyRecallSearcherWrapper) Ready() bool {
	return true
}
