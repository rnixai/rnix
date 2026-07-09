package kernel

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// RawWriter appends vfs.RawCapture records as NDJSON to
// <baseDir>/steps/<uuid>/raw.jsonl (Story 56.1 AC#4). Mirrors EventWriter
// (kernel/event_writer.go) — bufio + mu + O_APPEND, one Flush per write so
// historical reads observe records immediately. Close is idempotent so the
// reap path (kernel/reap.go) can call it without double-checking attachment.
//
// Lazy file creation (review patch P1): the on-disk file is created on the
// first successful WriteRaw, not at NewRawWriter. Story 56.1 has zero driver
// implementing rawCaptureDriver, so eagerly creating raw.jsonl would litter
// every <uuid>/ directory with an empty file. Lazy creation defers that cost
// until at least one record is actually written.
type RawWriter struct {
	mu       sync.Mutex
	baseDir  string
	procUUID string
	dir      string
	path     string
	file     *os.File
	writer   *bufio.Writer
	closed   bool
}

// NewRawWriter prepares a RawWriter pointing at <baseDir>/steps/<procUUID>/raw.jsonl.
// The on-disk file is NOT created here — first WriteRaw call performs MkdirAll
// + OpenFile (review patch P1, lazy creation).
func NewRawWriter(baseDir string, procUUID string) (*RawWriter, error) {
	if baseDir == "" {
		return nil, fmt.Errorf("raw_writer: baseDir is empty")
	}
	if procUUID == "" {
		return nil, fmt.Errorf("raw_writer: procUUID is empty")
	}
	dir := filepath.Join(baseDir, "steps", procUUID)
	path := filepath.Join(dir, "raw.jsonl")
	return &RawWriter{
		baseDir:  baseDir,
		procUUID: procUUID,
		dir:      dir,
		path:     path,
	}, nil
}

// openLocked materializes the on-disk file on demand (review patch P1). Must
// be called under rw.mu. After successful return rw.file + rw.writer are
// guaranteed non-nil.
func (rw *RawWriter) openLocked() error {
	if rw.file != nil {
		return nil
	}
	if err := os.MkdirAll(rw.dir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(rw.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	rw.file = f
	rw.writer = bufio.NewWriterSize(f, 64*1024)
	return nil
}

// WriteRaw appends a single RawCapture as one NDJSON line + immediate flush.
// Returns an error if the writer is already closed, json marshaling fails, or
// the lazy file creation fails.
func (rw *RawWriter) WriteRaw(rec vfs.RawCapture) error {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	if rw.closed {
		return errors.New("raw_writer: write after close")
	}
	if err := rw.openLocked(); err != nil {
		return err
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if _, err := rw.writer.Write(data); err != nil {
		return err
	}
	if err := rw.writer.WriteByte('\n'); err != nil {
		return err
	}
	// Match EventWriter: flush every record so historical reads observe
	// raw.jsonl immediately on disk.
	return rw.writer.Flush()
}

// Flush flushes the buffered writer to disk. No-op after Close or when the
// underlying file has not yet been created (review patch P1, lazy creation).
func (rw *RawWriter) Flush() error {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	if rw.closed || rw.writer == nil {
		return nil
	}
	return rw.writer.Flush()
}

// Close flushes and closes the underlying file. Idempotent — second + later
// calls return nil so reap paths can call it unconditionally (镜像 EventWriter
// 在 reap 的关闭路径). Also a no-op if the file was never opened (no
// WriteRaw happened during the process's lifetime).
func (rw *RawWriter) Close() error {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	if rw.closed {
		return nil
	}
	rw.closed = true
	if rw.writer == nil {
		return nil
	}
	flushErr := rw.writer.Flush()
	closeErr := rw.file.Close()
	if flushErr != nil {
		return flushErr
	}
	return closeErr
}

// Path returns the on-disk file path for this writer (even if the file has
// not been created yet — caller can use this to check filesystem state).
func (rw *RawWriter) Path() string {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	return rw.path
}

// ReadAllRaw scans a raw.jsonl file and returns all records in order. Lines
// that fail to unmarshal are skipped (best-effort, mirroring ReadAllEvents).
// A missing file returns ([]vfs.RawCapture(nil), nil) so callers can treat
// "never captured" and "capture disabled" alike.
//
// Thin wrapper over ReadAllRawWithErrors (Story 56.4 AC#8) — existing callers
// keep the 2-value signature; the parse-error count is discarded here.
func ReadAllRaw(path string) ([]vfs.RawCapture, error) {
	records, _, err := ReadAllRawWithErrors(path)
	return records, err
}

// ReadAllRawWithErrors scans a raw.jsonl file and returns all records in order
// plus a count of lines that failed to unmarshal (Story 56.4 AC#8 /消化
// deferred #17). The malformed count makes "silently skipped" lines observable
// to the三路 query consumers instead of being swallowed. A missing file returns
// (nil, 0, nil).
func ReadAllRawWithErrors(path string) ([]vfs.RawCapture, int, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, nil
		}
		return nil, 0, err
	}
	defer f.Close()

	var records []vfs.RawCapture
	parseErrors := 0
	scanner := bufio.NewScanner(f)
	// raw bodies can be large; bump the per-line limit to 4MB (the configured
	// MaxOutputBytes default).
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		// Skip blank lines without counting them as parse errors — a trailing
		// newline produces an empty final token under bufio.Scanner.
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var rec vfs.RawCapture
		if err := json.Unmarshal(line, &rec); err != nil {
			// Story 56.4 AC#8: count malformed lines so the三路 consumers can
			// surface "N lines skipped (malformed)" instead of silent loss.
			parseErrors++
			continue
		}
		records = append(records, rec)
	}
	if err := scanner.Err(); err != nil {
		return records, parseErrors, err
	}
	return records, parseErrors, nil
}

// ReadRawForStep returns the RawCapture record whose Step matches the given
// step number, or nil if no such record exists.
//
// Thin wrapper over ReadRawForStepWithErrors (Story 56.4 review patch) —
// existing callers keep the 2-value signature; the parse-error count is
// discarded here.
func ReadRawForStep(path string, step int) (*vfs.RawCapture, error) {
	rec, _, err := ReadRawForStepWithErrors(path, step)
	return rec, err
}

// ReadRawForStepWithErrors returns the RawCapture record whose Step matches the
// given step number (or nil), plus a count of malformed lines in the whole file
// (Story 56.4 review patch · decision 1→a). Scanning the full file for the
// parse-error count keeps "N lines skipped (malformed)" observable on the
// single-step query path too (dashboard lens / `strace --raw --step N`), not
// just the Step=0 full-listing path — matching AC#8's 三路 parity intent.
//
// Match semantics are last-match (Story 56.7 裁决 2): a step can now carry
// multiple records — a failed primary call (outcome=error) followed by its
// fallback call in the same reason-loop iteration. The fallback record is
// appended later and represents the step's terminal outcome, so the single-step
// query returns it. Files with one record per step (all pre-56.7 data) behave
// identically to the previous first-match scan.
func ReadRawForStepWithErrors(path string, step int) (*vfs.RawCapture, int, error) {
	all, parseErrors, err := ReadAllRawWithErrors(path)
	if err != nil {
		return nil, parseErrors, err
	}
	for i := range slices.Backward(all) {
		if all[i].Step == step {
			return &all[i], parseErrors, nil
		}
	}
	return nil, parseErrors, nil
}

// captureRawLLMCall is the kernel reasonStep hook (Story 56.1 AC#6/#7). It
// runs after a successful LLM Read (review patch P6 — moved post-Read so
// future Stream-style drivers can populate Last with both request and full
// response) and, since Story 56.7, also on the Write-failure branch and on
// all three attemptFallback exits — a non-nil callErr marks the record with
// Outcome="error" + Error so failed calls remain auditable (裁决 2/3). It:
//
//  1. Bails out fast when raw capture is disabled or the process has no
//     RawWriter attached (degrades to no-op).
//  2. Type-asserts the LLM file against vfs.RawCaptureProvider and pulls
//     the most recent capture; nil = driver did not opt into raw capture
//     (true for *all* drivers in 56.1).
//  3. Deep-copies Request / Response maps (review patch P4) so the kernel
//     never mutates the driver's cached struct in place.
//  4. Stamps Step (if zero) so the step number always matches the reason
//     loop iteration that produced the capture.
//  5. Truncates oversize Request / Response bodies via the configured
//     MaxOutputBytes per-record ceiling (review patch P2 — was per-field).
//  6. Applies defense-in-depth redaction on common credential header maps
//     so a forgetful driver never leaks plaintext (idempotent w.r.t. a
//     driver that already redacted — vfs.RedactCredential).
//  7. Writes the record to the per-process RawWriter.
//
// All failures are logged but never propagated upward; the reasonStep loop
// is the source of truth for liveness, raw observability is best-effort.
func (k *KernelImpl) captureRawLLMCall(proc *Process, fd types.FD, step int, callErr error) {
	cfg := k.RawCaptureCfg()
	if !cfg.Enabled {
		return
	}
	proc.mu.Lock()
	rw := proc.rawWriter
	proc.mu.Unlock()
	if rw == nil {
		return
	}

	file := k.vfs.GetFile(proc.PID, fd)
	if file == nil {
		return
	}
	provider, ok := file.(vfs.RawCaptureProvider)
	if !ok {
		return
	}
	capture := provider.LastRawCapture()
	if capture == nil {
		return
	}

	// Review patch P4: deep-copy Request/Response so subsequent driver-side
	// reads never see kernel's redaction/truncation; also avoids concurrent
	// map writes if the driver buffers Last and continues mutating during a
	// next Call.
	rec := *capture
	rec.Request = deepCopyMap(capture.Request)
	rec.Response = deepCopyMap(capture.Response)
	if rec.Step == 0 {
		rec.Step = step
	}
	// Story 56.7 裁决 3: failure markers go on the deep-copied rec only (P4
	// deep-copy discipline — never mutate the driver's cached capture).
	// Successful records leave both fields empty so omitempty keeps the JSON
	// keys absent (AC7 backward compatibility).
	if callErr != nil {
		rec.Outcome = "error"
		rec.Error = redactRawCaptureError(callErr.Error())
	}
	maxBytes := cfg.MaxOutputBytes
	if maxBytes <= 0 {
		maxBytes = defaultRawCaptureMaxOutputBytes
	}
	rec.Request = redactCredentialFields(rec.Request)
	rec.Response = redactCredentialFields(rec.Response)
	// truncateRawCapture toggles Truncated + OriginalBytes itself; we discard
	// the bool — call is for the side-effect.
	_ = truncateRawCapture(&rec, maxBytes)
	if err := rw.WriteRaw(rec); err != nil {
		log.Printf("[raw_writer] write error pid=%d step=%d: %v", proc.PID, step, err)
	}
}

// deepCopyMap returns a one-level deep copy of m. Map values that are
// themselves maps or slices are NOT recursively cloned — review patch P4
// scope is to break the top-level reference shared with the driver so
// kernel-side redact/truncate doesn't leak back into the driver's cache;
// nested mutation is out of scope (deferred to 56.2/56.3 driver shape).
func deepCopyMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	dst := make(map[string]any, len(m))
	maps.Copy(dst, m)
	return dst
}

// truncateRawCapture enforces the MaxOutputBytes per-record ceiling (review
// patch P2). Implementation:
//
//  1. Marshal the whole record. If under maxBytes, no-op.
//  2. Otherwise, peel off Response then Request bodies one string field at
//     a time (largest first) until the marshaled record fits, replacing
//     each with a `<truncated:N bytes>` marker and accumulating
//     OriginalBytes. Sets Truncated=true.
//  3. If after evicting every string field the marshaled record still
//     exceeds maxBytes (small max + large non-string nested data), give up
//     gracefully — Truncated already set, the writer's bufio.Scanner cap
//     will surface the issue downstream rather than us crashing here.
//
// Mirrors drivers/mcp/transport.go::truncateResultLocked semantics
// (truncate + flag + never crash).
func truncateRawCapture(rec *vfs.RawCapture, maxBytes int64) bool {
	if maxBytes <= 0 {
		return false
	}
	data, err := json.Marshal(rec)
	if err != nil || int64(len(data)) <= maxBytes {
		return false
	}
	truncated := false
	// Process Response first (typically the larger body), then Request.
	for _, m := range []map[string]any{rec.Response, rec.Request} {
		if m == nil {
			continue
		}
		// Repeatedly drop the largest string field until under budget.
		for {
			data, err := json.Marshal(rec)
			if err != nil || int64(len(data)) <= maxBytes {
				break
			}
			key, sz := largestStringKey(m)
			if key == "" {
				break // no more string fields in this map
			}
			rec.OriginalBytes += int64(sz)
			m[key] = fmt.Sprintf("<truncated: %d bytes, max=%d>", sz, maxBytes)
			truncated = true
		}
		// Re-check after exhausting this map.
		data, err = json.Marshal(rec)
		if err == nil && int64(len(data)) <= maxBytes {
			break
		}
	}
	if data, err = json.Marshal(rec); err == nil && int64(len(data)) > maxBytes &&
		rec.Error != "" && !strings.HasPrefix(rec.Error, "<truncated:") {
		sz := len(rec.Error)
		rec.OriginalBytes += int64(sz)
		rec.Error = fmt.Sprintf("<truncated: %d bytes, max=%d>", sz, maxBytes)
		truncated = true
	}
	if truncated {
		rec.Truncated = true
	}
	return truncated
}

var rawErrorCredentialKeys = []string{
	"authorization",
	"proxy-authorization",
	"api-key",
	"x-api-key",
	"x-goog-api-key",
	"x-auth-token",
	"cookie",
	"set-cookie",
}

// redactRawCaptureError is a conservative defense-in-depth pass for the top
// level RawCapture.Error field. Drivers should avoid putting secrets in errors,
// but provider/CLI diagnostics can echo header-looking lines. When a line
// contains "Authorization: ..." or similar, redact the value using the same VFS
// header redaction primitive as request/response maps.
func redactRawCaptureError(s string) string {
	if s == "" {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = redactRawCaptureErrorLine(line)
	}
	return strings.Join(lines, "\n")
}

func redactRawCaptureErrorLine(line string) string {
	lower := strings.ToLower(line)
	for _, key := range rawErrorCredentialKeys {
		idx := strings.Index(lower, key)
		if idx < 0 {
			continue
		}
		pos := idx + len(key)
		for pos < len(line) && (line[pos] == ' ' || line[pos] == '\t') {
			pos++
		}
		if pos >= len(line) || (line[pos] != ':' && line[pos] != '=') {
			continue
		}
		valueStart := pos + 1
		for valueStart < len(line) && (line[valueStart] == ' ' || line[valueStart] == '\t') {
			valueStart++
		}
		value := strings.TrimSpace(line[valueStart:])
		if value == "" {
			return line
		}
		redacted := vfs.RedactHeaders(map[string]string{key: value})[key]
		return line[:valueStart] + redacted
	}
	return line
}

// largestStringKey returns the key of the largest string-valued entry in m
// along with its byte length. Returns ("", 0) when no string-valued entry
// remains. Used by truncateRawCapture to drop fields biggest-first.
func largestStringKey(m map[string]any) (string, int) {
	bestKey := ""
	bestLen := 0
	for k, v := range m {
		s, ok := v.(string)
		if !ok {
			continue
		}
		// Only consider strings that have not yet been replaced with a
		// truncation marker (markers start with "<truncated:") to avoid an
		// infinite loop re-evicting our own marker text.
		if strings.HasPrefix(s, "<truncated:") {
			continue
		}
		if len(s) > bestLen {
			bestKey = k
			bestLen = len(s)
		}
	}
	return bestKey, bestLen
}

// redactCredentialFields is the kernel's defense-in-depth redaction pass.
// 56.1 AC#3 mandates plaintext credentials never reach disk. Drivers (56.2/
// 56.3) are responsible for primary redaction inside their Call/Stream
// paths; this pass catches the case where a future driver forgets, mirroring
// the "two layers, both safe" policy in组合矩阵第 5 行. Idempotent because
// vfs.RedactCredential short-circuits on already-fingerprinted strings.
//
// Review patch P5: preserve the original headers map type (map[string]any)
// when input is map[string]any with non-string elements (multi-value
// []string / []any). Previous implementation type-converted to
// map[string]string and silently dropped non-string values — defense-in-depth
// then *lost* the secret instead of fingerprinting it.
func redactCredentialFields(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	switch headers := m["headers"].(type) {
	case map[string]string:
		m["headers"] = vfs.RedactHeaders(headers)
	case map[string]any:
		out := make(map[string]any, len(headers))
		for k, v := range headers {
			out[k] = redactHeaderAny(k, v)
		}
		m["headers"] = out
	}
	// Some CLI drivers may surface inline tokens via env / argv-tail
	// fingerprints. We do not auto-walk those here — the spec assigns
	// primary responsibility to the driver layer (56.3). Future hardening
	// can add an env scan when 56.3 is implemented.
	return m
}

// redactHeaderAny applies vfs.RedactCredential to a single header value of
// arbitrary type. Strings get fingerprinted via RedactHeaders (single-key
// path); slices get per-element redaction; everything else is passed
// through (driver responsibility).
func redactHeaderAny(key string, v any) any {
	switch val := v.(type) {
	case string:
		// Reuse RedactHeaders single-key path so the credential set lookup
		// stays the single source of truth.
		return vfs.RedactHeaders(map[string]string{key: val})[key]
	case []string:
		out := make([]string, len(val))
		for i, s := range val {
			out[i] = vfs.RedactHeaders(map[string]string{key: s})[key]
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, e := range val {
			out[i] = redactHeaderAny(key, e)
		}
		return out
	default:
		return v
	}
}
