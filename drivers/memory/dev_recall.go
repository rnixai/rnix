package memory

import (
	kernelmemory "github.com/rnixai/rnix/kernel/memory"
	"github.com/rnixai/rnix/vfs"
)

// RecallSearcher abstracts the recall index search for VFS device use.
// Implemented by kernel/memory.RecallIndex.
type RecallSearcher interface {
	Search(query string, maxResults int) []kernelmemory.RecallResult
	Ready() bool
}

// MemoryRecallDriver is the VFS device driver for /dev/memory/recall.
// It provides read-only search over historical process conversations.
type MemoryRecallDriver struct {
	searcher RecallSearcher
	caller   kernelmemory.LLMCaller // optional, for LLM summarization
}

// compile-time interface check
var _ vfs.ToolDescriptor = (*MemoryRecallDriver)(nil)

// NewRecallDriver creates a new MemoryRecallDriver.
func NewRecallDriver(searcher RecallSearcher, caller kernelmemory.LLMCaller) *MemoryRecallDriver {
	return &MemoryRecallDriver{searcher: searcher, caller: caller}
}

// ToolDefs returns tool definitions for the memory recall device.
// Metadata follows Architecture Decision 35 (six-dimensional ToolDef).
func (d *MemoryRecallDriver) ToolDefs() []vfs.ToolDef {
	return []vfs.ToolDef{
		{
			Name:              "MemoryRecall",
			Description:       loadPrompt("memory_recall"),
			IsReadOnly:        true,
			IsConcurrencySafe: true,
			IsDestructive:     false,
			ShouldDefer:       true,
			SearchHint:        "search historical conversations and past agent knowledge",
			MaxResultTokens:   4000,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Search query — keywords describing what you want to find from past conversations",
					},
					"max_results": map[string]any{
						"type":        "integer",
						"description": "Maximum number of results to return (default: 20)",
						"default":     20,
					},
					"summarize": map[string]any{
						"type":        "boolean",
						"description": "If true, results are summarized by an auxiliary LLM for concise context injection (default: false)",
						"default":     false,
					},
				},
				"required": []string{"query"},
			},
		},
	}
}

// RecallFileFactory returns a VFSFileFactory that creates MemoryRecallFile instances.
func RecallFileFactory(driver *MemoryRecallDriver) vfs.VFSFileFactory {
	return func(subpath string, flags vfs.OpenFlag, workDir string) (vfs.VFSFile, error) {
		return &MemoryRecallFile{driver: driver}, nil
	}
}
