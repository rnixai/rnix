package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
	"github.com/spf13/cobra"
)

// gcCmd implements `rnix gc` (Story 42.5).
//
// Removes terminated process data under .rnix/data/steps/<uuid>/ that exceeds
// the active retention policy (config: gc.retention_days / gc.max_entries).
// Always exempts Running/Suspended processes — gc cannot kill live work.
var gcCmd = &cobra.Command{
	Use:   "gc",
	Short: "Garbage-collect terminated process data under .rnix/data/steps/",
	Long: `Remove .rnix/data/steps/<uuid>/ directories for Dead/Zombie processes that
exceed the retention policy declared in ~/.config/rnix/config.yaml:

    gc:
      retention_days: 30   # delete entries older than 30 days
      max_entries: 500     # keep at most 500 historical entries
      interval_seconds: 3600

Running and Suspended processes are NEVER eligible. Use --dry-run to preview
before deletion. Bulk operations (>100 candidates) prompt for confirmation
unless --force or --json is provided.`,
	Example: `  rnix gc --dry-run        # Preview candidates without deleting
  rnix gc                   # Interactive cleanup
  rnix gc --force           # Skip confirmation
  rnix gc --json            # Machine-readable output (implies --force)`,
	RunE: runGc,
}

var (
	flagGcDryRun bool
	flagGcForce  bool
	flagGcJSON   bool
)

// gcConfirmThreshold is the upper bound at which `rnix gc` prompts the user
// for confirmation. Anything ≤ this auto-proceeds; > triggers the prompt.
// AC#9 specifies "超过 100", so the boundary value 100 itself is below threshold.
const gcConfirmThreshold = 100

func init() {
	gcCmd.Flags().BoolVar(&flagGcDryRun, "dry-run", false, "Preview candidates without deleting")
	gcCmd.Flags().BoolVar(&flagGcForce, "force", false, "Skip confirmation for bulk deletes (>100 entries)")
	gcCmd.Flags().BoolVar(&flagGcJSON, "json", false, "Emit JSON output (implies --force)")
	rootCmd.AddCommand(gcCmd)
}

// runGc is the entry point of `rnix gc`. Mirrors the dial+orchestrate pattern
// from cmd/rnix/resume.go and cmd/rnix/compose_resume.go: a thin wrapper that
// builds the IPC client, delegates to runGcWithClient for the testable seam,
// and translates errors into exitCode.
func runGc(cmd *cobra.Command, args []string) error {
	_ = cmd
	_ = args
	client, err := ipc.Dial(ipc.SocketPath())
	if err != nil {
		// Daemon not running → emit structured error and bail.
		if flagGcJSON {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
				"ok":    false,
				"error": fmt.Sprintf("daemon unavailable: %v", err),
			})
		} else {
			fmt.Fprintf(os.Stderr, "rnix gc: daemon unavailable: %v\n", err)
		}
		exitCode = 1
		return nil
	}
	defer client.Close()

	if err := runGcWithClient(client, os.Stdout, os.Stdin, flagGcDryRun, flagGcForce, flagGcJSON); err != nil {
		if flagGcJSON {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
				"ok":    false,
				"error": err.Error(),
			})
		} else {
			fmt.Fprintf(os.Stderr, "rnix gc: %v\n", err)
		}
		exitCode = 1
		return nil
	}
	return nil
}

// gcClient is the minimal IPC surface runGcWithClient needs. *ipc.Client
// satisfies it directly; tests pass mock implementations to exercise behavior
// (AC#9 CLI-008 JSON_Implies_Force).
type gcClient interface {
	Gc() (*ipc.GcResponse, error)
	GcDryRun() (*ipc.GcDryRunResponse, error)
}

// runGcWithClient is the testable seam — orchestrates dry-run preview,
// optional confirmation, and the actual gc call. Writes output to `out`,
// reads confirmation input from `in`.
//
// Behavior contract:
//   - dryRun=true → emit candidates without deleting (AC#4).
//   - force=true OR jsonOut=true → skip confirmation prompt (AC#9 last clause).
//   - len(candidates) > gcConfirmThreshold AND prompt enabled → require [y].
//   - any IPC error short-circuits with err return.
func runGcWithClient(client gcClient, out io.Writer, in io.Reader, dryRun, force, jsonOut bool) error {
	if client == nil {
		return errors.New("nil ipc client")
	}

	if dryRun {
		resp, err := client.GcDryRun()
		if err != nil {
			return fmt.Errorf("gc_dry_run: %w", err)
		}
		if jsonOut {
			renderGcDryRunJSON(out, resp.Candidates)
		} else {
			renderGcDryRunTable(out, resp.Candidates)
		}
		return nil
	}

	// Real cleanup path: preview first so we can prompt the user about bulk
	// deletes. AC#9: jsonOut implies force.
	preview, err := client.GcDryRun()
	if err != nil {
		return fmt.Errorf("gc_dry_run (preview): %w", err)
	}

	needConfirm := len(preview.Candidates) > gcConfirmThreshold && !force && !jsonOut
	if needConfirm {
		if !gcConfirm(out, in, preview.Candidates) {
			// User declined or EOF → don't delete, exit code 0 (AC#9).
			if jsonOut {
				_ = json.NewEncoder(out).Encode(map[string]any{
					"ok":             true,
					"removed_count":  0,
					"freed_bytes":    0,
					"removed_uuids":  []string{},
					"declined":       true,
				})
			} else {
				fmt.Fprintln(out, "Cancelled.")
			}
			return nil
		}
	}

	resp, err := client.Gc()
	if err != nil {
		return fmt.Errorf("gc: %w", err)
	}
	if jsonOut {
		renderGcStatsJSON(out, resp)
	} else {
		renderGcStats(out, resp)
	}
	return nil
}

// renderGcDryRunTable formats the candidate list as a UTF-friendly table for
// the terminal (AC#4 output schema). RED PHASE: emits the header so tests can
// detect the column ordering, but candidate rows are stubbed.
func renderGcDryRunTable(w io.Writer, candidates []ipc.GcCandidateWire) {
	if w == nil {
		return
	}
	fmt.Fprintln(w, "UUID                                  | DEAD_AT              | SIZE     | REASON")
	for _, c := range candidates {
		fmt.Fprintf(w, "%-36s | %-20s | %-8s | %s\n",
			gcTruncateUUID(c.UUID), c.DeadAt, formatBytesIEC(c.SizeBytes), c.Reason)
	}
	if len(candidates) > 0 {
		var total int64
		for _, c := range candidates {
			total += c.SizeBytes
		}
		fmt.Fprintf(w, "---\n%d candidates, %s will be freed (dry-run, no changes)\n",
			len(candidates), formatBytesIEC(total))
	} else {
		fmt.Fprintln(w, "No candidates match the active retention policy.")
	}
}

// renderGcDryRunJSON emits the AC#4 JSON schema:
//
//	{"ok": true, "dry_run": true, "candidates": [{"uuid":"...","dead_at":"...",
//	 "size_bytes":N,"reason":"age"}, ...]}
func renderGcDryRunJSON(w io.Writer, candidates []ipc.GcCandidateWire) {
	if w == nil {
		return
	}
	if candidates == nil {
		candidates = []ipc.GcCandidateWire{}
	}
	payload := map[string]any{
		"ok":         true,
		"dry_run":    true,
		"candidates": candidates,
	}
	data, _ := json.Marshal(payload)
	fmt.Fprintln(w, string(data))
}

// renderGcStats prints the human-friendly summary after `rnix gc` actually
// performs deletion (AC#5):  "Removed N processes, freed M.M MB"
func renderGcStats(w io.Writer, resp *ipc.GcResponse) {
	if w == nil || resp == nil {
		return
	}
	prefix := ui.KernelStyle.Render("[gc]")
	fmt.Fprintf(w, "%s Removed %d processes, freed %s\n",
		prefix, resp.RemovedCount, formatBytesIEC(resp.FreedBytes))
}

// renderGcStatsJSON emits the AC#5 JSON schema:
//
//	{"ok": true, "removed_count": N, "freed_bytes": M, "removed_uuids": [...]}
func renderGcStatsJSON(w io.Writer, resp *ipc.GcResponse) {
	if w == nil || resp == nil {
		return
	}
	uuids := resp.RemovedUUIDs
	if uuids == nil {
		uuids = []string{}
	}
	payload := map[string]any{
		"ok":            true,
		"removed_count": resp.RemovedCount,
		"freed_bytes":   resp.FreedBytes,
		"removed_uuids": uuids,
	}
	data, _ := json.Marshal(payload)
	fmt.Fprintln(w, string(data))
}

// formatBytesIEC formats byte counts as IEC binary units (KiB/MiB/GiB) with
// 2-decimal precision. Aligns with Dev Notes AC#5 ("freed M.M MB" using IEC
// 单位). For consistency the suffix is the IEC form (KiB, MiB, GiB, TiB).
//
// Edge cases:
//   - 0 → "0 B"
//   - n < 1024 → "%d B"
//   - n ≥ 1024 PiB → falls back to PiB (no exabyte for now).
func formatBytesIEC(n int64) string {
	if n < 0 {
		return fmt.Sprintf("%d B", n) // negative shouldn't happen; report raw
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit && exp < 4; x /= unit {
		div *= unit
		exp++
	}
	suffix := []string{"KiB", "MiB", "GiB", "TiB", "PiB"}[exp]
	return fmt.Sprintf("%.2f %s", float64(n)/float64(div), suffix)
}

// gcTruncateUUID shortens a long UUID for table display. UUIDs are 36 chars
// canonical; we keep the full string in the column but mark anything weirdly
// long. Placeholder so render tests can assert that the formatter is invoked
// uniformly. (Named with `gc` prefix to avoid clashing with the dashboard
// helper of the same name.)
func gcTruncateUUID(uuid string) string {
	if len(uuid) <= 36 {
		return uuid
	}
	return uuid[:33] + "..."
}

// gcConfirm prompts the user with a candidate summary and reads a [y/N]
// response from in. Returns true iff the user typed 'y' or 'Y'. EOF, blank,
// and any other input return false (AC#9 default-deny).
//
// Callers should bypass this entirely when --force or --json is set.
func gcConfirm(out io.Writer, in io.Reader, candidates []ipc.GcCandidateWire) bool {
	if out == nil || in == nil {
		return false
	}
	if len(candidates) == 0 {
		return false
	}
	var total int64
	for _, c := range candidates {
		total += c.SizeBytes
	}
	fmt.Fprintf(out, "About to remove %d processes, freeing %s.\n", len(candidates), formatBytesIEC(total))
	fmt.Fprint(out, "Proceed? [y/N] ")

	reader := bufio.NewReader(in)
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return false
	}
	answer := strings.TrimSpace(strings.ToLower(line))
	return answer == "y" || answer == "yes"
}
