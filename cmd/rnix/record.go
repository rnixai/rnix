package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
	"github.com/spf13/cobra"
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
	w := cmd.OutOrStdout()
	client, err := ipc.Dial(ipc.SocketPath())
	if err != nil {
		if flagJSON {
			sessions := listStepSessions()
			resp := JSONResponse{OK: true, Data: map[string]any{"records": []any{}, "step_sessions": sessions}}
			data, _ := json.Marshal(resp)
			fmt.Fprintln(w, string(data))
		} else {
			fmt.Fprintln(w, "No recordings found.")
			printStepSessions(w)
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
		sessions := listStepSessions()
		resp := JSONResponse{OK: true, Data: map[string]any{"records": records, "step_sessions": sessions}}
		data, _ := json.Marshal(resp)
		fmt.Fprintln(w, string(data))
		return nil
	}

	if len(records) == 0 {
		fmt.Fprintln(w, "No recordings found.")
	} else {
		fmt.Fprintf(w, "%-20s %-6s %-12s %-8s %-20s %s\n",
			"RECORD-ID", "PID", "STATUS", "EVENTS", "START", "INTENT")
		for _, r := range records {
			startStr := time.UnixMilli(r.StartTime).Format("2006-01-02 15:04:05")
			intent := r.Intent
			if len(intent) > 30 {
				intent = intent[:27] + "..."
			}
			fmt.Fprintf(w, "%-20s %-6d %-12s %-8d %-20s %s\n",
				r.RecordID, r.PID, r.Status, r.EventCount, startStr, intent)
		}
	}

	printStepSessions(w)
	return nil
}

type stepSession struct {
	UUID     string    `json:"uuid"`
	PID      types.PID `json:"pid,omitempty"`
	Steps    int       `json:"steps"`
	Modified time.Time `json:"modified"`
	Legacy   bool      `json:"legacy"`
}

func listStepSessions() []stepSession {
	var sessions []stepSession
	for _, stepsDir := range allStepsDirs() {
		entries, err := os.ReadDir(stepsDir)
		if err != nil {
			continue
		}

		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			info, _ := e.Info()
			modified := time.Time{}
			if info != nil {
				modified = info.ModTime()
			}

			stepsPath := filepath.Join(stepsDir, name, "steps.jsonl")
			stepCount := countJSONLLines(stepsPath)

			if _, err := uuid.Parse(name); err == nil {
				pid := readMetaPID(filepath.Join(stepsDir, name, "process-meta.json"))
				sessions = append(sessions, stepSession{
					UUID:     name,
					PID:      pid,
					Steps:    stepCount,
					Modified: modified,
				})
			} else if _, err := strconv.Atoi(name); err == nil {
				pidNum, _ := strconv.ParseUint(name, 10, 64)
				sessions = append(sessions, stepSession{
					UUID:     name,
					PID:      types.PID(pidNum),
					Steps:    stepCount,
					Modified: modified,
					Legacy:   true,
				})
			}
		}
	}
	return sessions
}

func printStepSessions(w interface{ Write([]byte) (int, error) }) {
	sessions := listStepSessions()
	if len(sessions) == 0 {
		return
	}
	fmt.Fprintf(w, "\nStep Sessions:\n")
	fmt.Fprintf(w, "%-40s %-6s %-8s %s\n", "UUID", "PID", "STEPS", "MODIFIED")
	for _, s := range sessions {
		id := s.UUID
		if len(id) > 36 {
			id = id[:36]
		}
		suffix := ""
		if s.Legacy {
			suffix = " (legacy)"
		}
		pidStr := ""
		if s.PID > 0 {
			pidStr = fmt.Sprintf("%d", s.PID)
		}
		modStr := ""
		if !s.Modified.IsZero() {
			modStr = s.Modified.Format("2006-01-02 15:04:05")
		}
		fmt.Fprintf(w, "%-40s %-6s %-8d %s%s\n", id, pidStr, s.Steps, modStr, suffix)
	}
}

func countJSONLLines(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	count := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		if len(scanner.Bytes()) > 0 {
			count++
		}
	}
	return count
}

func readMetaPID(path string) types.PID {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	var meta struct {
		PID types.PID `json:"pid"`
	}
	if json.Unmarshal(data, &meta) == nil {
		return meta.PID
	}
	return 0
}

func isUUIDDir(name string) bool {
	_, err := uuid.Parse(name)
	return err == nil
}

func isLegacyPIDDir(name string) bool {
	n, err := strconv.Atoi(name)
	return err == nil && n > 0
}

func scanStepSessions(baseDir string) ([]StepSessionEntry, error) {
	stepsDir := filepath.Join(baseDir, "data", "steps")
	entries, err := os.ReadDir(stepsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var sessions []StepSessionEntry
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		stepsPath := filepath.Join(stepsDir, name, "steps.jsonl")
		if _, err := os.Stat(stepsPath); err != nil {
			continue
		}

		if isUUIDDir(name) {
			pid := readMetaPID(filepath.Join(stepsDir, name, "process-meta.json"))
			sessions = append(sessions, StepSessionEntry{
				UUID:     name,
				PID:      pid,
				StepFile: stepsPath,
			})
		} else if isLegacyPIDDir(name) {
			pidNum, _ := strconv.ParseUint(name, 10, 64)
			sessions = append(sessions, StepSessionEntry{
				UUID:     name,
				PID:      types.PID(pidNum),
				IsLegacy: true,
				StepFile: stepsPath,
			})
		}
	}
	return sessions, nil
}

func matchStepUUIDPrefix(baseDir, prefix string) (string, error) {
	stepsDir := filepath.Join(baseDir, "data", "steps")
	entries, err := os.ReadDir(stepsDir)
	if err != nil {
		return "", fmt.Errorf("steps directory not found: %w", err)
	}

	var matched string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if !isUUIDDir(name) {
			continue
		}
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		stepsPath := filepath.Join(stepsDir, name, "steps.jsonl")
		if _, err := os.Stat(stepsPath); err != nil {
			continue
		}
		if matched != "" {
			return "", fmt.Errorf("ambiguous prefix %q matches multiple directories", prefix)
		}
		matched = filepath.Join(stepsDir, name)
	}
	if matched == "" {
		return "", fmt.Errorf("no step session matching prefix %q", prefix)
	}
	return matched, nil
}

// StepSessionEntry represents a step session directory entry for CLI display.
type StepSessionEntry struct {
	UUID     string
	PID      types.PID
	IsLegacy bool
	StepFile string
}
