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
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description" json:"description"`
	Latest      string `yaml:"latest" json:"version"`
	Downloads   int    `yaml:"downloads" json:"downloads"`
}

// SearchResult represents a single result from a skill search.
type SearchResult struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
	Downloads   int    `json:"downloads"`
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

// UpdateResult captures the outcome of a skill update check.
type UpdateResult struct {
	Name       string `json:"name"`
	OldVersion string `json:"old_version"`
	NewVersion string `json:"new_version"`
	Updated    bool   `json:"updated"`
}

// UpdateOpts configures update behavior.
type UpdateOpts struct {
	Force bool // force update even if versions match
}
