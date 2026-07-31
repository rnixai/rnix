package kernel

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"

	"github.com/rnixai/rnix/agents"
	"github.com/rnixai/rnix/internal/types"
)

// LoopStatus represents the result of loop detection analysis.
type LoopStatus int

const (
	LoopNone    LoopStatus = iota
	LoopWarning            // N consecutive identical steps → inject warning
	LoopSuspend            // 2N consecutive identical steps → terminate
)

// DefaultLoopThreshold is the number of consecutive identical (action, result)
// tool-call steps before warning; 2N suspends.
//
// Raised from 10 to 30 in Story 70.1. The old value was tuned for short
// single-purpose agents; long-running orchestrators legitimately poll the same
// command for many steps (a 2400s child observed at a 60s interval is 40
// identical polls), and the observed false-positive incident showed the old
// ceiling firing on healthy work. 30 warns / 60 suspends leaves room for that
// while still catching a genuine spin well before the step/token/cost gates.
const DefaultLoopThreshold = 30

// DefaultCoarseLoopThreshold is the number of consecutive steps with the same
// (actionType, toolPath, resultHash) — ignoring toolInput — before warning.
// Higher than DefaultLoopThreshold because legitimate sequences of calls to the
// same tool (e.g., reading multiple files) are common.
//
// That reasoning still holds, but Story 70.1 changed the coarse track's role:
// now that the result hash is part of BOTH tracks' criteria, the coarse track no
// longer fires on "many Bash calls in a row" — only on "many Bash calls in a row
// that all produced the SAME result" (thrashing: the LLM varies toolInput but
// gets nowhere). That is a pure backstop, so the threshold was raised from 15 to
// 60 (120 suspends) rather than kept tight.
const DefaultCoarseLoopThreshold = 60

// LoopDetector detects repetitive action patterns in the reasoning loop.
// It uses a ring buffer of action hashes and checks if the last N steps are identical.
// A secondary coarse-grain buffer detects repetition by (actionType, toolPath) alone,
// catching cases where the LLM varies toolInput to evade the fine-grain check.
//
// # Result repetition is a necessary condition (Story 70.1)
//
// Both tracks mix a tool-result hash into the recorded value, so neither track
// can fire unless the results repeat too:
//
//   - fine   = same actionType + toolPath + toolInput + result
//   - coarse = same actionType + toolPath + result (toolInput ignored)
//
// Before this change the coarse hash was a constant for every `Bash` call, so
// "30 consecutive Bash steps" suspended the process regardless of whether it was
// making progress — the orchestrator hit the ceiling once per completed story.
//
// # Known limitation (do not paper over)
//
// When tool results embed a timestamp, PID, elapsed time, or random ID, every
// result hash differs and NEITHER track will ever fire. That is the inherent
// cost of making result repetition a necessary condition. The backstops for a
// genuinely stuck-but-noisy process are the four independent gates that do not
// look at content at all: --max-steps, MaxTokens, MaxCost, and StepTimeout.
//
// # Threshold semantics
//
// A threshold of 0 means "use the default"; a NEGATIVE threshold DISABLES that
// track entirely (its ring buffer is not even allocated — see NewLoopDetector).
type LoopDetector struct {
	window       []uint64 // ring buffer storing recent action hashes (fine-grain); nil when disabled
	pos          int      // current write position
	size         int      // number of filled slots
	threshold    int      // N: steps before warning
	warned       bool     // whether LoopWarning has been emitted
	fineDisabled bool     // true when constructed with a negative fine threshold

	coarseWindow   []uint64 // ring buffer for coarse hashes (actionType+toolPath+result); nil when disabled
	coarsePos      int
	coarseSize     int
	coarseThreshold int
	coarseWarned   bool
	coarseDisabled bool

	LastTriggeredThreshold int // set by Check/CheckDual to the threshold that fired

	// LastTriggeredTrack is "fine" or "coarse", identifying which track produced
	// the last non-LoopNone status. Needed because thresholds became configurable
	// in Story 70.1: LastTriggeredThreshold alone no longer identifies the track
	// (a process configured with fine=60 and coarse=60 reports 60 either way).
	// Read-only for callers; logged, never put on the IPC wire (AC10).
	LastTriggeredTrack string
}

// NewLoopDetector creates a LoopDetector with the given fine-grain and
// coarse-grain thresholds.
//
// Threshold semantics for BOTH parameters:
//
//	 0  → use the corresponding default (DefaultLoopThreshold / DefaultCoarseLoopThreshold)
//	>0  → warn at N consecutive matches, suspend at 2N
//	<0  → DISABLE that track (nothing recorded, always LoopNone)
//
// A disabled track allocates no ring buffer. The disabled path must be an
// explicit flag, not "a zero-length or negative-length window that happens not
// to match": make([]uint64, 2*threshold) panics outright for a negative
// threshold, and the `% len(window)` advance panics with a divide-by-zero for a
// zero-length one.
func NewLoopDetector(threshold, coarseThreshold int) *LoopDetector {
	d := &LoopDetector{}

	switch {
	case threshold < 0:
		d.fineDisabled = true
		d.threshold = threshold // preserved as-is for diagnostics
	default:
		if threshold == 0 {
			threshold = DefaultLoopThreshold
		}
		d.threshold = threshold
		d.window = make([]uint64, 2*threshold)
	}

	switch {
	case coarseThreshold < 0:
		d.coarseDisabled = true
		d.coarseThreshold = coarseThreshold
	default:
		if coarseThreshold == 0 {
			coarseThreshold = DefaultCoarseLoopThreshold
		}
		d.coarseThreshold = coarseThreshold
		d.coarseWindow = make([]uint64, 2*coarseThreshold)
	}

	return d
}

// Check records a fine-grain hash and returns the current loop status. It is the
// single-track entry point; callers that also have a coarse hash should use
// CheckDual, which mixes the tool-result hash into both tracks for them.
func (d *LoopDetector) Check(hash uint64) LoopStatus {
	if d.fineDisabled {
		return LoopNone
	}

	d.window[d.pos] = hash
	d.pos = (d.pos + 1) % len(d.window)
	if d.size < len(d.window) {
		d.size++
	}

	// Check for 2N consecutive identical (suspend)
	if d.size >= 2*d.threshold {
		if d.allSame(d.window, d.pos, d.size, 2*d.threshold) {
			d.LastTriggeredThreshold = d.threshold
			d.LastTriggeredTrack = "fine"
			return LoopSuspend
		}
	}

	// Check for N consecutive identical (warning)
	if d.size >= d.threshold {
		if d.allSame(d.window, d.pos, d.size, d.threshold) {
			if !d.warned {
				d.warned = true
				d.LastTriggeredThreshold = d.threshold
				d.LastTriggeredTrack = "fine"
				return LoopWarning
			}
		} else {
			d.warned = false
		}
	}

	return LoopNone
}

// CheckDual records both tracks for one tool-call step and returns the
// highest-severity status from either.
//
// resultHash is the tool-result hash for the PREVIOUS tool_call step, not this
// one (see the "off by one step" note in reason.go): the detection point sits
// before executeToolCalls, so this step's result does not exist yet. The
// equivalence that makes this sound: in a genuine loop of identical actions the
// result sequence is identical too, so lagging one step only moves the trigger
// from step 2N to step 2N+1 — it never changes which sequences are judged to be
// loops. The first tool_call step passes the sentinel 0.
func (d *LoopDetector) CheckDual(fineHash, coarseHash, resultHash uint64) LoopStatus {
	fineResult := d.Check(mixHash(fineHash, resultHash))

	if d.coarseDisabled {
		return fineResult
	}

	d.coarseWindow[d.coarsePos] = mixHash(coarseHash, resultHash)
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
		d.LastTriggeredTrack = "coarse"
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

// mixHash combines an action hash with a tool-result hash into the single value a
// ring buffer slot holds.
//
// It runs both operands through FNV-1a (big-endian 8 bytes each, separated by a
// zero byte) rather than XOR-ing them. XOR is commutative, so `action^result`
// would collide with `result^action` and with any other pair sharing that XOR —
// exactly the kind of spurious equality that would resurrect the false positive
// this criterion exists to fix.
func mixHash(actionHash, resultHash uint64) uint64 {
	var buf [17]byte
	binary.BigEndian.PutUint64(buf[0:8], actionHash)
	buf[8] = 0
	binary.BigEndian.PutUint64(buf[9:17], resultHash)
	h := fnv.New64a()
	h.Write(buf[:])
	return h.Sum64()
}

// ToolResultHash computes an order-sensitive FNV-1a hash over a whole batch of
// tool-call records, mixing each record's Name, Result, and Error. An empty batch
// hashes to the sentinel 0.
//
// Why the whole batch, while the action side (reason.go) hashes only
// resp.ToolCalls[0]:
//
//   - The two lists are NOT positionally aligned. tool_exec.go's loop `continue`s
//     without appending for a parse error, a `think` call, and an unknown tool, so
//     pairing toolCallsAcc[i] with resp.ToolCalls[i] is an implicit — and wrong —
//     assumption. Hashing the whole batch needs no such correspondence.
//   - Aggregating is also the conservative direction: more inputs means results
//     are more likely to be judged "different", which means FEWER loop
//     detections. That matches this story's goal (kill false positives) rather
//     than working against it.
//
// The asymmetry with the action side is deliberate; the single-call
// simplification there is tracked separately (deferred-work:1291).
//
// Error is hashed, not skipped: "same command, same error, over and over" is a
// textbook stuck loop and must stay detectable.
func ToolResultHash(records []types.ToolCallRecord) uint64 {
	if len(records) == 0 {
		return 0
	}
	h := fnv.New64a()
	for _, r := range records {
		h.Write([]byte(r.Name))
		h.Write([]byte{0})
		h.Write([]byte(r.Result))
		h.Write([]byte{0})
		h.Write([]byte(r.Error))
		h.Write([]byte{0x1e}) // record separator
	}
	return h.Sum64()
}

// ActionHash computes a FNV-1a hash of the action type, tool path, and the FULL
// tool input.
//
// The input used to be truncated at 256 bytes, which silently erased the
// difference between distinct calls sharing a long prefix. That was not
// theoretical: the four spawn commands from the investigated incident share a
// 232-byte prefix — 24 bytes of headroom below the old cutoff. Any slightly
// longer path or flag list would have made genuinely different actions hash
// identically, i.e. manufactured a loop that was not there.
//
// Hashing in full is the fix. Raising the cutoff would only move an arbitrary
// line, and head+tail sampling would leave a blind spot in the middle. The cost
// is negligible: the input is already in memory and this is one O(n) FNV-1a pass
// per reasoning step, against an LLM round trip.
func ActionHash(actionType, toolPath, toolInput string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(actionType))
	h.Write([]byte{0})
	h.Write([]byte(toolPath))
	h.Write([]byte{0})
	h.Write([]byte(toolInput))
	return h.Sum64()
}

// CoarseActionHash computes a FNV-1a hash using only actionType and toolPath,
// ignoring toolInput. Combined with the result hash by CheckDual, this catches
// loops where the LLM varies input parameters while repeating the same tool type
// and getting the same result back every time.
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

// applyLoopThresholds resolves the two loop detection thresholds onto the
// process. Priority: SpawnOpts > agent manifest > kernel default (Story 70.1).
//
// ⚠️ Every test here is `!= 0`, deliberately NOT the `> 0` used by the
// StepTimeout block in spawn.go. Under `> 0`, a caller or manifest asking for -1
// ("disable this track") would be misread as "unset", fall through to the next
// tier, and end up with detection still enabled — silently, with no error. `!= 0`
// keeps a negative value travelling all the way to NewLoopDetector, which owns
// the disable behaviour.
//
// A zero at every tier leaves proc.LoopThreshold at 0, which
// effectiveLoopThreshold() maps to the default; there is nothing to write.
func applyLoopThresholds(proc *Process, agent *agents.AgentInfo, opts SpawnOpts) {
	switch {
	case opts.LoopThreshold != 0:
		proc.LoopThreshold = opts.LoopThreshold
	case agent != nil && agent.Manifest.LoopThreshold != 0:
		proc.LoopThreshold = agent.Manifest.LoopThreshold
	}

	switch {
	case opts.CoarseLoopThreshold != 0:
		proc.CoarseLoopThreshold = opts.CoarseLoopThreshold
	case agent != nil && agent.Manifest.CoarseLoopThreshold != 0:
		proc.CoarseLoopThreshold = agent.Manifest.CoarseLoopThreshold
	}
}
