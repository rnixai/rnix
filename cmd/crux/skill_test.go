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

// --- ATDD RED Phase: Story 8.2 — skill search 搜索 ---

// TestSkillSearchCmd_Registered verifies the search subcommand is registered under skill.
// AC #1: search 子命令注册
func TestSkillSearchCmd_Registered(t *testing.T) {
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
		if cmd.Name() == "search" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected 'search' subcommand under 'skill'")
	}
}

// TestSkillSearch_JSONOutput verifies JSON output format for search results.
// AC #3: JSON 输出格式，字段 snake_case
func TestSkillSearch_JSONOutput(t *testing.T) {
	ui.InitStyles(ui.TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &ui.Renderer{Writer: &buf, OutputMode: ui.ModeJSON, Profile: ui.TerminalProfile{ColorLevel: 0}}

	// Given: search results with known data
	results := []skillpkg.SearchResult{
		{Name: "code-analysis", Description: "Analyze code quality and patterns", Version: "1.0.0", Downloads: 1234},
		{Name: "pr-reviewer", Description: "Review pull requests with AI", Version: "2.1.0", Downloads: 5678},
	}
	renderSkillSearchJSON(r, results)

	// Then: valid JSON with snake_case fields
	var resp JSONResponse
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse JSON: %v\nraw: %s", err, buf.String())
	}
	if !resp.OK {
		t.Error("expected ok=true")
	}

	raw := buf.String()
	// Verify snake_case fields
	if !strings.Contains(raw, `"name"`) {
		t.Error("expected snake_case 'name' field in JSON")
	}
	if !strings.Contains(raw, `"description"`) {
		t.Error("expected snake_case 'description' field in JSON")
	}
	if !strings.Contains(raw, `"version"`) {
		t.Error("expected snake_case 'version' field in JSON")
	}
	if !strings.Contains(raw, `"downloads"`) {
		t.Error("expected snake_case 'downloads' field in JSON")
	}
	if !strings.Contains(raw, `"code-analysis"`) {
		t.Errorf("expected 'code-analysis' in JSON output, got %s", raw)
	}
}

// TestSkillSearch_EmptyResult_JSONOutput verifies JSON output for empty search results.
// AC #2, #3: 无结果时 JSON 返回空数组
func TestSkillSearch_EmptyResult_JSONOutput(t *testing.T) {
	ui.InitStyles(ui.TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &ui.Renderer{Writer: &buf, OutputMode: ui.ModeJSON, Profile: ui.TerminalProfile{ColorLevel: 0}}

	// Given: empty search results
	results := []skillpkg.SearchResult{}
	renderSkillSearchJSON(r, results)

	// Then: valid JSON with ok=true and empty results array
	var resp JSONResponse
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse JSON: %v\nraw: %s", err, buf.String())
	}
	if !resp.OK {
		t.Error("expected ok=true for empty results")
	}

	raw := buf.String()
	if !strings.Contains(raw, `"results":[]`) && !strings.Contains(raw, `"results": []`) {
		t.Errorf("expected empty results array in JSON, got %s", raw)
	}
}

// --- ATDD RED Phase: Story 8.3 — skill update 更新 ---

// TestSkillUpdateCmd_Registered verifies the update subcommand is registered under skill.
// AC #1: update 子命令注册
func TestSkillUpdateCmd_Registered(t *testing.T) {
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
		if cmd.Name() == "update" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected 'update' subcommand under 'skill'")
	}
}

// TestSkillUpdate_JSONOutput verifies JSON output format for update results.
// AC #1, #3: JSON 输出格式，字段 snake_case
func TestSkillUpdate_JSONOutput(t *testing.T) {
	ui.InitStyles(ui.TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &ui.Renderer{Writer: &buf, OutputMode: ui.ModeJSON, Profile: ui.TerminalProfile{ColorLevel: 0}}

	// Given: update results with known data
	results := []skillpkg.UpdateResult{
		{Name: "code-analysis", OldVersion: "1.0.0", NewVersion: "1.1.0", Updated: true},
	}
	renderSkillUpdateJSON(r, results, nil)

	// Then: valid JSON with snake_case fields
	var resp JSONResponse
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse JSON: %v\nraw: %s", err, buf.String())
	}
	if !resp.OK {
		t.Error("expected ok=true")
	}

	raw := buf.String()
	// Verify snake_case fields present
	if !strings.Contains(raw, `"name"`) {
		t.Error("expected snake_case 'name' field in JSON")
	}
	if !strings.Contains(raw, `"old_version"`) {
		t.Error("expected snake_case 'old_version' field in JSON")
	}
	if !strings.Contains(raw, `"new_version"`) {
		t.Error("expected snake_case 'new_version' field in JSON")
	}
	if !strings.Contains(raw, `"updated"`) {
		t.Error("expected snake_case 'updated' field in JSON")
	}
	if !strings.Contains(raw, `"code-analysis"`) {
		t.Errorf("expected 'code-analysis' in JSON output, got %s", raw)
	}
}

// TestSkillUpdate_EmptyResult_JSONOutput verifies JSON output for empty update results (all up to date).
// AC #2, #3: 全量更新无结果时 JSON 返回空数组
func TestSkillUpdate_EmptyResult_JSONOutput(t *testing.T) {
	ui.InitStyles(ui.TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &ui.Renderer{Writer: &buf, OutputMode: ui.ModeJSON, Profile: ui.TerminalProfile{ColorLevel: 0}}

	// Given: empty update results (all skills up to date)
	results := []skillpkg.UpdateResult{}
	renderSkillUpdateJSON(r, results, nil)

	// Then: valid JSON with ok=true and empty results array
	var resp JSONResponse
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse JSON: %v\nraw: %s", err, buf.String())
	}
	if !resp.OK {
		t.Error("expected ok=true for empty results")
	}

	raw := buf.String()
	if strings.Contains(raw, `"results":null`) {
		t.Error("expected results to be [] not null")
	}
}

// TestSkillUpdate_NoArgs_Accepted verifies that update command accepts zero arguments (update all).
// AC #2: 不指定名称时更新全部
func TestSkillUpdate_NoArgs_Accepted(t *testing.T) {
	// Given: skill update command with ArbitraryArgs
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

	var updateCmd *cobra.Command
	for _, cmd := range sc.Commands() {
		if cmd.Name() == "update" {
			updateCmd = cmd
			break
		}
	}
	if updateCmd == nil {
		t.Fatal("update command not found")
	}

	// Then: 0 args should be accepted (ArbitraryArgs)
	err := updateCmd.Args(updateCmd, []string{})
	if err != nil {
		t.Fatalf("expected 0 args to be accepted for update all, got error: %v", err)
	}
}

// TestSkillUpdate_WithArgs_Accepted verifies that update command accepts one or more arguments.
// AC #1: 指定名称时更新指定 skill
func TestSkillUpdate_WithArgs_Accepted(t *testing.T) {
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

	var updateCmd *cobra.Command
	for _, cmd := range sc.Commands() {
		if cmd.Name() == "update" {
			updateCmd = cmd
			break
		}
	}
	if updateCmd == nil {
		t.Fatal("update command not found")
	}

	// Then: 1+ args should be accepted
	err := updateCmd.Args(updateCmd, []string{"code-analysis"})
	if err != nil {
		t.Fatalf("expected 1 arg to be accepted, got error: %v", err)
	}

	err = updateCmd.Args(updateCmd, []string{"code-analysis", "pr-reviewer"})
	if err != nil {
		t.Fatalf("expected 2 args to be accepted, got error: %v", err)
	}
}

// TestSkillUpdate_MixedResults_JSONOutput verifies JSON output with both updated and errored results.
// AC #1, #2: 混合结果（更新成功 + 错误）
func TestSkillUpdate_MixedResults_JSONOutput(t *testing.T) {
	ui.InitStyles(ui.TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &ui.Renderer{Writer: &buf, OutputMode: ui.ModeJSON, Profile: ui.TerminalProfile{ColorLevel: 0}}

	results := []skillpkg.UpdateResult{
		{Name: "code-analysis", OldVersion: "1.0.0", NewVersion: "1.1.0", Updated: true},
	}
	errs := []updateErrorEntry{
		{Name: "nonexistent", Code: "NOT_INSTALLED", Message: `skill "nonexistent" is not installed`},
	}
	renderSkillUpdateJSON(r, results, errs)

	var resp JSONResponse
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse JSON: %v\nraw: %s", err, buf.String())
	}
	if resp.OK {
		t.Error("expected ok=false when there are errors")
	}

	raw := buf.String()
	if !strings.Contains(raw, `"code-analysis"`) {
		t.Errorf("expected code-analysis in output, got %s", raw)
	}
	if !strings.Contains(raw, `"nonexistent"`) {
		t.Errorf("expected nonexistent in output, got %s", raw)
	}
	if !strings.Contains(raw, `"NOT_INSTALLED"`) {
		t.Errorf("expected NOT_INSTALLED code in output, got %s", raw)
	}
}

// TestSkillSearch_NoArgs_BrowseAll verifies that running search with no args accepts it.
// AC #1: 无参数时浏览全部
func TestSkillSearch_NoArgs_BrowseAll(t *testing.T) {
	// Given: skill search command with MaximumNArgs(1)
	// When: no arguments provided
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

	var searchCmd *cobra.Command
	for _, cmd := range sc.Commands() {
		if cmd.Name() == "search" {
			searchCmd = cmd
			break
		}
	}
	if searchCmd == nil {
		t.Fatal("search command not found")
	}

	// Then: 0 args should be accepted (MaximumNArgs(1))
	err := searchCmd.Args(searchCmd, []string{})
	if err != nil {
		t.Fatalf("expected 0 args to be accepted for browse all, got error: %v", err)
	}
}

// --- ATDD RED Phase: Story 8.4 — skill list 本地 Skill 注册表 ---

// TestSkillListCmd_Registered verifies the list subcommand is registered under skill.
// AC #1: list 子命令注册
func TestSkillListCmd_Registered(t *testing.T) {
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
		if cmd.Name() == "list" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected 'list' subcommand under 'skill'")
	}
}

// TestSkillList_NoArgs_Required verifies that list command accepts zero arguments (cobra.NoArgs).
// AC #1: list 命令不接受参数
func TestSkillList_NoArgs_Required(t *testing.T) {
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

	var listCmd *cobra.Command
	for _, cmd := range sc.Commands() {
		if cmd.Name() == "list" {
			listCmd = cmd
			break
		}
	}
	if listCmd == nil {
		t.Fatal("list command not found")
	}

	// Then: 0 args should be accepted
	err := listCmd.Args(listCmd, []string{})
	if err != nil {
		t.Fatalf("expected 0 args to be accepted, got error: %v", err)
	}

	// Then: 1+ args should be rejected (cobra.NoArgs)
	err = listCmd.Args(listCmd, []string{"extra-arg"})
	if err == nil {
		t.Fatal("expected error for extra arguments with cobra.NoArgs")
	}
}

// TestSkillList_JSONOutput verifies JSON output format for skill list results.
// AC #3: JSON 输出 snake_case 字段
func TestSkillList_JSONOutput(t *testing.T) {
	ui.InitStyles(ui.TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &ui.Renderer{Writer: &buf, OutputMode: ui.ModeJSON, Profile: ui.TerminalProfile{ColorLevel: 0}}

	// Given: list entries with known data
	entries := []skillpkg.ListEntry{
		{Name: "code-analysis", Version: "1.0.0", Path: "lib/skills/code-analysis/", Description: "Analyze code quality and patterns", Source: "builtin"},
		{Name: "pr-reviewer", Version: "2.1.0", Path: "lib/skills/pr-reviewer/", Description: "Review pull requests with AI", Source: "community"},
	}
	renderSkillListJSON(r, entries)

	// Then: valid JSON with snake_case fields
	var resp JSONResponse
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse JSON: %v\nraw: %s", err, buf.String())
	}
	if !resp.OK {
		t.Error("expected ok=true")
	}

	raw := buf.String()
	// Verify snake_case fields present
	if !strings.Contains(raw, `"name"`) {
		t.Error("expected snake_case 'name' field in JSON")
	}
	if !strings.Contains(raw, `"version"`) {
		t.Error("expected snake_case 'version' field in JSON")
	}
	if !strings.Contains(raw, `"path"`) {
		t.Error("expected snake_case 'path' field in JSON")
	}
	if !strings.Contains(raw, `"description"`) {
		t.Error("expected snake_case 'description' field in JSON")
	}
	if !strings.Contains(raw, `"source"`) {
		t.Error("expected snake_case 'source' field in JSON")
	}
	if !strings.Contains(raw, `"code-analysis"`) {
		t.Errorf("expected 'code-analysis' in JSON output, got %s", raw)
	}
	if !strings.Contains(raw, `"pr-reviewer"`) {
		t.Errorf("expected 'pr-reviewer' in JSON output, got %s", raw)
	}
}

// TestSkillList_EmptyResult_JSONOutput verifies JSON output for empty skill list (no skills at all).
// AC #3: 空列表 JSON 返回空 skills 数组
func TestSkillList_EmptyResult_JSONOutput(t *testing.T) {
	ui.InitStyles(ui.TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &ui.Renderer{Writer: &buf, OutputMode: ui.ModeJSON, Profile: ui.TerminalProfile{ColorLevel: 0}}

	// Given: empty skill list
	entries := []skillpkg.ListEntry{}
	renderSkillListJSON(r, entries)

	// Then: valid JSON with ok=true and skills=[]
	var resp JSONResponse
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse JSON: %v\nraw: %s", err, buf.String())
	}
	if !resp.OK {
		t.Error("expected ok=true for empty results")
	}

	raw := buf.String()
	// Verify skills is empty array, not null
	if strings.Contains(raw, `"skills":null`) {
		t.Error("expected skills to be [] not null")
	}
}

// TestSkillList_NilEntries_JSONOutput verifies JSON output handles nil entries gracefully.
// AC #3: nil 入参时 JSON skills 为 [] 不为 null
func TestSkillList_NilEntries_JSONOutput(t *testing.T) {
	ui.InitStyles(ui.TerminalProfile{ColorLevel: 0})
	var buf bytes.Buffer
	r := &ui.Renderer{Writer: &buf, OutputMode: ui.ModeJSON, Profile: ui.TerminalProfile{ColorLevel: 0}}

	// Given: nil entries
	renderSkillListJSON(r, nil)

	// Then: valid JSON with skills=[] not null
	var resp JSONResponse
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse JSON: %v\nraw: %s", err, buf.String())
	}
	if !resp.OK {
		t.Error("expected ok=true")
	}

	raw := buf.String()
	if strings.Contains(raw, `"skills":null`) {
		t.Error("expected skills to be [] not null when entries is nil")
	}
}
