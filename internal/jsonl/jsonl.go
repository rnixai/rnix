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
// The byte slice handed to fn includes its trailing newline (if any) and is
// freshly allocated per line, but the contract does not promise it stays valid
// after fn returns — callers must copy anything they retain.
//
// If fn returns a non-nil error, scanning stops and that error is returned
// verbatim. This is what lets callers who must fail hard on a malformed line
// (resume: restarting with a wrong context is worse than not restarting) keep
// that semantic, while best-effort callers simply return nil to skip.
func Scan(r io.Reader, name string, fn func(line []byte) error) error {
	br := bufio.NewReaderSize(r, initialBufSize)
	warned := false

	for {
		line, err := br.ReadBytes('\n')

		// ReadBytes returns the trailing fragment together with io.EOF when the
		// file does not end in a newline. Process the data BEFORE acting on the
		// error, or that last line is silently dropped.
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
