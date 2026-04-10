package shell

import (
	"testing"
)

// ============================================================
// ATDD RED PHASE — Story 11.1: 管道语法 (Pipe Syntax)
//
// Tests reference ParsePipeline, Pipeline, Command types
// which do NOT exist yet → compile failure = RED phase.
// ============================================================

// --- 11.1-UNIT-001: [P0] 单 spawn 命令解析 ---

func TestParsePipeline_SingleSpawn(t *testing.T) {
	pipeline, err := ParsePipeline(`spawn "分析代码"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pipeline.Commands) != 1 {
		t.Fatalf("commands count = %d, want 1", len(pipeline.Commands))
	}
	cmd := pipeline.Commands[0]
	if cmd.Type != "spawn" {
		t.Errorf("type = %q, want %q", cmd.Type, "spawn")
	}
	if cmd.Intent != "分析代码" {
		t.Errorf("intent = %q, want %q", cmd.Intent, "分析代码")
	}
	if cmd.Agent != "" {
		t.Errorf("agent = %q, want empty", cmd.Agent)
	}
	if cmd.Model != "" {
		t.Errorf("model = %q, want empty", cmd.Model)
	}
}

// --- 11.1-UNIT-002: [P0] 双管道解析 (AC1) ---

func TestParsePipeline_TwoStages(t *testing.T) {
	pipeline, err := ParsePipeline(`spawn "分析代码" | spawn "写文档"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pipeline.Commands) != 2 {
		t.Fatalf("commands count = %d, want 2", len(pipeline.Commands))
	}
	if pipeline.Commands[0].Intent != "分析代码" {
		t.Errorf("stage 0 intent = %q, want %q", pipeline.Commands[0].Intent, "分析代码")
	}
	if pipeline.Commands[1].Intent != "写文档" {
		t.Errorf("stage 1 intent = %q, want %q", pipeline.Commands[1].Intent, "写文档")
	}
}

// --- 11.1-UNIT-003: [P0] 三管道解析 (AC2) ---

func TestParsePipeline_ThreeStages(t *testing.T) {
	pipeline, err := ParsePipeline(`spawn "A" | spawn "B" | spawn "C"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pipeline.Commands) != 3 {
		t.Fatalf("commands count = %d, want 3", len(pipeline.Commands))
	}
	for i, want := range []string{"A", "B", "C"} {
		if pipeline.Commands[i].Intent != want {
			t.Errorf("stage %d intent = %q, want %q", i, pipeline.Commands[i].Intent, want)
		}
	}
}

// --- 11.1-UNIT-004: [P1] 带 --agent/--model 参数解析 ---

func TestParsePipeline_WithAgentAndModel(t *testing.T) {
	pipeline, err := ParsePipeline(`spawn "分析" --agent=analyst | spawn "写文档" --agent=writer --model=opus`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pipeline.Commands) != 2 {
		t.Fatalf("commands count = %d, want 2", len(pipeline.Commands))
	}

	cmd0 := pipeline.Commands[0]
	if cmd0.Intent != "分析" {
		t.Errorf("stage 0 intent = %q, want %q", cmd0.Intent, "分析")
	}
	if cmd0.Agent != "analyst" {
		t.Errorf("stage 0 agent = %q, want %q", cmd0.Agent, "analyst")
	}

	cmd1 := pipeline.Commands[1]
	if cmd1.Intent != "写文档" {
		t.Errorf("stage 1 intent = %q, want %q", cmd1.Intent, "写文档")
	}
	if cmd1.Agent != "writer" {
		t.Errorf("stage 1 agent = %q, want %q", cmd1.Agent, "writer")
	}
	if cmd1.Model != "opus" {
		t.Errorf("stage 1 model = %q, want %q", cmd1.Model, "opus")
	}
}

func TestParsePipeline_SingleQuotedIntent(t *testing.T) {
	pipeline, err := ParsePipeline(`spawn '分析 A|B 的差异'`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pipeline.Commands) != 1 {
		t.Fatalf("commands count = %d, want 1", len(pipeline.Commands))
	}
	if pipeline.Commands[0].Intent != "分析 A|B 的差异" {
		t.Errorf("intent = %q, want %q", pipeline.Commands[0].Intent, "分析 A|B 的差异")
	}
}

// --- 11.1-UNIT-005: [P0] 解析错误处理 (AC3 相关) ---

func TestParsePipeline_EmptyInput(t *testing.T) {
	_, err := ParsePipeline("")
	if err == nil {
		t.Fatal("expected error for empty input, got nil")
	}
}

func TestParsePipeline_NonSpawnCommand(t *testing.T) {
	_, err := ParsePipeline(`run "分析代码"`)
	if err == nil {
		t.Fatal("expected error for non-spawn command, got nil")
	}
}

func TestParsePipeline_UnclosedQuote(t *testing.T) {
	_, err := ParsePipeline(`spawn "分析代码`)
	if err == nil {
		t.Fatal("expected error for unclosed quote, got nil")
	}
}

func TestParsePipeline_EmptySegment(t *testing.T) {
	_, err := ParsePipeline(`spawn "A" | | spawn "B"`)
	if err == nil {
		t.Fatal("expected error for empty segment, got nil")
	}
}

func TestParsePipeline_MissingIntent(t *testing.T) {
	_, err := ParsePipeline(`spawn`)
	if err == nil {
		t.Fatal("expected error for spawn without intent, got nil")
	}
}

// --- 11.1-REG-002: [P2] 引号内含管道符不分割 ---

func TestParsePipeline_PipeInsideQuotes(t *testing.T) {
	pipeline, err := ParsePipeline(`spawn "分析 A|B"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pipeline.Commands) != 1 {
		t.Fatalf("commands count = %d, want 1 (pipe inside quotes should not split)", len(pipeline.Commands))
	}
	if pipeline.Commands[0].Intent != "分析 A|B" {
		t.Errorf("intent = %q, want %q", pipeline.Commands[0].Intent, "分析 A|B")
	}
}

// --- 11.1-UNIT-004b: [P1] spawn 关键字大小写不敏感 ---

func TestParsePipeline_CaseInsensitiveSpawn(t *testing.T) {
	pipeline, err := ParsePipeline(`Spawn "分析" | SPAWN "写文档"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pipeline.Commands) != 2 {
		t.Fatalf("commands count = %d, want 2", len(pipeline.Commands))
	}
}

func TestParsePipeline_ResultLastLine(t *testing.T) {
	pipeline, err := ParsePipeline(`spawn "分析" --agent=planner --result-last-line`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pipeline.Commands) != 1 {
		t.Fatalf("commands = %d, want 1", len(pipeline.Commands))
	}
	cmd := pipeline.Commands[0]
	if !cmd.ResultLastLine {
		t.Error("ResultLastLine = false, want true")
	}
	if cmd.Agent != "planner" {
		t.Errorf("agent = %q, want %q", cmd.Agent, "planner")
	}
	if cmd.Intent != "分析" {
		t.Errorf("intent = %q, want %q", cmd.Intent, "分析")
	}
}

func TestParsePipeline_ResultLastLine_WithoutFlag(t *testing.T) {
	pipeline, err := ParsePipeline(`spawn "分析" --agent=planner`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pipeline.Commands[0].ResultLastLine {
		t.Error("ResultLastLine = true, want false")
	}
}
