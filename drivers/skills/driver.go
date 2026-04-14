package skills

import (
	pkgskills "github.com/rnixai/rnix/skills"
	"github.com/rnixai/rnix/vfs"
)

// SkillManagerOps defines the operations the VFS driver needs from SkillManager.
type SkillManagerOps interface {
	Create(name, description, allowedTools, body string, callerDevices []string) error
	Patch(name, newBody string, callerDevices []string) error
	Delete(name string) error
}

// SkillManageDriver is the VFS device driver for /dev/skills/manage.
type SkillManageDriver struct {
	manager SkillManagerOps
}

// compile-time interface check
var _ vfs.ToolDescriptor = (*SkillManageDriver)(nil)

// NewSkillManageDriver creates a new SkillManageDriver.
func NewSkillManageDriver(manager SkillManagerOps) *SkillManageDriver {
	return &SkillManageDriver{manager: manager}
}

// NewSkillManageDriverFromManager creates a driver from a concrete SkillManager.
func NewSkillManageDriverFromManager(manager *pkgskills.SkillManager) *SkillManageDriver {
	return &SkillManageDriver{manager: manager}
}

// ToolDefs returns tool definitions for the skill manage device.
// Metadata follows Architecture Decision 35 (six-dimensional ToolDef).
func (d *SkillManageDriver) ToolDefs() []vfs.ToolDef {
	return []vfs.ToolDef{
		{
			Name:              "skill_manage",
			Description:       loadPrompt("skill_manage"),
			IsReadOnly:        false,
			IsConcurrencySafe: true,
			IsDestructive:     true,
			ShouldDefer:       true,
			SearchHint:        "create modify delete skill workflow automation procedural knowledge",
			MaxResultTokens:   1000,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action": map[string]any{
						"type":        "string",
						"enum":        []string{"create", "patch", "delete"},
						"description": "The operation to perform: create a new skill, patch an existing skill's body, or delete a skill",
					},
					"name": map[string]any{
						"type":        "string",
						"description": "The skill name (used as directory name under .rnix/skills/)",
					},
					"description": map[string]any{
						"type":        "string",
						"description": "Human-readable description of what the skill does (required for create)",
					},
					"allowed_tools": map[string]any{
						"type":        "string",
						"description": "Space-separated VFS device paths the skill needs (e.g., \"/dev/fs /dev/shell\")",
					},
					"body": map[string]any{
						"type":        "string",
						"description": "Markdown instructions for the skill body (required for create and patch)",
					},
				},
				"required": []string{"action", "name"},
			},
		},
	}
}

// SkillManageFileFactory returns a VFSFileFactory that creates SkillManageFile instances.
func SkillManageFileFactory(driver *SkillManageDriver) vfs.VFSFileFactory {
	return func(subpath string, flags vfs.OpenFlag, workDir string) (vfs.VFSFile, error) {
		return &SkillManageFile{driver: driver}, nil
	}
}
