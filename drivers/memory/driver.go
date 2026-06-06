// Package memory implements the /dev/memory/commit VFS device for reading and
// writing persistent memory entries.
package memory

import (
	kernelmemory "github.com/rnixai/rnix/kernel/memory"
	"github.com/rnixai/rnix/vfs"
)

// MemoryCommitDriver is the VFS device driver for /dev/memory/commit.
// It delegates all operations to the kernel MemoryStore.
type MemoryCommitDriver struct {
	store *kernelmemory.MemoryStore
}

// compile-time interface check
var _ vfs.ToolDescriptor = (*MemoryCommitDriver)(nil)

// NewDriver creates a new MemoryCommitDriver backed by the given MemoryStore.
func NewDriver(store *kernelmemory.MemoryStore) *MemoryCommitDriver {
	return &MemoryCommitDriver{store: store}
}

// ToolDefs returns tool definitions for the memory commit device.
// Metadata follows Architecture Decision 35 (six-dimensional ToolDef).
func (d *MemoryCommitDriver) ToolDefs() []vfs.ToolDef {
	return []vfs.ToolDef{
		{
			Name:              "memory_commit",
			Description:       loadPrompt("memory_commit"),
			IsReadOnly:        false,
			IsConcurrencySafe: true,
			IsDestructive:     false,
			MaxResultTokens:   1000,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action": map[string]any{
						"type":        "string",
						"enum":        []string{"add", "replace", "remove", "snapshot", "capacity"},
						"description": "Operation to perform on memory",
					},
					"target": map[string]any{
						"type":        "string",
						"enum":        []string{"memory", "global_memory"},
						"description": "memory = project scope, global_memory = global scope",
						"default":     "memory",
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

// FileFactory returns a VFSFileFactory that creates MemoryCommitFile instances.
func FileFactory(driver *MemoryCommitDriver) vfs.VFSFileFactory {
	return func(subpath string, flags vfs.OpenFlag, workDir string) (vfs.VFSFile, error) {
		return &MemoryCommitFile{driver: driver, workDir: workDir}, nil
	}
}
