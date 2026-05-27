package skillpkg

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rnixai/rnix/internal/config"
	"github.com/rnixai/rnix/skills"
)

const (
	// maxFileSize limits the size of a single extracted file (10 MB).
	maxFileSize = 10 << 20
	// maxTotalExtractSize limits the total extracted content (50 MB).
	maxTotalExtractSize = 50 << 20
)

// Installer orchestrates the skill installation flow: fetch -> verify -> extract -> validate -> register.
//
// Story 47.2: holds the priority-ordered slice of agentskills.io scopes plus a
// designated writeScope; each scope owns an independent LocalRegistry instance
// so install/update never silently cross scope boundaries.
type Installer struct {
	client      *RegistryClient
	skillLoader *skills.SkillLoader
	scopes      []config.ScopePath        // priority order: project/native > project/agents > user/native > user/agents
	writeScope  config.ScopePath          // Install/UpdateAll write target (Update preserves origin)
	registries  map[string]*LocalRegistry // key = ScopePath.Path (absolute)
}

// NewInstaller creates a new Installer wired to a set of agentskills.io scopes.
// Story 47.2 AC1: scopes preserved in priority order; writeScope governs install
// target; one LocalRegistry per scope path is created eagerly to keep ListAll
// and Install/Update logic uniform.
func NewInstaller(client *RegistryClient, skillLoader *skills.SkillLoader, scopes []config.ScopePath, writeScope config.ScopePath) *Installer {
	registries := make(map[string]*LocalRegistry, len(scopes))
	for _, sp := range scopes {
		registries[sp.Path] = NewLocalRegistry(sp.Path)
	}
	// Safety net: writeScope not in scopes (e.g. CLI bug) — register it lazily
	// so Install can proceed; ListAll will then surface its registry too.
	if writeScope.Path != "" {
		if _, ok := registries[writeScope.Path]; !ok {
			registries[writeScope.Path] = NewLocalRegistry(writeScope.Path)
		}
	}
	return &Installer{
		client:      client,
		skillLoader: skillLoader,
		scopes:      scopes,
		writeScope:  writeScope,
		registries:  registries,
	}
}

var errNoScopes = errors.New("no skill scopes resolved; check ResolveSkillScopes input")

// resolveWriteScope returns the registry for inst.writeScope, lazily creating
// it if missing. AC1 dev-decision: degrade gracefully rather than crash.
func (inst *Installer) resolveWriteScope() (*LocalRegistry, error) {
	if inst.writeScope.Path == "" {
		return nil, fmt.Errorf("installer has no writeScope configured")
	}
	reg, ok := inst.registries[inst.writeScope.Path]
	if !ok {
		reg = NewLocalRegistry(inst.writeScope.Path)
		inst.registries[inst.writeScope.Path] = reg
	}
	return reg, nil
}

// findExistingEntry searches every scope's registry for name. Returns the entry
// + owning scope path; nil if not found. AC5 easy-error防护: Install's
// "already installed" check must consider all scopes (not just writeScope).
func (inst *Installer) findExistingEntry(name string) (*RegistryEntry, string, error) {
	for _, sp := range inst.scopes {
		reg := inst.registries[sp.Path]
		if reg == nil {
			continue
		}
		entry, err := reg.Get(name)
		if err != nil {
			return nil, "", fmt.Errorf("check registry at %q: %w", sp.Path, err)
		}
		if entry != nil {
			return entry, sp.Path, nil
		}
	}
	return nil, "", nil
}

// Install installs a skill by name into the writeScope. AC5: fetch -> verify ->
// extract under writeScope -> validate via SkillLoader -> register in writeScope's
// LocalRegistry. The "already installed" check considers ALL scopes (so a user
// scope copy blocks a duplicate project-scope install).
func (inst *Installer) Install(name string, opts InstallOpts) (*InstallResult, error) {
	if len(inst.scopes) == 0 {
		return nil, errNoScopes
	}
	writeReg, err := inst.resolveWriteScope()
	if err != nil {
		return nil, err
	}

	existing, _, err := inst.findExistingEntry(name)
	if err != nil {
		return nil, fmt.Errorf("check existing installation: %w", err)
	}
	fresh := existing == nil

	if existing != nil && !opts.Force {
		return nil, &AlreadyInstalledError{
			Name:    name,
			Version: existing.Version,
		}
	}

	// Check if skill exists on filesystem (in any scope) but not registry — e.g.
	// a builtin or hand-placed skill. SkillLoader.LoadMetadata walks scopes per
	// ShadowResolve. Use raw LoadMetadata (not WithDiag) since we only care
	// whether *something* loadable exists, not its diagnostics.
	if existing == nil && !opts.Force {
		for _, sp := range inst.scopes {
			skillDir := filepath.Join(sp.Path, name)
			if info, statErr := os.Stat(skillDir); statErr == nil && info.IsDir() {
				if _, loadErr := inst.skillLoader.LoadMetadata(name); loadErr == nil {
					return nil, &AlreadyInstalledError{Name: name, Version: "local"}
				}
				break
			}
		}
	}

	// Resolve latest version
	ver, err := inst.client.Resolve(name)
	if err != nil {
		return nil, fmt.Errorf("resolve version: %w", err)
	}

	// Fetch package
	pkg, err := inst.client.Fetch(name, ver)
	if err != nil {
		return nil, fmt.Errorf("fetch package: %w", err)
	}

	// Verify checksum
	if err := inst.client.Verify(pkg); err != nil {
		return nil, fmt.Errorf("verify package: %w", err)
	}

	// Extract to writeScope directory
	targetDir := filepath.Join(inst.writeScope.Path, name)
	if err := inst.extract(pkg, targetDir); err != nil {
		// Rollback: remove partially extracted files
		os.RemoveAll(targetDir)
		return nil, fmt.Errorf("extract package: %w", err)
	}

	// Validate SKILL.md using existing SkillLoader
	if _, err := inst.skillLoader.LoadMetadata(name); err != nil {
		// Rollback: remove extracted files
		os.RemoveAll(targetDir)
		return nil, fmt.Errorf("validate installed skill: %w", err)
	}

	// Update writeScope registry
	entry := RegistryEntry{
		Name:        name,
		Version:     ver.Version,
		InstalledAt: time.Now().UTC(),
		Source:      "community",
		Checksum:    ver.Checksum,
	}
	if err := writeReg.Add(entry); err != nil {
		return nil, fmt.Errorf("update registry: %w", err)
	}

	return &InstallResult{
		Name:    name,
		Version: ver.Version,
		Fresh:   fresh,
	}, nil
}

// extract unpacks a .tar.gz skill package into the target directory.
func (inst *Installer) extract(pkg *SkillPackage, targetDir string) error {
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fmt.Errorf("create target directory: %w", err)
	}

	gr, err := gzip.NewReader(bytes.NewReader(pkg.Data))
	if err != nil {
		return fmt.Errorf("open gzip: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	var totalSize int64
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar entry: %w", err)
		}

		// Reject entries exceeding per-file size limit
		if header.Size > maxFileSize {
			return fmt.Errorf("file %s exceeds max size (%d > %d bytes)", header.Name, header.Size, maxFileSize)
		}
		totalSize += header.Size
		if totalSize > maxTotalExtractSize {
			return fmt.Errorf("total extracted size exceeds limit (%d bytes)", maxTotalExtractSize)
		}

		// Clean path and prevent directory traversal
		cleanName := filepath.Clean(header.Name)
		if strings.HasPrefix(cleanName, "..") || filepath.IsAbs(cleanName) {
			return fmt.Errorf("invalid tar entry path: %s", header.Name)
		}

		target := filepath.Join(targetDir, cleanName)

		// Verify the resolved path stays within targetDir
		absTarget, err := filepath.Abs(target)
		if err != nil {
			return fmt.Errorf("resolve path: %w", err)
		}
		absDir, err := filepath.Abs(targetDir)
		if err != nil {
			return fmt.Errorf("resolve target dir: %w", err)
		}
		if !strings.HasPrefix(absTarget, absDir+string(filepath.Separator)) && absTarget != absDir {
			return fmt.Errorf("tar entry escapes target directory: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("create directory %s: %w", cleanName, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("create parent directory: %w", err)
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode)&0o755)
			if err != nil {
				return fmt.Errorf("create file %s: %w", cleanName, err)
			}
			if _, err := io.Copy(f, io.LimitReader(tr, maxFileSize+1)); err != nil {
				f.Close()
				return fmt.Errorf("write file %s: %w", cleanName, err)
			}
			f.Close()
		case tar.TypeSymlink, tar.TypeLink:
			return fmt.Errorf("unsupported tar entry type (symlink/hardlink): %s", header.Name)
		}
	}

	return nil
}

// AlreadyInstalledError indicates a skill is already installed.
type AlreadyInstalledError struct {
	Name    string
	Version string
}

func (e *AlreadyInstalledError) Error() string {
	return fmt.Sprintf("skill %q is already installed (version %s), use --force to overwrite", e.Name, e.Version)
}

// Update checks for a newer version of an installed skill and updates it
// in-place at its current scope. Story 47.2 AC5: never silently migrates a
// skill across scopes — when foo lives in user/native and writeScope is
// project/native, Update writes back to user/native.
func (inst *Installer) Update(name string, opts UpdateOpts) (*UpdateResult, error) {
	if len(inst.scopes) == 0 {
		return nil, errNoScopes
	}
	existing, originPath, err := inst.findExistingEntry(name)
	if err != nil {
		return nil, fmt.Errorf("check installed skill: %w", err)
	}
	if existing == nil {
		return nil, fmt.Errorf("skill %q is not installed", name)
	}

	ver, err := inst.client.Resolve(name)
	if err != nil {
		return nil, fmt.Errorf("resolve latest version: %w", err)
	}

	if existing.Version == ver.Version && !opts.Force {
		return &UpdateResult{
			Name:       name,
			OldVersion: existing.Version,
			NewVersion: ver.Version,
			Updated:    false,
		}, nil
	}

	// Pivot writeScope to the origin so Install writes back to the same scope.
	originScope, ok := inst.findScopeByPath(originPath)
	if !ok {
		return nil, fmt.Errorf("internal: origin scope %q not in scopes slice", originPath)
	}
	savedWrite := inst.writeScope
	inst.writeScope = originScope
	defer func() { inst.writeScope = savedWrite }()

	oldVersion := existing.Version
	_, err = inst.Install(name, InstallOpts{Force: true})
	if err != nil {
		return nil, fmt.Errorf("update %s: %w", name, err)
	}

	return &UpdateResult{
		Name:       name,
		OldVersion: oldVersion,
		NewVersion: ver.Version,
		Updated:    true,
	}, nil
}

// findScopeByPath returns the ScopePath whose Path matches p (absolute) and a
// found bool.
func (inst *Installer) findScopeByPath(p string) (config.ScopePath, bool) {
	for _, sp := range inst.scopes {
		if sp.Path == p {
			return sp, true
		}
	}
	if inst.writeScope.Path == p {
		return inst.writeScope, true
	}
	return config.ScopePath{}, false
}

// UpdateAll checks and updates all installed community skills across every
// scope's registry. Story 47.2 AC5: each Update call writes back to its
// originating scope (no silent migration).
func (inst *Installer) UpdateAll(opts UpdateOpts) ([]UpdateResult, error) {
	if len(inst.scopes) == 0 {
		return nil, errNoScopes
	}

	results := make([]UpdateResult, 0)
	seen := make(map[string]bool)
	for _, sp := range inst.scopes {
		reg := inst.registries[sp.Path]
		if reg == nil {
			continue
		}
		entries, err := reg.List()
		if err != nil {
			return nil, fmt.Errorf("list installed skills in %q: %w", sp.Path, err)
		}
		for _, entry := range entries {
			if entry.Source != "community" {
				continue
			}
			if seen[entry.Name] {
				continue
			}
			seen[entry.Name] = true
			result, err := inst.Update(entry.Name, opts)
			if err != nil {
				continue
			}
			results = append(results, *result)
		}
	}
	return results, nil
}

// ListAll merges every scope's skill directories and registry entries into a
// single deduplicated list. Story 47.2 AC2 / AC3 / AC4 / AC7 / AC8:
//   - high-priority scope wins on name collision; losing entries are emitted as
//     ShadowWarning in LoadDiagnostics.Warnings.
//   - ListEntry.Path is the absolute path under the winning scope.
//   - Lenient validation failures from SkillLoader (missing description / unparseable
//     YAML / missing name) are recorded in LoadDiagnostics.Skipped without
//     surfacing as top-level errors.
//   - Lenient warn-but-load conditions (name mismatch / overlong name) populate
//     LoadDiagnostics.Lenient.
//   - Corrupt-community parity: when SKILL.md is broken but a registry entry
//     exists, the entry is still listed (Epic 8 behavior) AND a SkipEntry is
//     emitted (dual signal).
func (inst *Installer) ListAll() ([]ListEntry, LoadDiagnostics, error) {
	var diag LoadDiagnostics
	if len(inst.scopes) == 0 {
		return nil, diag, errNoScopes
	}

	// Step 1: discover all (scopeIdx, name) candidates from filesystem + registries.
	winners := make(map[string]int)              // name -> winning scopeIdx
	allByName := make(map[string][]int)          // name -> all scope indices where it appears
	registeredAtScope := make(map[string]map[int]bool)

	// Reusable helper to register a candidate.
	register := func(scopeIdx int, name string) {
		existing, ok := winners[name]
		if !ok || scopeIdx < existing {
			winners[name] = scopeIdx
		}
		seenSet, ok := registeredAtScope[name]
		if !ok {
			seenSet = make(map[int]bool)
			registeredAtScope[name] = seenSet
		}
		if !seenSet[scopeIdx] {
			seenSet[scopeIdx] = true
			allByName[name] = append(allByName[name], scopeIdx)
		}
	}

	for idx, sp := range inst.scopes {
		dirEntries, err := os.ReadDir(sp.Path)
		if err != nil {
			if os.IsNotExist(err) || os.IsPermission(err) {
				// Silent skip per AC2 (47.1 degradation philosophy).
				continue
			}
			return nil, diag, fmt.Errorf("read skills directory %q: %w", sp.Path, err)
		}
		for _, d := range dirEntries {
			if !d.IsDir() || strings.HasPrefix(d.Name(), ".") {
				continue
			}
			register(idx, d.Name())
		}
		// Registry entries also participate in dedup so cross-scope duplicates
		// (a name installed in both project and user) raise warnings.
		reg := inst.registries[sp.Path]
		if reg == nil {
			continue
		}
		regEntries, err := reg.List()
		if err != nil {
			return nil, diag, fmt.Errorf("list registry in %q: %w", sp.Path, err)
		}
		for _, e := range regEntries {
			register(idx, e.Name)
		}
	}

	// Step 2: build result entries from winning candidates; collect shadow warnings.
	result := make([]ListEntry, 0, len(winners))
	for name, winIdx := range winners {
		winningScope := inst.scopes[winIdx]
		winningDir := filepath.Join(winningScope.Path, name)

		// Try to load SKILL.md metadata for description.
		info, lenient, loadErr := inst.skillLoader.LoadMetadataWithDiag(name)

		// Accumulate warn-but-load diagnostics regardless of load outcome.
		for _, w := range lenient {
			diag.Lenient = append(diag.Lenient, LenientWarning{
				Path:   w.Path,
				Field:  w.Field,
				Reason: w.Reason,
				Detail: w.Detail,
			})
		}

		// Registry lookup at the winning scope determines version/source.
		var regEntry *RegistryEntry
		if reg, ok := inst.registries[winningScope.Path]; ok && reg != nil {
			if e, err := reg.Get(name); err == nil && e != nil {
				regEntry = e
			}
		}

		if loadErr != nil {
			// Lenient skip → diagnostics.Skipped, but preserve corrupt-community
			// dual-signal (Epic 8 behavior + dev-notes §5).
			var lerr *skills.LenientSkipError
			if errors.As(loadErr, &lerr) {
				diag.Skipped = append(diag.Skipped, SkipEntry{
					Path:   lerr.Path,
					Reason: lerr.Reason,
				})
			}
			if regEntry != nil {
				// Corrupt-community parity: still list, empty description.
				result = append(result, ListEntry{
					Name:      name,
					Version:   regEntry.Version,
					Path:      winningDir,
					Source:    regEntry.Source,
					Scope:     winningScope.Scope.String(),
					Namespace: winningScope.Namespace.String(),
				})
				// Only emit shadow warnings when the entry actually appears in
				// the result list; shadowing a non-listed skill is misleading.
				emitShadowWarnings(&diag, name, winningScope, winningDir, allByName[name], winIdx, inst.scopes)
			}
			continue
		}

		entry := ListEntry{
			Name:        name,
			Path:        winningDir,
			Description: info.Manifest.Description,
			Scope:       winningScope.Scope.String(),
			Namespace:   winningScope.Namespace.String(),
		}
		if regEntry != nil {
			entry.Version = regEntry.Version
			entry.Source = regEntry.Source
		} else {
			entry.Source = "builtin"
		}
		result = append(result, entry)

		emitShadowWarnings(&diag, name, winningScope, winningDir, allByName[name], winIdx, inst.scopes)
	}

	// Step 3: sort by name (deterministic output).
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })

	return result, diag, nil
}

// emitShadowWarnings appends one ShadowWarning per non-winning candidate scope.
// The resulting order matches scope priority of the SHADOWED entry (AC3 spec).
func emitShadowWarnings(
	diag *LoadDiagnostics,
	name string,
	winningScope config.ScopePath,
	winningDir string,
	scopeIdxs []int,
	winIdx int,
	scopes []config.ScopePath,
) {
	shadowed := make([]int, 0, len(scopeIdxs))
	for _, idx := range scopeIdxs {
		if idx == winIdx {
			continue
		}
		shadowed = append(shadowed, idx)
	}
	sort.Ints(shadowed)
	for _, idx := range shadowed {
		sp := scopes[idx]
		diag.Warnings = append(diag.Warnings, ShadowWarning{
			SkillName:     name,
			WinningPath:   winningDir,
			WinningScope:  winningScope.Scope.String(),
			WinningNS:     winningScope.Namespace.String(),
			ShadowedPath:  filepath.Join(sp.Path, name),
			ShadowedScope: sp.Scope.String(),
			ShadowedNS:    sp.Namespace.String(),
		})
	}
}
