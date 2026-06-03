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

type llmOpenerKey struct{}

// WithLLMOpener attaches an LLM file opener to the context, allowing VFSCaller
// to open project-level LLM providers not in the global device registry.
func WithLLMOpener(ctx context.Context, opener func(provider string) (vfs.VFSFile, error)) context.Context {
	return context.WithValue(ctx, llmOpenerKey{}, opener)
}

// VFSCaller implements intent.LLMCaller by routing calls through VFS /dev/llm/* devices.
type VFSCaller struct {
	devReg   *vfs.DeviceRegistry
	provider string
}

func NewVFSCaller(devReg *vfs.DeviceRegistry, provider string) *VFSCaller {
	return &VFSCaller{devReg: devReg, provider: provider}
}

func (c *VFSCaller) Call(ctx context.Context, prompt string, model string, provider string) (string, error) {
	p := c.provider
	if provider != "" {
		p = provider
	}
	devicePath := "/dev/llm/" + p

	var file vfs.VFSFile
	var err error
	if opener, ok := ctx.Value(llmOpenerKey{}).(func(string) (vfs.VFSFile, error)); ok {
		file, err = opener(p)
	}
	if file == nil {
		file, err = c.devReg.Open(devicePath, vfs.O_RDWR, "")
	}
	if err != nil {
		return "", fmt.Errorf("VFSCaller: provider %q via %s: %w", p, devicePath, err)
	}
	defer file.Close()

	req := llmRequest{Intent: prompt, Model: model}
	reqJSON, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("VFSCaller: provider %q via %s: marshal request: %w", p, devicePath, err)
	}

	if err := file.Write(ctx, reqJSON); err != nil {
		return "", fmt.Errorf("VFSCaller: provider %q via %s: %w", p, devicePath, err)
	}

	respData, err := file.Read(1 << 20)
	if err != nil {
		return "", fmt.Errorf("VFSCaller: provider %q via %s: read response: %w", p, devicePath, err)
	}
	if len(respData) == 0 {
		return "", fmt.Errorf("VFSCaller: provider %q via %s: empty response", p, devicePath)
	}

	var resp llmResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return "", fmt.Errorf("VFSCaller: provider %q via %s: invalid response JSON: %w", p, devicePath, err)
	}

	return resp.Content, nil
}
