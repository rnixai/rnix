package config

import (
	"maps"
	"os"
	"path/filepath"
	"sort"
)

// DeepMergeYAML recursively merges override into base.
// When both values for a key are map[string]any, they are recursively merged.
// Slices are replaced (not appended). Scalars in override take precedence.
func DeepMergeYAML(base, override map[string]any) map[string]any {
	result := make(map[string]any, len(base)+len(override))

	// Copy base
	maps.Copy(result, base)

	// Merge override
	for k, ov := range override {
		bv, exists := result[k]
		if !exists {
			result[k] = ov
			continue
		}

		// Both are maps: recursive merge
		bMap, bOK := bv.(map[string]any)
		oMap, oOK := ov.(map[string]any)
		if bOK && oOK {
			result[k] = DeepMergeYAML(bMap, oMap)
			continue
		}

		// All other cases: override wins
		result[k] = ov
	}

	return result
}

// ShadowResolve returns the full path to the first directory named name
// found in the given dirs (searched in order). If name is not found as a
// subdirectory in any of dirs, it returns an empty string.
func ShadowResolve(name string, dirs ...string) string {
	for _, dir := range dirs {
		candidate := filepath.Join(dir, name)
		info, err := os.Stat(candidate)
		if err == nil && info.IsDir() {
			return candidate
		}
	}
	return ""
}

// ListMerged returns a deduplicated, sorted list of subdirectory names
// found across all given dirs. Only directories are included (files are
// skipped). Nonexistent dirs are silently ignored.
func ListMerged(dirs ...string) ([]string, error) {
	seen := make(map[string]bool)

	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				seen[entry.Name()] = true
			}
		}
	}

	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}
