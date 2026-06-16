package kernel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/rnixai/rnix/vfs"
)

// ============================================================================
// ATDD Story 56.4 review patch — kernel 读层 malformed 计数 (decision 1→a)
//
// ReadRawForStepWithErrors 在单步查询路径也返回全文件 malformed 行计数，使
// dashboard lens / strace --raw --step N 也能暴露 "N lines skipped"。修复前
// 单步路径走 ReadRawForStep（丢弃计数）→ ParseErrors 恒 0。
// ============================================================================

// writeRawLines 写一个含 good/malformed 混合行的 raw.jsonl，返回路径。
func writeRawLines(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "raw.jsonl")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write raw.jsonl: %v", err)
	}
	return path
}

func rawLine(t *testing.T, step int, kind string) string {
	t.Helper()
	rw := vfs.RawCapture{Step: step, Kind: kind, Request: map[string]any{"url": "x"}}
	b, err := json.Marshal(rw)
	if err != nil {
		t.Fatalf("marshal RawCapture: %v", err)
	}
	return string(b)
}

func TestATDD_56_4_ReadRawForStepWithErrors_CountsMalformed(t *testing.T) {
	// step 1 good + malformed + step 2 good
	content := rawLine(t, 1, "api") + "\n" + "{ not valid json\n" + rawLine(t, 2, "cli") + "\n"
	path := writeRawLines(t, content)

	rec, parseErrors, err := ReadRawForStepWithErrors(path, 2)
	if err != nil {
		t.Fatalf("ReadRawForStepWithErrors: %v", err)
	}
	if rec == nil || rec.Step != 2 {
		t.Fatalf("expected step-2 record, got %+v", rec)
	}
	if parseErrors != 1 {
		t.Errorf("decision 1→a: single-step read must report full-file ParseErrors=1, got %d", parseErrors)
	}
}

func TestATDD_56_4_ReadRawForStepWithErrors_StepNotFound_StillCounts(t *testing.T) {
	// 不存在的 step 仍须返回 malformed 计数（nil record + count）。
	content := rawLine(t, 1, "api") + "\n" + "garbage line\n"
	path := writeRawLines(t, content)

	rec, parseErrors, err := ReadRawForStepWithErrors(path, 99)
	if err != nil {
		t.Fatalf("ReadRawForStepWithErrors: %v", err)
	}
	if rec != nil {
		t.Errorf("expected nil for absent step 99, got %+v", rec)
	}
	if parseErrors != 1 {
		t.Errorf("expected ParseErrors=1 even when step absent, got %d", parseErrors)
	}
}

func TestATDD_56_4_ReadRawForStep_ThinWrapper_Unchanged(t *testing.T) {
	// 薄封装 ReadRawForStep 仍返回正确记录（2-值签名零破坏）。
	content := rawLine(t, 1, "api") + "\n" + rawLine(t, 2, "cli") + "\n"
	path := writeRawLines(t, content)

	rec, err := ReadRawForStep(path, 1)
	if err != nil {
		t.Fatalf("ReadRawForStep: %v", err)
	}
	if rec == nil || rec.Step != 1 || rec.Kind != "api" {
		t.Errorf("ReadRawForStep(1) = %+v, want step=1 kind=api", rec)
	}
}
