package skillpkg

import (
	"github.com/rnixai/rnix/internal/config"
	"github.com/rnixai/rnix/skills"
)

// newSingleScopeInstaller wraps a single basePath as a project/native ScopePath
// for legacy single-scope tests authored against the pre-47.2 NewInstaller
// signature. The resulting Installer.scopes has exactly one entry whose
// LocalRegistry shares the basePath.
func newSingleScopeInstaller(client *RegistryClient, skillLoader *skills.SkillLoader, basePath string) *Installer {
	sp := config.ScopePath{
		Path:      basePath,
		Scope:     config.SkillScopeProject,
		Namespace: config.SkillNamespaceRnix,
	}
	return NewInstaller(client, skillLoader, []config.ScopePath{sp}, sp)
}

// singleScopeRegistry returns the LocalRegistry corresponding to a
// newSingleScopeInstaller's basePath.
func singleScopeRegistry(inst *Installer, basePath string) *LocalRegistry {
	return inst.registries[basePath]
}
