package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/rnixai/rnix/debug"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
)

var gdbCmd = &cobra.Command{
	Use:   "gdb <pid>",
	Short: "Attach to a running agent for interactive debugging",
	Long:  "Attach to a running agent process and enter an interactive debugging session.\n\nReceives both syscall events and reasoning logs in real-time.\nType 'detach', 'quit', or 'q' to end the session.\nPress Ctrl+C to gracefully detach without affecting the target process.",
	Example: `  rnix gdb 1              Attach to PID 1
  rnix gdb 1 --verbose    Show full event details
  rnix gdb 1 --json       Output as JSON stream`,
	Args: cobra.ExactArgs(1),
	RunE: runGdb,
}

func runGdb(cmd *cobra.Command, args []string) error {
	pidNum, err := strconv.Atoi(args[0])
	if err != nil {
		mode := resolveOutputMode()
		renderer := ui.NewRenderer(os.Stdout, mode)
		ui.InitStyles(renderer.Profile)
		ui.RenderError(renderer,
			fmt.Sprintf("PID %s", args[0]),
			"invalid PID (expected number)",
			fmt.Sprintf("PID %s: not a valid process ID", args[0]),
			"rnix ps  查看活跃进程")
		exitCode = 1
		return nil
	}
	pid := types.PID(pidNum)

	w := cmd.OutOrStdout()
	mode := resolveOutputMode()

	client, err := ipc.Dial(ipc.SocketPath())
	if err != nil {
		renderer := ui.NewRenderer(w, mode)
		ui.InitStyles(renderer.Profile)
		ui.RenderError(renderer, fmt.Sprintf("PID %d", pid),
			"no active daemon (process not found)", "",
			"rnix ps  查看活跃进程")
		exitCode = 1
		return nil
	}
	defer client.Close()

	gdbCtx, gdbCancel := context.WithCancel(context.Background())
	defer gdbCancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	go func() {
		<-sigCh
		_ = client.SendDetach(pid)
		gdbCancel()
	}()

	var renderer *ui.Renderer
	if !flagJSON {
		renderer = ui.NewRenderer(w, mode)
		ui.InitStyles(renderer.Profile)
		fmt.Fprintf(w, "[gdb] attaching to PID %d...\n", pid)
	}

	// AttachGdb blocks until stream ends or returns for interactive sessions
	info, err := client.AttachGdb(pid, func(ev ipc.GdbEvent) {
		select {
		case <-gdbCtx.Done():
			return
		default:
		}

		switch ev.Type {
		case ipc.StreamGdbSyscall:
			if flagJSON {
				fmt.Fprintln(w, string(ev.Payload))
			} else {
				var sew ipc.SyscallEventWire
				if err := json.Unmarshal(ev.Payload, &sew); err == nil {
					event := wireToSyscallEvent(sew)
					if flagVerbose {
						fmt.Fprintln(w, debug.FormatEvent(event, debug.Options{Verbose: true}))
					} else if renderer != nil {
						fmt.Fprintln(w, ui.FormatTraceLine(renderer, event, false))
					} else {
						fmt.Fprintln(w, debug.FormatEvent(event, debug.DefaultOptions()))
					}
				}
			}
		case ipc.StreamGdbLog:
			if flagJSON {
				fmt.Fprintln(w, string(ev.Payload))
			} else {
				var lew ipc.LogEntryWire
				if err := json.Unmarshal(ev.Payload, &lew); err == nil {
					fmt.Fprintln(w, FormatLogEntry(renderer, lew))
				}
			}
		case ipc.StreamGdbStateChange:
			if flagJSON {
				fmt.Fprintln(w, string(ev.Payload))
			} else {
				fmt.Fprintf(w, "[gdb] process state changed\n")
			}
		}
	})
	if err != nil {
		if isNotFoundError(err) {
			if renderer != nil {
				ui.RenderError(renderer, fmt.Sprintf("PID %d", pid),
					"process not found or not running",
					fmt.Sprintf("PID %d: 不存在或已退出", pid),
					"rnix ps  查看活跃进程")
			}
		} else {
			if renderer != nil {
				ui.RenderError(renderer, fmt.Sprintf("PID %d", pid),
					err.Error(), "", "rnix ps  查看活跃进程")
			}
		}
		exitCode = 1
		return nil
	}

	if !flagJSON && info != nil {
		fmt.Fprintf(w, "[gdb] attached to PID %d (state=%s, intent=%q)\n",
			info.PID, info.State, info.Intent)
		if len(info.Skills) > 0 {
			fmt.Fprintf(w, "[gdb] skills: %s\n", strings.Join(info.Skills, ", "))
		}
		fmt.Fprintf(w, "[gdb] tokens used: %d\n", info.TokensUsed)
		fmt.Fprintf(w, "[gdb] type 'help' for commands, 'detach' to disconnect\n\n")
	}

	// Interactive command loop (read stdin for commands)
	// Blocks until detach/quit or Ctrl+C
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Fprint(w, "gdb> ")
	for scanner.Scan() {
		select {
		case <-gdbCtx.Done():
			goto done
		default:
		}

		line := strings.TrimSpace(scanner.Text())
		switch strings.ToLower(line) {
		case "detach", "quit", "q":
			_ = client.SendDetach(pid)
			goto done
		case "help", "h":
			fmt.Fprintln(w, "  detach / quit / q  - Disconnect from debug session")
			fmt.Fprintln(w, "  info / i           - Show process information")
			fmt.Fprintln(w, "  help / h           - Show this help")
		case "info", "i":
			fmt.Fprintf(w, "  PID:    %d\n", info.PID)
			fmt.Fprintf(w, "  State:  %s\n", info.State)
			fmt.Fprintf(w, "  Intent: %s\n", info.Intent)
			if len(info.Skills) > 0 {
				fmt.Fprintf(w, "  Skills: %s\n", strings.Join(info.Skills, ", "))
			}
			fmt.Fprintf(w, "  Tokens: %d\n", info.TokensUsed)
		case "":
			// ignore empty lines
		default:
			fmt.Fprintf(w, "[gdb] unknown command: %s (type 'help' for commands)\n", line)
		}
		fmt.Fprint(w, "gdb> ")
	}

done:
	if !flagJSON {
		fmt.Fprintf(w, "\n[gdb] detached from PID %d\n", pid)
	}

	return nil
}
