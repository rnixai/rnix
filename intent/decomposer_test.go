package intent

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

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

	tree, err := decomposer.Decompose(context.Background(), "我要一个完整的博客系统", "")

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

	backend := tree.Nodes["backend"]
	if backend == nil {
		t.Fatal("expected node 'backend'")
	}
	if len(backend.DependsOn) != 1 || backend.DependsOn[0] != "design" {
		t.Fatalf("expected backend depends_on=['design'], got %v", backend.DependsOn)
	}

	for id, node := range tree.Nodes {
		if node.State != IntentPending {
			t.Fatalf("node %q should be pending, got %q", id, node.State)
		}
	}
}

func TestDecomposer_Decompose_InvalidJSON(t *testing.T) {
	caller := &mockLLMCaller{response: "this is not valid json at all"}
	decomposer := NewDecomposer(caller)

	_, err := decomposer.Decompose(context.Background(), "build a blog", "")

	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestDecomposer_Decompose_CyclicDeps(t *testing.T) {
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

	_, err := decomposer.Decompose(context.Background(), "cyclic intent", "")

	if err == nil {
		t.Fatal("expected cycle detection error, got nil")
	}
}

func TestDecomposer_Decompose_EmptyResult(t *testing.T) {
	caller := &mockLLMCaller{response: "[]"}
	decomposer := NewDecomposer(caller)

	_, err := decomposer.Decompose(context.Background(), "empty intent", "")

	if err == nil {
		t.Fatal("expected error for empty decomposition result, got nil")
	}
}

func TestDecomposer_Decompose_LLMError(t *testing.T) {
	caller := &mockLLMCaller{err: fmt.Errorf("API rate limit exceeded")}
	decomposer := NewDecomposer(caller)

	_, err := decomposer.Decompose(context.Background(), "will fail", "")

	if err == nil {
		t.Fatal("expected LLM error to propagate, got nil")
	}
}

func TestDecomposer_Decompose_Timeout(t *testing.T) {
	caller := &mockLLMCaller{delay: 10 * time.Second, response: "[]"}
	decomposer := NewDecomposer(caller)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := decomposer.Decompose(ctx, "slow intent", "")

	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

func TestDecomposer_Decompose_ModelPassthrough(t *testing.T) {
	var capturedModel string
	caller := &recordingLLMCaller{
		response: `[{"id":"a","intent":"task","depends_on":[]}]`,
		onCall: func(_ context.Context, _ string, model string) {
			capturedModel = model
		},
	}
	decomposer := NewDecomposer(caller)

	_, err := decomposer.Decompose(context.Background(), "test intent", "claude-opus")

	if err != nil {
		t.Fatalf("Decompose failed: %v", err)
	}
	if capturedModel != "claude-opus" {
		t.Fatalf("expected model='claude-opus' passed to LLM, got %q", capturedModel)
	}
}

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

// --- Story 19.3: Incremental decomposition ---

func TestDecomposer_DecomposeIncremental(t *testing.T) {
	// Existing tree with design completed, backend in progress
	existingTree := &IntentTree{
		ID:         "intent-1",
		RootIntent: "build blog",
		State:      IntentExecuting,
		Nodes: map[string]*IntentNode{
			"design":  {ID: "design", Intent: "design schema", State: IntentCompleted},
			"backend": {ID: "backend", Intent: "implement API", State: IntentExecuting, DependsOn: []string{"design"}},
		},
		DesiredNodes: map[string]IntentState{
			"design":  IntentCompleted,
			"backend": IntentCompleted,
		},
		CreatedAt: time.Now(),
	}

	// LLM returns merged node list: existing + new "comment" node
	llmResponse := `[
		{"id":"design","intent":"design schema","depends_on":[]},
		{"id":"backend","intent":"implement API","depends_on":["design"]},
		{"id":"comment","intent":"implement comment feature","depends_on":["design"]}
	]`

	var capturedPrompt string
	caller := &recordingLLMCaller{
		response: llmResponse,
		onCall: func(_ context.Context, prompt string, _ string) {
			capturedPrompt = prompt
		},
	}
	decomposer := NewDecomposer(caller)

	nodes, err := decomposer.DecomposeIncremental(context.Background(), existingTree, "add comment feature", "claude-sonnet")

	if err != nil {
		t.Fatalf("DecomposeIncremental failed: %v", err)
	}
	if nodes == nil {
		t.Fatal("expected non-nil nodes")
	}
	if len(nodes) != 3 {
		t.Fatalf("expected 3 nodes from incremental decompose, got %d", len(nodes))
	}

	// Verify prompt includes existing tree context
	if capturedPrompt == "" {
		t.Fatal("expected prompt to be captured")
	}
	// Prompt should contain existing node info
	if !containsStr(capturedPrompt, "design") || !containsStr(capturedPrompt, "backend") {
		t.Fatal("expected prompt to contain existing node IDs")
	}
	if !containsStr(capturedPrompt, "add comment feature") {
		t.Fatal("expected prompt to contain the new intent")
	}

	// Verify returned nodes have expected IDs
	nodeIDs := make(map[string]bool)
	for _, n := range nodes {
		nodeIDs[n.ID] = true
	}
	if !nodeIDs["design"] || !nodeIDs["backend"] || !nodeIDs["comment"] {
		t.Fatalf("expected nodes design, backend, comment; got %v", nodeIDs)
	}
}

func TestDecomposer_DecomposeIncremental_InvalidJSON(t *testing.T) {
	existingTree := &IntentTree{
		ID:         "intent-1",
		RootIntent: "build blog",
		State:      IntentExecuting,
		Nodes: map[string]*IntentNode{
			"design": {ID: "design", Intent: "design schema", State: IntentCompleted},
		},
		DesiredNodes: map[string]IntentState{
			"design": IntentCompleted,
		},
		CreatedAt: time.Now(),
	}

	caller := &mockLLMCaller{response: "this is not valid json"}
	decomposer := NewDecomposer(caller)

	_, err := decomposer.DecomposeIncremental(context.Background(), existingTree, "invalid", "")

	if err == nil {
		t.Fatal("expected error for invalid JSON from LLM, got nil")
	}
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && stringContains(s, substr))
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// --- Story 19.2: Decompose initializes DesiredNodes ---

func TestDecomposer_Decompose_InitDesired(t *testing.T) {
	nodes := []struct {
		ID        string   `json:"id"`
		Intent    string   `json:"intent"`
		DependsOn []string `json:"depends_on"`
	}{
		{ID: "a", Intent: "task a", DependsOn: []string{}},
		{ID: "b", Intent: "task b", DependsOn: []string{"a"}},
	}
	jsonBytes, _ := json.Marshal(nodes)

	caller := &mockLLMCaller{response: string(jsonBytes)}
	decomposer := NewDecomposer(caller)

	tree, err := decomposer.Decompose(context.Background(), "test init desired", "")
	if err != nil {
		t.Fatalf("Decompose failed: %v", err)
	}

	if tree.DesiredNodes == nil {
		t.Fatal("expected DesiredNodes to be initialized after Decompose")
	}
	if len(tree.DesiredNodes) != 2 {
		t.Fatalf("expected 2 desired nodes, got %d", len(tree.DesiredNodes))
	}
	for nodeID, desired := range tree.DesiredNodes {
		if desired != IntentCompleted {
			t.Fatalf("expected desired state for %q = %q, got %q", nodeID, IntentCompleted, desired)
		}
	}
}
