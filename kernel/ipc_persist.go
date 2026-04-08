package kernel

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// PersistentMessage is the NDJSON-serialized form of an IPC message for disk persistence.
type PersistentMessage struct {
	FromPID   types.PID `json:"from_pid"`
	ToPID     types.PID `json:"to_pid"`
	Seq       int64     `json:"seq"`
	Data      string    `json:"data"` // base64-encoded
	CreatedAt time.Time `json:"created_at"`
	TraceID   string    `json:"trace_id,omitempty"`
	SpanID    string    `json:"span_id,omitempty"`
}

// ipcPersistDir returns the directory path for IPC persistence files.
func ipcPersistDir(baseDir, uuid string) string {
	return filepath.Join(baseDir, "data", "ipc", uuid)
}

// enablePersistence sets up NDJSON append-mode file for the message queue.
func enablePersistence(q *MessageQueue, baseDir, uuid string) error {
	if uuid == "" {
		return fmt.Errorf("enablePersistence: empty uuid")
	}
	dir := ipcPersistDir(baseDir, uuid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create ipc persist dir: %w", err)
	}
	path := filepath.Join(dir, "inbox.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open ipc persist file: %w", err)
	}
	q.mu.Lock()
	if q.persistFile != nil {
		q.persistFile.Close()
	}
	q.persistFile = f
	q.persistPath = path
	q.mu.Unlock()
	return nil
}

// persistMessage writes a single message as NDJSON to the persist file.
// Called under q.mu lock from enqueue.
func persistMessage(f *os.File, msg *Message) error {
	pm := PersistentMessage{
		FromPID:   msg.FromPID,
		ToPID:     msg.ToPID,
		Seq:       int64(msg.Seq),
		Data:      base64.StdEncoding.EncodeToString(msg.Data),
		CreatedAt: msg.CreatedAt,
		TraceID:   string(msg.TraceID),
		SpanID:    string(msg.SpanID),
	}
	line, err := json.Marshal(pm)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	_, err = f.Write(line)
	return err
}

// loadPersistedMessages reads all persisted messages from a UUID's inbox.jsonl.
func loadPersistedMessages(baseDir, uuid string) ([]*Message, error) {
	path := filepath.Join(ipcPersistDir(baseDir, uuid), "inbox.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read ipc persist file: %w", err)
	}

	var messages []*Message
	for _, line := range splitNDJSONLines(data) {
		if len(line) == 0 {
			continue
		}
		var pm PersistentMessage
		if err := json.Unmarshal(line, &pm); err != nil {
			continue // skip malformed lines
		}
		rawData, err := base64.StdEncoding.DecodeString(pm.Data)
		if err != nil {
			continue
		}
		messages = append(messages, &Message{
			FromPID:   pm.FromPID,
			ToPID:     pm.ToPID,
			Seq:       types.MsgSeq(pm.Seq),
			Data:      rawData,
			CreatedAt: pm.CreatedAt,
			TraceID:   types.TraceID(pm.TraceID),
			SpanID:    types.SpanID(pm.SpanID),
		})
	}
	return messages, nil
}

// removePersistedMessages removes the IPC persistence directory for a UUID.
func removePersistedMessages(baseDir, uuid string) error {
	if uuid == "" {
		return fmt.Errorf("removePersistedMessages: empty uuid")
	}
	dir := ipcPersistDir(baseDir, uuid)
	return os.RemoveAll(dir)
}

// splitNDJSONLines splits byte data into lines (handles \n line ending).
func splitNDJSONLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i := range data {
		if data[i] == '\n' {
			lines = append(lines, data[start:i])
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}
