package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ============================================================
// AC1: String() behavior on SkillScope / SkillNamespace
// ============================================================

func TestSkillScope_String(t *testing.T) {
	cases := []struct {
		scope SkillScope
		want  string
	}{
		{SkillScopeProject, "project"},
		{SkillScopeUser, "user"},
		{SkillScope(99), "unknown"},
	}
	for _, c := range cases {
		if got := c.scope.String(); got != c.want {
			t.Errorf("SkillScope(%d).String() = %q, want %q", c.scope, got, c.want)
		}
	}
}

func TestSkillNamespace_String(t *testing.T) {
	cases := []struct {
		ns   SkillNamespace
		want string
	}{
		{SkillNamespaceRnix, "native"},
		{SkillNamespaceAgents, "agents"},
		{SkillNamespace(99), "unknown"},
	}
	for _, c := range cases {
		if got := c.ns.String(); got != c.want {
			t.Errorf("SkillNamespace(%d).String() = %q, want %q", c.ns, got, c.want)
		}
	}
}

// ============================================================
// AC2/AC3/AC4: ResolveSkillScopes default (no ancestor) behavior
// ============================================================

// setupIsolatedEnv redirects HOME and clears XDG_CONFIG_HOME so the test runs
// without leakage from the developer's actual home directory.
func setupIsolatedEnv(t *testing.T) (home string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	return home
}

// mkdirAll is a thin wrapper that fails the test on error.
func mkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", path, err)
	}
}

func TestResolveSkillScopes_AllFourExist(t *testing.T) {
	home := setupIsolatedEnv(t)
	projectDir := t.TempDir()

	projectRnix := filepath.Join(projectDir, ".rnix", "skills")
	projectAgents := filepath.Join(projectDir, ".agents", "skills")
	userRnix := filepath.Join(home, ".config", "rnix", "skills")
	userAgents := filepath.Join(home, ".agents", "skills")

	mkdirAll(t, projectRnix)
	mkdirAll(t, projectAgents)
	mkdirAll(t, userRnix)
	mkdirAll(t, userAgents)

	result := ResolveSkillScopes(projectDir)
	if len(result) != 4 {
		t.Fatalf("len(result) = %d, want 4; got %+v", len(result), result)
	}

	want := []ScopePath{
		{Path: projectRnix, Scope: SkillScopeProject, Namespace: SkillNamespaceRnix},
		{Path: projectAgents, Scope: SkillScopeProject, Namespace: SkillNamespaceAgents},
		{Path: userRnix, Scope: SkillScopeUser, Namespace: SkillNamespaceRnix},
		{Path: userAgents, Scope: SkillScopeUser, Namespace: SkillNamespaceAgents},
	}
	for i, w := range want {
		if result[i] != w {
			t.Errorf("result[%d] = %+v, want %+v", i, result[i], w)
		}
	}
}

func TestResolveSkillScopes_OnlyUserNative(t *testing.T) {
	// EchoMatrix reproduction: cwd has no .rnix/.agents, only user-native exists.
	home := setupIsolatedEnv(t)
	projectDir := t.TempDir() // no skill dirs

	userRnix := filepath.Join(home, ".config", "rnix", "skills")
	mkdirAll(t, userRnix)
	// Intentionally NOT creating user-agents, project-rnix, project-agents.

	result := ResolveSkillScopes(projectDir)
	if len(result) != 1 {
		t.Fatalf("len(result) = %d, want 1; got %+v", len(result), result)
	}
	want := ScopePath{Path: userRnix, Scope: SkillScopeUser, Namespace: SkillNamespaceRnix}
	if result[0] != want {
		t.Errorf("result[0] = %+v, want %+v", result[0], want)
	}
}

func TestResolveSkillScopes_OnlyProjectRnix(t *testing.T) {
	setupIsolatedEnv(t)
	projectDir := t.TempDir()
	projectRnix := filepath.Join(projectDir, ".rnix", "skills")
	mkdirAll(t, projectRnix)

	result := ResolveSkillScopes(projectDir)
	if len(result) != 1 {
		t.Fatalf("len(result) = %d, want 1; got %+v", len(result), result)
	}
	want := ScopePath{Path: projectRnix, Scope: SkillScopeProject, Namespace: SkillNamespaceRnix}
	if result[0] != want {
		t.Errorf("result[0] = %+v, want %+v", result[0], want)
	}
}

func TestResolveSkillScopes_NoneExist(t *testing.T) {
	setupIsolatedEnv(t)
	projectDir := t.TempDir()

	result := ResolveSkillScopes(projectDir)
	if len(result) != 0 {
		t.Errorf("len(result) = %d, want 0; got %+v", len(result), result)
	}
}

func TestResolveSkillScopes_XDGConfigHome(t *testing.T) {
	// XDG_CONFIG_HOME overrides HOME for user-native, but not for user-agents.
	home := t.TempDir()
	xdg := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", xdg)

	projectDir := t.TempDir() // no project skill dirs

	userNativeXDG := filepath.Join(xdg, "rnix", "skills")
	userAgents := filepath.Join(home, ".agents", "skills")
	mkdirAll(t, userNativeXDG)
	mkdirAll(t, userAgents)

	result := ResolveSkillScopes(projectDir)
	if len(result) != 2 {
		t.Fatalf("len(result) = %d, want 2; got %+v", len(result), result)
	}

	want := []ScopePath{
		{Path: userNativeXDG, Scope: SkillScopeUser, Namespace: SkillNamespaceRnix},
		{Path: userAgents, Scope: SkillScopeUser, Namespace: SkillNamespaceAgents},
	}
	for i, w := range want {
		if result[i] != w {
			t.Errorf("result[%d] = %+v, want %+v", i, result[i], w)
		}
	}
}

// AC7: ancestor traversal disabled by default (does not climb into parent
// directory's .rnix/skills/).
func TestResolveSkillScopes_DefaultNoAncestor(t *testing.T) {
	setupIsolatedEnv(t)
	repoRoot := t.TempDir()
	// Create .rnix/skills at repo root, but NOT in nested cwd.
	mkdirAll(t, filepath.Join(repoRoot, ".rnix", "skills"))
	cwd := filepath.Join(repoRoot, "sub1", "sub2")
	mkdirAll(t, cwd)

	result := ResolveSkillScopes(cwd) // default opts: ancestor disabled
	// No project-level skill dir at cwd, no user dirs. Should return empty.
	if len(result) != 0 {
		t.Errorf("default ResolveSkillScopes climbed into parent: %+v", result)
	}
}

// ============================================================
// AC5: walker limits — blacklist, max depth, max dirs truncation
// ============================================================

func TestResolveSkillScopes_BlacklistGitNodeModules(t *testing.T) {
	setupIsolatedEnv(t)
	// Build: <root>/.git/.rnix/skills (under blacklist; should be ignored)
	//        <root>/.rnix/skills (should be found via ancestor traversal)
	root := t.TempDir()
	mkdirAll(t, filepath.Join(root, ".rnix", "skills"))
	mkdirAll(t, filepath.Join(root, ".git"))
	// Create skills inside .git/ to ensure blacklist actually prevents it
	mkdirAll(t, filepath.Join(root, ".git", ".rnix", "skills"))

	// cwd is two levels deep, but second level is a blacklisted name.
	deep := filepath.Join(root, ".git", "objects")
	mkdirAll(t, deep)

	result := ResolveSkillScopes(deep, WithAncestorTraversal(true))

	// We should NOT see the .git/.rnix/skills path even though it exists.
	for _, sp := range result {
		if strings.Contains(sp.Path, ".git/.rnix") {
			t.Errorf("blacklist failed: result contains .git/.rnix path: %+v", sp)
		}
	}

	// We SHOULD see the root-level .rnix/skills via traversal beyond .git.
	wantRootRnix := filepath.Join(root, ".rnix", "skills")
	found := false
	for _, sp := range result {
		if sp.Path == wantRootRnix {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected to find %q in ancestor traversal results: %+v", wantRootRnix, result)
	}
}

func TestResolveSkillScopes_AncestorDepthLimit(t *testing.T) {
	setupIsolatedEnv(t)
	// Build 10-layer nesting; place .rnix/skills at the deepest cwd's
	// 8-th ancestor (out of reach with default depth 6).
	root := t.TempDir()
	dir := root
	layers := make([]string, 0, 10)
	for i := range 10 {
		dir = filepath.Join(dir, fmt.Sprintf("L%d", i))
		layers = append(layers, dir)
		mkdirAll(t, dir)
	}
	// Put .rnix/skills/ at L0 (8 levels above the deepest cwd L9). With
	// maxAncestorDepth=6 we should NOT find it.
	mkdirAll(t, filepath.Join(layers[0], ".rnix", "skills"))

	cwd := layers[9]
	result := ResolveSkillScopes(cwd, WithAncestorTraversal(true))
	for _, sp := range result {
		if strings.HasPrefix(sp.Path, layers[0]+string(os.PathSeparator)) {
			t.Errorf("depth limit failed: found out-of-reach skill at depth 9: %+v", sp)
		}
	}

	// Sanity: with WithMaxAncestorDepth(20) we SHOULD find it.
	deepResult := ResolveSkillScopes(cwd,
		WithAncestorTraversal(true),
		WithMaxAncestorDepth(20),
	)
	want := filepath.Join(layers[0], ".rnix", "skills")
	found := false
	for _, sp := range deepResult {
		if sp.Path == want {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("with depth=20 expected to find %q, got: %+v", want, deepResult)
	}
}

func TestResolveSkillScopes_MaxDirsTruncation(t *testing.T) {
	home := setupIsolatedEnv(t)
	projectDir := t.TempDir()
	// Set up all four paths to maximize stat opportunities.
	mkdirAll(t, filepath.Join(projectDir, ".rnix", "skills"))
	mkdirAll(t, filepath.Join(projectDir, ".agents", "skills"))
	mkdirAll(t, filepath.Join(home, ".config", "rnix", "skills"))
	mkdirAll(t, filepath.Join(home, ".agents", "skills"))

	var buf bytes.Buffer
	// MaxDirs=1 forces truncation after the first stat call.
	result := ResolveSkillScopes(projectDir,
		WithMaxDirs(1),
		WithWarningWriter(&buf),
	)

	// We expect at most 1 entry (the first stat succeeds, the next triggers
	// the counter > maxDirs check).
	if len(result) > 1 {
		t.Errorf("truncation failed: len(result) = %d, want <= 1; got %+v", len(result), result)
	}

	got := buf.String()
	if !strings.Contains(got, "ResolveSkillScopes truncated") {
		t.Errorf("expected truncation warning, got stderr=%q", got)
	}
	if !strings.Contains(got, "Set WithMaxDirs") {
		t.Errorf("warning missing override hint, got stderr=%q", got)
	}
}

func TestResolveSkillScopes_MaxDirsTruncation_WarnOnce(t *testing.T) {
	home := setupIsolatedEnv(t)
	projectDir := t.TempDir()
	mkdirAll(t, filepath.Join(projectDir, ".rnix", "skills"))
	mkdirAll(t, filepath.Join(projectDir, ".agents", "skills"))
	mkdirAll(t, filepath.Join(home, ".config", "rnix", "skills"))

	var buf bytes.Buffer
	ResolveSkillScopes(projectDir, WithMaxDirs(1), WithWarningWriter(&buf))

	got := buf.String()
	if count := strings.Count(got, "ResolveSkillScopes truncated"); count != 1 {
		t.Errorf("expected exactly 1 truncation warning, got %d: %q", count, got)
	}
}

// ============================================================
// AC6: ancestor traversal — monorepo + git root boundary
// ============================================================

func TestResolveSkillScopes_AncestorTraversal_Monorepo(t *testing.T) {
	setupIsolatedEnv(t)
	repoRoot := t.TempDir()
	// Set up: <repoRoot>/.git/, <repoRoot>/.rnix/skills/, <repoRoot>/sub1/sub2/
	mkdirAll(t, filepath.Join(repoRoot, ".git"))
	mkdirAll(t, filepath.Join(repoRoot, ".rnix", "skills"))
	cwd := filepath.Join(repoRoot, "sub1", "sub2")
	mkdirAll(t, cwd)

	result := ResolveSkillScopes(cwd, WithAncestorTraversal(true))

	want := filepath.Join(repoRoot, ".rnix", "skills")
	found := false
	for _, sp := range result {
		if sp.Path == want {
			found = true
			if sp.Scope != SkillScopeProject || sp.Namespace != SkillNamespaceRnix {
				t.Errorf("found %q but classification wrong: %+v", want, sp)
			}
			break
		}
	}
	if !found {
		t.Errorf("expected ancestor traversal to find %q, got: %+v", want, result)
	}
}

func TestResolveSkillScopes_AncestorTraversal_StopsAtGitRoot(t *testing.T) {
	setupIsolatedEnv(t)
	// Set up nested git repos:
	// /tmp/<outer>/.rnix/skills/         (outer project; outside any inner git)
	// /tmp/<outer>/repo/.git/
	// /tmp/<outer>/repo/.rnix/skills/    (inner repo; this should be the stop)
	// /tmp/<outer>/repo/sub1/sub2/       (cwd)
	outer := t.TempDir()
	mkdirAll(t, filepath.Join(outer, ".rnix", "skills"))

	repo := filepath.Join(outer, "repo")
	mkdirAll(t, filepath.Join(repo, ".git"))
	mkdirAll(t, filepath.Join(repo, ".rnix", "skills"))

	cwd := filepath.Join(repo, "sub1", "sub2")
	mkdirAll(t, cwd)

	result := ResolveSkillScopes(cwd, WithAncestorTraversal(true))

	// Inner repo's .rnix/skills/ should be found.
	innerWant := filepath.Join(repo, ".rnix", "skills")
	innerFound := false
	for _, sp := range result {
		if sp.Path == innerWant {
			innerFound = true
			break
		}
	}
	if !innerFound {
		t.Errorf("inner repo skill dir not found: %+v", result)
	}

	// Outer project's .rnix/skills/ should NOT be found (stopped at .git).
	outerWant := filepath.Join(outer, ".rnix", "skills")
	for _, sp := range result {
		if sp.Path == outerWant {
			t.Errorf("traversal leaked past git root: found %q", outerWant)
		}
	}
}

func TestResolveSkillScopes_AncestorTraversal_NestedPriority(t *testing.T) {
	// When ancestor traversal finds multiple project paths, the closest to
	// cwd should come first in the result list.
	setupIsolatedEnv(t)
	repoRoot := t.TempDir()
	mkdirAll(t, filepath.Join(repoRoot, ".git"))
	mkdirAll(t, filepath.Join(repoRoot, ".rnix", "skills"))

	mid := filepath.Join(repoRoot, "mid")
	mkdirAll(t, filepath.Join(mid, ".rnix", "skills"))

	cwd := filepath.Join(mid, "leaf")
	mkdirAll(t, cwd)

	result := ResolveSkillScopes(cwd, WithAncestorTraversal(true))

	// Order: ancestor traversal pushes closer-to-cwd entries first (mid before repoRoot).
	var midIdx, rootIdx = -1, -1
	midPath := filepath.Join(mid, ".rnix", "skills")
	rootPath := filepath.Join(repoRoot, ".rnix", "skills")
	for i, sp := range result {
		switch sp.Path {
		case midPath:
			midIdx = i
		case rootPath:
			rootIdx = i
		}
	}
	if midIdx < 0 || rootIdx < 0 {
		t.Fatalf("missing one of mid/root paths: midIdx=%d rootIdx=%d got=%+v", midIdx, rootIdx, result)
	}
	if midIdx > rootIdx {
		t.Errorf("priority wrong: mid (closer to cwd) should come before root; midIdx=%d rootIdx=%d", midIdx, rootIdx)
	}
}

// ============================================================
// AC8: performance — sanity time check + benchmarks
// ============================================================

func TestResolveSkillScopes_DefaultUnder50ms(t *testing.T) {
	// Sanity: default call (4 stats) finishes well under 50ms in normal cases.
	home := setupIsolatedEnv(t)
	projectDir := t.TempDir()
	mkdirAll(t, filepath.Join(projectDir, ".rnix", "skills"))
	mkdirAll(t, filepath.Join(home, ".config", "rnix", "skills"))

	start := time.Now()
	ResolveSkillScopes(projectDir)
	elapsed := time.Since(start)
	if elapsed > 50*time.Millisecond {
		t.Errorf("default ResolveSkillScopes took %v, want <= 50ms", elapsed)
	}
}

func TestResolveSkillScopes_AncestorUnder500ms(t *testing.T) {
	setupIsolatedEnv(t)
	repoRoot := t.TempDir()
	mkdirAll(t, filepath.Join(repoRoot, ".git"))
	mkdirAll(t, filepath.Join(repoRoot, ".rnix", "skills"))

	cwd := repoRoot
	for i := range 5 {
		cwd = filepath.Join(cwd, fmt.Sprintf("L%d", i))
	}
	mkdirAll(t, cwd)

	start := time.Now()
	ResolveSkillScopes(cwd, WithAncestorTraversal(true))
	elapsed := time.Since(start)
	if elapsed > 500*time.Millisecond {
		t.Errorf("ancestor ResolveSkillScopes took %v, want <= 500ms", elapsed)
	}
}

func BenchmarkResolveSkillScopes_Default(b *testing.B) {
	home := b.TempDir()
	b.Setenv("HOME", home)
	b.Setenv("XDG_CONFIG_HOME", "")
	projectDir := b.TempDir()
	mustMkdir := func(p string) {
		if err := os.MkdirAll(p, 0o755); err != nil {
			b.Fatal(err)
		}
	}
	mustMkdir(filepath.Join(projectDir, ".rnix", "skills"))
	mustMkdir(filepath.Join(projectDir, ".agents", "skills"))
	mustMkdir(filepath.Join(home, ".config", "rnix", "skills"))
	mustMkdir(filepath.Join(home, ".agents", "skills"))

	b.ResetTimer()
	for b.Loop() {
		result := ResolveSkillScopes(projectDir)
		if len(result) != 4 {
			b.Fatalf("len(result) = %d, want 4", len(result))
		}
	}
}

func BenchmarkResolveSkillScopes_AncestorTraversal(b *testing.B) {
	home := b.TempDir()
	b.Setenv("HOME", home)
	b.Setenv("XDG_CONFIG_HOME", "")

	repoRoot := b.TempDir()
	mustMkdir := func(p string) {
		if err := os.MkdirAll(p, 0o755); err != nil {
			b.Fatal(err)
		}
	}
	mustMkdir(filepath.Join(repoRoot, ".git"))
	mustMkdir(filepath.Join(repoRoot, ".rnix", "skills"))

	cwd := repoRoot
	for i := range 5 {
		cwd = filepath.Join(cwd, fmt.Sprintf("L%d", i))
	}
	mustMkdir(cwd)

	b.ResetTimer()
	for b.Loop() {
		result := ResolveSkillScopes(cwd, WithAncestorTraversal(true))
		if len(result) == 0 {
			b.Fatalf("expected at least 1 result")
		}
	}
}
