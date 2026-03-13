package skills

import "strings"

// SynergyDecl declares a synergy with another Skill.
type SynergyDecl struct {
	With        string `yaml:"with"`        // target Skill name
	Instruction string `yaml:"instruction"` // emergent instruction appended to system prompt
}

// SkillManifest represents the parsed YAML frontmatter of a SKILL.md file.
type SkillManifest struct {
	Name            string            `yaml:"name"`
	Description     string            `yaml:"description"`
	AllowedToolsRaw string            `yaml:"allowed-tools"`
	Metadata        map[string]string `yaml:"metadata"`
	Synergies       []SynergyDecl     `yaml:"synergy,omitempty"`
}

// AllowedTools returns the list of allowed tool device paths.
// The raw value is a space-separated string per Agent Skills standard.
func (m *SkillManifest) AllowedTools() []string {
	if m.AllowedToolsRaw == "" {
		return nil
	}
	return strings.Fields(m.AllowedToolsRaw)
}

// SkillInfo combines a skill's manifest with its body content.
type SkillInfo struct {
	Manifest SkillManifest
	Body     string
}
