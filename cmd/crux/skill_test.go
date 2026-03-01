package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gonewx/crux/internal/ui"
	"github.com/gonewx/crux/skillpkg"
	"github.com/spf13/cobra"
)

// --- AC #2: skill install sub-command registration ---

func TestSkillCmd_Registered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "skill" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected 'skill' subcommand registered on rootCmd")
	}
}

func TestSkillInstallCmd_Registered(t *testing.T) {
	var sc *cobra.Command
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "skill" {
			sc = cmd
			break
		}
	}
	if sc == nil {
		t.Fatal("skill command not found")
	}

	found := false
	for _, cmd := range sc.Commands() {
		if cmd.Name() == "install" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected 'install' subcommand under 'skill'")
	}
}

// --- AC #2: single skill install CLI ---

func TestSkillInstall_SingleSkill_JSONOutput(t *testing.T) {
	// This tests the JSON rendering logic
	ui.InitStyles(ui.TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &ui.Renderer{Writer: &buf, OutputMode: ui.ModeJSON, Profile: ui.TerminalProfile{ColorLevel: 0}}

	results := []skillpkg.InstallResult{
		{Name: "code-analysis", Version: "1.0.0", Fresh: true},
	}
	renderSkillInstallJSON(r, results, nil)

	var resp JSONResponse
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse JSON: %v\nraw: %s", err, buf.String())
	}
	if !resp.OK {
		t.Error("expected ok=true")
	}

	raw := buf.String()
	if !strings.Contains(raw, `"code-analysis"`) {
		t.Errorf("expected skill name in JSON output, got %s", raw)
	}
	if !strings.Contains(raw, `"1.0.0"`) {
		t.Errorf("expected version in JSON output, got %s", raw)
	}
}

// --- AC #3: batch install ---

func TestSkillInstall_BatchInstall_JSONOutput(t *testing.T) {
	ui.InitStyles(ui.TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &ui.Renderer{Writer: &buf, OutputMode: ui.ModeJSON, Profile: ui.TerminalProfile{ColorLevel: 0}}

	results := []skillpkg.InstallResult{
		{Name: "pr-reviewer", Version: "1.0.0", Fresh: true},
		{Name: "code-analyst", Version: "2.0.0", Fresh: true},
	}
	renderSkillInstallJSON(r, results, nil)

	var resp JSONResponse
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse JSON: %v\nraw: %s", err, buf.String())
	}
	if !resp.OK {
		t.Error("expected ok=true")
	}

	raw := buf.String()
	if !strings.Contains(raw, `"pr-reviewer"`) {
		t.Errorf("expected pr-reviewer in output, got %s", raw)
	}
	if !strings.Contains(raw, `"code-analyst"`) {
		t.Errorf("expected code-analyst in output, got %s", raw)
	}
}

// --- AC #4: already installed prompt ---

func TestSkillInstall_AlreadyInstalled_JSONOutput(t *testing.T) {
	ui.InitStyles(ui.TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &ui.Renderer{Writer: &buf, OutputMode: ui.ModeJSON, Profile: ui.TerminalProfile{ColorLevel: 0}}

	errs := []installErrorEntry{
		{Name: "code-analysis", Code: "ALREADY_INSTALLED", Message: "already installed v1.0.0"},
	}
	renderSkillInstallJSON(r, nil, errs)

	var resp JSONResponse
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse JSON: %v\nraw: %s", err, buf.String())
	}
	if resp.OK {
		t.Error("expected ok=false for error case")
	}
}

// --- Error Cases ---

func TestSkillInstall_NoArgs(t *testing.T) {
	// cobra.MinimumNArgs(1) should reject 0 args
	err := skillInstallCmd.Args(skillInstallCmd, []string{})
	if err == nil {
		t.Fatal("expected error for no args")
	}
}

// --- Flags ---

func TestSkillInstall_Flags_Force(t *testing.T) {
	var sc *cobra.Command
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "skill" {
			sc = cmd
			break
		}
	}
	if sc == nil {
		t.Fatal("skill command not found")
	}

	var installCmd *cobra.Command
	for _, cmd := range sc.Commands() {
		if cmd.Name() == "install" {
			installCmd = cmd
			break
		}
	}
	if installCmd == nil {
		t.Fatal("install command not found")
	}

	flag := installCmd.Flags().Lookup("force")
	if flag == nil {
		t.Fatal("--force flag not registered on install command")
	}
}

func TestSkillInstall_Flags_JSON(t *testing.T) {
	// JSON flag is global/persistent, verified through rootCmd
	flag := rootCmd.PersistentFlags().Lookup("json")
	if flag == nil {
		t.Fatal("--json persistent flag not registered on rootCmd")
	}
}

// --- renderSkillInstallJSON edge cases ---

func TestRenderSkillInstallJSON_EmptyResults(t *testing.T) {
	ui.InitStyles(ui.TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &ui.Renderer{Writer: &buf, OutputMode: ui.ModeJSON, Profile: ui.TerminalProfile{ColorLevel: 0}}

	renderSkillInstallJSON(r, nil, nil)

	var resp JSONResponse
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse JSON: %v\nraw: %s", err, buf.String())
	}
	if !resp.OK {
		t.Error("expected ok=true for empty results with no errors")
	}
	// Verify installed is empty array, not null
	raw := buf.String()
	if strings.Contains(raw, `"installed":null`) {
		t.Error("expected installed to be [] not null")
	}
}

func TestRenderSkillInstallJSON_MixedResults(t *testing.T) {
	ui.InitStyles(ui.TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &ui.Renderer{Writer: &buf, OutputMode: ui.ModeJSON, Profile: ui.TerminalProfile{ColorLevel: 0}}

	results := []skillpkg.InstallResult{
		{Name: "success-skill", Version: "1.0.0", Fresh: true},
	}
	errs := []installErrorEntry{
		{Name: "fail-skill", Code: "INSTALL_ERROR", Message: "network error"},
	}
	renderSkillInstallJSON(r, results, errs)

	var resp JSONResponse
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse JSON: %v\nraw: %s", err, buf.String())
	}
	if resp.OK {
		t.Error("expected ok=false when there are errors")
	}

	raw := buf.String()
	if !strings.Contains(raw, `"success-skill"`) {
		t.Errorf("expected success-skill in output, got %s", raw)
	}
	if !strings.Contains(raw, `"fail-skill"`) {
		t.Errorf("expected fail-skill in output, got %s", raw)
	}
}
