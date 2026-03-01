package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/gonewx/crux/internal/ui"
	"github.com/gonewx/crux/skills"
	"github.com/gonewx/crux/skillpkg"
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
	Example: `  crux skill install code-analysis          # Install a single skill
  crux skill install pr-reviewer code-analyst  # Install multiple skills
  crux skill install code-analysis --force     # Force reinstall
  crux skill install code-analysis --json      # JSON output`,
	Args: cobra.MinimumNArgs(1),
	RunE: runSkillInstall,
}

var flagSkillForce bool

// skillRegistryURL can be overridden for testing.
var skillRegistryURL = skillpkg.DefaultRegistryURL

func init() {
	skillInstallCmd.Flags().BoolVar(&flagSkillForce, "force", false, "Force install even if already installed")
	skillCmd.AddCommand(skillInstallCmd)
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
