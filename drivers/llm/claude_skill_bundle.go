package llm

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// prepareClaudeSkillsBundle materializes a content-addressed skills directory
// at <projectDir>/.rnix/data/claude-bundles/<sha>/.claude/skills/<name>. Each
// entry is a symlink to the source skill directory so that the same bundle
// hash always points at the same on-disk layout — a precondition for Claude
// CLI's prompt cache to hit the prefix across reasonSteps.
//
// Returns the bundle root to pass as --add-dir. An empty string with a nil
// error means "nothing to materialize" (e.g. no skills, or no project dir).
//
// Race-safe: concurrent calls for the same bundleKey may both materialize tmp
// trees, but only one will win the final Rename; the loser detects the winning
// directory via os.Stat and returns it.
func prepareClaudeSkillsBundle(projectDir string, skills []Skill) (string, error) {
	if len(skills) == 0 || projectDir == "" {
		return "", nil
	}
	key := bundleKey(skills)
	root := filepath.Join(projectDir, ".rnix", "data", "claude-bundles", key)
	skillsHome := filepath.Join(root, ".claude", "skills")

	// Fast path: bundle already exists.
	if _, err := os.Stat(skillsHome); err == nil {
		return root, nil
	}

	parent := filepath.Dir(root)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", fmt.Errorf("mkdir bundle parent: %w", err)
	}

	tmp, err := os.MkdirTemp(parent, ".bundle-tmp-*")
	if err != nil {
		return "", fmt.Errorf("create bundle tmp: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(tmp)
		}
	}()

	tmpSkills := filepath.Join(tmp, ".claude", "skills")
	if err := os.MkdirAll(tmpSkills, 0o755); err != nil {
		return "", fmt.Errorf("mkdir bundle skills: %w", err)
	}
	for _, s := range skills {
		if s.Dir == "" {
			continue // skip skills with no on-disk source
		}
		target := filepath.Join(tmpSkills, s.Name)
		if err := os.Symlink(s.Dir, target); err != nil {
			return "", fmt.Errorf("symlink skill %q: %w", s.Name, err)
		}
	}
	if err := os.Rename(tmp, root); err != nil {
		// Lost the race: a concurrent caller finished first. Verify their
		// directory is valid and return it.
		if _, statErr := os.Stat(root); statErr == nil {
			return root, nil
		}
		return "", fmt.Errorf("finalize bundle: %w", err)
	}
	cleanup = false
	return root, nil
}

// bundleKey derives a stable identifier from the skill set. Only skill name
// and body contribute — dir paths vary per host but the content should not.
// Truncated to 128 bits for path-friendly length (still ample collision resistance).
func bundleKey(skills []Skill) string {
	entries := append([]Skill(nil), skills...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })

	h := sha256.New()
	h.Write([]byte("rnix-claude-skills-bundle:v1\n"))
	for _, s := range entries {
		fmt.Fprintf(h, "name:%s\n", s.Name)
		fmt.Fprintf(h, "body:%d:", len(s.Body))
		h.Write([]byte(s.Body))
		h.Write([]byte("\n"))
	}
	return hex.EncodeToString(h.Sum(nil))[:32]
}
