package kernel

// Story 56.6 — CLI driver subagent 进程树重建.
//
// claude CLI runs the entire agentic loop — including Task/Agent subagent
// dispatch — inside a single `claude -p` OS process. rnix only *observes* the
// CLI's tool calls as steps and never takes the ActionSpawn path, so subagents
// were invisible: no rnix ProcInfo, no UUID, no tree node, and their internal
// Bash/Read/Edit steps polluted the host step stream.
//
// This file reconstructs the subagent tree from the stream-json sidechain. The
// driver (Story 56.6 AC1) now preserves the cross-process join key
// `parent_tool_use_id` on every frame. observe.go feeds frames here:
//   - A Task/Agent tool_use block whose input carries `subagent_type` synthesizes
//     a child node (deterministic UUID derived from host UUID + the tool_use id).
//   - Every later frame whose `parent_tool_use_id` matches that tool_use id is the
//     subagent's internal activity; its tool calls are routed to the child's own
//     steps.jsonl instead of the host's.
//   - The host's Task tool_result (or stream end) finalizes the child (Dead +
//     Result + DeadAt).
//
// Synthetic nodes have PID==0, PPID==host PID (so the tree builder's ParentUUID
// mount path attaches them under the host instead of short-circuiting them to
// roots — see internal/dashboard/tree/builder.go), a non-empty ParentUUID, and
// steps.jsonl but no process-meta.json. They are marked vfs.ProcInfo.Synthetic
// so resume / ps --resumable exclude them (they are not real rnix processes).

import (
	"encoding/json"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/rnixai/rnix/debug"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// cliSubagentNamespace is a fixed UUIDv5 namespace for deriving deterministic
// synthetic child UUIDs. Stable across daemon restarts so replay / LoadHistory
// produce the same UUID (idempotent dedup) and never collide with real uuidv7.
var cliSubagentNamespace = uuid.MustParse("9f1c0d2e-56e6-5a6b-9c0d-7e6f56e6c116")

// maxSubagentResultBytes caps the report text stored on a synthetic child's
// Result (UTF-8 safe), mirroring the spirit of max_output_bytes truncation.
const maxSubagentResultBytes = 64 * 1024

// syntheticChildUUID derives a deterministic UUIDv5 from the host UUID and the
// parent Task tool_use id. Same inputs → same UUID, so a replayed Task frame
// (or a second LoadHistory pass) dedupes to a single node (AC8).
func syntheticChildUUID(hostUUID, toolUseID string) string {
	return uuid.NewSHA1(cliSubagentNamespace, []byte(hostUUID+"\x00"+toolUseID)).String()
}

// toolUseBlock is a normalized view of an assistant tool_use content block.
type toolUseBlock struct {
	id    string
	name  string
	input map[string]any
}

// toolResultBlock is a normalized view of a user tool_result content block.
type toolResultBlock struct {
	toolUseID string
	content   string
	isError   bool
}

// normalizeContentBlocks flattens an event's `content` (which the driver may
// emit as []map[string]any, []any, or map[string]any — see observe.go's
// assistant/user handling) into a slice of generic block maps.
func normalizeContentBlocks(content any) []map[string]any {
	switch c := content.(type) {
	case []map[string]any:
		return c
	case []any:
		out := make([]map[string]any, 0, len(c))
		for _, item := range c {
			if block, ok := item.(map[string]any); ok {
				out = append(out, block)
			}
		}
		return out
	case map[string]any:
		return []map[string]any{c}
	default:
		return nil
	}
}

// extractToolUseBlocks returns the tool_use blocks in an event's content.
func extractToolUseBlocks(evt map[string]any) []toolUseBlock {
	var out []toolUseBlock
	for _, block := range normalizeContentBlocks(evt["content"]) {
		if bt, _ := block["type"].(string); bt != "tool_use" {
			continue
		}
		id, _ := block["id"].(string)
		name, _ := block["name"].(string)
		input, _ := block["input"].(map[string]any)
		out = append(out, toolUseBlock{id: id, name: name, input: input})
	}
	return out
}

// extractToolResultBlocks returns the tool_result blocks in an event's content.
func extractToolResultBlocks(evt map[string]any) []toolResultBlock {
	var out []toolResultBlock
	for _, block := range normalizeContentBlocks(evt["content"]) {
		if bt, _ := block["type"].(string); bt != "tool_result" {
			continue
		}
		// claude stream-json only carries `is_error` on failing tool_results;
		// the failed type assertion on a missing key defaults to false.
		isError, _ := block["is_error"].(bool)
		out = append(out, toolResultBlock{
			toolUseID: stringOf(block["tool_use_id"]),
			content:   extractContentText(block["content"]),
			isError:   isError,
		})
	}
	return out
}

// isSubagentDispatch reports whether a tool_use block is a Claude Code
// Task/Agent subagent dispatch — the shape gate (AC5 双保险): the input must
// carry `subagent_type`. This distinguishes the CLI's Task schema
// (description/prompt/subagent_type) from the rnix Agent schema
// (intent/agent/model) so API-driver Agent calls never synthesize.
func isSubagentDispatch(b toolUseBlock) bool {
	if b.id == "" || b.input == nil {
		return false
	}
	if _, ok := b.input["subagent_type"]; !ok {
		return false
	}
	return true
}

// stringOf coerces an any to string ("" when not a string).
func stringOf(v any) string {
	s, _ := v.(string)
	return s
}

// truncateUTF8Bytes truncates s to at most maxBytes on a valid UTF-8 boundary.
func truncateUTF8Bytes(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	return strings.ToValidUTF8(s[:maxBytes], "")
}

// ---------------------------------------------------------------------------
// syntheticChild — a single reconstructed subagent and its step accumulator.
// ---------------------------------------------------------------------------

// pendingChildTool tracks one in-flight tool call inside a subagent, mirroring
// the host's pendingToolState but local to a child (observe.go owns the host).
type pendingChildTool struct {
	step     int
	tool     string
	toolPath string
	start    time.Time
	inputBuf strings.Builder
	result   string
	callID   string
}

// syntheticChild owns a reconstructed subagent: its ProcInfo snapshot, a
// dedicated StepWriter (<base>/steps/<uuid>/steps.jsonl), and the tool-call
// accumulation state for its internal frames.
type syntheticChild struct {
	taskID    string
	uuid      string
	createdAt time.Time
	sw        *StepWriter
	ew        *EventWriter

	driverStep int
	pendings   map[string]*pendingChildTool
	order      []*pendingChildTool
	current    *pendingChildTool
	authInputs map[string]string

	info      vfs.ProcInfo
	finalized bool
}

// flush persists one pending tool as a step record on the child's steps.jsonl.
// Prefers the authoritative input captured from the assistant tool_use block
// (keyed by call_id) over the incrementally accumulated inputBuf, mirroring the
// host flush contract (Story 40.4).
func (c *syntheticChild) flush(p *pendingChildTool) {
	if p == nil || p.step == 0 || c.sw == nil {
		return
	}
	toolInput := p.inputBuf.String()
	if p.callID != "" {
		if auth, ok := c.authInputs[p.callID]; ok {
			toolInput = auth
		}
	}
	summary := driverToolSummary(p.tool, p.toolPath, toolInput)
	rec := types.StepRecord{
		Step:         p.step,
		Timestamp:    time.Since(c.createdAt),
		Action:       p.tool,
		Summary:      summary,
		ToolPath:     p.toolPath,
		ToolInput:    toolInput,
		ToolResult:   p.result,
		ToolDuration: time.Since(p.start),
	}
	_ = c.sw.WriteStep(rec)

	if p.callID != "" {
		delete(c.authInputs, p.callID)
		delete(c.pendings, p.callID)
	}
	for i, np := range c.order {
		if np == p {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
	if c.current == p {
		c.current = nil
	}
	p.step = 0
}

// flushAll drains every still-pending tool (stream-end / finalize backstop).
func (c *syntheticChild) flushAll() {
	for len(c.order) > 0 {
		c.flush(c.order[0])
	}
	c.current = nil
}

// feed routes one subagent-internal frame (already matched by parent_tool_use_id)
// into the child's step accumulator. Tool steps are written to the child's own
// steps.jsonl, never the host's (AC3).
func (c *syntheticChild) feed(evt map[string]any) {
	evtType, _ := evt["type"].(string)
	// Persist the frame as a syscall event on the child's OWN events.jsonl, so
	// the dashboard Timeline (which reads <base>/steps/<uuid>/events.jsonl by
	// UUID) shows the subagent's internal activity instead of the host's. This
	// is the events-side complement to the step routing below — together they
	// keep the host's events.jsonl/steps.jsonl free of subagent frames while the
	// child's own files capture them (Story 56.6 review: Timeline isolation).
	if c.ew != nil {
		ev := debug.NewEvent(0, c.createdAt, driverEventToSyscall(evtType), evt)
		debug.CompleteEvent(&ev, nil, nil, 0)
		_ = c.ew.WriteEvent(ev)
	}
	switch evtType {
	case "tool_call":
		content, _ := evt["content"].(string)
		switch content {
		case "started":
			c.driverStep++
			tool, _ := evt["tool"].(string)
			pathStr, _ := evt["path"].(string)
			cmd, _ := evt["command"].(string)
			toolPath := tool
			if pathStr != "" {
				toolPath = tool + ":" + pathStr
			}
			p := &pendingChildTool{
				step:     c.driverStep,
				tool:     tool,
				toolPath: toolPath,
				start:    time.Now(),
			}
			p.callID, _ = evt["call_id"].(string)
			if cmd != "" {
				p.inputBuf.WriteString(cmd)
			} else if pathStr != "" {
				p.inputBuf.WriteString(pathStr)
			}
			if p.callID != "" {
				c.pendings[p.callID] = p
			}
			c.order = append(c.order, p)
			c.current = p
		case "input_delta":
			if partial, ok := evt["partial_json"].(string); ok && c.current != nil {
				c.current.inputBuf.WriteString(partial)
			}
		case "completed":
			p := c.current
			if cid, _ := evt["call_id"].(string); cid != "" {
				if tracked, ok := c.pendings[cid]; ok {
					p = tracked
				}
			}
			if p != nil {
				if result, ok := evt["result"].(string); ok && result != "" {
					p.result = result
				}
				c.flush(p)
			}
		}
	case "assistant":
		// Capture authoritative tool input; flush is driven by the matching
		// tool_result (or finalize backstop) so the result is captured too.
		for _, b := range extractToolUseBlocks(evt) {
			if b.id == "" {
				continue
			}
			if b.input == nil {
				c.authInputs[b.id] = "{}"
			} else if data, err := json.Marshal(b.input); err == nil {
				c.authInputs[b.id] = string(data)
			}
			// A tool_use without a preceding `started` (no content_block_start in
			// the sidechain) still needs a pending slot so it can be flushed.
			if _, ok := c.pendings[b.id]; !ok {
				c.driverStep++
				p := &pendingChildTool{
					step:     c.driverStep,
					tool:     b.name,
					toolPath: b.name,
					start:    time.Now(),
					callID:   b.id,
				}
				c.pendings[b.id] = p
				c.order = append(c.order, p)
				c.current = p
			} else if c.pendings[b.id].tool == "" {
				c.pendings[b.id].tool = b.name
				c.pendings[b.id].toolPath = b.name
			}
		}
	case "user":
		for _, r := range extractToolResultBlocks(evt) {
			if r.toolUseID == "" {
				continue
			}
			if p, ok := c.pendings[r.toolUseID]; ok {
				p.result = r.content
				c.flush(p)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// cliSubagentTracker — owns all synthetic children for one host stream.
// ---------------------------------------------------------------------------

// cliSubagentTracker reconstructs and tracks every CLI subagent observed in a
// host process's stream. It is created lazily by observe.go's stream handler
// and accessed serially (stream events arrive on a single goroutine), so it
// needs no internal locking; ProcessHistory.Upsert handles its own.
type cliSubagentTracker struct {
	k        *KernelImpl
	host     *Process
	baseDir  string
	children map[string]*syntheticChild // keyed by parent Task tool_use id
}

// newCLISubagentTracker builds a tracker for the given host process. baseDir is
// resolved once so children land as siblings of the host's steps dir.
func (k *KernelImpl) newCLISubagentTracker(host *Process) *cliSubagentTracker {
	return &cliSubagentTracker{
		k:        k,
		host:     host,
		baseDir:  k.syntheticChildBaseDir(host),
		children: make(map[string]*syntheticChild),
	}
}

// syntheticChildBaseDir returns the step base directory under which synthetic
// children are written. Derived from the host's resolved steps dir
// (<base>/steps/<host-uuid>) when available so children are exact siblings of
// the host; falls back to ResolveStepBaseDir otherwise.
func (k *KernelImpl) syntheticChildBaseDir(host *Process) string {
	host.mu.Lock()
	stepsDir := host.stepsDir
	host.mu.Unlock()
	if stepsDir != "" {
		return filepath.Dir(filepath.Dir(stepsDir))
	}
	return k.ResolveStepBaseDir(host)
}

// getOrCreate returns the synthetic child for the given Task tool_use block,
// creating + registering + persisting it on first sight. Idempotent: a repeated
// tool_use id returns the existing child (deterministic UUID, AC8).
func (t *cliSubagentTracker) getOrCreate(b toolUseBlock) *syntheticChild {
	if c, ok := t.children[b.id]; ok {
		return c
	}
	now := time.Now()
	childUUID := syntheticChildUUID(t.host.UUID, b.id)

	intent := stringOf(b.input["description"])
	if intent == "" {
		intent = truncateUTF8Bytes(stringOf(b.input["prompt"]), 200)
	}
	model := stringOf(b.input["model"])
	if model == "" {
		model = t.host.Model
	}
	provider := stringOf(b.input["provider"])
	if provider == "" {
		provider = t.host.Provider
	}

	info := vfs.ProcInfo{
		PID:        0,
		PPID:       t.host.PID, // non-zero so tree builder uses the ParentUUID mount path
		UUID:       childUUID,
		ParentUUID: t.host.UUID,
		Synthetic:  true,
		State:      types.StateRunning,
		Intent:     intent,
		Provider:   provider,
		Model:      model,
		CreatedAt:  now, // deferred-work#765: never zero (ELAPSED render garbage)
	}

	c := &syntheticChild{
		taskID:     b.id,
		uuid:       childUUID,
		createdAt:  now,
		pendings:   make(map[string]*pendingChildTool),
		authInputs: make(map[string]string),
		info:       info,
	}
	// Synthetic children persist their own steps.jsonl + events.jsonl as
	// siblings of the host so the dashboard Steps view (steps.jsonl) and Timeline
	// (events.jsonl) both resolve them by UUID. A failure here — or an empty
	// baseDir (e.g. dataDir unset) — degrades to an in-memory-only node: it still
	// appears in the live tree via procHistory.Upsert below, but its steps/events
	// are dropped and vanish on daemon restart. Log a breadcrumb so the data loss
	// is diagnosable rather than silent (Story 56.6 review).
	if t.baseDir == "" {
		log.Printf("[cli-subagent] uuid=%s host=%s: empty step base dir — synthetic child steps/events will not be persisted",
			childUUID, t.host.UUID)
	} else {
		if sw, err := NewStepWriter(t.baseDir, childUUID); err == nil {
			c.sw = sw
		} else {
			log.Printf("[cli-subagent] uuid=%s host=%s: NewStepWriter failed: %v — child steps will not be persisted",
				childUUID, t.host.UUID, err)
		}
		if ew, err := NewEventWriter(t.baseDir, childUUID); err == nil {
			c.ew = ew
		} else {
			log.Printf("[cli-subagent] uuid=%s host=%s: NewEventWriter failed: %v — child events (Timeline) will not be persisted",
				childUUID, t.host.UUID, err)
		}
	}

	t.children[b.id] = c
	t.k.procHistory.Upsert(info)
	_ = SaveProcInfo(t.baseDir, info)
	return c
}

// route delivers a subagent-internal frame to its child when the frame's
// parent_tool_use_id matches a tracked Task id. Returns true when the frame was
// consumed (the host handler must then skip it to avoid polluting host steps).
func (t *cliSubagentTracker) route(evt map[string]any) bool {
	ptid := stringOf(evt["parent_tool_use_id"])
	if ptid == "" {
		return false
	}
	c, ok := t.children[ptid]
	if !ok {
		return false
	}
	c.feed(evt)
	return true
}

// finalizeByToolResult finalizes the child whose Task tool_use id matches a
// host-side tool_result (the subagent reported back). isError is the
// tool_result block's `is_error` flag and becomes the authoritative exit code
// (true→1, false/absent→0) so IsProcessFailed never falls back to the
// result-text heuristic — a successful report containing "fail"/"error" no
// longer renders red. Returns true when a child was finalized.
func (t *cliSubagentTracker) finalizeByToolResult(toolUseID, report string, isError bool) bool {
	c, ok := t.children[toolUseID]
	if !ok {
		return false
	}
	exitCode := 0
	if isError {
		exitCode = 1
	}
	t.finalize(c, report, exitCode, true)
	return true
}

// finalize transitions a child to Dead with the given report text, flushes any
// dangling internal steps, closes its step writer, and persists the snapshot.
//
// exitCodeSet=true is the tool_result path: exitCode is authoritative and
// persisted (ExitCodeSet round-trips via proc-info.json). exitCodeSet=false is
// the stream-end backstop: the subagent never reported back, so its outcome is
// unknown — Result is set to "interrupted" (the existing non-failure exception
// in internal/ui/symbols.go) and ExitReason to "interrupted" (the history
// stats Interrupted bucket key) instead of fabricating an exit code.
func (t *cliSubagentTracker) finalize(c *syntheticChild, report string, exitCode int, exitCodeSet bool) {
	if c.finalized {
		return
	}
	c.finalized = true
	c.flushAll()
	if c.sw != nil {
		_ = c.sw.Close()
		c.sw = nil
	}
	if c.ew != nil {
		_ = c.ew.Close()
		c.ew = nil
	}
	c.info.State = types.StateDead
	c.info.DeadAt = time.Now()
	switch {
	case report != "":
		c.info.Result = truncateUTF8Bytes(report, maxSubagentResultBytes)
	case exitCodeSet && exitCode == 0:
		// Empty report on a successful tool_result: leave a non-failure
		// sentinel — text-heuristic consumers (ui.StateBadge via
		// isFailedResult) treat an empty Result as failure, which would
		// contradict the authoritative exit code on the same row.
		c.info.Result = "done"
	case !exitCodeSet:
		// Stream-end backstop: Result carries the "interrupted" non-failure
		// exception for row/badge rendering, and ExitReason routes the entry
		// into the history stats Interrupted bucket
		// (internal/dashboard/tree/history.go keys on ExitReason, not Result).
		c.info.Result = "interrupted"
		c.info.ExitReason = exitReasonInterrupted
	}
	if exitCodeSet {
		c.info.ExitCode = exitCode
		c.info.ExitCodeSet = true
	}
	t.k.procHistory.Upsert(c.info)
	_ = SaveProcInfo(t.baseDir, c.info)
}

// finalizeAll finalizes every still-running child (stream end / error backstop,
// AC4 防泄漏). No exit code is recorded — the outcome is unknown.
func (t *cliSubagentTracker) finalizeAll() {
	for _, c := range t.children {
		if !c.finalized {
			t.finalize(c, "", 0, false)
		}
	}
}
