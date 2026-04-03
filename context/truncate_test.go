package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTruncateResult_NoTruncation(t *testing.T) {
	content := "Hello, World!"
	result, truncated := TruncateResult(content, 100)
	if truncated {
		t.Error("expected no truncation")
	}
	if result != content {
		t.Errorf("result = %q, want %q", result, content)
	}
}

func TestTruncateResult_ZeroMax(t *testing.T) {
	content := "Hello"
	result, truncated := TruncateResult(content, 0)
	if truncated {
		t.Error("expected no truncation with maxTokens=0")
	}
	if result != content {
		t.Errorf("result = %q, want %q", result, content)
	}
}

func TestTruncateResult_Empty(t *testing.T) {
	result, truncated := TruncateResult("", 100)
	if truncated {
		t.Error("expected no truncation for empty content")
	}
	if result != "" {
		t.Errorf("result = %q, want empty", result)
	}
}

func TestTruncateResult_ExactLimit(t *testing.T) {
	// 3500 ASCII chars = 1000 tokens
	content := strings.Repeat("a", 3500)
	result, truncated := TruncateResult(content, 1000)
	if truncated {
		t.Error("expected no truncation at exact limit")
	}
	if result != content {
		t.Errorf("result length = %d, want %d", len(result), len(content))
	}
}

func TestTruncateResult_Truncation(t *testing.T) {
	// 7000 ASCII chars = 2000 tokens, limit to 500
	content := strings.Repeat("a", 7000)
	result, truncated := TruncateResult(content, 500)
	if !truncated {
		t.Error("expected truncation")
	}
	resultTokens := EstimateTokens(result)
	if resultTokens > 500 {
		t.Errorf("result tokens = %d, should be <= 500", resultTokens)
	}
	if resultTokens < 400 {
		t.Errorf("result tokens = %d, should be >= 400 (reasonably close to limit)", resultTokens)
	}
}

func TestEndTruncatingAccumulator_NoTruncation(t *testing.T) {
	content := "line1\nline2\nline3"
	result, truncated := EndTruncatingAccumulator(content, 1000, 200, 200)
	if truncated {
		t.Error("expected no truncation")
	}
	if result != content {
		t.Errorf("result = %q, want %q", result, content)
	}
}

func TestEndTruncatingAccumulator_Truncation(t *testing.T) {
	// Generate 500 lines, each ~100 chars → ~50000 chars total
	var lines []string
	for i := range 500 {
		lines = append(lines, strings.Repeat("x", 99))
		_ = i
	}
	content := strings.Join(lines, "\n")

	result, truncated := EndTruncatingAccumulator(content, 30000, 200, 200)
	if !truncated {
		t.Error("expected truncation")
	}

	resultLines := strings.Split(result, "\n")
	// Should have 200 head + 1 notice line (may span multiple lines) + 200 tail
	if len(resultLines) < 400 {
		t.Errorf("result lines = %d, expected at least 400", len(resultLines))
	}

	if !strings.Contains(result, "lines truncated") {
		t.Error("expected truncation notice in result")
	}
}

func TestEndTruncatingAccumulator_ShortMaxChars(t *testing.T) {
	content := "short"
	result, truncated := EndTruncatingAccumulator(content, 30000, 200, 200)
	if truncated {
		t.Error("expected no truncation for short content")
	}
	if result != content {
		t.Errorf("result = %q, want %q", result, content)
	}
}

func TestWriteOverflow(t *testing.T) {
	tmpDir := t.TempDir()

	content := "overflow content here"
	path, err := WriteOverflow(content, tmpDir)
	if err != nil {
		t.Fatalf("WriteOverflow: %v", err)
	}

	// Verify file exists
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != content {
		t.Errorf("file content = %q, want %q", string(data), content)
	}

	// Verify path structure
	expectedDir := filepath.Join(tmpDir, ".rnix", "data", "overflow")
	if dir := filepath.Dir(path); dir != expectedDir {
		t.Errorf("overflow dir = %q, want %q", dir, expectedDir)
	}
}

func TestWriteOverflow_Dedup(t *testing.T) {
	tmpDir := t.TempDir()

	content := "same content"
	path1, err := WriteOverflow(content, tmpDir)
	if err != nil {
		t.Fatalf("WriteOverflow 1: %v", err)
	}
	path2, err := WriteOverflow(content, tmpDir)
	if err != nil {
		t.Fatalf("WriteOverflow 2: %v", err)
	}

	if path1 != path2 {
		t.Errorf("expected same path for same content: %s != %s", path1, path2)
	}
}

func TestFormatTruncationNotice(t *testing.T) {
	notice := FormatTruncationNotice(50000, 25000, "/overflow/abc123")
	if !strings.Contains(notice, "50000") || !strings.Contains(notice, "25000") || !strings.Contains(notice, "/overflow/abc123") {
		t.Errorf("unexpected notice format: %s", notice)
	}

	notice2 := FormatTruncationNotice(50000, 25000, "")
	if strings.Contains(notice2, "saved to") {
		t.Errorf("notice without overflow path should not mention 'saved to': %s", notice2)
	}
}
