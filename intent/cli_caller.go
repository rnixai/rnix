package intent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
)

// CLICaller implements LLMCaller by invoking the Claude Code CLI.
type CLICaller struct{}

type claudeJSONResponse struct {
	Type   string `json:"type"`
	Result string `json:"result"`
}

// Call invokes `claude -p <prompt> --output-format json [--model <model>]`
// and extracts the result text from the JSON response.
func (c *CLICaller) Call(ctx context.Context, prompt string, model string) (string, error) {
	args := []string{"-p", prompt, "--output-format", "json"}
	if model != "" {
		args = append(args, "--model", model)
	}

	cmd := exec.CommandContext(ctx, "claude", args...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("claude CLI: %w", err)
	}

	return extractResult(out)
}

func extractResult(data []byte) (string, error) {
	var resp claudeJSONResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		log.Printf("[intent] claude CLI response is not JSON envelope, using raw output (%d bytes)", len(data))
		return string(data), nil
	}
	if resp.Result != "" {
		return resp.Result, nil
	}
	return string(data), nil
}
