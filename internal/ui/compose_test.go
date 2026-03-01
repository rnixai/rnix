package ui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gonewx/crux/compose"
)

// --- Story 7.2: Compose Summary UI Tests ---
// These tests verify AC #4 (编排汇总) of Story 7.2.
// Tests reference RenderComposeSummary and RenderComposeSummaryJSON which will be
// created in internal/ui/compose.go during implementation.

func TestRenderComposeSummary_AllSuccess(t *testing.T) {
	// Given: all agents completed successfully
	// When: rendering compose summary
	// Then: output shows each agent with exit code 0 and durations

	InitStyles(TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeDefault, Profile: TerminalProfile{Width: 80, ColorLevel: 0}}

	results := []compose.ScheduleResult{
		{Name: "reviewer", PID: 1, ExitCode: 0, Duration: 6200 * time.Millisecond},
		{Name: "analyst", PID: 2, ExitCode: 0, Duration: 8500 * time.Millisecond},
	}

	RenderComposeSummary(r, results, nil)

	output := buf.String()
	if !strings.Contains(output, "reviewer") {
		t.Errorf("expected 'reviewer' in output, got %q", output)
	}
	if !strings.Contains(output, "analyst") {
		t.Errorf("expected 'analyst' in output, got %q", output)
	}
	if !strings.Contains(output, "2 succeeded") || !strings.Contains(output, "0 failed") {
		t.Errorf("expected success/failure counts in output, got %q", output)
	}
}

func TestRenderComposeSummary_WithFailures(t *testing.T) {
	// Given: some agents failed
	// When: rendering compose summary
	// Then: output shows failure status and count

	InitStyles(TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeDefault, Profile: TerminalProfile{Width: 80, ColorLevel: 0}}

	results := []compose.ScheduleResult{
		{Name: "reviewer", PID: 1, ExitCode: 0, Duration: 6 * time.Second},
		{Name: "writer", PID: 2, ExitCode: 1, Err: fmt.Errorf("LLM timeout"), Duration: 2 * time.Second},
		{Name: "reporter", PID: 0, Err: fmt.Errorf("upstream dependency failed"), Duration: 0},
	}

	RenderComposeSummary(r, results, nil)

	output := buf.String()
	if !strings.Contains(output, "writer") {
		t.Errorf("expected 'writer' in output, got %q", output)
	}
	if !strings.Contains(output, "failed") {
		t.Errorf("expected 'failed' status in output, got %q", output)
	}
	if !strings.Contains(output, "skipped") {
		t.Errorf("expected 'skipped' status for reporter, got %q", output)
	}
}

func TestRenderComposeSummary_WithSkipped(t *testing.T) {
	// Given: some agents were skipped due to upstream failure
	// When: rendering compose summary
	// Then: output shows skipped agents

	InitStyles(TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeDefault, Profile: TerminalProfile{Width: 80, ColorLevel: 0}}

	results := []compose.ScheduleResult{
		{Name: "upstream", PID: 1, ExitCode: 1, Err: fmt.Errorf("crashed"), Duration: 1 * time.Second},
		{Name: "downstream", PID: 0, Err: fmt.Errorf("upstream dependency failed"), Duration: 0},
	}

	RenderComposeSummary(r, results, nil)

	output := buf.String()
	if !strings.Contains(output, "downstream") {
		t.Errorf("expected 'downstream' in output, got %q", output)
	}
}

func TestRenderComposeSummary_EmptyResults(t *testing.T) {
	// Given: no agents ran
	// When: rendering compose summary
	// Then: output shows zero counts

	InitStyles(TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeDefault, Profile: TerminalProfile{Width: 80, ColorLevel: 0}}

	RenderComposeSummary(r, nil, nil)

	output := buf.String()
	if !strings.Contains(output, "0") {
		t.Errorf("expected zero count in empty results, got %q", output)
	}
}

func TestRenderComposeSummary_QuietMode(t *testing.T) {
	// Given: quiet output mode
	// When: rendering compose summary
	// Then: no output produced (or minimal)

	InitStyles(TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeQuiet, Profile: TerminalProfile{ColorLevel: 0}}

	results := []compose.ScheduleResult{
		{Name: "agent1", PID: 1, ExitCode: 0, Duration: 1 * time.Second},
	}

	RenderComposeSummary(r, results, nil)

	if buf.Len() != 0 {
		t.Errorf("expected no output in quiet mode, got %q", buf.String())
	}
}

func TestRenderComposeSummaryJSON_Valid(t *testing.T) {
	// Given: agents completed
	// When: rendering JSON summary
	// Then: output is valid JSON with agents array and summary object

	InitStyles(TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeJSON, Profile: TerminalProfile{ColorLevel: 0}}

	results := []compose.ScheduleResult{
		{Name: "agent1", PID: 1, ExitCode: 0, Duration: 5 * time.Second},
		{Name: "agent2", PID: 2, ExitCode: 1, Err: fmt.Errorf("failed"), Duration: 2 * time.Second},
	}

	RenderComposeSummaryJSON(r, results, nil)

	var parsed map[string]any
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %s", err, buf.String())
	}

	if parsed["ok"] != true {
		t.Error("expected ok=true")
	}
	data, ok := parsed["data"].(map[string]any)
	if !ok {
		t.Fatal("expected data to be object")
	}

	agentsList, ok := data["agents"].([]any)
	if !ok {
		t.Fatal("expected agents to be array")
	}
	if len(agentsList) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(agentsList))
	}

	summary, ok := data["summary"].(map[string]any)
	if !ok {
		t.Fatal("expected summary to be object")
	}

	// Verify summary fields
	for _, field := range []string{"total", "succeeded", "failed", "skipped", "total_tokens"} {
		if _, ok := summary[field]; !ok {
			t.Errorf("summary missing field %q", field)
		}
	}
}

func TestRenderComposeSummaryJSON_AgentFields(t *testing.T) {
	// Given: agents completed
	// When: rendering JSON summary
	// Then: each agent entry has required fields

	InitStyles(TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeJSON, Profile: TerminalProfile{ColorLevel: 0}}

	results := []compose.ScheduleResult{
		{Name: "reviewer", PID: 1, ExitCode: 0, Duration: 6200 * time.Millisecond},
	}

	RenderComposeSummaryJSON(r, results, nil)

	raw := buf.String()
	requiredFields := []string{"name", "status", "exit_code", "tokens_used", "elapsed_ms"}
	for _, field := range requiredFields {
		if !strings.Contains(raw, fmt.Sprintf(`"%s"`, field)) {
			t.Errorf("agent entry missing field %q in: %s", field, raw)
		}
	}
}

func TestRenderComposeSummaryJSON_EmptyResults(t *testing.T) {
	// Given: no agents ran
	// When: rendering JSON summary
	// Then: valid JSON with empty agents array

	InitStyles(TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeJSON, Profile: TerminalProfile{ColorLevel: 0}}

	RenderComposeSummaryJSON(r, nil, nil)

	var parsed map[string]any
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %s", err, buf.String())
	}

	data := parsed["data"].(map[string]any)
	agents := data["agents"].([]any)
	if len(agents) != 0 {
		t.Errorf("expected empty agents array, got %d items", len(agents))
	}
}

func TestRenderComposeProgress_Spawning(t *testing.T) {
	// Given: an agent is being spawned
	// When: rendering progress
	// Then: output shows spawning status with agent name

	InitStyles(TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeDefault, Profile: TerminalProfile{Width: 80, ColorLevel: 0}}

	RenderComposeProgress(r, "reviewer", "spawning", 1, 4, 1)

	output := buf.String()
	if !strings.Contains(output, "reviewer") {
		t.Errorf("expected agent name 'reviewer' in progress, got %q", output)
	}
	if !strings.Contains(output, "spawning") {
		t.Errorf("expected 'spawning' status in progress, got %q", output)
	}
	if !strings.Contains(output, "[compose]") {
		t.Errorf("expected '[compose]' prefix in progress, got %q", output)
	}
}

func TestRenderComposeProgress_Done(t *testing.T) {
	// Given: an agent completed successfully
	// When: rendering progress
	// Then: output shows done status with exit code and duration

	InitStyles(TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeDefault, Profile: TerminalProfile{Width: 80, ColorLevel: 0}}

	RenderComposeProgress(r, "analyst", "done", 2, 4, 0)

	output := buf.String()
	if !strings.Contains(output, "analyst") {
		t.Errorf("expected 'analyst' in progress, got %q", output)
	}
	if !strings.Contains(output, "done") {
		t.Errorf("expected 'done' status in progress, got %q", output)
	}
}

func TestRenderComposeProgress_Failed(t *testing.T) {
	// Given: an agent failed
	// When: rendering progress
	// Then: output shows failed status

	InitStyles(TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeDefault, Profile: TerminalProfile{Width: 80, ColorLevel: 0}}

	RenderComposeProgress(r, "writer", "failed", 3, 4, 1)

	output := buf.String()
	if !strings.Contains(output, "writer") {
		t.Errorf("expected 'writer' in progress, got %q", output)
	}
	if !strings.Contains(output, "failed") {
		t.Errorf("expected 'failed' status in progress, got %q", output)
	}
}

func TestRenderComposeProgress_Skipped(t *testing.T) {
	// Given: an agent was skipped
	// When: rendering progress
	// Then: output shows skipped status

	InitStyles(TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeDefault, Profile: TerminalProfile{Width: 80, ColorLevel: 0}}

	RenderComposeProgress(r, "reporter", "skipped", 4, 4, -1)

	output := buf.String()
	if !strings.Contains(output, "reporter") {
		t.Errorf("expected 'reporter' in progress, got %q", output)
	}
	if !strings.Contains(output, "skipped") {
		t.Errorf("expected 'skipped' status in progress, got %q", output)
	}
}

func TestRenderComposeProgress_QuietMode(t *testing.T) {
	// Given: quiet mode
	// When: rendering progress
	// Then: no output

	InitStyles(TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &Renderer{Writer: &buf, OutputMode: ModeQuiet, Profile: TerminalProfile{ColorLevel: 0}}

	RenderComposeProgress(r, "agent1", "spawning", 1, 1, 0)

	if buf.Len() != 0 {
		t.Errorf("expected no output in quiet mode, got %q", buf.String())
	}
}
