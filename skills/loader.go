package skills

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/goccy/go-yaml"
)

// LoadYAML reads a YAML file and unmarshals it into the target type T.
func LoadYAML[T any](path string) (T, error) {
	var zero T
	data, err := os.ReadFile(path)
	if err != nil {
		return zero, fmt.Errorf("read %s: %w", path, err)
	}
	var result T
	if err := yaml.Unmarshal(data, &result); err != nil {
		return zero, fmt.Errorf("parse %s: %w", path, err)
	}
	return result, nil
}

// SkillLoader loads skill definitions from a base directory.
type SkillLoader struct {
	basePath string
}

// NewSkillLoader creates a new SkillLoader rooted at basePath.
func NewSkillLoader(basePath string) *SkillLoader {
	return &SkillLoader{basePath: basePath}
}

// Load reads a skill's manifest.yaml and instructions.md from the given skill
// directory name under the loader's base path. It returns a populated SkillInfo
// or an error describing what went wrong.
func (l *SkillLoader) Load(skillName string) (*SkillInfo, error) {
	dir := filepath.Join(l.basePath, skillName)

	// Check if skill directory exists and is a directory.
	fi, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("skill %q not found: %w", skillName, err)
		}
		return nil, fmt.Errorf("stat skill directory %q: %w", dir, err)
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("skill path %q is not a directory", dir)
	}

	// Load and parse manifest.yaml.
	manifestPath := filepath.Join(dir, "manifest.yaml")
	manifest, err := LoadYAML[SkillManifest](manifestPath)
	if err != nil {
		return nil, fmt.Errorf("load manifest for skill %q: %w", skillName, err)
	}

	// Validate required fields.
	if manifest.Name == "" {
		return nil, fmt.Errorf("skill %q manifest missing required field: name", skillName)
	}

	// Read instructions.md.
	instructionsPath := filepath.Join(dir, "instructions.md")
	instrData, err := os.ReadFile(instructionsPath)
	if err != nil {
		return nil, fmt.Errorf("read instructions for skill %q: %w", skillName, err)
	}

	return &SkillInfo{
		Manifest:     manifest,
		Instructions: string(instrData),
	}, nil
}
