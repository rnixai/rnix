package kernel

import (
	gocontext "context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rnixai/rnix/agents"
	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

func TestATDD_62_6_AgentMessageContentMapsToLogOutput(t *testing.T) {
	cat, content, toolPath, ok := driverEventToLog(map[string]any{
		"type":    "content",
		"subtype": "agent_message",
		"content": "worker found the issue",
	})
	if !ok {
		t.Fatal("agent_message content should be visible in LogChan")
	}
	if cat != types.LogOutput {
		t.Fatalf("category = %q, want %q", cat, types.LogOutput)
	}
	if content != "worker found the issue" {
		t.Fatalf("content = %q, want original message", content)
	}
	if toolPath != "" {
		t.Fatalf("toolPath = %q, want empty", toolPath)
	}
}

func TestATDD_62_6_AgentMessageLogIsTruncatedTo80Runes(t *testing.T) {
	long := strings.Repeat("界", 90)
	_, content, _, ok := driverEventToLog(map[string]any{
		"type":    "content",
		"subtype": "agent_message",
		"content": long,
	})
	if !ok {
		t.Fatal("agent_message content should be visible in LogChan")
	}
	if got := len([]rune(strings.TrimSuffix(content, "..."))); got != 80 {
		t.Fatalf("truncated rune count = %d, want 80; content=%q", got, content)
	}
	if !strings.HasSuffix(content, "...") {
		t.Fatalf("truncated content should end with ellipsis, got %q", content)
	}
}

func TestATDD_62_6_TokenDeltaContentDoesNotMapToLog(t *testing.T) {
	if _, _, _, ok := driverEventToLog(map[string]any{
		"type":    "content",
		"content": "single token delta",
	}); ok {
		t.Fatal("token-level content without subtype=agent_message must not enter LogChan")
	}
}

type atdd62AgentMessageLLMFile struct {
	mu      sync.Mutex
	handler func(event map[string]any)
	read    []byte
}

func (f *atdd62AgentMessageLLMFile) Write(_ gocontext.Context, _ []byte) error {
	f.mu.Lock()
	h := f.handler
	f.mu.Unlock()
	if h != nil {
		h(map[string]any{
			"type":    "content",
			"subtype": "agent_message",
			"content": "intermediate worker output",
		})
	}
	return nil
}

func (f *atdd62AgentMessageLLMFile) Read(_ int) ([]byte, error) {
	return f.read, nil
}

func (f *atdd62AgentMessageLLMFile) Close() error { return nil }

func (f *atdd62AgentMessageLLMFile) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{IsDevice: true, Name: "/dev/llm/claude"}, nil
}

func (f *atdd62AgentMessageLLMFile) SupportsToolCalling() bool { return true }

func (f *atdd62AgentMessageLLMFile) SetStreamHandler(fn func(event map[string]any)) {
	f.mu.Lock()
	f.handler = fn
	f.mu.Unlock()
}

func TestATDD_62_6_AgentMessageStreamEventReachesProcessLogChan(t *testing.T) {
	llm := &atdd62AgentMessageLLMFile{read: makeLLMResponse("done", 1)}
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return llm, nil
	})
	k := NewKernel(vfs.NewVFS(reg), rnixctx.NewManager(), nil)
	defer k.Shutdown()

	pid, err := k.Spawn("trigger agent message", &agents.AgentInfo{
		Manifest: agents.AgentManifest{Name: "atdd62-agent-message"},
	}, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	logCh, ok := k.GetLogChan(pid)
	if !ok {
		t.Fatalf("GetLogChan(%d) returned false", pid)
	}

	select {
	case entry := <-logCh:
		if entry.Category != types.LogOutput {
			t.Fatalf("LogChan category = %q, want %q", entry.Category, types.LogOutput)
		}
		if entry.Content != "intermediate worker output" {
			t.Fatalf("LogChan content = %q, want agent_message text", entry.Content)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for agent_message LogChan entry")
	}
}
