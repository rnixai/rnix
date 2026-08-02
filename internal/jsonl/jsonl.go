// Package jsonl reads newline-delimited JSON without a per-line size ceiling.
//
// Story 72.1: the observation read paths (steps.jsonl / events.jsonl) used
// bufio.Scanner with fixed Buffer limits (1 MB / 256 KB). A single line above
// that limit made Scan() stop with ErrTooLong, silently dropping the oversized
// line *and every line after it*. Real consequences observed in production
// data: a 128-step process rendered as 45 steps in the dashboard, a process
// made permanently un-resumable, and a syscall event stream reported as empty.
//
// This package is a leaf: it depends only on the standard library, so the
// kernel, kernel/memory and cmd/rnix packages can all import it without
// creating an import cycle (kernel already imports kernel/memory).
package jsonl

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"log"
)

// hugeLineWarnBytes is a *warning* threshold, not a truncation limit.
//
// True unboundedness means a corrupt file with no newline would be read into
// memory in one allocation. Rather than reintroduce a ceiling (which is the
// very defect this package exists to remove), crossing this threshold only
// logs — the line is still returned in full.
const hugeLineWarnBytes = 8 * 1024 * 1024

// initialBufSize is the starting size of the underlying bufio.Reader. Lines
// larger than this grow on demand; it is a performance knob, never a limit.
const initialBufSize = 64 * 1024

// Scan reads r line by line and calls fn for each non-blank line. Unlike
// bufio.Scanner, a line has no maximum length — lines are allocated on demand
// and are never dropped for being too long.
//
// name identifies the source in log messages (typically the file path).
//
// The byte slice handed to fn includes its trailing newline (if any) and points
// into a buffer that Scan reuses for the next line — it stops being valid as
// soon as fn returns. Callers MUST copy anything they retain. (json.Unmarshal
// is safe: the decoder allocates its own strings, and json.RawMessage's
// UnmarshalJSON appends into the destination rather than aliasing the input.)
//
// If fn returns a non-nil error, scanning stops and that error is returned
// verbatim. This is what lets callers who must fail hard on a malformed line
// (resume: restarting with a wrong context is worse than not restarting) keep
// that semantic, while best-effort callers simply return nil to skip.
func Scan(r io.Reader, name string, fn func(line []byte) error) error {
	br := bufio.NewReaderSize(r, initialBufSize)
	warned := false
	// scratch assembles only those lines that exceed the reader's buffer. It is
	// reused across lines, so the growth cost is amortised over the whole file.
	//
	// 🔴 Why ReadSlice + scratch instead of the simpler ReadBytes: ReadBytes
	// allocates a fresh slice for EVERY line. On the 147 MB / 128-line carrier
	// that motivated this package that is tolerable, but the same helper indexes
	// every steps.jsonl on daemon startup (kernel/memory/recall.go) and serves
	// the dashboard's per-tick step fetch — measured at 20k lines it was ~200k
	// allocations and 3x the wall time of the bufio.Scanner it replaced.
	// ReadSlice hands back a window into the reader's own buffer: zero
	// allocations for any line under initialBufSize, which is all of them in
	// practice (observed max non-Messages field: 233 KB, and the 8 MB carriers
	// are a handful of lines out of 27,623).
	var scratch []byte

	for {
		line, err := br.ReadSlice('\n')

		// A line longer than the buffer arrives in chunks, each flagged
		// ErrBufferFull. Stitch them together in scratch — the only allocating
		// path, and only for genuinely oversized lines.
		if errors.Is(err, bufio.ErrBufferFull) {
			scratch = append(scratch[:0], line...)
			for errors.Is(err, bufio.ErrBufferFull) {
				line, err = br.ReadSlice('\n')
				scratch = append(scratch, line...)
			}
			line = scratch
		}

		// Deliver data to fn only when it forms a COMPLETE line:
		//   err == nil    → terminated by '\n'
		//   err == io.EOF → final fragment with no trailing newline (normal for
		//                   a file written without a closing \n)
		//
		// 🔴 Any other error means a torn read: the bytes in hand are the front
		// half of a line the reader never finished. Handing that fragment to fn
		// would make it fail to unmarshal, and the caller would report a PARSE
		// error while the real I/O cause silently vanishes — the exact
		// "read failure disguised as something else" defect this story exists to
		// remove (AC3). bufio.Scanner, which this package replaced, also never
		// yielded a token on a read error. Drop the fragment; surface the truth.
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}

		// Process the data BEFORE acting on io.EOF, or the last line of a file
		// without a trailing newline is silently dropped.
		if len(line) > 0 {
			if len(line) >= hugeLineWarnBytes && !warned {
				// Warn once per source: a file with hundreds of huge lines
				// should not produce hundreds of log entries.
				warned = true
				log.Printf("[jsonl] %s: line of %d bytes exceeds %d — reading it in full (no truncation)",
					name, len(line), hugeLineWarnBytes)
			}
			if len(bytes.TrimSpace(line)) > 0 {
				if fnErr := fn(line); fnErr != nil {
					return fnErr
				}
			}
		}

		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}
