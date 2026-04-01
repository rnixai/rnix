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

// LoopDetector detects repetitive action patterns in the reasoning loop.
// It uses a ring buffer of action hashes and checks if the last N steps are identical.
type LoopDetector struct {
	window    []uint64 // ring buffer storing recent action hashes
	pos       int      // current write position
	size      int      // number of filled slots
	threshold int      // N: steps before warning
	warned    bool     // whether LoopWarning has been emitted
}

// NewLoopDetector creates a LoopDetector with the given threshold.
// If threshold <= 0, DefaultLoopThreshold is used.
func NewLoopDetector(threshold int) *LoopDetector {
	if threshold <= 0 {
		threshold = DefaultLoopThreshold
	}
	return &LoopDetector{
		window:    make([]uint64, 2*threshold), // capacity = 2N for suspend detection
		threshold: threshold,
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
		if d.allSame(2 * d.threshold) {
			return LoopSuspend
		}
	}

	// Check for N consecutive identical (warning)
	if d.size >= d.threshold {
		if d.allSame(d.threshold) {
			if !d.warned {
				d.warned = true
				return LoopWarning
			}
		}
	}

	return LoopNone
}

// allSame checks if the last count entries in the ring buffer are all identical.
func (d *LoopDetector) allSame(count int) bool {
	if d.size < count {
		return false
	}
	// Get the most recent entry
	latest := (d.pos - 1 + len(d.window)) % len(d.window)
	ref := d.window[latest]
	for i := 1; i < count; i++ {
		idx := (d.pos - 1 - i + len(d.window)*2) % len(d.window)
		if d.window[idx] != ref {
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

// LoopWarningMessage returns the system warning message injected into context.
func LoopWarningMessage(threshold int) string {
	return fmt.Sprintf("[System Warning] Detected repetitive loop: same action repeated %d times. Please try a different approach.", threshold)
}
