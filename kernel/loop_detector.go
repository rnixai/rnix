package kernel

import (
	"fmt"
	"hash/fnv"
)

// LoopStatus represents the result of loop detection analysis.
type LoopStatus int

const (
	LoopNone    LoopStatus = iota
	LoopWarning            // N consecutive identical steps → inject warning
	LoopSuspend            // 2N consecutive identical steps → terminate
)

// DefaultLoopThreshold is the number of consecutive identical steps before warning.
const DefaultLoopThreshold = 10

// DefaultCoarseLoopThreshold is the number of consecutive steps with the same
// (actionType, toolPath) — ignoring toolInput — before warning. Higher than
// DefaultLoopThreshold because legitimate sequences of calls to the same tool
// (e.g., reading multiple files) are common.
const DefaultCoarseLoopThreshold = 15

// LoopDetector detects repetitive action patterns in the reasoning loop.
// It uses a ring buffer of action hashes and checks if the last N steps are identical.
// A secondary coarse-grain buffer detects repetition by (actionType, toolPath) alone,
// catching cases where the LLM varies toolInput to evade the fine-grain check.
type LoopDetector struct {
	window    []uint64 // ring buffer storing recent action hashes (fine-grain)
	pos       int      // current write position
	size      int      // number of filled slots
	threshold int      // N: steps before warning
	warned    bool     // whether LoopWarning has been emitted

	coarseWindow    []uint64 // ring buffer for coarse hashes (actionType+toolPath only)
	coarsePos       int
	coarseSize      int
	coarseThreshold int
	coarseWarned    bool

	LastTriggeredThreshold int // set by Check/CheckDual to the threshold that fired
}

// NewLoopDetector creates a LoopDetector with the given threshold.
// If threshold <= 0, DefaultLoopThreshold is used.
func NewLoopDetector(threshold int) *LoopDetector {
	if threshold <= 0 {
		threshold = DefaultLoopThreshold
	}
	return &LoopDetector{
		window:          make([]uint64, 2*threshold),
		threshold:       threshold,
		coarseWindow:    make([]uint64, 2*DefaultCoarseLoopThreshold),
		coarseThreshold: DefaultCoarseLoopThreshold,
	}
}

// Check records an action hash and returns the current loop status.
func (d *LoopDetector) Check(hash uint64) LoopStatus {
	d.window[d.pos] = hash
	d.pos = (d.pos + 1) % len(d.window)
	if d.size < len(d.window) {
		d.size++
	}

	// Check for 2N consecutive identical (suspend)
	if d.size >= 2*d.threshold {
		if d.allSame(d.window, d.pos, d.size, 2*d.threshold) {
			d.LastTriggeredThreshold = d.threshold
			return LoopSuspend
		}
	}

	// Check for N consecutive identical (warning)
	if d.size >= d.threshold {
		if d.allSame(d.window, d.pos, d.size, d.threshold) {
			if !d.warned {
				d.warned = true
				d.LastTriggeredThreshold = d.threshold
				return LoopWarning
			}
		} else {
			d.warned = false
		}
	}

	return LoopNone
}

// CheckDual records both a fine-grain hash and a coarse hash, returning the
// highest-severity status from either track.
func (d *LoopDetector) CheckDual(fineHash, coarseHash uint64) LoopStatus {
	fineResult := d.Check(fineHash)

	d.coarseWindow[d.coarsePos] = coarseHash
	d.coarsePos = (d.coarsePos + 1) % len(d.coarseWindow)
	if d.coarseSize < len(d.coarseWindow) {
		d.coarseSize++
	}

	coarseResult := LoopNone

	if d.coarseSize >= 2*d.coarseThreshold {
		if d.allSame(d.coarseWindow, d.coarsePos, d.coarseSize, 2*d.coarseThreshold) {
			coarseResult = LoopSuspend
		}
	}

	if coarseResult == LoopNone && d.coarseSize >= d.coarseThreshold {
		if d.allSame(d.coarseWindow, d.coarsePos, d.coarseSize, d.coarseThreshold) {
			if !d.coarseWarned {
				d.coarseWarned = true
				coarseResult = LoopWarning
			}
		} else {
			d.coarseWarned = false
		}
	}

	if coarseResult > fineResult {
		d.LastTriggeredThreshold = d.coarseThreshold
		return coarseResult
	}
	return fineResult
}

// allSame checks if the last count entries in the given ring buffer are all identical.
func (d *LoopDetector) allSame(window []uint64, pos, size, count int) bool {
	if size < count {
		return false
	}
	wlen := len(window)
	latest := (pos - 1 + wlen) % wlen
	ref := window[latest]
	for i := 1; i < count; i++ {
		idx := (pos - 1 - i + wlen*2) % wlen
		if window[idx] != ref {
			return false
		}
	}
	return true
}

// ActionHash computes a FNV-1a hash of the action type, tool path, and (truncated) tool input.
func ActionHash(actionType, toolPath, toolInput string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(actionType))
	h.Write([]byte{0})
	h.Write([]byte(toolPath))
	h.Write([]byte{0})
	if len(toolInput) > 256 {
		h.Write([]byte(toolInput[:256]))
	} else {
		h.Write([]byte(toolInput))
	}
	return h.Sum64()
}

// CoarseActionHash computes a FNV-1a hash using only actionType and toolPath,
// ignoring toolInput. This catches loops where the LLM varies input parameters
// while repeating the same tool type.
func CoarseActionHash(actionType, toolPath string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(actionType))
	h.Write([]byte{0})
	h.Write([]byte(toolPath))
	return h.Sum64()
}

// LoopWarningMessage returns the system warning message injected into context.
func LoopWarningMessage(threshold int) string {
	return fmt.Sprintf("[System Warning] Detected repetitive loop: same action repeated %d times. Please try a different approach.", threshold)
}
