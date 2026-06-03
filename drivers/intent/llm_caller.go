package intentdriver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/rnixai/rnix/intent"
	"github.com/rnixai/rnix/internal/types"
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

type projectConfigKey struct{}

// WithProjectConfig attaches a ProjectConfig to the context so downstream
// consumers (e.g., intent reconciler spawn) can inherit project-level settings.
func WithProjectConfig(ctx context.Context, cfg any) context.Context {
	return context.WithValue(ctx, projectConfigKey{}, cfg)
}

// ProjectConfigFromContext extracts the ProjectConfig from the context.
func ProjectConfigFromContext(ctx context.Context) any {
	return ctx.Value(projectConfigKey{})
}

type callerProcessInfoKey struct{}

// CallerProcessInfo carries the calling process's PID and depth through context
// so that intent reconciler SpawnFunc can set ParentPID and Depth on child processes.
type CallerProcessInfo struct {
	PID   types.PID
	Depth int
}

// WithCallerProcessInfo attaches caller process info to the context.
func WithCallerProcessInfo(ctx context.Context, info CallerProcessInfo) context.Context {
	return context.WithValue(ctx, callerProcessInfoKey{}, info)
}

// CallerProcessInfoFromContext extracts the caller process info from the context.
func CallerProcessInfoFromContext(ctx context.Context) (CallerProcessInfo, bool) {
	info, ok := ctx.Value(callerProcessInfoKey{}).(CallerProcessInfo)
	return info, ok
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
