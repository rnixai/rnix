package kernel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// makeParallelToolCallsResponse 构造一个含 N 个 parallel tool calls 的 LLM 响应。
func makeParallelToolCallsResponse(toolName string, inputs []map[string]any, tokens int) []byte {
	calls := make([]llmToolCall, len(inputs))
	for i, in := range inputs {
		calls[i] = llmToolCall{
			ID:    "call_" + toolName + "_" + string(rune('a'+i)),
			Name:  toolName,
			Input: in,
		}
	}
	resp := llmResponse{
		TokensUsed:   tokens,
		InputTokens:  tokens * 9 / 10,
		OutputTokens: tokens / 10,
		ToolCalls:    calls,
	}
	data, _ := json.Marshal(resp)
	return data
}

// TestATDD_ParallelToolCallsSingleRecord (spec-step-inspector-data-fidelity 缺陷 1)
//
// 期望：LLM 单次响应内含 N 个 parallel tool calls 时,kernel 应当只写 1 行 StepRecord,
// 其 ToolCalls 数组长度等于 N,每个元素含 name/path/input/result。
//
// 修复前：toolLoop 内每个 call 都调 writeStepRecord,产生 N 行 step=K 记录;ReadStep
// 去重保留最后一条 → ToolIO lens 只能看到末尾 1 个工具。
func TestATDD_ParallelToolCallsSingleRecord(t *testing.T) {
	baseDir := t.TempDir()

	reg := vfs.NewDeviceRegistry()
	// step 1: LLM 返回 3 个并行 tool_call;step 2: complete。
	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeParallelToolCallsResponse("/dev/tools/echo",
				[]map[string]any{
					{"msg": "alpha"},
					{"msg": "beta"},
					{"msg": "gamma"},
				}, 100),
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

	pid, err := k.Spawn("parallel echo test", nil, SpawnOpts{})
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

	// 统计 step==1 的行数,期望 1 行（修复前会是 3 行）。
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var step1Records []types.StepRecord
	for _, ln := range lines {
		var rec types.StepRecord
		if json.Unmarshal([]byte(ln), &rec) == nil && rec.Step == 1 {
			step1Records = append(step1Records, rec)
		}
	}
	if len(step1Records) != 1 {
		t.Fatalf("expected exactly 1 record for step 1, got %d (parallel tool calls should produce a single consolidated record per spec缺陷 1)", len(step1Records))
	}

	rec := step1Records[0]
	if len(rec.ToolCalls) != 3 {
		t.Fatalf("expected ToolCalls array length 3, got %d", len(rec.ToolCalls))
	}

	// 每个 ToolCallRecord 都应有 Path + Input + Result。
	for i, c := range rec.ToolCalls {
		if c.Path == "" {
			t.Errorf("ToolCalls[%d].Path empty", i)
		}
		if c.Input == "" {
			t.Errorf("ToolCalls[%d].Input empty", i)
		}
		if c.Result == "" && c.Error == "" {
			t.Errorf("ToolCalls[%d] has neither Result nor Error", i)
		}
	}

	// token 拆分字段应当被透传（spec缺陷 3）。
	if rec.InputTokens == 0 {
		t.Errorf("expected InputTokens > 0; got 0 (spec缺陷 3: token split should propagate from llmResponse)")
	}
	if rec.OutputTokens == 0 {
		t.Errorf("expected OutputTokens > 0; got 0")
	}
}

// TestATDD_ParallelToolCallsBackwardCompat (spec-step-inspector-data-fidelity)
//
// 期望：单个 tool call 路径下旧字段 (ToolPath/ToolInput/ToolResult/ToolDuration) 仍被填充,
// 兼容旧 dashboard 客户端读新 daemon。
func TestATDD_ParallelToolCallsBackwardCompat(t *testing.T) {
	baseDir := t.TempDir()

	reg := vfs.NewDeviceRegistry()
	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeToolCallResponse("/dev/tools/echo", map[string]any{"msg": "solo"}, 50),
			makeCompleteResponse("done", 20),
		},
	}
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return seqFile, nil
	})
	registerMockTool(reg, "/dev/tools/echo", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockToolFile{readData: []byte("solo-result")}, nil
	})

	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()
	k.SetStepDataDir(baseDir)

	pid, err := k.Spawn("solo test", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	stepsFile := filepath.Join(baseDir, "data", "steps", proc.UUID, "steps.jsonl")
	data, _ := os.ReadFile(stepsFile)
	for ln := range strings.SplitSeq(strings.TrimSpace(string(data)), "\n") {
		var rec types.StepRecord
		if json.Unmarshal([]byte(ln), &rec) != nil || rec.Step != 1 {
			continue
		}
		if len(rec.ToolCalls) != 1 {
			t.Fatalf("expected 1 ToolCall in single-call path, got %d", len(rec.ToolCalls))
		}
		// 旧字段必须从末元素回填以兼容旧 reader。
		if rec.ToolPath == "" {
			t.Error("ToolPath should be backfilled from ToolCalls[last] for backward compat")
		}
		if rec.ToolInput == "" {
			t.Error("ToolInput should be backfilled from ToolCalls[last]")
		}
		if rec.ToolResult == "" {
			t.Error("ToolResult should be backfilled from ToolCalls[last]")
		}
		return
	}
	t.Fatal("no step 1 record found")
}
