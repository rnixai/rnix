package skills

import (
	"os"
	"strings"
)

// SkillDiscovery scans available skills and provides metadata for matching.
type SkillDiscovery struct {
	loader   *SkillLoader
	basePath string
}

// NewSkillDiscovery creates a new SkillDiscovery that scans basePath for skills.
func NewSkillDiscovery(loader *SkillLoader, basePath string) *SkillDiscovery {
	return &SkillDiscovery{loader: loader, basePath: basePath}
}

// DiscoverAll scans the basePath directory for all valid skills and returns their metadata.
// Invalid skills (missing or malformed SKILL.md) are silently skipped.
// Returns an empty list (not an error) if the directory does not exist or is empty.
func (d *SkillDiscovery) DiscoverAll() ([]SkillInfo, error) {
	entries, err := os.ReadDir(d.basePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var result []SkillInfo
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		info, err := d.loader.LoadMetadata(entry.Name())
		if err != nil {
			continue // skip invalid skills silently
		}

		result = append(result, *info)
	}

	return result, nil
}
