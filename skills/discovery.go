package skills

import (
	"os"
	"strings"
)

// SkillDiscovery scans available skills and provides metadata for matching.
type SkillDiscovery struct {
	loader     *SkillLoader
	searchDirs []string
}

// NewSkillDiscovery creates a new SkillDiscovery that scans searchDirs for skills.
// Directories are searched in order; the first occurrence of a skill name wins (shadow resolution).
func NewSkillDiscovery(loader *SkillLoader, searchDirs []string) *SkillDiscovery {
	return &SkillDiscovery{loader: loader, searchDirs: searchDirs}
}

// DiscoverAll scans all search directories for valid skills and returns their metadata.
// Project-level skills shadow global skills with the same name (first directory wins).
// Invalid skills (missing or malformed SKILL.md) are silently skipped.
// Returns an empty list (not an error) if no directories exist or all are empty.
func (d *SkillDiscovery) DiscoverAll() ([]SkillInfo, error) {
	seen := make(map[string]bool)
	var result []SkillInfo

	for _, dir := range d.searchDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}

		for _, entry := range entries {
			if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
				continue
			}

			name := entry.Name()
			if seen[name] {
				continue // already discovered from higher-priority dir
			}

			info, err := d.loader.LoadMetadata(name)
			if err != nil {
				continue // skip invalid skills silently
			}

			seen[name] = true
			result = append(result, *info)
		}
	}

	return result, nil
}
