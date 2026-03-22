package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var watchCmd = &cobra.Command{
	Use:   "watch <pid>",
	Short: "Observe agent reasoning steps in real time",
	Long:  "Attach to a running process and display each reasoning step as it completes.",
	Args:  cobra.ExactArgs(1),
	RunE:  runWatch,
}

func runWatch(_ *cobra.Command, args []string) error {
	pidVal, err := strconv.ParseUint(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid pid: %s", args[0])
	}
	pid := types.PID(pidVal)

	client, err := ipc.EnsureDaemon()
	if err != nil {
		return err
	}
	defer client.Close()

	profile := ui.DetectProfile(os.Stdout)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if term.IsTerminal(int(os.Stdin.Fd())) {
		oldState, tErr := term.MakeRaw(int(os.Stdin.Fd()))
		if tErr == nil {
			defer func() { _ = term.Restore(int(os.Stdin.Fd()), oldState) }()
			go readQuitKey(func() {
				cancel()
				client.Close()
			})
		}
	}

	_, watchErr := client.WatchProcess(pid, func(ev ipc.StreamEvent) {
		select {
		case <-ctx.Done():
			return
		default:
		}
		renderWatchEvent(ev, profile)
	})

	fmt.Println()

	if watchErr != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("watch: %w", watchErr)
	}
	return nil
}

func readQuitKey(quit func()) {
	buf := make([]byte, 1)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 {
			return
		}
		if buf[0] == 'q' || buf[0] == 'Q' || buf[0] == 3 {
			quit()
			return
		}
	}
}

func renderWatchEvent(ev ipc.StreamEvent, profile ui.TerminalProfile) {
	if ev.Type != ipc.StreamProgress && ev.Type != ipc.StreamComplete && ev.Type != ipc.StreamError {
		return
	}

	var pp ipc.ProgressPayload
	if err := json.Unmarshal(ev.Payload, &pp); err != nil {
		return
	}

	switch pp.Event {
	case "spawn":
		if pp.Provider != "" && pp.Model != "" {
			fmt.Printf("\r\033[K  PID %d (%s/%s)\n", pp.PID, pp.Provider, pp.Model)
		} else {
			fmt.Printf("\r\033[K  PID %d spawned\n", pp.PID)
		}
	case "step":
		if pp.Total > 0 {
			fmt.Printf("\r\033[K  [step %d/%d] thinking...", pp.Step, pp.Total)
		} else {
			fmt.Printf("\r\033[K  [step %d] thinking...", pp.Step)
		}
	case "step_complete":
		icon := watchSuccessIcon(profile)
		if pp.HasError {
			icon = watchErrorIcon(profile)
		}
		dur := watchFormatDuration(pp.DurationMs)
		fmt.Printf("\r\033[K  [step %d] %s → %s  %s  %s\n", pp.Step, pp.Action, pp.Summary, dur, icon)
	case "complete":
		icon := watchSuccessIcon(profile)
		if pp.ExitCode != 0 {
			icon = watchErrorIcon(profile)
		}
		sep := "───────────────────────────"
		if !profile.IsUnicode {
			sep = "---------------------------"
		}
		fmt.Printf("\r\033[K  %s\n", sep)
		fmt.Printf("  %s PID %d completed (exit=%d)\n", icon, pp.PID, pp.ExitCode)
	case "error":
		icon := watchErrorIcon(profile)
		fmt.Printf("\r\033[K  %s error: %s\n", icon, pp.ErrorMessage)
	}
}

func watchSuccessIcon(p ui.TerminalProfile) string {
	if p.IsUnicode {
		return "✓"
	}
	return "OK"
}

func watchErrorIcon(p ui.TerminalProfile) string {
	if p.IsUnicode {
		return "✗"
	}
	return "ERR"
}

func watchFormatDuration(ms float64) string {
	if ms < 1000 {
		return fmt.Sprintf("%.1fs", ms/1000)
	}
	sec := ms / 1000
	if sec < 60 {
		return fmt.Sprintf("%.0fs", sec)
	}
	min := int(sec) / 60
	remSec := int(sec) % 60
	return fmt.Sprintf("%dm%ds", min, remSec)
}
