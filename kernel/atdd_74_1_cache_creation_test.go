package kernel

// ATDD for Story 74.1 — kernel 层贯通（AC2）。
//
//   - llmResponse JSON round-trip：构造含 cache_creation_input_tokens 的 JSON →
//     解码 → 字段正确（仿 66.6 wire/action 测试形态；json tag MUST match 纪律）。
//   - writeStepRecord 透传：mock LLM 响应带 creation → steps.jsonl 中
//     StepRecord.CacheCreationInputTokens 正确（走真实 spawn 路径）。
//   - 旧数据兼容：无该字段的 JSON 解码 → 0（omitempty 天然保证，断言钉住）。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/config"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// -----------------------------------------------------------------------------
// 74-1-KRN-001 (AC2-1): llmResponse JSON round-trip — cache_creation_input_tokens
// 经 json tag 自动解码（kernel 侧无手工字段映射，tag 必须与 drivers/llm.LLMResponse
// 逐字一致——66.6 MUST match 纪律）。
// -----------------------------------------------------------------------------
func TestATDD_74_1_KRN_001_llmResponseRoundTrip(t *testing.T) {
	data := []byte(`{
		"content": "ok",
		"tokens_used": 150,
		"input_tokens": 100,
		"output_tokens": 50,
		"cached_input_tokens": 80,
		"cache_creation_input_tokens": 10
	}`)
	var resp llmResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.CacheCreationInputTokens != 10 {
		t.Errorf("CacheCreationInputTokens = %d, want 10", resp.CacheCreationInputTokens)
	}
	if resp.CachedInputTokens != 80 {
		t.Errorf("CachedInputTokens = %d, want 80", resp.CachedInputTokens)
	}
	if resp.InputTokens != 100 || resp.OutputTokens != 50 {
		t.Errorf("unexpected input/output: %d/%d", resp.InputTokens, resp.OutputTokens)
	}
}

// -----------------------------------------------------------------------------
// 74-1-KRN-002 (AC2-3): 旧数据兼容 — 无 cache_creation_input_tokens 字段的 JSON
// 解码 → 0（omitempty 天然保证）。
// -----------------------------------------------------------------------------
func TestATDD_74_1_KRN_002_OldJSONDecodesZero(t *testing.T) {
	data := []byte(`{
		"content": "ok",
		"tokens_used": 150,
		"input_tokens": 100,
		"output_tokens": 50,
		"cached_input_tokens": 80
	}`)
	var resp llmResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.CacheCreationInputTokens != 0 {
		t.Errorf("CacheCreationInputTokens = %d, want 0 (旧数据无字段)", resp.CacheCreationInputTokens)
	}
	if resp.CachedInputTokens != 80 {
		t.Errorf("CachedInputTokens = %d, want 80 (既有字段不受影响)", resp.CachedInputTokens)
	}
}

// makeCacheCreationCompleteResponse builds a complete-action LLM response JSON
// carrying cache_creation_input_tokens (Story 74.1). The Complete ToolCall is
// what the kernel's reasonStep uses to terminate the process (exit 0).
func makeCacheCreationCompleteResponse(result string, tokens, creation int) []byte {
	resp := llmResponse{
		Content:                  result,
		TokensUsed:               tokens,
		InputTokens:              100,
		OutputTokens:             tokens - 100,
		CachedInputTokens:        80,
		CacheCreationInputTokens: creation,
		ToolCalls: []llmToolCall{{
			ID:    "call_complete",
			Name:  "Complete",
			Input: map[string]any{"result": result},
		}},
	}
	data, _ := json.Marshal(resp)
	return data
}

// -----------------------------------------------------------------------------
// 74-1-KRN-003 (AC2-2): writeStepRecord 透传 — mock LLM 响应带 creation →
// steps.jsonl 中 StepRecord.CacheCreationInputTokens 正确。走真实 spawn 路径
// （sequenceLLMFile），与既有 27.1 集成测试同形态。
// -----------------------------------------------------------------------------
func TestATDD_74_1_KRN_003_WriteStepRecordPassthrough(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeCacheCreationCompleteResponse("cache creation passthrough", 150, 10),
		},
	}
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return seqFile, nil
	})

	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	_, projBaseDir := TestSetupDataDir(t, k)

	pid, err := k.Spawn("cache creation test", nil, SpawnOpts{
		ProjectConfig: &config.ProjectConfig{ProjectDir: testProjectDir},
	})
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

	stepsFile := filepath.Join(projBaseDir, "steps", proc.UUID, "steps.jsonl")
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
	if rec.CacheCreationInputTokens != 10 {
		t.Errorf("StepRecord.CacheCreationInputTokens = %d, want 10 (writeStepRecord 透传)", rec.CacheCreationInputTokens)
	}
	if rec.CachedInputTokens != 80 {
		t.Errorf("StepRecord.CachedInputTokens = %d, want 80", rec.CachedInputTokens)
	}
	if rec.InputTokens != 100 || rec.OutputTokens != 50 {
		t.Errorf("unexpected input/output: %d/%d", rec.InputTokens, rec.OutputTokens)
	}
}
