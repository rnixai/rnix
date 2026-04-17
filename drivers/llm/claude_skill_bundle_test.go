package llm

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestBundleKey_Stable(t *testing.T) {
	t.Parallel()
	skills := []Skill{
		{Name: "alpha", Body: "body-alpha"},
		{Name: "beta", Body: "body-beta"},
	}
	k1 := bundleKey(skills)
	k2 := bundleKey(skills)
	if k1 != k2 {
		t.Errorf("same input produced different keys: %s vs %s", k1, k2)
	}

	// order-independent
	reversed := []Skill{skills[1], skills[0]}
	k3 := bundleKey(reversed)
	if k3 != k1 {
		t.Errorf("order-reversed input produced different key: %s vs %s", k3, k1)
	}
}

func TestBundleKey_SensitiveToBody(t *testing.T) {
	t.Parallel()
	a := []Skill{{Name: "x", Body: "hello"}}
	b := []Skill{{Name: "x", Body: "hellox"}}
	if bundleKey(a) == bundleKey(b) {
		t.Error("expected different keys for different body")
	}
}

func TestBundleKey_SensitiveToName(t *testing.T) {
	t.Parallel()
	a := []Skill{{Name: "x", Body: "same"}}
	b := []Skill{{Name: "y", Body: "same"}}
	if bundleKey(a) == bundleKey(b) {
		t.Error("expected different keys for different name")
	}
}

func TestPrepareClaudeSkillsBundle_EmptyInput(t *testing.T) {
	t.Parallel()
	root, err := prepareClaudeSkillsBundle("", []Skill{{Name: "x", Body: "y", Dir: "/tmp"}})
	if err != nil {
		t.Fatalf("empty projectDir: %v", err)
	}
	if root != "" {
		t.Errorf("expected empty root for empty projectDir, got %q", root)
	}
	root, err = prepareClaudeSkillsBundle(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("empty skills: %v", err)
	}
	if root != "" {
		t.Errorf("expected empty root for empty skills, got %q", root)
	}
}

func TestPrepareClaudeSkillsBundle_CreatesSymlinks(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()

	// Set up a fake skill source directory
	skillSrc := filepath.Join(t.TempDir(), "code-analysis")
	if err := os.MkdirAll(skillSrc, 0o755); err != nil {
		t.Fatalf("mkdir skill src: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillSrc, "SKILL.md"), []byte("# code-analysis"), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	root, err := prepareClaudeSkillsBundle(projectDir, []Skill{
		{Name: "code-analysis", Body: "# code-analysis", Dir: skillSrc},
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if root == "" {
		t.Fatal("expected non-empty root")
	}

	// Verify symlink exists and points at the source
	linkPath := filepath.Join(root, ".claude", "skills", "code-analysis")
	info, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatalf("lstat symlink: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("expected %s to be a symlink, mode=%v", linkPath, info.Mode())
	}
	target, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}
	if target != skillSrc {
		t.Errorf("symlink target = %q, want %q", target, skillSrc)
	}

	// Verify bundle root lives where we expect
	wantParent := filepath.Join(projectDir, ".rnix", "data", "claude-bundles")
	if filepath.Dir(root) != wantParent {
		t.Errorf("bundle root parent = %q, want %q", filepath.Dir(root), wantParent)
	}
}

func TestPrepareClaudeSkillsBundle_Idempotent(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	skillSrc := filepath.Join(t.TempDir(), "s")
	if err := os.MkdirAll(skillSrc, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	skills := []Skill{{Name: "s", Body: "body", Dir: skillSrc}}

	root1, err := prepareClaudeSkillsBundle(projectDir, skills)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	info1, err := os.Stat(root1)
	if err != nil {
		t.Fatalf("stat root1: %v", err)
	}

	root2, err := prepareClaudeSkillsBundle(projectDir, skills)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if root2 != root1 {
		t.Errorf("second call root = %q, want %q", root2, root1)
	}
	info2, err := os.Stat(root2)
	if err != nil {
		t.Fatalf("stat root2: %v", err)
	}
	if !info1.ModTime().Equal(info2.ModTime()) {
		t.Error("second call unexpectedly modified the bundle directory")
	}
}

func TestPrepareClaudeSkillsBundle_Race(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	skillSrc := filepath.Join(t.TempDir(), "s")
	if err := os.MkdirAll(skillSrc, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	skills := []Skill{{Name: "s", Body: "race-body", Dir: skillSrc}}

	const concurrency = 8
	roots := make([]string, concurrency)
	errs := make([]error, concurrency)
	var wg sync.WaitGroup
	wg.Add(concurrency)
	for i := range concurrency {
		go func(idx int) {
			defer wg.Done()
			roots[idx], errs[idx] = prepareClaudeSkillsBundle(projectDir, skills)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
	}
	for i := 1; i < concurrency; i++ {
		if roots[i] != roots[0] {
			t.Errorf("goroutine %d root = %q, want %q", i, roots[i], roots[0])
		}
	}

	// Ensure no leftover .bundle-tmp-* directories remain
	bundleParent := filepath.Join(projectDir, ".rnix", "data", "claude-bundles")
	entries, err := os.ReadDir(bundleParent)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if len(e.Name()) > 0 && e.Name()[0] == '.' {
			t.Errorf("leftover tmp directory: %s", e.Name())
		}
	}
}
