package kernel

import (
	"slices"
	"testing"

	"github.com/rnixai/rnix/agents"
	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/skills"
	"github.com/rnixai/rnix/vfs"
)

func newMinimalKernel(t *testing.T) *KernelImpl {
	t.Helper()
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockToolFile{readData: []byte(`{"content":"ok","tokens_used":1}`)}, nil
	})
	registerMockTool(reg, "/dev/fs", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockToolFile{readData: []byte("data")}, nil
	})
	registerMockTool(reg, "/dev/shell", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockToolFile{readData: []byte("ok")}, nil
	})
	v := vfs.NewVFS(reg)
	k := NewKernel(v, rnixctx.NewManager(), nil)
	t.Cleanup(func() { k.Shutdown() })
	return k
}

func agentWithAllowedTools(toolsCSV string) *agents.AgentInfo {
	return &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Name:   "test-agent",
			Models: agents.AgentModels{Provider: "claude", Preferred: "sonnet"},
		},
		Instructions: "test",
		Skills: []*skills.SkillInfo{
			{
				Manifest: skills.SkillManifest{
					Name:            "test-skill",
					AllowedToolsRaw: toolsCSV,
				},
				Body: "test",
			},
		},
	}
}

func TestSpawnAllowedDevicesInheritance(t *testing.T) {
	t.Run("restricted_parent_no_agent_child_inherits", func(t *testing.T) {
		k := newMinimalKernel(t)
		parentDevices := []string{"/dev/fs"}

		pid, err := k.Spawn("child", nil, SpawnOpts{
			AllowedDevices: parentDevices,
			SkipReasonLoop: true,
		})
		if err != nil {
			t.Fatalf("Spawn failed: %v", err)
		}
		proc, ok := k.GetProcess(pid)
		if !ok {
			t.Fatal("process not found")
		}
		if !slices.Equal(proc.AllowedDevices, parentDevices) {
			t.Errorf("AllowedDevices = %v, want %v", proc.AllowedDevices, parentDevices)
		}
	})

	t.Run("restricted_parent_agent_child_intersection", func(t *testing.T) {
		k := newMinimalKernel(t)
		parentDevices := []string{"/dev/fs", "/dev/shell"}
		agent := agentWithAllowedTools("/dev/fs")

		pid, err := k.Spawn("child", agent, SpawnOpts{
			AllowedDevices: parentDevices,
			SkipReasonLoop: true,
		})
		if err != nil {
			t.Fatalf("Spawn failed: %v", err)
		}
		proc, ok := k.GetProcess(pid)
		if !ok {
			t.Fatal("process not found")
		}
		want := []string{"/dev/fs"}
		if !slices.Equal(proc.AllowedDevices, want) {
			t.Errorf("AllowedDevices = %v, want %v", proc.AllowedDevices, want)
		}
	})

	t.Run("restricted_parent_agent_child_wider_capped", func(t *testing.T) {
		k := newMinimalKernel(t)
		parentDevices := []string{"/dev/fs"}
		agent := agentWithAllowedTools("/dev/fs /dev/shell")

		pid, err := k.Spawn("child", agent, SpawnOpts{
			AllowedDevices: parentDevices,
			SkipReasonLoop: true,
		})
		if err != nil {
			t.Fatalf("Spawn failed: %v", err)
		}
		proc, ok := k.GetProcess(pid)
		if !ok {
			t.Fatal("process not found")
		}
		want := []string{"/dev/fs"}
		if !slices.Equal(proc.AllowedDevices, want) {
			t.Errorf("AllowedDevices = %v, want %v (child should not exceed parent)", proc.AllowedDevices, want)
		}
	})

	t.Run("unrestricted_parent_no_agent_child_stays_unrestricted", func(t *testing.T) {
		k := newMinimalKernel(t)

		pid, err := k.Spawn("child", nil, SpawnOpts{
			SkipReasonLoop: true,
		})
		if err != nil {
			t.Fatalf("Spawn failed: %v", err)
		}
		proc, ok := k.GetProcess(pid)
		if !ok {
			t.Fatal("process not found")
		}
		if proc.AllowedDevices != nil {
			t.Errorf("AllowedDevices = %v, want nil (unrestricted)", proc.AllowedDevices)
		}
	})

	t.Run("unrestricted_parent_agent_child_uses_agent_perms", func(t *testing.T) {
		k := newMinimalKernel(t)
		agent := agentWithAllowedTools("/dev/fs")

		pid, err := k.Spawn("child", agent, SpawnOpts{
			SkipReasonLoop: true,
		})
		if err != nil {
			t.Fatalf("Spawn failed: %v", err)
		}
		proc, ok := k.GetProcess(pid)
		if !ok {
			t.Fatal("process not found")
		}
		want := []string{"/dev/fs"}
		if !slices.Equal(proc.AllowedDevices, want) {
			t.Errorf("AllowedDevices = %v, want %v", proc.AllowedDevices, want)
		}
	})

	t.Run("disjoint_parent_agent_devices_spawn_fails", func(t *testing.T) {
		k := newMinimalKernel(t)
		parentDevices := []string{"/dev/fs"}
		agent := agentWithAllowedTools("/dev/shell")

		_, err := k.Spawn("child", agent, SpawnOpts{
			AllowedDevices: parentDevices,
			SkipReasonLoop: true,
		})
		if err == nil {
			t.Fatal("Spawn should fail when parent and agent devices are disjoint")
		}
	})

	t.Run("cli_spawn_no_parent_no_agent_unrestricted", func(t *testing.T) {
		k := newMinimalKernel(t)

		pid, err := k.Spawn("top-level", nil, SpawnOpts{
			SkipReasonLoop: true,
		})
		if err != nil {
			t.Fatalf("Spawn failed: %v", err)
		}
		proc, ok := k.GetProcess(pid)
		if !ok {
			t.Fatal("process not found")
		}
		if proc.AllowedDevices != nil {
			t.Errorf("AllowedDevices = %v, want nil", proc.AllowedDevices)
		}
	})
}

func TestIntersectDevices(t *testing.T) {
	tests := []struct {
		name   string
		parent []string
		child  []string
		want   []string
	}{
		{"both_overlap", []string{"/dev/fs", "/dev/shell"}, []string{"/dev/fs"}, []string{"/dev/fs"}},
		{"no_overlap", []string{"/dev/fs"}, []string{"/dev/shell"}, nil},
		{"exact_match", []string{"/dev/fs"}, []string{"/dev/fs"}, []string{"/dev/fs"}},
		{"child_wider", []string{"/dev/fs"}, []string{"/dev/fs", "/dev/shell"}, []string{"/dev/fs"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := intersectDevices(tt.parent, tt.child)
			if !slices.Equal(got, tt.want) {
				t.Errorf("intersectDevices(%v, %v) = %v, want %v", tt.parent, tt.child, got, tt.want)
			}
		})
	}
}
