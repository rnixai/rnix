package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

// Story 48.4 — `rnix --agent playwright-demo` spawn preflight.
//
// Only triggers on the literal "playwright-demo" agent name; other agents
// (including future generic MCP agents) bypass entirely. A generic per-agent
// preflight is the natural product of Story 48.5's health registry; building
// it here would impose a 30-100ms startup tax on every spawn for no current
// benefit (Story §决策 2 + 易错点 5).
//
// Three checks run in order, short-circuit on the first ERROR:
//
//	1. mcp.yaml exists (otherwise hint `rnix init --with-mcp-examples`)
//	2. npx in $PATH      (otherwise list npm/brew/website install paths)
//	3. Chromium detected (WARN only — Playwright can install at runtime)
//
// Helpers are deliberately disjoint from check.go's package-level vars: the
// test pattern (atdd_48_4_preflight_test.go) installs its own monkey-patches
// so check.go's tests remain isolated.

var (
	preflightLookPath       func(string) (string, error) = exec.LookPath
	preflightDetectChromium func() (string, error)       = defaultDetectChromium
	preflightMcpYamlPath    string                       // empty = derive from globalDirForInit/config.GlobalDir
)

// preflightResult mirrors checkResult (check.go) but lives in its own type so
// the spawn-time semantic — "block or warn" — is not confused with check-mcp's
// "diagnostic only" intent (Story §易错点 9).
type preflightResult struct {
	Code    string
	Level   checkLevelMCP
	Message string
	Detail  string
	Hint    string
}

// runSpawnPreflight returns ok=false when the spawn must be aborted. When ok
// is false, the caller should set exitCode=1 and return early WITHOUT calling
// EnsureDaemon or any IPC method (Story §决策 4 + 易错点 8).
//
// The function writes any user-facing output (errors / warnings) to
// cmd.OutOrStdout(). The cobra command is only used as an io.Writer container;
// no Cobra flags / args are read here.
func runSpawnPreflight(cmd *cobra.Command, agentName string) (bool, error) {
	if agentName != "playwright-demo" {
		return true, nil
	}

	w := cmd.OutOrStdout()

	// Check 1: mcp.yaml exists.
	if r := preflightCheckMcpYaml(); r.Level == checkMCPError {
		emitPreflightError(w, r)
		exitCode = 1
		return false, nil
	}

	// Check 2: npx exists.
	if r := preflightCheckNpx(); r.Level == checkMCPError {
		emitPreflightError(w, r)
		exitCode = 1
		return false, nil
	}

	// Check 3: Chromium exists (WARN-only).
	if r := preflightCheckChromium(); r.Level == checkMCPWarn {
		emitPreflightWarn(w, r)
		// fall through, ok remains true
	}

	return true, nil
}

func preflightCheckMcpYaml() preflightResult {
	path := resolvePreflightMcpYamlPath()
	if path == "" {
		return preflightResult{
			Code:    "PREFLIGHT_MCP_YAML_PATH_UNRESOLVED",
			Level:   checkMCPError,
			Message: "mcp.yaml location could not be resolved",
			Detail:  "global config directory unknown",
			Hint:    "Run `rnix init --with-mcp-examples` to bootstrap the global config + mcp.yaml.",
		}
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return preflightResult{
				Code:    "PREFLIGHT_MCP_YAML_MISSING",
				Level:   checkMCPError,
				Message: "mcp.yaml not found at " + path,
				Detail:  "playwright-demo references the `playwright` server, which must be declared in mcp.yaml.",
				Hint:    "Run `rnix init --with-mcp-examples` to generate it.",
			}
		}
		return preflightResult{
			Code:    "PREFLIGHT_MCP_YAML_UNREADABLE",
			Level:   checkMCPError,
			Message: "mcp.yaml not readable at " + path,
			Detail:  err.Error(),
			Hint:    "Check filesystem permissions on " + path + ".",
		}
	}
	return preflightResult{Code: "PREFLIGHT_MCP_YAML", Level: checkMCPInfo, Message: "mcp.yaml found", Detail: path}
}

func preflightCheckNpx() preflightResult {
	path, err := preflightLookPath("npx")
	if err != nil {
		return preflightResult{
			Code:    "PREFLIGHT_NPX_MISSING",
			Level:   checkMCPError,
			Message: "npx not found in $PATH",
			Detail:  "playwright-demo needs npx to launch @playwright/mcp.",
			Hint:    "Install Node.js so npx ships alongside npm — see Linux/macOS hints below.",
		}
	}
	return preflightResult{Code: "PREFLIGHT_NPX", Level: checkMCPInfo, Message: "npx available", Detail: path}
}

func preflightCheckChromium() preflightResult {
	path, err := preflightDetectChromium()
	if err != nil || path == "" {
		detail := ""
		if err != nil {
			detail = err.Error()
		}
		return preflightResult{
			Code:    "PREFLIGHT_CHROMIUM_MISSING",
			Level:   checkMCPWarn,
			Message: "Chromium not found in known paths",
			Detail:  detail,
			Hint:    "Run `npx playwright install chromium` to provision the Playwright-managed copy.",
		}
	}
	return preflightResult{Code: "PREFLIGHT_CHROMIUM", Level: checkMCPInfo, Message: "Chromium available", Detail: path}
}

// emitPreflightError renders the three-section guidance (what / why / how).
// JSON mode renders a JSONResponse with a sentinel error code.
func emitPreflightError(w io.Writer, r preflightResult) {
	if flagJSON {
		emitPreflightErrorJSON(w, r)
		return
	}

	if flagQuiet {
		// Quiet: only the one-line summary (Story §AC6).
		fmt.Fprintln(w, r.Message)
		return
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "[preflight] playwright-demo startup check failed")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "What: %s\n", r.Message)
	if r.Detail != "" {
		fmt.Fprintf(w, "Why:  %s\n", r.Detail)
	}
	fmt.Fprintln(w)

	switch r.Code {
	case "PREFLIGHT_NPX_MISSING":
		fmt.Fprintln(w, "How to fix (pick one for your OS):")
		fmt.Fprintln(w, "  Linux/macOS:  npm install -g npm    (npx ships with npm)")
		fmt.Fprintln(w, "  macOS:        brew install node     (node + npm together)")
		fmt.Fprintln(w, "  Any OS:       https://nodejs.org    (download installer >= 18)")
	case "PREFLIGHT_MCP_YAML_MISSING", "PREFLIGHT_MCP_YAML_PATH_UNRESOLVED":
		fmt.Fprintln(w, "How to fix:")
		fmt.Fprintln(w, "  rnix init --with-mcp-examples       # generates ~/.config/rnix/mcp.yaml")
	case "PREFLIGHT_MCP_YAML_UNREADABLE":
		fmt.Fprintln(w, "How to fix:")
		fmt.Fprintln(w, "  Check filesystem permissions on the mcp.yaml file.")
	default:
		if r.Hint != "" {
			fmt.Fprintln(w, "How to fix:")
			fmt.Fprintf(w, "  %s\n", r.Hint)
		}
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Verify install:")
	fmt.Fprintln(w, "  rnix check mcp                      # should report [INFO] node + [INFO] npx")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Retry:")
	fmt.Fprintln(w, "  rnix --agent playwright-demo --intent \"...\"")
}

func emitPreflightErrorJSON(w io.Writer, r preflightResult) {
	payload := map[string]any{
		"code":    r.Code,
		"message": r.Message,
		"hint":    r.Hint,
	}
	if r.Detail != "" {
		payload["detail"] = r.Detail
	}
	data, _ := json.Marshal(JSONResponse{OK: false, Error: payload})
	fmt.Fprintln(w, string(data))
}

func emitPreflightWarn(w io.Writer, r preflightResult) {
	if flagJSON {
		// JSON mode swallows WARN output (preflight only blocks on ERROR, so
		// the spawn JSON path stays clean).
		return
	}
	if flagQuiet {
		// Suppress warn detail; preserve at least the one-line summary so the
		// user knows why later runtime failures might happen.
		fmt.Fprintln(w, "[WARN] "+r.Message)
		return
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "[preflight] "+r.Message)
	if r.Hint != "" {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Suggestion: %s\n", r.Hint)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Continuing anyway (some intents may not require Chromium)...")
}

// resolvePreflightMcpYamlPath returns the mcp.yaml path the preflight check
// should examine. preflightMcpYamlPath override takes precedence; otherwise
// derive from globalDirForInit (test override) or config.GlobalDir.
func resolvePreflightMcpYamlPath() string {
	if preflightMcpYamlPath != "" {
		return preflightMcpYamlPath
	}
	// Honor init.go's globalDirForInit override (kept disjoint from
	// check.go's mcpConfigPathForCheck so preflight tests are isolated).
	if globalDirForInit != "" {
		return globalDirForInit + string(os.PathSeparator) + "mcp.yaml"
	}
	// Fall back to resolveMCPConfigPath which honors check.go's override
	// (no-op in normal CLI usage); this keeps a single source of truth for
	// the global mcp.yaml path.
	return resolveMCPConfigPath()
}
