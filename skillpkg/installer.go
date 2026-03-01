package skillpkg

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gonewx/crux/skills"
)

const (
	// maxFileSize limits the size of a single extracted file (10 MB).
	maxFileSize = 10 << 20
	// maxTotalExtractSize limits the total extracted content (50 MB).
	maxTotalExtractSize = 50 << 20
)

// Installer orchestrates the skill installation flow: fetch -> verify -> extract -> validate -> register.
type Installer struct {
	client      *RegistryClient
	registry    *LocalRegistry
	skillLoader *skills.SkillLoader
	basePath    string // path to lib/skills/
}

// NewInstaller creates a new Installer.
func NewInstaller(client *RegistryClient, registry *LocalRegistry, skillLoader *skills.SkillLoader, basePath string) *Installer {
	return &Installer{
		client:      client,
		registry:    registry,
		skillLoader: skillLoader,
		basePath:    basePath,
	}
}

// Install installs a skill by name. It fetches from the registry, verifies integrity,
// extracts to the local skills directory, validates SKILL.md, and updates the registry.
func (inst *Installer) Install(name string, opts InstallOpts) (*InstallResult, error) {
	// Check if already installed
	existing, err := inst.registry.Get(name)
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

	// Extract to target directory
	targetDir := filepath.Join(inst.basePath, name)
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

	// Update registry
	entry := RegistryEntry{
		Name:        name,
		Version:     ver.Version,
		InstalledAt: time.Now().UTC(),
		Source:      "community",
		Checksum:    ver.Checksum,
	}
	if err := inst.registry.Add(entry); err != nil {
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

// Update checks for a newer version of an installed skill and updates it if available.
func (inst *Installer) Update(name string, opts UpdateOpts) (*UpdateResult, error) {
	existing, err := inst.registry.Get(name)
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

// UpdateAll checks and updates all installed community skills.
func (inst *Installer) UpdateAll(opts UpdateOpts) ([]UpdateResult, error) {
	entries, err := inst.registry.List()
	if err != nil {
		return nil, fmt.Errorf("list installed skills: %w", err)
	}

	results := make([]UpdateResult, 0)
	for _, entry := range entries {
		if entry.Source != "community" {
			continue
		}
		result, err := inst.Update(entry.Name, opts)
		if err != nil {
			continue
		}
		results = append(results, *result)
	}
	return results, nil
}

// ListAll returns a combined list of all skills available locally, aggregating
// both registry-tracked (community) skills and filesystem-only (builtin) skills.
func (inst *Installer) ListAll() ([]ListEntry, error) {
	// 1. Load registry entries
	regEntries, err := inst.registry.List()
	if err != nil {
		return nil, fmt.Errorf("list registry: %w", err)
	}

	// 2. Build map of registered names
	regMap := make(map[string]RegistryEntry)
	for _, e := range regEntries {
		regMap[e.Name] = e
	}

	// 3. Scan basePath directory for all skill subdirs
	dirEntries, err := os.ReadDir(inst.basePath)
	if err != nil {
		if os.IsNotExist(err) {
			return make([]ListEntry, 0), nil
		}
		return nil, fmt.Errorf("read skills directory: %w", err)
	}

	// 4. Build list entries from filesystem
	seen := make(map[string]bool)
	result := make([]ListEntry, 0)

	for _, d := range dirEntries {
		// Skip non-directories and hidden files (e.g. .registry.yaml)
		if !d.IsDir() || strings.HasPrefix(d.Name(), ".") {
			continue
		}

		name := d.Name()
		seen[name] = true

		// Try to load SKILL.md metadata for description
		var description string
		info, err := inst.skillLoader.LoadMetadata(name)
		if err != nil {
			// Directory exists but SKILL.md is invalid/missing.
			// If this skill is in the registry, still list it (with empty description).
			// Otherwise skip unregistered directories without valid SKILL.md.
			if regEntry, ok := regMap[name]; ok {
				result = append(result, ListEntry{
					Name:    name,
					Version: regEntry.Version,
					Path:    filepath.Join("lib/skills", name) + "/",
					Source:  regEntry.Source,
				})
			}
			continue
		}
		description = info.Manifest.Description

		entry := ListEntry{
			Name:        name,
			Path:        filepath.Join("lib/skills", name) + "/",
			Description: description,
		}

		// Use registry info if available
		if regEntry, ok := regMap[name]; ok {
			entry.Version = regEntry.Version
			entry.Source = regEntry.Source
		} else {
			entry.Source = "builtin"
		}

		result = append(result, entry)
	}

	// 5. Add registry entries whose directories are missing (registered but dir deleted)
	for name, regEntry := range regMap {
		if seen[name] {
			continue
		}
		result = append(result, ListEntry{
			Name:    name,
			Version: regEntry.Version,
			Path:    filepath.Join("lib/skills", name) + "/",
			Source:  regEntry.Source,
		})
	}

	// 6. Sort by name
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result, nil
}
