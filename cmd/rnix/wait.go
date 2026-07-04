package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
	"github.com/spf13/cobra"
)

// Story 63.1 — `rnix wait <pid>`.
//
// Blocks until the target process reaches a terminal state (Zombie/Dead) and
// propagates its exit code as this command's own exit code, enabling
// spawn-then-poll orchestration over the shell channel without parsing
// `rnix ps` output. With --timeout the wait is bounded: on expiry the command
// exits 124 (GNU timeout convention) and the target process is untouched, so
// the same PID can be waited again any number of times (pollable semantics).
//
// Exit codes: target's exit code (terminal) / 124 (timeout) / 1 (NOT_FOUND,
// daemon down, bad arguments).
//
// Like `rnix mcp test`, daemon-down is a hard fail WITHOUT EnsureDaemon:
// a freshly started daemon has no process to wait for.

var flagWaitTimeout string

var waitCmd = &cobra.Command{
	Use:   "wait <pid>",
	Short: "Block until a process exits and propagate its exit code",
	Long: `Wait for the target process to reach a terminal state, then exit with the
target's exit code. Already-terminated processes (including reaped ones found
in history) return immediately.

--timeout bounds the wait: on expiry the command exits 124 and the target
process is unaffected — rerun the same wait to keep polling. The duration
must be positive (e.g. 30s, 2m); 0 or negative values are rejected — omit
the flag to wait forever.

A suspended process does NOT complete a wait (Unix wait(2) semantics: wait
blocks across suspend/resume); use --timeout to bound that case.`,
	Args: cobra.ExactArgs(1),
	RunE: runWait,
}

func runWait(cmd *cobra.Command, args []string) error {
	mode := resolveOutputMode()
	w := cmd.OutOrStdout()
	errW := cmd.ErrOrStderr()

	pid64, err := strconv.ParseUint(args[0], 10, 64)
	if err != nil || pid64 == 0 {
		exitCode = 1
		waitEmitError(cmd, mode, "invalid_pid", fmt.Sprintf("invalid pid %q: expected a positive integer", args[0]))
		return nil
	}

	var timeoutMs int64
	if flagWaitTimeout != "" {
		d, err := time.ParseDuration(flagWaitTimeout)
		if err != nil {
			exitCode = 1
			waitEmitError(cmd, mode, "invalid_timeout", fmt.Sprintf("invalid --timeout %q: %v", flagWaitTimeout, err))
			return nil
		}
		if d <= 0 {
			exitCode = 1
			waitEmitError(cmd, mode, "invalid_timeout", fmt.Sprintf("invalid --timeout %q: duration must be positive (omit the flag to wait forever)", flagWaitTimeout))
			return nil
		}
		timeoutMs = d.Milliseconds()
		if timeoutMs == 0 {
			timeoutMs = 1
		}
	}

	client, err := ipc.Dial(ipc.SocketPath())
	if err != nil {
		exitCode = 1
		waitEmitError(cmd, mode, "daemon_down", "daemon not running")
		if mode != ui.ModeJSON {
			fmt.Fprintln(errW, "Daemon not running. Start the daemon with `rnix daemon` or any spawn command (e.g. `rnix --intent \"hello\"`).")
		}
		return nil
	}
	defer client.Close()

	resp, err := client.Wait(types.PID(pid64), timeoutMs)
	if err != nil {
		exitCode = 1
		code := "wait_failed"
		if strings.Contains(err.Error(), "[NOT_FOUND]") {
			code = "NOT_FOUND"
		}
		waitEmitError(cmd, mode, code, err.Error())
		return nil
	}

	if resp.TimedOut {
		exitCode = 124
		switch mode {
		case ui.ModeJSON:
			printWaitJSON(w, resp)
		default:
			// AC3: the timeout notice goes to stderr (stdout stays clean for
			// the terminal-state line), in quiet mode the 124 exit code alone
			// carries the result.
			fmt.Fprintf(errW, "wait: timed out after %s, pid %d not terminated\n", flagWaitTimeout, resp.PID)
		}
		return nil
	}

	exitCode = resp.ExitCode
	switch mode {
	case ui.ModeJSON:
		printWaitJSON(w, resp)
	case ui.ModeQuiet:
		fmt.Fprintln(w, resp.ExitCode)
	default:
		reason := resp.ExitReason
		if reason == "" {
			reason = "no reason recorded"
		}
		fmt.Fprintf(w, "PID %d exited with code %d (%s)\n", resp.PID, resp.ExitCode, reason)
	}
	return nil
}

// printWaitJSON renders the AC4 shape: pid / exit_code / exit_reason /
// timed_out as snake_case fields inside JSONResponse.data.
func printWaitJSON(w io.Writer, resp *ipc.WaitResponse) {
	payload := JSONResponse{OK: true, Data: map[string]any{
		"pid":         resp.PID,
		"exit_code":   resp.ExitCode,
		"exit_reason": resp.ExitReason,
		"timed_out":   resp.TimedOut,
	}}
	data, _ := json.Marshal(payload)
	fmt.Fprintln(w, string(data))
}

// waitEmitError renders an error in the current output mode: JSON gets the
// JSONResponse{OK:false} envelope on stdout (mirror runMCPReload), other
// modes get a one-line message on stderr.
func waitEmitError(cmd *cobra.Command, mode ui.OutputMode, code, message string) {
	if mode == ui.ModeJSON {
		payload := JSONResponse{OK: false, Error: map[string]any{
			"code":    code,
			"message": message,
		}}
		data, _ := json.Marshal(payload)
		fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return
	}
	if code != "daemon_down" { // daemon_down prints its own guidance line
		fmt.Fprintf(cmd.ErrOrStderr(), "wait: %s\n", message)
	}
}
