package intentdriver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/rnixai/rnix/intent"
	"github.com/rnixai/rnix/vfs"
)

var _ intent.LLMCaller = (*VFSCaller)(nil)

type llmRequest struct {
	Intent string `json:"intent"`
	Model  string `json:"model,omitempty"`
}

type llmResponse struct {
	Content string `json:"content"`
}

// VFSCaller implements intent.LLMCaller by routing calls through VFS /dev/llm/* devices.
type VFSCaller struct {
	devReg   *vfs.DeviceRegistry
	provider string
}

func NewVFSCaller(devReg *vfs.DeviceRegistry, provider string) *VFSCaller {
	return &VFSCaller{devReg: devReg, provider: provider}
}

func (c *VFSCaller) Call(ctx context.Context, prompt string, model string) (string, error) {
	devicePath := "/dev/llm/" + c.provider

	file, err := c.devReg.Open(devicePath, vfs.O_RDWR, "")
	if err != nil {
		return "", fmt.Errorf("VFSCaller: provider %q via %s: %w", c.provider, devicePath, err)
	}
	defer file.Close()

	req := llmRequest{Intent: prompt, Model: model}
	reqJSON, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("VFSCaller: provider %q via %s: marshal request: %w", c.provider, devicePath, err)
	}

	if err := file.Write(ctx, reqJSON); err != nil {
		return "", fmt.Errorf("VFSCaller: provider %q via %s: %w", c.provider, devicePath, err)
	}

	respData, err := file.Read(1 << 20)
	if err != nil {
		return "", fmt.Errorf("VFSCaller: provider %q via %s: read response: %w", c.provider, devicePath, err)
	}

	var resp llmResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return "", fmt.Errorf("VFSCaller: provider %q via %s: invalid response JSON: %w", c.provider, devicePath, err)
	}

	return resp.Content, nil
}
