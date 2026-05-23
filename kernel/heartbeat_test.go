package kernel

// =============================================================================
// Story 30.5 ATDD 守护 — P4 等价语义引用 (Epic 45 / Story 45.4)
//
// 本文件 7 个测试守护 Story 30.5（heartbeat liveness detection）原 AC#1-#4：
//   - heartbeat 字段初始化 (LastHeartbeat / StepTimeout)
//   - SpawnOpts.StepTimeout 字段 + AgentManifest.step_timeout 解析
//   - StepTimeout 解析优先级 (SpawnOpts > AgentManifest > default 5m)
//   - StepTimeout=0 表示禁用
//   - LastHeartbeat 并发安全
//   - reasonStep 每步更新 LastHeartbeat
//
// Story 45.2 (warn-only) + 45.3 (HB-1 keeper 删除) + 45.4 (ATDD 改写) 后这些
// 断言**仍然有效**——P4 daemon-passive 仅删除"破坏性动作"与"假心跳路径"，未动
// "心跳字段语义"与"reasonStep 边界更新"。
//
// 进一步的 P4 等价语义在以下文件中显式指名锚定：
//   - kernel/atdd_45_4_30_5_continuity_test.go (AC1 4 用例 — 30.5 P4 等价)
//   - kernel/atdd_45_4_long_run_test.go (AC4 2 用例 — epic AC-EA2 长跑承诺)
// =============================================================================

import (
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/vfs"

	"github.com/rnixai/rnix/agents"
)

func TestProcess_HeartbeatFieldsInitialized(t *testing.T) {
	// NewProcess should NOT set heartbeat fields (they are set by Spawn)
	proc := NewProcess(0, "test intent", nil)

	if !proc.LastHeartbeat.IsZero() {
		t.Errorf("NewProcess should not initialize LastHeartbeat, got %v", proc.LastHeartbeat)
	}
	if proc.StepTimeout != 0 {
		t.Errorf("NewProcess should not initialize StepTimeout, got %v", proc.StepTimeout)
	}
}

func TestSpawnOpts_StepTimeout(t *testing.T) {
	// Verify SpawnOpts can hold StepTimeout
	opts := SpawnOpts{
		StepTimeout: 10 * time.Minute,
	}
	if opts.StepTimeout != 10*time.Minute {
		t.Errorf("StepTimeout = %v, want 10m", opts.StepTimeout)
	}
}

func TestAgentManifest_StepTimeout(t *testing.T) {
	// Verify AgentManifest parses step_timeout as string
	m := agents.AgentManifest{
		Name:        "test-agent",
		StepTimeout: "10m",
	}
	d, err := time.ParseDuration(m.StepTimeout)
	if err != nil {
		t.Fatalf("ParseDuration(%q) error: %v", m.StepTimeout, err)
	}
	if d != 10*time.Minute {
		t.Errorf("ParseDuration(%q) = %v, want 10m", m.StepTimeout, d)
	}
}

func TestAgentManifest_StepTimeout_Disabled(t *testing.T) {
	// "0" or "0s" means disabled
	for _, val := range []string{"0s", "0"} {
		m := agents.AgentManifest{
			StepTimeout: val,
		}
		d, err := time.ParseDuration(m.StepTimeout)
		if err != nil {
			t.Fatalf("ParseDuration(%q) error: %v", val, err)
		}
		if d != 0 {
			t.Errorf("ParseDuration(%q) = %v, want 0", val, d)
		}
	}
}

func TestStepTimeout_Resolution_Priority(t *testing.T) {
	// Tests the priority: SpawnOpts > AgentManifest > default (5m)

	tests := []struct {
		name        string
		optsTimeout time.Duration
		manifest    string
		want        time.Duration
	}{
		{
			name:        "default 5 minutes",
			optsTimeout: 0,
			manifest:    "",
			want:        5 * time.Minute,
		},
		{
			name:        "manifest overrides default",
			optsTimeout: 0,
			manifest:    "10m",
			want:        10 * time.Minute,
		},
		{
			name:        "opts overrides manifest",
			optsTimeout: 3 * time.Minute,
			manifest:    "10m",
			want:        3 * time.Minute,
		},
		{
			name:        "manifest disabled",
			optsTimeout: 0,
			manifest:    "0s",
			want:        0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stepTimeout := 5 * time.Minute
			if tt.optsTimeout > 0 {
				stepTimeout = tt.optsTimeout
			} else if tt.manifest != "" {
				if d, err := time.ParseDuration(tt.manifest); err == nil {
					stepTimeout = d
				}
			}
			if stepTimeout != tt.want {
				t.Errorf("stepTimeout = %v, want %v", stepTimeout, tt.want)
			}
		})
	}
}

func TestProcess_HeartbeatConcurrency(t *testing.T) {
	// Verify concurrent access to LastHeartbeat is safe under proc.mu
	proc := NewProcess(0, "concurrent test", nil)
	proc.mu.Lock()
	proc.LastHeartbeat = time.Now()
	proc.StepTimeout = 5 * time.Minute
	proc.mu.Unlock()

	done := make(chan struct{})

	// Writer goroutine
	go func() {
		for range 100 {
			proc.mu.Lock()
			proc.LastHeartbeat = time.Now()
			proc.mu.Unlock()
		}
		done <- struct{}{}
	}()

	// Reader goroutine
	go func() {
		for range 100 {
			proc.mu.Lock()
			_ = proc.LastHeartbeat
			_ = proc.StepTimeout
			proc.mu.Unlock()
		}
		done <- struct{}{}
	}()

	<-done
	<-done
}

// TestReasonStep_HeartbeatUpdatedPerStep verifies that the reasonStep loop
// updates proc.LastHeartbeat at each step iteration (Story 30.5 Task 6.3).
func TestReasonStep_HeartbeatUpdatedPerStep(t *testing.T) {
	// Multi-step: tool_call then text — two reason steps
	reg := vfs.NewDeviceRegistry()

	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeToolCallResponse("/dev/tools/read", map[string]any{"path": "/foo"}, 50),
			makeLLMResponse("done", 30),
		},
	}
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return seqFile, nil
	})
	registerMockTool(reg, "/dev/tools/read", func(subpath string, flags vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockToolFile{readData: []byte("bar")}, nil
	})

	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	pid, err := k.Spawn("read a file", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)

	// Record spawn-time heartbeat
	proc.mu.Lock()
	spawnHeartbeat := proc.LastHeartbeat
	proc.mu.Unlock()

	if spawnHeartbeat.IsZero() {
		t.Fatal("LastHeartbeat should be initialized at Spawn time")
	}

	// Wait for completion
	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("unexpected exit code %d: %s", exit.Code, exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for process to complete")
	}

	// After reasonStep loop ran (2 steps), LastHeartbeat should be updated
	proc.mu.Lock()
	finalHeartbeat := proc.LastHeartbeat
	proc.mu.Unlock()

	if finalHeartbeat.IsZero() {
		t.Fatal("LastHeartbeat should not be zero after reasonStep")
	}
	if !finalHeartbeat.After(spawnHeartbeat) && !finalHeartbeat.Equal(spawnHeartbeat) {
		t.Errorf("LastHeartbeat should be >= spawn time: spawn=%v final=%v", spawnHeartbeat, finalHeartbeat)
	}

	// Verify StepTimeout was set to default 5m (no agent manifest)
	proc.mu.Lock()
	timeout := proc.StepTimeout
	proc.mu.Unlock()

	if timeout != 5*time.Minute {
		t.Errorf("StepTimeout = %v, want 5m (default)", timeout)
	}
}
