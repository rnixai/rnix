package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

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

func init() {
	mcpCmd.AddCommand(mcpListCmd)
	mcpCmd.AddCommand(mcpTestCmd)
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

// placeholderIfZero renders integer placeholders ("—") for fields that
// Story 48.3 deliberately leaves at zero pending Story 48.5 cache work.
func placeholderIfZero(n int) string {
	if n == 0 {
		return "—"
	}
	return fmt.Sprintf("%d", n)
}

// lastCheckLabel returns "—" for un-probed mounts, otherwise a wall-clock
// label. Story 48.3 always emits "—" because 48.5 owns the probing cadence.
func lastCheckLabel(_ int64) string {
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

func renderMCPTestHuman(w io.Writer, resp *ipc.MCPTestResponse) {
	totalStages := len(resp.Stages)
	for i, s := range resp.Stages {
		label := mcpStageLabel(s.Name)
		marker := "OK"
		detail := ""
		switch {
		case !s.OK && strings.Contains(strings.ToLower(s.Error), "context deadline exceeded"):
			marker = "TIMEOUT"
			detail = fmt.Sprintf(" (after %dms)", s.DurationMs)
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
		fmt.Fprintf(w, "[%d/%d] %s ... %s%s\n", i+1, totalStages, label, marker, detail)
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
func formatMCPTestErr(name string, err error) string {
	msg := err.Error()
	// Strip the leading "ipc: [CODE]" wrapper to surface the human reason.
	if i := strings.Index(msg, "] "); i >= 0 {
		msg = msg[i+2:]
	}
	return fmt.Sprintf("MCP server %q probe failed: %s. Run `rnix check mcp` for environment diagnostics.", name, msg)
}
