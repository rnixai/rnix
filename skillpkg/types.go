package skillpkg

import "time"

// SkillPackage represents a downloaded skill package from the community registry.
type SkillPackage struct {
	Name     string `yaml:"name"`
	Version  string `yaml:"version"`
	Checksum string `yaml:"checksum"` // "sha256:<hex>"
	Data     []byte `yaml:"-"`        // raw .tar.gz content
}

// SkillVersion represents version metadata for a skill in the registry.
type SkillVersion struct {
	Version  string `yaml:"version"`
	Checksum string `yaml:"checksum"`
	URL      string `yaml:"url"`
}

// RegistryEntry represents a locally installed skill in the registry.
type RegistryEntry struct {
	Name        string    `yaml:"name,omitempty"`
	Version     string    `yaml:"version"`
	InstalledAt time.Time `yaml:"installed_at"`
	Source      string    `yaml:"source"` // "community" or "builtin"
	Checksum    string    `yaml:"checksum"`
}

// SkillIndex represents the index response from the community registry.
type SkillIndex struct {
	Skills []SkillIndexEntry `yaml:"skills"`
}

// SkillIndexEntry represents a single entry in the skill index.
type SkillIndexEntry struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Latest      string `yaml:"latest"`
}

// InstallOpts configures installation behavior.
type InstallOpts struct {
	Force bool // overwrite existing installation
}

// InstallResult captures the outcome of a skill installation.
type InstallResult struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Fresh   bool   `json:"fresh"` // true if newly installed, false if overwritten
}
