package skills

// SkillModels defines LLM provider and model preferences for a skill.
type SkillModels struct {
	Provider  string `yaml:"provider"`
	Preferred string `yaml:"preferred"`
	Fallback  string `yaml:"fallback"`
}

// SkillManifest represents the parsed contents of a skill's manifest.yaml.
type SkillManifest struct {
	Name          string      `yaml:"name"`
	Description   string      `yaml:"description"`
	Tools         []string    `yaml:"tools"`
	Models        SkillModels `yaml:"models"`
	ContextBudget int         `yaml:"context_budget"`
}

// SkillInfo combines a skill's manifest with its instruction text.
type SkillInfo struct {
	Manifest     SkillManifest
	Instructions string
}
