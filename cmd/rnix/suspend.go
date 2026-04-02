package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
	"github.com/spf13/cobra"
)

var suspendCmd = &cobra.Command{
	Use:   "suspend <pid>",
	Short: "Suspend a running agent process",
	Args:  cobra.ExactArgs(1),
	RunE:  runSuspend,
}

func runSuspend(cmd *cobra.Command, args []string) error {
	pidNum, err := strconv.ParseUint(args[0], 10, 64)
	if err != nil {
		mode := resolveOutputMode()
		renderer := ui.NewRenderer(os.Stdout, mode)
		ui.InitStyles(renderer.Profile)
		ui.RenderError(renderer,
			fmt.Sprintf("PID %s", args[0]),
			"invalid PID (expected number)",
			fmt.Sprintf("PID %s: not a valid process ID", args[0]),
			"rnix ps  to see active processes")
		exitCode = 1
		return nil
	}
	pid := types.PID(pidNum)

	mode := resolveOutputMode()
	renderer := ui.NewRenderer(os.Stdout, mode)
	ui.InitStyles(renderer.Profile)

	client, err := ipc.Dial(ipc.SocketPath())
	if err != nil {
		ui.RenderError(renderer,
			fmt.Sprintf("PID %d", pid),
			"no active daemon (process not found)",
			fmt.Sprintf("PID %d: no active process", pid),
			"rnix ps  to see active processes")
		exitCode = 1
		return nil
	}
	defer client.Close()

	resp, err := client.Suspend(pid)
	if err != nil {
		ui.RenderError(renderer,
			fmt.Sprintf("PID %d", pid),
			err.Error(),
			fmt.Sprintf("suspend PID %d failed: %v", pid, err),
			"rnix ps  to see process states")
		exitCode = 1
		return nil
	}

	fmt.Fprintf(os.Stdout, "Process %d suspended (checkpoint at step %d)\n", resp.PID, resp.CheckpointStep)
	return nil
}
