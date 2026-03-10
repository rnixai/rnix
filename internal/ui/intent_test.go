package ui

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rnixai/rnix/ipc"
)

func TestRenderIntentTree_TTY(t *testing.T) {
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeDefault, Profile: TerminalProfile{Width: 120, ColorLevel: 0}}

	tree := &ipc.IntentTreeWire{
		ID:         "intent-1",
		RootIntent: "build blog",
		State:      "executing",
		Nodes: map[string]*ipc.IntentNodeWire{
			"design":  {ID: "design", Intent: "design data model", State: "completed", DependsOn: []string{}},
			"backend": {ID: "backend", Intent: "implement API", State: "executing", DependsOn: []string{"design"}},
		},
		CreatedAtMs: 1700000000000,
	}

	RenderIntentTree(r, tree, ModeDefault)

	output := buf.String()
	if !strings.Contains(output, "build blog") {
		t.Fatalf("expected root intent in output, got: %s", output)
	}
	if !strings.Contains(output, "intent-1") {
		t.Fatalf("expected intent ID in output, got: %s", output)
	}
	if !strings.Contains(output, "design") {
		t.Fatalf("expected 'design' node in output, got: %s", output)
	}
	if !strings.Contains(output, "backend") {
		t.Fatalf("expected 'backend' node in output, got: %s", output)
	}
}

func TestRenderIntentTree_JSON(t *testing.T) {
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeJSON, Profile: TerminalProfile{Width: 80, ColorLevel: 0}}

	tree := &ipc.IntentTreeWire{
		ID:         "intent-1",
		RootIntent: "test intent",
		State:      "pending",
		Nodes: map[string]*ipc.IntentNodeWire{
			"a": {ID: "a", Intent: "task A", State: "pending", DependsOn: []string{}},
		},
		CreatedAtMs: 1700000000000,
	}

	RenderIntentTree(r, tree, ModeJSON)

	output := buf.String()
	var parsed map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &parsed); err != nil {
		t.Fatalf("expected valid JSON output, got parse error: %v\noutput: %s", err, output)
	}
	if parsed["id"] != "intent-1" {
		t.Fatalf("expected id='intent-1' in JSON, got %v", parsed["id"])
	}
}

func TestRenderIntentTree_Quiet(t *testing.T) {
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeQuiet, Profile: TerminalProfile{Width: 80, ColorLevel: 0}}

	tree := &ipc.IntentTreeWire{
		ID:         "intent-1",
		RootIntent: "quiet test",
		State:      "pending",
		Nodes:      map[string]*ipc.IntentNodeWire{},
	}

	RenderIntentTree(r, tree, ModeQuiet)

	if buf.Len() != 0 {
		t.Fatalf("expected empty output in quiet mode, got: %s", buf.String())
	}
}

func TestRenderIntentProgress(t *testing.T) {
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeDefault, Profile: TerminalProfile{Width: 80, ColorLevel: 0}}

	RenderIntentProgress(r, 2, 4, ModeDefault)

	output := buf.String()
	if !strings.Contains(output, "2/4") {
		t.Fatalf("expected '2/4' in output, got: %s", output)
	}
}

func TestRenderIntentProgress_JSON(t *testing.T) {
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeJSON, Profile: TerminalProfile{Width: 80, ColorLevel: 0}}

	RenderIntentProgress(r, 3, 3, ModeJSON)

	output := strings.TrimSpace(buf.String())
	var parsed map[string]int
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Fatalf("expected valid JSON, got: %v", err)
	}
	if parsed["completed"] != 3 || parsed["total"] != 3 {
		t.Fatalf("expected completed=3, total=3, got %v", parsed)
	}
}

func TestRenderIntentNodeEvent_Start(t *testing.T) {
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeDefault, Profile: TerminalProfile{Width: 80, ColorLevel: 0}}

	RenderIntentNodeEvent(r, "start", "backend", "implement API", 42, ModeDefault)

	output := buf.String()
	if !strings.Contains(output, "backend") {
		t.Fatalf("expected 'backend' in output, got: %s", output)
	}
	if !strings.Contains(output, "42") {
		t.Fatalf("expected PID '42' in output, got: %s", output)
	}
}

func TestRenderIntentNodeEvent_JSON(t *testing.T) {
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeJSON, Profile: TerminalProfile{Width: 80, ColorLevel: 0}}

	RenderIntentNodeEvent(r, "done", "design", "success", 0, ModeJSON)

	output := strings.TrimSpace(buf.String())
	var parsed map[string]any
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Fatalf("expected valid JSON, got: %v", err)
	}
	if parsed["event"] != "done" {
		t.Fatalf("expected event='done', got %v", parsed["event"])
	}
}

// --- Story 19.2: Reconciler UI render tests ---

func TestRenderIntentNodeRetry_TTY(t *testing.T) {
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeDefault, Profile: TerminalProfile{Width: 120, ColorLevel: 0}}

	RenderIntentNodeRetry(r, "backend", 2, 3, ModeDefault)

	output := buf.String()
	if !strings.Contains(output, "backend") {
		t.Fatalf("expected 'backend' in retry output, got: %s", output)
	}
	if !strings.Contains(output, "2") {
		t.Fatalf("expected attempt '2' in retry output, got: %s", output)
	}
	if !strings.Contains(output, "3") {
		t.Fatalf("expected max '3' in retry output, got: %s", output)
	}
}

func TestRenderIntentNodeRetry_JSON(t *testing.T) {
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeJSON, Profile: TerminalProfile{Width: 80, ColorLevel: 0}}

	RenderIntentNodeRetry(r, "design", 1, 3, ModeJSON)

	output := strings.TrimSpace(buf.String())
	var parsed map[string]any
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Fatalf("expected valid JSON, got: %v\noutput: %s", err, output)
	}
	if parsed["event"] != "retry" {
		t.Fatalf("expected event='retry', got %v", parsed["event"])
	}
	if parsed["node_id"] != "design" {
		t.Fatalf("expected node_id='design', got %v", parsed["node_id"])
	}
}

func TestRenderIntentNodeTimeout_TTY(t *testing.T) {
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeDefault, Profile: TerminalProfile{Width: 120, ColorLevel: 0}}

	RenderIntentNodeTimeout(r, "slow-task", ModeDefault)

	output := buf.String()
	if !strings.Contains(output, "slow-task") {
		t.Fatalf("expected 'slow-task' in timeout output, got: %s", output)
	}
}

func TestRenderDriftList_TTY(t *testing.T) {
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeDefault, Profile: TerminalProfile{Width: 120, ColorLevel: 0}}

	drifts := []ipc.DriftItemWire{
		{NodeID: "a", Type: "node_failed", Message: "spawn error", DetectedAtMs: 1700000000000},
		{NodeID: "b", Type: "node_timeout", Message: "timed out", DetectedAtMs: 1700000001000},
	}

	RenderDriftList(r, drifts, ModeDefault)

	output := buf.String()
	if !strings.Contains(output, "a") || !strings.Contains(output, "b") {
		t.Fatalf("expected drift nodes in output, got: %s", output)
	}
}

func TestRenderDriftList_JSON(t *testing.T) {
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeJSON, Profile: TerminalProfile{Width: 80, ColorLevel: 0}}

	drifts := []ipc.DriftItemWire{
		{NodeID: "a", Type: "node_failed", Message: "error", DetectedAtMs: 1700000000000},
	}

	RenderDriftList(r, drifts, ModeJSON)

	output := strings.TrimSpace(buf.String())
	var parsed map[string]any
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Fatalf("expected valid JSON, got: %v\noutput: %s", err, output)
	}
	driftList, ok := parsed["drifts"].([]any)
	if !ok || len(driftList) != 1 {
		t.Fatalf("expected 1 drift in JSON output, got %v", parsed["drifts"])
	}
}

func TestRenderDriftList_Empty(t *testing.T) {
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeDefault, Profile: TerminalProfile{Width: 120, ColorLevel: 0}}

	RenderDriftList(r, nil, ModeDefault)

	output := buf.String()
	if output == "" {
		t.Fatal("expected non-empty output even with no drifts (should show 'no drift' message)")
	}
}

// --- Story 19.3: Incremental merge result & status detail rendering ---

func TestRenderIntentMergeResult_TTY(t *testing.T) {
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeDefault, Profile: TerminalProfile{Width: 120, ColorLevel: 0}}

	added := []string{"comment", "notification"}
	modified := []string{"design"}

	RenderIntentMergeResult(r, added, modified, ModeDefault)

	output := buf.String()
	if !strings.Contains(output, "comment") {
		t.Fatalf("expected 'comment' in added nodes output, got: %s", output)
	}
	if !strings.Contains(output, "notification") {
		t.Fatalf("expected 'notification' in added nodes output, got: %s", output)
	}
	if !strings.Contains(output, "design") {
		t.Fatalf("expected 'design' in modified nodes output, got: %s", output)
	}
}

func TestRenderIntentMergeResult_JSON(t *testing.T) {
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeJSON, Profile: TerminalProfile{Width: 80, ColorLevel: 0}}

	added := []string{"comment"}
	modified := []string{"design"}

	RenderIntentMergeResult(r, added, modified, ModeJSON)

	output := strings.TrimSpace(buf.String())
	var parsed map[string]any
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Fatalf("expected valid JSON output, got: %v\noutput: %s", err, output)
	}
	addedList, ok := parsed["added_nodes"].([]any)
	if !ok || len(addedList) != 1 {
		t.Fatalf("expected 1 added node in JSON, got %v", parsed["added_nodes"])
	}
	modifiedList, ok := parsed["modified_nodes"].([]any)
	if !ok || len(modifiedList) != 1 {
		t.Fatalf("expected 1 modified node in JSON, got %v", parsed["modified_nodes"])
	}
}

func TestRenderIntentStatusDetail_TTY(t *testing.T) {
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeDefault, Profile: TerminalProfile{Width: 120, ColorLevel: 0}}

	tree := &ipc.IntentTreeWire{
		ID:         "intent-1",
		RootIntent: "build blog",
		State:      "executing",
		Nodes: map[string]*ipc.IntentNodeWire{
			"design":   {ID: "design", Intent: "design data model", State: "completed", DependsOn: []string{}},
			"backend":  {ID: "backend", Intent: "implement API", State: "completed", DependsOn: []string{"design"}},
			"frontend": {ID: "frontend", Intent: "implement frontend", State: "executing", DependsOn: []string{"design"}, PID: 42},
			"comment":  {ID: "comment", Intent: "implement comments", State: "pending", DependsOn: []string{"design"}},
			"test":     {ID: "test", Intent: "write tests", State: "pending", DependsOn: []string{"backend", "frontend"}},
		},
		CreatedAtMs: 1700000000000,
	}

	RenderIntentStatusDetail(r, tree, ModeDefault)

	output := buf.String()
	// Should contain progress info
	if !strings.Contains(output, "2/5") || !strings.Contains(output, "40%") {
		t.Fatalf("expected progress '2/5 (40%%)' in output, got: %s", output)
	}
	// Should contain node states
	if !strings.Contains(output, "completed") {
		t.Fatalf("expected 'completed' state in output, got: %s", output)
	}
	if !strings.Contains(output, "executing") {
		t.Fatalf("expected 'executing' state in output, got: %s", output)
	}
	// Should contain active agent info
	if !strings.Contains(output, "frontend") && !strings.Contains(output, "42") {
		t.Fatalf("expected active agent info (frontend, PID 42) in output, got: %s", output)
	}
}

func TestRenderIntentList_TTY(t *testing.T) {
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeDefault, Profile: TerminalProfile{Width: 120, ColorLevel: 0}}

	trees := []*ipc.IntentTreeWire{
		{
			ID:         "intent-1",
			RootIntent: "build blog",
			State:      "executing",
			Nodes: map[string]*ipc.IntentNodeWire{
				"a": {ID: "a", Intent: "task a", State: "completed"},
				"b": {ID: "b", Intent: "task b", State: "executing"},
			},
			CreatedAtMs: 1700000000000,
		},
		{
			ID:         "intent-2",
			RootIntent: "build api",
			State:      "completed",
			Nodes: map[string]*ipc.IntentNodeWire{
				"x": {ID: "x", Intent: "task x", State: "completed"},
			},
			CreatedAtMs:   1700000100000,
			CompletedAtMs: 1700000200000,
		},
	}

	RenderIntentList(r, trees, ModeDefault)

	output := buf.String()
	if !strings.Contains(output, "intent-1") {
		t.Fatalf("expected 'intent-1' in list output, got: %s", output)
	}
	if !strings.Contains(output, "intent-2") {
		t.Fatalf("expected 'intent-2' in list output, got: %s", output)
	}
	if !strings.Contains(output, "build blog") {
		t.Fatalf("expected 'build blog' in list output, got: %s", output)
	}
	if !strings.Contains(output, "build api") {
		t.Fatalf("expected 'build api' in list output, got: %s", output)
	}
}

func TestRenderIntentList_Empty(t *testing.T) {
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeDefault, Profile: TerminalProfile{Width: 120, ColorLevel: 0}}

	RenderIntentList(r, nil, ModeDefault)

	output := buf.String()
	if output == "" {
		t.Fatal("expected non-empty output for empty list (should show 'no intents' message)")
	}
}
