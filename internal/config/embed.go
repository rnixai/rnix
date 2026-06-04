package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"log"
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

const builtinHashesFile = ".builtin-hashes.json"

// ExtractEmbeddedSmart extracts files from an fs.FS into targetDir using
// conffile-style three-way merge. It tracks SHA-256 hashes in
// .builtin-hashes.json to distinguish "unchanged from previous embedded
// version" (safe to overwrite) from "user-modified" (skip with warning).
// Errors are logged but never returned — sync failure must not block daemon startup.
func ExtractEmbeddedSmart(fsys fs.FS, srcRoot, targetDir string) {
	hashPath := filepath.Join(targetDir, builtinHashesFile)
	hashes := loadHashFile(hashPath)
	updated := false

	_ = fs.WalkDir(fsys, srcRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(srcRoot, path)
		if err != nil {
			return err
		}
		if relPath == "." {
			return nil
		}
		targetPath := filepath.Join(targetDir, relPath)

		if d.IsDir() {
			_ = os.MkdirAll(targetPath, 0o755)
			return nil
		}

		embeddedData, err := fs.ReadFile(fsys, path)
		if err != nil {
			log.Printf("builtin-sync: read embedded %s: %v", relPath, err)
			return nil
		}
		embeddedHash := sha256Hex(embeddedData)

		targetData, readErr := os.ReadFile(targetPath)
		if readErr != nil && !os.IsNotExist(readErr) {
			log.Printf("builtin-sync: read %s: %v (skipping)", relPath, readErr)
			return nil
		}
		if readErr != nil {
			// Target doesn't exist — extract and record.
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				log.Printf("builtin-sync: mkdir for %s: %v", relPath, err)
				return nil
			}
			if err := os.WriteFile(targetPath, embeddedData, 0o644); err != nil {
				log.Printf("builtin-sync: write %s: %v", relPath, err)
				return nil
			}
			hashes[relPath] = embeddedHash
			updated = true
			return nil
		}

		targetHash := sha256Hex(targetData)
		oldHash, hasRecord := hashes[relPath]

		if !hasRecord {
			// First upgrade — no prior hash record.
			if targetHash == embeddedHash {
				hashes[relPath] = embeddedHash
				updated = true
			} else {
				log.Printf("builtin-sync: skipping %s (user-modified, no prior hash)", relPath)
				hashes[relPath] = targetHash
				updated = true
			}
			return nil
		}

		if targetHash != oldHash {
			if targetHash == embeddedHash {
				hashes[relPath] = embeddedHash
				updated = true
			} else {
				log.Printf("builtin-sync: skipping %s (user-modified)", relPath)
			}
			return nil
		}

		// Target matches old hash — user hasn't changed it. Overwrite with new embedded.
		if embeddedHash == oldHash {
			return nil // already up to date
		}
		if err := os.WriteFile(targetPath, embeddedData, 0o644); err != nil {
			log.Printf("builtin-sync: write %s: %v", relPath, err)
			return nil
		}
		hashes[relPath] = embeddedHash
		updated = true
		return nil
	})

	if updated {
		saveHashFile(hashPath, hashes)
	}
}

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func loadHashFile(path string) map[string]string {
	data, err := os.ReadFile(path)
	if err != nil {
		return make(map[string]string)
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return make(map[string]string)
	}
	return m
}

func saveHashFile(path string, hashes map[string]string) {
	data, err := json.MarshalIndent(hashes, "", "  ")
	if err != nil {
		log.Printf("builtin-sync: marshal hashes: %v", err)
		return
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		log.Printf("builtin-sync: write hashes: %v", err)
	}
}
