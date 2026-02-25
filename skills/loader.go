package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/goccy/go-yaml"
)

// SkillLoader loads skill definitions from a base directory.
type SkillLoader struct {
	basePath string
}

// NewSkillLoader creates a new SkillLoader rooted at basePath.
func NewSkillLoader(basePath string) *SkillLoader {
	return &SkillLoader{basePath: basePath}
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

// loadAndParse reads a SKILL.md file from disk, validates the path, and parses its frontmatter.
// When fullLoad is true, the Markdown body is also extracted.
func (l *SkillLoader) loadAndParse(skillName string, fullLoad bool) (*SkillManifest, string, error) {
	dir := filepath.Join(l.basePath, skillName)

	// Path containment check: prevent directory traversal
	absBase, err := filepath.Abs(l.basePath)
	if err != nil {
		return nil, "", fmt.Errorf("resolve base path: %w", err)
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, "", fmt.Errorf("resolve skill path: %w", err)
	}
	if !strings.HasPrefix(absDir, absBase+string(filepath.Separator)) {
		return nil, "", fmt.Errorf("invalid skill name %q: path escapes base directory", skillName)
	}

	fi, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", fmt.Errorf("skill %q not found: %w", skillName, err)
		}
		return nil, "", fmt.Errorf("stat skill directory %q: %w", dir, err)
	}
	if !fi.IsDir() {
		return nil, "", fmt.Errorf("skill path %q is not a directory", dir)
	}

	skillPath := filepath.Join(dir, "SKILL.md")
	data, err := os.ReadFile(skillPath)
	if err != nil {
		return nil, "", fmt.Errorf("read SKILL.md for skill %q: %w", skillName, err)
	}

	fm, body, err := parseSKILLMD(string(data), fullLoad)
	if err != nil {
		return nil, "", fmt.Errorf("parse SKILL.md for skill %q: %w", skillName, err)
	}

	var manifest SkillManifest
	if err := yaml.Unmarshal([]byte(fm), &manifest); err != nil {
		return nil, "", fmt.Errorf("parse frontmatter for skill %q: %w", skillName, err)
	}

	if manifest.Name == "" {
		return nil, "", fmt.Errorf("skill %q manifest missing required field: name", skillName)
	}

	return &manifest, body, nil
}

// LoadMetadata reads only the YAML frontmatter of a SKILL.md file.
func (l *SkillLoader) LoadMetadata(skillName string) (*SkillInfo, error) {
	manifest, _, err := l.loadAndParse(skillName, false)
	if err != nil {
		return nil, err
	}
	return &SkillInfo{Manifest: *manifest}, nil
}

// LoadFull reads the complete SKILL.md file (frontmatter + body).
func (l *SkillLoader) LoadFull(skillName string) (*SkillInfo, error) {
	manifest, body, err := l.loadAndParse(skillName, true)
	if err != nil {
		return nil, err
	}
	return &SkillInfo{
		Manifest: *manifest,
		Body:     body,
	}, nil
}
