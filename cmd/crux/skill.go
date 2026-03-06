package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/usecrux/crux/internal/ui"
	"github.com/usecrux/crux/skillpkg"
	"github.com/usecrux/crux/skills"
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
	Example: `  crux skill install code-analysis          # Install a single skill
  crux skill install pr-reviewer code-analyst  # Install multiple skills
  crux skill install code-analysis --force     # Force reinstall
  crux skill install code-analysis --json      # JSON output`,
	Args: cobra.MinimumNArgs(1),
	RunE: runSkillInstall,
}

var skillSearchCmd = &cobra.Command{
	Use:   "search <keyword>",
	Short: "Search skills in the community registry",
	Long:  "Search for skills in the community registry by keyword. Run without arguments to browse all available skills.",
	Example: `  crux skill search code          # Search for skills matching "code"
  crux skill search                # Browse all available skills
  crux skill search code --json    # JSON output`,
	Args: cobra.MaximumNArgs(1),
	RunE: runSkillSearch,
}

var skillUpdateCmd = &cobra.Command{
	Use:   "update [name...]",
	Short: "Update installed skills to the latest version",
	Long:  "Check for updates and update one or more installed skills from the community registry. Run without arguments to update all installed community skills.",
	Example: `  crux skill update code-analysis          # Update a single skill
  crux skill update code-analysis pr-reviewer  # Update multiple skills
  crux skill update                        # Update all installed skills
  crux skill update code-analysis --json   # JSON output`,
	Args: cobra.ArbitraryArgs,
	RunE: runSkillUpdate,
}

var skillListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all installed skills",
	Long:  "List all locally available skills, including system built-in skills and community-installed skills.",
	Example: `  crux skill list          # List all skills
  crux skill list --json   # JSON output
  crux skill list --quiet  # Only skill names`,
	Args: cobra.NoArgs,
	RunE: runSkillList,
}

var flagSkillForce bool

// skillRegistryURL can be overridden for testing.
var skillRegistryURL = skillpkg.DefaultRegistryURL

func init() {
	skillInstallCmd.Flags().BoolVar(&flagSkillForce, "force", false, "Force install even if already installed")
	skillCmd.AddCommand(skillInstallCmd)
	skillCmd.AddCommand(skillSearchCmd)
	skillCmd.AddCommand(skillUpdateCmd)
	skillCmd.AddCommand(skillListCmd)
}

func runSkillInstall(cmd *cobra.Command, args []string) error {
	mode := resolveOutputMode()
	renderer := ui.NewRenderer(os.Stdout, mode)
	ui.InitStyles(renderer.Profile)

	// Set up components
	client := skillpkg.NewRegistryClient(skillRegistryURL, nil)
	basePath := "lib/skills"
	registry := skillpkg.NewLocalRegistry(basePath)
	skillLoader := skills.NewSkillLoader(basePath)
	installer := skillpkg.NewInstaller(client, registry, skillLoader, basePath)

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
			var alreadyErr *skillpkg.AlreadyInstalledError
			if errors.As(err, &alreadyErr) {
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
			fmt.Fprintf(renderer.Writer, "%s %s %s v%s\n", prefix, action, result.Name, result.Version)
		}
	}

	if mode == ui.ModeJSON {
		renderSkillInstallJSON(renderer, results, installErrors)
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

type skillInstallJSONData struct {
	Installed []skillpkg.InstallResult `json:"installed"`
	Errors    []installErrorEntry      `json:"errors,omitempty"`
}

func renderSkillInstallJSON(r *ui.Renderer, results []skillpkg.InstallResult, errs []installErrorEntry) {
	if results == nil {
		results = []skillpkg.InstallResult{}
	}

	ok := len(errs) == 0
	resp := JSONResponse{
		OK:   ok,
		Data: skillInstallJSONData{Installed: results, Errors: errs},
	}
	data, _ := json.Marshal(resp)
	fmt.Fprintln(r.Writer, string(data))
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

	client := skillpkg.NewRegistryClient(skillRegistryURL, nil)
	basePath := "lib/skills"
	registry := skillpkg.NewLocalRegistry(basePath)
	skillLoader := skills.NewSkillLoader(basePath)
	installer := skillpkg.NewInstaller(client, registry, skillLoader, basePath)

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
			switch mode {
			case ui.ModeQuiet:
				if result.Updated {
					fmt.Fprintln(renderer.Writer, name)
				}
			case ui.ModeJSON:
				// JSON output handled after loop
			default:
				prefix := ui.KernelStyle.Render("[skill]")
				if result.Updated {
					fmt.Fprintf(renderer.Writer, "%s Updated %s v%s → v%s\n", prefix, result.Name, result.OldVersion, result.NewVersion)
				} else {
					fmt.Fprintf(renderer.Writer, "%s %s is already up to date (v%s).\n", prefix, result.Name, result.OldVersion)
				}
			}
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
			switch mode {
			case ui.ModeQuiet:
				if result.Updated {
					fmt.Fprintln(renderer.Writer, result.Name)
				}
			case ui.ModeJSON:
				// JSON output handled after loop
			default:
				prefix := ui.KernelStyle.Render("[skill]")
				if result.Updated {
					fmt.Fprintf(renderer.Writer, "%s Updated %s v%s → v%s\n", prefix, result.Name, result.OldVersion, result.NewVersion)
				} else {
					fmt.Fprintf(renderer.Writer, "%s %s is already up to date (v%s).\n", prefix, result.Name, result.OldVersion)
				}
			}
		}
	}

	if mode == ui.ModeJSON {
		renderSkillUpdateJSON(renderer, results, updateErrors)
	}

	if len(updateErrors) > 0 {
		exitCode = 1
	}

	return nil
}

type updateErrorEntry struct {
	Name    string `json:"name"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type skillUpdateJSONData struct {
	Results []skillpkg.UpdateResult `json:"results"`
	Errors  []updateErrorEntry      `json:"errors,omitempty"`
}

func renderSkillUpdateJSON(r *ui.Renderer, results []skillpkg.UpdateResult, errs []updateErrorEntry) {
	if results == nil {
		results = []skillpkg.UpdateResult{}
	}

	ok := len(errs) == 0
	resp := JSONResponse{
		OK:   ok,
		Data: skillUpdateJSONData{Results: results, Errors: errs},
	}
	data, _ := json.Marshal(resp)
	fmt.Fprintln(r.Writer, string(data))
}

// --- Story 8.4: skill list ---

func runSkillList(cmd *cobra.Command, args []string) error {
	mode := resolveOutputMode()
	renderer := ui.NewRenderer(os.Stdout, mode)
	ui.InitStyles(renderer.Profile)

	client := skillpkg.NewRegistryClient(skillRegistryURL, nil)
	basePath := "lib/skills"
	registry := skillpkg.NewLocalRegistry(basePath)
	skillLoader := skills.NewSkillLoader(basePath)
	installer := skillpkg.NewInstaller(client, registry, skillLoader, basePath)

	entries, err := installer.ListAll()
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

	switch mode {
	case ui.ModeJSON:
		renderSkillListJSON(renderer, entries)
	case ui.ModeQuiet:
		for _, e := range entries {
			fmt.Fprintln(renderer.Writer, e.Name)
		}
	default:
		prefix := ui.KernelStyle.Render("[skill]")
		fmt.Fprintf(renderer.Writer, "%s %-20s %-10s %-11s %s\n", prefix, "NAME", "VERSION", "SOURCE", "DESCRIPTION")
		for _, e := range entries {
			desc := e.Description
			if len([]rune(desc)) > 40 {
				desc = string([]rune(desc)[:37]) + "..."
			}
			fmt.Fprintf(renderer.Writer, "%s %-20s %-10s %-11s %s\n", prefix, e.Name, e.Version, e.Source, desc)
		}
		// Show tip if no community skills installed
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

	return nil
}

type skillListJSONData struct {
	Skills []skillpkg.ListEntry `json:"skills"`
}

func renderSkillListJSON(r *ui.Renderer, entries []skillpkg.ListEntry) {
	if entries == nil {
		entries = []skillpkg.ListEntry{}
	}
	resp := JSONResponse{
		OK:   true,
		Data: skillListJSONData{Skills: entries},
	}
	data, _ := json.Marshal(resp)
	fmt.Fprintln(r.Writer, string(data))
}
