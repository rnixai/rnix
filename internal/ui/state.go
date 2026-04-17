package ui

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// UIState is the persisted UI preferences read/written to
// `$XDG_CONFIG_HOME/rnix/ui-state.json` or `$HOME/.config/rnix/ui-state.json`.
// Story 36-4 introduces the first field; the struct is extensible for future
// UI preferences (themes, density, layout).
type UIState struct {
	// TimelineSortMigrationShown records whether the one-time升序提示 was shown.
	TimelineSortMigrationShown bool `json:"timeline_sort_migration_shown"`
}

// uiStatePath resolves the absolute path of ui-state.json.
func uiStatePath() (string, error) {
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		return filepath.Join(dir, "rnix", "ui-state.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "rnix", "ui-state.json"), nil
}

// LoadUIState reads the persisted UI state. Returns an empty state (not nil)
// when the file does not exist. Unparseable files are treated as absent so
// that a malformed state file never blocks the UI.
func LoadUIState() (*UIState, error) {
	path, err := uiStatePath()
	if err != nil {
		return &UIState{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &UIState{}, nil
		}
		return &UIState{}, err
	}
	s := &UIState{}
	if err := json.Unmarshal(data, s); err != nil {
		return &UIState{}, err
	}
	return s, nil
}

// SaveUIState atomically persists the UI state. Writes a sibling temp file
// and renames it into place so partial writes cannot corrupt the config.
// The parent directory is created with 0o755 if missing.
func SaveUIState(s *UIState) error {
	if s == nil {
		return errors.New("nil UIState")
	}
	path, err := uiStatePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "ui-state-*.json.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, path)
}
