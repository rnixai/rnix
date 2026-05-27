package skillpkg

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rnixai/rnix/internal/config"
	"github.com/rnixai/rnix/skills"
)

// ============================================================
// ATDD RED PHASE — Story 47.2: Installer 多 scope 改造
//
// 覆盖 AC1 / AC2 / AC4：
//   - AC1: Installer 字段重构（scopes/writeScope/registries），新 NewInstaller 签名
//   - AC2: ListAll 多 scope 合并 + Shadow dedup（高优先级 scope 胜出）
//   - AC4: ListEntry 新字段（Scope / Namespace / Shadowed）
//
// 本文件引用尚不存在的：
//   * Installer.scopes / writeScope / registries 字段
//   * NewInstaller(client, loader, scopes, writeScope) 四参签名
//   * ListAll() 三值返回 ([]ListEntry, LoadDiagnostics, error)
//   * ListEntry.Scope / Namespace / Shadowed 字段
//
// RED → GREEN: 完成 AC1+AC2+AC4 实现后，本文件应编译且全部测试通过。
// ============================================================

// --- helper: setupFourScopes ----------------------------------------------

// fourScopeDirs holds the absolute paths for the four agentskills.io scope
// roots used by multi-scope tests.
type fourScopeDirs struct {
	projectRnix   string // <project>/.rnix/skills
	projectAgents string // <project>/.agents/skills
	userRnix      string // $XDG_CONFIG_HOME/rnix/skills
	userAgents    string // $HOME/.agents/skills
	projectDir    string // <project>
	homeDir       string // $HOME
}

// setupFourScopes creates the four agentskills.io scope directories under
// fresh temporary roots and returns:
//   - the ordered []config.ScopePath (priority: project-rnix > project-agents
//     > user-rnix > user-agents),
//   - a struct with absolute path fields for fixture authoring,
//
// It also points $HOME and $XDG_CONFIG_HOME at temp dirs via t.Setenv so the
// test is hermetic from the developer's real home directory.
func setupFourScopes(t *testing.T) ([]config.ScopePath, *fourScopeDirs) {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	projectDir := t.TempDir()

	dirs := &fourScopeDirs{
		projectRnix:   filepath.Join(projectDir, ".rnix", "skills"),
		projectAgents: filepath.Join(projectDir, ".agents", "skills"),
		userRnix:      filepath.Join(home, ".config", "rnix", "skills"),
		userAgents:    filepath.Join(home, ".agents", "skills"),
		projectDir:    projectDir,
		homeDir:       home,
	}

	for _, p := range []string{dirs.projectRnix, dirs.projectAgents, dirs.userRnix, dirs.userAgents} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatalf("mkdir %q: %v", p, err)
		}
	}

	scopes := []config.ScopePath{
		{Path: dirs.projectRnix, Scope: config.SkillScopeProject, Namespace: config.SkillNamespaceRnix},
		{Path: dirs.projectAgents, Scope: config.SkillScopeProject, Namespace: config.SkillNamespaceAgents},
		{Path: dirs.userRnix, Scope: config.SkillScopeUser, Namespace: config.SkillNamespaceRnix},
		{Path: dirs.userAgents, Scope: config.SkillScopeUser, Namespace: config.SkillNamespaceAgents},
	}

	return scopes, dirs
}

// allScopePaths is a small accessor to project four-scope dirs into a path
// slice in priority order (for SkillLoader wiring).
func (d *fourScopeDirs) allScopePaths() []string {
	return []string{d.projectRnix, d.projectAgents, d.userRnix, d.userAgents}
}

// --- 47.2-UNIT-001: [P0] Installer struct fields + NewInstaller signature (AC1) ---

func TestInstaller_NewInstaller_StructFields(t *testing.T) {
	// Given: 4-scope fixture
	scopes, dirs := setupFourScopes(t)
	srv, _, _ := setupMockRegistry(t, "dummy")
	client := NewRegistryClient(srv.URL, srv.Client())
	skillLoader := skills.NewSkillLoader(dirs.allScopePaths())

	// When: constructing Installer with new 4-arg signature
	writeScope := scopes[0] // project / native
	installer := NewInstaller(client, skillLoader, scopes, writeScope)

	// Then: (a) scopes preserved in priority order
	if got := len(installer.scopes); got != 4 {
		t.Fatalf("len(scopes) = %d, want 4", got)
	}
	for i, sp := range installer.scopes {
		if sp.Path != scopes[i].Path {
			t.Errorf("scopes[%d].Path = %q, want %q", i, sp.Path, scopes[i].Path)
		}
	}

	// (b) registries map: one LocalRegistry per scope, keyed by absolute path
	if got := len(installer.registries); got != 4 {
		t.Fatalf("len(registries) = %d, want 4", got)
	}
	for _, sp := range scopes {
		reg, ok := installer.registries[sp.Path]
		if !ok {
			t.Errorf("registries missing key %q", sp.Path)
			continue
		}
		// (c) each registry roots at its own scope path (registry.basePath
		// is unexported — verify by writing/reading an entry per AC5 contract).
		if reg == nil {
			t.Errorf("registry for %q is nil", sp.Path)
		}
	}

	// (d) writeScope correctly captured
	if installer.writeScope.Path != scopes[0].Path {
		t.Errorf("writeScope.Path = %q, want %q", installer.writeScope.Path, scopes[0].Path)
	}
}

// --- 47.2-UNIT-002: [P0] Empty scopes constructor + fail-fast on ListAll (AC1) ---

func TestInstaller_NewInstaller_EmptyScopes_FailFast(t *testing.T) {
	srv, _, _ := setupMockRegistry(t, "dummy")
	client := NewRegistryClient(srv.URL, srv.Client())
	skillLoader := skills.NewSkillLoader(nil)

	// Given: zero scopes
	var scopes []config.ScopePath

	// When: constructor should succeed (lazy validation)
	installer := NewInstaller(client, skillLoader, scopes, config.ScopePath{})
	if installer == nil {
		t.Fatal("NewInstaller returned nil for empty scopes; expected non-nil with deferred validation")
	}

	// Then: ListAll must return an error (fail-fast on operational call)
	_, _, err := installer.ListAll()
	if err == nil {
		t.Fatal("ListAll with empty scopes: expected error, got nil")
	}
}

// --- 47.2-INT-001: [P0] ListAll multi-scope merge + Shadow dedup (AC2) ---

func TestInstaller_ListAll_MultiScopeMerge(t *testing.T) {
	// Given: 4 scopes, each with one unique skill + one shared skill named "shared"
	scopes, dirs := setupFourScopes(t)

	// Each scope gets one unique skill...
	createTestSkillDir(t, dirs.projectRnix, "rnix-only", "rnix-native exclusive")
	createTestSkillDir(t, dirs.projectAgents, "agents-only", "agents-project exclusive")
	createTestSkillDir(t, dirs.userRnix, "user-rnix-only", "user-rnix exclusive")
	createTestSkillDir(t, dirs.userAgents, "user-agents-only", "user-agents exclusive")

	// ...plus a shared name across all four — different description per scope
	// so we can verify which one wins.
	createTestSkillDir(t, dirs.projectRnix, "shared", "winner from project/native")
	createTestSkillDir(t, dirs.projectAgents, "shared", "loser from project/agents")
	createTestSkillDir(t, dirs.userRnix, "shared", "loser from user/native")
	createTestSkillDir(t, dirs.userAgents, "shared", "loser from user/agents")

	srv, _, _ := setupMockRegistry(t, "dummy")
	client := NewRegistryClient(srv.URL, srv.Client())
	skillLoader := skills.NewSkillLoader(dirs.allScopePaths())
	installer := NewInstaller(client, skillLoader, scopes, scopes[0])

	// When: ListAll
	entries, _, err := installer.ListAll()
	if err != nil {
		t.Fatalf("ListAll failed: %v", err)
	}

	// Then: result has 5 entries (4 unique + 1 winning shared, others shadowed)
	if len(entries) != 5 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name)
		}
		t.Fatalf("len(entries) = %d, want 5; got: %v", len(entries), names)
	}

	// Build name → entry map
	byName := make(map[string]ListEntry, len(entries))
	for _, e := range entries {
		byName[e.Name] = e
	}

	// shared must come from project/native (highest priority scope)
	sharedEntry, ok := byName["shared"]
	if !ok {
		t.Fatal("expected entry 'shared' in result")
	}
	if sharedEntry.Description != "winner from project/native" {
		t.Errorf("shared.Description = %q, want %q (winning scope)",
			sharedEntry.Description, "winner from project/native")
	}
	wantPath := filepath.Join(dirs.projectRnix, "shared")
	if sharedEntry.Path != wantPath {
		t.Errorf("shared.Path = %q, want absolute path %q", sharedEntry.Path, wantPath)
	}

	// All four exclusive skills must appear
	for _, n := range []string{"rnix-only", "agents-only", "user-rnix-only", "user-agents-only"} {
		if _, ok := byName[n]; !ok {
			t.Errorf("missing expected entry %q", n)
		}
	}
}

// --- 47.2-INT-002: [P0] ListAll silently skips ENOENT scopes (AC2) ---

func TestInstaller_ListAll_MissingScopesSilent(t *testing.T) {
	// Given: 4 scopes declared but only project/native actually exists on disk
	scopes, dirs := setupFourScopes(t)

	// Remove the other three scope directories to simulate non-existence.
	for _, p := range []string{dirs.projectAgents, dirs.userRnix, dirs.userAgents} {
		if err := os.RemoveAll(p); err != nil {
			t.Fatalf("remove %q: %v", p, err)
		}
	}

	createTestSkillDir(t, dirs.projectRnix, "lone", "only project-native survives")

	srv, _, _ := setupMockRegistry(t, "dummy")
	client := NewRegistryClient(srv.URL, srv.Client())
	skillLoader := skills.NewSkillLoader([]string{dirs.projectRnix})
	installer := NewInstaller(client, skillLoader, scopes, scopes[0])

	// When: ListAll
	entries, _, err := installer.ListAll()

	// Then: no error (ENOENT silent), one entry returned
	if err != nil {
		t.Fatalf("ListAll: ENOENT should be silent, got error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	if entries[0].Name != "lone" {
		t.Errorf("entries[0].Name = %q, want %q", entries[0].Name, "lone")
	}
}

// --- 47.2-UNIT-003: [P1] ListEntry new fields populated correctly (AC4) ---

func TestListEntry_NewFields(t *testing.T) {
	// Given: a single project/native skill
	scopes, dirs := setupFourScopes(t)
	createTestSkillDir(t, dirs.projectRnix, "code-analysis", "Analyze code quality")

	srv, _, _ := setupMockRegistry(t, "dummy")
	client := NewRegistryClient(srv.URL, srv.Client())
	skillLoader := skills.NewSkillLoader([]string{dirs.projectRnix})
	installer := NewInstaller(client, skillLoader, scopes, scopes[0])

	// When: ListAll
	entries, _, err := installer.ListAll()
	if err != nil {
		t.Fatalf("ListAll failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}

	e := entries[0]

	// Then: Scope / Namespace / Shadowed are populated per AC4 contract
	if e.Scope != "project" {
		t.Errorf("entry.Scope = %q, want %q", e.Scope, "project")
	}
	if e.Namespace != "native" {
		t.Errorf("entry.Namespace = %q, want %q", e.Namespace, "native")
	}
	if e.Shadowed {
		t.Errorf("entry.Shadowed = true; winning entries must always have Shadowed=false")
	}
	// Path must be absolute (AC2 + AC4 alignment)
	if !filepath.IsAbs(e.Path) {
		t.Errorf("entry.Path = %q is not absolute; AC2 requires absolute paths", e.Path)
	}
}

// --- 47.2-UNIT-004: [P1] ListEntry Scope/Namespace per scope of origin (AC4) ---

func TestListEntry_NewFields_PerScopeMapping(t *testing.T) {
	scopes, dirs := setupFourScopes(t)
	// Description must be non-empty per AC7 lenient skip; the test exercises
	// per-scope Scope/Namespace mapping rather than empty-description handling.
	createTestSkillDir(t, dirs.projectRnix, "p-native", "project native skill")
	createTestSkillDir(t, dirs.projectAgents, "p-agents", "project agents skill")
	createTestSkillDir(t, dirs.userRnix, "u-native", "user native skill")
	createTestSkillDir(t, dirs.userAgents, "u-agents", "user agents skill")

	srv, _, _ := setupMockRegistry(t, "dummy")
	client := NewRegistryClient(srv.URL, srv.Client())
	skillLoader := skills.NewSkillLoader(dirs.allScopePaths())
	installer := NewInstaller(client, skillLoader, scopes, scopes[0])

	entries, _, err := installer.ListAll()
	if err != nil {
		t.Fatalf("ListAll failed: %v", err)
	}

	want := map[string]struct {
		scope     string
		namespace string
	}{
		"p-native": {"project", "native"},
		"p-agents": {"project", "agents"},
		"u-native": {"user", "native"},
		"u-agents": {"user", "agents"},
	}

	got := make(map[string]struct {
		scope     string
		namespace string
	}, len(entries))
	for _, e := range entries {
		got[e.Name] = struct {
			scope     string
			namespace string
		}{e.Scope, e.Namespace}
	}

	for name, w := range want {
		g, ok := got[name]
		if !ok {
			t.Errorf("missing entry %q", name)
			continue
		}
		if g.scope != w.scope || g.namespace != w.namespace {
			t.Errorf("entry %q: scope=%q namespace=%q, want scope=%q namespace=%q",
				name, g.scope, g.namespace, w.scope, w.namespace)
		}
	}
}
