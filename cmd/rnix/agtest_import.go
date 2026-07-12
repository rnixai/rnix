package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
	"github.com/spf13/cobra"
)

// =============================================================================
// Story 68.3 Task 2 — `rnix agtest import <uuid>` reads a persisted process
// run directly from disk (steps.jsonl / proc-info.json / events.jsonl — zero
// IPC, daemon-down safe, 裁决 4) and generates a Tier1 case-file + replay
// response-script SKELETON for human review. Assertions are emitted only as
// YAML comments (裁决 5): the generated case has no live `assert:` block, so
// agtest.ValidateTier1 rejects it (rule 2) if it were ever dropped straight
// into tests/agtest/tier1/ — the tool deliberately does not produce a
// ready-to-run case. Output lands in tests/agtest/imported/ (gitignored,
// --out overridable), never directly in tier1/.
// =============================================================================

// shortUUIDMinLen is both the length of the short-ID suffix `rnix agtest
// import` accepts (matching the dashboard's convention of showing "~xxxxxx")
// and the floor below which suffix/prefix matching is refused outright
// (Story 68.3 关键防错 #11 — a shorter fragment has too high a collision
// probability across a multi-project data directory).
const shortUUIDMinLen = 6

var flagAgtestImportOut string

var agtestImportCmd = &cobra.Command{
	Use:   "import <uuid>",
	Short: "Generate a Tier1 case skeleton from a persisted process run",
	Long: `Read a process's persisted steps.jsonl / proc-info.json / events.jsonl
directly from disk (no daemon required) and generate a case-file + replay
response-script skeleton for manual review.

The generated files are intentionally NOT wired into the Tier1 suite: the
case has no live "assert:" block (only commented-out suggestions), so
agtest.ValidateTier1 rejects it until a human fills in real assertions.
Review the output, then move both files into tests/agtest/tier1/ under the
next NN-slug ordinal (see tests/agtest/README.md and docs/eval-loop.md).`,
	Example: `  rnix agtest import a1b2c3d4-e5f6-4789-a012-3456789abcde   Full uuid
  rnix agtest import abcdef                                  Last-6 short id (dashboard convention)
  rnix agtest import a1b2c3                                  Prefix match
  rnix agtest import a1b2c3d4 --out /tmp/imported            Override output dir`,
	Args: cobra.ExactArgs(1),
	RunE: runAgtestImport,
}

func init() {
	agtestImportCmd.Flags().StringVar(&flagAgtestImportOut, "out", "tests/agtest/imported", "Output directory for the generated case + response script")
	agtestCmd.AddCommand(agtestImportCmd)
}

func runAgtestImport(cmd *cobra.Command, args []string) error {
	w := cmd.OutOrStdout()
	uuidArg := args[0]

	match, err := resolveImportUUID(uuidArg)
	if err != nil {
		return agtestError(w, err.Error())
	}

	// steps.jsonl is the only hard dependency — with no response sequence the
	// skeleton has nothing to replay, so a missing/empty file is fail-loud
	// (Story 68.3 关键防错 #6).
	stepsPath := filepath.Join(match.dir, "steps.jsonl")
	steps, _, err := kernel.ReadAllSteps(stepsPath, 0)
	if err != nil {
		return agtestError(w, fmt.Sprintf("read steps for %s: %v", match.uuid, err))
	}
	if len(steps) == 0 {
		return agtestError(w, fmt.Sprintf("no steps recorded for %s (%s is empty or unreadable)", match.uuid, stepsPath))
	}

	info, procWarn := readImportProcInfo(filepath.Join(match.dir, "proc-info.json"))

	// events.jsonl is raw material for a suggested assertion, not a hard
	// dependency — a truly absent file degrades silently to an empty suggestion
	// list, but a present-but-unreadable file (corrupt / oversized line) surfaces
	// a warning so real corruption isn't mislabeled as "missing".
	var eventTypes []string
	var eventWarn string
	if events, evErr := kernel.ReadAllEvents(filepath.Join(match.dir, "events.jsonl")); evErr == nil {
		eventTypes = dedupSyscallTypes(events)
	} else if !os.IsNotExist(evErr) {
		eventWarn = fmt.Sprintf("events.jsonl was present but unreadable (%v) — syscall suggestions degraded to empty, inspect the file manually", evErr)
	}

	responses, warnings := buildImportResponses(steps, info.Result)
	if procWarn != "" {
		warnings = append(warnings, procWarn)
	}
	if eventWarn != "" {
		warnings = append(warnings, eventWarn)
	}

	slug := "import-" + shortUUID(match.uuid)
	outDir := strings.TrimSpace(flagAgtestImportOut)
	if outDir == "" {
		// An empty --out would filepath.Join into a cwd-relative path, landing
		// the skeleton outside the gitignored tests/agtest/imported/ guard
		// (Review 2026-07-12).
		return agtestError(w, "--out must not be empty (the default is tests/agtest/imported)")
	}
	scriptRelPath := filepath.Join("scripts", slug+".responses.yaml")
	casePath := filepath.Join(outDir, slug+".yaml")
	scriptPath := filepath.Join(outDir, scriptRelPath)

	// Same-name conflicts are refused outright rather than overwritten — a
	// previously generated skeleton may already have been hand-edited
	// (Story 68.3 关键防错 #4).
	if _, statErr := os.Stat(casePath); statErr == nil {
		return agtestError(w, fmt.Sprintf("refusing to overwrite existing file %s (it may already be hand-edited)", casePath))
	}
	if _, statErr := os.Stat(scriptPath); statErr == nil {
		return agtestError(w, fmt.Sprintf("refusing to overwrite existing file %s (it may already be hand-edited)", scriptPath))
	}

	scriptYAML, err := renderImportScript(responses, warnings)
	if err != nil {
		return agtestError(w, fmt.Sprintf("render response script: %v", err))
	}
	caseYAML, err := renderImportCase(slug, info.Intent, scriptRelPath, eventTypes, info.Result)
	if err != nil {
		return agtestError(w, fmt.Sprintf("render case file: %v", err))
	}

	if err := os.MkdirAll(filepath.Join(outDir, "scripts"), 0o755); err != nil {
		return agtestError(w, fmt.Sprintf("create output directory %s: %v", filepath.Join(outDir, "scripts"), err))
	}
	if err := os.WriteFile(scriptPath, scriptYAML, 0o644); err != nil {
		return agtestError(w, fmt.Sprintf("write %s: %v", scriptPath, err))
	}
	if err := os.WriteFile(casePath, caseYAML, 0o644); err != nil {
		// Roll back the just-written script so a re-run isn't blocked by a
		// misleading refuse-overwrite pointing at an orphan script whose case
		// never landed (Review 2026-07-12).
		_ = os.Remove(scriptPath)
		return agtestError(w, fmt.Sprintf("write %s: %v", casePath, err))
	}

	if flagJSON {
		resp := JSONResponse{OK: true, Data: map[string]any{
			"uuid":        match.uuid,
			"case_file":   casePath,
			"script_file": scriptPath,
			"steps":       len(steps),
			"responses":   len(responses),
			"warnings":    warnings,
		}}
		data, _ := json.Marshal(resp)
		fmt.Fprintln(w, string(data))
		return nil
	}

	fmt.Fprintf(w, "[agtest import] uuid: %s\n", match.uuid)
	fmt.Fprintf(w, "[agtest import] generated %s\n", casePath)
	fmt.Fprintf(w, "[agtest import] generated %s\n", scriptPath)
	fmt.Fprintf(w, "[agtest import] %d step(s) -> %d scripted response(s)\n", len(steps), len(responses))
	if len(warnings) > 0 {
		fmt.Fprintf(w, "[agtest import] %d warning(s) — see comments in the generated files\n", len(warnings))
	}
	fmt.Fprintln(w, "[agtest import] review required: fill in `assert:`, then move both files into tests/agtest/tier1/ (see docs/eval-loop.md)")
	return nil
}

// --- UUID resolution ---------------------------------------------------

// importUUIDMatch is one candidate process directory discovered under a
// steps/ directory (Story 68.3 裁决 4 — allStepsDirs() is the same scan
// cmd/rnix/replay.go already uses for daemon-independent history access).
type importUUIDMatch struct {
	uuid string
	dir  string // <stepsDir>/<uuid>
}

// resolveImportUUID applies the three-level lookup AC4 calls for: exact
// match, then (only once the query itself is long enough) suffix match, then
// prefix match. Each level stops as soon as it finds exactly one candidate;
// multiple candidates at any level is reported as ambiguous instead of
// silently taking the first one.
func resolveImportUUID(arg string) (importUUIDMatch, error) {
	var all []importUUIDMatch
	for _, stepsDir := range allStepsDirs() {
		entries, err := os.ReadDir(stepsDir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() && isUUIDDir(e.Name()) {
				all = append(all, importUUIDMatch{uuid: e.Name(), dir: filepath.Join(stepsDir, e.Name())})
			}
		}
	}

	if exact := filterUUIDMatches(all, func(u string) bool { return u == arg }); len(exact) > 0 {
		if len(exact) == 1 {
			return exact[0], nil
		}
		return importUUIDMatch{}, ambiguousImportUUIDError(arg, exact)
	}

	if len(arg) < shortUUIDMinLen {
		return importUUIDMatch{}, fmt.Errorf(
			"uuid %q is shorter than the %d-character minimum for suffix/prefix matching — pass a longer id or the full uuid (try `rnix ps -a --uuid` to list known UUIDs)",
			arg, shortUUIDMinLen)
	}

	if suffix := filterUUIDMatches(all, func(u string) bool { return strings.HasSuffix(u, arg) }); len(suffix) > 0 {
		if len(suffix) == 1 {
			return suffix[0], nil
		}
		return importUUIDMatch{}, ambiguousImportUUIDError(arg, suffix)
	}

	if prefix := filterUUIDMatches(all, func(u string) bool { return strings.HasPrefix(u, arg) }); len(prefix) > 0 {
		if len(prefix) == 1 {
			return prefix[0], nil
		}
		return importUUIDMatch{}, ambiguousImportUUIDError(arg, prefix)
	}

	return importUUIDMatch{}, fmt.Errorf("no process found matching uuid %q — try `rnix ps -a --uuid` to list known UUIDs", arg)
}

func filterUUIDMatches(all []importUUIDMatch, pred func(string) bool) []importUUIDMatch {
	var out []importUUIDMatch
	for _, c := range all {
		if pred(c.uuid) {
			out = append(out, c)
		}
	}
	return out
}

func ambiguousImportUUIDError(arg string, matches []importUUIDMatch) error {
	uuids := make([]string, len(matches))
	for i, m := range matches {
		uuids[i] = m.uuid
	}
	sort.Strings(uuids)
	return fmt.Errorf("uuid %q is ambiguous, matches %d processes: %s", arg, len(uuids), strings.Join(uuids, ", "))
}

// shortUUID returns the last shortUUIDMinLen characters of a full uuid — the
// same "~xxxxxx" short form the dashboard displays — used to build the
// generated slug (import-<short>).
func shortUUID(u string) string {
	if len(u) <= shortUUIDMinLen {
		return u
	}
	return u[len(u)-shortUUIDMinLen:]
}

// --- Disk reads ----------------------------------------------------------

// importProcInfo is a local minimal decode of proc-info.json — only the six
// fields the generator needs (uuid/intent/provider/model/result/exit_code).
// Deliberately not kernel's exported API: process_history.go's procInfoDisk
// is unexported and the read need here is narrow and read-only, so a local
// struct (mirroring its json tags) is the same "仿 readMetaPID" precedent
// cmd/rnix/replay.go already uses for process-meta.json (Story 68.3 裁决 4).
type importProcInfo struct {
	UUID     string `json:"uuid"`
	Intent   string `json:"intent"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Result   string `json:"result"`
	ExitCode int    `json:"exit_code"`
}

// readImportProcInfo degrades to a zero-value struct on read/parse failure —
// proc-info.json is optional context (intent backfill / result candidate),
// never a hard dependency the way steps.jsonl is. A truly absent file degrades
// silently (empty warning); a present-but-corrupt file returns a warning so the
// generated skeleton doesn't mislabel real corruption as "no intent recorded".
func readImportProcInfo(path string) (importProcInfo, string) {
	var info importProcInfo
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return info, ""
		}
		return info, fmt.Sprintf("proc-info.json was present but unreadable (%v) — intent/result backfill degraded to empty", err)
	}
	if err := json.Unmarshal(data, &info); err != nil {
		return info, fmt.Sprintf("proc-info.json is corrupt (%v) — intent/result backfill degraded to empty", err)
	}
	return info, ""
}

// dedupSyscallTypes extracts the distinct SyscallEventDisk.Syscall type names
// (e.g. "ReasonStep", "Spawn", "Open"), sorted for deterministic output. This
// is raw material for a suggested `syscalls.includes` — EvalSyscalls is a
// name-set match (agtest/eval.go), so only the type name is useful.
func dedupSyscallTypes(events []kernel.SyscallEventDisk) []string {
	seen := make(map[string]bool)
	var out []string
	for _, e := range events {
		if e.Syscall == "" || seen[e.Syscall] {
			continue
		}
		seen[e.Syscall] = true
		out = append(out, e.Syscall)
	}
	sort.Strings(out)
	return out
}

// --- StepRecord -> replay response mapping --------------------------------

// metaActionToolNames mirrors kernel/toolgen.go's metaToolDefs mapping from
// ActionType to its canonical (Decision 45 PascalCase) tool name. Duplicated
// locally rather than exported from kernel: the need here is narrow and
// read-only — the same "local minimal struct" reasoning Story 68.3 裁决 4
// applies to procInfoDisk. If kernel ever renames one of these tools, the
// generated skeleton's tool_calls[].name would drift silently, which is
// exactly why every meta-action reconstruction also emits a review warning
// below instead of being treated as a precise replay.
var metaActionToolNames = map[string]string{
	string(kernel.ActionComplete):      "Complete",
	string(kernel.ActionSpawn):         "Agent",
	string(kernel.ActionReplan):        "Replan",
	string(kernel.ActionSpecialize):    "Skill",
	string(kernel.ActionDiscoverSkill): "ToolSearch",
	string(kernel.ActionPlan):          "EnterPlanMode",
}

type importedToolCallYAML struct {
	Name  string `yaml:"name"`
	Input any    `yaml:"input,omitempty"`
}

type importedResponseYAML struct {
	Content   string                 `yaml:"content,omitempty"`
	ToolCalls []importedToolCallYAML `yaml:"tool_calls,omitempty"`
}

// buildImportResponses maps recorded steps to 68-1 script responses per the
// story's "import 生成映射" table, in step order. Reconstruction is
// best-effort throughout — every meta-action or legacy-shape reconstruction
// appends a human-readable warning rather than failing generation, since the
// whole point of the skeleton is a starting point for manual review, not a
// guaranteed-faithful replay (Story 68.3 关键防错 #5 / ATDD 指引).
func buildImportResponses(steps []types.StepRecord, procResult string) (responses []importedResponseYAML, warnings []string) {
	for _, rec := range steps {
		switch {
		case len(rec.ToolCalls) > 0:
			calls := make([]importedToolCallYAML, 0, len(rec.ToolCalls))
			for _, tc := range rec.ToolCalls {
				input, warn := decodeToolInput(tc.Input)
				if warn != "" {
					warnings = append(warnings, fmt.Sprintf("step %d tool_call %q: %s", rec.Step, tc.Name, warn))
				}
				calls = append(calls, importedToolCallYAML{Name: tc.Name, Input: input})
			}
			responses = append(responses, importedResponseYAML{ToolCalls: calls})

		case rec.Action == string(kernel.ActionText):
			responses = append(responses, importedResponseYAML{Content: textOrSummary(rec)})

		case rec.Action == string(kernel.ActionComplete):
			result := procResult
			if result == "" {
				result = rec.ToolResult
			}
			if result == "" {
				result = rec.Summary
			}
			responses = append(responses, importedResponseYAML{
				ToolCalls: []importedToolCallYAML{{
					Name:  metaActionToolNames[string(kernel.ActionComplete)],
					Input: map[string]any{"result": result},
				}},
			})

		case metaActionToolNames[rec.Action] != "":
			name := metaActionToolNames[rec.Action]
			input, warn := decodeToolInput(rec.ToolInput)
			responses = append(responses, importedResponseYAML{
				ToolCalls: []importedToolCallYAML{{Name: name, Input: input}},
			})
			msg := fmt.Sprintf("step %d: reconstructed meta action %q as tool_call %q — verify manually against the original steps.jsonl", rec.Step, rec.Action, name)
			if warn != "" {
				msg += "; " + warn
			}
			warnings = append(warnings, msg)

		case rec.ToolPath != "":
			// CLI-driver aggregate / legacy shape: no ToolCalls array, but the
			// backfilled single-call fields are present (StepRecord doc comment,
			// internal/types/step_record.go). Best-effort reconstruction only.
			name := rec.ToolPath
			if idx := strings.LastIndex(name, "/"); idx >= 0 && idx+1 < len(name) {
				name = name[idx+1:]
			}
			input, warn := decodeToolInput(rec.ToolInput)
			responses = append(responses, importedResponseYAML{
				ToolCalls: []importedToolCallYAML{{Name: name, Input: input}},
			})
			msg := fmt.Sprintf("step %d: reconstructed from legacy tool_path %q (action=%q) using name %q — this is the device path's last segment, NOT a canonical (Decision 45 PascalCase) tool name; it almost certainly must be hand-edited to the real tool name (e.g. shell -> Bash) before this response will replay", rec.Step, rec.ToolPath, rec.Action, name)
			if warn != "" {
				msg += "; " + warn
			}
			warnings = append(warnings, msg)

		case rec.RawResponse != "" || rec.Summary != "":
			responses = append(responses, importedResponseYAML{Content: textOrSummary(rec)})
			warnings = append(warnings, fmt.Sprintf("step %d: action %q had no tool_calls/tool_path — reconstructed as a content-only response, verify this was really a terminal text turn", rec.Step, rec.Action))

		default:
			warnings = append(warnings, fmt.Sprintf("step %d: action %q had no reconstructable content (no tool_calls, no tool_path, no text) — SKIPPED, inspect steps.jsonl manually if this step matters", rec.Step, rec.Action))
		}
	}

	if len(responses) > 0 {
		last := responses[len(responses)-1]
		if len(last.ToolCalls) > 0 && !lastCallIsComplete(last) {
			warnings = append(warnings, "the last scripted response is a tool_call that is not Complete — a real replay run would either exhaust the script (fail-loud) or hit max_steps; add a trailing Complete response or confirm this is intentional (tests/agtest/README.md)")
		}
	} else if len(steps) > 0 {
		warnings = append(warnings, fmt.Sprintf("none of the %d recorded step(s) yielded a reconstructable response — the generated script's `responses:` list is EMPTY and the first replay Read will fail-loud; inspect steps.jsonl manually before using this skeleton", len(steps)))
	}

	return responses, warnings
}

func lastCallIsComplete(r importedResponseYAML) bool {
	completeName := metaActionToolNames[string(kernel.ActionComplete)]
	for _, tc := range r.ToolCalls {
		if tc.Name == completeName {
			return true
		}
	}
	return false
}

func textOrSummary(rec types.StepRecord) string {
	if rec.RawResponse != "" {
		return rec.RawResponse
	}
	return rec.Summary
}

// decodeToolInput best-effort JSON-decodes a recorded input string into any
// JSON value (object, array, or scalar) for YAML re-serialization. Empty input
// decodes to (nil, ""). A non-empty string that isn't valid JSON is passed
// through unchanged as a raw string value, together with a warning describing
// the failure — generation must never abort over one malformed step (Story
// 68.3 「import 生成映射」表).
func decodeToolInput(raw string) (value any, warning string) {
	if raw == "" {
		return nil, ""
	}
	// Decode into any (not map) so a legitimate JSON array/scalar input keeps
	// its structure instead of being misreported as "not valid JSON" and
	// flattened into a raw string (Review 2026-07-12).
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err == nil {
		return v, ""
	}
	return raw, fmt.Sprintf("input was not valid JSON (%s), inlined as a raw string", truncateForJudge(raw, 80))
}

// --- Rendering -------------------------------------------------------------

type importedScriptYAML struct {
	Version   string                 `yaml:"version"`
	Responses []importedResponseYAML `yaml:"responses"`
}

// renderImportScript marshals the response list via goccy/go-yaml (never
// hand-formatted string concatenation for the live YAML data — Story 68.3
// 关键防错 #12, CJK-safety) and prepends a comment header. No `usage` block
// is ever emitted, structurally: importedResponseYAML has no Usage field, so
// the 68-1 Compact-trigger trap (关键防错 #5) cannot leak in by omission.
func renderImportScript(responses []importedResponseYAML, warnings []string) ([]byte, error) {
	body, err := yaml.Marshal(importedScriptYAML{Version: "1", Responses: responses})
	if err != nil {
		return nil, err
	}

	var b strings.Builder
	b.WriteString("# Generated by `rnix agtest import` — REVIEW BEFORE USE.\n")
	b.WriteString("# Story 68.1 response-script format (drivers/llm/replay.go). No `usage` block\n")
	b.WriteString("# is ever generated here — large token counts trip the context Compactor and\n")
	b.WriteString("# destroy Tier1 determinism; add one by hand only if you know it stays small.\n")
	b.WriteString("#\n")
	b.WriteString("# DETERMINISM: replay only scripts the LLM's *output* — the tool_calls below\n")
	b.WriteString("# are RE-EXECUTED for real on replay. Before moving this into tier1/, confirm\n")
	b.WriteString("# every tool here is deterministic: no date/hostname/network/random, no reads\n")
	b.WriteString("# of machine-specific absolute paths — otherwise the Tier1 case will flake in CI.\n")
	writeWarningComments(&b, warnings)
	b.WriteString("\n")
	b.Write(body)
	return []byte(b.String()), nil
}

type importedAgentYAML struct {
	Provider string `yaml:"provider,omitempty"`
	Model    string `yaml:"model,omitempty"`
}

type importedCaseYAML struct {
	Version string            `yaml:"version"`
	Name    string            `yaml:"name,omitempty"`
	Intent  string            `yaml:"intent"`
	Agent   importedAgentYAML `yaml:"agent"`
	Timeout int               `yaml:"timeout,omitempty"`
}

// renderImportCase marshals the case skeleton (again via goccy/go-yaml —
// Intent may contain CJK text backfilled from proc-info.json) and prepends a
// comment header with suggested (commented-out) assertions. No `assert:` key
// is ever set on importedCaseYAML, so the generated case is structurally
// guaranteed to fail agtest.ValidateTier1 rule 2 if it were ever dropped
// straight into tests/agtest/tier1/ without review (Story 68.3 裁决 5).
func renderImportCase(slug, intent, scriptRelPath string, eventTypes []string, result string) ([]byte, error) {
	if intent == "" {
		intent = "TODO: proc-info.json had no intent recorded — fill this in manually"
	}
	c := importedCaseYAML{
		Version: "1.0",
		Name:    slug,
		Intent:  intent,
		Agent:   importedAgentYAML{Provider: "replay", Model: filepath.ToSlash(scriptRelPath)},
		Timeout: 30000,
	}
	body, err := yaml.Marshal(c)
	if err != nil {
		return nil, err
	}

	var b strings.Builder
	b.WriteString("# Generated by `rnix agtest import` — REVIEW BEFORE USE.\n")
	b.WriteString("# This skeleton has NO live `assert:` block on purpose: agtest.ValidateTier1\n")
	b.WriteString("# rejects an assertion-less case (rule 2), so it cannot silently land in\n")
	b.WriteString("# tests/agtest/tier1/ without a human filling one in first (Story 68.3 裁决 5).\n")
	b.WriteString("#\n")
	b.WriteString("# Suggested assertions — uncomment, edit, and verify against a real run:\n")
	b.WriteString("#\n")
	b.WriteString("# assert:\n")
	if candidate, ok := outputCandidate(result); ok {
		fmt.Fprintf(&b, "#   output:\n#     contains: [%s]\n", yamlQuoteComment(candidate))
	} else {
		b.WriteString("#   output:\n#     contains: [\"TODO\"]  # proc-info.result was empty or looked machine-dependent\n")
	}
	if len(eventTypes) > 0 {
		fmt.Fprintf(&b, "#   syscalls:\n#     includes: [%s]  # observed syscall types, deduped — trim to what actually matters\n", strings.Join(quoteAllForComment(eventTypes), ", "))
	} else {
		b.WriteString("#   syscalls:\n#     includes: [\"TODO\"]  # events.jsonl was missing/empty for this process\n")
	}
	b.WriteString("#\n")
	fmt.Fprintf(&b, "# After review, move this file and %s into tests/agtest/tier1/,\n", filepath.ToSlash(scriptRelPath))
	b.WriteString("# renaming both to the next NN-slug ordinal (see tests/agtest/README.md).\n")
	b.WriteString("\n")
	b.Write(body)
	return []byte(b.String()), nil
}

func writeWarningComments(b *strings.Builder, warnings []string) {
	if len(warnings) == 0 {
		return
	}
	b.WriteString("#\n# Warnings — review each one before trusting this script:\n")
	for _, w := range warnings {
		for line := range strings.SplitSeq(w, "\n") {
			b.WriteString("#   - " + line + "\n")
		}
	}
}

// outputCandidate derives a suggested `output.contains` value from the
// process's recorded result: its first line, truncated, and filtered against
// the same absolute-path heuristic as agtest.ValidateTier1 rule 4 (a
// suggestion that would itself be rejected by the guard it's meant to help
// pass is worse than no suggestion).
func outputCandidate(result string) (string, bool) {
	if result == "" {
		return "", false
	}
	line := strings.TrimSpace(strings.SplitN(result, "\n", 2)[0])
	// Truncate by rune (CJK-safe) but WITHOUT an ellipsis: this value is a
	// copy-paste-ready `contains:` suggestion, and a trailing "..." would make
	// the assertion string mismatch the real output if used verbatim
	// (Review 2026-07-12).
	if r := []rune(line); len(r) > 80 {
		line = string(r[:80])
	}
	if line == "" || looksMachineDependent(line) {
		return "", false
	}
	return line, true
}

// looksMachineDependent mirrors agtest.ValidateTier1's unexported rule-4
// heuristic (agtest/tier1.go checkNonDeterministic) — duplicated rather than
// exported since it's a narrow, stable one-liner and pulling it out of
// agtest/ would be 68-2's territory to change, not 68-3's.
func looksMachineDependent(s string) bool {
	return strings.HasPrefix(s, "/") || strings.Contains(s, "/home/") || strings.Contains(s, "/tmp/")
}

func yamlQuoteComment(s string) string {
	return fmt.Sprintf("%q", s)
}

func quoteAllForComment(items []string) []string {
	out := make([]string, len(items))
	for i, s := range items {
		out[i] = yamlQuoteComment(s)
	}
	return out
}
