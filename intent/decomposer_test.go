package intent

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// --- Story 19.1 ATDD: Decomposer Tests (AC: #1) ---
// Tests for LLM-based intent decomposition.
// Uses mock LLMCaller to verify decomposition logic without real LLM calls.

// mockLLMCaller implements LLMCaller for testing.
type mockLLMCaller struct {
	response string
	err      error
	delay    time.Duration
}

func (m *mockLLMCaller) Call(ctx context.Context, prompt string, model string) (string, error) {
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return m.response, m.err
}

func TestDecomposer_Decompose_Success(t *testing.T) {
	t.Skip("ATDD RED: Decomposer.Decompose not yet implemented")

	// Given: a mock LLM that returns valid decomposition JSON
	nodes := []struct {
		ID        string   `json:"id"`
		Intent    string   `json:"intent"`
		DependsOn []string `json:"depends_on"`
	}{
		{ID: "design", Intent: "设计数据模型和 API 接口", DependsOn: []string{}},
		{ID: "backend", Intent: "实现后端 API 服务", DependsOn: []string{"design"}},
		{ID: "frontend", Intent: "实现前端界面", DependsOn: []string{"design"}},
		{ID: "test", Intent: "编写集成测试", DependsOn: []string{"backend", "frontend"}},
	}
	jsonBytes, _ := json.Marshal(nodes)

	caller := &mockLLMCaller{response: string(jsonBytes)}
	decomposer := NewDecomposer(caller)

	// When: decomposing a high-level intent
	tree, err := decomposer.Decompose(context.Background(), "我要一个完整的博客系统", "")

	// Then: IntentTree is constructed correctly
	if err != nil {
		t.Fatalf("Decompose failed: %v", err)
	}
	if tree == nil {
		t.Fatal("expected non-nil IntentTree")
	}
	if len(tree.Nodes) != 4 {
		t.Fatalf("expected 4 nodes, got %d", len(tree.Nodes))
	}
	if tree.RootIntent != "我要一个完整的博客系统" {
		t.Fatalf("expected root intent preserved, got %q", tree.RootIntent)
	}

	// Verify dependency relationships
	backend := tree.Nodes["backend"]
	if backend == nil {
		t.Fatal("expected node 'backend'")
	}
	if len(backend.DependsOn) != 1 || backend.DependsOn[0] != "design" {
		t.Fatalf("expected backend depends_on=['design'], got %v", backend.DependsOn)
	}

	// All nodes should start in pending state
	for id, node := range tree.Nodes {
		if node.State != IntentPending {
			t.Fatalf("node %q should be pending, got %q", id, node.State)
		}
	}
}

func TestDecomposer_Decompose_InvalidJSON(t *testing.T) {
	t.Skip("ATDD RED: Decomposer.Decompose JSON parsing not yet implemented")

	// Given: a mock LLM that returns invalid JSON
	caller := &mockLLMCaller{response: "this is not valid json at all"}
	decomposer := NewDecomposer(caller)

	// When: decomposing
	_, err := decomposer.Decompose(context.Background(), "build a blog", "")

	// Then: error indicating JSON parse failure
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestDecomposer_Decompose_CyclicDeps(t *testing.T) {
	t.Skip("ATDD RED: Decomposer.Decompose cycle validation not yet implemented")

	// Given: a mock LLM returns nodes with cyclic dependencies
	nodes := []struct {
		ID        string   `json:"id"`
		Intent    string   `json:"intent"`
		DependsOn []string `json:"depends_on"`
	}{
		{ID: "a", Intent: "task A", DependsOn: []string{"b"}},
		{ID: "b", Intent: "task B", DependsOn: []string{"a"}},
	}
	jsonBytes, _ := json.Marshal(nodes)

	caller := &mockLLMCaller{response: string(jsonBytes)}
	decomposer := NewDecomposer(caller)

	// When: decomposing
	_, err := decomposer.Decompose(context.Background(), "cyclic intent", "")

	// Then: error indicating cyclic dependency
	if err == nil {
		t.Fatal("expected cycle detection error, got nil")
	}
}

func TestDecomposer_Decompose_EmptyResult(t *testing.T) {
	t.Skip("ATDD RED: Decomposer.Decompose empty validation not yet implemented")

	// Given: a mock LLM returns an empty array
	caller := &mockLLMCaller{response: "[]"}
	decomposer := NewDecomposer(caller)

	// When: decomposing
	_, err := decomposer.Decompose(context.Background(), "empty intent", "")

	// Then: error indicating no sub-intents generated
	if err == nil {
		t.Fatal("expected error for empty decomposition result, got nil")
	}
}

func TestDecomposer_Decompose_LLMError(t *testing.T) {
	t.Skip("ATDD RED: Decomposer.Decompose error handling not yet implemented")

	// Given: a mock LLM that returns an error
	caller := &mockLLMCaller{err: fmt.Errorf("API rate limit exceeded")}
	decomposer := NewDecomposer(caller)

	// When: decomposing
	_, err := decomposer.Decompose(context.Background(), "will fail", "")

	// Then: error is propagated
	if err == nil {
		t.Fatal("expected LLM error to propagate, got nil")
	}
}

func TestDecomposer_Decompose_Timeout(t *testing.T) {
	t.Skip("ATDD RED: Decomposer.Decompose timeout handling not yet implemented")

	// Given: a mock LLM that takes too long
	caller := &mockLLMCaller{delay: 10 * time.Second, response: "[]"}
	decomposer := NewDecomposer(caller)

	// When: decomposing with a short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := decomposer.Decompose(ctx, "slow intent", "")

	// Then: context deadline exceeded
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

func TestDecomposer_Decompose_ModelPassthrough(t *testing.T) {
	t.Skip("ATDD RED: Decomposer.Decompose model passthrough not yet implemented")

	// Given: a mock LLM caller that records the model parameter
	var capturedModel string
	caller := &recordingLLMCaller{
		response: `[{"id":"a","intent":"task","depends_on":[]}]`,
		onCall: func(_ context.Context, _ string, model string) {
			capturedModel = model
		},
	}
	decomposer := NewDecomposer(caller)

	// When: decomposing with a specific model
	_, err := decomposer.Decompose(context.Background(), "test intent", "claude-opus")

	// Then: the model is passed to the LLM caller
	if err != nil {
		t.Fatalf("Decompose failed: %v", err)
	}
	if capturedModel != "claude-opus" {
		t.Fatalf("expected model='claude-opus' passed to LLM, got %q", capturedModel)
	}
}

// recordingLLMCaller records call parameters for verification.
type recordingLLMCaller struct {
	response string
	err      error
	onCall   func(ctx context.Context, prompt string, model string)
}

func (r *recordingLLMCaller) Call(ctx context.Context, prompt string, model string) (string, error) {
	if r.onCall != nil {
		r.onCall(ctx, prompt, model)
	}
	return r.response, r.err
}
