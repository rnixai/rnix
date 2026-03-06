package ui

import (
	"github.com/charmbracelet/lipgloss"
)

// Color constants for the Rnix design system.
const (
	ColorKernel  = "#888888"
	ColorAgent   = "#5B9BD5"
	ColorSuccess = "#6BCB77"
	ColorWarning = "#FFD93D"
	ColorError   = "#FF6B6B"
	ColorMuted   = "#666666"
)

// Style set for styled terminal output.
var (
	KernelStyle    lipgloss.Style
	AgentStyle     lipgloss.Style
	AgentBoldStyle lipgloss.Style // AgentStyle + Bold, pre-computed for FormatTraceLine
	SuccessStyle   lipgloss.Style
	ErrorStyle     lipgloss.Style
	WarningStyle   lipgloss.Style
	MutedStyle     lipgloss.Style
	BoldStyle      lipgloss.Style
)

// InitStyles initializes the lipgloss style set based on the terminal profile.
// ColorLevel 0 means no color; all styles degrade to plain text.
func InitStyles(profile TerminalProfile) {
	if profile.ColorLevel == 0 {
		KernelStyle = lipgloss.NewStyle()
		AgentStyle = lipgloss.NewStyle()
		AgentBoldStyle = lipgloss.NewStyle()
		SuccessStyle = lipgloss.NewStyle()
		ErrorStyle = lipgloss.NewStyle()
		WarningStyle = lipgloss.NewStyle()
		MutedStyle = lipgloss.NewStyle()
		BoldStyle = lipgloss.NewStyle().Bold(true)
		return
	}

	KernelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorKernel))
	AgentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorAgent))
	AgentBoldStyle = AgentStyle.Bold(true)
	SuccessStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorSuccess))
	ErrorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorError))
	WarningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorWarning))
	MutedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorMuted))
	BoldStyle = lipgloss.NewStyle().Bold(true)
}
