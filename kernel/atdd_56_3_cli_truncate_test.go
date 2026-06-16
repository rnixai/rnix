package kernel

// ATDD coverage for Story 56.3 — CLI 形态在 kernel hook 路径的截断与脱敏纵深防御。
//
// 56.1 hook + truncateRawCapture 的 API 形态对 56.3 是硬约束（Kind="api"
// 与 Kind="cli" 走完全相同的落盘路径）。本 test 用 fakeRawLLMFile 注入
// CLI 形态 capture（Kind="cli", Response.stdout 超大），断言 kernel hook
// 截断 stdout 字段 + 设置 Truncated/OriginalBytes，与 56.1 INT-004 等价但
// 字段命名走 CLI 约定（stdout/stderr/exit_code，裁决 3）。

import (
	"path/filepath"
	"testing"

	"github.com/rnixai/rnix/vfs"
)

// 56-3-INT-K1 — CLI 形态超 MaxOutputBytes → kernel 自动截断 stdout 字段
//
// 等价于 56.1 INT-004 的 CLI 对偶版：API 路径用 Response.body，CLI 路径用
// Response.stdout（裁决 3 字段）。验证 kernel 的 truncateRawCapture +
// largestStringKey 选择驱逐最大 string 字段，对 CLI 命名同样生效（不依赖
// 字段名特定为 "body"）。
func TestATDD_56_3_INTK1_CaptureHook_CLI_TruncatesStdoutField(t *testing.T) {
	const oversize = 1024
	bigStdout := make([]byte, oversize)
	for i := range bigStdout {
		bigStdout[i] = 'X'
	}
	rec := &vfs.RawCapture{
		TsMs: 1,
		Step: 1,
		Kind: "cli",
		Request: map[string]any{
			"argv":  []string{"/usr/local/bin/claude", "--print", "-", "--effort", "high"},
			"stdin": "audit prompt",
		},
		Response: map[string]any{
			"stdout":    string(bigStdout),
			"stderr":    "",
			"exit_code": 0,
		},
	}
	llm := &fakeRawLLMFile{
		mockLLMFile: mockLLMFile{readData: makeLLMResponse("done", 5)},
		capture:     rec,
	}
	cfg := RawCaptureConfig{Enabled: true, MaxOutputBytes: 16}
	_, proc, baseDir := newRawCaptureTestKernel(t, llm, cfg)

	rawPath := filepath.Join(baseDir, "steps", proc.UUID, "raw.jsonl")
	records, err := ReadAllRaw(rawPath)
	if err != nil {
		t.Fatalf("ReadAllRaw: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(records))
	}
	got := records[0]
	if got.Kind != "cli" {
		t.Errorf("Kind=%q want cli", got.Kind)
	}
	if !got.Truncated {
		t.Errorf("Truncated = false, want true (stdout=%d > MaxOutputBytes=16)", oversize)
	}
	if got.OriginalBytes < int64(oversize) {
		t.Errorf("OriginalBytes = %d, want >= %d", got.OriginalBytes, oversize)
	}
}
