package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/rnixai/rnix/drivers/mcp"
	"github.com/rnixai/rnix/internal/config"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/spf13/cobra"
)

// Story 48.3 — `rnix check mcp` subsystem diagnostic.
//
// Distinct from `rnix doctor` which focuses on LLM providers; `rnix check`
// hosts subsystem diagnostics (currently `mcp`, future: `daemon`, `skill`).
// Combining would balloon doctor's scope (Story §决策 5).
//
// Test-injection seams:
//   - var lookPath = exec.LookPath  → tests monkey-patch
//   - var runCommand = ...           → tests replace
//   - var detectChromium = ...       → tests replace
//   - var mcpConfigPathForCheck      → tests redirect mcp.yaml lookup

var (
	lookPath              = exec.LookPath
	runCommand            = defaultRunCommand
	detectChromium        = defaultDetectChromium
	mcpConfigPathForCheck = "" // empty = use default (globalDir/mcp.yaml)
)

// checkLevelMCP is a thin alias to keep the file self-contained without
// importing doctor.go internals. Values mirror doctor.go's checkLevel.
type checkLevelMCP string

const (
	checkMCPInfo  checkLevelMCP = "info"
	checkMCPWarn  checkLevelMCP = "warn"
	checkMCPError checkLevelMCP = "error"
)

// checkResult mirrors doctorCheck for the JSON wire (Story §AC3) without
// reaching into doctor.go's private types.
type checkResult struct {
	Code    string        `json:"code"`
	Level   checkLevelMCP `json:"level"`
	Message string        `json:"message"`
	Detail  string        `json:"detail,omitempty"`
	Hint    string        `json:"hint,omitempty"`
}

type checkMcpReport struct {
	Status string        `json:"status"` // pass | warn | fail
	Checks []checkResult `json:"checks"`
}

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Diagnostic checks for subsystems",
	Long:  "Run targeted environment / configuration checks for one rnix subsystem (currently `mcp`).",
}

var checkMcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Verify MCP runtime prerequisites (node, npx, optional Chromium)",
	Long: `Check that the host environment has the binaries needed to run MCP servers
declared in mcp.yaml. By default probes node + npx; if any server in
mcp.yaml references "playwright", additionally probes for a Chromium
install (npx playwright install chromium).`,
	Args: cobra.NoArgs,
	RunE: runCheckMcp,
}

func init() {
	checkCmd.AddCommand(checkMcpCmd)
}

func runCheckMcp(cmd *cobra.Command, _ []string) error {
	w := cmd.OutOrStdout()
	mode := resolveOutputMode()

	report := checkMcpReport{}
	report.Checks = append(report.Checks, checkNode())
	report.Checks = append(report.Checks, checkNpx())

	yamlPath := resolveMCPConfigPath()
	yamlCheck, needsChromium := scanMCPYaml(yamlPath)
	report.Checks = append(report.Checks, yamlCheck)
	if needsChromium {
		report.Checks = append(report.Checks, checkChromium())
	}

	report.Status = summarizeMcpChecks(report.Checks)

	switch mode {
	case ui.ModeJSON:
		renderCheckMcpJSON(w, report)
	case ui.ModeQuiet:
		renderCheckMcpQuiet(w, report)
	default:
		renderCheckMcpHuman(w, report)
	}

	if report.Status == "fail" {
		exitCode = 1
	}
	return nil
}

// checkNode probes node availability + version.
func checkNode() checkResult {
	path, err := lookPath("node")
	if err != nil {
		return checkResult{
			Code:    "NODE",
			Level:   checkMCPWarn,
			Message: "node not found in $PATH",
			Hint:    "Install Node.js (https://nodejs.org) or via `nvm install --lts`.",
		}
	}
	ver, _ := runCommand("node", "--version")
	return checkResult{
		Code:    "NODE",
		Level:   checkMCPInfo,
		Message: "node available",
		Detail:  fmt.Sprintf("%s (%s)", strings.TrimSpace(ver), path),
	}
}

// checkNpx probes npx — required because most MCP servers boot via `npx`.
func checkNpx() checkResult {
	path, err := lookPath("npx")
	if err != nil {
		return checkResult{
			Code:    "NPX",
			Level:   checkMCPError,
			Message: "npx not found in $PATH (required for MCP server bootstrapping)",
			Hint:    "Install npm via Node.js — npx ships with npm. Run `npm i -g npm` to refresh.",
		}
	}
	ver, _ := runCommand("npx", "--version")
	return checkResult{
		Code:    "NPX",
		Level:   checkMCPInfo,
		Message: "npx available",
		Detail:  fmt.Sprintf("%s (%s)", strings.TrimSpace(ver), path),
	}
}

// checkChromium probes for a playwright-compatible Chromium install. Only
// invoked when mcp.yaml references playwright (Story §AC3 §"关键判定").
func checkChromium() checkResult {
	path, err := detectChromium()
	if err != nil || path == "" {
		return checkResult{
			Code:    "CHROMIUM",
			Level:   checkMCPWarn,
			Message: "chromium not found",
			Hint:    "Run `npx playwright install chromium` to provision the Playwright-managed copy.",
		}
	}
	return checkResult{
		Code:    "CHROMIUM",
		Level:   checkMCPInfo,
		Message: "chromium available",
		Detail:  path,
	}
}

// scanMCPYaml inspects mcp.yaml (if present) to learn whether any server
// references playwright. Returns an INFO check describing yaml status plus
// a bool indicating whether the Chromium probe should run.
func scanMCPYaml(path string) (checkResult, bool) {
	if path == "" {
		return checkResult{
			Code:    "MCP_YAML",
			Level:   checkMCPInfo,
			Message: "mcp.yaml not configured",
			Hint:    "Run `rnix init` to set up the global configuration directory.",
		}, false
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return checkResult{
				Code:    "MCP_YAML",
				Level:   checkMCPInfo,
				Message: "mcp.yaml not found",
				Detail:  path,
				Hint:    "Run `rnix init --with-mcp-examples` once that flag lands (Story 48.4), or write mcp.yaml manually.",
			}, false
		}
		return checkResult{
			Code:    "MCP_YAML",
			Level:   checkMCPWarn,
			Message: "mcp.yaml not readable",
			Detail:  err.Error(),
		}, false
	}
	cfg, err := mcp.LoadMCPConfig(path)
	if err != nil {
		return checkResult{
			Code:    "MCP_YAML",
			Level:   checkMCPWarn,
			Message: "mcp.yaml failed to parse",
			Detail:  err.Error(),
			Hint:    "Fix the YAML structure and re-run `rnix check mcp`.",
		}, false
	}
	needsChromium := false
	for name, server := range cfg.Servers {
		if mcpServerRequiresChromium(name, server) {
			needsChromium = true
			break
		}
	}
	return checkResult{
		Code:    "MCP_YAML",
		Level:   checkMCPInfo,
		Message: fmt.Sprintf("mcp.yaml loaded (%d server(s))", len(cfg.Servers)),
		Detail:  path,
	}, needsChromium
}

// mcpServerRequiresChromium returns true when a server name or command hints
// at playwright usage (the only MCP server today that requires a separate
// browser binary).
func mcpServerRequiresChromium(name string, server mcp.MCPServerConfig) bool {
	lowerName := strings.ToLower(name)
	if strings.Contains(lowerName, "playwright") {
		return true
	}
	if strings.Contains(strings.ToLower(server.Command), "playwright") {
		return true
	}
	for _, arg := range server.Args {
		if strings.Contains(strings.ToLower(arg), "playwright") {
			return true
		}
	}
	return false
}

// resolveMCPConfigPath returns the mcp.yaml path that `rnix check mcp` should
// inspect. Honors the test override, otherwise mirrors main.go's resolution
// (globalDir/mcp.yaml).
func resolveMCPConfigPath() string {
	if mcpConfigPathForCheck != "" {
		return mcpConfigPathForCheck
	}
	globalDir, err := config.GlobalDir()
	if err != nil {
		return ""
	}
	return filepath.Join(globalDir, "mcp.yaml")
}

// defaultRunCommand runs `name args...` with a 10s timeout and returns
// trimmed stdout. Replaceable for tests.
func defaultRunCommand(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// defaultDetectChromium scans platform-specific paths for an installed
// Chromium-compatible browser. Returns the path on success.
func defaultDetectChromium() (string, error) {
	candidates := chromiumCandidates()
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	// Last resort: PATH lookup.
	if p, err := lookPath("chromium"); err == nil {
		return p, nil
	}
	if p, err := lookPath("google-chrome"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("chromium not found in known paths or $PATH")
}

// chromiumCandidates returns plausible Chromium paths for the current OS.
func chromiumCandidates() []string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "linux":
		return []string{
			"/usr/bin/chromium",
			"/usr/bin/chromium-browser",
			"/usr/bin/google-chrome",
			"/usr/bin/google-chrome-stable",
			filepath.Join(home, ".cache", "ms-playwright", "chromium-linux", "chrome-linux", "chrome"),
		}
	case "darwin":
		return []string{
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			filepath.Join(home, "Library", "Caches", "ms-playwright", "chromium-mac", "chrome-mac", "Chromium.app", "Contents", "MacOS", "Chromium"),
		}
	}
	return nil
}

// summarizeMcpChecks reduces the per-check levels to a single status.
func summarizeMcpChecks(checks []checkResult) string {
	hasErr, hasWarn := false, false
	for _, c := range checks {
		switch c.Level {
		case checkMCPError:
			hasErr = true
		case checkMCPWarn:
			hasWarn = true
		}
	}
	switch {
	case hasErr:
		return "fail"
	case hasWarn:
		return "warn"
	}
	return "pass"
}

func renderCheckMcpHuman(w io.Writer, r checkMcpReport) {
	fmt.Fprintf(w, "rnix check mcp — tested at %s\n\n", time.Now().UTC().Format(time.RFC3339))
	for _, c := range r.Checks {
		marker := "[INFO]"
		switch c.Level {
		case checkMCPWarn:
			marker = "[WARN]"
		case checkMCPError:
			marker = "[ERROR]"
		}
		fmt.Fprintf(w, "%s %-12s %s\n", marker, c.Code, c.Message)
		if c.Detail != "" {
			fmt.Fprintf(w, "                detail: %s\n", c.Detail)
		}
		if c.Hint != "" {
			fmt.Fprintf(w, "                hint:   %s\n", c.Hint)
		}
	}
	info, warn, errs := tallyChecks(r.Checks)
	fmt.Fprintln(w)
	switch r.Status {
	case "pass":
		fmt.Fprintf(w, "All checks passed (%d info / %d warn / %d error).\n", info, warn, errs)
	case "warn":
		fmt.Fprintf(w, "Completed with warnings (%d info / %d warn / %d error).\n", info, warn, errs)
	case "fail":
		fmt.Fprintf(w, "Checks failed (%d info / %d warn / %d error). See hints above.\n", info, warn, errs)
	}
}

func renderCheckMcpJSON(w io.Writer, r checkMcpReport) {
	data, _ := json.Marshal(JSONResponse{OK: r.Status != "fail", Data: r})
	fmt.Fprintln(w, string(data))
}

// renderCheckMcpQuiet only emits ERROR-level findings (Story §AC6).
func renderCheckMcpQuiet(w io.Writer, r checkMcpReport) {
	for _, c := range r.Checks {
		if c.Level == checkMCPError {
			fmt.Fprintf(w, "%s: %s\n", c.Code, c.Message)
		}
	}
}

func tallyChecks(checks []checkResult) (info, warn, errs int) {
	for _, c := range checks {
		switch c.Level {
		case checkMCPInfo:
			info++
		case checkMCPWarn:
			warn++
		case checkMCPError:
			errs++
		}
	}
	return
}
