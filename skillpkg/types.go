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
//
// Story 47.3 AC3: Scope/Namespace/Path record the writeScope where the skill
// landed; CLI uses these to render `(<scope>/<ns>: <path>)` metadata while
// keeping the legacy three-field shape (Name/Version/Fresh) intact for
// downstream JSON consumers.
type InstallResult struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Fresh     bool   `json:"fresh"`               // true if newly installed, false if overwritten
	Scope     string `json:"scope,omitempty"`     // "project" / "user"
	Namespace string `json:"namespace,omitempty"` // "native" / "agents"
	Path      string `json:"path,omitempty"`      // absolute path to the installed skill directory
}

// UpdateResult captures the outcome of a skill update check.
//
// Story 47.3 AC4: Scope/Namespace/Path record where the updated skill lives —
// Update writes back to the origin scope (no silent migration), so these fields
// reflect the origin location not the installer's configured writeScope.
type UpdateResult struct {
	Name       string `json:"name"`
	OldVersion string `json:"old_version"`
	NewVersion string `json:"new_version"`
	Updated    bool   `json:"updated"`
	Scope      string `json:"scope,omitempty"`     // "project" / "user" — origin scope
	Namespace  string `json:"namespace,omitempty"` // "native" / "agents" — origin namespace
	Path       string `json:"path,omitempty"`      // absolute path to the skill directory under origin scope
}

// UpdateOpts configures update behavior.
type UpdateOpts struct {
	Force bool // force update even if versions match
}

// ListEntry represents a skill in the combined list output.
type ListEntry struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Path        string `json:"path"` // absolute path under the winning scope (Story 47.2 AC2)
	Description string `json:"description"`
	Source      string `json:"source"`    // "builtin" or "community"
	Scope       string `json:"scope"`     // "project" / "user" — winning ScopePath.Scope (Story 47.2 AC4)
	Namespace   string `json:"namespace"` // "native" / "agents" — winning ScopePath.Namespace (Story 47.2 AC4)
	Shadowed    bool   `json:"shadowed"`  // reserved for 47.3 --include-shadowed flag; always false in 47.2 result
}

// ShadowWarning describes a same-name skill conflict between two scopes,
// emitted by ListAll for every shadowed (lower-priority) entry. Story 47.2 AC3.
type ShadowWarning struct {
	SkillName     string `json:"skill_name"`
	WinningPath   string `json:"winning_path"` // absolute path of the winning scope's skill dir
	WinningScope  string `json:"winning_scope"`
	WinningNS     string `json:"winning_ns"`
	ShadowedPath  string `json:"shadowed_path"`
	ShadowedScope string `json:"shadowed_scope"`
	ShadowedNS    string `json:"shadowed_ns"`
}

// SkipEntry records a skill that was skipped during ListAll because its
// SKILL.md failed lenient validation. Story 47.2 AC7.
type SkipEntry struct {
	Path   string `json:"path"`   // absolute path to the offending SKILL.md directory
	Reason string `json:"reason"` // e.g. "missing description", "yaml parse error: ..."
}

// LenientWarning records a non-fatal SKILL.md issue surfaced by the loader
// (e.g. name mismatch with parent dir, name too long). The skill is still
// loaded. Story 47.2 AC6.
type LenientWarning struct {
	Path   string `json:"path"`   // absolute path to the offending SKILL.md directory
	Field  string `json:"field"`  // e.g. "name", "description"
	Reason string `json:"reason"` // short categorical reason
	Detail string `json:"detail"` // human-readable details
}

// LoadDiagnostics is the aggregate of non-fatal diagnostics surfaced by
// Installer.ListAll. The CLI (Story 47.3) renders these to stderr / JSON;
// 47.2 only populates the channel. Story 47.2 AC8.
type LoadDiagnostics struct {
	Warnings []ShadowWarning  `json:"warnings,omitempty"`
	Skipped  []SkipEntry      `json:"skipped,omitempty"`
	Lenient  []LenientWarning `json:"lenient,omitempty"`
}
