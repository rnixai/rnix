package kernel

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// writeTestStepsAndMeta writes minimal steps.jsonl + process-meta.json + proc-info.json
// to simulate a completed/dead process on disk for resume testing.
func writeTestStepsAndMeta(t *testing.T, baseDir, uuid string, lastStep int, exitReason string) {
	t.Helper()
	stepsDir := filepath.Join(baseDir, "data", "steps", uuid)
	if err := os.MkdirAll(stepsDir, 0o755); err != nil {
		t.Fatalf("mkdir steps dir: %v", err)
	}

	// steps.jsonl: one record per step
	var stepsContent []byte
	for i := 1; i <= lastStep; i++ {
		record := map[string]any{
			"step":   i,
			"action": "tool_call",
			"request": map[string]any{
				"messages": []map[string]string{
					{"role": "user", "content": fmt.Sprintf("step %d input", i)},
				},
			},
			"response": map[string]any{
				"content": fmt.Sprintf("step %d output", i),
			},
		}
		line, _ := json.Marshal(record)
		stepsContent = append(stepsContent, line...)
		stepsContent = append(stepsContent, '\n')
	}
	if err := os.WriteFile(filepath.Join(stepsDir, "steps.jsonl"), stepsContent, 0o600); err != nil {
		t.Fatalf("write steps.jsonl: %v", err)
	}

	// process-meta.json
	meta := map[string]any{
		"system_prompt": "You are a test agent for resume",
		"tool_defs":     []any{},
	}
	metaData, _ := json.Marshal(meta)
	if err := os.WriteFile(filepath.Join(stepsDir, "process-meta.json"), metaData, 0o600); err != nil {
		t.Fatalf("write process-meta.json: %v", err)
	}

	// proc-info.json
	procInfo := map[string]any{
		"pid":             99,
		"uuid":            uuid,
		"state":           "dead",
		"intent":          "test resume intent",
		"provider":        "claude",
		"model":           "claude-4",
		"skills":          []string{"code-analyst"},
		"allowed_devices": []string{"/dev/fs", "/dev/shell"},
		"exit_reason":     exitReason,
		"last_step":       lastStep,
		"tokens_used":     3000,
	}
	infoData, _ := json.Marshal(procInfo)
	if err := os.WriteFile(filepath.Join(stepsDir, "proc-info.json"), infoData, 0o600); err != nil {
		t.Fatalf("write proc-info.json: %v", err)
	}
}
