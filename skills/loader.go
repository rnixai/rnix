package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/rnixai/rnix/internal/config"
)

// SkillLoader loads skill definitions from multiple search directories.
type SkillLoader struct {
	searchDirs []string
}

// NewSkillLoader creates a new SkillLoader that searches directories in order.
// The first directory containing the skill wins (shadow resolution).
func NewSkillLoader(searchDirs []string) *SkillLoader {
	return &SkillLoader{searchDirs: searchDirs}
}

// parseSKILLMD parses a SKILL.md file content, extracting YAML frontmatter and optionally the Markdown body.
// When extractBody is false, body string construction is skipped (used by LoadMetadata for efficiency).
func parseSKILLMD(content string, extractBody bool) (frontmatter string, body string, err error) {
	const sep = "---"
	lines := strings.Split(content, "\n")

	if len(lines) == 0 || strings.TrimSpace(lines[0]) != sep {
		return "", "", fmt.Errorf("SKILL.md must start with ---")
	}

	endIdx := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == sep {
			endIdx = i
			break
		}
	}
	if endIdx == -1 {
		return "", "", fmt.Errorf("SKILL.md missing closing ---")
	}

	frontmatter = strings.Join(lines[1:endIdx], "\n")
	if extractBody {
		body = strings.TrimSpace(strings.Join(lines[endIdx+1:], "\n"))
	}
	return frontmatter, body, nil
}

// LenientSkipError signals that a SKILL.md was non-fatally skipped during
// loading per agentskills.io lenient validation rules — e.g. missing
// description, missing name, or fully unparseable YAML. Callers can use
// errors.As to detect this and route the failure to a diagnostics channel
// rather than treating it as a hard load error. Story 47.2 AC7.
type LenientSkipError struct {
	Path   string // absolute path to the offending SKILL.md directory
	Reason string // e.g. "missing description", "yaml parse error: ..."
}

func (e *LenientSkipError) Error() string {
	return fmt.Sprintf("skip skill at %q: %s", e.Path, e.Reason)
}

// LenientWarning describes a non-fatal SKILL.md issue that still permits the
// skill to be loaded (warn-but-load semantics — name mismatch with parent dir,
// name length > 64 chars). Story 47.2 AC6.
type LenientWarning struct {
	Path   string // absolute path to the offending SKILL.md directory
	Field  string // "name" / "description" / ...
	Reason string // short categorical reason
	Detail string // human-readable details, including any truncated representation
}

// maxSkillNameLen is the soft limit per agentskills.io for SKILL.md `name`.
const maxSkillNameLen = 64

// loadAndParse reads a SKILL.md file from disk, validates the path, and parses
// its frontmatter. When fullLoad is true, the Markdown body is also extracted.
// Lenient validation issues (warn-but-load) are returned via the lenient slice;
// fatal lenient cases (missing name/description, unparseable YAML) come back as
// *LenientSkipError so callers can route them to diagnostics. Other failures
// (path traversal, I/O) remain plain errors.
// Returns (manifest, body, skillDirAbsPath, lenient warnings, error).
func (l *SkillLoader) loadAndParse(skillName string, fullLoad bool) (*SkillManifest, string, string, []LenientWarning, error) {
	// Path traversal check: reject names with path separators
	if strings.ContainsAny(skillName, `/\`) || skillName == ".." || strings.Contains(skillName, "..") {
		return nil, "", "", nil, fmt.Errorf("invalid skill name %q: path traversal not allowed", skillName)
	}

	// Use ShadowResolve to find the skill directory in searchDirs (project-first)
	dir := config.ShadowResolve(skillName, l.searchDirs...)
	if dir == "" {
		return nil, "", "", nil, fmt.Errorf("skill %q not found (searched %v)", skillName, l.searchDirs)
	}

	// Path containment check: ensure resolved path is under one of the searchDirs
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, "", "", nil, fmt.Errorf("resolve skill path: %w", err)
	}
	if !isUnderAnySkillDir(absDir, l.searchDirs) {
		return nil, "", "", nil, fmt.Errorf("invalid skill name %q: resolved path escapes search directories", skillName)
	}

	fi, err := os.Stat(dir)
	if err != nil {
		return nil, "", "", nil, fmt.Errorf("stat skill directory %q: %w", dir, err)
	}
	if !fi.IsDir() {
		return nil, "", "", nil, fmt.Errorf("skill path %q is not a directory", dir)
	}

	skillPath := filepath.Join(dir, "SKILL.md")
	data, err := os.ReadFile(skillPath)
	if err != nil {
		return nil, "", "", nil, fmt.Errorf("read SKILL.md for skill %q: %w", skillName, err)
	}

	fm, body, err := parseSKILLMD(string(data), fullLoad)
	if err != nil {
		return nil, "", "", nil, fmt.Errorf("parse SKILL.md for skill %q: %w", skillName, err)
	}

	var manifest SkillManifest
	if err := yaml.Unmarshal([]byte(fm), &manifest); err != nil {
		return nil, "", "", nil, &LenientSkipError{
			Path:   absDir,
			Reason: "yaml parse error: " + err.Error(),
		}
	}

	if manifest.Name == "" {
		return nil, "", "", nil, &LenientSkipError{
			Path:   absDir,
			Reason: "missing name",
		}
	}

	if strings.TrimSpace(manifest.Description) == "" {
		return nil, "", "", nil, &LenientSkipError{
			Path:   absDir,
			Reason: "missing description",
		}
	}

	// Warn-but-load checks (AC6).
	var lenient []LenientWarning
	if manifest.Name != skillName {
		lenient = append(lenient, LenientWarning{
			Path:   absDir,
			Field:  "name",
			Reason: "name mismatch parent dir",
			Detail: fmt.Sprintf("manifest.Name=%q, parentDir=%q", manifest.Name, skillName),
		})
	}
	if len(manifest.Name) > maxSkillNameLen {
		truncated := manifest.Name[:maxSkillNameLen-3] + "..."
		lenient = append(lenient, LenientWarning{
			Path:   absDir,
			Field:  "name",
			Reason: fmt.Sprintf("name exceeds %d chars", maxSkillNameLen),
			Detail: fmt.Sprintf("truncated=%q (full len=%d)", truncated, len(manifest.Name)),
		})
	}

	return &manifest, body, absDir, lenient, nil
}

// LoadMetadata reads only the YAML frontmatter of a SKILL.md file.
// Backward-compat shim: discards lenient warnings (Epic 2 callers stay 2-value).
// Lenient-skip errors still surface as plain errors so existing callers continue
// to treat them as load failures.
func (l *SkillLoader) LoadMetadata(skillName string) (*SkillInfo, error) {
	info, _, err := l.LoadMetadataWithDiag(skillName)
	return info, err
}

// LoadMetadataWithDiag returns SkillInfo plus any warn-but-load lenient warnings.
// Lenient-skip cases (missing name/description, yaml errors) return a non-nil
// *LenientSkipError via the error return; callers should errors.As to detect.
// Story 47.2 AC6 / AC7 (dev-notes §4 method A).
func (l *SkillLoader) LoadMetadataWithDiag(skillName string) (*SkillInfo, []LenientWarning, error) {
	manifest, _, _, lenient, err := l.loadAndParse(skillName, false)
	if err != nil {
		return nil, lenient, err
	}
	return &SkillInfo{Manifest: *manifest}, lenient, nil
}

// LoadFull reads the complete SKILL.md file (frontmatter + body).
// Backward-compat shim: discards lenient warnings.
func (l *SkillLoader) LoadFull(skillName string) (*SkillInfo, error) {
	info, _, err := l.LoadFullWithDiag(skillName)
	return info, err
}

// LoadFullWithDiag returns full SkillInfo plus any warn-but-load lenient warnings.
// Story 47.2 AC6 / AC7.
func (l *SkillLoader) LoadFullWithDiag(skillName string) (*SkillInfo, []LenientWarning, error) {
	manifest, body, dir, lenient, err := l.loadAndParse(skillName, true)
	if err != nil {
		return nil, lenient, err
	}
	return &SkillInfo{
		Manifest: *manifest,
		Body:     body,
		Dir:      dir,
	}, lenient, nil
}

// isUnderAnySkillDir checks if absPath is under at least one of the given directories.
func isUnderAnySkillDir(absPath string, dirs []string) bool {
	for _, dir := range dirs {
		absBase, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		if strings.HasPrefix(absPath, absBase+string(filepath.Separator)) || absPath == absBase {
			return true
		}
	}
	return false
}
