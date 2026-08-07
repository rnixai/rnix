package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/goccy/go-yaml"

	"github.com/rnixai/rnix/internal/xsync"
	"github.com/rnixai/rnix/vfs"
)

// replayToolCall mirrors ToolCall in the on-disk script schema. ID is
// optional — loadReplayScript fills a deterministic one when absent.
type replayToolCall struct {
	ID    string         `yaml:"id,omitempty"`
	Name  string         `yaml:"name"`
	Input map[string]any `yaml:"input,omitempty"`
}

// replayUsage is the optional token-usage block of a scripted response.
// Any field left unset is a deterministic 0 (no estimation, no defaults
// borrowed from a "typical" call — Tier1 replay must never introduce
// nondeterminism through invented numbers).
type replayUsage struct {
	InputTokens              int `yaml:"input_tokens,omitempty"`
	OutputTokens             int `yaml:"output_tokens,omitempty"`
	CachedInputTokens        int `yaml:"cached_input_tokens,omitempty"`
	CacheCreationInputTokens int `yaml:"cache_creation_input_tokens,omitempty"`
}

// replayResponse is one scripted LLM turn. At least one of Content /
// ToolCalls must be non-empty (loadReplayScript enforces this at load time).
type replayResponse struct {
	Content    string           `yaml:"content,omitempty"`
	Reasoning  string           `yaml:"reasoning,omitempty"`
	ToolCalls  []replayToolCall `yaml:"tool_calls,omitempty"`
	Usage      *replayUsage     `yaml:"usage,omitempty"`
	StopReason string           `yaml:"stop_reason,omitempty"`
}

// replayScript is the parsed form of a *.responses.yaml fixture.
type replayScript struct {
	Version   string           `yaml:"version"`
	Responses []replayResponse `yaml:"responses"`
}

// loadReplayScript reads, parses and validates a replay script file.
// Validation failures name both the file path and the offending entry index
// (1-based) so a broken fixture is easy to locate.
func loadReplayScript(path string) (*replayScript, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("replay: read script %s: %w", path, err)
	}

	var script replayScript
	// Strict decoding so a fixture typo (e.g. stop_reasn:, usge:) fails loud at
	// load time instead of silently decoding to a zero value — a Tier1 script
	// must never drift through an unnoticed misspelled field.
	if err := yaml.UnmarshalWithOptions(data, &script, yaml.Strict()); err != nil {
		return nil, fmt.Errorf("replay: parse script %s: %w", path, err)
	}

	if len(script.Responses) == 0 {
		return nil, fmt.Errorf("replay: script %s: responses list is empty", path)
	}

	for i := range script.Responses {
		r := &script.Responses[i]
		if r.Content == "" && len(r.ToolCalls) == 0 {
			return nil, fmt.Errorf("replay: script %s entry #%d: must set content and/or tool_calls", path, i+1)
		}
		for j := range r.ToolCalls {
			tc := &r.ToolCalls[j]
			if tc.Name == "" {
				return nil, fmt.Errorf("replay: script %s entry #%d tool_call #%d: name is required", path, i+1, j+1)
			}
			// Deterministic id so replayed tool_calls are stable across runs
			// even when the fixture author omits id (68.1 Task 1).
			if tc.ID == "" {
				tc.ID = fmt.Sprintf("replay-s%d-c%d", i+1, j+1)
			}
		}
	}

	return &script, nil
}

// replaySession holds one process's cursor into a loaded script. Sessions are
// keyed by CallerUUID (裁决 4) so the shared driver singleton never mixes up
// two concurrent processes' progress through the same or different scripts.
//
// Once idx reaches the end of Responses — whether via ordinary consumption or
// an over-run attempt — the session is permanently "exhausted": script is
// dropped (frees the parsed YAML) but idx/total remain as a tombstone. This
// is deliberate: if the map entry were removed instead, a later call for the
// same key would silently reload the file and replay response #1 again —
// exactly the "repeat/restart" behavior 裁决 3 rules out in favor of fail-loud.
// A permanently exhausted tombstone costs a few bytes and guarantees every
// post-exhaustion call keeps failing loud, forever, for that key.
type replaySession struct {
	mu        sync.Mutex
	script    *replayScript
	idx       int
	total     int
	exhausted bool
	// loadedPath is the script this cursor was first bound to. It is set once
	// at construction and never mutated, so it is safe to read without mu.
	// sessionFor refuses to reuse a session for a different path (裁决 4: one
	// script per CallerUUID cursor) rather than silently continue the old one.
	loadedPath string
}

func newReplaySession(script *replayScript, path string) *replaySession {
	return &replaySession{script: script, total: len(script.Responses), loadedPath: path}
}

// consume returns the next scripted response and its 0-based index, or a
// non-transient error once the script is exhausted. scriptPath is only used
// to compose the error message.
func (s *replaySession) consume(scriptPath string) (replayResponse, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.exhausted || s.idx >= s.total {
		s.exhausted = true
		s.script = nil
		return replayResponse{}, s.idx, fmt.Errorf("replay script exhausted after %d responses: %s", s.total, scriptPath)
	}

	i := s.idx
	resp := s.script.Responses[i]
	s.idx++
	if s.idx >= s.total {
		s.exhausted = true
		s.script = nil
	}
	return resp, i, nil
}

// ReplayDriver implements LLMDriver by replaying a fixed, ordered sequence of
// scripted responses from a YAML file — no network, no subprocess, no LLM.
// It exists to give the agtest Tier1 suite (Epic 68) a zero-flake, zero-cost,
// offline-capable LLM substitute: the process still runs the real daemon /
// spawn / VFS / observability stack, only the LLM output is deterministic.
//
// Script path resolution is the two-tier req.Model channel (裁决 2): a
// per-spawn req.Model always wins; an empty req.Model falls back to the
// driver instance's configured default script (providers.yaml default_model).
type ReplayDriver struct {
	name          string
	defaultScript string
	sessions      *xsync.SyncMap[string, *replaySession]
}

// ReplayOption configures a ReplayDriver.
type ReplayOption func(*ReplayDriver)

// WithReplayDefaultScript sets the instance-level default script path, used
// when a spawn request does not override req.Model.
func WithReplayDefaultScript(path string) ReplayOption {
	return func(d *ReplayDriver) {
		d.defaultScript = path
	}
}

// NewReplayDriver creates a new ReplayDriver. name is the provider name
// (mirrors the API-family driver constructors — e.g. NewOpenAIDriver — since
// unlike the CLI drivers, multiple named replay providers can coexist).
func NewReplayDriver(name string, opts ...ReplayOption) *ReplayDriver {
	d := &ReplayDriver{
		name:     name,
		sessions: xsync.NewSyncMap[string, *replaySession](),
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

var (
	_ LLMDriver         = (*ReplayDriver)(nil)
	_ ToolCallingDriver = (*ReplayDriver)(nil)
)

// sessionKey returns the per-process cursor key (CallerUUID), falling back
// to a shared "default" key when unset (defensively mutex-guarded inside
// replaySession — see 裁决 4).
func (d *ReplayDriver) sessionKey(req LLMRequest) string {
	if req.CallerUUID != "" {
		return req.CallerUUID
	}
	return "default"
}

// resolveScriptPath implements 裁决 2's two-tier model→script-path channel.
func (d *ReplayDriver) resolveScriptPath(req LLMRequest) (string, error) {
	path := req.Model
	if path == "" {
		path = d.defaultScript
	}
	if path == "" {
		return "", fmt.Errorf("replay [%s]: no script path — set req.Model (per-spawn, e.g. --model /abs/case.responses.yaml) or default_model in providers.yaml", d.name)
	}
	if !filepath.IsAbs(path) && req.ProjectDir != "" {
		path = filepath.Join(req.ProjectDir, path)
	}
	return path, nil
}

// sessionFor returns the existing session for key, loading and registering a
// new one from scriptPath on first use. LoadOrStore ensures two concurrent
// callers racing under the same fallback "default" key never clobber each
// other's freshly-loaded session (only one load wins; the other discards its
// redundant parse and reuses the winner).
func (d *ReplayDriver) sessionFor(key, scriptPath string) (*replaySession, error) {
	if sess, ok := d.sessions.Load(key); ok {
		return d.ensurePath(sess, key, scriptPath)
	}
	script, err := loadReplayScript(scriptPath)
	if err != nil {
		return nil, err
	}
	// LoadOrStore may return another concurrent caller's session (racing under
	// the same key); ensurePath then also catches a same-key/different-path
	// race, not just the sequential fast path above.
	actual, _ := d.sessions.LoadOrStore(key, newReplaySession(script, scriptPath))
	return d.ensurePath(actual, key, scriptPath)
}

// ensurePath fails loud when an existing session is asked to serve a different
// script than the one it was bound to. Silently continuing the old script's
// cursor (the pre-review behavior) hid mid-flight model switches and produced
// an exhaustion error naming the wrong file.
func (d *ReplayDriver) ensurePath(sess *replaySession, key, scriptPath string) (*replaySession, error) {
	if sess.loadedPath != scriptPath {
		return nil, fmt.Errorf("replay [%s]: session %q already bound to script %s, refusing mid-flight switch to %s (裁决 4: one script per CallerUUID cursor)", d.name, key, sess.loadedPath, scriptPath)
	}
	return sess, nil
}

// call is the shared implementation behind Call/CallWithTools — tools is
// intentionally ignored (the scripted response already declares whatever
// tool_calls the fixture author wants for this step).
func (d *ReplayDriver) call(ctx context.Context, req LLMRequest) (*LLMResponse, error) {
	scriptPath, err := d.resolveScriptPath(req)
	if err != nil {
		llmErr := NewLLMError(d.name, 0, err)
		d.captureRaw(ctx, scriptPath, req, -1, nil, llmErr)
		return nil, llmErr
	}

	sess, err := d.sessionFor(d.sessionKey(req), scriptPath)
	if err != nil {
		llmErr := NewLLMError(d.name, 0, err)
		d.captureRaw(ctx, scriptPath, req, -1, nil, llmErr)
		return nil, llmErr
	}

	resp, idx, err := sess.consume(scriptPath)
	if err != nil {
		llmErr := NewLLMError(d.name, 0, err)
		d.captureRaw(ctx, scriptPath, req, idx, nil, llmErr)
		return nil, llmErr
	}

	llmResp := responseToLLMResponse(resp)
	d.captureRaw(ctx, scriptPath, req, idx, llmResp, nil)
	return llmResp, nil
}

func (d *ReplayDriver) Call(ctx context.Context, req LLMRequest) (*LLMResponse, error) {
	return d.call(ctx, req)
}

func (d *ReplayDriver) CallWithTools(ctx context.Context, req LLMRequest, _ []ToolDef) (*LLMResponse, error) {
	return d.call(ctx, req)
}

// stream is the shared implementation behind Stream/StreamWithTools. Event
// sequence is content (once, skipped if empty) → done (never carries Content
// — vfsfile.go's done handler Resets the accumulated content on a non-empty
// Content field, so leaving it empty routes through the same path every
// other driver family uses). A non-empty Reasoning is emitted as its own
// "reasoning" event before content (vfsfile.go accumulates Reasoning only
// from "reasoning" events, never from done).
func (d *ReplayDriver) stream(ctx context.Context, req LLMRequest) (<-chan StreamEvent, error) {
	scriptPath, err := d.resolveScriptPath(req)
	if err != nil {
		llmErr := NewLLMError(d.name, 0, err)
		d.captureRaw(ctx, scriptPath, req, -1, nil, llmErr)
		return nil, llmErr
	}

	sess, err := d.sessionFor(d.sessionKey(req), scriptPath)
	if err != nil {
		llmErr := NewLLMError(d.name, 0, err)
		d.captureRaw(ctx, scriptPath, req, -1, nil, llmErr)
		return nil, llmErr
	}

	resp, idx, err := sess.consume(scriptPath)
	if err != nil {
		llmErr := NewLLMError(d.name, 0, err)
		d.captureRaw(ctx, scriptPath, req, idx, nil, llmErr)
		return nil, llmErr
	}

	llmResp := responseToLLMResponse(resp)
	d.captureRaw(ctx, scriptPath, req, idx, llmResp, nil)

	ch := make(chan StreamEvent, streamChanBuffer)
	go func() {
		defer close(ch)

		if llmResp.Reasoning != "" {
			select {
			case ch <- StreamEvent{Type: "reasoning", Content: llmResp.Reasoning}:
			case <-ctx.Done():
				return
			}
		}
		if llmResp.Content != "" {
			select {
			case ch <- StreamEvent{Type: "content", Content: llmResp.Content}:
			case <-ctx.Done():
				return
			}
		}

		done := StreamEvent{
			Type:                     "done",
			TokensUsed:               llmResp.TokensUsed,
			InputTokens:              llmResp.InputTokens,
			OutputTokens:             llmResp.OutputTokens,
			CachedInputTokens:        llmResp.CachedInputTokens,
			CacheCreationInputTokens: llmResp.CacheCreationInputTokens,
			StopReason:               llmResp.StopReason,
			ToolCalls:                llmResp.ToolCalls,
		}
		select {
		case ch <- done:
		case <-ctx.Done():
		}
	}()
	return ch, nil
}

func (d *ReplayDriver) Stream(ctx context.Context, req LLMRequest) (<-chan StreamEvent, error) {
	return d.stream(ctx, req)
}

func (d *ReplayDriver) StreamWithTools(ctx context.Context, req LLMRequest, _ []ToolDef) (<-chan StreamEvent, error) {
	return d.stream(ctx, req)
}

// Info returns driver metadata. DefaultModel surfaces the configured default
// script path — a deliberate overload of the field (裁决 2), documented here
// and in providers.yaml examples.
func (d *ReplayDriver) Info() DriverInfo {
	return DriverInfo{
		Name:         d.name,
		Provider:     d.name,
		DefaultModel: d.defaultScript,
		DriverType:   DriverReplay,
	}
}

// responseToLLMResponse converts a scripted response into the driver-neutral
// LLMResponse. Usage fields default to the deterministic zero value when the
// fixture omits them; TokensUsed is input+output (aligned with the
// anthropic.go precedent for a declared-usage total).
func responseToLLMResponse(r replayResponse) *LLMResponse {
	resp := &LLMResponse{
		Content:    r.Content,
		Reasoning:  r.Reasoning,
		StopReason: r.StopReason,
		ToolCalls:  toReplayToolCalls(r.ToolCalls),
	}
	if r.Usage != nil {
		resp.InputTokens = r.Usage.InputTokens
		resp.OutputTokens = r.Usage.OutputTokens
		resp.CachedInputTokens = r.Usage.CachedInputTokens
		resp.CacheCreationInputTokens = r.Usage.CacheCreationInputTokens
		resp.TokensUsed = r.Usage.InputTokens + r.Usage.OutputTokens
	}
	return resp
}

func toReplayToolCalls(rtc []replayToolCall) []ToolCall {
	if len(rtc) == 0 {
		return nil
	}
	out := make([]ToolCall, len(rtc))
	for i, tc := range rtc {
		out[i] = ToolCall{ID: tc.ID, Name: tc.Name, Input: tc.Input}
	}
	return out
}

// captureRaw fills the ctx-scoped raw capture sink (Story 56's Epic 56 raw
// request/response recording, extended here to 9/9 with Kind="replay"). It
// is a no-op when no sink is attached (writeCall/writeStream always attach
// one, so this only skips in the rare case a caller invokes the driver
// directly outside that path — e.g. a unit test with no ctx sink).
//
// Response.body is a single JSON string, never a nested map: kernel's
// truncateRawCapture (raw_writer.go) only walks string-valued map fields when
// bounding record size, so a nested object would silently escape truncation
// (56.3 裁决 3 field convention, reused here for the same reason).
func (d *ReplayDriver) captureRaw(ctx context.Context, scriptPath string, req LLMRequest, responseIndex int, resp *LLMResponse, callErr error) {
	sink := rawSinkFromContext(ctx)
	if sink == nil {
		return
	}
	rc := &vfs.RawCapture{
		TsMs: time.Now().UnixMilli(),
		Kind: "replay",
		Request: map[string]any{
			"script_path":    scriptPath,
			"response_index": responseIndex,
			"model":          req.Model,
			"intent":         req.Intent,
		},
	}
	if resp != nil && callErr == nil {
		if body, err := json.Marshal(resp); err == nil {
			rc.Response = map[string]any{"body": string(body)}
		}
	}
	sink.set(rc)
}
