package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
)

var recordCmd = &cobra.Command{
	Use:   "record <start|stop|list> [pid]",
	Short: "Manage execution recording for agent processes",
	Long: `Record execution events (syscalls, LLM responses, context changes, state transitions)
for offline analysis and time-travel debugging.

Subcommands:
  start <pid>   Start recording events for the given process
  stop <pid>    Stop recording and persist to disk
  list          List all recorded sessions`,
	Example: `  rnix record start 1       Start recording PID 1
  rnix record stop 1        Stop recording PID 1
  rnix record list           List all recordings`,
}

var recordStartCmd = &cobra.Command{
	Use:   "start <pid>",
	Short: "Start recording execution events for a process",
	Args:  cobra.ExactArgs(1),
	RunE:  runRecordStart,
}

var recordStopCmd = &cobra.Command{
	Use:   "stop <pid>",
	Short: "Stop recording execution events for a process",
	Args:  cobra.ExactArgs(1),
	RunE:  runRecordStop,
}

var recordListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all recorded sessions",
	Args:  cobra.NoArgs,
	RunE:  runRecordList,
}

func init() {
	recordCmd.AddCommand(recordStartCmd)
	recordCmd.AddCommand(recordStopCmd)
	recordCmd.AddCommand(recordListCmd)
	rootCmd.AddCommand(recordCmd)
}

func runRecordStart(cmd *cobra.Command, args []string) error {
	pidNum, err := strconv.ParseUint(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid PID: %s", args[0])
	}
	pid := types.PID(pidNum)

	client, err := ipc.Dial(ipc.SocketPath())
	if err != nil {
		mode := resolveOutputMode()
		renderer := ui.NewRenderer(os.Stdout, mode)
		ui.InitStyles(renderer.Profile)
		ui.RenderError(renderer, fmt.Sprintf("PID %d", pid),
			"no active daemon", "", "rnix ps  to see active processes")
		exitCode = 1
		return nil
	}
	defer client.Close()

	recordID, err := client.RecordStart(pid)
	if err != nil {
		mode := resolveOutputMode()
		renderer := ui.NewRenderer(os.Stdout, mode)
		ui.InitStyles(renderer.Profile)
		ui.RenderError(renderer, fmt.Sprintf("PID %d", pid),
			err.Error(), "", "check that the process exists and is running")
		exitCode = 1
		return nil
	}

	if flagJSON {
		resp := JSONResponse{OK: true, Data: map[string]any{"record_id": recordID, "pid": pid}}
		data, _ := json.Marshal(resp)
		fmt.Fprintln(os.Stdout, string(data))
	} else {
		fmt.Fprintf(os.Stdout, "Recording started for PID %d (record-id: %s)\n", pid, recordID)
	}
	return nil
}

func runRecordStop(cmd *cobra.Command, args []string) error {
	pidNum, err := strconv.ParseUint(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid PID: %s", args[0])
	}
	pid := types.PID(pidNum)

	client, err := ipc.Dial(ipc.SocketPath())
	if err != nil {
		mode := resolveOutputMode()
		renderer := ui.NewRenderer(os.Stdout, mode)
		ui.InitStyles(renderer.Profile)
		ui.RenderError(renderer, fmt.Sprintf("PID %d", pid),
			"no active daemon", "", "rnix ps  to see active processes")
		exitCode = 1
		return nil
	}
	defer client.Close()

	eventCount, err := client.RecordStop(pid)
	if err != nil {
		mode := resolveOutputMode()
		renderer := ui.NewRenderer(os.Stdout, mode)
		ui.InitStyles(renderer.Profile)
		ui.RenderError(renderer, fmt.Sprintf("PID %d", pid),
			err.Error(), "", "check that the process is being recorded")
		exitCode = 1
		return nil
	}

	if flagJSON {
		resp := JSONResponse{OK: true, Data: map[string]any{"event_count": eventCount, "pid": pid}}
		data, _ := json.Marshal(resp)
		fmt.Fprintln(os.Stdout, string(data))
	} else {
		fmt.Fprintf(os.Stdout, "Recording stopped for PID %d (%d events captured)\n", pid, eventCount)
	}
	return nil
}

func runRecordList(cmd *cobra.Command, args []string) error {
	client, err := ipc.Dial(ipc.SocketPath())
	if err != nil {
		if flagJSON {
			resp := JSONResponse{OK: true, Data: map[string]any{"records": []any{}}}
			data, _ := json.Marshal(resp)
			fmt.Fprintln(os.Stdout, string(data))
		} else {
			fmt.Fprintln(os.Stdout, "No recordings found.")
		}
		return nil
	}
	defer client.Close()

	records, err := client.RecordList()
	if err != nil {
		mode := resolveOutputMode()
		renderer := ui.NewRenderer(os.Stdout, mode)
		ui.InitStyles(renderer.Profile)
		ui.RenderError(renderer, "record",
			err.Error(), "", "check that the daemon is running properly")
		exitCode = 1
		return nil
	}

	if flagJSON {
		resp := JSONResponse{OK: true, Data: map[string]any{"records": records}}
		data, _ := json.Marshal(resp)
		fmt.Fprintln(os.Stdout, string(data))
		return nil
	}

	if len(records) == 0 {
		fmt.Fprintln(os.Stdout, "No recordings found.")
		return nil
	}

	fmt.Fprintf(os.Stdout, "%-20s %-6s %-12s %-8s %-20s %s\n",
		"RECORD-ID", "PID", "STATUS", "EVENTS", "START", "INTENT")
	for _, r := range records {
		startStr := time.UnixMilli(r.StartTime).Format("2006-01-02 15:04:05")
		intent := r.Intent
		if len(intent) > 30 {
			intent = intent[:27] + "..."
		}
		fmt.Fprintf(os.Stdout, "%-20s %-6d %-12s %-8d %-20s %s\n",
			r.RecordID, r.PID, r.Status, r.EventCount, startStr, intent)
	}
	return nil
}
