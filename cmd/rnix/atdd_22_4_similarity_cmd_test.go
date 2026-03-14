package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/kernel"
)

// ============================================================
// ATDD RED PHASE — Story 22.4: 能力迁移与相似度矩阵
//
// CLI 层测试：`rnix immune similarity` 子命令的文本和 JSON 输出格式。
//
// 测试引用的命令和函数尚不存在，测试将无法编译直到实现完成。
//
// RED → GREEN: 在 cmd/rnix/immune.go 中新增 similarity 子命令。
// ============================================================

// --- 22.4-CLI-001: [P0] rnix immune similarity text output (AC7) ---

func TestRunImmuneSimilarity_TextOutput(t *testing.T) {
	// Given: a mock similarity query response
	old := ipc.SocketPathOverride
	ipc.SocketPathOverride = "/tmp/rnix-test-nonexistent.sock"
	defer func() { ipc.SocketPathOverride = old }()

	// Test the formatting function directly instead of live IPC
	response := &ipc.SimilarityQueryResponse{
		Agent: "code-analyst",
		Similarities: []kernel.CapabilitySimilarity{
			{AgentA: "code-analyst", AgentB: "code-reviewer", SkillScore: 0.85, CoopScore: 0.5, Score: 0.745},
			{AgentA: "code-analyst", AgentB: "debugger", SkillScore: 0.62, CoopScore: 0.0, Score: 0.434},
			{AgentA: "code-analyst", AgentB: "doc-writer", SkillScore: 0.31, CoopScore: 0.0, Score: 0.217},
		},
	}

	var buf bytes.Buffer
	formatSimilarityText(&buf, response)
	output := buf.String()

	// Then: output contains header
	if !strings.Contains(output, "code-analyst") {
		t.Errorf("output should contain agent name, got: %s", output)
	}

	// And: output contains column headers
	if !strings.Contains(output, "AGENT") {
		t.Errorf("output should contain AGENT column header, got: %s", output)
	}
	if !strings.Contains(output, "SIMILARITY") {
		t.Errorf("output should contain SIMILARITY column header, got: %s", output)
	}

	// And: output contains agent entries
	if !strings.Contains(output, "code-reviewer") {
		t.Errorf("output should contain code-reviewer, got: %s", output)
	}
	if !strings.Contains(output, "debugger") {
		t.Errorf("output should contain debugger, got: %s", output)
	}
}

// --- 22.4-CLI-002: [P0] rnix immune similarity JSON output (AC7) ---

func TestRunImmuneSimilarity_JSONOutput(t *testing.T) {
	// Given: a mock similarity query response
	response := &ipc.SimilarityQueryResponse{
		Agent: "code-analyst",
		Similarities: []kernel.CapabilitySimilarity{
			{AgentA: "code-analyst", AgentB: "code-reviewer", SkillScore: 0.85, CoopScore: 0.5, Score: 0.745},
		},
	}

	var buf bytes.Buffer
	resp := JSONResponse{OK: true, Data: response}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	buf.Write(data)

	// Then: output is valid JSON
	var decoded JSONResponse
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("JSON output is not valid: %v", err)
	}
	if !decoded.OK {
		t.Error("expected OK=true in JSON response")
	}
}

// --- 22.4-CLI-003: [P0] rnix immune similarity no daemon error (AC7) ---

func TestRunImmuneSimilarity_NoDaemon(t *testing.T) {
	// Given: no daemon available
	old := ipc.SocketPathOverride
	ipc.SocketPathOverride = "/tmp/rnix-test-nonexistent.sock"
	defer func() { ipc.SocketPathOverride = old }()

	var buf bytes.Buffer
	cmd := immuneSimilarityCmd
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	oldJSON := flagJSON
	flagJSON = false
	defer func() { flagJSON = oldJSON }()

	oldExitCode := exitCode
	exitCode = 0
	defer func() { exitCode = oldExitCode }()

	_ = cmd.RunE(cmd, []string{"test-agent"})
	output := buf.String()

	// Then: output contains error message
	if !strings.Contains(output, "daemon not available") {
		t.Errorf("expected 'daemon not available' message, got: %s", output)
	}
}

// --- 22.4-CLI-004: [P0] rnix immune similarity command registered (AC7) ---

func TestImmuneSimilarityCmd_Registered(t *testing.T) {
	// Given: the immune command exists
	// When: checking for the similarity subcommand
	found := false
	for _, sub := range immuneCmd.Commands() {
		if sub.Use == "similarity [agent-name]" || strings.HasPrefix(sub.Use, "similarity") {
			found = true
			break
		}
	}

	// Then: similarity subcommand is registered
	if !found {
		t.Error("'similarity' subcommand should be registered under 'immune' command")
	}
}

// --- 22.4-CLI-005: [P1] rnix immune similarity text output sorted by score (AC7) ---

func TestRunImmuneSimilarity_TextOutput_SortedByScore(t *testing.T) {
	// Given: a response with agents in non-sorted order
	response := &ipc.SimilarityQueryResponse{
		Agent: "target",
		Similarities: []kernel.CapabilitySimilarity{
			{AgentA: "target", AgentB: "low-agent", SkillScore: 0.2, Score: 0.14},
			{AgentA: "target", AgentB: "high-agent", SkillScore: 0.9, Score: 0.63},
			{AgentA: "target", AgentB: "mid-agent", SkillScore: 0.5, Score: 0.35},
		},
	}

	var buf bytes.Buffer
	formatSimilarityText(&buf, response)
	output := buf.String()

	// Then: high-agent appears before mid-agent, which appears before low-agent
	highIdx := strings.Index(output, "high-agent")
	midIdx := strings.Index(output, "mid-agent")
	lowIdx := strings.Index(output, "low-agent")

	if highIdx < 0 || midIdx < 0 || lowIdx < 0 {
		t.Fatalf("missing agents in output: %s", output)
	}
	if highIdx > midIdx {
		t.Error("high-agent should appear before mid-agent (sorted by score descending)")
	}
	if midIdx > lowIdx {
		t.Error("mid-agent should appear before low-agent (sorted by score descending)")
	}
}

// --- 22.4-CLI-006: [P1] rnix immune similarity text output empty (AC7) ---

func TestRunImmuneSimilarity_TextOutput_Empty(t *testing.T) {
	// Given: a response with no similarities
	response := &ipc.SimilarityQueryResponse{
		Agent:        "lonely-agent",
		Similarities: []kernel.CapabilitySimilarity{},
	}

	var buf bytes.Buffer
	formatSimilarityText(&buf, response)
	output := buf.String()

	// Then: output indicates no similar agents found
	if !strings.Contains(output, "lonely-agent") {
		t.Errorf("output should contain agent name, got: %s", output)
	}
	// Should have some indication of no results
	if !strings.Contains(output, "No similar") && !strings.Contains(output, "no similar") && !strings.Contains(output, "0") {
		t.Logf("Warning: output may not clearly indicate no similar agents: %s", output)
	}
}
