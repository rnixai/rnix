package memory

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// writebackChCap is the buffer size for the writeback job channel.
const writebackChCap = 16

const (
	writebackLLMTimeout   = 60 * time.Second
	writebackDrainTimeout = 5 * time.Second
	writebackMaxTokens    = 2000
)

// LLMCaller abstracts the LLM invocation for writeback extraction.
// Implemented by the daemon layer using DriverRegistry.
type LLMCaller interface {
	Call(ctx context.Context, systemPrompt string, userPrompt string, maxTokens int) (string, error)
}

// writebackJob carries the minimal data needed for async knowledge extraction.
type writebackJob struct {
	UUID       string
	StepsDir   string // .rnix/data/steps/<uuid>/
	ExitCode   int
	ExitReason string
}

// NewWritebackJob creates a writebackJob (exported for kernel integration).
func NewWritebackJob(uuid, stepsDir string, exitCode int, exitReason string) writebackJob {
	return writebackJob{
		UUID:       uuid,
		StepsDir:   stepsDir,
		ExitCode:   exitCode,
		ExitReason: exitReason,
	}
}

// extractionEntry represents a single knowledge entry extracted by the LLM.
type extractionEntry struct {
	Content string `json:"content"`
	Target  string `json:"target"`
}

// extractionResponse is the expected JSON structure from the LLM.
type extractionResponse struct {
	Entries []extractionEntry `json:"entries"`
}

// WritebackWorker consumes writebackJobs asynchronously and extracts
// knowledge from completed process conversations via an auxiliary LLM.
type WritebackWorker struct {
	ch              chan writebackJob
	store           *MemoryStore
	caller          LLMCaller
	cfg             WritebackConfig
	defaultProvider string
	wg              sync.WaitGroup
	stopOnce        sync.Once
	callerMu        sync.Mutex // protects caller replacement (for testing)
	recallIndex     *RecallIndex // optional, for incremental index updates (Story 35.4)
}

// NewWritebackWorker creates a new WritebackWorker.
func NewWritebackWorker(store *MemoryStore, caller LLMCaller, cfg WritebackConfig, defaultProvider string) *WritebackWorker {
	return &WritebackWorker{
		ch:              make(chan writebackJob, writebackChCap),
		store:           store,
		caller:          caller,
		cfg:             cfg,
		defaultProvider: defaultProvider,
	}
}

// Config returns the worker's writeback configuration.
func (w *WritebackWorker) Config() WritebackConfig {
	return w.cfg
}

// SetRecallIndex injects the recall index for incremental updates after knowledge extraction.
func (w *WritebackWorker) SetRecallIndex(ri *RecallIndex) {
	w.recallIndex = ri
}

// Start launches the background worker goroutine that consumes writeback jobs.
func (w *WritebackWorker) Start() {
	w.wg.Go(func() {
		for job := range w.ch {
			func() {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("[writeback] panic recovered for uuid=%s: %v", job.UUID, r)
					}
				}()
				w.processJob(job)
			}()
		}
	})
}

// Stop closes the job channel and waits for the worker to drain remaining jobs.
// If draining takes longer than writebackDrainTimeout, it returns anyway.
func (w *WritebackWorker) Stop() {
	w.stopOnce.Do(func() {
		close(w.ch)
	})
	done := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(writebackDrainTimeout):
		log.Printf("[writeback] drain timeout, abandoning remaining jobs")
	}
}

// Submit enqueues a writeback job. Non-blocking: if the channel is full,
// the job is dropped with a log warning.
func (w *WritebackWorker) Submit(job writebackJob) {
	select {
	case w.ch <- job:
	default:
		log.Printf("[writeback] channel full, dropping job uuid=%s", job.UUID)
	}
}

// replaceCaller swaps the LLM caller (used by tests for panic recovery verification).
func (w *WritebackWorker) replaceCaller(caller LLMCaller) {
	w.callerMu.Lock()
	defer w.callerMu.Unlock()
	w.caller = caller
}

// getCaller returns the current LLM caller under lock.
func (w *WritebackWorker) getCaller() LLMCaller {
	w.callerMu.Lock()
	defer w.callerMu.Unlock()
	return w.caller
}

// processJob performs the full knowledge extraction pipeline for one job.
func (w *WritebackWorker) processJob(job writebackJob) {
	// 1. Read steps.jsonl
	stepsPath := filepath.Join(job.StepsDir, "steps.jsonl")
	stepsData, err := os.ReadFile(stepsPath)
	if err != nil {
		log.Printf("[writeback] failed to read steps uuid=%s: %v", job.UUID, err)
		return
	}

	// 2. Read events.jsonl (best effort)
	eventsPath := filepath.Join(job.StepsDir, "events.jsonl")
	eventsData, _ := os.ReadFile(eventsPath)

	// 3. Build extraction prompt
	userPrompt := w.buildExtractionPrompt(stepsData, eventsData)

	// 4. Get LLM caller
	caller := w.getCaller()
	if caller == nil {
		log.Printf("[writeback] no LLM caller available, skipping uuid=%s", job.UUID)
		return
	}

	// 5. Call auxiliary LLM
	ctx, cancel := context.WithTimeout(context.Background(), writebackLLMTimeout)
	defer cancel()

	systemPrompt := loadPromptTemplate("writeback_extract.txt")
	resp, err := caller.Call(ctx, systemPrompt, userPrompt, writebackMaxTokens)
	if err != nil {
		log.Printf("[writeback] LLM call failed uuid=%s: %v", job.UUID, err)
		return
	}

	// 6. Parse structured knowledge entries
	entries, err := parseExtractionResponse(resp)
	if err != nil {
		log.Printf("[writeback] failed to parse LLM response uuid=%s: %v", job.UUID, err)
		return
	}

	// 7. Write each entry to MemoryStore
	written := 0
	for _, entry := range entries {
		target := entry.Target
		if target == "" {
			target = "memory"
		}
		if err := w.store.Add(target, entry.Content); err != nil {
			log.Printf("[writeback] store.Add failed uuid=%s target=%s: %v", job.UUID, target, err)
			// Continue with remaining entries
			continue
		}
		written++
	}
	if written > 0 {
		log.Printf("[writeback] extracted %d knowledge entries from uuid=%s", written, job.UUID)
	}

	// 8. Incremental recall index update (Story 35.4)
	if w.recallIndex != nil {
		if err := w.recallIndex.IndexProcess(job.UUID, job.StepsDir); err != nil {
			log.Printf("[writeback] recall index update failed uuid=%s: %v", job.UUID, err)
		}
	}
}

// buildExtractionPrompt constructs the user prompt for the auxiliary LLM,
// including the conversation steps data and optional events data.
func (w *WritebackWorker) buildExtractionPrompt(stepsData []byte, eventsData []byte) string {
	var b strings.Builder
	b.WriteString("## Conversation Steps\n\n")
	b.Write(stepsData)

	if len(eventsData) > 0 {
		b.WriteString("\n\n## Syscall Events\n\n")
		b.Write(eventsData)
	}

	return b.String()
}

// parseExtractionResponse parses the LLM response into extraction entries.
// Handles raw JSON, markdown-fenced JSON, and empty/malformed responses.
func parseExtractionResponse(content string) ([]extractionEntry, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, fmt.Errorf("empty response")
	}

	// Strip markdown code fences if present
	if strings.HasPrefix(content, "```") {
		lines := strings.Split(content, "\n")
		// Remove first line (```json or ```) and last line (```)
		if len(lines) >= 3 {
			end := len(lines) - 1
			for end > 0 && strings.TrimSpace(lines[end]) == "" {
				end--
			}
			if strings.TrimSpace(lines[end]) == "```" {
				lines = lines[1:end]
			} else {
				lines = lines[1:]
			}
			content = strings.Join(lines, "\n")
		}
	}

	content = strings.TrimSpace(content)

	var resp extractionResponse
	if err := json.Unmarshal([]byte(content), &resp); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	return resp.Entries, nil
}

// ShouldExtract determines whether a completed process should trigger
// async knowledge extraction. It performs a quick scan of steps.jsonl
// to count tool_call actions.
func ShouldExtract(cfg WritebackConfig, exitCode int, exitReason string, stepsDir string) bool {
	if !cfg.Enabled {
		return false
	}
	if exitCode != 0 {
		return false
	}
	if cfg.RequireSuccess && !strings.Contains(exitReason, "completed") {
		return false
	}
	// Quick scan: count tool_call actions in steps.jsonl
	stepsPath := filepath.Join(stepsDir, "steps.jsonl")
	toolCalls := countToolCalls(stepsPath)
	return toolCalls >= cfg.TriggerThreshold
}

// countToolCalls scans steps.jsonl and counts entries with action == "tool_call".
func countToolCalls(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()

	count := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		// Lightweight extraction: only parse the "action" field
		var partial struct {
			Action string `json:"action"`
		}
		if json.Unmarshal(scanner.Bytes(), &partial) == nil && partial.Action == "tool_call" {
			count++
		}
	}
	return count
}
