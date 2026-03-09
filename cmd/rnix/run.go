package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
	agentshell "github.com/rnixai/rnix/shell"
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run <script.ash> [args...]",
	Short: "Execute an AgentShell script file",
	Long:  "Read and execute an AgentShell script file.\n\nSupports shebang (#!/usr/bin/env rnix run) for direct execution.",
	Example: `  rnix run deploy.ash
  rnix run deploy.ash --env staging
  ./deploy.ash  (with shebang and chmod +x)`,
	Args: cobra.MinimumNArgs(1),
	RunE: runRunCmd,
}

func init() {
	rootCmd.AddCommand(runCmd)
}

func runRunCmd(cmd *cobra.Command, args []string) error {
	scriptPath := args[0]

	data, err := os.ReadFile(scriptPath)
	if err != nil {
		mode := resolveOutputMode()
		renderer := ui.NewRenderer(os.Stdout, mode)
		ui.InitStyles(renderer.Profile)
		outputError(renderer, mode, "shell/script",
			fmt.Sprintf("cannot read %q: %v", scriptPath, err),
			"脚本文件读取失败", "检查文件路径是否正确")
		exitCode = 1
		return nil
	}

	content := agentshell.StripShebang(string(data))

	absPath, err := filepath.Abs(scriptPath)
	if err != nil {
		absPath = scriptPath
	}
	scriptDir := filepath.Dir(absPath)

	env := agentshell.NewEnvironmentFromOS().All()
	env["RNIX_SCRIPT_FILE"] = absPath
	env["RNIX_SCRIPT_DIR"] = scriptDir

	scriptArgs := args[1:]
	if len(scriptArgs) > 0 {
		env["RNIX_ARGS"] = strings.Join(scriptArgs, " ")
		for i, a := range scriptArgs {
			env[fmt.Sprintf("RNIX_ARG_%d", i)] = a
		}
	}

	mode := resolveOutputMode()
	renderer := ui.NewRenderer(os.Stdout, mode)
	ui.InitStyles(renderer.Profile)
	progress := ui.NewProgressReporter(renderer)

	start := time.Now()

	client, err := ipc.EnsureDaemon()
	if err != nil {
		outputError(renderer, mode, "daemon", err.Error(), "daemon 启动失败", "检查 rnix 是否正确安装")
		exitCode = 1
		return nil
	}
	defer client.Close()

	req := ipc.ExecScriptRequest{
		Script:    content,
		Env:       env,
		ScriptDir: scriptDir,
	}

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	scriptDone := make(chan struct{})
	go func() {
		select {
		case <-sigCh:
			progress.KernelMessage("script interrupted (SIGINT)")
			client.Close()
			select {
			case <-sigCh:
				forceExitFunc(130)
			case <-time.After(2 * time.Second):
			}
		case <-scriptDone:
		}
	}()

	scriptResp, scriptErr := client.ExecScriptAndWatch(req, func(ev ipc.StreamEvent) {
		if ev.Type != ipc.StreamProgress {
			return
		}
		var pp ipc.ProgressPayload
		if err := json.Unmarshal(ev.Payload, &pp); err != nil {
			return
		}
		if pp.Event == "script_step" {
			if pp.Intent != "" {
				progress.KernelMessage("script step %d/%d: %s", pp.Step, pp.Total, pp.Intent)
			} else {
				progress.KernelMessage("script step %d/%d...", pp.Step, pp.Total)
			}
		}
	})
	close(scriptDone)

	if scriptErr != nil {
		outputError(renderer, mode, "shell/script", scriptErr.Error(), "脚本执行失败", "检查脚本语法或重试")
		exitCode = 1
		return nil
	}

	elapsed := time.Since(start)

	if scriptResp == nil {
		outputError(renderer, mode, "shell/script", "no script result", "脚本返回空结果", "检查脚本内容")
		exitCode = 1
		return nil
	}

	if scriptResp.LastExitCode == 0 {
		outputSuccess(renderer, mode, 0, scriptResp.LastResult, scriptResp.TotalTokens, elapsed)
	} else {
		outputError(renderer, mode, "shell/script",
			fmt.Sprintf("script failed (exit %d)", scriptResp.LastExitCode),
			"脚本执行中断", "检查脚本中的 spawn 命令")
		ui.RenderSummary(renderer, 0, scriptResp.LastExitCode, scriptResp.TotalTokens, elapsed)
		exitCode = 1
	}

	return nil
}
