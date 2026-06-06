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

// MergeNamedSlice merges two []any slices whose elements are map[string]any
// keyed by keyField (e.g. "name"). Base order is preserved; override entries
// with the same key are deep-merged into base entries; new entries from
// override are appended. If either slice contains an element that is not a
// map[string]any with a string keyField, the function falls back to returning
// override as-is (preserving DeepMergeYAML's original slice-replace semantics).
func MergeNamedSlice(base, override []any, keyField string) []any {
	if len(override) == 0 {
		return base
	}
	if len(base) == 0 {
		return override
	}

	baseByKey := make(map[string]int, len(base))
	for i, item := range base {
		m, ok := item.(map[string]any)
		if !ok {
			return override
		}
		key, ok := m[keyField].(string)
		if !ok {
			return override
		}
		baseByKey[key] = i
	}

	result := make([]any, len(base))
	for i, item := range base {
		m := item.(map[string]any)
		cp := make(map[string]any, len(m))
		maps.Copy(cp, m)
		result[i] = cp
	}

	for _, item := range override {
		m, ok := item.(map[string]any)
		if !ok {
			return override
		}
		key, ok := m[keyField].(string)
		if !ok {
			return override
		}
		if idx, exists := baseByKey[key]; exists {
			result[idx] = DeepMergeYAML(result[idx].(map[string]any), m)
		} else {
			cp := make(map[string]any, len(m))
			maps.Copy(cp, m)
			result = append(result, cp)
		}
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

// ShadowResolveAll returns the full paths to ALL directories named name found
// across the given dirs (searched in order). Unlike ShadowResolve, which stops
// at the first match, this returns every matching layer so callers can merge
// layered config (e.g. project-over-global agent.yaml) instead of shadowing the
// lower-priority layers entirely.
func ShadowResolveAll(name string, dirs ...string) []string {
	var matches []string
	for _, dir := range dirs {
		candidate := filepath.Join(dir, name)
		info, err := os.Stat(candidate)
		if err == nil && info.IsDir() {
			matches = append(matches, candidate)
		}
	}
	return matches
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
