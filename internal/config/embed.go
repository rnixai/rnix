package config

import (
	"io/fs"
	"os"
	"path/filepath"
)

// ExtractEmbedded extracts files from an fs.FS (typically embed.FS) rooted at
// srcRoot into targetDir. Existing files are skipped (not overwritten).
// Directories are created as needed via os.MkdirAll.
func ExtractEmbedded(fsys fs.FS, srcRoot, targetDir string) error {
	return extractEmbedded(fsys, srcRoot, targetDir, false)
}

// ExtractEmbeddedForce extracts files from an fs.FS into targetDir, overwriting
// any existing files. Used for upgrade scenarios.
func ExtractEmbeddedForce(fsys fs.FS, srcRoot, targetDir string) error {
	return extractEmbedded(fsys, srcRoot, targetDir, true)
}

func extractEmbedded(fsys fs.FS, srcRoot, targetDir string, force bool) error {
	return fs.WalkDir(fsys, srcRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Strip the srcRoot prefix to get the relative path
		relPath, err := filepath.Rel(srcRoot, path)
		if err != nil {
			return err
		}
		// Skip the root itself
		if relPath == "." {
			return nil
		}

		targetPath := filepath.Join(targetDir, relPath)

		if d.IsDir() {
			return os.MkdirAll(targetPath, 0o755)
		}

		// Check if target file already exists
		if !force {
			if _, err := os.Stat(targetPath); err == nil {
				return nil // Skip existing files
			}
		}

		// Ensure parent directory exists
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}

		// Read from embedded FS and write to target
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		return os.WriteFile(targetPath, data, 0o644)
	})
}
