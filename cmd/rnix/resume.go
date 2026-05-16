package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
	"github.com/spf13/cobra"
)

var resumeCmd = &cobra.Command{
	Use:   "resume [--fork] <pid|uuid>",
	Short: "Resume a suspended or exited agent process",
	Long:  "Resume a process from checkpoint (suspended) or history (dead/zombie). Use --fork to create a new process with a new UUID that tracks its origin.",
	Args:  cobra.ExactArgs(1),
	RunE:  runResume,
}

var resumeFork bool

func init() {
	resumeCmd.Flags().BoolVar(&resumeFork, "fork", false, "Fork: create new UUID process instead of inheriting original UUID")
}

func runResume(cmd *cobra.Command, args []string) error {
	mode := resolveOutputMode()
	renderer := ui.NewRenderer(os.Stdout, mode)
	ui.InitStyles(renderer.Profile)

	client, err := ipc.Dial(ipc.SocketPath())
	if err != nil {
		ui.RenderError(renderer,
			"resume",
			"no active daemon",
			"resume failed: no active daemon",
			"rnix daemon status  to check daemon")
		exitCode = 1
		return nil
	}
	defer client.Close()

	arg := args[0]

	// Determine if argument is PID (all digits) or UUID
	var uuid string
	if _, parseErr := strconv.ParseUint(arg, 10, 64); parseErr == nil {
		// Argument looks like a PID — resolve to UUID
		pid, _ := strconv.ParseUint(arg, 10, 64)
		procs, listErr := client.ListAllProcs()
		if listErr != nil {
			ui.RenderError(renderer,
				fmt.Sprintf("PID %s", arg),
				"failed to list processes",
				fmt.Sprintf("resume PID %s failed: %v", arg, listErr),
				"rnix ps  to see active processes")
			exitCode = 1
			return nil
		}
		found := false
		for _, p := range procs {
			if uint64(p.PID) == pid {
				uuid = p.UUID
				found = true
				break
			}
		}
		if !found {
			ui.RenderError(renderer,
				fmt.Sprintf("PID %s", arg),
				"process not found",
				fmt.Sprintf("PID %s: no process found with this PID", arg),
				"rnix ps --uuid  to see processes with UUIDs")
			exitCode = 1
			return nil
		}
	} else {
		// Argument is a UUID
		uuid = arg
	}

	resp, err := client.ResumeWithOpts(uuid, resumeFork)
	if err != nil {
		ui.RenderError(renderer,
			fmt.Sprintf("UUID %s", uuid),
			err.Error(),
			fmt.Sprintf("resume UUID %s failed: %v", uuid, err),
			"rnix ps --uuid  to see process states")
		exitCode = 1
		return nil
	}

	mode_label := "resumed"
	if resumeFork {
		mode_label = "forked"
	}
	fmt.Fprintf(os.Stdout, "Process %s: PID %d (UUID %s), continuing from step %d\n",
		mode_label, resp.PID, resp.UUID, resp.ResumedFromStep)
	return nil
}
