package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rnixai/rnix/internal/config"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/skillpkg"
	"github.com/rnixai/rnix/skills"
	"github.com/spf13/cobra"
)

var skillCmd = &cobra.Command{
	Use:   "skill",
	Short: "Manage skills",
	Long:  "Install, update, and manage skills from the community registry.",
}

var skillInstallCmd = &cobra.Command{
	Use:   "install <name> [name...]",
	Short: "Install skills from the community registry",
	Long:  "Download and install one or more skills from the community skill registry.",
	Example: `  rnix skill install code-analysis            # Install (project/native if inside .rnix/, else user/native)
  rnix skill install code-analysis -g         # Install to user scope (~/.config/rnix/skills/)
  rnix skill install code-analysis --shared   # Install to agents namespace (.agents/skills/)
  rnix skill install code-analysis --force    # Force reinstall
  rnix skill install code-analysis --json     # JSON output`,
	Args: cobra.MinimumNArgs(1),
	RunE: runSkillInstall,
}

var skillSearchCmd = &cobra.Command{
	Use:   "search <keyword>",
	Short: "Search skills in the community registry",
	Long:  "Search for skills in the community registry by keyword. Run without arguments to browse all available skills.",
	Example: `  rnix skill search code          # Search for skills matching "code"
  rnix skill search                # Browse all available skills
  rnix skill search code --json    # JSON output`,
	Args: cobra.MaximumNArgs(1),
	RunE: runSkillSearch,
}

var skillUpdateCmd = &cobra.Command{
	Use:   "update [name...]",
	Short: "Update installed skills to the latest version",
	Long:  "Check for updates and update one or more installed skills from the community registry. Update writes back to each skill's origin scope — never silently migrates across scopes. Run without arguments to update all installed community skills.",
	Example: `  rnix skill update code-analysis          # Update a single skill (writes back to origin scope)
  rnix skill update code-analysis pr-reviewer  # Update multiple skills
  rnix skill update                        # Update all installed skills
  rnix skill update code-analysis --json   # JSON output`,
	Args: cobra.ArbitraryArgs,
	RunE: runSkillUpdate,
}

var skillListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all installed skills",
	Long:  "List skills from all four scopes (project/native, project/agents, user/native, user/agents) with shadow resolution. Use -g/-p to filter by scope.",
	Example: `  rnix skill list           # List skills across all scopes
  rnix skill list -g        # Only user scope
  rnix skill list -p        # Only project scope
  rnix skill list --json    # JSON output (includes diagnostics)
  rnix skill list --quiet   # Only skill names`,
	Args: cobra.NoArgs,
	RunE: runSkillList,
}

var (
	flagSkillForce        bool
	flagSkillInstallGlobal bool
	flagSkillInstallShared bool
	flagSkillListGlobal    bool
	flagSkillListProject   bool
)

// skillRegistryURL can be overridden for testing.
var skillRegistryURL = skillpkg.DefaultRegistryURL

func init() {
	skillInstallCmd.Flags().BoolVar(&flagSkillForce, "force", false, "Force install even if already installed")
	skillInstallCmd.Flags().BoolVarP(&flagSkillInstallGlobal, "global", "g", false, "Install to user scope (~/.config/rnix/skills/ or ~/.agents/skills/) regardless of cwd")
	skillInstallCmd.Flags().BoolVar(&flagSkillInstallShared, "shared", false, "Install to agents namespace (.agents/skills/) for cross-tool interop with agentskills.io ecosystem")

	skillListCmd.Flags().BoolVarP(&flagSkillListGlobal, "global", "g", false, "Only show skills under user scope (~/.config/rnix/skills/ + ~/.agents/skills/)")
	skillListCmd.Flags().BoolVarP(&flagSkillListProject, "project", "p", false, "Only show skills under project scope (<projectDir>/.rnix/skills/ + <projectDir>/.agents/skills/)")
	skillListCmd.MarkFlagsMutuallyExclusive("global", "project")

	skillCmd.AddCommand(skillInstallCmd)
	skillCmd.AddCommand(skillSearchCmd)
	skillCmd.AddCommand(skillUpdateCmd)
	skillCmd.AddCommand(skillListCmd)
}

// resolveSkillCwd returns os.Getwd() with CLI-friendly error handling. Renders
// to the chosen output mode + sets exitCode and returns ok=false on failure.
func resolveSkillCwd(renderer *ui.Renderer, mode ui.OutputMode) (string, bool) {
	cwd, err := os.Getwd()
	if err == nil {
		return cwd, true
	}
	if mode == ui.ModeJSON {
		resp := JSONResponse{OK: false, Error: map[string]string{"code": "CWD_ERROR", "message": err.Error()}}
		data, _ := json.Marshal(resp)
		fmt.Fprintln(renderer.Writer, string(data))
	} else {
		prefix := ui.KernelStyle.Render("[skill]")
		fmt.Fprintf(renderer.Writer, "%s Failed to resolve current directory: %s\n", prefix, err.Error())
	}
	exitCode = 1
	return "", false
}

// resolveWriteScope decides where `skill install` writes a skill based on the
// (global, shared) flag combination and whether cwd is inside a .rnix/ project.
// See Story 47.3 AC2 decision matrix.
//
// Mkdir behavior: the returned ScopePath.Path is MkdirAll'd before return so
// downstream Installer.extract can safely create <path>/<skillName>/.
func resolveWriteScope(cwd string, global, shared bool) (config.ScopePath, error) {
	var (
		path  string
		scope config.SkillScope
		ns    config.SkillNamespace
	)

	inProject := false
	if cwd != "" {
		if proj, err := config.ProjectDir(cwd); err == nil && proj != "" {
			inProject = true
			// Resolve scope path based on namespace + project root.
			switch {
			case !global && !shared:
				path = filepath.Join(proj, ".rnix", "skills")
				scope = config.SkillScopeProject
				ns = config.SkillNamespaceRnix
			case !global && shared:
				path = filepath.Join(proj, ".agents", "skills")
				scope = config.SkillScopeProject
				ns = config.SkillNamespaceAgents
			}
		}
	}

	// Non-project paths + --global overrides land in user scope.
	if !inProject || global {
		switch {
		case !shared:
			globalDir, err := config.GlobalDir()
			if err != nil {
				return config.ScopePath{}, fmt.Errorf("resolve user config dir: %w", err)
			}
			path = filepath.Join(globalDir, "skills")
			scope = config.SkillScopeUser
			ns = config.SkillNamespaceRnix
		case shared:
			home, err := userHomeDirSkill()
			if err != nil {
				return config.ScopePath{}, fmt.Errorf("resolve home dir: %w", err)
			}
			path = filepath.Join(home, ".agents", "skills")
			scope = config.SkillScopeUser
			ns = config.SkillNamespaceAgents
		}
	}

	if err := os.MkdirAll(path, 0o755); err != nil {
		return config.ScopePath{}, fmt.Errorf("create scope dir %q: %w", path, err)
	}

	return config.ScopePath{Path: path, Scope: scope, Namespace: ns}, nil
}

// userHomeDirSkill mirrors config.userHomeDir behavior (HOME first, then
// os.UserHomeDir fallback) since the helper is unexported in internal/config.
func userHomeDirSkill() (string, error) {
	if h := os.Getenv("HOME"); h != "" {
		return h, nil
	}
	return os.UserHomeDir()
}

// filterScopes keeps only scopes whose .Scope matches target, preserving the
// caller's priority order. Used by `skill list -g` / `skill list -p`.
func filterScopes(scopes []config.ScopePath, target config.SkillScope) []config.ScopePath {
	out := make([]config.ScopePath, 0, len(scopes))
	for _, sp := range scopes {
		if sp.Scope == target {
			out = append(out, sp)
		}
	}
	return out
}

// scopeDiagnostic describes one of the four canonical skill scope paths with a
// human-readable status used in the empty-list diagnostic block.
type scopeDiagnostic struct {
	Path   string
	Status string // "not-found" or "existed-but-empty"
}

// diagnosticScopePaths returns the four canonical agentskills.io scope paths
// with a status string for each: "not-found" when the directory does not
// exist, "existed-but-empty" when it exists but contains no skill subdirectories.
// Filter selects which scopes to include (nil = all four).
func diagnosticScopePaths(cwd string, scopeFilter *config.SkillScope) []scopeDiagnostic {
	type candidate struct {
		path  string
		scope config.SkillScope
	}
	var candidates []candidate

	if cwd != "" {
		if abs, err := filepath.Abs(cwd); err == nil {
			candidates = append(candidates,
				candidate{path: filepath.Join(abs, ".rnix", "skills"), scope: config.SkillScopeProject},
				candidate{path: filepath.Join(abs, ".agents", "skills"), scope: config.SkillScopeProject},
			)
		}
	}
	if globalDir, err := config.GlobalDir(); err == nil {
		candidates = append(candidates, candidate{path: filepath.Join(globalDir, "skills"), scope: config.SkillScopeUser})
	}
	if home, err := userHomeDirSkill(); err == nil {
		candidates = append(candidates, candidate{path: filepath.Join(home, ".agents", "skills"), scope: config.SkillScopeUser})
	}

	out := make([]scopeDiagnostic, 0, len(candidates))
	for _, c := range candidates {
		if scopeFilter != nil && c.scope != *scopeFilter {
			continue
		}
		out = append(out, scopeDiagnostic{Path: c.path, Status: pathStatus(c.path)})
	}
	return out
}

// pathStatus inspects a single skill root directory and returns one of:
// "not-found" — the directory does not exist (ENOENT / not-a-dir)
// "existed-but-empty" — directory exists but has no non-dotfile subdirectory
// "existed-non-empty" — directory has at least one candidate skill entry
//
// Only "not-found" and "existed-but-empty" feed the diagnostic UI; existing
// non-empty paths contribute to the main table instead and are filtered out.
func pathStatus(path string) string {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return "not-found"
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return "existed-but-empty"
	}
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			return "existed-non-empty"
		}
	}
	return "existed-but-empty"
}

// renderDiagnosticsToStderr writes shadow/skipped/lenient/trust diagnostics
// collected by Installer.ListAll (or a stand-alone trust pre-check) to
// os.Stderr (one line per entry). Called only in non-JSON modes — JSON mode
// embeds the same data inside the top-level diagnostics node.
//
// Render order: Warnings → Skipped → Lenient → Trust. Story 47.4 places Trust
// last because it is a project-level advisory, not per-skill noise.
func renderDiagnosticsToStderr(diag skillpkg.LoadDiagnostics) {
	for _, w := range diag.Warnings {
		fmt.Fprintf(os.Stderr, "[skill] warning: shadowed skill %q: winner=%s (%s/%s); shadowed=%s (%s/%s)\n",
			w.SkillName, w.WinningPath, w.WinningScope, w.WinningNS, w.ShadowedPath, w.ShadowedScope, w.ShadowedNS)
	}
	for _, s := range diag.Skipped {
		fmt.Fprintf(os.Stderr, "[skill] warning: skipped skill at %s: %s\n", s.Path, s.Reason)
	}
	for _, l := range diag.Lenient {
		fmt.Fprintf(os.Stderr, "[skill] warning: %s: %s: %s (%s)\n", l.Path, l.Field, l.Reason, l.Detail)
	}
	for _, t := range diag.Trust {
		fmt.Fprintf(os.Stderr,
			"[skill] warning: untrusted project %s: %d skill root(s) will load — %s. Policy: %s. %s\n",
			t.ProjectDir, len(t.SkillsRootPaths), t.Reason, t.Policy, t.Recommendation)
	}
}

// resolveSkillScopesAndPaths runs ResolveSkillScopes(cwd) and converts the
// ScopePath slice into a plain []string for passing to NewSkillLoader. The
// loader expects path strings; the installer keeps full ScopePath metadata.
func resolveSkillScopesAndPaths(cwd string) ([]config.ScopePath, []string) {
	scopes := config.ResolveSkillScopes(cwd)
	paths := make([]string, len(scopes))
	for i, sp := range scopes {
		paths[i] = sp.Path
	}
	return scopes, paths
}

func runSkillInstall(cmd *cobra.Command, args []string) error {
	mode := resolveOutputMode()
	renderer := ui.NewRenderer(os.Stdout, mode)
	ui.InitStyles(renderer.Profile)

	cwd, ok := resolveSkillCwd(renderer, mode)
	if !ok {
		return nil
	}

	writeScope, err := resolveWriteScope(cwd, flagSkillInstallGlobal, flagSkillInstallShared)
	if err != nil {
		if mode == ui.ModeJSON {
			resp := JSONResponse{OK: false, Error: map[string]string{"code": "WRITE_SCOPE_ERROR", "message": err.Error()}}
			data, _ := json.Marshal(resp)
			fmt.Fprintln(renderer.Writer, string(data))
		} else {
			prefix := ui.KernelStyle.Render("[skill]")
			fmt.Fprintf(renderer.Writer, "%s Failed to resolve write target: %s\n", prefix, err.Error())
		}
		exitCode = 1
		return nil
	}

	scopes, paths := resolveSkillScopesAndPaths(cwd)
	// Ensure writeScope's loader path is visible to SkillLoader (so validate
	// after extract can locate the newly-installed skill).
	if writeScope.Path != "" && !slices.Contains(paths, writeScope.Path) {
		paths = append(paths, writeScope.Path)
	}

	client := skillpkg.NewRegistryClient(skillRegistryURL, nil)
	skillLoader := skills.NewSkillLoader(paths)
	installer := skillpkg.NewInstaller(client, skillLoader, scopes, writeScope)

	// Story 47.4 AC9: emit trust advisories exactly once per CLI invocation
	// (outside the install for-loop so multiple args do not duplicate).
	trustWarnings := skillpkg.CheckProjectTrust(scopes)
	if mode != ui.ModeJSON && len(trustWarnings) > 0 {
		renderDiagnosticsToStderr(skillpkg.LoadDiagnostics{Trust: trustWarnings})
	}

	var results []skillpkg.InstallResult
	var installErrors []installErrorEntry

	for _, name := range args {
		opts := skillpkg.InstallOpts{Force: flagSkillForce}

		if mode != ui.ModeJSON && mode != ui.ModeQuiet {
			prefix := ui.KernelStyle.Render("[skill]")
			fmt.Fprintf(renderer.Writer, "%s Installing %s...\n", prefix, name)
		}

		result, err := installer.Install(name, opts)
		if err != nil {
			if alreadyErr, ok := errors.AsType[*skillpkg.AlreadyInstalledError](err); ok {
				installErrors = append(installErrors, installErrorEntry{
					Name:    name,
					Code:    "ALREADY_INSTALLED",
					Message: alreadyErr.Error(),
				})
				if mode != ui.ModeJSON && mode != ui.ModeQuiet {
					prefix := ui.KernelStyle.Render("[skill]")
					fmt.Fprintf(renderer.Writer, "%s %s\n", prefix, alreadyErr.Error())
				}
			} else {
				installErrors = append(installErrors, installErrorEntry{
					Name:    name,
					Code:    "INSTALL_ERROR",
					Message: err.Error(),
				})
				if mode != ui.ModeJSON && mode != ui.ModeQuiet {
					prefix := ui.KernelStyle.Render("[skill]")
					fmt.Fprintf(renderer.Writer, "%s Failed to install %s: %s\n", prefix, name, err.Error())
				}
			}
			continue
		}

		results = append(results, *result)

		switch mode {
		case ui.ModeQuiet:
			fmt.Fprintln(renderer.Writer, name)
		case ui.ModeJSON:
			// JSON output handled after loop
		default:
			prefix := ui.KernelStyle.Render("[skill]")
			action := "Installed"
			if !result.Fresh {
				action = "Reinstalled"
			}
			fmt.Fprintf(renderer.Writer, "%s %s %s v%s (%s/%s: %s)\n",
				prefix, action, result.Name, result.Version,
				result.Scope, result.Namespace, result.Path)
		}
	}

	if mode == ui.ModeJSON {
		renderSkillInstallJSON(renderer, results, installErrors, skillpkg.LoadDiagnostics{Trust: trustWarnings})
	}

	if len(installErrors) > 0 {
		exitCode = 1
	}

	return nil
}

type installErrorEntry struct {
	Name    string `json:"name"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// skillInstallJSONData carries the wire shape of `rnix skill install --json`.
// Story 47.4 AC10: Diagnostics is a pointer + omitempty so the 47.3 wire
// shape stays byte-identical (no `diagnostics` key at all) when no
// trust/lenient/shadow/skipped advisories surfaced — encoding/json's
// omitempty does not elide zero-value structs, so a pointer is required.
type skillInstallJSONData struct {
	Installed   []skillpkg.InstallResult  `json:"installed"`
	Errors      []installErrorEntry       `json:"errors,omitempty"`
	Diagnostics *skillpkg.LoadDiagnostics `json:"diagnostics,omitempty"`
}

func renderSkillInstallJSON(r *ui.Renderer, results []skillpkg.InstallResult, errs []installErrorEntry, diag skillpkg.LoadDiagnostics) {
	if results == nil {
		results = []skillpkg.InstallResult{}
	}

	data := skillInstallJSONData{Installed: results, Errors: errs}
	if !diagIsEmpty(diag) {
		d := diag
		data.Diagnostics = &d
	}

	ok := len(errs) == 0
	resp := JSONResponse{OK: ok, Data: data}
	raw, _ := json.Marshal(resp)
	fmt.Fprintln(r.Writer, string(raw))
}

// diagIsEmpty reports whether every diagnostic channel is empty.
// LoadDiagnostics is a struct, so encoding/json's `omitempty` cannot elide it
// automatically; this helper drives the pointer assignment in install/update
// JSON renderers (Story 47.4 AC10).
func diagIsEmpty(d skillpkg.LoadDiagnostics) bool {
	return len(d.Warnings) == 0 && len(d.Skipped) == 0 && len(d.Lenient) == 0 && len(d.Trust) == 0
}

// --- Story 8.2: skill search ---

func runSkillSearch(cmd *cobra.Command, args []string) error {
	mode := resolveOutputMode()
	renderer := ui.NewRenderer(os.Stdout, mode)
	ui.InitStyles(renderer.Profile)

	client := skillpkg.NewRegistryClient(skillRegistryURL, nil)

	keyword := ""
	if len(args) > 0 {
		keyword = args[0]
	}

	results, err := client.Search(keyword)
	if err != nil {
		if mode == ui.ModeJSON {
			resp := JSONResponse{OK: false, Error: map[string]string{"code": "SEARCH_ERROR", "message": err.Error()}}
			data, _ := json.Marshal(resp)
			fmt.Fprintln(renderer.Writer, string(data))
		} else {
			prefix := ui.KernelStyle.Render("[skill]")
			fmt.Fprintf(renderer.Writer, "%s Failed to search: %s\n", prefix, err.Error())
		}
		exitCode = 1
		return nil
	}

	switch mode {
	case ui.ModeJSON:
		renderSkillSearchJSON(renderer, results)
	case ui.ModeQuiet:
		for _, r := range results {
			fmt.Fprintln(renderer.Writer, r.Name)
		}
	default:
		if len(results) == 0 {
			prefix := ui.KernelStyle.Render("[skill]")
			fmt.Fprintf(renderer.Writer, "%s No skills found for %q.\n", prefix, keyword)
			fmt.Fprintf(renderer.Writer, "%s Tip: Check spelling or run 'skill search' to browse all skills.\n", prefix)
			return nil
		}
		prefix := ui.KernelStyle.Render("[skill]")
		fmt.Fprintf(renderer.Writer, "%s %-20s %-40s %-10s %s\n", prefix, "NAME", "DESCRIPTION", "VERSION", "DOWNLOADS")
		for _, r := range results {
			desc := r.Description
			if len([]rune(desc)) > 40 {
				desc = string([]rune(desc)[:37]) + "..."
			}
			fmt.Fprintf(renderer.Writer, "%s %-20s %-40s %-10s %d\n", prefix, r.Name, desc, r.Version, r.Downloads)
		}
	}

	return nil
}

type searchJSONData struct {
	Results []skillpkg.SearchResult `json:"results"`
}

// renderSkillSearchJSON renders search results as JSON.
func renderSkillSearchJSON(r *ui.Renderer, results []skillpkg.SearchResult) {
	if results == nil {
		results = []skillpkg.SearchResult{}
	}
	resp := JSONResponse{
		OK:   true,
		Data: searchJSONData{Results: results},
	}
	data, _ := json.Marshal(resp)
	fmt.Fprintln(r.Writer, string(data))
}

// --- Story 8.3: skill update ---

func runSkillUpdate(cmd *cobra.Command, args []string) error {
	mode := resolveOutputMode()
	renderer := ui.NewRenderer(os.Stdout, mode)
	ui.InitStyles(renderer.Profile)

	cwd, ok := resolveSkillCwd(renderer, mode)
	if !ok {
		return nil
	}

	scopes, paths := resolveSkillScopesAndPaths(cwd)
	// Update pivots writeScope internally to each skill's origin scope; the
	// initial value here is only a safe default and never used in practice.
	var defaultWrite config.ScopePath
	if len(scopes) > 0 {
		defaultWrite = scopes[0]
	}

	client := skillpkg.NewRegistryClient(skillRegistryURL, nil)
	skillLoader := skills.NewSkillLoader(paths)
	installer := skillpkg.NewInstaller(client, skillLoader, scopes, defaultWrite)

	// Story 47.4 AC9: emit trust advisories once per CLI invocation (outside
	// the per-skill loop) so multi-arg / UpdateAll do not duplicate.
	trustWarnings := skillpkg.CheckProjectTrust(scopes)
	if mode != ui.ModeJSON && len(trustWarnings) > 0 {
		renderDiagnosticsToStderr(skillpkg.LoadDiagnostics{Trust: trustWarnings})
	}

	var results []skillpkg.UpdateResult
	var updateErrors []updateErrorEntry

	if len(args) > 0 {
		// Update specific skills
		for _, name := range args {
			if mode != ui.ModeJSON && mode != ui.ModeQuiet {
				prefix := ui.KernelStyle.Render("[skill]")
				fmt.Fprintf(renderer.Writer, "%s Checking %s...\n", prefix, name)
			}

			result, err := installer.Update(name, skillpkg.UpdateOpts{})
			if err != nil {
				code := "UPDATE_ERROR"
				if strings.Contains(err.Error(), "is not installed") {
					code = "NOT_INSTALLED"
				}
				updateErrors = append(updateErrors, updateErrorEntry{
					Name:    name,
					Code:    code,
					Message: err.Error(),
				})
				if mode != ui.ModeJSON && mode != ui.ModeQuiet {
					prefix := ui.KernelStyle.Render("[skill]")
					fmt.Fprintf(renderer.Writer, "%s Error: %s\n", prefix, err.Error())
				}
				continue
			}

			results = append(results, *result)
			renderUpdateLine(renderer, mode, *result)
		}
	} else {
		// Update all installed community skills
		allResults, err := installer.UpdateAll(skillpkg.UpdateOpts{})
		if err != nil {
			if mode == ui.ModeJSON {
				resp := JSONResponse{OK: false, Error: map[string]string{"code": "UPDATE_ERROR", "message": err.Error()}}
				data, _ := json.Marshal(resp)
				fmt.Fprintln(renderer.Writer, string(data))
			} else {
				prefix := ui.KernelStyle.Render("[skill]")
				fmt.Fprintf(renderer.Writer, "%s Failed to update: %s\n", prefix, err.Error())
			}
			exitCode = 1
			return nil
		}
		results = allResults

		for _, result := range results {
			renderUpdateLine(renderer, mode, result)
		}
	}

	if mode == ui.ModeJSON {
		renderSkillUpdateJSON(renderer, results, updateErrors, skillpkg.LoadDiagnostics{Trust: trustWarnings})
	}

	if len(updateErrors) > 0 {
		exitCode = 1
	}

	return nil
}

// renderUpdateLine prints one update result line in non-JSON modes, appending
// (<scope>/<ns>: <path>) metadata when present.
func renderUpdateLine(renderer *ui.Renderer, mode ui.OutputMode, result skillpkg.UpdateResult) {
	switch mode {
	case ui.ModeQuiet:
		if result.Updated {
			fmt.Fprintln(renderer.Writer, result.Name)
		}
	case ui.ModeJSON:
		// JSON handled after loop
	default:
		prefix := ui.KernelStyle.Render("[skill]")
		meta := ""
		if result.Scope != "" || result.Namespace != "" || result.Path != "" {
			meta = fmt.Sprintf(" (%s/%s: %s)", result.Scope, result.Namespace, result.Path)
		}
		if result.Updated {
			fmt.Fprintf(renderer.Writer, "%s Updated %s v%s → v%s%s\n",
				prefix, result.Name, result.OldVersion, result.NewVersion, meta)
		} else {
			fmt.Fprintf(renderer.Writer, "%s %s is already up to date (v%s)%s.\n",
				prefix, result.Name, result.OldVersion, meta)
		}
	}
}

type updateErrorEntry struct {
	Name    string `json:"name"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// skillUpdateJSONData carries the wire shape of `rnix skill update --json`.
// Story 47.4 AC10: Diagnostics is a pointer + omitempty so trusted-project
// updates emit the same 47.3 wire shape (no diagnostics key).
type skillUpdateJSONData struct {
	Results     []skillpkg.UpdateResult   `json:"results"`
	Errors      []updateErrorEntry        `json:"errors,omitempty"`
	Diagnostics *skillpkg.LoadDiagnostics `json:"diagnostics,omitempty"`
}

func renderSkillUpdateJSON(r *ui.Renderer, results []skillpkg.UpdateResult, errs []updateErrorEntry, diag skillpkg.LoadDiagnostics) {
	if results == nil {
		results = []skillpkg.UpdateResult{}
	}

	data := skillUpdateJSONData{Results: results, Errors: errs}
	if !diagIsEmpty(diag) {
		d := diag
		data.Diagnostics = &d
	}

	ok := len(errs) == 0
	resp := JSONResponse{OK: ok, Data: data}
	raw, _ := json.Marshal(resp)
	fmt.Fprintln(r.Writer, string(raw))
}

// --- Story 8.4 + 47.3: skill list ---

func runSkillList(cmd *cobra.Command, args []string) error {
	mode := resolveOutputMode()
	renderer := ui.NewRenderer(os.Stdout, mode)
	ui.InitStyles(renderer.Profile)

	cwd, ok := resolveSkillCwd(renderer, mode)
	if !ok {
		return nil
	}

	scopes, paths := resolveSkillScopesAndPaths(cwd)

	// Apply -g / -p filter to scope slice BEFORE handing to Installer so the
	// shadow dedup logic sees only the requested subset.
	var scopeFilter *config.SkillScope
	switch {
	case flagSkillListGlobal:
		s := config.SkillScopeUser
		scopeFilter = &s
		scopes = filterScopes(scopes, config.SkillScopeUser)
	case flagSkillListProject:
		s := config.SkillScopeProject
		scopeFilter = &s
		scopes = filterScopes(scopes, config.SkillScopeProject)
	}

	client := skillpkg.NewRegistryClient(skillRegistryURL, nil)
	skillLoader := skills.NewSkillLoader(paths)

	// When scopes is empty (no existing dirs, or filtered to nothing), do not
	// invoke Installer.ListAll — it would return errNoScopes which we treat as
	// "0 entries + empty diagnostics" instead of a fatal error.
	var (
		entries []skillpkg.ListEntry
		diag    skillpkg.LoadDiagnostics
	)
	if len(scopes) > 0 {
		// writeScope unused in ListAll path; pass zero value.
		installer := skillpkg.NewInstaller(client, skillLoader, scopes, config.ScopePath{})
		var err error
		entries, diag, err = installer.ListAll()
		if err != nil {
			if mode == ui.ModeJSON {
				resp := JSONResponse{OK: false, Error: map[string]string{"code": "LIST_ERROR", "message": err.Error()}}
				data, _ := json.Marshal(resp)
				fmt.Fprintln(renderer.Writer, string(data))
			} else {
				prefix := ui.KernelStyle.Render("[skill]")
				fmt.Fprintf(renderer.Writer, "%s Failed to list skills: %s\n", prefix, err.Error())
			}
			exitCode = 1
			return nil
		}
	}

	switch mode {
	case ui.ModeJSON:
		renderSkillListJSON(renderer, entries, diag)
	case ui.ModeQuiet:
		for _, e := range entries {
			fmt.Fprintln(renderer.Writer, e.Name)
		}
	default:
		prefix := ui.KernelStyle.Render("[skill]")
		fmt.Fprintf(renderer.Writer, "%s %-20s %-10s %-11s %-9s %-10s %s\n",
			prefix, "NAME", "VERSION", "SOURCE", "SCOPE", "NAMESPACE", "DESCRIPTION")
		for _, e := range entries {
			desc := e.Description
			if len([]rune(desc)) > 40 {
				desc = string([]rune(desc)[:37]) + "..."
			}
			fmt.Fprintf(renderer.Writer, "%s %-20s %-10s %-11s %-9s %-10s %s\n",
				prefix, e.Name, e.Version, e.Source, e.Scope, e.Namespace, desc)
		}

		if len(entries) == 0 {
			// AC6: print "Scanned paths" diagnostic — restricted to the filter
			// active in this invocation (so `-g` produces 2 lines, no flag = 4).
			fmt.Fprintf(renderer.Writer, "%s No skills found. Scanned paths:\n", prefix)
			for _, d := range diagnosticScopePaths(cwd, scopeFilter) {
				fmt.Fprintf(renderer.Writer, "%s   - %s (%s)\n", prefix, d.Path, d.Status)
			}
			fmt.Fprintf(renderer.Writer, "%s Tip: skill search <keyword> to discover more skills.\n", prefix)
		} else {
			// Tip when no community skill present (existing Epic 8 behavior).
			hasCommunity := false
			for _, e := range entries {
				if e.Source == "community" {
					hasCommunity = true
					break
				}
			}
			if !hasCommunity {
				fmt.Fprintf(renderer.Writer, "%s Tip: skill search <keyword> to discover more skills.\n", prefix)
			}
		}
	}

	// AC8: lenient/shadow/skipped warnings → stderr in non-JSON modes only.
	// JSON mode embeds the same data inside the diagnostics node (avoid
	// double-signal to consumers parsing stdout JSON).
	if mode != ui.ModeJSON {
		renderDiagnosticsToStderr(diag)
	}

	return nil
}

// skillListJSONData carries the wire shape of `rnix skill list --json`.
// Story 47.3 AC9: the top-level `diagnostics` node is always present (empty
// LoadDiagnostics{} when no warnings) so consumers always parse the same shape.
type skillListJSONData struct {
	Skills      []skillpkg.ListEntry    `json:"skills"`
	Diagnostics skillpkg.LoadDiagnostics `json:"diagnostics"`
}

func renderSkillListJSON(r *ui.Renderer, entries []skillpkg.ListEntry, diag skillpkg.LoadDiagnostics) {
	if entries == nil {
		entries = []skillpkg.ListEntry{}
	}
	resp := JSONResponse{
		OK:   true,
		Data: skillListJSONData{Skills: entries, Diagnostics: diag},
	}
	data, _ := json.Marshal(resp)
	fmt.Fprintln(r.Writer, string(data))
}
