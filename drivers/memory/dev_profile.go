package memory

import (
	kernelmemory "github.com/rnixai/rnix/kernel/memory"
	"github.com/rnixai/rnix/vfs"
)

// MemoryProfileDriver is the VFS device driver for /dev/memory/profile.
// It delegates all operations to the kernel MemoryStore with target fixed to "user".
type MemoryProfileDriver struct {
	store *kernelmemory.MemoryStore
}

// compile-time interface check
var _ vfs.ToolDescriptor = (*MemoryProfileDriver)(nil)

// NewProfileDriver creates a new MemoryProfileDriver backed by the given MemoryStore.
func NewProfileDriver(store *kernelmemory.MemoryStore) *MemoryProfileDriver {
	return &MemoryProfileDriver{store: store}
}

// ToolDefs returns tool definitions for the memory profile device.
// Metadata follows Architecture Decision 35 (six-dimensional ToolDef).
func (d *MemoryProfileDriver) ToolDefs() []vfs.ToolDef {
	return []vfs.ToolDef{
		{
			Name:              "memory_profile",
			Description:       loadPrompt("memory_profile"),
			IsReadOnly:        false,
			IsConcurrencySafe: true,
			IsDestructive:     false,
			ShouldDefer:       true,
			SearchHint:        "user profile preferences role expertise knowledge level personalization",
			MaxResultTokens:   1000,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action": map[string]any{
						"type":        "string",
						"enum":        []string{"add", "replace", "remove", "snapshot", "capacity"},
						"description": "Operation to perform on user profile",
					},
					"content": map[string]any{
						"type":        "string",
						"description": "Content for add action",
					},
					"old": map[string]any{
						"type":        "string",
						"description": "Existing entry content for replace/remove actions (exact match)",
					},
					"new": map[string]any{
						"type":        "string",
						"description": "Replacement content for replace action",
					},
				},
				"required": []string{"action"},
			},
		},
	}
}

// ProfileFileFactory returns a VFSFileFactory that creates MemoryProfileFile instances.
func ProfileFileFactory(driver *MemoryProfileDriver) vfs.VFSFileFactory {
	return func(subpath string, flags vfs.OpenFlag, workDir string) (vfs.VFSFile, error) {
		return &MemoryProfileFile{driver: driver, workDir: workDir}, nil
	}
}
