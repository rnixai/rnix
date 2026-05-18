package kernel

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/rnixai/rnix/internal/config"
	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// ATDD Epic 42 fix: openLLMDeviceForResume 优先走 ProjectConfig.LLMFileOpener
//
// Without this, resumed processes fall back to the global VFS driver and lose
// project-level API keys → DeepSeek 401 (the Dashboard `r` key bug).
// =============================================================================

// fakeResumeLLMFile is a minimal VFSFile mock just to verify which path opened.
type fakeResumeLLMFile struct{ name string }

func (f *fakeResumeLLMFile) Write(_ context.Context, _ []byte) error  { return nil }
func (f *fakeResumeLLMFile) Read(_ int) ([]byte, error)               { return nil, nil }
func (f *fakeResumeLLMFile) Close() error                              { return nil }
func (f *fakeResumeLLMFile) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{IsDevice: true, Name: f.name}, nil
}
func (f *fakeResumeLLMFile) SupportsToolCalling() bool { return true }

func TestATDD_42_Fix_OpenLLMForResume_PrefersProjectOpener(t *testing.T) {
	k := newThrottleTestKernel(t)

	var opener atomic.Uint32 // counts LLMFileOpener invocations
	projectOpener := func(_ string, _ int) (any, error) {
		opener.Add(1)
		return &fakeResumeLLMFile{name: "/dev/llm/deepseek"}, nil
	}

	proc := NewProcess(0, "fix test", nil)
	proc.ProjectConfig = &config.ProjectConfig{
		ProjectDir:    "/tmp/test-project",
		LLMFileOpener: projectOpener,
	}
	_ = proc.Start()
	k.AddProcess(proc)

	fd, err := k.openLLMDeviceForResume(proc, "/dev/llm/deepseek")
	if err != nil {
		t.Fatalf("openLLMDeviceForResume: %v", err)
	}
	if fd <= 0 {
		t.Errorf("FD = %d, want > 0", fd)
	}
	if opener.Load() != 1 {
		t.Errorf("ProjectConfig.LLMFileOpener calls = %d, want 1 (Epic 42 fix)", opener.Load())
	}
}

func TestATDD_42_Fix_OpenLLMForResume_FallsBackWhenNoOpener(t *testing.T) {
	k := newThrottleTestKernel(t)
	// Register a fake /dev/llm/claude device so the global VFS path works for
	// the fallback case (back-compat verification).
	devReg := k.vfs.DeviceRegistry()
	_ = devReg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &fakeResumeLLMFile{name: "/dev/llm/claude"}, nil
	})

	proc := NewProcess(0, "fix test fallback", nil)
	proc.ProjectConfig = nil // ← no opener
	_ = proc.Start()
	k.AddProcess(proc)

	fd, err := k.openLLMDeviceForResume(proc, "/dev/llm/claude")
	if err != nil {
		t.Fatalf("openLLMDeviceForResume fallback: %v", err)
	}
	if fd <= 0 {
		t.Errorf("FD = %d, want > 0", fd)
	}
}
