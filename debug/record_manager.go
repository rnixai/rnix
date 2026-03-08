package debug

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/xsync"
)

// RecordManager manages recording sessions for multiple processes.
type RecordManager struct {
	baseDir   string
	recorders *xsync.SyncMap[types.PID, *Recorder]
}

// NewRecordManager creates a new RecordManager with the given base directory.
func NewRecordManager(baseDir string) *RecordManager {
	return &RecordManager{
		baseDir:   baseDir,
		recorders: xsync.NewSyncMap[types.PID, *Recorder](),
	}
}

// StartRecording starts a new recording session for the given PID.
// Returns the recording ID or an error if the PID is already being recorded.
func (m *RecordManager) StartRecording(pid types.PID, intent string) (string, error) {
	// Check-then-act with LoadOrStore to avoid creating orphan directories on disk.
	// We use a sentinel-then-replace pattern: store a nil sentinel first to claim the slot,
	// then create the Recorder and replace the sentinel.
	if _, exists := m.recorders.Load(pid); exists {
		return "", fmt.Errorf("record: PID %d is already being recorded", pid)
	}

	rec, err := NewRecorder(m.baseDir, pid, intent)
	if err != nil {
		return "", err
	}

	// Use LoadOrStore to handle concurrent start attempts
	if _, loaded := m.recorders.LoadOrStore(pid, rec); loaded {
		// Another goroutine started recording first; clean up the orphan
		_ = rec.Stop()
		removeRecordDir(rec.dir)
		return "", fmt.Errorf("record: PID %d is already being recorded", pid)
	}

	return rec.RecordID(), nil
}

// StopRecording stops the recording for the given PID.
func (m *RecordManager) StopRecording(pid types.PID) error {
	rec, ok := m.recorders.LoadAndDelete(pid)
	if !ok {
		return fmt.Errorf("record: PID %d is not being recorded", pid)
	}
	return rec.Stop()
}

// RecordEvent writes an event for the given PID's recording.
// If the PID has no active recording, the call is silently ignored.
func (m *RecordManager) RecordEvent(pid types.PID, event RecordEvent) error {
	rec, ok := m.recorders.Load(pid)
	if !ok {
		return nil // silent skip
	}
	return rec.WriteEvent(event)
}

// IsRecording returns whether the given PID has an active recording.
func (m *RecordManager) IsRecording(pid types.PID) bool {
	_, ok := m.recorders.Load(pid)
	return ok
}

// CloseAll stops and closes all active recordings.
func (m *RecordManager) CloseAll() {
	// Collect PIDs first to avoid deadlock (Range holds RLock, Delete needs Lock).
	var pids []types.PID
	m.recorders.Range(func(pid types.PID, rec *Recorder) bool {
		pids = append(pids, pid)
		return true
	})
	for _, pid := range pids {
		if rec, ok := m.recorders.LoadAndDelete(pid); ok {
			_ = rec.Close()
		}
	}
}

// GetEventCount returns the number of events recorded for a PID.
// Returns 0 if the PID is not being recorded.
func (m *RecordManager) GetEventCount(pid types.PID) uint64 {
	rec, ok := m.recorders.Load(pid)
	if !ok {
		return 0
	}
	return rec.EventCount()
}

// ListRecords scans the base directory and returns metadata for all recordings.
func (m *RecordManager) ListRecords() ([]RecordMetadata, error) {
	entries, err := os.ReadDir(m.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("record: read dir %s: %w", m.baseDir, err)
	}

	var records []RecordMetadata
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		metaPath := filepath.Join(m.baseDir, entry.Name(), "metadata.json")
		data, err := os.ReadFile(metaPath)
		if err != nil {
			continue // skip directories without metadata
		}
		var meta RecordMetadata
		if err := json.Unmarshal(data, &meta); err != nil {
			continue // skip invalid metadata
		}
		records = append(records, meta)
	}

	return records, nil
}

// FindRecord finds a recording directory by recordID.
// Returns the full directory path or an error if not found.
func (m *RecordManager) FindRecord(recordID string) (string, error) {
	dir := filepath.Join(m.baseDir, recordID)
	metaPath := filepath.Join(dir, "metadata.json")
	if _, err := os.Stat(metaPath); err != nil {
		return "", fmt.Errorf("record: recording %q not found", recordID)
	}
	return dir, nil
}

// LoadRecord loads a recording by recordID and returns a RecordReader.
func (m *RecordManager) LoadRecord(recordID string) (*RecordReader, error) {
	dir, err := m.FindRecord(recordID)
	if err != nil {
		return nil, err
	}
	return NewRecordReader(dir)
}

// BaseDir returns the base directory for recordings.
func (m *RecordManager) BaseDir() string {
	return m.baseDir
}

// removeRecordDir removes an orphan recording directory created during a race.
func removeRecordDir(dir string) {
	_ = os.RemoveAll(dir)
}
