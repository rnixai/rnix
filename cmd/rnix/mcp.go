package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/rnixai/rnix/drivers/mcp"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
	"github.com/spf13/cobra"
)

// Story 48.3 — `rnix mcp` subcommand tree.
//
// Design notes:
//   - `mcp list` is a pure read (analog to `rnix ps`): daemon-down → friendly
//     empty output, exitCode stays 0, NO EnsureDaemon side-effect (易错点 2).
//   - `mcp test` is a diagnostic that requires daemon-side transport
//     plumbing — daemon-down is a hard fail (exitCode=1) so users see the
//     real reason for failure instead of a generic IPC timeout.
//   - The CLI never imports drivers/mcp at this layer. All transport
//     interactions go through the daemon via client.MCPTest, satisfying
//     Story §易错点 14.

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Manage MCP servers",
	Long:  "Inspect and validate Model Context Protocol (MCP) server mounts on the running daemon.",
}

var mcpListCmd = &cobra.Command{
	Use:   "list",
	Short: "List active MCP server mounts on the daemon",
	Long: `List MCP mounts currently held by the running daemon.

Pure read command: when the daemon is not running, this prints a friendly
empty list and exits 0 (consistent with ` + "`rnix ps`" + `). Tools / Resources
columns stay at "—" until Story 48.5 wires the registry cache.`,
	Args: cobra.NoArgs,
	RunE: runMCPList,
}

var mcpTestCmd = &cobra.Command{
	Use:   "test <name>",
	Short: "Probe a configured MCP server (connect → tools/list → ...)",
	Long: `Run a one-shot probe against a server declared in mcp.yaml. The probe
spins up a fresh transport on the daemon, walks 4 stages (connect /
tools_list / resources_list / prompts_list), and tears it down — leaves no
mount in the registry.

This command REQUIRES the daemon to be running because the transport must
be spawned in the daemon's process tree (not the short-lived CLI's) to
avoid orphan subprocesses. Daemon-down therefore exits 1, unlike ` + "`mcp list`" + `.`,
	Args: cobra.ExactArgs(1),
	RunE: runMCPTest,
}

var mcpLogsCmd = &cobra.Command{
	Use:   "logs <name>",
	Short: "Show captured stderr of a mounted MCP server",
	Long: `Print the recent stderr lines (most recent 256) captured from a mounted
MCP server's child process — the first place to look when an MCP tool starts
failing (e.g. ` + "`npx error: ENOENT`" + `, ` + "`chromium not found`" + `).

Like ` + "`mcp list`" + `, this is a pure read: when the daemon is not running it
prints a friendly hint and exits 0. An unknown / unmounted server name exits 1
with the list of available servers.`,
	Args: cobra.ExactArgs(1),
	RunE: runMCPLogs,
}

func init() {
	mcpCmd.AddCommand(mcpListCmd)
	mcpCmd.AddCommand(mcpTestCmd)
	mcpCmd.AddCommand(mcpLogsCmd)
}

// runMCPLogs implements `rnix mcp logs <name>` (Story 48.5 AC4). Daemon-down is
// graceful (exit 0, parity with `mcp list`); an unknown server is a hard fail
// (exit 1) with the available-server hint surfaced from the daemon.
func runMCPLogs(cmd *cobra.Command, args []string) error {
	mode := resolveOutputMode()
	w := cmd.OutOrStdout()
	name := args[0]

	client, err := ipc.Dial(ipc.SocketPath())
	if err != nil {
		// Daemon down → friendly, exit 0 (parity with `mcp list`).
		switch mode {
		case ui.ModeJSON:
			resp := JSONResponse{OK: true, Data: map[string]any{"lines": []any{}}}
			data, _ := json.Marshal(resp)
			fmt.Fprintln(w, string(data))
		case ui.ModeQuiet:
			// silent
		default:
			fmt.Fprintln(w, "Daemon not running, no MCP logs to show.")
		}
		return nil
	}
	defer client.Close()

	resp, err := client.MCPLogs(name)
	if err != nil {
		exitCode = 1
		if mode == ui.ModeJSON {
			payload := JSONResponse{OK: false, Error: map[string]any{
				"code":    classifyMCPTestErr(err),
				"message": err.Error(),
			}}
			data, _ := json.Marshal(payload)
			fmt.Fprintln(w, string(data))
			return nil
		}
		if mode != ui.ModeQuiet {
			fmt.Fprintln(cmd.ErrOrStderr(), formatMCPLogsErr(name, err))
		}
		return nil
	}

	switch mode {
	case ui.ModeJSON:
		payload := JSONResponse{OK: true, Data: map[string]any{"lines": resp.Lines}}
		data, _ := json.Marshal(payload)
		fmt.Fprintln(w, string(data))
	case ui.ModeQuiet:
		for _, line := range resp.Lines {
			fmt.Fprintln(w, line)
		}
	default:
		renderMCPLogsHuman(w, resp.Lines)
	}
	return nil
}

// renderMCPLogsHuman prints each captured stderr line verbatim, or a friendly
// note when the buffer is empty.
func renderMCPLogsHuman(w io.Writer, lines []string) {
	if len(lines) == 0 {
		fmt.Fprintln(w, "No stderr captured for this MCP server yet.")
		return
	}
	for _, line := range lines {
		fmt.Fprintln(w, line)
	}
}

// formatMCPLogsErr produces a human-friendly NOT_FOUND/INVALID message, reusing
// the `ipc: [` prefix-strip from formatMCPTestErr's pattern.
func formatMCPLogsErr(name string, err error) string {
	msg := err.Error()
	const prefix = "ipc: ["
	if strings.HasPrefix(msg, prefix) {
		if i := strings.Index(msg, "] "); i >= 0 {
			msg = msg[i+2:]
		}
	}
	return fmt.Sprintf("MCP server %q: %s", name, msg)
}

// runMCPList implements `rnix mcp list` (Story 48.3 AC1).
func runMCPList(cmd *cobra.Command, _ []string) error {
	mode := resolveOutputMode()
	w := cmd.OutOrStdout()

	client, err := ipc.Dial(ipc.SocketPath())
	if err != nil {
		// Story §AC1 (daemon down branch) — match `rnix ps` graceful empty.
		renderMCPListEmpty(w, mode, "Daemon not running, no MCP mounts to list.")
		return nil
	}
	defer client.Close()

	resp, err := client.MCPList()
	if err != nil {
		if mode == ui.ModeJSON {
			payload := JSONResponse{OK: false, Error: map[string]any{
				"code":    "ipc_error",
				"message": err.Error(),
			}}
			data, _ := json.Marshal(payload)
			fmt.Fprintln(w, string(data))
		} else if mode != ui.ModeQuiet {
			fmt.Fprintf(cmd.ErrOrStderr(), "✗ mcp list failed: %v\n", err)
		}
		exitCode = 1
		return nil
	}

	switch mode {
	case ui.ModeJSON:
		renderMCPListJSON(w, resp.Mounts)
	case ui.ModeQuiet:
		renderMCPListQuiet(w, resp.Mounts)
	default:
		renderMCPListHuman(w, resp.Mounts)
	}
	return nil
}

// renderMCPListEmpty emits the "no mounts" message in the requested mode.
// daemon-down (err on Dial) and empty registry both route here so callers
// see a consistent shape regardless of cause.
func renderMCPListEmpty(w io.Writer, mode ui.OutputMode, humanHint string) {
	switch mode {
	case ui.ModeJSON:
		resp := JSONResponse{OK: true, Data: map[string]any{"mounts": []any{}}}
		data, _ := json.Marshal(resp)
		fmt.Fprintln(w, string(data))
	case ui.ModeQuiet:
		// silent
	default:
		fmt.Fprintln(w, humanHint)
	}
}

func renderMCPListHuman(w io.Writer, mounts []ipc.MCPMountWire) {
	if len(mounts) == 0 {
		fmt.Fprintln(w, "No active MCP mounts.")
		// Investigation mcp-list-empty: the common confusion is that `check mcp`
		// and `mcp test` look healthy while `mcp list` is empty. They measure
		// different layers — mounts are per-process and created at spawn
		// (kernel/spawn.go `/mnt/mcp/<pid>-<name>`), so a configured-but-unmounted
		// server is expected, not a fault. Surface that distinction additively.
		if names := mcpConfiguredServerNames(); len(names) > 0 {
			fmt.Fprintln(w, formatConfiguredMCPHint(names))
		}
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tTRANSPORT\tSTATUS\tTOOLS\tRESOURCES\tLAST CHECK")
	for _, m := range mounts {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			m.Name,
			m.Transport,
			m.Status,
			placeholderIfZero(m.Tools),
			placeholderIfZero(m.Resources),
			lastCheckLabel(m.LastCheckMs),
		)
	}
	_ = tw.Flush()
}

// mcpConfiguredServerNames returns the server names declared in mcp.yaml,
// sorted, as a best-effort hint source for `mcp list`. Any error (config
// missing / unreadable / unparseable) yields nil: the hint is purely additive
// and must never make `mcp list` fail or emit noise. Reuses resolveMCPConfigPath
// (and its `mcpConfigPathForCheck` test hook) so the lookup matches `check mcp`.
func mcpConfiguredServerNames() []string {
	path := resolveMCPConfigPath()
	if path == "" {
		return nil
	}
	cfg, err := mcp.LoadMCPConfig(path)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(cfg.Servers))
	for name := range cfg.Servers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// formatConfiguredMCPHint builds the additive line shown after "No active MCP
// mounts." when mcp.yaml declares servers that simply aren't mounted yet.
// names must be non-empty. ASCII-safe (no Unicode glyphs) per RNIX_ASCII.
func formatConfiguredMCPHint(names []string) string {
	if len(names) == 1 {
		return fmt.Sprintf(
			"1 server configured in mcp.yaml (%s) but not mounted yet. Mounts appear when an agent using it is running; probe it now with `rnix mcp test %s`.",
			names[0], names[0],
		)
	}
	return fmt.Sprintf(
		"%d servers configured in mcp.yaml (%s) but not mounted yet. Mounts appear when an agent using them is running; probe one with `rnix mcp test <name>`.",
		len(names), strings.Join(names, ", "),
	)
}

func renderMCPListJSON(w io.Writer, mounts []ipc.MCPMountWire) {
	resp := JSONResponse{OK: true, Data: map[string]any{"mounts": mounts}}
	data, _ := json.Marshal(resp)
	fmt.Fprintln(w, string(data))
}

func renderMCPListQuiet(w io.Writer, mounts []ipc.MCPMountWire) {
	for _, m := range mounts {
		fmt.Fprintln(w, m.Name)
	}
}

// placeholderIfZero renders integer placeholders for fields that Story 48.3
// deliberately leaves at zero pending Story 48.5 cache work. Code review
// patch P4 — honor RNIX_ASCII=1 (project convention from CLAUDE.md) so the
// table renders on non-UTF-8 terminals.
func placeholderIfZero(n int) string {
	if n == 0 {
		return mcpPlaceholder()
	}
	return fmt.Sprintf("%d", n)
}

// lastCheckLabel renders the LAST CHECK column (Story 48.5 AC5). ms is the age
// of the last L2 readiness ping in milliseconds (0 / negative = never checked →
// placeholder). Non-zero renders a coarse relative label like "12s ago".
func lastCheckLabel(ms int64) string {
	if ms <= 0 {
		return mcpPlaceholder()
	}
	d := time.Duration(ms) * time.Millisecond
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
}

// mcpPlaceholder returns the placeholder glyph for empty cells, honoring
// RNIX_ASCII=1 (em-dash → hyphen for ASCII-only terminals).
func mcpPlaceholder() string {
	if ui.IsASCIIMode() {
		return "-"
	}
	return "—"
}

// runMCPTest implements `rnix mcp test <name>` (Story 48.3 AC2).
func runMCPTest(cmd *cobra.Command, args []string) error {
	mode := resolveOutputMode()
	w := cmd.OutOrStdout()
	errW := cmd.ErrOrStderr()
	name := args[0]

	client, err := ipc.Dial(ipc.SocketPath())
	if err != nil {
		// Story §AC2 (daemon-down hard fail) — diagnostic commands must not
		// EnsureDaemon (易错点 14) and must surface the cause clearly.
		exitCode = 1
		if mode == ui.ModeJSON {
			payload := JSONResponse{OK: false, Error: map[string]any{
				"code":    "daemon_down",
				"message": "daemon not running",
			}}
			data, _ := json.Marshal(payload)
			fmt.Fprintln(w, string(data))
			return nil
		}
		fmt.Fprintln(errW, "Daemon not running. Start the daemon with `rnix daemon` or any spawn command (e.g. `rnix --intent \"hello\"`).")
		return nil
	}
	defer client.Close()

	resp, err := client.MCPTest(name)
	if err != nil {
		// IPC-level error (NOT_FOUND / INVALID): map to exit 1 + human hint.
		exitCode = 1
		if mode == ui.ModeJSON {
			payload := JSONResponse{OK: false, Error: map[string]any{
				"code":    classifyMCPTestErr(err),
				"message": err.Error(),
			}}
			data, _ := json.Marshal(payload)
			fmt.Fprintln(w, string(data))
			return nil
		}
		fmt.Fprintln(errW, formatMCPTestErr(name, err))
		return nil
	}

	switch mode {
	case ui.ModeJSON:
		renderMCPTestJSON(w, resp)
	case ui.ModeQuiet:
		renderMCPTestQuiet(w, resp)
	default:
		renderMCPTestHuman(w, resp)
	}
	if !resp.OK {
		exitCode = 1
	}
	return nil
}

// mcpProbeTotalStages is the fixed denominator used in human-mode stage
// labels. Code review patch P6 — earlier `len(resp.Stages)` made the connect-
// failure path render `[1/1]` instead of `[1/4]`, hiding from the operator
// that 3 more stages were planned but skipped.
const mcpProbeTotalStages = 4

func renderMCPTestHuman(w io.Writer, resp *ipc.MCPTestResponse) {
	for i, s := range resp.Stages {
		label := mcpStageLabel(s.Name)
		marker := "OK"
		detail := ""
		switch {
		case !s.OK && strings.Contains(strings.ToLower(s.Error), "context deadline exceeded"):
			marker = "TIMEOUT"
			detail = fmt.Sprintf(" (after %dms)", s.DurationMs)
		case !s.OK && mcpStageUnsupported(s.Error):
			// JSON-RPC -32601 "Method not found" means the server does not
			// implement this (optional) capability — not a failure. Render it
			// distinctly so e.g. a tools-only server (Playwright: no resources /
			// prompts) doesn't look broken. The probe already treats these
			// stages as non-fatal (kernel.RunMCPProbe / Story §决策 6).
			marker = "N/A"
			detail = " (not supported)"
		case !s.OK:
			marker = "FAILED"
			if s.Error != "" {
				detail = fmt.Sprintf(" (%s)", s.Error)
			}
		default:
			detail = fmt.Sprintf(" (%dms", s.DurationMs)
			switch s.Name {
			case "tools_list":
				detail += fmt.Sprintf(", tools=%d", resp.Tools)
			case "resources_list":
				detail += fmt.Sprintf(", resources=%d", resp.Resources)
			case "prompts_list":
				detail += fmt.Sprintf(", prompts=%d", resp.Prompts)
			}
			detail += ")"
		}
		fmt.Fprintf(w, "[%d/%d] %s ... %s%s\n", i+1, mcpProbeTotalStages, label, marker, detail)
	}
	// Summary line — emit a final FAILED/OK marker when the probe finishes
	// (covers the rare "all stages skipped" path).
	if !resp.OK {
		return
	}
	if resp.ServerInfo != "" {
		fmt.Fprintf(w, "OK (server: %s)\n", resp.ServerInfo)
	}
}

func renderMCPTestJSON(w io.Writer, resp *ipc.MCPTestResponse) {
	payload := JSONResponse{OK: resp.OK, Data: resp}
	data, _ := json.Marshal(payload)
	fmt.Fprintln(w, string(data))
}

func renderMCPTestQuiet(w io.Writer, resp *ipc.MCPTestResponse) {
	if resp.OK {
		fmt.Fprintln(w, "OK")
		return
	}
	fmt.Fprintln(w, "FAILED")
}

// mcpStageUnsupported reports whether a stage error is JSON-RPC -32601
// ("Method not found") — i.e. the server legitimately does not implement that
// optional capability, rather than failing it. Matches the numeric code or the
// canonical message text so it survives transport-specific error wrapping.
func mcpStageUnsupported(errMsg string) bool {
	if errMsg == "" {
		return false
	}
	return strings.Contains(errMsg, "-32601") ||
		strings.Contains(strings.ToLower(errMsg), "method not found")
}

// mcpStageLabel maps the wire stage name to a human-friendly label.
func mcpStageLabel(name string) string {
	switch name {
	case "connect":
		return "connect"
	case "tools_list":
		return "tools/list"
	case "resources_list":
		return "resources/list"
	case "prompts_list":
		return "prompts/list"
	}
	return name
}

// classifyMCPTestErr maps a client.MCPTest error string to a JSON code field.
func classifyMCPTestErr(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "NOT_FOUND"):
		return "NOT_FOUND"
	case strings.Contains(msg, "INVALID"):
		return "INVALID"
	default:
		return "ipc_error"
	}
}

// formatMCPTestErr produces a three-segment what/why/how message for the
// human render path (Story §AC7).
//
// Code review patch P3 — anchor the prefix strip on the literal `ipc: [`
// produced by `ipc.Client.readResponse` (client.go:861). Without the anchor,
// any error message containing `] ` (e.g. a server name with that substring)
// would be silently chopped at the first occurrence.
func formatMCPTestErr(name string, err error) string {
	msg := err.Error()
	const prefix = "ipc: ["
	if strings.HasPrefix(msg, prefix) {
		if i := strings.Index(msg, "] "); i >= 0 {
			msg = msg[i+2:]
		}
	}
	return fmt.Sprintf("MCP server %q probe failed: %s. Run `rnix check mcp` for environment diagnostics.", name, msg)
}
