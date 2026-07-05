package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
	"github.com/spf13/cobra"
)

var heartbeatCmd = &cobra.Command{
	Use:   "heartbeat",
	Short: "Heartbeat monitor management",
}

var heartbeatStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show heartbeat monitor status",
	Args:  cobra.NoArgs,
	RunE:  runHeartbeatStatus,
}

func init() {
	heartbeatCmd.AddCommand(heartbeatStatusCmd)
}

func runHeartbeatStatus(cmd *cobra.Command, args []string) error {
	mode := resolveOutputMode()
	renderer := ui.NewRenderer(os.Stdout, mode)
	ui.InitStyles(renderer.Profile)

	client, err := ipc.Dial(ipc.SocketPath())
	if err != nil {
		ui.RenderError(renderer,
			"heartbeat",
			"no active daemon",
			"daemon is not running",
			"start a process to auto-start the daemon")
		exitCode = 1
		return nil
	}
	defer client.Close()

	status, err := client.HeartbeatStatus()
	if err != nil {
		ui.RenderError(renderer,
			"heartbeat",
			err.Error(),
			"failed to get heartbeat status",
			"check daemon version")
		exitCode = 1
		return nil
	}

	w := renderer.Writer

	runningStr := "stopped"
	if status.Running {
		runningStr = "running"
	}

	fmt.Fprintf(w, "Heartbeat Monitor: %s\n", runningStr)
	fmt.Fprintf(w, "Check Interval: %s\n", time.Duration(status.CheckIntervalMs)*time.Millisecond)
	fmt.Fprintf(w, "Total Stalled Detected: %d\n", status.TotalStalledDetected)

	if len(status.CurrentStalled) == 0 {
		fmt.Fprintln(w, "No stalled processes.")
	} else {
		fmt.Fprintln(w, "\nCurrent Stalled Processes:")
		fmt.Fprintf(w, "  %-6s %-14s %-8s %-12s %-12s %s\n", "PID", "UUID", "STALLS", "HB GAP", "DETECTED", "ACTION")
		for _, sp := range status.CurrentStalled {
			uuidShort := ui.ShortUUID(sp.UUID)
			// %-14s pads by bytes, but the "…" prefix is 3 bytes / 1 display
			// column — pad by display width (runewidth) to keep the column
			// aligned with the %-14s header above.
			uuidCol := uuidShort
			if pad := 14 - ui.DisplayWidth(uuidShort); pad > 0 {
				uuidCol += strings.Repeat(" ", pad)
			}
			gap := time.Duration(sp.HeartbeatGapMs) * time.Millisecond
			detected := time.Duration(sp.StalledDurationMs) * time.Millisecond
			fmt.Fprintf(w, "  %-6d %s %-8d %-12s %-12s %s\n",
				sp.PID, uuidCol, sp.ConsecutiveStalls, gap.Round(time.Second), detected.Round(time.Second), sp.LastAction)
		}
	}

	return nil
}
