package kernel

// Story 62.4 — Codex multi_agent_v2 subagent reconstruction from rollout files.
//
// Codex `--json` exposes collab_tool_call host activity, but subagent-internal
// function_call/function_call_output frames live only in
// ~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl. This tracker polls that side
// channel and writes synthetic child ProcInfo + steps/events in the same shape
// as Story 56.6 CLI subagents.

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

var codexRolloutNamespace = uuid.MustParse("62c0d3e0-6240-5a6b-9c0d-000000000624")

var codexRolloutWallTimeRE = regexp.MustCompile(`Wall time:\s*([0-9]+(?:\.[0-9]+)?)\s*seconds`)

const codexRolloutPollInterval = time.Second

var codexRolloutDirForNow = defaultCodexRolloutDir

type codexRolloutRecord struct {
	Timestamp string         `json:"timestamp"`
	Type      string         `json:"type"`
	Payload   map[string]any `json:"payload"`
}

type pendingCodexRolloutTool struct {
	step   int
	name   string
	start  time.Time
	input  string
	callID string
}

type codexRolloutChild struct {
	threadID  string
	filePath  string
	seenLines int

	uuid      string
	createdAt time.Time
	info      vfs.ProcInfo
	sw        *StepWriter
	ew        *EventWriter

	driverStep int
	pending    map[string]*pendingCodexRolloutTool
	finalized  bool
}

type codexRolloutTracker struct {
	k              *KernelImpl
	host           *Process
	baseDir        string
	parentThreadID string
	rolloutDir     string

	children     map[string]*codexRolloutChild // keyed by child thread id
	childByFile  map[string]*codexRolloutChild
	rejectedFile map[string]bool // negative cache for unrelated rollout files
	warnedBadDir bool

	stopOnce sync.Once
	stopCh   chan struct{}
	doneCh   chan struct{}
}

func newCodexRolloutTracker(k *KernelImpl, host *Process, baseDir, parentThreadID, rolloutDir string) *codexRolloutTracker {
	return &codexRolloutTracker{
		k:              k,
		host:           host,
		baseDir:        baseDir,
		parentThreadID: parentThreadID,
		rolloutDir:     rolloutDir,
		children:       make(map[string]*codexRolloutChild),
		childByFile:    make(map[string]*codexRolloutChild),
		rejectedFile:   make(map[string]bool),
		stopCh:         make(chan struct{}),
		doneCh:         make(chan struct{}),
	}
}

func defaultCodexRolloutDir(now time.Time) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	root := os.Getenv("CODEX_HOME")
	if root == "" {
		root = filepath.Join(home, ".codex")
	}
	return filepath.Join(root, "sessions", now.Format("2006"), now.Format("01"), now.Format("02"))
}

func (t *codexRolloutTracker) start() {
	_ = t.scanOnce()
	if len(t.children) == 0 {
		close(t.doneCh)
		return
	}
	go func() {
		defer close(t.doneCh)
		ticker := time.NewTicker(codexRolloutPollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_ = t.scanOnce()
			case <-t.stopCh:
				_ = t.scanOnce()
				t.finalizeAll()
				return
			}
		}
	}()
}

func (t *codexRolloutTracker) stop() {
	t.stopOnce.Do(func() {
		close(t.stopCh)
		<-t.doneCh
	})
}

func (t *codexRolloutTracker) scanOnce() error {
	if t == nil || t.rolloutDir == "" || t.parentThreadID == "" {
		return nil
	}
	entries, err := os.ReadDir(t.rolloutDir)
	if err != nil {
		if !t.warnedBadDir {
			log.Printf("[codex-rollout] parent_thread_id=%s: cannot read %s: %v — subagent reconstruction disabled",
				t.parentThreadID, t.rolloutDir, err)
			t.warnedBadDir = true
		}
		return nil
	}
	var paths []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, "rollout-") && strings.HasSuffix(name, ".jsonl") {
			paths = append(paths, filepath.Join(t.rolloutDir, name))
		}
	}
	sort.Strings(paths)
	for _, path := range paths {
		if t.rejectedFile[path] {
			continue
		}
		if err := t.scanFile(path); err != nil {
			if errors.Is(err, errCodexRolloutUnrelated) {
				t.rejectedFile[path] = true
			} else {
				log.Printf("[codex-rollout] scan %s: %v", path, err)
			}
		}
	}
	return nil
}

var errCodexRolloutUnrelated = errors.New("codex rollout unrelated")

func (t *codexRolloutTracker) scanFile(path string) error {
	child := t.childByFile[path]
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 256*1024), 16*1024*1024)
	lineNo := 0
	lastGoodLine := 0
	if child != nil {
		lastGoodLine = child.seenLines
	}
	processed := false
	for sc.Scan() {
		lineNo++
		if child != nil && lineNo <= child.seenLines {
			continue
		}
		var rec codexRolloutRecord
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			continue
		}
		if child == nil {
			meta, ok := parseCodexSubagentMeta(rec)
			if !ok {
				continue
			}
			if meta.parentThreadID != t.parentThreadID {
				return errCodexRolloutUnrelated
			}
			child = t.getOrCreate(meta, path, parseRolloutTimestamp(rec.Timestamp))
			t.childByFile[path] = child
		}
		t.processRecord(child, rec)
		processed = true
		lastGoodLine = lineNo
	}
	if err := sc.Err(); err != nil {
		return err
	}
	if child != nil {
		child.seenLines = lastGoodLine
	}
	if !processed && child == nil {
		return errCodexRolloutUnrelated
	}
	return nil
}

type codexSubagentMeta struct {
	threadID       string
	parentThreadID string
	depth          int
	agentPath      string
	nickname       string
}

func parseCodexSubagentMeta(rec codexRolloutRecord) (codexSubagentMeta, bool) {
	if rec.Type != "session_meta" || rec.Payload == nil {
		return codexSubagentMeta{}, false
	}
	source := mapOf(rec.Payload["source"])
	subagent := mapOf(source["subagent"])
	threadSpawn := mapOf(subagent["thread_spawn"])
	parentThreadID := stringOf(threadSpawn["parent_thread_id"])
	if parentThreadID == "" {
		return codexSubagentMeta{}, false
	}
	depth := intOf(threadSpawn["depth"])
	threadID := stringOf(rec.Payload["id"])
	if threadID == "" {
		threadID = stringOf(rec.Payload["thread_id"])
	}
	return codexSubagentMeta{
		threadID:       threadID,
		parentThreadID: parentThreadID,
		depth:          depth,
		agentPath:      stringOf(threadSpawn["agent_path"]),
		nickname:       stringOf(threadSpawn["agent_nickname"]),
	}, true
}

func (t *codexRolloutTracker) getOrCreate(meta codexSubagentMeta, path string, createdAt time.Time) *codexRolloutChild {
	key := meta.threadID
	if key == "" {
		key = path
	}
	if c, ok := t.children[key]; ok {
		return c
	}
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	childUUID := uuid.NewSHA1(codexRolloutNamespace, []byte(t.host.UUID+"\x00"+key)).String()
	intent := meta.nickname
	if intent == "" {
		intent = filepath.Base(meta.agentPath)
	}
	if intent == "." || intent == string(filepath.Separator) {
		intent = meta.agentPath
	}
	if intent == "" {
		intent = "codex subagent"
	}
	info := vfs.ProcInfo{
		PID:        0,
		PPID:       t.host.PID,
		UUID:       childUUID,
		ParentUUID: t.host.UUID,
		Synthetic:  true,
		State:      types.StateRunning,
		Intent:     intent,
		Provider:   t.host.Provider,
		Model:      t.host.Model,
		CreatedAt:  createdAt,
		DriverMeta: map[string]string{
			"codex_thread_id":        meta.threadID,
			"codex_parent_thread_id": meta.parentThreadID,
			"codex_agent_path":       meta.agentPath,
			"codex_agent_nickname":   meta.nickname,
			"codex_rollout_file":     path,
			"codex_depth":            fmt.Sprintf("%d", meta.depth),
		},
	}
	c := &codexRolloutChild{
		threadID:  key,
		filePath:  path,
		uuid:      childUUID,
		createdAt: createdAt,
		info:      info,
		pending:   make(map[string]*pendingCodexRolloutTool),
	}
	if t.baseDir != "" {
		if sw, err := NewStepWriter(t.baseDir, childUUID); err == nil {
			c.sw = sw
		} else {
			log.Printf("[codex-rollout] uuid=%s: NewStepWriter failed: %v", childUUID, err)
		}
		if ew, err := NewEventWriter(t.baseDir, childUUID); err == nil {
			c.ew = ew
		} else {
			log.Printf("[codex-rollout] uuid=%s: NewEventWriter failed: %v", childUUID, err)
		}
	}
	t.children[key] = c
	t.k.procHistory.Upsert(info)
	_ = SaveProcInfo(t.baseDir, info)
	return c
}

func (t *codexRolloutTracker) processRecord(c *codexRolloutChild, rec codexRolloutRecord) {
	if c == nil || rec.Payload == nil {
		return
	}
	ts := parseRolloutTimestamp(rec.Timestamp)
	switch rec.Type {
	case "response_item":
		switch stringOf(rec.Payload["type"]) {
		case "function_call":
			t.processFunctionCall(c, rec.Payload, ts)
		case "function_call_output":
			t.processFunctionCallOutput(c, rec.Payload, ts)
		case "message", "reasoning":
			t.writeChildEvent(c, rec.Type, rec.Payload, ts, 0)
		}
	case "event_msg":
		payloadType := stringOf(rec.Payload["type"])
		switch payloadType {
		case "task_complete", "task_completed", "task_failed":
			report := stringOf(rec.Payload["message"])
			t.finalize(c, report)
		default:
			t.writeChildEvent(c, payloadType, rec.Payload, ts, 0)
		}
	}
}

func (t *codexRolloutTracker) processFunctionCall(c *codexRolloutChild, payload map[string]any, ts time.Time) {
	c.driverStep++
	name := stringOf(payload["name"])
	callID := stringOf(payload["call_id"])
	input := stringOf(payload["arguments"])
	if name == "" {
		name = "function_call"
	}
	p := &pendingCodexRolloutTool{
		step:   c.driverStep,
		name:   name,
		start:  ts,
		input:  input,
		callID: callID,
	}
	if p.start.IsZero() {
		p.start = time.Now()
	}
	if callID != "" {
		c.pending[callID] = p
	}
	t.writeChildEvent(c, "tool_call", map[string]any{
		"type":    "tool_call",
		"content": "started",
		"tool":    name,
		"call_id": callID,
		"args":    input,
		"subtype": "started",
	}, ts, 0)
}

func (t *codexRolloutTracker) processFunctionCallOutput(c *codexRolloutChild, payload map[string]any, ts time.Time) {
	callID := stringOf(payload["call_id"])
	p := c.pending[callID]
	if p == nil {
		c.driverStep++
		p = &pendingCodexRolloutTool{step: c.driverStep, name: "function_call", start: ts, callID: callID}
	}
	result := stringOf(payload["output"])
	dur := ts.Sub(p.start)
	if wall, ok := parseCodexWallTime(result); ok {
		dur = wall
	}
	if dur < 0 {
		dur = 0
	}
	summary := driverToolSummary(p.name, p.name, p.input)
	if c.sw != nil {
		_ = c.sw.WriteStep(types.StepRecord{
			Step:         p.step,
			Timestamp:    sinceOrZero(ts, c.createdAt),
			Action:       p.name,
			Summary:      summary,
			ToolPath:     p.name,
			ToolInput:    p.input,
			ToolResult:   result,
			ToolDuration: dur,
		})
	}
	t.writeChildEvent(c, "tool_call", map[string]any{
		"type":    "tool_call",
		"content": "completed",
		"tool":    p.name,
		"call_id": callID,
		"result":  result,
		"subtype": "completed",
	}, ts, dur)
	if callID != "" {
		delete(c.pending, callID)
	}
}

func (t *codexRolloutTracker) writeChildEvent(c *codexRolloutChild, evtType string, args map[string]any, ts time.Time, dur time.Duration) {
	if c.ew == nil {
		return
	}
	syscallName := driverEventToSyscall(evtType)
	ev := types.SyscallEvent{
		Timestamp: sinceOrZero(ts, c.createdAt),
		PID:       0,
		Syscall:   syscallName,
		Args:      args,
		Duration:  dur,
	}
	_ = c.ew.WriteEvent(ev)
}

func (t *codexRolloutTracker) finalize(c *codexRolloutChild, report string) {
	if c == nil || c.finalized {
		return
	}
	c.finalized = true
	for _, p := range c.pending {
		if c.sw != nil {
			_ = c.sw.WriteStep(types.StepRecord{
				Step:      p.step,
				Timestamp: sinceOrZero(p.start, c.createdAt),
				Action:    p.name,
				Summary:   driverToolSummary(p.name, p.name, p.input),
				ToolPath:  p.name,
				ToolInput: p.input,
				ToolError: "interrupted",
			})
		}
	}
	c.pending = nil
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
	if report != "" {
		c.info.Result = truncateUTF8Bytes(report, maxSubagentResultBytes)
	}
	t.k.procHistory.Upsert(c.info)
	_ = SaveProcInfo(t.baseDir, c.info)
}

func (t *codexRolloutTracker) finalizeAll() {
	for _, c := range t.children {
		t.finalize(c, "")
	}
}

func parseRolloutTimestamp(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if ts, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return ts
	}
	return time.Time{}
}

func sinceOrZero(ts, base time.Time) time.Duration {
	if ts.IsZero() || base.IsZero() {
		return 0
	}
	d := ts.Sub(base)
	if d < 0 {
		return 0
	}
	return d
}

func parseCodexWallTime(output string) (time.Duration, bool) {
	m := codexRolloutWallTimeRE.FindStringSubmatch(output)
	if len(m) != 2 {
		return 0, false
	}
	seconds, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, false
	}
	return time.Duration(seconds * float64(time.Second)), true
}

func mapOf(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func intOf(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	default:
		return 0
	}
}
